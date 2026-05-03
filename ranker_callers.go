package repomap

import "sort"

// ApplyCallerBonus boosts files proportional to their caller count.
// Only meaningful in --calls mode where caller data is available.
//
// Score delta: min(uniqueCallerFiles*2, 30) — capped at +30 so caller-heavy
// files boost rank without dominating files with high import counts.
func ApplyCallerBonus(ranked []RankedFile, callerCounts map[string]int) {
	for i := range ranked {
		if count, ok := callerCounts[ranked[i].Path]; ok && count > 0 {
			bonus := count * 2
			if bonus > 30 {
				bonus = 30
			}
			ranked[i].Score += bonus
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})
}

// CallerCountsFromSymbolCallers derives a per-file unique-caller-file count
// from the SymbolCallers map produced by ExpandCallers.
//
// The key in SymbolCallers is "targetFile\x00symbol"; each value is a slice
// of Locations (one per call site). We count distinct caller files per target
// file across all its symbols.
func CallerCountsFromSymbolCallers(callers SymbolCallers) map[string]int {
	// targetFile → set of unique caller files
	seen := make(map[string]map[string]struct{})
	for key, locs := range callers {
		// key is "targetFile\x00symbol" (see callsKey in calls.go)
		idx := indexByte(key, '\x00')
		if idx < 0 {
			continue
		}
		targetFile := key[:idx]
		if seen[targetFile] == nil {
			seen[targetFile] = make(map[string]struct{})
		}
		for _, loc := range locs {
			seen[targetFile][loc.File] = struct{}{}
		}
	}

	counts := make(map[string]int, len(seen))
	for file, callerSet := range seen {
		counts[file] = len(callerSet)
	}
	return counts
}

// indexByte returns the index of the first occurrence of b in s, or -1.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
