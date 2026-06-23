# Changelog

All notable changes to repomap are documented here.

---

## v0.16.0 — 2026-06-22

### Changes

- **`repomap brief` map is leaner** — the embedded repository map is now capped to the top-ranked files (top 20) with an honest `+N more files — run `repomap` for the full map` footer, instead of dumping every file. The uniform `imported by N` annotation is suppressed when every shown file shares the same importer count (the single-package degenerate case where that metric carries no per-file signal), roughly halving the digest length while preserving the high-signal header (Verify / State / Rules / Flow / Dependencies).

### API

- **`Map.StringBriefMap(maxFiles int) (body string, total int)`** — renders the enriched map for the top `maxFiles` ranked files and reports the total ranked-file count, for callers building bounded digests.

---

## v0.15.0 — 2026-06-22

### Features

- **`repomap brief`** — new top-level agent boot digest command. Prints a time-aware greeting, a project-specific Verify chain (`build`/`test`/`vet`, plus `lint` only when a golangci config is present), a State section (branch, dirty-file count with the changed paths, recent commit subjects), a Rules section flagging agent-convention docs such as `CLAUDE.md`, and a warning when an active `.repomap.yaml` is filtering the map — followed by the enriched repository map. Makefile/justfile targets are verified to exist before being advertised.
- **`[dead]` annotation** — exported symbols with no detected references are marked `[dead]` in the default output.
- **Richer level-1 summaries** — summary-level file blocks now list their top exported symbol names instead of bare counts.
- **Per-symbol content hashing** — `--json-structured` emits a per-symbol content hash so consumers can detect which individual symbols changed; cache version bumped to 9.
- **audit** — extract job, model, and policy framework-role surfaces.

---

## v0.14.0 — 2026-06-14

### Features

- audit: every audit packet (`risks`, `surface`, `effects`, `brief`) is self-describing at `schema_version` 2 — a stable `id` for citation (e.g. `repomap:risk:<path>`), an `evidence_class` (`import_graph`/`ast`/`git_history`/`heuristic`) with a derived `confidence` tier, a per-file `verify_cmd` for Go targets, and an external-consumer `caveat` (capped to `low` confidence) on signals blind to out-of-repo callers such as dead code and untested exports.
- audit: empty file lists serialize as `[]` instead of `null` and carry a `files_omitted_reason`; truncated packets report an `omitted_reason`. Additive and backward-compatible apart from the `null`→`[]` fix.
- structured JSON: add top-level parser coverage metrics and per-file relation evidence so consumers can distinguish exact Go import graph signals from heuristic non-Go basename and symbol-reference signals.

---

## v0.13.1 — 2026-06-13

### Fixes

- commit: collapse finding placeholder replacement into a focused helper with coverage for generic project paths and placeholder-shaped test credentials.

---

## v0.13.0 — 2026-06-13

### Features

- audit: `audit brief --json` now emits a bounded `review_plan` projecting the first-read queue into per-lane review obligations (files, gates, suggested verify commands, why). Go-specific verify commands appear only when Go sources are detected. Derived deterministically from existing packets; additive and backward-compatible.
- serve: stdio JSON-RPC 2.0 server for warm map queries (NDJSON; map/render, map/status, symbol/find, file/explain, file/context)

---

## v0.12.0 — 2026-05-29

### Features

- Add cache and context inspection subcommands.

### Fixes

- Preserve exported API compatibility while keeping context-aware cache and commit-prep internals.
- Serialize LSP startup retries after failed language-server launches.
- Unwrap commit execute exit codes with `errors.As`.

### Other

- Document cache, context, and `--json-structured` usage.
- Add CLI cache/explain coverage and modernize test loops.
- Extract commit finish I/O helpers and simplify commit flow.
- Clean up incremental cache and commit-prep internals.

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
