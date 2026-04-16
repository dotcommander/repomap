package repomap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlocklistConfig_ShouldSkipSymbol_Glob(t *testing.T) {
	t.Parallel()
	c := &BlocklistConfig{MethodBlocklist: []string{"Test*", "*Mock", "mustJSON"}}
	require.NoError(t, c.compile())

	cases := []struct {
		name string
		want bool
	}{
		{"TestFoo", true},
		{"TestBar", true},
		{"Regular", false},
		{"UserMock", true},
		{"MockUser", false},
		{"mustJSON", true},
		{"mustJson", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, c.ShouldSkipSymbol(tc.name))
		})
	}
}

func TestBlocklistConfig_ShouldSkipSymbol_Regex(t *testing.T) {
	t.Parallel()
	c := &BlocklistConfig{MethodBlocklist: []string{"/^pb_/", "/Marshal$/"}}
	require.NoError(t, c.compile())

	cases := []struct {
		name string
		want bool
	}{
		{"pb_User", true},
		{"Pb_User", false},
		{"User", false},
		{"UnmarshalJSON", false},
		{"MarshalJSON", false},
		{"fooMarshal", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, c.ShouldSkipSymbol(tc.name))
		})
	}
}

func TestBlocklistConfig_ShouldSkipSymbol_NilReceiver(t *testing.T) {
	t.Parallel()
	var c *BlocklistConfig
	assert.False(t, c.ShouldSkipSymbol("anything"))
}

func TestBlocklistConfig_ShouldSkipSymbol_EmptyConfig(t *testing.T) {
	t.Parallel()
	c := &BlocklistConfig{}
	assert.False(t, c.ShouldSkipSymbol("anything"))
}

func TestLoadBlocklistConfig_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c, err := LoadBlocklistConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.False(t, c.ShouldSkipSymbol("TestFoo"))
}

func TestLoadBlocklistConfig_Malformed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".repomap.yaml")
	require.NoError(t, os.WriteFile(path, []byte("method_blocklist: [\n"), 0o644))
	_, err := LoadBlocklistConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".repomap.yaml")
}

func TestLoadBlocklistConfig_InvalidRegex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".repomap.yaml")
	require.NoError(t, os.WriteFile(path, []byte("method_blocklist:\n  - \"/[/\"\n"), 0o644))
	_, err := LoadBlocklistConfig(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex")
}

func TestLoadBlocklistConfig_Valid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `method_blocklist:
  - "Test*"
  - "/^pb_/"
  - "mustJSON"
`
	path := filepath.Join(dir, ".repomap.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))
	c, err := LoadBlocklistConfig(dir)
	require.NoError(t, err)
	assert.True(t, c.ShouldSkipSymbol("TestFoo"))
	assert.True(t, c.ShouldSkipSymbol("pb_User"))
	assert.True(t, c.ShouldSkipSymbol("mustJSON"))
	assert.False(t, c.ShouldSkipSymbol("Regular"))
}

// TestBlocklistIntegration verifies Build filters symbols matching the blocklist.
// Requires a git repo because ScanFiles returns nil outside one.
func TestBlocklistIntegration(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()

	goSrc := `package demo

// Foo is kept.
func Foo() {}

// TestFoo should be filtered out.
func TestFoo() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "demo.go"), []byte(goSrc), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module demo\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".repomap.yaml"),
		[]byte("method_blocklist:\n  - \"Test*\"\n"), 0o644))

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		_ = cmd.Run()
	}
	runGit("init")
	runGit("add", ".")
	runGit("-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "init")

	m := New(dir, DefaultConfig())
	require.NoError(t, m.Build(context.Background()))

	out := m.StringVerbose()
	assert.Contains(t, out, "Foo", "Foo must be kept")
	assert.False(t, strings.Contains(out, "TestFoo"),
		"TestFoo must be filtered; got: %s", out)
}
