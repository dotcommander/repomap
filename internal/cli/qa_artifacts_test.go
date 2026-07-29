package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type qaLedger struct {
	SchemaVersion int `json:"schema_version"`
	Entries       []struct {
		ID            string   `json:"id"`
		Command       string   `json:"command"`
		Flag          string   `json:"flag"`
		Expected      string   `json:"expected"`
		Observed      string   `json:"observed"`
		Repro         string   `json:"repro"`
		Severity      string   `json:"severity"`
		OutputQuality []string `json:"output_quality"`
		Status        string   `json:"status"`
		ProposedFix   string   `json:"proposed_fix"`
		Verification  string   `json:"verification"`
		Resolution    string   `json:"resolution"`
	} `json:"entries"`
}

func TestQALedgerAndFrozenHTMLStayInSync(t *testing.T) {
	t.Parallel()

	root := findRootTestRepo(t)
	ledgerData, err := os.ReadFile(filepath.Join(root, ".work", "qa", "repomap-cli-improvements.json"))
	require.NoError(t, err)
	var ledger qaLedger
	require.NoError(t, json.Unmarshal(ledgerData, &ledger))
	assert.Equal(t, 1, ledger.SchemaVersion)
	require.NotEmpty(t, ledger.Entries)

	htmlData, err := os.ReadFile(filepath.Join(root, ".work", "qa", "repomap-cli-audit.html"))
	require.NoError(t, err)
	html := string(htmlData)
	seen := map[string]bool{}
	for _, entry := range ledger.Entries {
		assert.NotEmpty(t, entry.ID)
		assert.False(t, seen[entry.ID], "duplicate ledger ID %s", entry.ID)
		seen[entry.ID] = true
		assert.NotEmpty(t, entry.Command)
		assert.NotEmpty(t, entry.Flag)
		assert.NotEmpty(t, entry.Expected)
		assert.NotEmpty(t, entry.Observed)
		assert.NotEmpty(t, entry.Repro)
		assert.NotEmpty(t, entry.Severity)
		assert.NotEmpty(t, entry.OutputQuality)
		assert.Contains(t, []string{"CONFIRMED", "PARTIAL", "REFUTED"}, entry.Status)
		assert.NotEmpty(t, entry.ProposedFix)
		assert.NotEmpty(t, entry.Verification)
		assert.Contains(t, html, entry.ID)
	}
}
