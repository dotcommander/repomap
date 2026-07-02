package callgraph

import (
	"context"
	"errors"

	// Phase B (precise-callgraph.md) imports go/packages, go/ssa and
	// go/callgraph/cha here for the real load. Blank-imported in Phase A so
	// `go mod tidy` keeps golang.org/x/tools pinned in go.mod.
	_ "golang.org/x/tools/go/packages"
)

// CallEdge is one resolved caller→callee relationship in the typed call graph.
// It carries enough structure to build SymbolCallers via the root package's
// TypedGraphToSymbolCallers adapter (Decision 3).
type CallEdge struct {
	CallerFile, CallerSymbol string
	CallerLine               int
	CalleeFile, CalleeSymbol string
}

// ErrLoadFailed is the exported sentinel returned when go/packages fails to
// load any usable package. Callers catch it via errors.Is and fall back to the
// gopls --calls tier (never a hard CLI error).
var ErrLoadFailed = errors.New("callgraph: go/packages load failed")

// Build returns the type-checked whole-program call graph for the module rooted
// at root.
//
// Phase A stub: always fails open with ErrLoadFailed so --precise falls back to
// the existing --calls path. Phase B (precise-callgraph.md) replaces this body
// with packages.Load(LoadSyntax) → ssautil.Packages → cha.CallGraph.
func Build(ctx context.Context, root string) ([]CallEdge, error) {
	return nil, ErrLoadFailed
}
