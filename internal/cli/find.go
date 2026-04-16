package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newFindCmd() *cobra.Command {
	var (
		kind   string
		file   string
		limit  int
		format string
	)
	cmd := &cobra.Command{
		Use:   "find <query> [directory]",
		Short: "Locate a symbol by name with optional kind/file qualifiers",
		Long: `Resolve a symbol across the ranked symbol set.

Query syntax (positional):
  repomap find Config                       name = Config
  repomap find kind:struct:Config           kind = struct, name = Config
  repomap find file:parser:Parse            file = parser, name = Parse
  repomap find kind:struct:file:cli:Root    kind = struct, file = cli, name = Root

Flags override or supplement query qualifiers.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			dir := "."
			if len(args) > 1 {
				dir = args[1]
			}

			name, qKind, qFile := repomap.ParseFindQuery(query)
			if kind == "" {
				kind = qKind
			}
			if file == "" {
				file = qFile
			}

			m := repomap.New(dir, repomap.DefaultConfig())
			if err := m.Build(context.Background()); err != nil {
				return fmt.Errorf("build: %w", err)
			}

			matches := m.FindSymbol(name, kind, file)
			if limit > 0 && len(matches) > limit {
				matches = matches[:limit]
			}

			if format == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(matches)
			}
			// text: SCORE  FILE:LINE  KIND  SIGNATURE
			w := cmd.OutOrStdout()
			for _, mt := range matches {
				sig := mt.Symbol.Signature
				if sig == "" {
					sig = mt.Symbol.Name
				}
				fmt.Fprintf(w, "%-5.0f  %s:%d  %-10s  %s\n",
					mt.Score, mt.File, mt.Symbol.Line, mt.Symbol.Kind, sig)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "Filter by symbol kind (struct, func, method, type, interface, var, const)")
	cmd.Flags().StringVar(&file, "file", "", "Filter to files matching this substring")
	cmd.Flags().IntVar(&limit, "limit", 20, "Max results (0 = unlimited)")
	cmd.Flags().StringVar(&format, "format", "text", `Output format: "text" or "json"`)
	return cmd
}
