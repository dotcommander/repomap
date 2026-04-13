package repomap

import (
	"regexp"
	"strings"
)

// --- Rust patterns ---

var (
	rustPubItem  = regexp.MustCompile(`^pub\s+(fn|struct|enum|trait|type|const|static)\s+(\w+)`)
	rustPubAsync = regexp.MustCompile(`^pub\s+async\s+fn\s+(\w+)`)
	rustImpl     = regexp.MustCompile(`^impl(?:<[^>]*>)?\s+(\w+)`)
	rustUse      = regexp.MustCompile(`^use\s+([^;{]+)`)
)

// --- C / C++ patterns ---

var (
	cFunc    = regexp.MustCompile(`^(?:[\w:*&\s]+)\s+(\w+)\s*\(`)
	cTagDecl = regexp.MustCompile(`^(?:struct|class|enum|typedef)\s+(\w+)`)
	cInclude = regexp.MustCompile(`^#include\s*[<"]([^>"]+)[>"]`)
)

// --- Java patterns ---

var (
	javaTypeDecl   = regexp.MustCompile(`public\s+(?:static\s+)?(?:final\s+)?(?:class|interface|enum|record)\s+(\w+)`)
	javaMethodDecl = regexp.MustCompile(`public\s+(?:static\s+)?(?:[\w<>\[\],\s]+)\s+(\w+)\s*\(`)
	javaImport     = regexp.MustCompile(`^import\s+(?:static\s+)?([^;]+)`)
)

// parseRust processes Rust lines.
func parseRust(lines []string, fs *FileSymbols) {
	scanLines(lines, func(e lineEntry) bool {
		if tryAppendSymbol(rustPubAsync, e, "fn", true, fs) {
			return true
		}
		if m := rustPubItem.FindStringSubmatch(e.trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[2], Kind: m[1], Exported: true, Line: e.idx + 1})
			return true
		}
		if tryAppendSymbol(rustImpl, e, "impl", true, fs) {
			return true
		}
		tryAppendImport(rustUse, e, fs)
		return true
	})
}

// parseC processes C/C++ lines.
func parseC(lines []string, fs *FileSymbols) {
	scanLines(lines, func(e lineEntry) bool {
		// Skip preprocessor directives other than #include
		if strings.HasPrefix(e.trimmed, "#") {
			if m := cInclude.FindStringSubmatch(e.trimmed); m != nil {
				fs.Imports = append(fs.Imports, m[1])
			}
			return true
		}

		if m := cTagDecl.FindStringSubmatch(e.trimmed); m != nil {
			kind := strings.Fields(e.trimmed)[0] // struct / class / enum / typedef
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: kind, Exported: true, Line: e.idx + 1})
			return true
		}

		// Function declarations: must start at column 0 (no leading whitespace)
		// and contain a '('.
		if e.line == e.trimmed && strings.Contains(e.line, "(") {
			if m := cFunc.FindStringSubmatch(e.trimmed); m != nil {
				fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "function", Exported: true, Line: e.idx + 1})
			}
		}
		return true
	})
}

// parseJava processes Java lines.
func parseJava(lines []string, fs *FileSymbols) {
	scanLines(lines, func(e lineEntry) bool {
		if tryAppendImport(javaImport, e, fs) {
			return true
		}
		if m := javaTypeDecl.FindStringSubmatch(e.trimmed); m != nil {
			// Determine the kind from the keyword preceding the name
			kind := "class"
			for _, kw := range []string{"interface", "enum", "record"} {
				if strings.Contains(e.trimmed, kw) {
					kind = kw
					break
				}
			}
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: kind, Exported: true, Line: e.idx + 1})
			return true
		}
		tryAppendSymbol(javaMethodDecl, e, "method", true, fs)
		return true
	})
}

// lineEntry represents a non-comment line yielded by scanLines.
type lineEntry struct {
	idx     int    // 0-based line index
	line    string // original line (with whitespace)
	trimmed string // trimmed line
}

// scanLines iterates over lines, skipping block comments (/* ... */).
// It calls fn for each non-comment line. If fn returns false, iteration stops.
func scanLines(lines []string, fn func(entry lineEntry) bool) {
	inBlockComment := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		inBlockComment = trackBlockComment(trimmed, inBlockComment)
		if inBlockComment {
			continue
		}
		if !fn(lineEntry{idx: i, line: line, trimmed: trimmed}) {
			break
		}
	}
}

// trackBlockComment advances the block-comment state machine for C-family
// languages (/* ... */). It returns the new inBlockComment state.
func trackBlockComment(trimmed string, inBlockComment bool) bool {
	if inBlockComment {
		if idx := strings.Index(trimmed, "*/"); idx >= 0 {
			return false
		}
		return true
	}
	if idx := strings.Index(trimmed, "/*"); idx >= 0 {
		// Only enter block comment if the closing */ is not on the same line.
		rest := trimmed[idx+2:]
		if !strings.Contains(rest, "*/") {
			return true
		}
	}
	return false
}

// tryAppendSymbol matches a regex and appends a symbol if found.
func tryAppendSymbol(re *regexp.Regexp, e lineEntry, kind string, exported bool, fs *FileSymbols) bool {
	m := re.FindStringSubmatch(e.trimmed)
	if m == nil {
		return false
	}
	fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: kind, Exported: exported, Line: e.idx + 1})
	return true
}

// tryAppendImport matches a regex and appends an import if found.
func tryAppendImport(re *regexp.Regexp, e lineEntry, fs *FileSymbols) bool {
	m := re.FindStringSubmatch(e.trimmed)
	if m == nil {
		return false
	}
	fs.Imports = append(fs.Imports, strings.TrimSpace(m[1]))
	return true
}
