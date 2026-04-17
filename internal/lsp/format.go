package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FormatLocations formats a list of locations as readable text.
func FormatLocations(locs []Location, cwd string, contextLines int) string {
	if len(locs) == 0 {
		return "No results found."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d location(s) found:\n\n", len(locs))

	// Cache file contents to avoid reading the same file multiple times.
	fileCache := make(map[string][]string)

	for _, loc := range locs {
		path := uriToPath(loc.URI)
		rel := relativePath(path, cwd)
		line := loc.Range.Start.Line + 1

		fmt.Fprintf(&b, "%s:%d\n", rel, line)

		lines := cachedLines(fileCache, path)
		if context := formatContext(lines, loc.Range.Start.Line, contextLines); context != "" {
			b.WriteString(context)
			b.WriteByte('\n')
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// FormatHover formats hover information.
func FormatHover(hover *HoverResult) string {
	if hover == nil || hover.Contents.Value == "" {
		return "No hover information available."
	}
	return hover.Contents.Value
}

// FormatSymbols formats document symbols as a structured list.
func FormatSymbols(symbols []DocumentSymbol, cwd string) string {
	if len(symbols) == 0 {
		return "No symbols found."
	}

	var b strings.Builder
	formatSymbolTree(&b, symbols, 0)
	return strings.TrimRight(b.String(), "\n")
}

func formatSymbolTree(b *strings.Builder, symbols []DocumentSymbol, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, s := range symbols {
		line := s.Range.Start.Line + 1
		fmt.Fprintf(b, "%s%s %s (line %d)\n", indent, s.Kind, s.Name, line)
		if len(s.Children) > 0 {
			formatSymbolTree(b, s.Children, depth+1)
		}
	}
}

func cachedLines(cache map[string][]string, path string) []string {
	if lines, ok := cache[path]; ok {
		return lines
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(content), "\n")
	cache[path] = lines
	return lines
}

func formatContext(lines []string, targetLine, contextSize int) string {
	if len(lines) == 0 {
		return ""
	}

	start := targetLine - contextSize
	if start < 0 {
		start = 0
	}
	end := targetLine + contextSize + 1
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		marker := "  "
		if i == targetLine {
			marker = "> "
		}
		fmt.Fprintf(&b, "%s%4d | %s\n", marker, i+1, lines[i])
	}
	return strings.TrimRight(b.String(), "\n")
}

func relativePath(path, cwd string) string {
	if cwd == "" {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return path
	}
	return rel
}
