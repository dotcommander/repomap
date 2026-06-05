package repomap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyFixFindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		fileContent  string
		findings     []Finding
		wantApplied  int
		wantSkipped  int
		wantContains []string // substrings that must appear in result
		wantAbsent   []string // substrings that must NOT appear in result
	}{
		{
			name: "aws key replacement",
			fileContent: `package fixture
const awsKey = "YOUR_API_KEY"
`,
			findings: []Finding{
				{Class: "FLAG", Kind: "secret", File: "fix.go", Line: 2,
					Snippet: "YOUR_API_KEY", DefaultAction: ActionFix},
			},
			wantApplied:  1,
			wantSkipped:  0,
			wantContains: []string{"REDACTED"},
			wantAbsent:   []string{"YOUR_API_KEY"},
		},
		{
			name: "user path replacement",
			fileContent: `config: /path/to/project/projects/app/config.yaml
`,
			findings: []Finding{
				{Class: "FLAG", Kind: "pii", File: "fix.go", Line: 1,
					Snippet: "/path/to/project/", DefaultAction: ActionFix},
			},
			wantApplied:  1,
			wantSkipped:  0,
			wantContains: []string{"/path/to/project/"},
			wantAbsent:   []string{"/path/to/project/"},
		},
		{
			name: "idempotent: placeholder already present",
			fileContent: `const apiKey = "REDACTED"
`,
			findings: []Finding{
				{Class: "FLAG", Kind: "secret", File: "fix.go", Line: 1,
					Snippet: "REDACTED", DefaultAction: ActionFix},
			},
			wantApplied:  0,
			wantSkipped:  1,
			wantContains: []string{"REDACTED"},
		},
		{
			name: "non-fix finding skipped",
			fileContent: `const x = "safe"
`,
			findings: []Finding{
				{Class: "REVIEW", Kind: "secret", File: "fix.go", Line: 1,
					Snippet: "safe", DefaultAction: ActionReview},
			},
			wantApplied:  0,
			wantSkipped:  1,
			wantContains: []string{`"safe"`},
		},
		{
			name: "line out of range skipped",
			fileContent: `line one
`,
			findings: []Finding{
				{Class: "FLAG", Kind: "secret", File: "fix.go", Line: 99,
					DefaultAction: ActionFix},
			},
			wantApplied: 0,
			wantSkipped: 1, // fix-action finding skipped due to OOR line
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			fpath := filepath.Join(dir, "fix.go")
			if err := os.WriteFile(fpath, []byte(tc.fileContent), 0o644); err != nil {
				t.Fatal(err)
			}

			// Adjust file refs to match the dir.
			findings := make([]Finding, len(tc.findings))
			copy(findings, tc.findings)

			applied, skipped, err := ApplyFixFindings(context.Background(), dir, findings)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(applied) != tc.wantApplied {
				t.Errorf("applied=%d want=%d", len(applied), tc.wantApplied)
			}
			if len(skipped) != tc.wantSkipped {
				t.Errorf("skipped=%d want=%d", len(skipped), tc.wantSkipped)
			}

			result, _ := os.ReadFile(fpath)
			resultStr := string(result)
			for _, s := range tc.wantContains {
				if !strings.Contains(resultStr, s) {
					t.Errorf("result missing %q\ngot: %s", s, resultStr)
				}
			}
			for _, s := range tc.wantAbsent {
				if strings.Contains(resultStr, s) {
					t.Errorf("result should not contain %q\ngot: %s", s, resultStr)
				}
			}
		})
	}
}

func TestApplyReviewDecisions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		fileContent  string
		decisions    []ReviewDecision
		findings     []Finding
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:        "unsafe verdict applies replacement",
			fileContent: "api_key: myRealKey123\n",
			decisions: []ReviewDecision{
				{ID: "secrets.go:1", Verdict: VerdictUnsafe, Replacement: "api_key: YOUR_API_KEY"},
			},
			findings: []Finding{
				{File: "secrets.go", Line: 1, DefaultAction: ActionReview},
			},
			wantContains: []string{"YOUR_API_KEY"},
			wantAbsent:   []string{"myRealKey123"},
		},
		{
			name:        "safe verdict leaves line unchanged",
			fileContent: "# author: jane@example.com\n",
			decisions: []ReviewDecision{
				{ID: "secrets.go:1", Verdict: VerdictSafe, Replacement: ""},
			},
			findings: []Finding{
				{File: "secrets.go", Line: 1, DefaultAction: ActionReview},
			},
			wantContains: []string{"jane@example.com"},
		},
		{
			name:        "unknown decision ID is silently skipped",
			fileContent: "x = 1\n",
			decisions: []ReviewDecision{
				{ID: "nonexistent.go:999", Verdict: VerdictUnsafe, Replacement: "x = 0"},
			},
			findings:     []Finding{},
			wantContains: []string{"x = 1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			fpath := filepath.Join(dir, "secrets.go")
			if err := os.WriteFile(fpath, []byte(tc.fileContent), 0o644); err != nil {
				t.Fatal(err)
			}

			err := ApplyReviewDecisions(context.Background(), dir, tc.decisions, tc.findings)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result, _ := os.ReadFile(fpath)
			resultStr := string(result)
			for _, s := range tc.wantContains {
				if !strings.Contains(resultStr, s) {
					t.Errorf("result missing %q\ngot: %s", s, resultStr)
				}
			}
			for _, s := range tc.wantAbsent {
				if strings.Contains(resultStr, s) {
					t.Errorf("result should not contain %q\ngot: %s", s, resultStr)
				}
			}
		})
	}
}

func TestValidateReviewDecisions(t *testing.T) {
	t.Parallel()
	findings := []Finding{
		{File: "secrets.go", Line: 1, DefaultAction: ActionReview},
		{File: "safe.go", Line: 2, DefaultAction: ActionSafe},
		{File: "also-secrets.go", Line: 3, DefaultAction: ActionReview},
	}

	tests := []struct {
		name      string
		decisions []ReviewDecision
		wantErr   string
	}{
		{
			name: "all review findings decided",
			decisions: []ReviewDecision{
				{ID: "secrets.go:1", Verdict: VerdictSafe},
				{ID: "also-secrets.go:3", Verdict: VerdictUnsafe, Replacement: "redacted"},
			},
		},
		{
			name: "missing review finding decision",
			decisions: []ReviewDecision{
				{ID: "secrets.go:1", Verdict: VerdictSafe},
			},
			wantErr: "missing decision for review finding also-secrets.go:3",
		},
		{
			name: "unknown finding rejected",
			decisions: []ReviewDecision{
				{ID: "secrets.go:1", Verdict: VerdictSafe},
				{ID: "also-secrets.go:3", Verdict: VerdictSafe},
				{ID: "missing.go:9", Verdict: VerdictSafe},
			},
			wantErr: "decision for unknown review finding missing.go:9",
		},
		{
			name: "unsafe requires replacement",
			decisions: []ReviewDecision{
				{ID: "secrets.go:1", Verdict: VerdictSafe},
				{ID: "also-secrets.go:3", Verdict: VerdictUnsafe},
			},
			wantErr: "unsafe decision for also-secrets.go:3 missing replacement",
		},
		{
			name: "duplicate rejected",
			decisions: []ReviewDecision{
				{ID: "secrets.go:1", Verdict: VerdictSafe},
				{ID: "secrets.go:1", Verdict: VerdictSafe},
				{ID: "also-secrets.go:3", Verdict: VerdictSafe},
			},
			wantErr: "duplicate decision for secrets.go:1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateReviewDecisions(findings, tc.decisions)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestPlaceholderFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind string
		want string
	}{
		{"secret_api_key", "YOUR_API_KEY"},
		{"secret_password", "YOUR_PASSWORD"},
		{"secret_token", "YOUR_TOKEN"},
		{"secret", "REDACTED"},
		{"pii", "/path/to/project"},
		{"path_user_home", "/path/to/project"},
		{"path_machine_specific", "/path/to/project"},
		{"unknown_kind", ""},
	}
	for _, tc := range tests {
		got := placeholderFor(tc.kind)
		if got != tc.want {
			t.Errorf("placeholderFor(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestApplyPlaceholder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		line        string
		placeholder string
		wantChanged bool
		wantContain string
	}{
		{
			name:        "aws key",
			line:        `const key = "YOUR_API_KEY"`,
			placeholder: "REDACTED",
			wantChanged: true,
			wantContain: "REDACTED",
		},
		{
			name:        "user path",
			line:        `path: /path/to/project/workspace/project`,
			placeholder: "/path/to/project",
			wantChanged: true,
			wantContain: "/path/to/project",
		},
		{
			name:        "already substituted",
			line:        `key: REDACTED`,
			placeholder: "REDACTED",
			wantChanged: false,
			wantContain: "REDACTED",
		},
		{
			name:        "no pattern match",
			line:        `just a regular comment`,
			placeholder: "REDACTED",
			wantChanged: false,
			wantContain: "just a regular comment",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, changed := applyPlaceholder(tc.line, tc.placeholder)
			if changed != tc.wantChanged {
				t.Errorf("changed=%v want=%v (line=%q result=%q)", changed, tc.wantChanged, tc.line, got)
			}
			if !strings.Contains(got, tc.wantContain) {
				t.Errorf("result %q does not contain %q", got, tc.wantContain)
			}
		})
	}
}
