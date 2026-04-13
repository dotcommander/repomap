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
	scanLines(lines, func(e lineEntry) bool {
		if e.trimmed == "<?php" || e.trimmed == "?>" || e.trimmed == "<?" {
			return true
		}
		if strings.HasPrefix(e.trimmed, "//") || strings.HasPrefix(e.trimmed, "#") {
			return true
		}

		if m := phpNamespace.FindStringSubmatch(e.trimmed); m != nil {
			fs.Package = strings.TrimSpace(m[1])
			return true
		}
		if tryAppendImport(phpUse, e, fs) {
			return true
		}
		if m := phpFunction.FindStringSubmatch(e.trimmed); m != nil {
			if strings.HasPrefix(m[1], "__") {
				return true
			}
			kind := "function"
			if len(e.line) > len(e.trimmed) {
				kind = "method"
			}
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: kind, Exported: true, Line: e.idx + 1})
			return true
		}
		if tryAppendSymbol(phpClass, e, "class", true, fs) ||
			tryAppendSymbol(phpInterface, e, "interface", true, fs) ||
			tryAppendSymbol(phpTrait, e, "trait", true, fs) ||
			tryAppendSymbol(phpEnum, e, "enum", true, fs) ||
			tryAppendSymbol(phpConst, e, "constant", true, fs) {
			return true
		}
		return true
	})
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
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: kind, Exported: true, Line: lineIdx + 1})
		}
	}
}
