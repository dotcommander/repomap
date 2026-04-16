package repomap

import (
	"sort"
	"strings"
)

// SymbolMatch is a single hit from FindSymbol. Results are sorted by Score
// descending; use the File+Symbol.Line pair for a stable identifier.
type SymbolMatch struct {
	File        string  // path relative to root
	Symbol      Symbol  // the matching symbol
	Score       float64 // relevance score: 100=exact, 75=exact-CI, 50=prefix, 25=contains
	DetailLevel int     // copied from the owning RankedFile (for budget-aware callers)
}

// ParseFindQuery splits a positional query of the form
//
//	[kind:][file:<path>:]<name>
//
// into (name, kind, file). Qualifier prefixes may appear in either order; the
// final token is always the name. Empty input returns all empties.
func ParseFindQuery(q string) (name, kind, file string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", "", ""
	}
	parts := strings.Split(q, ":")
	// Walk left-to-right consuming qualifier prefixes. Remaining tokens rejoin
	// as the name (so a name with a literal `:` still works if no qualifiers).
	for len(parts) > 1 {
		switch parts[0] {
		case "kind":
			kind = parts[1]
			parts = parts[2:]
		case "file":
			file = parts[1]
			parts = parts[2:]
		default:
			// No more qualifiers — everything left is the name.
			name = strings.Join(parts, ":")
			return name, kind, file
		}
	}
	if len(parts) == 1 {
		name = parts[0]
	}
	return name, kind, file
}

// FindSymbol searches the ranked symbol set for matches.
//
//	name:  required (empty → empty result). Matched in priority order:
//	       exact (100) > case-insensitive exact (75) > prefix (50) > contains (25).
//	kind:  optional filter; "" matches any. Matched case-insensitively against Symbol.Kind.
//	file:  optional substring filter against RankedFile.Path; "" matches any.
//
// Results are sorted by Score desc, then the owning RankedFile.Score desc
// (tiebreaker), then File asc (stable tiebreaker). Safe for concurrent use.
func (m *Map) FindSymbol(name, kind, file string) []SymbolMatch {
	out := []SymbolMatch{}
	if name == "" {
		return out
	}
	m.mu.RLock()
	ranked := m.ranked
	m.mu.RUnlock()

	nameLower := strings.ToLower(name)
	kindLower := strings.ToLower(kind)

	type scored struct {
		match     SymbolMatch
		fileScore int
	}
	var hits []scored

	for i := range ranked {
		rf := &ranked[i]
		if rf.FileSymbols == nil {
			continue
		}
		if file != "" && !strings.Contains(rf.Path, file) {
			continue
		}
		for _, sym := range rf.Symbols {
			if kindLower != "" && strings.ToLower(sym.Kind) != kindLower {
				continue
			}
			score := scoreSymbolMatch(sym.Name, name, nameLower)
			if score == 0 {
				continue
			}
			hits = append(hits, scored{
				match: SymbolMatch{
					File:        rf.Path,
					Symbol:      sym,
					Score:       score,
					DetailLevel: rf.DetailLevel,
				},
				fileScore: rf.Score,
			})
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].match.Score != hits[j].match.Score {
			return hits[i].match.Score > hits[j].match.Score
		}
		if hits[i].fileScore != hits[j].fileScore {
			return hits[i].fileScore > hits[j].fileScore
		}
		return hits[i].match.File < hits[j].match.File
	})

	out = make([]SymbolMatch, len(hits))
	for i, h := range hits {
		out[i] = h.match
	}
	return out
}

// scoreSymbolMatch returns the relevance score, or 0 for no match.
// Caller passes nameLower pre-computed to avoid per-symbol allocation.
func scoreSymbolMatch(symName, name, nameLower string) float64 {
	switch {
	case symName == name:
		return 100
	case strings.EqualFold(symName, name):
		return 75
	case strings.HasPrefix(symName, name):
		return 50
	case strings.Contains(strings.ToLower(symName), nameLower):
		return 25
	}
	return 0
}
