package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dotcommander/repomap"
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
	var jsonLegacy bool
	var jsonStructured bool

	// --calls flags
	var callsMode bool
	var callsThreshold int
	var callsLimit int
	var callsIncludeTests bool
	var noCache bool
	var callsUseBinary bool // hidden fallback: shell out to lspq instead of in-process gopls
	var intent string
	var consumed []string
	var symbolRefs bool
	var explain bool
	var includeTests bool

	cmd := &cobra.Command{
		Use:   "repomap [directory]",
		Short: "Token-budgeted repository structure map with symbol extraction",
		Long: `Scans a project's source files, extracts exported symbols
(functions, methods, structs, interfaces, types, constants, variables),
ranks files by importance, and outputs a compact Markdown summary.
Pass --intent to bias the output toward files relevant to a specific task.`,
		Example: `  # Default "enriched" map — exported symbols + signatures + godoc, 2048-token budget
  repomap ./src

  # Lean orientation — symbol NAMES only; fits more files in the same budget
  repomap -f compact ./src

  # Every symbol with full signatures + struct fields, no budget limit
  repomap -f detail ./src

  # Machine-readable XML — dependency graph + symbol attributes (line, span, params)
  repomap -f xml ./src

  # Structured JSON repository map (distinct schema from --json)
  repomap --json-structured ./src

  # Task-aware ranking; pair with --explain to see WHY each file ranked
  repomap --intent "auth middleware" --explain ./src`,
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
				Intent:         intent,
				ConsumedPaths:  consumed,
				SymbolRefs:     symbolRefs,
				Explain:        explain,
				IncludeTests:   includeTests,
			}
			m := repomap.New(absDir, cfg)

			if err := m.Build(cmd.Context()); err != nil {
				return err
			}

			if !callsMode {
				return renderStandard(m, format, asJSON, jsonLegacy, jsonStructured)
			}

			return renderWithCalls(cmd.Context(), m, format, asJSON, jsonLegacy, jsonStructured, absDir, callsThreshold, callsLimit, callsIncludeTests, noCache, callsUseBinary)
		},
	}

	cmd.Flags().IntVarP(&tokens, "tokens", "t", 2048, "Token budget")
	cmd.Flags().StringVarP(&format, "format", "f", "", "Output format: compact (orientation: names only), verbose, detail, lines, xml (default: enriched — signatures + godoc + fields)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON array of lines")
	cmd.Flags().BoolVar(&jsonLegacy, "json-legacy", false, "Emit --json output as a bare array (pre-v0.7.0 format). Use only for legacy scripts; will be removed in a future release.")
	cmd.Flags().BoolVar(&jsonStructured, "json-structured", false, "Output a structured JSON repository map")

	cmd.Flags().BoolVar(&callsMode, "calls", false, "Expand exported symbols with caller information via gopls")
	cmd.Flags().IntVar(&callsThreshold, "calls-threshold", 2, "Only expand symbols in files with at least N importers")
	cmd.Flags().IntVar(&callsLimit, "calls-limit", 10, "Max callers shown per symbol")
	cmd.Flags().BoolVar(&callsIncludeTests, "calls-include-tests", false, "Include _test.go callers (excluded by default)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Bypass --calls cache (force fresh queries)")
	cmd.Flags().StringVarP(&intent, "intent", "i", "", "Natural-language query for task-aware ranking (BM25). Reranks files silently — add --explain to see the score breakdown.")
	cmd.Flags().StringSliceVar(&consumed, "consumed", nil, "Comma-separated file paths already read; these are downranked and their importers upranked")
	cmd.Flags().BoolVar(&symbolRefs, "symbol-refs", false, "Enable approximate cross-language symbol reference scoring")
	cmd.Flags().BoolVar(&explain, "explain", false, "Append per-file confidence-tier score breakdown (including the --intent contribution) to text output.")
	cmd.Flags().BoolVar(&includeTests, "include-tests", false, "Rank _test.go files at full weight (default: demoted)")
	cmd.Flags().BoolVar(&callsUseBinary, "calls-use-binary", false, "Fall back to shelling out to lspq instead of in-process gopls")
	if err := cmd.Flags().MarkHidden("calls-use-binary"); err != nil {
		panic(err)
	}

	cmd.AddCommand(newCommitCmd())
	cmd.AddCommand(newCommitPreflightCmd())
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newAuditCmd())
	cmd.AddCommand(newCacheCmd())
	cmd.AddCommand(newFindCmd())
	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newImpactCmd())
	cmd.AddCommand(newContextCmd())
	cmd.AddCommand(newExplainCmd())
	cmd.AddCommand(newOrphansCmd())
	cmd.AddCommand(newBriefCmd())

	for _, sub := range newLSPCmds() {
		cmd.AddCommand(sub)
	}

	return cmd
}

func renderStandard(m *repomap.Map, format string, asJSON bool, jsonLegacy bool, jsonStructured bool) error {
	if jsonStructured {
		data, err := m.StructuredJSON()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(data, '\n'))
		return err
	}
	if asJSON {
		return printJSON(os.Stdout, m, jsonLegacy)
	}

	var out string
	switch format {
	case "compact":
		out = m.StringCompact() // lean orientation: path + exported names only
	case "verbose":
		out = m.StringVerbose()
	case "detail":
		out = m.StringDetail()
	case "lines":
		out = m.StringLines()
	case "xml":
		out = m.StringXML()
	default:
		out = m.String() // enriched default: signatures + godoc + fields
	}
	fmt.Print(out)
	return nil
}
