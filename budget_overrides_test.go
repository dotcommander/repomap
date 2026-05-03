package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBudgetOverride_ForceFullLowRankedFile verifies that a file which would
// normally receive a low detail level (0 or 1) is promoted to DetailLevel 2
// when a "full" override matches its path.
func TestBudgetOverride_ForceFullLowRankedFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		pattern string
	}{
		{"exact match", "cmd/main.go", "cmd/main.go"},
		{"glob match", "cmd/main.go", "cmd/*.go"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// File has many symbols — won't fit at level 2 under tiny budget alone.
			// Budget: 4 tokens = 16 bytes. summaryCost for 1 group = 30 bytes > 16.
			// Without override the file gets DetailLevel -1 or 0.
			file := mkRankedFunc(tc.path, 10, 20, 0)

			cfg := &BlocklistConfig{
				FileOverrides: map[string]string{tc.pattern: "full"},
			}
			require.NoError(t, cfg.compile())

			ranked := BudgetFiles([]RankedFile{file}, 4, cfg)
			require.Len(t, ranked, 1)
			assert.Equal(t, 2, ranked[0].DetailLevel,
				"force-full override must set DetailLevel=2 regardless of budget")
		})
	}
}

// TestBudgetOverride_ForceOmit verifies that files matching an "omit" override
// are set to DetailLevel -1 even when they would otherwise be included.
func TestBudgetOverride_ForceOmit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		pattern string
	}{
		{"exact match", "internal/gen/pb.go", "internal/gen/pb.go"},
		{"glob match", "internal/gen/pb.go", "internal/gen/**"},
		{"wildcard suffix", "internal/gen/pb.go", "internal/**"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Generous budget — file would normally get DetailLevel 2.
			file := mkRankedFunc(tc.path, 2, 5, 0)

			cfg := &BlocklistConfig{
				FileOverrides: map[string]string{tc.pattern: "omit"},
			}
			require.NoError(t, cfg.compile())

			ranked := BudgetFiles([]RankedFile{file}, 2048, cfg)
			require.Len(t, ranked, 1)
			assert.Equal(t, -1, ranked[0].DetailLevel,
				"force-omit override must set DetailLevel=-1 regardless of budget")
		})
	}
}

// TestBudgetOverride_NilConfig verifies that nil config applies no overrides
// and existing budget behaviour is fully preserved.
func TestBudgetOverride_NilConfig(t *testing.T) {
	t.Parallel()

	// Two files: one that fits at level 2, one that overflows to level 1.
	f1 := mkRankedFunc("small.go", 2, 5, 0)   // cheap — fits level 2
	f2 := mkRankedFunc("large.go", 30, 20, 0) // expensive — falls to level 1

	ranked := BudgetFiles([]RankedFile{f1, f2}, 200, nil)
	require.Len(t, ranked, 2)
	// Nil config must not alter the budget-assigned levels.
	assert.Equal(t, 2, ranked[0].DetailLevel, "small file must reach level 2 with nil config")
	assert.Equal(t, 1, ranked[1].DetailLevel, "large file must fall to level 1 with nil config")
}

// TestBudgetOverride_OmitReclaimsBudget verifies that omitting a high-detail file
// reclaims its cost so lower-ranked files may be considered for promotion.
// (applyFileOverrides adjusts *used; subsequent callers can use reclaimed bytes.)
func TestBudgetOverride_OmitReclaimsBudget(t *testing.T) {
	t.Parallel()

	// f1 is expensive and would consume most of the budget at level 2.
	// f2 would be left at level 1 due to budget exhaustion.
	// With an omit override on f1, f2 should reach level 2.
	f1 := mkRankedFunc("internal/gen/big.go", 20, 20, 0)
	f2 := mkRankedFunc("cmd/main.go", 2, 5, 0)

	cfg := &BlocklistConfig{
		FileOverrides: map[string]string{"internal/gen/big.go": "omit"},
	}
	require.NoError(t, cfg.compile())

	// Budget sized so f1 alone fills it, but after omit f2 should fit at level 2.
	// enrichedCost(f1) ≈ 20*(8+1+21) = 600 bytes. Budget = 200 tokens = 800 bytes.
	// Without override: f1 fits at 2 (600 ≤ 800), f2 may also fit.
	// With omit override on f1: f1 forced to -1, f2 definitely fits at 2.
	ranked := BudgetFiles([]RankedFile{f1, f2}, 200, cfg)
	require.Len(t, ranked, 2)
	assert.Equal(t, -1, ranked[0].DetailLevel, "f1 must be omitted by override")
	assert.Equal(t, 2, ranked[1].DetailLevel, "f2 must reach level 2 after f1 reclaims budget")
}
