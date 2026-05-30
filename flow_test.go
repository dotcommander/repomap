package repomap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlowSpineNamesEntry(t *testing.T) {
	t.Parallel()
	files := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "cmd/app/main.go",
				Language: "go",
				Symbols:  []Symbol{{Name: "main", Kind: "function"}},
			},
			Score: 50,
			Tag:   "entry",
		},
		{
			FileSymbols: &FileSymbols{
				Path:     "handler.go",
				Language: "go",
				Symbols:  []Symbol{{Name: "Handle", Kind: "function", Exported: true}},
			},
			Score: 10,
		},
	}

	out := formatFlowSpine(files)
	assert.Contains(t, out, "### Flow")
	assert.Contains(t, out, "entry: cmd/app/main.go")
	assert.True(t, strings.Contains(out, "spine: handler.go"),
		"expected spine line naming handler.go, got: %q", out)
}

func TestFlowSpineNoEntryReturnsEmpty(t *testing.T) {
	t.Parallel()
	files := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "handler.go",
				Language: "go",
				Symbols:  []Symbol{{Name: "Handle", Kind: "function", Exported: true}},
			},
			Score: 10,
		},
	}

	assert.Equal(t, "", formatFlowSpine(files))
}
