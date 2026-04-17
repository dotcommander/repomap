package lsp

import (
	"context"
	"fmt"
	"path/filepath"
)

// RefLocation is a source position returned by a refs query.
// It mirrors the JSON shape used by the --calls pipeline.
type RefLocation struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Querier implements in-process LSP reference lookups via a shared Manager.
// Spawn one Manager per repomap invocation; all symbol queries reuse it.
type Querier struct {
	mgr *Manager
	cwd string // root for making paths relative
}

// NewQuerier returns a Querier backed by the given Manager.
func NewQuerier(mgr *Manager) *Querier {
	return &Querier{mgr: mgr, cwd: mgr.CWD()}
}

// Refs returns all reference locations for the named symbol at file:line.
// line is 1-based (same convention as the --calls pipeline).
func (q *Querier) Refs(ctx context.Context, file string, line int, symbol string) ([]RefLocation, error) {
	// Resolve to absolute path.
	if !filepath.IsAbs(file) {
		file = filepath.Join(q.cwd, file)
	}

	line0 := line - 1 // convert to 0-based
	col, err := FindSymbolColumn(file, line0, symbol)
	if err != nil {
		return nil, fmt.Errorf("lsp refs find column %s:%d %s: %w", file, line, symbol, err)
	}

	client, lang, err := q.mgr.ForFile(ctx, file)
	if err != nil {
		return nil, fmt.Errorf("lsp refs %s:%d: %w", file, line, err)
	}
	if err := q.mgr.EnsureFileOpen(ctx, client, file, lang); err != nil {
		return nil, fmt.Errorf("lsp refs open %s: %w", file, err)
	}

	locs, err := client.References(ctx, file, line0, col)
	if err != nil {
		return nil, fmt.Errorf("lsp refs %s:%d: %w", file, line, err)
	}

	out := make([]RefLocation, 0, len(locs))
	for _, loc := range locs {
		out = append(out, RefLocation{
			File:   uriToPath(loc.URI),
			Line:   loc.Range.Start.Line + 1,
			Column: loc.Range.Start.Character + 1,
		})
	}
	return out, nil
}
