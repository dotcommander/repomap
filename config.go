package repomap

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// configFileName is the on-disk config file name loaded from the project root.
const configFileName = ".repomap.yaml"

// BlocklistConfig holds loaded-from-disk repomap settings that filter parsed
// symbols. Start minimal — more fields land in follow-up versions.
// Safe for concurrent reads after Load returns.
type BlocklistConfig struct {
	// MethodBlocklist lists symbol-name patterns to drop at parse time.
	// Each entry is either:
	//   - a regex wrapped in forward slashes, e.g. "/^pb_/"
	//   - a glob matched with path.Match, e.g. "Test*" or "*Mock"
	MethodBlocklist []string `yaml:"method_blocklist"`

	// compiled patterns. Populated by compile(); zero value = match nothing.
	compiled []blocklistMatcher
}

// blocklistMatcher is the compiled form of a single pattern entry.
type blocklistMatcher struct {
	raw   string
	glob  string         // path.Match pattern; empty if regex
	regex *regexp.Regexp // compiled regex; nil if glob
}

// LoadBlocklistConfig reads <root>/.repomap.yaml. Returns zero-value config
// when the file is absent. Returns a wrapped error only when the file exists
// but is malformed or has invalid patterns.
func LoadBlocklistConfig(root string) (*BlocklistConfig, error) {
	p := filepath.Join(root, configFileName)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &BlocklistConfig{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", configFileName, err)
	}
	var c BlocklistConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configFileName, err)
	}
	if err := c.compile(); err != nil {
		return nil, fmt.Errorf("%s: %w", configFileName, err)
	}
	return &c, nil
}

// compile pre-compiles all patterns in MethodBlocklist.
func (c *BlocklistConfig) compile() error {
	c.compiled = make([]blocklistMatcher, 0, len(c.MethodBlocklist))
	for _, raw := range c.MethodBlocklist {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		m := blocklistMatcher{raw: raw}
		if strings.HasPrefix(raw, "/") && strings.HasSuffix(raw, "/") && len(raw) >= 2 {
			re, err := regexp.Compile(raw[1 : len(raw)-1])
			if err != nil {
				return fmt.Errorf("invalid regex %q: %w", raw, err)
			}
			m.regex = re
		} else {
			if _, err := path.Match(raw, "probe"); err != nil {
				return fmt.Errorf("invalid glob %q: %w", raw, err)
			}
			m.glob = raw
		}
		c.compiled = append(c.compiled, m)
	}
	return nil
}

// ShouldSkipSymbol reports whether a symbol name matches any blocklist pattern.
// A nil receiver or empty blocklist returns false.
func (c *BlocklistConfig) ShouldSkipSymbol(name string) bool {
	if c == nil || len(c.compiled) == 0 {
		return false
	}
	for _, m := range c.compiled {
		if m.regex != nil {
			if m.regex.MatchString(name) {
				return true
			}
			continue
		}
		if ok, _ := path.Match(m.glob, name); ok {
			return true
		}
	}
	return false
}

// filterSymbols removes blocklisted symbols from fs in place.
// No-op when fs is nil, the config is nil, or the blocklist is empty.
func (c *BlocklistConfig) filterSymbols(fs *FileSymbols) {
	if fs == nil || c == nil || len(c.compiled) == 0 || len(fs.Symbols) == 0 {
		return
	}
	kept := fs.Symbols[:0]
	for _, s := range fs.Symbols {
		if c.ShouldSkipSymbol(s.Name) {
			continue
		}
		kept = append(kept, s)
	}
	fs.Symbols = kept
}
