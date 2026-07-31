package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findRootTestRepo walks up from cwd to locate the repo root (go.mod).
func findRootTestRepo(t *testing.T) string {
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

// buildTestMap builds and returns a repomap.Map over the given directory.
func buildTestMap(t *testing.T, root string) *repomap.Map {
	t.Helper()
	cfg := repomap.Config{MaxTokens: 8192, MaxTokensNoCtx: 8192}
	m := repomap.New(root, cfg)
	require.NoError(t, m.Build(context.Background()))
	return m
}

// TestPrintJSON_DefaultEmitsEnvelope verifies that --json (no --json-legacy)
// produces {"schema_version":1,"lines":[...]} and not a bare array.
func TestPrintJSON_DefaultEmitsEnvelope(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)

	var buf bytes.Buffer
	require.NoError(t, printJSON(&buf, m, false))

	var out jsonOutput
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out), "output must unmarshal into jsonOutput envelope")
	assert.Equal(t, 1, out.SchemaVersion, "schema_version must be 1")
	assert.NotEmpty(t, out.Lines, "lines must be non-empty")
}

// TestPrintJSON_LegacyEmitsBareArray verifies that --json --json-legacy
// produces a bare JSON array (no envelope object).
func TestPrintJSON_LegacyEmitsBareArray(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)

	var buf bytes.Buffer
	require.NoError(t, printJSON(&buf, m, true))

	// Must parse as bare array.
	var lines []string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &lines), "legacy output must unmarshal as bare []string")
	assert.NotEmpty(t, lines, "lines must be non-empty")

	// Must NOT be an object with schema_version — first byte must be '['.
	assert.Equal(t, byte('['), bytes.TrimSpace(buf.Bytes())[0], "legacy output must start with '['")
}

// TestRenderCallsOutput_JSONEmitsEnvelope verifies that --calls --json
// produces {"schema_version":1,"lines":[...]} via renderCallsOutput.
func TestRenderCallsOutput_JSONEmitsEnvelope(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)
	ranked := m.Ranked()
	callers := repomap.SymbolCallers{} // empty — no gopls needed

	var buf bytes.Buffer
	require.NoError(t, renderCallsOutput(&buf, io.Discard, m, "compact", true, false, ranked, callers, 10))

	var out jsonOutput
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out), "calls --json output must unmarshal into jsonOutput envelope")
	assert.Equal(t, 1, out.SchemaVersion, "schema_version must be 1")
	assert.NotEmpty(t, out.Lines, "lines must be non-empty")
}

// TestRenderCallsOutput_JSONLegacyEmitsBareArray verifies that --calls --json --json-legacy
// produces a bare JSON array via renderCallsOutput.
func TestRenderCallsOutput_JSONLegacyEmitsBareArray(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)
	ranked := m.Ranked()
	callers := repomap.SymbolCallers{}

	var buf bytes.Buffer
	require.NoError(t, renderCallsOutput(&buf, io.Discard, m, "compact", true, true, ranked, callers, 10))

	var lines []string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &lines), "calls legacy output must unmarshal as bare []string")
	assert.NotEmpty(t, lines, "lines must be non-empty")
	assert.Equal(t, byte('['), bytes.TrimSpace(buf.Bytes())[0], "legacy output must start with '['")
}

// TestJSONEnvelopeShape verifies the exact field names in the JSON output
// match what downstream consumers expect (schema_version, lines).
func TestJSONEnvelopeShape(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)

	var buf bytes.Buffer
	require.NoError(t, printJSON(&buf, m, false))

	// Parse as generic map to check field names exactly.
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))

	schemaRaw, hasSchema := doc["schema_version"]
	require.True(t, hasSchema, "output must contain schema_version field")
	var version int
	require.NoError(t, json.Unmarshal(schemaRaw, &version))
	assert.Equal(t, 1, version)

	linesRaw, hasLines := doc["lines"]
	require.True(t, hasLines, "output must contain lines field")
	var ls []string
	require.NoError(t, json.Unmarshal(linesRaw, &ls))
	assert.NotEmpty(t, ls)

	// Envelope must have exactly schema_version + lines — no extra keys.
	assert.Len(t, doc, 2, "envelope must have exactly two top-level keys: schema_version and lines")
}

// TestJSONLegacySchemaVersionAbsent verifies --json-legacy output starts with '[' (bare array).
func TestJSONLegacySchemaVersionAbsent(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)

	var buf bytes.Buffer
	require.NoError(t, printJSON(&buf, m, true))

	raw := bytes.TrimSpace(buf.Bytes())
	assert.Equal(t, byte('['), raw[0], "legacy output must start with '[' (bare array)")
}

// TestRootCmd_JSONFlagRegistered verifies the CLI registers --json and --json-legacy flags.
func TestRootCmd_JSONFlagRegistered(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	require.NoError(t, executeTest(t, []string{"--help"}, &out, io.Discard))
	assert.Contains(t, out.String(), "--json ")
	assert.Contains(t, out.String(), "schema-versioned JSON envelope")
	assert.Contains(t, out.String(), "--json-legacy")
	assert.Contains(t, out.String(), "pre-v0.7.0")
}

// TestRootCmd_JSONLegacyHasNoShortForm verifies --json-legacy has no short flag form.
func TestRootCmd_JSONLegacyHasNoShortForm(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	require.NoError(t, executeTest(t, []string{"--help"}, &out, io.Discard))
	assert.NotContains(t, out.String(), "-j, --json-legacy")
}

func TestRootCmdArtifactWritesOutputFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	artifact := filepath.Join(t.TempDir(), "repomap.md")

	var stdout bytes.Buffer
	require.NoError(t, executeTest(t, []string{"--artifact", artifact, root}, &stdout, io.Discard))
	assert.Empty(t, stdout.String())
	data, err := os.ReadFile(artifact)
	require.NoError(t, err)
	assert.Contains(t, string(data), "main.go")
}

// TestCompactModeOrientation_NamesOnly verifies that StringCompact (the -f compact backend)
// produces lean output (names only, no function signatures).
func TestCompactModeOrientation_NamesOnly(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)

	out := m.StringCompact()
	require.NotEmpty(t, out, "compact output must not be empty")

	// Compact mode must NOT include function signatures (parenthesised param lists).
	// context.Context is a typed parameter that appears in enriched signatures
	// but not in the lean names-only compact output.
	assert.NotContains(t, out, "context.Context",
		"compact mode must not include typed signatures")
}

// TestCompactModeOrientation_ContainsNames verifies that compact mode
// includes exported symbol names from the repomap package itself.
func TestCompactModeOrientation_ContainsNames(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)

	out := m.StringCompact()
	// New is the top-ranked exported function from repomap.go — stable canary for compact mode.
	assert.Contains(t, out, "New",
		"compact mode must include exported symbol names")
}

// TestDefaultModeContainsSymbols verifies that the default format (m.String()) includes
// exported symbol names and signatures — regression guard ensuring Items 1-4 behaviour is intact.
func TestDefaultModeContainsSymbols(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := repomap.New(root, repomap.Config{MaxTokens: 32768, MaxTokensNoCtx: 8192})
	require.NoError(t, m.Build(context.Background()))

	out := m.String()
	require.NotEmpty(t, out, "default output must not be empty")

	// repomap.go is a top-ranked file that should appear at level 2 in the default output.
	// It has a known exported function with a typed signature.
	assert.Contains(t, out, "New",
		"default mode must include exported symbol names from top-ranked files")

	// Default mode must include at least some parenthesised signatures — the hallmark
	// of the enriched format vs. the lean compact format.
	assert.Contains(t, out, "func (*Map)",
		"default mode must include method signatures in enriched format")

	// Default mode must append a " :N" start-line anchor to symbol lines so an LLM
	// can write precise edits in its first turn. Use a regexp — line numbers drift.
	assert.Regexp(t, ` :\d+`, out,
		"default mode must include a start-line anchor for symbols")
}

// TestMapFormatDefault verifies the parser's explicit enriched default.
func TestMapFormatDefault(t *testing.T) {
	t.Parallel()

	var tree rootCommand
	parser, err := newRootParser(&tree, t.Context(), &commandIO{stdout: io.Discard, stderr: io.Discard}, io.Discard, io.Discard)
	require.NoError(t, err)
	_, err = parser.Parse([]string{"."})
	require.NoError(t, err)
	assert.Equal(t, "enriched", tree.Map.Format)
}

// TestCompactModeVerboseUnaffected verifies that verbose output is richer than compact —
// regression guard ensuring -f verbose is not degraded by the compact mode split.
func TestCompactModeVerboseUnaffected(t *testing.T) {
	t.Parallel()
	root := findRootTestRepo(t)
	m := buildTestMap(t, root)

	verbose := m.StringVerbose()
	compact := m.StringCompact()

	// Verbose must be richer than compact (more content).
	assert.Greater(t, len(verbose), len(compact),
		"verbose output must be longer than compact output")
}
