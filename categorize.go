package repomap

import "strings"

func summarizeSymbols(f RankedFile) []symbolGroup {
	categorized := categorizeByKind(f.Path, f.Symbols)

	var groups []symbolGroup
	for _, item := range categoryOrder {
		syms := categorized[item.key]
		if len(syms) == 0 {
			continue
		}
		groups = append(groups, symbolGroup{
			label:   item.label,
			summary: summarizeGroup(item.key, syms),
			count:   len(syms),
		})
	}
	return groups
}

func symbolCategory(path string, s Symbol) string {
	if isTestSymbol(path, s) {
		return "tests"
	}

	switch s.Kind {
	case "struct", "type":
		return "types"
	case "interface":
		return "interfaces"
	case "class":
		return "classes"
	case "enum":
		return "enums"
	case "function", "fn":
		return "funcs"
	case "method":
		return "methods"
	case "constant", "const":
		return "consts"
	case "variable", "static":
		return "vars"
	default:
		return "other"
	}
}

// categorizeByKind groups symbols by their category key.
func categorizeByKind(path string, syms []Symbol) map[string][]Symbol {
	m := make(map[string][]Symbol)
	for _, s := range syms {
		cat := symbolCategory(path, s)
		m[cat] = append(m[cat], s)
	}
	return m
}

func isTestSymbol(path string, s Symbol) bool {
	if !strings.HasSuffix(path, "_test.go") {
		return false
	}
	return strings.HasPrefix(s.Name, "Test") || strings.HasPrefix(s.Name, "Benchmark") || strings.HasPrefix(s.Name, "Fuzz")
}

func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}
