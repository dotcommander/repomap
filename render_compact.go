package repomap

import (
	"fmt"
	"slices"
	"strings"
)

// countTotals returns the total number of files and the total symbol count.
func countTotals(files []RankedFile) (int, int) {
	nSymbols := 0
	for _, f := range files {
		nSymbols += len(f.Symbols)
	}
	return len(files), nSymbols
}

// kindKeyword maps a symbol kind to its Go syntax keyword for display.
// Unknown kinds fall back to the raw kind string so output is never empty.
func kindKeyword(kind string) string {
	switch kind {
	case "function", "fn":
		return "func"
	case "struct", "class":
		return "type"
	case "interface":
		return "type"
	case "type":
		return "type"
	case "method":
		return "func"
	case "constant", "const":
		return "const"
	case "variable", "var", "static":
		return "var"
	default:
		return kind
	}
}

// formatFileBlockLean renders the lean orientation block for -f compact:
// path + exported symbol names only — no signatures, no godoc, no struct fields.
// Symbols are grouped as comma-separated names on a single indented line per category
// (using existing orderedGroups machinery) to keep the output scannable.
func formatFileBlockLean(f RankedFile) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLine(f))

	for _, g := range orderedGroups(f.Path, f.Symbols) {
		names := make([]string, 0, len(g.syms))
		for _, s := range g.syms {
			if s.Exported {
				names = append(names, s.Name)
			}
		}
		slices.Sort(names)
		if len(names) > 0 {
			fmt.Fprintf(&b, "  %s: %s\n", g.label, strings.Join(names, ", "))
		}
	}
	fmt.Fprint(&b, "\n")
	return b.String()
}

// formatFileBlockSummary returns a summary block showing category counts only.
func formatFileBlockSummary(f RankedFile) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLine(f))

	groups := summarizeSymbols(f)
	counts := make([]string, 0, len(groups))
	for _, g := range groups {
		counts = append(counts, fmt.Sprintf("%d %s", g.count, g.label))
	}
	if len(counts) > 0 {
		fmt.Fprintf(&b, "  %s\n", strings.Join(counts, ", "))
	}
	fmt.Fprint(&b, "\n")
	return b.String()
}

// formatFileBlockCompact returns the compact block with struct fields for top-ranked types.
func formatFileBlockCompact(f RankedFile, topTypes map[string]bool) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLine(f))

	groups := summarizeSymbols(f)
	for _, g := range groups {
		fmt.Fprintf(&b, "  %s: %s\n", g.label, g.summary)
	}

	for _, s := range f.Symbols {
		if s.Signature == "" || s.Signature == "{}" {
			continue
		}
		if (s.Kind == "struct" || s.Kind == "interface") && topTypes[s.Name] {
			fmt.Fprintf(&b, "  %s %s%s\n", s.Name, s.Signature, implementsTag(s))
		}
	}

	fmt.Fprint(&b, "\n")
	return b.String()
}
