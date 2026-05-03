package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarkDeadExports(t *testing.T) {
	t.Parallel()

	t.Run("ImportedBy zero marks all exported symbols Dead", func(t *testing.T) {
		t.Parallel()
		ranked := []RankedFile{
			{
				FileSymbols: &FileSymbols{
					Path: "pkg/handler.go",
					Symbols: []Symbol{
						{Name: "Handle", Kind: "function", Exported: true},
						{Name: "Config", Kind: "struct", Exported: true},
						{Name: "helper", Kind: "function", Exported: false},
					},
				},
				ImportedBy: 0,
			},
		}
		markDeadExports(ranked)

		assert.True(t, ranked[0].Symbols[0].Dead, "exported func in unimported file must be Dead")
		assert.True(t, ranked[0].Symbols[1].Dead, "exported struct in unimported file must be Dead")
		assert.False(t, ranked[0].Symbols[2].Dead, "unexported symbol must never be marked Dead")
	})

	t.Run("ImportedBy greater than zero leaves symbols alive", func(t *testing.T) {
		t.Parallel()
		ranked := []RankedFile{
			{
				FileSymbols: &FileSymbols{
					Path: "pkg/server.go",
					Symbols: []Symbol{
						{Name: "Serve", Kind: "function", Exported: true},
						{Name: "Listen", Kind: "method", Exported: true},
					},
				},
				ImportedBy: 3,
			},
		}
		markDeadExports(ranked)

		assert.False(t, ranked[0].Symbols[0].Dead, "exported func in imported file must not be Dead")
		assert.False(t, ranked[0].Symbols[1].Dead, "exported method in imported file must not be Dead")
	})

	t.Run("ImportedBy zero with no exported symbols has no Dead marks", func(t *testing.T) {
		t.Parallel()
		ranked := []RankedFile{
			{
				FileSymbols: &FileSymbols{
					Path: "pkg/internal.go",
					Symbols: []Symbol{
						{Name: "helper", Kind: "function", Exported: false},
						{Name: "process", Kind: "function", Exported: false},
					},
				},
				ImportedBy: 0,
			},
		}
		markDeadExports(ranked)

		for _, s := range ranked[0].Symbols {
			assert.False(t, s.Dead, "unexported-only file must have no Dead marks")
		}
	})
}
