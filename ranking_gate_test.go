package repomap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type rankingGateTask struct {
	name               string
	question           string
	maxTokens          int
	expectedFiles      []string
	expectedSymbols    []string
	minFileRecall      float64
	minSymbolRecall    float64
	minTokenEfficiency float64
}

func TestRankingGateIntentRetrieval(t *testing.T) {
	t.Parallel()

	root := makeRankingGateRepo(t)
	task := rankingGateTask{
		name:               "token-refresh-debug",
		question:           "fix token refresh race",
		maxTokens:          512,
		expectedFiles:      []string{"auth/token.go", "auth/session.go"},
		expectedSymbols:    []string{"RefreshToken", "TokenSession"},
		minFileRecall:      1.0,
		minSymbolRecall:    1.0,
		minTokenEfficiency: 0.20,
	}

	result := runRankingGateTask(t, root, task)
	require.GreaterOrEqual(t, result.fileRecall, task.minFileRecall, "file recall")
	require.GreaterOrEqual(t, result.symbolRecall, task.minSymbolRecall, "symbol recall")
	require.GreaterOrEqual(t, result.tokenEfficiency, task.minTokenEfficiency, "token efficiency")
}

type rankingGateResult struct {
	fileRecall      float64
	symbolRecall    float64
	tokenEfficiency float64
}

func runRankingGateTask(t *testing.T, root string, task rankingGateTask) rankingGateResult {
	t.Helper()

	m := New(root, Config{
		MaxTokens:      task.maxTokens,
		MaxTokensNoCtx: task.maxTokens,
		Intent:         task.question,
	})
	require.NoError(t, m.Build(context.Background()))
	out := m.StructuredOutput()

	visibleFiles := map[string]bool{}
	visibleSymbols := map[string]bool{}
	for _, file := range out.Files {
		if file.DetailLevel < 0 {
			continue
		}
		visibleFiles[file.Path] = true
		for _, sym := range file.Symbols {
			visibleSymbols[sym.Name] = true
		}
	}

	rendered := m.String()
	rawTokens := rawRepoTokens(t, root)
	renderedTokens := estimateTokens(rendered)
	tokenEfficiency := 0.0
	if rawTokens > 0 {
		tokenEfficiency = float64(rawTokens-renderedTokens) / float64(rawTokens)
	}

	return rankingGateResult{
		fileRecall:      recall(task.expectedFiles, visibleFiles),
		symbolRecall:    recall(task.expectedSymbols, visibleSymbols),
		tokenEfficiency: tokenEfficiency,
	}
}

func recall(expected []string, seen map[string]bool) float64 {
	if len(expected) == 0 {
		return 1.0
	}
	hits := 0
	for _, item := range expected {
		if seen[item] {
			hits++
		}
	}
	return float64(hits) / float64(len(expected))
}

func rawRepoTokens(t *testing.T, root string) int {
	t.Helper()

	total := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		total += estimateTokens(string(data))
		return nil
	})
	require.NoError(t, err)
	return total
}

func makeRankingGateRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeRankingGateFile(t, dir, "go.mod", "module example.com/rankinggate\n\ngo 1.22\n")
	writeRankingGateFile(t, dir, "auth/token.go", `package auth

import "sync"

type TokenSession struct {
	mu sync.Mutex
	value string
}

func RefreshToken(session *TokenSession) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.value = "fresh"
	return nil
}
`)
	writeRankingGateFile(t, dir, "auth/session.go", `package auth

func NewTokenSession() *TokenSession {
	return &TokenSession{}
}

func LoadSession(id string) (*TokenSession, error) {
	return NewTokenSession(), nil
}
`)
	writeRankingGateFile(t, dir, "billing/invoice.go", `package billing

type Invoice struct {
	ID string
}

func CreateInvoice(id string) Invoice {
	return Invoice{ID: id}
}
`)
	writeRankingGateFile(t, dir, "reports/noise.go", "package reports\n\n"+strings.Repeat(`func helperNoise() string {
	return "billing report unrelated to token refresh"
}

`, 80))
	writeRankingGateFile(t, dir, "cmd/app/main.go", `package main

import "example.com/rankinggate/auth"

func main() {
	session, _ := auth.LoadSession("demo")
	_ = auth.RefreshToken(session)
}
`)
	return dir
}

func writeRankingGateFile(t *testing.T, root, rel, content string) {
	t.Helper()

	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
