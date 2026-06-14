package repomap

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStructuredOutputIncludesMachineReadableFields(t *testing.T) {
	t.Parallel()

	m := New("/repo", Config{MaxTokens: 2048, Intent: "token refresh", ConsumedPaths: []string{"auth/session.go"}})
	m.coverage = ParseCoverage{
		FilesScanned:      1,
		FilesParsed:       1,
		ByLanguage:        map[string]int{"go": 1},
		ByParseMethod:     map[string]int{"go_ast": 1},
		TreeSitterEnabled: true,
		CtagsEnabled:      false,
	}
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:        "auth/token.go",
				Language:    "go",
				Package:     "auth",
				ImportPath:  "example.com/app/auth",
				ParseMethod: "go_ast",
				Imports:     []string{"sync"},
				Symbols: []Symbol{
					{Name: "RefreshToken", Kind: "function", Exported: true, Line: 12, Signature: "() error"},
				},
			},
			Score:           42,
			ScoreComponents: map[string]int{scoreComponentSymbols: 1, scoreComponentIntent: 41},
			ImportedBy:      2,
			DependsOn:       1,
			DetailLevel:     2,
		},
	}

	out := m.StructuredOutput()
	require.Len(t, out.Files, 1)
	assert.Equal(t, 1, out.SchemaVersion)
	assert.Equal(t, "/repo", out.Root)
	assert.Equal(t, "token refresh", out.Config.Intent)
	assert.Equal(t, []string{"auth/session.go"}, out.Config.ConsumedPaths)
	assert.Equal(t, 1, out.Totals.Files)
	assert.Equal(t, 1, out.Totals.Symbols)
	assert.Equal(t, 1, out.Coverage.FilesScanned)
	assert.Equal(t, 1, out.Coverage.FilesParsed)
	assert.Equal(t, 1, out.Coverage.ByLanguage["go"])
	assert.Equal(t, 1, out.Coverage.ByParseMethod["go_ast"])
	assert.Equal(t, "auth/token.go", out.Files[0].Path)
	assert.Equal(t, "go_ast", out.Files[0].ParseMethod)
	assert.Equal(t, 42, out.Files[0].Score)
	assert.Equal(t, 41, out.Files[0].ScoreComponents[scoreComponentIntent])
	assert.Equal(t, "RefreshToken", out.Files[0].Symbols[0].Name)

	data, err := m.StructuredJSON()
	require.NoError(t, err)
	var decoded StructuredOutput
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, out.Files[0].Path, decoded.Files[0].Path)
	assert.Equal(t, 1, decoded.Coverage.FilesParsed)
}

func TestIntentTokenizeDropsWorkflowStopwords(t *testing.T) {
	t.Parallel()

	tokens := tokenizeIntent("fix bug issue support token refresh cleanup")
	assert.Equal(t, []string{"token", "refresh"}, tokens)
}

func TestStructuredOutputIncludesBudgetOmittedReason(t *testing.T) {
	t.Parallel()

	m := New("/repo", Config{MaxTokens: 1})
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:    "very/long/path/that/will/not/fit.go",
				Symbols: []Symbol{{Name: "Run", Kind: "function", Exported: true}},
			},
			Score:           1,
			ScoreComponents: map[string]int{scoreComponentSymbols: 1},
		},
	}

	out := m.StructuredOutput()
	require.Len(t, out.Files, 1)
	assert.Equal(t, -1, out.Files[0].DetailLevel)
	assert.Equal(t, "budget", out.Files[0].OmittedReason)
}

func TestStructuredOutputFallsBackToCoverageFromRanked(t *testing.T) {
	t.Parallel()

	m := New("/repo", Config{MaxTokens: 0})
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:        "src/app.ts",
				Language:    "typescript",
				ParseMethod: "tree_sitter",
			},
		},
		{
			FileSymbols: &FileSymbols{
				Path:        "src/view.tsx",
				Language:    "tsx",
				ParseMethod: "regex",
			},
		},
	}

	out := m.StructuredOutput()
	assert.Equal(t, 2, out.Coverage.FilesScanned)
	assert.Equal(t, 2, out.Coverage.FilesParsed)
	assert.Equal(t, 1, out.Coverage.ByLanguage["typescript"])
	assert.Equal(t, 1, out.Coverage.ByLanguage["tsx"])
	assert.Equal(t, 1, out.Coverage.ByParseMethod["tree_sitter"])
	assert.Equal(t, 1, out.Coverage.ByParseMethod["regex"])
}

func TestBuildParseCoverageCountsFailures(t *testing.T) {
	t.Parallel()

	coverage := buildParseCoverage(
		[]FileInfo{
			{Path: "ok.go", Language: "go"},
			{Path: "bad.ts", Language: "typescript"},
		},
		[]*FileSymbols{
			{Path: "ok.go", Language: "go", ParseMethod: "go_ast"},
		},
		true,
		false,
	)

	assert.Equal(t, 2, coverage.FilesScanned)
	assert.Equal(t, 1, coverage.FilesParsed)
	assert.Equal(t, 1, coverage.ParseFailures)
	assert.Equal(t, 1, coverage.FailuresByLang["typescript"])
	assert.True(t, coverage.TreeSitterEnabled)
	assert.False(t, coverage.CtagsEnabled)
}

func TestStructuredOutputIncludesRelationEvidence(t *testing.T) {
	t.Parallel()

	m := New("/repo", Config{MaxTokens: 0})
	m.ranked = []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "pkg/auth/auth.go",
				Language: "go",
			},
			ImportedBy: 1,
		},
		{
			FileSymbols: &FileSymbols{
				Path:     "src/session.ts",
				Language: "typescript",
			},
			ImportedBy:      2,
			ScoreComponents: map[string]int{scoreComponentSymbolRefs: 4},
		},
	}

	out := m.StructuredOutput()
	require.Len(t, out.Files, 2)

	goEvidence := out.Files[0].RelationEvidence
	require.Len(t, goEvidence, 1)
	assert.Equal(t, "import_reference", goEvidence[0].Kind)
	assert.Equal(t, "import_graph", goEvidence[0].EvidenceClass)
	assert.Equal(t, "high", goEvidence[0].Confidence)

	tsEvidence := out.Files[1].RelationEvidence
	require.Len(t, tsEvidence, 2)
	assert.Equal(t, "import_reference", tsEvidence[0].Kind)
	assert.Equal(t, "heuristic", tsEvidence[0].EvidenceClass)
	assert.Equal(t, "medium", tsEvidence[0].Confidence)
	assert.NotEmpty(t, tsEvidence[0].Caveat)
	assert.Equal(t, "symbol_reference", tsEvidence[1].Kind)
	assert.Equal(t, "low", tsEvidence[1].Confidence)
}
