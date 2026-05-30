package repomap

import "math/bits"

// BudgetFiles assigns a DetailLevel to each RankedFile within the given token budget.
// cfg may be nil — nil means no file-level overrides (backward compatible).
// When maxTokens is 0, all files get DetailLevel 2 (unlimited mode for verbose/detail).
//
// Detail levels:
//
//	-1: omitted (budget overflow, counted in footer)
//	 0: header only — path + optional (package name)
//	 1: summary — path + "5 types, 3 funcs"
//	 2: full symbol groups
//	 3: symbols + struct/interface field expansion
//
// fileAllDead reports whether all exported symbols in the slice are marked Dead.
// Returns false when there are no exported symbols (nothing to be dead).
func fileAllDead(syms []Symbol) bool {
	hasExported := false
	for _, s := range syms {
		if s.Exported {
			hasExported = true
			if !s.Dead {
				return false
			}
		}
	}
	return hasExported
}

func BudgetFiles(ranked []RankedFile, maxTokens int, cfg *BlocklistConfig) []RankedFile {
	budgetFilesBreadthFirst(ranked, maxTokens, enrichedCost, cfg)
	// promoteFieldExpansion is enriched-only: it upgrades level-2 files to level 3
	// (inline field expansion), which the lean/compact renderer does not emit.
	cutoff := len(ranked)
	budgetBytes := maxTokens * 4
	used := 0
	for i := range ranked {
		if ranked[i].DetailLevel == 2 {
			used += enrichedCost(ranked[i].Symbols)
		}
	}
	promoteFieldExpansion(ranked, cutoff, budgetBytes, used)
	return ranked
}

// enrichedCost estimates the byte length of a file's rendered output under the
// v0.7.0 enriched-default format (Item 1 in v0.7.0-output-quality.md): for each
// exported symbol, a name+signature line and an optional godoc subtitle.
// Unexported symbols contribute zero since the default renderer excludes them.
//
// Struct/interface symbols render their typed field list inline on the name line
// (e.g. "  type Config{Name string, ID int}") rather than as a separate field block.
// enrichedCost reflects this: it counts the signature once in the name-line cost
// and does NOT add a separate field-block term.
//
// The estimate MUST track formatFileBlockDefault's actual output within ±10%.
// See TestEnrichedCost_MatchesRenderer.
func enrichedCost(syms []Symbol) int {
	cost := 0
	for _, s := range syms {
		if !s.Exported {
			continue
		}
		// Name line: "  " + kindKeyword + " " + Name + Signature + "\n"
		// kindKeyword is at most 5 bytes ("const"). Use 8 as total overhead:
		// 2 (indent) + 4 (keyword approx) + 1 (space) + 1 (newline) = 8.
		cost += 8 + len(s.Name) + len(s.Signature)
		// Methods render as "  func (*Receiver) Name(sig)\n".
		// The receiver adds "(*" + Receiver + ") " = len(Receiver) + 4 bytes beyond the base.
		if s.Kind == "method" && s.Receiver != "" {
			cost += len(s.Receiver) + 4
		}
		// Godoc subtitle: "    // " + Doc + "\n" = 8 + len(Doc)
		if s.Doc != "" {
			cost += 8 + len(s.Doc)
		}
	}
	return cost
}

// compactCost estimates the byte length of a file's rendered output under the
// lean orientation mode (m.StringCompact / -f compact): for each exported symbol,
// a single name-only line ("  kindKeyword Name\n"). No signatures, no doc, no fields.
// Unexported symbols are excluded by the compact renderer and contribute zero cost.
//
// compactCost is strictly less than enrichedCost for any file with exported symbols,
// so compact mode fits more files within the same token budget.
func compactCost(syms []Symbol) int {
	cost := 0
	for _, s := range syms {
		if !s.Exported {
			continue
		}
		// "  " + kindKeyword (≤5 bytes) + " " + Name + "\n"
		// 8 bytes of fixed overhead (indent, keyword approx, space, newline).
		cost += 8 + len(s.Name)
	}
	return cost
}

// BudgetFilesCompact assigns DetailLevel to each RankedFile using compactCost estimates,
// matching the lean orientation renderer (path + exported symbol names only).
// cfg may be nil — nil means no file-level overrides (backward compatible).
// When maxTokens is 0, all files get DetailLevel 2 (unlimited mode).
//
// This is separate from BudgetFiles (which uses enrichedCost) so compact-mode callers
// get accurate budgeting without rewriting the enriched budget loop.
func BudgetFilesCompact(ranked []RankedFile, maxTokens int, cfg *BlocklistConfig) []RankedFile {
	return budgetFilesWithCost(ranked, maxTokens, compactCost, cfg)
}

// budgetFilesWithCost is the shared budget loop parameterised by cost function.
// BudgetFiles (enriched default) and BudgetFilesCompact (lean orientation) both use it.
// cfg may be nil — nil means no file-level overrides (backward compatible).
func budgetFilesWithCost(ranked []RankedFile, maxTokens int, costFn func([]Symbol) int, cfg *BlocklistConfig) []RankedFile {
	return budgetFilesBreadthFirst(ranked, maxTokens, costFn, cfg)
}

// promoteFieldExpansion upgrades up to 10 top-ranked DetailLevel-2 files to
// DetailLevel 3 (field expansion) while honoring the remaining byte budget.
// Mutates ranked in place.
func promoteFieldExpansion(ranked []RankedFile, cutoff, budgetBytes, used int) {
	promoted := 0
	for i := 0; i < cutoff && promoted < 10; i++ {
		if ranked[i].DetailLevel < 2 {
			continue
		}
		for _, s := range ranked[i].Symbols {
			if s.HasFields() {
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
}

// categoryBit maps category names to a unique bit, derived from categoryOrder.
var categoryBit = func() map[string]uint16 {
	m := make(map[string]uint16, len(categoryOrder))
	for i, cat := range categoryOrder {
		m[cat.key] = 1 << i
	}
	return m
}()

// countGroups returns the number of distinct symbol categories in the given symbols.
// Uses a bitmask to avoid heap allocation.
func countGroups(path string, syms []Symbol) int {
	var seen uint16
	for _, s := range syms {
		seen |= categoryBit[symbolCategory(path, s)]
	}
	return bits.OnesCount16(seen)
}
