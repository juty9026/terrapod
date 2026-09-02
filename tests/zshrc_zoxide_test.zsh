#!/usr/bin/env zsh

set -u

repo_root="${0:A:h:h}"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

assert_log_contains() {
  local expected="$1"
  local message="$2"

  if ! grep -F "$expected" "$ZELLIJ_TEST_LOG" >/dev/null 2>&1; then
    fail "$message; expected log to contain '$expected'"
  fi

  pass "$message"
}

render_zshrc() {
  local data="$1"

  "$chezmoi_bin" \
    --config "$tmp_dir/chezmoi.toml" \
    --destination "$tmp_dir/home" \
    --source "$repo_root" \
    --override-data "$data" \
    cat "$tmp_dir/home/.zshrc" \
    >"$tmp_dir/home/.zshrc"
}

assert_pwd() {
  local expected_dir="$1"
  local message="$2"
  local actual_pwd expected_pwd

  actual_pwd="$(pwd -P)"
  expected_pwd="$(cd "$expected_dir" && pwd -P)"

  if [[ "$actual_pwd" != "$expected_pwd" ]]; then
    fail "$message; expected $expected_pwd, got $actual_pwd"
  fi
}

mkdir -p \
  "$tmp_dir/bin" \
  "$tmp_dir/git-only-bin" \
  "$tmp_dir/home" \
  "$tmp_dir/home/.local/share/zinit/zinit.git" \
  "$tmp_dir/start" \
  "$tmp_dir/selected" \
  "$tmp_dir/recent" \
  "$tmp_dir/git-project/.git" \
  "$tmp_dir/worktree-seed" \
  "$tmp_dir/plain"

cat >"$tmp_dir/home/.local/share/zinit/zinit.git/zinit.zsh" <<'STUB'
function zinit() {
  :
}

alias zi=zinit
STUB

cat >"$tmp_dir/bin/fzf" <<'STUB'
#!/bin/sh
if [ "${1:-}" = "--zsh" ]; then
  printf '%s\n' '# fzf zsh integration stub'
  exit 0
fi

if [ -n "${FZF_TEST_STDIN_LOG:-}" ]; then
  cat >"$FZF_TEST_STDIN_LOG"
else
  cat >/dev/null
fi

if [ -n "${FZF_TEST_SELECTION:-}" ]; then
  printf '%s\n' "$FZF_TEST_SELECTION"
  exit 0
fi

exit 130
STUB

cat >"$tmp_dir/bin/zoxide" <<'STUB'
#!/bin/sh
if [ "${1:-}" = "init" ] && [ "${2:-}" = "zsh" ]; then
  cat <<'INIT'
function z() {
  builtin cd -- "$1"
}

function zi() {
  local dir
  dir="$(zoxide query -i "$@")" || return
  [[ -n "$dir" ]] || return
  builtin cd -- "$dir"
}
INIT
  exit 0
fi

if [ "${1:-}" = "query" ] && [ "${2:-}" = "-i" ]; then
  if [ -n "${ZOXIDE_TEST_SELECTION:-}" ]; then
    printf '%s\n' "$ZOXIDE_TEST_SELECTION"
    exit 0
  fi

  exit 1
fi

if [ "${1:-}" = "query" ] && [ "${2:-}" = "-l" ]; then
  if [ -n "${ZOXIDE_TEST_LIST:-}" ]; then
    printf '%s\n' "$ZOXIDE_TEST_LIST"
  fi

  exit 0
fi

exit 64
STUB

cat >"$tmp_dir/bin/zellij" <<'STUB'
#!/bin/sh
printf '%s\n' "zellij args:$*" >>"$ZELLIJ_TEST_LOG"
STUB

cat >"$tmp_dir/git-only-bin/git" <<'STUB'
#!/bin/sh
if [ "$1" = "rev-parse" ] && [ "$2" = "--is-inside-work-tree" ]; then
  exit 0
fi

exit 64
STUB

chmod +x "$tmp_dir/bin/fzf" "$tmp_dir/bin/zellij" "$tmp_dir/bin/zoxide" "$tmp_dir/git-only-bin/git"

chezmoi_bin="$(command -v chezmoi)" || fail "chezmoi is required to render templates"

export HOME="$tmp_dir/home"
export PATH="$tmp_dir/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export CLAUDECODE=1
export ZOXIDE_TEST_SELECTION="$tmp_dir/selected"
export ZELLIJ_TEST_LOG="$tmp_dir/zellij.log"

: >"$tmp_dir/chezmoi.toml"
: >"$ZELLIJ_TEST_LOG"
render_zshrc '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}'

cd "$tmp_dir/start" || fail "could not enter test start directory"
source "$tmp_dir/home/.zshrc"

if ! alias zj >/dev/null 2>&1; then
  fail "default shell should expose the general Zellij launcher"
fi

pass "default shell exposes the general Zellij launcher"

if [[ "$(whence -w zja)" != "zja: function" ]]; then
  fail "default shell should expose the general Zellij attach helper"
fi

pass "default shell exposes the general Zellij attach helper"

zja
assert_log_contains "zellij args:attach start" "zja attaches to a session named after the current directory"

if alias zd >/dev/null 2>&1; then
  fail "default shell should not expose the Optional Development Workspace launcher"
fi

pass "default shell does not expose the Optional Development Workspace launcher"

if whence -w zdac >/dev/null 2>&1; then
  fail "default shell should not expose the Optional Development Workspace attach-or-create helper"
fi

pass "default shell does not expose the Optional Development Workspace attach-or-create helper"

render_zshrc '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableAiCliTools":true}'

cd "$tmp_dir/start" || fail "could not enter test start directory"
source "$tmp_dir/home/.zshrc"

if ! alias zj >/dev/null 2>&1; then
  fail "zj should remain the general Zellij launcher"
fi

pass "zj remains the general Zellij launcher"

if alias zd >/dev/null 2>&1; then
  fail "enableAiCliTools alone should not expose the Optional Development Workspace launcher"
fi

pass "enableAiCliTools alone does not expose the Optional Development Workspace launcher"

if whence -w zdac >/dev/null 2>&1; then
  fail "enableAiCliTools alone should not expose the Optional Development Workspace attach-or-create helper"
fi

pass "enableAiCliTools alone does not expose the Optional Development Workspace attach-or-create helper"

if alias zi >/dev/null 2>&1; then
  fail ".zshrc should not define a zi alias over zoxide's function"
fi

zi_kind="$(whence -w zi)"
if [[ "$zi_kind" != "zi: function" ]]; then
  fail "zi should resolve to zoxide's generated function, got '$zi_kind'"
fi

pass "zi remains zoxide's generated function"

if ! whence -w zinit >/dev/null 2>&1; then
  fail "zinit should remain available after resolving the zi shortcut conflict"
fi

pass "zinit remains available by its full command name"

if ! eval "zi" >"$tmp_dir/zi.out" 2>&1; then
  fail "zi should complete successfully"
fi

assert_pwd "$tmp_dir/selected" "zi should change directory to the selected path"
pass "zi changes directory to the selected zoxide path"

cd "$tmp_dir/start" || fail "could not reset to test start directory"
export ZOXIDE_TEST_SELECTION=""
eval "zi" >"$tmp_dir/zi-cancel.out" 2>&1
assert_pwd "$tmp_dir/start" "cancelled zi should leave the current directory unchanged"
pass "cancelled zi keeps the current directory"

z "$tmp_dir/selected"
assert_pwd "$tmp_dir/selected" "z should still change directory"
pass "z continues to change directory"

cd "$tmp_dir/start" || fail "could not reset to test start directory"
export FZF_TEST_SELECTION="$tmp_dir/recent"
export ZOXIDE_TEST_LIST="$tmp_dir/recent"$'\n'"$tmp_dir/plain"
zr
assert_pwd "$tmp_dir/recent" "zr should still jump to the selected recent directory"
pass "zr continues to change directory"

cd "$tmp_dir/start" || fail "could not reset to test start directory"
export FZF_TEST_SELECTION="$tmp_dir/git-project"
export ZOXIDE_TEST_LIST="$tmp_dir/git-project"$'\n'"$tmp_dir/plain"
zg
assert_pwd "$tmp_dir/git-project" "zg should still jump to the selected git directory"
pass "zg continues to change directory"

cd "$tmp_dir/plain" || fail "could not enter plain directory"
if wcd >"$tmp_dir/wcd-not-git.out" 2>&1; then
  fail "wcd should fail outside a git repository"
fi
assert_file_contains "$tmp_dir/wcd-not-git.out" "이 디렉토리는 Git 저장소가 아닙니다." "wcd explains non-git directories"

old_path="$PATH"
PATH="$tmp_dir/git-only-bin"
if wcd >"$tmp_dir/wcd-missing-fzf.out" 2>&1; then
  fail "wcd should fail when fzf is unavailable"
fi
PATH="$old_path"
assert_file_contains "$tmp_dir/wcd-missing-fzf.out" "fzf가 설치되어 있지 않습니다." "wcd explains missing fzf"

# `git worktree list` only emits a `bare` record for a bare main repository, so
# the fixture's main is a bare clone and every checkout is a linked worktree.
# The seed repository exists only to give that clone a commit; it is a separate
# repository and never appears in the worktree list.
git -C "$tmp_dir/worktree-seed" init -b main >/dev/null
git -C "$tmp_dir/worktree-seed" config user.email "test@example.com"
git -C "$tmp_dir/worktree-seed" config user.name "Test User"
: >"$tmp_dir/worktree-seed/file.txt"
git -C "$tmp_dir/worktree-seed" add file.txt
git -C "$tmp_dir/worktree-seed" commit -m "initial commit" >/dev/null
git clone --bare "$tmp_dir/worktree-seed" "$tmp_dir/worktree-bare.git" >/dev/null 2>&1
git -C "$tmp_dir/worktree-bare.git" worktree add "$tmp_dir/worktree-main" main >/dev/null 2>&1
git -C "$tmp_dir/worktree-bare.git" worktree add "$tmp_dir/worktree selected" -b feature/worktree >/dev/null 2>&1
git -C "$tmp_dir/worktree-bare.git" worktree add "$tmp_dir/worktree third" -b feature/third >/dev/null 2>&1
# A worktree whose directory has been removed is what git reports as prunable.
git -C "$tmp_dir/worktree-bare.git" worktree add "$tmp_dir/worktree pruned" -b feature/pruned >/dev/null 2>&1
rm -rf "$tmp_dir/worktree pruned"

# Guard the fixture itself: if git stopped reporting either record, the
# exclusion assertions below would pass without testing anything.
typeset -a fixture_porcelain_lines
fixture_porcelain_lines=("${(@f)$(git -C "$tmp_dir/worktree-main" worktree list --porcelain)}")
if (( ! ${fixture_porcelain_lines[(I)bare]} )); then
  fail "fixture should contain a bare worktree record"
fi
if (( ! ${fixture_porcelain_lines[(I)prunable *]} )); then
  fail "fixture should contain a prunable worktree record"
fi
pass "worktree fixture reports one bare and one prunable record"

# The fzf stub answers with FZF_TEST_SELECTION no matter what it is fed, so
# nothing above this point can see the awk half of wcd's pipeline. These
# assertions read what wcd actually wrote to fzf's stdin.
cd "$tmp_dir/worktree-main" || fail "could not enter worktree main directory"
export FZF_TEST_STDIN_LOG="$tmp_dir/wcd-fzf-stdin.log"
export FZF_TEST_SELECTION=""
wcd
cd "$tmp_dir/worktree-main" || fail "could not reset to worktree main directory"
unset FZF_TEST_STDIN_LOG

typeset -a pipeline_lines
# .zshrc aliases cat to bat, which is not on the stub PATH.
pipeline_lines=("${(@f)$(command cat "$tmp_dir/wcd-fzf-stdin.log")}")

if (( ${#pipeline_lines} != 3 )); then
  fail "wcd should offer one row per ordinary worktree; got ${#pipeline_lines} for 3 ordinary, 1 bare and 1 prunable"
fi
pass "wcd offers one row per ordinary worktree"

# git reports resolved paths, and mktemp -d hands out a symlinked one on macOS.
bare_path="${tmp_dir:A}/worktree-bare.git"
pruned_path="${tmp_dir:A}/worktree pruned"
selected_row=""

typeset -a offered_paths
offered_paths=()
for pipeline_line in "${pipeline_lines[@]}"; do
  worktree_path="${pipeline_line%%$'\t'*}"
  worktree_row="${pipeline_line#*$'\t'}"

  if [[ "$worktree_path" == "$bare_path" ]]; then
    fail "wcd should not offer the bare repository; got '$worktree_path'"
  fi

  if [[ "$worktree_path" == "$pruned_path" ]]; then
    fail "wcd should not offer a prunable worktree; got '$worktree_path'"
  fi

  if [[ ! -d "$worktree_path" ]]; then
    fail "wcd's first field should be a worktree directory; got '$worktree_path'"
  fi

  # The display half is built from the same porcelain record as the path, so
  # it starts with that path just as `git worktree list` rows do.
  if [[ "$worktree_row" != "$worktree_path"* ]]; then
    fail "wcd should pair each path with its own display row; '$worktree_path' got '$worktree_row'"
  fi

  if [[ "$worktree_path" == "${tmp_dir:A}/worktree selected" ]]; then
    selected_row="$pipeline_line"
  fi

  offered_paths+=("$worktree_path")
done
pass "wcd does not offer the bare repository"
pass "wcd does not offer a prunable worktree"
pass "wcd's first field is the worktree directory cd receives"
pass "wcd pairs each path with its own display row"

# git lists worktrees in its own order, so both sides are sorted before the
# comparison. Without the @, a nested ${(o)array} collapses to one unsorted word.
typeset -a expected_paths
expected_paths=("${tmp_dir:A}/worktree-main" "${tmp_dir:A}/worktree selected" "${tmp_dir:A}/worktree third")
if [[ "${(j:\n:)${(@o)offered_paths}}" != "${(j:\n:)${(@o)expected_paths}}" ]]; then
  fail "wcd should offer every ordinary worktree path verbatim; got ${(j:, :)offered_paths}"
fi
pass "wcd offers every ordinary worktree path verbatim, spaces included"

if [[ -z "$selected_row" ]]; then
  fail "wcd should offer the worktree the selection test picks"
fi
assert_contains "${selected_row#*$'\t'}" "[feature/worktree]" "wcd's display row names the worktree's branch"

# Selecting a row the pipeline really produced proves the field cd splits on
# is the one wcd wrote, not one the test made up.
cd "$tmp_dir/worktree-main" || fail "could not reset to worktree main directory"
export FZF_TEST_SELECTION="$selected_row"
wcd
assert_pwd "$tmp_dir/worktree selected" "wcd should jump to the selected worktree path"
pass "wcd changes directory to a selected worktree path containing spaces"

cd "$tmp_dir/worktree-main" || fail "could not reset to worktree main directory"
export FZF_TEST_SELECTION=""
if ! wcd >"$tmp_dir/wcd-cancel.out" 2>&1; then
  fail "cancelled wcd should complete successfully"
fi
assert_pwd "$tmp_dir/worktree-main" "cancelled wcd should leave the current directory unchanged"
pass "cancelled wcd keeps the current directory"

render_zshrc '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableDevelopmentWorkspace":true}'
source "$tmp_dir/home/.zshrc"

if ! alias zd >/dev/null 2>&1; then
  fail "enableDevelopmentWorkspace should expose the Optional Development Workspace launcher"
fi

pass "enableDevelopmentWorkspace exposes the Optional Development Workspace launcher"

if [[ "$(whence -w zdac)" != "zdac: function" ]]; then
  fail "enableDevelopmentWorkspace should expose the Optional Development Workspace attach-or-create helper"
fi

pass "enableDevelopmentWorkspace exposes the Optional Development Workspace attach-or-create helper"

cd "$tmp_dir/git-project" || fail "could not enter git project directory"
: >"$ZELLIJ_TEST_LOG"
zdac
assert_log_contains "zellij args:--layout dev attach --create git-project" "zdac creates or attaches to a dev-layout session named after the current directory"
