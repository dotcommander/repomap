package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dotcommander/repomap/internal/lsp"
)

// ---------------------------------------------------------------------------
// JSON output shapes (match lspq byte-for-byte)
// ---------------------------------------------------------------------------

type jsonLocation struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type jsonDefOutput struct {
	Definition *jsonLocation `json:"definition"`
}

type jsonRefsOutput struct {
	References []jsonLocation `json:"references"`
}

type jsonHoverOutput struct {
	Hover string `json:"hover"`
}

type jsonSymbol struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type jsonSymbolsOutput struct {
	Symbols []jsonSymbol `json:"symbols"`
}

// ---------------------------------------------------------------------------
// LSP commands
// ---------------------------------------------------------------------------

type lspCommand struct {
	Status lspStatusCommand `cmd:"" help:"Report detected LSP server coverage without starting servers"`
}

type lspStatusCommand struct {
	Directory string `arg:"" optional:"" type:"path" default:"." help:"Directory to inspect"`
	JSON      bool   `help:"Emit machine-readable LSP status JSON"`
}

func (c *lspStatusCommand) Run(ctx context.Context, ioctx *commandIO) error {
	absDir, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	report, err := lsp.DetectStatus(ctx, absDir)
	if err != nil {
		return err
	}
	if c.JSON {
		return writeJSON(ioctx.stdout, report)
	}
	return printLSPStatus(ioctx.stdout, report)
}

type refsCommand struct {
	File   string `arg:"" type:"path" help:"Source file"`
	Line   string `arg:"" help:"1-based source line"`
	Symbol string `arg:"" help:"Identifier name on that line"`
	JSON   bool   `help:"Output JSON"`
}

type defCommand struct {
	File   string `arg:"" type:"path" help:"Source file"`
	Line   string `arg:"" help:"1-based source line"`
	Symbol string `arg:"" help:"Identifier name on that line"`
	JSON   bool   `help:"Output JSON"`
}

type hoverCommand struct {
	File   string `arg:"" type:"path" help:"Source file"`
	Line   string `arg:"" help:"1-based source line"`
	Symbol string `arg:"" help:"Identifier name on that line"`
	JSON   bool   `help:"Output JSON"`
}

type symbolsCommand struct {
	File string `arg:"" type:"path" help:"Source file"`
	JSON bool   `help:"Output JSON"`
}

func printLSPStatus(w io.Writer, report lsp.StatusReport) error {
	if _, err := fmt.Fprintf(w, "lsp status: %s\n", report.Root); err != nil {
		return err
	}
	if len(report.Servers) == 0 && len(report.Missing) == 0 {
		_, err := fmt.Fprintln(w, "  no LSP-supported source files found")
		return err
	}
	if len(report.Servers) > 0 {
		if _, err := fmt.Fprintln(w, "available:"); err != nil {
			return err
		}
		for _, server := range report.Servers {
			if _, err := fmt.Fprintf(w, "  %s  %s\n", server.Language, server.Root); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "    command: %s\n", lspCommandDisplay(server)); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "    file types: %s\n", strings.Join(server.FileTypes, ", ")); err != nil {
				return err
			}
		}
	}
	if len(report.Missing) > 0 {
		if _, err := fmt.Fprintln(w, "missing:"); err != nil {
			return err
		}
		for _, missing := range report.Missing {
			if _, err := fmt.Fprintf(w, "  %s\n", missing.Language); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "    tried: %s\n", strings.Join(missing.TriedCommands, ", ")); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "    found: %s\n", strings.Join(missing.FoundExtensions, ", ")); err != nil {
				return err
			}
		}
	}
	return nil
}

func lspCommandDisplay(server lsp.StatusServer) string {
	if len(server.Args) == 0 {
		return server.Command
	}
	return server.Command + " " + strings.Join(server.Args, " ")
}

func (c *refsCommand) Run(ctx context.Context, ioctx *commandIO) error {
	sess, err := setupLSPSession(ctx, []string{c.File, c.Line, c.Symbol})
	if err != nil {
		return err
	}
	defer sess.mgr.Shutdown(context.Background())
	locs, err := sess.client.References(ctx, sess.file, sess.line, sess.col)
	if err != nil {
		return fmt.Errorf("references: %w", err)
	}
	if c.JSON {
		return writeJSON(ioctx.stdout, buildRefsJSON(locs, sess.cwd))
	}
	_, err = fmt.Fprintln(ioctx.stdout, lsp.FormatLocations(locs, sess.cwd, 1))
	return err
}

func (c *defCommand) Run(ctx context.Context, ioctx *commandIO) error {
	sess, err := setupLSPSession(ctx, []string{c.File, c.Line, c.Symbol})
	if err != nil {
		return err
	}
	defer sess.mgr.Shutdown(context.Background())
	locs, err := sess.client.Definition(ctx, sess.file, sess.line, sess.col)
	if err != nil {
		return fmt.Errorf("definition: %w", err)
	}
	if c.JSON {
		return writeJSON(ioctx.stdout, buildDefJSON(locs, sess.cwd))
	}
	_, err = fmt.Fprintln(ioctx.stdout, lsp.FormatLocations(locs, sess.cwd, 2))
	return err
}

func (c *hoverCommand) Run(ctx context.Context, ioctx *commandIO) error {
	sess, err := setupLSPSession(ctx, []string{c.File, c.Line, c.Symbol})
	if err != nil {
		return err
	}
	defer sess.mgr.Shutdown(context.Background())
	hover, err := sess.client.Hover(ctx, sess.file, sess.line, sess.col)
	if err != nil {
		return fmt.Errorf("hover: %w", err)
	}
	if c.JSON {
		return writeJSON(ioctx.stdout, buildHoverJSON(hover))
	}
	_, err = fmt.Fprintln(ioctx.stdout, lsp.FormatHover(hover))
	return err
}

func (c *symbolsCommand) Run(ctx context.Context, ioctx *commandIO) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	file := resolveFilePath(c.File, cwd)
	mgr := lsp.NewManager(cwd)
	defer mgr.Shutdown(context.Background())
	client, lang, err := mgr.ForFile(ctx, file)
	if err != nil {
		return err
	}
	if err := mgr.EnsureFileOpen(ctx, client, file, lang); err != nil {
		return err
	}
	syms, err := client.DocumentSymbols(ctx, file)
	if err != nil {
		return fmt.Errorf("symbols: %w", err)
	}
	if c.JSON {
		return writeJSON(ioctx.stdout, buildSymbolsJSON(syms, file))
	}
	_, err = fmt.Fprintln(ioctx.stdout, lsp.FormatSymbols(syms, cwd))
	return err
}

// ---------------------------------------------------------------------------
// JSON builders
// ---------------------------------------------------------------------------

func buildDefJSON(locs []lsp.Location, cwd string) jsonDefOutput {
	if len(locs) == 0 {
		return jsonDefOutput{Definition: nil}
	}
	loc := locs[0]
	return jsonDefOutput{Definition: &jsonLocation{
		File:   uriToRel(loc.URI, cwd),
		Line:   loc.Range.Start.Line + 1,
		Column: loc.Range.Start.Character + 1,
	}}
}

func buildRefsJSON(locs []lsp.Location, cwd string) jsonRefsOutput {
	out := jsonRefsOutput{References: make([]jsonLocation, 0, len(locs))}
	for _, loc := range locs {
		out.References = append(out.References, jsonLocation{
			File:   uriToRel(loc.URI, cwd),
			Line:   loc.Range.Start.Line + 1,
			Column: loc.Range.Start.Character + 1,
		})
	}
	return out
}

func buildHoverJSON(hover *lsp.HoverResult) jsonHoverOutput {
	if hover == nil {
		return jsonHoverOutput{}
	}
	return jsonHoverOutput{Hover: hover.Contents.Value}
}

func buildSymbolsJSON(syms []lsp.DocumentSymbol, file string) jsonSymbolsOutput {
	out := jsonSymbolsOutput{Symbols: make([]jsonSymbol, 0, len(syms))}
	flattenSymbols(&out.Symbols, syms, file)
	return out
}

func flattenSymbols(dst *[]jsonSymbol, syms []lsp.DocumentSymbol, file string) {
	for _, s := range syms {
		*dst = append(*dst, jsonSymbol{
			Name:   s.Name,
			Kind:   s.Kind.String(),
			File:   file,
			Line:   s.Range.Start.Line + 1,
			Column: s.Range.Start.Character + 1,
		})
		if len(s.Children) > 0 {
			flattenSymbols(dst, s.Children, file)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// lspSession holds a ready-to-use LSP client positioned at a symbol column.
type lspSession struct {
	client *lsp.Client
	mgr    *lsp.Manager
	file   string
	line   int // 0-based
	col    int // 0-based
	cwd    string
}

// setupLSPSession parses position args, starts an LSP manager, opens the file,
// and resolves the symbol column. The caller must defer session.mgr.Shutdown.
func setupLSPSession(ctx context.Context, args []string) (*lspSession, error) {
	file, line, symbol, cwd, err := parsePositionArgs(args)
	if err != nil {
		return nil, err
	}

	mgr := lsp.NewManager(cwd)

	client, lang, err := mgr.ForFile(ctx, file)
	if err != nil {
		mgr.Shutdown(context.Background())
		return nil, err
	}
	if err := mgr.EnsureFileOpen(ctx, client, file, lang); err != nil {
		mgr.Shutdown(context.Background())
		return nil, err
	}

	col, err := lsp.FindSymbolColumn(file, line-1, symbol)
	if err != nil {
		mgr.Shutdown(context.Background())
		return nil, fmt.Errorf("find column: %w", err)
	}

	return &lspSession{
		client: client,
		mgr:    mgr,
		file:   file,
		line:   line - 1, // convert to 0-based
		col:    col,
		cwd:    cwd,
	}, nil
}

func writeJSON(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// parsePositionArgs extracts file, line (1-based), symbol, and cwd from args.
func parsePositionArgs(args []string) (file string, line int, symbol string, cwd string, err error) {
	cwd, err = os.Getwd()
	if err != nil {
		return "", 0, "", "", fmt.Errorf("getwd: %w", err)
	}

	file = resolveFilePath(args[0], cwd)

	line, err = strconv.Atoi(args[1])
	if err != nil || line < 1 {
		return "", 0, "", "", fmt.Errorf("line must be a positive integer, got %q", args[1])
	}

	symbol = args[2]
	return file, line, symbol, cwd, nil
}

// resolveFilePath makes a path absolute relative to cwd if it isn't already.
func resolveFilePath(path, cwd string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}

// uriToRel converts an LSP file:// URI to a path relative to cwd.
func uriToRel(uri, cwd string) string {
	path := lsp.URIToPath(uri)
	if cwd == "" {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return path
	}
	return rel
}
