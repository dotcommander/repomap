package repomap

import (
	"strings"
	"testing"
)

func TestPlaceholderFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind string
		want string
	}{
		{"secret_api_key", "YOUR_API_KEY"},
		{"secret_password", "YOUR_PASSWORD"},
		{"secret_token", "YOUR_TOKEN"},
		{"secret", "REDACTED"},
		{"pii", "/path/to/project"},
		{"path_user_home", "/path/to/project"},
		{"path_machine_specific", "/path/to/project"},
		{"unknown_kind", ""},
	}
	for _, tc := range tests {
		got := placeholderFor(tc.kind)
		if got != tc.want {
			t.Errorf("placeholderFor(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestApplyPlaceholder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		line        string
		placeholder string
		wantChanged bool
		wantContain string
	}{
		{
			name:        "aws key",
			line:        `const key = "YOUR_API_KEY"`,
			placeholder: "REDACTED",
			wantChanged: true,
			wantContain: "REDACTED",
		},
		{
			name:        "user path",
			line:        `path: /path/to/project/workspace/project`,
			placeholder: "/path/to/project",
			wantChanged: true,
			wantContain: "/path/to/project",
		},
		{
			name:        "already substituted",
			line:        `key: REDACTED`,
			placeholder: "REDACTED",
			wantChanged: false,
			wantContain: "REDACTED",
		},
		{
			name:        "no pattern match",
			line:        `just a regular comment`,
			placeholder: "REDACTED",
			wantChanged: false,
			wantContain: "just a regular comment",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, changed := applyPlaceholder(tc.line, tc.placeholder)
			if changed != tc.wantChanged {
				t.Errorf("changed=%v want=%v (line=%q result=%q)", changed, tc.wantChanged, tc.line, got)
			}
			if !strings.Contains(got, tc.wantContain) {
				t.Errorf("result %q does not contain %q", got, tc.wantContain)
			}
		})
	}
}
