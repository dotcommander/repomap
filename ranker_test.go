package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarkDeadExports(t *testing.T) {
	t.Parallel()

	t.Run("ImportedBy zero marks all exported dead", func(t *testing.T) {
		t.Parallel()
		ranked := []RankedFile{
			{
				FileSymbols: &FileSymbols{
					Path: "dead.go",
					Symbols: []Symbol{
						{Name: "A", Kind: "function", Exported: true},
						{Name: "B", Kind: "struct", Exported: true},
						{Name: "c", Kind: "function", Exported: false},
					},
				},
				ImportedBy: 0,
			},
		}
		markDeadExports(ranked)
		assert.True(t, ranked[0].Symbols[0].Dead, "exported symbol A must be Dead")
		assert.True(t, ranked[0].Symbols[1].Dead, "exported symbol B must be Dead")
		assert.False(t, ranked[0].Symbols[2].Dead, "unexported symbol c must not be marked Dead")
	})

	t.Run("ImportedBy positive leaves exports live", func(t *testing.T) {
		t.Parallel()
		ranked := []RankedFile{
			{
				FileSymbols: &FileSymbols{
					Path: "live.go",
					Symbols: []Symbol{
						{Name: "A", Kind: "function", Exported: true},
						{Name: "B", Kind: "struct", Exported: true},
					},
				},
				ImportedBy: 3,
			},
		}
		markDeadExports(ranked)
		assert.False(t, ranked[0].Symbols[0].Dead, "imported file's exported symbols must stay live")
		assert.False(t, ranked[0].Symbols[1].Dead, "imported file's exported symbols must stay live")
	})

	t.Run("ImportedBy zero no exported symbols", func(t *testing.T) {
		t.Parallel()
		ranked := []RankedFile{
			{
				FileSymbols: &FileSymbols{
					Path: "internal.go",
					Symbols: []Symbol{
						{Name: "a", Kind: "function", Exported: false},
						{Name: "b", Kind: "function", Exported: false},
					},
				},
				ImportedBy: 0,
			},
		}
		markDeadExports(ranked)
		assert.False(t, ranked[0].Symbols[0].Dead, "unexported symbols must not be marked Dead")
		assert.False(t, ranked[0].Symbols[1].Dead, "unexported symbols must not be marked Dead")
	})
}
