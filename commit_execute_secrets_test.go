package repomap

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// makePlanFileWithSecrets builds a plan file with the given SecretsSummary.
func makePlanFileWithSecrets(t *testing.T, groups []CommitGroup, secrets SecretsSummary) string {
	t.Helper()
	a := CommitAnalysis{
		Version: 1,
		Secrets: secrets,
		Groups:  groups,
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	f, err := os.CreateTemp(t.TempDir(), "plan-*.json")
	if err != nil {
		t.Fatalf("create plan file: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	f.Close()
	return f.Name()
}

// Test_Execute_BadTagExitCode verifies that an invalid tag returns an execError
// with code 2 and makes no commits.
func Test_Execute_BadTagExitCode(t *testing.T) {
	t.Parallel()
	root := initTestRepo(t,
		map[string]string{"a.go": "package fixture\n"},
		map[string]string{"a.go": "package fixture\n// changed\n"},
	)
	groups := []CommitGroup{
		{ID: "g1", SuggestedMsg: "feat: update a", Files: []string{"a.go"}},
	}
	planFile := makePlanFile(t, groups)
	beforeCount := gitLogCount(t, root)

	_, err := ExecuteCommit(context.Background(), ExecuteOptions{
		Root:     root,
		PlanFile: planFile,
		Tag:      "not-semver",
		SkipFix:  true,
	})
	if err == nil {
		t.Fatal("expected error for invalid tag, got nil")
	}
	if code := ExecExitCode(err); code != 2 {
		t.Errorf("ExecExitCode = %d, want 2", code)
	}
	if after := gitLogCount(t, root); after != beforeCount {
		t.Errorf("commits landed despite validation failure: before=%d after=%d", beforeCount, after)
	}
}

// Test_Execute_AmbiguousSecretsRefused verifies that a plan with
// ambiguous_count > 0 is refused with exit code 2, before any git work.
func Test_Execute_AmbiguousSecretsRefused(t *testing.T) {
	t.Parallel()
	root := initTestRepo(t,
		map[string]string{"a.go": "package fixture\n"},
		map[string]string{"a.go": "package fixture\n// changed\n"},
	)
	groups := []CommitGroup{
		{ID: "g1", SuggestedMsg: "feat: update a", Files: []string{"a.go"}},
	}
	planFile := makePlanFileWithSecrets(t, groups, SecretsSummary{
		Clean:          true,
		AmbiguousCount: 1,
	})
	beforeCount := gitLogCount(t, root)

	_, err := ExecuteCommit(context.Background(), ExecuteOptions{
		Root:     root,
		PlanFile: planFile,
		SkipFix:  true,
	})
	if err == nil {
		t.Fatal("expected error for ambiguous secrets, got nil")
	}
	if code := ExecExitCode(err); code != 2 {
		t.Errorf("ExecExitCode = %d, want 2", code)
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error message missing 'ambiguous': %v", err)
	}
	if after := gitLogCount(t, root); after != beforeCount {
		t.Errorf("commits landed despite secrets gate: before=%d after=%d", beforeCount, after)
	}
}

// Test_Execute_DetectedSecretsRefused verifies that a plan with flag_count > 0
// is refused with exit code 2, before any git work.
func Test_Execute_DetectedSecretsRefused(t *testing.T) {
	t.Parallel()
	root := initTestRepo(t,
		map[string]string{"a.go": "package fixture\n"},
		map[string]string{"a.go": "package fixture\n// changed\n"},
	)
	groups := []CommitGroup{
		{ID: "g1", SuggestedMsg: "feat: update a", Files: []string{"a.go"}},
	}
	planFile := makePlanFileWithSecrets(t, groups, SecretsSummary{
		Clean:     false,
		FlagCount: 1,
	})
	beforeCount := gitLogCount(t, root)

	_, err := ExecuteCommit(context.Background(), ExecuteOptions{
		Root:     root,
		PlanFile: planFile,
		SkipFix:  true,
	})
	if err == nil {
		t.Fatal("expected error for detected secrets, got nil")
	}
	if code := ExecExitCode(err); code != 2 {
		t.Errorf("ExecExitCode = %d, want 2", code)
	}
	if !strings.Contains(err.Error(), "detected") {
		t.Errorf("error message missing 'detected': %v", err)
	}
	if after := gitLogCount(t, root); after != beforeCount {
		t.Errorf("commits landed despite secrets gate: before=%d after=%d", beforeCount, after)
	}
}
