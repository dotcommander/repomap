package repomap

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultContextSourceLines = 200
	maxContextLineBytes       = 256 * 1024
)

// ContextOptions controls symbol context extraction.
type ContextOptions struct {
	Kind           string
	File           string
	MaxSourceLines int
}

// SourceLine is one line of source extracted for a symbol.
type SourceLine struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

// SymbolContext is a bounded, symbol-centered context bundle.
type SymbolContext struct {
	Query      string         `json:"query"`
	Match      SymbolMatch    `json:"match"`
	Ambiguous  []SymbolMatch  `json:"ambiguous,omitempty"`
	Callers    []Location     `json:"callers,omitempty"`
	Source     []SourceLine   `json:"source,omitempty"`
	ReadNext   []ReadNextItem `json:"read_next,omitempty"`
	Truncated  bool           `json:"truncated,omitempty"`
	Impact     ImpactResult   `json:"impact"`
	SourceNote string         `json:"source_note,omitempty"`
}

// Context returns a bounded context bundle for the best matching symbol.
func (m *Map) Context(query string, opts ContextOptions) (SymbolContext, error) {
	name, qKind, qFile := ParseFindQuery(query)
	if opts.Kind == "" {
		opts.Kind = qKind
	}
	if opts.File == "" {
		opts.File = qFile
	}
	if opts.MaxSourceLines <= 0 {
		opts.MaxSourceLines = defaultContextSourceLines
	}

	matches := m.FindSymbol(name, opts.Kind, opts.File)
	if len(matches) == 0 {
		return SymbolContext{}, fmt.Errorf("symbol %q not found", query)
	}

	match := matches[0]
	impact, err := m.Impact(match.File)
	if err != nil {
		return SymbolContext{}, err
	}

	m.mu.RLock()
	root := m.root
	m.mu.RUnlock()

	source, truncated, note := readSymbolSource(filepath.Join(root, match.File), match.Symbol, opts.MaxSourceLines)
	out := SymbolContext{
		Query:      query,
		Match:      match,
		Impact:     impact,
		Source:     source,
		ReadNext:   contextReadNext(match),
		Truncated:  truncated,
		SourceNote: note,
	}
	if len(matches) > 1 {
		limit := len(matches)
		if limit > 5 {
			limit = 5
		}
		out.Ambiguous = append([]SymbolMatch(nil), matches[1:limit]...)
	}
	return out, nil
}

func contextReadNext(match SymbolMatch) []ReadNextItem {
	sym := match.Symbol
	if sym.Line <= 0 {
		return nil
	}
	end := sym.EndLine
	if end < sym.Line {
		end = sym.Line
	}
	return []ReadNextItem{
		readNextRange(match.File, sym.Line, end, "inspect the matched symbol before editing"),
	}
}

func readSymbolSource(path string, sym Symbol, maxLines int) ([]SourceLine, bool, string) {
	if sym.Line <= 0 {
		return nil, false, "source span unavailable"
	}
	end := sym.EndLine
	if end < sym.Line {
		end = sym.Line
	}
	if maxLines <= 0 {
		maxLines = defaultContextSourceLines
	}
	if end-sym.Line+1 > maxLines {
		end = sym.Line + maxLines - 1
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Sprintf("read source: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxContextLineBytes)

	var out []SourceLine
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo < sym.Line {
			continue
		}
		if lineNo > end {
			break
		}
		out = append(out, SourceLine{
			Number: lineNo,
			Text:   strings.TrimRight(sc.Text(), " \t\r"),
		})
	}
	if err := sc.Err(); err != nil {
		return out, true, fmt.Sprintf("source truncated: %v", err)
	}
	return out, sym.EndLine >= sym.Line && sym.EndLine-sym.Line+1 > maxLines, ""
}
