#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"
macos_jetendard_settings="$(render_template "$macos_data" ".chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl")"

jetendard_adapter_fixture="$tmp_dir/jetendard-adapter-fixture"
mkdir -p "$jetendard_adapter_fixture"
jetendard_adapter_log="$jetendard_adapter_fixture/actions.log"
jetendard_warnings_stub="$jetendard_adapter_fixture/install-warnings.sh"
jetendard_helper_stub="$jetendard_adapter_fixture/helper.py"
cat >"$jetendard_warnings_stub" <<'SH'
terrapod_install_warning_clear() {
  printf '%s\n' clear >>"$JETENDARD_ADAPTER_LOG"
  [ "${JETENDARD_CLEAR_FAIL:-0}" != 1 ]
}
terrapod_install_warning_write() {
  printf '%s\n' write >>"$JETENDARD_ADAPTER_LOG"
  printf '%s\n' "$2" >"$JETENDARD_ADAPTER_LOG.summary"
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

# The settings adapter inlines install-warnings.sh; override its definitions
# after the helper assignment for these existing settings-policy checks.
render_jetendard_adapter_fixture() {
  rendered="$1"
  destination="$2"
  printf '%s\n' "$rendered" |
    sed \
      -e "s#^settings_helper=.*#settings_helper=\"$jetendard_helper_stub\"#" \
      -e "/^settings_helper=/r $jetendard_warnings_stub" \
      >"$destination"
}

jetendard_settings_fixture="$jetendard_adapter_fixture/settings.sh"
render_jetendard_adapter_fixture "$macos_jetendard_settings" "$jetendard_settings_fixture"

# Font scheduling and warning outcomes are covered through real chezmoi apply
# in install_source_schedule_test.sh. Keep the settings-only behavior here.
: >"$jetendard_adapter_log"
JETENDARD_ADAPTER_LOG="$jetendard_adapter_log" JETENDARD_CLEAR_FAIL=1 \
  sh "$jetendard_settings_fixture" >/dev/null 2>&1 ||
  fail "successful Jetendard settings continue when warning clear fails"
pass "Jetendard settings leave warning clear failures for a later apply"

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
