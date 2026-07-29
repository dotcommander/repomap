package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var cliMatrixFlags = map[string][]string{
	"map":              {"artifact", "calls", "calls-include-tests", "calls-limit", "calls-threshold", "calls-use-binary", "consumed", "explain", "format", "help", "include-tests", "intent", "json", "json-legacy", "json-structured", "no-cache", "precise", "symbol-refs", "tokens"},
	"brief":            {"artifact", "help"},
	"audit hygiene":    {"artifact", "help", "json"},
	"audit brief":      {"artifact", "help", "intent", "json", "limit", "top-files"},
	"audit risks":      {"artifact", "help", "intent", "json", "limit", "top-files"},
	"audit surface":    {"artifact", "help", "intent", "json", "limit", "top-files"},
	"audit effects":    {"artifact", "help", "intent", "json", "kind", "limit", "paths-only", "top-files"},
	"cache status":     {"artifact", "cache-dir", "help", "json"},
	"cache warm":       {"artifact", "cache-dir", "help"},
	"find":             {"artifact", "file", "format", "help", "kind", "limit"},
	"impact":           {"artifact", "help", "json", "markdown"},
	"inventory":        {"artifact", "boundary", "help", "json"},
	"task":             {"artifact", "consumed", "help", "json", "tokens"},
	"context":          {"artifact", "calls", "calls-include-tests", "calls-limit", "file", "help", "json", "kind", "max-output-bytes", "max-output-lines", "max-source-lines", "precise"},
	"endpoint":         {"artifact", "help", "json", "max-output-lines"},
	"explain":          {"artifact", "help", "json"},
	"orphans":          {"artifact", "help", "json"},
	"init":             {"artifact", "force", "help", "no-config", "no-hook"},
	"lsp status":       {"artifact", "help", "json"},
	"refs":             {"artifact", "help", "json"},
	"def":              {"artifact", "help", "json"},
	"hover":            {"artifact", "help", "json"},
	"symbols":          {"artifact", "help", "json"},
	"serve":            {"artifact", "help"},
	"commit analyze":   {"artifact", "confidence", "help", "pretty", "tag", "tmpdir"},
	"commit execute":   {"artifact", "dry-run", "help", "json", "no-release", "plan-file", "push", "release-notes-from", "skip-fix", "tag"},
	"commit prep":      {"allow-large", "artifact", "help", "json", "no-review", "tag"},
	"commit finish":    {"artifact", "decisions", "help", "json", "prep-token", "push", "tag"},
	"commit auto":      {"allow-large", "artifact", "decisions", "force-mode", "help", "no-review", "tag"},
	"commit-preflight": {"artifact", "help"},
}

func TestCLIModelMatrixCoversEveryExecutableLeafAndFlag(t *testing.T) {
	t.Parallel()

	parser := parserForInventoryTest(t)
	actual := liveCLILeafFlags(parser.Model.Node)
	assert.Equal(t, 30, len(actual), "executable leaf count changed; update the matrix")
	assert.Equal(t, cliMatrixFlags, actual, "the live Kong model and QA matrix must evolve together")
}

func TestVisibleCLIFlagsAppearInHelpAndConfigurationDocs(t *testing.T) {
	t.Parallel()

	parser := parserForInventoryTest(t)
	root := findRootTestRepo(t)
	docs := readTestFile(t, root, "docs", "04-configuration.md")
	readme := readTestFile(t, root, "README.md")
	seen := map[string]bool{}
	for _, leaf := range parser.Model.Leaves(false) {
		command := matrixCommandName(leaf)
		if command == "map" {
			continue
		}
		var help strings.Builder
		args := append(strings.Fields(command), "--help")
		require.NoError(t, executeTest(t, args, &help, io.Discard), command)
		for _, group := range leaf.AllFlags(true) {
			for _, flag := range group {
				assert.Contains(t, help.String(), "--"+flag.Name, "%s help omits --%s", command, flag.Name)
				if flag.Name != "help" {
					seen[flag.Name] = true
				}
			}
		}
	}
	for flag := range seen {
		assert.Contains(t, docs, "--"+flag, "configuration docs omit visible --%s", flag)
		assert.Contains(t, readme, "--"+flag, "README omits visible --%s", flag)
	}
}

func parserForInventoryTest(t *testing.T) *kong.Kong {
	t.Helper()
	var tree rootCommand
	parser, err := newRootParser(&tree, context.Background(), &commandIO{stdout: io.Discard, stderr: io.Discard}, io.Discard, io.Discard)
	require.NoError(t, err)
	return parser
}

func liveCLILeafFlags(model *kong.Node) map[string][]string {
	out := map[string][]string{}
	for _, leaf := range model.Leaves(false) {
		name := matrixCommandName(leaf)
		flags := []string{}
		for _, group := range leaf.AllFlags(false) {
			for _, flag := range group {
				flags = append(flags, flag.Name)
			}
		}
		slices.Sort(flags)
		out[name] = flags
	}
	return out
}

func matrixCommandName(leaf *kong.Node) string {
	path := strings.TrimPrefix(leaf.Path(), "repomap ")
	if leaf.Hidden && leaf.Name == "map" {
		return "map"
	}
	return path
}

func readTestFile(t *testing.T, parts ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(parts...))
	require.NoError(t, err)
	return string(data)
}
