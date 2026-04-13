package repomap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeGoFile writes a minimal .go file with the given number of import lines.
func writeGoFile(t *testing.T, path, pkg string, importLines []string, extraLines int) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	if len(importLines) > 0 {
		b.WriteString("import (\n")
		for _, imp := range importLines {
			fmt.Fprintf(&b, "\t%q\n", imp)
		}
		b.WriteString(")\n\n")
	}
	for range extraLines {
		b.WriteString("// placeholder\n")
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0600))
}

func TestScanInventory_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inv, err := ScanInventory(context.Background(), dir)
	require.NoError(t, err)
	assert.Empty(t, inv.Files)
	assert.False(t, inv.Truncated)
	assert.Equal(t, dir, inv.RootPath)
	assert.NotEmpty(t, inv.Scanned)
}

func TestScanInventory_SingleFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "main.go"), "main", []string{"fmt", "os"}, 5)

	inv, err := ScanInventory(context.Background(), dir)
	require.NoError(t, err)
	require.Len(t, inv.Files, 1)
	assert.Equal(t, "main.go", inv.Files[0].Path)
	assert.Equal(t, 2, inv.Files[0].Imports)
	assert.Greater(t, inv.Files[0].Lines, 0)
	assert.False(t, inv.Truncated)
}

func TestScanInventory_MultipleFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "small.go"), "main", nil, 2)
	writeGoFile(t, filepath.Join(dir, "medium.go"), "main", []string{"fmt"}, 10)
	writeGoFile(t, filepath.Join(dir, "large.go"), "main", []string{"fmt", "os", "io"}, 20)

	inv, err := ScanInventory(context.Background(), dir)
	require.NoError(t, err)
	require.Len(t, inv.Files, 3)
	// Results should be sorted by lines descending
	assert.GreaterOrEqual(t, inv.Files[0].Lines, inv.Files[1].Lines)
	assert.GreaterOrEqual(t, inv.Files[1].Lines, inv.Files[2].Lines)
}

func TestScanInventory_NonGoFilesSkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "main.go"), "main", nil, 0)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# doc"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("key: val"), 0600))

	inv, err := ScanInventory(context.Background(), dir)
	require.NoError(t, err)
	assert.Len(t, inv.Files, 1)
	assert.Equal(t, "main.go", inv.Files[0].Path)
}

func TestScanInventory_SkipsHiddenDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "main.go"), "main", nil, 0)
	hiddenDir := filepath.Join(dir, ".hidden")
	require.NoError(t, os.MkdirAll(hiddenDir, 0700))
	writeGoFile(t, filepath.Join(hiddenDir, "secret.go"), "hidden", nil, 0)

	inv, err := ScanInventory(context.Background(), dir)
	require.NoError(t, err)
	for _, f := range inv.Files {
		assert.False(t, strings.HasPrefix(f.Path, ".hidden"), "hidden dir files should be skipped")
	}
}

func TestScanInventory_SkipsUnderscoreDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "main.go"), "main", nil, 0)
	underscoreDir := filepath.Join(dir, "_gen")
	require.NoError(t, os.MkdirAll(underscoreDir, 0700))
	writeGoFile(t, filepath.Join(underscoreDir, "gen.go"), "gen", nil, 0)

	inv, err := ScanInventory(context.Background(), dir)
	require.NoError(t, err)
	for _, f := range inv.Files {
		assert.False(t, strings.HasPrefix(f.Path, "_gen"), "underscore dir files should be skipped")
	}
}

func TestScanInventory_RelativePaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	subDir := filepath.Join(dir, "pkg", "foo")
	writeGoFile(t, filepath.Join(subDir, "foo.go"), "foo", nil, 0)

	inv, err := ScanInventory(context.Background(), dir)
	require.NoError(t, err)
	require.Len(t, inv.Files, 1)
	// Path should be relative to root
	assert.Equal(t, filepath.Join("pkg", "foo", "foo.go"), inv.Files[0].Path)
}

func TestScanInventory_ContextCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range 5 {
		writeGoFile(t, filepath.Join(dir, fmt.Sprintf("file%d.go", i)), "main", nil, 0)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Should return either an error or partial results — not panic
	_, err := ScanInventory(ctx, dir)
	// Could return context.Canceled or nil with partial results
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}

func TestScanInventory_LastModField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGoFile(t, filepath.Join(dir, "main.go"), "main", nil, 0)

	inv, err := ScanInventory(context.Background(), dir)
	require.NoError(t, err)
	require.Len(t, inv.Files, 1)
	// LastMod should match YYYY-MM-DD format
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, inv.Files[0].LastMod)
}

func TestCountImports_NoImports(t *testing.T) {
	t.Parallel()
	src := []byte("package main\n\nfunc main() {}\n")
	assert.Equal(t, 0, countImports(src))
}

func TestCountImports_SingleImport(t *testing.T) {
	t.Parallel()
	src := []byte("package main\n\nimport \"fmt\"\n\nfunc main() {}\n")
	assert.Equal(t, 1, countImports(src))
}

func TestCountImports_BlockImport(t *testing.T) {
	t.Parallel()
	src := []byte(`package main

import (
	"fmt"
	"os"
	"io"
)

func main() {}
`)
	assert.Equal(t, 3, countImports(src))
}

func TestCountImports_BlockImportWithComments(t *testing.T) {
	t.Parallel()
	src := []byte(`package main

import (
	// system
	"fmt"
	"os"
)
`)
	// comment lines should not be counted
	assert.Equal(t, 2, countImports(src))
}

func TestCountImports_EmptySource(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0, countImports([]byte{}))
}

func TestFormatInventoryTable_Empty(t *testing.T) {
	t.Parallel()
	got := formatInventoryTable(nil, "")
	assert.Contains(t, got, "file")
	assert.Contains(t, got, "lines")
	assert.Contains(t, got, "imports")
	assert.Contains(t, got, "modified")
}

func TestFormatInventoryTable_WithFiles(t *testing.T) {
	t.Parallel()
	files := []FileMetrics{
		{Path: "main.go", Lines: 42, Imports: 3, LastMod: "2026-01-01"},
	}
	got := formatInventoryTable(files, "")
	assert.Contains(t, got, "main.go")
	assert.Contains(t, got, "42")
	assert.Contains(t, got, "3")
	assert.Contains(t, got, "2026-01-01")
}

func TestFormatInventoryTable_WithHeader(t *testing.T) {
	t.Parallel()
	files := []FileMetrics{{Path: "a.go", Lines: 1, Imports: 0, LastMod: "2026-01-01"}}
	got := formatInventoryTable(files, "## Header\n")
	assert.True(t, strings.HasPrefix(got, "## Header\n"))
}

func TestParseFilter_ValidOperators(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   string
		wantOp  string
		wantKey string
		wantVal string
	}{
		{"lines>100", ">", "lines", "100"},
		{"lines>=50", ">=", "lines", "50"},
		{"imports<5", "<", "imports", "5"},
		{"imports<=10", "<=", "imports", "10"},
		{"lines=42", "=", "lines", "42"},
		{"path=internal/", "=", "path", "internal/"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			op, key, val, err := parseFilter(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.wantOp, op)
			assert.Equal(t, tc.wantKey, key)
			assert.Equal(t, tc.wantVal, val)
		})
	}
}

func TestParseFilter_Invalid(t *testing.T) {
	t.Parallel()
	_, _, _, err := parseFilter("lines100")
	assert.Error(t, err)
}

func TestMatchesFilter_Lines(t *testing.T) {
	t.Parallel()
	f := FileMetrics{Path: "a.go", Lines: 100, Imports: 5}
	assert.True(t, matchesFilter(f, ">", "lines", "50"))
	assert.False(t, matchesFilter(f, ">", "lines", "100"))
	assert.True(t, matchesFilter(f, ">=", "lines", "100"))
	assert.True(t, matchesFilter(f, "=", "lines", "100"))
	assert.False(t, matchesFilter(f, "=", "lines", "99"))
}

func TestMatchesFilter_Imports(t *testing.T) {
	t.Parallel()
	f := FileMetrics{Path: "a.go", Lines: 50, Imports: 3}
	assert.True(t, matchesFilter(f, "<", "imports", "5"))
	assert.False(t, matchesFilter(f, ">", "imports", "3"))
}

func TestMatchesFilter_Path(t *testing.T) {
	t.Parallel()
	f := FileMetrics{Path: "internal/foo/bar.go"}
	assert.True(t, matchesFilter(f, "=", "path", "internal/"))
	assert.False(t, matchesFilter(f, "=", "path", "cmd/"))
	assert.True(t, matchesFilter(f, "!=", "path", "cmd/"))
}

func TestMatchesFilter_InvalidKey(t *testing.T) {
	t.Parallel()
	f := FileMetrics{Path: "a.go", Lines: 10}
	assert.False(t, matchesFilter(f, "=", "unknown", "10"))
}

func TestMatchesFilter_NonNumericValue(t *testing.T) {
	t.Parallel()
	f := FileMetrics{Path: "a.go", Lines: 10}
	assert.False(t, matchesFilter(f, ">", "lines", "abc"))
}

func TestPersistAndLoadInventory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inv := &Inventory{
		Files: []FileMetrics{
			{Path: "main.go", Lines: 20, Imports: 2, LastMod: "2026-01-01"},
		},
		Scanned:   "2026-01-01T00:00:00Z",
		RootPath:  "/tmp/project",
		Truncated: false,
	}

	require.NoError(t, PersistInventory(inv, dir))

	loaded := LoadInventory(dir)
	require.NotNil(t, loaded)
	require.Len(t, loaded.Files, 1)
	assert.Equal(t, "main.go", loaded.Files[0].Path)
	assert.Equal(t, 20, loaded.Files[0].Lines)
}

func TestLoadInventory_MissingFile(t *testing.T) {
	t.Parallel()
	inv := LoadInventory(t.TempDir())
	assert.Nil(t, inv)
}

func TestLoadInventory_EmptyDir(t *testing.T) {
	t.Parallel()
	inv := LoadInventory("")
	assert.Nil(t, inv)
}
