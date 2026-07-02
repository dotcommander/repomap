package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/dotcommander/repomap"
	"github.com/spf13/cobra"
)

func newEndpointCmd() *cobra.Command {
	var (
		jsonOut        bool
		maxOutputLines int
	)
	cmd := &cobra.Command{
		Use:   "endpoint [pattern] [directory]",
		Short: "Show route registration, handler, callee names, and touching tests",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Arg disambiguation: with one arg, an existing directory means
			// list-mode; anything else is a route pattern (directory defaults
			// to "."). Two args are always pattern + directory.
			var pattern string
			dir := "."
			switch len(args) {
			case 1:
				if info, err := os.Stat(args[0]); err == nil && info.IsDir() {
					dir = args[0]
				} else {
					pattern = args[0]
				}
			case 2:
				pattern = args[0]
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

			if pattern == "" {
				routes, err := m.Routes(cmd.Context())
				if err != nil {
					return err
				}
				if jsonOut {
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(routes)
				}
				if len(routes) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no routes found")
					return nil
				}
				tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				fmt.Fprintln(tw, "METHOD\tPATTERN\tHANDLER\tFRAMEWORK\tFILE:LINE")
				for _, r := range routes {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s:%d\n", r.Method, r.Pattern, r.Handler, r.Framework, r.File, r.Line)
				}
				return tw.Flush()
			}

			ec, err := m.Endpoint(cmd.Context(), pattern)
			if err != nil {
				return err
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(ec)
			}
			// Phase C wires bounded text rendering of the bundle here
			// (printEndpointContext + formatBoundedText).
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable endpoint JSON")
	cmd.Flags().IntVar(&maxOutputLines, "max-output-lines", 400, "Max text output lines (0 = unlimited)")
	return cmd
}
