package repomap

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// callsCacheEntry is the on-disk format for a cached --calls expansion.
type callsCacheEntry struct {
	Version int           `json:"version"`
	Hash    string        `json:"hash"`
	Callers SymbolCallers `json:"callers"`
}

const callsCacheVersion = 1

// CallsCacheKey computes the FNV hash key for a --calls cache entry.
// Components: absolute repo root + sorted (path, mtime) pairs + flag combo string.
func CallsCacheKey(root string, ranked []RankedFile, cfg CallsConfig) string {
	h := fnv.New64a()

	// Repo root.
	fmt.Fprint(h, root)
	fmt.Fprint(h, "\x00")

	// Sorted file paths and their mtimes (use the path as mtime proxy —
	// we scan mtimes from disk to detect changes).
	type entry struct {
		path  string
		mtime time.Time
	}
	entries := make([]entry, 0, len(ranked))
	for _, rf := range ranked {
		abs := filepath.Join(root, rf.Path)
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		entries = append(entries, entry{rf.Path, info.ModTime()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%d\x00", e.path, e.mtime.UnixNano())
	}

	// Flag combo.
	fmt.Fprintf(h, "t=%d,l=%d,tests=%v", cfg.Threshold, cfg.Limit, cfg.IncludeTests)

	return fmt.Sprintf("%016x", h.Sum64())
}

// callsCachePath returns the path to the cache file for the given hash.
func callsCachePath(cacheDir, hash string) string {
	return filepath.Join(cacheDir, fmt.Sprintf("calls-%s.json", hash))
}

// LoadCallsCache loads a cached SymbolCallers map from disk.
// Returns nil if the cache is missing, corrupt, or version-mismatched.
func LoadCallsCache(cacheDir, hash string) SymbolCallers {
	data, err := os.ReadFile(callsCachePath(cacheDir, hash))
	if err != nil {
		return nil
	}
	var entry callsCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	if entry.Version != callsCacheVersion || entry.Hash != hash {
		return nil
	}
	return entry.Callers
}

// SaveCallsCache writes a SymbolCallers map to disk atomically.
func SaveCallsCache(cacheDir, hash string, callers SymbolCallers) error {
	entry := callsCacheEntry{
		Version: callsCacheVersion,
		Hash:    hash,
		Callers: callers,
	}
	data, err := json.Marshal(&entry)
	if err != nil {
		return fmt.Errorf("marshal calls cache: %w", err)
	}
	path := callsCachePath(cacheDir, hash)
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write calls cache: %w", err)
	}
	return nil
}
