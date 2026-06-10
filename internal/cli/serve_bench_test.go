package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/repomap"
	"github.com/dotcommander/repomap/internal/serve"
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

func BenchmarkColdBuild(b *testing.B) {
	root := findBenchRoot(b)
	for b.Loop() {
		m := repomap.New(root, repomap.DefaultConfig())
		if err := m.Build(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWarmMapRender(b *testing.B) {
	root := findBenchRoot(b)
	m := repomap.New(root, repomap.DefaultConfig())
	if err := m.Build(b.Context()); err != nil {
		b.Fatal(err)
	}

	var out bytes.Buffer
	s := &serveServer{
		root:   root,
		m:      m,
		codec:  serve.NewCodec(bytes.NewReader(nil), &out),
		stderr: io.Discard,
	}
	req := rawRequest{ID: json.RawMessage("1"), Method: "map/render", Params: json.RawMessage(`{}`)}

	b.ResetTimer()
	for b.Loop() {
		out.Reset()
		s.handle(b.Context(), req)

		var resp struct {
			Result mapRenderResult `json:"result"`
			Error  *rpcErrObj      `json:"error"`
		}
		if err := json.NewDecoder(&out).Decode(&resp); err != nil {
			b.Fatal(err)
		}
		if resp.Error != nil {
			b.Fatal(resp.Error.Message)
		}
		if resp.Result.Content == "" {
			b.Fatal("empty map render response")
		}
	}
}
