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
	inBlockComment := false

	for lineIdx, line := range lines {
		trimmed := strings.TrimSpace(line)

		inBlockComment = trackBlockComment(trimmed, inBlockComment)
		if inBlockComment {
			continue
		}

		if m := rustPubAsync.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "fn", Line: lineIdx + 1})
			continue
		}
		if m := rustPubItem.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[2], Kind: m[1], Line: lineIdx + 1})
			continue
		}
		if m := rustImpl.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "impl", Line: lineIdx + 1})
			continue
		}
		if m := rustUse.FindStringSubmatch(trimmed); m != nil {
			fs.Imports = append(fs.Imports, strings.TrimSpace(m[1]))
		}
	}
}

// parseC processes C/C++ lines.
func parseC(lines []string, fs *FileSymbols) {
	inBlockComment := false

	for lineIdx, line := range lines {
		trimmed := strings.TrimSpace(line)

		inBlockComment = trackBlockComment(trimmed, inBlockComment)
		if inBlockComment {
			continue
		}

		// Skip preprocessor directives other than #include
		if strings.HasPrefix(trimmed, "#") {
			if m := cInclude.FindStringSubmatch(trimmed); m != nil {
				fs.Imports = append(fs.Imports, m[1])
			}
			continue
		}

		if m := cTagDecl.FindStringSubmatch(trimmed); m != nil {
			kind := strings.Fields(trimmed)[0] // struct / class / enum / typedef
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: kind, Line: lineIdx + 1})
			continue
		}

		// Function declarations: must start at column 0 (no leading whitespace)
		// and contain a '('.
		if line == trimmed && strings.Contains(line, "(") {
			if m := cFunc.FindStringSubmatch(trimmed); m != nil {
				fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "function", Line: lineIdx + 1})
			}
		}
	}
}

// parseJava processes Java lines.
func parseJava(lines []string, fs *FileSymbols) {
	inBlockComment := false

	for lineIdx, line := range lines {
		trimmed := strings.TrimSpace(line)

		inBlockComment = trackBlockComment(trimmed, inBlockComment)
		if inBlockComment {
			continue
		}

		if m := javaImport.FindStringSubmatch(trimmed); m != nil {
			fs.Imports = append(fs.Imports, strings.TrimSpace(m[1]))
			continue
		}
		if m := javaTypeDecl.FindStringSubmatch(trimmed); m != nil {
			// Determine the kind from the keyword preceding the name
			kind := "class"
			for _, kw := range []string{"interface", "enum", "record"} {
				if strings.Contains(trimmed, kw) {
					kind = kw
					break
				}
			}
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: kind, Line: lineIdx + 1})
			continue
		}
		if m := javaMethodDecl.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "method", Line: lineIdx + 1})
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
