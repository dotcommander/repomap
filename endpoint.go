package repomap

import (
	"context"
	"fmt"
)

// EndpointContext is the vertical-slice bundle for one resolved route:
// the registration, its handler symbol, the handler's direct callee names,
// the tests that touch it, and the file-level impact summary.
type EndpointContext struct {
	Route   RouteRegistration `json:"route"`
	Handler *SymbolMatch      `json:"handler,omitempty"`
	Callees []string          `json:"callees,omitempty"`
	Tests   []Location        `json:"tests,omitempty"`
	Impact  ImpactResult      `json:"impact"`
}

// Endpoint resolves a single route by pattern and returns its vertical-slice
// bundle. Phase A stub: always returns an error (real resolution lands in
// Phase C).
func (m *Map) Endpoint(ctx context.Context, pattern string) (EndpointContext, error) {
	return EndpointContext{}, fmt.Errorf("endpoint: pattern resolution not implemented")
}
