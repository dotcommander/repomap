package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyConsumedBonus(t *testing.T) {
	t.Parallel()

	// makeRankedFile builds a RankedFile for test data.
	// opts: optional variadic for ImportPath (string) — everything else is derived.
	makeRankedFile := func(path string, score int, imports []string, opts ...string) RankedFile {
		rf := RankedFile{
			FileSymbols: &FileSymbols{
				Path:    path,
				Imports: imports,
			},
			Score: score,
		}
		if len(opts) > 0 {
			rf.ImportPath = opts[0]
		}
		return rf
	}

	tests := []struct {
		name          string
		ranked        []RankedFile
		consumedPaths map[string]bool
		wantScores    map[string]int // path → expected score
		wantOrder     []string       // expected path order after sort
	}{
		{
			name: "no-op when consumedPaths is empty",
			ranked: []RankedFile{
				makeRankedFile("a.go", 20, nil),
				makeRankedFile("b.go", 10, nil),
			},
			consumedPaths: map[string]bool{},
			wantScores: map[string]int{
				"a.go": 20,
				"b.go": 10,
			},
			wantOrder: []string{"a.go", "b.go"},
		},
		{
			name: "no-op when consumedPaths is nil",
			ranked: []RankedFile{
				makeRankedFile("a.go", 20, nil),
			},
			consumedPaths: nil,
			wantScores:    map[string]int{"a.go": 20},
			wantOrder:     []string{"a.go"},
		},
		{
			name: "consumed file downranked: score halved",
			ranked: []RankedFile{
				makeRankedFile("consumed.go", 20, nil),
				makeRankedFile("fresh.go", 20, nil),
			},
			consumedPaths: map[string]bool{"consumed.go": true},
			wantScores: map[string]int{
				"consumed.go": 10, // 20/2
				"fresh.go":   20,
			},
			wantOrder: []string{"fresh.go", "consumed.go"},
		},
		{
			name: "odd score integer division",
			ranked: []RankedFile{
				makeRankedFile("odd.go", 21, nil),
			},
			consumedPaths: map[string]bool{"odd.go": true},
			wantScores:    map[string]int{"odd.go": 10}, // 21/2 = 10
			wantOrder:    []string{"odd.go"},
		},
		{
			name: "importer of consumed file upranked +15",
			ranked: []RankedFile{
				makeRankedFile("dep.go", 30, nil, "pkg/dep"),
				makeRankedFile("caller.go", 10, []string{"pkg/dep"}, "pkg/caller"),
			},
			consumedPaths: map[string]bool{"dep.go": true},
			wantScores: map[string]int{
				"dep.go":    15, // 30/2
				"caller.go": 25, // 10 + 15
			},
			wantOrder: []string{"caller.go", "dep.go"},
		},
		{
			name: "bonus capped at +45: file importing 4+ consumed deps",
			ranked: []RankedFile{
				makeRankedFile("a.go", 50, nil, "pkg/a"),
				makeRankedFile("b.go", 50, nil, "pkg/b"),
				makeRankedFile("c.go", 50, nil, "pkg/c"),
				makeRankedFile("d.go", 50, nil, "pkg/d"),
				makeRankedFile("heavy.go", 10, []string{"pkg/a", "pkg/b", "pkg/c", "pkg/d"}, "pkg/heavy"),
			},
			consumedPaths: map[string]bool{
				"a.go": true, "b.go": true, "c.go": true, "d.go": true,
			},
			wantScores: map[string]int{
				"a.go":     25, // 50/2
				"b.go":     25,
				"c.go":     25,
				"d.go":     25,
				"heavy.go": 55, // 10 + 45 (cap) not 10 + 60
			},
			wantOrder: []string{"heavy.go", "a.go", "b.go", "c.go", "d.go"},
		},
		{
			name: "unrecognized consumed paths ignored",
			ranked: []RankedFile{
				makeRankedFile("real.go", 20, nil, "pkg/real"),
			},
			consumedPaths: map[string]bool{
				"phantom.go": true, // not in ranked slice
			},
			wantScores: map[string]int{"real.go": 20},
			wantOrder:  []string{"real.go"},
		},
		{
			name: "mixed consumed and non-consumed",
			ranked: []RankedFile{
				makeRankedFile("read1.go", 40, nil, "pkg/read1"),
				makeRankedFile("read2.go", 30, nil, "pkg/read2"),
				makeRankedFile("unread.go", 20, []string{"pkg/read1", "pkg/read2"}, "pkg/unread"),
			},
			consumedPaths: map[string]bool{"read1.go": true, "read2.go": true},
			wantScores: map[string]int{
				"read1.go": 20, // 40/2
				"read2.go": 15, // 30/2
				"unread.go": 50, // 20 + 15 + 15 = 50
			},
			wantOrder: []string{"unread.go", "read1.go", "read2.go"},
		},
		{
			name: "re-sorting after bonus: order reflects new scores",
			ranked: []RankedFile{
				makeRankedFile("top.go", 50, nil, "pkg/top"),
				makeRankedFile("mid.go", 30, []string{"pkg/top"}, "pkg/mid"),
			},
			consumedPaths: map[string]bool{"top.go": true},
			wantScores: map[string]int{
				"top.go": 25, // 50/2
				"mid.go": 45, // 30 + 15
			},
			wantOrder: []string{"mid.go", "top.go"}, // mid overtook top
		},
		{
			name: "score ties broken by path alphabetical order",
			ranked: []RankedFile{
				makeRankedFile("z.go", 20, nil),
				makeRankedFile("a.go", 20, nil),
				makeRankedFile("m.go", 20, nil),
			},
			consumedPaths: map[string]bool{"z.go": true},
			wantScores: map[string]int{
				"z.go": 10,
				"a.go": 20,
				"m.go": 20,
			},
			wantOrder: []string{"a.go", "m.go", "z.go"},
		},
		{
			name: "Go import-path matching: consumed matched by ImportPath not Path",
			ranked: []RankedFile{
				makeRankedFile("internal/svc/handler.go", 40, nil, "myapp/svc"),
				makeRankedFile("cmd/app/main.go", 10, []string{"myapp/svc"}, "myapp/cmd"),
			},
			consumedPaths: map[string]bool{"internal/svc/handler.go": true},
			wantScores: map[string]int{
				"internal/svc/handler.go": 20, // 40/2
				"cmd/app/main.go":        25, // 10 + 15 (matched via ImportPath "myapp/svc")
			},
			wantOrder: []string{"cmd/app/main.go", "internal/svc/handler.go"},
		},
		{
			name: "importer only gets bonus for consumed deps actually in ranked",
			ranked: []RankedFile{
				makeRankedFile("present.go", 30, nil, "pkg/present"),
				makeRankedFile("importer.go", 10, []string{"pkg/present", "pkg/absent"}, "pkg/importer"),
			},
			consumedPaths: map[string]bool{
				"present.go": true,
				"absent.go":  true, // not in ranked → no bonus for pkg/absent
			},
			wantScores: map[string]int{
				"present.go":   15, // 30/2
				"importer.go":  25, // 10 + 15 (only present matched)
			},
			wantOrder: []string{"importer.go", "present.go"},
		},
		{
			name: "non-Go project: basename matching for imports",
			ranked: []RankedFile{
				// No ImportPath → non-Go mode, uses basenameWithoutExt
				{
					FileSymbols: &FileSymbols{
						Path:    "src/utils.py",
						Imports: nil,
					},
					Score: 40,
				},
				{
					FileSymbols: &FileSymbols{
						Path:    "src/main.py",
						Imports: []string{"utils"}, // basename match
					},
					Score: 10,
				},
			},
			consumedPaths: map[string]bool{"src/utils.py": true},
			wantScores: map[string]int{
				"src/utils.py": 20, // 40/2
				"src/main.py": 25, // 10 + 15 (basename "utils" matches)
			},
			wantOrder: []string{"src/main.py", "src/utils.py"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ApplyConsumedBonus(tc.ranked, tc.consumedPaths)

			for _, rf := range tc.ranked {
				if want, ok := tc.wantScores[rf.Path]; ok {
					assert.Equal(t, want, rf.Score, "score for %s", rf.Path)
				}
			}

			require.Equal(t, len(tc.wantOrder), len(tc.ranked), "result length")
			for i, path := range tc.wantOrder {
				assert.Equal(t, path, tc.ranked[i].Path, "position %d", i)
			}
		})
	}
}
