#!/bin/sh
set -eu

runner_version=1
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)

usage() {
  echo "usage: $0" >&2
  exit 2
}

[ "$#" -eq 0 ] || usage

testing=${REPOMAP_QA_TESTING:-0}
case "$testing" in
  0|1) ;;
  *)
    echo "REPOMAP_QA_TESTING must be 0 or 1" >&2
    exit 2
    ;;
esac

if [ "$testing" -ne 1 ] &&
  { [ "${REPOMAP_QA_TEST_BIN_DIR+x}" = x ] ||
    [ "${REPOMAP_QA_TEST_OUTPUT_DIR+x}" = x ]; }; then
  echo "test overrides require REPOMAP_QA_TESTING=1" >&2
  exit 2
fi

validate_test_directory() {
  directory=$1
  label=$2
  case "$directory" in
    /*) ;;
    *)
      echo "$label must be absolute" >&2
      exit 2
      ;;
  esac
  if [ ! -d "$directory" ] || [ -L "$directory" ]; then
    echo "$label must be an existing non-symlinked directory" >&2
    exit 2
  fi
}

if [ "$testing" -eq 1 ]; then
  test_bin_dir=${REPOMAP_QA_TEST_BIN_DIR:-}
  output_root=${REPOMAP_QA_TEST_OUTPUT_DIR:-}
  validate_test_directory "$test_bin_dir" REPOMAP_QA_TEST_BIN_DIR
  validate_test_directory "$output_root" REPOMAP_QA_TEST_OUTPUT_DIR
else
  test_bin_dir=
  output_root=$script_dir
fi

canonical_report=$output_root/repomap_e2e_qa_report.md
latest_dir=$output_root/latest
owner_marker=.repomap-qa-owned
run_tmp=
stage=
latest_backup=
publication_started=0
report_tmp=
gate_pid=
mv_cmd=

remove_owned_directory() {
  target=$1
  [ -d "$target" ] || return 0
  if [ ! -f "$target/$owner_marker" ]; then
    echo "refusing to remove unowned directory: $target" >&2
    return 1
  fi
  rm -rf "$target"
}

rollback_publication() {
  if [ "$publication_started" -eq 1 ]; then
    if [ -n "$report_tmp" ] && [ ! -e "$report_tmp" ]; then
      # The report rename is the commit point for the report and latest pair.
      publication_started=0
      return
    fi
    if [ -n "$stage" ] && [ ! -e "$stage" ]; then
      remove_owned_directory "$latest_dir" || true
    fi
    if [ -n "$latest_backup" ] && [ -d "$latest_backup" ] &&
      [ ! -e "$latest_dir" ] && [ ! -L "$latest_dir" ]; then
      "$mv_cmd" "$latest_backup" "$latest_dir" || true
    fi
    publication_started=0
  fi
}

cleanup() {
  rollback_publication
  if [ -n "$run_tmp" ] && [ -d "$run_tmp" ]; then
    remove_owned_directory "$run_tmp" || true
  fi
}

on_signal() {
  trap - 0 HUP INT TERM
  if [ -n "$gate_pid" ]; then
    kill -TERM "$gate_pid" 2>/dev/null || true
    wait "$gate_pid" 2>/dev/null || true
    gate_pid=
  fi
  cleanup
  exit 130
}

trap cleanup 0
trap on_signal HUP INT TERM

resolve_required_tool() {
  tool_name=$1
  if [ "$testing" -eq 1 ]; then
    candidate=$test_bin_dir/$tool_name
    [ -x "$candidate" ] && printf '%s\n' "$candidate"
  else
    command -v "$tool_name" 2>/dev/null || true
  fi
  return 0
}

resolve_optional_override() {
  tool_name=$1
  if [ "$testing" -eq 1 ] && [ -x "$test_bin_dir/$tool_name" ]; then
    printf '%s\n' "$test_bin_dir/$tool_name"
  else
    command -v "$tool_name" 2>/dev/null || true
  fi
}

git_cmd=$(resolve_required_tool git)
go_cmd=$(resolve_required_tool go)
lint_cmd=$(resolve_required_tool golangci-lint)
gopls_cmd=$(resolve_required_tool gopls)
mv_cmd=$(resolve_optional_override mv)

if [ -z "$mv_cmd" ]; then
  echo "required system utility is missing: mv" >&2
  exit 2
fi

sha256_file() {
  target=$1
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$target" | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$target" | awk '{print $1}'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$target" | sed 's/^.*= //'
  else
    return 1
  fi
}

if ! sha256_file "$0" >/dev/null 2>&1; then
  echo "no supported SHA-256 utility found" >&2
  exit 2
fi

if [ -L "$latest_dir" ]; then
  echo "refusing symlinked latest-run directory: $latest_dir" >&2
  exit 2
fi
if [ -e "$latest_dir" ] &&
  { [ ! -d "$latest_dir" ] || [ ! -f "$latest_dir/$owner_marker" ]; }; then
  echo "refusing to replace unowned latest-run path: $latest_dir" >&2
  exit 2
fi
if [ -L "$canonical_report" ]; then
  echo "refusing to replace symlinked report: $canonical_report" >&2
  exit 2
fi
if [ -e "$canonical_report" ] && [ ! -f "$canonical_report" ]; then
  echo "refusing to replace non-regular report: $canonical_report" >&2
  exit 2
fi

run_tmp=$(mktemp -d "$output_root/.repomap-e2e.XXXXXX")
: >"$run_tmp/$owner_marker"
stage=$run_tmp/latest
mkdir "$stage"
: >"$stage/$owner_marker"

cd "$repo_root"
started_at_utc=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
any_failed=0
any_incomplete=0
passed_count=0
gate_count=0

quote_argv() {
  separator=
  for argument do
    if [ -n "$separator" ]; then
      printf ' '
    fi
    printf "'"
    printf '%s' "$argument" | sed "s/'/'\\\\''/g"
    printf "'"
    separator=1
  done
  printf '\n'
}

record_gate_metadata() {
  gate=$1
  gate_status=$2
  gate_exit=$3
  gate_duration=$4
  printf '%s\n' "$gate_status" >"$stage/$gate.status"
  printf '%s\n' "$gate_exit" >"$stage/$gate.exit"
  printf '%s\n' "$gate_duration" >"$stage/$gate.duration_ms"
  sha256_file "$stage/$gate.stdout" >"$stage/$gate.stdout.sha256"
  sha256_file "$stage/$gate.stderr" >"$stage/$gate.stderr.sha256"
}

run_gate() {
  gate=$1
  shift
  gate_count=$((gate_count + 1))
  quote_argv "$@" >"$stage/$gate.argv"
  echo "qa-e2e: running $gate" >&2
  start_seconds=$(date +%s)
  "$@" >"$stage/$gate.stdout" 2>"$stage/$gate.stderr" &
  gate_pid=$!
  set +e
  wait "$gate_pid"
  gate_exit=$?
  set -e
  gate_pid=
  end_seconds=$(date +%s)
  gate_duration=$(((end_seconds - start_seconds) * 1000))
  if [ "$gate_exit" -eq 0 ]; then
    gate_status=PASS
    passed_count=$((passed_count + 1))
  else
    gate_status=FAIL
    any_failed=1
  fi
  record_gate_metadata "$gate" "$gate_status" "$gate_exit" "$gate_duration"
}

block_gate() {
  gate=$1
  reason=$2
  shift 2
  gate_count=$((gate_count + 1))
  quote_argv "$@" >"$stage/$gate.argv"
  : >"$stage/$gate.stdout"
  printf '%s\n' "$reason" >"$stage/$gate.stderr"
  record_gate_metadata "$gate" BLOCKED not-run 0
  any_incomplete=1
}

if [ -n "$git_cmd" ]; then
  if ! "$git_cmd" rev-parse HEAD >"$stage/git-head.txt" 2>"$stage/git-head.stderr"; then
    printf '%s\n' UNKNOWN >"$stage/git-head.txt"
    any_incomplete=1
  fi
  if ! "$git_cmd" status --porcelain=v1 -z --untracked-files=all \
    >"$stage/source-status.raw" 2>"$stage/source-status.stderr"; then
    : >"$stage/source-status.raw"
    any_incomplete=1
    source_state=UNKNOWN
  elif [ -s "$stage/source-status.raw" ]; then
    source_state=DIRTY
  else
    source_state=CLEAN
  fi
  if ! "$git_cmd" -c core.quotePath=true status --porcelain=v1 \
    --untracked-files=all >"$stage/dirty-paths.txt" \
    2>"$stage/dirty-paths.stderr"; then
    printf '%s\n' '<unavailable>' >"$stage/dirty-paths.txt"
    any_incomplete=1
  elif [ ! -s "$stage/dirty-paths.txt" ]; then
    printf '%s\n' '<none>' >"$stage/dirty-paths.txt"
  fi
else
  printf '%s\n' UNKNOWN >"$stage/git-head.txt"
  : >"$stage/source-status.raw"
  printf '%s\n' '<unavailable: git missing>' >"$stage/dirty-paths.txt"
  : >"$stage/git-head.stderr"
  : >"$stage/source-status.stderr"
  : >"$stage/dirty-paths.stderr"
  source_state=UNKNOWN
  any_incomplete=1
fi

if [ -n "$go_cmd" ]; then
  "$go_cmd" version >"$stage/go-version.txt" 2>"$stage/go-version.stderr" ||
    any_incomplete=1
else
  printf '%s\n' MISSING >"$stage/go-version.txt"
  : >"$stage/go-version.stderr"
  any_incomplete=1
fi
if [ -n "$lint_cmd" ]; then
  "$lint_cmd" version >"$stage/golangci-lint-version.txt" \
    2>"$stage/golangci-lint-version.stderr" || any_incomplete=1
else
  printf '%s\n' MISSING >"$stage/golangci-lint-version.txt"
  : >"$stage/golangci-lint-version.stderr"
  any_incomplete=1
fi
if [ -n "$gopls_cmd" ]; then
  "$gopls_cmd" version >"$stage/gopls-version.txt" \
    2>"$stage/gopls-version.stderr" || any_incomplete=1
else
  printf '%s\n' MISSING >"$stage/gopls-version.txt"
  : >"$stage/gopls-version.stderr"
  any_incomplete=1
fi

binary=$run_tmp/repomap
focused_pattern='TestCLIModelMatrixCoversEveryExecutableLeafAndFlag|TestVisibleCLIFlagsAppearInHelpAndConfigurationDocs|TestQALedgerAndFrozenHTMLStayInSync'

if [ -n "$go_cmd" ]; then
  run_gate build "$go_cmd" build -o "$binary" ./cmd/repomap
  run_gate cli_model "$go_cmd" test ./internal/cli -run "$focused_pattern"
  run_gate vet "$go_cmd" vet ./...
else
  block_gate build 'go is required' go build -o "$binary" ./cmd/repomap
  block_gate cli_model 'go is required' go test ./internal/cli -run "$focused_pattern"
  block_gate vet 'go is required' go vet ./...
fi

if [ -n "$lint_cmd" ]; then
  run_gate lint "$lint_cmd" run ./...
else
  block_gate lint 'golangci-lint is required' golangci-lint run ./...
fi

if [ -n "$go_cmd" ]; then
  run_gate test "$go_cmd" test ./...
  run_gate race "$go_cmd" test -race -short ./...
  run_gate mod_verify "$go_cmd" mod verify
else
  block_gate test 'go is required' go test ./...
  block_gate race 'go is required' go test -race -short ./...
  block_gate mod_verify 'go is required' go mod verify
fi

build_status=$(cat "$stage/build.status")
if [ "$build_status" != PASS ]; then
  block_gate cli_matrix 'the built Repomap binary is unavailable' \
    env REPOMAP_QA_REQUIRE_GOPLS=1 sh "$script_dir/run-cli-matrix.sh" \
    "$binary" "$repo_root"
elif [ -z "$git_cmd" ]; then
  block_gate cli_matrix 'git is required by the CLI matrix' \
    env REPOMAP_QA_REQUIRE_GOPLS=1 sh "$script_dir/run-cli-matrix.sh" \
    "$binary" "$repo_root"
elif [ -z "$gopls_cmd" ]; then
  block_gate cli_matrix 'gopls is required by the CLI matrix' \
    env REPOMAP_QA_REQUIRE_GOPLS=1 sh "$script_dir/run-cli-matrix.sh" \
    "$binary" "$repo_root"
elif [ "$testing" -eq 1 ]; then
  matrix_override=$test_bin_dir/repomap-qa-cli-matrix
  if [ -x "$matrix_override" ]; then
    run_gate cli_matrix env REPOMAP_QA_REQUIRE_GOPLS=1 \
      "$matrix_override" "$binary" "$repo_root"
  else
    block_gate cli_matrix 'test CLI matrix override is missing' \
      env REPOMAP_QA_REQUIRE_GOPLS=1 repomap-qa-cli-matrix \
      "$binary" "$repo_root"
  fi
else
  run_gate cli_matrix env REPOMAP_QA_REQUIRE_GOPLS=1 \
    sh "$script_dir/run-cli-matrix.sh" "$binary" "$repo_root"
fi

if [ "$build_status" = PASS ]; then
  if ! sha256_file "$binary" >"$stage/binary.sha256"; then
    printf '%s\n' MISSING >"$stage/binary.sha256"
    any_incomplete=1
  fi
  if ! "$go_cmd" version -m "$binary" >"$stage/binary-build-info.txt" \
    2>"$stage/binary-build-info.stderr"; then
    printf '%s\n' '<unavailable>' >"$stage/binary-build-info.txt"
    any_incomplete=1
  fi
else
  printf '%s\n' MISSING >"$stage/binary.sha256"
  printf '%s\n' '<unavailable: build gate did not pass>' \
    >"$stage/binary-build-info.txt"
  : >"$stage/binary-build-info.stderr"
  any_incomplete=1
fi

if [ "$gate_count" -ne 8 ]; then
  any_incomplete=1
fi
if [ "$any_failed" -eq 1 ]; then
  final_status=FAIL
  final_exit=1
elif [ "$any_incomplete" -eq 1 ] || [ "$passed_count" -ne 8 ]; then
  final_status=INCOMPLETE
  final_exit=2
else
  final_status=PASS
  final_exit=0
fi

git_head=$(sed -n '1p' "$stage/git-head.txt")
go_version=$(sed -n '1p' "$stage/go-version.txt")
lint_version=$(sed -n '1p' "$stage/golangci-lint-version.txt")
gopls_version=$(sed -n '1p' "$stage/gopls-version.txt")
binary_sha256=$(sed -n '1p' "$stage/binary.sha256")
source_status_sha256=$(sha256_file "$stage/source-status.raw")
report_tmp=$run_tmp/report.md

render_report() {
  cat <<EOF
# Repomap End-to-End QA Evidence
Status: $final_status
source_state: $source_state
git_head: $git_head
binary_sha256: $binary_sha256
runner_version: $runner_version

## Run Summary

- started_at_utc: \`$started_at_utc\`
- status: \`$final_status\`
- required_gates: \`8\`
- passed_gates: \`$passed_count\`
- source_state: \`$source_state\`

## Source and Binary Provenance

- git_head: \`$git_head\`
- source_state: \`$source_state\`
- source_status_raw_sha256: \`$source_status_sha256\`
- go_version: \`$go_version\`
- golangci_lint_version: \`$lint_version\`
- gopls_version: \`$gopls_version\`
- binary_sha256: \`$binary_sha256\`
- runner_version: \`$runner_version\`

dirty_paths:

\`\`\`text
EOF
  cat "$stage/dirty-paths.txt"
  cat <<'EOF'
```

binary_build_info:

```text
EOF
  cat "$stage/binary-build-info.txt"
  cat <<'EOF'
```

## Gate Results

| Gate | Result | Exact argv | Exit | Duration ms | stdout_sha256 | stderr_sha256 |
|---|---|---|---:|---:|---|---|
EOF
  for gate in build cli_model vet lint test race mod_verify cli_matrix; do
    gate_status=$(cat "$stage/$gate.status")
    gate_argv=$(cat "$stage/$gate.argv")
    gate_exit=$(cat "$stage/$gate.exit")
    gate_duration=$(cat "$stage/$gate.duration_ms")
    stdout_digest=$(cat "$stage/$gate.stdout.sha256")
    stderr_digest=$(cat "$stage/$gate.stderr.sha256")
    printf '| `%s` | %s | `%s` | %s | %s | `%s` | `%s` |\n' \
      "$gate" "$gate_status" "$gate_argv" "$gate_exit" "$gate_duration" \
      "$stdout_digest" "$stderr_digest"
  done
  cat <<EOF

## Coverage and Prerequisites

| Prerequisite | Evidence |
|---|---|
| git | \`$(if [ -n "$git_cmd" ]; then printf present; else printf MISSING; fi)\` |
| go | \`$go_version\` |
| golangci-lint | \`$lint_version\` |
| gopls | \`$gopls_version\` |

EOF
  if [ "$(cat "$stage/cli_matrix.status")" = PASS ]; then
    echo '- LSP query coverage: PASS (required mode).'
  else
    echo '- LSP query coverage was not promoted; the CLI matrix did not pass.'
  fi
  cat <<'EOF'

## Resolved Evidence

The gate rows resolve to these files beneath `.work/qa/latest` (or the
test-owned output directory when `REPOMAP_QA_TESTING=1`):

EOF
  for gate in build cli_model vet lint test race mod_verify cli_matrix; do
    stdout_digest=$(cat "$stage/$gate.stdout.sha256")
    stderr_digest=$(cat "$stage/$gate.stderr.sha256")
    printf -- '- `%s.stdout` — sha256 `%s`\n' "$gate" "$stdout_digest"
    printf -- '- `%s.stderr` — sha256 `%s`\n' "$gate" "$stderr_digest"
  done
  cat <<'EOF'

## Residual Gaps

EOF
  case "$final_status" in
    PASS)
      echo '- None recorded by the required gate set.'
      ;;
    FAIL)
      echo '- One or more required gates failed; inspect the FAIL rows and resolving logs.'
      ;;
    INCOMPLETE)
      echo '- One or more prerequisites or required results were unavailable; no complete-pass claim is made.'
      ;;
  esac
}

set +e
render_report >"$report_tmp"
render_status=$?
set -e
if [ "$render_status" -ne 0 ] || [ ! -s "$report_tmp" ]; then
  echo "failed to render QA report; previous report preserved" >&2
  exit 1
fi

publication_started=1
if [ -e "$latest_dir" ]; then
  latest_backup=$run_tmp/previous-latest
  if ! "$mv_cmd" "$latest_dir" "$latest_backup"; then
    echo "failed to stage previous QA evidence; previous report preserved" >&2
    exit 1
  fi
fi
if ! "$mv_cmd" "$stage" "$latest_dir"; then
  rollback_publication
  echo "failed to publish QA logs; previous report preserved" >&2
  exit 1
fi

if ! "$mv_cmd" "$report_tmp" "$canonical_report"; then
  rollback_publication
  echo "failed to publish QA report; previous report and logs restored" >&2
  exit 1
fi
publication_started=0
if [ -n "$latest_backup" ] && [ -d "$latest_backup" ]; then
  remove_owned_directory "$latest_backup"
fi

echo "report: $canonical_report" >&2
echo "qa-e2e: $final_status"
exit "$final_exit"
