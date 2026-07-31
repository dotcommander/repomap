# Configuration

This page covers root map flags and the main subcommand flags.

## Flags

| Flag | Short | Default | Description |
| --- | --- | --- | --- |
| `--tokens` | `-t` | `2048` | Complete-output budget using `ceil(UTF-8 bytes / 4)` |
| `--format` | `-f` | `enriched` | One of `enriched`, `compact`, `verbose`, `detail`, `lines`, `xml` |
| `--json` | — | `false` | Emit verbose output as a JSON envelope of lines |
| `--json-legacy` | — | `false` | Emit the legacy bare `[]string` JSON shape |
| `--json-structured` | — | `false` | Emit schema-versioned file/symbol/call-site/ranking data |
| `--calls` | — | `false` | Expand exported symbols with receiver-qualified semantic Go callers |
| `--calls-threshold` | — | `2` | Only expand symbols in files with at least this many importers |
| `--calls-limit` | — | `10` | Max callers shown per symbol |
| `--calls-include-tests` | — | `false` | Include `_test.go` callers |
| `--no-cache` | — | `false` | Deprecated compatibility flag; semantic callers use the map build |
| `--precise` | — | `false` | Deprecated compatibility flag; ignore `--calls-threshold` |
| `--intent` | `-i` | `""` | Natural language query for BM25 task-aware ranking |
| `--consumed` | — | `[]` | File paths already read; these are downranked and their importers upranked |
| `--symbol-refs` | — | `false` | Enable approximate cross-language symbol reference scoring |
| `--explain` | — | `false` | Append rank-score evidence to text output |
| `--include-tests` | — | `false` | Rank test files at full weight |
| `--artifact` | — | `""` | Atomically write successful stdout to a regular file; existing permissions are preserved |
| `--help` | `-h` | `false` | Show help without running the command or creating an artifact |

## Positional argument

```bash
repomap [directory]
```

Zero or one argument. Defaults to `.`. Must be a directory that contains source files.

## Token budget

A token is roughly four bytes. The budget controls:

- How many files make it into compact output
- How much detail each file gets (header only, summary, symbols, symbols + fields)
- How many source lines show in lines format

```bash
repomap -t 1024    # tight prompt
repomap -t 4096    # twice the 2048 default
repomap -t 16384   # practically uncapped
```

Every bounded format counts the complete encoded output, including headers and
structured wrappers. Records are omitted whole when necessary; structured
output is never cut mid-record.

## Format

```bash
repomap               # default; enriched — signatures + godoc + fields
repomap -f compact    # explicit compact; budget-trimmed
repomap -f verbose    # all symbols
repomap -f detail     # verbose + signatures + struct fields
repomap -f lines      # actual source lines
repomap -f xml        # structured output
```

See [Output Formats](03-output-formats.md) for examples.

## JSON

```bash
repomap --json
```

Equivalent to running `-f verbose` and wrapping the output in `{schema_version, lines}`. The flag overrides `-f`. Add `--json-legacy` only for scripts that still expect a bare array of lines.

## Intent ranking

```bash
repomap --intent "fix token refresh" .
repomap -i "add rate limiting to the API" ./src
```

When `--intent` is set, repomap BM25-scores each file against the query using field-weighted keywords from symbols, file paths, and imports. High-scoring files are promoted to higher detail levels before budget allocation — task-relevant code gets more visibility without changing the token budget.

Omit the flag and behavior is unchanged.

## Consumed paths

```bash
repomap --consumed internal/auth/jwt.go,internal/auth/handler.go .
repomap --consumed auth/jwt.go --consumed auth/handler.go .
```

When `--consumed` is set, each named file has its score halved (downranked) and files that import a consumed file gain +15 per consumed dependency (capped at +45). This pushes unfamiliar code higher in the output without changing the token budget.

Composes with `--intent`: BM25 runs first, then consumed adjustment is applied. Omit the flag and behavior is unchanged.

## Symbol context command

```bash
repomap context RankFiles
repomap context kind:function:file:ranker:RankFiles --json
```

| Flag | Default | Description |
| --- | --- | --- |
| `--kind` | `""` | Filter by symbol kind |
| `--file` | `""` | Filter to files matching this substring |
| `--json` | `false` | Emit structured context JSON |
| `--calls` | `false` | Include exact callers from the semantic Go graph |
| `--precise` | `false` | Deprecated compatibility flag; semantic callers are already used |
| `--calls-include-tests` | `false` | Include `_test.go` callers |
| `--calls-limit` | `10` | Max callers to include with `--calls` |
| `--max-source-lines` | `200` | Max source lines to include for the selected symbol |
| `--max-output-lines` | `400` | Max text output lines; `0` means unlimited |
| `--max-output-bytes` | `65536` | Max text output bytes; `0` means unlimited |

The positional query accepts the same qualifiers as `find`: `kind:<kind>:`, `file:<substring>:`, then the symbol name.

## Cache status command

```bash
repomap cache status
repomap cache status --cache-dir /tmp/repomap-cache --json
repomap cache warm . --cache-dir /tmp/repomap-cache
```

| Flag | Default | Description |
| --- | --- | --- |
| `--cache-dir` | `$HOME/.cache/repomap` | Cache directory to inspect |
| `--json` | `false` | Emit structured cache status JSON |

`cache warm` accepts the same `--cache-dir` flag. It prints text cache status only after the written cache is usable and fresh.

## Complete command and flag reference

The executable leaves are:

`map` (the default command), `brief`, `task`, `audit hygiene`, `audit brief`,
`audit risks`, `audit surface`, `audit effects`, `cache status`, `cache warm`,
`find`, `impact`, `inventory`, `context`, `endpoint`, `explain`, `orphans`,
`init`, `lsp status`, `refs`, `def`, `hover`, `symbols`, `serve`,
`commit analyze`, `commit execute`, `commit prep`, `commit finish`,
`commit auto`, and `commit-preflight`.

All commands inherit `--artifact` and `--help`. The remaining visible flags are
listed below. Empty string and `false` are the defaults unless stated otherwise.

| Command | Flags, defaults, and accepted values |
| --- | --- |
| default map | `--tokens, -t=2048` (>0); `--format, -f=enriched` (`enriched`, `compact`, `verbose`, `detail`, `lines`, `xml`); `--json`; `--json-legacy`; `--json-structured`; `--calls`; `--precise`; `--calls-threshold=2` (>=0); `--calls-limit=10` (0 = unlimited); `--calls-include-tests`; `--no-cache`; `--intent, -i`; `--consumed`; `--symbol-refs`; `--explain`; `--include-tests` |
| `task` | `--tokens, -t=4096` (>0); `--json`; `--consumed` |
| `audit hygiene` | `--json` |
| `audit brief`, `audit risks`, `audit surface` | `--limit=20` (0 = all); `--top-files=0` (0 = use `--limit`); `--intent, -i`; `--json` |
| `audit effects` | audit packet flags plus `--kind` (`all`, `database`, `filesystem-write`, `filesystem-read`, `subprocess`, `process-exit`, `http`, `serialization`, `secret`, `crypto`, `time`, `randomness`, `context-background`, `goroutine`, `unbounded-read`) and `--paths-only` |
| `cache status` | `--cache-dir=$HOME/.cache/repomap`; `--json` |
| `cache warm` | `--cache-dir=$HOME/.cache/repomap` |
| `find` | `--kind`; `--file`; `--limit=20` (0 = unlimited); `--format=text` (`text`, `json`) |
| `impact` | `--json`; `--markdown` |
| `inventory` | required `--boundary`; `--json` |
| `context` | `--kind`; `--file`; `--max-source-lines=200` (>0); `--max-output-lines=400` (0 = unlimited); `--max-output-bytes=65536` (0 = unlimited); `--json`; `--calls`; `--precise`; `--calls-include-tests`; `--calls-limit=10` (0 = unlimited) |
| `endpoint` | `--json`; `--max-output-lines=400` (0 = unlimited) |
| `explain`, `orphans`, `lsp status`, `refs`, `def`, `hover`, `symbols` | `--json` |
| `init` | `--force`; `--no-hook`; `--no-config` |
| `commit analyze` | `--tag`; `--pretty`; `--tmpdir`; `--confidence=0.75` (0 through 1) |
| `commit execute` | required `--plan-file`; `--push`; `--tag`; `--no-release`; `--release-notes-from`; `--dry-run`; `--json`; `--skip-fix` |
| `commit prep` | `--json`; `--no-review`; `--tag`; `--allow-large` |
| `commit finish` | required `--prep-token`; `--decisions`; `--push`; `--tag`; `--json=true` |
| `commit auto` | `--no-review`; `--allow-large`; `--tag`; `--decisions` |
| `brief`, `serve`, `commit-preflight` | no command-specific flags |

`task` requires a nonblank positional goal and accepts an optional repository
directory after it. Consumed paths are normalized relative to that repository;
paths outside it are rejected. The library API uses 4096 tokens when
`TaskOptions.MaxTokens` is zero and rejects negative values.

Task JSON is schema version 1. Its top-level fields are `root`, `goal`,
`budget`, `selection`, `rules`, `related_changes`, `targets`, `read_next`,
`verify_commands`, `follow_up_commands`, `diagnostics`, and `truncations`.
Selection confidence is deterministic match strength, not a correctness claim.
Relationship provenance is `exact` for semantic evidence, `syntactic` for
parsed structure, and `heuristic` for naming or adjacency. Every omitted
bounded field records `{field, shown, total, reason}` in `truncations`.

All bounded map and task formats count their complete encoded stdout, including
headers, dependency flow, explanations, and JSON or XML wrappers. Structured
output is reduced only at whole records and never byte-cut. A budget too small
for the minimum valid envelope fails before stdout or an artifact is written.

Precedence and compatibility rules:

- `--json-legacy` requires `--json`. `--json-structured` is mutually exclusive
  with both line-oriented JSON flags. JSON modes take precedence over `--format`.
- `--top-files` overrides `--limit` when nonzero.
- Explicit `find --kind` and `find --file` override qualifiers embedded in the
  positional query.
- `impact --json` and `impact --markdown` are mutually exclusive.
- `--precise` only affects caller output with `--calls`; `--no-cache` remains a
  no-op compatibility flag. The removed hidden `--calls-use-binary` flag fails
  with a migration message.
- Hidden `commit auto --force-mode` accepts only `FULL` or `LOCAL` and exists
  for isolated tests; it is not a supported operator flag.

## Environment

None. repomap reads no environment variables.

## Config file

repomap reads `.repomap.yaml` from the project root when it exists. The file controls what gets scanned and how detail levels are forced. Absent file = no-op; every run without it is fully explicit.

```yaml
method_blocklist:
  - "Test*"           # glob: drop symbols starting with "Test"
  - "*Mock"           # glob: drop symbols ending in "Mock"
  - "/^pb_/"          # regex: drop generated protobuf symbols

include_paths:
  - "cmd/*"
  - "internal/*"
  - "pkg/*"

exclude_paths:
  - "internal/generated/*"
  - "vendor/*"

file_overrides:
  "cmd/*/main.go": "full"
  "internal/generated/**": "omit"
```

| Field | Purpose |
| --- | --- |
| `method_blocklist` | Glob (`Test*`) or regex (`/^pb_/`) patterns — drops matching symbol names at parse time |
| `include_paths` | When non-empty, only files matching these path globs are scanned |
| `exclude_paths` | Files matching these path globs are always excluded; takes precedence over `include_paths` |
| `file_overrides` | Forces a file to a fixed detail level regardless of rank. Values: `"full"` or `"omit"` |

Path globs use `path.Match` semantics. Patterns containing `**` match any path with the corresponding prefix (e.g. `internal/generated/**` covers all files under that directory).

## What lives in `Config` (library)

The library exposes these build and ranking controls via `repomap.Config`:

| Field | Default | Purpose |
| --- | --- | --- |
| `MaxTokens` | `1024` | Budget for compact and XML formats |
| `MaxTokensNoCtx` | `2048` | Budget for lines format |
| `Intent` | `""` | BM25 query for task-aware ranking (omit for standard behavior) |
| `ConsumedPaths` | `nil` | File paths the caller has already read; these are downranked in ranking |
| `SymbolRefs` | `false` | Enable approximate cross-language symbol reference scoring |
| `Explain` | `false` | Append rank-score explanations to text output |
| `IncludeTests` | `false` | Rank test files at full weight |
| `GoAnalysis` | `false` | Load active Go packages for semantic metadata and relationships |
| `GoAnalysisCalls` | `false` | Build the Go SSA/CHA caller graph |
| `GoAnalysisTests` | `false` | Load Go test variants for semantic relationships |
| `MaxFileSize` | `50000` | Maximum scanned file size; negative disables the cap |

The CLI wires both token-budget fields to the same `-t` value. Call the library directly if you want to set them independently — see [Library Usage](05-library-usage.md).

## Next

- [Library Usage](05-library-usage.md) — use repomap from Go code
- [Caching](07-caching.md) — speed up repeated runs
