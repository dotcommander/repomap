# Output Formats

Five formats. Pick one with `-f` or `--format`.

## Compact

```bash
repomap -f compact
```

The default. Ranks files, fits them inside the token budget, collapses long symbol lists into counts.

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

Every symbol from every file. No summarization. No budget trimming.

```
repomap.go
  types: Config, Map
  methods: Build, SetCacheDir, Stale, String, StringDetail, StringLines, StringVerbose, StringXML
  funcs: DefaultConfig, New
```

Use this when you want the whole skeleton and you're not worried about size.

## Detail

```bash
repomap -f detail
```

Verbose, plus signatures for functions and methods and field lists for structs.

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
  <file path="repomap.go" lang="go" score="120" pkg="repomap" parsed="ast">
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

Wraps verbose output as a JSON array of lines:

```json
[
  "## Repository Map (41 files, 119 symbols)",
  "",
  "### Dependencies",
  "repomap/cmd/repomap → repomap/internal/cli"
]
```

Use this when you need line-by-line parsing without imposing a schema.

## Budget behavior

| Format | Respects `-t` |
| --- | --- |
| compact | yes |
| verbose | no |
| detail | no |
| lines | yes |
| xml | yes |

Verbose and detail are for humans. Compact, lines, and XML are for prompts.

## Next

- [Configuration](04-configuration.md) — every flag, what it does
- [Ranking](06-ranking.md) — how repomap decides what goes first
