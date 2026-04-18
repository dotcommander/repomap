package repomap

import (
	"encoding/json"
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

	sym := Symbol{
		Name:      "Run",
		Kind:      "function",
		Exported:  true,
		Line:      10,
		Signature: "() error",
		Doc:       "starts the main server loop",
	}
	files := []RankedFile{makeRankedFile("cmd/main.go", 2, []Symbol{sym})}

	t.Run("detail=true verbose=true emits subtitle", func(t *testing.T) {
		t.Parallel()
		out := FormatMap(files, 0, true, true)
		assert.Contains(t, out, "// starts the main server loop")
	})

	t.Run("detail=false verbose=true no subtitle", func(t *testing.T) {
		t.Parallel()
		out := FormatMap(files, 0, true, false)
		assert.NotContains(t, out, "//")
	})

	t.Run("default mode emits subtitle", func(t *testing.T) {
		t.Parallel()
		out := FormatMap(files, 0, false, false)
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

	out := FormatXML(files, 0)
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

	out := FormatXML(files, 0)
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
			name: "struct with compact field signature rendered inline",
			syms: []Symbol{
				{Name: "Config", Kind: "struct", Signature: "{Host, Port}", Exported: true},
			},
			// Signature != "" and != "{}", so rendered inline: "  type Config{Host, Port}"
			shouldContain:    []string{"type Config", "{Host, Port}"},
			shouldNotContain: nil,
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
