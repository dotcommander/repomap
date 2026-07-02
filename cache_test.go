package repomap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheConfigMismatchMiss(t *testing.T) {
	t.Parallel()

	repo := newGitRepo(t)
	cacheDir := t.TempDir()
	ctx := context.Background()

	cfgA := DefaultConfig()
	cfgA.MaxTokens = 512
	m1 := New(repo, cfgA)
	m1.SetCacheDir(cacheDir)
	require.NoError(t, m1.Build(ctx))

	// Different config → both load paths must miss.
	cfgB := DefaultConfig()
	cfgB.MaxTokens = 4096
	m2 := New(repo, cfgB)
	require.False(t, m2.LoadCache(cacheDir))

	ok, _ := m2.LoadCacheIncremental(ctx, cacheDir)
	require.False(t, ok)

	// Same config as original → must hit.
	m3 := New(repo, cfgA)
	require.True(t, m3.LoadCache(cacheDir))
}

func TestCacheBlocklistChangeMiss(t *testing.T) {
	t.Parallel()

	repo := newGitRepo(t)
	cacheDir := t.TempDir()
	cfg := DefaultConfig()

	m1 := New(repo, cfg)
	m1.SetCacheDir(cacheDir)
	require.NoError(t, m1.Build(context.Background()))

	blYAML := "method_blocklist:\n  - \"Zzz*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".repomap.yaml"), []byte(blYAML), 0o644))

	// New reads the .repomap.yaml → different blocklist → different config hash.
	m2 := New(repo, cfg)
	require.False(t, m2.LoadCache(cacheDir))
}
