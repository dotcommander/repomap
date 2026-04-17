package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_GoplsRefs runs a real gopls session against a small fixture
// and verifies that References returns results. Skipped under -short.
func TestIntegration_GoplsRefs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test under -short")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not in PATH")
	}

	dir := makeGoFixture(t)

	ctx := context.Background()
	mgr := NewManager(dir)
	t.Cleanup(func() { mgr.Shutdown(context.Background()) })

	file := filepath.Join(dir, "main.go")
	client, lang, err := mgr.ForFile(ctx, file)
	require.NoError(t, err)
	require.Equal(t, "go", lang)

	require.NoError(t, mgr.EnsureFileOpen(ctx, client, file, lang))

	// Line 5 (0-based: 4) in the fixture is `func Hello() string {`
	// Column of "Hello" is 5 ("func " = 5 chars before the identifier).
	locs, err := client.References(ctx, file, 4, 5)
	require.NoError(t, err)
	assert.NotEmpty(t, locs, "expected at least one reference to Hello")
}

// TestIntegration_GoplsSymbols verifies DocumentSymbols returns named symbols
// with correct line numbers.
func TestIntegration_GoplsSymbols(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test under -short")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not in PATH")
	}

	dir := makeGoFixture(t)

	ctx := context.Background()
	mgr := NewManager(dir)
	t.Cleanup(func() { mgr.Shutdown(context.Background()) })

	file := filepath.Join(dir, "main.go")
	client, lang, err := mgr.ForFile(ctx, file)
	require.NoError(t, err)

	require.NoError(t, mgr.EnsureFileOpen(ctx, client, file, lang))

	syms, err := client.DocumentSymbols(ctx, file)
	require.NoError(t, err)
	require.NotEmpty(t, syms)

	// Find the Hello symbol.
	var helloLine int = -1
	for _, s := range syms {
		if s.Name == "Hello" {
			helloLine = s.Range.Start.Line + 1
			break
		}
	}
	assert.Equal(t, 5, helloLine, "Hello should be on line 5 (1-based)")
}

// TestIntegration_GoplsHover verifies Hover returns content for a known symbol.
func TestIntegration_GoplsHover(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test under -short")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not in PATH")
	}

	dir := makeGoFixture(t)

	ctx := context.Background()
	mgr := NewManager(dir)
	t.Cleanup(func() { mgr.Shutdown(context.Background()) })

	file := filepath.Join(dir, "main.go")
	client, lang, err := mgr.ForFile(ctx, file)
	require.NoError(t, err)
	require.NoError(t, mgr.EnsureFileOpen(ctx, client, file, lang))

	// Line 5 (0-based: 4), col 5 = "Hello".
	hover, err := client.Hover(ctx, file, 4, 5)
	require.NoError(t, err)
	require.NotNil(t, hover)
	assert.Contains(t, hover.Contents.Value, "Hello")
}

// ---------------------------------------------------------------------------
// Fixture builder
// ---------------------------------------------------------------------------

// makeGoFixture creates a minimal Go module in a temp dir and returns its path.
// Layout:
//
//	go.mod
//	main.go  — package main, func Hello() string, func main() { Hello() }
func makeGoFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	modContent := "module example.com/fixture\n\ngo 1.21\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(modContent), 0o644))

	mainContent := `package main

import "fmt"

func Hello() string {
	return "hello"
}

func main() {
	fmt.Println(Hello())
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainContent), 0o644))
	return dir
}
