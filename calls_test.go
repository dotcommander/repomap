package repomap

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQuerier is a test double for refsQuerier.
type fakeQuerier struct {
	// results maps "file:symbol" to the locations to return.
	results map[string][]Location
	// err is returned for all calls when non-nil.
	err error
}

func (f *fakeQuerier) Refs(_ context.Context, file string, line int, symbol string) ([]Location, error) {
	if f.err != nil {
		return nil, f.err
	}
	key := file + ":" + symbol
	return f.results[key], nil
}

// makeTestRankedFile builds a minimal RankedFile for calls tests.
func makeTestRankedFile(path string, importedBy int, syms []Symbol) RankedFile {
	return RankedFile{
		FileSymbols: &FileSymbols{
			Path:     path,
			Language: "go",
			Package:  "mypkg",
			Symbols:  syms,
		},
		ImportedBy: importedBy,
	}
}

// --------------------------------------------------------------------------
// ExpandCallers tests
// --------------------------------------------------------------------------

func TestExpandCallers_BasicHit(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "LoadIndex", Kind: "function", Exported: true, Line: 10}
	rf := makeTestRankedFile("internal/index/index.go", 3, []Symbol{sym})

	fq := &fakeQuerier{
		results: map[string][]Location{
			"/repo/internal/index/index.go:LoadIndex": {
				{File: "internal/hook/hook.go", Line: 18, Column: 5},
				{File: "internal/search/scorer.go", Line: 55, Column: 3},
			},
		},
	}

	cfg := CallsConfig{Threshold: 2, Limit: 10}
	callers, stats := ExpandCallers(context.Background(), "/repo", []RankedFile{rf}, cfg, fq, nil)

	require.Equal(t, 1, stats.OK)
	require.Equal(t, 0, stats.Error)
	require.Equal(t, 0, stats.Timeout)

	key := callsKey("internal/index/index.go", "LoadIndex")
	locs := callers[key]
	require.Len(t, locs, 2)
	assert.Equal(t, "internal/hook/hook.go", locs[0].File)
}

func TestExpandCallers_BelowThreshold(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "helper", Kind: "function", Exported: true, Line: 5}
	rf := makeTestRankedFile("internal/util/util.go", 1, []Symbol{sym}) // ImportedBy=1 < threshold=2

	fq := &fakeQuerier{results: map[string][]Location{}}

	cfg := CallsConfig{Threshold: 2, Limit: 10}
	callers, stats := ExpandCallers(context.Background(), "/repo", []RankedFile{rf}, cfg, fq, nil)

	// No tasks should have been generated.
	assert.Equal(t, 0, stats.OK)
	assert.Empty(t, callers)
}

func TestExpandCallers_UnexportedSkipped(t *testing.T) {
	t.Parallel()

	unexported := Symbol{Name: "unexportedHelper", Kind: "function", Exported: false, Line: 8}
	exported := Symbol{Name: "ExportedFunc", Kind: "function", Exported: true, Line: 12}
	rf := makeTestRankedFile("pkg/foo/foo.go", 5, []Symbol{unexported, exported})

	fq := &fakeQuerier{
		results: map[string][]Location{
			"/repo/pkg/foo/foo.go:ExportedFunc": {
				{File: "cmd/main.go", Line: 42, Column: 1},
			},
		},
	}

	cfg := CallsConfig{Threshold: 2, Limit: 10}
	callers, _ := ExpandCallers(context.Background(), "/repo", []RankedFile{rf}, cfg, fq, nil)

	// Only ExportedFunc should have callers.
	assert.Contains(t, callers, callsKey("pkg/foo/foo.go", "ExportedFunc"))
	assert.NotContains(t, callers, callsKey("pkg/foo/foo.go", "unexportedHelper"))
}

func TestExpandCallers_TestFileFiltering(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Parse", Kind: "function", Exported: true, Line: 20}
	rf := makeTestRankedFile("internal/parser/parser.go", 4, []Symbol{sym})

	fq := &fakeQuerier{
		results: map[string][]Location{
			"/repo/internal/parser/parser.go:Parse": {
				{File: "cmd/main.go", Line: 10, Column: 1},
				{File: "internal/parser/parser_test.go", Line: 55, Column: 3},
			},
		},
	}

	t.Run("tests excluded by default", func(t *testing.T) {
		t.Parallel()
		cfg := CallsConfig{Threshold: 2, Limit: 10, IncludeTests: false}
		callers, _ := ExpandCallers(context.Background(), "/repo", []RankedFile{rf}, cfg, fq, nil)
		locs := callers[callsKey("internal/parser/parser.go", "Parse")]
		require.Len(t, locs, 1, "test file should be filtered out")
		assert.Equal(t, "cmd/main.go", locs[0].File)
	})

	t.Run("tests included when requested", func(t *testing.T) {
		t.Parallel()
		cfg := CallsConfig{Threshold: 2, Limit: 10, IncludeTests: true}
		callers, _ := ExpandCallers(context.Background(), "/repo", []RankedFile{rf}, cfg, fq, nil)
		locs := callers[callsKey("internal/parser/parser.go", "Parse")]
		assert.Len(t, locs, 2)
	})
}

func TestExpandCallers_LimitCap(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Run", Kind: "function", Exported: true, Line: 5}
	rf := makeTestRankedFile("core/core.go", 10, []Symbol{sym})

	// Return 8 callers.
	locs := make([]Location, 8)
	for i := range locs {
		locs[i] = Location{File: "cmd/main.go", Line: i + 1, Column: 1}
	}
	fq := &fakeQuerier{
		results: map[string][]Location{"/repo/core/core.go:Run": locs},
	}

	cfg := CallsConfig{Threshold: 2, Limit: 3} // cap at 3
	callers, _ := ExpandCallers(context.Background(), "/repo", []RankedFile{rf}, cfg, fq, nil)
	assert.Len(t, callers[callsKey("core/core.go", "Run")], 3)
}

func TestExpandCallers_DefinitionSiteFiltered(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "New", Kind: "function", Exported: true, Line: 15}
	rf := makeTestRankedFile("pkg/client/client.go", 3, []Symbol{sym})

	// lspq returns the definition site as a reference.
	fq := &fakeQuerier{
		results: map[string][]Location{
			"/repo/pkg/client/client.go:New": {
				{File: "/repo/pkg/client/client.go", Line: 15, Column: 6}, // definition — should be filtered
				{File: "cmd/app.go", Line: 8, Column: 3},
			},
		},
	}

	cfg := CallsConfig{Threshold: 2, Limit: 10}
	callers, _ := ExpandCallers(context.Background(), "/repo", []RankedFile{rf}, cfg, fq, nil)
	locs := callers[callsKey("pkg/client/client.go", "New")]
	require.Len(t, locs, 1)
	assert.Equal(t, "cmd/app.go", locs[0].File)
}

func TestExpandCallers_ErrorCounted(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Do", Kind: "function", Exported: true, Line: 1}
	rf := makeTestRankedFile("svc/svc.go", 5, []Symbol{sym})

	fq := &fakeQuerier{err: errFakeFailure}

	cfg := CallsConfig{Threshold: 1, Limit: 10}
	callers, stats := ExpandCallers(context.Background(), "/repo", []RankedFile{rf}, cfg, fq, nil)

	assert.Equal(t, 1, stats.Error)
	assert.Equal(t, 0, stats.OK)
	assert.Empty(t, callers)
}

// errFakeFailure is used by fakeQuerier to simulate errors.
var errFakeFailure = errStr("fake lspq failure")

type errStr string

func (e errStr) Error() string { return string(e) }

// --------------------------------------------------------------------------
// filterLocations tests
// --------------------------------------------------------------------------

func TestFilterLocations_ExcludesDefinitionSite(t *testing.T) {
	t.Parallel()
	locs := []Location{
		{File: "/repo/foo.go", Line: 10, Column: 1},
		{File: "bar.go", Line: 20, Column: 1},
	}
	cfg := CallsConfig{IncludeTests: true}
	out := filterLocations(locs, cfg, "/repo/foo.go", 10)
	require.Len(t, out, 1)
	assert.Equal(t, "bar.go", out[0].File)
}

func TestFilterLocations_ExcludesTests(t *testing.T) {
	t.Parallel()
	locs := []Location{
		{File: "foo_test.go", Line: 5},
		{File: "bar.go", Line: 9},
	}
	cfg := CallsConfig{IncludeTests: false}
	out := filterLocations(locs, cfg, "/repo/other.go", 1)
	require.Len(t, out, 1)
	assert.Equal(t, "bar.go", out[0].File)
}

// --------------------------------------------------------------------------
// formatCallersInline tests
// --------------------------------------------------------------------------

func TestFormatCallersInline(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", formatCallersInline(nil, 0))
	})

	t.Run("all shown", func(t *testing.T) {
		t.Parallel()
		locs := []Location{
			{File: "a.go", Line: 1},
			{File: "b.go", Line: 2},
		}
		out := formatCallersInline(locs, 2)
		assert.Contains(t, out, "a.go:1")
		assert.Contains(t, out, "b.go:2")
		assert.NotContains(t, out, "total")
	})

	t.Run("truncated shows total", func(t *testing.T) {
		t.Parallel()
		locs := []Location{
			{File: "a.go", Line: 1},
			{File: "b.go", Line: 2},
		}
		out := formatCallersInline(locs, 5) // total=5 but only 2 shown
		assert.Contains(t, out, "(5 total)")
	})
}

// --------------------------------------------------------------------------
// Render with callers tests
// --------------------------------------------------------------------------

func TestFormatMapWithCallers_CompactInjectsCallers(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "LoadIndex", Kind: "function", Exported: true, Line: 10}
	rf := makeRankedFile("internal/index/index.go", 2, []Symbol{sym})
	rf.ImportedBy = 3

	callers := SymbolCallers{
		callsKey("internal/index/index.go", "LoadIndex"): {
			{File: "hook/hook.go", Line: 18},
		},
	}

	out := FormatMapWithCallers([]RankedFile{rf}, 0, false, false, callers, 10, nil)
	assert.Contains(t, out, "callers")
}

func TestFormatMapWithCallers_VerboseInjectsCallerCount(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Build", Kind: "function", Exported: true, Line: 5}
	rf := makeRankedFile("pkg/builder/builder.go", 2, []Symbol{sym})
	rf.ImportedBy = 4

	callers := SymbolCallers{
		callsKey("pkg/builder/builder.go", "Build"): {
			{File: "cmd/main.go", Line: 10},
			{File: "cmd/other.go", Line: 20},
		},
	}

	out := FormatMapWithCallers([]RankedFile{rf}, 0, true, false, callers, 10, nil)
	assert.Contains(t, out, "[callers: 2]")
}

func TestFormatMapWithCallers_EmptyCallersNoAnnotation(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Foo", Kind: "function", Exported: true, Line: 1}
	rf := makeRankedFile("pkg/foo/foo.go", 2, []Symbol{sym})

	out := FormatMapWithCallers([]RankedFile{rf}, 0, true, false, SymbolCallers{}, 10, nil)
	assert.NotContains(t, out, "callers")
}

// --------------------------------------------------------------------------
// Cache key tests
// --------------------------------------------------------------------------

func TestCallsCacheKey_DifferentConfigs(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Foo", Kind: "function", Exported: true, Line: 1}
	rf := makeTestRankedFile("foo.go", 2, []Symbol{sym})
	ranked := []RankedFile{rf}

	cfg1 := CallsConfig{Threshold: 2, Limit: 10}
	cfg2 := CallsConfig{Threshold: 5, Limit: 10}

	k1 := CallsCacheKey("/tmp/repo", ranked, cfg1)
	k2 := CallsCacheKey("/tmp/repo", ranked, cfg2)
	assert.NotEqual(t, k1, k2, "different thresholds should produce different keys")
}

func TestCallsCacheKey_DifferentRoots(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Bar", Kind: "function", Exported: true, Line: 1}
	rf := makeTestRankedFile("bar.go", 2, []Symbol{sym})
	ranked := []RankedFile{rf}
	cfg := CallsConfig{Threshold: 2, Limit: 10}

	k1 := CallsCacheKey("/repo/a", ranked, cfg)
	k2 := CallsCacheKey("/repo/b", ranked, cfg)
	assert.NotEqual(t, k1, k2)
}

// --------------------------------------------------------------------------
// Cache round-trip test
// --------------------------------------------------------------------------

func TestCallsCache_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := "deadbeef12345678"

	callers := SymbolCallers{
		callsKey("foo.go", "Bar"): {
			{File: "cmd/main.go", Line: 42, Column: 1},
		},
	}

	require.NoError(t, SaveCallsCache(dir, hash, callers))

	loaded := LoadCallsCache(dir, hash)
	require.NotNil(t, loaded)

	locs := loaded[callsKey("foo.go", "Bar")]
	require.Len(t, locs, 1)
	assert.Equal(t, "cmd/main.go", locs[0].File)
	assert.Equal(t, 42, locs[0].Line)
}

func TestCallsCache_WrongHash(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, SaveCallsCache(dir, "aaa", SymbolCallers{}))
	result := LoadCallsCache(dir, "bbb")
	assert.Nil(t, result)
}

func TestCallsCache_MissingFile(t *testing.T) {
	t.Parallel()
	result := LoadCallsCache(t.TempDir(), "nonexistent")
	assert.Nil(t, result)
}

// --------------------------------------------------------------------------
// Token budget truncation test
// --------------------------------------------------------------------------

func TestFormatMapWithCallers_TokenBudget(t *testing.T) {
	t.Parallel()

	// Build a large file set to force budget trimming.
	files := make([]RankedFile, 20)
	for i := range files {
		sym := Symbol{Name: "Func", Kind: "function", Exported: true, Line: 1}
		files[i] = makeRankedFile("pkg/module/file.go", 2, []Symbol{sym})
		files[i].ImportedBy = 5
		files[i].Score = 20 - i
	}

	callers := SymbolCallers{
		callsKey("pkg/module/file.go", "Func"): {
			{File: "cmd/main.go", Line: 1},
		},
	}

	// Very tight budget — should still produce output without panicking.
	out := FormatMapWithCallers(files, 128, false, false, callers, 10, nil)
	assert.NotEmpty(t, out)
	// Output should be bounded.
	assert.Less(t, len(out), 128*10, "output should respect budget roughly")
}

// --------------------------------------------------------------------------
// Progress callback test
// --------------------------------------------------------------------------

func TestExpandCallers_ProgressCallback(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Alpha", Kind: "function", Exported: true, Line: 1}
	rf := makeTestRankedFile("pkg/alpha/alpha.go", 3, []Symbol{sym})

	fq := &fakeQuerier{results: map[string][]Location{
		"/repo/pkg/alpha/alpha.go:Alpha": {{File: "cmd/main.go", Line: 1}},
	}}

	var calls []int
	progress := func(done, total int) {
		calls = append(calls, done)
	}

	cfg := CallsConfig{Threshold: 2, Limit: 10}
	_, _ = ExpandCallers(context.Background(), "/repo", []RankedFile{rf}, cfg, fq, progress)

	require.Len(t, calls, 1)
	assert.Equal(t, 1, calls[0])
}

// --------------------------------------------------------------------------
// Compact caller annotation format test
// --------------------------------------------------------------------------

func TestFormatFileBlockCompactWithCallers(t *testing.T) {
	t.Parallel()

	sym := Symbol{Name: "Index", Kind: "function", Exported: true, Line: 10}
	rf := makeRankedFile("internal/index/index.go", 2, []Symbol{sym})
	rf.ImportedBy = 6

	callers := SymbolCallers{
		callsKey("internal/index/index.go", "Index"): {
			{File: "hook/hook.go", Line: 18},
			{File: "search/scorer.go", Line: 55},
			{File: "actions/search.go", Line: 42},
		},
	}

	out := formatFileBlockCompactWithCallers(rf, nil, callers)
	assert.Contains(t, out, "callers")
	assert.True(t, strings.Contains(out, "callers") || strings.Contains(out, "3"))
}
