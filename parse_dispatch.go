package repomap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dotcommander/repomap/internal/goanalysis"
	"golang.org/x/sync/errgroup"
)

// sha256OfFile returns the hex-encoded SHA-256 digest of a file's contents.
// Returns ("", err) on any error (missing file, permission denied, I/O failure)
// so callers can log/observe failures rather than silently skipping.
func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// populateSymbolHashes sets each symbol's Hash to the sha256 hex of its raw
// source bytes over the Line..EndLine span (1-based, inclusive). Symbols whose
// span is unavailable (EndLine == 0, EndLine < Line, or Line == 0) are left
// with an empty Hash. Raw bytes — no normalization. Read failures leave all
// hashes empty (best-effort; consumers treat empty Hash as "unknown").
func populateSymbolHashes(fs *FileSymbols, abs string) {
	if fs == nil || len(fs.Symbols) == 0 {
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return
	}
	lines := splitLines(string(data))
	for i := range fs.Symbols {
		s := &fs.Symbols[i]
		if s.Line <= 0 || s.EndLine < s.Line {
			continue
		}
		// 1-based inclusive span → 0-based slice bounds.
		start := s.Line - 1
		end := s.EndLine
		if start >= len(lines) {
			continue
		}
		if end > len(lines) {
			end = len(lines)
		}
		span := joinLines(lines[start:end])
		sum := sha256.Sum256([]byte(span))
		s.Hash = hex.EncodeToString(sum[:])
	}
}

// joinLines rejoins lines with "\n" for hashing. Deterministic and independent
// of the file's original line-ending bytes (splitLines already stripped them),
// so the hash is stable across CRLF/LF differences.
func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

// absPath returns the absolute path for a file relative to the project root.
func (m *Map) absPath(rel string) string {
	return filepath.Join(m.root, rel)
}

// parseFiles parses all discovered files in parallel and returns the symbols,
// a path→mtime map, a path→sha256-hex map for stale checking, and parser
// coverage counters.
// Non-Go files use tree-sitter when available, then ctags, then regex.
func (m *Map) parseFiles(ctx context.Context, files []FileInfo) ([]*FileSymbols, map[string]time.Time, map[string]string, ParseCoverage, error) {
	mtimes := make(map[string]time.Time, len(files))
	hashes := make(map[string]string, len(files))
	for _, fi := range files {
		abs := m.absPath(fi.Path)
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		mtimes[abs] = info.ModTime()
		h, hErr := sha256OfFile(abs)
		if hErr != nil {
			slog.Default().Warn("sha256 hash failed; skipping content hash for file", "path", abs, "err", hErr)
			continue
		}
		hashes[abs] = h
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
		var analysis *goanalysis.Result
		var analysisErr error
		active := make(map[string]goanalysis.File)
		if m.config.GoAnalysis {
			analysis, analysisErr = goanalysis.Analyze(egCtx, goanalysis.Options{
				Root: m.root, IncludeCalls: m.config.GoAnalysisCalls,
				IncludeTests: m.config.GoAnalysisTests,
			})
		}
		if analysis != nil {
			active = analysis.Files
			callers := semanticCallsToSymbolCallers(analysis.Calls)
			diagnostics := make([]GoDiagnostic, 0, len(analysis.Diagnostics))
			for _, diagnostic := range analysis.Diagnostics {
				diagnostics = append(diagnostics, GoDiagnostic{
					PackagePath: diagnostic.PackagePath, Position: diagnostic.Position, Message: diagnostic.Message,
				})
			}
			m.mu.Lock()
			m.semanticCallers = callers
			m.goDiagnostics = diagnostics
			m.mu.Unlock()
		} else if analysisErr != nil {
			m.mu.Lock()
			m.semanticCallers = nil
			m.goDiagnostics = []GoDiagnostic{{Message: analysisErr.Error()}}
			m.mu.Unlock()
		}
		goParsed = parallelParse(goFiles, func(fi FileInfo) *FileSymbols {
			sym, err := ParseGoFile(m.absPath(fi.Path), m.root)
			if err != nil {
				return nil
			}
			if file, ok := active[filepath.ToSlash(fi.Path)]; ok {
				sym.Package = file.PackageName
				sym.ImportPath = file.PackagePath
				sym.BuildActive = true
				sym.AnalysisMode = "semantic"
			} else if analysisErr != nil {
				sym.AnalysisMode = "analysis_failed"
			}
			return sym
		})
		if analysis != nil {
			applySemanticImplementations(goParsed, analysis.Implementations)
		}
		return nil
	})
	eg.Go(func() error {
		nonGoParsed = m.parseNonGoFiles(egCtx, files)
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, nil, nil, ParseCoverage{}, err
	}

	parsed := make([]*FileSymbols, 0, len(goParsed)+len(nonGoParsed))
	parsed = append(parsed, goParsed...)
	parsed = append(parsed, nonGoParsed...)

	// Apply blocklist filter once for all parse methods (go_ast/tree_sitter/ctags/regex).
	for _, fs := range parsed {
		m.blocklist.filterSymbols(fs)
	}

	// Populate per-symbol content hashes once, centrally, after all parse methods
	// complete — keyed off Line/EndLine against the file's source bytes. Single
	// source of truth: avoids editing every parser inline (which would drift).
	for _, fs := range parsed {
		if fs == nil {
			continue
		}
		populateSymbolHashes(fs, m.absPath(fs.Path))
	}

	// Go implementation relationships come from go/types. The legacy syntax
	// matcher remains available to direct ParseGoSource consumers only.

	return parsed, mtimes, hashes, buildParseCoverage(files, parsed, m.tsAvailable, m.ctagsAvailable), nil
}

func buildParseCoverage(files []FileInfo, parsed []*FileSymbols, tsEnabled, ctagsEnabled bool) ParseCoverage {
	coverage := ParseCoverage{
		FilesScanned:      len(files),
		FilesParsed:       len(parsed),
		ByLanguage:        make(map[string]int),
		ByParseMethod:     make(map[string]int),
		FailuresByLang:    make(map[string]int),
		TreeSitterEnabled: tsEnabled,
		CtagsEnabled:      ctagsEnabled,
	}
	for _, fi := range files {
		coverage.ByLanguage[fi.Language]++
	}
	parsedByLang := make(map[string]int)
	for _, fs := range parsed {
		if fs == nil {
			continue
		}
		parsedByLang[fs.Language]++
		if fs.ParseMethod != "" {
			coverage.ByParseMethod[fs.ParseMethod]++
		}
		if fs.Language == "go" {
			switch fs.AnalysisMode {
			case "semantic":
				coverage.GoSemanticActive++
			case "syntax_only":
				coverage.GoSyntaxInactive++
			case "analysis_failed":
				coverage.GoAnalysisFailed++
			}
		}
	}
	if goScanned := coverage.ByLanguage["go"]; goScanned > coverage.GoSemanticActive+coverage.GoSyntaxInactive+coverage.GoAnalysisFailed {
		coverage.GoAnalysisFailed = goScanned - coverage.GoSemanticActive - coverage.GoSyntaxInactive
	}
	for lang, scanned := range coverage.ByLanguage {
		if failed := scanned - parsedByLang[lang]; failed > 0 {
			coverage.FailuresByLang[lang] = failed
			coverage.ParseFailures += failed
		}
	}
	if len(coverage.ByLanguage) == 0 {
		coverage.ByLanguage = nil
	}
	if len(coverage.ByParseMethod) == 0 {
		coverage.ByParseMethod = nil
	}
	if len(coverage.FailuresByLang) == 0 {
		coverage.FailuresByLang = nil
	}
	return coverage
}

func semanticCallsToSymbolCallers(edges []goanalysis.CallEdge) SymbolCallers {
	out := make(SymbolCallers)
	for _, edge := range edges {
		if edge.CalleeFile == "" || edge.CalleeSymbol == "" || edge.CallerFile == "" {
			continue
		}
		key := semanticCallsKey(edge.CalleeFile, edge.CalleeReceiver, edge.CalleeSymbol)
		out[key] = append(out[key], Location{File: edge.CallerFile, Line: edge.CallerLine})
	}
	for key := range out {
		slices.SortFunc(out[key], func(a, b Location) int {
			if a.File != b.File {
				return strings.Compare(a.File, b.File)
			}
			return a.Line - b.Line
		})
		out[key] = slices.Compact(out[key])
	}
	return out
}

func applySemanticImplementations(files []*FileSymbols, implementations []goanalysis.Implementation) {
	byType := make(map[string][]string)
	for _, implementation := range implementations {
		key := filepath.ToSlash(implementation.TypeFile) + "\x00" + implementation.TypeName
		byType[key] = append(byType[key], implementation.InterfaceName)
	}
	for _, file := range files {
		if file == nil || file.AnalysisMode != "semantic" {
			continue
		}
		for i := range file.Symbols {
			symbol := &file.Symbols[i]
			if symbol.Kind != "struct" {
				continue
			}
			values := byType[filepath.ToSlash(file.Path)+"\x00"+symbol.Name]
			slices.Sort(values)
			symbol.Implements = slices.Compact(values)
		}
	}
}

func parseCoverageFromRanked(ranked []RankedFile, tsEnabled, ctagsEnabled bool) ParseCoverage {
	files := make([]FileInfo, 0, len(ranked))
	parsed := make([]*FileSymbols, 0, len(ranked))
	for _, rf := range ranked {
		if rf.FileSymbols == nil {
			continue
		}
		files = append(files, FileInfo{Path: rf.Path, Language: rf.Language})
		parsed = append(parsed, rf.FileSymbols)
	}
	return buildParseCoverage(files, parsed, tsEnabled, ctagsEnabled)
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
