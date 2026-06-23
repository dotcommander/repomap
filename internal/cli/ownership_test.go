package cli

import (
	"strings"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/stretchr/testify/assert"
)

// rf is a fixture helper: a ranked file at path with score and exported symbols.
func rf(path string, score int, syms ...string) repomap.RankedFile {
	var symbols []repomap.Symbol
	for _, n := range syms {
		symbols = append(symbols, repomap.Symbol{Name: n, Exported: true})
	}
	return repomap.RankedFile{
		FileSymbols: &repomap.FileSymbols{Path: path, Symbols: symbols},
		Score:       score,
	}
}

func TestBriefOwnership(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		ranked      []repomap.RankedFile
		wantEmpty   bool
		wantHeader  bool
		wantContain []string
		wantOrder   []string // substrings that must appear in this relative order
		wantAbsent  []string
	}{
		{
			name: "multi-feature repo produces hints",
			ranked: []repomap.RankedFile{
				rf("internal/cli/brief.go", 60, "NewBriefCmd"),
				rf("internal/cli/root.go", 50, "Execute"),
				rf("internal/auth/token.go", 90, "Validate"),
				rf("internal/auth/middleware.go", 80, "Require"),
			},
			wantHeader: true,
			wantContain: []string{
				"## Likely ownership",
				"internal/auth/ — auth (2 files",
				"internal/cli/ — cli (2 files",
			},
			// auth aggregate score 170 > cli 110 → auth first
			wantOrder: []string{"internal/auth/", "internal/cli/"},
		},
		{
			name: "flat repo suppressed (all root)",
			ranked: []repomap.RankedFile{
				rf("main.go", 50, "Main"),
				rf("util.go", 40, "Util"),
				rf("config.go", 30, "Load"),
			},
			wantEmpty: true,
		},
		{
			name: "single cluster suppressed",
			ranked: []repomap.RankedFile{
				rf("internal/cli/brief.go", 60, "NewBriefCmd"),
				rf("internal/cli/root.go", 50, "Execute"),
				rf("main.go", 40, "Main"),
			},
			wantEmpty: true,
		},
		{
			name: "generic segment walks up to meaningful label",
			ranked: []repomap.RankedFile{
				rf("features/footer/lib/types.ts", 70, "FooterProps"),
				rf("features/footer/lib/render.ts", 60, "Render"),
				rf("features/header/lib/types.ts", 50, "HeaderProps"),
				rf("features/header/lib/render.ts", 40, "Render"),
			},
			wantHeader: true,
			wantContain: []string{
				"features/footer/lib/ — footer (2 files",
				"features/header/lib/ — header (2 files",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := briefOwnership(tc.ranked)
			if tc.wantEmpty {
				assert.Empty(t, got)
				return
			}
			if tc.wantHeader {
				assert.True(t, strings.HasPrefix(got, "\n## Likely ownership\n"), "got:\n%s", got)
			}
			for _, want := range tc.wantContain {
				assert.Contains(t, got, want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, got, absent)
			}
			last := -1
			for _, frag := range tc.wantOrder {
				idx := strings.Index(got, frag)
				assert.GreaterOrEqual(t, idx, 0, "missing %q in:\n%s", frag, got)
				assert.Greater(t, idx, last, "fragment %q out of order in:\n%s", frag, got)
				last = idx
			}
		})
	}
}

func TestOwnerLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		dir  string
		want string
	}{
		{"internal/cli", "cli"},
		{"internal/auth", "auth"},
		{"features/footer/lib", "footer"},
		{"a/b/src", "b"},
		{"internal", "internal"}, // all-generic → raw last segment
		{"lib", "lib"},
	}
	for _, tc := range tests {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ownerLabel(tc.dir))
		})
	}
}

func TestTopClusterSymbols(t *testing.T) {
	t.Parallel()
	files := []repomap.RankedFile{
		rf("a/low.go", 10, "Low1", "Low2"),
		rf("a/high.go", 90, "High1", "High2", "High3", "High4"),
	}
	got := topClusterSymbols(files)
	// highest-scored file's exports first, capped at 3
	assert.Equal(t, []string{"High1", "High2", "High3"}, got)
}
