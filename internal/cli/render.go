package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dotcommander/repomap"
	"github.com/dotcommander/repomap/internal/callgraph"
	"github.com/dotcommander/repomap/internal/lsp"
)

// jsonOutput is the versioned envelope for --json output.
// Increment SchemaVersion on any breaking change to the lines format.
type jsonOutput struct {
	SchemaVersion int      `json:"schema_version"`
	Lines         []string `json:"lines"`
}

func renderWithCalls(
	ctx context.Context,
	w, stderr io.Writer,
	m *repomap.Map,
	format string,
	asJSON bool,
	jsonLegacy bool,
	jsonStructured bool,
	root string,
	threshold, limit int,
	includeTests bool,
	noCache bool,
	useBinary bool,
	precise bool,
) error {
	ranked := m.Ranked()
	callsCfg := repomap.CallsConfig{
		Threshold:    threshold,
		Limit:        limit,
		IncludeTests: includeTests,
	}
	if precise {
		// The legacy precise mode analyzed every exported symbol. It is now an
		// alias for the canonical semantic backend and keeps that threshold.
		callsCfg.Threshold = 0
	}

	_ = ctx
	_ = root
	_ = noCache
	_ = useBinary
	_ = precise
	callers := repomap.SelectSemanticCallers(m.SemanticCallers(), ranked, callsCfg)

	callerCounts := repomap.CallerCountsFromSymbolCallers(callers)
	repomap.ApplyCallerBonus(ranked, callerCounts)

	if jsonStructured {
		out := m.StructuredOutputForRanked(ranked)
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		_, err = w.Write(append(data, '\n'))
		return err
	}

	return renderCallsOutput(w, stderr, m, format, asJSON, jsonLegacy, ranked, callers, limit)
}

// resolvePreciseCallers attempts to build the type-checked whole-program call
// graph (--precise, precise-callgraph.md Decision 3) and adapt it into the
// SymbolCallers shape every --calls consumer already reads. It returns
// (callers, true, nil) on success. On callgraph.ErrLoadFailed it prints the
// fallback notice and returns (nil, false, nil) so renderWithCalls falls back to
// the existing gopls --calls tier — --precise never turns a working --calls
// invocation into a hard error. Any other error propagates unchanged.
// internal/callgraph.Build is the precise go/packages + SSA + CHA
// implementation; ErrLoadFailed is now only the fail-open path for package load
// failures.
func resolvePreciseCallers(ctx context.Context, root string, stderr io.Writer) (repomap.SymbolCallers, bool, error) {
	edges, err := callgraph.Build(ctx, root)
	if err != nil {
		if errors.Is(err, callgraph.ErrLoadFailed) {
			fmt.Fprintln(stderr, "repomap: --precise disabled \u2014 go/packages load failed, falling back to --calls")
			return nil, false, nil
		}
		return nil, false, err
	}
	return repomap.TypedGraphToSymbolCallers(edges), true, nil
}

func runExpansion(ctx context.Context, stderr io.Writer, root string, ranked []repomap.RankedFile, cfg repomap.CallsConfig, useBinary bool) (repomap.SymbolCallers, repomap.CallsStats, error) {
	var q repomap.RefsQuerier
	if useBinary {
		if err := repomap.CheckLspq(); err != nil {
			return nil, repomap.CallsStats{}, err
		}
		q = repomap.DefaultQuerier()
	} else {
		if err := repomap.CheckGopls(); err != nil {
			return nil, repomap.CallsStats{}, err
		}
		mgr := lsp.NewManager(root)
		defer mgr.Shutdown(context.WithoutCancel(ctx))
		q = repomap.NewInProcessQuerier(mgr)
	}

	isTTY := isTTYWriter(stderr)
	progress := buildProgressFn(stderr, isTTY)

	callers, stats := repomap.ExpandCallers(ctx, root, ranked, cfg, q, progress)

	if isTTY {
		// Clear the progress line.
		fmt.Fprint(stderr, "\r\033[K")
	}

	if stats.OK+stats.Timeout+stats.Error > 0 {
		fmt.Fprintf(stderr, "call expansion: %d OK, %d timeout, %d error\n", stats.OK, stats.Timeout, stats.Error)
	}
	return callers, stats, nil
}

// callsRunDegraded reports whether an expansion run had any LSP timeout or
// error, in which case its (possibly incomplete) result must NOT be cached as
// authoritative. Pure predicate so the cache-write gate is unit-testable
// without a live LSP backend.
func callsRunDegraded(stats repomap.CallsStats) bool {
	return stats.Timeout > 0 || stats.Error > 0
}

func buildProgressFn(stderr io.Writer, isTTY bool) func(done, total int) {
	if !isTTY {
		return nil
	}
	return func(done, total int) {
		fmt.Fprintf(stderr, "\rexpanding callers: %d/%d", done, total)
	}
}

func renderCallsOutput(
	w, stderr io.Writer,
	m *repomap.Map,
	format string,
	asJSON bool,
	jsonLegacy bool,
	ranked []repomap.RankedFile,
	callers repomap.SymbolCallers,
	limit int,
) error {
	explain := m.Config().Explain
	switch {
	case asJSON:
		verbose := repomap.FormatMapWithCallers(ranked, 0, true, false, callers, limit, nil, false)
		lines := strings.Split(strings.TrimRight(verbose, "\n"), "\n")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if jsonLegacy {
			return enc.Encode(lines)
		}
		return enc.Encode(jsonOutput{SchemaVersion: 1, Lines: lines})
	case format == "verbose":
		fmt.Fprint(w, repomap.FormatMapWithCallers(ranked, 0, true, false, callers, limit, nil, explain))
	case format == "detail":
		fmt.Fprint(w, repomap.FormatMapWithCallers(ranked, 0, true, true, callers, limit, nil, explain))
	case format == "compact":
		fmt.Fprintf(stderr, "warning: --calls has no effect with --format compact\n")
		fmt.Fprint(w, m.StringCompact())
	case format == "lines":
		fmt.Fprintf(stderr, "warning: --calls has no effect with --format lines\n")
		fmt.Fprint(w, m.StringLines())
	case format == "xml":
		fmt.Fprintf(stderr, "warning: --calls has no effect with --format xml\n")
		fmt.Fprint(w, m.StringXML())
	default:
		// enriched default with callers.
		maxTokens := m.Config().MaxTokens
		fmt.Fprint(w, repomap.FormatMapWithCallers(ranked, maxTokens, false, false, callers, limit, nil, explain))
	}
	return nil
}

func isTTYWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func printJSON(w io.Writer, m *repomap.Map, legacy bool) error {
	verbose := m.StringVerbose()
	lines := strings.Split(strings.TrimRight(verbose, "\n"), "\n")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if legacy {
		return enc.Encode(lines)
	}
	return enc.Encode(jsonOutput{SchemaVersion: 1, Lines: lines})
}
