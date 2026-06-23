package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNonGoImportKeys_PathAware is the regression test for the monorepo
// mis-ranking bug: an `import "./types"` must credit ONLY the sibling-dir
// types file, never a same-named types file in another directory.
func TestNonGoImportKeys_PathAware(t *testing.T) {
	t.Parallel()

	// Two distinct types.ts files in different directories collapse to the
	// SAME bare basename ("types") under the old logic. The path-aware keys
	// keep them distinct: "pkg/a/types" vs "pkg/b/types".
	existing := map[string]struct{}{
		"pkg/a/types":            {},
		"pkg/b/types":            {},
		"pkg/a/components/index": {},
	}

	tests := []struct {
		name     string
		importer string
		imp      string // stored form INCLUDES quotes, mirroring tree-sitter
		want     []string
	}{
		{
			name:     "relative import credits only the sibling-dir target",
			importer: "pkg/a/main.ts",
			imp:      "\"./types\"",
			want:     []string{"pkg/a/types"}, // NOT pkg/b/types
		},
		{
			name:     "relative import from other dir credits its own sibling only",
			importer: "pkg/b/main.ts",
			imp:      "\"./types\"",
			want:     []string{"pkg/b/types"}, // NOT pkg/a/types
		},
		{
			name:     "parent-relative import resolves up a directory",
			importer: "pkg/a/components/widget.ts",
			imp:      "\"../types\"",
			want:     []string{"pkg/a/types"},
		},
		{
			name:     "directory import resolves to index",
			importer: "pkg/a/main.ts",
			imp:      "\"./components\"",
			want:     []string{"pkg/a/components/index"},
		},
		{
			name:     "js extension on relative import is normalized away",
			importer: "pkg/a/main.ts",
			imp:      "\"./types.js\"",
			want:     []string{"pkg/a/types"},
		},
		{
			name:     "bare package import falls back to basename (unmatched here)",
			importer: "pkg/a/main.ts",
			imp:      "\"react\"",
			want:     nil,
		},
		{
			name:     "relative import with no matching target returns nothing",
			importer: "pkg/a/main.ts",
			imp:      "\"./does-not-exist\"",
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nonGoImportKeys(tc.importer, tc.imp, existing)
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestStripImportQuotes(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`"./types"`:     "./types",
		`'./x'`:         "./x",
		`"./y"`:         "./y",
		`react`:          "react",
		`"unterminated`: `"unterminated`,
	}
	for in, want := range cases {
		assert.Equal(t, want, stripImportQuotes(in), "stripImportQuotes(%q)", in)
	}
}
