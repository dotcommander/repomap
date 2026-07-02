package repomap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// outputCache lazily computes and caches formatted output strings.
type outputCache struct {
	compact     *string // enriched default (m.String())
	verbose     *string
	detail      *string
	lines       *string
	xml         *string
	orientation *string // lean compact / orientation mode (m.StringCompact())
}

func (c *outputCache) get(ptr **string, fn func() string) string {
	if *ptr == nil {
		s := fn()
		*ptr = &s
	}
	return **ptr
}

func (c *outputCache) reset() {
	c.compact = nil
	c.verbose = nil
	c.detail = nil
	c.lines = nil
	c.xml = nil
	c.orientation = nil
}

// diskCache is the on-disk format for a cached repomap build.
type diskCache struct {
	Version    int    `json:"version"`
	Root       string `json:"root"`
	ConfigHash string `json:"config_hash"`
	BuiltAt       time.Time            `json:"built_at"`
	Mtimes        map[string]time.Time `json:"mtimes"`
	ContentHashes map[string]string    `json:"content_hashes,omitempty"` // path → sha256 hex; absent in old caches (mtime fallback)
	ScanFP        string               `json:"scan_fp,omitempty"`
	Coverage      ParseCoverage        `json:"coverage,omitempty"`
	Output        string               `json:"output"`
	OutputLines   string               `json:"output_lines"`
	Ranked        []RankedFile         `json:"ranked"`
	LastSHA       string               `json:"last_sha,omitempty"` // HEAD sha at write time; "" when not a git repo
	GitRoot       bool                 `json:"git_root,omitempty"` // true if root was inside a git repo at write time
}

const cacheVersion = 12

// SaveCache writes the current map state to disk.
func (m *Map) SaveCache(cacheDir string) error {
	return m.SaveCacheContext(context.Background(), cacheDir)
}

// SaveCacheContext writes the current map state to disk with caller cancellation.
func (m *Map) SaveCacheContext(ctx context.Context, cacheDir string) error {
	m.mu.Lock()
	if m.builtAt.IsZero() || len(m.ranked) == 0 {
		m.mu.Unlock()
		return nil
	}
	// Compute lazy strings if not yet cached, so they are persisted.
	// Cache always stores the non-explain output; explain is a rendering flag, not a build artifact.
	compact := m.outputs.get(&m.outputs.compact, func() string {
		return FormatMap(m.ranked, m.config.MaxTokens, false, false, m.blocklist, false)
	})
	lines := m.outputs.get(&m.outputs.lines, func() string {
		return FormatLines(m.ranked, m.config.MaxTokensNoCtx, m.root)
	})
	entry := diskCache{
		Version:       cacheVersion,
		Root:          m.root,
		ConfigHash:    m.configHash(),
		BuiltAt:       m.builtAt,
		Mtimes:        m.mtimes,
		ContentHashes: m.contentHashes,
		ScanFP:        m.scanFP,
		Coverage:      m.coverage,
		Output:        compact,
		OutputLines:   lines,
		Ranked:        m.ranked,
	}
	if isInsideGitRepo(m.root) {
		entry.GitRoot = true
		if sha, err := gitHeadSHA(ctx, m.root); err == nil {
			entry.LastSHA = sha
		}
	}
	m.mu.Unlock()

	data, err := json.Marshal(&entry)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	path := cachePath(cacheDir, m.root)
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}

// LoadCache loads a previously saved map from disk. Returns false if
// the cache is missing, corrupt, or for a different version.
func (m *Map) LoadCache(cacheDir string) bool {
	path := cachePath(cacheDir, m.root)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var entry diskCache
	if err := json.Unmarshal(data, &entry); err != nil {
		return false
	}
	if !m.cacheEntryValid(&entry) {
		return false
	}

	m.mu.Lock()
	m.ranked = entry.Ranked
	m.outputs.compact = &entry.Output
	m.outputs.lines = &entry.OutputLines
	m.builtAt = entry.BuiltAt
	m.mtimes = entry.Mtimes
	m.contentHashes = entry.ContentHashes // nil for old caches → mtime-only fallback
	m.scanFP = entry.ScanFP
	m.coverage = entry.Coverage
	m.mu.Unlock()

	return true
}

func cachePath(cacheDir, root string) string {
	h := sha256.Sum256([]byte(root))
	name := fmt.Sprintf("repomap-%x.json", h[:8])
	return filepath.Join(cacheDir, name)
}

// configHash fingerprints every config input that shapes ranked output or the
// persisted output strings, plus the blocklist (captures .repomap.yaml edits).
// A cache entry is valid only for the exact config that produced it.
func (m *Map) configHash() string {
	bl, _ := json.Marshal(m.blocklist)
	c := m.config
	s := fmt.Sprintf("%d|%d|%s|%s|%t|%t|%t|%d|%s",
		c.MaxTokens, c.MaxTokensNoCtx, c.Intent, strings.Join(c.ConsumedPaths, ","),
		c.SymbolRefs, c.Explain, c.IncludeTests, c.MaxFileSize, bl)
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// cacheEntryValid is the single validity check shared by LoadCache and
// LoadCacheIncremental — the two paths must never diverge on what "valid" means.
func (m *Map) cacheEntryValid(entry *diskCache) bool {
	return entry.Version == cacheVersion && entry.Root == m.root && entry.ConfigHash == m.configHash()
}
