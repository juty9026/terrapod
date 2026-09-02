#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir
chezmoi_bin="$(command -v chezmoi)"

assert_first_occurrence_before() {
  haystack="$1"
  first="$2"
  second="$3"
  message="$4"

  if ! printf '%s\n' "$haystack" | awk -v first="$first" -v second="$second" '
    first_line == 0 && index($0, first) { first_line = NR }
    second_line == 0 && index($0, second) { second_line = NR }
    END { exit !(first_line > 0 && second_line > 0 && first_line < second_line) }
  '; then
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

rendered="$tmp_dir/bootstrap-ubuntu.sh"
"$chezmoi_bin" execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableAiCliTools":false,"enableDevelopmentWorkspace":false}' \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl" \
  >"$rendered"
sh -n "$rendered" || fail "rendered Ubuntu bootstrap script should be valid sh"
pass "rendered Ubuntu bootstrap script is valid sh"

write_stub "$tmp_dir/bin/id" \
  'case "$1" in' \
  '  -u) printf "%s\n" 1000 ;;' \
  '  -un) printf "%s\n" testuser ;;' \
  '  *) exit 64 ;;' \
  'esac'

write_stub "$tmp_dir/bin/sudo" \
  'exec "$@"'

write_stub "$tmp_dir/bin/apt-get" \
  'printf "%s\n" "apt-get args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'if [ "$1" = "install" ] && [ "${BOOTSTRAP_APT_INSTALL_STATUS:-0}" != "0" ]; then' \
  '  exit "$BOOTSTRAP_APT_INSTALL_STATUS"' \
  'fi' \
  'exit 0'

write_stub "$tmp_dir/bin/install" \
  'printf "%s\n" "install args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'exit 0'

write_stub "$tmp_dir/bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'case "$*" in' \
  '  *mise.en.dev*) printf "%s\n" mise-key ;;' \
  '  *repo.charm.sh*)' \
  '    if [ "${BOOTSTRAP_CHARM_KEY_CURL_STATUS:-0}" != "0" ]; then' \
  '      exit "$BOOTSTRAP_CHARM_KEY_CURL_STATUS"' \
  '    fi' \
  '    output_file=""' \
  '    want_output_file="no"' \
  '    for arg do' \
  '      if [ "$want_output_file" = "yes" ]; then' \
  '        output_file="$arg"' \
  '        want_output_file="no"' \
  '        continue' \
  '      fi' \
  '      if [ "$arg" = "-o" ]; then' \
  '        want_output_file="yes"' \
  '      fi' \
  '    done' \
  '    if [ -n "$output_file" ]; then' \
  '      printf "%s\n" charm-key >"$output_file"' \
  '    else' \
  '      printf "%s\n" charm-key' \
  '    fi' \
  '    ;;' \
  '  *) exit 64 ;;' \
  'esac'

write_stub "$tmp_dir/bin/gpg" \
  'printf "%s\n" "gpg args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'cat >/dev/null'

write_stub "$tmp_dir/bin/tee" \
  'printf "%s\n" "tee args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'while IFS= read -r line || [ -n "$line" ]; do' \
  '  printf "%s\n" "tee stdin:$line" >>"$BOOTSTRAP_TEST_LOG"' \
  'done'

write_stub "$tmp_dir/bin/getent" \
  'if [ "$1" = passwd ] && [ "$2" = testuser ]; then' \
  '  printf "%s\n" "testuser:x:1000:1000::/home/testuser:/bin/bash"' \
  '  exit 0' \
  'fi' \
  'exit 2'

write_stub "$tmp_dir/bin/zsh" \
  'printf "%s\n" "PATH zsh should not be used for login shell changes" >&2' \
  'exit 1'

write_stub "$tmp_dir/bin/chsh" \
  'printf "%s\n" "$*" >"$CHSH_TEST_LOG"' \
  'exit "${CHSH_TEST_STATUS:-0}"'

export PATH="$tmp_dir/bin:/usr/bin:/bin"
export HOME="$tmp_dir/home"
export CHSH_TEST_LOG="$tmp_dir/chsh.log"
export BOOTSTRAP_TEST_LOG="$tmp_dir/bootstrap.log"

sh "$rendered" >"$tmp_dir/bootstrap.out" 2>"$tmp_dir/bootstrap.err"

expected="-s /usr/bin/zsh testuser"
actual="$(cat "$CHSH_TEST_LOG" 2>/dev/null || true)"

if [ "$actual" != "$expected" ]; then
  fail "Ubuntu bootstrap should set the login shell to zsh; expected '$expected', got '${actual:-<no chsh call>}'"
fi

pass "Ubuntu bootstrap sets the login shell to zsh"

bootstrap_log="$(cat "$BOOTSTRAP_TEST_LOG")"
assert_contains "$bootstrap_log" "apt-get args:install -y build-essential ca-certificates curl file git" "Ubuntu declared bootstrap installs Homebrew prerequisites"
assert_contains "$bootstrap_log" "procps" "Ubuntu declared bootstrap includes the Homebrew procps prerequisite"
assert_not_contains "$bootstrap_log" "mise.en.dev" "Ubuntu declared bootstrap removes the mise APT repository"
assert_not_contains "$bootstrap_log" "repo.charm.sh" "Ubuntu declared bootstrap removes the Charm APT repository"
assert_not_contains "$bootstrap_log" "apt-get args:install -y mise" "Ubuntu declared bootstrap does not install mise through APT"
assert_not_contains "$bootstrap_log" "apt-get args:install -y gum" "Ubuntu declared bootstrap does not install gum through APT"

rm -rf "$HOME/.local/state/terrapod/install-warnings"
: >"$BOOTSTRAP_TEST_LOG"
CHSH_TEST_STATUS=23
export CHSH_TEST_STATUS
if ! sh "$rendered" >"$tmp_dir/bootstrap-chsh-failure.out" 2>"$tmp_dir/bootstrap-chsh-failure.err"; then
  unset CHSH_TEST_STATUS
  fail "Ubuntu bootstrap should continue when automatic login shell change fails"
fi
unset CHSH_TEST_STATUS

ubuntu_bootstrap_marker="$HOME/.local/state/terrapod/install-warnings/ubuntu-bootstrap"
shell_integrations_marker="$HOME/.local/state/terrapod/install-warnings/shell-integrations"
if [ ! -f "$ubuntu_bootstrap_marker" ]; then
  fail "Ubuntu bootstrap should keep an ubuntu-bootstrap warning when automatic login shell change fails"
fi
pass "Ubuntu bootstrap keeps an ubuntu-bootstrap warning when automatic login shell change fails"

if [ -e "$shell_integrations_marker" ]; then
  fail "Ubuntu bootstrap should not write shell-integrations warnings for login shell failures"
fi
pass "Ubuntu bootstrap does not write shell-integrations warnings for login shell failures"

ubuntu_bootstrap_marker_text="$(cat "$ubuntu_bootstrap_marker")"
assert_contains "$ubuntu_bootstrap_marker_text" "guidance='Run chsh -s /usr/bin/zsh after fixing shell permission issues.'" "Ubuntu bootstrap marker preserves manual chsh guidance"

rm -rf "$HOME/.local/state/terrapod/install-warnings"
: >"$BOOTSTRAP_TEST_LOG"
BOOTSTRAP_APT_INSTALL_STATUS=42
export BOOTSTRAP_APT_INSTALL_STATUS
if ! sh "$rendered" >"$tmp_dir/bootstrap-apt-failure.out" 2>"$tmp_dir/bootstrap-apt-failure.err"; then
  unset BOOTSTRAP_APT_INSTALL_STATUS
  fail "Ubuntu bootstrap should continue routine apply when APT prerequisite install fails"
fi
pass "Ubuntu bootstrap continues routine apply when APT prerequisite install fails"

if [ ! -f "$ubuntu_bootstrap_marker" ]; then
  unset BOOTSTRAP_APT_INSTALL_STATUS
  fail "Ubuntu bootstrap should record an ubuntu-bootstrap warning when routine APT prerequisite install fails"
fi
pass "Ubuntu bootstrap records an ubuntu-bootstrap warning when routine APT prerequisite install fails"

rm -rf "$HOME/.local/state/terrapod/install-warnings"
: >"$BOOTSTRAP_TEST_LOG"
if ! TERRAPOD_FIRST_RUN_APPLY=1 sh "$rendered" >"$tmp_dir/bootstrap-first-run-apt-failure.out" 2>"$tmp_dir/bootstrap-first-run-apt-failure.err"; then
  unset BOOTSTRAP_APT_INSTALL_STATUS
  fail "Ubuntu bootstrap should continue first-run apply when APT prerequisite install fails"
fi
pass "Ubuntu bootstrap continues first-run apply when APT prerequisite install fails"

if [ ! -f "$ubuntu_bootstrap_marker" ]; then
  unset BOOTSTRAP_APT_INSTALL_STATUS
  fail "Ubuntu bootstrap should keep an ubuntu-bootstrap warning when first-run APT prerequisite install fails"
fi
pass "Ubuntu bootstrap keeps an ubuntu-bootstrap warning when first-run APT prerequisite install fails"

ubuntu_bootstrap_marker_text="$(cat "$ubuntu_bootstrap_marker")"
assert_contains "$ubuntu_bootstrap_marker_text" "guidance='Review APT install output for system and Homebrew prerequisites, then rerun tpod apply.'" "Ubuntu bootstrap marker preserves APT prerequisite failure guidance"

unwritable_state_parent="$tmp_dir/unwritable-state-parent"
: >"$unwritable_state_parent"
marker_write_failure_status=0
XDG_STATE_HOME="$unwritable_state_parent/state" sh "$rendered" \
  >"$tmp_dir/bootstrap-marker-write-failure.out" 2>"$tmp_dir/bootstrap-marker-write-failure.err" ||
  marker_write_failure_status=$?
unset BOOTSTRAP_APT_INSTALL_STATUS
if [ "$marker_write_failure_status" -eq 0 ]; then
  fail "Ubuntu bootstrap should fail when the install warning marker cannot be written"
fi
pass "Ubuntu bootstrap fails when the install warning marker cannot be written"

unsupported_rendered="$tmp_dir/bootstrap-ubuntu-unsupported.sh"
"$chezmoi_bin" execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"22.04"}},"enableAiCliTools":false,"enableDevelopmentWorkspace":false}' \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl" \
  >"$unsupported_rendered"

rm -rf "$HOME/.local/state/terrapod/install-warnings"
: >"$BOOTSTRAP_TEST_LOG"
if ! sh "$unsupported_rendered" >"$tmp_dir/bootstrap-unsupported.out" 2>"$tmp_dir/bootstrap-unsupported.err"; then
  fail "Ubuntu bootstrap should continue routine apply on an unsupported release"
fi
pass "Ubuntu bootstrap continues routine apply on an unsupported release"

rm -rf "$HOME/.local/state/terrapod/install-warnings"
: >"$BOOTSTRAP_TEST_LOG"
if ! TERRAPOD_FIRST_RUN_APPLY=1 sh "$unsupported_rendered" >"$tmp_dir/bootstrap-first-run-unsupported.out" 2>"$tmp_dir/bootstrap-first-run-unsupported.err"; then
  fail "Ubuntu bootstrap should continue first-run apply on an unsupported release"
fi
pass "Ubuntu bootstrap continues first-run apply on an unsupported release"

if [ ! -f "$ubuntu_bootstrap_marker" ]; then
  fail "Ubuntu bootstrap should keep an ubuntu-bootstrap warning for an unsupported release during first-run apply"
fi
pass "Ubuntu bootstrap keeps an ubuntu-bootstrap warning for an unsupported release during first-run apply"

ubuntu_bootstrap_marker_text="$(cat "$ubuntu_bootstrap_marker")"
assert_contains "$ubuntu_bootstrap_marker_text" "guidance='Run Terrapod on Ubuntu 24.04, or use tpod doctor for current platform guidance.'" "Ubuntu bootstrap marker preserves unsupported release guidance"

retry_rendered="$tmp_dir/retry-ubuntu-bootstrap.sh"
"$chezmoi_bin" execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableAiCliTools":false,"enableDevelopmentWorkspace":false}' \
  --file "$repo_root/.chezmoiscripts/run_before_01-retry-ubuntu-bootstrap.sh.tmpl" \
  >"$retry_rendered"
sh -n "$retry_rendered" || fail "rendered Ubuntu bootstrap retry script should be valid sh"
pass "rendered Ubuntu bootstrap retry script is valid sh"

macos_retry_rendered="$tmp_dir/retry-ubuntu-bootstrap-macos.sh"
"$chezmoi_bin" execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"darwin"},"enableAiCliTools":false,"enableDevelopmentWorkspace":false}' \
  --file "$repo_root/.chezmoiscripts/run_before_01-retry-ubuntu-bootstrap.sh.tmpl" \
  >"$macos_retry_rendered"
if [ -n "$(tr -d '[:space:]' <"$macos_retry_rendered")" ]; then
  fail "Ubuntu bootstrap retry script should render empty on macOS"
fi
pass "Ubuntu bootstrap retry script renders empty on macOS"

rm -rf "$HOME/.local/state/terrapod/install-warnings"
: >"$BOOTSTRAP_TEST_LOG"
if ! sh "$retry_rendered" >"$tmp_dir/retry-no-marker.out" 2>"$tmp_dir/retry-no-marker.err"; then
  fail "Ubuntu bootstrap retry should succeed when no ubuntu-bootstrap warning exists"
fi
pass "Ubuntu bootstrap retry succeeds when no ubuntu-bootstrap warning exists"

if [ -s "$BOOTSTRAP_TEST_LOG" ]; then
  fail "Ubuntu bootstrap retry should not run APT when no ubuntu-bootstrap warning exists"
fi
pass "Ubuntu bootstrap retry skips APT when no ubuntu-bootstrap warning exists"

rm -rf "$HOME/.local/state/terrapod/install-warnings"
: >"$BOOTSTRAP_TEST_LOG"
BOOTSTRAP_APT_INSTALL_STATUS=42
export BOOTSTRAP_APT_INSTALL_STATUS
sh "$rendered" >"$tmp_dir/retry-seed.out" 2>"$tmp_dir/retry-seed.err" ||
  fail "Ubuntu bootstrap should record a warning to seed the retry test"
unset BOOTSTRAP_APT_INSTALL_STATUS

: >"$BOOTSTRAP_TEST_LOG"
BOOTSTRAP_APT_INSTALL_STATUS=42
export BOOTSTRAP_APT_INSTALL_STATUS
if ! sh "$retry_rendered" >"$tmp_dir/retry-failure.out" 2>"$tmp_dir/retry-failure.err"; then
  unset BOOTSTRAP_APT_INSTALL_STATUS
  fail "Ubuntu bootstrap retry should continue apply when the retried bootstrap fails again"
fi
unset BOOTSTRAP_APT_INSTALL_STATUS
pass "Ubuntu bootstrap retry continues apply when the retried bootstrap fails again"

assert_contains "$(cat "$BOOTSTRAP_TEST_LOG")" "apt-get args:install -y build-essential" "Ubuntu bootstrap retry reruns the APT prerequisite install"

if [ ! -f "$ubuntu_bootstrap_marker" ]; then
  fail "Ubuntu bootstrap retry should keep the ubuntu-bootstrap warning after a failed retry"
fi
pass "Ubuntu bootstrap retry keeps the ubuntu-bootstrap warning after a failed retry"

: >"$BOOTSTRAP_TEST_LOG"
if ! sh "$retry_rendered" >"$tmp_dir/retry-success.out" 2>"$tmp_dir/retry-success.err"; then
  fail "Ubuntu bootstrap retry should succeed when the retried bootstrap succeeds"
fi
pass "Ubuntu bootstrap retry succeeds when the retried bootstrap succeeds"

if [ -e "$ubuntu_bootstrap_marker" ]; then
  fail "Ubuntu bootstrap retry should clear the ubuntu-bootstrap warning after a successful retry"
fi
pass "Ubuntu bootstrap retry clears the ubuntu-bootstrap warning after a successful retry"

missing_lib_source="$tmp_dir/onchange-missing-lib"
mkdir -p "$missing_lib_source"
cp -R "$repo_root/dot_local" "$missing_lib_source/dot_local"
rm -f \
  "$missing_lib_source/dot_local/lib/terrapod/install-warnings.sh" \
  "$missing_lib_source/dot_local/lib/terrapod/install-warning-script.sh"

rendered_missing_lib="$tmp_dir/onchange-missing-lib.sh"
"$chezmoi_bin" execute-template \
  --source "$repo_root" \
  --override-data "{\"chezmoi\":{\"os\":\"linux\",\"sourceDir\":\"$missing_lib_source\",\"osRelease\":{\"id\":\"ubuntu\",\"versionID\":\"24.04\"}}}" \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl" \
  >"$rendered_missing_lib"

if sh "$rendered_missing_lib" >/dev/null 2>&1; then
  fail "Ubuntu bootstrap stops when the install warning library is missing"
fi
pass "Ubuntu bootstrap stops when the install warning library is missing"
