package repomap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatXML_Basic(t *testing.T) {
	t.Parallel()

	files := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "cmd/main.go",
				Language: "go",
				Package:  "main",
				Symbols: []Symbol{
					{Name: "Run", Kind: "function", Exported: true, Line: 10, Signature: "() error"},
					{Name: "Config", Kind: "struct", Exported: true, Line: 5, EndLine: 8, Signature: "{Name, Age}"},
				},
			},
			Tag:   "entry",
			Score: 50,
		},
		{
			FileSymbols: &FileSymbols{
				Path:     "core/handler.go",
				Language: "go",
				Package:  "core",
				Symbols: []Symbol{
					{Name: "Handle", Kind: "method", Exported: true, Line: 20, Receiver: "*Server", Signature: "(r *http.Request)"},
				},
			},
			Score:      30,
			ImportedBy: 3,
			DependsOn:  2,
		},
	}

	out := FormatXML(files, 0)

	assert.Contains(t, out, `<?xml version="1.0" encoding="UTF-8"?>`)
	assert.Contains(t, out, `<repomap files="2" symbols="3">`)
	assert.Contains(t, out, `</repomap>`)
	assert.Contains(t, out, `path="cmd/main.go"`)
	assert.Contains(t, out, `tag="entry"`)
	assert.Contains(t, out, `path="core/handler.go"`)
	assert.Contains(t, out, `imported-by="3"`)
	assert.Contains(t, out, `imports="2"`)
	assert.Contains(t, out, `name="Handle" kind="method"`)
	assert.Contains(t, out, `receiver="*Server"`)
	assert.Contains(t, out, `(r *http.Request)</sym>`, "signature should be in element body")
	assert.Contains(t, out, `name="Config" kind="struct"`)
	assert.Contains(t, out, `{Name, Age}</sym>`)
	assert.Contains(t, out, `span="4"`, "Config spans lines 5-8")
}

func TestFormatXML_Empty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", FormatXML(nil, 0))
	assert.Equal(t, "", FormatXML([]RankedFile{}, 0))
}

func TestFormatXML_XMLEscaping(t *testing.T) {
	t.Parallel()

	files := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "web/router.go",
				Language: "go",
				Symbols: []Symbol{
					{Name: "Handle", Kind: "function", Signature: "(x List<Pair<int, string>>)"},
				},
			},
		},
	}

	out := FormatXML(files, 0)
	// The signature "(x List<Pair<int, string>>)" must have < and > escaped.
	assert.Contains(t, out, "&lt;int, string&gt;", "angle brackets must be escaped")
}

func TestFormatXML_EmptyFile(t *testing.T) {
	t.Parallel()

	files := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "util/helpers.go",
				Language: "go",
				Package:  "util",
			},
			Score: 5,
		},
	}

	out := FormatXML(files, 0)
	// File with no symbols should use self-closing tag.
	assert.Contains(t, out, `<file `, "must contain file element")
	assert.Contains(t, out, `/>`, "no-symbols file should be self-closing")
}

func TestFormatXML_BudgetTruncation(t *testing.T) {
	t.Parallel()

	makeFile := func(path string, score int, n int) RankedFile {
		syms := make([]Symbol, n)
		for i := range syms {
			syms[i] = Symbol{Name: strings.ToUpper(string(rune('A' + i))), Kind: "function"}
		}
		return RankedFile{
			FileSymbols: &FileSymbols{Path: path, Symbols: syms},
			Score:       score,
		}
	}

	files := []RankedFile{
		makeFile("a.go", 10, 5),
		makeFile("b.go", 5, 5),
		makeFile("c.go", 1, 5),
	}

	// With budget, some files should be omitted.
	out := FormatXML(files, 100)
	assert.Contains(t, out, `<repomap files="3"`, "header reports total files")
	// At least one file must be present.
	assert.Contains(t, out, `<file `, "at least one file must appear")
}

func TestFormatXML_UntestedDiagnostic(t *testing.T) {
	t.Parallel()

	files := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "core/handler.go",
				Language: "go",
				Symbols: []Symbol{
					{Name: "Handle", Kind: "function", Exported: true},
				},
			},
			Untested: true,
		},
	}

	out := FormatXML(files, 0)
	assert.Contains(t, out, `untested="true"`)
}

func TestFormatXML_Dependencies(t *testing.T) {
	t.Parallel()

	files := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:       "cmd/main.go",
				Language:   "go",
				ImportPath: "myapp/cmd",
				Imports:    []string{"myapp/core"},
			},
		},
		{
			FileSymbols: &FileSymbols{
				Path:       "core/handler.go",
				Language:   "go",
				ImportPath: "myapp/core",
				Imports:    []string{"myapp/util"},
			},
		},
		{
			FileSymbols: &FileSymbols{
				Path:       "util/helpers.go",
				Language:   "go",
				ImportPath: "myapp/util",
			},
		},
	}

	out := FormatXML(files, 0)
	assert.Contains(t, out, "<dependencies>")
	assert.Contains(t, out, "</dependencies>")
	assert.Contains(t, out, `myapp/cmd`)
	assert.Contains(t, out, `myapp/core`)
}

func TestFormatXML_NoDependencies(t *testing.T) {
	t.Parallel()

	files := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "main.py",
				Language: "python",
				Symbols:  []Symbol{{Name: "main", Kind: "function"}},
			},
		},
	}

	out := FormatXML(files, 0)
	assert.NotContains(t, out, "<dependencies>", "non-Go project should have no dependency section")
}

func TestFormatXML_SymbolNoSignature(t *testing.T) {
	t.Parallel()

	files := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "main.go",
				Language: "go",
				Symbols: []Symbol{
					{Name: "ErrNotFound", Kind: "constant", Exported: true, Line: 15},
				},
			},
		},
	}

	out := FormatXML(files, 0)
	assert.Contains(t, out, `name="ErrNotFound" kind="constant"`)
	// No signature means self-closing sym tag.
	assert.Contains(t, out, `/>`)
}
