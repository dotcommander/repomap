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
	var matchKeys func(importerPath, imp string, keys map[string]struct{}) []string

	if isGo {
		keyOf = func(rf *RankedFile) string { return rf.ImportPath }
		matchKeys = func(_ string, imp string, keys map[string]struct{}) []string {
			if _, ok := keys[imp]; ok {
				return []string{imp}
			}
			return nil
		}
	} else {
		keyOf = func(rf *RankedFile) string { return pathKey(rf.Path) }
		matchKeys = nonGoImportKeys
	}

	// Build a path→key index plus a key-set for all ranked files so we can
	// match consumed paths to their key representation and resolve imports.
	pathToKey := make(map[string]string, len(ranked))
	keySet := make(map[string]struct{}, len(ranked))
	for i := range ranked {
		if k := keyOf(&ranked[i]); k != "" {
			pathToKey[ranked[i].Path] = k
			keySet[k] = struct{}{}
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
		seen := make(map[string]bool)
		for _, imp := range ranked[i].Imports {
			for _, k := range matchKeys(ranked[i].Path, imp, keySet) {
				if activeConsumedKeys[k] && !seen[k] {
					seen[k] = true
					bonus += bonusPerDep
				}
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
