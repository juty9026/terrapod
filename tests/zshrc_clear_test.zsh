#!/usr/bin/env zsh

set -u

repo_root="${0:A:h:h}"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

assert_log_contains() {
  local expected="$1"
  local message="$2"

  if ! command grep -F "$expected" "$CLEAR_TEST_LOG" >/dev/null 2>&1; then
    fail "$message; expected log to contain '$expected'"
  fi

  pass "$message"
}

assert_startup_fastfetch() {
  local expected="$1"
  local message="$2"

  : >"$CLEAR_TEST_LOG"
  source "$tmp_dir/home/.zshrc"

  if command grep -F "fastfetch" "$CLEAR_TEST_LOG" >/dev/null 2>&1; then
    if [[ "$expected" != ran ]]; then
      fail "$message; fastfetch should not have run at startup"
    fi
  elif [[ "$expected" != skipped ]]; then
    fail "$message; expected fastfetch to run at startup"
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

mkdir -p "$tmp_dir/bin" "$tmp_dir/clear-only-bin" "$tmp_dir/home" "$tmp_dir/home/.scm_breeze"

cat >"$tmp_dir/home/.scm_breeze/scm_breeze.sh" <<'STUB'
function git_index() {
  print "git_index" >>"$CLEAR_TEST_LOG"
}

alias c=git_index
STUB

cat >"$tmp_dir/bin/clear" <<'STUB'
#!/bin/sh
printf '%s\n' "clear" >>"$CLEAR_TEST_LOG"
STUB

cat >"$tmp_dir/bin/fastfetch" <<'STUB'
#!/bin/sh
printf '%s\n' "fastfetch" >>"$CLEAR_TEST_LOG"
STUB

cp "$tmp_dir/bin/clear" "$tmp_dir/clear-only-bin/clear"

chmod +x "$tmp_dir/bin/clear" "$tmp_dir/bin/fastfetch" "$tmp_dir/clear-only-bin/clear"

chezmoi_bin="$(command -v chezmoi)" || fail "chezmoi is required to render templates"

export HOME="$tmp_dir/home"
export PATH="$tmp_dir/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export CLEAR_TEST_LOG="$tmp_dir/clear.log"
# Empty rather than unset: .zshrc reads $CLAUDECODE and this test runs under set -u.
export CLAUDECODE=""

: >"$tmp_dir/chezmoi.toml"
: >"$CLEAR_TEST_LOG"
render_zshrc '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}'

source "$tmp_dir/home/.zshrc"

if alias c >/dev/null 2>&1; then
  fail "c should no longer be a plain clear alias"
fi

pass "c is not a plain clear alias"

if [[ "$(whence -w c)" != "c: function" ]]; then
  fail "shell should expose the clear helper as a function"
fi

pass "shell exposes the clear helper as a function"

: >"$CLEAR_TEST_LOG"
c
assert_log_contains "clear" "c clears the screen"
assert_log_contains "fastfetch" "c prints system information after clearing"

if [[ "$(command cat "$CLEAR_TEST_LOG")" != $'clear\nfastfetch' ]]; then
  fail "c should clear the screen before printing system information"
fi

pass "c clears the screen before printing system information"

old_path="$PATH"
PATH="$tmp_dir/clear-only-bin:/usr/bin:/bin:/usr/sbin:/sbin"
: >"$CLEAR_TEST_LOG"
if ! c >"$tmp_dir/c-missing-fastfetch.out" 2>&1; then
  PATH="$old_path"
  fail "c should succeed when fastfetch is unavailable"
fi
PATH="$old_path"

assert_log_contains "clear" "c still clears the screen without fastfetch"

if command grep -F "fastfetch" "$CLEAR_TEST_LOG" >/dev/null 2>&1; then
  fail "c should not run fastfetch when it is unavailable"
fi

pass "c skips system information when fastfetch is unavailable"

if alias c >/dev/null 2>&1; then
  fail "the SCM Breeze repo index alias should not shadow the clear helper"
fi

pass "the SCM Breeze repo index alias does not shadow the clear helper"

: >"$CLEAR_TEST_LOG"
c
if command grep -F "git_index" "$CLEAR_TEST_LOG" >/dev/null 2>&1; then
  fail "c should not fall through to the SCM Breeze repo index"
fi

pass "c does not fall through to the SCM Breeze repo index"

if ! whence -w git_index >/dev/null 2>&1; then
  fail "SCM Breeze should keep its repo index under its full command name"
fi

pass "SCM Breeze keeps its repo index under its full command name"

# The greeting belongs to the first shell of a session. Zellij panes and nested
# shells inherit .zshrc too, and the dev layout opens five panes at once.
original_shlvl="$SHLVL"

SHLVL=1
unset ZELLIJ
assert_startup_fastfetch ran "the first shell greets with system information"

export ZELLIJ="0"
assert_startup_fastfetch skipped "a Zellij pane does not repeat the greeting"

: >"$CLEAR_TEST_LOG"
c
assert_log_contains "fastfetch" "c still prints system information inside a Zellij pane"

unset ZELLIJ
SHLVL=2
assert_startup_fastfetch skipped "a nested shell does not repeat the greeting"

SHLVL="$original_shlvl"
