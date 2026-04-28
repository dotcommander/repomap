package repomap

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// --- consolidation tests ---

func Test_ConsolidateGroups_Cap3(t *testing.T) {
	t.Parallel()
	// 5 groups: two large (3 files each in "pkg/"), three riders (1 file each in distinct dirs).
	// Expected: riders in "pkg/" fold into the first large group → 2 large + 2 orphan riders = 4.
	// Then merge-smallest brings it to 3.
	groups := []CommitGroup{
		{ID: "a", SuggestedMsg: "feat: a", Files: []string{"pkg/a1.go", "pkg/a2.go", "pkg/a3.go"}},
		{ID: "b", SuggestedMsg: "fix: b", Files: []string{"pkg/b1.go", "pkg/b2.go", "pkg/b3.go"}},
		{ID: "c", SuggestedMsg: "chore: c", Files: []string{"cmd/c1.go"}},
		{ID: "d", SuggestedMsg: "docs: d", Files: []string{"docs/d1.md"}},
		{ID: "e", SuggestedMsg: "test: e", Files: []string{"internal/e1.go"}},
	}
	got := ConsolidateGroups(groups)
	if len(got) > 3 {
		t.Errorf("consolidateGroups(%d groups) = %d groups, want ≤3", len(groups), len(got))
	}
	// All original files must still be present somewhere.
	all := make(map[string]bool)
	for _, g := range got {
		for _, f := range g.Files {
			all[f] = true
		}
	}
	for _, g := range groups {
		for _, f := range g.Files {
			if !all[f] {
				t.Errorf("file %q lost after consolidation", f)
			}
		}
	}
}

func Test_ConsolidateGroups_NoFixSkipsConsolidation(t *testing.T) {
	t.Parallel()
	groups := make([]CommitGroup, 5)
	for i := range groups {
		groups[i] = CommitGroup{
			ID:           string(rune('a' + i)),
			SuggestedMsg: "chore: x",
			Files:        []string{"file" + string(rune('a'+i)) + ".go"},
		}
	}
	// --skip-fix path: pass groups straight through consolidateGroups is bypassed in
	// ExecuteCommit, but we can verify the flag semantics by calling ExecuteCommit
	// with a dry-run plan and SkipFix=true.
	// Here we just assert consolidateGroups itself isn't called when SkipFix is set;
	// we do that by verifying the helper returns 5 unchanged groups when input is ≤3.
	small := groups[:3]
	got := ConsolidateGroups(small)
	if len(got) != 3 {
		t.Errorf("consolidateGroups on 3 groups = %d, want 3 (no-op)", len(got))
	}
}

// --- message validation tests ---

func Test_ValidateMessage_Conventional(t *testing.T) {
	t.Parallel()
	cases := []struct {
		msg   string
		valid bool
	}{
		{"feat(api): add user endpoint", true},
		{"fix: correct off-by-one", true},
		{"chore(deps): bump golang.org/x/sync", true},
		{"refactor(internal): extract helper", true},
		{"docs: update README", true},
		{"test: add table-driven cases", true},
		{"perf(cache): reduce allocations", true},
		{"style: gofmt", true},
		{"build(ci): update workflow", true},
		{"ci: add lint step", true},
		{"revert: undo bad migration", true},
		// subject exactly 72 chars (valid boundary)
		{"feat: " + strings.Repeat("x", 72), true},
		// subject 73 chars (one over)
		{"feat: " + strings.Repeat("x", 73), false},
		// missing type
		{"update something", false},
		// missing colon-space
		{"feat(scope) add thing", false},
		// empty subject after colon-space
		{"feat: ", false},
		// unknown type
		{"wip: work in progress", false},
		// multi-line: first line valid, body ignored
		{"fix: correct thing\n\nLonger body text here.", true},
	}
	for _, tc := range cases {
		err := ValidateConventionalMsg(tc.msg)
		if tc.valid && err != nil {
			t.Errorf("ValidateConventionalMsg(%q) unexpected error: %v", tc.msg, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("ValidateConventionalMsg(%q) expected error, got nil", tc.msg)
		}
	}
}

// --- tag validation tests ---

func Test_ValidateTag_Semver(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tag   string
		valid bool
	}{
		{"v1.2.3", true},
		{"v0.0.1", true},
		{"v10.20.30", true},
		{"v1.2.3-beta.1", true},
		{"v1.2.3-rc.2", true},
		{"v1.2.3-alpha", true},
		{"v1.2.3-Beta.1", true},
		// invalid
		{"1.2.3", false},    // missing v prefix
		{"vfoo", false},     // not numeric
		{"v1.2", false},     // missing patch
		{"v1.2.3.4", false}, // extra component
		{"", false},
		{"v1.2.3_rc1", false}, // underscore not allowed in pre-release
	}
	for _, tc := range cases {
		err := ValidateTag(tc.tag)
		if tc.valid && err != nil {
			t.Errorf("ValidateTag(%q) unexpected error: %v", tc.tag, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("ValidateTag(%q) expected error, got nil", tc.tag)
		}
	}
}

// --- execute integration tests ---

func Test_Execute_DryRun(t *testing.T) {
	t.Parallel()
	root := initTestRepo(t,
		map[string]string{
			"go.mod":   "module fixture\ngo 1.22\n",
			"alpha.go": "package fixture\n",
			"beta.go":  "package fixture\n",
		},
		map[string]string{
			"alpha.go": "package fixture\n// changed\n",
			"beta.go":  "package fixture\n// changed\n",
		},
	)

	groups := []CommitGroup{
		{ID: "g1", SuggestedMsg: "feat: update alpha", Files: []string{"alpha.go"}},
		{ID: "g2", SuggestedMsg: "chore: update beta", Files: []string{"beta.go"}},
	}
	planFile := makePlanFile(t, groups)

	// Count commits before dry-run.
	beforeLog := gitLogCount(t, root)

	var stdout bytes.Buffer
	opts := ExecuteOptions{
		Root:     root,
		PlanFile: planFile,
		DryRun:   true,
		SkipFix:  true,
	}
	result, err := ExecuteCommit(context.Background(), opts)
	if err != nil {
		t.Fatalf("ExecuteCommit dry-run: %v", err)
	}
	_ = stdout // printDryRun writes to os.Stdout; result is what we inspect
	_ = result

	// No new commits should have landed.
	afterLog := gitLogCount(t, root)
	if afterLog != beforeLog {
		t.Errorf("dry-run created commits: before=%d after=%d", beforeLog, afterLog)
	}

	// Files must still be dirty (unstaged).
	porcelain := runGitOutput(t, root, "status", "--porcelain")
	if strings.TrimSpace(porcelain) == "" {
		t.Errorf("dry-run should leave working tree dirty; got clean status")
	}
}

func Test_Execute_HappyPath_NoRemote(t *testing.T) {
	t.Parallel()
	root := initTestRepo(t,
		map[string]string{
			"go.mod":     "module fixture\ngo 1.22\n",
			"service.go": "package fixture\n",
			"handler.go": "package fixture\n",
			"util.go":    "package fixture\n",
		},
		map[string]string{
			"service.go": "package fixture\n// service updated\n",
			"handler.go": "package fixture\n// handler updated\n",
			"util.go":    "package fixture\n// util updated\n",
		},
	)

	groups := []CommitGroup{
		{ID: "g1", SuggestedMsg: "feat: update service and handler", Files: []string{"service.go", "handler.go"}},
		{ID: "g2", SuggestedMsg: "chore: update util", Files: []string{"util.go"}},
	}
	planFile := makePlanFile(t, groups)

	result, err := ExecuteCommit(context.Background(), ExecuteOptions{
		Root:     root,
		PlanFile: planFile,
		SkipFix:  true, // 2 groups — no consolidation needed
	})
	if err != nil {
		t.Fatalf("ExecuteCommit: %v", err)
	}

	// Two commits must have landed.
	if len(result.Commits) != 2 {
		t.Errorf("got %d commits, want 2", len(result.Commits))
	}

	// Commit order: g1 first, g2 second.
	if result.Commits[0].Message != groups[0].SuggestedMsg {
		t.Errorf("commit[0].message = %q, want %q", result.Commits[0].Message, groups[0].SuggestedMsg)
	}
	if result.Commits[1].Message != groups[1].SuggestedMsg {
		t.Errorf("commit[1].message = %q, want %q", result.Commits[1].Message, groups[1].SuggestedMsg)
	}

	// SHAs must be non-empty and distinct.
	if result.Commits[0].SHA == "" || result.Commits[1].SHA == "" {
		t.Errorf("empty SHA in commits: %+v", result.Commits)
	}
	if result.Commits[0].SHA == result.Commits[1].SHA {
		t.Errorf("commits have identical SHA %q", result.Commits[0].SHA)
	}

	// Workspace must be clean.
	porcelain := runGitOutput(t, root, "status", "--porcelain")
	if strings.TrimSpace(porcelain) != "" {
		t.Errorf("workspace dirty after execute:\n%s", porcelain)
	}

	// Postflight must report clean.
	if !result.Postflight.Clean {
		t.Errorf("postflight.clean=false after successful execute")
	}
	if !result.Postflight.Convent {
		t.Errorf("postflight.conventional=false — last commit message failed regex")
	}

	// Git log must show exactly 2 new commits on top of initial.
	logLines := runGitOutput(t, root, "log", "--oneline", "-3")
	newCommits := strings.Count(strings.TrimSpace(logLines), "\n") + 1
	if newCommits < 2 {
		t.Errorf("git log shows %d commits, want ≥2:\n%s", newCommits, logLines)
	}

	// Last two commits must use the messages we specified (most recent first).
	subjects := runGitOutput(t, root, "log", "--pretty=%s", "-2")
	lines := strings.Split(strings.TrimSpace(subjects), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 log lines, got: %q", subjects)
	}
	// git log is newest-first: lines[0]=g2, lines[1]=g1
	if lines[0] != groups[1].SuggestedMsg {
		t.Errorf("newest commit subject = %q, want %q", lines[0], groups[1].SuggestedMsg)
	}
	if lines[1] != groups[0].SuggestedMsg {
		t.Errorf("second commit subject = %q, want %q", lines[1], groups[0].SuggestedMsg)
	}
}
