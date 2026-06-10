package repomap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCallsCacheKey_ContentHashInvalidation verifies that the key changes when
// a tracked source file's CONTENT changes, even though its path and flags are
// identical. (The old mtime-based key could collide when mtime was preserved.)
func TestCallsCacheKey_ContentHashInvalidation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	rel := "foo.go"
	abs := filepath.Join(root, rel)
	require.NoError(t, os.WriteFile(abs, []byte("package foo\n\nfunc A() {}\n"), 0o644))

	sym := Symbol{Name: "A", Kind: "function", Exported: true, Line: 3}
	ranked := []RankedFile{makeTestRankedFile(rel, 2, []Symbol{sym})}
	cfg := CallsConfig{Threshold: 2, Limit: 10}

	k1 := CallsCacheKey(root, ranked, cfg)

	// Rewrite with different content but force the SAME mtime, so an
	// mtime-based key would (wrongly) collide.
	info, err := os.Stat(abs)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(abs, []byte("package foo\n\nfunc A() { _ = 1 }\n"), 0o644))
	require.NoError(t, os.Chtimes(abs, info.ModTime(), info.ModTime()))

	k2 := CallsCacheKey(root, ranked, cfg)
	assert.NotEqual(t, k1, k2, "content change must change the cache key even with identical mtime")
}

// TestCallsCacheKey_FlagInvalidation verifies that the key changes when any of
// Threshold, Limit, or IncludeTests changes (same path + content).
func TestCallsCacheKey_FlagInvalidation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	rel := "bar.go"
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("package bar\n\nfunc B() {}\n"), 0o644))

	sym := Symbol{Name: "B", Kind: "function", Exported: true, Line: 3}
	ranked := []RankedFile{makeTestRankedFile(rel, 2, []Symbol{sym})}

	base := CallsConfig{Threshold: 2, Limit: 10, IncludeTests: false}
	kBase := CallsCacheKey(root, ranked, base)

	kThreshold := CallsCacheKey(root, ranked, CallsConfig{Threshold: 3, Limit: 10, IncludeTests: false})
	kLimit := CallsCacheKey(root, ranked, CallsConfig{Threshold: 2, Limit: 5, IncludeTests: false})
	kTests := CallsCacheKey(root, ranked, CallsConfig{Threshold: 2, Limit: 10, IncludeTests: true})

	assert.NotEqual(t, kBase, kThreshold, "Threshold change must change key")
	assert.NotEqual(t, kBase, kLimit, "Limit change must change key")
	assert.NotEqual(t, kBase, kTests, "IncludeTests change must change key")
}
