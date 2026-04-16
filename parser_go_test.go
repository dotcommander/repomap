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
