package repomap

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStructuredOutputIncludesMachineReadableFields(t *testing.T) {
	t.Parallel()

	m := New("/repo", Config{MaxTokens: 2048, Intent: "token refresh", ConsumedPaths: []string{"auth/session.go"}})
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:        "auth/token.go",
				Language:    "go",
				Package:     "auth",
				ImportPath:  "example.com/app/auth",
				ParseMethod: "go_ast",
				Imports:     []string{"sync"},
				Symbols: []Symbol{
					{Name: "RefreshToken", Kind: "function", Exported: true, Line: 12, Signature: "() error"},
				},
			},
			Score:           42,
			ScoreComponents: map[string]int{scoreComponentSymbols: 1, scoreComponentIntent: 41},
			ImportedBy:      2,
			DependsOn:       1,
			DetailLevel:     2,
		},
	}

	out := m.StructuredOutput()
	require.Len(t, out.Files, 1)
	assert.Equal(t, 1, out.SchemaVersion)
	assert.Equal(t, "/repo", out.Root)
	assert.Equal(t, "token refresh", out.Config.Intent)
	assert.Equal(t, []string{"auth/session.go"}, out.Config.ConsumedPaths)
	assert.Equal(t, 1, out.Totals.Files)
	assert.Equal(t, 1, out.Totals.Symbols)
	assert.Equal(t, "auth/token.go", out.Files[0].Path)
	assert.Equal(t, "go_ast", out.Files[0].ParseMethod)
	assert.Equal(t, 42, out.Files[0].Score)
	assert.Equal(t, 41, out.Files[0].ScoreComponents[scoreComponentIntent])
	assert.Equal(t, "RefreshToken", out.Files[0].Symbols[0].Name)

	data, err := m.StructuredJSON()
	require.NoError(t, err)
	var decoded StructuredOutput
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, out.Files[0].Path, decoded.Files[0].Path)
}

func TestIntentTokenizeDropsWorkflowStopwords(t *testing.T) {
	t.Parallel()

	tokens := tokenizeIntent("fix bug issue support token refresh cleanup")
	assert.Equal(t, []string{"token", "refresh"}, tokens)
}

func TestStructuredOutputIncludesBudgetOmittedReason(t *testing.T) {
	t.Parallel()

	m := New("/repo", Config{MaxTokens: 1})
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:    "very/long/path/that/will/not/fit.go",
				Symbols: []Symbol{{Name: "Run", Kind: "function", Exported: true}},
			},
			Score:           1,
			ScoreComponents: map[string]int{scoreComponentSymbols: 1},
		},
	}

	out := m.StructuredOutput()
	require.Len(t, out.Files, 1)
	assert.Equal(t, -1, out.Files[0].DetailLevel)
	assert.Equal(t, "budget", out.Files[0].OmittedReason)
}
