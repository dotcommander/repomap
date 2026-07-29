#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 /absolute/path/to/repomap /absolute/path/to/checkout" >&2
  exit 2
fi

binary=$1
checkout=$2
case "$binary:$checkout" in
  /*:/*) ;;
  *) echo "binary and checkout must be absolute paths" >&2; exit 2 ;;
esac

qa_tmp=$(mktemp -d "${TMPDIR:-/tmp}/repomap-cli-matrix.XXXXXX")
trap 'rm -rf "$qa_tmp"' EXIT HUP INT TERM
fixture="$qa_tmp/fixture"
cache_dir="$qa_tmp/cache"
mkdir -p "$fixture" "$cache_dir" "$qa_tmp/bin"

cat >"$fixture/go.mod" <<'EOF'
module example.com/fixture

go 1.26
EOF
cat >"$fixture/main.go" <<'EOF'
package fixture

func Hello() string { return "ok" }
EOF
git -C "$fixture" init -q
git -C "$fixture" config user.email qa@example.invalid
git -C "$fixture" config user.name "Repomap QA"
git -C "$fixture" add go.mod main.go
git -C "$fixture" commit -qm initial

cat >"$qa_tmp/bin/gh" <<'EOF'
#!/bin/sh
case "$*" in
  "auth status") exit 1 ;;
  *) echo "gh stub: unsupported invocation: $*" >&2; exit 2 ;;
esac
EOF
chmod 0755 "$qa_tmp/bin/gh"

check_json() {
  if command -v jq >/dev/null 2>&1; then
    jq . >/dev/null
  else
    python3 -m json.tool >/dev/null
  fi
}

expect_failure() {
  if "$@" >"$qa_tmp/unexpected.stdout" 2>"$qa_tmp/expected.stderr"; then
    echo "expected failure but command succeeded: $*" >&2
    exit 1
  fi
  test ! -s "$qa_tmp/unexpected.stdout"
}

echo "matrix: help"
"$binary" --help >/dev/null
for parent in audit cache commit lsp; do
  "$binary" "$parent" --help >/dev/null
  "$binary" help "$parent" >/dev/null
done
while IFS= read -r leaf; do
  "$binary" $leaf --help >/dev/null
  "$binary" help $leaf >/dev/null
done <<'EOF'
brief
task
audit hygiene
audit brief
audit risks
audit surface
audit effects
cache status
cache warm
find
impact
inventory
context
endpoint
explain
orphans
init
lsp status
refs
def
hover
symbols
serve
commit analyze
commit execute
commit prep
commit finish
commit auto
commit-preflight
EOF

echo "matrix: map and validation"
for format in enriched compact verbose detail lines xml; do
  "$binary" --format "$format" --tokens 64 "$fixture" >"$qa_tmp/map-$format.out"
  test -s "$qa_tmp/map-$format.out"
  test "$(wc -c <"$qa_tmp/map-$format.out" | tr -d ' ')" -le 256
done
"$binary" --tokens 256 --json "$fixture" >"$qa_tmp/map.json"
test "$(wc -c <"$qa_tmp/map.json" | tr -d ' ')" -le 1024
check_json <"$qa_tmp/map.json"
"$binary" --tokens 256 --json --json-legacy "$fixture" | check_json
"$binary" --tokens 256 --json-structured "$fixture" | check_json
expect_failure "$binary" --json-legacy "$fixture"
expect_failure "$binary" --json --json-structured "$fixture"
expect_failure "$binary" --tokens 0 "$fixture"
expect_failure "$binary" --calls-use-binary "$fixture"

artifact="$qa_tmp/map.md"
printf 'old\n' >"$artifact"
chmod 0600 "$artifact"
"$binary" --artifact "$artifact" "$fixture"
test "$(stat -f '%Lp' "$artifact")" = "600"
test -s "$artifact"

echo "matrix: task"
"$binary" task --tokens 512 "update Hello behavior" "$fixture" >"$qa_tmp/task.txt"
test -s "$qa_tmp/task.txt"
test "$(wc -c <"$qa_tmp/task.txt" | tr -d ' ')" -le 2048
"$binary" task --tokens 512 --json "update Hello behavior" "$fixture" >"$qa_tmp/task.json"
test "$(wc -c <"$qa_tmp/task.json" | tr -d ' ')" -le 2048
check_json <"$qa_tmp/task.json"
"$binary" task --tokens 512 --json --consumed=main.go "update Hello behavior" "$fixture" | check_json
expect_failure "$binary" task "" "$fixture"
expect_failure "$binary" task --tokens 0 "update Hello behavior" "$fixture"
expect_failure "$binary" task --consumed=../outside.go "update Hello behavior" "$fixture"
task_artifact="$qa_tmp/task-artifact.json"
"$binary" --artifact "$task_artifact" task --tokens 512 --json "update Hello behavior" "$fixture"
check_json <"$task_artifact"

echo "matrix: audit and cache"
"$binary" brief "$fixture" >/dev/null
"$binary" audit hygiene --json "$fixture" | check_json
"$binary" audit brief --json --limit 1 "$fixture" | check_json
"$binary" audit risks --json --limit 1 "$fixture" | check_json
"$binary" audit surface --json --limit 1 "$checkout" | check_json
"$binary" audit effects --json --limit 1 "$fixture" | check_json
"$binary" audit effects --kind filesystem-write --paths-only "$checkout" >/dev/null
"$binary" cache status --cache-dir "$cache_dir" --json "$fixture" | check_json
"$binary" cache warm --cache-dir "$cache_dir" "$fixture" >/dev/null
"$binary" cache status --cache-dir "$cache_dir" "$fixture" | grep -q 'fresh'
"$binary" cache status --cache-dir "$cache_dir" --json "$fixture" | check_json

echo "matrix: queries and lsp"
echo "matrix: find"
"$binary" find AuditBrief --limit 1 "$checkout" | grep -q AuditBrief
"$binary" find NoSuchSymbol --format json "$checkout" | check_json
"$binary" find NoSuchSymbol "$checkout" | grep -q 'no symbols found'
echo "matrix: impact"
"$binary" impact "$fixture/main.go" >/dev/null
"$binary" impact --json "$fixture/main.go" | check_json
echo "matrix: inventory-context-endpoint-explain-init-status"
echo "matrix: inventory"
"$binary" inventory --boundary Postgres --json "$fixture" | check_json
echo "matrix: context"
"$binary" context AuditBrief --max-source-lines 1 --json "$checkout" | check_json
echo "matrix: endpoint"
"$binary" endpoint --json "$checkout" | check_json
echo "matrix: explain"
"$binary" explain --json "$fixture/main.go" | check_json
echo "matrix: init"
"$binary" init --no-hook "$fixture" >/dev/null
echo "matrix: lsp status"
"$binary" lsp status --json "$fixture" | check_json

if command -v gopls >/dev/null 2>&1; then
  echo "matrix: gopls queries"
  "$binary" symbols --json "$fixture/main.go" | check_json
  "$binary" refs --json "$fixture/main.go" 3 Hello | check_json
  "$binary" def --json "$fixture/main.go" 3 Hello | check_json
  "$binary" hover --json "$fixture/main.go" 3 Hello | check_json
  "$binary" orphans --json "$fixture" | check_json
fi

echo "matrix: serve"
cat <<'EOF' | "$binary" serve "$fixture" 2>"$qa_tmp/serve.stderr" >"$qa_tmp/serve.ndjson"
{"jsonrpc":"2.0","id":1,"method":"map/status","params":{}}
{"jsonrpc":"2.0","id":2,"method":"map/render","params":{"format":"compact"}}
{"jsonrpc":"2.0","id":3,"method":"symbol/find","params":{"query":"Hello"}}
{"jsonrpc":"2.0","id":4,"method":"file/explain","params":{"path":"main.go"}}
{"jsonrpc":"2.0","id":5,"method":"file/context","params":{"query":"Hello","max_source_lines":1}}
EOF
test "$(wc -l <"$qa_tmp/serve.ndjson" | tr -d ' ')" = "5"
while IFS= read -r response; do
  printf '%s\n' "$response" | check_json
done <"$qa_tmp/serve.ndjson"

echo "matrix: commit dry run"
printf '\n// matrix change\n' >>"$fixture/main.go"
echo "matrix: commit analyze"
"$binary" commit analyze --tmpdir "$qa_tmp" "$fixture" >"$qa_tmp/plan.json"
check_json <"$qa_tmp/plan.json"
echo "matrix: commit execute"
"$binary" commit execute --plan-file "$qa_tmp/plan.json" --dry-run --json | check_json
echo "matrix: commit preflight"
(
  cd "$fixture"
  PATH="$qa_tmp/bin:$PATH" "$binary" commit-preflight >/dev/null
)

echo "PASS: 30 leaves, help aliases, formats, validation, task, artifact, audit, cache, query, LSP, serve, and dry-run commit surfaces"
