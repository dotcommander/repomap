package repomap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initGitRepo creates a committed git repo at dir with all files staged.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run())
	}
	run("init")
	run("add", ".")
	run("-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "init")
}

// mkfile creates parent dirs and writes content to path.
func mkfile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// TestScanFiles_ExcludePaths verifies that files matching ExcludePaths are absent.
func TestScanFiles_ExcludePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "cmd", "main.go"), "package main")
	mkfile(t, filepath.Join(dir, "internal", "gen", "gen.go"), "package gen")
	mkfile(t, filepath.Join(dir, "internal", "util", "util.go"), "package util")
	initGitRepo(t, dir)

	cfg := &BlocklistConfig{ExcludePaths: []string{"internal/gen/*"}}
	require.NoError(t, cfg.compile())

	files, err := ScanFiles(context.Background(), dir, cfg)
	require.NoError(t, err)

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, filepath.ToSlash(f.Path))
	}

	assert.Contains(t, paths, "cmd/main.go", "cmd/main.go must be present")
	assert.Contains(t, paths, "internal/util/util.go", "internal/util/util.go must be present")
	for _, p := range paths {
		assert.NotEqual(t, "internal/gen/gen.go", p, "excluded file must not appear")
	}
}

// TestScanFiles_IncludePaths verifies that only files matching IncludePaths are returned.
func TestScanFiles_IncludePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "cmd", "main.go"), "package main")
	mkfile(t, filepath.Join(dir, "internal", "app", "app.go"), "package app")
	mkfile(t, filepath.Join(dir, "pkg", "util", "util.go"), "package util")
	initGitRepo(t, dir)

	cfg := &BlocklistConfig{IncludePaths: []string{"cmd/*"}}
	require.NoError(t, cfg.compile())

	files, err := ScanFiles(context.Background(), dir, cfg)
	require.NoError(t, err)

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, filepath.ToSlash(f.Path))
	}

	assert.Contains(t, paths, "cmd/main.go", "cmd/main.go must be present")
	for _, p := range paths {
		assert.NotEqual(t, "internal/app/app.go", p, "non-included file must not appear")
		assert.NotEqual(t, "pkg/util/util.go", p, "non-included file must not appear")
	}
}

// TestScanFiles_NilConfig verifies nil config returns all files (backward compatible).
func TestScanFiles_NilConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "cmd", "main.go"), "package main")
	mkfile(t, filepath.Join(dir, "internal", "gen", "gen.go"), "package gen")
	initGitRepo(t, dir)

	files, err := ScanFiles(context.Background(), dir, nil)
	require.NoError(t, err)

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, filepath.ToSlash(f.Path))
	}

	assert.Contains(t, paths, "cmd/main.go")
	assert.Contains(t, paths, "internal/gen/gen.go")
}
