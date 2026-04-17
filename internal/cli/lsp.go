package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/dotcommander/repomap/internal/lsp"
	"github.com/spf13/cobra"
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
// LSP subcommand group
// ---------------------------------------------------------------------------

// newLSPCmds returns the lsp subcommands (refs, def, hover, symbols).
// They share a --json flag via a closure.
func newLSPCmds() []*cobra.Command {
	return []*cobra.Command{
		newRefsCmd(),
		newDefCmd(),
		newHoverCmd(),
		newSymbolsCmd(),
	}
}

// newRefsCmd builds `repomap refs FILE LINE SYMBOL`.
func newRefsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "refs FILE LINE SYMBOL",
		Short: "Find all references to a symbol",
		Long: `Find all references to a symbol at FILE:LINE named SYMBOL.
LINE is 1-based. SYMBOL is the identifier name on that line.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, err := setupLSPSession(cmd.Context(), args)
			if err != nil {
				return err
			}
			defer sess.mgr.Shutdown(context.Background())

			locs, err := sess.client.References(cmd.Context(), sess.file, sess.line, sess.col)
			if err != nil {
				return fmt.Errorf("references: %w", err)
			}

			if asJSON {
				return writeJSON(buildRefsJSON(locs, sess.cwd))
			}
			fmt.Println(lsp.FormatLocations(locs, sess.cwd, 1))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output JSON")
	return cmd
}

// newDefCmd builds `repomap def FILE LINE SYMBOL`.
func newDefCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "def FILE LINE SYMBOL",
		Short: "Jump to the definition of a symbol",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, err := setupLSPSession(cmd.Context(), args)
			if err != nil {
				return err
			}
			defer sess.mgr.Shutdown(context.Background())

			locs, err := sess.client.Definition(cmd.Context(), sess.file, sess.line, sess.col)
			if err != nil {
				return fmt.Errorf("definition: %w", err)
			}

			if asJSON {
				return writeJSON(buildDefJSON(locs, sess.cwd))
			}
			fmt.Println(lsp.FormatLocations(locs, sess.cwd, 2))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output JSON")
	return cmd
}

// newHoverCmd builds `repomap hover FILE LINE SYMBOL`.
func newHoverCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "hover FILE LINE SYMBOL",
		Short: "Get type info and docs for a symbol",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, err := setupLSPSession(cmd.Context(), args)
			if err != nil {
				return err
			}
			defer sess.mgr.Shutdown(context.Background())

			hover, err := sess.client.Hover(cmd.Context(), sess.file, sess.line, sess.col)
			if err != nil {
				return fmt.Errorf("hover: %w", err)
			}

			if asJSON {
				return writeJSON(buildHoverJSON(hover))
			}
			fmt.Println(lsp.FormatHover(hover))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output JSON")
	return cmd
}

// newSymbolsCmd builds `repomap symbols FILE`.
func newSymbolsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "symbols FILE",
		Short: "List symbols defined in a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}

			file := resolveFilePath(args[0], cwd)

			mgr := lsp.NewManager(cwd)
			defer mgr.Shutdown(context.Background())

			client, lang, err := mgr.ForFile(cmd.Context(), file)
			if err != nil {
				return err
			}
			if err := mgr.EnsureFileOpen(cmd.Context(), client, file, lang); err != nil {
				return err
			}

			syms, err := client.DocumentSymbols(cmd.Context(), file)
			if err != nil {
				return fmt.Errorf("symbols: %w", err)
			}

			if asJSON {
				return writeJSON(buildSymbolsJSON(syms, file))
			}
			fmt.Println(lsp.FormatSymbols(syms, cwd))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output JSON")
	return cmd
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

func writeJSON(v any) error {
	return json.NewEncoder(os.Stdout).Encode(v)
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
