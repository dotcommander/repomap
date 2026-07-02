package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
			// Symbol-level touching tests via gopls; fail-open (empty on
			// gopls-unavailable or resolution error) so the bundle still
			// renders — Decision 6.
			if ec.Handler != nil {
				if tests, terr := endpointTests(cmd.Context(), absDir, *ec.Handler, 10, m.Ranked()); terr == nil {
					ec.Tests = tests
				}
			}
			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(ec)
			}
			var b strings.Builder
			printEndpointContext(&b, ec)
			out := formatBoundedText(b.String(), maxOutputLines, 0)
			_, err = io.WriteString(cmd.OutOrStdout(), out.Text)
			return err
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable endpoint JSON")
	cmd.Flags().IntVar(&maxOutputLines, "max-output-lines", 400, "Max text output lines (0 = unlimited)")
	return cmd
}

// endpointTests returns the _test.go locations that reference the handler
// symbol via the existing gopls-backed contextCallers (includeTests forced on),
// filtered to test files (Decision 6). precise is always false here.
func endpointTests(ctx context.Context, root string, handler repomap.SymbolMatch, limit int, ranked []repomap.RankedFile) ([]repomap.Location, error) {
	locs, err := contextCallers(ctx, root, handler, true, limit, false, ranked)
	if err != nil {
		return nil, err
	}
	out := locs[:0]
	for _, loc := range locs {
		if strings.Contains(loc.File, "_test.go") {
			out = append(out, loc)
		}
	}
	return out, nil
}

// printEndpointContext renders the endpoint bundle as bounded text: the route
// registration line (always carries the registered handler identifier), the
// resolved handler symbol block, ambiguous alternatives, direct callee names,
// touching tests, and the file-level impact summary.
func printEndpointContext(w io.Writer, ec repomap.EndpointContext) {
	r := ec.Route
	fmt.Fprintf(w, "%s:%d  %s %s  %s  [%s]\n", r.File, r.Line, r.Method, r.Pattern, r.Handler, r.Framework)

	if ec.Handler != nil {
		fmt.Fprintf(w, "\nhandler: %s\n", symbolDisplay(ec.Handler.Symbol))
		fmt.Fprintf(w, "  %s:%d\n", ec.Handler.File, ec.Handler.Symbol.Line)
	}
	if len(ec.Ambiguous) > 0 {
		fmt.Fprintln(w, "\nalso matched:")
		for _, mt := range ec.Ambiguous {
			fmt.Fprintf(w, "  %s:%d  %s\n", mt.File, mt.Symbol.Line, symbolDisplay(mt.Symbol))
		}
	}
	if len(ec.Callees) > 0 {
		fmt.Fprintln(w, "\ncallees:")
		for _, c := range ec.Callees {
			fmt.Fprintf(w, "  %s\n", c)
		}
	}
	if len(ec.Tests) > 0 {
		fmt.Fprintln(w, "\ntests:")
		for _, loc := range ec.Tests {
			fmt.Fprintf(w, "  %s:%d:%d\n", loc.File, loc.Line, loc.Column)
		}
	}
	fmt.Fprintln(w, "\nimpact:")
	printImpact(w, ec.Impact)
}
