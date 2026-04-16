package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkGitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o755))
	return dir
}

func TestInit_FreshGitDir_CreatesBoth(t *testing.T) {
	t.Parallel()
	dir := mkGitDir(t)
	var buf bytes.Buffer
	require.NoError(t, runInit(&buf, dir, false, false, false))

	cfg, err := os.ReadFile(filepath.Join(dir, ".repomap.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(cfg), "method_blocklist")

	hookPath := filepath.Join(dir, ".git", "hooks", "post-commit")
	info, err := os.Stat(hookPath)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o111, "hook must be executable")

	assert.Contains(t, buf.String(), "write .repomap.yaml")
	assert.Contains(t, buf.String(), "write .git/hooks/post-commit")
}

func TestInit_Idempotent_SkipsExisting(t *testing.T) {
	t.Parallel()
	dir := mkGitDir(t)
	var buf bytes.Buffer

	// First run creates both files.
	require.NoError(t, runInit(&buf, dir, false, false, false))

	// Second run should skip both.
	buf.Reset()
	require.NoError(t, runInit(&buf, dir, false, false, false))

	out := buf.String()
	assert.Contains(t, out, "skip  .repomap.yaml (exists)")
	assert.Contains(t, out, "skip  .git/hooks/post-commit (exists)")
}

func TestInit_Force_Overwrites(t *testing.T) {
	t.Parallel()
	dir := mkGitDir(t)

	// Pre-write stub files with different content.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".repomap.yaml"), []byte("stub"), 0o644))
	hookPath := filepath.Join(dir, ".git", "hooks", "post-commit")
	require.NoError(t, os.WriteFile(hookPath, []byte(hookMarker+"\nstub\n"), 0o755))

	var buf bytes.Buffer
	require.NoError(t, runInit(&buf, dir, true, false, false))

	cfg, err := os.ReadFile(filepath.Join(dir, ".repomap.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(cfg), "method_blocklist")

	hook, err := os.ReadFile(hookPath)
	require.NoError(t, err)
	assert.Contains(t, string(hook), hookMarker)
	assert.Contains(t, string(hook), "repomap .")
}

func TestInit_NonGitDir_SkipsHook(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no .git directory

	var buf bytes.Buffer
	require.NoError(t, runInit(&buf, dir, false, false, false))

	// Config should be written.
	cfg, err := os.ReadFile(filepath.Join(dir, ".repomap.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(cfg), "method_blocklist")

	// Hook should be skipped.
	out := buf.String()
	assert.Contains(t, out, "not a git repo")

	// No hook file created.
	_, err = os.Stat(filepath.Join(dir, ".git", "hooks", "post-commit"))
	assert.True(t, os.IsNotExist(err))
}

func TestInit_NoHook_OnlyConfig(t *testing.T) {
	t.Parallel()
	dir := mkGitDir(t)

	var buf bytes.Buffer
	require.NoError(t, runInit(&buf, dir, false, true, false))

	// Config written.
	_, err := os.ReadFile(filepath.Join(dir, ".repomap.yaml"))
	require.NoError(t, err)

	// Hook not written.
	_, err = os.Stat(filepath.Join(dir, ".git", "hooks", "post-commit"))
	assert.True(t, os.IsNotExist(err))
}

func TestInit_NoConfig_OnlyHook(t *testing.T) {
	t.Parallel()
	dir := mkGitDir(t)

	var buf bytes.Buffer
	require.NoError(t, runInit(&buf, dir, false, false, true))

	// Config not written.
	_, err := os.Stat(filepath.Join(dir, ".repomap.yaml"))
	assert.True(t, os.IsNotExist(err))

	// Hook written.
	hook, err := os.ReadFile(filepath.Join(dir, ".git", "hooks", "post-commit"))
	require.NoError(t, err)
	assert.Contains(t, string(hook), hookMarker)
}

func TestInit_ExistingForeignHook_RefusesWithoutForce(t *testing.T) {
	t.Parallel()
	dir := mkGitDir(t)

	// Pre-write a hook without the repomap marker.
	hookPath := filepath.Join(dir, ".git", "hooks", "post-commit")
	require.NoError(t, os.WriteFile(hookPath, []byte("#!/bin/sh\necho foreign\n"), 0o755))

	// Without --force: must error.
	var buf bytes.Buffer
	err := runInit(&buf, dir, false, false, true) // noConfig=true to isolate hook
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge manually")

	// With --force: overwrites.
	buf.Reset()
	require.NoError(t, runInit(&buf, dir, true, false, true))

	hook, err := os.ReadFile(hookPath)
	require.NoError(t, err)
	assert.Contains(t, string(hook), hookMarker)
}
