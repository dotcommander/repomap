//go:build notreesitter

package repomap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNonGoFileFallsBackWithoutTreeSitter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "handler.ts")
	require.NoError(t, os.WriteFile(path, []byte("export function handle() {}\n"), 0o600))

	assert.False(t, TreeSitterAvailable())
	symbols := parseNonGoFile(path, root, "typescript")
	require.NotNil(t, symbols)
	assert.Equal(t, "regex", symbols.ParseMethod)
	require.Len(t, symbols.Symbols, 1)
	assert.Equal(t, "handle", symbols.Symbols[0].Name)
}
