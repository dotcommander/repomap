package repomap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyPrepStateFresh_CleanPasses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := newGitRepo(t)

	groups := []CommitGroup{{ID: "g1", Files: []string{"main.go"}}}
	headSHA, fileHashes, err := BuildPrepStateBinding(ctx, dir, groups)
	require.NoError(t, err)

	state := &PrepState{RepoRoot: dir, HeadSHA: headSHA, FileHashes: fileHashes}
	require.NoError(t, VerifyPrepStateFresh(ctx, state))
}

func TestVerifyPrepStateFresh_HeadMoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := newGitRepo(t)

	groups := []CommitGroup{{ID: "g1", Files: []string{"main.go"}}}
	headSHA, fileHashes, err := BuildPrepStateBinding(ctx, dir, groups)
	require.NoError(t, err)

	// Move HEAD by committing a change.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Changed() int { return 2 }\n"), 0o644))
	gitCommitAll(t, dir, "change main.go")

	state := &PrepState{RepoRoot: dir, HeadSHA: headSHA, FileHashes: fileHashes}
	err = VerifyPrepStateFresh(ctx, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HEAD moved")
}

func TestVerifyPrepStateFresh_FileChanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := newGitRepo(t)

	groups := []CommitGroup{{ID: "g1", Files: []string{"main.go"}}}
	headSHA, fileHashes, err := BuildPrepStateBinding(ctx, dir, groups)
	require.NoError(t, err)

	// Modify main.go WITHOUT committing.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Changed() int { return 3 }\n"), 0o644))

	state := &PrepState{RepoRoot: dir, HeadSHA: headSHA, FileHashes: fileHashes}
	err = VerifyPrepStateFresh(ctx, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed since prep")
	assert.Contains(t, err.Error(), "main.go")
}

func TestVerifyPrepStateFresh_LegacyStateRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	state := &PrepState{RepoRoot: dir, HeadSHA: ""}
	err := VerifyPrepStateFresh(ctx, state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "predates freshness binding")
}

func TestPersistPrepStateAt_RoundTrip(t *testing.T) {
	t.Parallel()

	state := &PrepState{RepoRoot: "/tmp/fake-repo-roundtrip"}
	token, err := PersistPrepState(state)
	require.NoError(t, err)
	t.Cleanup(func() { _ = DeletePrepState(token) })

	// Mutate in memory then rewrite.
	state.HeadSHA = "deadbeef"
	require.NoError(t, PersistPrepStateAt(token, state))

	// Reload and verify the mutation persisted.
	loaded, err := LoadPrepState(token)
	require.NoError(t, err)
	assert.Equal(t, "deadbeef", loaded.HeadSHA)
}

func TestDeletePrepState_Idempotent(t *testing.T) {
	t.Parallel()

	state := &PrepState{RepoRoot: "/tmp/fake-repo-idempotent"}
	token, err := PersistPrepState(state)
	require.NoError(t, err)

	// First delete — should succeed.
	require.NoError(t, DeletePrepState(token))

	// Second delete — idempotent, must not error.
	require.NoError(t, DeletePrepState(token))

	// Load should fail now.
	_, err = LoadPrepState(token)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file"),
		"expected load error to indicate missing file, got: %v", err)
}
