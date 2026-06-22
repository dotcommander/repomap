package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runBriefCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := newBriefCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestBriefCmd_OnSelfRepo(t *testing.T) {
	t.Parallel()
	root := findCLITestRoot(t)
	out, err := runBriefCmd(t, root)
	require.NoError(t, err)
	assert.Contains(t, out, "# repomap")
	assert.Contains(t, out, "module github.com/dotcommander/repomap")
	assert.Contains(t, out, "## Verify")
	assert.Contains(t, out, "build: go build ./...")
	assert.Contains(t, out, "test:  go test ./...")
	assert.Contains(t, out, "## State")
	assert.Contains(t, out, "branch:")
	assert.Contains(t, out, "## Map")
}

func TestBriefCmd_DirtyCount(t *testing.T) {
	t.Parallel()
	root := initAutoTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/tmp\n\ngo 1.26\n"), 0o644))
	runGitAuto(t, root, "add", "go.mod")
	runGitAuto(t, root, "commit", "-q", "-m", "add go.mod")
	require.NoError(t, os.WriteFile(filepath.Join(root, "foo.go"), []byte("package main\n\nfunc Foo() {}\n"), 0o644))

	out, err := runBriefCmd(t, root)
	require.NoError(t, err)
	assert.Contains(t, out, "branch: main")
	assert.Contains(t, out, "dirty: 1 file(s)")
}

func TestDetectVerify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		files     map[string]string
		wantBuild string
		wantTest  string
	}{
		{"go", map[string]string{"go.mod": "module x\n"}, "go build ./...", "go test ./..."},
		{"npm", map[string]string{"package.json": `{"scripts":{"build":"tsc","test":"vitest"}}`}, "npm run build", "npm test"},
		{"makefile-both", map[string]string{"Makefile": "build:\n\techo hi\ntest:\n\techo t\n"}, "make build", "make test"},
		{"makefile-no-test", map[string]string{"Makefile": "build:\n\techo hi\n"}, "make build", "(unknown)"},
		{"empty", map[string]string{}, "(unknown)", "(unknown)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for name, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
			}
			vc := detectVerify(dir)
			assert.Equal(t, tc.wantBuild, vc.build)
			assert.Equal(t, tc.wantTest, vc.test)
		})
	}
}

func TestReadModulePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	assert.Equal(t, "", readModulePath(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.26\n"), 0o644))
	assert.Equal(t, "example.com/foo", readModulePath(dir))
}
