package repomap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dotcommander/repomap/internal/lsp"
	"golang.org/x/sync/errgroup"
)

// Location is a source position returned by a refs query.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// CallsConfig controls --calls mode behaviour.
type CallsConfig struct {
	// Threshold: only expand symbols in files with ImportedBy >= Threshold.
	Threshold int
	// Limit: max callers shown per symbol.
	Limit int
	// IncludeTests: when false, filter out callers whose file path contains _test.go.
	IncludeTests bool
}

// DefaultCallsConfig returns the default --calls settings.
func DefaultCallsConfig() CallsConfig {
	return CallsConfig{
		Threshold:    2,
		Limit:        10,
		IncludeTests: false,
	}
}

// RefsQuerier abstracts the refs backend so tests can inject a fake and callers
// can choose between in-process gopls and exec-based lspq.
type RefsQuerier interface {
	Refs(ctx context.Context, file string, line int, symbol string) ([]Location, error)
}

// ---------------------------------------------------------------------------
// In-process querier (gopls via internal/lsp) — default for --calls
// ---------------------------------------------------------------------------

// inProcessQuerier wraps lsp.Querier to implement RefsQuerier.
type inProcessQuerier struct {
	q *lsp.Querier
}

// NewInProcessQuerier returns a RefsQuerier that uses an already-running LSP
// Manager. The caller owns the Manager lifecycle (Shutdown).
func NewInProcessQuerier(mgr *lsp.Manager) RefsQuerier {
	return &inProcessQuerier{q: lsp.NewQuerier(mgr)}
}

func (p *inProcessQuerier) Refs(ctx context.Context, file string, line int, symbol string) ([]Location, error) {
	locs, err := p.q.Refs(ctx, file, line, symbol)
	if err != nil {
		return nil, err
	}
	out := make([]Location, len(locs))
	for i, l := range locs {
		out[i] = Location{File: l.File, Line: l.Line, Column: l.Column}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// exec-based querier (lspq binary) — kept as --calls-use-binary fallback
// ---------------------------------------------------------------------------

// lspqQuerier shells out to the lspq binary.
type lspqQuerier struct{}

// lspqRefsOutput matches the JSON shape returned by `lspq --json refs`.
type lspqRefsOutput struct {
	References []Location `json:"references"`
}

const lspqMaxBytes = 1 << 20 // 1 MB cap per invocation

func (lspqQuerier) Refs(ctx context.Context, file string, line int, symbol string) ([]Location, error) {
	cmd := exec.CommandContext(ctx, "lspq", "--json", "refs", file, fmt.Sprintf("%d", line), symbol)
	pr, pw := io.Pipe()

	cmd.Stdout = pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return nil, fmt.Errorf("lspq start: %w", err)
	}

	// Read up to lspqMaxBytes in a goroutine, then wait for the command.
	dataCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(pr, lspqMaxBytes))
		pr.Close()
		if err != nil {
			errCh <- err
			return
		}
		dataCh <- data
	}()

	waitErr := cmd.Wait()
	pw.Close()

	var data []byte
	select {
	case data = <-dataCh:
	case err := <-errCh:
		return nil, fmt.Errorf("lspq read: %w", err)
	}

	if waitErr != nil {
		return nil, fmt.Errorf("lspq: %w", waitErr)
	}

	var out lspqRefsOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("lspq parse: %w", err)
	}
	return out.References, nil
}

// ---------------------------------------------------------------------------
// SymbolCallers and ExpandCallers
// ---------------------------------------------------------------------------

// SymbolCallers maps "file:symbol" -> caller locations.
type SymbolCallers map[string][]Location

// callsKey builds the lookup key for a file+symbol pair.
func callsKey(file, symbol string) string {
	return file + "\x00" + symbol
}

// CallsStats holds counters from a call-expansion run.
type CallsStats struct {
	OK      int
	Timeout int
	Error   int
}

// ExpandCallers queries a RefsQuerier for each exported symbol in files that
// meet the threshold, returning a SymbolCallers map and run statistics.
//
// progress is called with (done, total) as each symbol completes; pass nil to disable.
func ExpandCallers(
	ctx context.Context,
	root string,
	ranked []RankedFile,
	cfg CallsConfig,
	q RefsQuerier,
	progress func(done, total int),
) (SymbolCallers, CallsStats) {
	type task struct {
		file    string // absolute path
		relFile string // relative path (for key)
		line    int
		symbol  string
	}

	// Collect tasks: exported symbols in files meeting the threshold.
	var tasks []task
	for _, rf := range ranked {
		if rf.ImportedBy < cfg.Threshold {
			continue
		}
		absFile := root + "/" + rf.Path
		for _, sym := range rf.Symbols {
			if !sym.Exported || sym.Line == 0 {
				continue
			}
			tasks = append(tasks, task{
				file:    absFile,
				relFile: rf.Path,
				line:    sym.Line,
				symbol:  sym.Name,
			})
		}
	}

	total := len(tasks)
	result := make(SymbolCallers, total)
	var mu sync.Mutex // protect result map
	var stats CallsStats
	var okAtomic, timeoutAtomic, errAtomic atomic.Int64
	var doneAtomic atomic.Int64

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())

	for _, t := range tasks {
		g.Go(func() error {
			callCtx, cancel := context.WithTimeout(gctx, 5*time.Second)
			defer cancel()

			locs, err := q.Refs(callCtx, t.file, t.line, t.symbol)

			done := int(doneAtomic.Add(1))
			if progress != nil {
				progress(done, total)
			}

			if err != nil {
				if callCtx.Err() != nil {
					timeoutAtomic.Add(1)
				} else {
					errAtomic.Add(1)
				}
				return nil // don't fail the whole run
			}

			okAtomic.Add(1)

			// Filter based on config.
			filtered := filterLocations(locs, cfg, t.file, t.line)

			if len(filtered) > cfg.Limit {
				filtered = filtered[:cfg.Limit]
			}

			if len(filtered) > 0 {
				key := callsKey(t.relFile, t.symbol)
				mu.Lock()
				result[key] = filtered
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait()

	stats.OK = int(okAtomic.Load())
	stats.Timeout = int(timeoutAtomic.Load())
	stats.Error = int(errAtomic.Load())
	return result, stats
}

// filterLocations removes the definition site itself and optionally test files.
func filterLocations(locs []Location, cfg CallsConfig, defFile string, defLine int) []Location {
	filtered := make([]Location, 0, len(locs))
	for _, loc := range locs {
		// Skip the definition itself.
		if loc.Line == defLine && isSameFile(loc.File, defFile) {
			continue
		}
		// Skip test files unless requested.
		if !cfg.IncludeTests && strings.Contains(loc.File, "_test.go") {
			continue
		}
		filtered = append(filtered, loc)
	}
	return filtered
}

// isSameFile does a simple suffix comparison — refs may return relative or
// absolute paths; we just check whether one is a suffix of the other.
func isSameFile(a, b string) bool {
	if a == b {
		return true
	}
	return strings.HasSuffix(a, b) || strings.HasSuffix(b, a)
}

// CheckLspq verifies that the lspq binary is on PATH, returning a descriptive
// error if it is not. Used only when --calls-use-binary is set.
func CheckLspq() error {
	if _, err := exec.LookPath("lspq"); err != nil {
		return fmt.Errorf("lspq not found on PATH: install it to use --calls-use-binary")
	}
	return nil
}

// CheckGopls verifies that gopls is on PATH.
func CheckGopls() error {
	if _, err := exec.LookPath("gopls"); err != nil {
		return fmt.Errorf("gopls not found on PATH: install it to use --calls (go install golang.org/x/tools/gopls@latest)")
	}
	return nil
}

// DefaultQuerier returns the lspq exec-based querier (legacy fallback).
func DefaultQuerier() RefsQuerier {
	return lspqQuerier{}
}
