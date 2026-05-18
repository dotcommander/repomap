package repomap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplySymbolReferenceBonus(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}

	write("src/service.ts", "export class PaymentService {}\n")
	write("src/controller.ts", "const svc = new PaymentService(); PaymentService.boot();\n")
	write("src/view.py", "from app import PaymentService\nPaymentService()\n")
	write("src/same.ts", "const PaymentService = 'local mention only';\n")
	write("src/tiny.ts", "export class URL {}\n")
	write("src/use_tiny.ts", "URL();\n")

	ranked := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "src/service.ts",
				Language: "typescript",
				Symbols:  []Symbol{{Name: "PaymentService", Kind: "class", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path:     "src/same.ts",
				Language: "typescript",
				Symbols:  []Symbol{{Name: "LocalOnly", Kind: "class", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path:     "src/tiny.ts",
				Language: "typescript",
				Symbols:  []Symbol{{Name: "URL", Kind: "class", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols:     &FileSymbols{Path: "src/controller.ts", Language: "typescript"},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols:     &FileSymbols{Path: "src/view.py", Language: "python"},
			ScoreComponents: map[string]int{},
		},
	}

	ApplySymbolReferenceBonus(dir, ranked)

	byPath := rankedByPath(ranked)
	assert.Equal(t, 6, byPath["src/service.ts"].ScoreComponents[scoreComponentSymbolRefs])
	assert.Equal(t, 6, byPath["src/service.ts"].Score)
	assert.Zero(t, byPath["src/same.ts"].ScoreComponents[scoreComponentSymbolRefs])
	assert.Zero(t, byPath["src/tiny.ts"].ScoreComponents[scoreComponentSymbolRefs], "short symbols are too noisy for lexical refs")
}

func TestApplySymbolReferenceBonus_SkipsGoTargets(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "core.go"), []byte("package core\ntype CoreThing struct{}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "use.ts"), []byte("CoreThing\n"), 0o644))

	ranked := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path:     "core.go",
				Language: "go",
				Symbols:  []Symbol{{Name: "CoreThing", Kind: "struct", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols:     &FileSymbols{Path: "use.ts", Language: "typescript"},
			ScoreComponents: map[string]int{},
		},
	}

	ApplySymbolReferenceBonus(dir, ranked)

	assert.Zero(t, ranked[0].ScoreComponents[scoreComponentSymbolRefs])
	assert.Zero(t, ranked[0].Score)
}

func rankedByPath(ranked []RankedFile) map[string]RankedFile {
	out := make(map[string]RankedFile, len(ranked))
	for _, rf := range ranked {
		out[rf.Path] = rf
	}
	return out
}
