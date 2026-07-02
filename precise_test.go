package repomap

import (
	"testing"

	"github.com/dotcommander/repomap/internal/callgraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTypedGraphToSymbolCallers_ShapeAndRenderDrop — Verification Surface §5.
// The adapter keys edges by callsKey(calleeFile, calleeSymbol) and does NOT
// filter against RankedFile.Symbols; an edge whose callee is absent from the
// rendered file's symbols survives in the map but is dropped at render time.
func TestTypedGraphToSymbolCallers_ShapeAndRenderDrop(t *testing.T) {
	t.Parallel()

	edges := []callgraph.CallEdge{
		{CallerFile: "cmd/main.go", CallerLine: 8, CalleeFile: "svc/svc.go", CalleeSymbol: "Serve"},
		{CallerFile: "cmd/main.go", CallerLine: 12, CalleeFile: "svc/svc.go", CalleeSymbol: "Ghost"},
	}

	callers := TypedGraphToSymbolCallers(edges)

	// Keys match callsKey format; the adapter filters nothing, so BOTH edges
	// (including the callee with no matching symbol) are present in the map.
	require.Contains(t, callers, callsKey("svc/svc.go", "Serve"))
	require.Contains(t, callers, callsKey("svc/svc.go", "Ghost"))
	serveLocs := callers[callsKey("svc/svc.go", "Serve")]
	require.Len(t, serveLocs, 1)
	assert.Equal(t, "cmd/main.go", serveLocs[0].File)
	assert.Equal(t, 8, serveLocs[0].Line)

	// Round-trip through the detail renderer with a RankedFile whose Symbols
	// contain Serve but NOT Ghost: Serve's callers render; Ghost drops silently.
	rf := makeTestRankedFile("svc/svc.go", 3, []Symbol{
		{Name: "Serve", Kind: "function", Exported: true, Line: 5},
	})
	out := formatFileBlockDetailWithCallers(rf, callers, 10, false)
	assert.Contains(t, out, "cmd/main.go:8", "matched callee renders its caller location")
	assert.NotContains(t, out, "Ghost", "unmatched callee produces no rendered output")
}
