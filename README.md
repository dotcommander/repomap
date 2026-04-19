# repomap

A repository becomes a map.

```
$ repomap .
## Repository Map (41 files, 119 symbols)

### Dependencies
repomap/cmd/repomap → repomap/internal/cli
repomap/internal/cli → repomap

cmd/repomap/main.go [entry]
  func main()

repomap.go [imported by 1]
  type Config{MaxTokens int, MaxTokensNoCtx int, ...}
    // holds options for a Map build
  type Map
    // is the top-level orchestrator. Thread-safe.
  func New(root string, cfg Config) *Map
    // creates a Map for root with the given config
  func (m *Map) Build(ctx context.Context) error
    // scans, parses, ranks, and budgets the repository
  func (m *Map) String() string
  func (m *Map) StringCompact() string
  func (m *Map) StringVerbose() string
  func (m *Map) StringDetail() string
  func (m *Map) StringLines() string
  func (m *Map) StringXML() string
```

That is the point.

## The Problem

LLMs work best when they can see a whole codebase at once. Most codebases are too large. `find`, `tree`, and `ls -R` hand the model noise. A full `cat **/*.go` blows the context window in one shot.

You want the skeleton. Every package. Every exported symbol. Nothing else.

## The Shape

repomap runs a five-stage pipeline on your project root:

```
scan → parse → rank → budget → format
```

**Scan** walks `git ls-files` and detects languages.
**Parse** extracts symbols — `go/ast` for Go, tree-sitter for seven other languages, regex where neither reaches.
**Rank** scores files: entry points float, leaves sink.
**Budget** trims to fit a token ceiling you pass in.
**Format** prints compact Markdown, verbose text, source lines, or structured XML.

The output fits in a prompt. The prompt fits in the model. The model reads the repository.

## Install

```bash
go install github.com/dotcommander/repomap/cmd/repomap@latest
```

Or clone and build:

```bash
git clone https://github.com/dotcommander/repomap
cd repomap
go build -o repomap ./cmd/repomap
```

## Use

```bash
repomap                             # current directory, 2048 tokens, enriched default
repomap ./src                       # target a subtree
repomap -t 4096                     # wider budget
repomap -f compact                  # lean orientation: file paths + symbol names only
repomap -f verbose                  # every symbol, no summarization
repomap -f detail                   # signatures and struct fields (same as default, no budget cap)
repomap -f lines                    # actual source lines, not summaries
repomap -f xml                      # structured output for programmatic consumers
repomap --json                      # JSON envelope {schema_version:1, lines:[...]}
repomap --json --json-legacy        # bare []string JSON (pre-v0.7.0 compat)
```

### Output formats

| Format | What it shows | Budget enforced |
|--------|---------------|----------------|
| *(default)* | Paths, exported symbol names, typed signatures, godoc first sentence, typed struct fields | Yes |
| `compact` | Paths + exported symbol names only — lean orientation mode | Yes |
| `verbose` | All symbols, names only, no summarization | No |
| `detail` | All symbols, signatures, and struct fields | No |
| `lines` | Actual source lines | No |
| `xml` | Structured XML for programmatic consumers | No |

**Budget guarantee**: repomap never truncates a file mid-symbol. Files either render fully at their assigned detail level or drop to a lower level or are omitted — token budget is honored exactly. The footer reports how many files were omitted.

## Quickstart

```bash
repomap init                        # scaffold .repomap.yaml + post-commit hook
repomap init --no-hook              # config only
repomap init --force                # overwrite existing
```

`repomap init` writes `.repomap.yaml` at the project root and installs a
git `post-commit` hook that refreshes the cache in the background after
every commit, so `repomap` stays instant. Idempotent — re-running without
`--force` skips existing files.

## Languages

Go, PHP, Python, Rust, TypeScript, JavaScript, Java, C, C++, Ruby, HTML, CSS. Go parses through `go/ast` directly; PHP parses through tree-sitter with full 8.x coverage (signatures, visibility, constructor property promotion, PHPDoc). The rest go through tree-sitter when the grammar is present, ctags when it is not, regex when neither exists. Quality degrades gracefully; the map is never empty because a parser was missing.

## Configuration

Create `.repomap.yaml` at the project root to filter symbols from the output:

```yaml
method_blocklist:
  - "Test*"           # glob: drop anything starting with "Test"
  - "*Mock"           # glob: drop anything ending in "Mock"
  - "/^pb_/"          # regex: drop generated protobuf methods
```

Patterns wrapped in `/.../` are Go regex; others are `path.Match` globs.
Absent file = no filtering.

## Library

```go
import "github.com/dotcommander/repomap"

m := repomap.New(".", repomap.Config{MaxTokens: 2048})
if err := m.Build(context.Background()); err != nil {
    return err
}
fmt.Print(m.String())
```

Every format has a method: `String`, `StringCompact`, `StringVerbose`, `StringDetail`, `StringLines`, `StringXML`. Results cache per format; call them as often as you like.

`String()` is the enriched default: full typed signatures, godoc first sentences, and typed struct fields for exported symbols, within the token budget. `StringCompact()` is the lean orientation mode — symbol names only, no signatures or docs.

`m.Stale()` reports whether source files have changed since the last build. Rebuild when it returns true.

## Design

One package. No internal taxonomy of subpackages. Files named after what they do — `scanner.go`, `ranker.go`, `budget.go`, `render.go`. If a thing has a name, the file has that name. If a thing has a pipeline stage, the file has that stage.

Docs live in `docs/`. Read them in the order they're numbered.

## Acknowledgments

The repository map concept was pioneered by [aider.chat](https://aider.chat/) — an AI pair programming tool that introduced the idea of distilling a codebase into a compact symbol map that fits in an LLM context window.

## License

[MIT](LICENSE)
