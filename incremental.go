package repomap

import (
	"context"
	"encoding/json"
	"os"
)

// incrementalThreshold is the max fraction of total files that can change before
// we give up and do a full rebuild. Past this, the bookkeeping (rank re-seed,
// importer-count re-scan) stops being cheaper than parsing everything.
const incrementalThreshold = 0.30

// LoadCacheIncremental attempts a fast-path rebuild. Returns (true, changedRel)
// when the cache is valid for incremental use and the caller should merge
// `changedRel` (relative paths of added+modified files; deletions handled via
// side channel). Returns (false, nil) for any of:
//   - cache missing / corrupt / wrong version / wrong root
//   - cache was written for a non-git root (GitRoot=false)
//   - `git rev-parse HEAD` fails or returns empty
//   - diff between LastSHA and HEAD fails (e.g., SHA pruned by rebase)
//   - change set exceeds incrementalThreshold of total files
//
// On (true, changedRel) the Map has been hydrated with the cached state and
// its mtimes map is populated. Deleted paths have already been removed from
// m.ranked. The caller is expected to re-parse changedRel, splice the results
// back in, re-rank, re-budget, and SaveCache.
func (m *Map) LoadCacheIncremental(ctx context.Context, cacheDir string) (bool, []string) {
	path := cachePath(cacheDir, m.root)
	data, err := os.ReadFile(path)
	if err != nil {
		return false, nil
	}

	var entry diskCache
	if err := json.Unmarshal(data, &entry); err != nil {
		return false, nil
	}
	if entry.Version != cacheVersion || entry.Root != m.root {
		return false, nil
	}
	if !entry.GitRoot || entry.LastSHA == "" {
		return false, nil
	}
	if !isInsideGitRepo(m.root) {
		return false, nil
	}

	headSHA, err := gitHeadSHA(ctx, m.root)
	if err != nil || headSHA == "" {
		return false, nil
	}

	// Fast path: HEAD hasn't moved AND no worktree/untracked changes. Whole
	// cache is authoritative.
	if headSHA == entry.LastSHA {
		added, modified, deleted, diffErr := gitChangedFiles(ctx, m.root, entry.LastSHA)
		if diffErr != nil {
			return false, nil
		}
		if len(added) == 0 && len(modified) == 0 && len(deleted) == 0 {
			m.hydrateFromCache(entry)
			return true, nil
		}
		// HEAD unchanged but worktree dirty — treat those as the change set.
		return m.prepareIncremental(entry, added, modified, deleted)
	}

	added, modified, deleted, err := gitChangedFiles(ctx, m.root, entry.LastSHA)
	if err != nil {
		return false, nil
	}
	return m.prepareIncremental(entry, added, modified, deleted)
}

// prepareIncremental applies the eligibility threshold and, if the change set
// is small enough, hydrates m from the cache with deletions already pruned.
// Returns (true, changedRelPaths) where changedRel = added ∪ modified.
func (m *Map) prepareIncremental(entry diskCache, added, modified, deleted []string) (bool, []string) {
	total := len(entry.Ranked)
	changeCount := len(added) + len(modified) + len(deleted)
	if total == 0 {
		return false, nil
	}
	if float64(changeCount)/float64(total) > incrementalThreshold {
		return false, nil
	}

	m.hydrateFromCache(entry)
	m.dropPaths(append(append([]string{}, deleted...), modified...))

	changed := make([]string, 0, len(added)+len(modified))
	changed = append(changed, added...)
	changed = append(changed, modified...)
	changed = dedupePaths(changed)
	return true, changed
}

// hydrateFromCache populates the Map from the deserialized disk entry.
// Mirrors LoadCache's post-decode block. Must be called under m.mu NOT held.
func (m *Map) hydrateFromCache(entry diskCache) {
	m.mu.Lock()
	m.ranked = entry.Ranked
	m.outputs.compact = &entry.Output
	m.outputs.lines = &entry.OutputLines
	m.builtAt = entry.BuiltAt
	m.mtimes = entry.Mtimes
	m.outputs.verbose = nil
	m.outputs.detail = nil
	m.outputs.xml = nil
	m.mu.Unlock()
}

// dropPaths removes entries with matching FileSymbols.Path (relative) from
// m.ranked and m.mtimes. Caller has NOT yet re-ranked — this is pre-merge
// cleanup.
func (m *Map) dropPaths(relPaths []string) {
	if len(relPaths) == 0 {
		return
	}
	drop := make(map[string]struct{}, len(relPaths))
	for _, p := range relPaths {
		drop[p] = struct{}{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.ranked[:0]
	for _, rf := range m.ranked {
		if rf.FileSymbols == nil {
			continue
		}
		if _, remove := drop[rf.Path]; remove {
			delete(m.mtimes, joinAbs(m.root, rf.Path))
			continue
		}
		kept = append(kept, rf)
	}
	m.ranked = kept
}
