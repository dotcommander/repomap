package repomap

import (
	"path/filepath"
	"strings"
)

// basenameWithoutExt returns the filename component of path with its extension stripped.
// Used for non-Go basename-matching heuristics.
func basenameWithoutExt(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// applyTransitiveImportScores adds +5 per transitive dependent, capped at +50.
//
// "Transitive dependent" means: if A imports B and B imports C, then A (and B)
// are both transitive dependents of C. A deeply-imported file is more central.
//
// Uses Go import paths when available (ImportPath != ""), otherwise basename
// matching — mirrors the strategy in applyReferenceCounts.
func applyTransitiveImportScores(ranked []RankedFile) {
	if len(ranked) == 0 {
		return
	}

	// Detect Go vs non-Go project by checking whether any file has an ImportPath.
	isGo := false
	for i := range ranked {
		if ranked[i].ImportPath != "" {
			isGo = true
			break
		}
	}

	var keyOf func(rf *RankedFile) string
	// matchKeys maps (importerPath, import string, existing key-set) to the
	// internal keys that import targets — path-aware for non-Go relative
	// imports, exact for Go.
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

	// Build index: key → ranked-file index, plus a key-set for resolver lookups.
	keyIndex := make(map[string]int, len(ranked))
	keySet := make(map[string]struct{}, len(ranked))
	for i := range ranked {
		if k := keyOf(&ranked[i]); k != "" {
			keyIndex[k] = i
			keySet[k] = struct{}{}
		}
	}

	// Build reverse adjacency: key → set of keys that directly import it.
	// reverseDeps[k] = {keys that import k}
	reverseDeps := make(map[string]map[string]bool, len(ranked))
	for i := range ranked {
		srcKey := keyOf(&ranked[i])
		if srcKey == "" {
			continue
		}
		for _, imp := range ranked[i].Imports {
			for _, destKey := range matchKeys(ranked[i].Path, imp, keySet) {
				if _, ok := keyIndex[destKey]; !ok {
					continue // not an internal package
				}
				if reverseDeps[destKey] == nil {
					reverseDeps[destKey] = make(map[string]bool)
				}
				reverseDeps[destKey][srcKey] = true
			}
		}
	}

	// For each file, BFS upward (who depends on me, transitively).
	// Count unique transitive dependents, apply score.
	const scorePerDep = 5
	const maxScore = 50

	for i := range ranked {
		k := keyOf(&ranked[i])
		if k == "" {
			continue
		}
		count := transitiveDepCount(k, reverseDeps)
		if count == 0 {
			continue
		}
		score := count * scorePerDep
		if score > maxScore {
			score = maxScore
		}
		addScoreComponent(&ranked[i], scoreComponentTransitive, score)
	}
}

// transitiveDepCount returns the number of distinct transitive dependents of
// startKey via BFS over the reverse dependency graph. Cycle-safe via visited set.
func transitiveDepCount(startKey string, reverseDeps map[string]map[string]bool) int {
	visited := make(map[string]bool)
	queue := []string{startKey}
	visited[startKey] = true

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for dep := range reverseDeps[cur] {
			if !visited[dep] {
				visited[dep] = true
				queue = append(queue, dep)
			}
		}
	}

	// Subtract 1: startKey itself is in visited but is not a dependent of itself.
	return len(visited) - 1
}
