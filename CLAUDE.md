# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test

```bash
just check                              # build, test, vet, lint, module verification
go test -run TestParseGoFile ./...      # focused test
go test -short ./...                    # skip integration tests
go test -bench=. -benchmem ./...        # benchmarks
```

Run `just --list` for the repository-owned build recipes. Standard Go commands
remain valid for focused checks.

## Architecture

The public `repomap` package owns repository analysis. The Kong CLI lives in
`internal/cli/`; its entry point is `cmd/repomap/main.go`.

### Pipeline: Scan → Parse → Rank → Detail Selection → Format

1. **ScanFiles** (`scanner.go`) — discovers source files via `git ls-files`
   (fallback: directory walk). Skips vendor, node_modules, build artifacts,
   files over 50 KB, and binary files. Language detection uses
   `LanguageFor()` in `language.go`.

2. **parseFiles** (`parse_dispatch.go`) — parallel Go + non-Go parsing:
   - **Go**: `ParseGoFile` (`parser_go.go`) always uses `go/ast`; commands that
     need semantic relationships additionally use `internal/goanalysis`
   - **Non-Go**: tiered fallback: **tree-sitter** → **ctags** → **regex**
     - Tree-sitter grammars: `parser_ts_*.go` (C/C++, Java, Python, Rust, TypeScript/JS, Web)
     - Regex fallback: `parser_generic.go`, `parser_cfamily.go`, `parser_web.go`
     - Availability is reported by `TreeSitterAvailable()` and
       `CtagsAvailable()`

3. **RankFiles** (`ranker.go`) — combines structural score, dependency
   relationships, and optional task-intent evidence without changing the
   structural ranker's global ordering contract.

4. **BudgetFiles** (`budget.go`) — assigns detail levels within the selection
   estimate: -1 (omit), 0 (header), 1 (summary), 2 (full symbols), 3 (symbols +
   struct field expansion). `internal/cli/output_budget.go` then enforces the
   public token limit against the complete encoded response.

5. **Format** — enriched Markdown is the default. Compact, verbose, detail,
   source lines, XML, JSON line envelopes, and structured JSON are also
   available. Bounded CLI output is encoded atomically; structured records are
   never byte-cut.

`Map.Task` (`task.go`, `task_select.go`, `task_render.go`) composes a separate
bounded implementation handoff from goal evidence, source excerpts,
relationships, repository rules, related changes, verification commands, and
explicit truncation accounting.

### Key Types

- `Map` (`repomap.go`) — main orchestrator. Thread-safe (`sync.RWMutex`). Lazy output caching. Stale-checking via mtime polling with 30s debounce.
- `Symbol` (`types.go`) — name, kind, signature, receiver, exported, span, and
  documentation
- `FileSymbols` — symbols + imports from one file
- `RankedFile` — FileSymbols + Score, DetailLevel, ImportedBy
- `TaskReport` (`task.go`) — schema-versioned task evidence packet shared by
  human and JSON rendering

### Intent Ranking

Optional BM25 re-ranking before detail selection is activated by `--intent`.
Field-level evidence covers paths, packages, symbols, signatures,
documentation, and imports. The `task` command uses the same evidence but
selects at most six primary targets by positive relevance first, structural
score second, and path last; structural fallbacks are labeled when the goal has
no positive match.

### Caching

- `cache.go` — disk cache via `SaveCache`/`LoadCache` (JSON, keyed by SHA-256 of
  the absolute root path). Version 15 includes content hashes, saved Git HEAD,
  and a deterministic cache-relevant worktree digest so dirty-worktree reuse is
  safe.
- `outputCache` — lazy in-memory render cache owned by `Map`.
- `cache status` inspects an entry without rebuilding; `cache warm` builds,
  saves, and verifies a fresh entry.

### Language Support

Defined in `language.go` as a single `languageDefs` slice. To add a language: add an entry there, then optionally add a tree-sitter registration in a new `parser_ts_<lang>.go`. The regex parsers in `parser_generic.go` handle remaining languages by pattern.

### Configuration

Optional `.repomap.yaml` at project root. Loader in `config.go`; filters applied in `parse_dispatch.go`, `scanner.go`, and `commit_analyze.go`. Absent file = no-op.

| Field | Type | Purpose |
|---|---|---|
| `method_blocklist` | `[]string` | Glob (`Test*`) or regex (`/^pb_/`) patterns — drops matching symbol names at parse time |
| `include_paths` | `[]string` | Glob patterns (relative to root). When non-empty, only matching files are scanned |
| `exclude_paths` | `[]string` | Glob patterns. Matching files are always excluded; takes precedence over `include_paths` |
| `file_overrides` | `map[string]string` | Glob → detail level (`"full"` or `"omit"`). Forces a file to that level regardless of rank |

`file_overrides` uses `path.Match` globs; patterns containing `**` match any path with the corresponding prefix.

## Testing Patterns

- Tests use the standard library plus `testify` assertions.
- Integration tests create temporary Git repositories and real source files.
- `testing.Short()` gates expensive integration coverage.
- `testdata/task/` contains twelve deterministic Go, TypeScript, and PHP task
  manifests.

## CLI

```
repomap [directory]                    # default: enriched, 2048 tokens
repomap -t 4096 -f verbose ./src       # bounded verbose map
repomap -f lines ./src                 # bounded source-line format
repomap --json-structured ./src        # structured repository schema
repomap task "fix auth middleware" .   # bounded implementation handoff
```

For the current flags, commands, task-report schema, confidence/provenance
meanings, and worked examples, see
[docs/11-usage-examples.md](docs/11-usage-examples.md) and
[docs/03-output-formats.md](docs/03-output-formats.md).
