package repomap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditHygieneReportsIgnoredAndUntrackedSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runGitForAuditTest(t, root, "init")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "untracked.go"), []byte("package main\n\nfunc Extra() {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "ignored"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ignored", "local.go"), []byte("package ignored\n\nfunc Local() {}\n"), 0o644))
	runGitForAuditTest(t, root, "add", ".gitignore", "main.go")

	report, err := AuditHygiene(context.Background(), root)
	require.NoError(t, err)

	assert.True(t, report.GitAvailable)
	assert.Equal(t, 1, report.Counts.TrackedSource)
	assert.Equal(t, []string{"untracked.go"}, report.UntrackedCode)
	assert.Equal(t, []string{"ignored/local.go"}, report.IgnoredSource)
	require.Len(t, report.Issues, 2)
	assert.Equal(t, "ignored_source_file", report.Issues[0].ID)
	assert.Equal(t, "high", report.Issues[0].Severity)
	assert.Equal(t, "untracked_source_file", report.Issues[1].ID)
}

func TestAuditRisksMapsFilesToAuditLanes(t *testing.T) {
	t.Parallel()

	m := New("/repo", DefaultConfig())
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:        "cmd/app/main.go",
				Language:    "go",
				Package:     "main",
				ParseMethod: "go_ast",
				Symbols:     []Symbol{{Name: "main", Kind: "function", Line: 10, EndLine: 95}},
			},
			Score:      60,
			Boundaries: []string{"HTTP"},
		},
		{
			FileSymbols: &FileSymbols{
				Path:        "internal/domain/models.go",
				Language:    "go",
				Package:     "domain",
				ParseMethod: "go_ast",
				Symbols:     []Symbol{{Name: "Request", Kind: "struct", Exported: true}},
			},
			Score:      90,
			ImportedBy: 8,
		},
	}

	report := m.AuditRisks(0)

	require.Len(t, report.Files, 2)
	assert.Equal(t, "cmd/app/main.go", report.Files[0].Path)
	assert.Contains(t, report.Files[0].Lanes, "cli-ux")
	assert.Contains(t, report.Files[0].Lanes, "api-contracts")
	assert.Contains(t, report.Files[0].Lanes, "large-functions")
	assert.Contains(t, report.Files[1].Lanes, "architecture")

	var laneNames []string
	for _, lane := range report.Lanes {
		laneNames = append(laneNames, lane.Name)
	}
	assert.Contains(t, laneNames, "cli-ux")
	assert.Contains(t, laneNames, "architecture")
	assert.NotContains(t, laneNames, "tests")
}

func TestAuditSurfaceAndEffectsExtractStaticPackets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := `package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
)

type Config struct {
	Token string ` + "`json:\"token\"`" + `
}

func main() {
	cmd := &cobra.Command{Use: "serve"}
	_ = cmd
	_ = os.Getenv("API_TOKEN")
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {})
	_ = os.WriteFile("out.json", []byte("{}"), 0o644)
	_ = exec.Command("git", "status").Run()
	_ = json.NewEncoder(os.Stdout).Encode(Config{})
	fmt.Fprintln(os.Stderr, "done")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package main\nfunc TestIgnored() {}\n"), 0o644))

	m := New(root, DefaultConfig())
	m.ranked = []RankedFile{
		{FileSymbols: &FileSymbols{Path: "main.go", Language: "go", Package: "main"}, Score: 100},
		{FileSymbols: &FileSymbols{Path: "main_test.go", Language: "go", Package: "main"}, Score: 100},
	}

	surface, err := m.AuditSurface(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, surface.Files, 1)
	assert.Equal(t, "main.go", surface.Files[0].Path)
	assert.NotEmpty(t, surface.Commands)
	assert.NotEmpty(t, surface.EnvVars)
	assert.NotEmpty(t, surface.SchemaFields)
	assert.NotEmpty(t, surface.Routes)
	assert.NotEmpty(t, surface.Outputs)

	effects, err := m.AuditEffects(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, effects.Files, 1)
	assert.Equal(t, "main.go", effects.Files[0].Path)
	assert.Contains(t, effects.Files[0].Lanes, "data-integrity")
	assert.Contains(t, effects.Files[0].Lanes, "error-handling")
	assert.Contains(t, effects.Files[0].Lanes, "api-contracts")

	brief, err := m.AuditBrief(context.Background(), 0)
	require.NoError(t, err)
	assert.NotEmpty(t, brief.Risks.Files)
	assert.NotEmpty(t, brief.Surface.Files)
	assert.NotEmpty(t, brief.Effects.Files)
	assert.NotEmpty(t, brief.FirstReadQueue)
}

func runGitForAuditTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}
