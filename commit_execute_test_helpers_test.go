package repomap

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// makePlanFile serializes groups into a CommitAnalysis JSON file and returns its path.
func makePlanFile(t *testing.T, groups []CommitGroup) string {
	t.Helper()
	a := CommitAnalysis{
		Version: 1,
		Secrets: SecretsSummary{Clean: true},
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

// gitLogCount returns the number of commits reachable from HEAD.
func gitLogCount(t *testing.T, root string) int {
	t.Helper()
	out := runGitOutput(t, root, "rev-list", "--count", "HEAD")
	count := 0
	for _, ch := range strings.TrimSpace(out) {
		if ch >= '0' && ch <= '9' {
			count = count*10 + int(ch-'0')
		}
	}
	return count
}

// runGitOutput runs git in root and returns stdout; ignores errors (caller asserts).
func runGitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, _ := gitOutput(context.Background(), root, args...)
	return out
}
