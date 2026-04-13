package repomap

import (
	"path/filepath"
	"slices"
	"strings"
)

// RankedFile is a FileSymbols with an importance score.
type RankedFile struct {
	FileSymbols
	Score       int    // higher = more important
	Tag         string // e.g. "entry", ""
	DetailLevel int    // set by BudgetFiles: -1=omit, 0=header, 1=summary, 2=symbols, 3=symbols+fields
	ImportedBy  int    // number of files that import this file's package
}

// RankFiles scores and sorts files by importance.
// Returns files sorted by score descending, then by path ascending for ties.
func RankFiles(files []*FileSymbols) []RankedFile {
	ranked := make([]RankedFile, len(files))
	for i, f := range files {
		ranked[i] = RankedFile{FileSymbols: *f}
	}

	applyEntryBoosts(ranked)
	applySymbolBonus(ranked)
	applyDepthPenalty(ranked)
	applyReferenceCounts(ranked, files)

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

func applySymbolBonus(ranked []RankedFile) {
	for i := range ranked {
		for _, sym := range ranked[i].Symbols {
			if sym.Exported {
				ranked[i].Score++
			}
		}
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

// applyGoReferenceCounts scores Go files by how many other files import their package.
func applyGoReferenceCounts(ranked []RankedFile, files []*FileSymbols) {
	// Map importPath → all file indices sharing that package.
	importIndex := make(map[string][]int, len(files))
	for i, f := range files {
		if f.ImportPath != "" {
			importIndex[f.ImportPath] = append(importIndex[f.ImportPath], i)
		}
	}

	// Count unique importers per package.
	importerCount := make(map[string]int, len(importIndex))
	for _, f := range files {
		seen := make(map[string]bool)
		for _, imp := range f.Imports {
			if _, ok := importIndex[imp]; ok && !seen[imp] {
				seen[imp] = true
				importerCount[imp]++
			}
		}
	}

	// Distribute score and ImportedBy to all files in each imported package.
	for pkg, count := range importerCount {
		for _, idx := range importIndex[pkg] {
			ranked[idx].Score += count * 10
			ranked[idx].ImportedBy = count
		}
	}
}

// applyBasenameReferenceCounts scores non-Go files by basename matching in imports.
func applyBasenameReferenceCounts(ranked []RankedFile, files []*FileSymbols) {
	// Map basename (without ext) → all file indices sharing that name.
	basenameIndex := make(map[string][]int, len(files))
	for i, f := range files {
		name := strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
		basenameIndex[name] = append(basenameIndex[name], i)
	}

	// Count unique importers per basename.
	importerCount := make(map[string]int, len(basenameIndex))
	for _, f := range files {
		seen := make(map[string]bool)
		for _, imp := range f.Imports {
			seg := filepath.Base(imp)
			seg = strings.TrimSuffix(seg, filepath.Ext(seg))
			if _, ok := basenameIndex[seg]; ok && !seen[seg] {
				seen[seg] = true
				importerCount[seg]++
			}
		}
	}

	// Distribute score and ImportedBy to all files matching each basename.
	for name, count := range importerCount {
		for _, idx := range basenameIndex[name] {
			ranked[idx].Score += count * 10
			ranked[idx].ImportedBy = count
		}
	}
}
