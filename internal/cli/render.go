package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/repomap"
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
) error {
	ranked := m.Ranked()
	callsCfg := repomap.CallsConfig{
		Threshold:    threshold,
		Limit:        limit,
		IncludeTests: includeTests,
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	cacheDir := filepath.Join(home, ".cache", "repomap")
	var callers repomap.SymbolCallers

	if !noCache {
		hash := repomap.CallsCacheKey(root, ranked, callsCfg)
		cached := repomap.LoadCallsCache(cacheDir, hash)
		if cached != nil {
			callers = cached
		} else {
			var (
				err   error
				stats repomap.CallsStats
			)
			callers, stats, err = runExpansion(ctx, root, ranked, callsCfg, useBinary)
			if err != nil {
				return err
			}
			// Degraded run = any LSP timeout or error. Caching a degraded
			// (possibly incomplete) result as authoritative would poison
			// future runs, so skip the write and tell the user once.
			if callsRunDegraded(stats) {
				fmt.Fprintf(os.Stderr, "repomap: calls cache not written: %d LSP errors/timeouts\n", stats.Timeout+stats.Error)
			} else {
				_ = repomap.SaveCallsCache(cacheDir, hash, callers) // best-effort
			}
		}
	} else {
		var err error
		callers, _, err = runExpansion(ctx, root, ranked, callsCfg, useBinary)
		if err != nil {
			return err
		}
	}

	callerCounts := repomap.CallerCountsFromSymbolCallers(callers)
	repomap.ApplyCallerBonus(ranked, callerCounts)

	if jsonStructured {
		out := m.StructuredOutputForRanked(ranked)
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(data, '\n'))
		return err
	}

	return renderCallsOutput(os.Stdout, m, format, asJSON, jsonLegacy, ranked, callers, limit)
}

func runExpansion(ctx context.Context, root string, ranked []repomap.RankedFile, cfg repomap.CallsConfig, useBinary bool) (repomap.SymbolCallers, repomap.CallsStats, error) {
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

	isTTY := isTTYStderr()
	progress := buildProgressFn(isTTY)

	callers, stats := repomap.ExpandCallers(ctx, root, ranked, cfg, q, progress)

	if isTTY {
		// Clear the progress line.
		fmt.Fprint(os.Stderr, "\r\033[K")
	}

	if stats.OK+stats.Timeout+stats.Error > 0 {
		fmt.Fprintf(os.Stderr, "call expansion: %d OK, %d timeout, %d error\n", stats.OK, stats.Timeout, stats.Error)
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

func buildProgressFn(isTTY bool) func(done, total int) {
	if !isTTY {
		return nil
	}
	return func(done, total int) {
		fmt.Fprintf(os.Stderr, "\rexpanding callers: %d/%d", done, total)
	}
}

func renderCallsOutput(
	w io.Writer,
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
		fmt.Fprintf(os.Stderr, "warning: --calls has no effect with --format compact\n")
		fmt.Fprint(w, m.StringCompact())
	case format == "lines":
		fmt.Fprintf(os.Stderr, "warning: --calls has no effect with --format lines\n")
		fmt.Fprint(w, m.StringLines())
	case format == "xml":
		fmt.Fprintf(os.Stderr, "warning: --calls has no effect with --format xml\n")
		fmt.Fprint(w, m.StringXML())
	default:
		// enriched default with callers.
		maxTokens := m.Config().MaxTokens
		fmt.Fprint(w, repomap.FormatMapWithCallers(ranked, maxTokens, false, false, callers, limit, nil, explain))
	}
	return nil
}

func isTTYStderr() bool {
	info, err := os.Stderr.Stat()
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
