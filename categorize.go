package repomap

import (
	"path/filepath"
	"strings"
)

// kindToCategory maps symbol kinds to their display category.
var kindToCategory = map[string]string{
	"struct":    "types",
	"type":      "types",
	"interface": "interfaces",
	"class":     "classes",
	"enum":      "enums",
	"function":  "funcs",
	"fn":        "funcs",
	"method":    "methods",
	"constant":  "consts",
	"const":     "consts",
	"variable":  "vars",
	"static":    "vars",
}

// categoryOrder defines the display order for symbol categories.
var categoryOrder = []struct {
	key   string
	label string
}{
	{"tests", "tests"},
	{"types", "types"},
	{"interfaces", "interfaces"},
	{"classes", "classes"},
	{"enums", "enums"},
	{"funcs", "funcs"},
	{"methods", "methods"},
	{"consts", "consts"},
	{"vars", "vars"},
	{"other", "other"},
}

// categorizedGroup holds a category key, display label, and its symbols.
type categorizedGroup struct {
	key   string
	label string
	syms  []Symbol
}

// orderedGroups returns symbols grouped by category in display order,
// skipping empty categories.
func orderedGroups(path string, syms []Symbol) []categorizedGroup {
	categorized := categorizeByKind(path, syms)
	var groups []categorizedGroup
	for _, item := range categoryOrder {
		s := categorized[item.key]
		if len(s) == 0 {
			continue
		}
		groups = append(groups, categorizedGroup{key: item.key, label: item.label, syms: s})
	}
	return groups
}

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

	if cat, ok := kindToCategory[s.Kind]; ok {
		return cat
	}
	return "other"
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
	ext := filepath.Ext(path)
	base := filepath.Base(path)
	switch ext {
	case ".go":
		return strings.HasSuffix(path, "_test.go")
	case ".py":
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
	case ".rs":
		return strings.Contains(path, "/tests/") || strings.HasSuffix(base, "_test.rs")
	case ".java":
		return strings.HasSuffix(base, "Test.java") || strings.HasSuffix(base, "Tests.java")
	case ".ts", ".tsx", ".js", ".jsx":
		return strings.HasSuffix(base, ".test"+ext) || strings.HasSuffix(base, ".spec"+ext)
	case ".rb":
		return strings.HasSuffix(base, "_test.rb") || strings.HasSuffix(base, "_spec.rb")
	case ".php":
		return strings.HasSuffix(base, "Test.php")
	default:
		return false
	}
}
