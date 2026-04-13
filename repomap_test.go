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
		FileSymbols: FileSymbols{
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
		FileSymbols: FileSymbols{
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
		FileSymbols: FileSymbols{
			Path: "util/misc.go",
			Symbols: []Symbol{
				{Name: "Helper", Kind: "function"},
			},
		},
		Score: 0,
	}

	out := FormatMap([]RankedFile{entry, mid, low}, 8192, false, false)

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
			FileSymbols: FileSymbols{Path: path, Symbols: syms},
			Score:       score,
		}
	}

	files := []RankedFile{
		makeRanked("a.go", 10, 2),
		makeRanked("b.go", 5, 3),
		makeRanked("c.go", 1, 1),
	}

	// maxTokens=3 → budgetBytes=12, far smaller than any single file block.
	out := FormatMap(files, 3, false, false)
	assert.NotEmpty(t, out)

	// Footer must appear because not all content fits.
	assert.Contains(t, out, "symbols across", "truncated output must contain footer")
	assert.Contains(t, out, "showing top", "truncated output must contain 'showing top N'")
}

// TestFormatMap_Empty verifies that an empty slice returns "".
func TestFormatMap_Empty(t *testing.T) {
	t.Parallel()

	out := FormatMap(nil, 4096, false, false)
	assert.Equal(t, "", out)

	out = FormatMap([]RankedFile{}, 4096, false, false)
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
			syms[i] = Symbol{Name: fmt.Sprintf("Symbol%d", i), Kind: "function"}
		}
		return RankedFile{
			FileSymbols: FileSymbols{Path: path, Symbols: syms},
			Score:       10,
		}
	}

	files := []RankedFile{
		makeFile("big.go", 10),
		makeFile("small.go", 2),
	}

	// Compressed mode should have "..." for truncation
	compressed := FormatMap(files, 8192, false, false)
	assert.Contains(t, compressed, "...")
	assert.Contains(t, compressed, "(10 total)")

	// Verbose mode should show all symbols without "..."
	verbose := FormatMap(files, 8192, true, false)
	assert.NotContains(t, verbose, "...")
	assert.Contains(t, verbose, "Symbol9") // Last symbol should be visible
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
		FileSymbols: FileSymbols{
			Path:    "core/types.go",
			Symbols: []Symbol{{Name: "Agent", Kind: "struct"}},
		},
		Score: 30,
	}
	noSymbols := RankedFile{
		FileSymbols: FileSymbols{
			Path:    "cmd/main.go",
			Package: "main",
		},
		Tag:   "entry",
		Score: 50,
	}

	files := []RankedFile{noSymbols, withSymbols}

	out := FormatMap(files, 8192, false, false)

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
