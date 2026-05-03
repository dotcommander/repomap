package repomap

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeRankedFile builds a minimal RankedFile for rendering tests.
func makeRankedFile(path string, detailLevel int, syms []Symbol) RankedFile {
	return RankedFile{
		FileSymbols: &FileSymbols{
			Path:     path,
			Language: "go",
			Package:  "mypkg",
			Symbols:  syms,
		},
		DetailLevel: detailLevel,
		Score:       10,
	}
}

func TestDocSubtitleRendering_DetailFormat(t *testing.T) {
	t.Parallel()

	sym := Symbol{
		Name:      "ProcessBatch",
		Kind:      "function",
		Exported:  true,
		Line:      5,
		Signature: "(items []string) error",
		Doc:       "applies the batch rules to items",
	}

	t.Run("formatFileBlockDetail emits subtitle", func(t *testing.T) {
		t.Parallel()
		f := makeRankedFile("core/batch.go", 2, []Symbol{sym})
		out := formatFileBlockDetail(f)
		assert.Contains(t, out, "ProcessBatch")
		assert.Contains(t, out, "// applies the batch rules to items")
	})

	t.Run("formatFileBlockDetail no doc means no subtitle line", func(t *testing.T) {
		t.Parallel()
		noDoc := sym
		noDoc.Doc = ""
		f := makeRankedFile("core/batch.go", 2, []Symbol{noDoc})
		out := formatFileBlockDetail(f)
		assert.Contains(t, out, "ProcessBatch")
		assert.NotContains(t, out, "//")
	})

	t.Run("formatFileBlockVerbose does not emit subtitle", func(t *testing.T) {
		t.Parallel()
		f := makeRankedFile("core/batch.go", 2, []Symbol{sym})
		out := formatFileBlockVerbose(f)
		assert.Contains(t, out, "ProcessBatch")
		assert.NotContains(t, out, "//")
	})

	t.Run("formatFileBlockCompact does not emit subtitle", func(t *testing.T) {
		t.Parallel()
		f := makeRankedFile("core/batch.go", 2, []Symbol{sym})
		out := formatFileBlockCompact(f, nil)
		assert.NotContains(t, out, "//")
	})
}

func TestDocSubtitleRendering_FormatMap(t *testing.T) {
	t.Parallel()

	// sym is read-only — safe to share. Each subtest builds its own files slice
	// because FormatMap → BudgetFiles mutates RankedFile.DetailLevel in-place.
	sym := Symbol{
		Name:      "Run",
		Kind:      "function",
		Exported:  true,
		Line:      10,
		Signature: "() error",
		Doc:       "starts the main server loop",
	}

	t.Run("detail=true verbose=true emits subtitle", func(t *testing.T) {
		t.Parallel()
		files := []RankedFile{makeRankedFile("cmd/main.go", 2, []Symbol{sym})}
		out := FormatMap(files, 0, true, true, nil)
		assert.Contains(t, out, "// starts the main server loop")
	})

	t.Run("detail=false verbose=true no subtitle", func(t *testing.T) {
		t.Parallel()
		files := []RankedFile{makeRankedFile("cmd/main.go", 2, []Symbol{sym})}
		out := FormatMap(files, 0, true, false, nil)
		assert.NotContains(t, out, "//")
	})

	t.Run("default mode emits subtitle", func(t *testing.T) {
		t.Parallel()
		files := []RankedFile{makeRankedFile("cmd/main.go", 2, []Symbol{sym})}
		out := FormatMap(files, 0, false, false, nil)
		assert.Contains(t, out, "// starts the main server loop")
	})
}

func TestDocSubtitleRendering_XML(t *testing.T) {
	t.Parallel()

	sym := Symbol{
		Name:      "Handle",
		Kind:      "method",
		Exported:  true,
		Receiver:  "*Server",
		Line:      20,
		Signature: "(r *http.Request)",
		Doc:       "processes incoming HTTP requests",
	}
	files := []RankedFile{makeRankedFile("server/handler.go", 2, []Symbol{sym})}

	out := FormatXML(files, 0, nil)
	assert.Contains(t, out, `doc="processes incoming HTTP requests"`)
}

func TestDocSubtitleRendering_XMLNoDocNoAttr(t *testing.T) {
	t.Parallel()

	sym := Symbol{
		Name:     "Handle",
		Kind:     "method",
		Exported: true,
		Receiver: "*Server",
		Line:     20,
	}
	files := []RankedFile{makeRankedFile("server/handler.go", 2, []Symbol{sym})}

	out := FormatXML(files, 0, nil)
	assert.NotContains(t, out, `doc=`)
}

func TestCacheBackwardCompatibility_DocField(t *testing.T) {
	t.Parallel()

	// Simulate an old cache entry without the doc field.
	oldJSON := `{"name":"Process","kind":"function","exported":true,"line":5}`
	var sym Symbol
	require.NoError(t, json.Unmarshal([]byte(oldJSON), &sym))
	assert.Equal(t, "Process", sym.Name)
	assert.Equal(t, "", sym.Doc, "Doc should be empty string when absent in old JSON")
}

// TestFormatFileBlockDefault covers the enriched default renderer's core behaviours:
// signatures, docs, method receivers, annotation tags, and unexported-symbol exclusion.
func TestFormatFileBlockDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		syms             []Symbol
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name: "exported function with signature",
			syms: []Symbol{
				{Name: "Run", Kind: "function", Signature: "(ctx context.Context) error", Exported: true},
			},
			shouldContain:    []string{"func Run", "(ctx context.Context) error"},
			shouldNotContain: nil,
		},
		{
			name: "exported function with no signature",
			syms: []Symbol{
				{Name: "Init", Kind: "function", Signature: "", Exported: true},
			},
			// Should render the name but no signature parens.
			shouldContain:    []string{"func Init"},
			shouldNotContain: []string{"()"},
		},
		{
			name: "exported function with empty-parens signature",
			syms: []Symbol{
				{Name: "Reset", Kind: "function", Signature: "()", Exported: true},
			},
			shouldContain:    []string{"func Reset()"},
			shouldNotContain: nil,
		},
		{
			name: "unexported function not rendered",
			syms: []Symbol{
				{Name: "helper", Kind: "function", Signature: "(x int) bool", Exported: false},
			},
			shouldContain:    nil,
			shouldNotContain: []string{"helper"},
		},
		{
			name: "exported method with receiver",
			syms: []Symbol{
				{Name: "Run", Kind: "method", Receiver: "*Agent", Signature: "(cfg Config) error", Exported: true},
			},
			// Must contain "func (*Agent) Run" and the signature.
			shouldContain:    []string{"func (*Agent) Run", "(cfg Config) error"},
			shouldNotContain: nil,
		},
		{
			name: "annotation tag for high param count",
			syms: []Symbol{
				// ParamCount > paramThreshold (4) triggers [Np] tag.
				{Name: "Build", Kind: "function", Signature: "(a, b, c, d, e int) error", Exported: true, ParamCount: 5},
			},
			shouldContain:    []string{"Build", "[5p]"},
			shouldNotContain: nil,
		},
		{
			name: "struct with no exported fields renders name only (no field suffix for empty sigs)",
			syms: []Symbol{
				{Name: "Empty", Kind: "struct", Signature: "{}", Exported: true},
			},
			// Signature == "{}" falls through to the default branch: "  type Empty"
			shouldContain:    []string{"type Empty"},
			shouldNotContain: []string{"{}"},
		},
		{
			name: "struct with typed field signature rendered inline",
			syms: []Symbol{
				{Name: "Config", Kind: "struct", Signature: "{Host string, Port int}", Exported: true},
			},
			// Signature != "" and != "{}", so rendered inline: "  type Config{Host string, Port int}"
			shouldContain:    []string{"type Config", "{Host string, Port int}"},
			shouldNotContain: nil,
		},
		{
			// Parser emits only exported fields; unexported fields are dropped at parse time.
			// The Signature stored on the Symbol already contains only exported fields.
			// The renderer shows whatever is in Signature — this test verifies the render path.
			name: "struct signature with only exported fields rendered correctly",
			syms: []Symbol{
				{Name: "Server", Kind: "struct", Signature: "{Host string, Port int}", Exported: true},
			},
			shouldContain:    []string{"type Server", "Host string", "Port int"},
			shouldNotContain: []string{"unexported"},
		},
		{
			name: "struct with complex types in fields",
			syms: []Symbol{
				{Name: "RankedFile", Kind: "struct", Signature: "{Tags []string, Meta map[string]int, Next *Node}", Exported: true},
			},
			shouldContain:    []string{"type RankedFile", "Tags []string", "Meta map[string]int", "Next *Node"},
			shouldNotContain: nil,
		},
		{
			name: "struct with empty fields: no field suffix",
			syms: []Symbol{
				{Name: "Empty", Kind: "struct", Signature: "{}", Exported: true},
			},
			// Signature == "{}" → default branch: "  type Empty" (no field block appended)
			shouldContain:    []string{"type Empty"},
			shouldNotContain: []string{"{}"},
		},
		{
			name: "type alias rendered with type keyword",
			syms: []Symbol{
				{Name: "ID", Kind: "type", Signature: "", Exported: true},
			},
			shouldContain:    []string{"type ID"},
			shouldNotContain: nil,
		},
		{
			name: "const rendered with const keyword",
			syms: []Symbol{
				{Name: "DefaultTimeout", Kind: "constant", Signature: "", Exported: true},
			},
			shouldContain:    []string{"const DefaultTimeout"},
			shouldNotContain: nil,
		},
		{
			name: "var rendered with var keyword",
			syms: []Symbol{
				{Name: "ErrNotFound", Kind: "variable", Signature: "", Exported: true},
			},
			shouldContain:    []string{"var ErrNotFound"},
			shouldNotContain: nil,
		},
		{
			name: "doc subtitle emitted after symbol line",
			syms: []Symbol{
				{Name: "Start", Kind: "function", Signature: "() error", Exported: true, Doc: "starts the server"},
			},
			shouldContain:    []string{"func Start() error", "// starts the server"},
			shouldNotContain: nil,
		},
		{
			name: "no doc line when doc is empty",
			syms: []Symbol{
				{Name: "Stop", Kind: "function", Signature: "() error", Exported: true, Doc: ""},
			},
			shouldContain:    []string{"func Stop() error"},
			shouldNotContain: []string{"//"},
		},
		{
			name: "unexported function with doc is not rendered at all",
			syms: []Symbol{
				{Name: "internal", Kind: "function", Signature: "()", Exported: false, Doc: "private helper"},
			},
			shouldNotContain: []string{"internal", "private helper", "//"},
		},
		{
			name: "mixed exported and unexported: only exported appear",
			syms: []Symbol{
				{Name: "Exported", Kind: "function", Signature: "()", Exported: true},
				{Name: "unexported", Kind: "function", Signature: "()", Exported: false},
			},
			shouldContain:    []string{"Exported"},
			shouldNotContain: []string{"unexported"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := makeRankedFile("pkg/test.go", 2, tc.syms)
			out := formatFileBlockDefault(f)
			for _, want := range tc.shouldContain {
				assert.Contains(t, out, want, "output should contain %q", want)
			}
			for _, skip := range tc.shouldNotContain {
				assert.NotContains(t, out, skip, "output should NOT contain %q", skip)
			}
		})
	}
}

// makeRankedFileWithLang is like makeRankedFile but allows specifying the language.
func makeRankedFileWithLang(path, lang string, detailLevel int, syms []Symbol) RankedFile {
	return RankedFile{
		FileSymbols: &FileSymbols{
			Path:     path,
			Language: lang,
			Symbols:  syms,
		},
		DetailLevel: detailLevel,
		Score:       10,
	}
}

// TestDocTag verifies that docTag returns "" for Go files and " [doc: n/a]" for all others.
func TestDocTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lang string
		want string
	}{
		{"go file: no tag", "go", ""},
		{"php file: no tag (PHPDoc extraction supported)", "php", ""},
		{"typescript file: doc n/a tag", "typescript", " [doc: n/a]"},
		{"python file: doc n/a tag", "python", " [doc: n/a]"},
		{"rust file: doc n/a tag", "rust", " [doc: n/a]"},
		{"empty/unknown language: doc n/a tag", "", " [doc: n/a]"},
		{"jsx file: doc n/a tag", "jsx", " [doc: n/a]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := makeRankedFileWithLang("file.ext", tc.lang, 2, nil)
			assert.Equal(t, tc.want, docTag(f))
		})
	}
}

// TestFormatFileBlockDefault_DocNATag verifies that the [doc: n/a] tag appears on
// non-Go file headers and is absent on Go file headers.
func TestFormatFileBlockDefault_DocNATag(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Handle", Kind: "function", Signature: "()", Exported: true}

	t.Run("go file header: no doc n/a tag", func(t *testing.T) {
		t.Parallel()
		f := makeRankedFileWithLang("server/handler.go", "go", 2, []Symbol{sym})
		out := formatFileBlockDefault(f)
		assert.Contains(t, out, "server/handler.go")
		assert.NotContains(t, out, "[doc: n/a]")
	})

	t.Run("php file header: no doc n/a tag (PHPDoc extraction supported)", func(t *testing.T) {
		t.Parallel()
		f := makeRankedFileWithLang("src/Controller.php", "php", 2, []Symbol{sym})
		out := formatFileBlockDefault(f)
		assert.Contains(t, out, "src/Controller.php")
		assert.NotContains(t, out, "[doc: n/a]")
	})

	t.Run("typescript file header: doc n/a tag present", func(t *testing.T) {
		t.Parallel()
		f := makeRankedFileWithLang("lib/utils.ts", "typescript", 2, []Symbol{sym})
		out := formatFileBlockDefault(f)
		assert.Contains(t, out, "lib/utils.ts")
		assert.Contains(t, out, "[doc: n/a]")
	})

	t.Run("empty language: doc n/a tag present", func(t *testing.T) {
		t.Parallel()
		f := makeRankedFileWithLang("misc/script", "", 2, []Symbol{sym})
		out := formatFileBlockDefault(f)
		assert.Contains(t, out, "misc/script")
		assert.Contains(t, out, "[doc: n/a]")
	})

	t.Run("no doc n/a tag on php header line (PHPDoc supported)", func(t *testing.T) {
		t.Parallel()
		f := makeRankedFileWithLang("app/router.php", "php", 2, []Symbol{sym})
		// PHP now supports PHPDoc extraction — no [doc: n/a] tag on the header line.
		line := formatFileLineDefault(f)
		assert.Contains(t, line, "app/router.php", "header line should contain file path")
		assert.NotContains(t, line, "[doc: n/a]", "PHP header must not carry doc tag")
	})

	t.Run("doc n/a tag combined with imported-by badge", func(t *testing.T) {
		t.Parallel()
		f := makeRankedFileWithLang("lib/base.rb", "ruby", 2, []Symbol{sym})
		f.ImportedBy = 3
		out := formatFileBlockDefault(f)
		// Header should contain path, imported-by badge, and doc n/a tag.
		assert.Contains(t, out, "lib/base.rb")
		assert.Contains(t, out, "imported by 3")
		assert.Contains(t, out, "[doc: n/a]")
	})
}

// TestFormatFileBlockDefault_GoDocEndToEnd verifies the full pipeline from
// parsing a Go source with a doc comment through to rendered output.
func TestFormatFileBlockDefault_GoDocEndToEnd(t *testing.T) {
	t.Parallel()

	const src = `package mypkg

// Foo returns the bar value for the given key.
func Foo(key string) int { return 0 }

// Bar is an exported type.
type Bar struct{ X int }
`

	// Write source to a temp file so ParseGoFile can read it from disk.
	dir := t.TempDir()
	srcPath := dir + "/foo.go"
	require.NoError(t, os.WriteFile(srcPath, []byte(src), 0o600))

	fs, err := ParseGoFile(srcPath, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)

	var fooDoc, barDoc string
	for _, s := range fs.Symbols {
		switch s.Name {
		case "Foo":
			fooDoc = s.Doc
		case "Bar":
			barDoc = s.Doc
		}
	}

	// Verify the parser captured doc comments.
	assert.NotEmpty(t, fooDoc, "Foo should have a doc comment")
	assert.NotEmpty(t, barDoc, "Bar should have a doc comment")

	rf := RankedFile{
		FileSymbols: fs,
		DetailLevel: 2,
		Score:       10,
	}

	out := formatFileBlockDefault(rf)

	// File header must not carry [doc: n/a] for Go.
	assert.NotContains(t, out, "[doc: n/a]")

	// Doc comment must appear beneath the symbol line.
	assert.Contains(t, out, "// "+fooDoc, "rendered output should contain Foo doc line")
	assert.Contains(t, out, "// "+barDoc, "rendered output should contain Bar doc line")
}

// TestFormatFileBlockLean covers the lean orientation renderer:
// path + exported symbol names only — no signatures, no godoc, no struct fields.
func TestFormatFileBlockLean(t *testing.T) {
	t.Parallel()

	syms := []Symbol{
		{Name: "Run", Kind: "function", Signature: "(ctx context.Context) error", Exported: true, Doc: "starts the server"},
		{Name: "Config", Kind: "struct", Signature: "{Host string, Port int}", Exported: true, Doc: "holds server config"},
		{Name: "helper", Kind: "function", Signature: "(x int) bool", Exported: false, Doc: "internal only"},
	}
	f := makeRankedFile("server/main.go", 2, syms)
	out := formatFileBlockLean(f)

	// Path header must appear.
	assert.Contains(t, out, "server/main.go")

	// Exported symbol names must appear.
	assert.Contains(t, out, "Run", "exported function name must appear")
	assert.Contains(t, out, "Config", "exported struct name must appear")

	// Unexported symbol must not appear.
	assert.NotContains(t, out, "helper", "unexported symbol must be omitted")

	// No signatures.
	assert.NotContains(t, out, "context.Context", "signature must not appear in lean mode")
	assert.NotContains(t, out, "Host string", "struct fields must not appear in lean mode")

	// No godoc.
	assert.NotContains(t, out, "//", "godoc must not appear in lean mode")
}

// TestFormatMapCompact_NamesOnly verifies FormatMapCompact renders names only (no sigs).
func TestFormatMapCompact_NamesOnly(t *testing.T) {
	t.Parallel()

	syms := []Symbol{
		{Name: "BudgetFiles", Kind: "function", Signature: "(ranked []RankedFile, maxTokens int) []RankedFile", Exported: true, Doc: "assigns detail levels"},
		{Name: "RankedFile", Kind: "struct", Signature: "{Score float64, DetailLevel int}", Exported: true},
	}
	files := []RankedFile{makeRankedFile("budget.go", 2, syms)}

	out := FormatMapCompact(files, 4096, nil)

	// Names must appear.
	assert.Contains(t, out, "BudgetFiles")
	assert.Contains(t, out, "RankedFile")

	// Signatures and docs must not appear.
	assert.NotContains(t, out, "ranked []RankedFile", "function signature must not appear in compact mode")
	assert.NotContains(t, out, "Score float64", "struct fields must not appear in compact mode")
	assert.NotContains(t, out, "assigns detail levels", "godoc must not appear in compact mode")
	assert.NotContains(t, out, "//", "no comment lines in compact mode")
}

// TestFormatMapCompact_DefaultModeUnchanged verifies FormatMap (default) still includes
// signatures while FormatMapCompact does not — regression guard for Items 1-4.
func TestFormatMapCompact_DefaultModeUnchanged(t *testing.T) {
	t.Parallel()

	syms := []Symbol{
		{Name: "New", Kind: "function", Signature: "(cfg Config) *Server", Exported: true, Doc: "creates a server"},
	}
	files := []RankedFile{makeRankedFile("server.go", 2, syms)}

	defaultOut := FormatMap(files, 0, false, false, nil)
	compactOut := FormatMapCompact(files, 4096, nil)

	// Default must include the signature.
	assert.Contains(t, defaultOut, "(cfg Config) *Server",
		"default mode must include function signature")

	// Compact must NOT include the signature.
	assert.NotContains(t, compactOut, "(cfg Config) *Server",
		"compact mode must omit function signature")

	// Both must include the symbol name.
	assert.Contains(t, defaultOut, "New")
	assert.Contains(t, compactOut, "New")
}

// TestFormatMapCompact_BudgetHonored verifies that compact mode respects the token budget.
func TestFormatMapCompact_BudgetHonored(t *testing.T) {
	t.Parallel()

	// Create a file with many symbols.
	syms := make([]Symbol, 20)
	for i := range syms {
		syms[i] = Symbol{Name: string(rune('A' + i)), Kind: "function", Signature: "(x int) error", Exported: true}
	}
	files := []RankedFile{makeRankedFile("big.go", 2, syms)}

	// Very tight budget — should still honour it (output bytes ≤ budget*4 approximately).
	const budget = 32 // tokens
	out := FormatMapCompact(files, budget, nil)

	// Output must not grossly exceed budget (allow 2x for headers/overhead).
	assert.LessOrEqual(t, len(out), budget*4*2,
		"compact output must not grossly exceed token budget")
}
