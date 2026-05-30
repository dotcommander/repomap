package repomap

import (
	"fmt"
	"path/filepath"
)

// ExplainResult describes why one file ranked and rendered the way it did.
type ExplainResult struct {
	File            StructuredFile    `json:"file"`
	Score           int               `json:"score"`
	ScoreComponents map[string]int    `json:"score_components,omitempty"`
	ComponentTotal  int               `json:"component_total"`
	DetailLevel     int               `json:"detail_level"`
	OmittedReason   string            `json:"omitted_reason,omitempty"`
	ScoreByTier     map[string]int    `json:"score_by_tier,omitempty"`   // tier label -> summed subtotal
	ComponentTiers  map[string]string `json:"component_tiers,omitempty"` // component key -> tier label
}

// Explain returns score and budget evidence for relPath.
func (m *Map) Explain(relPath string) (ExplainResult, error) {
	m.mu.RLock()
	ranked := cloneRanked(m.ranked)
	cfg := m.config
	blocklist := m.blocklist
	m.mu.RUnlock()

	if cfg.MaxTokens > 0 {
		ranked = BudgetFiles(ranked, cfg.MaxTokens, blocklist)
	}

	relPath = filepath.ToSlash(filepath.Clean(relPath))
	for _, f := range ranked {
		if filepath.ToSlash(f.Path) != relPath {
			continue
		}
		omitted := omittedReason(f, cfg.MaxTokens, blocklist)
		components := cloneScoreComponents(f.ScoreComponents)
		var scoreByTier map[string]int
		var componentTiers map[string]string
		if len(components) > 0 {
			scoreByTier = make(map[string]int, 4)
			componentTiers = make(map[string]string, len(components))
			for k, v := range components {
				tier := string(tierOf(k))
				componentTiers[k] = tier
				scoreByTier[tier] += v
			}
		}
		return ExplainResult{
			File:            structuredFile(f, omitted),
			Score:           f.Score,
			ScoreComponents: components,
			ComponentTotal:  ScoreComponentTotal(f),
			DetailLevel:     f.DetailLevel,
			OmittedReason:   omitted,
			ScoreByTier:     scoreByTier,
			ComponentTiers:  componentTiers,
		}, nil
	}
	return ExplainResult{}, fmt.Errorf("file %q not found in repomap", relPath)
}

// ScoreComponentTotal returns the sum of tracked score components.
func ScoreComponentTotal(f RankedFile) int {
	total := 0
	for _, v := range f.ScoreComponents {
		total += v
	}
	return total
}
