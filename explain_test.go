package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplainReportsScoreAndDetailEvidence(t *testing.T) {
	t.Parallel()

	m := New("/repo", Config{MaxTokens: 2048})
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:        "ranker.go",
				Language:    "go",
				ParseMethod: "go_ast",
				Symbols:     []Symbol{{Name: "RankFiles", Kind: "function", Exported: true}},
			},
			Score:           13,
			ScoreComponents: map[string]int{scoreComponentSymbols: 3, scoreComponentImports: 10},
		},
	}

	got, err := m.Explain("ranker.go")
	require.NoError(t, err)
	assert.Equal(t, "ranker.go", got.File.Path)
	assert.Equal(t, 13, got.Score)
	assert.Equal(t, 13, got.ComponentTotal)
	assert.Equal(t, 3, got.ScoreComponents[scoreComponentSymbols])
	assert.Equal(t, "go_ast", got.File.ParseMethod)
	assert.NotEqual(t, -1, got.DetailLevel)
}
