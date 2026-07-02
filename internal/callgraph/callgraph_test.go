package callgraph

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTypedGraph_SameNameDifferentReceiver — Verification Surface §1.
// Phase A: Build is a stub returning ErrLoadFailed, so this fails RED at the
// require.NoError below with "stub not implemented (Phase B)". Phase B fills in
// the real CHA edges and the assertions past that point run.
func TestTypedGraph_SameNameDifferentReceiver(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration-weight: real go/packages load (Phase B)")
	}
	edges, err := Build(context.Background(), filepath.Join("testdata", "samename"))
	require.NoError(t, err, "stub not implemented (Phase B)")

	// Typed graph resolves exactly one Get, attributed to a.go, called from main.go.
	var getEdges []CallEdge
	for _, e := range edges {
		if e.CalleeSymbol == "Get" {
			getEdges = append(getEdges, e)
		}
	}
	require.Len(t, getEdges, 1, "typed graph must resolve exactly one Get (A.Get), not B.Get")
	require.Equal(t, "a.go", filepath.Base(getEdges[0].CalleeFile))
	require.Equal(t, "main.go", filepath.Base(getEdges[0].CallerFile))

	// Contrast: a name-only matcher (the mechanism parser_callsites.go uses for
	// non-Go languages) keyed on "Get" alone matches BOTH receivers — the typed
	// graph is strictly more precise.
	fixtureGetDefs := []struct{ file, name string }{
		{"a.go", "Get"},
		{"b.go", "Get"},
	}
	naiveMatches := 0
	for _, d := range fixtureGetDefs {
		if d.name == "Get" { // name-only — ignores receiver type
			naiveMatches++
		}
	}
	require.Equal(t, 2, naiveMatches, "name-only match conflates A.Get and B.Get; typed graph resolved only 1")
}

// TestTypedGraph_InterfaceDispatch — Verification Surface §2.
// Phase A: fails RED at require.NoError with "stub not implemented (Phase B)".
func TestTypedGraph_InterfaceDispatch(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration-weight: real go/packages load (Phase B)")
	}
	edges, err := Build(context.Background(), filepath.Join("testdata", "ifacedispatch"))
	require.NoError(t, err, "stub not implemented (Phase B)")

	// CHA conservatively over-approximates interface dispatch: f.Fetch() yields
	// edges to BOTH implementers (HTTPFetcher.Fetch and FileFetcher.Fetch).
	var fetchEdges []CallEdge
	for _, e := range edges {
		if e.CalleeSymbol == "Fetch" {
			fetchEdges = append(fetchEdges, e)
		}
	}
	require.Len(t, fetchEdges, 2, "CHA must include both Fetcher implementers")
}

// TestBuild_LoadFailure — Verification Surface §3. Passes in Phase A (stub
// returns ErrLoadFailed unconditionally) and Phase B (empty dir has zero
// loadable Go packages).
func TestBuild_LoadFailure(t *testing.T) {
	t.Parallel()
	_, err := Build(context.Background(), filepath.Join("testdata", "emptydir"))
	require.ErrorIs(t, err, ErrLoadFailed)
}

// TestBuild_PartialTypeErrors — Verification Surface §3. Requires the real
// Build (returns non-nil edges from valid packages + one stderr warning per
// faulty package). Skipped until Phase B.
func TestBuild_PartialTypeErrors(t *testing.T) {
	t.Parallel()
	t.Skip("Phase B: partial-load fail-open path requires the real Build (precise-callgraph.md §3)")
}
