package repomap

import "context"

// RouteRegistration is one HTTP route binding discovered lexically in a source
// file: an HTTP method, the path pattern as written, the handler identifier
// text, and the framework it was registered with.
type RouteRegistration struct {
	Method    string // "GET", "POST", ... or "ANY" for a method-less net/http registration
	Pattern   string // path pattern as written, e.g. "/users/{id}"
	Handler   string // handler identifier text, or "<inline>" for a closure
	Framework string // "net/http" | "chi"
	File      string // path relative to the map root
	Line      int    // 1-based line number of the registration
}

// Routes returns every route registration discovered across the scanned Go
// source files. Phase A stub: no routes (real lexical extraction lands in
// Phase B). An empty result is success, not an error.
func (m *Map) Routes(ctx context.Context) ([]RouteRegistration, error) {
	return nil, nil
}

// extractRoutes pulls route registrations out of one file's lines by lexical
// pattern match. Phase A stub: no routes (the real regex set lands in Phase B).
func extractRoutes(path string, lines []auditLine) []RouteRegistration {
	return nil
}
