package repomap

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"
)

// absPath returns the absolute path for a file relative to the project root.
func (m *Map) absPath(rel string) string {
	return filepath.Join(m.root, rel)
}

// parseFiles parses all discovered files in parallel and returns the symbols
// and a path→mtime map for stale checking.
// Non-Go files use tree-sitter when available, then ctags, then regex.
func (m *Map) parseFiles(ctx context.Context, files []FileInfo) ([]*FileSymbols, map[string]time.Time, error) {
	mtimes := make(map[string]time.Time, len(files))
	for _, fi := range files {
		absPath := m.absPath(fi.Path)
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		mtimes[absPath] = info.ModTime()
	}

	var (
		goParsed    []*FileSymbols
		nonGoParsed []*FileSymbols
	)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		goFiles := make([]FileInfo, 0, len(files))
		for _, fi := range files {
			if fi.Language == "go" {
				goFiles = append(goFiles, fi)
			}
		}
		goParsed = parallelParse(goFiles, func(fi FileInfo) *FileSymbols {
			sym, err := ParseGoFile(m.absPath(fi.Path), m.root)
			if err != nil {
				return nil
			}
			return sym
		})
		return nil
	})
	eg.Go(func() error {
		nonGoParsed = m.parseNonGoFiles(egCtx, files)
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, nil, err
	}

	parsed := make([]*FileSymbols, 0, len(goParsed)+len(nonGoParsed))
	parsed = append(parsed, goParsed...)
	parsed = append(parsed, nonGoParsed...)

	// Apply blocklist filter once for all parse methods (ast/treesitter/ctags/regex).
	for _, fs := range parsed {
		m.blocklist.filterSymbols(fs)
	}

	DetectImplementations(parsed)

	return parsed, mtimes, nil
}

// parseNonGoFiles parses non-Go files using the tiered fallback:
// tree-sitter → ctags → regex. Filters out Go files once at the entry so
// downstream stages can assume a non-Go file slice.
func (m *Map) parseNonGoFiles(ctx context.Context, files []FileInfo) []*FileSymbols {
	nonGo := make([]FileInfo, 0, len(files))
	for _, fi := range files {
		if fi.Language != "go" {
			nonGo = append(nonGo, fi)
		}
	}
	if len(nonGo) == 0 {
		return nil
	}
	if m.tsAvailable {
		tsParsed, fallbackFiles := m.parseTreeSitterFiles(ctx, nonGo)
		if len(fallbackFiles) > 0 {
			fallbackParsed := m.parseWithCtagsOrRegex(ctx, fallbackFiles)
			tsParsed = append(tsParsed, fallbackParsed...)
		}
		return tsParsed
	}
	return m.parseWithCtagsOrRegex(ctx, nonGo)
}

// parseWithCtagsOrRegex tries ctags, then falls back to regex parsing.
func (m *Map) parseWithCtagsOrRegex(ctx context.Context, files []FileInfo) []*FileSymbols {
	if m.ctagsAvailable {
		ctagsParsed, err := ParseWithCtags(ctx, m.root, files)
		if err == nil {
			return ctagsParsed
		}
	}
	return m.parseGenericFiles(files)
}

// parseGenericFiles parses non-Go files using regex patterns in parallel.
// Caller must pass only non-Go files (enforced by parseNonGoFiles).
func (m *Map) parseGenericFiles(files []FileInfo) []*FileSymbols {
	return parallelParse(files, func(fi FileInfo) *FileSymbols {
		sym, err := ParseGenericFile(m.absPath(fi.Path), m.root, fi.Language)
		if err != nil {
			return nil
		}
		return sym
	})
}

// parseNonGoFile parses a single non-Go file using the standalone ladder:
// tree-sitter (when available) → regex. No ctags step — ctags only pays off
// as a batch operation, so serial callers (commit analyze) skip it. Used
// where no Map instance exists. Returns nil on total miss.
func parseNonGoFile(abs, root, lang string) *FileSymbols {
	if TreeSitterAvailable() {
		if data, err := os.ReadFile(abs); err == nil {
			if sym := parseWithTreeSitter(data, lang, relPath(root, abs)); sym != nil {
				if sym.ImportPath == "" {
					sym.ImportPath = deriveImportPath(abs, root, lang, splitLines(string(data)))
				}
				return sym
			}
		}
	}
	sym, err := ParseGenericFile(abs, root, lang)
	if err != nil {
		return nil
	}
	return sym
}
