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

assert_contains() {
  haystack="$1"
  needle="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  haystack="$1"
  needle="$2"
  message="$3"

  if printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
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

mkdir -p "$tmp_dir/bin" "$tmp_dir/home"

rendered="$tmp_dir/shell-integrations.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux"}}' \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl" \
  >"$rendered"

sh -n "$rendered" || fail "rendered shell integrations script should be valid sh"
pass "rendered shell integrations script is valid sh"

write_stub "$tmp_dir/bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$SHELL_INTEGRATIONS_TEST_LOG"' \
  'exit "${SHELL_INTEGRATIONS_CURL_STATUS:-0}"'

write_stub "$tmp_dir/bin/git" \
  'printf "%s\n" "git args:$*" >>"$SHELL_INTEGRATIONS_TEST_LOG"' \
  'if [ "$1" = clone ] && [ "$2" = https://github.com/zdharma-continuum/zinit ]; then' \
  '  zinit_status="${SHELL_INTEGRATIONS_ZINIT_STATUS:-0}"' \
  '  if [ "$zinit_status" -ne 0 ]; then' \
  '    exit "$zinit_status"' \
  '  fi' \
  '  mkdir -p "$3"' \
  '  exit 0' \
  'fi' \
  'if [ "$1" = clone ] && [ "$2" = https://github.com/scmbreeze/scm_breeze.git ]; then' \
  '  mkdir -p "$3"' \
  '  printf "%s\n" "#!/bin/sh" "printf \"%s\\n\" \"scm-breeze install\" >>\"\$SHELL_INTEGRATIONS_TEST_LOG\"" "exit \"\${SHELL_INTEGRATIONS_SCM_INSTALL_STATUS:-0}\"" >"$3/install.sh"' \
  '  chmod +x "$3/install.sh"' \
  '  exit 0' \
  'fi' \
  'exit 64'

export PATH="$tmp_dir/bin:/usr/bin:/bin"
export HOME="$tmp_dir/home"
export XDG_STATE_HOME="$tmp_dir/state"
export SHELL_INTEGRATIONS_TEST_LOG="$tmp_dir/shell-integrations.log"

HOME="$HOME" XDG_STATE_HOME="$XDG_STATE_HOME" sh -c \
  '. "$1"; terrapod_install_warning_write shell-integrations "Shell integration setup needs attention" "Previous shell integration warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

SHELL_INTEGRATIONS_CURL_STATUS=23
export SHELL_INTEGRATIONS_CURL_STATUS
if ! sh "$rendered" >"$tmp_dir/shell-integrations-curl-failure.out" 2>"$tmp_dir/shell-integrations-curl-failure.err"; then
  unset SHELL_INTEGRATIONS_CURL_STATUS
  fail "shell integrations should continue after recording an Oh My Zsh installer download warning"
fi
unset SHELL_INTEGRATIONS_CURL_STATUS

shell_integrations_marker="$XDG_STATE_HOME/terrapod/install-warnings/shell-integrations"
if [ ! -f "$shell_integrations_marker" ]; then
  fail "shell integrations should keep a warning marker when the Oh My Zsh installer download fails"
fi
pass "shell integrations keeps a warning marker when the Oh My Zsh installer download fails"

marker_text="$(cat "$shell_integrations_marker")"
assert_contains "$marker_text" "summary='Shell integration setup needs attention'" "shell integrations marker keeps the expected summary"
assert_contains "$marker_text" "Oh My Zsh" "shell integrations marker mentions the failed Oh My Zsh step"

test_log="$(cat "$SHELL_INTEGRATIONS_TEST_LOG")"
assert_contains "$test_log" "curl args:-fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh" "shell integrations attempts to download the Oh My Zsh installer"
assert_contains "$test_log" "git args:clone https://github.com/zdharma-continuum/zinit" "shell integrations continues to zinit when Oh My Zsh download fails"
assert_contains "$test_log" "git args:clone https://github.com/scmbreeze/scm_breeze.git" "shell integrations continues to SCM Breeze when Oh My Zsh download fails"

first_marker_text="$marker_text"

: >"$SHELL_INTEGRATIONS_TEST_LOG"
rm -rf "$HOME/.local/share/zinit/zinit.git"
SHELL_INTEGRATIONS_ZINIT_STATUS=31
export SHELL_INTEGRATIONS_ZINIT_STATUS
if ! sh "$rendered" >"$tmp_dir/shell-integrations-zinit-failure.out" 2>"$tmp_dir/shell-integrations-zinit-failure.err"; then
  unset SHELL_INTEGRATIONS_ZINIT_STATUS
  fail "shell integrations should continue after replacing a warning marker for zinit failure"
fi
unset SHELL_INTEGRATIONS_ZINIT_STATUS

if [ ! -f "$shell_integrations_marker" ]; then
  fail "shell integrations should keep a warning marker when zinit fails on rerun"
fi
pass "shell integrations keeps a warning marker when zinit fails on rerun"

replacement_marker_text="$(cat "$shell_integrations_marker")"
assert_contains "$replacement_marker_text" "summary='Shell integration setup needs attention'" "shell integrations replacement marker keeps the expected summary"
assert_contains "$replacement_marker_text" "zinit" "shell integrations replacement marker mentions the failed zinit step"
assert_not_contains "$replacement_marker_text" "Oh My Zsh" "shell integrations replacement marker drops the previous Oh My Zsh failure"
assert_contains "$replacement_marker_text" "updated_at='" "shell integrations replacement marker records update time"
if [ "$replacement_marker_text" = "$first_marker_text" ]; then
  fail "shell integrations replacement marker should change from the first marker"
fi
pass "shell integrations replacement marker changes from the first marker"

: >"$SHELL_INTEGRATIONS_TEST_LOG"
if ! sh "$rendered" >"$tmp_dir/shell-integrations-successful-rerun.out" 2>"$tmp_dir/shell-integrations-successful-rerun.err"; then
  fail "shell integrations should succeed when all installers succeed on rerun"
fi

successful_rerun_log="$(cat "$SHELL_INTEGRATIONS_TEST_LOG")"
assert_contains "$successful_rerun_log" "git args:clone https://github.com/zdharma-continuum/zinit" "shell integrations retries zinit during successful rerun"

if [ -f "$shell_integrations_marker" ]; then
  fail "shell integrations should clear warning marker after successful rerun"
fi
pass "shell integrations clears warning marker after successful rerun"

rm -f "$shell_integrations_marker"
: >"$SHELL_INTEGRATIONS_TEST_LOG"
SHELL_INTEGRATIONS_CURL_STATUS=23
export SHELL_INTEGRATIONS_CURL_STATUS
if ! TERRAPOD_FIRST_RUN_APPLY=1 sh "$rendered" >"$tmp_dir/shell-integrations-first-run-curl-failure.out" 2>"$tmp_dir/shell-integrations-first-run-curl-failure.err"; then
  unset SHELL_INTEGRATIONS_CURL_STATUS
  fail "first-run shell integrations should continue when the Oh My Zsh installer download warning is recorded"
fi
unset SHELL_INTEGRATIONS_CURL_STATUS

if [ ! -f "$shell_integrations_marker" ]; then
  fail "first-run shell integrations should record a warning marker when the Oh My Zsh installer download fails"
fi
pass "first-run shell integrations records a warning marker when the Oh My Zsh installer download fails"

export HOME="$tmp_dir/scm-home"
export XDG_STATE_HOME="$tmp_dir/scm-state"
export SHELL_INTEGRATIONS_TEST_LOG="$tmp_dir/scm-shell-integrations.log"
mkdir -p "$HOME"
: >"$SHELL_INTEGRATIONS_TEST_LOG"
scm_shell_integrations_marker="$XDG_STATE_HOME/terrapod/install-warnings/shell-integrations"

SHELL_INTEGRATIONS_SCM_INSTALL_STATUS=42
export SHELL_INTEGRATIONS_SCM_INSTALL_STATUS
if ! sh "$rendered" >"$tmp_dir/shell-integrations-scm-install-failure.out" 2>"$tmp_dir/shell-integrations-scm-install-failure.err"; then
  unset SHELL_INTEGRATIONS_SCM_INSTALL_STATUS
  fail "shell integrations should continue after recording an SCM Breeze installer warning"
fi

if [ ! -f "$scm_shell_integrations_marker" ]; then
  unset SHELL_INTEGRATIONS_SCM_INSTALL_STATUS
  fail "shell integrations should keep a warning marker when SCM Breeze installer fails"
fi
pass "shell integrations keeps a warning marker when SCM Breeze installer fails"

scm_marker_text="$(cat "$scm_shell_integrations_marker")"
assert_contains "$scm_marker_text" "SCM Breeze" "shell integrations marker mentions the failed SCM Breeze installer"

: >"$SHELL_INTEGRATIONS_TEST_LOG"
if ! sh "$rendered" >"$tmp_dir/shell-integrations-scm-install-failure-rerun.out" 2>"$tmp_dir/shell-integrations-scm-install-failure-rerun.err"; then
  unset SHELL_INTEGRATIONS_SCM_INSTALL_STATUS
  fail "shell integrations should continue after retrying a failed SCM Breeze installer"
fi

if [ ! -f "$scm_shell_integrations_marker" ]; then
  unset SHELL_INTEGRATIONS_SCM_INSTALL_STATUS
  fail "shell integrations should keep a warning marker when SCM Breeze installer fails on rerun"
fi
pass "shell integrations keeps a warning marker when SCM Breeze installer fails on rerun"

scm_replacement_marker_text="$(cat "$scm_shell_integrations_marker")"
assert_contains "$scm_replacement_marker_text" "SCM Breeze" "shell integrations replacement marker keeps the SCM Breeze failure"
if [ "$scm_replacement_marker_text" = "$scm_marker_text" ]; then
  unset SHELL_INTEGRATIONS_SCM_INSTALL_STATUS
  fail "shell integrations should replace the SCM Breeze marker on failed rerun"
fi
pass "shell integrations replaces the SCM Breeze marker on failed rerun"

scm_failure_rerun_log="$(cat "$SHELL_INTEGRATIONS_TEST_LOG")"
assert_contains "$scm_failure_rerun_log" "scm-breeze install" "shell integrations retries SCM Breeze installer when warning marker exists"

SHELL_INTEGRATIONS_SCM_INSTALL_STATUS=0
export SHELL_INTEGRATIONS_SCM_INSTALL_STATUS
: >"$SHELL_INTEGRATIONS_TEST_LOG"
if ! sh "$rendered" >"$tmp_dir/shell-integrations-scm-install-success-rerun.out" 2>"$tmp_dir/shell-integrations-scm-install-success-rerun.err"; then
  unset SHELL_INTEGRATIONS_SCM_INSTALL_STATUS
  fail "shell integrations should succeed after SCM Breeze installer recovers"
fi
unset SHELL_INTEGRATIONS_SCM_INSTALL_STATUS

scm_success_rerun_log="$(cat "$SHELL_INTEGRATIONS_TEST_LOG")"
assert_contains "$scm_success_rerun_log" "scm-breeze install" "shell integrations reruns SCM Breeze installer during successful recovery"

if [ -f "$scm_shell_integrations_marker" ]; then
  fail "shell integrations should clear warning marker after SCM Breeze installer recovers"
fi
pass "shell integrations clears warning marker after SCM Breeze installer recovers"
