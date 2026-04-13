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
repomap --json > map.json                   # structured for programmatic use
```

## Change the format

repomap has five output formats. Compact is the default.

```bash
repomap -f compact    # default; budget-aware, skips long tails
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

A token is roughly four bytes. `-t 2048` (the default) targets about 8KB of output. `verbose`, `detail`, and `lines` formats ignore the budget — they're meant for humans, not prompts.

## Next

- [Output Formats](03-output-formats.md) — what each format looks like
- [Configuration](04-configuration.md) — every flag, what it does
- [Library Usage](05-library-usage.md) — call repomap from Go code
