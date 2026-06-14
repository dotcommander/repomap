package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditCommandHygieneJSON(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runGitForCLIAuditTest(t, root, "init")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "ignored"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ignored", "local.go"), []byte("package ignored\n\nfunc Local() {}\n"), 0o644))
	runGitForCLIAuditTest(t, root, "add", ".gitignore", "main.go")

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"audit", "hygiene", "--json", root})

	require.NoError(t, cmd.Execute())

	var report repomap.AuditHygieneReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	assert.Equal(t, 1, report.SchemaVersion)
	assert.Equal(t, []string{"ignored/local.go"}, report.IgnoredSource)
}

func TestAuditCommandSurfaceAndEffectsJSON(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := `package main

import (
	"net/http"
	"os"
	"os/exec"
)

type Config struct {
	Endpoint string ` + "`json:\"endpoint\"`" + `
}

func main() {
	_ = os.Getenv("API_TOKEN")
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {})
	_ = os.WriteFile("out.json", []byte("{}"), 0o644)
	_ = exec.Command("git", "status").Run()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"audit-fixture"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644))

	cmd := newRootCmd()
	var surfaceOut bytes.Buffer
	cmd.SetOut(&surfaceOut)
	cmd.SetArgs([]string{"audit", "surface", "--json", root})
	require.NoError(t, cmd.Execute())

	var surface repomap.AuditSurfaceReport
	require.NoError(t, json.Unmarshal(surfaceOut.Bytes(), &surface))
	assert.NotEmpty(t, surface.EnvVars)
	assert.NotEmpty(t, surface.Routes)
	assert.NotEmpty(t, surface.SchemaFields)
	assert.NotEmpty(t, surface.DependencyManifests)

	cmd = newRootCmd()
	var effectsOut bytes.Buffer
	cmd.SetOut(&effectsOut)
	cmd.SetArgs([]string{"audit", "effects", "--json", root})
	require.NoError(t, cmd.Execute())

	var effects repomap.AuditEffectReport
	require.NoError(t, json.Unmarshal(effectsOut.Bytes(), &effects))
	require.NotEmpty(t, effects.Files)
	assert.Contains(t, effects.Files[0].Lanes, "data-integrity")
	assert.Contains(t, effects.Files[0].Lanes, "error-handling")

	cmd = newRootCmd()
	var briefOut bytes.Buffer
	cmd.SetOut(&briefOut)
	cmd.SetArgs([]string{"audit", "brief", "--json", root})
	require.NoError(t, cmd.Execute())

	var brief repomap.AuditBriefReport
	require.NoError(t, json.Unmarshal(briefOut.Bytes(), &brief))
	assert.NotEmpty(t, brief.Risks.Files)
	assert.NotEmpty(t, brief.Surface.Files)
	assert.NotEmpty(t, brief.Effects.Files)
	assert.NotEmpty(t, brief.FirstReadQueue)
	assert.NotEmpty(t, brief.ReviewPlan)
	assert.NotEmpty(t, brief.ReviewPlan[0].Gates)
}

func TestAuditCommandRegistered(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd()
	require.NotNil(t, cmd.Commands())
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "audit" {
			found = true
			break
		}
	}
	assert.True(t, found, "audit command must be registered")
}

func runGitForCLIAuditTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}

func TestAuditCommandSurfaceFilesNeverNull(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// A source file with no user-facing surface: the scan finds the file but
	// extracts zero surface hits, so Files ends empty. That empty slice used to
	// serialize as null; it must now serialize as []. The command still
	// succeeds because a source file exists.
	require.NoError(t, os.WriteFile(filepath.Join(root, "noop.go"),
		[]byte("package noop\n\nfunc helper() int { return 1 }\n"), 0o644))

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"audit", "surface", "--json", root})
	require.NoError(t, cmd.Execute())

	raw := out.String()
	assert.Contains(t, raw, `"files": []`, "empty surface must emit [] not null")
	assert.NotContains(t, raw, `"files": null`)

	var surface repomap.AuditSurfaceReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &surface))
	assert.Equal(t, 2, surface.SchemaVersion)
	assert.NotEmpty(t, surface.FilesOmittedReason)
}
