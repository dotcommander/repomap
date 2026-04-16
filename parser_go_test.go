package repomap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstSentence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		comment string // empty means nil CommentGroup
		ident   string
		want    string
	}{
		{
			name:    "strip prefix and take first sentence",
			comment: "Foo returns the widget count.",
			ident:   "Foo",
			want:    "returns the widget count",
		},
		{
			name:    "too short after prefix strip",
			comment: "Foo",
			ident:   "Foo",
			want:    "",
		},
		{
			name:    "empty comment",
			comment: "",
			ident:   "Foo",
			want:    "",
		},
		{
			name:    "nil comment group",
			comment: "", // handled via nilCG flag below
			ident:   "Foo",
			want:    "",
		},
		{
			name:    "truncate at 60 runes",
			comment: "Foo " + strings.Repeat("x", 80),
			ident:   "Foo",
			want:    strings.Repeat("x", 60),
		},
		{
			name:    "unicode rune boundary",
			comment: "Foo returns 你好世界 from the server.",
			ident:   "Foo",
			want:    "returns 你好世界 from the server",
		},
		{
			name:    "multi-line takes first line only",
			comment: "Foo does X.\nIt also does Y.",
			ident:   "Foo",
			want:    "does X",
		},
		{
			name:    "no prefix match uses full first sentence",
			comment: "Returns widgets.",
			ident:   "Foo",
			want:    "Returns widgets",
		},
		{
			name:    "newline separator before period",
			comment: "Foo does X\nIt also does Y.",
			ident:   "Foo",
			want:    "does X",
		},
	}

	// nil CommentGroup case
	t.Run("nil comment group", func(t *testing.T) {
		t.Parallel()
		got := firstSentence(nil, "Foo")
		assert.Equal(t, "", got)
	})

	for _, tc := range cases {
		if tc.name == "nil comment group" {
			continue // handled above
		}
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var cg *ast.CommentGroup
			if tc.comment != "" {
				// Build a synthetic CommentGroup from the raw text.
				// We parse a Go file snippet so the AST comment is real.
				src := "package p\n\n// " + strings.ReplaceAll(tc.comment, "\n", "\n// ") + "\nfunc " + tc.ident + "() {}\n"
				fset := token.NewFileSet()
				f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
				require.NoError(t, err)
				for _, d := range f.Decls {
					if fn, ok := d.(*ast.FuncDecl); ok && fn.Doc != nil {
						cg = fn.Doc
						break
					}
				}
			}
			got := firstSentence(cg, tc.ident)
			assert.Equal(t, tc.want, got)
		})
	}
}

// writeGoFixture is a helper that writes a Go source file and a minimal go.mod
// to a temp directory, returning the temp dir and absolute file path.
func writeGoFixture(t *testing.T, filename, src string) (dir, goFile string) {
	t.Helper()
	dir = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0o644))
	goFile = filepath.Join(dir, filename)
	require.NoError(t, os.WriteFile(goFile, []byte(src), 0o644))
	return dir, goFile
}

func TestParseGoFile_UnexportedFallback_Triggers(t *testing.T) {
	t.Parallel()

	// Three unexported functions: one short (3-line body), two long (8-line bodies).
	src := `package internal

func tinyHelper() {
	_ = 1
	_ = 2
}

func runServer(addr string) error {
	_ = addr
	_ = addr
	_ = addr
	_ = addr
	_ = addr
	_ = addr
	return nil
}

func handleRequest(req string) string {
	_ = req
	_ = req
	_ = req
	_ = req
	_ = req
	_ = req
	return req
}
`
	dir, goFile := writeGoFixture(t, "internal.go", src)

	fs, err := ParseGoFile(goFile, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)

	// Only the two >=5-line functions should appear; tinyHelper (3-line body) is excluded.
	require.Len(t, fs.Symbols, 2)
	names := map[string]bool{}
	for _, s := range fs.Symbols {
		names[s.Name] = true
		assert.False(t, s.Exported, "fallback symbol must have Exported=false")
	}
	assert.True(t, names["runServer"], "runServer should be present")
	assert.True(t, names["handleRequest"], "handleRequest should be present")
}

func TestParseGoFile_UnexportedFallback_SuppressedWhenExportedExists(t *testing.T) {
	t.Parallel()

	// One exported function plus two unexported large helpers.
	src := `package mypkg

func Foo() {}

func helperA() error {
	_ = 1
	_ = 2
	_ = 3
	_ = 4
	_ = 5
	return nil
}

func helperB() string {
	_ = ""
	_ = ""
	_ = ""
	_ = ""
	_ = ""
	return ""
}
`
	dir, goFile := writeGoFixture(t, "mypkg.go", src)

	fs, err := ParseGoFile(goFile, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)

	// Fallback must NOT fire because the exported pass already found Foo.
	require.Len(t, fs.Symbols, 1)
	assert.Equal(t, "Foo", fs.Symbols[0].Name)
	assert.True(t, fs.Symbols[0].Exported)
}

func TestParseGoFile_UnexportedFallback_SkipsTrivialHelpers(t *testing.T) {
	t.Parallel()

	// Only 1-4 line unexported functions — all below threshold.
	src := `package internal

func a() { _ = 1 }

func b() error {
	_ = 1
	return nil
}

func c() string {
	_ = ""
	_ = ""
	return ""
}
`
	dir, goFile := writeGoFixture(t, "trivial.go", src)

	fs, err := ParseGoFile(goFile, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)

	assert.Empty(t, fs.Symbols, "all functions below threshold: no symbols expected")
}

func TestParseGoFile_UnexportedFallback_IncludesMethods(t *testing.T) {
	t.Parallel()

	// Unexported method on an unexported type with a >=5-line body.
	src := `package internal

type server struct{ addr string }

func (s *server) run() error {
	_ = s.addr
	_ = s.addr
	_ = s.addr
	_ = s.addr
	_ = s.addr
	return nil
}
`
	dir, goFile := writeGoFixture(t, "server.go", src)

	fs, err := ParseGoFile(goFile, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)

	require.Len(t, fs.Symbols, 1)
	sym := fs.Symbols[0]
	assert.Equal(t, "run", sym.Name)
	assert.Equal(t, "method", sym.Kind)
	assert.False(t, sym.Exported)
	assert.NotEmpty(t, sym.Receiver, "Receiver must be set for methods")
}

func TestParseGoFile_UnexportedFallback_PopulatesDoc(t *testing.T) {
	t.Parallel()

	// Unexported function with a doc comment.
	src := `package internal

// runServer starts the HTTP server on the given address and blocks.
func runServer(addr string) error {
	_ = addr
	_ = addr
	_ = addr
	_ = addr
	_ = addr
	return nil
}
`
	dir, goFile := writeGoFixture(t, "server.go", src)

	fs, err := ParseGoFile(goFile, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)

	require.Len(t, fs.Symbols, 1)
	assert.NotEmpty(t, fs.Symbols[0].Doc, "Doc must be populated from the doc comment")
}

func TestParseGoFile_UnexportedFallback_MainInitPath(t *testing.T) {
	t.Parallel()

	// package main: main() and init() are captured by the hardcoded path;
	// the fallback must NOT double-add them.
	src := `package main

func main() {
	_ = 1
	_ = 2
	_ = 3
	_ = 4
	_ = 5
}

func init() {
	_ = 1
	_ = 2
	_ = 3
	_ = 4
	_ = 5
}

func helper() {
	_ = 1
	_ = 2
	_ = 3
	_ = 4
	_ = 5
}
`
	dir, goFile := writeGoFixture(t, "main.go", src)

	fs, err := ParseGoFile(goFile, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)

	// main() and init() should be captured by the hardcoded path (Symbols non-empty),
	// so the fallback must NOT fire and helper must NOT appear.
	names := map[string]bool{}
	for _, s := range fs.Symbols {
		names[s.Name] = true
	}
	assert.True(t, names["main"], "main must be present via hardcoded path")
	assert.True(t, names["init"], "init must be present via hardcoded path")
	assert.False(t, names["helper"], "helper must NOT appear — fallback suppressed by main/init")
	// Total: exactly main + init (helper filtered by fallback suppression).
	assert.Len(t, fs.Symbols, 2)
}

func TestParseGoFile_DocExtraction(t *testing.T) {
	t.Parallel()

	src := `package mypkg

// ProcessBatch applies the batch rules to items and returns an error if any fail.
func ProcessBatch(items []string) error { return nil }

// Config holds the application configuration.
type Config struct {
	Name string
}

// DefaultTimeout is the default timeout duration.
const DefaultTimeout = 30
`
	dir := t.TempDir()
	// Need a git repo for ScanFiles; for ParseGoFile we just need a go.mod.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0o644))
	goFile := filepath.Join(dir, "myfile.go")
	require.NoError(t, os.WriteFile(goFile, []byte(src), 0o644))

	fs, err := ParseGoFile(goFile, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)

	byName := make(map[string]Symbol)
	for _, s := range fs.Symbols {
		byName[s.Name] = s
	}

	// Function doc: strip prefix "ProcessBatch ", truncated at 60 runes.
	// Full sentence: "applies the batch rules to items and returns an error if any fail"
	// = 65 runes → truncated to 60.
	fn, ok := byName["ProcessBatch"]
	require.True(t, ok, "ProcessBatch not found")
	assert.Equal(t, "applies the batch rules to items and returns an error if any", fn.Doc)

	// Type doc: strip prefix "Config "
	cfg, ok := byName["Config"]
	require.True(t, ok, "Config not found")
	assert.Equal(t, "holds the application configuration", cfg.Doc)

	// Const doc: strip prefix "DefaultTimeout "
	dt, ok := byName["DefaultTimeout"]
	require.True(t, ok, "DefaultTimeout not found")
	assert.Equal(t, "is the default timeout duration", dt.Doc)
}
