package repomap

import "strings"

// boundaryRule maps well-known import prefixes to a semantic boundary label
// and a per-file score bump awarded when any prefix in the rule matches.
// Rules are defined per-language in language.go (languageDef.BoundaryRules).
type boundaryRule struct {
	Label     string   // e.g. "HTTP", "Postgres"
	Prefixes  []string // import path prefixes that trigger this label
	ScoreBump int      // score added when any prefix matches
}

// maxBoundaryBump is the maximum total score bump a single file can earn
// from boundary classification, regardless of how many boundaries match.
const maxBoundaryBump = 15

// classifyBoundaries inspects imports for the given language and returns the
// set of boundary labels present (in langBoundaryRules order) and the total
// score bump (capped at maxBoundaryBump). Each label is emitted at most once
// even if multiple imports in the same file match the same rule.
// Languages with no boundary rules (e.g. C/C++) return empty labels and 0 bump.
func classifyBoundaries(langID string, imports []string) (labels []string, bump int) {
	rules := langBoundaryRules[langID]
	for _, rule := range rules {
		matched := false
		for _, imp := range imports {
			for _, prefix := range rule.Prefixes {
				if strings.HasPrefix(imp, prefix) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			labels = append(labels, rule.Label)
			bump += rule.ScoreBump
		}
	}
	if bump > maxBoundaryBump {
		bump = maxBoundaryBump
	}
	return labels, bump
}
