package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// TestBriefCmd_DigestFormat pins the exact header block (everything before the
// appended map) so accidental wording/spacing drift in this agent-facing output
// is caught. The temp repo is fully controlled: branch main, a committed go.mod
// fixing module+identity+verify lines, and exactly one untracked file → dirty 1.
func TestBriefCmd_DigestFormat(t *testing.T) {
	t.Parallel()
	root := initAutoTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644))
	runGitAuto(t, root, "add", "go.mod")
	runGitAuto(t, root, "commit", "-q", "-m", "add go.mod")
	require.NoError(t, os.WriteFile(filepath.Join(root, "foo.go"), []byte("package main\n\nfunc Foo() {}\n"), 0o644))

	out, err := runBriefCmd(t, root)
	require.NoError(t, err)

	const marker = "## Map\n"
	idx := strings.Index(out, marker)
	require.GreaterOrEqual(t, idx, 0, "output missing %q:\n%s", marker, out)
	start := strings.Index(out, "# demo")
	require.GreaterOrEqual(t, start, 0, "output missing title:\n%s", out)
	header := out[start : idx+len(marker)]

	assert.Contains(t, out, ", agent — here's your briefing.")

	want := "# demo — Go module\n" +
		"  module example.com/demo\n" +
		"\n## Verify\n" +
		"  build: go build ./...\n" +
		"  test:  go test ./...\n" +
		"\n## State\n" +
		"  branch: main   dirty: 1 file(s)\n" +
		"    ?? foo.go\n" +
		"  recent:\n" +
		"    add go.mod\n" +
		"    initial\n" +
		"\n## Map\n"
	assert.Equal(t, want, header)
}

func TestBriefRules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	assert.Equal(t, "", briefRules(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# rules\n"), 0o644))
	assert.Equal(t, "\n## Rules\n  conventions: CLAUDE.md — read before editing\n", briefRules(dir))
}

func TestGreeting(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Good morning", greeting(0))
	assert.Equal(t, "Good morning", greeting(11))
	assert.Equal(t, "Good afternoon", greeting(12))
	assert.Equal(t, "Good afternoon", greeting(17))
	assert.Equal(t, "Good evening", greeting(18))
	assert.Equal(t, "Good evening", greeting(23))
}
