package lsp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatLocations_Empty(t *testing.T) {
	t.Parallel()
	out := FormatLocations(nil, "/cwd", 1)
	assert.Equal(t, "No results found.", out)
}

func TestFormatLocations_Single(t *testing.T) {
	t.Parallel()
	locs := []Location{
		{
			URI: pathToURI("/cwd/foo.go"),
			Range: Range{
				Start: Position{Line: 9, Character: 0},
				End:   Position{Line: 9, Character: 5},
			},
		},
	}
	out := FormatLocations(locs, "/cwd", 0)
	assert.Contains(t, out, "foo.go:10")
}

func TestFormatHover_Nil(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "No hover information available.", FormatHover(nil))
}

func TestFormatHover_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "No hover information available.", FormatHover(&HoverResult{}))
}

func TestFormatHover_Content(t *testing.T) {
	t.Parallel()
	h := &HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "func Foo() int"}}
	assert.Equal(t, "func Foo() int", FormatHover(h))
}

func TestFormatSymbols_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "No symbols found.", FormatSymbols(nil, "/cwd"))
}

func TestFormatSymbols_Flat(t *testing.T) {
	t.Parallel()
	syms := []DocumentSymbol{
		{
			Name:  "Foo",
			Kind:  SymbolKindFunction,
			Range: Range{Start: Position{Line: 4}},
		},
		{
			Name:  "Bar",
			Kind:  SymbolKindStruct,
			Range: Range{Start: Position{Line: 9}},
		},
	}
	out := FormatSymbols(syms, "/cwd")
	assert.Contains(t, out, "Foo")
	assert.Contains(t, out, "line 5")
	assert.Contains(t, out, "Bar")
	assert.Contains(t, out, "line 10")
}

func TestFormatSymbols_Nested(t *testing.T) {
	t.Parallel()
	child := DocumentSymbol{
		Name:  "field",
		Kind:  SymbolKindField,
		Range: Range{Start: Position{Line: 2}},
	}
	parent := DocumentSymbol{
		Name:     "MyStruct",
		Kind:     SymbolKindStruct,
		Range:    Range{Start: Position{Line: 0}},
		Children: []DocumentSymbol{child},
	}
	out := FormatSymbols([]DocumentSymbol{parent}, "/cwd")
	lines := strings.Split(out, "\n")
	// parent should be at indent 0, child at indent 2.
	assert.True(t, strings.HasPrefix(lines[0], "struct MyStruct"))
	assert.True(t, strings.HasPrefix(lines[1], "  field"))
}

func TestSymbolKind_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "function", SymbolKindFunction.String())
	assert.Equal(t, "struct", SymbolKindStruct.String())
	assert.Equal(t, "unknown", SymbolKind(99).String())
}
