package repomap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContext_ReturnsSymbolSourceAndImpact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/context\n\ngo 1.22\n"), 0o644))
	src := `package contextdemo

// Config stores settings.
type Config struct {
	Name string
}

// Run starts the service.
func Run(cfg Config) error {
	return nil
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "service.go"), []byte(src), 0o644))

	m := New(dir, Config{MaxTokens: 4096, MaxTokensNoCtx: 4096})
	require.NoError(t, m.Build(context.Background()))

	got, err := m.Context("Run", ContextOptions{})
	require.NoError(t, err)

	assert.Equal(t, "Run", got.Match.Symbol.Name)
	assert.Equal(t, "service.go", got.Match.File)
	assert.Equal(t, "service.go", got.Impact.File.Path)
	require.NotEmpty(t, got.Source)
	assert.Equal(t, 9, got.Source[0].Number)
	assert.Contains(t, got.Source[0].Text, "func Run")
}

func TestContext_TruncatesLongSourceSpan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "long.go"), []byte("package p\n"), 0o644))
	m := New(dir, DefaultConfig())
	m.ranked = []RankedFile{{
		FileSymbols: &FileSymbols{
			Path: "long.go",
			Symbols: []Symbol{{
				Name:     "Long",
				Kind:     "function",
				Exported: true,
				Line:     1,
				EndLine:  10,
			}},
		},
	}}

	got, err := m.Context("Long", ContextOptions{MaxSourceLines: 2})
	require.NoError(t, err)

	assert.True(t, got.Truncated)
	assert.Len(t, got.Source, 1)
}
