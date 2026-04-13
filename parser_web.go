package repomap

import (
	"regexp"
	"strings"
)

// --- Ruby patterns ---

var rubyDecl = regexp.MustCompile(`^(?:def|class|module)\s+(\w+)`)

// --- PHP patterns ---

var (
	phpClass     = regexp.MustCompile(`^(?:abstract\s+|final\s+)?class\s+(\w+)`)
	phpInterface = regexp.MustCompile(`^interface\s+(\w+)`)
	phpTrait     = regexp.MustCompile(`^trait\s+(\w+)`)
	phpEnum      = regexp.MustCompile(`^enum\s+(\w+)`)
	phpFunction  = regexp.MustCompile(`^(?:public\s+|protected\s+|private\s+)?(?:static\s+)?function\s+(\w+)`)
	phpConst     = regexp.MustCompile(`^(?:public\s+|protected\s+|private\s+)?const\s+(\w+)`)
	phpUse       = regexp.MustCompile(`^use\s+([^;{]+)`)
	phpNamespace = regexp.MustCompile(`^namespace\s+([^;]+)`)
)

// parsePHP processes PHP lines.
func parsePHP(lines []string, fs *FileSymbols) {
	inBlockComment := false

	for lineIdx, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "<?php" || trimmed == "?>" || trimmed == "<?" {
			continue
		}

		inBlockComment = trackBlockComment(trimmed, inBlockComment)
		if inBlockComment {
			continue
		}

		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if m := phpNamespace.FindStringSubmatch(trimmed); m != nil {
			fs.Package = strings.TrimSpace(m[1])
			continue
		}
		if m := phpUse.FindStringSubmatch(trimmed); m != nil {
			fs.Imports = append(fs.Imports, strings.TrimSpace(m[1]))
			continue
		}
		if m := phpClass.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "class", Exported: true, Line: lineIdx + 1})
			continue
		}
		if m := phpInterface.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "interface", Exported: true, Line: lineIdx + 1})
			continue
		}
		if m := phpTrait.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "trait", Exported: true, Line: lineIdx + 1})
			continue
		}
		if m := phpEnum.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "enum", Exported: true, Line: lineIdx + 1})
			continue
		}
		if m := phpFunction.FindStringSubmatch(trimmed); m != nil {
			// Skip magic methods and constructors
			if strings.HasPrefix(m[1], "__") {
				continue
			}
			kind := "function"
			// If indented (inside a class), treat as method
			if len(line) > len(trimmed) {
				kind = "method"
			}
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: kind, Exported: true, Line: lineIdx + 1})
			continue
		}
		if m := phpConst.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "constant", Exported: true, Line: lineIdx + 1})
			continue
		}
	}
}

// parseRuby processes Ruby lines.
func parseRuby(lines []string, fs *FileSymbols) {
	for lineIdx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := rubyDecl.FindStringSubmatch(trimmed); m != nil {
			kind := strings.Fields(trimmed)[0] // def / class / module
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: kind, Line: lineIdx + 1})
		}
	}
}
