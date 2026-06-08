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

retry_rendered="$tmp_dir/shell-integrations-retry.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux"}}' \
  --file "$repo_root/.chezmoiscripts/run_before_31-retry-shell-integrations.sh.tmpl" \
  >"$retry_rendered"

sh -n "$rendered" || fail "rendered shell integrations script should be valid sh"
pass "rendered shell integrations script is valid sh"

sh -n "$retry_rendered" || fail "rendered shell integrations retry script should be valid sh"
pass "rendered shell integrations retry script is valid sh"

write_stub "$tmp_dir/bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$SHELL_INTEGRATIONS_TEST_LOG"' \
  'output_file=' \
  'while [ "$#" -gt 0 ]; do' \
  '  case "$1" in' \
  '    -o)' \
  '      shift' \
  '      output_file="$1"' \
  '      ;;' \
  '  esac' \
  '  shift' \
  'done' \
  'curl_status="${SHELL_INTEGRATIONS_CURL_STATUS:-0}"' \
  'if [ "$curl_status" -ne 0 ]; then' \
  '  exit "$curl_status"' \
  'fi' \
  'if [ -n "$output_file" ]; then' \
  '  printf "%s\n" "#!/bin/sh" "mkdir -p \"\$HOME/.oh-my-zsh\"" ": >\"\$HOME/.oh-my-zsh/oh-my-zsh.sh\"" "exit \"\${SHELL_INTEGRATIONS_OMZ_INSTALL_STATUS:-0}\"" >"$output_file"' \
  'fi' \
  'exit "${SHELL_INTEGRATIONS_CURL_STATUS:-0}"'

write_stub "$tmp_dir/bin/git" \
  'printf "%s\n" "git args:$*" >>"$SHELL_INTEGRATIONS_TEST_LOG"' \
  'if [ "$1" = clone ] && [ "$2" = https://github.com/zdharma-continuum/zinit ]; then' \
  '  zinit_status="${SHELL_INTEGRATIONS_ZINIT_STATUS:-0}"' \
  '  if [ "$zinit_status" -ne 0 ]; then' \
  '    exit "$zinit_status"' \
  '  fi' \
  '  mkdir -p "$3"' \
  '  : >"$3/zinit.zsh"' \
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

retry_no_marker_home="$tmp_dir/retry-no-marker-home"
retry_no_marker_state="$tmp_dir/retry-no-marker-state"
retry_no_marker_log="$tmp_dir/retry-no-marker-shell-integrations.log"
mkdir -p "$retry_no_marker_home"
: >"$retry_no_marker_log"
HOME="$retry_no_marker_home" \
  XDG_STATE_HOME="$retry_no_marker_state" \
  SHELL_INTEGRATIONS_TEST_LOG="$retry_no_marker_log" \
  SHELL_INTEGRATIONS_CURL_STATUS=23 \
  PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$retry_rendered" >"$tmp_dir/shell-integrations-retry-no-marker.out" 2>"$tmp_dir/shell-integrations-retry-no-marker.err"
if [ -s "$retry_no_marker_log" ]; then
  fail "shell integrations retry should be a no-op when no marker exists"
fi
pass "shell integrations retry is a no-op when no marker exists"

retry_home="$tmp_dir/retry-home"
retry_state="$tmp_dir/retry-state"
retry_log="$tmp_dir/retry-shell-integrations.log"
mkdir -p "$retry_home"
: >"$retry_log"
HOME="$retry_home" XDG_STATE_HOME="$retry_state" sh -c \
  '. "$1"; terrapod_install_warning_write shell-integrations "Shell integration setup needs attention" "Previous shell integration warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

SHELL_INTEGRATIONS_CURL_STATUS=23 \
  HOME="$retry_home" \
  XDG_STATE_HOME="$retry_state" \
  SHELL_INTEGRATIONS_TEST_LOG="$retry_log" \
  PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$retry_rendered" >"$tmp_dir/shell-integrations-retry-curl-failure.out" 2>"$tmp_dir/shell-integrations-retry-curl-failure.err"

retry_marker="$retry_state/terrapod/install-warnings/shell-integrations"
if [ ! -f "$retry_marker" ]; then
  fail "shell integrations retry should keep a warning marker when retry still fails"
fi
pass "shell integrations retry keeps a warning marker when retry still fails"

retry_marker_text="$(cat "$retry_marker")"
assert_contains "$retry_marker_text" "Oh My Zsh" "shell integrations retry marker mentions the failed Oh My Zsh step"
retry_log_text="$(cat "$retry_log")"
assert_contains "$retry_log_text" "curl args:-fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh" "shell integrations retry attempts Oh My Zsh when marker exists"
assert_contains "$retry_log_text" "git args:clone https://github.com/zdharma-continuum/zinit" "shell integrations retry continues to zinit when Oh My Zsh fails"

: >"$retry_log"
HOME="$retry_home" \
  XDG_STATE_HOME="$retry_state" \
  SHELL_INTEGRATIONS_TEST_LOG="$retry_log" \
  PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$retry_rendered" >"$tmp_dir/shell-integrations-retry-recovery.out" 2>"$tmp_dir/shell-integrations-retry-recovery.err"
if [ -e "$retry_marker" ]; then
  fail "shell integrations retry should clear warning marker after recovery"
fi
pass "shell integrations retry clears warning marker after recovery"

partial_retry_home="$tmp_dir/partial-retry-home"
partial_retry_state="$tmp_dir/partial-retry-state"
partial_retry_log="$tmp_dir/partial-retry-shell-integrations.log"
mkdir -p \
  "$partial_retry_home/.oh-my-zsh" \
  "$partial_retry_home/.local/share/zinit/zinit.git" \
  "$partial_retry_home/.scm_breeze"
: >"$partial_retry_log"
HOME="$partial_retry_home" XDG_STATE_HOME="$partial_retry_state" sh -c \
  '. "$1"; terrapod_install_warning_write shell-integrations "Shell integration setup needs attention" "Previous shell integration warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

HOME="$partial_retry_home" \
  XDG_STATE_HOME="$partial_retry_state" \
  SHELL_INTEGRATIONS_TEST_LOG="$partial_retry_log" \
  PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$retry_rendered" >"$tmp_dir/shell-integrations-retry-partial-dirs.out" 2>"$tmp_dir/shell-integrations-retry-partial-dirs.err"

partial_retry_marker="$partial_retry_state/terrapod/install-warnings/shell-integrations"
if [ -f "$partial_retry_marker" ]; then
  fail "shell integrations retry should clear marker after reinstalling partial directories"
fi
pass "shell integrations retry clears marker after reinstalling partial directories"

if [ ! -f "$partial_retry_home/.oh-my-zsh/oh-my-zsh.sh" ]; then
  fail "shell integrations retry reinstalls partial Oh My Zsh directory"
fi
pass "shell integrations retry reinstalls partial Oh My Zsh directory"

if [ ! -f "$partial_retry_home/.local/share/zinit/zinit.git/zinit.zsh" ]; then
  fail "shell integrations retry reclones partial zinit directory"
fi
pass "shell integrations retry reclones partial zinit directory"

if [ ! -x "$partial_retry_home/.scm_breeze/install.sh" ]; then
  fail "shell integrations retry reclones partial SCM Breeze directory"
fi
pass "shell integrations retry reclones partial SCM Breeze directory"

partial_retry_log_text="$(cat "$partial_retry_log")"
assert_contains "$partial_retry_log_text" "curl args:-fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh" "shell integrations retry reruns Oh My Zsh installer for partial directory"
assert_contains "$partial_retry_log_text" "git args:clone https://github.com/zdharma-continuum/zinit" "shell integrations retry reclones zinit for partial directory"
assert_contains "$partial_retry_log_text" "git args:clone https://github.com/scmbreeze/scm_breeze.git" "shell integrations retry reclones SCM Breeze for partial directory"

partial_onchange_home="$tmp_dir/partial-onchange-home"
partial_onchange_state="$tmp_dir/partial-onchange-state"
partial_onchange_log="$tmp_dir/partial-onchange-shell-integrations.log"
mkdir -p \
  "$partial_onchange_home/.oh-my-zsh" \
  "$partial_onchange_home/.local/share/zinit/zinit.git" \
  "$partial_onchange_home/.scm_breeze"
: >"$partial_onchange_log"
HOME="$partial_onchange_home" XDG_STATE_HOME="$partial_onchange_state" sh -c \
  '. "$1"; terrapod_install_warning_write shell-integrations "Shell integration setup needs attention" "Previous shell integration warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

HOME="$partial_onchange_home" \
  XDG_STATE_HOME="$partial_onchange_state" \
  SHELL_INTEGRATIONS_TEST_LOG="$partial_onchange_log" \
  PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$rendered" >"$tmp_dir/shell-integrations-onchange-partial-dirs.out" 2>"$tmp_dir/shell-integrations-onchange-partial-dirs.err"

partial_onchange_marker="$partial_onchange_state/terrapod/install-warnings/shell-integrations"
if [ -f "$partial_onchange_marker" ]; then
  fail "shell integrations onchange should clear marker after reinstalling partial directories"
fi
pass "shell integrations onchange clears marker after reinstalling partial directories"

if [ ! -f "$partial_onchange_home/.oh-my-zsh/oh-my-zsh.sh" ]; then
  fail "shell integrations onchange reinstalls partial Oh My Zsh directory"
fi
pass "shell integrations onchange reinstalls partial Oh My Zsh directory"

if [ ! -f "$partial_onchange_home/.local/share/zinit/zinit.git/zinit.zsh" ]; then
  fail "shell integrations onchange reclones partial zinit directory"
fi
pass "shell integrations onchange reclones partial zinit directory"

if [ ! -x "$partial_onchange_home/.scm_breeze/install.sh" ]; then
  fail "shell integrations onchange reclones partial SCM Breeze directory"
fi
pass "shell integrations onchange reclones partial SCM Breeze directory"

partial_onchange_log_text="$(cat "$partial_onchange_log")"
assert_contains "$partial_onchange_log_text" "curl args:-fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh" "shell integrations onchange reruns Oh My Zsh installer for partial directory"
assert_contains "$partial_onchange_log_text" "git args:clone https://github.com/zdharma-continuum/zinit" "shell integrations onchange reclones zinit for partial directory"
assert_contains "$partial_onchange_log_text" "git args:clone https://github.com/scmbreeze/scm_breeze.git" "shell integrations onchange reclones SCM Breeze for partial directory"

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
rm -rf "$HOME/.oh-my-zsh"
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
