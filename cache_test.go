package repomap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheVersion(t *testing.T) {
	assert.Equal(t, 15, cacheVersion)
}

func TestCacheExactHitDoesNotRewriteCache(t *testing.T) {
	t.Parallel()

	repo := newGitRepo(t)
	cacheDir := t.TempDir()
	buildWithCache(t, repo, cacheDir)
	cacheFile := cachePath(cacheDir, repo)
	fixedTime := time.Unix(1, 0)
	require.NoError(t, os.Chtimes(cacheFile, fixedTime, fixedTime))
	before, err := os.Stat(cacheFile)
	require.NoError(t, err)
	beforeData, err := os.ReadFile(cacheFile)
	require.NoError(t, err)

	buildWithCache(t, repo, cacheDir)
	after, err := os.Stat(cacheFile)
	require.NoError(t, err)
	afterData, err := os.ReadFile(cacheFile)
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime())
	assert.Equal(t, before.Size(), after.Size())
	assert.Equal(t, beforeData, afterData)
}

func TestCacheDirtyDigestIgnoresStagingAndSupportsRevert(t *testing.T) {
	t.Parallel()

	repo := newGitRepo(t)
	cacheDir := t.TempDir()
	buildWithCache(t, repo, cacheDir)

	path := filepath.Join(repo, "feature.py")
	first := []byte("def feature():\n    return 'first'\n")
	require.NoError(t, os.WriteFile(path, first, 0o644))
	buildWithCache(t, repo, cacheDir) // save a dirty cache for first.

	cacheFile := cachePath(cacheDir, repo)
	before, err := os.Stat(cacheFile)
	require.NoError(t, err)
	gitRun(t, repo, "add", "feature.py")
	buildWithCache(t, repo, cacheDir) // index-only change: exact digest hit.
	after, err := os.Stat(cacheFile)
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime())

	require.NoError(t, os.WriteFile(path, []byte("def feature():\n    return 'second'\n"), 0o644))
	mChanged := New(repo, DefaultConfig())
	_, ok := mChanged.cacheLoadPlan(context.Background(), cacheDir)
	assert.False(t, ok, "a changed dirty baseline must not be incrementally reused")

	require.NoError(t, os.WriteFile(path, first, 0o644))
	mReverted := New(repo, DefaultConfig())
	plan, ok := mReverted.cacheLoadPlan(context.Background(), cacheDir)
	require.True(t, ok)
	assert.True(t, plan.exactHit, "reverting to saved dirty contents restores the exact hit")
}

func TestCacheV14IsRejected(t *testing.T) {
	t.Parallel()

	repo := newGitRepo(t)
	cacheDir := t.TempDir()
	buildWithCache(t, repo, cacheDir)
	cacheFile := cachePath(cacheDir, repo)
	data, err := os.ReadFile(cacheFile)
	require.NoError(t, err)
	var entry diskCache
	require.NoError(t, json.Unmarshal(data, &entry))
	entry.Version = 14
	data, err = json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cacheFile, data, 0o644))

	m := New(repo, DefaultConfig())
	assert.False(t, m.LoadCache(cacheDir))
	ok, _ := m.LoadCacheIncremental(context.Background(), cacheDir)
	assert.False(t, ok)
}

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
