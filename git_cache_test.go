package repomap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCacheStatusSnapshot_NULSafeRenameAndUntracked(t *testing.T) {
	t.Parallel()

	const oid = "0123456789abcdef0123456789abcdef01234567"
	raw := "# branch.oid " + oid + "\x00" +
		"# branch.head main\x00" +
		"1 .M N... 100644 100644 100644 a b ordinary.py\x00" +
		"2 R. N... 100644 100644 100644 a b R100 renamed\n新.py\x00old name.py\x00" +
		"? untracked\n文件.py\x00"

	snapshot, err := parseCacheStatusSnapshot(raw)
	require.NoError(t, err)
	assert.Equal(t, oid, snapshot.headSHA)
	require.Equal(t, []cacheWorktreeChange{
		{path: "ordinary.py"},
		{path: "renamed\n新.py", oldPath: "old name.py", renamed: true},
		{path: "untracked\n文件.py", untracked: true},
	}, snapshot.changes)
}

func TestCacheWorktreeDigestIncludesRenameOldPathAndUnicode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	newPath := "renamed\n新.py"
	require.NoError(t, os.WriteFile(filepath.Join(root, newPath), []byte("print('new')\n"), 0o644))
	m := New(root, DefaultConfig())
	changes := []cacheWorktreeChange{{path: newPath, oldPath: "old.py"}}

	digest, err := m.cacheWorktreeDigest(changes)
	require.NoError(t, err)
	assert.NotEqual(t, cacheCleanWorktreeDigest(), digest)

	// A present old path is a different rename state than an absent one.
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.py"), []byte("print('old')\n"), 0o644))
	changedDigest, err := m.cacheWorktreeDigest(changes)
	require.NoError(t, err)
	assert.NotEqual(t, digest, changedDigest)
}

func TestParseCacheStatusSnapshotFailsClosed(t *testing.T) {
	t.Parallel()

	const oid = "0123456789abcdef0123456789abcdef01234567"
	for name, raw := range map[string]string{
		"missing_head": "? file.py\x00",
		"unmerged":     "# branch.oid " + oid + "\x00u UU N... 100644 100644 100644 100644 a b c conflict.py\x00",
		"malformed":    "# branch.oid " + oid + "\x001 .M short\x00",
		"rename_old":   "# branch.oid " + oid + "\x002 R. N... 0 0 0 a b R100 new.py\x00",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseCacheStatusSnapshot(raw)
			assert.Error(t, err)
		})
	}
}

func TestGitStatusSnapshotFailureDisablesCacheReuse(t *testing.T) {
	t.Parallel()

	m := New(t.TempDir(), DefaultConfig())
	_, err := m.gitStatusSnapshot(context.Background())
	assert.Error(t, err)
}

func TestCacheNestedGitRootTracksMapRelativeChanges(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	mapRoot := filepath.Join(repo, "nested")
	require.NoError(t, os.MkdirAll(mapRoot, 0o755))
	path := filepath.Join(mapRoot, "feature.py")
	require.NoError(t, os.WriteFile(path, []byte("def old_name():\n    return 1\n"), 0o644))
	gitRun(t, repo, "init")
	gitRun(t, repo, "add", ".")
	gitCommitAll(t, repo, "initial nested fixture")

	cacheDir := t.TempDir()
	first := buildWithCache(t, mapRoot, cacheDir)
	assert.Contains(t, first.String(), "old_name")

	require.NoError(t, os.WriteFile(path, []byte("def new_name():\n    return 2\n"), 0o644))
	second := buildWithCache(t, mapRoot, cacheDir)
	assert.NotContains(t, second.String(), "old_name")
	assert.Contains(t, second.String(), "new_name")
}
