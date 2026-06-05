package lsp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestForFileConcurrentFailedStartIsBounded(t *testing.T) {
	// Not parallel: mutates package-global defaultServers.
	original := defaultServers["go"]
	defaultServers["go"] = []ServerConfig{{Command: "sh", Args: []string{"-c", "sleep 0.02; exit 1"}}}
	t.Cleanup(func() { defaultServers["go"] = original })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mgr := NewManager(t.TempDir())
	errs := make(chan error, 6)
	for range 6 {
		go func() {
			_, _, err := mgr.ForFile(ctx, "main.go")
			errs <- err
		}()
	}

	for range 6 {
		require.Error(t, <-errs)
	}
}

func TestForFileFailedInitializeCleansStartedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX process signaling")
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	script := "echo $$ > " + pidFile + "\n" +
		"while true; do sleep 30; done\n"

	original := defaultServers["go"]
	defaultServers["go"] = []ServerConfig{{Command: "sh", Args: []string{"-c", script}}}
	t.Cleanup(func() { defaultServers["go"] = original })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	mgr := NewManager(dir)
	_, _, err := mgr.ForFile(ctx, "main.go")
	require.Error(t, err)

	pid := waitForPIDFile(t, pidFile)
	require.Eventually(t, func() bool {
		return !processAlive(pid)
	}, 2*time.Second, 20*time.Millisecond)
}

// writeFile is a test helper that writes content to path.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			require.NoError(t, parseErr)
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid file %s was not written", path)
	return 0
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
