#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"
macos_jetendard_installer="$(render_template "$macos_data" ".chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl")"
macos_jetendard_retry="$(render_template "$macos_data" ".chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl")"
macos_jetendard_settings="$(render_template "$macos_data" ".chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl")"
macos_jetendard_installer_script="$tmp_dir/macos-jetendard-installer.sh"
printf '%s\n' "$macos_jetendard_installer" >"$macos_jetendard_installer_script"
sh -n "$macos_jetendard_installer_script" || fail "macOS Jetendard installer script should be valid sh"
pass "macOS Jetendard installer script is valid sh"

macos_jetendard_retry_script="$tmp_dir/macos-jetendard-retry.sh"
printf '%s\n' "$macos_jetendard_retry" >"$macos_jetendard_retry_script"
sh -n "$macos_jetendard_retry_script" || fail "macOS Jetendard retry script should be valid sh"
pass "macOS Jetendard retry script is valid sh"

assert_contains \
  "$macos_jetendard_installer" \
  "Jetendard font helper checksum:" \
  "Jetendard installer tracks the helper checksum"

assert_contains \
  "$macos_jetendard_installer" \
  "Jetendard font install body checksum:" \
  "Jetendard installer tracks the install body checksum"

assert_contains \
  "$macos_jetendard_retry" \
  'if ! terrapod_install_warning_existing_path jetendard-font >/dev/null 2>&1; then' \
  "Jetendard retry is gated by its warning marker"

# The install body lives once, in jetendard-font-install.sh. The retry inlines
# that library, so only the template source shows what the script itself says.
for jetendard_pair_template in \
  .chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl \
  .chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl; do
  assert_file_contains \
    "$repo_root/$jetendard_pair_template" \
    'terrapod_jetendard_font_install "$font_helper"' \
    "Jetendard script calls the shared install body: $jetendard_pair_template"

  for jetendard_body_detail in \
    'command -v python3' \
    'python3 "$font_helper" install' \
    'terrapod_jetendard_font_guidance' \
    'declare_install_warning_category'; do
    assert_file_not_contains \
      "$repo_root/$jetendard_pair_template" \
      "$jetendard_body_detail" \
      "Jetendard script leaves $jetendard_body_detail to the shared body: $jetendard_pair_template"
  done
done

jetendard_missing_lib_source="$tmp_dir/jetendard-onchange-missing-lib"
mkdir -p "$jetendard_missing_lib_source"
cp -R "$repo_root/dot_local" "$jetendard_missing_lib_source/dot_local"
rm -f "$jetendard_missing_lib_source/dot_local/lib/terrapod/install-warnings.sh"

jetendard_missing_lib_data="{\"chezmoi\":{\"os\":\"darwin\",\"sourceDir\":\"$jetendard_missing_lib_source\"},\"enableEditorStack\":false,\"enableAiCliTools\":false,\"enableDevelopmentWorkspace\":false}"
macos_jetendard_installer_missing_lib="$(render_template "$jetendard_missing_lib_data" ".chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl")"

macos_jetendard_installer_missing_lib_script="$tmp_dir/macos-jetendard-installer-missing-lib.sh"
printf '%s\n' "$macos_jetendard_installer_missing_lib" >"$macos_jetendard_installer_missing_lib_script"

if sh "$macos_jetendard_installer_missing_lib_script" >/dev/null 2>&1; then
  fail "Jetendard font install should stop when the install warning library is missing"
fi
pass "Jetendard font install stops when the install warning library is missing"

jetendard_adapter_fixture="$tmp_dir/jetendard-adapter-fixture"
mkdir -p "$jetendard_adapter_fixture"
jetendard_adapter_log="$jetendard_adapter_fixture/actions.log"
jetendard_warnings_stub="$jetendard_adapter_fixture/install-warnings.sh"
jetendard_helper_stub="$jetendard_adapter_fixture/helper.py"
cat >"$jetendard_warnings_stub" <<'SH'
terrapod_install_warning_existing_path() {
  printf '%s\n' marker-check >>"$JETENDARD_ADAPTER_LOG"
  [ "${JETENDARD_MARKER_EXISTS:-0}" = 1 ]
}
terrapod_install_warning_clear() {
  printf '%s\n' clear >>"$JETENDARD_ADAPTER_LOG"
  [ "${JETENDARD_CLEAR_FAIL:-0}" != 1 ]
}
terrapod_install_warning_write() {
  printf '%s\n' write >>"$JETENDARD_ADAPTER_LOG"
  printf '%s\n' "$3" >"$JETENDARD_ADAPTER_LOG.guidance"
  [ "${JETENDARD_WRITE_FAIL:-0}" != 1 ]
}
SH
cat >"$jetendard_helper_stub" <<'PY'
import os
from pathlib import Path
with Path(os.environ["JETENDARD_ADAPTER_LOG"]).open("a") as stream:
    stream.write("helper\n")
raise SystemExit(int(os.environ.get("JETENDARD_HELPER_EXIT", "0")))
PY

# The adapters inline install-warnings.sh, so the stub is appended after the
# helper assignment to override the real definitions rather than replacing a path.
render_jetendard_adapter_fixture() {
  rendered="$1"
  destination="$2"
  printf '%s\n' "$rendered" |
    sed \
      -e "s#^warnings_lib=.*#warnings_lib=\"$jetendard_warnings_stub\"#" \
      -e "s#^font_helper=.*#font_helper=\"$jetendard_helper_stub\"#" \
      -e "s#^settings_helper=.*#settings_helper=\"$jetendard_helper_stub\"#" \
      -e "/^font_helper=/r $jetendard_warnings_stub" \
      -e "/^settings_helper=/r $jetendard_warnings_stub" \
      >"$destination"
}

jetendard_installer_fixture="$jetendard_adapter_fixture/installer.sh"
jetendard_retry_fixture="$jetendard_adapter_fixture/retry.sh"
jetendard_settings_fixture="$jetendard_adapter_fixture/settings.sh"
render_jetendard_adapter_fixture "$macos_jetendard_installer" "$jetendard_installer_fixture"
render_jetendard_adapter_fixture "$macos_jetendard_retry" "$jetendard_retry_fixture"
render_jetendard_adapter_fixture "$macos_jetendard_settings" "$jetendard_settings_fixture"

for adapter in "$jetendard_installer_fixture" "$jetendard_retry_fixture" "$jetendard_settings_fixture"; do
  : >"$jetendard_adapter_log"
  JETENDARD_ADAPTER_LOG="$jetendard_adapter_log" JETENDARD_MARKER_EXISTS=1 JETENDARD_CLEAR_FAIL=1 sh "$adapter" >/dev/null 2>&1 || \
    fail "successful Jetendard adapter continues when warning clear fails: $adapter"
done
pass "Jetendard adapters leave stale warning-clear failures for a later rerun"

: >"$jetendard_adapter_log"
JETENDARD_ADAPTER_LOG="$jetendard_adapter_log" JETENDARD_MARKER_EXISTS=0 JETENDARD_CLEAR_FAIL=0 sh "$jetendard_retry_fixture"
assert_equals "$(cat "$jetendard_adapter_log")" 'marker-check' \
  "Jetendard retry does not install without a warning marker"

: >"$jetendard_adapter_log"
JETENDARD_ADAPTER_LOG="$jetendard_adapter_log" JETENDARD_MARKER_EXISTS=1 JETENDARD_CLEAR_FAIL=0 sh "$jetendard_retry_fixture" >/dev/null
assert_equals "$(cat "$jetendard_adapter_log")" 'marker-check
helper
clear' \
  "Jetendard retry checks the marker before install and clear"

for adapter in "$jetendard_installer_fixture" "$jetendard_retry_fixture"; do
  : >"$jetendard_adapter_log"
  JETENDARD_ADAPTER_LOG="$jetendard_adapter_log" JETENDARD_MARKER_EXISTS=1 JETENDARD_CLEAR_FAIL=0 \
    sh "$adapter" >"$jetendard_adapter_fixture/success.out" 2>&1 ||
    fail "successful Jetendard install exits 0: $adapter"
  assert_contains "$(cat "$jetendard_adapter_log")" 'helper
clear' "successful Jetendard install clears its marker: $adapter"
  assert_file_contains "$jetendard_adapter_fixture/success.out" "Jetendard is ready." \
    "successful Jetendard install says the font is ready: $adapter"

  : >"$jetendard_adapter_log"
  jetendard_write_fail_status=0
  JETENDARD_ADAPTER_LOG="$jetendard_adapter_log" JETENDARD_MARKER_EXISTS=1 JETENDARD_CLEAR_FAIL=0 \
    JETENDARD_HELPER_EXIT=1 JETENDARD_WRITE_FAIL=1 \
    sh "$adapter" >/dev/null 2>&1 || jetendard_write_fail_status=$?
  assert_status "$jetendard_write_fail_status" 1 \
    "failed Jetendard install exits 1 when its marker cannot be written: $adapter"
done

# The helper exit status, not its message, is what the wrappers branch on:
# 2 rate limit, 3 unreachable GitHub, 4 unusable release, anything else generic.
assert_jetendard_guidance() {
  adapter="$1"
  helper_exit="$2"
  expected="$3"
  label="$4"

  : >"$jetendard_adapter_log"
  rm -f "$jetendard_adapter_log.guidance"
  JETENDARD_ADAPTER_LOG="$jetendard_adapter_log" \
    JETENDARD_MARKER_EXISTS=1 \
    JETENDARD_CLEAR_FAIL=0 \
    JETENDARD_HELPER_EXIT="$helper_exit" \
    sh "$adapter" >/dev/null 2>&1 ||
    fail "$label (adapter exited non-zero)"

  assert_equals "$(cat "$jetendard_adapter_log.guidance")" "$expected" "$label"
}

for adapter in "$jetendard_installer_fixture" "$jetendard_retry_fixture"; do
  assert_jetendard_guidance "$adapter" 2 \
    "GitHub API rate limit reached while resolving the Jetendard release. Export a temporary GITHUB_TOKEN or run gh auth login, then rerun tpod apply." \
    "Jetendard adapter offers GITHUB_TOKEN guidance on a rate limit: $adapter"

  assert_jetendard_guidance "$adapter" 3 \
    "GitHub was unreachable while resolving the Jetendard release. Restore network access, then rerun tpod apply." \
    "Jetendard adapter offers network guidance on an unreachable GitHub: $adapter"

  assert_jetendard_guidance "$adapter" 4 \
    "The latest Jetendard GitHub release is not installable. Wait for a corrected upstream release, then rerun tpod apply." \
    "Jetendard adapter offers release guidance on an unusable release: $adapter"

  assert_jetendard_guidance "$adapter" 1 \
    "Restore Python and GitHub access, then rerun tpod apply." \
    "Jetendard adapter keeps the generic guidance for an uncategorized failure: $adapter"
done

jetendard_no_python_bin="$tmp_dir/jetendard-no-python-bin"
mkdir -p "$jetendard_no_python_bin"

# Invoke the fixture by its own shebang rather than "sh $file": with PATH
# restricted to an empty directory, a bare "sh" word would itself fail to
# resolve (the shell searches the temporary PATH for "sh" too), which would
# fail before the script ever ran. Direct execution resolves the interpreter
# via the kernel's shebang handling, so only the script's internal PATH
# lookups (e.g. "command -v python3") are affected.
chmod +x "$jetendard_settings_fixture"
: >"$jetendard_adapter_log"
if ! JETENDARD_ADAPTER_LOG="$jetendard_adapter_log" \
  JETENDARD_MARKER_EXISTS=0 \
  JETENDARD_CLEAR_FAIL=0 \
  PATH="$jetendard_no_python_bin" \
  "$jetendard_settings_fixture" >/dev/null 2>&1; then
  fail "Jetendard settings adapter records a warning and succeeds without python3"
fi
pass "Jetendard settings adapter records a warning and succeeds without python3"

assert_equals \
  "$(cat "$jetendard_adapter_log")" \
  'write' \
  "Jetendard settings adapter writes exactly one warning when python3 is missing"
