package repomap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectCache_Missing(t *testing.T) {
	t.Parallel()

	got := InspectCache(context.Background(), t.TempDir(), t.TempDir())

	assert.False(t, got.Exists)
	assert.False(t, got.Usable)
	assert.Equal(t, "missing_cache", got.Reason)
}

func TestInspectCache_FreshAndContentChanged(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cacheDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/cache\n\ngo 1.22\n"), 0o644))
	path := filepath.Join(root, "a.go")
	require.NoError(t, os.WriteFile(path, []byte("package a\n\nfunc Run() {}\n"), 0o644))

	m := New(root, DefaultConfig())
	m.SetCacheDir(cacheDir)
	require.NoError(t, m.Build(context.Background()))

	fresh := InspectCache(context.Background(), root, cacheDir)
	assert.True(t, fresh.Exists)
	assert.True(t, fresh.Usable)
	assert.False(t, fresh.Stale)
	assert.Equal(t, "fresh", fresh.Reason)

	require.NoError(t, os.WriteFile(path, []byte("package a\n\nfunc Run() error { return nil }\n"), 0o644))

	stale := InspectCache(context.Background(), root, cacheDir)
	assert.True(t, stale.Stale)
	assert.Equal(t, "content_changed", stale.Reason)
}

func TestInspectCache_Corrupt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cacheDir := t.TempDir()
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	require.NoError(t, os.WriteFile(cachePath(cacheDir, root), []byte("{bad json"), 0o644))

	got := InspectCache(context.Background(), root, cacheDir)

	assert.True(t, got.Exists)
	assert.False(t, got.Usable)
	assert.Equal(t, "corrupt_cache", got.Reason)
}

func TestSaveCacheLegacySignature(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cacheDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/cache\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc Run() {}\n"), 0o644))

	m := New(root, DefaultConfig())
	require.NoError(t, m.Build(context.Background()))
	require.NoError(t, m.SaveCache(cacheDir))
	require.True(t, m.LoadCache(cacheDir))
}
