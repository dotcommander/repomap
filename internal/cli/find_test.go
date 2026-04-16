package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findCLITestRoot walks up from cwd to locate the repo root (go.mod).
func findCLITestRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("cannot find repo root")
		}
		dir = parent
	}
}

func runFindCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := newFindCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestFindCmd_TextOutput(t *testing.T) {
	t.Parallel()
	root := findCLITestRoot(t)
	out, err := runFindCmd(t, "FindSymbol", root)
	require.NoError(t, err)
	// score column should show 100 for exact match
	assert.Contains(t, out, "100")
	// file column should reference find.go where FindSymbol lives
	assert.Contains(t, out, "find.go")
	// text format: SCORE  FILE:LINE  KIND  SIGNATURE — signature for FindSymbol
	// contains the parameter list, which includes "name"
	assert.Contains(t, out, "name")
}

func TestFindCmd_JSONOutput(t *testing.T) {
	t.Parallel()
	root := findCLITestRoot(t)
	out, err := runFindCmd(t, "FindSymbol", root, "--format=json")
	require.NoError(t, err)

	var matches []repomap.SymbolMatch
	require.NoError(t, json.Unmarshal([]byte(out), &matches))
	require.NotEmpty(t, matches)
	assert.Equal(t, float64(100), matches[0].Score)
}

func TestFindCmd_LimitFlag(t *testing.T) {
	t.Parallel()
	root := findCLITestRoot(t)
	// "New" should produce many hits; limit to 1
	out, err := runFindCmd(t, "New", root, "--limit=1")
	require.NoError(t, err)
	lines := nonEmptyLines(out)
	assert.Len(t, lines, 1)
}

func TestFindCmd_KindFlag(t *testing.T) {
	t.Parallel()
	root := findCLITestRoot(t)
	out, err := runFindCmd(t, "Config", root, "--kind=struct", "--format=json")
	require.NoError(t, err)

	var matches []repomap.SymbolMatch
	require.NoError(t, json.Unmarshal([]byte(out), &matches))
	require.NotEmpty(t, matches)
	for _, m := range matches {
		assert.Equal(t, "struct", m.Symbol.Kind)
	}
}

func TestFindCmd_NoResults(t *testing.T) {
	t.Parallel()
	root := findCLITestRoot(t)
	out, err := runFindCmd(t, "NoSuchSymbolFoo99999", root)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(out))
}

// nonEmptyLines splits output into non-blank lines.
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
