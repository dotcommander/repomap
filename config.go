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

// fileDetailOverride is the compiled form of one file_overrides entry.
type fileDetailOverride struct {
	// glob is the path.Match pattern; used when prefix == "".
	glob string
	// prefix is set instead of glob when the original pattern contained "**".
	// Match succeeds when the relative path starts with this prefix.
	prefix string
	// level is the forced DetailLevel: -1 (omit) or 2 (full).
	level int
}

// BlocklistConfig holds loaded-from-disk repomap settings that filter parsed
// symbols and file paths. Safe for concurrent reads after Load returns.
type BlocklistConfig struct {
	// MethodBlocklist lists symbol-name patterns to drop at parse time.
	// Each entry is either:
	//   - a regex wrapped in forward slashes, e.g. "/^pb_/"
	//   - a glob matched with path.Match, e.g. "Test*" or "*Mock"
	MethodBlocklist []string `yaml:"method_blocklist"`

	// ExcludePaths lists path glob patterns (relative to project root) to drop
	// at scan time. Any file whose relative path matches is excluded.
	// Example: ["internal/gen/*", "vendor/*"]
	ExcludePaths []string `yaml:"exclude_paths"`

	// IncludePaths lists path glob patterns (relative to project root) to keep
	// at scan time. When non-empty, only matching files are included.
	// Example: ["cmd/*", "internal/cli/*"]
	IncludePaths []string `yaml:"include_paths"`

	// FileOverrides maps relative-path globs to forced detail levels.
	// Accepted values: "full" (DetailLevel 2) and "omit" (DetailLevel -1).
	// Example:
	//   file_overrides:
	//     "cmd/main.go": full
	//     "internal/gen/**": omit
	FileOverrides map[string]string `yaml:"file_overrides"`

	// compiled symbol patterns. Populated by compile(); zero value = match nothing.
	compiled []blocklistMatcher

	// compiledExclude and compiledInclude are compiled path patterns.
	compiledExclude []string
	compiledInclude []string

	// compiledOverrides holds the sorted/compiled file override rules.
	compiledOverrides []fileDetailOverride
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

// compile pre-compiles all patterns in MethodBlocklist, ExcludePaths,
// IncludePaths, and FileOverrides. Invalid patterns return an error; all-or-nothing semantics.
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

	if err := validateGlobs("exclude_paths", c.ExcludePaths); err != nil {
		return err
	}
	c.compiledExclude = compactPatterns(c.ExcludePaths)

	if err := validateGlobs("include_paths", c.IncludePaths); err != nil {
		return err
	}
	c.compiledInclude = compactPatterns(c.IncludePaths)

	c.compiledOverrides = make([]fileDetailOverride, 0, len(c.FileOverrides))
	for glob, val := range c.FileOverrides {
		glob = strings.TrimSpace(glob)
		if glob == "" {
			continue
		}
		var level int
		switch strings.TrimSpace(val) {
		case "full":
			level = 2
		case "omit":
			level = -1
		default:
			return fmt.Errorf("file_overrides: %q has unknown value %q (want \"full\" or \"omit\")", glob, val)
		}
		ov := fileDetailOverride{level: level}
		if idx := strings.Index(glob, "**"); idx >= 0 {
			// path.Match does not support **: treat everything before ** as a prefix.
			ov.prefix = glob[:idx]
		} else {
			if _, err := path.Match(glob, "probe"); err != nil {
				return fmt.Errorf("file_overrides: invalid glob %q: %w", glob, err)
			}
			ov.glob = glob
		}
		c.compiledOverrides = append(c.compiledOverrides, ov)
	}

	return nil
}

// validateGlobs returns an error if any pattern in ps is not a valid path.Match glob.
func validateGlobs(field string, ps []string) error {
	for _, p := range ps {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := path.Match(p, "probe"); err != nil {
			return fmt.Errorf("%s: invalid glob %q: %w", field, p, err)
		}
	}
	return nil
}

// compactPatterns trims whitespace and drops empty entries.
func compactPatterns(ps []string) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MatchFileOverride reports whether a relative file path matches any file_overrides
// rule. Returns the forced DetailLevel (2 or -1) and true on match; 0 and false otherwise.
// A nil receiver returns (0, false) — no overrides.
// Globs use path.Match semantics; patterns containing "**" match any path with the
// corresponding prefix (everything before the first "**").
func (c *BlocklistConfig) MatchFileOverride(rel string) (level int, ok bool) {
	if c == nil || len(c.compiledOverrides) == 0 {
		return 0, false
	}
	for _, ov := range c.compiledOverrides {
		if ov.prefix != "" {
			if strings.HasPrefix(rel, ov.prefix) {
				return ov.level, true
			}
			continue
		}
		if matched, _ := path.Match(ov.glob, rel); matched {
			return ov.level, true
		}
	}
	return 0, false
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

// ShouldExcludePath reports whether rel matches any ExcludePaths pattern.
// rel must be a slash-separated path relative to the project root.
// A nil receiver returns false (nothing excluded).
func (c *BlocklistConfig) ShouldExcludePath(rel string) bool {
	if c == nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, pat := range c.compiledExclude {
		if ok, _ := path.Match(pat, rel); ok {
			return true
		}
	}
	return false
}

// ShouldIncludePath reports whether rel passes the IncludePaths filter.
// When IncludePaths is empty, all paths are included (returns true).
// When non-empty, returns true only if rel matches at least one pattern.
// A nil receiver returns true (nothing excluded).
func (c *BlocklistConfig) ShouldIncludePath(rel string) bool {
	if c == nil || len(c.compiledInclude) == 0 {
		return true
	}
	rel = filepath.ToSlash(rel)
	for _, pat := range c.compiledInclude {
		if ok, _ := path.Match(pat, rel); ok {
			return true
		}
	}
	return false
}
