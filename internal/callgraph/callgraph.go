// Package callgraph preserves the legacy caller-edge API while delegating all
// semantic work to the canonical goanalysis owner.
package callgraph

import (
	"context"
	"errors"
	"fmt"

	"github.com/dotcommander/repomap/internal/goanalysis"
)

type CallEdge struct {
	CallerFile, CallerSymbol string
	CallerLine               int
	CalleeFile, CalleeSymbol string
	CalleeReceiver           string
}

var ErrLoadFailed = errors.New("callgraph: Go analysis load failed")

// Build is retained for library compatibility. New Map/CLI code consumes the
// call edges already produced during Map.Build and does not invoke this path.
func Build(ctx context.Context, root string) ([]CallEdge, error) {
	analysis, err := goanalysis.Analyze(ctx, goanalysis.Options{Root: root, IncludeCalls: true})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLoadFailed, err)
	}
	edges := make([]CallEdge, 0, len(analysis.Calls))
	for _, edge := range analysis.Calls {
		edges = append(edges, CallEdge(edge))
	}
	return edges, nil
}
