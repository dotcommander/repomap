package repomap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dotcommander/repomap/internal/callgraph"
)

// BenchmarkBuildPrecise measures the type-checked --precise call-graph build
// (Decision 6, precise-callgraph.md) against repomap's own module, using the
// same findBenchRoot helper as BenchmarkBuild/BenchmarkStale.
func BenchmarkBuildPrecise(b *testing.B) {
	root := findBenchRoot(b)
	for b.Loop() {
		_, _ = callgraph.Build(context.Background(), root)
	}
}

// TestPreciseBudget enforces the Decision 6 perf ceiling: the --precise
// whole-program call-graph build on repomap's own module must complete within
// max(10 * default Map.Build() time, 5*time.Second). Gated by testing.Short()
// and the race detector because either instrumentation invalidates the wall
// time comparison; a go/packages load failure skips rather than fails
// (environmental, not a perf regression — mirrors the --precise fail-open
// contract).
func TestPreciseBudget(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("precise budget builds the whole module; skipped under -short")
	}
	if raceEnabled {
		t.Skip("precise budget is a wall-time comparison; skipped under -race")
	}
	root := findBenchRoot(t)

	// Baseline: default Map.Build() wall time.
	buildStart := time.Now()
	m := New(root, DefaultConfig())
	if err := m.Build(context.Background()); err != nil {
		t.Fatalf("default build: %v", err)
	}
	defaultDur := time.Since(buildStart)

	// Measured: --precise whole-program call-graph build.
	preciseStart := time.Now()
	if _, err := callgraph.Build(context.Background(), root); err != nil {
		if errors.Is(err, callgraph.ErrLoadFailed) {
			t.Skipf("precise build unavailable (go/packages load failed): %v", err)
		}
		t.Fatalf("precise build: %v", err)
	}
	preciseDur := time.Since(preciseStart)

	// Decision 6 ceiling: precise wall time <= max(10 * default, 5s).
	ceiling := 10 * defaultDur
	if ceiling < 5*time.Second {
		ceiling = 5 * time.Second
	}
	t.Logf("precise budget: default=%s precise=%s ceiling=%s", defaultDur, preciseDur, ceiling)
	if preciseDur > ceiling {
		t.Fatalf("precise build %s exceeds Decision 6 ceiling %s (default build %s)", preciseDur, ceiling, defaultDur)
	}
}
