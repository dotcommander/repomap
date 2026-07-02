package repomap

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// EndpointContext is the vertical-slice bundle for one resolved route:
// the registration, its handler symbol, the handler's direct callee names,
// the tests that touch it, and the file-level impact summary.
type EndpointContext struct {
	Route     RouteRegistration `json:"route"`
	Handler   *SymbolMatch      `json:"handler,omitempty"`
	Ambiguous []SymbolMatch     `json:"ambiguous,omitempty"`
	Callees   []string          `json:"callees,omitempty"`
	Tests     []Location        `json:"tests,omitempty"`
	Impact    ImpactResult      `json:"impact"`
}

// Endpoint resolves a single route by pattern and returns its vertical-slice
// bundle: the matched RouteRegistration, its handler symbol (best FindSymbol
// hit), the handler's depth-1 lexical callee names, and the file-level impact.
// The CLI layer fills EndpointContext.Tests afterward (gopls-backed). Zero
// matches is an error; multiple matches populate Ambiguous (capped at 5).
func (m *Map) Endpoint(ctx context.Context, pattern string) (EndpointContext, error) {
	routes, err := m.Routes(ctx)
	if err != nil {
		return EndpointContext{}, err
	}
	matches := matchRoutes(routes, pattern)
	if len(matches) == 0 {
		return EndpointContext{}, fmt.Errorf("no route matching %q", pattern)
	}

	route := matches[0]
	ec := EndpointContext{Route: route}

	// Resolve the handler symbol (best FindSymbol hit). An inline/unknown
	// handler leaves Handler nil, so Callees/Tests stay empty (Decision 5/6).
	if hits := m.FindSymbol(route.Handler, "", ""); len(hits) > 0 {
		h := hits[0]
		ec.Handler = &h
		// Depth-1 lexical callees over the handler's line span. A parse
		// failure degrades to nil callees, never a hard failure (Decision 5).
		if callees, cerr := handlerCallees(filepath.Join(m.root, h.File), h.Symbol.Line, h.Symbol.EndLine); cerr == nil {
			ec.Callees = callees
		}
	}

	// Extra matching routes surface as Ambiguous alternatives (capped 5),
	// mirroring SymbolContext.Ambiguous (context.go:80-86).
	if len(matches) > 1 {
		limit := len(matches)
		if limit > 5 {
			limit = 5
		}
		for _, r := range matches[1:limit] {
			if hits := m.FindSymbol(r.Handler, "", ""); len(hits) > 0 {
				ec.Ambiguous = append(ec.Ambiguous, hits[0])
			}
		}
	}

	// File-level impact: the handler's file when resolved, else the route's
	// file (both are always present in the ranked map).
	impactFile := route.File
	if ec.Handler != nil {
		impactFile = ec.Handler.File
	}
	impact, err := m.Impact(impactFile)
	if err != nil {
		return EndpointContext{}, err
	}
	ec.Impact = impact

	return ec, nil
}

// matchRoutes selects the routes satisfying the query. Exact matches — the full
// "METHOD PATTERN" string or the bare path pattern — win; only when there is no
// exact match does it fall back to a substring match on "METHOD PATTERN".
func matchRoutes(routes []RouteRegistration, pattern string) []RouteRegistration {
	query := strings.TrimSpace(pattern)
	var exact, partial []RouteRegistration
	for _, r := range routes {
		canonical := r.Method + " " + r.Pattern
		switch {
		case canonical == query || r.Pattern == query:
			exact = append(exact, r)
		case query != "" && strings.Contains(canonical, query):
			partial = append(partial, r)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}
