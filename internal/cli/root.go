package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

			cfg := repomap.Config{
				MaxTokens:      tokens,
				MaxTokensNoCtx: tokens,
			}
			m := repomap.New(dir, cfg)

			if err := m.Build(context.Background()); err != nil {
				return err
			}

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
		},
	}

	cmd.Flags().IntVarP(&tokens, "tokens", "t", 2048, "Token budget")
	cmd.Flags().StringVarP(&format, "format", "f", "compact", "Output format: compact, verbose, detail, lines, xml")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON array of lines")

	cmd.AddCommand(newCommitCmd())

	return cmd
}

func printJSON(m *repomap.Map) error {
	verbose := m.StringVerbose()
	lines := strings.Split(strings.TrimRight(verbose, "\n"), "\n")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(lines)
}
