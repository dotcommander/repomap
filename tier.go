// Package repomap: tiers are a pure projection over ScoreComponents.
// They add provenance labels to explain why a file scored as it did,
// but never mutate Score or ScoreComponents.
package repomap

import (
	"fmt"
	"strings"
)

// Confidence represents the reliability tier of a score component.
type Confidence string

const (
	ConfidenceConfirmed  Confidence = "confirmed"  // LSP/gopls-verified references
	ConfidenceStructural Confidence = "structural" // parsed structure / import graph
	ConfidenceLexical    Confidence = "lexical"    // by-name string match, may be coincidental
	ConfidenceContextual Confidence = "contextual" // query- or caller-dependent
)

// confidenceOrder is the canonical render order for tiers (highest confidence first).
var confidenceOrder = []Confidence{ConfidenceConfirmed, ConfidenceStructural, ConfidenceLexical, ConfidenceContextual}

// componentConfidence maps each active score-component key to its confidence tier.
// scoreComponentDiagnostics is intentionally excluded — it is defined but never passed
// to addScoreComponent, so it has no meaningful tier assignment.
var componentConfidence = map[string]Confidence{
	scoreComponentCallers:    ConfidenceConfirmed,
	scoreComponentImports:    ConfidenceStructural,
	scoreComponentTransitive: ConfidenceStructural,
	scoreComponentEntry:      ConfidenceStructural,
	scoreComponentSymbols:    ConfidenceStructural,
	scoreComponentBoundary:   ConfidenceStructural,
	scoreComponentDepth:      ConfidenceStructural,
	scoreComponentSymbolRefs: ConfidenceLexical,
	scoreComponentIntent:     ConfidenceContextual,
	scoreComponentConsumed:   ConfidenceContextual,
}

// allScoreComponents is the canonical enumeration of active component keys —
// every key that is actually passed to addScoreComponent. Used by tier_test.go
// to enforce completeness of componentConfidence.
var allScoreComponents = []string{
	scoreComponentEntry,
	scoreComponentSymbols,
	scoreComponentDepth,
	scoreComponentImports,
	scoreComponentTransitive,
	scoreComponentBoundary,
	scoreComponentIntent,
	scoreComponentConsumed,
	scoreComponentCallers,
	scoreComponentSymbolRefs,
}

// tierOf returns the confidence tier for a score component key.
// Completeness is guaranteed by tier_test.go; this fallback is defense-in-depth
// for any component added in future without a corresponding map entry.
func tierOf(componentKey string) Confidence {
	if c, ok := componentConfidence[componentKey]; ok {
		return c
	}
	return ConfidenceStructural
}

// ConfidenceOrder returns tier labels in canonical render order (highest confidence first).
func ConfidenceOrder() []string {
	out := make([]string, len(confidenceOrder))
	for i, c := range confidenceOrder {
		out[i] = string(c)
	}
	return out
}

// scoreExplainAnnotation returns the " # score <N> · <tier>:<subtotal> …" trailing
// annotation for a file header line. Tiers are iterated in confidenceOrder; tiers
// with a zero subtotal are omitted. Leading space is included.
func scoreExplainAnnotation(f RankedFile) string {
	if len(f.ScoreComponents) == 0 {
		return fmt.Sprintf(" # score %d", f.Score)
	}

	// Accumulate subtotals in canonical order (no map iteration for output).
	var parts []string
	for _, tier := range confidenceOrder {
		subtotal := 0
		for key, delta := range f.ScoreComponents {
			if tierOf(key) == tier {
				subtotal += delta
			}
		}
		if subtotal != 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", tier, subtotal))
		}
	}

	if len(parts) == 0 {
		return fmt.Sprintf(" # score %d", f.Score)
	}
	return fmt.Sprintf(" # score %d · %s", f.Score, strings.Join(parts, " "))
}
