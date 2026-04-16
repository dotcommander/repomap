package repomap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findTestRoot walks up from cwd to find the repo root (go.mod).
func findTestRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("cannot find repo root")
		}
		dir = parent
	}
}

func TestParseFindQuery(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input    string
		wantName string
		wantKind string
		wantFile string
	}{
		{"Config", "Config", "", ""},
		{"kind:struct:Config", "Config", "struct", ""},
		{"file:parser:Parse", "Parse", "", "parser"},
		{"kind:struct:file:cli:Root", "Root", "struct", "cli"},
		{"file:cli:kind:struct:Root", "Root", "struct", "cli"},
		{"", "", "", ""},
		{"   ", "", "", ""},
		{"kind:func:New", "New", "func", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			name, kind, file := ParseFindQuery(tc.input)
			assert.Equal(t, tc.wantName, name, "name")
			assert.Equal(t, tc.wantKind, kind, "kind")
			assert.Equal(t, tc.wantFile, file, "file")
		})
	}
}

func TestFindSymbol(t *testing.T) {
	t.Parallel()

	root := findTestRoot(t)
	m := New(root, DefaultConfig())
	require.NoError(t, m.Build(context.Background()))

	t.Run("exact match", func(t *testing.T) {
		t.Parallel()
		hits := m.FindSymbol("Map", "", "")
		require.NotEmpty(t, hits)
		// First result must be exact (score=100).
		assert.Equal(t, float64(100), hits[0].Score)
		// Should come from repomap.go.
		found := false
		for _, h := range hits {
			if h.Score == 100 && strings.Contains(h.File, "repomap.go") {
				found = true
				break
			}
		}
		assert.True(t, found, "expected exact Map hit in repomap.go")
	})

	t.Run("case-insensitive exact", func(t *testing.T) {
		t.Parallel()
		hits := m.FindSymbol("map", "", "")
		require.NotEmpty(t, hits)
		hasCI := false
		for _, h := range hits {
			if h.Score == 75 {
				hasCI = true
				break
			}
		}
		assert.True(t, hasCI, "expected at least one score=75 case-insensitive hit")
	})

	t.Run("prefix match", func(t *testing.T) {
		t.Parallel()
		hits := m.FindSymbol("Find", "", "")
		require.NotEmpty(t, hits)
		// FindSymbol itself should appear with score >= 50 (prefix match).
		found := false
		for _, h := range hits {
			if h.Symbol.Name == "FindSymbol" {
				assert.GreaterOrEqual(t, h.Score, float64(50), "FindSymbol must score as prefix or better")
				found = true
				break
			}
		}
		assert.True(t, found, "expected FindSymbol in prefix results")
		// The first result must be prefix-or-better (sorted by score desc).
		assert.GreaterOrEqual(t, hits[0].Score, float64(50), "top result must be prefix or better")
	})

	t.Run("contains match", func(t *testing.T) {
		t.Parallel()
		// "Rank" is contained in RankFiles, RankedFile, etc.
		hits := m.FindSymbol("ank", "", "")
		require.NotEmpty(t, hits)
		for _, h := range hits {
			assert.Equal(t, float64(25), h.Score)
		}
	})

	t.Run("kind filter", func(t *testing.T) {
		t.Parallel()
		hits := m.FindSymbol("Config", "struct", "")
		require.NotEmpty(t, hits)
		for _, h := range hits {
			assert.Equal(t, "struct", h.Symbol.Kind)
		}
	})

	t.Run("file filter", func(t *testing.T) {
		t.Parallel()
		hits := m.FindSymbol("New", "", "ranker")
		for _, h := range hits {
			assert.Contains(t, h.File, "ranker")
		}
	})

	t.Run("combined kind and file", func(t *testing.T) {
		t.Parallel()
		hits := m.FindSymbol("Map", "struct", "repomap.go")
		require.NotEmpty(t, hits)
		for _, h := range hits {
			assert.Equal(t, "struct", h.Symbol.Kind)
			assert.Contains(t, h.File, "repomap.go")
		}
	})

	t.Run("empty name returns empty slice", func(t *testing.T) {
		t.Parallel()
		hits := m.FindSymbol("", "", "")
		assert.NotNil(t, hits)
		assert.Empty(t, hits)
	})

	t.Run("unbuilt map no panic", func(t *testing.T) {
		t.Parallel()
		fresh := New(".", DefaultConfig())
		hits := fresh.FindSymbol("X", "", "")
		assert.NotNil(t, hits)
		assert.Empty(t, hits)
	})

	t.Run("tiebreaker ordering", func(t *testing.T) {
		t.Parallel()
		// Build a minimal in-memory Map with two same-named symbols in two files
		// with different scores to verify sort order.
		lowFile := &FileSymbols{
			Path:    "z_low_score.go",
			Symbols: []Symbol{{Name: "Foo", Kind: "func"}},
		}
		highFile := &FileSymbols{
			Path:    "a_high_score.go",
			Symbols: []Symbol{{Name: "Foo", Kind: "func"}},
		}
		tm := &Map{}
		tm.ranked = []RankedFile{
			{FileSymbols: lowFile, Score: 10},
			{FileSymbols: highFile, Score: 100},
		}
		hits := tm.FindSymbol("Foo", "", "")
		require.Len(t, hits, 2)
		// Higher-scored file must come first when symbol score ties.
		assert.Equal(t, "a_high_score.go", hits[0].File)
		assert.Equal(t, "z_low_score.go", hits[1].File)
	})
}
