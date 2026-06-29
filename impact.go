package repomap

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// ImpactResult is the factual blast-radius summary for one file.
type ImpactResult struct {
	File               StructuredFile `json:"file"`
	Imports            []string       `json:"imports,omitempty"`
	ImportedBy         []string       `json:"imported_by,omitempty"`
	Tests              []string       `json:"tests,omitempty"`
	ExportedSymbols    []Symbol       `json:"exported_symbols,omitempty"`
	Boundaries         []string       `json:"boundaries,omitempty"`
	ScoreComponents    map[string]int `json:"score_components,omitempty"`
	ParseMethod        string         `json:"parse_method,omitempty"`
	RiskLevel          string         `json:"risk_level,omitempty"`
	AffectedPackages   []string       `json:"affected_packages,omitempty"`
	CheckNext          []string       `json:"check_next,omitempty"`
	LikelyTestCommands []string       `json:"likely_test_commands,omitempty"`
	ReadNext           []ReadNextItem `json:"read_next,omitempty"`
	OmittedReason      string         `json:"omitted_reason,omitempty"`
}

// Impact returns a deterministic local blast-radius summary for relPath.
func (m *Map) Impact(relPath string) (ImpactResult, error) {
	m.mu.RLock()
	ranked := cloneRanked(m.ranked)
	m.mu.RUnlock()

	relPath = filepath.ToSlash(filepath.Clean(relPath))
	idx := -1
	for i := range ranked {
		if filepath.ToSlash(ranked[i].Path) == relPath {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ImpactResult{}, fmt.Errorf("file %q not found in repomap", relPath)
	}

	target := ranked[idx]
	importers := impactImporters(target, ranked)
	tests := impactTests(target, ranked)
	return ImpactResult{
		File:               structuredFile(target, ""),
		Imports:            append([]string(nil), target.Imports...),
		ImportedBy:         importers,
		Tests:              tests,
		ExportedSymbols:    exportedSymbols(target.Symbols),
		Boundaries:         append([]string(nil), target.Boundaries...),
		ScoreComponents:    cloneScoreComponents(target.ScoreComponents),
		ParseMethod:        target.ParseMethod,
		RiskLevel:          impactRiskLevel(target, importers, tests),
		AffectedPackages:   affectedPackages(target, importers, ranked),
		CheckNext:          impactCheckNext(importers, tests),
		LikelyTestCommands: likelyTestCommands(target.Language, tests),
		ReadNext:           impactReadNext(target, importers, tests, ranked),
		OmittedReason:      impactOmittedReason(importers, tests),
	}, nil
}

func impactImporters(target RankedFile, ranked []RankedFile) []string {
	targetKey, matches := impactKeys(target, ranked)
	if targetKey == "" {
		return nil
	}
	importers := make([]string, 0)
	for _, f := range ranked {
		if f.Path == target.Path {
			continue
		}
		for _, imp := range f.Imports {
			if matches(f.Path, imp) {
				importers = append(importers, f.Path)
				break
			}
		}
	}
	slices.Sort(importers)
	return importers
}

func impactKeys(target RankedFile, ranked []RankedFile) (targetKey string, matches func(importerPath, imp string) bool) {
	isGo := false
	for _, f := range ranked {
		if f.ImportPath != "" {
			isGo = true
			break
		}
	}
	if isGo {
		tk := target.ImportPath
		return tk, func(_ string, imp string) bool { return imp == tk }
	}
	tk := pathKey(target.Path)
	keySet := make(map[string]struct{}, len(ranked))
	for _, f := range ranked {
		if k := pathKey(f.Path); k != "" {
			keySet[k] = struct{}{}
		}
	}
	return tk, func(importerPath, imp string) bool {
		for _, k := range nonGoImportKeys(importerPath, imp, keySet) {
			if k == tk {
				return true
			}
		}
		return false
	}
}

func impactTests(target RankedFile, ranked []RankedFile) []string {
	targetDir := filepath.Dir(target.Path)
	targetBase := strings.TrimSuffix(filepath.Base(target.Path), filepath.Ext(target.Path))
	targetStem := strings.TrimSuffix(target.Path, filepath.Ext(target.Path))
	tests := make([]string, 0)
	for _, f := range ranked {
		if !isTestFile(f.Path) {
			continue
		}
		testBase := strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
		if strings.HasPrefix(f.Path, targetStem) || (filepath.Dir(f.Path) == targetDir && strings.Contains(testBase, targetBase)) {
			tests = append(tests, f.Path)
		}
	}
	slices.Sort(tests)
	return tests
}

func exportedSymbols(symbols []Symbol) []Symbol {
	out := make([]Symbol, 0, len(symbols))
	for _, s := range symbols {
		if s.Exported {
			out = append(out, s)
		}
	}
	return out
}

func impactRiskLevel(target RankedFile, importers, tests []string) string {
	switch {
	case len(importers) > 10 || len(target.Boundaries) > 0 && len(importers) > 3:
		return "high"
	case len(importers) > 3 || len(target.Boundaries) > 0 || len(tests) == 0 && len(exportedSymbols(target.Symbols)) > 0:
		return "medium"
	default:
		return "low"
	}
}

func affectedPackages(target RankedFile, importers []string, ranked []RankedFile) []string {
	packages := map[string]struct{}{}
	if target.Package != "" {
		packages[target.Package] = struct{}{}
	}
	for _, importer := range importers {
		if rf, ok := rankedFileByPath(ranked, importer); ok && rf.Package != "" {
			packages[rf.Package] = struct{}{}
		}
	}
	out := make([]string, 0, len(packages))
	for pkg := range packages {
		out = append(out, pkg)
	}
	slices.Sort(out)
	return out
}

func impactCheckNext(importers, tests []string) []string {
	var out []string
	for _, file := range importers {
		out = append(out, "inspect importer "+file)
		if len(out) >= 3 {
			break
		}
	}
	for _, file := range tests {
		out = append(out, "run or inspect likely test "+file)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func likelyTestCommands(language string, tests []string) []string {
	if len(tests) == 0 || language != "go" {
		return nil
	}
	dirs := map[string]struct{}{}
	for _, test := range tests {
		dir := filepath.ToSlash(filepath.Dir(test))
		if dir == "." {
			dirs["."] = struct{}{}
			continue
		}
		dirs["./"+dir] = struct{}{}
	}
	out := make([]string, 0, len(dirs))
	for dir := range dirs {
		out = append(out, "go test "+dir)
	}
	slices.Sort(out)
	return out
}

func impactReadNext(target RankedFile, importers, tests []string, ranked []RankedFile) []ReadNextItem {
	var items []ReadNextItem
	if len(target.Symbols) > 0 {
		for _, sym := range target.Symbols {
			if sym.Exported && sym.Line > 0 {
				end := sym.EndLine
				if end < sym.Line {
					end = sym.Line
				}
				items = append(items, readNextRange(target.Path, sym.Line, end, "inspect exported symbol "+sym.Name))
				break
			}
		}
	}
	if len(items) == 0 {
		items = append(items, readNextAround(target.Path, 1, "inspect target file"))
	}
	for _, file := range importers {
		items = append(items, readNextAround(file, firstSymbolLine(ranked, file), "inspect importer before changing target"))
		if len(items) >= 3 {
			break
		}
	}
	for _, file := range tests {
		items = append(items, readNextAround(file, firstSymbolLine(ranked, file), "inspect likely test coverage"))
		if len(items) >= 5 {
			break
		}
	}
	out, _ := dedupeReadNext(items, 5)
	return out
}

func impactOmittedReason(importers, tests []string) string {
	if len(importers) > 3 || len(tests) > 2 {
		return "check_next and read_next are capped to keep impact output bounded"
	}
	return ""
}

func rankedFileByPath(ranked []RankedFile, path string) (RankedFile, bool) {
	for _, rf := range ranked {
		if filepath.ToSlash(rf.Path) == filepath.ToSlash(path) {
			return rf, true
		}
	}
	return RankedFile{}, false
}

func firstSymbolLine(ranked []RankedFile, path string) int {
	rf, ok := rankedFileByPath(ranked, path)
	if !ok {
		return 1
	}
	for _, sym := range rf.Symbols {
		if sym.Line > 0 {
			return sym.Line
		}
	}
	return 1
}
