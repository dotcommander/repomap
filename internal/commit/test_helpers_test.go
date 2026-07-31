package commit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc Hello() string { return \"hello\" }\n"), 0o644))
	gitRun(t, dir, "init")
	gitRun(t, dir, "add", ".")
	gitCommitAll(t, dir, "init")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	all := append([]string{"-c", "user.email=test@example.com", "-c", "user.name=Test"}, args...)
	cmd := exec.Command("git", all...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", msg)
}

func initTestRepo(t *testing.T, initial, mutations map[string]string) string {
	t.Helper()
	root := t.TempDir()
	runGitT(t, root, "init", "-q", "-b", "main")
	runGitT(t, root, "config", "user.email", "test@example.com")
	runGitT(t, root, "config", "user.name", "Test")
	runGitT(t, root, "config", "commit.gpgsign", "false")
	for path, content := range initial {
		writeFixture(t, root, path, content)
	}
	if len(initial) > 0 {
		runGitT(t, root, "add", "-A")
		runGitT(t, root, "commit", "-q", "-m", "initial")
	}
	for path, content := range mutations {
		writeFixture(t, root, path, content)
	}
	return root
}

func writeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func runGitT(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
