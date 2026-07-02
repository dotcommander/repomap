package repomap

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
)

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

// routePattern pairs a line-level registration regex with the framework it
// detects. The two frameworks capture different groups, so extractRoutes
// dispatches on framework.
type routePattern struct {
	re        *regexp.Regexp
	framework string
}

// routePatterns are the lexical route detectors. net/http captures group 1 =
// the quoted "METHOD /path" (or bare "/path") literal and group 2 = the handler
// argument; chi captures group 1 = the verb method, group 2 = the "/"-prefixed
// path, and group 3 = the handler argument. The "/"-prefix gate on chi's path
// argument is the false-positive control against unrelated same-named
// .Get/.Post methods (e.g. a cache's .Get(key, val)).
var routePatterns = []routePattern{
	{
		re:        regexp.MustCompile(`\b(?:\w+\.)?(?:HandleFunc|Handle)\(\s*"([^"]+)"\s*,\s*([A-Za-z_][\w.]*|\bfunc\b)`),
		framework: "net/http",
	},
	{
		re:        regexp.MustCompile(`\b\w+\.(Get|Post|Put|Delete|Patch|Head|Options|Connect|Trace)\(\s*"(/[^"]*)"\s*,\s*([A-Za-z_][\w.]*|\bfunc\b)`),
		framework: "chi",
	},
}

// Routes returns every route registration discovered across the scanned Go
// source files. It reuses auditStaticFiles (non-test file set) and
// readAuditLines (bounded, ctx-cancellable line read). An empty result is
// success, not an error.
func (m *Map) Routes(ctx context.Context) ([]RouteRegistration, error) {
	files := m.auditStaticFiles()
	var out []RouteRegistration
	for _, f := range files {
		if !strings.HasSuffix(f.path, ".go") {
			continue
		}
		lines, err := readAuditLines(ctx, filepath.Join(m.root, filepath.FromSlash(f.path)))
		if err != nil {
			return nil, err
		}
		out = append(out, extractRoutes(f.path, lines)...)
	}
	return out, nil
}

// extractRoutes pulls route registrations out of one file's lines by applying
// every routePattern to each line. net/http splits the quoted "METHOD /path"
// literal on the first space (no space → method "ANY"); chi uppercases the verb
// method name. A "func" handler argument (an inline closure) is recorded as
// "<inline>".
func extractRoutes(path string, lines []auditLine) []RouteRegistration {
	var out []RouteRegistration
	for _, line := range lines {
		for _, rp := range routePatterns {
			match := rp.re.FindStringSubmatch(line.text)
			if match == nil {
				continue
			}
			reg := RouteRegistration{
				Framework: rp.framework,
				File:      path,
				Line:      line.number,
			}
			switch rp.framework {
			case "net/http":
				reg.Method, reg.Pattern = splitMethodPath(match[1])
				reg.Handler = handlerName(match[2])
			case "chi":
				reg.Method = strings.ToUpper(match[1])
				reg.Pattern = match[2]
				reg.Handler = handlerName(match[3])
			}
			out = append(out, reg)
		}
	}
	return out
}

// splitMethodPath splits a net/http "METHOD /path" literal on the first space.
// A literal with no space is a legacy method-less registration → method "ANY".
func splitMethodPath(s string) (method, pattern string) {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "ANY", s
}

// handlerName maps a captured handler argument to its recorded form: an inline
// func literal (captured as the bare keyword "func") becomes "<inline>".
func handlerName(s string) string {
	if s == "func" {
		return "<inline>"
	}
	return s
}
