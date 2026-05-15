package repomap

import "sort"

// ApplyConsumedBonus adjusts scores for files the caller has already read.
//
// Consumed files are downranked (score halved) since the caller already has
// their content. Files that import consumed files are upranked (+15 per
// consumed dependency, capped at +45) because they are likely next files the
// caller will need to understand.
//
// Follows the same pattern as ApplyCallerBonus: mutates scores in-place,
// re-sorts by score descending / path ascending for ties, returns nothing.
// No-op when consumedPaths is empty.
func ApplyConsumedBonus(ranked []RankedFile, consumedPaths map[string]bool) {
	if len(consumedPaths) == 0 {
		return
	}

	// Detect Go vs non-Go project: Go uses import-path matching,
	// non-Go falls back to basename matching.
	isGo := false
	for i := range ranked {
		if ranked[i].ImportPath != "" {
			isGo = true
			break
		}
	}

	var keyOf func(rf *RankedFile) string
	var matchKey func(imp string) string

	if isGo {
		keyOf = func(rf *RankedFile) string { return rf.ImportPath }
		matchKey = func(imp string) string { return imp }
	} else {
		keyOf = func(rf *RankedFile) string { return basenameWithoutExt(rf.Path) }
		matchKey = basenameWithoutExt
	}

	// Build a path→key index for all ranked files so we can match consumed
	// paths to their key representation.
	pathToKey := make(map[string]string, len(ranked))
	for i := range ranked {
		if k := keyOf(&ranked[i]); k != "" {
			pathToKey[ranked[i].Path] = k
		}
	}

	// Collect consumed keys that actually exist in ranked files.
	activeConsumedKeys := make(map[string]bool)
	for p := range consumedPaths {
		if k, ok := pathToKey[p]; ok {
			activeConsumedKeys[k] = true
		}
	}

	// Downrank consumed files: score / 2 (integer division).
	for i := range ranked {
		if consumedPaths[ranked[i].Path] {
			old := ranked[i].Score
			addScoreComponent(&ranked[i], scoreComponentConsumed, old/2-old)
		}
	}

	// Uprank importers of consumed files: +15 per consumed dependency, capped at +45.
	const bonusPerDep = 15
	const maxBonus = 45

	for i := range ranked {
		bonus := 0
		for _, imp := range ranked[i].Imports {
			k := matchKey(imp)
			if activeConsumedKeys[k] {
				bonus += bonusPerDep
			}
		}
		if bonus > maxBonus {
			bonus = maxBonus
		}
		addScoreComponent(&ranked[i], scoreComponentConsumed, bonus)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Path < ranked[j].Path
	})
}
