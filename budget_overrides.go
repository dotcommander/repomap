package repomap

// applyFileOverrides walks ALL ranked files and applies any cfg.MatchFileOverride
// matches, adjusting the used-byte counter accordingly. Overrides are authoritative
// and apply regardless of whether a file was within or beyond the budget cutoff.
//
//   - force-omit (level == -1): reclaims previously-spent symbol cost back into
//     the budget so lower-ranked files may be promoted in a subsequent pass.
//   - force-full (level == 2): deducts the full symbol cost; override is applied
//     even when the budget is exhausted.
//
// No-op when cfg is nil or has no compiled overrides.
func applyFileOverrides(ranked []RankedFile, _ int, _ int, used *int, costFn func([]Symbol) int, cfg *BlocklistConfig) {
	if cfg == nil || len(cfg.compiledOverrides) == 0 {
		return
	}
	for i := range ranked {
		level, ok := cfg.MatchFileOverride(ranked[i].Path)
		if !ok {
			continue
		}
		old := ranked[i].DetailLevel
		ranked[i].DetailLevel = level
		// Adjust budget tracking based on old → new transition.
		switch {
		case level == -1:
			// Reclaim previously-spent symbol cost.
			if old == 2 {
				*used -= costFn(ranked[i].Symbols)
			} else if old == 1 {
				groups := countGroups(ranked[i].Path, ranked[i].Symbols)
				*used -= groups * 30
			}
		case level == 2 && old != 2:
			// Deduct full cost; override is authoritative even when budget is tight.
			*used += costFn(ranked[i].Symbols)
		}
	}
}
