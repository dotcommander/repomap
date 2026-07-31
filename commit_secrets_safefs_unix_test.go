//go:build unix

package repomap

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterScannableRejectsNonRegularFiles(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	root := filepath.Join(workspace, "repo")
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "regular.txt"), []byte("ok"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "large.txt"), bytes.Repeat([]byte("x"), 1<<20+1), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "outside.txt"), []byte("outside"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "directory"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "regular.txt"), filepath.Join(root, "link.txt")))
	fifo := filepath.Join(root, "pipe")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))

	got := filterScannable(root, []fileChange{
		{Path: "regular.txt"},
		{Path: "large.txt"},
		{Path: "directory"},
		{Path: "link.txt"},
		{Path: "pipe"},
		{Path: "../outside.txt"},
		{Path: filepath.Join(workspace, "outside.txt")},
	})
	require.Equal(t, []scannableFile{{Path: "regular.txt", Data: []byte("ok")}}, got)
}
