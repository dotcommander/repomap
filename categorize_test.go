package repomap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderKindWeight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     string
		exported bool
		want     int
	}{
		// Exported weight tiers.
		{"exported struct", "struct", true, 10},
		{"exported interface", "interface", true, 10},
		{"exported type alias", "type", true, 8},
		{"exported function", "function", true, 6},
		{"exported fn alias", "fn", true, 6},
		{"exported method", "method", true, 5},
		{"exported constant", "constant", true, 3},
		{"exported const", "const", true, 3},
		{"exported variable", "variable", true, 3},
		{"exported var", "var", true, 3},
		{"exported static", "static", true, 3},
		{"exported unknown kind", "macro", true, 2},
		{"exported class", "class", true, 2},
		// Unexported always returns 1, regardless of kind.
		{"unexported struct", "struct", false, 1},
		{"unexported function", "function", false, 1},
		{"unexported method", "method", false, 1},
		{"unexported unknown", "macro", false, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := renderKindWeight(tc.kind, tc.exported)
			assert.Equal(t, tc.want, got, "renderKindWeight(%q, %v)", tc.kind, tc.exported)
		})
	}
}

func TestRenderKindWeight_OrderingProperty(t *testing.T) {
	t.Parallel()

	// Verify the ordering invariants hold for the weight tiers.
	assert.Greater(t, renderKindWeight("struct", true), renderKindWeight("type", true), "struct > type alias")
	assert.Greater(t, renderKindWeight("interface", true), renderKindWeight("type", true), "interface > type alias")
	assert.Greater(t, renderKindWeight("type", true), renderKindWeight("function", true), "type alias > function")
	assert.Greater(t, renderKindWeight("function", true), renderKindWeight("method", true), "function > method")
	assert.Greater(t, renderKindWeight("method", true), renderKindWeight("const", true), "method > const")
	assert.Greater(t, renderKindWeight("const", true), renderKindWeight("macro", true), "const > unknown exported")
	assert.Greater(t, renderKindWeight("macro", true), renderKindWeight("struct", false), "any exported > unexported")
}

// TestKindWeightSymbolOrdering is an integration test that builds a FileSymbols
// with symbols in deliberately reverse order and verifies that formatFileBlockDefault
// emits them in weight-descending order: struct → var → (unexported func omitted).
func TestKindWeightSymbolOrdering(t *testing.T) {
	t.Parallel()

	syms := []Symbol{
		{Name: "helper", Kind: "function", Exported: false, Line: 1}, // unexported — should be omitted
		{Name: "MaxSize", Kind: "variable", Exported: true, Line: 2}, // weight 3
		{Name: "Agent", Kind: "struct", Exported: true, Line: 3},     // weight 10
	}

	f := makeRankedFile("core/agent.go", 2, syms)
	out := formatFileBlockDefault(f)

	// Unexported symbol must not appear.
	assert.NotContains(t, out, "helper", "unexported func must be omitted")

	// Both exported symbols must appear.
	assert.Contains(t, out, "Agent", "exported struct must appear")
	assert.Contains(t, out, "MaxSize", "exported var must appear")

	// Agent (struct, weight 10) must appear before MaxSize (var, weight 3).
	agentPos := strings.Index(out, "Agent")
	maxSizePos := strings.Index(out, "MaxSize")
	assert.Greater(t, maxSizePos, agentPos, "struct (Agent) must appear before var (MaxSize)")
}

// TestKindWeightSymbolOrdering_WithFunction verifies that exported struct → exported
// function → exported const ordering is preserved across all three weight tiers.
func TestKindWeightSymbolOrdering_WithFunction(t *testing.T) {
	t.Parallel()

	// Intentionally listed in reverse weight order.
	syms := []Symbol{
		{Name: "DefaultTimeout", Kind: "const", Exported: true, Line: 1}, // weight 3
		{Name: "New", Kind: "function", Exported: true, Line: 2},         // weight 6
		{Name: "Config", Kind: "struct", Exported: true, Line: 3},        // weight 10
	}

	f := makeRankedFile("pkg/config.go", 2, syms)
	out := formatFileBlockDefault(f)

	configPos := strings.Index(out, "Config")
	newPos := strings.Index(out, "New")
	timeoutPos := strings.Index(out, "DefaultTimeout")

	assert.Greater(t, newPos, configPos, "struct (Config) must appear before function (New)")
	assert.Greater(t, timeoutPos, newPos, "function (New) must appear before const (DefaultTimeout)")
}
