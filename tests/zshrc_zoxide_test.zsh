#!/usr/bin/env zsh

set -u

repo_root="${0:A:h:h}"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  print -u2 -- "not ok - $1"
  exit 1
}

pass() {
  print -- "ok - $1"
}

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
  "$tmp_dir/home" \
  "$tmp_dir/home/.local/share/zinit/zinit.git" \
  "$tmp_dir/start" \
  "$tmp_dir/selected" \
  "$tmp_dir/recent" \
  "$tmp_dir/git-project/.git" \
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

if [ -n "${FZF_TEST_SELECTION:-}" ]; then
  cat >/dev/null
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

chmod +x "$tmp_dir/bin/fzf" "$tmp_dir/bin/zellij" "$tmp_dir/bin/zoxide"

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
