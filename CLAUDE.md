# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test

```bash
go build ./...
go test ./...                          # all tests
go test -run TestParseGoFile ./...     # single test
go test -short ./...                   # skip integration tests
go test -bench=. -benchmem ./...       # benchmarks
go vet ./...
```

No Makefile or Taskfile — standard Go tooling only.

## Architecture

Single Go package (`repomap`) with a Cobra CLI in `internal/cli/`. Entry point: `cmd/repomap/main.go`.

### Pipeline: Scan → Parse → Rank → Budget → Format

1. **ScanFiles** (`scanner.go`) — discovers source files via `git ls-files` (fallback: directory walk). Skips vendor, node_modules, build artifacts, files >50KB. Language detection via `LanguageFor()` in `language.go`.

2. **parseFiles** (`parse_dispatch.go`) — parallel Go + non-Go parsing:
   - **Go**: `ParseGoFile` (`parser_go.go`) uses `go/ast` — always available, highest fidelity
   - **Non-Go**: tiered fallback: **tree-sitter** → **ctags** → **regex**
     - Tree-sitter grammars: `parser_ts_*.go` (C/C++, Java, Python, Rust, TypeScript/JS, Web)
     - Regex fallback: `parser_generic.go`, `parser_cfamily.go`, `parser_web.go`
     - Availability checked at init: `TreeSitterAvailable()`, `CtagsAvailable()`

3. **RankFiles** (`ranker.go`) — scores files by: entry boosts (main.go +50, index.ts +30), exported symbol count (+1 each), depth penalty, import-reference counts (+10 per importer via Go import paths or basename matching)

4. **BudgetFiles** (`budget.go`) — assigns detail levels within token budget: -1 (omit), 0 (header), 1 (summary), 2 (full symbols), 3 (symbols + struct field expansion)

5. **Format** — four output modes:
   - `String()` → compact, budget-trimmed (`render.go`)
   - `StringVerbose()` → all symbols, no budget limit
   - `StringDetail()` → verbose + signatures/fields
   - `StringLines()` → actual source lines (`render_lines.go`)

### Key Types

- `Map` (`repomap.go`) — main orchestrator. Thread-safe (`sync.RWMutex`). Lazy output caching. Stale-checking via mtime polling with 30s debounce.
- `Symbol` (`types.go`) — name, kind, signature, receiver, exported, line
- `FileSymbols` — symbols + imports from one file
- `RankedFile` — FileSymbols + Score, DetailLevel, ImportedBy

### Caching

- `cache.go` — disk cache via `SaveCache`/`LoadCache` (JSON, keyed by SHA-256 of root path). Output strings are lazily computed and cached in `outputCache`.
- `inventory_*.go` — file metrics persistence for the inventory/scan subsystem.

### Language Support

Defined in `language.go` as a single `languageDefs` slice. To add a language: add an entry there, then optionally add a tree-sitter registration in a new `parser_ts_<lang>.go`. The regex parsers in `parser_generic.go` handle remaining languages by pattern.

### Configuration

Optional `.repomap.yaml` at project root filters symbols at parse time:
- `method_blocklist`: list of glob (`Test*`) or regex (`/^pb_/`) patterns
- Loader in `config.go`; filter applied in `parse_dispatch.go` and `commit_analyze.go`
- Absent file = no-op

## Testing Patterns

- `testify` (assert/require), `t.Parallel()` on all tests
- Integration tests create temp dirs with git repos and real source files
- `testing.Short()` gates integration tests (`TestBuildIntegration`)
- Benchmarks: `BenchmarkBuild`, `BenchmarkStale` — use `findBenchRoot` to locate repo root

## CLI

```
repomap [directory]                    # default: compact, 2048 tokens
repomap -t 4096 -f verbose ./src      # more tokens, verbose
repomap -f lines ./src                # source-line format
repomap --json                         # JSON array of lines
```

Flags: `-t/--tokens`, `-f/--format` (compact|verbose|detail|lines), `--json`
