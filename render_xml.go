package repomap

import (
	"fmt"
	"strings"
)

// FormatXML formats ranked files as a structured XML document.
// maxTokens controls the output size (estimated as len(text)/4).
// cfg may be nil — nil means no file-level detail overrides.
// Returns empty string if no files have symbols.
func FormatXML(files []RankedFile, maxTokens int, cfg *BlocklistConfig) string {
	totalFiles, totalSymbols := countTotals(files)
	if totalFiles == 0 {
		return ""
	}

	if maxTokens > 0 {
		files = BudgetFiles(files, maxTokens, cfg)
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteByte('\n')
	fmt.Fprintf(&b, "<repomap files=\"%d\" symbols=\"%d\">\n", totalFiles, totalSymbols)

	if graph := xmlDependencyGraph(files); graph != "" {
		fmt.Fprint(&b, graph)
	}

	for _, f := range files {
		if f.DetailLevel < 0 && maxTokens > 0 {
			continue
		}
		xmlFileBlock(&b, f)
	}

	b.WriteString("</repomap>\n")
	return b.String()
}

// xmlEscape returns s with XML-special characters escaped.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// xmlDependencyGraph builds the <dependencies> section for Go packages.
func xmlDependencyGraph(files []RankedFile) string {
	deps, _ := buildPackageDeps(files)
	if deps == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("  <dependencies>\n")
	for _, pkg := range sortedPkgs(deps) {
		fmt.Fprintf(&b, "    <pkg name=\"%s\">%s</pkg>\n",
			xmlEscape(pkg), xmlEscape(strings.Join(deps[pkg], ", ")))
	}
	b.WriteString("  </dependencies>\n")
	return b.String()
}

// xmlFileBlock appends the XML block for one file.
func xmlFileBlock(b *strings.Builder, f RankedFile) {
	attrs := fmt.Sprintf(`path="%s" lang="%s"`, xmlEscape(f.Path), xmlEscape(f.Language))
	if f.Score > 0 {
		attrs += fmt.Sprintf(` score="%d"`, f.Score)
	}
	if f.Tag != "" {
		attrs += fmt.Sprintf(` tag="%s"`, xmlEscape(f.Tag))
	}
	if f.DetailLevel >= 0 {
		attrs += fmt.Sprintf(` detail="%d"`, f.DetailLevel)
	}
	if f.Package != "" {
		attrs += fmt.Sprintf(` pkg="%s"`, xmlEscape(f.Package))
	}
	if f.ParseMethod != "" {
		attrs += fmt.Sprintf(` parsed="%s"`, xmlEscape(f.ParseMethod))
	}
	if len(f.Boundaries) > 0 {
		attrs += fmt.Sprintf(` boundaries="%s"`, xmlEscape(strings.Join(f.Boundaries, ",")))
	}

	// Diagnostic attrs — minImportedBy=1 emits raw counts (XML is machine-read).
	// Tag/ParseMethod attrs above already cover the full values; skip diagnostics
	// that would duplicate them.
	var diagAttrs []string
	for _, d := range fileDiagnostics(f, 1) {
		switch d.Attr {
		case `tag="entry"`, `parsed="regex"`:
			continue // emitted as top-level attrs above
		}
		diagAttrs = append(diagAttrs, d.Attr)
	}

	openAttrs := attrs
	if len(diagAttrs) > 0 {
		openAttrs = attrs + " " + strings.Join(diagAttrs, " ")
	}

	if len(f.Symbols) == 0 {
		fmt.Fprintf(b, "  <file %s/>\n", openAttrs)
		return
	}
	fmt.Fprintf(b, "  <file %s>\n", openAttrs)
	xmlSymbols(b, f.Symbols)
	b.WriteString("  </file>\n")
}

// xmlSymbols appends <symbols> with each symbol as a child element.
func xmlSymbols(b *strings.Builder, syms []Symbol) {
	b.WriteString("    <symbols>\n")

	for _, s := range syms {
		attr := fmt.Sprintf(`name="%s" kind="%s"`, xmlEscape(s.Name), xmlEscape(s.Kind))
		if s.Exported {
			attr += ` exported="true"`
		}
		if s.Line > 0 {
			attr += fmt.Sprintf(` line="%d"`, s.Line)
		}
		if span := s.LineSpan(); span > 0 {
			attr += fmt.Sprintf(` span="%d"`, span)
		}
		if s.Receiver != "" {
			attr += fmt.Sprintf(` receiver="%s"`, xmlEscape(s.Receiver))
		}
		switch s.Kind {
		case "function", "method":
			if s.ParamCount > 0 {
				attr += fmt.Sprintf(` params="%d"`, s.ParamCount)
			}
			if s.ResultCount > 0 {
				attr += fmt.Sprintf(` results="%d"`, s.ResultCount)
			}
		case "interface":
			if s.ParamCount > 0 {
				attr += fmt.Sprintf(` methods="%d"`, s.ParamCount)
			}
		}
		if len(s.Implements) > 0 {
			attr += fmt.Sprintf(` implements="%s"`, xmlEscape(strings.Join(s.Implements, ",")))
		}
		if s.Doc != "" {
			attr += fmt.Sprintf(` doc="%s"`, xmlEscape(s.Doc))
		}

		if s.Signature != "" {
			fmt.Fprintf(b, "      <sym %s>%s</sym>\n", attr, xmlEscape(s.Signature))
		} else {
			fmt.Fprintf(b, "      <sym %s/>\n", attr)
		}
	}

	b.WriteString("    </symbols>\n")
}
