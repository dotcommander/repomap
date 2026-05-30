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

// TestApplySymbolReferenceBonus_DocFreqFilter verifies that names exported by
// more than 40% of ranked files are treated as identifier-space stop-words and
// excluded from the bonus calculation, while rare names still score.
func TestApplySymbolReferenceBonus_DocFreqFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}

	// "Common" is exported by 3 of 6 files — exceeds the 40% threshold (floor=2,
	// threshold=int(6*0.40)=2, so >2 means ≥3 triggers the drop).
	write("src/a.ts", "export class Common {}\n")
	write("src/b.ts", "export class Common {}\n")
	write("src/c.ts", "export class Common {}\n")

	// "Auth" is exported by exactly 1 file — well under the threshold.
	write("src/auth.ts", "export class Auth {}\n")

	// Two files reference Auth (and also mention Common, which should be filtered).
	write("src/use1.ts", "import { Auth } from './auth'; import { Common } from './a'; Auth.verify();\n")
	write("src/use2.ts", "import { Auth } from './auth'; import { Common } from './b'; Auth.check();\n")

	// 6 ranked files → threshold = int(6*0.40) = 2; names in >2 files are dropped.
	ranked := []RankedFile{
		{
			FileSymbols: &FileSymbols{
				Path: "src/a.ts", Language: "typescript",
				Symbols: []Symbol{{Name: "Common", Kind: "class", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path: "src/b.ts", Language: "typescript",
				Symbols: []Symbol{{Name: "Common", Kind: "class", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path: "src/c.ts", Language: "typescript",
				Symbols: []Symbol{{Name: "Common", Kind: "class", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols: &FileSymbols{
				Path: "src/auth.ts", Language: "typescript",
				Symbols: []Symbol{{Name: "Auth", Kind: "class", Exported: true}},
			},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols:     &FileSymbols{Path: "src/use1.ts", Language: "typescript"},
			ScoreComponents: map[string]int{},
		},
		{
			FileSymbols:     &FileSymbols{Path: "src/use2.ts", Language: "typescript"},
			ScoreComponents: map[string]int{},
		},
	}

	ApplySymbolReferenceBonus(dir, ranked)

	byPath := rankedByPath(ranked)

	// "Common" is a stop-word: the files that export it must not receive a bonus.
	assert.Zero(t, byPath["src/a.ts"].ScoreComponents[scoreComponentSymbolRefs], "Common is a doc-freq stop-word; a.ts must not score")
	assert.Zero(t, byPath["src/b.ts"].ScoreComponents[scoreComponentSymbolRefs], "Common is a doc-freq stop-word; b.ts must not score")
	assert.Zero(t, byPath["src/c.ts"].ScoreComponents[scoreComponentSymbolRefs], "Common is a doc-freq stop-word; c.ts must not score")

	// "Auth" is rare: auth.ts is referenced by use1.ts and use2.ts → bonus = 2*2 = 4.
	assert.Equal(t, 4, byPath["src/auth.ts"].ScoreComponents[scoreComponentSymbolRefs], "Auth is rare; auth.ts must score")
}

func rankedByPath(ranked []RankedFile) map[string]RankedFile {
	out := make(map[string]RankedFile, len(ranked))
	for _, rf := range ranked {
		out[rf.Path] = rf
	}
	return out
}
