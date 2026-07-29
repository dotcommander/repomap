package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteArtifactFailurePreservesTargetAndCleansTemp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "report.md")
	require.NoError(t, os.WriteFile(target, []byte("previous report\n"), 0o600))

	err := executeTest(t, []string{"--artifact", target, t.TempDir()}, io.Discard, io.Discard)
	require.Error(t, err)
	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "previous report\n", string(data))
	assertNoArtifactTemps(t, dir, filepath.Base(target))
}

func TestExecuteArtifactRejectsSymlinkAndNonRegularTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	destination := filepath.Join(dir, "destination.md")
	require.NoError(t, os.WriteFile(destination, []byte("destination\n"), 0o644))
	symlink := filepath.Join(dir, "report.md")
	if err := os.Symlink(destination, symlink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	err := executeTest(t, []string{"--artifact", symlink, t.TempDir()}, io.Discard, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a symlink")
	data, readErr := os.ReadFile(destination)
	require.NoError(t, readErr)
	assert.Equal(t, "destination\n", string(data))

	nonRegular := filepath.Join(dir, "directory-target")
	require.NoError(t, os.Mkdir(nonRegular, 0o755))
	err = executeTest(t, []string{"--artifact", nonRegular, t.TempDir()}, io.Discard, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a regular file")
}

func TestExecuteArtifactUsesTargetDirectoryAndPreservesMode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Main() {}\n"), 0o644))
	dir := t.TempDir()
	target := filepath.Join(dir, "report.md")
	require.NoError(t, os.WriteFile(target, []byte("old\n"), 0o600))

	var stdout bytes.Buffer
	require.NoError(t, executeTest(t, []string{"--artifact", target, root}, &stdout, io.Discard))
	assert.Empty(t, stdout.String())
	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assertNoArtifactTemps(t, dir, filepath.Base(target))

	newTarget := filepath.Join(dir, "new-report.md")
	require.NoError(t, executeTest(t, []string{"--artifact", newTarget, root}, io.Discard, io.Discard))
	info, err = os.Stat(newTarget)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func assertNoArtifactTemps(t *testing.T, dir, targetBase string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	prefix := "." + targetBase + ".tmp-"
	for _, entry := range entries {
		assert.False(t, strings.HasPrefix(entry.Name(), prefix), "temporary artifact remains: %s", entry.Name())
	}
}
