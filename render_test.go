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
