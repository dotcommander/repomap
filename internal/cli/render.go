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

	return renderCallsOutputStructured(w, stderr, m, format, asJSON, jsonLegacy, jsonStructured, ranked, callers, limit)
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
	return renderCallsOutputStructured(w, stderr, m, format, asJSON, jsonLegacy, false, ranked, callers, limit)
}

func renderCallsOutputStructured(
	w, stderr io.Writer,
	m *repomap.Map,
	format string,
	asJSON bool,
	jsonLegacy bool,
	jsonStructured bool,
	ranked []repomap.RankedFile,
	callers repomap.SymbolCallers,
	limit int,
) error {
	switch format {
	case "compact", "lines", "xml":
		fmt.Fprintf(stderr, "warning: --calls has no effect with --format %s\n", format)
	}
	return writeRankedWithinBudget(w, m.Config().MaxTokens, ranked, func(selected []repomap.RankedFile) ([]byte, error) {
		return encodeCallsOutput(m, format, asJSON, jsonLegacy, jsonStructured, selected, callers, limit)
	})
}

func encodeCallsOutput(
	m *repomap.Map,
	format string,
	asJSON bool,
	jsonLegacy bool,
	jsonStructured bool,
	ranked []repomap.RankedFile,
	callers repomap.SymbolCallers,
	limit int,
) ([]byte, error) {
	explain := m.Config().Explain
	switch {
	case jsonStructured:
		out := structuredOutputForSelection(m, ranked)
		data, err := json.MarshalIndent(out, "", "  ")
		return append(data, '\n'), err
	case asJSON:
		verbose := repomap.FormatMapWithCallers(ranked, 0, true, false, callers, limit, nil, false)
		lines := outputLines(verbose)
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		if jsonLegacy {
			if err := enc.Encode(lines); err != nil {
				return nil, err
			}
			return []byte(b.String()), nil
		}
		if err := enc.Encode(jsonOutput{SchemaVersion: 1, Lines: lines}); err != nil {
			return nil, err
		}
		return []byte(b.String()), nil
	case format == "verbose":
		return []byte(repomap.FormatMapWithCallers(ranked, 0, true, false, callers, limit, nil, explain)), nil
	case format == "detail":
		return []byte(repomap.FormatMapWithCallers(ranked, 0, true, true, callers, limit, nil, explain)), nil
	case format == "compact":
		return encodeStandard(m, ranked, format, false, false, false)
	case format == "lines":
		return encodeStandard(m, ranked, format, false, false, false)
	case format == "xml":
		return encodeStandard(m, ranked, format, false, false, false)
	default:
		if len(ranked) == 0 {
			return minimumMapEncoding("calls"), nil
		}
		return []byte(repomap.FormatMapWithCallers(ranked, 0, false, false, callers, limit, nil, explain)), nil
	}
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

func encodeStandard(m *repomap.Map, ranked []repomap.RankedFile, format string, asJSON, jsonLegacy, jsonStructured bool) ([]byte, error) {
	if jsonStructured {
		out := structuredOutputForSelection(m, ranked)
		data, err := json.MarshalIndent(out, "", "  ")
		return append(data, '\n'), err
	}
	if asJSON {
		verbose := repomap.FormatMap(ranked, 0, true, false, nil, false)
		lines := outputLines(verbose)
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		if jsonLegacy {
			if err := enc.Encode(lines); err != nil {
				return nil, err
			}
		} else if err := enc.Encode(jsonOutput{SchemaVersion: 1, Lines: lines}); err != nil {
			return nil, err
		}
		return []byte(b.String()), nil
	}
	if len(ranked) == 0 {
		return minimumMapEncoding(format), nil
	}

	switch format {
	case "compact":
		return []byte(repomap.FormatMapCompact(ranked, 0, nil, m.Config().Explain)), nil
	case "verbose":
		return []byte(repomap.FormatMap(ranked, 0, true, false, nil, m.Config().Explain)), nil
	case "detail":
		return []byte(repomap.FormatMap(ranked, 0, true, true, nil, m.Config().Explain)), nil
	case "lines":
		root := m.StructuredOutputForRanked(nil).Root
		return []byte(repomap.FormatLines(ranked, 0, root)), nil
	case "xml":
		return []byte(repomap.FormatXML(ranked, 0, nil)), nil
	default:
		return []byte(repomap.FormatMap(ranked, 0, false, false, nil, m.Config().Explain)), nil
	}
}

func structuredOutputForSelection(m *repomap.Map, ranked []repomap.RankedFile) repomap.StructuredOutput {
	full := m.StructuredOutput()
	out := m.StructuredOutputForRanked(ranked)
	out.Totals = full.Totals
	out.FilesOmitted = full.Totals.Files - len(out.Files)
	if out.FilesOmitted > 0 {
		out.FilesOmittedReason = "complete-output token budget"
	}
	return out
}

func minimumMapEncoding(format string) []byte {
	if format == "xml" {
		return []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<repomap files=\"0\" symbols=\"0\">\n</repomap>\n")
	}
	if format == "" {
		format = "enriched"
	}
	return []byte(fmt.Sprintf("## Repository Map · %s (0 files, 0 symbols)\n\n", format))
}

func outputLines(rendered string) []string {
	rendered = strings.TrimRight(rendered, "\n")
	if rendered == "" {
		return []string{}
	}
	return strings.Split(rendered, "\n")
}

func printJSON(w io.Writer, m *repomap.Map, legacy bool) error {
	return renderStandard(w, m, "", true, legacy, false)
}
