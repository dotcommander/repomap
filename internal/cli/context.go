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
	"github.com/spf13/cobra"
)

func newContextCmd() *cobra.Command {
	var (
		kind           string
		file           string
		jsonOut        bool
		withCalls      bool
		precise        bool
		includeTests   bool
		callsLimit     int
		maxSourceLines int
		maxOutputLines int
		maxOutputBytes int
	)
	cmd := &cobra.Command{
		Use:   "context <symbol> [directory]",
		Short: "Show bounded source and impact context for a symbol",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 1 {
				dir = args[1]
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			m := repomap.New(absDir, repomap.Config{MaxTokens: 8192, MaxTokensNoCtx: 8192})
			if err := m.Build(cmd.Context()); err != nil {
				return err
			}

			if precise && !withCalls {
				fmt.Fprintln(os.Stderr, "repomap: --precise has no effect without --calls")
			}

			result, err := m.Context(args[0], repomap.ContextOptions{
				Kind:           kind,
				File:           file,
				MaxSourceLines: maxSourceLines,
			})
			if err != nil {
				return err
			}

			if withCalls {
				callers, err := contextCallers(cmd.Context(), absDir, result.Match, includeTests, callsLimit, precise, m.Ranked())
				if err != nil {
					return err
				}
				result.Callers = callers
			}

			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			var b strings.Builder
			printSymbolContext(&b, result)
			out := formatBoundedText(b.String(), maxOutputLines, maxOutputBytes)
			_, err = io.WriteString(cmd.OutOrStdout(), out.Text)
			return err
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "Filter by symbol kind")
	cmd.Flags().StringVar(&file, "file", "", "Filter to files matching this substring")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable context JSON")
	cmd.Flags().BoolVar(&withCalls, "calls", false, "Include exact Go callers via gopls")
	// --precise: type-checked whole-program call graph for --calls (Phase C,
	// precise-callgraph.md). Without --calls it only drives the no-effect notice above.
	cmd.Flags().BoolVar(&precise, "precise", false, "Use a type-checked whole-program call graph (go/packages+CHA) instead of gopls for --calls")
	cmd.Flags().BoolVar(&includeTests, "calls-include-tests", false, "Include _test.go callers")
	cmd.Flags().IntVar(&callsLimit, "calls-limit", 10, "Max callers to include when --calls is set")
	cmd.Flags().IntVar(&maxSourceLines, "max-source-lines", 200, "Max source lines to include for the symbol")
	cmd.Flags().IntVar(&maxOutputLines, "max-output-lines", 400, "Max text output lines (0 = unlimited)")
	cmd.Flags().IntVar(&maxOutputBytes, "max-output-bytes", 64*1024, "Max text output bytes (0 = unlimited)")
	return cmd
}

func contextCallers(ctx context.Context, root string, match repomap.SymbolMatch, includeTests bool, limit int, precise bool, ranked []repomap.RankedFile) ([]repomap.Location, error) {
	if precise {
		return preciseContextCallers(ctx, root, match, includeTests, limit, ranked)
	}
	if err := repomap.CheckGopls(); err != nil {
		return nil, err
	}
	mgr := lsp.NewManager(root)
	defer mgr.Shutdown(context.WithoutCancel(ctx))

	q := repomap.NewInProcessQuerier(mgr)
	absFile := filepath.Join(root, match.File)
	locs, err := q.Refs(ctx, absFile, match.Symbol.Line, match.Symbol.Name)
	if err != nil {
		return nil, err
	}

	out := make([]repomap.Location, 0, len(locs))
	for _, loc := range locs {
		if loc.Line == match.Symbol.Line && samePath(root, loc.File, match.File) {
			continue
		}
		if !includeTests && strings.Contains(loc.File, "_test.go") {
			continue
		}
		loc.File = relPathForDisplay(root, loc.File)
		out = append(out, loc)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// preciseContextCallers resolves callers for the matched symbol from the
// type-checked whole-program call graph (--precise), reusing the same on-disk
// cache as renderWithCalls (precise-<hash>.json). On a callgraph load failure it
// returns (nil, nil) so ` + "`" + `context --calls --precise` + "`" + ` falls back to no callers
// rather than erroring — mirroring resolvePreciseCallers' fail-open contract.
func preciseContextCallers(ctx context.Context, root string, match repomap.SymbolMatch, includeTests bool, limit int, ranked []repomap.RankedFile) ([]repomap.Location, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	cacheDir := filepath.Join(home, ".cache", "repomap")

	hash := repomap.PreciseCacheKey(root, ranked)
	callers := repomap.LoadPreciseCache(cacheDir, hash)
	if callers == nil {
		typed, ok, err := resolvePreciseCallers(ctx, root)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		callers = typed
		_ = repomap.SavePreciseCache(cacheDir, hash, typed) // best-effort
	}

	locs := callers.CallersFor(match.File, match.Symbol.Name)
	out := make([]repomap.Location, 0, len(locs))
	for _, loc := range locs {
		if !includeTests && strings.Contains(loc.File, "_test.go") {
			continue
		}
		loc.File = relPathForDisplay(root, loc.File)
		out = append(out, loc)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func samePath(root, got, rel string) bool {
	return relPathForDisplay(root, got) == filepath.ToSlash(rel)
}

func relPathForDisplay(root, path string) string {
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func printSymbolContext(w io.Writer, ctx repomap.SymbolContext) {
	sym := ctx.Match.Symbol
	fmt.Fprintf(w, "%s:%d  %s  %s\n", ctx.Match.File, sym.Line, sym.Kind, symbolDisplay(sym))
	if ctx.SourceNote != "" {
		fmt.Fprintf(w, "source: %s\n", ctx.SourceNote)
	}
	if len(ctx.Ambiguous) > 0 {
		fmt.Fprintln(w, "also matched:")
		for _, mt := range ctx.Ambiguous {
			fmt.Fprintf(w, "  %s:%d  %s  %s\n", mt.File, mt.Symbol.Line, mt.Symbol.Kind, symbolDisplay(mt.Symbol))
		}
	}
	if len(ctx.Source) > 0 {
		fmt.Fprintln(w, "\nsource:")
		for _, line := range ctx.Source {
			fmt.Fprintf(w, "%4d | %s\n", line.Number, line.Text)
		}
		if ctx.Truncated {
			fmt.Fprintln(w, "     ...")
		}
	}
	if len(ctx.Callers) > 0 {
		fmt.Fprintln(w, "\ncallers:")
		for _, loc := range ctx.Callers {
			fmt.Fprintf(w, "  %s:%d:%d\n", loc.File, loc.Line, loc.Column)
		}
	}
	printImpact(w, ctx.Impact)
}

func symbolDisplay(sym repomap.Symbol) string {
	if sym.Signature == "" {
		return sym.Name
	}
	if sym.Kind == "method" && sym.Receiver != "" {
		return fmt.Sprintf("(%s) %s%s", sym.Receiver, sym.Name, sym.Signature)
	}
	return sym.Name + sym.Signature
}
