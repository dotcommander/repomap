package repomap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findBenchRoot walks up from cwd to find the repo root (go.mod).
func findBenchRoot(b *testing.B) string {
	b.Helper()
	dir, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			b.Skip("cannot find repo root")
		}
		dir = parent
	}
}

func BenchmarkBuild(b *testing.B) {
	root := findBenchRoot(b)
	for b.Loop() {
		m := New(root, DefaultConfig())
		if err := m.Build(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStale(b *testing.B) {
	root := findBenchRoot(b)
	m := New(root, DefaultConfig())
	if err := m.Build(context.Background()); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		m.Stale()
	}
}

// TestLanguageFor checks extension-to-language mapping.
func TestLanguageFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ext  string
		want string
	}{
		{".go", "go"},
		{".ts", "typescript"},
		{".py", "python"},
		{".rs", "rust"},
		{".java", "java"},
		{".txt", ""},
		{".md", ""},
		{".json", ""},
	}

	for _, tc := range tests {
		t.Run(tc.ext, func(t *testing.T) {
			t.Parallel()
			got := LanguageFor(tc.ext)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestParseGoFile verifies that ParseGoFile correctly extracts exported symbols,
// imports, package name, and import path from a Go source file.
func TestParseGoFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	goMod := "module example.com/test\ngo 1.22\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644))

	src := `package test

import "fmt"

type Server struct{}
func New() *Server { return nil }
func (s *Server) Run() error { return nil }
func helper() {}
const Version = "1.0"
var ErrNotFound = fmt.Errorf("not found")
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte(src), 0o644))

	fs, err := ParseGoFile(filepath.Join(dir, "test.go"), dir)
	require.NoError(t, err)

	assert.Equal(t, "test", fs.Package)
	assert.Equal(t, "example.com/test", fs.ImportPath)
	assert.Contains(t, fs.Imports, "fmt")

	byName := make(map[string]Symbol, len(fs.Symbols))
	for _, s := range fs.Symbols {
		byName[s.Name] = s
	}

	// Exported symbols must be present.
	serverSym, ok := byName["Server"]
	require.True(t, ok, "expected Server symbol")
	assert.Equal(t, "struct", serverSym.Kind)

	newSym, ok := byName["New"]
	require.True(t, ok, "expected New symbol")
	assert.Equal(t, "function", newSym.Kind)

	runSym, ok := byName["Run"]
	require.True(t, ok, "expected Run symbol")
	assert.Equal(t, "method", runSym.Kind)
	assert.Equal(t, "*Server", runSym.Receiver)

	versionSym, ok := byName["Version"]
	require.True(t, ok, "expected Version symbol")
	assert.Equal(t, "constant", versionSym.Kind)

	errSym, ok := byName["ErrNotFound"]
	require.True(t, ok, "expected ErrNotFound symbol")
	assert.Equal(t, "variable", errSym.Kind)

	// Unexported function must be absent.
	_, hasHelper := byName["helper"]
	assert.False(t, hasHelper, "helper is unexported and must be excluded")
}

// TestParseGenericFile_Python verifies symbol and import extraction from Python.
func TestParseGenericFile_Python(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	src := `import os
from pathlib import Path

class MyClass:
    pass

def process():
    pass

MAX_SIZE = 100

def _private():
    pass
`
	path := filepath.Join(dir, "lib.py")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	fs, err := ParseGenericFile(path, dir, "python")
	require.NoError(t, err)

	byName := make(map[string]Symbol, len(fs.Symbols))
	for _, s := range fs.Symbols {
		byName[s.Name] = s
	}

	// Symbols.
	cls, ok := byName["MyClass"]
	require.True(t, ok, "expected MyClass symbol")
	assert.Equal(t, "class", cls.Kind)

	proc, ok := byName["process"]
	require.True(t, ok, "expected process symbol")
	assert.Equal(t, "function", proc.Kind)

	maxSize, ok := byName["MAX_SIZE"]
	require.True(t, ok, "expected MAX_SIZE symbol")
	assert.Equal(t, "const", maxSize.Kind)

	// _private starts with '_', which the pyFunc pattern ([A-Za-z]) does not match,
	// so it is intentionally excluded from symbols. We verify it is absent.
	_, hasPrivate := byName["_private"]
	assert.False(t, hasPrivate, "_private uses leading underscore not matched by pyFunc pattern")

	// Imports.
	assert.Contains(t, fs.Imports, "os")
	assert.Contains(t, fs.Imports, "pathlib")
}

// TestRankFiles verifies scoring, ordering, and depth penalties.
func TestRankFiles(t *testing.T) {
	t.Parallel()

	makeFile := func(path, importPath, pkg string, symCount int, imports []string) *FileSymbols {
		syms := make([]Symbol, symCount)
		for i := range syms {
			syms[i] = Symbol{Name: strings.ToUpper(string(rune('A' + i))), Kind: "function", Exported: true}
		}
		return &FileSymbols{
			Path:       path,
			Language:   "go",
			Package:    pkg,
			ImportPath: importPath,
			Symbols:    syms,
			Imports:    imports,
		}
	}

	// core/types.go and core/agent.go are imported by 3 other files each.
	coreTypes := makeFile("core/types.go", "mod/core", "core", 5, nil)
	coreAgent := makeFile("core/agent.go", "mod/core", "core", 3, nil)
	cmdMain := makeFile("cmd/main.go", "mod/cmd", "main", 1, nil)
	deep := makeFile("internal/deep/nested/helper.go", "mod/internal/deep/nested", "helper", 1, nil)

	// Three consumer files that import mod/core.
	consumer := func(n int) *FileSymbols {
		return makeFile(
			"app/consumer"+string(rune('0'+n))+".go",
			"mod/app",
			"app",
			0,
			[]string{"mod/core"},
		)
	}

	files := []*FileSymbols{
		cmdMain,
		coreTypes,
		coreAgent,
		deep,
		consumer(1),
		consumer(2),
		consumer(3),
	}

	ranked := RankFiles(files)

	// Run twice — output must be identical (deterministic).
	ranked2 := RankFiles(files)
	require.Equal(t, len(ranked), len(ranked2))
	for i := range ranked {
		assert.Equal(t, ranked[i].Path, ranked2[i].Path, "position %d not deterministic", i)
	}

	// Files with symbols should all appear.
	paths := make([]string, 0, len(ranked))
	for _, r := range ranked {
		paths = append(paths, r.Path)
	}

	assert.Contains(t, paths, "cmd/main.go")
	assert.Contains(t, paths, "core/types.go")
	assert.Contains(t, paths, "core/agent.go")
	assert.Contains(t, paths, "internal/deep/nested/helper.go")

	// Core files (same package) should each have ImportedBy=3 from the three consumers.
	for _, r := range ranked {
		if r.ImportPath == "mod/core" {
			assert.Equal(t, 3, r.ImportedBy, "%s must have ImportedBy=3", r.Path)
		}
		if r.ImportPath == "mod/internal/deep/nested" {
			assert.Equal(t, 0, r.ImportedBy, "deep file with no importers must have ImportedBy=0")
		}
	}

	// The deep file (depth penalty, no importers) must rank last among files with symbols.
	var lastWithSymbols int
	for i, r := range ranked {
		if len(r.Symbols) > 0 {
			lastWithSymbols = i
		}
	}
	assert.Equal(t, "internal/deep/nested/helper.go", ranked[lastWithSymbols].Path,
		"deep nested file with no importers should rank last among files with symbols")

	// The first result must be a high-value file (cmd/main.go entry boost or a core/ file).
	first := ranked[0].Path
	isHighValue := first == "cmd/main.go" || strings.HasPrefix(first, "core/")
	assert.True(t, isHighValue, "top-ranked file should be cmd/main.go or a core/ file, got %s", first)
}

// TestFormatMap verifies tag annotation and score display in output.
func TestFormatMap(t *testing.T) {
	t.Parallel()

	entry := RankedFile{
		FileSymbols: &FileSymbols{
			Path: "cmd/main.go",
			Symbols: []Symbol{
				{Name: "Run", Kind: "function"},
				{Name: "Stop", Kind: "function"},
			},
		},
		Tag:   "entry",
		Score: 50,
	}
	mid := RankedFile{
		FileSymbols: &FileSymbols{
			Path: "core/types.go",
			Symbols: []Symbol{
				{Name: "Agent", Kind: "struct"},
				{Name: "Config", Kind: "struct"},
				{Name: "New", Kind: "function"},
			},
		},
		Score:      30,
		ImportedBy: 3,
	}
	low := RankedFile{
		FileSymbols: &FileSymbols{
			Path:     "util/misc.go",
			Language: "go",
			Symbols: []Symbol{
				{Name: "Helper", Kind: "function"},
			},
		},
		Score: 0,
	}

	out := FormatMap([]RankedFile{entry, mid, low}, 8192, false, false, nil)

	assert.True(t, strings.HasPrefix(out, "## Repository Map"), "output must start with '## Repository Map'")
	assert.Contains(t, out, "[entry]")
	assert.Contains(t, out, "[imported by 3]")

	// Zero-score file must have no annotation bracket.
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "util/misc.go") {
			assert.NotContains(t, line, "[", "zero-score file must have no annotation")
		}
	}
}

// TestFormatMap_TokenBudget verifies that output is truncated with a footer
// when maxTokens is very small.
func TestFormatMap_TokenBudget(t *testing.T) {
	t.Parallel()

	makeRanked := func(path string, score int, n int) RankedFile {
		syms := make([]Symbol, n)
		for i := range syms {
			syms[i] = Symbol{Name: strings.ToUpper(string(rune('A' + i))), Kind: "function"}
		}
		return RankedFile{
			FileSymbols: &FileSymbols{Path: path, Symbols: syms},
			Score:       score,
		}
	}

	files := []RankedFile{
		makeRanked("a.go", 10, 2),
		makeRanked("b.go", 5, 3),
		makeRanked("c.go", 1, 1),
	}

	// maxTokens=3 → budgetBytes=12, far smaller than any single file block.
	out := FormatMap(files, 3, false, false, nil)
	assert.NotEmpty(t, out)

	// Footer must appear because not all content fits. v0.7.0 format: omission
	// count + recovery hint.
	assert.Contains(t, out, "files omitted", "truncated output must contain omission footer")
	assert.Contains(t, out, "increase -t", "truncated output must contain recovery hint")
}

// TestFormatMap_Empty verifies that an empty slice returns "".
func TestFormatMap_Empty(t *testing.T) {
	t.Parallel()

	out := FormatMap(nil, 4096, false, false, nil)
	assert.Equal(t, "", out)

	out = FormatMap([]RankedFile{}, 4096, false, false, nil)
	assert.Equal(t, "", out)
}

// TestScanFiles verifies that ScanFiles returns only supported language files
// and excludes vendor directories and unsupported extensions.
func TestScanFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create files.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.py"), []byte("x = 1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# hi"), 0o644))

	vendorDir := filepath.Join(dir, "vendor")
	require.NoError(t, os.MkdirAll(vendorDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "dep.go"), []byte("package dep"), 0o644))

	// Init git repo so scanGit is used and vendor/ exclusion follows git ls-files rules.
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "-c", "user.email=test@test.com", "-c", "user.name=Test",
		"commit", "-m", "init")
	cmd.Dir = dir
	_ = cmd.Run()

	files, err := ScanFiles(context.Background(), dir)
	require.NoError(t, err)

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}

	assert.Contains(t, paths, "main.go", "main.go should be found")
	assert.Contains(t, paths, "lib.py", "lib.py should be found")

	for _, p := range paths {
		assert.NotEqual(t, "readme.md", p, "readme.md must not be returned (unsupported ext)")
		assert.False(t, strings.HasPrefix(p, "vendor/"), "vendor files must not be returned")
	}

	// All returned files must have a supported language.
	for _, f := range files {
		assert.NotEmpty(t, f.Language, "every returned file must have a language, got empty for %s", f.Path)
	}
}

// TestBuildIntegration exercises the full scan → parse → rank → format pipeline
// on a minimal Go project.
func TestBuildIntegration(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()

	goMod := "module example.com/myapp\ngo 1.22\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644))

	mainSrc := `package main

type App struct{}
func New() *App { return nil }
func (a *App) Run() error { return nil }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o644))

	helperSrc := `package main

import "fmt"

const Version = "0.1"
var ErrBad = fmt.Errorf("bad")
func Init() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helper.go"), []byte(helperSrc), 0o644))

	// Init git repo so ScanFiles uses git ls-files.
	cmds := [][]string{
		{"git", "init"},
		{"git", "add", "."},
		{"git", "-c", "user.email=t@t.com", "-c", "user.name=T", "commit", "-m", "init"},
	}
	for _, args := range cmds {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		_ = c.Run()
	}

	m := New(dir, DefaultConfig())
	err := m.Build(context.Background())
	require.NoError(t, err)

	out := m.String()
	require.NotEmpty(t, out, "Build must produce non-empty output")

	assert.Contains(t, out, "## Repository Map")
	assert.Contains(t, out, "App")
	assert.Contains(t, out, "New")
	assert.Contains(t, out, "Run")
	assert.Contains(t, out, "Version")
}

// TestFormatMap_Verbose verifies that verbose mode shows all symbols without summarization.
func TestFormatMap_Verbose(t *testing.T) {
	t.Parallel()

	makeFile := func(path string, nSymbols int) RankedFile {
		syms := make([]Symbol, nSymbols)
		for i := range nSymbols {
			syms[i] = Symbol{Name: fmt.Sprintf("Symbol%d", i), Kind: "function", Exported: true}
		}
		return RankedFile{
			FileSymbols: &FileSymbols{Path: path, Symbols: syms},
			Score:       10,
		}
	}

	files := []RankedFile{
		makeFile("big.go", 10),
		makeFile("small.go", 2),
	}

	// Default (budget) mode shows exported symbol names directly (no category summaries).
	defaultOut := FormatMap(files, 8192, false, false, nil)
	assert.Contains(t, defaultOut, "Symbol0")
	assert.Contains(t, defaultOut, "Symbol9")

	// Verbose mode shows all symbols grouped by category.
	verbose := FormatMap(files, 8192, true, false, nil)
	assert.Contains(t, verbose, "Symbol9") // Last symbol must be visible
	// Verbose uses group labels, not the enriched default format.
	assert.Contains(t, verbose, "funcs:")
}

// TestParseGoFile_PackageMain verifies that main() and init() are captured
// as unexported symbols in package main files.
func TestParseGoFile_PackageMain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	goMod := "module example.com/app\ngo 1.22\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644))

	src := `package main

func main() {}
func init() {}
func helper() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644))

	fs, err := ParseGoFile(filepath.Join(dir, "main.go"), dir)
	require.NoError(t, err)

	byName := make(map[string]Symbol, len(fs.Symbols))
	for _, s := range fs.Symbols {
		byName[s.Name] = s
	}

	// main and init must be captured.
	mainSym, ok := byName["main"]
	require.True(t, ok, "expected main symbol")
	assert.Equal(t, "function", mainSym.Kind)
	assert.False(t, mainSym.Exported, "main must not be marked exported")
	assert.Greater(t, mainSym.Line, 0, "main must have a line number")

	initSym, ok := byName["init"]
	require.True(t, ok, "expected init symbol")
	assert.Equal(t, "function", initSym.Kind)
	assert.False(t, initSym.Exported, "init must not be marked exported")

	// helper must NOT be captured (only main/init get special treatment).
	_, hasHelper := byName["helper"]
	assert.False(t, hasHelper, "helper is unexported and must not be captured")
}

// TestFormatMap_ZeroSymbolFiles verifies that files with no exported symbols
// appear in the output with a header-only block.
func TestFormatMap_ZeroSymbolFiles(t *testing.T) {
	t.Parallel()

	withSymbols := RankedFile{
		FileSymbols: &FileSymbols{
			Path:    "core/types.go",
			Symbols: []Symbol{{Name: "Agent", Kind: "struct", Exported: true}},
		},
		Score: 30,
	}
	noSymbols := RankedFile{
		FileSymbols: &FileSymbols{
			Path:    "cmd/main.go",
			Package: "main",
		},
		Tag:   "entry",
		Score: 50,
	}

	files := []RankedFile{noSymbols, withSymbols}

	out := FormatMap(files, 8192, false, false, nil)

	// Header must count all files.
	assert.Contains(t, out, "2 files", "header must count all files including zero-symbol")

	// Zero-symbol file must appear.
	assert.Contains(t, out, "cmd/main.go [entry]", "zero-symbol entry point must appear")
	assert.Contains(t, out, "(package main)", "zero-symbol file must show package info")

	// File with symbols must still appear.
	assert.Contains(t, out, "core/types.go")
	assert.Contains(t, out, "Agent")
}

// TestSummarizeGroup_MethodDedup verifies that methods with the same name
// on different receivers are displayed with receiver qualification.
func TestSummarizeGroup_MethodDedup(t *testing.T) {
	t.Parallel()

	syms := []Symbol{
		{Name: "Scan", Kind: "method", Receiver: "DirScanner"},
		{Name: "Scan", Kind: "method", Receiver: "GlobScanner"},
		{Name: "Scan", Kind: "method", Receiver: "ListScanner"},
	}

	result := summarizeGroup("methods", syms)

	// Must NOT contain bare "Scan, Scan, Scan".
	assert.NotContains(t, result, "Scan, Scan, Scan", "duplicate method names must be qualified")

	// Must contain receiver-qualified names.
	assert.Contains(t, result, "DirScanner.Scan")
	assert.Contains(t, result, "GlobScanner.Scan")
	assert.Contains(t, result, "ListScanner.Scan")
}

// TestApplyDiagnosticSignals_DependsOn verifies internal import counting.
func TestApplyDiagnosticSignals_DependsOn(t *testing.T) {
	t.Parallel()

	files := []*FileSymbols{
		{Path: "core/types.go", Language: "go", ImportPath: "mod/core", Symbols: []Symbol{{Name: "T", Kind: "struct", Exported: true}}},
		{Path: "core/agent.go", Language: "go", ImportPath: "mod/core", Imports: []string{"mod/config", "mod/util"}, Symbols: []Symbol{{Name: "Agent", Kind: "struct", Exported: true}}},
		{Path: "core/util.go", Language: "go", ImportPath: "mod/util", Symbols: []Symbol{{Name: "Helper", Kind: "function", Exported: true}}},
		{Path: "cmd/main.go", Language: "go", ImportPath: "mod/cmd", Imports: []string{"fmt", "os", "mod/core"}, Symbols: []Symbol{{Name: "main", Kind: "function", Exported: false}}},
	}

	ranked := RankFiles(files)

	byPath := make(map[string]RankedFile, len(ranked))
	for _, r := range ranked {
		byPath[r.Path] = r
	}

	// agent.go imports 2 internal packages (mod/config is not a file, but mod/util is).
	// Only imports matching a known ImportPath count as internal.
	agent := byPath["core/agent.go"]
	assert.Equal(t, 1, agent.DependsOn, "agent.go imports mod/util (internal) but not mod/config (unknown); expected DependsOn=1")

	// main.go imports mod/core (internal) + 2 external.
	main := byPath["cmd/main.go"]
	assert.Equal(t, 1, main.DependsOn, "main.go imports 1 internal package (mod/core)")

	// types.go has no imports.
	types := byPath["core/types.go"]
	assert.Equal(t, 0, types.DependsOn, "types.go has no imports")
}

// TestApplyDiagnosticSignals_Untested verifies untested flag for Go packages.
func TestApplyDiagnosticSignals_Untested(t *testing.T) {
	t.Parallel()

	t.Run("no_test_file", func(t *testing.T) {
		t.Parallel()

		files := []*FileSymbols{
			{Path: "core/agent.go", Language: "go", ImportPath: "mod/core", Symbols: []Symbol{{Name: "Agent", Kind: "struct", Exported: true}}},
		}
		ranked := RankFiles(files)
		assert.True(t, ranked[0].Untested, "file with no test file should be untested")
	})

	t.Run("with_test_file", func(t *testing.T) {
		t.Parallel()

		files := []*FileSymbols{
			{Path: "core/agent.go", Language: "go", ImportPath: "mod/core", Symbols: []Symbol{{Name: "Agent", Kind: "struct", Exported: true}}},
			{Path: "core/agent_test.go", Language: "go", ImportPath: "mod/core", Symbols: []Symbol{{Name: "TestAgent", Kind: "function", Exported: false}}},
		}
		ranked := RankFiles(files)

		for _, r := range ranked {
			if r.Path == "core/agent.go" {
				assert.False(t, r.Untested, "file with test coverage should not be untested")
			}
		}
	})

	t.Run("test_file_no_test_symbols", func(t *testing.T) {
		t.Parallel()

		// A _test.go file with only helper functions (no Test/Benchmark/Fuzz) doesn't count.
		files := []*FileSymbols{
			{Path: "core/agent.go", Language: "go", ImportPath: "mod/core", Symbols: []Symbol{{Name: "Agent", Kind: "struct", Exported: true}}},
			{Path: "core/agent_test.go", Language: "go", ImportPath: "mod/core", Symbols: []Symbol{{Name: "setupFixture", Kind: "function", Exported: false}}},
		}
		ranked := RankFiles(files)

		for _, r := range ranked {
			if r.Path == "core/agent.go" {
				assert.True(t, r.Untested, "file with test file but no test symbols should still be untested")
			}
		}
	})

	t.Run("no_exported_symbols", func(t *testing.T) {
		t.Parallel()

		// Files with no exported symbols should not be tagged.
		files := []*FileSymbols{
			{Path: "core/agent.go", Language: "go", ImportPath: "mod/core", Symbols: []Symbol{{Name: "helper", Kind: "function", Exported: false}}},
		}
		ranked := RankFiles(files)
		assert.False(t, ranked[0].Untested, "file with no exported symbols should not be tagged untested")
	})
}

// TestApplyDiagnosticSignals_NonGoFallback verifies import counting for non-Go files.
func TestApplyDiagnosticSignals_NonGoFallback(t *testing.T) {
	t.Parallel()

	t.Run("above_threshold", func(t *testing.T) {
		t.Parallel()

		files := []*FileSymbols{
			{Path: "app.py", Language: "python", Imports: []string{"os", "sys", "json", "pathlib"}, Symbols: []Symbol{{Name: "run", Kind: "function", Exported: true}}},
		}
		ranked := RankFiles(files)
		assert.Equal(t, 4, ranked[0].DependsOn, "Python file with 4 imports (>= threshold) should show DependsOn=4")
	})

	t.Run("below_threshold", func(t *testing.T) {
		t.Parallel()

		files := []*FileSymbols{
			{Path: "app.py", Language: "python", Imports: []string{"os", "sys"}, Symbols: []Symbol{{Name: "run", Kind: "function", Exported: true}}},
		}
		ranked := RankFiles(files)
		assert.Equal(t, 0, ranked[0].DependsOn, "Python file with 2 imports (< threshold) should show DependsOn=0")
	})

	t.Run("deduplication", func(t *testing.T) {
		t.Parallel()

		files := []*FileSymbols{
			{Path: "app.py", Language: "python", Imports: []string{"os", "os", "sys", "json"}, Symbols: []Symbol{{Name: "run", Kind: "function", Exported: true}}},
		}
		ranked := RankFiles(files)
		assert.Equal(t, 3, ranked[0].DependsOn, "duplicate imports should be deduplicated: 3 unique from 4 total")
	})
}

// TestFormatFileLine_DependsOn verifies that DependsOn appears in rendered output.
func TestFormatFileLine_DependsOn(t *testing.T) {
	t.Parallel()

	f := RankedFile{
		FileSymbols: &FileSymbols{Path: "core/agent.go"},
		DependsOn:   4,
	}
	line := formatFileLine(f)
	assert.Contains(t, line, "imports: 4", "DependsOn should render as 'imports: 4'")
	assert.Contains(t, line, "core/agent.go", "path should be present")
}

// TestFormatFileLine_CombinedTags verifies all 5 tags render together.
func TestFormatFileLine_CombinedTags(t *testing.T) {
	t.Parallel()

	f := RankedFile{
		FileSymbols: &FileSymbols{
			Path:        "core/agent.go",
			ParseMethod: "regex",
		},
		Tag:        "entry",
		ImportedBy: 3,
		DependsOn:  2,
		Untested:   true,
	}
	line := formatFileLine(f)

	assert.Contains(t, line, "entry", "must contain 'entry' tag")
	assert.Contains(t, line, "imported by 3", "must contain 'imported by 3' tag")
	assert.Contains(t, line, "imports: 2", "must contain 'imports: 2' tag")
	assert.Contains(t, line, "untested", "must contain 'untested' tag")
	assert.Contains(t, line, "inferred", "must contain 'inferred' tag")

	// Verify tag ordering: entry before imported-by before imports before untested before inferred.
	entryIdx := strings.Index(line, "entry")
	importedByIdx := strings.Index(line, "imported by 3")
	importsIdx := strings.Index(line, "imports: 2")
	untestedIdx := strings.Index(line, "untested")
	inferredIdx := strings.Index(line, "inferred")

	assert.Less(t, entryIdx, importedByIdx, "entry should come before imported by")
	assert.Less(t, importedByIdx, importsIdx, "imported by should come before imports")
	assert.Less(t, importsIdx, untestedIdx, "imports should come before untested")
	assert.Less(t, untestedIdx, inferredIdx, "untested should come before inferred")
}

// TestSymbolBonusCap verifies that exported symbol bonus is capped at 20.
func TestSymbolBonusCap(t *testing.T) {
	t.Parallel()

	// File with 30 exported functions — bonus should cap at 20.
	syms := make([]Symbol, 30)
	for i := range syms {
		syms[i] = Symbol{Name: fmt.Sprintf("Func%d", i), Kind: "function", Exported: true}
	}
	files := []*FileSymbols{
		{Path: "big.go", Language: "go", Symbols: syms},
	}

	ranked := RankFiles(files)
	// Entry boost: 0 (not main.go). Symbol bonus: capped at 20. Depth: 0.
	assert.LessOrEqual(t, ranked[0].Score, 20+50, "score should not exceed bonus cap + max entry boost")
	assert.GreaterOrEqual(t, ranked[0].Score, 20, "score should include at least the capped bonus")
}

// TestKindWeighting verifies that different symbol kinds contribute different scores.
func TestKindWeighting(t *testing.T) {
	t.Parallel()

	t.Run("interface_weights_more_than_function", func(t *testing.T) {
		t.Parallel()

		ifaceFile := &FileSymbols{
			Path:     "a.go",
			Language: "go",
			Symbols:  []Symbol{{Name: "Handler", Kind: "interface", Exported: true}},
		}
		funcFile := &FileSymbols{
			Path:     "b.go",
			Language: "go",
			Symbols:  []Symbol{{Name: "Run", Kind: "function", Exported: true}},
		}

		ranked := RankFiles([]*FileSymbols{ifaceFile, funcFile})

		byPath := make(map[string]RankedFile)
		for _, r := range ranked {
			byPath[r.Path] = r
		}

		// Interface (weight 3) should score higher than function (weight 1).
		assert.Greater(t, byPath["a.go"].Score, byPath["b.go"].Score,
			"interface should rank higher than function with kind weighting")
	})

	t.Run("struct_weights_more_than_constant", func(t *testing.T) {
		t.Parallel()

		structFile := &FileSymbols{
			Path:     "s.go",
			Language: "go",
			Symbols:  []Symbol{{Name: "Config", Kind: "struct", Exported: true}},
		}
		constFile := &FileSymbols{
			Path:     "c.go",
			Language: "go",
			Symbols:  []Symbol{{Name: "Version", Kind: "constant", Exported: true}},
		}

		ranked := RankFiles([]*FileSymbols{structFile, constFile})

		byPath := make(map[string]RankedFile)
		for _, r := range ranked {
			byPath[r.Path] = r
		}

		// Struct (weight 2) should score higher than constant (weight 0).
		assert.Greater(t, byPath["s.go"].Score, byPath["c.go"].Score,
			"struct should rank higher than constant with kind weighting")
	})
}

// TestLineSpan verifies the Symbol.LineSpan method.
func TestLineSpan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		line     int
		endLine  int
		expected int
	}{
		{"normal span", 10, 20, 11},
		{"single line", 5, 5, 0},    // EndLine == Line → 0 (unknown span)
		{"unknown end", 5, 0, 0},    // EndLine == 0 → unknown
		{"unknown start", 0, 10, 0}, // Line == 0 → unknown
		{"inverted", 20, 10, 0},     // EndLine < Line → invalid
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := Symbol{Line: tc.line, EndLine: tc.endLine}
			assert.Equal(t, tc.expected, s.LineSpan())
		})
	}
}

// TestSizeTag verifies size annotation formatting.
func TestSizeTag(t *testing.T) {
	t.Parallel()

	t.Run("above_threshold", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Line: 1, EndLine: 100}
		assert.Equal(t, " [100L]", sizeTag(s))
	})

	t.Run("at_threshold", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Line: 1, EndLine: 50}
		assert.Equal(t, " [50L]", sizeTag(s))
	})

	t.Run("below_threshold", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Line: 1, EndLine: 49}
		assert.Equal(t, "", sizeTag(s))
	})

	t.Run("unknown_span", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Line: 1, EndLine: 0}
		assert.Equal(t, "", sizeTag(s))
	})
}

// TestFormatFileBlockVerbose_SizeAnnotation verifies large symbols get [NL] in verbose mode.
func TestFormatFileBlockVerbose_SizeAnnotation(t *testing.T) {
	t.Parallel()

	f := RankedFile{
		FileSymbols: &FileSymbols{
			Path: "big.go",
			Symbols: []Symbol{
				{Name: "TinyFunc", Kind: "function", Exported: true, Line: 1, EndLine: 10},
				{Name: "GodObject", Kind: "struct", Exported: true, Line: 1, EndLine: 200},
			},
		},
		Score: 10,
	}

	out := formatFileBlockVerbose(f)
	assert.NotContains(t, out, "TinyFunc [", "small symbol should have no size tag")
	assert.Contains(t, out, "GodObject [200L]", "large struct should have size tag")
}

// TestAnnotationTag verifies combined diagnostic annotations.
func TestAnnotationTag(t *testing.T) {
	t.Parallel()

	t.Run("size_only", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Kind: "function", Line: 1, EndLine: 100}
		assert.Equal(t, " [100L]", annotationTag(s))
	})

	t.Run("fat_params", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Kind: "function", ParamCount: 5}
		assert.Equal(t, " [5p]", annotationTag(s))
	})

	t.Run("params_at_threshold", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Kind: "function", ParamCount: 4}
		assert.Equal(t, "", annotationTag(s), "4 params is exactly at threshold (> 4 triggers)")
	})

	t.Run("wide_returns", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Kind: "function", ResultCount: 3}
		assert.Equal(t, " [3r]", annotationTag(s))
	})

	t.Run("combined_signals", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Kind: "function", Line: 1, EndLine: 185, ParamCount: 5, ResultCount: 3}
		assert.Equal(t, " [185L, 5p, 3r]", annotationTag(s))
	})

	t.Run("wide_interface", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Kind: "interface", ParamCount: 7}
		assert.Equal(t, " [7m]", annotationTag(s))
	})

	t.Run("struct_not_smelly", func(t *testing.T) {
		t.Parallel()
		// Struct members aren't tagged — only size applies.
		s := Symbol{Kind: "struct", ParamCount: 40}
		assert.Equal(t, "", annotationTag(s))
	})

	t.Run("method_with_fat_signature", func(t *testing.T) {
		t.Parallel()
		s := Symbol{Kind: "method", Receiver: "*Agent", ParamCount: 6, ResultCount: 1}
		assert.Equal(t, " [6p]", annotationTag(s))
	})
}

// TestFuncSignatureCountsFromAST verifies ParamCount/ResultCount populate from Go AST.
func TestFuncSignatureCountsFromAST(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module ex\ngo 1.22\n"), 0o644))
	src := `package fat

func Many(a, b, c int, d string, e bool, f float64) (int, error, bool) { return 0, nil, false }

type Big interface {
	A()
	B()
	C()
	D()
	E()
	F()
}
`
	path := filepath.Join(dir, "fat.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	fs, err := ParseGoFile(path, dir)
	require.NoError(t, err)

	byName := map[string]Symbol{}
	for _, s := range fs.Symbols {
		byName[s.Name] = s
	}

	many := byName["Many"]
	assert.Equal(t, 6, many.ParamCount, "grouped decl 'a, b, c int' counts as 3")
	assert.Equal(t, 3, many.ResultCount)

	big := byName["Big"]
	assert.Equal(t, 6, big.ParamCount, "interface method count stored in ParamCount")
}

// TestDetectImplementations verifies struct → interface matching by exported method name set.
func TestDetectImplementations(t *testing.T) {
	t.Parallel()

	files := []*FileSymbols{
		{
			Path: "iface.go",
			Symbols: []Symbol{
				{Name: "Runner", Kind: "interface", Signature: "{Run, Stop}", Exported: true},
				{Name: "Closer", Kind: "interface", Signature: "{Close}", Exported: true},
				{Name: "Stringer", Kind: "interface", Signature: "{String}", Exported: true},
			},
		},
		{
			Path: "agent.go",
			Symbols: []Symbol{
				{Name: "Agent", Kind: "struct", Signature: "{Name}", Exported: true},
				{Name: "Run", Kind: "method", Receiver: "*Agent", Exported: true},
				{Name: "Stop", Kind: "method", Receiver: "*Agent", Exported: true},
				{Name: "Close", Kind: "method", Receiver: "*Agent", Exported: true},
			},
		},
		{
			Path: "other.go",
			Symbols: []Symbol{
				{Name: "Noop", Kind: "struct", Signature: "{}", Exported: true},
				// No methods — implements nothing.
			},
		},
	}

	DetectImplementations(files)

	agent := files[1].Symbols[0]
	assert.Equal(t, []string{"Closer", "Runner"}, agent.Implements,
		"Agent has Run+Stop+Close methods, should satisfy Runner and Closer")

	noop := files[2].Symbols[0]
	assert.Empty(t, noop.Implements, "Noop has no methods, implements nothing")
}

// TestDetectImplementations_NoSelfMatch verifies a struct never implements an interface of its own name.
func TestDetectImplementations_NoSelfMatch(t *testing.T) {
	t.Parallel()

	files := []*FileSymbols{{
		Path: "weird.go",
		Symbols: []Symbol{
			{Name: "Foo", Kind: "interface", Signature: "{Bar}", Exported: true},
			{Name: "Foo", Kind: "struct", Signature: "{}", Exported: true},
			{Name: "Bar", Kind: "method", Receiver: "*Foo", Exported: true},
		},
	}}

	DetectImplementations(files)

	foo := files[0].Symbols[1] // the struct
	assert.Empty(t, foo.Implements, "struct Foo must not claim to implement interface Foo")
}

// TestParseMemberList verifies signature parsing.
func TestParseMemberList(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"A", "B", "C"}, parseMemberList("{A, B, C}"))
	assert.Nil(t, parseMemberList("{}"))
	assert.Nil(t, parseMemberList(""))
	assert.Nil(t, parseMemberList("not braces"))
}

// TestEndLineFromGoAST verifies that EndLine is populated when parsing Go files.
func TestEndLineFromGoAST(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	goMod := "module example.com/test\ngo 1.22\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644))

	src := `package test

type Server struct {
	Name string
	Host string
	Port int
}

func Process(a, b, c int) error {
	return nil
}

func Short() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte(src), 0o644))

	fs, err := ParseGoFile(filepath.Join(dir, "test.go"), dir)
	require.NoError(t, err)

	byName := make(map[string]Symbol, len(fs.Symbols))
	for _, s := range fs.Symbols {
		byName[s.Name] = s
	}

	server := byName["Server"]
	assert.Greater(t, server.EndLine, server.Line, "Server struct should have EndLine > Line")
	assert.GreaterOrEqual(t, server.LineSpan(), 5, "Server struct spans at least 5 lines")

	process := byName["Process"]
	assert.Greater(t, process.EndLine, process.Line, "Process func should have EndLine > Line")

	short := byName["Short"]
	assert.Equal(t, short.Line, short.EndLine, "Short one-liner should have EndLine == Line")
	assert.Equal(t, 0, short.LineSpan(), "Short one-liner should have LineSpan == 0")
}
