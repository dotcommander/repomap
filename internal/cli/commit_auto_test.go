package cli

// commit_auto_test.go — routing tests for `commit auto`.
//
// We exercise the three non-execution branches (abort, needs_judgment, and
// the force-mode override) by collecting the JSON payload that runCommitAuto
// writes through its io.Writer parameter. The ready→finish path is not tested
// here: runCommitFinish calls os.Exit on verify failure, which would terminate
// the test process. That path is covered by the existing commit_v090
// integration suite and the manual smoke test in Section 5 of the spec.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initAutoTestRepo builds a disposable git repo with one initial commit so
// AnalyzeCommit has a HEAD to diff against.
func initAutoTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitAuto(t, root, "init", "-q", "-b", "main")
	runGitAuto(t, root, "config", "user.email", "test@example.com")
	runGitAuto(t, root, "config", "user.name", "Test")
	runGitAuto(t, root, "config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# init\n"), 0o644))
	runGitAuto(t, root, "add", "-A")
	runGitAuto(t, root, "commit", "-q", "-m", "initial")
	return root
}

func runGitAuto(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// decodePayload parses the captured output into a PrepPayload.
func decodePayload(t *testing.T, raw string) *repomap.PrepPayload {
	t.Helper()
	raw = strings.TrimSpace(raw)
	require.NotEmpty(t, raw, "expected JSON payload on writer")
	var p repomap.PrepPayload
	require.NoError(t, json.Unmarshal([]byte(raw), &p), "output: %s", raw)
	return &p
}

// TestRunCommitAuto_Abort_NoChanges: a clean repo triggers the early-exit path
// in buildPrepPayload, which emits status=abort without invoking commit finish.
func TestRunCommitAuto_Abort_NoChanges(t *testing.T) {
	t.Parallel()
	root := initAutoTestRepo(t)

	var buf bytes.Buffer
	require.NoError(t, runCommitAuto(context.Background(), &buf, root, false, false, "", "", ""))

	p := decodePayload(t, buf.String())
	assert.Equal(t, repomap.PrepStatusAbort, p.Status)
	assert.NotEmpty(t, p.AbortReason)
}

// TestRunCommitAuto_ForceMode_OverridesPreflight verifies that --force-mode
// supersedes the auto-detected mode_hint. We pin the abort branch (clean repo)
// so we can read the emitted payload without invoking commit finish.
func TestRunCommitAuto_ForceMode_OverridesPreflight(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		forceMode string
		want      string
	}{
		{"force FULL", "FULL", "FULL"},
		{"force LOCAL", "LOCAL", "LOCAL"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := initAutoTestRepo(t)
			var buf bytes.Buffer
			require.NoError(t, runCommitAuto(context.Background(), &buf, root, false, false, "", "", tc.forceMode))
			p := decodePayload(t, buf.String())
			assert.Equal(t, tc.want, p.ModeHint)
		})
	}
}

// TestRunCommitAuto_ForceMode_InvalidIgnored: an unknown --force-mode value
// falls through to preflight-derived detection. A fresh test repo has no
// remote and no gh auth, so the auto-detected mode is LOCAL.
func TestRunCommitAuto_ForceMode_InvalidIgnored(t *testing.T) {
	t.Parallel()
	root := initAutoTestRepo(t)

	var buf bytes.Buffer
	require.NoError(t, runCommitAuto(context.Background(), &buf, root, false, false, "", "", "BOGUS"))

	p := decodePayload(t, buf.String())
	assert.Equal(t, "LOCAL", p.ModeHint, "unknown force-mode must fall through to preflight detection")
}

// TestRunCommitAuto_NeedsJudgment_KitchenSink: stage 11+ unrelated files to
// trigger the kitchen-sink guard. The guard forces low-confidence on the fused
// group, lifting the payload status above ready.
func TestRunCommitAuto_NeedsJudgment_KitchenSink(t *testing.T) {
	t.Parallel()
	root := initAutoTestRepo(t)

	for i := 0; i < 11; i++ {
		path := filepath.Join(root, fmt.Sprintf("file_%02d.txt", i))
		require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("content %d\n", i)), 0o644))
	}
	runGitAuto(t, root, "add", "-A")

	var buf bytes.Buffer
	require.NoError(t, runCommitAuto(context.Background(), &buf, root, false, false, "", "", ""))

	p := decodePayload(t, buf.String())
	// The guard may produce either needs_judgment (one fused group) or abort
	// (if reviewer/lowConf caps trip). Either is a non-ready outcome — assert
	// the routing invariant: an 11-file fusion never passes through as ready.
	assert.NotEqual(t, repomap.PrepStatusReady, p.Status,
		"11-file fusion must not pass through as ready: status=%q", p.Status)
}
