# Languages

repomap parses source files in three tiers. It tries the best parser first and falls back gracefully.

## The tiers

| Tier | Parser | Fidelity |
| --- | --- | --- |
| 1 | `go/ast` for Go, tree-sitter for seven others | High |
| 2 | ctags (if installed) | Medium |
| 3 | Regex | Low but always available |

A file never fails to parse. Worst case, regex finds function and type declarations and the ranker takes it from there.

## Go

Parsed with `go/ast` directly. No fallback needed — the standard library ships the parser.

Extracts:

- Functions, methods (with receiver), signatures, parameter and return counts
- Structs, interfaces, type aliases, generic constraints
- Constants, variables (package-level only)
- Imports (for the dependency graph)
- Package name and import path (from `go.mod`)

Every Go file gets `parsed="ast"` in XML output.

## Tree-sitter supported

Tree-sitter grammars ship with repomap for:

- Python
- Rust
- TypeScript, JavaScript, JSX, TSX
- Java
- C, C++
- HTML, CSS

If tree-sitter is available at build time, these languages get `parsed="treesitter"`. If not, they drop to ctags or regex.

## ctags fallback

When tree-sitter doesn't cover a language and `ctags` is on `$PATH`, repomap runs it once per build and parses the tag file.

Install universal-ctags:

```bash
brew install universal-ctags    # macOS
apt install universal-ctags     # Debian/Ubuntu
```

Files parsed this way get `parsed="ctags"`.

## Regex fallback

Always present. Pattern-matches common declarations for:

- Ruby (`def`, `class`, `module`)
- PHP (`function`, `class`, `interface`)
- Shell (`function`, top-level `name() {}`)
- Lua, Perl, Elixir, and anything else with a declaration keyword

Noisy — a comment that looks like a function declaration will produce a false symbol — but cheap and language-independent. Files parsed this way get `parsed="regex"`.

## Adding a language

1. Add an entry to `languageDefs` in `language.go` with the extension and language ID.
2. Optionally add a tree-sitter registration: `parser_ts_<lang>.go`.
3. Optionally add regex patterns: extend `parser_generic.go`.

The scanner, ranker, budget, and formatter don't need changes. They're language-agnostic.

## What gets skipped

The scanner excludes:

- `vendor/`, `node_modules/`, `.git/`, `dist/`, `build/`, `target/`
- Files over 50 KB
- Binary files (detected by null bytes in the first 512 bytes)
- Files that don't match a known language

Everything else is candidate input.

## Detecting availability

From Go:

```go
repomap.TreeSitterAvailable()   // bool
repomap.CtagsAvailable()        // bool
```

Both return once; results are cached for the process lifetime.

## Next

- [Architecture](09-architecture.md) — the whole pipeline
- [Troubleshooting](10-troubleshooting.md) — when things look wrong
