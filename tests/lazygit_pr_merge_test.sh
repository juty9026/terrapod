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

mkdir -p "$tmp_dir/bin" "$tmp_dir/home" "$tmp_dir/editors" "$tmp_dir/tmp"

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

helper="$repo_root/dot_config/lazygit/scripts/executable_merge-pr"

write_stub "$tmp_dir/bin/gh" \
  'printf "%s\n" "$*" >>"$GH_CALL_LOG"' \
  'if [ "$1" = "pr" ] && [ "$2" = "view" ]; then' \
  '  printf "%s\n" "Implement lazygit PR merge"' \
  '  printf "%s\n" "This PR adds a merge menu."' \
  '  exit 0' \
  'fi' \
  'if [ "$1" = "pr" ] && [ "$2" = "merge" ]; then' \
  '  body_file=""' \
  '  while [ "$#" -gt 0 ]; do' \
  '    if [ "$1" = "--body-file" ]; then' \
  '      body_file="$2"' \
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

export PATH="$tmp_dir/bin:/usr/bin:/bin"
export GH_CALL_LOG="$tmp_dir/gh-calls.log"
export GH_BODY_CAPTURE="$tmp_dir/body.txt"
export GH_BODY_FILE_PATH="$tmp_dir/body-file-path.txt"
export EDITOR="$tmp_dir/editors/append-editor"
export TMPDIR="$tmp_dir/tmp"

sh "$helper" feature/pr-menu merge

if ! grep -F "pr merge feature/pr-menu --merge --delete-branch" "$GH_CALL_LOG" >/dev/null; then
  fail "merge strategy should call gh pr merge with --merge"
fi

pass "merge strategy calls gh pr merge with --merge"

: >"$GH_CALL_LOG"
sh "$helper" feature/pr-menu squash

if ! grep -F "pr view feature/pr-menu --json title,body --jq .title, .body" "$GH_CALL_LOG" >/dev/null; then
  fail "squash strategy should read PR title and body"
fi

pass "squash strategy reads PR title and body"

if ! grep -F "pr merge feature/pr-menu --squash --delete-branch --subject Implement lazygit PR merge --body-file" "$GH_CALL_LOG" >/dev/null; then
  fail "squash strategy should pass edited title as subject"
fi

pass "squash strategy passes edited title as subject"

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

if find "$TMPDIR" -type f | grep . >/dev/null; then
  fail "squash strategy should not leave temporary files after a successful merge"
fi

pass "squash strategy removes temporary files after success"

write_stub "$tmp_dir/editors/remove-title-editor" \
  'sed "/^Title:/d" "$1" >"$1.tmp"' \
  'mv "$1.tmp" "$1"'

export EDITOR="$tmp_dir/editors/remove-title-editor"

if sh "$helper" feature/pr-menu squash >"$tmp_dir/missing-title.out" 2>"$tmp_dir/missing-title.err"; then
  fail "squash strategy should reject an edited message without a title"
fi

if ! grep -F "missing a Title" "$tmp_dir/missing-title.err" >/dev/null; then
  fail "squash strategy should explain missing title errors"
fi

if find "$TMPDIR" -type f | grep . >/dev/null; then
  fail "squash strategy should not leave temporary files after a missing-title abort"
fi

pass "squash strategy rejects missing title"
