package repomap

import (
	"path/filepath"
	"sort"
)

// ApplyIntraPackageRefs adds a cheap, AST/lexical intra-package usage signal by
// counting how many other files reference each exported symbol. Unlike
// ApplySymbolReferenceBonus it INCLUDES Go files, since intra-package coupling
// is the strongest within-package importance signal when the import graph is
// flat (every file in a Go package shares one import path).
//
// It is lexical by design — no gopls — and capped the same way as the
// cross-language symbol-reference bonus.
func ApplyIntraPackageRefs(root string, ranked []RankedFile) {
	if root == "" || len(ranked) == 0 {
		return
	}

	type target struct {
		file string
		name string
	}

	byName := make(map[string][]target)
	for i := range ranked {
		rf := ranked[i]
		for _, sym := range rf.Symbols {
			if !sym.Exported || !symbolRefNameOK(sym.Name) {
				continue
			}
			byName[sym.Name] = append(byName[sym.Name], target{file: rf.Path, name: sym.Name})
		}
	}
	if len(byName) == 0 {
		return
	}

	refFiles := make(map[string]map[string]struct{})
	for _, rf := range ranked {
		words, ok := fileIdentifierSet(filepath.Join(root, rf.Path))
		if !ok {
			continue
		}
		for word := range words {
			for _, tgt := range byName[word] {
				if tgt.file == rf.Path {
					continue
				}
				key := tgt.file + "\x00" + tgt.name
				if refFiles[key] == nil {
					refFiles[key] = make(map[string]struct{})
				}
				refFiles[key][rf.Path] = struct{}{}
			}
		}
	}

	fileRefs := make(map[string]int)
	for key, callers := range refFiles {
		idx := indexNul(key)
		if idx < 0 {
			continue
		}
		fileRefs[key[:idx]] += len(callers)
	}

	for i := range ranked {
		count := fileRefs[ranked[i].Path]
		if count == 0 {
			continue
		}
		bonus := count * symbolRefsBonusPerRef
		if bonus > symbolRefsMaxBonus {
			bonus = symbolRefsMaxBonus
		}
		addScoreComponent(&ranked[i], scoreComponentIntraRefs, bonus)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Path < ranked[j].Path
	})
}

// applyTestDemotion down-ranks test files in the default ranking so impl files
// orient the reader first. Gated by includeTests: when true it is a no-op, so
// callers can opt test files back to full weight. Demotion lives here (one
// source of truth) — applyDepthPenalty no longer touches test files.
//
// Re-sorts by score descending / path ascending for ties, following
// ApplyConsumedBonus.
func applyTestDemotion(ranked []RankedFile, includeTests bool) {
	if includeTests {
		return
	}

	for i := range ranked {
		if isTestFile(ranked[i].Path) {
			addScoreComponent(&ranked[i], scoreComponentTestDemote, -40)
		}
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Path < ranked[j].Path
	})
}

// indexNul returns the index of the first NUL byte in s, or -1 if absent.
func indexNul(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\x00' {
			return i
		}
	}
	return -1
}

// dtoExportedKinds are the symbol kinds that count toward a file being DTO-only:
// pure data/type declarations with no exported behavior.
var dtoExportedKinds = map[string]struct{}{
	"type":      {},
	"struct":    {},
	"interface": {},
	"enum":      {},
	"class":     {},
}

// dtoMinDataCluster is the minimum number of exported data-kind symbols a
// MIXED file (data + behaviour) must have before the DTO penalty applies.
// Pure data-only files (zero exported funcs/methods) are penalised regardless
// of count — see applyDTOPenalty.
const dtoMinDataCluster = 4

// applyDTOPenalty down-weights files whose exported surface is data-only.
// A file is "DTO-only" when it has ≥dtoMinDataCluster exported data-kind
// symbols, ≥80% of its exported symbols are type/struct/interface/enum/class,
// and it exports zero functions or methods. Such files define shapes but no
// behavior and rarely orient a reader within a package.
func applyDTOPenalty(ranked []RankedFile) {
	for i := range ranked {
		exported := 0
		dtoKinds := 0
		hasFunc := false
		for _, sym := range ranked[i].Symbols {
			if !sym.Exported {
				continue
			}
			exported++
			if _, ok := dtoExportedKinds[sym.Kind]; ok {
				dtoKinds++
			}
			switch sym.Kind {
			case "function", "fn", "method":
				hasFunc = true
			}
		}
		if exported == 0 || hasFunc {
			continue
		}
		// A file that is 100% data-kind with zero exported funcs/methods defines
		// no behaviour — penalise it regardless of count (catches lone-type
		// barrels). Otherwise require a data-only cluster (>=dtoMinDataCluster,
		// >=80% data) before penalising.
		pureData := dtoKinds == exported
		if !pureData && dtoKinds < dtoMinDataCluster {
			continue
		}
		if pureData || float64(dtoKinds) >= 0.80*float64(exported) {
			addScoreComponent(&ranked[i], scoreComponentDTO, -12)
		}
	}
}
