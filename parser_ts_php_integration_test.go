//go:build !notreesitter

package repomap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phpIntegrationSetup creates a temp git repo containing integration.php and
// returns the dir and the built Map. The fixture is read from testdata/php/.
func phpIntegrationSetup(t *testing.T) (string, *Map) {
	t.Helper()

	dir := t.TempDir()

	// Copy the fixture into the temp dir.
	src, err := os.ReadFile(filepath.Join("testdata", "php", "integration.php"))
	require.NoError(t, err, "read integration fixture")
	dst := filepath.Join(dir, "integration.php")
	require.NoError(t, os.WriteFile(dst, src, 0o644))

	// Init git so scanGit picks up the file via git ls-files.
	for _, args := range [][]string{
		{"init"},
		{"add", "."},
		{"-c", "user.email=t@t.com", "-c", "user.name=T", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		_ = cmd.Run()
	}

	m := New(dir, Config{MaxTokens: 8192, MaxTokensNoCtx: 8192})
	require.NoError(t, m.Build(context.Background()))

	return dir, m
}

// TestPHPIntegrationRender exercises the full scan→parse→rank→format pipeline
// on a realistic PHP fixture and validates each render mode for correctness,
// duplication absence, and PHPDoc rendering.
func TestPHPIntegrationRender(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping PHP integration render test in short mode")
	}

	_, m := phpIntegrationSetup(t)

	defaultOut := m.String()
	compactOut := m.StringCompact()
	verboseOut := m.StringVerbose()
	detailOut := m.StringDetail()

	// Emit the default output for human inspection when run with -v.
	t.Log("=== default render ===\n" + defaultOut)

	// ── File path appears in all outputs ──────────────────────────────────────
	for _, tc := range []struct {
		name string
		out  string
	}{
		{"default", defaultOut},
		{"compact", compactOut},
		{"verbose", verboseOut},
		{"detail", detailOut},
	} {
		assert.Contains(t, tc.out, "integration.php",
			"%s: file path must appear in output", tc.name)
	}

	// ── No [doc: n/a] tag ─────────────────────────────────────────────────────
	// PHP now has PHPDoc extraction — [doc: n/a] must not appear.
	assert.NotContains(t, defaultOut, "[doc: n/a]",
		"PHP files must not carry [doc: n/a] — PHPDoc extraction is supported")

	// ── Expected class and type names ─────────────────────────────────────────
	for _, name := range []string{"OrderService", "OrderStatus", "Loggable", "OrderRepositoryInterface"} {
		assert.Contains(t, defaultOut, name, "default: symbol %s must appear", name)
		assert.Contains(t, verboseOut, name, "verbose: symbol %s must appear", name)
	}

	// ── No keyword duplication ────────────────────────────────────────────────
	// PHP signatures include the keyword already; rendering must not double-print.
	assert.NotContains(t, defaultOut, "class class ",
		"default: 'class class' duplication must not occur")
	assert.NotContains(t, defaultOut, "interface interface ",
		"default: 'interface interface' duplication must not occur")
	assert.NotContains(t, defaultOut, "trait trait ",
		"default: 'trait trait' duplication must not occur")
	assert.NotContains(t, defaultOut, "enum enum ",
		"default: 'enum enum' duplication must not occur")
	assert.NotContains(t, defaultOut, "function function ",
		"default: 'function function' duplication must not occur")

	// ── Signature correctness ─────────────────────────────────────────────────
	assert.Contains(t, defaultOut, "class OrderService extends BaseService implements Identifiable",
		"default: class signature with extends+implements must be rendered verbatim")
	assert.Contains(t, defaultOut, "enum OrderStatus: string implements HasLabel",
		"default: enum signature with backing type + implements must be rendered verbatim")
	assert.Contains(t, defaultOut, "interface OrderRepositoryInterface",
		"default: interface name must appear in output")
	assert.Contains(t, defaultOut, "trait Loggable",
		"default: trait signature must appear in output")

	// ── PHPDoc first sentences ────────────────────────────────────────────────
	// Class and enum both have /** ... */ blocks — their first sentences must appear.
	assert.Contains(t, defaultOut, "Manages order lifecycle",
		"default: PHPDoc first sentence for OrderService must appear")
	assert.Contains(t, defaultOut, "Represents the current status",
		"default: PHPDoc first sentence for OrderStatus must appear")

	// ── Promoted properties appear ────────────────────────────────────────────
	// logger and queueName are promoted — their signatures must be in default output.
	assert.Contains(t, defaultOut, "public readonly LoggerInterface $logger",
		"default: promoted property 'logger' must be rendered with full signature")
	assert.Contains(t, defaultOut, "public readonly string $queueName",
		"default: promoted property 'queueName' must be rendered with full signature")

	// ── Protected method is excluded from default (unexported) ───────────────
	// loadById is protected → Exported=false → must not appear in default output.
	assert.NotContains(t, defaultOut, "loadById",
		"default: protected method must be filtered out (unexported)")

	// ── Protected method IS visible in verbose (all symbols shown) ───────────
	assert.Contains(t, verboseOut, "loadById",
		"verbose: protected method must appear (verbose shows all symbols)")

	// ── Compact is shorter than default ───────────────────────────────────────
	assert.Less(t, len(compactOut), len(defaultOut),
		"compact output must be substantially shorter than default (signal density check)")

	// ── Free function at file level ───────────────────────────────────────────
	assert.Contains(t, defaultOut, "findOrdersByStatus",
		"default: file-level free function must appear")

	// ── Enum case names ───────────────────────────────────────────────────────
	// Cases are unexported in the default render (they live inside the enum block);
	// at least one should appear in verbose where all symbols show.
	assert.True(t,
		strings.Contains(verboseOut, "Pending") || strings.Contains(verboseOut, "Confirmed"),
		"verbose: enum cases must appear")

	// ── Namespace note ───────────────────────────────────────────────────────
	// The namespace_definition node is not captured by the current php_tags.scm
	// query (spec hard limit: no query changes in this task). The namespace text
	// does not appear as a Symbol and therefore is not in any render output.
	// This comment documents the known gap; no assertion is made here.
}
