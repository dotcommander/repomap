package cli

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootParserUsesLiveCommandModel(t *testing.T) {
	t.Parallel()

	var tree rootCommand
	parser, err := newRootParser(&tree, context.Background(), &commandIO{stdout: io.Discard, stderr: io.Discard}, io.Discard, io.Discard)
	require.NoError(t, err)
	require.NotNil(t, parser.Model)

	commands := make([]string, 0, len(parser.Model.Children))
	for _, command := range parser.Model.Children {
		commands = append(commands, command.Name)
	}
	assert.Contains(t, commands, "find")
	assert.Contains(t, commands, "audit")

	_, err = parser.Parse([]string{"find", "--format=json", "FindSymbol"})
	require.NoError(t, err)
	assert.Equal(t, "json", tree.Find.Format)
}

func TestExecuteValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "map format", args: []string{"--format=unknown", "."}, want: "must be one of"},
		{name: "tokens", args: []string{"--tokens=0", "."}, want: "--tokens must be greater than zero"},
		{name: "legacy json requires json", args: []string{"--json-legacy", "."}, want: "--json-legacy requires --json"},
		{name: "structured json exclusive", args: []string{"--json", "--json-structured", "."}, want: "--json-structured is mutually exclusive"},
		{name: "binary callers migration", args: []string{"--calls-use-binary", "."}, want: "no longer supported"},
		{name: "negative caller limit", args: []string{"--calls-limit=-1", "."}, want: "--calls-limit must be zero or greater"},
		{name: "find format", args: []string{"find", "--format=yaml", "FindSymbol"}, want: "must be one of"},
		{name: "negative find limit", args: []string{"find", "--limit=-1", "FindSymbol"}, want: "--limit must be zero or greater"},
		{name: "context source bound", args: []string{"context", "--max-source-lines=0", "FindSymbol"}, want: "--max-source-lines must be greater than zero"},
		{name: "negative context output", args: []string{"context", "--max-output-bytes=-1", "FindSymbol"}, want: "--max-output-bytes must be zero or greater"},
		{name: "negative endpoint output", args: []string{"endpoint", "--max-output-lines=-1"}, want: "--max-output-lines must be zero or greater"},
		{name: "negative audit limit", args: []string{"audit", "effects", "--limit=-1"}, want: "--limit must be zero or greater"},
		{name: "invalid audit kind", args: []string{"audit", "effects", "--kind=network"}, want: "--kind must be one of"},
		{name: "confidence range", args: []string{"commit", "analyze", "--confidence=1.1"}, want: "--confidence must be between 0 and 1"},
		{name: "force mode", args: []string{"commit", "auto", "--force-mode=bogus"}, want: "--force-mode must be FULL or LOCAL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := executeTest(t, tc.args, io.Discard, io.Discard)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidationPreservesDocumentedZeroUnlimited(t *testing.T) {
	t.Parallel()

	assert.NoError(t, (&mapCommand{Tokens: 1, CallsLimit: 0}).Validate())
	assert.NoError(t, (&findCommand{Limit: 0}).Validate())
	assert.NoError(t, (&contextCommand{MaxSourceLines: 1, MaxOutputLines: 0, MaxOutputBytes: 0, CallsLimit: 0}).Validate())
	assert.NoError(t, (&endpointCommand{MaxOutputLines: 0}).Validate())
	assert.NoError(t, (&auditMapOptions{Limit: 0, TopFiles: 0}).Validate())
}
