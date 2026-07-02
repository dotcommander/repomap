package repomap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"os"
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
	MaxTokens      int      // token budget for output (default: 1024)
	MaxTokensNoCtx int      // budget when no files in conversation (default: 2048)
	Intent         string   // optional BM25 query for task-aware ranking
	ConsumedPaths  []string // optional: paths the caller has already read — these are downranked
	SymbolRefs     bool     // optional approximate cross-language symbol reference scoring
	Explain        bool     // append per-file confidence-tier score breakdown to text output
	IncludeTests   bool     // rank test files at full weight (default: demoted)
	MaxFileSize    int      // max file size in bytes to scan (default: 50_000; negative disables the cap)
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
	root          string
	config        Config
	blocklist     *BlocklistConfig
	cacheDir      string // if set, Build saves cache here
	mu            sync.RWMutex
	ranked        []RankedFile
	builtAt       time.Time
	mtimes        map[string]time.Time // path → mtime at last build
	contentHashes map[string]string    // path → sha256 hex at last build; nil on old cache loads
	scanFP        string             // fingerprint of the scanned file set at last build; "" = mtime-only (legacy/tests)
	coverage      ParseCoverage
	outputs       outputCache

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
	if cfg.MaxFileSize == 0 {
		cfg.MaxFileSize = defaultMaxFileSize
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

	files, err := scanFilesLimited(ctx, m.root, m.blocklist, m.config.MaxFileSize)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return ErrNotCodeProject
	}

	parsed, mtimes, hashes, coverage, err := m.parseFiles(ctx, files)
	if err != nil {
		return err
	}

	ranked := RankFiles(parsed)

	ranked = m.applyRankPasses(ranked)

	m.mu.Lock()
	m.ranked = ranked
	m.builtAt = time.Now()
	m.mtimes = mtimes
	m.contentHashes = hashes
	m.coverage = coverage
	m.scanFP = scanFingerprint(files)
	m.outputs.reset()
	m.mu.Unlock()

	if m.cacheDir != "" {
		_ = m.SaveCacheContext(ctx, m.cacheDir) // best-effort
	}

	return nil
}

// applyRankPasses runs every post-rank scoring pass. Build and the
// incremental rebuild MUST both route through this — a pass added to only
// one path silently diverges cache-hit output from cold-build output.
func (m *Map) applyRankPasses(ranked []RankedFile) []RankedFile {
	ApplyIntraPackageRefs(m.root, ranked)
	ApplyCallSiteReferenceBonus(ranked)

	applyTestDemotion(ranked, m.config.IncludeTests)

	if m.config.Intent != "" {
		scorer := NewIntentScorer(ranked)
		ranked = scorer.Score(ranked, m.config.Intent)
	}

	if m.config.SymbolRefs {
		ApplySymbolReferenceBonus(m.root, ranked)
	}

	if len(m.config.ConsumedPaths) > 0 {
		consumed := make(map[string]bool, len(m.config.ConsumedPaths))
		for _, p := range m.config.ConsumedPaths {
			consumed[p] = true
		}
		ApplyConsumedBonus(ranked, consumed)
	}
	return ranked
}

// String returns the current formatted map output.
// Returns empty string if Build has not been called or produced no symbols.
func (m *Map) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs.get(&m.outputs.compact, func() string {
		return FormatMap(m.ranked, m.config.MaxTokens, false, false, m.blocklist, m.config.Explain)
	})
}

// StringBriefMap returns the enriched map for the top maxFiles ranked files
// (the highest-scoring, since m.ranked is sorted descending) and the total
// ranked-file count so the caller can report how many were dropped. Single-
// package repos report a uniform ImportedBy — every file "imported by N" where
// N is the package's importer count, a constant with no per-file signal — which
// is zeroed out (when uniform across the shown files) so the digest map drops
// that noise. maxFiles <= 0 means no cap.
func (m *Map) StringBriefMap(maxFiles int) (body string, total int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ranked := cloneRanked(m.ranked)
	total = len(ranked)
	if maxFiles > 0 && len(ranked) > maxFiles {
		ranked = ranked[:maxFiles]
	}
	// Judge uniformity over what is actually shown: when every displayed file
	// shares one ImportedBy (e.g. a digest of same-package files), the column is
	// a constant carrying no per-file signal, so zero it to drop the noise.
	if uniformImportedBy(ranked) {
		for i := range ranked {
			ranked[i].ImportedBy = 0
		}
	}
	return FormatMap(ranked, m.config.MaxTokens, false, false, m.blocklist, m.config.Explain), total
}

// uniformImportedBy reports whether every file carrying a positive ImportedBy
// shares the same count — the single-package degenerate case where the metric
// is a package-wide constant rather than a per-file signal. Returns false when
// no file has a positive count.
func uniformImportedBy(ranked []RankedFile) bool {
	first, seen := 0, false
	for _, f := range ranked {
		if f.ImportedBy <= 0 {
			continue
		}
		if !seen {
			first, seen = f.ImportedBy, true
			continue
		}
		if f.ImportedBy != first {
			return false
		}
	}
	return seen
}

// StringCompact returns the lean orientation output: path + exported symbol names only.
// No signatures, no godoc, no struct fields. Budget is applied using compactCost so
// more files fit vs. the enriched default (m.String()).
// Returns empty string if Build has not been called or produced no symbols.
func (m *Map) StringCompact() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs.get(&m.outputs.orientation, func() string {
		return FormatMapCompact(m.ranked, m.config.MaxTokens, m.blocklist, m.config.Explain)
	})
}

// StringVerbose returns the full verbose map output (all symbols, no summarization).
func (m *Map) StringVerbose() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs.get(&m.outputs.verbose, func() string {
		return FormatMap(m.ranked, 0, true, false, m.blocklist, m.config.Explain)
	})
}

// StringDetail returns the full detailed map output with signatures and struct fields.
func (m *Map) StringDetail() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outputs.get(&m.outputs.detail, func() string {
		return FormatMap(m.ranked, 0, true, true, m.blocklist, m.config.Explain)
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
		return FormatXML(m.ranked, m.config.MaxTokens, m.blocklist)
	})
}

// Ranked returns the ranked file list built by Build.
// Returns nil if Build has not been called.
func (m *Map) Ranked() []RankedFile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneRanked(m.ranked)
}

// Config returns the configuration this Map was created with.
func (m *Map) Config() Config {
	return m.config
}

// BuiltAt returns the time of the last successful build, or zero time if never built.
func (m *Map) BuiltAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.builtAt
}

// scanFingerprint identifies the discovered file set (relative paths, already
// sorted by the scanner). Stale compares it against a fresh scan so newly
// created files mark the map stale — mtime polling alone can never see them.
func scanFingerprint(files []FileInfo) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Path))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Stale reports whether the tracked tree has changed since the last build.
// See StaleContext.
func (m *Map) Stale() bool {
	return m.StaleContext(context.Background())
}

// StaleContext reports whether any tracked file has been modified since the
// last build (mtime polling), or whether the discovered file set itself has
// changed (fresh scan vs stored fingerprint — catches newly created files,
// which mtime polling over recorded paths can never see).
// Also stale if Build has never been called.
// Debounced: returns false if last build was <30s ago.
func (m *Map) StaleContext(ctx context.Context) bool {
	m.mu.RLock()
	builtAt := m.builtAt
	fp := m.scanFP
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
	if stale.Load() {
		return true
	}

	// "" = populated without a fingerprint (direct state injection in tests,
	// pre-fingerprint caches) — mtime-only behavior for those.
	if fp == "" {
		return false
	}
	files, err := scanFilesLimited(ctx, m.root, m.blocklist, m.config.MaxFileSize)
	if err != nil {
		return true // fail-stale: cannot prove the set unchanged
	}
	return scanFingerprint(files) != fp
}
