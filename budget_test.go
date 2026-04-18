package repomap

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkSym builds a minimal Symbol for cost/budget tests.
func mkSym(name, kind, sig, doc string, exported bool) Symbol {
	return Symbol{Name: name, Kind: kind, Signature: sig, Doc: doc, Exported: exported}
}

// mkRankedFunc builds a minimal RankedFile with n exported function symbols.
func mkRankedFunc(path string, n int, sigLen, docLen int) RankedFile {
	syms := make([]Symbol, n)
	sig := strings.Repeat("x", sigLen)
	doc := strings.Repeat("d", docLen)
	for i := range syms {
		syms[i] = mkSym(string(rune('A'+i)), "function", "("+sig+") error", doc, true)
	}
	return RankedFile{FileSymbols: &FileSymbols{Path: path, Symbols: syms}}
}

// TestEnrichedCost_ZeroSymbols verifies that a nil symbol slice costs zero.
func TestEnrichedCost_ZeroSymbols(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0, enrichedCost(nil))
}

// TestEnrichedCost_UnexportedIgnored verifies that unexported symbols contribute zero cost.
func TestEnrichedCost_UnexportedIgnored(t *testing.T) {
	t.Parallel()
	syms := []Symbol{
		mkSym("a", "function", "()", "", false),
		mkSym("b", "function", "()", "doc", false),
		mkSym("c", "struct", "{ X int }", "", false),
		mkSym("d", "variable", "", "", false),
		mkSym("e", "constant", "", "", false),
	}
	assert.Equal(t, 0, enrichedCost(syms))
}

// TestEnrichedCost_ExportedCounted verifies a single exported function without doc.
func TestEnrichedCost_ExportedCounted(t *testing.T) {
	t.Parallel()
	name := "Run"
	sig := "(ctx context.Context) error"
	sym := mkSym(name, "function", sig, "", true)
	got := enrichedCost([]Symbol{sym})
	// Minimum: 8 + len(name) + len(sig)
	assert.GreaterOrEqual(t, got, 8+len(name)+len(sig))
}

// TestEnrichedCost_DocAdded verifies that a non-empty Doc increases the cost.
func TestEnrichedCost_DocAdded(t *testing.T) {
	t.Parallel()
	name := "Run"
	sig := "(ctx context.Context) error"
	doc := "starts the loop"
	withoutDoc := enrichedCost([]Symbol{mkSym(name, "function", sig, "", true)})
	withDoc := enrichedCost([]Symbol{mkSym(name, "function", sig, doc, true)})
	// Doc adds at least 8 + len(doc) bytes.
	assert.GreaterOrEqual(t, withDoc, withoutDoc+8+len(doc))
}

// TestEnrichedCost_StructFields verifies that a struct's typed field list is counted
// as part of the name line only — no separate field-block term.
//
// formatFileBlockDefault renders struct symbols inline: "  type Config{Name string, ID int}\n"
// The signature appears once on the name line; enrichedCost must not double-count it.
func TestEnrichedCost_StructFields(t *testing.T) {
	t.Parallel()
	// Use the typed field format that parser_go.go now produces (fields with types).
	sig := "{Name string, ID int}"
	sym := mkSym("Config", "struct", sig, "", true)
	require.True(t, sym.HasFields(), "test setup: HasFields must be true for this symbol")

	// Name line only: 8 + len(Name) + len(Signature)
	// No separate field block — the signature appears inline on the name line.
	expected := 8 + len(sym.Name) + len(sig)
	assert.Equal(t, expected, enrichedCost([]Symbol{sym}))
}

// TestEnrichedCost_MixedSymbols verifies that only exported symbols contribute cost.
func TestEnrichedCost_MixedSymbols(t *testing.T) {
	t.Parallel()
	exported := []Symbol{
		mkSym("A", "function", "()", "", true),
		mkSym("B", "function", "()", "", true),
		mkSym("C", "function", "()", "", true),
	}
	unexported := []Symbol{
		mkSym("a", "function", "()", "", false),
		mkSym("b", "function", "()", "", false),
	}
	mixed := append(exported, unexported...)

	exportedCost := enrichedCost(exported)
	mixedCost := enrichedCost(mixed)
	assert.Equal(t, exportedCost, mixedCost, "unexported symbols must not affect cost")
	assert.Greater(t, exportedCost, 0, "exported symbols must contribute positive cost")
}

// TestBudgetAllOrNothing_FitsEnriched verifies a small file under a generous budget gets level 2.
func TestBudgetAllOrNothing_FitsEnriched(t *testing.T) {
	t.Parallel()
	f := mkRankedFunc("small.go", 2, 10, 0)
	ranked := BudgetFiles([]RankedFile{f}, 2048)
	require.Len(t, ranked, 1)
	assert.Equal(t, 2, ranked[0].DetailLevel, "file with cost well within budget must be level 2")
}

// TestBudgetAllOrNothing_FallsToSummary verifies demotion to level 1 when enriched cost overflows.
func TestBudgetAllOrNothing_FallsToSummary(t *testing.T) {
	t.Parallel()
	// Build a file whose enriched cost exceeds the budget but whose summary fits.
	// Budget: 100 tokens = 400 bytes.
	// File has 30 symbols with sig=20 chars, no doc → enrichedCost ≈ 30*(8+1+20) = 870 bytes > 400.
	// countGroups returns 1 (all "function") → summaryCost = 30 bytes ≤ 400.
	f := mkRankedFunc("large.go", 30, 20, 0)
	ranked := BudgetFiles([]RankedFile{f}, 100)
	require.Len(t, ranked, 1)
	assert.Equal(t, 1, ranked[0].DetailLevel, "file whose enriched cost exceeds budget must fall to level 1")
}

// TestBudgetAllOrNothing_Omitted verifies level -1 when neither enriched nor summary fit.
func TestBudgetAllOrNothing_Omitted(t *testing.T) {
	t.Parallel()
	// Budget: 2 tokens = 8 bytes. Any file with symbols will have enrichedCost > 8.
	// summaryCost = groups*30 > 8 as well.
	// Phase 1 headerCap = 8*70/100 = 5 bytes; path "x.go" = 4+30 = 34 bytes > 5,
	// so cutoff = 0 and the file gets level -1 via the beyond-cutoff loop.
	f := mkRankedFunc("x.go", 1, 5, 0)
	ranked := BudgetFiles([]RankedFile{f}, 2)
	require.Len(t, ranked, 1)
	assert.Equal(t, -1, ranked[0].DetailLevel, "file that overflows even summary budget must be omitted")
}

// TestBudgetAllOrNothing_VerboseNoInvariant verifies that maxTokens=0 gives all files level 2.
func TestBudgetAllOrNothing_VerboseNoInvariant(t *testing.T) {
	t.Parallel()
	files := []RankedFile{
		mkRankedFunc("a.go", 5, 50, 20),
		mkRankedFunc("b.go", 5, 50, 20),
		mkRankedFunc("c.go", 5, 50, 20),
	}
	ranked := BudgetFiles(files, 0)
	require.Len(t, ranked, 3)
	for i, f := range ranked {
		assert.Equal(t, 2, f.DetailLevel, "maxTokens=0 must give DetailLevel=2 for file %d", i)
	}
}

// TestBudgetAllOrNothing_ZeroSymbols verifies that a file with no symbols gets level 0.
func TestBudgetAllOrNothing_ZeroSymbols(t *testing.T) {
	t.Parallel()
	f := RankedFile{FileSymbols: &FileSymbols{Path: "empty.go", Symbols: nil}}
	ranked := BudgetFiles([]RankedFile{f}, 2048)
	require.Len(t, ranked, 1)
	assert.Equal(t, 0, ranked[0].DetailLevel, "file with no symbols must be level 0 regardless of budget")
}

// TestBudgetAllOrNothing_MultiFile verifies mixed detail levels across 3 files under a tight budget.
func TestBudgetAllOrNothing_MultiFile(t *testing.T) {
	t.Parallel()
	// File 1: 2 funcs, sig=5 chars → enrichedCost ≈ 2*(8+1+6) = 30 bytes.
	// File 2: 10 funcs, sig=10 chars → enrichedCost ≈ 10*(8+1+11) = 200 bytes.
	// File 3: 30 funcs, sig=20 chars → enrichedCost ≈ 30*(8+1+21) = 900 bytes.
	//
	// Budget: 200 tokens = 800 bytes.
	// Phase 1 headerCap = 560 bytes; all 3 paths fit (each ~4+30=34 bytes each → 102 total).
	// headerCost ≈ 102 bytes. used = 102.
	// File 1: enrichedCost ≈ 30 → 102+30=132 ≤ 800 → level 2. used=132.
	// File 2: enrichedCost ≈ 200 → 132+200=332 ≤ 800 → level 2. used=332.
	// File 3: enrichedCost ≈ 900 → 332+900=1232 > 800. summaryCost=30 → 332+30=362 ≤ 800 → level 1.
	f1 := mkRankedFunc("a.go", 2, 5, 0)
	f2 := mkRankedFunc("b.go", 10, 10, 0)
	f3 := mkRankedFunc("c.go", 30, 20, 0)
	ranked := BudgetFiles([]RankedFile{f1, f2, f3}, 200)
	require.Len(t, ranked, 3)
	assert.Equal(t, 2, ranked[0].DetailLevel, "file 1 must be level 2")
	assert.Equal(t, 2, ranked[1].DetailLevel, "file 2 must be level 2")
	assert.Equal(t, 1, ranked[2].DetailLevel, "file 3 must fall to level 1")
}

// TestBudgetAllOrNothing_NoPartialLevel2 verifies that no file gets DetailLevel=2
// when its enriched cost exceeds the remaining budget. Proves the invariant.
func TestBudgetAllOrNothing_NoPartialLevel2(t *testing.T) {
	t.Parallel()
	// 5 identical files each with enriched cost that won't fit under a 50-token budget.
	// Budget: 50 tokens = 200 bytes.
	// Each file: 10 funcs, sig=20 chars → enrichedCost ≈ 10*(8+1+21) = 300 bytes > 200.
	// summaryCost = 1 group * 30 = 30 bytes.
	// So each file should end up at level 1 (summary) or -1, never 2.
	files := make([]RankedFile, 5)
	for i := range files {
		files[i] = mkRankedFunc("file.go", 10, 20, 0)
	}
	ranked := BudgetFiles(files, 50)
	require.Len(t, ranked, 5)
	level2Count := 0
	for _, f := range ranked {
		if f.DetailLevel == 2 {
			level2Count++
		}
	}
	assert.Equal(t, 0, level2Count, "no file should get DetailLevel=2 when enriched cost exceeds budget")
}

// TestEnrichedCost_MatchesRenderer verifies enrichedCost stays within ±10% of
// formatFileBlockDefault's actual output length. This is the regression guard
// that ensures budget decisions match what the renderer actually produces.
//
// Three symbol mixes are tested: functions only, functions+doc, and a struct+func mix.
// The path overhead (e.g. "test.go\n") is excluded from the comparison because
// enrichedCost estimates symbol cost only — the caller adds the path separately.
func TestEnrichedCost_MatchesRenderer(t *testing.T) {
	t.Parallel()

	// helper strips the first line (file path header) from formatFileBlockDefault output
	// so we compare only the symbol lines against enrichedCost.
	symbolLines := func(rendered string) string {
		idx := strings.IndexByte(rendered, '\n')
		if idx < 0 {
			return rendered
		}
		return rendered[idx+1:]
	}

	checkWithin10Pct := func(t *testing.T, syms []Symbol, path string) {
		t.Helper()
		f := RankedFile{FileSymbols: &FileSymbols{Path: path, Language: "go", Symbols: syms}}
		rendered := symbolLines(formatFileBlockDefault(f))
		cost := enrichedCost(syms)
		actual := len(rendered)
		lo := int(float64(cost) * 0.90)
		hi := int(float64(cost) * 1.10)
		assert.GreaterOrEqual(t, actual, lo,
			"rendered len %d is below 90%% of enrichedCost %d (lo=%d)", actual, cost, lo)
		assert.LessOrEqual(t, actual, hi,
			"rendered len %d exceeds 110%% of enrichedCost %d (hi=%d)", actual, cost, hi)
	}

	t.Run("functions only", func(t *testing.T) {
		t.Parallel()
		syms := []Symbol{
			mkSym("Run", "function", "(ctx context.Context) error", "", true),
			mkSym("Stop", "function", "()", "", true),
			mkSym("New", "function", "(cfg Config) *Server", "", true),
		}
		checkWithin10Pct(t, syms, "server.go")
	})

	t.Run("functions with doc", func(t *testing.T) {
		t.Parallel()
		syms := []Symbol{
			mkSym("Start", "function", "(addr string) error", "listens and serves HTTP", true),
			mkSym("Shutdown", "function", "(ctx context.Context) error", "gracefully stops the server", true),
		}
		checkWithin10Pct(t, syms, "http.go")
	})

	t.Run("struct and function mix", func(t *testing.T) {
		t.Parallel()
		syms := []Symbol{
			mkSym("Config", "struct", "{Host string, Port int}", "holds server settings", true),
			mkSym("New", "function", "(cfg Config) *Server", "creates a new server", true),
			mkSym("helper", "function", "()", "", false), // unexported — excluded from both sides
		}
		checkWithin10Pct(t, syms, "pkg.go")
	})

	t.Run("mixed with receiver method", func(t *testing.T) {
		t.Parallel()
		syms := []Symbol{
			{Name: "Build", Kind: "method", Receiver: "*Builder", Signature: "(opts Options) error", Exported: true},
			mkSym("Options", "struct", "{Timeout time.Duration, MaxRetry int}", "", true),
		}
		checkWithin10Pct(t, syms, "builder.go")
	})
}
