package repomap

import "math/bits"

// BudgetFiles assigns a DetailLevel to each RankedFile within the given token budget.
// When maxTokens is 0, all files get DetailLevel 2 (unlimited mode for verbose/detail).
//
// Detail levels:
//
//	-1: omitted (budget overflow, counted in footer)
//	 0: header only — path + optional (package name)
//	 1: summary — path + "5 types, 3 funcs"
//	 2: full symbol groups
//	 3: symbols + struct/interface field expansion
func BudgetFiles(ranked []RankedFile, maxTokens int) []RankedFile {
	if len(ranked) == 0 {
		return ranked
	}

	// Unlimited mode: everything gets full symbols.
	if maxTokens == 0 {
		for i := range ranked {
			ranked[i].DetailLevel = 2
		}
		return ranked
	}

	budgetBytes := maxTokens * 4

	// Phase 1: Estimate header cost per file, cap at 70% of budget.
	headerCap := budgetBytes * 70 / 100
	headerCost := 0
	cutoff := len(ranked)
	for i, f := range ranked {
		cost := len(f.Path) + 20 // path + annotation + newline overhead
		if headerCost+cost > headerCap {
			cutoff = i
			break
		}
		headerCost += cost
	}

	// Files beyond cutoff are omitted.
	for i := cutoff; i < len(ranked); i++ {
		ranked[i].DetailLevel = -1
	}

	// Phase 2: Walk files in rank order, upgrade detail levels within budget.
	used := headerCost
	for i := 0; i < cutoff; i++ {
		if len(ranked[i].Symbols) == 0 {
			ranked[i].DetailLevel = 0
			continue
		}

		// Estimate cost of summary line (~30 bytes per group).
		groups := countGroups(ranked[i].Path, ranked[i].Symbols)
		summaryCost := groups * 30
		if used+summaryCost <= budgetBytes {
			ranked[i].DetailLevel = 1
			used += summaryCost
		}

		// Try upgrading to full symbols — estimate ~20 bytes per symbol.
		symbolCost := len(ranked[i].Symbols) * 20
		if used+symbolCost-summaryCost <= budgetBytes {
			ranked[i].DetailLevel = 2
			used += symbolCost - summaryCost
		}
	}

	// Phase 3: Top 10 structs/interfaces by file score get field expansion.
	promoted := 0
	for i := 0; i < cutoff && promoted < 10; i++ {
		if ranked[i].DetailLevel < 2 {
			continue
		}
		for _, s := range ranked[i].Symbols {
			if (s.Kind == "struct" || s.Kind == "interface") && s.Signature != "" && s.Signature != "{}" {
				// Estimate field expansion cost.
				fieldCost := len(s.Signature) + 10
				if used+fieldCost <= budgetBytes {
					ranked[i].DetailLevel = 3
					used += fieldCost
					promoted++
				}
				break
			}
		}
	}

	return ranked
}

// categoryBit maps a category name to a bit position (0-9).
// Categories match categoryOrder in render.go.
var categoryBit = map[string]uint16{
	"tests":      1 << 0,
	"types":      1 << 1,
	"interfaces": 1 << 2,
	"classes":    1 << 3,
	"enums":      1 << 4,
	"funcs":      1 << 5,
	"methods":    1 << 6,
	"consts":     1 << 7,
	"vars":       1 << 8,
	"other":      1 << 9,
}

// countGroups returns the number of distinct symbol categories in the given symbols.
// Uses a bitmask to avoid heap allocation.
func countGroups(path string, syms []Symbol) int {
	var seen uint16
	for _, s := range syms {
		seen |= categoryBit[symbolCategory(path, s)]
	}
	return bits.OnesCount16(seen)
}
