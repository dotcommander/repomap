package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLargestRankedEncodingKeepsWholeRecords(t *testing.T) {
	t.Parallel()

	ranked := []repomap.RankedFile{
		{FileSymbols: &repomap.FileSymbols{Path: "a.go"}},
		{FileSymbols: &repomap.FileSymbols{Path: "b.go"}},
		{FileSymbols: &repomap.FileSymbols{Path: "c.go"}},
	}
	encode := func(selected []repomap.RankedFile) ([]byte, error) {
		var out bytes.Buffer
		out.WriteString("map\n")
		for _, file := range selected {
			fmt.Fprintln(&out, file.Path)
		}
		return out.Bytes(), nil
	}

	data, err := largestRankedEncoding(3, ranked, encode)
	require.NoError(t, err)
	assert.Equal(t, "map\na.go\n", string(data))
	assert.LessOrEqual(t, encodedTokens(data), 3)
}

func TestWriteRankedWithinBudgetFailsBeforeWritingMinimumEnvelope(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := writeRankedWithinBudget(&stdout, 1, nil, func([]repomap.RankedFile) ([]byte, error) {
		return []byte("minimum envelope\n"), nil
	})
	require.Error(t, err)
	assert.Empty(t, stdout.String())
}

func TestMapFormatsHonorCompleteEncodedBudget(t *testing.T) {
	t.Parallel()

	root := writeBudgetFixture(t)
	tests := []struct {
		name     string
		args     []string
		jsonLike bool
	}{
		{name: "enriched"},
		{name: "compact", args: []string{"--format", "compact"}},
		{name: "verbose", args: []string{"--format", "verbose"}},
		{name: "detail", args: []string{"--format", "detail"}},
		{name: "lines", args: []string{"--format", "lines"}},
		{name: "xml", args: []string{"--format", "xml"}},
		{name: "json", args: []string{"--json"}, jsonLike: true},
		{name: "json structured", args: []string{"--json-structured"}, jsonLike: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := append([]string{"--tokens", "256"}, tc.args...)
			args = append(args, root)
			var stdout bytes.Buffer
			require.NoError(t, executeTest(t, args, &stdout, io.Discard))
			assert.LessOrEqual(t, encodedTokens(stdout.Bytes()), 256)
			if tc.jsonLike {
				var decoded any
				require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
			}
		})
	}
}

func TestMapBudgetFailureLeavesStdoutEmpty(t *testing.T) {
	t.Parallel()

	root := writeBudgetFixture(t)
	var stdout bytes.Buffer
	err := executeTest(t, []string{"--tokens", "1", root}, &stdout, io.Discard)
	require.Error(t, err)
	assert.Empty(t, stdout.String())
}

func TestStructuredBudgetPreservesRepositoryTotalsAndAccountsForOmissions(t *testing.T) {
	t.Parallel()

	root := writeBudgetFixture(t)
	var stdout bytes.Buffer
	require.NoError(t, executeTest(t, []string{"--tokens", "256", "--json-structured", root}, &stdout, io.Discard))
	var output repomap.StructuredOutput
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &output))
	assert.Equal(t, 16, output.Totals.Files)
	assert.Equal(t, output.Totals.Files, len(output.Files)+output.FilesOmitted)
	if output.FilesOmitted > 0 {
		assert.NotEmpty(t, output.FilesOmittedReason)
	}
}

func writeBudgetFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module example.com/budget\n\ngo 1.26\n"),
		0o644,
	))
	for i := range 16 {
		source := fmt.Sprintf(
			"package budget\n\n// Type%d owns budget fixture behavior.\ntype Type%d struct { Value string }\n\n// Run%d executes fixture behavior.\nfunc Run%d(value string) string { return value }\n",
			i, i, i, i,
		)
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("file_%02d.go", i)), []byte(source), 0o644))
	}
	return root
}
