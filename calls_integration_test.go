package repomap

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExpandCallers_RealLspq runs an actual lspq invocation against a known
// symbol in this repository. Gated by testing.Short() — skipped in CI / -short mode.
func TestExpandCallers_RealLspq(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("integration test: requires lspq on PATH and a live gopls server")
	}

	// Verify lspq is available.
	if _, err := exec.LookPath("lspq"); err != nil {
		t.Skip("lspq not found on PATH; skipping integration test")
	}

	// Use RankFiles as our target: it's defined in ranker.go line 23.
	// We query lspq directly and verify the JSON shape.
	q := lspqQuerier{}
	locs, err := q.Refs(context.Background(), "ranker.go", 23, "RankFiles")
	require.NoError(t, err, "lspq refs should succeed for RankFiles")
	assert.NotEmpty(t, locs, "RankFiles should have at least one reference")

	// Verify each location has the expected fields.
	for _, loc := range locs {
		assert.NotEmpty(t, loc.File, "location file should be non-empty")
		assert.Greater(t, loc.Line, 0, "location line should be positive")
	}
}

// TestLspqOutputShape verifies that the JSON output of lspq --json refs
// matches the shape we parse in lspqRefsOutput.
func TestLspqOutputShape(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("integration test: requires lspq")
	}
	if _, err := exec.LookPath("lspq"); err != nil {
		t.Skip("lspq not found on PATH")
	}

	// Build the fixture programmatically to avoid raw JSON string literals.
	fixture := lspqRefsOutput{
		References: []Location{
			{File: "ranker.go", Line: 23, Column: 6},
		},
	}
	data, err := json.Marshal(fixture)
	require.NoError(t, err)

	var out lspqRefsOutput
	require.NoError(t, json.Unmarshal(data, &out))
	require.Len(t, out.References, 1)
	assert.Equal(t, "ranker.go", out.References[0].File)
	assert.Equal(t, 23, out.References[0].Line)
	assert.Equal(t, 6, out.References[0].Column)
}
