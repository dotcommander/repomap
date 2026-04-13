# Ranking

Why does `repomap.go` come before `internal_helper.go`? Because the ranker scored it higher. Here's what the ranker looks at.

## The signals

Each file gets a score. Higher scores show up first and keep more detail.

| Signal | Weight |
| --- | --- |
| Entry point (`main.go`, `index.ts`, `mod.rs`, …) | +50 |
| Well-known entry filenames (`index.*`, `server.*`) | +30 |
| Each exported symbol | +1 |
| Each file that imports this one | +10 |
| Path depth (per level) | -1 |

A file imported by five others, with twelve exported symbols, three levels deep scores roughly `5×10 + 12×1 − 3 = 59`.

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
