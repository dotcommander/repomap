package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeIntentRankedFile builds a minimal RankedFile for intent scoring tests.
func makeIntentRankedFile(path, pkg string, score int, symbols []Symbol, imports []string) RankedFile {
	return RankedFile{
		FileSymbols: &FileSymbols{
			Path:    path,
			Package: pkg,
			Symbols: symbols,
			Imports: imports,
		},
		Score: score,
	}
}

func TestIntentScorer_NoQuery(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		makeIntentRankedFile("scanner.go", "repomap", 100, nil, nil),
		makeIntentRankedFile("parser.go", "repomap", 80, nil, nil),
		makeIntentRankedFile("ranker.go", "repomap", 60, nil, nil),
	}
	scorer := NewIntentScorer(ranked)
	result := scorer.Score(ranked, "")
	require.Len(t, result, 3)
	assert.Equal(t, "scanner.go", result[0].Path)
	assert.Equal(t, "parser.go", result[1].Path)
	assert.Equal(t, "ranker.go", result[2].Path)
}

func TestIntentScorer_ExactMatch(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		makeIntentRankedFile("scanner.go", "repomap", 100, nil, nil),
		makeIntentRankedFile("parser.go", "repomap", 80, nil, nil),
		makeIntentRankedFile("auth_token.go", "repomap", 50, nil, nil),
	}
	scorer := NewIntentScorer(ranked)
	result := scorer.Score(ranked, "token auth")
	require.Len(t, result, 3)
	assert.Equal(t, "auth_token.go", result[0].Path, "auth_token.go should rank highest for 'token auth'")
}

func TestIntentScorer_SymbolMatch(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		makeIntentRankedFile("scanner.go", "repomap", 100, []Symbol{
			{Name: "ScanFiles", Exported: true},
		}, nil),
		makeIntentRankedFile("parser.go", "repomap", 80, []Symbol{
			{Name: "ParseGoFile", Exported: true},
			{Name: "ParseSymbols", Exported: true},
		}, nil),
		makeIntentRankedFile("ranker.go", "repomap", 60, []Symbol{
			{Name: "RankFiles", Exported: true},
		}, nil),
	}
	scorer := NewIntentScorer(ranked)
	result := scorer.Score(ranked, "parse symbols")
	require.Len(t, result, 3)
	assert.Equal(t, "parser.go", result[0].Path, "parser.go should rank highest for 'parse symbols'")
}

func TestIntentScorer_PluralMatch(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		makeIntentRankedFile("scanner.go", "repomap", 100, nil, nil),
		makeIntentRankedFile("token.go", "repomap", 80, []Symbol{
			{Name: "Token", Exported: true},
			{Name: "TokenType", Exported: true},
		}, nil),
	}
	scorer := NewIntentScorer(ranked)
	result := scorer.Score(ranked, "tokens")
	require.Len(t, result, 2)
	assert.Equal(t, "token.go", result[0].Path, "token.go should rank highest for 'tokens' (plural match)")
}

func TestIntentScorer_PrefixMatch(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		makeIntentRankedFile("scanner.go", "repomap", 100, nil, nil),
		makeIntentRankedFile("authentication.go", "repomap", 50, []Symbol{
			{Name: "Authenticate", Exported: true},
			{Name: "Authorize", Exported: true},
		}, nil),
	}
	scorer := NewIntentScorer(ranked)
	result := scorer.Score(ranked, "auth")
	// "auth" is only 4 chars, so prefix matching kicks in with min 4 chars
	require.Len(t, result, 2)
	assert.Equal(t, "authentication.go", result[0].Path, "authentication.go should rank highest for 'auth' (prefix match)")
}

func TestIntentScorer_Negation(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		makeIntentRankedFile("parser.go", "repomap", 100, []Symbol{
			{Name: "ParseGoFile", Exported: true},
		}, nil),
		makeIntentRankedFile("parser_test.go", "repomap", 80, []Symbol{
			{Name: "TestParseGoFile", Exported: false},
		}, nil),
	}
	scorer := NewIntentScorer(ranked)
	result := scorer.Score(ranked, "parser not test")
	require.Len(t, result, 2)
	assert.Equal(t, "parser.go", result[0].Path, "parser.go should rank above parser_test.go when negating 'test'")
}

func TestIntentScorer_BigramBonus(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		makeIntentRankedFile("token.go", "repomap", 100, []Symbol{
			{Name: "Token", Exported: true},
		}, nil),
		makeIntentRankedFile("refresh.go", "repomap", 100, []Symbol{
			{Name: "Refresh", Exported: true},
		}, nil),
		makeIntentRankedFile("token_refresh.go", "repomap", 100, []Symbol{
			{Name: "TokenRefresh", Exported: true},
			{Name: "RefreshToken", Exported: true},
		}, nil),
	}
	scorer := NewIntentScorer(ranked)
	result := scorer.Score(ranked, "token refresh")
	require.Len(t, result, 3)
	assert.Equal(t, "token_refresh.go", result[0].Path, "token_refresh.go should rank highest for bigram 'token refresh'")
}

func TestIntentScorer_PreservesStructuralRanking(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		makeIntentRankedFile("main.go", "main", 200, []Symbol{
			{Name: "Run", Exported: true},
		}, nil),
		makeIntentRankedFile("obscure.go", "repomap", 10, nil, nil),
	}
	scorer := NewIntentScorer(ranked)
	result := scorer.Score(ranked, "zzzzunmatchedquery")
	require.Len(t, result, 2)
	// main.go should still be first — no BM25 match means multiplier is 1.0 for all
	assert.Equal(t, "main.go", result[0].Path, "high structural score should be preserved when no BM25 match")
}

func TestIntentScorer_CamelCaseSplit(t *testing.T) {
	t.Parallel()
	ranked := []RankedFile{
		makeIntentRankedFile("scanner.go", "repomap", 100, nil, nil),
		makeIntentRankedFile("parser.go", "repomap", 80, []Symbol{
			{Name: "ParseGoFile", Exported: true},
		}, nil),
	}
	scorer := NewIntentScorer(ranked)
	// "parse" should match "ParseGoFile" via camelCase split
	result := scorer.Score(ranked, "parse")
	require.Len(t, result, 2)
	assert.Equal(t, "parser.go", result[0].Path, "parser.go should rank highest via CamelCase 'parse' from ParseGoFile")
}

func TestTokenizeIntent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  []string
	}{
		{"parse symbols", []string{"parse", "symbols"}},
		{"token-refresh handler", []string{"token-refresh", "handler"}},
		{"fix the parser", []string{"parser"}}, // "fix" and "the" are stopwords; "parser" is kept
		{"BM25 ranking", []string{"bm25", "ranking"}},
		{"", nil},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := tokenizeIntent(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTokenizeCamelCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  []string
	}{
		{"ParseGoFile", []string{"parse", "go", "file"}},
		{"http_client", []string{"http", "client"}},
		{"ALLCAPS", []string{"allcaps"}},
		{"NewIntentScorer", []string{"new", "intent", "scorer"}},
		{"BM25Score", []string{"bm25score"}},
		{"simple", []string{"simple"}},
		{"", nil},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := tokenizeCamelCase(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestExtractNegated(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   []string
		keep    []string
		negated []string
	}{
		{
			input:   []string{"parser", "not", "test"},
			keep:    []string{"parser"},
			negated: []string{"test"},
		},
		{
			input:   []string{"auth", "without", "vendor"},
			keep:    []string{"auth"},
			negated: []string{"vendor"},
		},
		{
			input:   []string{"scanner", "except", "bench"},
			keep:    []string{"scanner"},
			negated: []string{"bench"},
		},
		{
			input:   []string{"token", "no", "cache"},
			keep:    []string{"token"},
			negated: []string{"cache"},
		},
		{
			input:   []string{"parser", "scanner"},
			keep:    []string{"parser", "scanner"},
			negated: nil,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input[0], func(t *testing.T) {
			t.Parallel()
			keep, negated := extractNegated(tc.input)
			assert.Equal(t, tc.keep, keep)
			assert.Equal(t, tc.negated, negated)
		})
	}
}
