package repomap

import "testing"

func TestModeHint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		p    PrepPreflight
		want string
	}{
		{
			name: "remote and gh auth present",
			p:    PrepPreflight{Remote: "git@github.com:foo/bar.git", GHAuth: "Logged in to github.com as user"},
			want: "FULL",
		},
		{
			name: "remote present, gh not logged in",
			p:    PrepPreflight{Remote: "git@github.com:foo/bar.git", GHAuth: "Not logged in"},
			want: "LOCAL",
		},
		{
			name: "no remote, gh logged in",
			p:    PrepPreflight{Remote: "(none)", GHAuth: "Logged in to github.com"},
			want: "LOCAL",
		},
		{
			name: "no remote, gh not logged in",
			p:    PrepPreflight{Remote: "(none)", GHAuth: "Not logged in"},
			want: "LOCAL",
		},
		{
			name: "empty remote string treated as none",
			p:    PrepPreflight{Remote: "", GHAuth: "Logged in"},
			want: "LOCAL",
		},
		{
			name: "auth string with mixed case",
			p:    PrepPreflight{Remote: "https://github.com/foo/bar.git", GHAuth: "LOGGED IN to github.com"},
			want: "FULL",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ModeHint(tc.p); got != tc.want {
				t.Errorf("ModeHint(%+v) = %q, want %q", tc.p, got, tc.want)
			}
		})
	}
}
