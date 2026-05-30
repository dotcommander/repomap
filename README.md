# repomap

Turn a repository into a compact, deterministic code map for coding agents, scripts, and humans.

```bash
repomap --intent "fix token refresh race" -t 4096
```

```text
## Repository Map (138 files, 807 symbols)

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

### Orient a Coding Agent

```bash
repomap --intent "add structured json output" -t 4096
```

Use this as first context. It gives the agent entry points, central packages, public APIs, and the most task-relevant files without dumping source.

### Ask What a File Can Affect

```bash
repomap impact ranker.go
```

```text
ranker.go
  parsed: go_ast
  imports: path/filepath, slices, strings
  imported by: internal/cli/root.go, internal/cli/find.go, ...
  tests: ranker_test.go, ranker_callers_test.go, ranker_consumed_test.go
  exported: RankFiles, RankedFile
  score: 133 map[imports:120 symbols:3 transitive:10]
```

`impact` reports local facts only: imports, reverse imports, nearby tests, exported symbols, boundaries, parser backend, and score components.

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
repomap find RankFiles              # locate symbols
repomap context RankFiles           # source + impact context for one symbol
repomap impact ranker.go            # blast-radius facts for a file
repomap cache status                # inspect disk cache freshness
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
| `--json-structured` | Files, symbols, ranks, parser data, budget data | yes |

Budgeting is all-or-lower-detail: repomap does not cut a file halfway through a symbol. A file renders at its assigned detail level, drops to a summary/header, or is marked omitted.

## Ranking

repomap ranks files before budgeting. Main signals:

| Signal | Effect |
| --- | --- |
| Entry point (`main.go`, `index.ts`, `app.py`, etc.) | strong boost |
| Exported symbols | contracts and public API rise |
| Direct importers | heavily depended-on files rise |
| Transitive fan-in | deep core files rise |
| Boundary imports | HTTP, database, shell, and similar edges rise |
| Tests and deep paths | mild penalty |
| `--intent` | task-relevant files rise |
| `--symbol-refs` | non-Go symbols mentioned by many other files rise |
| `--consumed` | read files fall; their importers rise |
| `--calls` | files with many caller sites rise |

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
| Python, Rust, TypeScript, JavaScript, Java, C, C++, Ruby, HTML, CSS | tree-sitter when available, ctags/regex fallback |

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
