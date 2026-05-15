package repomap

import (
	"fmt"
	"path/filepath"
)

// ExplainResult describes why one file ranked and rendered the way it did.
type ExplainResult struct {
	File            StructuredFile `json:"file"`
	Score           int            `json:"score"`
	ScoreComponents map[string]int `json:"score_components,omitempty"`
	ComponentTotal  int            `json:"component_total"`
	DetailLevel     int            `json:"detail_level"`
	OmittedReason   string         `json:"omitted_reason,omitempty"`
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
		return ExplainResult{
			File:            structuredFile(f, omitted),
			Score:           f.Score,
			ScoreComponents: cloneScoreComponents(f.ScoreComponents),
			ComponentTotal:  ScoreComponentTotal(f),
			DetailLevel:     f.DetailLevel,
			OmittedReason:   omitted,
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
