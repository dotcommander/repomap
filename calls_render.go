package repomap

import (
	"fmt"
	"slices"
	"strings"
)

// formatCallersInline formats callers for a symbol as a compact inline string.
// Example: "callers: ../hook/hook.go:18, ../search/scorer.go:55 (3 total)"
func formatCallersInline(locs []Location, total int) string {
	if len(locs) == 0 {
		return ""
	}
	parts := make([]string, len(locs))
	for i, loc := range locs {
		parts[i] = fmt.Sprintf("%s:%d", loc.File, loc.Line)
	}
	slices.Sort(parts)
	body := strings.Join(parts, ", ")
	if total > len(locs) {
		return fmt.Sprintf("callers: %s (%d total)", body, total)
	}
	return fmt.Sprintf("callers: %s", body)
}

// formatFileBlockVerboseWithCallers is like formatFileBlockVerbose but annotates
// each symbol group line with caller counts from the callers map.
func formatFileBlockVerboseWithCallers(f RankedFile, callers SymbolCallers, limit int) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLineDetail(f))

	for _, g := range orderedGroups(f.Path, f.Symbols) {
		names := make([]string, 0, len(g.syms))
		for _, s := range g.syms {
			name := symDisplayName(s)
			if locs, ok := callers[callsKey(f.Path, s.Name)]; ok {
				name += fmt.Sprintf(" [callers: %d]", len(locs))
			}
			names = append(names, name)
		}
		slices.Sort(names)
		fmt.Fprintf(&b, "  %s: %s\n", g.label, strings.Join(names, ", "))
	}
	fmt.Fprint(&b, "\n")
	return b.String()
}

// formatFileBlockDetailWithCallers extends formatFileBlockDetail to show callers.
func formatFileBlockDetailWithCallers(f RankedFile, callers SymbolCallers, limit int) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLineDetail(f))

	for _, g := range orderedGroups(f.Path, f.Symbols) {
		slices.SortFunc(g.syms, func(a, b Symbol) int {
			return strings.Compare(a.Name, b.Name)
		})

		fmt.Fprintf(&b, "  %s:\n", g.label)
		for _, s := range g.syms {
			var line string
			switch {
			case g.key == "methods" && s.Receiver != "":
				if s.Signature != "" {
					line = fmt.Sprintf("%s.%s%s%s", s.Receiver, s.Name, s.Signature, annotationTag(s))
				} else {
					line = fmt.Sprintf("%s.%s%s", s.Receiver, s.Name, annotationTag(s))
				}
			case (g.key == "types" || g.key == "interfaces") && s.Signature != "":
				line = fmt.Sprintf("%s %s%s%s", s.Name, s.Signature, annotationTag(s), implementsTag(s))
			case g.key == "funcs" && s.Signature != "":
				line = fmt.Sprintf("%s%s%s", s.Name, s.Signature, annotationTag(s))
			default:
				line = s.Name + annotationTag(s)
			}
			fmt.Fprintf(&b, "    %s\n", line)
			if s.Doc != "" {
				fmt.Fprintf(&b, "      // %s\n", s.Doc)
			}
			if locs, ok := callers[callsKey(f.Path, s.Name)]; ok {
				fmt.Fprintf(&b, "      callers: %s\n", formatCallersInline(locs, len(locs)))
			}
		}
	}
	fmt.Fprint(&b, "\n")
	return b.String()
}

// formatFileBlockCompactWithCallers extends compact output to show per-group caller counts.
// For each symbol group, if any symbol in the group has callers, it appends
// a callers annotation to the group header.
func formatFileBlockCompactWithCallers(f RankedFile, topTypes map[string]bool, callers SymbolCallers) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLine(f))

	// Use orderedGroups (which carries the symbols) to compute per-group caller counts,
	// but still emit the summary strings from summarizeSymbols for display consistency.
	summaryGroups := summarizeSymbols(f)
	categorizedGroups := orderedGroups(f.Path, f.Symbols)

	// Build a label → caller-count map from the categorized groups.
	callersByLabel := make(map[string]int, len(categorizedGroups))
	for _, cg := range categorizedGroups {
		total := 0
		for _, s := range cg.syms {
			if locs, ok := callers[callsKey(f.Path, s.Name)]; ok {
				total += len(locs)
			}
		}
		if total > 0 {
			callersByLabel[cg.label] = total
		}
	}

	for _, g := range summaryGroups {
		line := fmt.Sprintf("  %s: %s", g.label, g.summary)
		if n := callersByLabel[g.label]; n > 0 {
			line += fmt.Sprintf(" [%d callers]", n)
		}
		fmt.Fprintln(&b, line)
	}

	for _, s := range f.Symbols {
		if s.Signature == "" || s.Signature == "{}" {
			continue
		}
		if (s.Kind == "struct" || s.Kind == "interface") && topTypes[s.Name] {
			locs := callers[callsKey(f.Path, s.Name)]
			suffix := ""
			if len(locs) > 0 {
				suffix = fmt.Sprintf(" [callers: %d]", len(locs))
			}
			fmt.Fprintf(&b, "  %s %s%s%s\n", s.Name, s.Signature, implementsTag(s), suffix)
		}
	}

	fmt.Fprint(&b, "\n")
	return b.String()
}

// FormatMapWithCallers formats the ranked files like FormatMap but injects caller
// information from the callers map into the output.
func FormatMapWithCallers(files []RankedFile, maxTokens int, verbose, detail bool, callers SymbolCallers, limit int) string {
	totalFiles, totalSymbols := countTotals(files)
	if totalFiles == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprint(&b, buildHeader(files, totalFiles, totalSymbols))

	if verbose {
		for _, f := range files {
			if len(f.Symbols) == 0 {
				fmt.Fprint(&b, formatFileHeaderOnly(f))
				continue
			}
			if detail {
				fmt.Fprint(&b, formatFileBlockDetailWithCallers(f, callers, limit))
			} else {
				fmt.Fprint(&b, formatFileBlockVerboseWithCallers(f, callers, limit))
			}
		}
		return b.String()
	}

	// Budget mode.
	files = BudgetFiles(files, maxTokens)

	var headerOnly []string
	shownFiles := 0
	for _, f := range files {
		if f.DetailLevel < 0 {
			continue
		}
		if f.DetailLevel == 0 && len(f.Symbols) == 0 && f.Tag == "" {
			headerOnly = append(headerOnly, f.Path)
			shownFiles++
			continue
		}
		fmt.Fprint(&b, f.formatDetailWithCallers(callers, limit))
		shownFiles++
	}

	if len(headerOnly) > 0 {
		fmt.Fprint(&b, formatCollapsedPaths(headerOnly))
	}

	if shownFiles < totalFiles {
		fmt.Fprintf(&b, "(%d symbols across %d files, showing top %d)\n", totalSymbols, totalFiles, shownFiles)
	}

	return b.String()
}

// formatDetailWithCallers renders the file at its assigned DetailLevel, injecting callers.
func (f RankedFile) formatDetailWithCallers(callers SymbolCallers, limit int) string {
	switch f.DetailLevel {
	case 0:
		return formatFileHeaderOnly(f)
	case 1:
		return formatFileBlockSummary(f)
	case 2:
		return formatFileBlockCompactWithCallers(f, nil, callers)
	case 3:
		top := make(map[string]bool)
		for _, s := range f.Symbols {
			if s.HasFields() {
				top[s.Name] = true
			}
		}
		return formatFileBlockCompactWithCallers(f, top, callers)
	default:
		return ""
	}
}
