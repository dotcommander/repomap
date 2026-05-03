# Library Usage

Everything the CLI does, you can do from Go.

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
    MaxTokens:      4096,  // compact + xml budget
    MaxTokensNoCtx: 8192,  // lines budget
    Intent:         "fix token refresh",  // BM25 task-aware ranking (optional)
}
m := repomap.New("./src", cfg)
```

Zero values get defaults (`1024` and `2048`).

## Rendering

Every format has a method. Call as many as you want; results are cached per format until the next `Build`.

```go
m.String()         // compact, budget-trimmed
m.StringVerbose()  // all symbols, no budget
m.StringDetail()   // verbose + signatures + struct fields
m.StringLines()    // actual source lines
m.StringXML()      // structured XML
```

Each returns an empty string if `Build` hasn't run or the project contains no symbols.

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
if err := m.LoadCache("/tmp/repomap-cache"); err == nil && !m.Stale() {
    fmt.Print(m.String())
    return
}
m.Build(ctx)
```

Cache keys are SHA-256 of the absolute project root. Multiple projects can share one cache directory.

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

    if err := m.LoadCache(os.TempDir() + "/repomap"); err == nil && !m.Stale() {
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
