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

// TestBlocklistConfig_EmptyPattern verifies that an empty string in the blocklist
// is silently skipped and does not match any symbol.
func TestBlocklistConfig_EmptyPattern(t *testing.T) {
	t.Parallel()
	c := &BlocklistConfig{MethodBlocklist: []string{"", "  ", "Foo"}}
	require.NoError(t, c.compile())
	assert.True(t, c.ShouldSkipSymbol("Foo"), "Foo must be blocked")
	assert.False(t, c.ShouldSkipSymbol("Bar"), "Bar must not be blocked")
	assert.False(t, c.ShouldSkipSymbol(""), "empty name must not be blocked by empty pattern")
	assert.False(t, c.ShouldSkipSymbol("anything"), "arbitrary name must not be blocked by empty/whitespace patterns")
}

// TestBlocklistConfig_DotPrefixedSymbols verifies that patterns work correctly when
// symbol names start with dots or slashes (edge cases for path.Match).
func TestBlocklistConfig_DotPrefixedSymbols(t *testing.T) {
	t.Parallel()
	c := &BlocklistConfig{MethodBlocklist: []string{".*", "/^\\./", "Normal"}}
	require.NoError(t, c.compile())

	cases := []struct {
		name string
		want bool
	}{
		{".hidden", true}, // matches ".*" glob
		{".github", true}, // matches ".*" glob
		{"Normal", true},  // explicit match
		{"Public", false}, // no match
		{"notdot", false}, // no leading dot
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, c.ShouldSkipSymbol(tc.name))
		})
	}
}

// TestBlocklistConfig_OverlappingPatterns verifies that a symbol matching multiple
// patterns is blocked (first-match wins; no double-counting needed).
func TestBlocklistConfig_OverlappingPatterns(t *testing.T) {
	t.Parallel()
	// Both "Test*" and "/^Test/" match "TestFoo" — must still block, not panic.
	c := &BlocklistConfig{MethodBlocklist: []string{"Test*", "/^Test/"}}
	require.NoError(t, c.compile())
	assert.True(t, c.ShouldSkipSymbol("TestFoo"), "symbol matching multiple patterns must be blocked")
	assert.False(t, c.ShouldSkipSymbol("Regular"), "non-matching symbol must not be blocked")
}

// TestBlocklistConfig_MixedGlobAndRegex verifies that glob and regex patterns
// coexist correctly — each evaluates independently.
func TestBlocklistConfig_MixedGlobAndRegex(t *testing.T) {
	t.Parallel()
	c := &BlocklistConfig{MethodBlocklist: []string{"*Mock", "/^gen_/", "mustJSON"}}
	require.NoError(t, c.compile())

	cases := []struct {
		sym  string
		want bool
	}{
		{"ServerMock", true},  // glob *Mock
		{"gen_user", true},    // regex ^gen_
		{"mustJSON", true},    // exact glob
		{"realFunc", false},   // no match
		{"GenUser", false},    // case-sensitive regex — ^gen_ requires lowercase
		{"mockServer", false}, // glob *Mock requires suffix
	}
	for _, tc := range cases {
		t.Run(tc.sym, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, c.ShouldSkipSymbol(tc.sym))
		})
	}
}

// TestBlocklistConfig_FilterSymbols_DotfilePattern verifies filterSymbols works
// end-to-end with a pattern that targets dot-prefixed names.
func TestBlocklistConfig_FilterSymbols_DotfilePattern(t *testing.T) {
	t.Parallel()
	c := &BlocklistConfig{MethodBlocklist: []string{".*"}}
	require.NoError(t, c.compile())

	fs := &FileSymbols{
		Path: ".github/workflows/ci.yml",
		Symbols: []Symbol{
			{Name: ".hidden", Kind: "function", Exported: true},
			{Name: "Visible", Kind: "function", Exported: true},
		},
	}
	c.filterSymbols(fs)
	require.Len(t, fs.Symbols, 1)
	assert.Equal(t, "Visible", fs.Symbols[0].Name)
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
