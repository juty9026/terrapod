#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

write_stub() {
  path="$1"
  shift
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' "$@"
  } >"$path"
  chmod +x "$path"
}

assert_contains() {
  haystack="$1"
  needle="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_no_arg() {
  call_file="$1"
  unexpected="$2"
  message="$3"

  while IFS= read -r arg; do
    if [ "$arg" = "$unexpected" ]; then
      fail "$message"
    fi
  done <"$call_file"

  pass "$message"
}

assert_arg_at() {
  call_file="$1"
  position="$2"
  expected="$3"
  message="$4"
  actual="$(sed -n "${position}p" "$call_file")"

  if [ "$actual" != "$expected" ]; then
    fail "$message: expected arg $position to be '$expected', got '$actual'"
  fi

  pass "$message"
}

assert_call_args() {
  call_file="$1"
  message="$2"
  shift 2
  expected_file="$tmp_dir/expected.args"
  : >"$expected_file"

  for arg do
    printf '%s\n' "$arg" >>"$expected_file"
  done

  if ! cmp -s "$expected_file" "$call_file"; then
    printf '%s\n' "expected args:" >&2
    sed 's/^/  /' "$expected_file" >&2
    printf '%s\n' "actual args:" >&2
    sed 's/^/  /' "$call_file" >&2
    fail "$message"
  fi

  pass "$message"
}

assert_call_count() {
  expected="$1"
  message="$2"
  actual=0

  if [ -f "$GH_CALL_COUNT" ]; then
    actual="$(cat "$GH_CALL_COUNT")"
  fi

  if [ "$actual" != "$expected" ]; then
    fail "$message: expected $expected gh calls, got $actual"
  fi

  pass "$message"
}

assert_tmp_empty() {
  message="$1"

  if find "$TMPDIR" -mindepth 1 | grep . >/dev/null; then
    find "$TMPDIR" -mindepth 1 >&2
    fail "$message"
  fi

  pass "$message"
}

reset_gh_calls() {
  rm -rf "$GH_CALL_DIR"
  mkdir -p "$GH_CALL_DIR"
  rm -f "$GH_CALL_COUNT" "$GH_BODY_CAPTURE" "$GH_BODY_FILE_PATH"
}

mkdir -p "$tmp_dir/bin" "$tmp_dir/home" "$tmp_dir/editors" "$tmp_dir/tmp" "$tmp_dir/calls"

rendered_config="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    cat "$tmp_dir/home/.config/lazygit/config.yml"
)"

assert_contains "$rendered_config" "key: X" "lazygit config binds the PR merge command to X"
assert_contains "$rendered_config" "context: localBranches" "lazygit config exposes the command in local branches"
assert_contains "$rendered_config" "description: Merge GitHub PR" "lazygit config exposes the PR merge command"
assert_contains "$rendered_config" "title: Merge pull request" "lazygit config prompts for merge strategy"
assert_contains "$rendered_config" "key: MergeStrategy" "lazygit config stores the selected merge strategy"
assert_contains "$rendered_config" "name: Merge commit" "lazygit config offers merge commit strategy"
assert_contains "$rendered_config" "value: merge" "lazygit config maps merge commit to merge"
assert_contains "$rendered_config" "name: Squash" "lazygit config offers squash strategy"
assert_contains "$rendered_config" "value: squash" "lazygit config maps squash to squash"
assert_contains "$rendered_config" "title: Merge PR?" "lazygit config asks for confirmation"
assert_contains "$rendered_config" "body: Merge the GitHub PR for the selected branch?" "lazygit config explains confirmation"
assert_contains "$rendered_config" "merge-pr {{.SelectedLocalBranch.Name | quote}} {{.Form.MergeStrategy | quote}}" "lazygit config delegates PR merge behavior to helper"
assert_contains "$rendered_config" "output: terminal" "lazygit command runs in a terminal"
assert_contains "$rendered_config" "loadingText: Merging PR..." "lazygit command shows merge progress"

helper_target="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    target-path dot_config/lazygit/scripts/executable_merge-pr
)"
expected_helper_target="$tmp_dir/home/.config/lazygit/scripts/merge-pr"

if [ "$helper_target" != "$expected_helper_target" ]; then
  fail "chezmoi should install executable_merge-pr as merge-pr; expected '$expected_helper_target', got '$helper_target'"
fi

pass "chezmoi installs executable_merge-pr as the helper path lazygit calls"

helper="$repo_root/dot_config/lazygit/scripts/executable_merge-pr"

write_stub "$tmp_dir/bin/gh" \
  'count=0' \
  '[ -f "$GH_CALL_COUNT" ] && count="$(cat "$GH_CALL_COUNT")"' \
  'count=$((count + 1))' \
  'printf "%s\n" "$count" >"$GH_CALL_COUNT"' \
  'call_file="$GH_CALL_DIR/call-$count.args"' \
  ': >"$call_file"' \
  'for arg do' \
  '  printf "%s\n" "$arg" >>"$call_file"' \
  'done' \
  'if [ "$1" = "pr" ] && [ "$2" = "view" ]; then' \
  '  printf "%s\n" "$GH_PR_TITLE"' \
  '  printf "%s\n" "$GH_PR_BODY"' \
  '  exit 0' \
  'fi' \
  'if [ "$1" = "pr" ] && [ "$2" = "merge" ]; then' \
  '  if [ "${GH_MERGE_EXIT:-0}" != "0" ]; then' \
  '    exit "$GH_MERGE_EXIT"' \
  '  fi' \
  '  body_file=""' \
  '  while [ "$#" -gt 0 ]; do' \
  '    if [ "$1" = "--body-file" ]; then' \
  '      body_file="${2:-}"' \
  '      break' \
  '    fi' \
  '    shift' \
  '  done' \
  '  if [ -n "$body_file" ]; then' \
  '    cat "$body_file" >"$GH_BODY_CAPTURE"' \
  '    printf "%s\n" "$body_file" >"$GH_BODY_FILE_PATH"' \
  '  fi' \
  '  exit 0' \
  'fi' \
  'exit 64'

write_stub "$tmp_dir/editors/append-editor" \
  'printf "%s\n" "Edited body line." >>"$1"'

write_stub "$tmp_dir/editors/require-flag-editor" \
  'if [ "${1:-}" != "--wait" ]; then' \
  '  printf "%s\n" "missing --wait flag" >&2' \
  '  exit 65' \
  'fi' \
  'printf "%s\n" "Edited body line." >>"$2"'

write_stub "$tmp_dir/editors/remove-title-editor" \
  'sed "/^Title:/d" "$1" >"$1.tmp"' \
  'mv "$1.tmp" "$1"'

write_stub "$tmp_dir/editors/failing-editor" \
  'printf "%s\n" "editor failed intentionally" >&2' \
  'exit 42'

export PATH="$tmp_dir/bin:/usr/bin:/bin"
export GH_CALL_DIR="$tmp_dir/calls"
export GH_CALL_COUNT="$tmp_dir/gh-call-count"
export GH_BODY_CAPTURE="$tmp_dir/body.txt"
export GH_BODY_FILE_PATH="$tmp_dir/body-file-path.txt"
export GH_PR_TITLE='Implement lazygit PR merge; keep $HOME safe'
export GH_PR_BODY="This PR adds a merge menu."
export GH_MERGE_EXIT=0
export TMPDIR="$tmp_dir/tmp"

reset_gh_calls
sh "$helper" feature/pr-menu merge

assert_call_count 1 "merge strategy should call gh once"
assert_call_args "$GH_CALL_DIR/call-1.args" \
  "merge strategy should call gh pr merge with --merge and keep the branch" \
  pr merge feature/pr-menu --merge

reset_gh_calls
export EDITOR="$tmp_dir/editors/require-flag-editor --wait"
sh "$helper" feature/pr-menu squash

assert_call_count 2 "squash strategy should view then merge"
assert_call_args "$GH_CALL_DIR/call-1.args" \
  "squash strategy should read PR title and body" \
  pr view feature/pr-menu --json title,body --jq ".title, .body"

squash_merge_call="$GH_CALL_DIR/call-2.args"
assert_arg_at "$squash_merge_call" 1 pr "squash merge call starts with gh pr"
assert_arg_at "$squash_merge_call" 2 merge "squash merge call targets gh merge"
assert_arg_at "$squash_merge_call" 3 feature/pr-menu "squash merge call targets the selected branch"
assert_arg_at "$squash_merge_call" 4 --squash "squash merge call uses squash strategy"
assert_arg_at "$squash_merge_call" 5 --subject "squash merge call passes a subject flag"
assert_arg_at "$squash_merge_call" 6 "$GH_PR_TITLE" "squash strategy passes the metacharacter title as one subject argument"
assert_arg_at "$squash_merge_call" 7 --body-file "squash merge call passes a body file"
line_count="$(wc -l <"$squash_merge_call" | tr -d ' ')"
if [ "$line_count" != 8 ]; then
  fail "squash merge call should only pass expected arguments"
fi
pass "squash merge call only passes expected arguments"
assert_no_arg "$squash_merge_call" --delete-branch "squash strategy should not delete the branch"

if ! grep -F "This PR adds a merge menu." "$GH_BODY_CAPTURE" >/dev/null; then
  fail "squash strategy should pass PR description as body"
fi

if ! grep -F "Edited body line." "$GH_BODY_CAPTURE" >/dev/null; then
  fail "squash strategy should include editor changes in body"
fi

pass "squash strategy passes edited description as body"

body_file_path="$(cat "$GH_BODY_FILE_PATH")"
if [ -e "$body_file_path" ]; then
  fail "squash strategy should remove its temporary body file after a successful merge"
fi

assert_tmp_empty "squash strategy should not leave temporary files or directories after success"

reset_gh_calls
export EDITOR="$tmp_dir/editors/remove-title-editor"

if sh "$helper" feature/pr-menu squash >"$tmp_dir/missing-title.out" 2>"$tmp_dir/missing-title.err"; then
  fail "squash strategy should reject an edited message without a title"
fi

if ! grep -F "missing a Title" "$tmp_dir/missing-title.err" >/dev/null; then
  fail "squash strategy should explain missing title errors"
fi

assert_call_count 1 "missing-title abort should not call gh merge"
assert_tmp_empty "squash strategy should not leave temporary files or directories after a missing-title abort"
pass "squash strategy rejects missing title"

reset_gh_calls
export EDITOR="$tmp_dir/editors/append-editor"
export GH_MERGE_EXIT=45

if sh "$helper" feature/pr-menu squash >"$tmp_dir/merge-failure.out" 2>"$tmp_dir/merge-failure.err"; then
  fail "squash strategy should fail when gh merge exits non-zero"
else
  merge_failure_status="$?"
fi

if [ "$merge_failure_status" != "45" ]; then
  fail "squash strategy should preserve gh merge failure status"
fi

assert_call_count 2 "merge failure should happen after view and merge calls"
assert_tmp_empty "squash strategy should not leave temporary files or directories after gh merge failure"
pass "squash strategy preserves gh merge failures"

reset_gh_calls
export GH_MERGE_EXIT=0
export EDITOR="$tmp_dir/editors/failing-editor"

if sh "$helper" feature/pr-menu squash >"$tmp_dir/editor-failure.out" 2>"$tmp_dir/editor-failure.err"; then
  fail "squash strategy should fail when the editor exits non-zero"
fi

if ! grep -F "editor exited unsuccessfully" "$tmp_dir/editor-failure.err" >/dev/null; then
  fail "squash strategy should explain editor failures"
fi

assert_call_count 1 "editor failure should not call gh merge"
assert_tmp_empty "squash strategy should not leave temporary files or directories after editor failure"
pass "squash strategy rejects editor failure"
