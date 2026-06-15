package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dotcommander/repomap"
	"github.com/dotcommander/repomap/internal/serve"
	"github.com/stretchr/testify/require"
)

type serveSession struct {
	enc        *json.Encoder
	dec        *json.Decoder
	stdin      io.Writer
	closeStdin func()
	done       <-chan error
	file       string
}

func startServeSession(t *testing.T) serveSession {
	t.Helper()

	dir := t.TempDir()

	goMod := "module example.com/myapp\ngo 1.22\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644))

	mainSrc := `package main

type Map struct{}
func (m *Map) Build() error { return nil }
func main() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o644))

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

	m := repomap.New(dir, repomap.DefaultConfig())
	require.NoError(t, m.Build(t.Context()))

	clientInR, clientInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	s := &serveServer{
		root:   dir,
		m:      m,
		codec:  serve.NewCodec(clientInR, serverOutW),
		stderr: io.Discard,
	}

	done := make(chan error, 1)
	go func() {
		done <- s.Run(t.Context())
	}()

	closeStdin := func() {
		_ = clientInW.Close()
	}

	t.Cleanup(func() {
		_ = clientInW.Close()
		_ = clientInR.Close()
		_ = serverOutW.Close()
		_ = serverOutR.Close()
	})

	return serveSession{
		enc:        json.NewEncoder(clientInW),
		dec:        json.NewDecoder(bufio.NewReader(serverOutR)),
		stdin:      clientInW,
		closeStdin: closeStdin,
		done:       done,
		file:       "main.go",
	}
}

func TestServeMapRender(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := startServeSession(t)
	require.NoError(t, s.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "map/render", "params": map[string]any{}}))

	var resp map[string]any
	require.NoError(t, s.dec.Decode(&resp))
	require.Equal(t, float64(1), resp["id"])
	_, hasError := resp["error"]
	require.False(t, hasError)
	result := resp["result"].(map[string]any)
	require.NotEmpty(t, result["content"].(string))
}

func TestServeSymbolFind(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := startServeSession(t)
	require.NoError(t, s.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "symbol/find", "params": map[string]any{"query": "Map"}}))

	var resp map[string]any
	require.NoError(t, s.dec.Decode(&resp))
	result := resp["result"].(map[string]any)
	matches := result["matches"].([]any)
	require.NotEmpty(t, matches)
	first := matches[0].(map[string]any)
	symbol := first["Symbol"].(map[string]any)
	require.Equal(t, "Map", symbol["Name"])
	require.Equal(t, "struct", symbol["Kind"])
	require.NotEmpty(t, first["Handle"])
	require.NotEmpty(t, first["FileHandle"])
}

func TestServeFileExplain(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := startServeSession(t)
	require.NoError(t, s.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "file/explain", "params": map[string]any{"path": s.file}}))

	var resp map[string]any
	require.NoError(t, s.dec.Decode(&resp))
	_, hasError := resp["error"]
	require.False(t, hasError)
	require.Contains(t, resp, "result")
}

func TestServeFileContext(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := startServeSession(t)
	require.NoError(t, s.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "file/context", "params": map[string]any{"query": "Build"}}))

	var resp map[string]any
	require.NoError(t, s.dec.Decode(&resp))
	result := resp["result"].(map[string]any)
	require.NotEmpty(t, result)
	match := result["match"].(map[string]any)
	symbol := match["Symbol"].(map[string]any)
	require.Equal(t, "Build", symbol["Name"])
	require.NotEmpty(t, match["Handle"])
}

func TestServeMapStatus(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := startServeSession(t)
	require.NoError(t, s.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "map/status", "params": map[string]any{}}))

	var resp map[string]any
	require.NoError(t, s.dec.Decode(&resp))
	result := resp["result"].(map[string]any)
	require.NotEmpty(t, result["built_at"])
	require.NotEmpty(t, result["root"])
}

func TestServeUnknownMethod(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := startServeSession(t)
	require.NoError(t, s.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "no/such", "params": map[string]any{}}))

	var resp map[string]any
	require.NoError(t, s.dec.Decode(&resp))
	rpcErr := resp["error"].(map[string]any)
	require.Equal(t, float64(-32601), rpcErr["code"])
}

func TestServeInvalidJSON(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := startServeSession(t)
	_, err := s.stdin.Write([]byte("not-json\n"))
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, s.dec.Decode(&resp))
	rpcErr := resp["error"].(map[string]any)
	require.Equal(t, float64(-32700), rpcErr["code"])

	require.NoError(t, s.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "map/status", "params": map[string]any{}}))
	resp = map[string]any{}
	require.NoError(t, s.dec.Decode(&resp))
	_, hasError := resp["error"]
	require.False(t, hasError)
	require.Contains(t, resp, "result")
}

func TestServeEOFShutdown(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := startServeSession(t)
	s.closeStdin()
	select {
	case err := <-s.done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not shut down on EOF")
	}
}
