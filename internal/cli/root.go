package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/dotcommander/repomap"
)

type commandIO struct {
	stdout io.Writer
	stderr io.Writer
}

type rootCommand struct {
	Artifact        string                 `help:"Write command output to this file instead of stdout"`
	Map             mapCommand             `cmd:"" default:"withargs" hidden:""`
	Brief           briefCommand           `cmd:"" help:"Print an agent boot digest (identity + verify + state) followed by the repo map"`
	Audit           auditCommand           `cmd:"" help:"Emit deterministic audit prepass facts"`
	Cache           cacheCommand           `cmd:"" help:"Inspect repomap disk cache state"`
	Find            findCommand            `cmd:"" help:"Locate a symbol by name with optional kind/file qualifiers"`
	Impact          impactCommand          `cmd:"" help:"Show deterministic local impact facts for a file"`
	Inventory       inventoryCommand       `cmd:"" help:"Answer ownership for a boundary such as Postgres"`
	Context         contextCommand         `cmd:"" help:"Show bounded source and impact context for a symbol"`
	Endpoint        endpointCommand        `cmd:"" help:"Show route registration, handler, callee names, and touching tests"`
	Explain         explainCommand         `cmd:"" help:"Show why a file ranked and rendered the way it did"`
	Orphans         orphansCommand         `cmd:"" help:"List exported symbols with zero inbound references (dead-code candidates)"`
	Init            initCommand            `cmd:"" help:"Scaffold .repomap.yaml and install a post-commit cache-warm hook"`
	LSP             lspCommand             `cmd:"" help:"Inspect LSP semantic coverage"`
	Refs            refsCommand            `cmd:"" help:"Find all references to a symbol"`
	Def             defCommand             `cmd:"" help:"Jump to the definition of a symbol"`
	Hover           hoverCommand           `cmd:"" help:"Get type info and docs for a symbol"`
	Symbols         symbolsCommand         `cmd:"" help:"List symbols defined in a file"`
	Serve           serveCommand           `cmd:"" help:"Start a long-lived JSON-RPC 2.0 server on stdin/stdout"`
	Commit          commitCommand          `cmd:"" help:"Commit-flow helpers (analyze changesets, emit group plans)"`
	CommitPreflight commitPreflightCommand `cmd:"" name:"commit-preflight" help:"Emit git/gh context block for commit preflight"`
}

type mapCommand struct {
	Directory         string   `arg:"" optional:"" type:"path" default:"." help:"Directory to map"`
	Tokens            int      `short:"t" default:"2048" help:"Token budget"`
	Format            string   `short:"f" help:"Output format: compact (orientation: names only), verbose, detail, lines, xml (default: enriched — signatures + godoc + fields)"`
	JSON              bool     `help:"Output as JSON array of lines"`
	JSONLegacy        bool     `help:"Emit --json output as a bare array (pre-v0.7.0 format). Use only for legacy scripts; will be removed in a future release."`
	JSONStructured    bool     `help:"Output a structured JSON repository map"`
	Calls             bool     `help:"Expand exported symbols with semantic caller information"`
	Precise           bool     `help:"Deprecated compatibility flag; include callers regardless of --calls-threshold"`
	CallsThreshold    int      `default:"2" help:"Only expand symbols in files with at least N importers"`
	CallsLimit        int      `default:"10" help:"Max callers shown per symbol"`
	CallsIncludeTests bool     `help:"Include _test.go callers (excluded by default)"`
	NoCache           bool     `help:"Deprecated compatibility flag; semantic callers are built with the map"`
	CallsUseBinary    bool     `hidden:""`
	Intent            string   `short:"i" help:"Natural-language query for task-aware ranking (BM25). Reranks files silently — add --explain to see the score breakdown."`
	Consumed          []string `sep:"," help:"Comma-separated file paths already read; these are downranked and their importers upranked"`
	SymbolRefs        bool     `help:"Enable approximate cross-language symbol reference scoring"`
	ExplainScores     bool     `name:"explain" help:"Append per-file confidence-tier score breakdown (including the --intent contribution) to text output."`
	IncludeTests      bool     `help:"Rank _test.go files at full weight (default: demoted)"`
}

// Execute parses and runs one CLI invocation without terminating the process.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return execute(ctx, args, stdout, stderr)
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	args = normalizeHelpArgs(args)
	var tree rootCommand
	ioctx := &commandIO{stdout: stdout, stderr: stderr}
	parser, err := kong.New(&tree,
		kong.Name("repomap"),
		kong.Description(rootDescription),
		kong.Writers(stdout, stderr),
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.Bind(ioctx),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact:             true,
			Tree:                true,
			Summary:             true,
			FlagsLast:           true,
			NoExpandSubcommands: true,
		}),
		kong.Help(repomapHelp),
	)
	if err != nil {
		return err
	}
	exited := false
	parser.Exit = func(int) { exited = true }
	parsed, err := parser.Parse(args)
	if exited {
		return nil
	}
	if err != nil {
		return err
	}
	if tree.Artifact == "" {
		return parsed.Run()
	}
	f, err := os.Create(tree.Artifact)
	if err != nil {
		return fmt.Errorf("create artifact: %w", err)
	}
	ioctx.stdout = f
	runErr := parsed.Run()
	closeErr := f.Close()
	if runErr != nil {
		return runErr
	}
	if closeErr != nil {
		return fmt.Errorf("close artifact: %w", closeErr)
	}
	return nil
}

func normalizeHelpArgs(args []string) []string {
	helpAt := 0
	for helpAt < len(args) {
		switch {
		case args[helpAt] == "--artifact" && helpAt+1 < len(args):
			helpAt += 2
		case strings.HasPrefix(args[helpAt], "--artifact="):
			helpAt++
		default:
			if args[helpAt] != "help" {
				return args
			}
			out := append([]string{}, args[:helpAt]...)
			out = append(out, args[helpAt+1:]...)
			return append(out, "--help")
		}
	}
	return args
}

// ExitCode maps a returned command error to its process status.
func ExitCode(err error) int {
	var commandErr commandExitError
	if errors.As(err, &commandErr) {
		return commandErr.code
	}
	if code := repomap.ExecExitCode(err); code != 0 {
		return code
	}
	return 1
}

func (c *mapCommand) Run(ctx context.Context, ioctx *commandIO) error {
	absDir, err := filepath.Abs(c.Directory)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	cfg := repomap.Config{
		MaxTokens: c.Tokens, MaxTokensNoCtx: c.Tokens, Intent: c.Intent,
		ConsumedPaths: c.Consumed, SymbolRefs: c.SymbolRefs,
		Explain: c.ExplainScores, IncludeTests: c.IncludeTests,
		GoAnalysis:      c.Calls || c.JSONStructured,
		GoAnalysisCalls: c.Calls,
		GoAnalysisTests: c.Calls && c.CallsIncludeTests,
	}
	m := repomap.New(absDir, cfg)
	if err := m.Build(ctx); err != nil {
		return err
	}
	if c.Precise && !c.Calls {
		fmt.Fprintln(ioctx.stderr, "repomap: --precise has no effect without --calls")
	}
	if !c.Calls {
		return renderStandard(ioctx.stdout, m, c.Format, c.JSON, c.JSONLegacy, c.JSONStructured)
	}
	return renderWithCalls(ctx, ioctx.stdout, ioctx.stderr, m, c.Format, c.JSON, c.JSONLegacy, c.JSONStructured, absDir, c.CallsThreshold, c.CallsLimit, c.CallsIncludeTests, c.NoCache, c.CallsUseBinary, c.Precise)
}

func renderStandard(w io.Writer, m *repomap.Map, format string, asJSON bool, jsonLegacy bool, jsonStructured bool) error {
	if jsonStructured {
		data, err := m.StructuredJSON()
		if err != nil {
			return err
		}
		_, err = w.Write(append(data, '\n'))
		return err
	}
	if asJSON {
		return printJSON(w, m, jsonLegacy)
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
	_, err := fmt.Fprint(w, out)
	return err
}
