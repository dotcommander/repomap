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
	if entry.GitRoot {
		// Git cache entries are keyed by an exact worktree digest, not per-file
		// mtimes. This keeps a saved dirty cache fresh when its contents have not
		// changed, including after staging or unstaging those same bytes.
		m := New(root, DefaultConfig())
		snapshot, snapshotErr := m.gitStatusSnapshot(ctx)
		if snapshotErr != nil {
			status.Stale = true
			status.Reason = "git_unavailable"
			return status
		}
		status.CurrentHead = snapshot.headSHA
		if entry.LastSHA != snapshot.headSHA {
			status.Stale = true
			status.Reason = "head_changed"
			return status
		}
		if entry.WorktreeDigest != snapshot.digest {
			status.Stale = true
			status.Reason = "content_changed"
			return status
		}
		status.Reason = "fresh"
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
			currentHash, hErr := sha256OfFile(path)
			if hErr != nil || currentHash != savedHash {
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
