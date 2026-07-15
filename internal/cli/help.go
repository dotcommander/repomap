package cli

import (
	"fmt"

	"github.com/alecthomas/kong"
)

const rootDescription = `Scans a project's source files, extracts exported symbols
(functions, methods, structs, interfaces, types, constants, variables),
ranks files by importance, and outputs a compact Markdown summary.
Pass --intent to bias the output toward files relevant to a specific task.`

const rootHelp = rootDescription + `

Usage:
  repomap [directory] [flags]
  repomap <command>

Examples:
  # Default "enriched" map — exported symbols + signatures + godoc, 2048-token budget
  repomap ./src

  # Lean orientation — symbol NAMES only; fits more files in the same budget
  repomap -f compact ./src

  # Every symbol with full signatures + struct fields, no budget limit
  repomap -f detail ./src

  # Machine-readable XML — dependency graph + symbol attributes (line, span, params)
  repomap -f xml ./src

  # Structured JSON repository map (distinct schema from --json)
  repomap --json-structured ./src

  # Task-aware ranking; pair with --explain to see WHY each file ranked
  repomap --intent "auth middleware" --explain ./src

Commands:
  audit             Emit deterministic audit prepass facts
  brief             Print an agent boot digest (identity + verify + state) followed by the repo map
  cache             Inspect repomap disk cache state
  commit            Commit-flow helpers (analyze changesets, emit group plans)
  commit-preflight  Emit git/gh context block for commit preflight
  context           Show bounded source and impact context for a symbol
  def               Jump to the definition of a symbol
  endpoint          Show route registration, handler, callee names, and touching tests
  explain           Show why a file ranked and rendered the way it did
  find              Locate a symbol by name with optional kind/file qualifiers
  hover             Get type info and docs for a symbol
  help              Show help for a command
  impact            Show deterministic local impact facts for a file
  init              Scaffold .repomap.yaml and install a post-commit cache-warm hook
  inventory         Answer ownership for a boundary such as Postgres
  lsp               Inspect LSP semantic coverage
  orphans           List exported symbols with zero inbound references (dead-code candidates)
  refs              Find all references to a symbol
  serve             Start a long-lived JSON-RPC 2.0 server on stdin/stdout
  symbols           List symbols defined in a file

Flags:
      --artifact=STRING       Write command output to this file instead of stdout
      --calls                 Expand exported symbols with semantic caller information
      --calls-include-tests   Include _test.go callers (excluded by default)
      --calls-limit=10        Max callers shown per symbol
      --calls-threshold=2     Only expand symbols in files with at least N importers
      --consumed=STRINGS      Comma-separated file paths already read; these are downranked and their importers upranked
      --explain               Append per-file confidence-tier score breakdown (including the --intent contribution) to text output.
  -f, --format=STRING         Output format: compact (orientation: names only), verbose, detail, lines, xml (default: enriched — signatures + godoc + fields)
      --include-tests         Rank _test.go files at full weight (default: demoted)
  -i, --intent=STRING         Natural-language query for task-aware ranking (BM25). Reranks files silently — add --explain to see the score breakdown.
      --json                  Output as JSON array of lines
      --json-legacy           Emit --json output as a bare array (pre-v0.7.0 format). Use only for legacy scripts; will be removed in a future release.
      --json-structured       Output a structured JSON repository map
      --no-cache              Deprecated compatibility flag; semantic callers are built with the map
      --precise               Deprecated compatibility flag; include callers regardless of --calls-threshold
      --symbol-refs           Enable approximate cross-language symbol reference scoring
  -t, --tokens=2048           Token budget
  -h, --help                  Show help

Run "repomap <command> --help" for more information on a command.
`

var commandLongHelp = map[string]string{
	"audit": `Emit deterministic audit prepass facts for deep-review workflows.

Audit commands produce leads and lane packets, not final findings. Promote a
lead only after checking source, docs, runtime behavior, or another
authoritative signal.`,
	"find": `Resolve a symbol across the ranked symbol set.

Query syntax (positional):
  repomap find Config                       name = Config
  repomap find kind:struct:Config           kind = struct, name = Config
  repomap find file:parser:Parse            file = parser, name = Parse
  repomap find kind:struct:file:cli:Root    kind = struct, file = cli, name = Root

Flags override or supplement query qualifiers.`,
	"init": `Creates .repomap.yaml at the project root (if absent) and installs a
git post-commit hook that refreshes the repomap cache in the background.
Idempotent: re-running without --force skips existing files.`,
	"refs": `Find all references to a symbol at FILE:LINE named SYMBOL.
LINE is 1-based. SYMBOL is the identifier name on that line.`,
	"orphans": `Lists exported symbols that have no inbound references within this repository.

Candidates only — repomap sees one repo. Verify external/library consumers
before deleting. Never treat this list as a verdict.

Requires gopls on PATH.`,
	"serve": `Start a long-lived JSON-RPC 2.0 server on stdin/stdout.

The repository map is built once on startup and kept warm. Subsequent queries
skip the scan→parse→rank pipeline unless the map becomes stale. Requests and
responses use NDJSON framing: one JSON-RPC 2.0 object per line.`,
	"commit analyze": `Analyzes the current git changeset and emits a compact JSON plan on stdout.
Large content (diffs, untracked, findings) is written to files referenced by
CommitAnalysis.Refs.*.

Typical usage from a commit agent:

    repomap commit analyze | jq .
    repomap commit analyze --tag    # activate release gate`,
	"commit execute": `Reads a CommitAnalysis JSON plan file and executes the commits deterministically.

Typical usage:

    repomap commit analyze > /tmp/plan.json
    repomap commit execute --plan-file /tmp/plan.json [--push] [--tag v1.2.3]`,
	"commit auto": `Runs 'commit prep --json' and inspects the status field:

  ready           runs 'commit finish' with auto-detected --push/--tag and emits finish's JSON
  needs_judgment  emits prep's JSON unchanged; caller must adjudicate, then call 'commit finish'
  abort           emits prep's JSON unchanged; caller surfaces abort_reason`,
	"commit prep": `Runs the full pre-commit pipeline and emits a JSON payload for the agent.

The agent calls 'commit finish --prep-token <t>' with any decisions for
ambiguous findings or low-confidence subjects.`,
	"commit finish": `Loads the state written by 'commit prep --prep-token <t>' and executes it.

Accepts optional decisions for ambiguous findings and low-confidence subjects
via --decisions (inline JSON or @path).`,
}

func repomapHelp(options kong.HelpOptions, ctx *kong.Context) error {
	if ctx.Selected() == nil {
		_, err := fmt.Fprint(ctx.Stdout, rootHelp)
		return err
	}
	if long := commandLongHelp[ctx.Command()]; long != "" {
		if _, err := fmt.Fprintln(ctx.Stdout, long+"\n"); err != nil {
			return err
		}
	}
	return kong.DefaultHelpPrinter(options, ctx)
}
