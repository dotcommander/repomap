package repomap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FormatLines formats ranked files showing actual source code lines.
// root is the project root for resolving file paths.
func FormatLines(files []RankedFile, maxTokens int, root string) string {
	totalFiles, totalSymbols := countTotals(files)
	if totalFiles == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprint(&b, buildHeader(files, totalFiles, totalSymbols))

	budgetBytes := maxTokens * 4
	shownFiles := 0

	for _, f := range files {
		if len(f.Symbols) == 0 {
			block := formatFileHeaderOnly(f)
			if shownFiles > 0 && budgetBytes > 0 && b.Len()+len(block) > budgetBytes {
				break
			}
			fmt.Fprint(&b, block)
			shownFiles++
			continue
		}

		if isTestFile(f.Path) {
			continue
		}

		// Skip file read if we're clearly over budget (200 bytes = minimum useful block).
		if shownFiles > 0 && budgetBytes > 0 && b.Len() > budgetBytes-200 {
			break
		}

		block := formatFileBlockLines(f, root)

		if shownFiles > 0 && budgetBytes > 0 && b.Len()+len(block) > budgetBytes {
			break
		}

		fmt.Fprint(&b, block)
		shownFiles++
	}

	if shownFiles < totalFiles {
		fmt.Fprintf(&b, "(%d files shown of %d total)\n", shownFiles, totalFiles)
	}

	return b.String()
}

// formatFileBlockLines shows actual source lines for each symbol in a file.
// Reads the source file on demand to avoid loading files that get budget-cut.
func formatFileBlockLines(f RankedFile, root string) string {
	var b strings.Builder
	fmt.Fprint(&b, formatFileLine(f))

	var lines []string
	absPath := filepath.Join(root, f.Path)
	if data, err := os.ReadFile(absPath); err == nil {
		lines = strings.Split(string(data), "\n")
	}

	// Collect symbols with known line numbers, sorted by position.
	type symLine struct {
		sym  Symbol
		line int
	}
	var syms []symLine
	for _, s := range f.Symbols {
		if s.Line > 0 && s.Exported && isSignificantKind(f.Language, s.Kind) {
			syms = append(syms, symLine{sym: s, line: s.Line})
		}
	}

	sort.Slice(syms, func(i, j int) bool {
		return syms[i].line < syms[j].line
	})

	for _, sl := range syms {
		var text string

		// For structs/interfaces with field info, prefer synthesized line
		if (sl.sym.Kind == "struct" || sl.sym.Kind == "interface") &&
			sl.sym.Signature != "" && sl.sym.Signature != "{}" {
			text = synthesizeLine(sl.sym)
		} else if lines != nil && sl.line > 0 && sl.line <= len(lines) {
			text = strings.TrimRight(lines[sl.line-1], " \t\r")
			text = strings.TrimLeft(text, "\t ")
			// Trim trailing opening brace from Go definitions
			text, _ = strings.CutSuffix(text, " {")
		}

		if text == "" {
			text = synthesizeLine(sl.sym)
		}
		fmt.Fprintf(&b, "│ %s\n", text)
	}

	fmt.Fprint(&b, "\n")
	return b.String()
}

// synthesizeLine creates a readable definition line from symbol metadata
// when source line is unavailable.
func synthesizeLine(s Symbol) string {
	switch s.Kind {
	case "function", "fn":
		if s.Signature != "" {
			return "func " + s.Name + s.Signature
		}
		return "func " + s.Name + "()"
	case "method":
		prefix := "func "
		if s.Receiver != "" {
			prefix += "(" + s.Receiver + ") "
		}
		if s.Signature != "" {
			return prefix + s.Name + s.Signature
		}
		return prefix + s.Name + "()"
	case "struct":
		if s.Signature != "" && s.Signature != "{}" {
			return "type " + s.Name + " struct " + s.Signature
		}
		return "type " + s.Name + " struct{}"
	case "interface":
		if s.Signature != "" && s.Signature != "{}" {
			return "type " + s.Name + " interface " + s.Signature
		}
		return "type " + s.Name + " interface{}"
	case "type":
		return "type " + s.Name
	case "class":
		return "class " + s.Name
	case "constant", "const":
		return "const " + s.Name
	case "variable", "static":
		return "var " + s.Name
	case "enum":
		return "enum " + s.Name
	default:
		return s.Name
	}
}

// isSignificantKind returns true for symbol kinds worth showing in lines mode.
// Filters out local variables and constants for non-Go languages where ctags
// picks up function-scoped declarations.
func isSignificantKind(lang, kind string) bool {
	switch kind {
	case "function", "fn", "method", "class", "struct", "interface",
		"type", "enum", "trait", "impl":
		return true
	case "constant", "const":
		// Go package-level constants and PHP class constants are significant.
		return lang == "go" || lang == "php"
	case "variable", "static":
		// Only Go package-level vars are significant — ctags picks up
		// local variables in other languages.
		return lang == "go"
	default:
		return false
	}
}
