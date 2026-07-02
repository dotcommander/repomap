package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
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
// Not parallel: captureStderr mutates the process-global os.Stderr.
func TestRenderWithCalls_PreciseFallback(t *testing.T) {
	stderr := captureStderr(t, func() {
		callers, resolved, err := resolvePreciseCallers(context.Background(), t.TempDir())
		require.NoError(t, err, "ErrLoadFailed must fail open, not surface as an error")
		assert.False(t, resolved, "Phase A stub always fails open → not resolved")
		assert.Nil(t, callers)
	})
	assert.Contains(t, stderr, "repomap: --precise disabled — go/packages load failed, falling back to --calls")
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
	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--precise", "--calls", "--no-cache", "-f", "detail", "testdata/samename"})
	require.NoError(t, cmd.Execute())

	out := stdout.String()
	// A.Get's caller is the call site in main.go (line 4).
	assert.Contains(t, out, "main.go:4", "CHA must resolve A.Get's caller (main.go:4)")
	// main.go:4 is the fixture's only call edge; it must appear exactly once. A
	// second occurrence would mean B.Get inherited A.Get's caller — the lexical
	// cross-contamination CHA exists to prevent.
	assert.Equal(t, 1, strings.Count(out, "main.go:4"),
		"same-named B.Get must not cross-contaminate A.Get's caller")
}

// TestPreciseCalls_UnloadableFallback — Verification Surface §6 (golden CLI).
// A directory that HAS Go source the scanner accepts but go/packages cannot
// type-check (an undefined identifier) exercises the --precise fail-open path:
// resolvePreciseCallers emits the fallback notice and defers to the gopls
// --calls tier. (An empty dir cannot reach this — the scanner rejects it with
// ErrNotCodeProject before --precise runs.)
// Not parallel: captureStderr mutates the process-global os.Stderr.
func TestPreciseCalls_UnloadableFallback(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module broken\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.go"),
		[]byte("package broken\n\nfunc Broken() { _ = undefinedSymbol }\n"), 0o644))

	var stdout bytes.Buffer
	var execErr error
	stderr := captureStderr(t, func() {
		cmd := newRootCmd()
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{"--precise", "--calls", "--no-cache", dir})
		execErr = cmd.Execute()
	})

	// Ungated: the fail-open fallback notice must always be emitted.
	assert.Contains(t, stderr,
		"repomap: --precise disabled — go/packages load failed, falling back to --calls",
		"unloadable Go source must trigger the --precise fail-open notice")

	// Gated: the fallback lands on the gopls --calls route, which errors when
	// gopls is absent. Only assert exit-0 when gopls is on PATH.
	if _, err := exec.LookPath("gopls"); err == nil {
		assert.NoError(t, execErr,
			"with gopls present, --precise on unloadable source falls back and exits 0")
	}
}
