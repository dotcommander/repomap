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
	Cache           cacheCommand           `cmd:"" help:"Inspect or warm repomap disk cache state"`
	Find            findCommand            `cmd:"" help:"Locate a symbol by name with optional kind/file qualifiers"`
	Impact          impactCommand          `cmd:"" help:"Show deterministic local impact facts for a file"`
	Inventory       inventoryCommand       `cmd:"" help:"Answer ownership for a boundary such as Postgres"`
	Task            taskCommand            `cmd:"" help:"Build a bounded implementation handoff for a goal"`
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
	Format            string   `short:"f" default:"enriched" enum:"enriched,compact,verbose,detail,lines,xml" help:"Output format: compact (orientation: names only), verbose, detail, lines, xml (default: enriched — signatures + godoc + fields)"`
	JSON              bool     `help:"Output a schema-versioned JSON envelope containing rendered lines"`
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

func (c *mapCommand) Validate() error {
	if c.Tokens <= 0 {
		return fmt.Errorf("--tokens must be greater than zero")
	}
	if c.JSONLegacy && !c.JSON {
		return fmt.Errorf("--json-legacy requires --json")
	}
	if c.JSONStructured && (c.JSON || c.JSONLegacy) {
		return fmt.Errorf("--json-structured is mutually exclusive with --json and --json-legacy")
	}
	if c.CallsUseBinary {
		return fmt.Errorf("--calls-use-binary is no longer supported; remove it and use the built-in semantic callers")
	}
	for _, value := range []struct {
		name  string
		value int
	}{
		{"calls-threshold", c.CallsThreshold},
		{"calls-limit", c.CallsLimit},
	} {
		if value.value < 0 {
			return fmt.Errorf("--%s must be zero or greater", value.name)
		}
	}
	return nil
}

// Execute parses and runs one CLI invocation without terminating the process.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return execute(ctx, args, stdout, stderr)
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	args = normalizeHelpArgs(args)
	var tree rootCommand
	ioctx := &commandIO{stdout: stdout, stderr: stderr}
	parser, err := newRootParser(&tree, ctx, ioctx, stdout, stderr)
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
	return writeArtifact(tree.Artifact, &ioctx.stdout, func() error { return parsed.Run() })
}

// newRootParser constructs the live CLI model used for both execution and
// command-surface inventory. Keeping it in one place prevents inventory from
// drifting from the parser users actually invoke.
func newRootParser(tree *rootCommand, ctx context.Context, ioctx *commandIO, stdout, stderr io.Writer) (*kong.Kong, error) {
	return kong.New(tree,
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
}

func writeArtifact(target string, stdout *io.Writer, run func() error) (err error) {
	mode, err := artifactMode(target)
	if err != nil {
		return err
	}
	directory := filepath.Dir(target)
	temp, err := os.CreateTemp(directory, "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create artifact temp file: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		if temp != nil {
			_ = temp.Close()
		}
		if err != nil {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		return fmt.Errorf("set artifact permissions: %w", err)
	}

	*stdout = temp
	if err := run(); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync artifact: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close artifact: %w", err)
	}
	temp = nil
	if err := os.Rename(tempName, target); err != nil {
		return fmt.Errorf("replace artifact: %w", err)
	}
	return nil
}

func artifactMode(target string) (os.FileMode, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return 0o644, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect artifact target: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("artifact target %q is a symlink; choose a regular file path", target)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("artifact target %q is not a regular file", target)
	}
	return info.Mode().Perm(), nil
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
		_, _ = fmt.Fprintln(ioctx.stderr, "repomap: --precise has no effect without --calls")
	}
	if !c.Calls {
		return renderStandard(ioctx.stdout, m, c.Format, c.JSON, c.JSONLegacy, c.JSONStructured)
	}
	return renderWithCalls(ctx, ioctx.stdout, ioctx.stderr, m, c.Format, c.JSON, c.JSONLegacy, c.JSONStructured, absDir, c.CallsThreshold, c.CallsLimit, c.CallsIncludeTests, c.NoCache, c.CallsUseBinary, c.Precise)
}

func renderStandard(w io.Writer, m *repomap.Map, format string, asJSON bool, jsonLegacy bool, jsonStructured bool) error {
	ranked := m.Ranked()
	return writeRankedWithinBudget(w, m.Config().MaxTokens, ranked, func(selected []repomap.RankedFile) ([]byte, error) {
		return encodeStandard(m, selected, format, asJSON, jsonLegacy, jsonStructured)
	})
}
