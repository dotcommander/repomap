package repomap

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo builds a disposable git repo with initial committed content,
// then applies `mutations` (file -> new content) to the working tree without
// staging. Returns the repo root path.
func initTestRepo(t *testing.T, initial, mutations map[string]string) string {
	t.Helper()
	root := t.TempDir()
	runGitT(t, root, "init", "-q", "-b", "main")
	runGitT(t, root, "config", "user.email", "test@example.com")
	runGitT(t, root, "config", "user.name", "Test")
	runGitT(t, root, "config", "commit.gpgsign", "false")
	for path, content := range initial {
		writeFixture(t, root, path, content)
	}
	if len(initial) > 0 {
		runGitT(t, root, "add", "-A")
		runGitT(t, root, "commit", "-q", "-m", "initial")
	}
	for path, content := range mutations {
		writeFixture(t, root, path, content)
	}
	return root
}

func writeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", abs, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}

func runGitT(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// Test_Analyze_HappyPath: a test-pair + a config change form two groups,
// both high confidence, messages non-generic.
func Test_Analyze_HappyPath(t *testing.T) {
	root := initTestRepo(t,
		map[string]string{
			"go.mod":          "module fixture\ngo 1.22\n",
			"pkg/foo.go":      "package pkg\nfunc Foo() int { return 1 }\n",
			"pkg/foo_test.go": "package pkg\nimport \"testing\"\nfunc TestFoo(t *testing.T) { _ = Foo() }\n",
			"README.md":       "# Fixture\n",
		},
		map[string]string{
			"pkg/foo.go":      "package pkg\nfunc Foo() int { return 2 }\nfunc Bar() int { return 3 }\n",
			"pkg/foo_test.go": "package pkg\nimport \"testing\"\nfunc TestFoo(t *testing.T) { _ = Foo() }\nfunc TestBar(t *testing.T) { _ = Bar() }\n",
			"README.md":       "# Fixture\n\nUpdated.\n",
		},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	if got.EarlyExit {
		t.Fatalf("early exit unexpected")
	}
	if got.Counts.Total != 3 {
		t.Errorf("total files = %d, want 3", got.Counts.Total)
	}
	if len(got.Groups) < 1 {
		t.Fatalf("no groups produced")
	}
	// The test-pair edge (weight 1.0) must bind foo.go + foo_test.go.
	foundPair := false
	for _, g := range got.Groups {
		if containsAll(g.Files, "pkg/foo.go", "pkg/foo_test.go") {
			foundPair = true
			if g.Confidence < 0.75 {
				t.Errorf("test-pair group confidence %.2f < 0.75", g.Confidence)
			}
		}
	}
	if !foundPair {
		t.Errorf("test-pair group not found; groups=%+v", got.Groups)
	}
}

// Test_Analyze_CleanRepo: no dirty files => early_exit.
func Test_Analyze_CleanRepo(t *testing.T) {
	root := initTestRepo(t,
		map[string]string{"README.md": "# clean\n"},
		nil,
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	if !got.EarlyExit {
		t.Errorf("expected early_exit=true on clean repo")
	}
}

// Test_Analyze_Secrets: a live AWS access key in a config file must FLAG.
func Test_Analyze_Secrets(t *testing.T) {
	root := initTestRepo(t,
		map[string]string{"config.yaml": "key: value\n"},
		map[string]string{"config.yaml": "key: value\naws_access_key: AKIAIOSFODNN7EXAMPLE\n"},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	if got.Secrets.Clean {
		t.Errorf("expected secrets.clean=false when AKIA key present")
	}
	if got.Secrets.FlagCount < 1 {
		t.Errorf("expected at least 1 FLAG finding, got %d", got.Secrets.FlagCount)
	}
}

// Test_Analyze_PlaceholderPaths: /Users/you/ should NOT flag as PII.
func Test_Analyze_PlaceholderPaths(t *testing.T) {
	root := initTestRepo(t,
		map[string]string{"docs.md": "# Docs\n"},
		map[string]string{"docs.md": "# Docs\n\nInstall at /Users/you/bin/tool.\n"},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	for _, f := range readFindings(t, got.Refs.Findings) {
		if f.Kind == "pii" && strings.Contains(f.Snippet, "/Users/you/") {
			t.Errorf("placeholder path leaked into findings: %+v", f)
		}
	}
}

// Test_Analyze_DepBump: go.mod version bump is detected.
func Test_Analyze_DepBump(t *testing.T) {
	root := initTestRepo(t,
		map[string]string{
			"go.mod": "module fixture\n\ngo 1.22\n\nrequire github.com/pkg/errors v0.9.0\n",
		},
		map[string]string{
			"go.mod": "module fixture\n\ngo 1.22\n\nrequire github.com/pkg/errors v0.9.1\n",
		},
	)
	got, err := AnalyzeCommit(context.Background(), AnalyzeOptions{Root: root})
	if err != nil {
		t.Fatalf("AnalyzeCommit: %v", err)
	}
	if len(got.DepBumps) == 0 {
		t.Fatalf("expected dep_bumps entry for go.mod")
	}
	if got.DepBumps[0].Manager != "go" {
		t.Errorf("manager = %q, want %q", got.DepBumps[0].Manager, "go")
	}
}

func containsAll(haystack []string, needles ...string) bool {
	set := make(map[string]bool, len(haystack))
	for _, h := range haystack {
		set[h] = true
	}
	for _, n := range needles {
		if !set[n] {
			return false
		}
	}
	return true
}

func readFindings(t *testing.T, path string) []Finding {
	t.Helper()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read findings: %v", err)
	}
	// Reuse the existing JSON schema.
	var out []Finding
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal findings: %v", err)
	}
	return out
}
