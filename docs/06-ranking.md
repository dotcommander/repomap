# Ranking

Why does `repomap.go` come before `internal_helper.go`? Because the ranker scored it higher. Here's what the ranker looks at.

## The signals

Each file gets a score. Higher scores show up first and keep more detail.

| Signal | Weight |
| --- | --- |
| Entry point (`main.go`, `index.ts`, `mod.rs`, …) | +50 |
| Well-known entry filenames (`index.*`, `server.*`) | +30 |
| Each exported symbol | +1 |
| Each file that directly imports this one | +10 |
| Transitive fan-in bonus (deeply depended-on files) | additional score |
| Path depth (per level) | -1 |

A file imported by five others, with twelve exported symbols, three levels deep scores roughly `5×10 + 12×1 − 3 = 59`.

**Transitive fan-in**: files that sit deep in the import graph — depended on by many files indirectly — receive an additional score bonus proportional to their reachability. This keeps core library files visible even when only a few direct importers exist.

## Import references

repomap resolves imports two ways:

- **Go**: matches import paths against the module root. A file in `pkg/auth` imported by `cmd/api` credits `pkg/auth` for one reference.
- **Other languages**: matches import strings (or `require`, `from`, `use`) against basenames. It's approximate — you'll miss aliased imports and package re-exports — but it's fast and language-agnostic.

## Entry points

A file is an entry point if any of these match:

- `cmd/<name>/main.go`
- `main.go`, `main.py`, `main.rs`, `main.ts`, `main.js`
- `index.ts`, `index.js`, `index.html`
- `server.ts`, `app.py`, `Program.cs`

Entries get `+50`, surface first, and render with a `[entry]` tag.

## Detail levels

Within the budget, each file is assigned one of five detail levels:

| Level | What shows |
| --- | --- |
| -1 | Omitted (counted in the trailing summary) |
| 0 | Path only, with optional `(package name)` |
| 1 | Summary: `3 types, 7 funcs` |
| 2 | Full symbol groups |
| 3 | Full symbols plus struct/interface field expansion |

Top-ranked files push toward level 3. Tails collapse to level 0 and fold into a single `(+12 more: a.go, b.go, ...)` line when they share no symbols worth showing.

## Dead export detection

When a file has exported symbols but zero importers, those symbols are marked **dead**. Dead exports cost half a budget unit instead of a full one — the file stays visible but compresses more aggressively. This keeps genuinely unused public API out of the way without hiding it entirely.

## Boundary detection

Per-language boundary scoring identifies natural module or package boundaries and factors them into the ranking. This works for: **Go**, **TypeScript/JavaScript**, **Python**, **Rust**, **Java**. Files that sit at package/module entry boundaries rank higher relative to files deep inside the same boundary.

## Caller-count bonus (`--calls` mode)

In `--calls` mode, files with many callers receive an additional score bonus. The more places that call into a file, the higher it ranks — useful for surfacing heavily-used utilities that might otherwise score low due to few importers.

## Stale detection

repomap uses **content-hash stale detection**: a file whose mtime changed but whose content is unchanged does not trigger a rebuild. Only actual byte-level content changes invalidate the cache. This avoids spurious rebuilds caused by `touch`, `git checkout`, or filesystem metadata updates.

## Intent ranking (`--intent`)

When you pass `--intent "natural language query"`, repomap runs a BM25 pass over the parsed files before budget allocation. The query is tokenized and scored against three fields with different weights:

| Field | Weight | Source |
| --- | --- | --- |
| Symbols | high | exported symbol names |
| Paths | medium | directory and filename components |
| Imports | low | import path components |

High-scoring files receive a score bonus that promotes them to higher detail levels within the same token budget. This is purely additive — it cannot demote files that would otherwise rank high.

```bash
repomap --intent "fix token refresh" .
```

No external dependencies. No configuration. Omit the flag and behavior is identical to before.

## Tuning

You can't tune ranking from the CLI. The weights are constants in `ranker.go` and `budget.go`. If you need different weights:

1. Fork the module
2. Edit the constants
3. Vendor the fork

This is on purpose. A tunable ranker that nobody tunes the same way is worse than an opinionated one.

## What doesn't count

- Test files (`*_test.go`, `*.test.ts`) — parsed but ranked lower
- Generated code — scanned if present, ranked normally
- File size — not used as a signal
- Git history — not used

Ranking looks at the symbol graph, not at the history. A two-day-old file with heavy imports beats a five-year-old helper.

## Next

- [Caching](07-caching.md) — persist the ranked state to disk
- [Languages](08-languages.md) — what each parser extracts
