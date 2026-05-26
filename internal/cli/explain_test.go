package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExplainCmdUsesConfiguredOutputWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	root := findRootTestRepo(t)
	cmd := newExplainCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{filepath.Join(root, "ranker.go"), "--json"})

	require.NoError(t, cmd.Execute())
	require.NotEmpty(t, buf.String())

	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	require.Contains(t, doc, "file")
	require.Contains(t, doc, "score")
}
