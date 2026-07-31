# Library Usage

The public Go API provides repository analysis, mapping, rendering, audit,
task, context, and impact reports. Commit mutation workflows remain CLI-only.

## Install

```bash
go get github.com/dotcommander/repomap
```

## Build a map

```go
package main

import (
    "context"
    "fmt"

    "github.com/dotcommander/repomap"
)

func main() {
    m := repomap.New(".", repomap.DefaultConfig())
    if err := m.Build(context.Background()); err != nil {
        panic(err)
    }
    fmt.Print(m.String())
}
```

`New` is cheap — it just holds configuration. `Build` does the work: scan, parse, rank, rank files, cache output.

## Configuration

```go
cfg := repomap.Config{
    MaxTokens:      4096,  // ranked map/detail-selection budget
    MaxTokensNoCtx: 8192,  // lines budget
    Intent:         "fix token refresh",  // BM25 task-aware ranking (optional)
}
m := repomap.New("./src", cfg)
```

Zero values get defaults (`1024` and `2048`).

## Rendering

Every format has a method. Call as many as you want; results are cached per format until the next `Build`.

```go
m.String()         // enriched, budget-trimmed
m.StringVerbose()  // all symbols; unbounded direct-library rendering
m.StringDetail()   // verbose + signatures + struct fields
m.StringLines()    // actual source lines
m.StringXML()      // structured XML
```

Each returns an empty string if `Build` hasn't run or the project contains no symbols.
`String()` and `StringCompact()` use the map configuration's render budget.
`StringVerbose()` and `StringDetail()` are deliberately unbounded full-map
renderings; they do not have the CLI's final encoded-output budget layer.

## Build a task handoff

Use `Map.Task` when an integration needs a bounded implementation packet rather
than a general repository map. It derives a task-specific build from the map's
root, ranks for the goal, adds semantic Go relationships and audit effects, and
then packs a single report for either `FormatTask` or `MarshalTaskJSON`.

```go
report, err := m.Task(ctx, "fix token budget overshoot", repomap.TaskOptions{
    MaxTokens:     4096,
    ConsumedPaths: []string{"budget.go", "internal/cli/render.go"},
})
if err != nil {
    return err
}
fmt.Print(repomap.FormatTask(report))
```

`TaskOptions.MaxTokens` defaults to `4096` when zero; a negative value returns
`task max tokens must not be negative`. `ConsumedPaths` are normalized against
the map root, mark those targets as consumed, downrank them, and prefer their
importers. A blank goal returns `task goal must not be blank`; a consumed path
outside the root also returns an error.

`TaskReport` is schema version 1. Its compatibility keys cover `schema_version`,
the root and goal, `budget` (`max_tokens` and `used_tokens`), selection strategy, applicable
rule paths, related git changes, selected targets, read-next ranges, verification
and follow-up commands, diagnostics, and explicit `truncations`. A target carries
its symbols, task-match `evidence`, relationship and effect `provenance`, a
`confidence` tier, package/risk/parse metadata, callers/consumers/tests/imports,
and bounded source excerpts. The report reduces sources, relationship lists, and
metadata before dropping targets; every reduction is recorded in `truncations`.
The final report fits whichever is larger: its JSON encoding or its human
`FormatTask` rendering, measured as `ceil(bytes / 4)` tokens.

Selection confidence describes deterministic match strength:

| Confidence | Meaning |
| --- | --- |
| `high` | Positive task evidence scored at least 16 |
| `medium` | Positive task evidence scored from 8 through 15 |
| `low` | Positive task evidence scored below 8 |
| `fallback` | No file had positive task evidence; structural rank supplied the target |

Confidence is not a correctness or preservation claim. Relationship provenance
is `exact` for semantic Go evidence, `syntactic` for parsed source structure,
and `heuristic` for naming or adjacency.

To replay a private evaluation manifest without adding its paths or results to
the repository or CI configuration:

```bash
REPOMAP_TASK_REPLAY_MANIFEST=/path/to/tasks.json \
  go test . -run '^TestTaskManifestContractAndOptionalReplay$' -count=1
```

The manifest is a JSON array using the same task records as `testdata/task`.

## Symbol context

```go
ctx, err := m.Context("RankFiles", repomap.ContextOptions{
    MaxSourceLines: 50,
})
if err != nil {
    return err
}
fmt.Println(ctx.Match.File, ctx.Match.Symbol.Line)
```

`Context` resolves the best symbol match, extracts a bounded source span, includes ambiguity hints, and attaches the owning file's `Impact` result. Set `Config.GoAnalysisCalls`, call `Build`, then use `Map.SemanticCallers()` for receiver-qualified Go callers. `ExpandCallers` remains available for integrations that explicitly need an external `RefsQuerier` such as `gopls`.

## Staleness

```go
if m.Stale() {
    _ = m.Build(context.Background())
}
```

`Stale` walks the tracked file mtimes and returns true if any source file changed since the last `Build`. Debounced at 30 seconds — two calls inside the debounce window both return `false`.

## Error handling

`Build` returns `repomap.ErrNotCodeProject` when the directory has no recognisable source files. Treat this as expected, not fatal:

```go
err := m.Build(ctx)
switch {
case errors.Is(err, repomap.ErrNotCodeProject):
    return // not a code project; skip
case err != nil:
    return fmt.Errorf("repomap build: %w", err)
}
```

## Concurrency

`Map` is safe for concurrent use. `Build` takes a write lock; the `String*` methods take a read lock. You can call them from many goroutines.

## Caching to disk

```go
m := repomap.New(".", cfg)
m.SetCacheDir("/tmp/repomap-cache")
m.Build(ctx)   // builds then saves
```

On the next run, `LoadCache` reads the saved state:

```go
m := repomap.New(".", cfg)
if m.LoadCache("/tmp/repomap-cache") && !m.Stale() {
    fmt.Print(m.String())
    return
}
m.Build(ctx)
```

Cache keys are SHA-256 of the absolute project root. Multiple projects can share one cache directory.

Inspect a cache entry without loading it:

```go
status := repomap.InspectCache(ctx, root, "/tmp/repomap-cache")
if status.Stale {
    fmt.Println(status.Reason)
}
```

## A full example

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "os"

    "github.com/dotcommander/repomap"
)

func run(root string) error {
    m := repomap.New(root, repomap.Config{MaxTokens: 4096})
    m.SetCacheDir(os.TempDir() + "/repomap")

    if m.LoadCache(os.TempDir() + "/repomap") && !m.Stale() {
        fmt.Print(m.String())
        return nil
    }

    if err := m.Build(context.Background()); err != nil {
        if errors.Is(err, repomap.ErrNotCodeProject) {
            return nil
        }
        return err
    }

    fmt.Print(m.String())
    return nil
}

func main() {
    if err := run("."); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

## Next

- [Ranking](06-ranking.md) — how `Build` scores files
- [Caching](07-caching.md) — more on disk cache behavior
