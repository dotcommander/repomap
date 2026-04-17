package lsp

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLanguageForFile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		file string
		want string
	}{
		{"main.go", "go"},
		{"server.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"index.js", "javascript"},
		{"main.py", "python"},
		{"lib.rs", "rust"},
		{"foo.c", "c"},
		{"foo.h", "c"},
		{"foo.cpp", "cpp"},
		{"foo.java", "java"},
		{"unknown.xyz", ""},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, LanguageForFile(tc.file))
		})
	}
}

func TestFindSymbolColumn_Found(t *testing.T) {
	t.Parallel()

	// Write a temp file with a known symbol on line 0.
	dir := t.TempDir()
	content := "func Foo() int {\n\treturn 42\n}\n"
	file := dir + "/foo.go"
	if err := writeFile(file, content); err != nil {
		t.Fatal(err)
	}

	col, err := FindSymbolColumn(file, 0, "Foo")
	assert.NoError(t, err)
	assert.Equal(t, 5, col) // "func " = 5 chars
}

func TestFindSymbolColumn_NotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := dir + "/foo.go"
	if err := writeFile(file, "func Foo() {}\n"); err != nil {
		t.Fatal(err)
	}

	_, err := FindSymbolColumn(file, 0, "Bar")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), `"Bar" not found`)
}

func TestFindSymbolColumn_LineOutOfRange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := dir + "/foo.go"
	if err := writeFile(file, "line1\n"); err != nil {
		t.Fatal(err)
	}

	_, err := FindSymbolColumn(file, 5, "x")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestIsSymbolInformationArray(t *testing.T) {
	t.Parallel()

	siJSON := []byte(`[{"name":"Foo","kind":12,"location":{"uri":"file:///a.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}}}}]`)
	dsJSON := []byte(`[{"name":"Foo","kind":12,"range":{"start":{"line":0,"character":0},"end":{"line":5,"character":1}},"selectionRange":{"start":{"line":0,"character":5},"end":{"line":0,"character":8}}}]`)

	assert.True(t, isSymbolInformationArray(siJSON), "should detect SymbolInformation array")
	assert.False(t, isSymbolInformationArray(dsJSON), "should not flag DocumentSymbol array")
	assert.False(t, isSymbolInformationArray(nil), "nil input")
	assert.False(t, isSymbolInformationArray([]byte(`{}`)), "object not array")
}

// writeFile is a test helper that writes content to path.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
