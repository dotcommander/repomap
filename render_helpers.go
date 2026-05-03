package repomap

import (
	"fmt"
	"slices"
	"strings"
)

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

// docTag returns a parenthetical tag annotating whether doc-line extraction
// is available for this file's language. Go and PHP are fully supported;
// all other languages omit doc lines, and their file header carries "[doc: n/a]".
func docTag(f RankedFile) string {
	switch f.Language {
	case "go", "php":
		return ""
	default:
		return " [doc: n/a]"
	}
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

// Symbol-level diagnostic thresholds. A symbol exceeding any of these gets
// an annotation in the rendered output so an LLM can spot the smell quickly.
const (
	sizeThreshold    = 50 // line span → [NL]
	paramThreshold   = 4  // function/method params > this → [Np]
	resultThreshold  = 2  // function/method returns > this → [Nr]
	methodsThreshold = 5  // interface methods > this → [Nm]
)

// collapsedPreviewLimit is the max number of paths shown before truncation.
const collapsedPreviewLimit = 5

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

// implementsTag returns " [impl: A, B]" for structs that satisfy interfaces,
// or "" if the symbol has no detected implementations.
func implementsTag(s Symbol) string {
	if len(s.Implements) == 0 {
		return ""
	}
	return " [impl: " + strings.Join(s.Implements, ", ") + "]"
}

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
