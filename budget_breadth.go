package repomap

// budgetFilesBreadthFirst assigns DetailLevel to each RankedFile using a
// breadth-first strategy: ensure every file is at least VISIBLE (level 0 or 1)
// before spending any budget on depth (level 2 enrichment).
//
// This differs from the old greedy single-pass loop, which spent the budget
// front-to-back on full enrichment and omitted (-1) lower-ranked files once the
// budget was exhausted. Breadth-first keeps lower-ranked files visible at a
// summary or header line instead of dropping them entirely.
//
// costFn is the caller's per-file symbol cost estimator (enrichedCost for the
// default renderer, compactCost for lean orientation).
// cfg may be nil — nil means no file-level overrides (backward compatible).
// When maxTokens is 0, all files get DetailLevel 2 (unlimited mode).
func budgetFilesBreadthFirst(ranked []RankedFile, maxTokens int, costFn func([]Symbol) int, cfg *BlocklistConfig) []RankedFile {
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

	// headerCap guard: bound output under pathological tiny budgets. Files whose
	// path header can't fit within 70% of the budget are omitted (-1). This is the
	// only place a file is set to -1 in pass 1.
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

	// Files beyond cutoff are omitted (path header can't fit).
	for i := cutoff; i < len(ranked); i++ {
		ranked[i].DetailLevel = -1
	}

	// Pass 1 (breadth): walk all files in rank order. Reserve a one-line summary
	// cost where it fits; otherwise leave the file at header-only (level 0). No
	// file is dropped to -1 here (except the headerCap guard above).
	// summaryCostOf records the per-file summary cost actually spent so pass 2 can
	// reclaim it when promoting to level 2.
	used := headerCost
	summaryCostOf := make([]int, cutoff)
	for i := 0; i < cutoff; i++ {
		if len(ranked[i].Symbols) == 0 {
			ranked[i].DetailLevel = 0
			continue
		}

		groups := countGroups(ranked[i].Path, ranked[i].Symbols)
		summaryCost := groups * 30
		if full := costFn(ranked[i].Symbols); full < summaryCost {
			summaryCost = full
		}

		if used+summaryCost <= budgetBytes {
			ranked[i].DetailLevel = 1
			used += summaryCost
			summaryCostOf[i] = summaryCost
			continue
		}

		// Summary doesn't fit — keep the file visible at header-only.
		ranked[i].DetailLevel = 0
	}

	// Pass 2 (depth): walk files again in rank order, promoting level-1 files to
	// level 2 (full symbols) where the enriched cost fits. Reclaim the summary
	// cost already spent for the file before charging the full cost.
	for i := 0; i < cutoff; i++ {
		if ranked[i].DetailLevel != 1 {
			continue
		}
		enriched := costFn(ranked[i].Symbols)
		if fileAllDead(ranked[i].Symbols) {
			enriched = enriched / 2
		}
		if used-summaryCostOf[i]+enriched <= budgetBytes {
			ranked[i].DetailLevel = 2
			used += enriched - summaryCostOf[i]
		}
	}

	applyFileOverrides(ranked, cutoff, budgetBytes, &used, costFn, cfg)
	return ranked
}
