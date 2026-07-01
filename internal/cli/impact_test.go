package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImpactCommandMarkdown(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runGitForImpactTest(t, root, "init")
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "http"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "auth", "token.go"), []byte(`package auth

import "sync"

func RefreshToken() string {
	var once sync.Once
	once.Do(func() {})
	return "token"
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "auth", "token_test.go"), []byte(`package auth

import "testing"

func TestRefreshToken(t *testing.T) {
	_ = RefreshToken()
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "http", "middleware.go"), []byte(`package http

import "example.com/app/internal/auth"

func Middleware() string {
	return auth.RefreshToken()
}
`), 0o644))

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"impact", "--markdown", filepath.Join(root, "internal", "auth", "token.go")})

	require.NoError(t, cmd.Execute())

	got := out.String()
	assert.Contains(t, got, "# Impact: `internal/auth/token.go`")
	assert.Contains(t, got, "- **Risk:**")
	assert.Contains(t, got, "## Imports")
	assert.Contains(t, got, "- `sync`")
	assert.Contains(t, got, "## Imported By")
	assert.Contains(t, got, "- `internal/http/middleware.go`")
	assert.Contains(t, got, "## Tests")
	assert.Contains(t, got, "- `internal/auth/token_test.go`")
	assert.Contains(t, got, "## Exported Symbols")
	assert.Contains(t, got, "- `RefreshToken (function)`")
	assert.Contains(t, got, "## Likely Test Commands")
	assert.Contains(t, got, "- `go test ./internal/auth`")
	assert.Contains(t, got, "## Read Next")
}

func TestImpactCommandRejectsMultipleFormats(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd()
	cmd.SetArgs([]string{"impact", "--json", "--markdown", "file.go"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--json and --markdown are mutually exclusive")
}

func runGitForImpactTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}
