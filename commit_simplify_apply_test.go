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

func TestCommitPrepLegacyWrappers(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("HOME", t.TempDir())

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), nil, 0o644))

	StashArtifacts(root, nil)
	gate := RunReleaseGate(root)
	require.NotNil(t, gate)
	require.True(t, gate.BuildOK)
	require.Empty(t, gate.Applied)
}
