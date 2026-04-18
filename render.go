package repomap

import (
	"fmt"
	"slices"
	"strings"
)

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
		fmt.Fprint(&b, f.formatDetail())
		shownFiles++
	}

	if len(headerOnly) > 0 {
		fmt.Fprint(&b, formatCollapsedPaths(headerOnly))
	}

	if shownFiles < totalFiles {
		omitted := totalFiles - shownFiles
		fmt.Fprintf(&b, "(%d files omitted — increase -t or use -f compact)\n", omitted)
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
	fmt.Fprint(&b, formatFileLineDetail(f))

	for _, g := range orderedGroups(f.Path, f.Symbols) {
		names := make([]string, 0, len(g.syms))
		for _, s := range g.syms {
			names = append(names, symDisplayName(s))
		}
		slices.Sort(names)
		fmt.Fprintf(&b, "  %s: %s\n", g.label, strings.Join(names, ", "))
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
		}
	}
	fmt.Fprint(&b, "\n")
	return b.String()
}

// fileDiagnostic is one active diagnostic signal on a file. Each renderer
// chooses whether to emit Label (text) or Attr (XML).
type fileDiagnostic struct {
	Label string // text form: "imported by 4", "untested"
	Attr  string // xml form:  `imported-by="4"`, `untested="true"`
}

// fileDiagnostics returns the active signals for a file. minImportedBy is the
// threshold below which ImportedBy is suppressed (text uses 2 to hide
// single-importer noise; XML uses 1 to preserve raw counts).
func fileDiagnostics(f RankedFile, minImportedBy int) []fileDiagnostic {
	var out []fileDiagnostic
	if f.Tag == "entry" {
		out = append(out, fileDiagnostic{Label: "entry", Attr: `tag="entry"`})
	}
	if f.ImportedBy >= minImportedBy && f.ImportedBy > 0 {
		out = append(out, fileDiagnostic{
			Label: fmt.Sprintf("imported by %d", f.ImportedBy),
			Attr:  fmt.Sprintf(`imported-by="%d"`, f.ImportedBy),
		})
	}
	if f.DependsOn > 0 {
		out = append(out, fileDiagnostic{
			Label: fmt.Sprintf("imports: %d", f.DependsOn),
			Attr:  fmt.Sprintf(`imports="%d"`, f.DependsOn),
		})
	}
	if f.Untested {
		out = append(out, fileDiagnostic{Label: "untested", Attr: `untested="true"`})
	}
	if f.ParseMethod == "regex" {
		out = append(out, fileDiagnostic{Label: "inferred", Attr: `parsed="regex"`})
	}
	return out
}

// formatFileLine returns the header line for a file block (path + tag/badge annotations).
// Does NOT include boundary labels — use formatFileLineVerbose for detail/verbose modes.
func formatFileLine(f RankedFile) string {
	diags := fileDiagnostics(f, 2)
	if len(diags) == 0 {
		return f.Path + "\n"
	}
	labels := make([]string, len(diags))
	for i, d := range diags {
		labels[i] = d.Label
	}
	return fmt.Sprintf("%s [%s]\n", f.Path, strings.Join(labels, ", "))
}

// formatFileLineDetail returns the header line for detail/verbose file blocks,
// including boundary labels when present. Compact mode uses formatFileLine instead.
func formatFileLineDetail(f RankedFile) string {
	diags := fileDiagnostics(f, 2)
	labels := make([]string, 0, len(diags)+len(f.Boundaries))
	for _, d := range diags {
		labels = append(labels, d.Label)
	}
	labels = append(labels, f.Boundaries...)
	if len(labels) == 0 {
		return f.Path + "\n"
	}
	return fmt.Sprintf("%s [%s]\n", f.Path, strings.Join(labels, ", "))
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

// docTag returns a parenthetical tag annotating whether doc-line extraction
// is available for this file's language. Go is fully supported; all other
// languages omit doc lines, and their file header carries "[doc: n/a]".
func docTag(f RankedFile) string {
	if f.Language == "go" {
		return ""
	}
	return " [doc: n/a]"
}

// formatFileLineDefault returns the header line for the enriched default block,
// appending [doc: n/a] for non-Go files where doc extraction is unavailable.
func formatFileLineDefault(f RankedFile) string {
	tag := docTag(f)
	if tag == "" {
		return formatFileLine(f)
	}
	// Insert docTag before the trailing newline.
	base := formatFileLine(f)
	// base ends with "\n"; trim it, append tag, restore newline.
	return strings.TrimSuffix(base, "\n") + tag + "\n"
}

// formatFileBlockDefault renders the enriched default block for an LLM consumer:
// exported symbol names + signatures + godoc first line + exported struct fields.
// Symbols are ordered by renderKindWeight descending, then alphabetically for ties.
// Unexported symbols are omitted.
func formatFileBlockDefault(f RankedFile) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLineDefault(f))

	// Sort symbols: high renderKindWeight first, then alphabetically within the same weight.
	syms := make([]Symbol, len(f.Symbols))
	copy(syms, f.Symbols)
	slices.SortStableFunc(syms, func(a, b Symbol) int {
		wa, wb := renderKindWeight(a.Kind, a.Exported), renderKindWeight(b.Kind, b.Exported)
		if wa != wb {
			return wb - wa // descending by weight
		}
		return strings.Compare(a.Name, b.Name)
	})

	for _, s := range syms {
		if !s.Exported {
			continue
		}
		var line string
		switch {
		case s.Kind == "method" && s.Receiver != "":
			line = fmt.Sprintf("  func (%s) %s%s%s", s.Receiver, s.Name, s.Signature, annotationTag(s))
		case s.Signature != "" && s.Signature != "{}":
			line = fmt.Sprintf("  %s %s%s%s", kindKeyword(s.Kind), s.Name, s.Signature, annotationTag(s))
		default:
			line = fmt.Sprintf("  %s %s%s", kindKeyword(s.Kind), s.Name, annotationTag(s))
		}
		fmt.Fprintln(&b, line)
		if s.Doc != "" {
			fmt.Fprintf(&b, "    // %s\n", s.Doc)
		}
	}
	fmt.Fprint(&b, "\n")
	return b.String()
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

// formatDetail renders the file at its assigned DetailLevel.
// DetailLevel 2 and 3 both dispatch to formatFileBlockDefault (enriched default).
// DetailLevel 3 aliasing DetailLevel 2 is intentional for v0.7.0;
// distinction deferred to v0.8.0 (e.g. unexported fields at level 3).
func (f RankedFile) formatDetail() string {
	switch f.DetailLevel {
	case 0:
		return formatFileHeaderOnly(f)
	case 1:
		return formatFileBlockSummary(f)
	case 2, 3:
		return formatFileBlockDefault(f)
	default:
		return ""
	}
}

// Symbol-level diagnostic thresholds. A symbol exceeding any of these gets
// an annotation in the rendered output so an LLM can spot the smell quickly.
const (
	sizeThreshold    = 50 // line span → [NL]
	paramThreshold   = 4  // function/method params > this → [Np]
	resultThreshold  = 2  // function/method returns > this → [Nr]
	methodsThreshold = 5  // interface methods > this → [Nm]
)

// sizeTag returns " [NL]" for symbols exceeding the size threshold, or "".
// Kept for existing tests and single-signal callers.
func sizeTag(s Symbol) string {
	if span := s.LineSpan(); span >= sizeThreshold {
		return fmt.Sprintf(" [%dL]", span)
	}
	return ""
}

// annotationTag returns a single bracketed suffix combining all symbol-level
// diagnostic signals (size + signature smells), e.g. " [185L, 5p, 3r]".
// Empty string if no signal crosses its threshold.
func annotationTag(s Symbol) string {
	var parts []string
	if span := s.LineSpan(); span >= sizeThreshold {
		parts = append(parts, fmt.Sprintf("%dL", span))
	}
	switch s.Kind {
	case "function", "method":
		if s.ParamCount > paramThreshold {
			parts = append(parts, fmt.Sprintf("%dp", s.ParamCount))
		}
		if s.ResultCount > resultThreshold {
			parts = append(parts, fmt.Sprintf("%dr", s.ResultCount))
		}
	case "interface":
		if s.ParamCount > methodsThreshold {
			parts = append(parts, fmt.Sprintf("%dm", s.ParamCount))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, ", ") + "]"
}

// symDisplayName returns the display name for a symbol, with diagnostic tags.
func symDisplayName(s Symbol) string {
	name := s.Name
	if s.Kind == "method" && s.Receiver != "" {
		name = s.Receiver + "." + s.Name
	}
	return name + annotationTag(s) + implementsTag(s)
}

// implementsTag returns " [impl: A, B]" for structs that satisfy interfaces,
// or "" if the symbol has no detected implementations.
func implementsTag(s Symbol) string {
	if len(s.Implements) == 0 {
		return ""
	}
	return " [impl: " + strings.Join(s.Implements, ", ") + "]"
}

// collapsedPreviewLimit is the max number of paths shown before truncation.
const collapsedPreviewLimit = 5

// formatCollapsedPaths renders header-only files as a single compact line.
// Shows: (+N more: a.go, b.go, c.go, ...)
func formatCollapsedPaths(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	slices.Sort(paths)
	preview := paths
	truncated := false
	if len(preview) > collapsedPreviewLimit {
		preview = preview[:collapsedPreviewLimit]
		truncated = true
	}

	body := strings.Join(preview, ", ")
	if truncated {
		body += ", ..."
	}
	return fmt.Sprintf("(+%d more: %s)\n", len(paths), body)
}
