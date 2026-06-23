package repomap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyIntraPackageRefs_GoOwnerOutranksDTO builds a synthetic single-package
// set where file A's exported type is referenced by B and C. A must end up
// ranked above a DTO-only file D that has more exported symbols but no callers.
func TestApplyIntraPackageRefs_GoOwnerOutranksDTO(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}

	write("a.go", "package pkg\ntype Orchestrator struct{}\nfunc (o *Orchestrator) Run() {}\n")
	write("b.go", "package pkg\nfunc useB() { _ = Orchestrator{} }\n")
	write("c.go", "package pkg\nfunc useC() { var o Orchestrator; _ = o }\n")
	write("d.go", "package pkg\ntype Req struct{}\ntype Resp struct{}\ntype Opts struct{}\n")

	ranked := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path: "a.go", Language: "go", Package: "pkg",
				Symbols: []Symbol{
					{Name: "Orchestrator", Kind: "struct", Exported: true},
					{Name: "Run", Kind: "method", Exported: true},
				},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols:     &FileSymbols{Path: "b.go", Language: "go", Package: "pkg"},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols:     &FileSymbols{Path: "c.go", Language: "go", Package: "pkg"},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path: "d.go", Language: "go", Package: "pkg",
				Symbols: []Symbol{
					{Name: "Req", Kind: "struct", Exported: true},
					{Name: "Resp", Kind: "struct", Exported: true},
					{Name: "Opts", Kind: "struct", Exported: true},
				},
			},
			ScoreComponents: map[string]int{},
		},
	}

	applyDTOPenalty(ranked)
	ApplyIntraPackageRefs(dir, ranked)

	byPath := rankedByPath(ranked)
	assert.Equal(t, 4, byPath["a.go"].ScoreComponents[scoreComponentIntraRefs], "a.go referenced by b.go and c.go → 2*2")
	assert.Greater(t, byPath["a.go"].Score, byPath["d.go"].Score, "owner file A must outrank DTO-only file D")
}

// TestApplyEntryBoosts_PackageNamedOrientation verifies a file named after its
// package receives a positive orientation component (and is not tagged "entry").
func TestApplyEntryBoosts_PackageNamedOrientation(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		{
			FileSymbols:     &FileSymbols{Path: "repomap.go", Language: "go", Package: "repomap"},
			ScoreComponents: map[string]int{},
		},
	}

	applyEntryBoosts(ranked)

	assert.Greater(t, ranked[0].ScoreComponents[scoreComponentOrient], 0, "package-named file gets orientation boost")
	assert.NotEqual(t, "entry", ranked[0].Tag, "orientation boost must not mislabel file as entry")
}

// TestApplyDTOPenalty_DataOnlyFile verifies a DTO-only file (all exported
// symbols are types, no exported functions/methods) receives a negative penalty.
func TestApplyDTOPenalty_DataOnlyFile(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path: "dto.go", Language: "go", Package: "pkg",
				Symbols: []Symbol{
					{Name: "Req", Kind: "struct", Exported: true},
					{Name: "Resp", Kind: "struct", Exported: true},
					{Name: "Header", Kind: "struct", Exported: true},
					{Name: "Status", Kind: "type", Exported: true},
				},
			},
			ScoreComponents: map[string]int{},
		},
	}

	applyDTOPenalty(ranked)

	assert.Less(t, ranked[0].ScoreComponents[scoreComponentDTO], 0, "DTO-only file gets a penalty")
}

// TestApplyDTOPenalty_PureSingleType verifies a pure single exported type gets
// the same DTO penalty despite the count floor.
func TestApplyDTOPenalty_PureSingleType(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path: "pure.go", Language: "go", Package: "pkg",
				Symbols: []Symbol{{Name: "Req", Kind: "type", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path: "mixed.go", Language: "go", Package: "pkg",
				Symbols: []Symbol{
					{Name: "Req", Kind: "type", Exported: true},
					{Name: "Run", Kind: "function", Exported: true},
				},
			},
			ScoreComponents: map[string]int{},
		},
	}

	applyDTOPenalty(ranked)
	byPath := rankedByPath(ranked)

	assert.Equal(t, -12, byPath["pure.go"].ScoreComponents[scoreComponentDTO])
	assert.Equal(t, 0, byPath["mixed.go"].ScoreComponents[scoreComponentDTO])
}

// TestApplyTestDemotion verifies a _test.go file rich in exported symbols ranks
// below a small impl file when includeTests=false, and above it when true.
func TestApplyTestDemotion(t *testing.T) {
	t.Parallel()

	build := func() []RankedFile {
		ranked := []RankedFile{
			{
				FileSymbols: &FileSymbols{
					Path: "big_test.go", Language: "go", Package: "pkg",
					Symbols: []Symbol{
						{Name: "Alpha", Kind: "function", Exported: true},
						{Name: "Beta", Kind: "function", Exported: true},
						{Name: "Gamma", Kind: "function", Exported: true},
						{Name: "Delta", Kind: "function", Exported: true},
					},
				},
				ScoreComponents: map[string]int{},
			},
			{
				FileSymbols: &FileSymbols{
					Path: "small.go", Language: "go", Package: "pkg",
					Symbols: []Symbol{
						{Name: "Do", Kind: "function", Exported: true},
					},
				},
				ScoreComponents: map[string]int{},
			},
		}
		applySymbolBonus(ranked)
		return ranked
	}

	demoted := build()
	applyTestDemotion(demoted, false)
	demotedByPath := rankedByPath(demoted)
	assert.Less(t, demotedByPath["big_test.go"].Score, demotedByPath["small.go"].Score,
		"test file with many exported symbols ranks below impl file when includeTests=false")
	assert.Equal(t, -40, demotedByPath["big_test.go"].ScoreComponents[scoreComponentTestDemote],
		"test file gets the test_demote penalty")

	kept := build()
	applyTestDemotion(kept, true)
	keptByPath := rankedByPath(kept)
	assert.Greater(t, keptByPath["big_test.go"].Score, keptByPath["small.go"].Score,
		"test file outranks impl file when includeTests=true")
	assert.Equal(t, 0, keptByPath["big_test.go"].ScoreComponents[scoreComponentTestDemote],
		"no demotion applied when includeTests=true")
}
