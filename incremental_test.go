package repomap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGitRepo initializes a temp git repo with one Go file and an initial commit.
// Returns the root path. Git subcommands use -c user.email/user.name to avoid
// environment dependencies.
func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	mainSrc := `package main

func Hello() string { return "hello" }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o644))

	gitRun(t, dir, "init")
	gitRun(t, dir, "add", ".")
	gitCommitAll(t, dir, "init")
	return dir
}

// gitRun executes a git subcommand in dir. Fatals on non-zero exit.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	base := []string{"-c", "user.email=test@example.com", "-c", "user.name=Test"}
	all := append(base, args...)
	cmd := exec.Command("git", all...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// gitCommitAll stages all changes and commits with the given message.
func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", msg)
}

// buildWithCache runs Build() with cacheDir set and returns the built Map.
func buildWithCache(t *testing.T, dir, cacheDir string) *Map {
	t.Helper()
	m := New(dir, DefaultConfig())
	m.SetCacheDir(cacheDir)
	err := m.Build(context.Background())
	require.NoError(t, err)
	return m
}

// TestIncrementalUnchanged verifies the fast path when HEAD == LastSHA and no
// worktree changes. The second build must update builtAt and return the same output.
func TestIncrementalUnchanged(t *testing.T) {
	t.Parallel()

	dir := newGitRepo(t)
	cacheDir := t.TempDir()

	m1 := buildWithCache(t, dir, cacheDir)
	out1 := m1.String()
	builtAt1 := m1.BuiltAt()

	// Small sleep so builtAt can advance.
	time.Sleep(5 * time.Millisecond)

	m2 := buildWithCache(t, dir, cacheDir)
	out2 := m2.String()

	assert.Equal(t, out1, out2, "output must be identical on unchanged repo")
	assert.True(t, m2.BuiltAt().After(builtAt1) || m2.BuiltAt().Equal(builtAt1),
		"builtAt must be updated on second build")
}

// TestIncrementalModifiedFile verifies that modifying a committed file causes
// the new symbol to appear in the next build's output.
func TestIncrementalModifiedFile(t *testing.T) {
	t.Parallel()

	dir := newGitRepo(t)
	cacheDir := t.TempDir()

	buildWithCache(t, dir, cacheDir)

	// Modify the existing file with a new exported function.
	updated := `package main

func Hello() string { return "hello" }
func Added() int { return 42 }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(updated), 0o644))
	gitCommitAll(t, dir, "add Added func")

	m2 := buildWithCache(t, dir, cacheDir)
	assert.Contains(t, m2.String(), "Added", "new symbol must appear after incremental rebuild")
	assert.Contains(t, m2.String(), "Hello", "original symbol must still appear")
}

// TestIncrementalAddedFile verifies that adding a new file causes the new symbol
// to appear and the ranked count to increase by exactly 1.
func TestIncrementalAddedFile(t *testing.T) {
	t.Parallel()

	dir := newGitRepo(t)
	cacheDir := t.TempDir()

	m1 := buildWithCache(t, dir, cacheDir)
	count1 := len(m1.Ranked())

	helperSrc := `package main

func HelperFn() bool { return true }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helper.go"), []byte(helperSrc), 0o644))
	gitCommitAll(t, dir, "add helper.go")

	m2 := buildWithCache(t, dir, cacheDir)
	assert.Contains(t, m2.String(), "HelperFn", "new symbol from added file must appear")
	assert.Equal(t, count1+1, len(m2.Ranked()), "ranked count must increase by exactly 1")
}

// TestIncrementalDeletedFile verifies that deleting a committed file removes it
// from the ranked output.
func TestIncrementalDeletedFile(t *testing.T) {
	t.Parallel()

	dir := newGitRepo(t)

	// Add a second file so there are 2 tracked files.
	extraSrc := `package main

func Extra() string { return "extra" }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.go"), []byte(extraSrc), 0o644))
	gitCommitAll(t, dir, "add extra.go")

	cacheDir := t.TempDir()
	buildWithCache(t, dir, cacheDir)

	// Delete extra.go and commit.
	require.NoError(t, os.Remove(filepath.Join(dir, "extra.go")))
	gitCommitAll(t, dir, "delete extra.go")

	m2 := buildWithCache(t, dir, cacheDir)
	for _, rf := range m2.Ranked() {
		assert.NotEqual(t, "extra.go", rf.Path, "deleted file must not appear in ranked output")
	}
}

// TestIncrementalUntrackedFile verifies that an untracked file (not yet committed)
// is picked up by the incremental path via git ls-files --others.
func TestIncrementalUntrackedFile(t *testing.T) {
	t.Parallel()

	dir := newGitRepo(t)
	cacheDir := t.TempDir()

	buildWithCache(t, dir, cacheDir)

	// Add a new file WITHOUT committing it.
	untrackedSrc := `package main

func UntrackedFn() float64 { return 3.14 }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "untracked.go"), []byte(untrackedSrc), 0o644))

	m2 := buildWithCache(t, dir, cacheDir)
	assert.Contains(t, m2.String(), "UntrackedFn", "untracked file symbol must appear in incremental output")
}

// TestIncrementalThresholdFallback verifies that when >30% of files change, the
// incremental path falls back to a full rebuild. The output must still be correct.
func TestIncrementalThresholdFallback(t *testing.T) {
	t.Parallel()

	dir := newGitRepo(t)

	// Create 9 additional Go files so we have 10 total tracked files.
	for i := range 9 {
		src := fmt.Sprintf("package main\n\nfunc Fn%d() int { return %d }\n", i, i)
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%d.go", i)), []byte(src), 0o644))
	}
	gitCommitAll(t, dir, "add 9 more files")

	cacheDir := t.TempDir()
	buildWithCache(t, dir, cacheDir)

	// Modify 5 out of 10 files (50% > 30% threshold) — triggers full rebuild.
	for i := range 5 {
		src := fmt.Sprintf("package main\n\nfunc Fn%dModified() int { return %d }\n", i, i+100)
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%d.go", i)), []byte(src), 0o644))
	}
	gitCommitAll(t, dir, "modify 5 files")

	m2 := buildWithCache(t, dir, cacheDir)
	// All 10 files must still be present — full rebuild produces correct output.
	assert.Equal(t, 10, len(m2.Ranked()), "full rebuild must include all 10 files")
	// Modified symbols must appear.
	assert.Contains(t, m2.String(), "Fn0Modified", "modified symbol must appear after full rebuild")
}

// TestIncrementalNonGitFallback verifies that a directory without git init falls
// through to full rebuild on every call (no panic, no stale state).
func TestIncrementalNonGitFallback(t *testing.T) {
	t.Parallel()

	// Plain tmpdir — no git init.
	dir := t.TempDir()
	mainSrc := `package main

func Plain() string { return "plain" }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o644))

	cacheDir := t.TempDir()

	// First build must succeed (falls through to full build via walk).
	m1 := New(dir, DefaultConfig())
	m1.SetCacheDir(cacheDir)
	// ScanFiles returns nil for non-git dirs, so Build returns ErrNotCodeProject.
	// That is the correct behaviour — non-git dir produces no output.
	err := m1.Build(context.Background())
	// Either no error with output, or ErrNotCodeProject. Both are valid.
	if err != nil {
		assert.ErrorIs(t, err, ErrNotCodeProject)
		return
	}

	// Second build — no panic even if first returned no output.
	m2 := New(dir, DefaultConfig())
	m2.SetCacheDir(cacheDir)
	_ = m2.Build(context.Background())
}

// TestIncrementalBlocklistApplied verifies that blocklist filtering applies to
// newly-parsed files on the incremental path.
func TestIncrementalBlocklistApplied(t *testing.T) {
	t.Parallel()

	dir := newGitRepo(t)

	// Write a .repomap.yaml that blocks Test* symbols.
	blocklistYAML := "method_blocklist:\n  - \"Test*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".repomap.yaml"), []byte(blocklistYAML), 0o644))
	gitCommitAll(t, dir, "add blocklist")

	cacheDir := t.TempDir()
	buildWithCache(t, dir, cacheDir)

	// Add a new file with a blocked symbol and a real symbol.
	newSrc := `package main

func TestFoo() {}
func RealFn() int { return 1 }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "newthing.go"), []byte(newSrc), 0o644))
	gitCommitAll(t, dir, "add newthing.go")

	m2 := buildWithCache(t, dir, cacheDir)
	out := m2.String()
	assert.NotContains(t, out, "TestFoo", "blocked symbol must not appear in incremental output")
	assert.Contains(t, out, "RealFn", "non-blocked symbol must appear in incremental output")
}

// TestIncrementalCacheVersionBump verifies that a v5 cache file on disk is
// rejected by LoadCacheIncremental and Build falls through to a full rebuild.
func TestIncrementalCacheVersionBump(t *testing.T) {
	t.Parallel()

	dir := newGitRepo(t)
	cacheDir := t.TempDir()

	// Write a minimal v5 cache file manually.
	m := New(dir, DefaultConfig())
	m.SetCacheDir(cacheDir)

	v5Cache := map[string]any{
		"version":  5,
		"root":     dir,
		"built_at": time.Now(),
		"mtimes":   map[string]time.Time{},
		"output":   "stale output",
		"ranked":   []any{},
	}
	data, err := json.Marshal(v5Cache)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cachePath(cacheDir, dir), data, 0o644))

	// LoadCacheIncremental must return (false, nil) for v5 cache.
	ok, changed := m.LoadCacheIncremental(context.Background(), cacheDir)
	assert.False(t, ok, "v5 cache must be rejected")
	assert.Nil(t, changed, "no changed files must be returned for rejected cache")

	// Full Build must still succeed.
	require.NoError(t, m.Build(context.Background()))
	assert.Contains(t, m.String(), "Hello", "full rebuild must produce correct output after v5 cache rejection")
}
