package repomap

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// preciseCacheEntry is the on-disk format for a cached --precise call-graph
// expansion. It is kept separate from callsCacheEntry / calls-<hash>.json so a
// precise entry is never loaded as a gopls --calls entry or vice versa
// (precise-callgraph.md Decision 5).
type preciseCacheEntry struct {
	Version int           `json:"version"`
	Hash    string        `json:"hash"`
	Callers SymbolCallers `json:"callers"`
}

const preciseCacheVersion = 1

// PreciseCacheKey computes the FNV hash key for a --precise cache entry.
// Components: absolute repo root + sorted (path, content-sha256) pairs +
// toolchain version + a fixed "precise=cha" algorithm tag. It mirrors
// CallsCacheKey (calls_cache.go:27-57) but additionally folds in
// runtime.Version(): CHA output is driven by go/types behaviour that can shift
// across Go toolchains even when scanned file content is unchanged, so a
// toolchain bump must invalidate the cache (precise-callgraph.md Decision 5).
func PreciseCacheKey(root string, ranked []RankedFile) string {
	h := fnv.New64a()

	// Repo root.
	_, _ = fmt.Fprint(h, root)
	_, _ = fmt.Fprint(h, "\x00")

	// Sorted file paths and their content hashes.
	type entry struct {
		path string
		hash string
	}
	entries := make([]entry, 0, len(ranked))
	for _, rf := range ranked {
		abs := filepath.Join(root, rf.Path)
		sum, err := sha256OfFile(abs)
		if err != nil {
			continue
		}
		entries = append(entries, entry{rf.Path, sum})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	for _, e := range entries {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00", e.path, e.hash)
	}

	// Toolchain version + fixed algorithm tag (invalidates on Go upgrade or a
	// future --precise=rta variant).
	_, _ = fmt.Fprintf(h, "toolchain=%s,precise=cha", runtime.Version())

	return fmt.Sprintf("%016x", h.Sum64())
}

// preciseCachePath returns the path to the precise cache file for the given
// hash. Always precise-<hash>.json — never collides with calls-<hash>.json.
func preciseCachePath(cacheDir, hash string) string {
	return filepath.Join(cacheDir, fmt.Sprintf("precise-%s.json", hash))
}

// LoadPreciseCache loads a cached SymbolCallers map produced by --precise.
// Returns nil if the cache is missing, corrupt, or version/hash-mismatched.
func LoadPreciseCache(cacheDir, hash string) SymbolCallers {
	data, err := os.ReadFile(preciseCachePath(cacheDir, hash))
	if err != nil {
		return nil
	}
	var entry preciseCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	if entry.Version != preciseCacheVersion || entry.Hash != hash {
		return nil
	}
	return entry.Callers
}

// SavePreciseCache writes a --precise SymbolCallers map to disk atomically.
func SavePreciseCache(cacheDir, hash string, callers SymbolCallers) error {
	entry := preciseCacheEntry{
		Version: preciseCacheVersion,
		Hash:    hash,
		Callers: callers,
	}
	data, err := json.Marshal(&entry)
	if err != nil {
		return fmt.Errorf("marshal precise cache: %w", err)
	}
	path := preciseCachePath(cacheDir, hash)
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write precise cache: %w", err)
	}
	return nil
}
