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

// mkGoFile builds a minimal *FileSymbols for a Go file with a known import path and imports.
func mkGoFile(path, importPath string, imports []string) *FileSymbols {
	return &FileSymbols{
		Path:       path,
		Language:   "go",
		ImportPath: importPath,
		Imports:    imports,
	}
}

// TestApplyTransitiveImportScores_Chain verifies that a deeply-imported file
// receives a higher transitive score than a file with only one direct importer.
//
// Graph: A → B → C → D  (A imports B, B imports C, C imports D)
// D has 3 transitive dependents (A, B, C) → +15
// E has 1 direct importer (A imports E) → only direct score via applyReferenceCounts,
// transitive score = 0 (no one imports E's importers).
//
// We test applyTransitiveImportScores in isolation so scores are purely transitive.
func TestApplyTransitiveImportScores_Chain(t *testing.T) {
	t.Parallel()

	files := []*FileSymbols{
		mkGoFile("a.go", "example.com/a", []string{"example.com/b", "example.com/e"}),
		mkGoFile("b.go", "example.com/b", []string{"example.com/c"}),
		mkGoFile("c.go", "example.com/c", []string{"example.com/d"}),
		mkGoFile("d.go", "example.com/d", nil),
		mkGoFile("e.go", "example.com/e", nil),
	}

	ranked := make([]RankedFile, len(files))
	for i, f := range files {
		ranked[i] = RankedFile{FileSymbols: f}
	}

	applyTransitiveImportScores(ranked)

	// Find scores by import path for clarity.
	scores := make(map[string]int, len(ranked))
	for _, rf := range ranked {
		scores[rf.ImportPath] = rf.Score
	}

	// D is imported by C, which is imported by B, which is imported by A: 3 transitive deps → +15.
	assert.Equal(t, 15, scores["example.com/d"], "D should have 3 transitive dependents (+15)")

	// C is imported by B and A (transitively via B): 2 transitive deps → +10.
	assert.Equal(t, 10, scores["example.com/c"], "C should have 2 transitive dependents (+10)")

	// B is imported by A: 1 transitive dep → +5.
	assert.Equal(t, 5, scores["example.com/b"], "B should have 1 transitive dependent (+5)")

	// E is imported only by A: 1 transitive dep → +5.
	assert.Equal(t, 5, scores["example.com/e"], "E should have 1 transitive dependent (+5)")

	// A is imported by nobody: 0 transitive deps → 0.
	assert.Equal(t, 0, scores["example.com/a"], "A should have 0 transitive dependents")

	// D outscores E (D has deeper fan-in).
	assert.Greater(t, scores["example.com/d"], scores["example.com/e"],
		"deeply-depended-on D should outrank shallowly-depended-on E")
}

// TestApplyTransitiveImportScores_Cap verifies the +50 cap.
// Create 12 files all importing a single hub; hub should be capped at 50 not 60.
func TestApplyTransitiveImportScores_Cap(t *testing.T) {
	t.Parallel()

	files := make([]*FileSymbols, 13)
	files[0] = mkGoFile("hub.go", "example.com/hub", nil)
	for i := 1; i <= 12; i++ {
		files[i] = mkGoFile("", "example.com/leaf", []string{"example.com/hub"})
		// Give each a unique import path so they're distinct in the index.
		files[i].ImportPath = "example.com/leaf" + string(rune('a'+i-1))
	}

	ranked := make([]RankedFile, len(files))
	for i, f := range files {
		ranked[i] = RankedFile{FileSymbols: f}
	}

	applyTransitiveImportScores(ranked)

	for _, rf := range ranked {
		if rf.ImportPath == "example.com/hub" {
			assert.Equal(t, 50, rf.Score, "hub with 12 dependents should be capped at 50")
			return
		}
	}
	t.Fatal("hub file not found in ranked slice")
}

// TestApplyTransitiveImportScores_Cycle verifies no panic and no infinite loop
// when the import graph contains a cycle (A → B → A).
func TestApplyTransitiveImportScores_Cycle(t *testing.T) {
	t.Parallel()

	files := []*FileSymbols{
		mkGoFile("a.go", "example.com/a", []string{"example.com/b"}),
		mkGoFile("b.go", "example.com/b", []string{"example.com/a"}),
	}

	ranked := make([]RankedFile, len(files))
	for i, f := range files {
		ranked[i] = RankedFile{FileSymbols: f}
	}

	// Must not panic or hang.
	assert.NotPanics(t, func() {
		applyTransitiveImportScores(ranked)
	})

	// Both files are mutual dependents: each has 1 transitive dependent → +5.
	for _, rf := range ranked {
		assert.Equal(t, 5, rf.Score, "each node in a 2-cycle should have 1 transitive dependent")
	}
}

// TestApplyTransitiveImportScores_Empty verifies no panic on empty input.
func TestApplyTransitiveImportScores_Empty(t *testing.T) {
	t.Parallel()
	assert.NotPanics(t, func() {
		applyTransitiveImportScores(nil)
		applyTransitiveImportScores([]RankedFile{})
	})
}

// TestRankFiles_TransitiveChainRaisesDeepFile is an end-to-end integration of
// RankFiles: after all passes, D (3 transitive dependents) should outscore A (leaf).
func TestRankFiles_TransitiveChainRaisesDeepFile(t *testing.T) {
	t.Parallel()

	files := []*FileSymbols{
		mkGoFile("a.go", "example.com/a", []string{"example.com/b"}),
		mkGoFile("b.go", "example.com/b", []string{"example.com/c"}),
		mkGoFile("c.go", "example.com/c", []string{"example.com/d"}),
		mkGoFile("d.go", "example.com/d", nil),
	}

	ranked := RankFiles(files)

	scoreOf := make(map[string]int, len(ranked))
	for _, rf := range ranked {
		scoreOf[rf.ImportPath] = rf.Score
	}

	assert.Greater(t, scoreOf["example.com/d"], scoreOf["example.com/a"],
		"D (3 transitive dependents) should outscore A (leaf) in full RankFiles pass")
}
