package repomap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunSimplifyDetect_MissingScriptIsNoop(t *testing.T) {
	got, err := RunSimplifyDetect(context.Background(), t.TempDir())
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestRunSimplifyDetect_StatErrorReturnsError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "home-is-file")
	require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o644))
	t.Setenv("HOME", blocker)
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")

	got, err := RunSimplifyDetect(context.Background(), t.TempDir())
	require.Error(t, err)
	require.Contains(t, err.Error(), "stat simplify-detect script")
	require.Nil(t, got)
}
