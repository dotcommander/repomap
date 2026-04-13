package repomap

import (
	"fmt"
	"sort"
	"strings"
)

// categoryOrder defines the display order for symbol categories.
var categoryOrder = []struct {
	key   string
	label string
}{
	{"tests", "tests"},
	{"types", "types"},
	{"interfaces", "interfaces"},
	{"classes", "classes"},
	{"enums", "enums"},
	{"funcs", "funcs"},
	{"methods", "methods"},
	{"consts", "consts"},
	{"vars", "vars"},
	{"other", "other"},
}

// FormatMap formats ranked files into a token-budgeted text representation.
// maxTokens controls the output size (estimated as len(text)/4).
// Returns empty string if no files have symbols.
// When verbose is true, shows all symbols without summarization.
// When detail is true, shows signatures for funcs/methods and fields for structs.
func FormatMap(files []RankedFile, maxTokens int, verbose, detail bool) string {
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
				fmt.Fprint(&b, formatFileBlockDetail(f))
			} else {
				fmt.Fprint(&b, formatFileBlockVerbose(f))
			}
		}
		return b.String()
	}

	// Budget mode: assign detail levels, then render.
	files = BudgetFiles(files, maxTokens)

	shownFiles := 0
	for _, f := range files {
		switch f.DetailLevel {
		case -1:
			continue
		case 0:
			fmt.Fprint(&b, formatFileHeaderOnly(f))
		case 1:
			fmt.Fprint(&b, formatFileBlockSummary(f))
		case 2:
			fmt.Fprint(&b, formatFileBlockCompact(f, nil))
		case 3:
			top := map[string]bool{}
			for _, s := range f.Symbols {
				if (s.Kind == "struct" || s.Kind == "interface") && s.Signature != "" && s.Signature != "{}" {
					top[s.Name] = true
				}
			}
			fmt.Fprint(&b, formatFileBlockCompact(f, top))
		}
		shownFiles++
	}

	if shownFiles < totalFiles {
		fmt.Fprintf(&b, "(%d symbols across %d files, showing top %d)\n", totalSymbols, totalFiles, shownFiles)
	}

	return b.String()
}

// countTotals returns the total number of files and the total symbol count.
func countTotals(files []RankedFile) (int, int) {
	nSymbols := 0
	for _, f := range files {
		nSymbols += len(f.Symbols)
	}
	return len(files), nSymbols
}

// buildHeader returns the shared header block (title + dependency graph) used
// by all format modes.
func buildHeader(files []RankedFile, totalFiles, totalSymbols int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Repository Map (%d files, %d symbols)\n\n", totalFiles, totalSymbols)
	if graph := formatDependencyGraph(files); graph != "" {
		fmt.Fprint(&b, graph)
		fmt.Fprint(&b, "\n")
	}
	return b.String()
}

// formatFileBlockVerbose returns a verbose block showing all symbols without summarization.
func formatFileBlockVerbose(f RankedFile) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLine(f))

	categorized := categorizeByKind(f.Path, f.Symbols)

	for _, item := range categoryOrder {
		syms := categorized[item.key]
		if len(syms) == 0 {
			continue
		}
		names := make([]string, 0, len(syms))
		for _, s := range syms {
			if item.key == "methods" && s.Receiver != "" {
				names = append(names, s.Receiver+"."+s.Name)
			} else {
				names = append(names, s.Name)
			}
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "  %s: %s\n", item.label, strings.Join(names, ", "))
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

// formatFileBlockDetail returns a detailed block showing signatures and struct fields.
func formatFileBlockDetail(f RankedFile) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLine(f))

	categorized := categorizeByKind(f.Path, f.Symbols)

	for _, item := range categoryOrder {
		syms := categorized[item.key]
		if len(syms) == 0 {
			continue
		}

		sort.Slice(syms, func(i, j int) bool {
			return syms[i].Name < syms[j].Name
		})

		var lines []string
		for _, s := range syms {
			var line string
			switch {
			case item.key == "methods" && s.Receiver != "":
				if s.Signature != "" {
					line = fmt.Sprintf("%s.%s%s", s.Receiver, s.Name, s.Signature)
				} else {
					line = fmt.Sprintf("%s.%s", s.Receiver, s.Name)
				}
			case (item.key == "types" || item.key == "interfaces") && s.Signature != "":
				line = fmt.Sprintf("%s %s", s.Name, s.Signature)
			case item.key == "funcs" && s.Signature != "":
				line = fmt.Sprintf("%s%s", s.Name, s.Signature)
			default:
				line = s.Name
			}
			lines = append(lines, line)
		}

		fmt.Fprintf(&b, "  %s:\n", item.label)
		for _, line := range lines {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	fmt.Fprint(&b, "\n")
	return b.String()
}

// formatFileLine returns the header line for a file block (path + tag/badge annotations).
func formatFileLine(f RankedFile) string {
	var tags []string
	if f.Tag == "entry" {
		tags = append(tags, "entry")
	}
	if f.ImportedBy > 0 {
		tags = append(tags, fmt.Sprintf("imported by %d", f.ImportedBy))
	}
	if f.ParseMethod == "regex" {
		tags = append(tags, "inferred")
	}
	if len(tags) == 0 {
		return f.Path + "\n"
	}
	return fmt.Sprintf("%s [%s]\n", f.Path, strings.Join(tags, ", "))
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
			fmt.Fprintf(&b, "  %s %s\n", s.Name, s.Signature)
		}
	}

	fmt.Fprint(&b, "\n")
	return b.String()
}

// formatFileHeaderOnly returns a minimal block for files with no exported symbols.
func formatFileHeaderOnly(f RankedFile) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLine(f))
	if f.Package != "" {
		fmt.Fprintf(&b, "  (package %s)\n", f.Package)
	}
	fmt.Fprint(&b, "\n")
	return b.String()
}
