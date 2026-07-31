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

`repomap` is local static analysis: `git ls-files`, fast Go `go/ast` mapping with on-demand `go/packages`/`go/types` semantics, tree-sitter, ctags/regex fallback, import graphs, and BM25 intent ranking. It does not call an LLM.

## Why LLMs Should Use Repomap

Outputs below are abridged; paths, counts, scores, and timestamps vary by repository.

1. **Spend context on the code that matters.** Repomap ranks files and fits their most useful symbols, signatures, and documentation into a bounded token budget.

   ```bash
   repomap -t 256 .
   ```

   ```text
   ## Repository Map · enriched (244 files, 1189 symbols, ~248 tokens)

   ### Flow
   entry: cmd/repomap/main.go
   spine: repomap.go, types.go, calls.go, internal/commit/commit_prep_helpers.go, audit_packets.go

   repomap.go [imported by 33]
     3 types, 2 funcs, 16 methods, 1 vars · Config, GoDiagnostic
   ...
   ```

2. **Orient before reading broadly.** `repomap brief` combines repository identity, agent instructions, verification commands, Git state, likely ownership, and the ranked map.

   ```bash
   repomap brief .
   ```

   ```text
   Good evening, agent — here's your briefing.

   # repomap — Go module
     module github.com/dotcommander/repomap

   ## Verify
     build: go build ./...
     test:  go test ./...
     vet:   go vet ./...

   ## State
     branch: main   dirty: ...

   ## Rules
     conventions: CLAUDE.md — read before editing

   ## Map
   ## Repository Map · enriched (...)
   ```

3. **Focus the map on the current task.** `--intent` reranks paths, packages, imports, symbols, and signatures against the work the LLM is about to perform.

   ```bash
   repomap --intent "harden CLI output" -t 256 .
   ```

   ```text
   ## Repository Map · enriched (...)

   ### Flow
   entry: cmd/repomap/main.go
   spine: repomap.go, structured_json.go, audit_packets.go,
          internal/commit/commit_execute.go, commit_analyze.go
   ...
   ```

4. **Build an implementation packet.** `task` composes task-relevant owners,
   symbols, source, consumers, callers, tests, effects, rules, dirty overlap,
   and verification commands within one complete-output budget.

   ```bash
   repomap task "harden CLI output" -t 4096 .
   ```

   Abridged output:

   ```text
   # Task: harden CLI output

   Root: /checkout
   Budget: 1638/4096 tokens

   ## Targets
   - internal/cli/render.go confidence=high package=cli risk=medium parse=go_ast
     evidence path: internal/cli/render.go
     symbol function renderStandard lines=43-61 (...) error
     relationship consumer internal/cli/root.go (syntactic)
     relationship test internal/cli/root_test.go (exact)
     source renderStandard:
       43: func renderStandard(...) error {
       ...

   ## Verify
   - go test ./internal/cli
   ```

   The same report drives schema-versioned JSON. Selected fields:

   ```bash
   repomap task "harden CLI output" --json .
   ```

   ```json
   {
     "schema_version": 1,
     "root": "/checkout",
     "goal": "harden CLI output",
     "budget": {"max_tokens": 4096, "used_tokens": 1638},
     "selection": {"strategy": "positive task relevance, structural score, path", "limit": 6, "selected": 1},
     "rules": [{"path": "AGENTS.md"}],
     "related_changes": [],
     "targets": [{"path": "internal/cli/render.go", "confidence": "high"}],
     "read_next": [],
     "verify_commands": ["go test ./internal/cli"],
     "follow_up_commands": [],
     "diagnostics": [],
     "truncations": []
   }
   ```

   Confidence is deterministic selection strength, not a correctness claim.
   Relationship provenance is `exact` for semantic evidence, `syntactic` for
   parsed source relationships, and `heuristic` for naming or adjacency.
   Generated follow-ups suggest an exact larger-budget rerun; fixed field caps
   may still require direct inspection.
   Pass `--consumed=PATHS` to retain known owners and relationships while
   spending source budget on unread files.

5. **Avoid rereading known files.** `--consumed` downranks files already in context and raises their importers, helping the next map add new information.

   ```bash
   repomap --consumed audit_packets.go -t 256 .
   ```

   ```text
   ## Repository Map · enriched (...)

   ### Flow
   entry: cmd/repomap/main.go
   spine: repomap.go, types.go, calls.go, internal/commit/commit_prep_helpers.go,
          structured_json.go
   ...
   ```

6. **Retrieve a coherent symbol packet.** `repomap context` returns the best symbol match, bounded source, ambiguity hints, caller context, and owning-file impact facts.

   ```bash
   repomap context AuditBrief --max-source-lines 8 .
   ```

   ```text
   audit_brief.go:57  method  (*Map) AuditBrief(ctx context.Context, limit int) (AuditBriefReport, error)
   also matched:
     audit_brief.go:13  struct  AuditBriefReport{...}

   source:
     57 | func (m *Map) AuditBrief(ctx context.Context, limit int) (...) {
     58 |     risks := m.AuditRisks(limit)
        ...
   audit_brief.go
     parsed: go_ast
     risk: high
     affected packages: cli, repomap
     check next: inspect importer internal/cli/audit.go
   ```

6. **Estimate change risk before editing.** `repomap impact` reports imports, reverse imports, tests, exported symbols, boundaries, risk, likely test commands, and what to inspect next.

   ```bash
   repomap impact audit_packets.go --markdown
   ```

   ```text
   # Impact: `audit_packets.go`

   - **Risk:** high
   - **Parsed:** go_ast
   - **Score:** 166

   ## Affected Packages
   - `cli`
   - `repomap`

   ## Imported By
   - `internal/cli/audit.go`
   - `internal/cli/brief.go`
   ...
   ```

7. **Trace code with semantic evidence.** Go caller expansion uses a type-checked SSA graph, while `refs`, `def`, `hover`, and `symbols` expose installed language-server results.

   ```bash
   repomap context AuditBrief --calls --calls-limit 2 --max-source-lines 4 .
   ```

   ```text
   audit_brief.go:57  method  (*Map) AuditBrief(...)

   source:
     57 | func (m *Map) AuditBrief(...) (...) {
     58 |     risks := m.AuditRisks(limit)
        ...

   callers:
     internal/cli/audit.go:96:0
   ```

8. **Start audits from deterministic leads.** Audit packets identify risks, public surfaces, side effects, trust boundaries, first-read queues, evidence quality, and every truncation.

   ```bash
   repomap audit brief --json --limit 1 . |
     jq '{schema_version, risk_files: (.risks.files|length),
          surface_files: (.surface.files|length),
          effect_files: (.effects.files|length),
          first_read_groups: (.first_read_queue|length)}'
   ```

   ```json
   {
     "schema_version": 3,
     "risk_files": 1,
     "surface_files": 1,
     "effect_files": 1,
     "first_read_groups": 11
   }
   ```

9. **Integrate without scraping prose.** Schema-versioned JSON, XML, line output, artifacts, and JSON-RPC let agents consume stable machine-readable results.

   ```bash
   repomap --json-structured -t 64 . |
     jq '{schema_version, files: [.files[0] |
          {path, score, detail_level, omitted_reason}]}'
   ```

   ```json
   {
     "schema_version": 1,
     "files": [
       {
         "path": "repomap.go",
         "score": 195,
         "detail_level": 0,
         "omitted_reason": null
       }
     ]
   }
   ```

10. **Keep repository analysis local and repeatable.** Repomap sends no code to an LLM, and disk caching plus `repomap serve` make repeated queries cheaper without changing the evidence source.

    ```bash
    repomap cache warm . --cache-dir /tmp/repomap-cache
    ```

    ```text
    cache: fresh
      path: /tmp/repomap-cache/repomap-<root-hash>.json
      reason: fresh
      built: <timestamp>
      tracked files: 244
      saved HEAD: 28cce277...
      current HEAD: 28cce277...
    ```

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
repomap task "debug caller expansion timeouts" .
```

Build a bounded implementation-decision packet instead of chaining map,
impact, context, and audit-effect queries manually.

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

`context` is a symbol-centered bundle: best match, bounded source span, ambiguity hints, and the owning file's impact facts. Use `--json` for structured output, or `--calls` to include exact callers from the in-process Go semantic graph.

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
  "totals": {"files": 244, "symbols": 1189},
  "files_omitted": 231,
  "files_omitted_reason": "complete-output token budget",
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
      "detail_level": 2,
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

`totals` describes the complete repository. The CLI emits the largest
whole-file prefix that fits, then records the remainder in `files_omitted` and
`files_omitted_reason`; it never cuts a JSON record.

### Expand Go Callers

```bash
repomap --calls --calls-threshold 2 --calls-limit 8
```

`--calls` builds one type-checked, whole-program Go call graph with `go/packages`, SSA, and Class Hierarchy Analysis, then selects exported symbols in files meeting `--calls-threshold`. Receiver-qualified identities keep same-named methods distinct. Add `--calls-include-tests` to load test variants and include test callers.

`--precise` remains as a deprecated compatibility flag and includes callers regardless of `--calls-threshold`. `--no-cache` is also accepted for compatibility but has no effect because callers are built with the map instead of a separate caller cache. The standalone `refs`, `def`, `hover`, `symbols`, and `lsp status` commands still use installed language servers.

### Inspect Cache State

```bash
repomap cache status
repomap cache status --json
repomap cache warm .
repomap cache warm . --cache-dir /tmp/repomap-cache
```

`cache status` reports whether the disk cache for the current root exists, is usable, and appears fresh. It checks the saved cache version, root, tracked file hashes/mtimes, and saved HEAD when present.
`cache warm` builds the map, saves it, and prints the same fresh status only after the saved entry is usable and fresh.

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

Risk packets remain at `schema_version` 2. Surface, effects, and brief packets
use `schema_version` 3 and add structured `truncations` entries (`field`,
`shown`, `total`, `reason`) so every cap is accounted for. Each packet carries a stable
`id` (e.g. `repomap:risk:internal-cli-audit-go`) for citation, an `evidence_class`
(`import_graph`, `ast`, `git_history`, or `heuristic`) with a derived `confidence`
tier, and a per-file `verify_cmd` for Go targets. Signals blind to out-of-repo
callers — dead code, untested exports — carry a `caveat` and are capped at `low`
confidence. Empty file lists serialize as `[]` (never `null`) with a
`files_omitted_reason`, and truncated per-file packets report an `omitted_reason`.

## Commands

```bash
repomap [directory]                 # default enriched map
repomap -t 4096                     # token budget
repomap -f compact                  # path + exported symbol names
repomap -f verbose                  # all symbols, complete-output budget
repomap -f detail                   # all symbols with signatures and fields
repomap -f lines                    # declaration source lines
repomap -f xml                      # structured XML
repomap --json                      # JSON envelope with rendered lines
repomap --json --json-legacy        # legacy bare []string JSON
repomap --json-structured           # schema-versioned map data
repomap --artifact out.md           # save long output without shell redirection
repomap task "add task packets" .   # owners + source + contracts + impact + verify
repomap task "add task packets" --json --consumed=task.go .
repomap brief [directory]           # agent boot digest: identity + verify + state + map
repomap find RankFiles              # locate symbols
repomap context RankFiles           # source + impact context for one symbol
repomap impact ranker.go            # blast-radius facts for a file
repomap impact ranker.go --markdown # compact human handoff
repomap endpoint "GET /users/{id}"  # route -> handler -> callees -> tests
repomap inventory --boundary Postgres # ownership answer for DB work
repomap audit brief                 # single-pass audit packets + first-read queue
repomap audit hygiene               # tracked/untracked/ignored source leads
repomap audit risks                 # lane-oriented audit risk packets
repomap audit surface               # command/flag/config/schema/API/output surfaces
repomap audit effects               # side-effect and trust-boundary packets
repomap audit effects --kind database --paths-only # DB boundary paths
repomap cache status                # inspect disk cache freshness
repomap cache warm .                # build and save a fresh disk cache
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

### Complete CLI flag index

There are 30 executable leaves: the default map; `brief`; `task`; `audit hygiene`,
`audit brief`, `audit risks`, `audit surface`, `audit effects`; `cache status`,
`cache warm`; `find`, `impact`, `inventory`, `context`, `endpoint`, `explain`,
`orphans`, `init`; `lsp status`, `refs`, `def`, `hover`, `symbols`; `serve`;
`commit analyze`, `commit execute`, `commit prep`, `commit finish`,
`commit auto`; and `commit-preflight`.

All leaves accept `--artifact` and `--help`. Root map flags are `--tokens=2048`
(>0), `--format=enriched` (`enriched|compact|verbose|detail|lines|xml`),
`--json`, `--json-legacy`, `--json-structured`, `--calls`, `--precise`,
`--calls-threshold=2` (>=0), `--calls-limit=10` (0 = unlimited),
`--calls-include-tests`, `--no-cache`, `--intent`, `--consumed`,
`--symbol-refs`, `--explain`, and `--include-tests`.

Subcommand flags are: task `--tokens, -t=4096`, `--json`, `--consumed`; audit
`--limit=20`, `--top-files=0`, `--intent`, `--json`,
plus effects `--kind` and `--paths-only`; cache `--cache-dir` and status
`--json`; find `--kind`, `--file`, `--limit=20`, `--format=text`; impact
`--json`, `--markdown`; inventory `--boundary`, `--json`; context `--kind`,
`--file`, `--max-source-lines=200`, `--max-output-lines=400`,
`--max-output-bytes=65536`, `--json`, `--calls`, `--precise`,
`--calls-include-tests`, `--calls-limit=10`; endpoint `--json`,
`--max-output-lines=400`; explain/orphans/LSP query commands `--json`; init
`--force`, `--no-hook`, `--no-config`; commit analyze `--tag`, `--pretty`,
`--tmpdir`, `--confidence=0.75`; execute `--plan-file`, `--push`, `--tag`,
`--no-release`, `--release-notes-from`, `--dry-run`, `--json`, `--skip-fix`;
prep `--json`, `--no-review`, `--tag`, `--allow-large`; finish
`--prep-token`, `--decisions`, `--push`, `--tag`, `--json=true`; auto
`--no-review`, `--allow-large`, `--tag`, `--decisions`.

Zero means unlimited only for documented result/caller/audit/output limits;
tokens and context source lines must be positive. `--json-legacy` requires
`--json`; structured JSON is mutually exclusive with both line-oriented JSON
modes; `--top-files` overrides `--limit`; impact JSON and Markdown conflict.
See [Configuration](docs/04-configuration.md) for accepted audit kinds,
required flags, aliases, hidden compatibility behavior, and full precedence.

## Output Formats

| Format | What it shows | Budget enforced |
| --- | --- | --- |
| default | Exported symbols, signatures, docs, struct/interface fields | yes |
| `compact` | File paths and exported symbol names | yes |
| `verbose` | All symbols, no summarization | yes |
| `detail` | All symbols plus signatures and fields | yes |
| `lines` | Actual declaration lines | yes |
| `xml` | Structured XML | yes |
| `--json` | Rendered verbose lines in JSON | yes |
| `--json-structured` | Files, symbols, call sites, ranks, parser data, budget data | yes |

Every bounded format counts the complete encoded stdout using
`ceil(UTF-8 bytes / 4)`, including headers, dependency flow, explanations, and
JSON or XML envelopes. Repomap never byte-cuts structured output. Map formats
select the largest whole-file prefix that fits; task packets additionally
record field-level omissions in `truncations`. If the minimum valid envelope
cannot fit, the command fails before writing stdout or replacing an artifact.

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
| Go | `go/packages` + `go/types` for active packages; `go/ast` syntax fallback |
| PHP | tree-sitter with signatures, visibility, constructor promotion, PHPDoc |
| TypeScript, TSX, JavaScript, JSX, Python, Rust, C, C++, Java, Ruby | tree-sitter when available, ctags/regex fallback |
| Lua, Zig, Swift, Kotlin | extension-only: ctags/regex fallback |

Structured output includes `parse_method`: `go_ast`, `tree_sitter`, `ctags`, or `regex`. Go files also report `build_active` and `analysis_mode`.

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

The installed post-commit hook runs `repomap cache warm .` in the background.

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
m.Task(ctx, "goal", repomap.TaskOptions{}) // bounded implementation evidence
m.Impact("ranker.go")   // file blast-radius facts
m.Explain("ranker.go")  // rank and budget evidence
m.Stale()               // source changed since build
```

## Design

repomap is intentionally boring:

- local only
- deterministic
- public analysis API
- no LLM calls
- no embeddings
- no hidden network dependency
- graceful parser fallback

Pipeline:

```text
scan -> parse -> rank -> budget -> format
```

Docs live in `docs/`. Start with [docs/02-quick-start.md](docs/02-quick-start.md), then [docs/03-output-formats.md](docs/03-output-formats.md), [docs/06-ranking.md](docs/06-ranking.md), and [docs/08-languages.md](docs/08-languages.md). For a task-by-task tour from cold start to commit, see [docs/11-usage-examples.md](docs/11-usage-examples.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, formatting, tests, and local
verification.

## Acknowledgments

The repository map concept was pioneered by [aider.chat](https://aider.chat/), which popularized compact codebase maps for LLM-assisted development.

## License

[MIT](LICENSE)
