package repomap

import (
	"fmt"
	"slices"
	"strings"
)

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

	isPHP := f.Language == "php"
	for _, s := range syms {
		if !s.Exported {
			continue
		}
		var line string
		switch {
		case isPHP && s.Signature != "":
			// PHP signatures are self-contained (include keyword + name + params).
			// Rendering "  <signature>" avoids double-printing keyword or name.
			line = fmt.Sprintf("  %s%s", s.Signature, annotationTag(s))
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

// symDisplayName returns the display name for a symbol, with diagnostic tags.
func symDisplayName(s Symbol) string {
	name := s.Name
	if s.Kind == "method" && s.Receiver != "" {
		name = s.Receiver + "." + s.Name
	}
	return name + annotationTag(s) + implementsTag(s)
}
