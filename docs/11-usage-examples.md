# Usage Examples

A tour of repomap by task. Each example is a real command with real output — copy,
run, adapt.

repomap exists to answer one question better than `ls -R` or `grep` ever could:
**given a token budget, what should I read first, and how much should I trust it?**
That framing matters most for LLM agents, which pay for every token and have to
decide what to load before they can reason. The examples below walk from a cold
first look through task-focused selection, trust calibration, impact analysis, and
machine-readable output.

Every command assumes you're at the root of a git repository.

---

## Choosing an output mode

| Mode | Invocation | What it emits | Best for |
|------|-----------|---------------|----------|
| enriched (default) | _(no `-f`)_ | exported symbols + signatures + godoc first line + struct fields, budget-trimmed | the everyday "read me first" map |
| compact | `-f compact` | exported symbol **names** only, no signatures/docs/fields | wide orientation — more files fit the same budget |
| verbose | `-f verbose` | **all** files (no budget cut), all symbols incl. unexported, grouped by kind, names only | a full inventory of every symbol |
| detail | `-f detail` | all files, full signatures + struct fields | the most verbose text mode |
| lines | `-f lines` | actual source lines read from disk, budget-trimmed | reading code, not summaries |
| xml | `-f xml` | structured XML: dependency graph + `<file>`/`<symbols>`/`<sym>` (name, kind, line, span, params, implements) | machine consumption |
| json-structured | `--json-structured` | structured JSON repository map (files, symbols, scores; tier breakdown after `explain`) | programmatic ranking/selection |

**Gotchas.** The default (enriched) is **richer** than `-f compact` — compact is names only. `-f verbose` is *wider* but *shallower* per file than the default: it lists every file and symbol but drops signatures. And `--intent` reranks files **silently** — add `--explain` to see why files ranked.

The header now reports `~T tokens` (e.g. `## Repository Map · enriched (168 files, 874 symbols, ~1496 tokens)`), so an orchestrating agent can scale `-t` from the estimate.

---

## 1. Get oriented in an unfamiliar repo

The default run is the one you'll use most. No flags: repomap discovers source files,
ranks them, and prints the most important ones until it hits the token budget
(default `2048`).

```bash
repomap
```

```
## Repository Map (162 files, 880 symbols)

### Flow
entry: cmd/repomap/main.go
spine: repomap.go, types.go, calls.go, ranker.go, budget.go

### Dependencies
repomap → repomap/internal/lsp
repomap/cmd/repomap → repomap/internal/cli
repomap/internal/cli → repomap, repomap/internal/lsp

calls.go [imported by 17, imports: 1]
  type CallsConfig{Threshold int, Limit int, IncludeTests bool}
    // controls --calls mode behaviour
  type CallsStats{OK int, Timeout int, Error int}
    // holds counters from a call-expansion run
```

Two lines orient you before the file list:

- **`### Flow`** names the detected entry point and the *spine* — the top behavioral
  files (rank-ordered implementation files with exported functions or methods). It
  answers "where does this start, and what's the backbone?" before you read a single
  symbol.
- The map leads with implementation. Test files are **demoted by default**, so
  `_test.go` never crowds out the code it covers. When you're working *on* the tests,
  rank them at full weight with `--include-tests`:

```bash
repomap --include-tests ./src
```

Need a wider or narrower view? Move the budget:

```bash
repomap -t 4096            # more files / more detail
repomap -t 512             # just the load-bearing files
```

When you only want **names** for orientation — no signatures, no docs — drop to
compact:

```bash
repomap -f compact
```

```
calls.go [imported by 17, imports: 1]
  types: CallsConfig, CallsStats, Location, SymbolCallers
  interfaces: RefsQuerier
  funcs: CheckGopls, CheckLspq, DefaultCallsConfig, DefaultQuerier, ExpandCallers
  methods: Refs, Refs
```

The format ladder is `compact` → (default enriched) → `verbose` → `detail`, trading
breadth for depth. `verbose` shows every symbol; `detail` adds full signatures and
struct fields.

---

## 2. Rank for the task at hand

A generic map ranks by structural importance. When you have a *task*, tell repomap —
it re-ranks with BM25 so the files relevant to your query rise to the top of the same
budget.

```bash
repomap --intent "parse php class methods"
repomap -i "retry with exponential backoff"
```

Intent matching reads file paths, package names, symbol names, imports, signatures —
**and doc comments**. A function documented as `// Retry implements exponential
backoff` will surface for `-i "exponential backoff"` even if nothing in its name says
so.

---

## 3. See *why* a file ranked — and how much to trust it

repomap's score is a sum of heuristic signals, and not all signals are equally
trustworthy. `--explain` annotates each file with a per-tier breakdown so you can tell
a verified fact from a lexical guess.

```bash
repomap --explain
```

```
calls.go [imported by 17, imports: 1] # score 203 · structural:203
```

The tiers, from most to least trustworthy:

| Tier | Means | Backed by |
|------|-------|-----------|
| `confirmed` | verified references | gopls / LSP (`--calls`) |
| `structural` | parsed structure & import graph | always available |
| `lexical` | by-name match, may be coincidental | `--symbol-refs` |
| `contextual` | depends on your query / session | `--intent`, `--consumed` |

Tiers only appear when their signal is active. Combine flags to light up more of them:

```bash
repomap --explain --symbol-refs --intent "ranking score"
```

```
find.go [imported by 17] # score 368 · structural:184 contextual:184
```

To drill into a single file, use the `explain` subcommand — it shows the total, the
chosen detail level, and every component grouped by tier:

```bash
repomap explain ranker.go
```

```
ranker.go
  score: 199
  detail: 1
  structural
    imports      +170
    symbols      +19
    transitive   +10
```

Want it as data? `repomap explain ranker.go --json` adds `score_by_tier` and
`component_tiers` fields.

---

## 4. Stop re-reading what you've already seen

Mid-investigation, an agent has usually read a few files already. Tell repomap, and it
downranks those files while *upranking the things that import them* — pushing fresh,
adjacent context into the budget instead of repeating yourself.

```bash
repomap --consumed ranker.go,budget.go -i "detail level assignment"
```

---

## 5. Scope a change before you make it

Before editing a symbol, find out what leans on it. `impact` reports deterministic
local facts — importers and tests — for a file:

```bash
repomap impact ranker.go
repomap impact ranker.go --json
```

For a symbol-level view with bounded source and its blast radius, use `context`:

```bash
repomap context RankFiles
repomap context RankFiles --calls --max-source-lines 120
```

---

## 6. Trace callers and references

With gopls available, `--calls` expands exported symbols with their real callers
(verified references — the `confirmed` tier):

```bash
repomap --calls
repomap --calls --calls-threshold 1 --calls-limit 20 --calls-include-tests
```

For pinpoint navigation, the LSP subcommands take `FILE LINE SYMBOL` (1-based line):

```bash
repomap refs ranker.go 52 RankFiles      # every reference
repomap def  ranker.go 52 RankFiles      # jump to definition
repomap hover ranker.go 52 RankFiles     # type + docs
repomap symbols ranker.go                # everything defined in a file
```

---

## 7. Find a symbol by name

When you know the name but not the file:

```bash
repomap find RankFiles
repomap find Config --kind struct
repomap find Parse --file parser --limit 5
repomap find ExpandCallers --format json
```

---

## 8. Output for machines

Agents usually want structured output, not prose. Three shapes:

```bash
repomap --json              # JSON array of the rendered lines
repomap --json-structured   # structured repository map (files, symbols, scores)
repomap -f lines            # actual source lines instead of a symbol summary
```

`--json-structured` is the richest: it carries per-file scores and, after an
`explain`, the tier breakdown — ideal for an agent that ranks and selects
programmatically.

---

## 9. Prepare commits

repomap can analyze a changeset, group related files, and flag breaking changes — a
workflow built for an agent assembling a clean PR.

```bash
repomap commit analyze                   # emit a structured commit plan as JSON
repomap commit prep                      # full pre-commit pipeline → JSON payload
repomap commit auto                      # prep + finish when ready, else report
```

`analyze` accepts `--confidence` to tune how aggressively files cluster into groups;
`execute`/`finish` apply a plan (optionally `--push`, `--tag`).

---

## 10. Set up a project

Scaffold a `.repomap.yaml` and install a post-commit hook that keeps the cache warm:

```bash
repomap init
repomap init --no-hook        # config only
repomap init --force          # overwrite existing
```

The config file lets you blocklist noisy method names, restrict or exclude paths, and
pin specific files to a detail level. See [Configuration](04-configuration.md).

---

## A worked agent loop

Putting it together — how an agent might use repomap across a single task ("fix the
token budget overshoot"):

```bash
# 1. Orient, focused on the task.
repomap -i "token budget overshoot" -t 3072

# 2. Inspect why the top suspect ranked, and trust its signals.
repomap explain budget.go

# 3. Before editing, learn the blast radius.
repomap impact budget.go

# 4. After reading budget.go and ranker.go, refocus without repeating them.
repomap --consumed budget.go,ranker.go -i "enriched cost estimate" -t 2048
```

Each step narrows the context the agent loads next — which is the whole point:
spend the budget on what matters, and know how much to trust it.

---

See also: [Quick Start](02-quick-start.md) · [Output Formats](03-output-formats.md) ·
[Configuration](04-configuration.md) · [Ranking](06-ranking.md).
