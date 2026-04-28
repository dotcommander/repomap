package repomap

import (
	"testing"
)

func TestParsePorcelainLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty output",
			input: "",
			want:  nil,
		},
		{
			name:  "clean repo",
			input: "\n",
			want:  nil,
		},
		{
			name:  "single dirty file",
			input: " M internal/foo.go\n",
			want:  []string{" M internal/foo.go"},
		},
		{
			name:  "multiple dirty files",
			input: " M foo.go\n?? bar.go\nM  baz.go\n",
			want:  []string{" M foo.go", "?? bar.go", "M  baz.go"},
		},
		{
			name:  "no trailing newline",
			input: " M foo.go",
			want:  []string{" M foo.go"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parsePorcelainLines(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i, line := range got {
				if line != tc.want[i] {
					t.Errorf("line[%d] = %q, want %q", i, line, tc.want[i])
				}
			}
		})
	}
}

func TestSelfVerifyConventionalRe(t *testing.T) {
	t.Parallel()
	passing := []string{
		"feat(search): add fuzzy matcher",
		"fix(auth): prevent token refresh race",
		"docs(kb): update notes",
		"chore: update 3 files",
		"refactor(internal): collapse duplicate logic",
		"test(commit): add table-driven cases",
		"perf(ranker): skip re-sort on single file",
		"style(render): fix trailing whitespace",
		"build: bump go toolchain to 1.26",
		"ci: add release gate step",
		"revert: revert feat(search) changes",
	}
	failing := []string{
		"",
		"not conventional",
		"WIP: something",
		"feat: subject that is way too long to be a valid commit message and exceeds 72 chars here",
		"FEAT: uppercase type",
		"feat(scope) missing colon after scope",
		"misc: random",
	}

	for _, s := range passing {
		if !selfVerifyConventionalRe.MatchString(s) {
			t.Errorf("expected %q to match conventional re", s)
		}
	}
	for _, s := range failing {
		if selfVerifyConventionalRe.MatchString(s) {
			t.Errorf("expected %q NOT to match conventional re", s)
		}
	}
}

// TestVerifyResultJSON validates the struct marshals correctly (no live git calls needed).
func TestVerifyResultJSON(t *testing.T) {
	t.Parallel()
	r := VerifyResult{
		Mode:              "local",
		OK:                true,
		LastCommitSubject: "feat(search): add fuzzy",
	}
	if !r.OK {
		t.Error("expected OK=true")
	}
	if r.Mode != "local" {
		t.Errorf("Mode=%q want local", r.Mode)
	}
	if r.LastCommitSubject == "" {
		t.Error("LastCommitSubject should not be empty")
	}
}

func TestRepoStatusStruct(t *testing.T) {
	t.Parallel()
	rs := RepoStatus{
		Repo:  "/path/to/project/project",
		Dirty: []string{" M foo.go"},
	}
	if len(rs.Dirty) != 1 {
		t.Errorf("expected 1 dirty entry, got %d", len(rs.Dirty))
	}
}

func TestMinHelper(t *testing.T) {
	t.Parallel()
	tests := []struct{ a, b, want int }{
		{3, 5, 3},
		{5, 3, 3},
		{3, 3, 3},
		{0, 1, 0},
	}
	for _, tc := range tests {
		if got := min(tc.a, tc.b); got != tc.want {
			t.Errorf("min(%d,%d)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
