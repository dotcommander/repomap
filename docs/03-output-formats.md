# Output Formats

Six formats are available through `-f` or `--format`; enriched is the default.

## Enriched

```bash
repomap
repomap -f enriched
```

Ranks files and emits exported symbols, signatures, first-sentence
documentation, and struct/interface fields within the complete CLI budget.

## Compact

```bash
repomap -f compact
```

Ranks files, fits them inside the token budget, and collapses long symbol lists
into counts.

```
repomap.go [imported by 1]
  types: Config, Map (2 total)
  methods: Build, SetCacheDir, Stale, String, StringDetail, StringLines, StringVerbose, StringXML
```

Use this when you're pasting output into an LLM prompt. It respects `-t` and trims long tails into summary lines.

## Verbose

```bash
repomap -f verbose
```

Every symbol in each selected file. No per-file summarization. The CLI still
applies `-t` to the complete encoded response and therefore omits whole files
when necessary.

```
repomap.go
  types: Config, Map
  methods: Build, SetCacheDir, Stale, String, StringDetail, StringLines, StringVerbose, StringXML
  funcs: DefaultConfig, New
```

Use this when you want the broadest symbol skeleton that fits the requested CLI
budget. For an unbounded Go-library rendering, use `Map.StringVerbose()`.

## Detail

```bash
repomap -f detail
```

Verbose, plus signatures for functions and methods and field lists for structs.
As with verbose, the CLI preserves whole files and bounds the complete response.

```
repomap.go
  funcs:
    DefaultConfig() Config
    New(root string, cfg Config) *Map
  methods:
    Map.Build(ctx context.Context) error
    Map.Stale() bool
  types:
    Config { MaxTokens int; MaxTokensNoCtx int }
```

Use this when you want enough to write code against the API without opening the files.

## Lines

```bash
repomap -f lines
```

Actual source lines from each file — the real declaration, not a summary.

```
repomap.go
  19: var ErrNotCodeProject = errors.New("no source files found")
  28: type Config struct {
  57: func New(root string, cfg Config) *Map {
  79: func (m *Map) Build(ctx context.Context) error {
```

Use this when you want grep-style context. Respects the `-t` budget.

## XML

```bash
repomap -f xml
```

Structured output for programmatic consumers.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<repomap files="41" symbols="119">
  <dependencies>
    <pkg name="repomap/internal/cli">repomap</pkg>
  </dependencies>
  <file path="repomap.go" lang="go" score="120" pkg="repomap" parsed="go_ast">
    <symbols>
      <sym name="Config" kind="struct" exported="true" line="28" span="4"/>
      <sym name="New" kind="function" exported="true" line="57" params="2" results="1">
        (root string, cfg Config) *Map
      </sym>
    </symbols>
  </file>
</repomap>
```

Use this when you're feeding repomap into another tool. XML parsers eat it; LLMs read it as structured input.

## JSON

```bash
repomap --json
```

Wraps verbose output in a schema-versioned JSON envelope:

```json
{
  "schema_version": 1,
  "lines": [
    "## Repository Map (41 files, 119 symbols)",
    "",
    "### Dependencies",
    "repomap/cmd/repomap → repomap/internal/cli"
  ]
}
```

Use this when you need line-by-line parsing with a small stable envelope. Use `--json --json-legacy` only for scripts that still expect the old bare `[]string` shape.

## Structured JSON

```bash
repomap --json-structured
```

Emits schema-versioned file, symbol, call-site, rank, parser, and budget data:

```json
{
  "schema_version": 1,
  "totals": {"files": 41, "symbols": 119},
  "files": [
    {
      "path": "ranker.go",
      "parse_method": "go_ast",
      "score": 123,
      "score_components": {"imports": 110, "symbols": 3, "transitive": 10},
      "detail_level": 2,
      "symbols": [{"name": "RankFiles", "kind": "function", "line": 48}],
      "call_sites": [{"name": "BudgetFiles", "line": 54}]
    }
  ],
  "files_omitted": 12,
  "files_omitted_reason": "complete-output token budget"
}
```

Use this for coding-agent tooling, editor integrations, or scripts that need stable fields. Parser-backed non-Go call sites appear when the language grammar supports call-expression extraction; they are structural, not type-resolved.

## Command JSON

Subcommands that return focused data have their own JSON shapes:

```bash
repomap context RankFiles --json
repomap impact ranker.go --markdown
repomap impact ranker.go --json
repomap cache status --json
```

`impact --markdown` emits a compact human handoff from the same impact facts as
JSON. `context` includes the selected symbol, ambiguity hints, bounded source
lines, optional callers, and the owning file's impact facts. `cache status`
reports cache existence, usability, freshness, reason, path, saved/current HEAD,
and tracked file count.

## Budget behavior

Every CLI map format respects `-t`: enriched, compact, verbose, detail, lines,
XML, `--json`, `--json-legacy`, and `--json-structured`. The limit applies to
the complete encoded stdout payload, not only to the map's detail-level
selection. repomap measures encoded bytes as `ceil(bytes / 4)` tokens, renders
whole files or records only, and writes only after a complete payload fits. If
the requested budget cannot hold even that format's minimum valid envelope, the
command returns an error and writes no partial stdout (or partial `--artifact`).

The structured JSON CLI returns only the selected `files` and keeps repository
wide counts in `totals`. When selection omits files, `files_omitted` and
`files_omitted_reason` account for them; omitted files do not remain inline as
`detail_level: -1` records.

This is a CLI boundary. Direct Go callers can choose unbounded rendering:
`Map.StringVerbose()` returns the full verbose map and `Map.StringDetail()` the
full detailed map. `BudgetFiles` is the renderer's detail-selection heuristic;
it does not by itself prove that a serialized CLI response has an exact byte
size.

## Next

- [Configuration](04-configuration.md) — every flag, what it does
- [Ranking](06-ranking.md) — how repomap decides what goes first
