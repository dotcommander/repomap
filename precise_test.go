package repomap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/repomap/internal/callgraph"
	"github.com/dotcommander/repomap/internal/goanalysis"
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
	assert.NotContains(t, out, "callers: callers:", "caller label renders exactly once")
	assert.NotContains(t, out, "Ghost", "unmatched callee produces no rendered output")
}

func TestSemanticCallerLookupNormalizesGenericReceivers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, callsKey("box.go", "Box.Value"), semanticCallsKey("box.go", "Box[T]", "Value"))
	assert.Equal(t, callsKey("box.go", "*Box.Pointer"), semanticCallsKey("box.go", "*Box[T]", "Pointer"))

	callers := semanticCallsToSymbolCallers([]goanalysis.CallEdge{
		{CallerFile: "use.go", CallerLine: 12, CalleeFile: "box.go", CalleeSymbol: "Pointer", CalleeReceiver: "*Box"},
		{CallerFile: "use.go", CallerLine: 12, CalleeFile: "box.go", CalleeSymbol: "Pointer", CalleeReceiver: "*Box[T]"},
	})

	lookedUp := callers.CallersForSymbol("box.go", Symbol{Name: "Pointer", Receiver: "*Box[T]"})
	require.Equal(t, []Location{{File: "use.go", Line: 12}}, lookedUp)
}

func TestMapBuildResolvesGenericMethodCallers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/generic\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "box.go"), []byte(`package generic

type Box[T any] struct{}

func (Box[T]) Value() {}
func (*Box[T]) Pointer() {}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "use.go"), []byte(`package generic

func Use() {
	box := Box[int]{}
	box.Value()
	(&box).Pointer()
}
`), 0o644))

	cfg := DefaultConfig()
	cfg.GoAnalysisCalls = true
	m := New(root, cfg)
	require.NoError(t, m.Build(context.Background()))

	var value, pointer *Symbol
	for _, ranked := range m.Ranked() {
		if ranked.Path != "box.go" {
			continue
		}
		for i := range ranked.Symbols {
			symbol := &ranked.Symbols[i]
			switch symbol.Name {
			case "Value":
				value = symbol
			case "Pointer":
				pointer = symbol
			}
		}
	}
	require.NotNil(t, value)
	require.NotNil(t, pointer)
	require.Equal(t, "Box[T]", value.Receiver)
	require.Equal(t, "*Box[T]", pointer.Receiver)

	callers := m.SemanticCallers()
	require.Equal(t, []Location{{File: "use.go", Line: 5}}, callers.CallersForSymbol("box.go", *value))
	require.Equal(t, []Location{{File: "use.go", Line: 6}}, callers.CallersForSymbol("box.go", *pointer))
}
