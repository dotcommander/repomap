package repomap

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// outputCache lazily computes and caches formatted output strings.
type outputCache struct {
	compact *string
	verbose *string
	detail  *string
	lines   *string
	xml     *string
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
}

// diskCache is the on-disk format for a cached repomap build.
type diskCache struct {
	Version     int                  `json:"version"`
	Root        string               `json:"root"`
	BuiltAt     time.Time            `json:"built_at"`
	Mtimes      map[string]time.Time `json:"mtimes"`
	Output      string               `json:"output"`
	OutputLines string               `json:"output_lines"`
	Ranked      []RankedFile         `json:"ranked"`
}

const cacheVersion = 5

// SaveCache writes the current map state to disk.
func (m *Map) SaveCache(cacheDir string) error {
	m.mu.Lock()
	if m.builtAt.IsZero() || len(m.ranked) == 0 {
		m.mu.Unlock()
		return nil
	}
	// Compute lazy strings if not yet cached, so they are persisted.
	compact := m.outputs.get(&m.outputs.compact, func() string {
		return FormatMap(m.ranked, m.config.MaxTokens, false, false)
	})
	lines := m.outputs.get(&m.outputs.lines, func() string {
		return FormatLines(m.ranked, m.config.MaxTokensNoCtx, m.root)
	})
	entry := diskCache{
		Version:     cacheVersion,
		Root:        m.root,
		BuiltAt:     m.builtAt,
		Mtimes:      m.mtimes,
		Output:      compact,
		OutputLines: lines,
		Ranked:      m.ranked,
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
	if entry.Version != cacheVersion || entry.Root != m.root {
		return false
	}

	m.mu.Lock()
	m.ranked = entry.Ranked
	m.outputs.compact = &entry.Output
	m.outputs.lines = &entry.OutputLines
	m.builtAt = entry.BuiltAt
	m.mtimes = entry.Mtimes
	m.mu.Unlock()

	return true
}

// Dirty marks the map as needing a rebuild by zeroing builtAt,
// bypassing the stale debounce. Use after code-changing tool calls.
func (m *Map) Dirty() {
	m.mu.Lock()
	m.builtAt = time.Time{}
	m.mu.Unlock()
}

func cachePath(cacheDir, root string) string {
	h := sha256.Sum256([]byte(root))
	name := fmt.Sprintf("repomap-%x.json", h[:8])
	return filepath.Join(cacheDir, name)
}
