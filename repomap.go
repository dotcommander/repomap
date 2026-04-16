package repomap

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

// ErrNotCodeProject is returned by Build when the target directory contains no
// recognisable source files. Callers should treat this as a normal condition,
// not an error — the project is simply not a code project.
var ErrNotCodeProject = errors.New("no source files found")

const (
	defaultMaxTokens      = 1024
	defaultMaxTokensNoCtx = 2048
	staleDebounce         = 30 * time.Second
)

// Config holds repomap configuration.
type Config struct {
	MaxTokens      int // token budget for output (default: 1024)
	MaxTokensNoCtx int // budget when no files in conversation (default: 2048)
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		MaxTokens:      defaultMaxTokens,
		MaxTokensNoCtx: defaultMaxTokensNoCtx,
	}
}

// Map holds the built repository map state.
type Map struct {
	root      string
	config    Config
	blocklist *BlocklistConfig
	cacheDir  string // if set, Build saves cache here
	mu        sync.RWMutex
	ranked    []RankedFile
	builtAt   time.Time
	mtimes    map[string]time.Time // path → mtime at last build
	outputs   outputCache

	tsAvailable    bool // tree-sitter parsing available
	ctagsAvailable bool // ctags binary available
}

// New creates a new Map for the given project root.
func New(root string, cfg Config) *Map {
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	if cfg.MaxTokensNoCtx == 0 {
		cfg.MaxTokensNoCtx = defaultMaxTokensNoCtx
	}
	bl, _ := LoadBlocklistConfig(root)
	if bl == nil {
		bl = &BlocklistConfig{}
	}
	return &Map{
		root:           root,
		config:         cfg,
		blocklist:      bl,
		tsAvailable:    TreeSitterAvailable(),
		ctagsAvailable: CtagsAvailable(),
	}
}

// SetCacheDir enables disk caching. Build() will save to this directory.
func (m *Map) SetCacheDir(dir string) {
	m.cacheDir = dir
}

// Build performs a full scan → parse → rank pipeline.
// When cacheDir is set, first tries an incremental rebuild via git diff
// against the cached HEAD SHA. Falls through to full rebuild on any
// eligibility failure — correctness over speed.
// Safe for concurrent use.
func (m *Map) Build(ctx context.Context) error {
	if m.cacheDir != "" {
		if ok, changed := m.LoadCacheIncremental(ctx, m.cacheDir); ok {
			if err := m.applyIncremental(ctx, changed); err == nil {
				return nil
			}
			// Incremental merge failed — clear state and fall through.
			m.mu.Lock()
			m.ranked = nil
			m.mtimes = nil
			m.outputs.reset()
			m.mu.Unlock()
		}
	}

	files, err := ScanFiles(ctx, m.root)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return ErrNotCodeProject
	}

	parsed, mtimes, err := m.parseFiles(ctx, files)
	if err != nil {
		return err
	}

	ranked := RankFiles(parsed)

	m.mu.Lock()
	m.ranked = ranked
	m.builtAt = time.Now()
	m.mtimes = mtimes
	m.outputs.reset()
	m.mu.Unlock()

	if m.cacheDir != "" {
		_ = m.SaveCache(m.cacheDir) // best-effort
	}

	return nil
}

// applyIncremental re-parses only the changed paths, merges them into the
// already-hydrated m.ranked, re-detects implementations over the full merged
// set, re-ranks, and saves the cache. Returns an error if re-parsing fails
// entirely; caller must fall through to a full rebuild.
func (m *Map) applyIncremental(ctx context.Context, changedRel []string) error {
	if len(changedRel) == 0 {
		// Nothing to re-parse — cache is authoritative. Still refresh builtAt
		// and save so LastSHA advances if HEAD moved without touching tracked
		// files (rare but possible).
		m.mu.Lock()
		m.builtAt = time.Now()
		m.mu.Unlock()
		if m.cacheDir != "" {
			_ = m.SaveCache(m.cacheDir)
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
		if tooBig(abs) || isBuildArtifact(rel) || inSkipDir(rel) {
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
	if len(infos) > 0 {
		var err error
		parsed, newMtimes, err = m.parseFiles(ctx, infos)
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
	existing := make([]*FileSymbols, 0, len(m.ranked)+len(parsed))
	for _, rf := range m.ranked {
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
	// Refresh mtimes for newly parsed files.
	if m.mtimes == nil {
		m.mtimes = make(map[string]time.Time, len(existing))
	}
	for path, t := range newMtimes {
		m.mtimes[path] = t
	}
	m.mu.Unlock()

	// DetectImplementations must see the FULL merged set, not just parsed subset.
	DetectImplementations(existing)
	ranked := RankFiles(existing)

	m.mu.Lock()
	m.ranked = ranked
	m.builtAt = time.Now()
	m.outputs.reset()
	m.mu.Unlock()

	if m.cacheDir != "" {
		_ = m.SaveCache(m.cacheDir)
	}
	return nil
}

// String returns the current formatted map output.
// Returns empty string if Build has not been called or produced no symbols.
func (m *Map) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs.get(&m.outputs.compact, func() string {
		return FormatMap(m.ranked, m.config.MaxTokens, false, false)
	})
}

// StringVerbose returns the full verbose map output (all symbols, no summarization).
func (m *Map) StringVerbose() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs.get(&m.outputs.verbose, func() string {
		return FormatMap(m.ranked, 0, true, false)
	})
}

// StringDetail returns the full detailed map output with signatures and struct fields.
func (m *Map) StringDetail() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs.get(&m.outputs.detail, func() string {
		return FormatMap(m.ranked, 0, true, true)
	})
}

// StringLines returns the source-line format showing actual code definitions.
func (m *Map) StringLines() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs.get(&m.outputs.lines, func() string {
		return FormatLines(m.ranked, m.config.MaxTokensNoCtx, m.root)
	})
}

// StringXML returns the structured XML format.
func (m *Map) StringXML() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs.get(&m.outputs.xml, func() string {
		return FormatXML(m.ranked, m.config.MaxTokens)
	})
}

// Ranked returns the ranked file list built by Build.
// Returns nil if Build has not been called.
func (m *Map) Ranked() []RankedFile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ranked
}

// BuiltAt returns the time of the last successful build, or zero time if never built.
func (m *Map) BuiltAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.builtAt
}

// Stale reports whether any tracked file has been modified since the last build.
// Uses file mtimes for fast checking.
// Also stale if Build has never been called.
// Debounced: returns false if last build was <30s ago.
func (m *Map) Stale() bool {
	m.mu.RLock()
	builtAt := m.builtAt
	mtimes := make(map[string]time.Time, len(m.mtimes))
	maps.Copy(mtimes, m.mtimes)
	m.mu.RUnlock()

	if builtAt.IsZero() {
		return true
	}
	if time.Since(builtAt) < staleDebounce {
		return false
	}

	var stale atomic.Bool
	g := new(errgroup.Group)
	g.SetLimit(runtime.NumCPU())

	for path, recorded := range mtimes {
		g.Go(func() error {
			if stale.Load() {
				return nil
			}
			info, err := os.Stat(path)
			if err != nil || info.ModTime().After(recorded) {
				stale.Store(true)
			}
			return nil
		})
	}
	_ = g.Wait()

	return stale.Load()
}
