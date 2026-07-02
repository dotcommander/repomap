package cli

import (
	"context"
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
