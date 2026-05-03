package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyCallerBonus(t *testing.T) {
	t.Parallel()

	makeFile := func(path string, score int) RankedFile {
		return RankedFile{
			FileSymbols: &FileSymbols{Path: path},
			Score:       score,
		}
	}

	tests := []struct {
		name       string
		ranked     []RankedFile
		counts     map[string]int
		wantScores map[string]int // path → expected score
		wantOrder  []string       // expected path order after sort
	}{
		{
			name: "basic bonus: more callers → higher score",
			ranked: []RankedFile{
				makeFile("b.go", 10),
				makeFile("a.go", 10),
			},
			counts: map[string]int{
				"a.go": 3,
				"b.go": 1,
			},
			wantScores: map[string]int{
				"a.go": 10 + 6, // 3*2
				"b.go": 10 + 2, // 1*2
			},
			wantOrder: []string{"a.go", "b.go"},
		},
		{
			name: "cap at +30: 20 callers → bonus 30, not 40",
			ranked: []RankedFile{
				makeFile("hot.go", 5),
			},
			counts: map[string]int{
				"hot.go": 20,
			},
			wantScores: map[string]int{
				"hot.go": 5 + 30,
			},
			wantOrder: []string{"hot.go"},
		},
		{
			name: "empty map: no panic, no score changes",
			ranked: []RankedFile{
				makeFile("x.go", 7),
			},
			counts:     map[string]int{},
			wantScores: map[string]int{"x.go": 7},
			wantOrder:  []string{"x.go"},
		},
		{
			name:       "nil map: no panic",
			ranked:     []RankedFile{makeFile("y.go", 3)},
			counts:     nil,
			wantScores: map[string]int{"y.go": 3},
			wantOrder:  []string{"y.go"},
		},
		{
			name: "file not in counts: score unchanged",
			ranked: []RankedFile{
				makeFile("known.go", 10),
				makeFile("unknown.go", 8),
			},
			counts: map[string]int{"known.go": 2},
			wantScores: map[string]int{
				"known.go":   10 + 4,
				"unknown.go": 8,
			},
			wantOrder: []string{"known.go", "unknown.go"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ApplyCallerBonus(tc.ranked, tc.counts)

			for _, rf := range tc.ranked {
				if want, ok := tc.wantScores[rf.Path]; ok {
					assert.Equal(t, want, rf.Score, "score for %s", rf.Path)
				}
			}

			require.Equal(t, len(tc.wantOrder), len(tc.ranked))
			for i, path := range tc.wantOrder {
				assert.Equal(t, path, tc.ranked[i].Path, "position %d", i)
			}
		})
	}
}

func TestCallerCountsFromSymbolCallers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		callers SymbolCallers
		want    map[string]int
	}{
		{
			name:    "empty map",
			callers: SymbolCallers{},
			want:    map[string]int{},
		},
		{
			name: "single symbol, two unique caller files",
			callers: SymbolCallers{
				callsKey("pkg/foo.go", "Bar"): {
					{File: "main.go", Line: 10},
					{File: "cmd/run.go", Line: 5},
				},
			},
			want: map[string]int{"pkg/foo.go": 2},
		},
		{
			name: "duplicate caller file across locations de-duped",
			callers: SymbolCallers{
				callsKey("pkg/foo.go", "Bar"): {
					{File: "main.go", Line: 10},
					{File: "main.go", Line: 20}, // same file, different line
				},
			},
			want: map[string]int{"pkg/foo.go": 1},
		},
		{
			name: "multiple symbols in same target file: union of callers",
			callers: SymbolCallers{
				callsKey("svc/handler.go", "New"):    {{File: "main.go"}, {File: "cmd/a.go"}},
				callsKey("svc/handler.go", "Handle"): {{File: "main.go"}, {File: "cmd/b.go"}},
			},
			// unique callers: main.go, cmd/a.go, cmd/b.go → 3
			want: map[string]int{"svc/handler.go": 3},
		},
		{
			name: "malformed key without separator: skipped",
			callers: SymbolCallers{
				"no-separator": {{File: "other.go"}},
			},
			want: map[string]int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CallerCountsFromSymbolCallers(tc.callers)
			assert.Equal(t, tc.want, got)
		})
	}
}
