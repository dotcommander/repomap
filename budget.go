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
		cost := len(f.Path) + 30 // path + annotation + newline overhead
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

	// Phase 2: Walk files in rank order, assign detail levels within budget.
	// Invariant: a file at DetailLevel=2 gets ALL enriched content or is demoted.
	// Never assign DetailLevel=2 with partial content — the LLM consumer must
	// either see a file in full or know (via footer) that it was omitted.
	used := headerCost
	for i := 0; i < cutoff; i++ {
		if len(ranked[i].Symbols) == 0 {
			ranked[i].DetailLevel = 0
			continue
		}

		// Estimate cost of summary line (~30 bytes per group).
		groups := countGroups(ranked[i].Path, ranked[i].Symbols)
		summaryCost := groups * 30

		// Try enriched (DetailLevel=2) FIRST using the all-or-nothing cost.
		enriched := enrichedCost(ranked[i].Symbols)
		if fileAllDead(ranked[i].Symbols) {
			enriched = enriched / 2
		}
		if used+enriched <= budgetBytes {
			ranked[i].DetailLevel = 2
			used += enriched
			continue
		}

		// Fall back to summary if it fits.
		if used+summaryCost <= budgetBytes {
			ranked[i].DetailLevel = 1
			used += summaryCost
			continue
		}

		// Neither fits — omit. Header bytes were already counted in Phase 1; we
		// leave them "spent" even though the file won't render. This is
		// conservative and correct: over-omitting under tight budgets is the
		// documented v0.7.0 tradeoff per the Risk table in the parent spec.
		ranked[i].DetailLevel = -1
	}

	promoteFieldExpansion(ranked, cutoff, budgetBytes, used)
	return ranked
}

// fileAllDead reports whether all exported symbols in the slice are marked Dead.
// Returns false when there are no exported symbols (nothing to demote).
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
// When maxTokens is 0, all files get DetailLevel 2 (unlimited mode).
//
// This is separate from BudgetFiles (which uses enrichedCost) so compact-mode callers
// get accurate budgeting without rewriting the enriched budget loop.
func BudgetFilesCompact(ranked []RankedFile, maxTokens int) []RankedFile {
	return budgetFilesWithCost(ranked, maxTokens, compactCost)
}

// budgetFilesWithCost is the shared budget loop parameterised by cost function.
// BudgetFiles (enriched default) and BudgetFilesCompact (lean orientation) both use it.
func budgetFilesWithCost(ranked []RankedFile, maxTokens int, costFn func([]Symbol) int) []RankedFile {
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
		cost := len(f.Path) + 30
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

	// Phase 2: Walk files in rank order, assign detail levels within budget.
	used := headerCost
	for i := 0; i < cutoff; i++ {
		if len(ranked[i].Symbols) == 0 {
			ranked[i].DetailLevel = 0
			continue
		}

		groups := countGroups(ranked[i].Path, ranked[i].Symbols)
		summaryCost := groups * 30

		// Try full detail (level 2) using the caller-supplied cost function.
		fullCost := costFn(ranked[i].Symbols)
		if fileAllDead(ranked[i].Symbols) {
			fullCost = fullCost / 2
		}
		if used+fullCost <= budgetBytes {
			ranked[i].DetailLevel = 2
			used += fullCost
			continue
		}

		// Fall back to summary if it fits.
		if used+summaryCost <= budgetBytes {
			ranked[i].DetailLevel = 1
			used += summaryCost
			continue
		}

		ranked[i].DetailLevel = -1
	}

	return ranked
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
