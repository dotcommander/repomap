package repomap

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

// CacheStatus describes the usability and freshness of one disk cache entry.
type CacheStatus struct {
	CachePath    string     `json:"cache_path"`
	Exists       bool       `json:"exists"`
	Usable       bool       `json:"usable"`
	Stale        bool       `json:"stale"`
	Reason       string     `json:"reason,omitempty"`
	Root         string     `json:"root,omitempty"`
	BuiltAt      *time.Time `json:"built_at,omitempty"`
	TrackedFiles int        `json:"tracked_files,omitempty"`
	SavedHead    string     `json:"saved_head,omitempty"`
	CurrentHead  string     `json:"current_head,omitempty"`
	GitRoot      bool       `json:"git_root,omitempty"`
	Version      int        `json:"version,omitempty"`
}

// InspectCache reports whether the cache for root is present, loadable, and fresh.
func InspectCache(ctx context.Context, root, cacheDir string) CacheStatus {
	status := CacheStatus{CachePath: cachePath(cacheDir, root)}
	data, err := os.ReadFile(status.CachePath)
	if err != nil {
		status.Reason = "missing_cache"
		return status
	}
	status.Exists = true

	var entry diskCache
	if err := json.Unmarshal(data, &entry); err != nil {
		status.Reason = "corrupt_cache"
		return status
	}

	status.Version = entry.Version
	status.Root = entry.Root
	if !entry.BuiltAt.IsZero() {
		status.BuiltAt = &entry.BuiltAt
	}
	status.TrackedFiles = len(entry.Mtimes)
	status.SavedHead = entry.LastSHA
	status.GitRoot = entry.GitRoot

	if entry.Version != cacheVersion {
		status.Reason = "version_mismatch"
		return status
	}
	if entry.Root != root {
		status.Reason = "root_mismatch"
		return status
	}

	status.Usable = true
	status.CurrentHead, _ = gitHeadSHA(ctx, root)
	if entry.LastSHA != "" && status.CurrentHead != "" && entry.LastSHA != status.CurrentHead {
		status.Stale = true
		status.Reason = "head_changed"
		return status
	}

	if stale, reason := cacheEntryFilesStale(entry); stale {
		status.Stale = true
		status.Reason = reason
		return status
	}

	status.Reason = "fresh"
	return status
}

func cacheEntryFilesStale(entry diskCache) (bool, string) {
	for path, recorded := range entry.Mtimes {
		info, err := os.Stat(path)
		if err != nil {
			return true, "tracked_file_missing"
		}
		if savedHash := entry.ContentHashes[path]; savedHash != "" {
			if currentHash := sha256OfFile(path); currentHash == "" || currentHash != savedHash {
				return true, "content_changed"
			}
			continue
		}
		if info.ModTime().After(recorded) {
			return true, "mtime_changed"
		}
	}
	return false, ""
}
