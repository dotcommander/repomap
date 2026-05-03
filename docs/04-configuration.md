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

## Config file

repomap reads `.repomap.yaml` from the project root when it exists. The file controls what gets scanned and how detail levels are forced. Absent file = no-op; every run without it is fully explicit.

```yaml
method_blocklist:
  - "Test*"           # glob: drop symbols starting with "Test"
  - "*Mock"           # glob: drop symbols ending in "Mock"
  - "/^pb_/"          # regex: drop generated protobuf symbols

include_paths:
  - "cmd/*"
  - "internal/*"
  - "pkg/*"

exclude_paths:
  - "internal/generated/*"
  - "vendor/*"

file_overrides:
  "cmd/*/main.go": "full"
  "internal/generated/**": "omit"
```

| Field | Purpose |
| --- | --- |
| `method_blocklist` | Glob (`Test*`) or regex (`/^pb_/`) patterns — drops matching symbol names at parse time |
| `include_paths` | When non-empty, only files matching these path globs are scanned |
| `exclude_paths` | Files matching these path globs are always excluded; takes precedence over `include_paths` |
| `file_overrides` | Forces a file to a fixed detail level regardless of rank. Values: `"full"` or `"omit"` |

Path globs use `path.Match` semantics. Patterns containing `**` match any path with the corresponding prefix (e.g. `internal/generated/**` covers all files under that directory).

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
