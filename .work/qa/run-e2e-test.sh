#!/bin/sh
set -eu

runner=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)/run-e2e.sh
test_root=$(mktemp -d "${TMPDIR:-/tmp}/repomap-e2e-test.XXXXXX")
trap 'rm -rf "$test_root"' 0 HUP INT TERM

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_contains() {
  pattern=$1
  file=$2
  grep -F "$pattern" "$file" >/dev/null 2>&1 ||
    fail "$file does not contain: $pattern"
}

assert_status() {
  want=$1
  file=$2
  count=$(grep -Ec '^Status: (PASS|FAIL|INCOMPLETE)$' "$file" || true)
  [ "$count" -eq 1 ] || fail "$file has $count status lines"
  assert_contains "Status: $want" "$file"
}

assert_same_file() {
  expected=$1
  actual=$2
  cmp -s "$expected" "$actual" ||
    fail "$actual does not match $expected byte-for-byte"
}

make_tools() {
  bin_dir=$1
  mkdir -p "$bin_dir"

  cat >"$bin_dir/git" <<'EOF'
#!/bin/sh
if [ "${1:-}" = -c ]; then
  shift 2
fi
case "$1:$2" in
  rev-parse:HEAD)
    case "${REPOMAP_QA_TEST_RUN_ID:-one}" in
      two) printf '%s\n' 2222222222222222222222222222222222222222 ;;
      *) printf '%s\n' 1111111111111111111111111111111111111111 ;;
    esac
    ;;
  status:--porcelain=v1)
    case " $* " in
      *" -z "*)
        if [ "${REPOMAP_QA_TEST_SCENARIO:-pass}" = dirty ]; then
          printf '?? odd\nname\000'
        fi
        ;;
      *)
        if [ "${REPOMAP_QA_TEST_SCENARIO:-pass}" = dirty ]; then
          printf '%s\n' '?? "odd\nname"'
        fi
        ;;
    esac
    ;;
  *) echo "git stub: unsupported invocation: $*" >&2; exit 2 ;;
esac
EOF

  cat >"$bin_dir/go" <<'EOF'
#!/bin/sh
case "$1" in
  version)
    if [ "${2:-}" = "-m" ]; then
      printf '%s\n' "$3: go1.26.5" 'path github.com/dotcommander/repomap/cmd/repomap'
    else
      printf '%s\n' 'go version go1.26.5 darwin/arm64'
    fi
    ;;
  build)
    output=
    previous=
    for argument do
      if [ "$previous" = output ]; then
        output=$argument
        previous=
      elif [ "$argument" = -o ]; then
        previous=output
      fi
    done
    [ -n "$output" ] || exit 2
    printf '%s\n' '#!/bin/sh' 'exit 0' >"$output"
    chmod 0755 "$output"
    ;;
  vet)
    if [ "${REPOMAP_QA_TEST_SCENARIO:-pass}" = fail ]; then
      echo 'intentional vet failure' >&2
      exit 7
    fi
    ;;
  test)
    if [ "${REPOMAP_QA_TEST_SCENARIO:-pass}" = interrupt ]; then
      case " $* " in
        *" ./... "*)
          : >"${REPOMAP_QA_TEST_INTERRUPT_MARKER:?}"
          while :; do sleep 1; done
          ;;
      esac
    fi
    ;;
  mod)
    [ "${2:-}" = verify ] || exit 2
    printf '%s\n' 'all modules verified'
    ;;
  *) echo "go stub: unsupported invocation: $*" >&2; exit 2 ;;
esac
EOF

  cat >"$bin_dir/golangci-lint" <<'EOF'
#!/bin/sh
case "$1" in
  version) printf '%s\n' 'golangci-lint has version 2.4.0' ;;
  run) exit 0 ;;
  *) exit 2 ;;
esac
EOF

  cat >"$bin_dir/gopls" <<'EOF'
#!/bin/sh
[ "${1:-}" = version ] || exit 2
printf '%s\n' 'golang.org/x/tools/gopls v0.20.0'
EOF

  cat >"$bin_dir/repomap-qa-cli-matrix" <<'EOF'
#!/bin/sh
[ "$#" -eq 2 ] || exit 2
[ "${REPOMAP_QA_REQUIRE_GOPLS:-}" = 1 ] || {
  echo 'required gopls mode was not enabled' >&2
  exit 9
}
[ -x "$1" ] || exit 2
[ "${2#/}" != "$2" ] || exit 2
if [ -n "${REPOMAP_QA_TEST_MATRIX_MARKER:-}" ]; then
  : >"$REPOMAP_QA_TEST_MATRIX_MARKER"
fi
printf '%s\n' 'PASS: deterministic CLI matrix stub'
EOF

  cat >"$bin_dir/mv" <<'EOF'
#!/bin/sh
last=
for argument do last=$argument; done
case "${REPOMAP_QA_TEST_SCENARIO:-pass}" in
  report_publish_fail)
    case "$last" in
      */repomap_e2e_qa_report.md)
        echo 'intentional report publication failure' >&2
        exit 1
        ;;
    esac
    ;;
  report_publish_signal)
    case "$last" in
      */repomap_e2e_qa_report.md)
        /bin/mv "$@"
        kill -TERM "$PPID"
        exit 0
        ;;
    esac
    ;;
  latest_backup_signal)
    case "$last" in
      */previous-latest)
        /bin/mv "$@"
        kill -TERM "$PPID"
        exit 0
        ;;
    esac
    ;;
  latest_publish_signal)
    case "$1" in
      */.repomap-e2e.*/latest)
        /bin/mv "$@"
        kill -TERM "$PPID"
        exit 0
        ;;
    esac
    ;;
esac
exec /bin/mv "$@"
EOF

  chmod 0755 "$bin_dir"/*
}

new_case() {
  case_name=$1
  case_dir="$test_root/$case_name"
  case_bin="$case_dir/bin"
  case_output="$case_dir/output with spaces"
  mkdir -p "$case_output"
  make_tools "$case_bin"
}

run_case() {
  scenario=$1
  shift
  REPOMAP_QA_TESTING=1 \
  REPOMAP_QA_TEST_BIN_DIR=$case_bin \
  REPOMAP_QA_TEST_OUTPUT_DIR=$case_output \
  REPOMAP_QA_TEST_SCENARIO=$scenario \
    "$runner" "$@"
}

pass_writes_complete_report_and_exits_zero() {
  new_case pass
  matrix_marker="$case_dir/matrix-ran"
  REPOMAP_QA_TEST_MATRIX_MARKER=$matrix_marker run_case pass \
    >"$case_dir/stdout" 2>"$case_dir/stderr"
  report="$case_output/repomap_e2e_qa_report.md"
  assert_status PASS "$report"
  [ -f "$matrix_marker" ] || fail "CLI matrix did not run"
  [ "$(grep -c '^| `[^`]*` | PASS |' "$report")" -eq 8 ] ||
    fail "PASS report does not contain eight passing gates"
  assert_contains "output with spaces" "$report"
  assert_contains "runner_version: 1" "$report"
  assert_contains "qa-e2e: PASS" "$case_dir/stdout"
}

failed_gate_records_argv_logs_and_exits_one() {
  new_case failed-gate
  matrix_marker="$case_dir/matrix-ran-after-vet-failure"
  set +e
  REPOMAP_QA_TEST_MATRIX_MARKER=$matrix_marker run_case fail \
    >"$case_dir/stdout" 2>"$case_dir/stderr"
  status=$?
  set -e
  [ "$status" -eq 1 ] || fail "failed gate exited $status, want 1"
  report="$case_output/repomap_e2e_qa_report.md"
  assert_status FAIL "$report"
  assert_contains '| `vet` | FAIL |' "$report"
  assert_contains "go' 'vet' './..." "$report"
  assert_contains "vet.stdout" "$report"
  assert_contains "vet.stderr" "$report"
  assert_contains "stdout_sha256" "$report"
  assert_contains "stderr_sha256" "$report"
  [ -f "$matrix_marker" ] || fail "CLI matrix did not run after vet failed"
  for gate in lint test race mod_verify cli_matrix; do
    assert_contains "| \`$gate\` | PASS |" "$report"
  done
}

missing_required_tool_is_incomplete_and_exits_two() {
  new_case missing-tool
  rm "$case_bin/gopls"
  set +e
  run_case pass >"$case_dir/stdout" 2>"$case_dir/stderr"
  status=$?
  set -e
  [ "$status" -eq 2 ] || fail "missing tool exited $status, want 2"
  report="$case_output/repomap_e2e_qa_report.md"
  assert_status INCOMPLETE "$report"
  assert_contains '| `cli_matrix` | BLOCKED |' "$report"
  ! grep -F 'LSP query coverage: PASS' "$report" >/dev/null 2>&1 ||
    fail "blocked LSP coverage was promoted"
}

gopls_branch_cannot_silently_skip_in_required_mode() {
  new_case required-gopls
  matrix_marker="$case_dir/required-mode-ran"
  REPOMAP_QA_TEST_MATRIX_MARKER=$matrix_marker run_case pass \
    >"$case_dir/stdout" 2>"$case_dir/stderr"
  [ -f "$matrix_marker" ] || fail "required-mode CLI matrix did not run"
  assert_status PASS "$case_output/repomap_e2e_qa_report.md"
}

dirty_source_is_disclosed_without_changing_gate_result() {
  new_case dirty
  run_case dirty >"$case_dir/stdout" 2>"$case_dir/stderr"
  report="$case_output/repomap_e2e_qa_report.md"
  assert_status PASS "$report"
  assert_contains 'source_state: DIRTY' "$report"
  assert_contains '?? "odd\nname"' "$report"
}

failed_report_publication_restores_previous_report_and_latest() {
  new_case report-publication-failure
  report="$case_output/repomap_e2e_qa_report.md"
  report_before="$case_dir/previous-report.md"
  printf '%s\n' 'previous report sentinel' >"$report"
  cp "$report" "$report_before"
  latest="$case_output/latest"
  latest_before="$case_dir/latest-before"
  mkdir "$latest" "$latest_before"
  : >"$latest/.repomap-qa-owned"
  printf '%s\n' 'previous latest evidence sentinel' >"$latest/evidence.txt"
  cp "$latest/.repomap-qa-owned" "$latest_before/.repomap-qa-owned"
  cp "$latest/evidence.txt" "$latest_before/evidence.txt"
  set +e
  run_case report_publish_fail >"$case_dir/stdout" 2>"$case_dir/stderr"
  status=$?
  set -e
  [ "$status" -ne 0 ] || fail "report publication failure exited zero"
  assert_same_file "$report_before" "$report"
  assert_same_file "$latest_before/.repomap-qa-owned" \
    "$latest/.repomap-qa-owned"
  assert_same_file "$latest_before/evidence.txt" "$latest/evidence.txt"
  [ "$(find "$latest" -type f | wc -l | tr -d ' ')" -eq 2 ] ||
    fail "report publication failure did not restore the previous latest tree"
  [ -z "$(find "$case_output" -name '.repomap-e2e.*' -print)" ] ||
    fail "report publication failure left run-owned temporary files"

  new_case latest-backup-signal
  report="$case_output/repomap_e2e_qa_report.md"
  report_before="$case_dir/previous-report.md"
  printf '%s\n' 'previous report sentinel' >"$report"
  cp "$report" "$report_before"
  latest="$case_output/latest"
  latest_before="$case_dir/latest-before"
  mkdir "$latest" "$latest_before"
  : >"$latest/.repomap-qa-owned"
  printf '%s\n' 'previous latest evidence sentinel' >"$latest/evidence.txt"
  cp "$latest/.repomap-qa-owned" "$latest_before/.repomap-qa-owned"
  cp "$latest/evidence.txt" "$latest_before/evidence.txt"
  set +e
  run_case latest_backup_signal >"$case_dir/stdout" 2>"$case_dir/stderr"
  status=$?
  set -e
  [ "$status" -eq 130 ] ||
    fail "latest backup signal exited $status, want 130"
  assert_same_file "$report_before" "$report"
  assert_same_file "$latest_before/.repomap-qa-owned" \
    "$latest/.repomap-qa-owned"
  assert_same_file "$latest_before/evidence.txt" "$latest/evidence.txt"
  [ "$(find "$latest" -type f | wc -l | tr -d ' ')" -eq 2 ] ||
    fail "latest backup signal left new nested evidence"
  [ -z "$(find "$case_output" -name '.repomap-e2e.*' -print)" ] ||
    fail "latest backup signal left run-owned temporary files"

  new_case latest-publish-signal
  report="$case_output/repomap_e2e_qa_report.md"
  report_before="$case_dir/previous-report.md"
  printf '%s\n' 'previous report sentinel' >"$report"
  cp "$report" "$report_before"
  latest="$case_output/latest"
  latest_before="$case_dir/latest-before"
  mkdir "$latest" "$latest_before"
  : >"$latest/.repomap-qa-owned"
  printf '%s\n' 'previous latest evidence sentinel' >"$latest/evidence.txt"
  cp "$latest/.repomap-qa-owned" "$latest_before/.repomap-qa-owned"
  cp "$latest/evidence.txt" "$latest_before/evidence.txt"
  set +e
  run_case latest_publish_signal >"$case_dir/stdout" 2>"$case_dir/stderr"
  status=$?
  set -e
  [ "$status" -eq 130 ] ||
    fail "latest publish signal exited $status, want 130"
  assert_same_file "$report_before" "$report"
  assert_same_file "$latest_before/.repomap-qa-owned" \
    "$latest/.repomap-qa-owned"
  assert_same_file "$latest_before/evidence.txt" "$latest/evidence.txt"
  [ "$(find "$latest" -type f | wc -l | tr -d ' ')" -eq 2 ] ||
    fail "latest publish signal left new nested evidence"
  [ -z "$(find "$case_output" -name '.repomap-e2e.*' -print)" ] ||
    fail "latest publish signal left run-owned temporary files"

  new_case report-publication-signal
  report="$case_output/repomap_e2e_qa_report.md"
  report_before="$case_dir/previous-report.md"
  printf '%s\n' 'previous report sentinel' >"$report"
  cp "$report" "$report_before"
  latest="$case_output/latest"
  mkdir "$latest"
  : >"$latest/.repomap-qa-owned"
  printf '%s\n' 'previous latest evidence sentinel' >"$latest/evidence.txt"
  set +e
  run_case report_publish_signal >"$case_dir/stdout" 2>"$case_dir/stderr"
  status=$?
  set -e
  [ "$status" -eq 130 ] ||
    fail "post-publication signal exited $status, want 130"
  assert_status PASS "$report"
  ! cmp -s "$report_before" "$report" ||
    fail "post-publication signal restored the previous report"
  assert_contains 1111111111111111111111111111111111111111 "$report"
  assert_contains 1111111111111111111111111111111111111111 \
    "$latest/git-head.txt"
  [ ! -e "$latest/evidence.txt" ] ||
    fail "post-publication signal restored the previous latest evidence"
  [ -z "$(find "$case_output" -name '.repomap-e2e.*' -print)" ] ||
    fail "post-publication signal left run-owned temporary files"
}

second_run_replaces_one_canonical_report() {
  new_case replacement
  REPOMAP_QA_TEST_RUN_ID=one run_case pass \
    >"$case_dir/first.stdout" 2>"$case_dir/first.stderr"
  REPOMAP_QA_TEST_RUN_ID=two run_case pass \
    >"$case_dir/second.stdout" 2>"$case_dir/second.stderr"
  report="$case_output/repomap_e2e_qa_report.md"
  assert_contains 2222222222222222222222222222222222222222 "$report"
  ! grep -F 1111111111111111111111111111111111111111 "$report" >/dev/null 2>&1 ||
    fail "second run retained the first report"
  [ "$(find "$case_output" -name 'repomap_e2e_qa_report.md' -type f | wc -l | tr -d ' ')" -eq 1 ] ||
    fail "runner created duplicate canonical reports"
}

interrupt_removes_temporary_binary_and_partial_report() {
  new_case interrupt
  report="$case_output/repomap_e2e_qa_report.md"
  marker="$case_dir/test-started"
  printf '%s\n' 'previous report sentinel' >"$report"
  REPOMAP_QA_TESTING=1 \
  REPOMAP_QA_TEST_BIN_DIR=$case_bin \
  REPOMAP_QA_TEST_OUTPUT_DIR=$case_output \
  REPOMAP_QA_TEST_SCENARIO=interrupt \
  REPOMAP_QA_TEST_INTERRUPT_MARKER=$marker \
    "$runner" >"$case_dir/stdout" 2>"$case_dir/stderr" &
  runner_pid=$!
  attempts=0
  while [ ! -f "$marker" ] && [ "$attempts" -lt 100 ]; do
    sleep 0.05
    attempts=$((attempts + 1))
  done
  [ -f "$marker" ] || {
    kill "$runner_pid" 2>/dev/null || true
    wait "$runner_pid" 2>/dev/null || true
    fail "interrupt scenario did not reach the full test gate"
  }
  kill -TERM "$runner_pid"
  set +e
  wait "$runner_pid"
  status=$?
  set -e
  [ "$status" -ne 0 ] || fail "interrupted runner exited zero"
  [ "$(cat "$report")" = 'previous report sentinel' ] ||
    fail "interruption replaced the previous report"
  [ -z "$(find "$case_output" -name '.repomap-e2e.*' -print)" ] ||
    fail "interruption left run-owned temporary files"
}

test_mode_boundaries_and_non_regular_reports_are_rejected() {
  new_case isolated-override
  set +e
  REPOMAP_QA_TEST_BIN_DIR=$case_bin \
  REPOMAP_QA_TEST_OUTPUT_DIR=$case_output \
    "$runner" >"$case_dir/stdout" 2>"$case_dir/stderr"
  status=$?
  set -e
  [ "$status" -eq 2 ] || fail "production override exited $status, want 2"
  assert_contains 'test overrides require REPOMAP_QA_TESTING=1' "$case_dir/stderr"
  [ ! -e "$case_output/repomap_e2e_qa_report.md" ] ||
    fail "rejected production override wrote a report"

  for target_kind in directory fifo; do
    new_case "$target_kind-report-target"
    report="$case_output/repomap_e2e_qa_report.md"
    case "$target_kind" in
      directory)
        mkdir "$report"
        printf '%s\n' 'existing directory sentinel' >"$report/sentinel"
        ;;
      fifo)
        mkfifo "$report"
        ;;
    esac
    set +e
    run_case pass >"$case_dir/stdout" 2>"$case_dir/stderr"
    status=$?
    set -e
    [ "$status" -eq 2 ] ||
      fail "$target_kind report target exited $status, want 2"
    assert_contains "refusing to replace non-regular report: $report" \
      "$case_dir/stderr"
    [ ! -e "$case_output/latest" ] && [ ! -L "$case_output/latest" ] ||
      fail "$target_kind report target published latest evidence"
    [ -z "$(find "$case_output" -name '.repomap-e2e.*' -print)" ] ||
      fail "$target_kind report target left run-owned temporary files"
    case "$target_kind" in
      directory)
        [ "$(cat "$report/sentinel")" = 'existing directory sentinel' ] ||
          fail "directory report target was changed"
        [ ! -e "$report/repomap_e2e_qa_report.md" ] ||
          fail "directory report target received a nested publication"
        ;;
      fifo)
        [ -p "$report" ] || fail "FIFO report target was changed"
        ;;
    esac
  done
}

tests='
pass_writes_complete_report_and_exits_zero
failed_gate_records_argv_logs_and_exits_one
missing_required_tool_is_incomplete_and_exits_two
gopls_branch_cannot_silently_skip_in_required_mode
dirty_source_is_disclosed_without_changing_gate_result
failed_report_publication_restores_previous_report_and_latest
second_run_replaces_one_canonical_report
interrupt_removes_temporary_binary_and_partial_report
test_mode_boundaries_and_non_regular_reports_are_rejected
'

for test_name in $tests; do
  "$test_name"
  echo "PASS: $test_name"
done
