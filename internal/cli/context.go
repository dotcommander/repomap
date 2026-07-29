package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/dotcommander/repomap"
)

type contextCommand struct {
	Kind              string `help:"Filter by symbol kind"`
	File              string `help:"Filter to files matching this substring"`
	MaxSourceLines    int    `name:"max-source-lines" default:"200" help:"Max source lines to include for the symbol"`
	MaxOutputLines    int    `name:"max-output-lines" default:"400" help:"Max text output lines (0 = unlimited)"`
	MaxOutputBytes    int    `name:"max-output-bytes" default:"65536" help:"Max text output bytes (0 = unlimited)"`
	JSON              bool   `help:"Emit machine-readable context JSON"`
	Calls             bool   `help:"Include exact Go callers from the semantic Go analysis"`
	Precise           bool   `help:"Deprecated compatibility flag; Go callers are always resolved semantically"`
	CallsIncludeTests bool   `name:"calls-include-tests" help:"Include _test.go callers"`
	CallsLimit        int    `name:"calls-limit" default:"10" help:"Max callers to include when --calls is set"`
	Symbol            string `arg:"" help:"Symbol to inspect"`
	Directory         string `arg:"" optional:"" type:"path" default:"." help:"Directory to inspect"`
}

func (c *contextCommand) Validate() error {
	if c.MaxSourceLines <= 0 {
		return fmt.Errorf("--max-source-lines must be greater than zero")
	}
	for _, value := range []struct {
		name  string
		value int
	}{
		{"max-output-lines", c.MaxOutputLines},
		{"max-output-bytes", c.MaxOutputBytes},
		{"calls-limit", c.CallsLimit},
	} {
		if value.value < 0 {
			return fmt.Errorf("--%s must be zero or greater", value.name)
		}
	}
	return nil
}

func (c *contextCommand) Run(ctx context.Context, ioctx *commandIO) error {
	absDir, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	m := repomap.New(absDir, repomap.Config{
		MaxTokens: 8192, MaxTokensNoCtx: 8192,
		GoAnalysis:      c.Calls,
		GoAnalysisCalls: c.Calls,
		GoAnalysisTests: c.Calls && c.CallsIncludeTests,
	})
	if err := m.Build(ctx); err != nil {
		return err
	}
	if c.Precise && !c.Calls {
		_, _ = fmt.Fprintln(ioctx.stderr, "repomap: --precise has no effect without --calls")
	}
	result, err := m.Context(c.Symbol, repomap.ContextOptions{Kind: c.Kind, File: c.File, MaxSourceLines: c.MaxSourceLines})
	if err != nil {
		return err
	}
	if c.Calls {
		result.Callers = contextCallers(m.SemanticCallers(), result.Match, c.CallsIncludeTests, c.CallsLimit)
	}
	if c.JSON {
		enc := json.NewEncoder(ioctx.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	var b strings.Builder
	if err := printSymbolContext(&b, result); err != nil {
		return err
	}
	out := formatBoundedText(b.String(), c.MaxOutputLines, c.MaxOutputBytes)
	_, err = io.WriteString(ioctx.stdout, out.Text)
	return err
}

func contextCallers(callers repomap.SymbolCallers, match repomap.SymbolMatch, includeTests bool, limit int) []repomap.Location {
	locs := callers.CallersForSymbol(match.File, match.Symbol)
	out := make([]repomap.Location, 0, len(locs))
	for _, loc := range locs {
		if !includeTests && strings.Contains(loc.File, "_test.go") {
			continue
		}
		out = append(out, loc)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func printSymbolContext(w io.Writer, ctx repomap.SymbolContext) error {
	sym := ctx.Match.Symbol
	if _, err := fmt.Fprintf(w, "%s:%d  %s  %s\n", ctx.Match.File, sym.Line, sym.Kind, symbolDisplay(sym)); err != nil {
		return err
	}
	if ctx.SourceNote != "" {
		if _, err := fmt.Fprintf(w, "source: %s\n", ctx.SourceNote); err != nil {
			return err
		}
	}
	if len(ctx.Ambiguous) > 0 {
		if _, err := fmt.Fprintln(w, "also matched:"); err != nil {
			return err
		}
		for _, mt := range ctx.Ambiguous {
			if _, err := fmt.Fprintf(w, "  %s:%d  %s  %s\n", mt.File, mt.Symbol.Line, mt.Symbol.Kind, symbolDisplay(mt.Symbol)); err != nil {
				return err
			}
		}
	}
	if len(ctx.Source) > 0 {
		if _, err := fmt.Fprintln(w, "\nsource:"); err != nil {
			return err
		}
		for _, line := range ctx.Source {
			if _, err := fmt.Fprintf(w, "%4d | %s\n", line.Number, line.Text); err != nil {
				return err
			}
		}
		if ctx.Truncated {
			if _, err := fmt.Fprintln(w, "     ..."); err != nil {
				return err
			}
		}
	}
	if len(ctx.Callers) > 0 {
		if _, err := fmt.Fprintln(w, "\ncallers:"); err != nil {
			return err
		}
		for _, loc := range ctx.Callers {
			if _, err := fmt.Fprintf(w, "  %s:%d:%d\n", loc.File, loc.Line, loc.Column); err != nil {
				return err
			}
		}
	}
	return printImpact(w, ctx.Impact)
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
