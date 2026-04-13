# Troubleshooting

Things that can go wrong, and what to do.

## "no source files found"

```
Error: no source files found
```

The directory has no files in any language repomap recognises. Causes:

- You pointed at a non-code directory
- The directory isn't a git repo and the scanner's `git ls-files` fallback didn't find anything
- Every file is over 50 KB or in an excluded directory (vendor, node_modules, dist, build, target)

Fix: point at a real project root. If the project uses an unusual language, see [Adding a language](08-languages.md#adding-a-language).

## Output is empty

Build succeeded but `repomap` printed nothing.

- The project has files, but none export any symbols (e.g. an all-private Go package). Compact output suppresses empty files.
- Try `-f verbose` — it prints file headers even without symbols.

```bash
repomap -f verbose
```

## Output is truncated

The budget clipped too much.

```bash
repomap -t 8192       # double the default
repomap -t 16384      # roughly 64 KB of output
repomap -f verbose    # ignore the budget entirely
```

## Symbols are wrong or missing

Depends on which parser ran:

- **Go files** (`parsed="ast"`) — report a bug. The standard library parser is authoritative.
- **Tree-sitter** — report the file and the grammar version.
- **ctags** — try without ctags (install `universal-ctags` if you haven't; it's more accurate than the legacy variant).
- **regex** — expect false positives and missing symbols. That's the regex tier's tradeoff.

Check which parser handled a file:

```bash
repomap -f xml | grep 'parsed=' | head
```

## Wrong files rank first

The ranker uses import references, exported symbol counts, and entry tags. To understand a specific ranking:

```bash
repomap -f xml | grep -A1 'your_file.go'
```

Look at `score=`, `imported-by=`, and `imports=`. Unexpected scores usually mean:

- Imports aren't resolving — the ranker's basename-matching is approximate for non-Go languages
- The file is misclassified as an entry point

See [Ranking](06-ranking.md) for the weights.

## Cache isn't invalidating

```go
m := repomap.New(".", cfg)
m.SetCacheDir(dir)
m.LoadCache(dir)
fmt.Println(m.Stale())   // expected true, got false?
```

Two reasons:

1. **Debounce window.** `Stale` returns false for 30 seconds after the last `Build`, regardless of file changes. Wait 30 seconds or call `Build` directly.
2. **File wasn't tracked.** `Stale` only checks mtimes of files that were part of the last build. A newly-added file won't flag stale until something else does.

For authoritative freshness, just rebuild. It's usually fast.

## Tree-sitter isn't being used

```go
repomap.TreeSitterAvailable()   // returns false
```

Tree-sitter is gated behind a build tag. If you're consuming the library, make sure your build includes the tree-sitter-enabled variant. Check your build flags:

```bash
go build -tags treesitter ./...
```

The prebuilt CLI (`go install ...@latest`) includes tree-sitter.

## Output is too slow

Repomap on a 10,000-file monorepo can take a few seconds. Options:

- Point at a subtree: `repomap ./pkg`
- Enable disk cache: `m.SetCacheDir(...)` (see [Caching](07-caching.md))
- Use the `--json` output and cache it in your tooling layer

## The binary isn't on PATH after `go install`

```bash
which repomap
# nothing
```

`go install` puts binaries in `$GOBIN`, which defaults to `~/go/bin`. Add it to your shell config:

```bash
export PATH="$HOME/go/bin:$PATH"
```

## Still stuck

File an issue with:

- The command you ran
- The output (or the first 50 lines if huge)
- `go version`
- OS
- A link to the project you ran it against (or a minimal reproduction)

https://github.com/dotcommander/repomap/issues
