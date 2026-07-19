package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/dotcommander/repomap/internal/lsp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrphansPrinting(t *testing.T) {
	t.Parallel()

	report := repomap.OrphanReport{
		Caveat: "Static analysis warning",
		ZeroRefs: []repomap.OrphanCandidate{
			{Name: "UnusedFunc", Kind: "function", File: "main.go", Line: 10},
			{Name: "UnusedMethod", Receiver: "*Server", Kind: "method", File: "server.go", Line: 25},
		},
		TestOnlyRefs: []repomap.OrphanCandidate{
			{Name: "TestOnlyFunc", Kind: "function", File: "util.go", Line: 15},
		},
	}

	var buf bytes.Buffer
	printOrphans(&buf, report)

	out := buf.String()
	assert.Contains(t, out, "Static analysis warning")
	assert.Contains(t, out, "zero references (incl. tests): 2")
	assert.Contains(t, out, "UnusedFunc")
	assert.Contains(t, out, "(*Server) UnusedMethod")
	assert.Contains(t, out, "test-only references: 1")
	assert.Contains(t, out, "TestOnlyFunc")
}

func TestInventoryHelpers(t *testing.T) {
	t.Parallel()

	t.Run("fileContainsAny", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "db.go")
		require.NoError(t, os.WriteFile(filePath, []byte("func connectPostgres() {}"), 0o644))

		assert.True(t, fileContainsAny(filePath, []string{"postgres", "mysql"}))
		assert.False(t, fileContainsAny(filePath, []string{"redis", "mongo"}))
		assert.False(t, fileContainsAny(filepath.Join(tmpDir, "nonexistent.go"), []string{"postgres"}))
	})

	t.Run("printInventory", func(t *testing.T) {
		report := inventoryReport{
			Boundary:     "Postgres",
			Constructors: []string{"NewDB()"},
			Writers:      nil,
			Readers:      []string{"QueryUser()"},
		}
		var buf bytes.Buffer
		err := printInventory(&buf, report)
		require.NoError(t, err)

		out := buf.String()
		assert.Contains(t, out, "inventory: Postgres")
		assert.Contains(t, out, "constructors:")
		assert.Contains(t, out, "- NewDB()")
		assert.Contains(t, out, "writers:")
		assert.Contains(t, out, "(none)")
		assert.Contains(t, out, "readers:")
		assert.Contains(t, out, "- QueryUser()")
	})
}

func TestContextHelpers(t *testing.T) {
	t.Parallel()

	t.Run("symbolDisplay", func(t *testing.T) {
		symNoSig := repomap.Symbol{Name: "Init"}
		assert.Equal(t, "Init", symbolDisplay(symNoSig))

		symMethod := repomap.Symbol{Name: "Serve", Kind: "method", Receiver: "s *Server", Signature: "() error"}
		assert.Equal(t, "(s *Server) Serve() error", symbolDisplay(symMethod))

		symFunc := repomap.Symbol{Name: "DoWork", Kind: "function", Signature: "(ctx Context)"}
		assert.Equal(t, "DoWork(ctx Context)", symbolDisplay(symFunc))
	})

	t.Run("contextCallers", func(t *testing.T) {
		callers := repomap.SymbolCallers{
			"main.go\x00Target": []repomap.Location{
				{File: "app.go", Line: 10},
				{File: "app_test.go", Line: 20},
				{File: "service.go", Line: 30},
			},
		}
		match := repomap.SymbolMatch{File: "main.go", Symbol: repomap.Symbol{Name: "Target"}}

		// Exclude tests
		res := contextCallers(callers, match, false, 0)
		assert.Len(t, res, 2)
		assert.Equal(t, "app.go", res[0].File)
		assert.Equal(t, "service.go", res[1].File)

		// Include tests with limit
		resLimit := contextCallers(callers, match, true, 2)
		assert.Len(t, resLimit, 2)
	})

	t.Run("printSymbolContext", func(t *testing.T) {
		symCtx := repomap.SymbolContext{
			Match: repomap.SymbolMatch{
				File:   "main.go",
				Symbol: repomap.Symbol{Name: "Run", Kind: "function", Line: 5, Signature: "()"},
			},
			SourceNote: "Found in main package",
			Ambiguous: []repomap.SymbolMatch{
				{File: "runner.go", Symbol: repomap.Symbol{Name: "Run", Kind: "method", Line: 12}},
			},
			Source: []repomap.SourceLine{
				{Number: 5, Text: "func Run() {"},
			},
			Callers: []repomap.Location{
				{File: "app.go", Line: 15, Column: 2},
			},
		}

		var buf bytes.Buffer
		printSymbolContext(&buf, symCtx)
		out := buf.String()

		assert.Contains(t, out, "main.go:5")
		assert.Contains(t, out, "source: Found in main package")
		assert.Contains(t, out, "also matched:")
		assert.Contains(t, out, "callers:")
		assert.Contains(t, out, "app.go:15:2")
	})
}

func TestExplainPrinting(t *testing.T) {
	t.Parallel()

	explain := repomap.ExplainResult{
		File:            repomap.StructuredFile{Path: "service.go"},
		Score:           150,
		DetailLevel:     2,
		ParseMethod:     "regex",
		ParseConfidence: "lexical",
		ScoreComponents: map[string]int{"entry": 100},
		ComponentTiers:  map[string]string{"entry": "confirmed"},
	}

	var buf bytes.Buffer
	printExplain(&buf, explain)
	out := buf.String()

	assert.Contains(t, out, "service.go")
	assert.Contains(t, out, "score: 150")
	assert.Contains(t, out, "detail: 2")
	assert.Contains(t, out, "parsed: regex (lexical-confidence) ⚠ low-fidelity symbols")
	assert.Contains(t, out, "confirmed (gopls-verified)")
	assert.Contains(t, out, "entry        +100")
}

func TestLSPJSONBuildersAndHelpers(t *testing.T) {
	t.Parallel()

	cwd := "/repo"

	t.Run("buildDefJSON", func(t *testing.T) {
		empty := buildDefJSON(nil, cwd)
		assert.Nil(t, empty.Definition)

		locs := []lsp.Location{
			{
				URI: "file:///repo/main.go",
				Range: lsp.Range{
					Start: lsp.Position{Line: 9, Character: 4},
				},
			},
		}
		def := buildDefJSON(locs, cwd)
		require.NotNil(t, def.Definition)
		assert.Equal(t, "main.go", def.Definition.File)
		assert.Equal(t, 10, def.Definition.Line)
		assert.Equal(t, 5, def.Definition.Column)
	})

	t.Run("buildRefsJSON", func(t *testing.T) {
		locs := []lsp.Location{
			{
				URI: "file:///repo/app.go",
				Range: lsp.Range{
					Start: lsp.Position{Line: 14, Character: 2},
				},
			},
		}
		refs := buildRefsJSON(locs, cwd)
		require.Len(t, refs.References, 1)
		assert.Equal(t, "app.go", refs.References[0].File)
		assert.Equal(t, 15, refs.References[0].Line)
	})

	t.Run("buildHoverJSON", func(t *testing.T) {
		assert.Empty(t, buildHoverJSON(nil).Hover)

		h := &lsp.HoverResult{
			Contents: lsp.MarkupContent{Value: "func DoWork()"},
		}
		assert.Equal(t, "func DoWork()", buildHoverJSON(h).Hover)
	})

	t.Run("buildSymbolsJSON", func(t *testing.T) {
		syms := []lsp.DocumentSymbol{
			{
				Name:  "MyStruct",
				Kind:  lsp.SymbolKindStruct,
				Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 0}},
				Children: []lsp.DocumentSymbol{
					{
						Name:  "FieldA",
						Kind:  lsp.SymbolKindField,
						Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 4}},
					},
				},
			},
		}
		out := buildSymbolsJSON(syms, "struct.go")
		require.Len(t, out.Symbols, 2)
		assert.Equal(t, "MyStruct", out.Symbols[0].Name)
		assert.Equal(t, "FieldA", out.Symbols[1].Name)
	})

	t.Run("parsePositionArgs & resolveFilePath", func(t *testing.T) {
		cwd, err := os.Getwd()
		require.NoError(t, err)

		file, line, sym, gotCwd, err := parsePositionArgs([]string{"main.go", "10", "Run"})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(cwd, "main.go"), file)
		assert.Equal(t, 10, line)
		assert.Equal(t, "Run", sym)
		assert.Equal(t, cwd, gotCwd)

		_, _, _, _, errInvalidLine := parsePositionArgs([]string{"main.go", "invalid", "Run"})
		assert.Error(t, errInvalidLine)
	})
}

func TestCommitFinishIOHelpers(t *testing.T) {
	t.Parallel()

	t.Run("buildFinishResult", func(t *testing.T) {
		tag := "v1.2.3"
		relURL := "https://github.com/example/repo/releases/tag/v1.2.3"
		execRes := &repomap.ExecuteResult{
			Commits:    []repomap.CommitRecord{{SHA: "abc1234"}},
			Tag:        &tag,
			ReleaseURL: &relURL,
		}
		res := buildFinishResult("success", execRes, "all good")
		assert.Equal(t, "success", res.Status)
		assert.Equal(t, "v1.2.3", res.Tag)
		assert.Equal(t, relURL, res.ReleaseURL)
		assert.Equal(t, "all good", res.FailureDetail)
	})

	t.Run("parseDecisions", func(t *testing.T) {
		rawJSON := `{"review_decisions": [{"group_id": "group-1", "action": "accept"}]}`
		dec, err := parseDecisions(rawJSON)
		require.NoError(t, err)
		assert.Len(t, dec.ReviewDecisions, 1)

		tmpDir := t.TempDir()
		decFile := filepath.Join(tmpDir, "decisions.json")
		require.NoError(t, os.WriteFile(decFile, []byte(rawJSON), 0o644))

		decFromFile, err := parseDecisions("@" + decFile)
		require.NoError(t, err)
		assert.Len(t, decFromFile.ReviewDecisions, 1)
	})

	t.Run("bumpLevel", func(t *testing.T) {
		assert.Equal(t, "v2.0.0", bumpLevel(nil, "v2.0.0"))

		breakingGroups := []repomap.CommitGroup{{Breaking: true}}
		assert.Equal(t, "major", bumpLevel(breakingGroups, ""))

		featGroups := []repomap.CommitGroup{{Type: "feat"}}
		assert.Equal(t, "minor", bumpLevel(featGroups, ""))

		fixGroups := []repomap.CommitGroup{{Type: "fix"}}
		assert.Equal(t, "patch", bumpLevel(fixGroups, ""))
	})
}

func TestServeMethods(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	goMod := "module example.com/servetest\ngo 1.22\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0o644))
	mainGo := "package main\n\nfunc ServeApp() {}\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0o644))

	m := repomap.New(tmpDir, repomap.DefaultConfig())
	require.NoError(t, m.Build(context.Background()))

	server := &serveServer{
		root: tmpDir,
		m:    m,
	}

	t.Run("rpcMapRender", func(t *testing.T) {
		formats := []string{"", "compact", "verbose", "detail", "lines", "xml", "structured"}
		for _, fmtName := range formats {
			req := rawRequest{Params: []byte(`{"format": "` + fmtName + `"}`)}
			res, rpcErr := server.rpcMapRender(req)
			require.Nil(t, rpcErr)
			renderRes, ok := res.(mapRenderResult)
			require.True(t, ok)
			assert.NotEmpty(t, renderRes.Content)
		}

		invalidReq := rawRequest{Params: []byte(`{"format": "invalid_format"}`)}
		_, errRes := server.rpcMapRender(invalidReq)
		require.NotNil(t, errRes)
	})

	t.Run("rpcMapStatus", func(t *testing.T) {
		req := rawRequest{}
		res, rpcErr := server.rpcMapStatus(req)
		require.Nil(t, rpcErr)
		statusRes, ok := res.(mapStatusResult)
		require.True(t, ok)
		assert.Equal(t, tmpDir, statusRes.Root)
		assert.False(t, statusRes.Stale)
	})

	t.Run("rpcSymbolFind", func(t *testing.T) {
		req := rawRequest{Params: []byte(`{"query": "ServeApp"}`)}
		res, rpcErr := server.rpcSymbolFind(req)
		require.Nil(t, rpcErr)
		findRes, ok := res.(symbolFindResult)
		require.True(t, ok)
		assert.NotEmpty(t, findRes.Matches)
	})

	t.Run("rpcErr", func(t *testing.T) {
		e := rpcErr(123, "custom error")
		assert.Equal(t, 123, e.Code)
		assert.Equal(t, "custom error", e.Message)
	})
}
