//go:build notreesitter

package repomap

import "context"

// TreeSitterAvailable reports whether tree-sitter parsing is available.
// Always false when built with the notreesitter tag.
func TreeSitterAvailable() bool {
	return false
}

// parseTreeSitterFiles is a no-op when built without tree-sitter.
// Returns nil results and all files as fallback.
func (m *Map) parseTreeSitterFiles(_ context.Context, files []FileInfo) ([]*FileSymbols, []FileInfo) {
	return nil, files
}
