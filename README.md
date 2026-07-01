# repomap

Turn a repository into a compact, deterministic code map for coding agents, scripts, and humans.

```bash
repomap --intent "fix token refresh race" -t 4096
```

```text
## Repository Map (138 files, 807 symbols)

### Flow
entry: cmd/repomap/main.go
spine: repomap.go, types.go, ranker.go, budget.go, render.go

### Dependencies
repomap/cmd/repomap -> repomap/internal/cli
repomap/internal/cli -> repomap, repomap/internal/lsp

repomap.go [imported by 12]
  type Config{MaxTokens int, MaxTokensNoCtx int, Intent string, ConsumedPaths []string}
    // holds repomap configuration
  type Map
    // holds the built repository map state
  func New(root string, cfg Config) *Map
  func (*Map) Build(ctx context.Context) error
```

`repomap` is local static analysis: `git ls-files`, Go `go/ast`, tree-sitter, ctags/regex fallback, import graphs, BM25 intent ranking, and optional `gopls` caller expansion. It does not call an LLM.

## Install

```bash
go install github.com/dotcommander/repomap/cmd/repomap@latest
```

Or build from a checkout:

```bash
git clone https://github.com/dotcommander/repomap
cd repomap
go build -o repomap ./cmd/repomap
```

## Quick Start

```bash
repomap
```

Scans the current repository, ranks important files first, and renders exported symbols, signatures, first-sentence docs, and struct/interface fields within the default token budget.

```bash
repomap ./internal/cli -t 6000
```

Map a subtree with a larger budget.

```bash
repomap --intent "debug caller expansion timeouts"
```

Bias ranking toward files whose paths, packages, exported symbols, imports, and signatures match the task.

```bash
repomap --symbol-refs
```

Add a cheap cross-language lexical reference signal for non-Go symbols when imports are too weak and LSP caller data is unavailable.

```bash
repomap --intent "debug caller expansion timeouts" --consumed calls.go,internal/lsp/client.go
```

Downrank files you already read and uprank files that import them.

## Workflow Examples

### Boot an Agent with `brief`

```bash
repomap brief
```

One call answers everything an agent needs at session start: a time-aware greeting, module identity, the project's verify chain (`build`/`test`/`vet`, plus `lint` only when a golangci config exists), current git state (branch, changed files, recent commits), any agent-convention rules it should read first (`CLAUDE.md`, `AGENTS.md`, `.cursorrules`), and the enriched repo map capped to the top-ranked files.

For multi-package repos the digest ends with a **Likely ownership** routing section that clusters the top files by owning directory (e.g. `internal/cli/ — cli (38 files: Execute, Run, Write)`), so the agent knows which packages own the surface before opening anything. It is omitted entirely for flat or single-area repos so it never adds noise.

```bash
repomap brief ./other-repo     # defaults to the current directory
```

### Orient a Coding Agent

```bash
repomap --intent "add structured json output" -t 4096
```

Use this as first context. It gives the agent entry points, central packages, public APIs, and the most task-relevant files without dumping source.

### Ask What a File Can Affect

```bash
repomap impact ranker.go
repomap impact ranker.go --markdown
```

```text
ranker.go
  parsed: go_ast
  imports: path/filepath, slices, strings
  imported by: internal/cli/root.go, internal/cli/find.go, ...
  tests: ranker_test.go, ranker_callers_test.go, ranker_consumed_test.go
  exported: RankFiles, RankedFile
  score: 133 map[imports:120 symbols:3 transitive:10]
  risk: medium
  check next: inspect importer internal/cli/root.go; run or inspect likely test ranker_test.go
  likely test commands: go test .
  read next:
    - ranker.go:49-92 inspect exported symbol RankFiles
```

Use `--markdown` for a compact human handoff and `--json` for tooling.
`impact` reports local facts plus deterministic workflow guidance: imports,
reverse imports, nearby tests, exported symbols, boundaries, parser backend,
score components, risk level, next files to inspect, likely Go test commands,
and bounded `read_next` source ranges.

### Get Context for One Symbol

```bash
repomap context RankFiles
```

```text
ranker.go:49  function  RankFiles(files []*FileSymbols) []RankedFile
also matched:
  repomap_test.go:200  function  TestRankFiles(t *testing.T)

source:
  49 | func RankFiles(files []*FileSymbols) []RankedFile {
  50 |     ranked := make([]RankedFile, len(files))
     ...
ranker.go
  parsed: go_ast
  imports: path/filepath, slices, strings
  tests: ranker_test.go, ranker_callers_test.go
```

`context` is a symbol-centered bundle: best match, bounded source span, ambiguity hints, and the owning file's impact facts. Use `--json` for structured output, or `--calls` to include exact Go callers through `gopls`.

### Explain a Ranking Decision

```bash
repomap explain ranker.go
```

```text
ranker.go
  score: 133
  detail: omitted (budget)
  components:
    imports: +120
    symbols: +3
    transitive: +10
```

Use `explain` when a match looks suspicious. Every score component is deterministic and auditable.

### Feed a Tool Structured Data

```bash
repomap --json-structured -t 4096 > map.json
```

```json
{
  "schema_version": 1,
  "files": [
    {
      "path": "ranker.go",
      "language": "go",
      "parse_method": "go_ast",
      "score": 133,
      "score_components": {
        "imports": 120,
        "symbols": 3,
        "transitive": 10
      },
      "detail_level": -1,
      "omitted_reason": "budget",
      "symbols": [
        {
          "name": "RankFiles",
          "kind": "function",
          "line": 48
        }
      ]
    }
  ]
}
```

Files excluded by the budget remain present with `detail_level: -1` and `omitted_reason`.

### Expand Callers with gopls

```bash
repomap --calls --calls-threshold 2 --calls-limit 8
```

`--calls` asks `gopls` for references to exported symbols in highly imported files, then boosts files with many caller sites. Caller data is cached under `~/.cache/repomap` unless `--no-cache` is set.

### Inspect Cache State

```bash
repomap cache status
repomap cache status --json
```

`cache status` reports whether the disk cache for the current root exists, is usable, and appears fresh. It checks the saved cache version, root, tracked file hashes/mtimes, and saved HEAD when present.

### Seed a Deep Audit

```bash
repomap audit brief --json --limit 20
repomap audit hygiene --json
repomap audit risks --json --limit 20
repomap audit surface --json --limit 20
repomap audit effects --json --limit 20
```

`audit brief` builds the map once and emits risks, surface, effects, a
grouped first-read queue, and a `review_plan` for workflow tools. First-read
groups include bounded `read_next` ranges when the static evidence has line
numbers. The
`review_plan` projects the first-read queue into per-lane review obligations —
each lane lists the files to cover, the gates to discharge, suggested verify
commands (Go-specific commands appear only when Go sources are detected), and
why the lane matters — so deep-audit tools get coverage targets without
inventing findings. Use the narrower commands when you only need one packet.
`audit hygiene` reports tracked, untracked, and ignored source-file leads so
release audits can catch local-only code. It suppresses dependency/archive noise
from paths such as `node_modules/`, `vendor/`, `.work/archive/`, and `archive/`,
while retaining suppressed counts in JSON. `audit risks` converts rank, boundary,
and symbol-size facts into lane packets for tools such as repo-audit-deep.
`audit surface` extracts commands, flags, env vars, config keys, JSON schema
fields, routes, and output paths. `audit effects` extracts side-effect
boundaries such as filesystem writes, subprocesses, HTTP, DB calls,
serialization, secrets, crypto, time, and randomness. These are deterministic
leads, not final findings.

Every audit packet is self-describing (`schema_version` 2): each carries a stable
`id` (e.g. `repomap:risk:internal-cli-audit-go`) for citation, an `evidence_class`
(`import_graph`, `ast`, `git_history`, or `heuristic`) with a derived `confidence`
tier, and a per-file `verify_cmd` for Go targets. Signals blind to out-of-repo
callers — dead code, untested exports — carry a `caveat` and are capped at `low`
confidence. Empty file lists serialize as `[]` (never `null`) with a
`files_omitted_reason`, and truncated packets report an `omitted_reason`.

## Commands

```bash
repomap [directory]                 # default enriched map
repomap -t 4096                     # token budget
repomap -f compact                  # path + exported symbol names
repomap -f verbose                  # all symbols, no budget cap
repomap -f detail                   # all symbols with signatures and fields
repomap -f lines                    # declaration source lines
repomap -f xml                      # structured XML
repomap --json                      # JSON envelope with rendered lines
repomap --json --json-legacy        # legacy bare []string JSON
repomap --json-structured           # schema-versioned map data
repomap --artifact out.md           # save long output without shell redirection
repomap brief [directory]           # agent boot digest: identity + verify + state + map
repomap find RankFiles              # locate symbols
repomap context RankFiles           # source + impact context for one symbol
repomap impact ranker.go            # blast-radius facts for a file
repomap impact ranker.go --markdown # compact human handoff
repomap inventory --boundary Postgres # ownership answer for DB work
repomap audit brief                 # single-pass audit packets + first-read queue
repomap audit hygiene               # tracked/untracked/ignored source leads
repomap audit risks                 # lane-oriented audit risk packets
repomap audit surface               # command/flag/config/schema/API/output surfaces
repomap audit effects               # side-effect and trust-boundary packets
repomap audit effects --kind database --paths-only # DB boundary paths
repomap cache status                # inspect disk cache freshness
repomap lsp status                  # inspect LSP server coverage without starting servers
repomap explain ranker.go           # ranking and budget evidence
repomap init                        # scaffold .repomap.yaml and post-commit cache hook
```

LSP commands are also available when `gopls` is installed:

```bash
repomap symbols ranker.go
repomap def ranker.go 48 RankFiles
repomap refs ranker.go 48 RankFiles
repomap hover ranker.go 48 RankFiles
```

## Output Formats

| Format | What it shows | Budget enforced |
| --- | --- | --- |
| default | Exported symbols, signatures, docs, struct/interface fields | yes |
| `compact` | File paths and exported symbol names | yes |
| `verbose` | All symbols, no summarization | no |
| `detail` | All symbols plus signatures and fields | no |
| `lines` | Actual declaration lines | yes |
| `xml` | Structured XML | yes |
| `--json` | Rendered verbose lines in JSON | no |
| `--json-structured` | Files, symbols, call sites, ranks, parser data, budget data | yes |

Budgeting is all-or-lower-detail: repomap does not cut a file halfway through a symbol. A file renders at its assigned detail level, drops to a summary/header, or is marked omitted.

## Ranking

repomap ranks files before budgeting. Main signals:

| Signal | Effect |
| --- | --- |
| Entry point (`main.go`, `index.ts`, `app.py`, etc.) | strong boost |
| Exported symbols | contracts and public API rise |
| Direct importers | heavily depended-on files rise |
| Transitive fan-in | deep core files rise |
| Structural call sites | non-Go files called by other scanned files rise |
| Boundary imports | HTTP, database, shell, and similar edges rise |
| Deep paths | mild penalty |
| Tests (`_test.go`) | demoted by default; `--include-tests` ranks them at full weight |
| `--intent` | task-relevant files rise |
| `--symbol-refs` | non-Go symbols mentioned by many other files rise |
| `--consumed` | read files fall; their importers rise |
| `--calls` | files with many caller sites rise |

For database work, a compact flow is:

```bash
repomap --intent "PostgreSQL database psql pgx migrations schema queries" --explain
repomap inventory --boundary Postgres --json
repomap audit effects --kind database --paths-only
repomap impact internal/database/connection.go --markdown
```

Check exact evidence with:

```bash
repomap explain path/to/file.go --json
```

## Languages

Supported file types:

| Language | Parser path |
| --- | --- |
| Go | `go/ast` |
| PHP | tree-sitter with signatures, visibility, constructor promotion, PHPDoc |
| TypeScript, TSX, JavaScript, JSX, Python, Rust, C, C++, Java, Ruby | tree-sitter when available, ctags/regex fallback |
| Lua, Zig, Swift, Kotlin | extension-only: ctags/regex fallback |

Structured output includes `parse_method`: `go_ast`, `tree_sitter`, `ctags`, or `regex`.

## Configuration

Create `.repomap.yaml` at the repo root:

```yaml
method_blocklist:
  - "Test*"
  - "*Mock"
  - "/^pb_/"

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
| `method_blocklist` | Drop matching symbols at parse time. Supports globs and `/regex/`. |
| `include_paths` | If set, only matching paths are scanned. |
| `exclude_paths` | Always excluded; wins over includes. |
| `file_overrides` | Force matched files to `"full"` or `"omit"` detail. |

Scaffold a config and cache-warming hook:

```bash
repomap init
repomap init --no-hook
repomap init --force
```

## Library Usage

```go
package main

import (
	"context"
	"fmt"

	"github.com/dotcommander/repomap"
)

func main() {
	m := repomap.New(".", repomap.Config{
		MaxTokens: 4096,
		Intent:    "debug caller expansion",
	})
	if err := m.Build(context.Background()); err != nil {
		panic(err)
	}

	fmt.Print(m.String())
}
```

Useful methods:

```go
m.String()              // enriched default
m.StringCompact()       // lean orientation
m.StringVerbose()       // all symbols
m.StringDetail()        // all signatures and fields
m.StringLines()         // declaration lines
m.StringXML()           // XML
m.StructuredOutput()    // structured Go value
m.StructuredJSON()      // indented JSON bytes
m.Impact("ranker.go")   // file blast-radius facts
m.Explain("ranker.go")  // rank and budget evidence
m.Stale()               // source changed since build
```

## Design

repomap is intentionally boring:

- local only
- deterministic
- small Go API
- no LLM calls
- no embeddings
- no hidden network dependency
- graceful parser fallback

Pipeline:

```text
scan -> parse -> rank -> budget -> format
```

Docs live in `docs/`. Start with [docs/02-quick-start.md](docs/02-quick-start.md), then [docs/03-output-formats.md](docs/03-output-formats.md), [docs/06-ranking.md](docs/06-ranking.md), and [docs/08-languages.md](docs/08-languages.md). For a task-by-task tour from cold start to commit, see [docs/11-usage-examples.md](docs/11-usage-examples.md).

## Acknowledgments

The repository map concept was pioneered by [aider.chat](https://aider.chat/), which popularized compact codebase maps for LLM-assisted development.

## License

[MIT](LICENSE)
