package cli

import (
	"bytes"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderCallsOutput_UnsupportedFormatsWarn is the BUG-01 regression test.
// compact, lines, and xml do not integrate caller data; each must emit a
// "warning: --calls has no effect with --format <fmt>" line to stderr so the
// user is not silently surprised when --calls output is ignored.
func TestRenderCallsOutput_UnsupportedFormatsWarn(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)
	ranked := m.Ranked()
	callers := repomap.SymbolCallers{} // empty — no gopls needed

	for _, format := range []string{"compact", "lines", "xml"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var out, stderr bytes.Buffer
			require.NoError(t, renderCallsOutput(&out, &stderr, m, format, false, false, ranked, callers, 10))
			assert.Contains(t, stderr.String(), "warning: --calls has no effect with --format "+format,
				"format %q must emit a --calls warning to stderr", format)
			assert.NotEmpty(t, out.String(),
				"format %q must still produce output despite --calls being ignored", format)
		})
	}
}

// TestRenderCallsOutput_SupportedFormatsNoWarn is the BUG-01 positive regression.
// verbose, detail, and the enriched default integrate caller data and must NOT
// emit the unsupported-format warning.
func TestRenderCallsOutput_SupportedFormatsNoWarn(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)
	ranked := m.Ranked()
	callers := repomap.SymbolCallers{}

	cases := []struct {
		format string
		label  string
	}{
		{"verbose", "verbose"},
		{"detail", "detail"},
		{"", "(default)"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			var out, stderr bytes.Buffer
			require.NoError(t, renderCallsOutput(&out, &stderr, m, tc.format, false, false, ranked, callers, 10))
			assert.NotContains(t, stderr.String(), "warning: --calls has no effect",
				"format %q must not emit a --calls warning", tc.format)
			assert.NotEmpty(t, out.String(),
				"format %q must produce output", tc.format)
		})
	}
}
