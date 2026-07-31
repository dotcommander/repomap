package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotcommander/repomap"
	commitflow "github.com/dotcommander/repomap/internal/commit"
	"github.com/dotcommander/repomap/internal/serve"
	"github.com/stretchr/testify/require"
)

var errOutputWrite = errors.New("output write failed")

type outputFailWriter struct{}

func (outputFailWriter) Write([]byte) (int, error) {
	return 0, errOutputWrite
}

type failAfterWriter struct {
	writes int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.writes > 0 {
		return 0, errOutputWrite
	}
	w.writes++
	return len(p), nil
}

func TestEmitPrepReturnsWriteError(t *testing.T) {
	t.Parallel()

	payload := &commitflow.PrepPayload{Status: commitflow.PrepStatusReady}
	for _, tc := range []struct {
		name    string
		jsonOut bool
	}{
		{name: "summary", jsonOut: false},
		{name: "json", jsonOut: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := emitPrep(outputFailWriter{}, tc.jsonOut, payload)
			require.ErrorIs(t, err, errOutputWrite)
		})
	}
}

func TestInitOutputReturnsWriteError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".repomap.yaml"), []byte(configTemplate), 0o644))
	err := writeConfig(outputFailWriter{}, dir, false)
	require.ErrorIs(t, err, errOutputWrite)

	err = writeHook(outputFailWriter{}, t.TempDir(), false)
	require.ErrorIs(t, err, errOutputWrite)
}

func TestServeReturnsResponseWriteError(t *testing.T) {
	t.Parallel()

	s := &serveServer{
		codec:  serve.NewCodec(bytes.NewBufferString("not-json\n"), outputFailWriter{}),
		stderr: io.Discard,
	}
	err := s.Run(t.Context())
	require.ErrorIs(t, err, errOutputWrite)
}

func TestHumanOutputHelpersReturnWriteError(t *testing.T) {
	t.Parallel()

	impact := repomap.ImpactResult{File: repomap.StructuredFile{Path: "main.go"}}
	contextResult := repomap.SymbolContext{
		Match:  repomap.SymbolMatch{File: "main.go", Symbol: repomap.Symbol{Name: "Run", Kind: "function"}},
		Impact: impact,
	}
	endpointResult := repomap.EndpointContext{
		Route:  repomap.RouteRegistration{File: "routes.go", Method: "GET", Pattern: "/", Handler: "handler"},
		Impact: impact,
	}
	for _, tc := range []struct {
		name string
		emit func(io.Writer) error
	}{
		{
			name: "explain",
			emit: func(w io.Writer) error {
				return printExplain(w, repomap.ExplainResult{File: repomap.StructuredFile{Path: "main.go"}})
			},
		},
		{name: "impact", emit: func(w io.Writer) error { return printImpact(w, impact) }},
		{name: "impact markdown", emit: func(w io.Writer) error { return printImpactMarkdown(w, impact) }},
		{name: "inventory", emit: func(w io.Writer) error { return printInventory(w, inventoryReport{Boundary: "Postgres"}) }},
		{name: "orphans", emit: func(w io.Writer) error { return printOrphans(w, repomap.OrphanReport{Caveat: "caveat"}) }},
		{name: "context", emit: func(w io.Writer) error { return printSymbolContext(w, contextResult) }},
		{name: "endpoint", emit: func(w io.Writer) error { return printEndpointContext(w, endpointResult) }},
		{
			name: "render calls",
			emit: func(w io.Writer) error {
				m := repomap.New(".", repomap.DefaultConfig())
				ranked := []repomap.RankedFile{{FileSymbols: &repomap.FileSymbols{Path: "main.go", Language: "go"}}}
				return renderCallsOutput(w, io.Discard, m, "verbose", false, false, ranked, nil, 0)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.emit(outputFailWriter{}), errOutputWrite)
		})
	}
}

func TestEndpointListReturnsBufferedWriterFlushError(t *testing.T) {
	t.Parallel()

	dir := writeEndpointFixture(t)
	err := (&endpointCommand{Args: []string{dir}}).Run(t.Context(), &commandIO{stdout: outputFailWriter{}, stderr: io.Discard})
	require.ErrorIs(t, err, errOutputWrite)
}

func TestEmitFinishResultReturnsJSONNewlineWriteError(t *testing.T) {
	t.Parallel()

	err := emitFinishResult(&failAfterWriter{}, true, 0, &finishResult{Status: finishStatusPassed})
	require.ErrorIs(t, err, errOutputWrite)
}
