package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/repomap"
	"github.com/dotcommander/repomap/internal/lsp"
	"github.com/spf13/cobra"
)

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	var tokens int
	var format string
	var asJSON bool

	// --calls flags
	var callsMode bool
	var callsThreshold int
	var callsLimit int
	var callsIncludeTests bool
	var noCache bool
	var callsUseBinary bool // hidden fallback: shell out to lspq instead of in-process gopls

	cmd := &cobra.Command{
		Use:   "repomap [directory]",
		Short: "Token-budgeted repository structure map with symbol extraction",
		Long: `Scans a project's source files, extracts exported symbols
(functions, methods, structs, interfaces, types, constants, variables),
ranks files by importance, and outputs a compact Markdown summary.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			cfg := repomap.Config{
				MaxTokens:      tokens,
				MaxTokensNoCtx: tokens,
			}
			m := repomap.New(absDir, cfg)

			if err := m.Build(context.Background()); err != nil {
				return err
			}

			if !callsMode {
				return renderStandard(m, format, asJSON)
			}

			return renderWithCalls(cmd.Context(), m, format, asJSON, absDir, callsThreshold, callsLimit, callsIncludeTests, noCache, callsUseBinary)
		},
	}

	cmd.Flags().IntVarP(&tokens, "tokens", "t", 2048, "Token budget")
	cmd.Flags().StringVarP(&format, "format", "f", "compact", "Output format: compact, verbose, detail, lines, xml")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON array of lines")

	cmd.Flags().BoolVar(&callsMode, "calls", false, "Expand exported symbols with caller information via gopls")
	cmd.Flags().IntVar(&callsThreshold, "calls-threshold", 2, "Only expand symbols in files with at least N importers")
	cmd.Flags().IntVar(&callsLimit, "calls-limit", 10, "Max callers shown per symbol")
	cmd.Flags().BoolVar(&callsIncludeTests, "calls-include-tests", false, "Include _test.go callers (excluded by default)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Bypass --calls cache (force fresh queries)")
	cmd.Flags().BoolVar(&callsUseBinary, "calls-use-binary", false, "Fall back to shelling out to lspq instead of in-process gopls")
	if err := cmd.Flags().MarkHidden("calls-use-binary"); err != nil {
		panic(err)
	}

	cmd.AddCommand(newCommitCmd())
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newFindCmd())

	for _, sub := range newLSPCmds() {
		cmd.AddCommand(sub)
	}

	return cmd
}

func renderStandard(m *repomap.Map, format string, asJSON bool) error {
	if asJSON {
		return printJSON(m)
	}

	var out string
	switch format {
	case "verbose":
		out = m.StringVerbose()
	case "detail":
		out = m.StringDetail()
	case "lines":
		out = m.StringLines()
	case "xml":
		out = m.StringXML()
	default:
		out = m.String()
	}
	fmt.Print(out)
	return nil
}

func renderWithCalls(
	ctx context.Context,
	m *repomap.Map,
	format string,
	asJSON bool,
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

	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache", "repomap")
	var callers repomap.SymbolCallers

	if !noCache {
		hash := repomap.CallsCacheKey(root, ranked, callsCfg)
		cached := repomap.LoadCallsCache(cacheDir, hash)
		if cached != nil {
			callers = cached
		} else {
			var err error
			callers, err = runExpansion(ctx, root, ranked, callsCfg, useBinary)
			if err != nil {
				return err
			}
			_ = repomap.SaveCallsCache(cacheDir, hash, callers) // best-effort
		}
	} else {
		var err error
		callers, err = runExpansion(ctx, root, ranked, callsCfg, useBinary)
		if err != nil {
			return err
		}
	}

	return renderCallsOutput(m, format, asJSON, ranked, callers, limit)
}

func runExpansion(ctx context.Context, root string, ranked []repomap.RankedFile, cfg repomap.CallsConfig, useBinary bool) (repomap.SymbolCallers, error) {
	var q repomap.RefsQuerier
	if useBinary {
		if err := repomap.CheckLspq(); err != nil {
			return nil, err
		}
		q = repomap.DefaultQuerier()
	} else {
		if err := repomap.CheckGopls(); err != nil {
			return nil, err
		}
		mgr := lsp.NewManager(root)
		defer mgr.Shutdown(context.Background())
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
	return callers, nil
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
	m *repomap.Map,
	format string,
	asJSON bool,
	ranked []repomap.RankedFile,
	callers repomap.SymbolCallers,
	limit int,
) error {
	switch {
	case asJSON:
		verbose := repomap.FormatMapWithCallers(ranked, 0, true, false, callers, limit)
		lines := strings.Split(strings.TrimRight(verbose, "\n"), "\n")
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(lines)
	case format == "verbose":
		fmt.Print(repomap.FormatMapWithCallers(ranked, 0, true, false, callers, limit))
	case format == "detail":
		fmt.Print(repomap.FormatMapWithCallers(ranked, 0, true, true, callers, limit))
	case format == "lines":
		// lines format doesn't integrate callers — fall back to standard.
		fmt.Print(m.StringLines())
	case format == "xml":
		// xml format doesn't integrate callers — fall back to standard.
		fmt.Print(m.StringXML())
	default:
		// compact
		maxTokens := m.Config().MaxTokens
		fmt.Print(repomap.FormatMapWithCallers(ranked, maxTokens, false, false, callers, limit))
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

func printJSON(m *repomap.Map) error {
	verbose := m.StringVerbose()
	lines := strings.Split(strings.TrimRight(verbose, "\n"), "\n")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(lines)
}
