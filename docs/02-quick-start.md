# Quick Start

Run repomap on the current directory:

```bash
repomap
```

You'll see something like this:

```
## Repository Map (41 files, 119 symbols)

### Dependencies
repomap/cmd/repomap → repomap/internal/cli
repomap/internal/cli → repomap

cmd/repomap/main.go [entry]
  funcs: main

repomap.go [imported by 1]
  types: Config, Map (2 total)
  methods: Build, SetCacheDir, Stale, String, StringDetail, StringLines, StringVerbose, StringXML

ranker.go [imported by 1]
  types: RankedFile
  funcs: RankFiles
  methods: Len, Less, Swap (3 total)
```

That's the whole idea. Files ranked by importance. Symbols summarized. Budget respected.

## Target a different directory

```bash
repomap ./src
repomap ~/projects/my-app
```

## Pipe to an LLM

```bash
repomap -t 4000 | pbcopy                    # macOS clipboard
repomap | llm "summarize this codebase"     # pipe to an LLM CLI
repomap --json > map-lines.json             # schema-versioned rendered lines
repomap --json-structured > map-data.json   # structured file/symbol/call-site/ranking data
repomap --artifact map.md                    # write output without shell redirection
```

## Build an implementation packet

```bash
repomap task "harden CLI output" .
repomap task "harden CLI output" --json --consumed=internal/cli/render.go .
```

`task` selects up to six goal-relevant targets and composes selection evidence,
source excerpts, callers, consumers, tests, effects, applicable instructions,
dirty overlap, and verification commands. It uses a 4096-token complete-output
budget by default. Consumed files retain their identity and relationships, but
source space goes to files that are not already in context.

## Inventory a Boundary

```bash
repomap --intent "PostgreSQL database psql pgx migrations schema queries" --explain
repomap inventory --boundary Postgres
repomap audit effects --kind database --paths-only
```

## Inspect one symbol

```bash
repomap context RankFiles
repomap context RankFiles --json
```

`context` returns the best symbol match, a bounded source span, ambiguity hints, and impact facts for the owning file. Add `--calls` for exact callers from the in-process Go semantic graph.

## Change the format

repomap has six output formats. Enriched is the default.

```bash
repomap               # default; enriched — signatures + godoc + fields
repomap -f compact    # explicit compact; budget-aware, skips long tails
repomap -f verbose    # every symbol, no summarization
repomap -f detail     # verbose plus signatures and struct fields
repomap -f lines      # actual source lines from each file
repomap -f xml        # structured XML for parsers and programmatic consumers
```

See [Output Formats](03-output-formats.md) for when to use which.

## Change the budget

```bash
repomap -t 1024       # tight — roughly 1K tokens
repomap -t 8192       # generous — roughly 8K tokens
repomap -t 32000      # no real limit
```

A token is roughly four bytes. `-t 2048` (the default) limits the complete
encoded output to about 8KB. This applies to every bounded format, including
`verbose` and `detail`.

## Check cache status

```bash
repomap cache status
repomap cache status --json
repomap cache warm .
```

This inspects the optional disk cache for the current root and reports whether it is missing, fresh, stale, or unusable.
Use `cache warm` to build, save, and verify a fresh entry.

## Next

- [Output Formats](03-output-formats.md) — what each format looks like
- [Configuration](04-configuration.md) — every flag, what it does
- [Library Usage](05-library-usage.md) — call repomap from Go code
- [Usage Examples](11-usage-examples.md) — task packets and focused workflows
