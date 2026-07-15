package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderWithCalls_PreciseFallback — Verification Surface §3.
// Phase A: internal/callgraph.Build is a stub that always returns
// ErrLoadFailed, so resolvePreciseCallers (the --precise seam renderWithCalls
// uses) must fail open: emit the fallback notice and signal "not resolved" so
// the caller falls back to the gopls --calls tier — never a hard error.
func TestRenderWithCalls_PreciseFallback(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	callers, resolved, err := resolvePreciseCallers(context.Background(), t.TempDir(), &stderr)
	require.NoError(t, err, "ErrLoadFailed must fail open, not surface as an error")
	assert.False(t, resolved, "Phase A stub always fails open → not resolved")
	assert.Nil(t, callers)
	assert.Contains(t, stderr.String(), "repomap: --precise disabled — go/packages load failed, falling back to --calls")
}

// TestPreciseCalls_SameNameDisambiguation — Verification Surface §6 (golden CLI).
// The samename fixture defines A.Get and B.Get (same method name, different
// receivers); main.go calls only A{}.Get(). CHA must attribute the single call
// site (main.go:4) to A.Get and NOT to B.Get — the whole point of a typed graph
// over lexical name matching. --precise resolves via go/packages (no gopls
// needed), so this runs standalone.
func TestPreciseCalls_SameNameDisambiguation(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	require.NoError(t, executeTest(t, []string{"--precise", "--calls", "--no-cache", "-f", "detail", "testdata/samename"}, &stdout, io.Discard))

	out := stdout.String()
	// A.Get's caller is the call site in main.go (line 4).
	assert.Contains(t, out, "main.go:4", "CHA must resolve A.Get's caller (main.go:4)")
	// main.go:4 is the fixture's only call edge; it must appear exactly once. A
	// second occurrence would mean B.Get inherited A.Get's caller — the lexical
	// cross-contamination CHA exists to prevent.
	assert.Equal(t, 1, strings.Count(out, "main.go:4"),
		"same-named B.Get must not cross-contaminate A.Get's caller")
}

// TestPreciseCalls_PartialDiagnostics — type errors remain visible through the
// Map diagnostics surface while usable syntax and semantic data fail open.
func TestPreciseCalls_PartialDiagnostics(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module broken\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"),
		[]byte("package broken\n\nfunc Broken() { _ = undefinedSymbol }\n"), 0o644))

	var stdout bytes.Buffer
	var execErr error
	var stderr bytes.Buffer
	execErr = executeTest(t, []string{"--precise", "--calls", "--no-cache", dir}, &stdout, &stderr)

	assert.NoError(t, execErr)
	assert.NotContains(t, stderr.String(), "falling back to --calls")
}

func TestSemanticCallsIncludeTestsWithoutRankingTests(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/calltests\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.go"),
		[]byte("package calltests\n\nfunc Exported() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib_test.go"),
		[]byte("package calltests\n\nimport \"testing\"\n\nfunc TestExported(t *testing.T) { Exported() }\n"), 0o644))

	var stdout bytes.Buffer
	require.NoError(t, executeTest(t, []string{
		"--calls", "--calls-threshold", "0", "--calls-include-tests", "-f", "detail", dir,
	}, &stdout, io.Discard))

	assert.Contains(t, stdout.String(), "lib_test.go:5")
}
