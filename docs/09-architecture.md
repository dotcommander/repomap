# Architecture

Five stages. One package. No magic.

```
scan → parse → rank → budget → format
```

## Stage 1: Scan

`scanner.go` → `ScanFiles(ctx, root) ([]FileInfo, error)`

1. Run `git ls-files` for the root. If that fails (not a git repo), walk the directory tree with `filepath.WalkDir`.
2. For each file, detect language by extension using `languageDefs` from `language.go`.
3. Skip the exclude list: vendor, node_modules, .git, dist, build, target, and anything over 50 KB.
4. Return a `[]FileInfo` with path and language.

No parsing here. No I/O beyond `stat`. This stage is always fast.

## Stage 2: Parse

`parse_dispatch.go` → `parseFiles(ctx, files) ([]*FileSymbols, map[string]time.Time, error)`

Two parallel groups via `errgroup.WithContext`:

1. **Go group** — every Go file runs through `ParseGoFile` (`parser_go.go`), which uses the standard library's `go/ast` parser.
2. **Non-Go group** — tiered fallback in `parseNonGoFiles`:
   - Tree-sitter if `tsAvailable`
   - ctags if the binary is on `$PATH`
   - Regex (`parser_generic.go`, `parser_cfamily.go`, `parser_web.go`)

Each parser returns a `*FileSymbols` with the file's symbols, imports, and parse method.

After parsing, `DetectImplementations` walks Go structs and interfaces to link `impl: Writer` tags.

## Stage 3: Rank

`ranker.go` → `RankFiles(parsed []*FileSymbols) []RankedFile`

See [Ranking](06-ranking.md) for the weights. The output is files sorted by score, each carrying:

- Its symbols
- `Score` — the numeric rank
- `ImportedBy` — count of files that import it
- `DependsOn` — count of imports this file has
- `Tag` — e.g. `entry`
- `Untested` — true if it has no sibling `_test` file

## Stage 4: Budget

`budget.go` → `BudgetFiles(ranked, maxTokens) []RankedFile`

Assigns a `DetailLevel` to each file so the rendered output fits the token budget:

```
-1: omitted
 0: header only
 1: summary line
 2: full symbol groups
 3: symbols + field expansion
```

Walk rank order, promote each file to the highest level the remaining budget allows. Reserved 70% budget goes to headers and symbols; 30% to field expansion on top-ranked types.

When `maxTokens == 0`, everything gets level 2 (used by verbose and detail formats).

## Stage 5: Format

Four renderers, chosen by the caller:

| Renderer | File | Output |
| --- | --- | --- |
| `FormatMap` | `render.go` | Compact, verbose, or detail Markdown |
| `FormatLines` | `render_lines.go` | Actual source lines |
| `FormatXML` | `render_xml.go` | Structured XML |

Each renderer consumes `RankedFile.DetailLevel` and shapes output accordingly. Renderers are pure functions — no state, no I/O.

## The `Map` type

`repomap.go` → `type Map struct`

Holds everything:

- `root` and `config`
- The ranked file slice (`ranked`)
- Recorded mtimes for staleness (`mtimes`)
- Lazy-computed output cache (`outputs`)
- Parser availability flags (`tsAvailable`, `ctagsAvailable`)

Protected by a `sync.RWMutex`. `Build` takes a write lock; `String*` and `Stale` take a read lock.

## Why one package?

Because the pipeline is linear, the types are shared, and every stage depends on the one before. Splitting `scan`, `parse`, `rank`, `budget`, and `render` into separate packages would create five circular dependency hazards and no new abstractions.

Files are named by what they do. When you need to change ranking, open `ranker.go`. Parser logic lives in `parser_*.go`. Renderers in `render*.go`. The orchestrator is `repomap.go`.

## Next

- [Troubleshooting](10-troubleshooting.md) — when results look wrong
- [Library Usage](05-library-usage.md) — call the pipeline programmatically
