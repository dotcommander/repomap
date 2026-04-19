# Changelog

All notable changes to repomap are documented here.

---

## v0.8.0 — 2026-04-18

### PHP parity with Go

PHP files now render at the same fidelity as Go: full signatures with visibility, types, defaults; class headers with `extends` / `implements`; properties and constants visible; PHPDoc first sentences inlined.

- **Tree-sitter PHP parser** replacing the regex fallback. Covers PHP 8.x grammar: classes, interfaces, traits, enums (including backed enums and cases), functions, methods, properties, constants, namespaces.
- **Visibility-in-signature** — `public function foo(): string`, `private readonly LoggerInterface $logger`. No schema change; visibility lives where it renders.
- **Constructor property promotion** extracted as real properties — `private readonly LoggerInterface $logger = new NullLogger()` appears in the property list, signatures byte-identical to non-promoted declarations.
- **PHPDoc extraction** — first sentence of `/** */` blocks rendered as subtitle; @-tags stripped. No `[doc: n/a]` noise on PHP files.
- **Kind-weighted ordering** for PHP: class/interface first, then trait, enum, function, method, case, property, const.

Before (v0.7.0) vs after (v0.8.0) on LLPhant `src/Chat/OpenAIChat.php`:

```
# v0.7.0
src/Chat/OpenAIChat.php [untested] [doc: n/a]
  func __construct
  func generateText
  func getLastResponse
  type OpenAIChat

# v0.8.0
src/Chat/OpenAIChat.php [untested]
  class OpenAIChat implements ChatInterface [440L]
  public function __construct(OpenAIConfig $config = new OpenAIConfig(), private readonly LoggerInterface $logger = new NullLogger())
  public function generateText(string $prompt): string
  public function getLastResponse(): ?CreateResponse
  public ?FunctionInfo $lastFunctionCalled = null
```

Symbol count on the same codebase: 968 → 1398 (+44%) within the same 2048-token budget.

---

## v0.7.0 — 2026-04-18

### LLM-first output quality

Default output is now richer within the same token budget. An LLM reading `repomap .` no longer needs to open source files to generate correct call sites.

- **Full typed Go signatures** — `func Foo(ctx context.Context, id int) (*User, error)` instead of `func Foo`. The 40-char truncation cap is removed.
- **Typed struct fields** — `{Name string, ID int}` inline for exported structs instead of bare field names.
- **Leading godoc sentence** inlined after each exported symbol: `// BudgetFiles assigns a DetailLevel to each RankedFile within the token budget.`
- **`[doc: n/a]` tag** on file header lines for non-Go files — explicit signal that the language tier does not extract doc comments (not that docs are absent).
- **Kind-weighted symbol ordering** within each file block: structs and interfaces first, then types, functions, methods, constants, and vars. Highest architectural signal at the top.

### New

- **`-f compact` format** — lean orientation mode: file paths + exported symbol names, no signatures, no godoc, no struct fields. Use for first-pass codebase scans when you need inventory without detail. The old default (category summaries) is replaced by this named mode.
- **JSON schema envelope** — `--json` now emits `{"schema_version": 1, "lines": [...]}` instead of a bare `[]string`. Downstream consumers should parse `.lines`. Use `--json-legacy` to get the pre-v0.7.0 bare array for scripts that cannot be updated immediately.

### Fixed

- **All-or-nothing per-file budget invariant** — files never truncate mid-symbol. Fallback chain on budget pressure: full enriched rendering (level 2) → summary (level 1) → omit. A half-shown file is worse than an omitted one: the footer now reports `(N files omitted — increase -t or use -f compact)`.
- 5 `nilerr` bugs where `return nil` silently swallowed non-nil errors: `inventory_scan.go` (×3), `init.go` (×1), `gitstate.go` (×1).
- 3 `pw.Close()` error drops in `calls.go` that could hang a reader goroutine.

### Breaking

- **Default JSON output changed from `[...]` to `{"schema_version":1,"lines":[...]}`.** Consumers must parse `.lines`. Use `--json-legacy` for bare-array compatibility.
- **Default non-JSON output is richer** (signatures, godoc, typed struct fields). Scripts asserting exact default output will need updating. To get the old terse output, use `-f compact`.
