package repomap

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreciseCache_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hash := "0123456789abcdef"
	want := SymbolCallers{
		callsKey("a.go", "Get"): {{File: "main.go", Line: 8}},
		callsKey("b.go", "Get"): {{File: "main.go", Line: 12}},
	}
	require.NoError(t, SavePreciseCache(dir, hash, want))
	got := LoadPreciseCache(dir, hash)
	require.Equal(t, want, got)
}

func TestPreciseCache_MissOnKeyMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, SavePreciseCache(dir, "0123456789abcdef",
		SymbolCallers{callsKey("a.go", "Get"): {{File: "main.go", Line: 8}}}))
	// A different key — as a toolchain bump would produce — is a clean miss.
	require.Nil(t, LoadPreciseCache(dir, "deadbeefdeadbeef"))
}

func TestPreciseCache_MissOnVersionMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hash := "0123456789abcdef"
	// Hand-write an entry with an incompatible schema version.
	data, err := json.Marshal(preciseCacheEntry{
		Version: preciseCacheVersion + 1,
		Hash:    hash,
		Callers: SymbolCallers{callsKey("a.go", "Get"): {{File: "main.go", Line: 8}}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(preciseCachePath(dir, hash), data, 0o644))
	require.Nil(t, LoadPreciseCache(dir, hash))
}

func TestPreciseCacheKey_StableAndRootSensitive(t *testing.T) {
	t.Parallel()
	k1 := PreciseCacheKey("/repo/a", nil)
	require.Equal(t, k1, PreciseCacheKey("/repo/a", nil))    // stable for same inputs
	require.NotEqual(t, k1, PreciseCacheKey("/repo/b", nil)) // sensitive to root
	require.Len(t, k1, 16)                                   // %016x
}
