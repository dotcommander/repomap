package repomap

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// ImpactResult is the factual blast-radius summary for one file.
type ImpactResult struct {
	File            StructuredFile `json:"file"`
	Imports         []string       `json:"imports,omitempty"`
	ImportedBy      []string       `json:"imported_by,omitempty"`
	Tests           []string       `json:"tests,omitempty"`
	ExportedSymbols []Symbol       `json:"exported_symbols,omitempty"`
	Boundaries      []string       `json:"boundaries,omitempty"`
	ScoreComponents map[string]int `json:"score_components,omitempty"`
	ParseMethod     string         `json:"parse_method,omitempty"`
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
	return ImpactResult{
		File:            structuredFile(target, ""),
		Imports:         append([]string(nil), target.Imports...),
		ImportedBy:      impactImporters(target, ranked),
		Tests:           impactTests(target, ranked),
		ExportedSymbols: exportedSymbols(target.Symbols),
		Boundaries:      append([]string(nil), target.Boundaries...),
		ScoreComponents: cloneScoreComponents(target.ScoreComponents),
		ParseMethod:     target.ParseMethod,
	}, nil
}

func impactImporters(target RankedFile, ranked []RankedFile) []string {
	targetKey, matchKey := impactKeys(target, ranked)
	if targetKey == "" {
		return nil
	}
	importers := make([]string, 0)
	for _, f := range ranked {
		if f.Path == target.Path {
			continue
		}
		for _, imp := range f.Imports {
			if matchKey(imp) == targetKey {
				importers = append(importers, f.Path)
				break
			}
		}
	}
	slices.Sort(importers)
	return importers
}

func impactKeys(target RankedFile, ranked []RankedFile) (targetKey string, matchKey func(string) string) {
	isGo := false
	for _, f := range ranked {
		if f.ImportPath != "" {
			isGo = true
			break
		}
	}
	if isGo {
		return target.ImportPath, func(imp string) string { return imp }
	}
	return basenameWithoutExt(target.Path), basenameWithoutExt
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
