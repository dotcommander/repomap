package repomap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestScoreExplainAnnotation verifies the deterministic annotation string built
// by scoreExplainAnnotation: canonical tier order, zero tiers omitted, leading space.
func TestScoreExplainAnnotation(t *testing.T) {
	t.Parallel()

	t.Run("mixed tiers — zeros omitted order canonical", func(t *testing.T) {
		t.Parallel()
		f := RankedFile{
			FileSymbols: &FileSymbols{Path: "ranker.go"},
			Score:       47,
			ScoreComponents: map[string]int{
				scoreComponentImports:    110, // structural
				scoreComponentCallers:    0,   // confirmed — zero, must be omitted
				scoreComponentSymbolRefs: 0,   // lexical — zero, must be omitted
				scoreComponentIntent:     0,   // contextual — zero, must be omitted
			},
		}
		ann := scoreExplainAnnotation(f)
		assert.Equal(t, " # score 47 · structural:110", ann)
	})

	t.Run("confirmed and structural both nonzero", func(t *testing.T) {
		t.Parallel()
		f := RankedFile{
			FileSymbols: &FileSymbols{Path: "foo.go"},
			Score:       60,
			ScoreComponents: map[string]int{
				scoreComponentCallers: 20, // confirmed
				scoreComponentImports: 40, // structural
			},
		}
		ann := scoreExplainAnnotation(f)
		// confirmed must appear before structural
		assert.Equal(t, " # score 60 · confirmed:20 structural:40", ann)
	})

	t.Run("all four tiers nonzero order confirmed structural lexical contextual", func(t *testing.T) {
		t.Parallel()
		f := RankedFile{
			FileSymbols: &FileSymbols{Path: "all.go"},
			Score:       100,
			ScoreComponents: map[string]int{
				scoreComponentCallers:    10, // confirmed
				scoreComponentImports:    20, // structural
				scoreComponentSymbolRefs: 30, // lexical
				scoreComponentIntent:     40, // contextual
			},
		}
		ann := scoreExplainAnnotation(f)
		assert.Equal(t, " # score 100 · confirmed:10 structural:20 lexical:30 contextual:40", ann)
	})

	t.Run("no ScoreComponents emits score only", func(t *testing.T) {
		t.Parallel()
		f := RankedFile{
			FileSymbols: &FileSymbols{Path: "empty.go"},
			Score:       5,
		}
		ann := scoreExplainAnnotation(f)
		assert.Equal(t, " # score 5", ann)
	})

	t.Run("all components zero emits score only", func(t *testing.T) {
		t.Parallel()
		f := RankedFile{
			FileSymbols: &FileSymbols{Path: "zero.go"},
			Score:       0,
			ScoreComponents: map[string]int{
				scoreComponentCallers: 0,
				scoreComponentImports: 0,
			},
		}
		ann := scoreExplainAnnotation(f)
		assert.Equal(t, " # score 0", ann)
	})
}

// TestExplainFlag_FormatMapCompact verifies that the compact formatter appends
// annotations when explain=true and emits nothing extra when explain=false.
func TestExplainFlag_FormatMapCompact(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Run", Kind: "function", Exported: true, Line: 1}
	makeFiles := func() []RankedFile {
		return []RankedFile{{
			FileSymbols: &FileSymbols{Path: "server.go", Language: "go", Symbols: []Symbol{sym}},
			Score:       47,
			DetailLevel: 2,
			ScoreComponents: map[string]int{
				scoreComponentImports: 110,
			},
		}}
	}

	t.Run("explain=true annotation present on header line", func(t *testing.T) {
		t.Parallel()
		out := FormatMapCompact(makeFiles(), 4096, nil, true)
		// Header line must contain the score annotation.
		assert.True(t, strings.Contains(out, "# score"), "annotation must appear: %q", out)
		assert.Contains(t, out, "structural:110")
	})

	t.Run("explain=false NO annotation default unchanged", func(t *testing.T) {
		t.Parallel()
		out := FormatMapCompact(makeFiles(), 4096, nil, false)
		assert.NotContains(t, out, "# score")
		assert.NotContains(t, out, "structural:")
	})
}
