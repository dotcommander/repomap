package repomap

import (
	"context"
	"testing"

	"github.com/dotcommander/repomap/internal/callgraph"
)

// BenchmarkBuildPrecise measures the type-checked --precise call-graph build
// (Decision 6, precise-callgraph.md) against repomap's own module, using the
// same findBenchRoot helper as BenchmarkBuild/BenchmarkStale.
//
// Phase A: callgraph.Build is still a stub returning ErrLoadFailed, so this is
// skipped. Phase C removes the skip and records the measured multiple in the
// PR description.
func BenchmarkBuildPrecise(b *testing.B) {
	b.Skip("--precise not yet implemented, Phase B")
	root := findBenchRoot(b)
	for b.Loop() {
		_, _ = callgraph.Build(context.Background(), root)
	}
}

// TestPreciseBudget enforces the Decision 6 perf ceiling: precise build wall
// time <= max(10 * default Map.Build() time, 5*time.Second).
//
// Phase A: callgraph.Build is a stub, so this is skipped. Phase C fills in the
// measurement body (gated by testing.Short() per Decision 6) and removes this
// skip.
func TestPreciseBudget(t *testing.T) {
	t.Parallel()
	t.Skip("--precise not yet implemented, Phase B")
}
