package repomap

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// populateMapState directly sets internal state on a Map to simulate a post-Build
// state without running the full scan→parse→rank pipeline. Used only in unit tests
// that need fine-grained control over builtAt and mtimes.
func populateMapState(m *Map, builtAt time.Time, mtimes map[string]time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.builtAt = builtAt
	m.mtimes = mtimes
	m.ranked = []RankedFile{} // non-nil so builtAt.IsZero() check doesn't short-circuit
}

// TestStale_NeverBuilt verifies that Stale returns true when Build has never been called.
func TestStale_NeverBuilt(t *testing.T) {
	t.Parallel()
	m := New(t.TempDir(), DefaultConfig())
	assert.True(t, m.Stale(), "unbuilt map must always be stale")
}

// TestStale_WithinDebounce verifies that Stale returns false immediately after a
// build, even if files have been modified, because the debounce window hasn't elapsed.
func TestStale_WithinDebounce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(path, []byte("package a"), 0o644))

	info, err := os.Stat(path)
	require.NoError(t, err)

	m := New(dir, DefaultConfig())
	// builtAt = now → within 30s debounce
	populateMapState(m, time.Now(), map[string]time.Time{path: info.ModTime()})

	assert.False(t, m.Stale(), "map built moments ago must not be stale (within debounce)")
}

// TestStale_NoChangeAfterDebounce verifies that Stale returns false after the debounce
// window when no file has been modified.
func TestStale_NoChangeAfterDebounce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "b.go")
	require.NoError(t, os.WriteFile(path, []byte("package b"), 0o644))

	info, err := os.Stat(path)
	require.NoError(t, err)

	m := New(dir, DefaultConfig())
	// builtAt = far in the past → past debounce; mtime matches recorded mtime
	pastBuild := time.Now().Add(-(staleDebounce + time.Second))
	populateMapState(m, pastBuild, map[string]time.Time{path: info.ModTime()})

	assert.False(t, m.Stale(), "map with no file changes must not be stale after debounce")
}

// TestStale_FileModifiedAfterDebounce verifies that Stale returns true after the debounce
// window when a tracked file's mtime has advanced past the recorded value.
func TestStale_FileModifiedAfterDebounce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "c.go")
	require.NoError(t, os.WriteFile(path, []byte("package c"), 0o644))

	// Record an old mtime (1 minute before the actual file was written).
	oldMtime := time.Now().Add(-time.Minute)

	m := New(dir, DefaultConfig())
	pastBuild := time.Now().Add(-(staleDebounce + time.Second))
	populateMapState(m, pastBuild, map[string]time.Time{path: oldMtime})

	// The file's real mtime is after oldMtime → stale.
	assert.True(t, m.Stale(), "file with newer mtime than recorded must mark map stale")
}

// TestStale_MissingFileAfterDebounce verifies that Stale returns true when a tracked
// file has been deleted (os.Stat fails → treated as stale).
func TestStale_MissingFileAfterDebounce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gone := filepath.Join(dir, "gone.go")

	m := New(dir, DefaultConfig())
	pastBuild := time.Now().Add(-(staleDebounce + time.Second))
	populateMapState(m, pastBuild, map[string]time.Time{gone: time.Now()})

	// gone.go was never created → Stat fails → stale.
	assert.True(t, m.Stale(), "map tracking a missing file must be stale")
}

// TestStale_MultipleFiles verifies that a single modified file among many marks the
// whole map stale, and that no modification keeps it fresh.
func TestStale_MultipleFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "x.go"),
		filepath.Join(dir, "y.go"),
		filepath.Join(dir, "z.go"),
	}
	for _, p := range paths {
		require.NoError(t, os.WriteFile(p, []byte("package p"), 0o644))
	}

	pastBuild := time.Now().Add(-(staleDebounce + time.Second))

	t.Run("no_changes", func(t *testing.T) {
		t.Parallel()
		mtimes := make(map[string]time.Time, len(paths))
		for _, p := range paths {
			info, err := os.Stat(p)
			require.NoError(t, err)
			mtimes[p] = info.ModTime()
		}
		m := New(dir, DefaultConfig())
		populateMapState(m, pastBuild, mtimes)
		assert.False(t, m.Stale(), "no file changes → not stale")
	})

	t.Run("one_file_older_mtime", func(t *testing.T) {
		t.Parallel()
		// Record one file with an mtime older than its real mtime.
		mtimes := make(map[string]time.Time, len(paths))
		for _, p := range paths {
			info, err := os.Stat(p)
			require.NoError(t, err)
			mtimes[p] = info.ModTime()
		}
		// Back-date the recorded mtime for y.go so the real mtime looks newer.
		mtimes[paths[1]] = time.Now().Add(-2 * time.Minute)
		m := New(dir, DefaultConfig())
		populateMapState(m, pastBuild, mtimes)
		assert.True(t, m.Stale(), "one file with newer real mtime must mark map stale")
	})
}

// TestStale_EmptyMtimes verifies that a map with builtAt set but no tracked files
// reports not-stale after debounce (nothing to check → fresh).
func TestStale_EmptyMtimes(t *testing.T) {
	t.Parallel()

	m := New(t.TempDir(), DefaultConfig())
	pastBuild := time.Now().Add(-(staleDebounce + time.Second))
	populateMapState(m, pastBuild, map[string]time.Time{})

	assert.False(t, m.Stale(), "map with no tracked files must not be stale")
}
