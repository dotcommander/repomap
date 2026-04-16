# Caching

Building a map of a large repo takes real time. Caching makes repeated runs nearly free.

## Two caches

repomap has two independent caches:

1. **Output cache** — in-memory, per `Map`. Each `String*` method computes once, stores the result, returns the cached string on subsequent calls. Cleared when `Build` runs.
2. **Disk cache** — optional, on-disk. Saves the ranked state and pre-rendered outputs to a file. Survives process restarts.

Output caching is automatic and always on. Disk caching is opt-in.

## Enable disk caching

```go
m := repomap.New(".", repomap.DefaultConfig())
m.SetCacheDir("/tmp/repomap-cache")

if err := m.Build(ctx); err != nil {
    return err
}
```

After `Build`, the cache file is written automatically (best-effort — errors are swallowed).

## Load a cached build

```go
m := repomap.New(".", repomap.DefaultConfig())
if err := m.LoadCache("/tmp/repomap-cache"); err != nil {
    // no cache or corrupt; fall through
}

if m.Stale() {
    _ = m.Build(ctx)
}

fmt.Print(m.String())
```

`LoadCache` hydrates the `Map` from disk. `Stale` then walks the recorded mtimes and returns `true` if any source file changed.

## Cache keys

Each project gets a separate cache file. Keys are `sha256(absolute root path)`. A single cache directory can hold maps for many projects:

```
/tmp/repomap-cache/
  3a7f...b0d1.json      # one project
  b2e1...9ac3.json      # another project
```

## Cache format

JSON. Contains:

- The recorded mtimes for staleness checks
- The full `RankedFile` slice
- Pre-rendered `compact` and `lines` outputs

Verbose, detail, and XML outputs are recomputed on demand — they're fast relative to the full pipeline.

## Cache invalidation

There's one rule: **the cache is stale if any tracked mtime changed.**

`Stale()` is O(tracked files) and runs each file stat in parallel. On a 500-file repo it takes single-digit milliseconds.

Staleness is debounced at 30 seconds. If you call `Stale` twice inside that window, the second call returns `false` without touching the filesystem. This makes it safe to call on a hot path.

## Incremental rebuild

When the disk cache contains a saved HEAD commit SHA (`LastSHA`), `Build` re-parses only the files changed between that commit and HEAD (via `git diff --name-status`), plus any untracked files respecting `.gitignore`. This makes repeated builds on large repos nearly instantaneous when few files changed.

Falls through to a full rebuild if:
- The cached SHA is unreachable (e.g., after a rebase or force-push)
- More than 30% of cached files changed (rank recomputation dominates at that point)
- The directory is not inside a git repo

Any git failure triggers a silent full rebuild — correctness is always preferred over speed.

## Cache versioning

The disk format has a version number (`cacheVersion = 6`). A mismatched version causes `LoadCache` to discard the cache and fall back to a full `Build`. The v5→v6 bump (which added `LastSHA` and `GitRoot` fields) triggers a one-time full rebuild for existing users.

## When caching hurts

Skip the disk cache if:

- The repo is small enough that `Build` takes under a second
- You're running once per process
- You change source files faster than `Stale` debounces

For CI pipelines that build, inspect, and exit, skip caching. For editors and long-running tools, enable it.

## Next

- [Languages](08-languages.md) — which parsers handle which extensions
- [Architecture](09-architecture.md) — the pipeline, stage by stage
