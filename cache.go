package repomap

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

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

const cacheVersion = 2

// SaveCache writes the current map state to disk.
func (m *Map) SaveCache(cacheDir string) error {
	m.mu.Lock()
	if m.builtAt.IsZero() || len(m.ranked) == 0 {
		m.mu.Unlock()
		return nil
	}
	// Compute lazy strings if not yet cached, so they are persisted.
	if m.output == nil {
		s := FormatMap(m.ranked, m.config.MaxTokens, false, false)
		m.output = &s
	}
	if m.outputLines == nil {
		s := FormatLines(m.ranked, m.config.MaxTokensNoCtx, m.root)
		m.outputLines = &s
	}
	entry := diskCache{
		Version:     cacheVersion,
		Root:        m.root,
		BuiltAt:     m.builtAt,
		Mtimes:      m.mtimes,
		Output:      *m.output,
		OutputLines: *m.outputLines,
		Ranked:      m.ranked,
	}
	m.mu.Unlock()

	data, err := json.Marshal(&entry)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	path := cachePath(cacheDir, m.root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("write cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename cache: %w", err)
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
	m.output = &entry.Output
	m.outputLines = &entry.OutputLines
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
