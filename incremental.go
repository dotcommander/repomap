package repomap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// incrementalThreshold is the max fraction of total files that can change before
// we give up and do a full rebuild. Past this, the bookkeeping (rank re-seed,
// importer-count re-scan) stops being cheaper than parsing everything.
const incrementalThreshold = 0.30

// cacheLoadPlan is the internal result used by Build. exactHit means the cache
// already represents the precise HEAD+worktree contents and must not be
// rewritten merely to refresh its timestamp.
type cacheLoadPlan struct {
	changed         []string
	persistMetadata bool
	exactHit        bool
}

// LoadCacheIncremental preserves the historical public fast-path API. Build
// consumes cacheLoadPlan directly so it can distinguish exact cache hits from
// incremental merges that need cache metadata persisted.
func (m *Map) LoadCacheIncremental(ctx context.Context, cacheDir string) (bool, []string) {
	plan, ok := m.cacheLoadPlan(ctx, cacheDir)
	if !ok {
		return false, nil
	}
	return true, plan.changed
}

// cacheLoadPlan attempts a fast-path rebuild. It returns false for any of:
//   - cache missing / corrupt / wrong version / wrong root
//   - cache was written for a non-git root (GitRoot=false)
//   - Git status cannot produce a complete HEAD+worktree snapshot
//   - diff between LastSHA and HEAD fails (e.g., SHA pruned by rebase)
//   - change set exceeds incrementalThreshold of total files
//
// On success the Map has been hydrated with cached state and deleted paths have
// already been removed from m.ranked for non-exact plans.
func (m *Map) cacheLoadPlan(ctx context.Context, cacheDir string) (cacheLoadPlan, bool) {
	path := cachePath(cacheDir, m.root)
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheLoadPlan{}, false
	}

	var entry diskCache
	if err := json.Unmarshal(data, &entry); err != nil {
		return cacheLoadPlan{}, false
	}
	if !m.cacheEntryValid(&entry) {
		return cacheLoadPlan{}, false
	}
	if !entry.GitRoot || entry.LastSHA == "" || entry.WorktreeDigest == "" {
		return cacheLoadPlan{}, false
	}

	snapshot, err := m.gitStatusSnapshot(ctx)
	if err != nil {
		return cacheLoadPlan{}, false
	}

	if snapshot.headSHA == entry.LastSHA && snapshot.digest == entry.WorktreeDigest {
		m.hydrateFromCache(entry)
		return cacheLoadPlan{exactHit: true}, true
	}

	// A dirty cache cannot safely be composed with a changed worktree or a new
	// HEAD: its cached symbols came from an unknown overlay rather than LastSHA.
	if entry.WorktreeDigest != cacheCleanWorktreeDigest() {
		return cacheLoadPlan{}, false
	}

	added, modified, deleted, err := m.snapshotChangedFiles(snapshot)
	if err != nil {
		return cacheLoadPlan{}, false
	}
	if snapshot.headSHA != entry.LastSHA {
		committedAdded, committedModified, committedDeleted, diffErr := gitCommittedChangedFiles(ctx, m.root, entry.LastSHA)
		if diffErr != nil {
			return cacheLoadPlan{}, false
		}
		committedAdded, committedModified, committedDeleted = m.filterCacheRelevantChanges(committedAdded, committedModified, committedDeleted)
		added = append(added, committedAdded...)
		modified = append(modified, committedModified...)
		deleted = append(deleted, committedDeleted...)
	}

	ok, changed := m.prepareIncremental(entry, dedupePaths(added), dedupePaths(modified), dedupePaths(deleted))
	if !ok {
		return cacheLoadPlan{}, false
	}
	return cacheLoadPlan{changed: changed, persistMetadata: true}, true
}

func (m *Map) filterCacheRelevantChanges(added, modified, deleted []string) ([]string, []string, []string) {
	filter := func(paths []string) []string {
		out := make([]string, 0, len(paths))
		for _, path := range paths {
			if m.cacheRelevantPath(path) {
				out = append(out, path)
			}
		}
		return out
	}
	return filter(added), filter(modified), filter(deleted)
}

// prepareIncremental applies the eligibility threshold and, if the change set
// is small enough, hydrates m from the cache with deletions already pruned.
// Returns (true, changedRelPaths) where changedRel = added ∪ modified.
func (m *Map) prepareIncremental(entry diskCache, added, modified, deleted []string) (bool, []string) {
	if containsGoSemanticInput(added, modified, deleted) {
		return false, nil
	}
	total := len(entry.Ranked)
	changeCount := len(added) + len(modified) + len(deleted)
	if total == 0 {
		return false, nil
	}
	if float64(changeCount)/float64(total) > incrementalThreshold {
		return false, nil
	}

	m.hydrateFromCache(entry)
	m.dropPaths(slices.Concat(deleted, modified))

	changed := make([]string, 0, len(added)+len(modified))
	changed = append(changed, added...)
	changed = append(changed, modified...)
	changed = dedupePaths(changed)
	return true, changed
}

// containsGoSemanticInput reports changes that invalidate the whole-program Go
// semantic graph. It runs before cache hydration so deleted callers cannot
// survive in the cached semantic caller projection.
func containsGoSemanticInput(changes ...[]string) bool {
	for _, paths := range changes {
		for _, path := range paths {
			base := filepath.Base(path)
			if filepath.Ext(path) == ".go" || base == "go.mod" || base == "go.sum" || base == "go.work" {
				return true
			}
		}
	}
	return false
}

// hydrateFromCache populates the Map from the deserialized disk entry.
// Must be called with m.mu NOT held.
func (m *Map) hydrateFromCache(entry diskCache) {
	m.mu.Lock()
	m.ranked = entry.Ranked
	m.builtAt = entry.BuiltAt
	m.mtimes = entry.Mtimes
	m.contentHashes = entry.ContentHashes // nil for old caches → mtime-only fallback
	m.scanFP = entry.ScanFP
	m.coverage = entry.Coverage
	m.semanticCallers = entry.SemanticCallers
	m.goDiagnostics = entry.GoDiagnostics
	// One source of truth for derived-output invalidation: reset() clears every
	// format (including ones added later), then the two cache-persisted strings
	// are restored.
	m.outputs.reset()
	m.outputs.compact = &entry.Output
	m.outputs.lines = &entry.OutputLines
	m.mu.Unlock()
}

// applyIncremental re-parses only the changed paths, merges them into the
// already-hydrated m.ranked, re-detects implementations over the full merged
// set, re-ranks, and saves the cache. Returns an error if re-parsing fails
// entirely; caller must fall through to a full rebuild.
func (m *Map) applyIncremental(ctx context.Context, changedRel []string, persistMetadata bool) error {
	if len(changedRel) == 0 {
		// Nothing to re-parse — cache is authoritative. Still refresh builtAt
		// and save so LastSHA advances if HEAD moved without touching tracked
		// files (rare but possible). No fingerprint change needed — hydrated
		// ScanFP already matches the tree as it stands.
		m.mu.Lock()
		m.builtAt = time.Now()
		m.mu.Unlock()
		if persistMetadata && m.cacheDir != "" {
			_ = m.SaveCacheContext(ctx, m.cacheDir)
		}
		return nil
	}

	// Build FileInfo list for changed paths that still exist and have a
	// recognised language. Silently skip unknown extensions / missing files —
	// deletions are already applied to m.ranked by LoadCacheIncremental.
	infos := make([]FileInfo, 0, len(changedRel))
	for _, rel := range changedRel {
		abs := m.absPath(rel)
		info, err := os.Stat(abs)
		if err != nil {
			continue // deleted or unreadable — drop silently
		}
		if info.IsDir() {
			continue
		}
		if tooBig(abs, m.config.MaxFileSize) || isBuildArtifact(rel) || inSkipDir(rel) {
			continue
		}
		lang := LanguageFor(filepath.Ext(rel))
		if lang == "" {
			continue
		}
		infos = append(infos, FileInfo{Path: rel, Language: lang})
	}

	var parsed []*FileSymbols
	var newMtimes map[string]time.Time
	var newHashes map[string]string
	if len(infos) > 0 {
		var err error
		parsed, newMtimes, newHashes, _, err = m.parseFiles(ctx, infos)
		if err != nil {
			return err
		}
	}

	// Build a set of paths being replaced so we can skip them from cached ranked.
	relNew := make(map[string]struct{}, len(parsed))
	for _, fs := range parsed {
		if fs != nil {
			relNew[fs.Path] = struct{}{}
		}
	}

	m.mu.Lock()
	// Carry forward existing RankedFiles (modified paths were already dropped
	// from m.ranked by LoadCacheIncremental.dropPaths; this is defensive).
	// RankFiles mutates symbol metadata, so detach the carried-forward file
	// symbols before releasing the lock rather than mutating published state.
	cachedRanked := cloneRanked(m.ranked)
	existing := make([]*FileSymbols, 0, len(m.ranked)+len(parsed))
	for _, rf := range cachedRanked {
		if rf.FileSymbols == nil {
			continue
		}
		if _, re := relNew[rf.Path]; re {
			continue // replaced by freshly parsed version (defensive)
		}
		existing = append(existing, rf.FileSymbols)
	}
	for _, fs := range parsed {
		if fs != nil {
			existing = append(existing, fs)
		}
	}
	// Refresh mtimes and hashes for newly parsed files.
	if m.mtimes == nil {
		m.mtimes = make(map[string]time.Time, len(existing))
	}
	for path, t := range newMtimes {
		m.mtimes[path] = t
	}
	if len(newHashes) > 0 {
		if m.contentHashes == nil {
			m.contentHashes = make(map[string]string, len(newHashes))
		}
		for path, h := range newHashes {
			m.contentHashes[path] = h
		}
	}
	m.mu.Unlock()

	// Go inputs force a full rebuild above, so cached semantic implementation
	// relationships remain authoritative during a non-Go incremental merge.
	ranked := RankFiles(existing)
	ranked = m.applyRankPasses(ranked)

	// The file set may have changed; refresh the fingerprint from a real scan
	// so Stale() compares against exactly what a rebuild would discover.
	// On scan failure keep the old fingerprint — a later false-stale just
	// triggers a full rebuild, which self-heals.
	newFP := ""
	if files, scanErr := scanFilesLimited(ctx, m.root, m.blocklist, m.config.MaxFileSize); scanErr == nil {
		newFP = scanFingerprint(files)
	}

	m.mu.Lock()
	m.ranked = ranked
	m.builtAt = time.Now()
	m.coverage = parseCoverageFromRanked(ranked, m.tsAvailable, m.ctagsAvailable)
	if newFP != "" {
		m.scanFP = newFP
	}
	m.outputs.reset()
	m.mu.Unlock()

	if persistMetadata && m.cacheDir != "" {
		_ = m.SaveCacheContext(ctx, m.cacheDir)
	}
	return nil
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
