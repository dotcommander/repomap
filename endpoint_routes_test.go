package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func endpointTestLines(src ...string) []auditLine {
	out := make([]auditLine, len(src))
	for i, s := range src {
		out[i] = auditLine{number: i + 1, text: s}
	}
	return out
}

func TestExtractRoutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		file  string
		lines []auditLine
		want  []RouteRegistration
	}{
		{
			name:  "net/http HandleFunc Go 1.22 method+path",
			file:  "routes.go",
			lines: endpointTestLines(`mux.HandleFunc("GET /users/{id}", getUser)`),
			want:  []RouteRegistration{{Method: "GET", Pattern: "/users/{id}", Handler: "getUser", Framework: "net/http", File: "routes.go", Line: 1}},
		},
		{
			name:  "net/http HandleFunc legacy path-only is ANY",
			file:  "routes.go",
			lines: endpointTestLines(`http.HandleFunc("/health", healthHandler)`),
			want:  []RouteRegistration{{Method: "ANY", Pattern: "/health", Handler: "healthHandler", Framework: "net/http", File: "routes.go", Line: 1}},
		},
		{
			name:  "chi verb method",
			file:  "routes.go",
			lines: endpointTestLines(`r.Get("/users/{id}", getUser)`),
			want:  []RouteRegistration{{Method: "GET", Pattern: "/users/{id}", Handler: "getUser", Framework: "chi", File: "routes.go", Line: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractRoutes(tt.file, tt.lines)
			assert.Equal(t, tt.want, got)
		})
	}
}
