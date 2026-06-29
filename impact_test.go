package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImpactReportsLocalFacts(t *testing.T) {
	t.Parallel()

	m := New("/repo", DefaultConfig())
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:        "internal/auth/token.go",
				Language:    "go",
				Package:     "auth",
				ImportPath:  "example.com/app/internal/auth",
				ParseMethod: "go_ast",
				Imports:     []string{"sync"},
				Symbols: []Symbol{
					{Name: "RefreshToken", Kind: "function", Exported: true},
					{Name: "helper", Kind: "function", Exported: false},
				},
			},
			Score:           30,
			ScoreComponents: map[string]int{scoreComponentSymbols: 1, scoreComponentImports: 20},
			Boundaries:      []string{"HTTP"},
		},
		{
			FileSymbols: &FileSymbols{
				Path:       "internal/http/middleware.go",
				Language:   "go",
				Package:    "http",
				ImportPath: "example.com/app/internal/http",
				Imports:    []string{"example.com/app/internal/auth"},
			},
		},
		{
			FileSymbols: &FileSymbols{
				Path:       "internal/auth/token_test.go",
				Language:   "go",
				Package:    "auth",
				ImportPath: "example.com/app/internal/auth",
				Symbols:    []Symbol{{Name: "TestRefreshToken", Kind: "function"}},
			},
		},
	}

	impact, err := m.Impact("internal/auth/token.go")
	require.NoError(t, err)
	assert.Equal(t, "internal/auth/token.go", impact.File.Path)
	assert.Equal(t, "go_ast", impact.ParseMethod)
	assert.Equal(t, []string{"sync"}, impact.Imports)
	assert.Equal(t, []string{"internal/http/middleware.go"}, impact.ImportedBy)
	assert.Equal(t, []string{"internal/auth/token_test.go"}, impact.Tests)
	require.Len(t, impact.ExportedSymbols, 1)
	assert.Equal(t, "RefreshToken", impact.ExportedSymbols[0].Name)
	assert.Equal(t, []string{"HTTP"}, impact.Boundaries)
	assert.Equal(t, 20, impact.ScoreComponents[scoreComponentImports])
	assert.Equal(t, "medium", impact.RiskLevel)
	assert.Equal(t, []string{"auth", "http"}, impact.AffectedPackages)
	assert.Contains(t, impact.CheckNext, "inspect importer internal/http/middleware.go")
	assert.Equal(t, []string{"go test ./internal/auth"}, impact.LikelyTestCommands)
	require.NotEmpty(t, impact.ReadNext)
	assert.Equal(t, "internal/auth/token.go", impact.ReadNext[0].File)
	assert.Equal(t, "inspect target file", impact.ReadNext[0].Reason)
}
