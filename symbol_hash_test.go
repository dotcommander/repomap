package repomap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeGoFileForHash writes src to a temp dir (with a go.mod so ParseGoFile is
// happy), parses it, populates per-symbol hashes, and returns the symbols by name.
func writeGoFileForHash(t *testing.T, src string) map[string]Symbol {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module hashtest\n\ngo 1.26\n"), 0o644))
	path := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	fs, err := ParseGoFile(path, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)
	populateSymbolHashes(fs, path)

	byName := make(map[string]Symbol, len(fs.Symbols))
	for _, s := range fs.Symbols {
		byName[s.Name] = s
	}
	return byName
}

// TestSymbolHash_NonEmptyForKnownSpans verifies symbols with a real Line..EndLine
// span receive a non-empty sha256 hex hash.
func TestSymbolHash_NonEmptyForKnownSpans(t *testing.T) {
	t.Parallel()
	const src = `package sample

// Alpha does a thing.
func Alpha() int {
	return 1
}

// Beta does another thing.
func Beta() int {
	return 2
}
`
	syms := writeGoFileForHash(t, src)
	require.Contains(t, syms, "Alpha")
	require.Contains(t, syms, "Beta")
	assert.NotEmpty(t, syms["Alpha"].Hash, "Alpha has a known span; hash must be populated")
	assert.NotEmpty(t, syms["Beta"].Hash, "Beta has a known span; hash must be populated")
	assert.Len(t, syms["Alpha"].Hash, 64, "sha256 hex is 64 chars")
}

// TestSymbolHash_BodyChangeIsolated is the core behavioral guarantee: changing
// ONE function's body changes only that symbol's hash; an unrelated symbol's
// hash stays stable. The per-FILE hash cannot make this distinction.
func TestSymbolHash_BodyChangeIsolated(t *testing.T) {
	t.Parallel()
	const before = `package sample

func Alpha() int {
	return 1
}

func Beta() int {
	return 2
}
`
	// Change Alpha's body; Beta's source bytes are unchanged. A trailing
	// blank-line tweak elsewhere must NOT move Beta's hash, because Beta's
	// own Line..EndLine span bytes are identical.
	const after = `package sample

func Alpha() int {
	return 42
}

func Beta() int {
	return 2
}
`
	b := writeGoFileForHash(t, before)
	a := writeGoFileForHash(t, after)

	require.Contains(t, b, "Alpha")
	require.Contains(t, a, "Alpha")
	require.Contains(t, b, "Beta")
	require.Contains(t, a, "Beta")

	assert.NotEqual(t, b["Alpha"].Hash, a["Alpha"].Hash, "Alpha body changed → its hash must change")
	assert.Equal(t, b["Beta"].Hash, a["Beta"].Hash, "Beta source bytes unchanged → its hash must stay stable")
}

// TestSymbolHash_EmptyWhenNoSpan verifies a symbol with no usable span
// (Line/EndLine zeroed) gets an empty hash rather than a bogus one.
func TestSymbolHash_EmptyWhenNoSpan(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module hashtest\n\ngo 1.26\n"), 0o644))
	path := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(path, []byte("package sample\n\nfunc Alpha() int { return 1 }\n"), 0o644))

	fs, err := ParseGoFile(path, dir)
	require.NoError(t, err)
	require.NotNil(t, fs)

	// Zero out spans to simulate a parser that could not determine them.
	for i := range fs.Symbols {
		fs.Symbols[i].Line = 0
		fs.Symbols[i].EndLine = 0
	}
	populateSymbolHashes(fs, path)
	for _, s := range fs.Symbols {
		assert.Empty(t, s.Hash, "symbol %q has no span; hash must be empty", s.Name)
	}
}
