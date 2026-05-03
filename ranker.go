package repomap

import (
	"path/filepath"
	"slices"
	"strings"
)

// RankedFile is a FileSymbols with an importance score.
type RankedFile struct {
	*FileSymbols
	Score       int      // higher = more important
	Tag         string   // e.g. "entry", ""
	DetailLevel int      // set by BudgetFiles: -1=omit, 0=header, 1=summary, 2=symbols, 3=symbols+fields
	ImportedBy  int      // number of files that import this file's package
	DependsOn   int      // number of internal imports (fan-out coupling proxy)
	Untested    bool     // true if package lacks test coverage
	Boundaries  []string `json:"boundaries,omitempty"` // semantic boundary labels, e.g. ["HTTP", "Postgres"]
}

// RankFiles scores and sorts files by importance.
// Returns files sorted by score descending, then by path ascending for ties.
func RankFiles(files []*FileSymbols) []RankedFile {
	ranked := make([]RankedFile, len(files))
	for i, f := range files {
		ranked[i] = RankedFile{FileSymbols: f}
	}

	applyEntryBoosts(ranked)
	applySymbolBonus(ranked)
	applyDepthPenalty(ranked)
	applyReferenceCounts(ranked, files)
	applyDiagnosticSignals(ranked, files)
	applyBoundaryBoost(ranked)
	markDeadExports(ranked)

	slices.SortFunc(ranked, func(a, b RankedFile) int {
		if b.Score != a.Score {
			return b.Score - a.Score
		}
		return strings.Compare(a.Path, b.Path)
	})

	return ranked
}

func applyEntryBoosts(ranked []RankedFile) {
	for i := range ranked {
		path := ranked[i].Path
		base := filepath.Base(path)

		// Go entry points
		if base == "main.go" {
			ranked[i].Score += 50
			ranked[i].Tag = "entry"
			continue
		}

		// Other language entry points
		switch base {
		case "main.ts", "index.ts", "index.js", "app.py", "main.py", "main.rs":
			ranked[i].Score += 30
			ranked[i].Tag = "entry"
		}
	}
}

// kindWeight assigns ranking weight by symbol kind.
// Types/interfaces define contracts (highest), structs define data,
// functions/methods implement behavior, constants/vars are low signal.
var kindWeight = map[string]int{
	"interface": 3,
	"type":      3,
	"struct":    2,
	"class":     2,
	"enum":      2,
	"function":  1,
	"fn":        1,
	"method":    1,
	"constant":  0,
	"const":     0,
	"variable":  0,
	"static":    0,
}

const maxSymbolBonus = 20

func applySymbolBonus(ranked []RankedFile) {
	for i := range ranked {
		bonus := 0
		for _, sym := range ranked[i].Symbols {
			if sym.Exported {
				w := kindWeight[sym.Kind]
				if w == 0 {
					// Unknown kinds get +1 (original behavior).
					w = 1
				}
				bonus += w
			}
		}
		if bonus > maxSymbolBonus {
			bonus = maxSymbolBonus
		}
		ranked[i].Score += bonus
	}
}

func applyDepthPenalty(ranked []RankedFile) {
	for i := range ranked {
		depth := strings.Count(ranked[i].Path, string(filepath.Separator))
		if depth > 2 {
			ranked[i].Score -= depth - 2
		}
		if isTestFile(ranked[i].Path) {
			ranked[i].Score -= 5
		}
	}
}

func applyReferenceCounts(ranked []RankedFile, files []*FileSymbols) {
	if len(files) == 0 {
		return
	}

	// Use Go import-path matching for Go projects; fall back to basename
	// matching for other languages. Check all files, not just the first,
	// since parseFiles appends Go results before non-Go results but callers
	// should not depend on that ordering.
	for _, f := range files {
		if f.Language == "go" {
			applyGoReferenceCounts(ranked, files)
			return
		}
	}
	applyBasenameReferenceCounts(ranked, files)
}

// distributeImportScores builds a key→indices map, counts unique importers per key,
// then distributes score and ImportedBy to matching files.
func distributeImportScores(ranked []RankedFile, files []*FileSymbols,
	keyFunc func(*FileSymbols) string,
	matchFunc func(string) string,
) {
	// Build index: key → file indices.
	index := make(map[string][]int, len(files))
	for i, f := range files {
		if k := keyFunc(f); k != "" {
			index[k] = append(index[k], i)
		}
	}

	// Count unique importers per key.
	importerCount := make(map[string]int, len(index))
	for _, f := range files {
		seen := make(map[string]bool)
		for _, imp := range f.Imports {
			k := matchFunc(imp)
			if _, ok := index[k]; ok && !seen[k] {
				seen[k] = true
				importerCount[k]++
			}
		}
	}

	// Distribute score and ImportedBy to all files matching each key.
	for key, count := range importerCount {
		for _, idx := range index[key] {
			ranked[idx].Score += count * 10
			ranked[idx].ImportedBy = count
		}
	}
}

// applyGoReferenceCounts scores Go files by how many other files import their package.
func applyGoReferenceCounts(ranked []RankedFile, files []*FileSymbols) {
	distributeImportScores(ranked, files,
		func(f *FileSymbols) string { return f.ImportPath },
		func(imp string) string { return imp },
	)
}

// applyBasenameReferenceCounts scores non-Go files by basename matching in imports.
func applyBasenameReferenceCounts(ranked []RankedFile, files []*FileSymbols) {
	basenameOf := func(s string) string {
		seg := filepath.Base(s)
		return strings.TrimSuffix(seg, filepath.Ext(seg))
	}
	distributeImportScores(ranked, files,
		func(f *FileSymbols) string { return basenameOf(f.Path) },
		basenameOf,
	)
}

// applyDiagnosticSignals populates DependsOn and Untested on each RankedFile.
func applyDiagnosticSignals(ranked []RankedFile, files []*FileSymbols) {
	internalPkgs := buildInternalPackageSet(files)
	testCoverage := buildTestCoverageMap(files)

	for i := range ranked {
		ranked[i].DependsOn = countInternalImports(ranked[i], internalPkgs)
		if shouldTagUntested(ranked[i], testCoverage) {
			ranked[i].Untested = true
		}
	}
}

// buildInternalPackageSet returns the set of all Go import paths present in the project.
func buildInternalPackageSet(files []*FileSymbols) map[string]bool {
	internalPkgs := make(map[string]bool)
	for _, f := range files {
		if f.ImportPath != "" {
			internalPkgs[f.ImportPath] = true
		}
	}
	return internalPkgs
}

// countInternalImports returns the number of distinct internal imports for a file.
// For Go, only imports matching a known internal package are counted.
// For non-Go, total distinct imports are used if above a noise threshold.
func countInternalImports(f RankedFile, internalPkgs map[string]bool) int {
	if len(f.Imports) == 0 {
		return 0
	}

	// Go: count only imports that match known internal packages.
	if f.Language == "go" && len(internalPkgs) > 0 {
		seen := make(map[string]bool, len(f.Imports))
		count := 0
		for _, imp := range f.Imports {
			if internalPkgs[imp] && !seen[imp] {
				seen[imp] = true
				count++
			}
		}
		return count
	}

	// Non-Go fallback: total distinct imports, but only if above noise threshold.
	// Below 3, most imports are stdlib/framework — the signal is noise.
	const nonGoThreshold = 3
	seen := make(map[string]bool, len(f.Imports))
	for _, imp := range f.Imports {
		seen[imp] = true
	}
	if len(seen) < nonGoThreshold {
		return 0
	}
	return len(seen)
}

// diagnosticPackageKey returns the grouping key for test coverage detection.
// Uses Go import path when available, otherwise the file's directory.
func diagnosticPackageKey(f *FileSymbols) string {
	if f.ImportPath != "" {
		return f.ImportPath
	}
	return filepath.Dir(f.Path)
}

// buildTestCoverageMap returns a set of package keys that have test coverage.
// A package is covered if any test file in it contains test symbols.
func buildTestCoverageMap(files []*FileSymbols) map[string]bool {
	covered := make(map[string]bool)
	for _, f := range files {
		if !isTestFile(f.Path) {
			continue
		}
		if f.Language == "go" {
			for _, s := range f.Symbols {
				if isTestSymbol(f.Path, s) {
					covered[diagnosticPackageKey(f)] = true
					break
				}
			}
		} else if len(f.Symbols) > 0 {
			covered[diagnosticPackageKey(f)] = true
		}
	}
	return covered
}

// shouldTagUntested reports whether a file should be flagged as lacking test coverage.
func shouldTagUntested(f RankedFile, covered map[string]bool) bool {
	if isTestFile(f.Path) {
		return false
	}

	// Only flag files with exported symbols.
	hasExported := false
	for _, s := range f.Symbols {
		if s.Exported {
			hasExported = true
			break
		}
	}
	if !hasExported {
		return false
	}

	return !covered[diagnosticPackageKey(f.FileSymbols)]
}

// markDeadExports marks exported symbols as Dead when no other file in the
// scanned tree imports their package (ImportedBy == 0). Dead exports are still
// rendered but are deprioritised in budget allocation.
func markDeadExports(ranked []RankedFile) {
	for i := range ranked {
		if ranked[i].ImportedBy > 0 {
			continue
		}
		for j := range ranked[i].Symbols {
			if ranked[i].Symbols[j].Exported {
				ranked[i].Symbols[j].Dead = true
			}
		}
	}
}

// applyBoundaryBoost classifies each file's import list into boundary labels,
// stores them on the RankedFile, and adds a capped score bump.
func applyBoundaryBoost(ranked []RankedFile) {
	for i := range ranked {
		labels, bump := classifyBoundaries(ranked[i].Language, ranked[i].Imports)
		if len(labels) > 0 {
			ranked[i].Boundaries = labels
			ranked[i].Score += bump
		}
	}
}
