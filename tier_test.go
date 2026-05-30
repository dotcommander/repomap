package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComponentConfidence_Complete ensures every active score component has an
// explicit tier mapping. If this test fails, add the missing key to componentConfidence.
func TestComponentConfidence_Complete(t *testing.T) {
	t.Parallel()
	for _, key := range allScoreComponents {
		_, ok := componentConfidence[key]
		require.True(t, ok, "allScoreComponents entry %q has no mapping in componentConfidence", key)
	}
}

// TestTierOf_FallbackStructural verifies that an unknown component key falls
// back to ConfidenceStructural rather than panicking or returning an empty string.
func TestTierOf_FallbackStructural(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ConfidenceStructural, tierOf("nonexistent_key"))
}

// TestConfidenceOrder_CoversAllTiers verifies confidenceOrder contains exactly
// the four tier values with no duplicates, so every tier reachable via
// componentConfidence appears in the render order exactly once.
func TestConfidenceOrder_CoversAllTiers(t *testing.T) {
	t.Parallel()

	allTiers := []Confidence{
		ConfidenceConfirmed,
		ConfidenceStructural,
		ConfidenceLexical,
		ConfidenceContextual,
	}

	require.Len(t, confidenceOrder, len(allTiers), "confidenceOrder length mismatch")

	seen := make(map[Confidence]int, len(confidenceOrder))
	for _, c := range confidenceOrder {
		seen[c]++
	}

	for _, tier := range allTiers {
		assert.Equal(t, 1, seen[tier], "tier %q should appear exactly once in confidenceOrder", tier)
	}
}
