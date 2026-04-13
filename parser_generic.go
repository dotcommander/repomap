package repomap

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxScanLines is the maximum number of lines scanned per file.
const maxScanLines = 500

// --- TypeScript / JavaScript patterns ---

var (
	tsExportDecl    = regexp.MustCompile(`export\s+(function|class|interface|type|const|enum)\s+(\w+)`)
	tsExportDefault = regexp.MustCompile(`export\s+default\s+(function|class)\s+(\w+)`)
	tsReExport      = regexp.MustCompile(`export\s+\{([^}]+)\}`)
	tsImportFrom    = regexp.MustCompile(`import\s+.*\s+from\s+['"]([^'"]+)['"]`)
	tsRequire       = regexp.MustCompile(`require\s*\(\s*['"]([^'"]+)['"]\s*\)`)
)

// --- Python patterns ---

var (
	pyFunc   = regexp.MustCompile(`^def\s+([A-Za-z]\w*)\s*\(`)
	pyClass  = regexp.MustCompile(`^class\s+(\w+)`)
	pyConst  = regexp.MustCompile(`^([A-Z][A-Z_0-9]+)\s*=`)
	pyImport = regexp.MustCompile(`^import\s+(\w+)`)
	pyFrom   = regexp.MustCompile(`^from\s+(\w+)`)
)

// ParseGenericFile extracts symbols from a non-Go source file using regex
// patterns. path is absolute, root is the project root for relative path
// calculation.
func ParseGenericFile(path, root, language string) (*FileSymbols, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}

	fs := &FileSymbols{
		Path:        rel,
		Language:    language,
		ParseMethod: "regex",
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > maxScanLines {
		lines = lines[:maxScanLines]
	}

	switch language {
	case "typescript", "javascript", "tsx", "jsx":
		parseTS(lines, fs)
	case "python":
		parsePython(lines, fs)
	case "rust":
		parseRust(lines, fs)
	case "c", "cpp", "c++", "cxx":
		parseC(lines, fs)
	case "java":
		parseJava(lines, fs)
	case "ruby":
		parseRuby(lines, fs)
	case "php":
		parsePHP(lines, fs)
		// swift, kotlin, lua, zig — unsupported, return empty
	}

	return fs, nil
}

// parseTS processes TypeScript/JavaScript lines.
func parseTS(lines []string, fs *FileSymbols) {
	for lineIdx, line := range lines {
		trimmed := strings.TrimSpace(line)

		if m := tsExportDecl.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[2], Kind: m[1], Line: lineIdx + 1})
			continue
		}
		if m := tsExportDefault.FindStringSubmatch(trimmed); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[2], Kind: m[1], Line: lineIdx + 1})
			continue
		}
		if m := tsReExport.FindStringSubmatch(trimmed); m != nil {
			for _, name := range splitReExportNames(m[1]) {
				fs.Symbols = append(fs.Symbols, Symbol{Name: name, Kind: "reexport", Line: lineIdx + 1})
			}
			continue
		}

		if m := tsImportFrom.FindStringSubmatch(trimmed); m != nil {
			fs.Imports = append(fs.Imports, m[1])
			continue
		}
		if m := tsRequire.FindStringSubmatch(trimmed); m != nil {
			fs.Imports = append(fs.Imports, m[1])
		}
	}
}

// splitReExportNames splits a re-export list like "Foo, Bar as Baz" into
// individual exported names.
func splitReExportNames(raw string) []string {
	parts := strings.Split(raw, ",")
	var names []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Handle "Foo as Bar" — take the local name (first word)
		fields := strings.Fields(p)
		if len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	return names
}

// parsePython processes Python lines, skipping triple-quoted docstrings.
func parsePython(lines []string, fs *FileSymbols) {
	inDocstring := false
	docQuote := ""

	for lineIdx, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track triple-quoted strings used as block comments / docstrings.
		if inDocstring {
			if strings.Contains(trimmed, docQuote) {
				inDocstring = false
			}
			continue
		}
		for _, q := range []string{`"""`, `'''`} {
			if strings.HasPrefix(trimmed, q) {
				rest := trimmed[len(q):]
				if !strings.Contains(rest, q) {
					inDocstring = true
					docQuote = q
				}
				break
			}
		}
		if inDocstring {
			continue
		}

		if m := pyFunc.FindStringSubmatch(line); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "function", Line: lineIdx + 1})
			continue
		}
		if m := pyClass.FindStringSubmatch(line); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "class", Line: lineIdx + 1})
			continue
		}
		if m := pyConst.FindStringSubmatch(line); m != nil {
			fs.Symbols = append(fs.Symbols, Symbol{Name: m[1], Kind: "const", Line: lineIdx + 1})
			continue
		}
		if m := pyImport.FindStringSubmatch(line); m != nil {
			fs.Imports = append(fs.Imports, m[1])
			continue
		}
		if m := pyFrom.FindStringSubmatch(line); m != nil {
			fs.Imports = append(fs.Imports, m[1])
		}
	}
}
