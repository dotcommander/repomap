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

// TestFullPipelineWithConfig exercises the complete scan→parse→rank→budget→format
// pipeline with multiple files, import relationships, and a blocklist config.
//
// Layout under the temp git repo:
//
//	cmd/main.go                   — entry point (entry boost +50)
//	pkg/api/handler.go            — HTTP layer importing pkg/api/db (boundary boost)
//	pkg/api/db/repo.go            — DB layer imported by handler (ImportedBy=1)
//	pkg/api/db/helpers.go         — low-level helpers imported by repo
//	internal/generated/gen.go     — generated code; blocklisted symbols filtered
//	internal/unused/orphan.go     — exported symbols, no importers, no tests (Untested)
func TestFullPipelineWithConfig(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()

	// ── go.mod ──────────────────────────────────────────────────────────────────
	writeTestFile(t, dir, "go.mod",
		"module example.com/fullpipeline\n\ngo 1.26\n")

	// ── cmd/main.go — entry point ────────────────────────────────────────────────
	writeTestFile(t, dir, filepath.Join("cmd", "main.go"), `package main

import "example.com/fullpipeline/pkg/api"

func main() {
	api.New().Run()
}
`)

	// ── pkg/api/handler.go — HTTP boundary, imports DB ──────────────────────────
	writeTestFile(t, dir, filepath.Join("pkg", "api", "handler.go"), `package api

import (
	"net/http"

	"example.com/fullpipeline/pkg/api/db"
)

// Handler is the HTTP entry point.
type Handler struct {
	repo *db.Repo
}

// New returns a new Handler.
func New() *Handler {
	return &Handler{repo: db.NewRepo()}
}

// Run starts the server.
func (h *Handler) Run() {
	http.ListenAndServe(":8080", nil) //nolint
}
`)

	// ── pkg/api/db/repo.go — DB layer ────────────────────────────────────────────
	writeTestFile(t, dir, filepath.Join("pkg", "api", "db", "repo.go"), `package db

// Repo is the repository.
type Repo struct{}

// NewRepo creates a Repo.
func NewRepo() *Repo { return &Repo{} }

// Find retrieves an entity by id.
func (r *Repo) Find(id int) string {
	return Format(id)
}
`)

	// ── pkg/api/db/helpers.go — low-level helpers in same db package ─────────────
	// Both repo.go and helpers.go share ImportPath "example.com/fullpipeline/pkg/api/db".
	// handler.go imports that path, so both files get ImportedBy >= 1.
	writeTestFile(t, dir, filepath.Join("pkg", "api", "db", "helpers.go"), `package db

import "strconv"

// Format converts an int to a display string.
func Format(n int) string {
	return strconv.Itoa(n)
}
`)

	// ── internal/generated/gen.go — symbols will be blocklisted ──────────────────
	// The blocklist blocks "pb_*" symbols; gen.go exports one pb_ func.
	writeTestFile(t, dir, filepath.Join("internal", "generated", "gen.go"), `package generated

// pb_Message is a generated symbol that should be filtered by the blocklist.
func pb_Message() string { return "" }

// GeneratedVersion is kept (not blocklisted).
const GeneratedVersion = "v1"
`)

	// ── internal/unused/orphan.go — no importers, no tests ──────────────────────
	writeTestFile(t, dir, filepath.Join("internal", "unused", "orphan.go"), `package unused

// OrphanFunc is exported but nobody imports this package.
func OrphanFunc() {}

// OrphanType is exported but nobody imports this package.
type OrphanType struct{}
`)

	// ── .repomap.yaml — blocklist generated pb_ symbols ──────────────────────────
	writeTestFile(t, dir, ".repomap.yaml",
		"method_blocklist:\n  - \"/^pb_/\"\n")

	// ── Git init + commit ────────────────────────────────────────────────────────
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "-c", "user.email=test@test.com", "-c", "user.name=Test",
		"commit", "-m", "init")

	// ── Build ────────────────────────────────────────────────────────────────────
	m := New(dir, Config{MaxTokens: 4096, MaxTokensNoCtx: 8192})
	require.NoError(t, m.Build(context.Background()))

	ranked := m.Ranked()
	require.NotEmpty(t, ranked, "Build must produce ranked files")

	byPath := make(map[string]RankedFile, len(ranked))
	for _, r := range ranked {
		byPath[r.Path] = r
	}

	// ── Assert: all non-generated files are present in ranked output ─────────────
	expectedPaths := []string{
		filepath.Join("cmd", "main.go"),
		filepath.Join("pkg", "api", "handler.go"),
		filepath.Join("pkg", "api", "db", "repo.go"),
		filepath.Join("pkg", "api", "db", "helpers.go"),
		filepath.Join("internal", "unused", "orphan.go"),
		filepath.Join("internal", "generated", "gen.go"),
	}
	for _, p := range expectedPaths {
		assert.Contains(t, byPath, p, "expected file in ranked results: %s", p)
	}

	// ── Assert: config filtering (blocklist) ─────────────────────────────────────
	// pb_Message matched /^pb_/ → must be absent from all symbols.
	for _, r := range ranked {
		for _, sym := range r.Symbols {
			assert.NotEqual(t, "pb_Message", sym.Name,
				"pb_Message must be removed by blocklist in file %s", r.Path)
		}
	}
	// GeneratedVersion is NOT blocklisted — it must still appear.
	genFile, ok := byPath[filepath.Join("internal", "generated", "gen.go")]
	if assert.True(t, ok, "generated/gen.go must appear in ranked output") {
		genSymNames := symbolNames(genFile)
		assert.Contains(t, genSymNames, "GeneratedVersion",
			"GeneratedVersion must survive the blocklist filter")
	}

	// ── Assert: ranking — entry boost for cmd/main.go ────────────────────────────
	main, ok := byPath[filepath.Join("cmd", "main.go")]
	require.True(t, ok, "cmd/main.go must appear in ranked output")
	assert.Equal(t, "entry", main.Tag, "cmd/main.go must carry the 'entry' tag")
	assert.GreaterOrEqual(t, main.Score, 50,
		"cmd/main.go must have score >= 50 from entry boost")

	// ── Assert: ranking — reference count on DB repo ─────────────────────────────
	// handler.go imports "example.com/fullpipeline/pkg/api/db", which is the
	// ImportPath for both repo.go and helpers.go (same package). Both should have
	// ImportedBy >= 1.
	repo, ok := byPath[filepath.Join("pkg", "api", "db", "repo.go")]
	require.True(t, ok, "pkg/api/db/repo.go must appear in ranked output")
	assert.GreaterOrEqual(t, repo.ImportedBy, 1,
		"pkg/api/db/repo.go must have ImportedBy >= 1 because handler.go imports its package")

	helpers, ok := byPath[filepath.Join("pkg", "api", "db", "helpers.go")]
	require.True(t, ok, "pkg/api/db/helpers.go must appear in ranked output")
	assert.GreaterOrEqual(t, helpers.ImportedBy, 1,
		"pkg/api/db/helpers.go must share the ImportedBy count from its package")

	// ── Assert: ranking — orphan file has zero importers ─────────────────────────
	orphan, ok := byPath[filepath.Join("internal", "unused", "orphan.go")]
	require.True(t, ok, "internal/unused/orphan.go must appear in ranked output")
	assert.Equal(t, 0, orphan.ImportedBy,
		"internal/unused/orphan.go must have ImportedBy=0 (nobody imports it)")

	// ── Assert: ranking — orphan is flagged Untested (no test coverage, has exports)
	assert.True(t, orphan.Untested,
		"internal/unused/orphan.go has exported symbols but no test file; must be Untested")

	// ── Assert: budget — BudgetFiles assigns DetailLevel at render time.
	// Call BudgetFiles directly on a copy of ranked to verify budget behaviour.
	// With 4096 tokens all six files should be included (DetailLevel >= 0).
	budgeted := BudgetFiles(append([]RankedFile(nil), ranked...), 4096)
	budgetByPath := make(map[string]RankedFile, len(budgeted))
	for _, r := range budgeted {
		budgetByPath[r.Path] = r
	}
	for _, r := range budgeted {
		assert.GreaterOrEqual(t, r.DetailLevel, 0,
			"with 4096 tokens no file should be omitted (path: %s)", r.Path)
	}
	// cmd/main.go is entry-boosted and ranks first — must get full detail (level >= 2).
	mainBudgeted := budgetByPath[filepath.Join("cmd", "main.go")]
	assert.GreaterOrEqual(t, mainBudgeted.DetailLevel, 2,
		"cmd/main.go (highest-ranked) must receive DetailLevel >= 2")

	// ── Assert: format output ─────────────────────────────────────────────────────
	out := m.String()
	require.NotEmpty(t, out, "String() must return non-empty output after Build")

	assert.True(t, strings.HasPrefix(out, "## Repository Map"),
		"output must start with '## Repository Map'")
	assert.Contains(t, out, filepath.Join("cmd", "main.go"),
		"output must contain cmd/main.go")
	// pb_Message was blocklisted — it must not appear in rendered output.
	assert.NotContains(t, out, "pb_Message",
		"blocklisted symbol pb_Message must not appear in rendered output")

	// StringVerbose shows all surviving symbols.
	verbose := m.StringVerbose()
	assert.Contains(t, verbose, "GeneratedVersion",
		"verbose output must contain GeneratedVersion (not blocklisted)")
	assert.Contains(t, verbose, "OrphanFunc",
		"verbose output must contain OrphanFunc from unused/orphan.go")
	assert.Contains(t, verbose, "Repo",
		"verbose output must contain Repo type from db/repo.go")
}

// writeTestFile creates parent directories and writes content into a temp repo.
func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

// runGitCmd runs a git command in dir, skipping on failure (non-blocking for setup).
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Logf("git %v: %v (non-fatal during test setup)", args, err)
	}
}

// symbolNames returns all symbol names from a RankedFile.
func symbolNames(r RankedFile) []string {
	names := make([]string, 0, len(r.Symbols))
	for _, s := range r.Symbols {
		names = append(names, s.Name)
	}
	return names
}
