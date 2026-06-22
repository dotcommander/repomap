package repomap

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestAuditSurfaceExtractsFrameworkRoles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := `package main

type User struct {
	ID   int    ` + "`gorm:\"primaryKey\"`" + `
	Name string ` + "`db:\"name\"`" + `
}

func register(q *queue, r *router) {
	q.RegisterJob("send-email", nil)
	q.NewTask("reindex", nil)
	r.RegisterPolicy("admin-only", nil)
	r.RequireRole("editor")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/roles\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "app.go"), []byte(source), 0o644))

	m := New(root, DefaultConfig())
	m.ranked = []RankedFile{
		{FileSymbols: &FileSymbols{Path: "app.go", Language: "go", Package: "main"}, Score: 100},
	}

	surface, err := m.AuditSurface(context.Background(), 0)
	require.NoError(t, err)

	jobNames := surfaceHitNames(surface.Jobs)
	assert.Contains(t, jobNames, "send-email")
	assert.Contains(t, jobNames, "reindex")

	modelNames := surfaceHitNames(surface.ModelFields)
	assert.Contains(t, modelNames, "primaryKey")
	assert.Contains(t, modelNames, "name")

	policyNames := surfaceHitNames(surface.Policies)
	assert.Contains(t, policyNames, "admin-only")
	assert.Contains(t, policyNames, "editor")

	var appFile *AuditSurfaceFile
	for i := range surface.Files {
		if surface.Files[i].Path == "app.go" {
			appFile = &surface.Files[i]
			break
		}
	}
	require.NotNil(t, appFile, "app.go should appear as a surface file")
	assert.Equal(t, "repomap:surface:app-go", appFile.ID)
	assert.Contains(t, appFile.Kinds, "job")
	assert.Contains(t, appFile.Kinds, "model-field")
	assert.Contains(t, appFile.Kinds, "policy")
}

func surfaceHitNames(hits []AuditSurfaceHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Name)
	}
	return out
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

func TestAuditBriefReviewPlan(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := `package main

import (
	"context"
	"os"
)

func main() {
	_ = context.Background()
	go func() {}()
	_ = os.WriteFile("out.json", []byte("{}"), 0o644)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/audit\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644))

	m := New(root, DefaultConfig())
	m.ranked = []RankedFile{
		{FileSymbols: &FileSymbols{Path: "main.go", Language: "go", Package: "main"}, Score: 100},
	}

	brief, err := m.AuditBrief(context.Background(), 0)
	require.NoError(t, err)
	require.NotEmpty(t, brief.ReviewPlan)

	lane := findReviewLane(t, brief.ReviewPlan, "lifecycle-concurrency")
	assert.Equal(t, lane.Lane, lane.Group, "group mirrors lane for shape stability")
	assert.NotEmpty(t, lane.Files)
	assert.NotEmpty(t, lane.Gates)
	assert.Contains(t, lane.Gates, "goroutine ownership")
	assert.Contains(t, lane.Verify, "go test -race ./...")
	assert.NotEmpty(t, lane.Why)
}

func TestBuildAuditReviewPlanSuppressesGoVerifyWhenNotGo(t *testing.T) {
	t.Parallel()

	queue := []AuditReadGroup{
		{Group: "lifecycle-concurrency", Lane: "lifecycle-concurrency", Reasons: []string{"goroutine launch"}, Files: []string{"a.py"}},
		{Group: "secret-and-crypto", Lane: "security", Reasons: []string{"crypto use"}, Files: []string{"b.py"}},
	}

	withGo := BuildAuditReviewPlan(queue, true)
	lifecycle := findReviewLane(t, withGo, "lifecycle-concurrency")
	assert.Equal(t, []string{"go test -race ./..."}, lifecycle.Verify)

	noGo := BuildAuditReviewPlan(queue, false)
	lifecycleNoGo := findReviewLane(t, noGo, "lifecycle-concurrency")
	assert.Empty(t, lifecycleNoGo.Verify, "Go-specific verify suppressed for non-Go targets")
	assert.NotEmpty(t, lifecycleNoGo.Gates, "gates still emitted regardless of language")

	security := findReviewLane(t, noGo, "security")
	assert.Empty(t, security.Verify, "review-only lane has no verify command")
	assert.NotEmpty(t, security.Gates)
}

func findReviewLane(t *testing.T, lanes []AuditReviewLane, name string) AuditReviewLane {
	t.Helper()
	for _, lane := range lanes {
		if lane.Lane == name {
			return lane
		}
	}
	t.Fatalf("expected review lane %q in %#v", name, lanes)
	return AuditReviewLane{}
}

func TestAuditRiskPacketsCarryIDsEvidenceAndCaveat(t *testing.T) {
	t.Parallel()

	m := New("/repo", DefaultConfig())
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path: "internal/domain/models.go", Language: "go", Package: "domain", ParseMethod: "go_ast",
				Symbols: []Symbol{{Name: "Request", Kind: "struct", Exported: true}},
			},
			Score: 90, ImportedBy: 8,
		},
		{
			FileSymbols: &FileSymbols{
				Path: "internal/orphan/thing.go", Language: "go", Package: "orphan", ParseMethod: "go_ast",
				Symbols: []Symbol{{Name: "Thing", Kind: "function", Exported: true, Dead: true}},
			},
			Score: 70, Untested: true,
		},
	}

	report := m.AuditRisks(0)
	assert.Equal(t, 2, report.SchemaVersion)

	models := findRiskFile(t, report.Files, "internal/domain/models.go")
	assert.Equal(t, "repomap:risk:internal-domain-models-go", models.ID)
	assert.Equal(t, "import_graph", models.EvidenceClass)
	assert.Equal(t, "high", models.Confidence)
	assert.Empty(t, models.Caveat)
	assert.Equal(t, "go test ./internal/domain/...", models.VerifyCmd)

	orphan := findRiskFile(t, report.Files, "internal/orphan/thing.go")
	require.Contains(t, orphan.Lanes, "dead-code")
	assert.NotEmpty(t, orphan.Caveat, "dead-code packet must carry external-consumer caveat")
	assert.Equal(t, "low", orphan.Confidence, "external-consumer caveat caps confidence at low")

	deadLane := findRiskLane(t, report.Lanes, "dead-code")
	assert.Equal(t, "repomap:lane:dead-code", deadLane.ID)
	assert.NotEmpty(t, deadLane.Caveat)

	queue := BuildAuditReadQueue(report, AuditSurfaceReport{}, AuditEffectReport{})
	g := findReadGroup(t, queue, "dead-export-surface")
	assert.Equal(t, "repomap:queue:dead-export-surface", g.ID)
	assert.NotEmpty(t, g.Caveat)
	assert.Equal(t, "low", g.Confidence)
}

func TestAuditSurfaceEmptyFilesSerializeAsArray(t *testing.T) {
	t.Parallel()

	m := New(t.TempDir(), DefaultConfig())
	m.ranked = nil

	surface, err := m.AuditSurface(context.Background(), 0)
	require.NoError(t, err)
	assert.NotNil(t, surface.Files)
	assert.Empty(t, surface.Files)
	assert.Equal(t, 2, surface.SchemaVersion)
	assert.NotEmpty(t, surface.FilesOmittedReason)

	data, err := json.Marshal(surface)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"files":[]`)
	assert.NotContains(t, string(data), `"files":null`)
}

func TestAuditReviewPlanRecordsTruncation(t *testing.T) {
	t.Parallel()

	files := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		files = append(files, fmt.Sprintf("pkg/file%02d.go", i))
	}
	queue := []AuditReadGroup{{Group: "cli-ux", Lane: "cli-ux", Reasons: []string{"flags"}, Files: files}}

	plan := BuildAuditReviewPlan(queue, true)
	lane := findReviewLane(t, plan, "cli-ux")
	assert.Equal(t, "repomap:review:cli-ux", lane.ID)
	assert.Len(t, lane.Files, 12)
	assert.Contains(t, lane.OmittedReason, "20")
}

func findRiskFile(t *testing.T, files []AuditFileRisk, path string) AuditFileRisk {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("expected risk file %q in %#v", path, files)
	return AuditFileRisk{}
}

func findRiskLane(t *testing.T, lanes []AuditLane, name string) AuditLane {
	t.Helper()
	for _, l := range lanes {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("expected risk lane %q in %#v", name, lanes)
	return AuditLane{}
}

func findReadGroup(t *testing.T, groups []AuditReadGroup, name string) AuditReadGroup {
	t.Helper()
	for _, g := range groups {
		if g.Group == name {
			return g
		}
	}
	t.Fatalf("expected read group %q in %#v", name, groups)
	return AuditReadGroup{}
}
