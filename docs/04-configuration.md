# Configuration

repomap has three flags. That's it.

## Flags

| Flag | Short | Default | Description |
| --- | --- | --- | --- |
| `--tokens` | `-t` | `2048` | Approximate token budget for the output |
| `--format` | `-f` | `compact` | One of `compact`, `verbose`, `detail`, `lines`, `xml` |
| `--json` | — | `false` | Emit verbose output as a JSON array of lines |

## Positional argument

```bash
repomap [directory]
```

Zero or one argument. Defaults to `.`. Must be a directory that contains source files.

## Token budget

A token is roughly four bytes. The budget controls:

- How many files make it into compact output
- How much detail each file gets (header only, summary, symbols, symbols + fields)
- How many source lines show in lines format

```bash
repomap -t 1024    # tight prompt
repomap -t 4096    # default doubled
repomap -t 16384   # practically uncapped
```

Verbose and detail formats ignore the budget — they always emit everything.

## Format

```bash
repomap -f compact    # default; budget-trimmed
repomap -f verbose    # all symbols
repomap -f detail     # verbose + signatures + struct fields
repomap -f lines      # actual source lines
repomap -f xml        # structured output
```

See [Output Formats](03-output-formats.md) for examples.

## JSON

```bash
repomap --json
```

Equivalent to running `-f verbose` and wrapping the output in a JSON array of lines. The flag overrides `-f`.

## Environment

None. repomap reads no environment variables.

## Config files

None. repomap reads no config files. Every run is explicit.

## What lives in `Config` (library)

The library exposes two fields via `repomap.Config`:

| Field | Default | Purpose |
| --- | --- | --- |
| `MaxTokens` | `1024` | Budget for compact and XML formats |
| `MaxTokensNoCtx` | `2048` | Budget for lines format |

The CLI wires both fields to the same `-t` value. Call the library directly if you want to set them independently — see [Library Usage](05-library-usage.md).

## Next

- [Library Usage](05-library-usage.md) — use repomap from Go code
- [Caching](07-caching.md) — speed up repeated runs
