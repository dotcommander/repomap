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
		{
			FileSymbols: &FileSymbols{
				Path:        "internal/orphan/thing.py",
				Language:    "python",
				Package:     "orphan",
				ParseMethod: "regex",
				Symbols: []Symbol{
					{Name: "Thing", Kind: "class", Exported: true, Dead: true},
				},
			},
			Score:     70,
			DependsOn: 4,
			Untested:  true,
		},
	}

	report := m.AuditRisks(0)

	require.Len(t, report.Files, 3)
	assert.Equal(t, "cmd/app/main.go", report.Files[0].Path)
	assert.Contains(t, report.Files[0].Lanes, "cli-ux")
	assert.Contains(t, report.Files[0].Lanes, "api-contracts")
	assert.Contains(t, report.Files[0].Lanes, "large-functions")
	assert.Contains(t, report.Files[1].Lanes, "coupling")
	assert.Contains(t, report.Files[1].Lanes, "test-risk")
	assert.Contains(t, report.Files[1].Lanes, "dead-code")
	assert.Contains(t, report.Files[1].Lanes, "parse-fidelity")

	var laneNames []string
	for _, lane := range report.Lanes {
		laneNames = append(laneNames, lane.Name)
	}
	assert.Contains(t, laneNames, "cli-ux")
	assert.Contains(t, laneNames, "architecture")
	assert.Contains(t, laneNames, "coupling")
	assert.Contains(t, laneNames, "test-risk")
	assert.Contains(t, laneNames, "dead-code")
	assert.Contains(t, laneNames, "parse-fidelity")
	assert.NotContains(t, laneNames, "tests")
}

func TestAuditSurfaceAndEffectsExtractStaticPackets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	_ = context.Background()
	go func() {}()
	_, _ = io.ReadAll(os.Stdin)
	_, _ = io.ReadAll(io.LimitReader(os.Stdin, 1024))
	fmt.Fprintln(os.Stderr, "done")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/audit\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package main\nfunc TestIgnored() {}\n"), 0o644))

	m := New(root, DefaultConfig())
	m.ranked = []RankedFile{
		{FileSymbols: &FileSymbols{Path: "main.go", Language: "go", Package: "main"}, Score: 100},
		{FileSymbols: &FileSymbols{Path: "main_test.go", Language: "go", Package: "main"}, Score: 100},
	}

	surface, err := m.AuditSurface(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, surface.Files, 2)
	assert.NotEmpty(t, surface.DependencyManifests)
	assert.Equal(t, "go.mod", surface.DependencyManifests[0].Path)
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
	assert.Contains(t, effects.Files[0].Lanes, "lifecycle-concurrency")
	assert.Contains(t, effects.Files[0].Lanes, "performance")

	var readAllHits int
	for _, effect := range effects.Files[0].Effects {
		if effect.Kind == "unbounded-read" {
			readAllHits++
		}
	}
	assert.Equal(t, 1, readAllHits, "io.ReadAll wrapped in io.LimitReader should not be reported")

	brief, err := m.AuditBrief(context.Background(), 0)
	require.NoError(t, err)
	assert.NotEmpty(t, brief.Risks.Files)
	assert.NotEmpty(t, brief.Surface.Files)
	assert.NotEmpty(t, brief.Effects.Files)
	assert.NotEmpty(t, brief.FirstReadQueue)
	assertAuditReadGroup(t, brief.FirstReadQueue, "dependency-policy")
	assertAuditReadGroup(t, brief.FirstReadQueue, "lifecycle-concurrency")
	assertAuditReadGroup(t, brief.FirstReadQueue, "resource-bounds")
}

func assertAuditReadGroup(t *testing.T, groups []AuditReadGroup, name string) {
	t.Helper()
	for _, group := range groups {
		if group.Group == name {
			return
		}
	}
	t.Fatalf("expected audit read group %q in %#v", name, groups)
}

func runGitForAuditTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}
