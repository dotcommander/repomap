package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/require"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestPrintCacheStatusReturnsWriteError(t *testing.T) {
	t.Parallel()

	err := printCacheStatus(failingWriter{}, repomap.CacheStatus{CachePath: "/tmp/repomap-cache"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "write failed")
}

func TestResolveCacheDir(t *testing.T) {
	t.Parallel()

	custom := filepath.Join(t.TempDir(), "cache")
	got, err := resolveCacheDir(custom)
	require.NoError(t, err)
	require.Equal(t, custom, got)

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	got, err = resolveCacheDir("")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".cache", "repomap"), got)
}

func TestCacheWarmCreatesFreshLoadableCache(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))
	cacheDir := filepath.Join(t.TempDir(), "cache")
	var stdout bytes.Buffer

	require.NoError(t, executeTest(t, []string{"cache", "warm", root, "--cache-dir", cacheDir}, &stdout, &bytes.Buffer{}))
	require.Contains(t, stdout.String(), "cache: fresh")

	status := repomap.InspectCache(t.Context(), root, cacheDir)
	require.True(t, status.Usable)
	require.False(t, status.Stale)
	require.Equal(t, "fresh", status.Reason)

	m := repomap.New(root, repomap.DefaultConfig())
	require.True(t, m.LoadCache(cacheDir), "cache warm must save a loadable entry")
}

func TestCacheWarmCreatesFreshCacheInDefaultDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))
	var stdout bytes.Buffer

	require.NoError(t, executeTest(t, []string{"cache", "warm", root}, &stdout, &bytes.Buffer{}))
	require.Contains(t, stdout.String(), "cache: fresh")

	cacheDir := filepath.Join(home, ".cache", "repomap")
	status := repomap.InspectCache(t.Context(), root, cacheDir)
	require.True(t, status.Usable)
	require.False(t, status.Stale)
	m := repomap.New(root, repomap.DefaultConfig())
	require.True(t, m.LoadCache(cacheDir), "default cache warm must save a loadable entry")
}

func TestCacheWarmDoesNotPrintSuccessWhenSaveFails(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))
	cacheDir := filepath.Join(t.TempDir(), "cache-file")
	require.NoError(t, os.WriteFile(cacheDir, []byte("not a directory"), 0o644))
	var stdout bytes.Buffer

	err := executeTest(t, []string{"cache", "warm", root, "--cache-dir", cacheDir}, &stdout, &bytes.Buffer{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "save cache")
	require.Empty(t, strings.TrimSpace(stdout.String()))
}

func TestCacheWarmDoesNotPrintSuccessWhenBuildFails(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := executeTest(t, []string{"cache", "warm", t.TempDir(), "--cache-dir", t.TempDir()}, &stdout, &bytes.Buffer{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "build map")
	require.Empty(t, strings.TrimSpace(stdout.String()))
}
