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
)

type endpointCommand struct {
	JSON           bool     `help:"Emit machine-readable endpoint JSON"`
	MaxOutputLines int      `name:"max-output-lines" default:"400" help:"Max text output lines (0 = unlimited)"`
	Args           []string `arg:"" optional:"" help:"Optional route pattern and directory"`
}

func (c *endpointCommand) Run(ctx context.Context, ioctx *commandIO) error {
	if len(c.Args) > 2 {
		return fmt.Errorf("accepts at most 2 arg(s), received %d", len(c.Args))
	}
	// Arg disambiguation: with one arg, an existing directory means
	// list-mode; anything else is a route pattern (directory defaults
	// to "."). Two args are always pattern + directory.
	var pattern string
	dir := "."
	switch len(c.Args) {
	case 1:
		if info, err := os.Stat(c.Args[0]); err == nil && info.IsDir() {
			dir = c.Args[0]
		} else {
			pattern = c.Args[0]
		}
	case 2:
		pattern = c.Args[0]
		dir = c.Args[1]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	m := repomap.New(absDir, repomap.Config{
		MaxTokens: 8192, MaxTokensNoCtx: 8192,
		GoAnalysis:      true,
		GoAnalysisCalls: true,
		GoAnalysisTests: true,
	})
	if err := m.Build(ctx); err != nil {
		return err
	}

	if pattern == "" {
		routes, err := m.Routes(ctx)
		if err != nil {
			return err
		}
		if c.JSON {
			enc := json.NewEncoder(ioctx.stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(routes)
		}
		if len(routes) == 0 {
			fmt.Fprintln(ioctx.stdout, "no routes found")
			return nil
		}
		tw := tabwriter.NewWriter(ioctx.stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "METHOD\tPATTERN\tHANDLER\tFRAMEWORK\tFILE:LINE")
		for _, r := range routes {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s:%d\n", r.Method, r.Pattern, r.Handler, r.Framework, r.File, r.Line)
		}
		return tw.Flush()
	}

	ec, err := m.Endpoint(ctx, pattern)
	if err != nil {
		return err
	}
	// Symbol-level touching tests reuse the semantic graph already built for
	// this map, so endpoint resolution does not start a second backend.
	if ec.Handler != nil {
		ec.Tests = endpointTests(m.SemanticCallers(), *ec.Handler, 10)
	}
	if c.JSON {
		enc := json.NewEncoder(ioctx.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(ec)
	}
	var b strings.Builder
	printEndpointContext(&b, ec)
	out := formatBoundedText(b.String(), c.MaxOutputLines, 0)
	_, err = io.WriteString(ioctx.stdout, out.Text)
	return err
}

// endpointTests returns the _test.go semantic callers for the handler.
func endpointTests(callers repomap.SymbolCallers, handler repomap.SymbolMatch, limit int) []repomap.Location {
	locs := contextCallers(callers, handler, true, limit)
	out := locs[:0]
	for _, loc := range locs {
		if strings.Contains(loc.File, "_test.go") {
			out = append(out, loc)
		}
	}
	return out
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
