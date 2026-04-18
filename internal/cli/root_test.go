package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	cfg := repomap.Config{MaxTokens: 2048, MaxTokensNoCtx: 2048}
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
	require.NoError(t, renderCallsOutput(&buf, m, "compact", true, false, ranked, callers, 10))

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
	require.NoError(t, renderCallsOutput(&buf, m, "compact", true, true, ranked, callers, 10))

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
	cmd := newRootCmd()

	jsonFlag := cmd.Flags().Lookup("json")
	require.NotNil(t, jsonFlag, "--json flag must be registered")

	legacyFlag := cmd.Flags().Lookup("json-legacy")
	require.NotNil(t, legacyFlag, "--json-legacy flag must be registered")
	assert.Contains(t, legacyFlag.Usage, "pre-v0.7.0", "--json-legacy help text must mention pre-v0.7.0")
}

// TestRootCmd_JSONLegacyHasNoShortForm verifies --json-legacy has no short flag form.
func TestRootCmd_JSONLegacyHasNoShortForm(t *testing.T) {
	t.Parallel()
	cmd := newRootCmd()

	legacyFlag := cmd.Flags().Lookup("json-legacy")
	require.NotNil(t, legacyFlag)
	assert.Empty(t, legacyFlag.Shorthand, "--json-legacy must have no short form")
}
