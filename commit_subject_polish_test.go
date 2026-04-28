package repomap

import (
	"testing"
)

func TestPolish(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		group       CommitGroup
		wantSubject string
		wantMinConf float64
		wantMaxConf float64
	}{
		{
			name: "docs kb update",
			group: CommitGroup{
				Type:  "docs",
				Files: []string{"kb/foo.md", "kb/bar.md", "kb/baz.md"},
			},
			wantSubject: "docs(kb): update notes",
			wantMinConf: 0.6,
			wantMaxConf: 1.0,
		},
		{
			name: "single go file in internal/auth",
			group: CommitGroup{
				Type:  "chore",
				Files: []string{"internal/auth/authenticator.go"},
			},
			// code family, group type=chore → chore(internal): update implementation
			wantMinConf: 0.45,
			wantMaxConf: 0.65,
		},
		{
			name: "test file",
			group: CommitGroup{
				Type:  "test",
				Files: []string{"internal/auth/auth_test.go"},
			},
			wantSubject: "test(internal): update tests",
			wantMinConf: 0.7,
			wantMaxConf: 1.0,
		},
		{
			name: "gitignore + random file mixed",
			group: CommitGroup{
				Type:  "chore",
				Files: []string{".gitignore", "README.md"},
			},
			// mixed family (.gitignore=other, README=docs) → other→chore, no scope
			wantMinConf: 0.3,
			wantMaxConf: 0.6,
		},
		{
			name: "empty files fallback",
			group: CommitGroup{
				Type:  "",
				Files: []string{},
			},
			wantSubject: "chore: update files",
			wantMinConf: 0.3,
			wantMaxConf: 0.35,
		},
		{
			name: "feat type preserved",
			group: CommitGroup{
				Type:  "feat",
				Files: []string{"internal/search/fuzzy.go"},
			},
			wantSubject: "feat(internal): add implementation",
			wantMinConf: 0.5,
			wantMaxConf: 1.0,
		},
		{
			name: "low confidence fallback via buildFallbackSubject",
			group: CommitGroup{
				Type:  "",
				Files: []string{"a.bin", "b.bin", "c.bin"},
			},
			// all "other" family, no scope → confidence around 0.4
			wantMinConf: 0.3,
			wantMaxConf: 0.55,
		},
		{
			name: "config files",
			group: CommitGroup{
				Type:  "chore",
				Files: []string{"ci/deploy.yml", "ci/test.yml"},
			},
			wantSubject: "chore(ci): update 2 config files",
			wantMinConf: 0.6,
			wantMaxConf: 1.0,
		},
		{
			name: "script file",
			group: CommitGroup{
				Type:  "",
				Files: []string{"scripts/build.sh"},
			},
			wantSubject: "chore(scripts): update script",
			wantMinConf: 0.6,
			wantMaxConf: 1.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			subject, conf := Polish(tc.group)

			if tc.wantSubject != "" && subject != tc.wantSubject {
				t.Errorf("subject = %q, want %q", subject, tc.wantSubject)
			}
			if conf < tc.wantMinConf || conf > tc.wantMaxConf {
				t.Errorf("confidence = %.2f, want [%.2f, %.2f] (subject=%q)",
					conf, tc.wantMinConf, tc.wantMaxConf, subject)
			}

			// Invariant: subject must be non-empty and start with a valid type.
			if subject == "" {
				t.Error("subject must not be empty")
			}
			// Must not contain WIP or misc.
			for _, banned := range []string{"WIP", "misc", "Misc"} {
				if len(subject) >= len(banned) {
					for i := 0; i <= len(subject)-len(banned); i++ {
						if subject[i:i+len(banned)] == banned {
							t.Errorf("subject %q contains banned word %q", subject, banned)
						}
					}
				}
			}
		})
	}
}

func TestBuildFallbackSubject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		files []string
		want  string
	}{
		{[]string{}, "chore: update files"},
		{[]string{"a.go"}, "chore: update 1 files"},
		{[]string{"a.go", "b.go", "c.go"}, "chore: update 3 files"},
	}
	for _, tc := range tests {
		got := buildFallbackSubject(tc.files)
		if got != tc.want {
			t.Errorf("buildFallbackSubject(%v) = %q, want %q", tc.files, got, tc.want)
		}
	}
}

func TestPolishGroup(t *testing.T) {
	t.Parallel()
	g := CommitGroup{
		Type:         "docs",
		Files:        []string{"kb/note.md"},
		SuggestedMsg: "original message",
		Confidence:   0.3,
	}

	updated := PolishGroup(&g, 0.6)
	if !updated {
		t.Error("expected PolishGroup to update high-confidence docs group")
	}
	if g.SuggestedMsg == "original message" {
		t.Error("SuggestedMsg should have been updated")
	}
	if g.Confidence < 0.6 {
		t.Errorf("confidence after polish = %.2f, want >= 0.6", g.Confidence)
	}
}

func TestPolishGroup_LowConfidence(t *testing.T) {
	t.Parallel()
	g := CommitGroup{
		Type:         "chore",
		Files:        []string{"a.bin", "b.xyz"},
		SuggestedMsg: "original",
		Confidence:   0.9,
	}
	// Other/other files with no scope → confidence ~0.4-0.45
	// Threshold 0.8 should not be met.
	updated := PolishGroup(&g, 0.8)
	if updated {
		t.Error("expected PolishGroup to NOT update low-confidence group when threshold is high")
	}
	if g.SuggestedMsg != "original" {
		t.Error("SuggestedMsg should not have been changed")
	}
}
