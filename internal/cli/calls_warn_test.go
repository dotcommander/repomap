package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStderr redirects os.Stderr to a pipe for the duration of fn and
// returns everything written to it.
// WARNING: mutates the global os.Stderr — callers must NOT call t.Parallel()
// at either the parent or subtest level, or races will corrupt the redirect.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)

	orig := os.Stderr
	os.Stderr = w

	fn()

	w.Close()
	os.Stderr = orig

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)
	r.Close()
	return buf.String()
}

// TestRenderCallsOutput_UnsupportedFormatsWarn is the BUG-01 regression test.
// compact, lines, and xml do not integrate caller data; each must emit a
// "warning: --calls has no effect with --format <fmt>" line to stderr so the
// user is not silently surprised when --calls output is ignored.
func TestRenderCallsOutput_UnsupportedFormatsWarn(t *testing.T) {
	// Not parallel: mutates os.Stderr (process-global).
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)
	ranked := m.Ranked()
	callers := repomap.SymbolCallers{} // empty — no gopls needed

	for _, format := range []string{"compact", "lines", "xml"} {
		format := format
		t.Run(format, func(t *testing.T) {
			// Not parallel: shares os.Stderr redirect with parent.
			var out bytes.Buffer
			stderr := captureStderr(t, func() {
				require.NoError(t, renderCallsOutput(&out, m, format, false, false, ranked, callers, 10))
			})
			assert.Contains(t, stderr, "warning: --calls has no effect with --format "+format,
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
	// Not parallel: mutates os.Stderr (process-global).
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
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			// Not parallel: shares os.Stderr redirect with parent.
			var out bytes.Buffer
			stderr := captureStderr(t, func() {
				require.NoError(t, renderCallsOutput(&out, m, tc.format, false, false, ranked, callers, 10))
			})
			assert.NotContains(t, stderr, "warning: --calls has no effect",
				"format %q must not emit a --calls warning", tc.format)
			assert.NotEmpty(t, out.String(),
				"format %q must produce output", tc.format)
		})
	}
}
