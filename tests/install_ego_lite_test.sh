#!/bin/sh
# Behavior of the rendered ego lite installer. Each case runs the real rendered
# chezmoi script under an isolated home, state directory and Applications
# directory, with the network and macOS installation commands stubbed. Nothing
# here downloads, mounts, installs or opens a real browser.
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"

installer_template=".chezmoiscripts/run_after_80-install-ego-lite.sh.tmpl"
enabled_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupBrowsers":true}'
enabled_script="$tmp_dir/enabled-installer.sh"
render_template "$enabled_data" "$installer_template" >"$enabled_script"
sh -n "$enabled_script" || fail "rendered ego lite installer is valid sh"
pass "rendered ego lite installer is valid sh"

disabled_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupBrowsers":false}'
disabled_script="$tmp_dir/disabled-installer.sh"
render_template "$disabled_data" "$installer_template" >"$disabled_script"
sh -n "$disabled_script" || fail "rendered disabled ego lite installer is valid sh"
pass "rendered disabled ego lite installer is valid sh"

arm64_url="https://cdn.ego.app/setup/macos/arm64/egolite-fHoqgZ74bOEM.dmg"
x64_url="https://cdn.ego.app/setup/macos/x64/egolite-fHoqgZ74bOEM.dmg"
ego_lite_bundle_id="com.citrolabs.ego.lite"

# The fake volume's Info.plist holds the bare bundle ID, and the plutil stub
# prints that file, so a case changes the identity through EGO_DMG_BUNDLE_ID.
stub_bin="$tmp_dir/stub-bin"
write_stub "$stub_bin/curl" \
  'printf "%s\n" "curl $*" >>"$EGO_STUB_LOG"' \
  '[ "${EGO_CURL_FAIL:-0}" = 0 ] || exit 22' \
  'while [ "$#" -gt 0 ]; do' \
  '  if [ "$1" = "-o" ]; then output="$2"; shift 2; else shift; fi' \
  'done' \
  'printf "%s\n" dmg >"$output"'
write_stub "$stub_bin/hdiutil" \
  'printf "%s\n" "hdiutil $*" >>"$EGO_STUB_LOG"' \
  'case "$1" in' \
  '  attach)' \
  '    [ "${EGO_ATTACH_FAIL:-0}" = 0 ] || exit 1' \
  '    while [ "$#" -gt 0 ]; do' \
  '      if [ "$1" = "-mountpoint" ]; then mount_dir="$2"; shift 2; else shift; fi' \
  '    done' \
  '    mkdir -p "$mount_dir/ego lite.app/Contents"' \
  '    printf "%s\n" "${EGO_DMG_BUNDLE_ID:-com.citrolabs.ego.lite}" >"$mount_dir/ego lite.app/Contents/Info.plist"' \
  '    ;;' \
  '  detach)' \
  '    [ "${EGO_DETACH_FAIL:-0}" = 0 ] || exit 1' \
  '    rm -rf "$2/ego lite.app"' \
  '    ;;' \
  '  *) exit 64 ;;' \
  'esac'
write_stub "$stub_bin/plutil" \
  'printf "%s\n" "plutil $*" >>"$EGO_STUB_LOG"' \
  'for last_arg do :; done' \
  'cat "$last_arg"'
write_stub "$stub_bin/ditto" \
  'printf "%s\n" "ditto $*" >>"$EGO_STUB_LOG"' \
  'if [ "${EGO_DITTO_FAIL:-0}" != 0 ]; then' \
  '  mkdir -p "$2"' \
  '  printf "%s\n" partial >"$2/partial-copy"' \
  '  exit 1' \
  'fi' \
  'exec cp -R "$1" "$2"'
write_stub "$stub_bin/mv" \
  'for arg do case "$arg" in */Applications/*) [ "${EGO_PUBLISH_FAIL:-0}" = 0 ] || exit 1 ;; esac; done' \
  'exec /bin/mv "$@"'
write_stub "$stub_bin/open" 'printf "%s\n" "APP-OPENED $*" >>"$EGO_STUB_LOG"'
write_stub "$stub_bin/sudo" 'printf "%s\n" "SUDO-USED $*" >>"$EGO_STUB_LOG"'

new_case() {
  case_root="$tmp_dir/$1"
  case_home="$case_root/home"
  case_state="$case_root/state"
  case_apps="$case_root/Applications"
  case_tmp="$case_root/tmp"
  case_log="$case_root/calls.log"
  case_marker="$case_state/terrapod/install-warnings/ego-lite"
  mkdir -p "$case_home" "$case_state" "$case_apps/Unrelated.app/Contents" "$case_tmp"
  printf '%s\n' unrelated >"$case_apps/Unrelated.app/Contents/Info.plist"
  : >"$case_log"
}

# Runs a rendered script for the current case. Extra arguments are environment
# assignments, such as a stub failure switch.
run_script() {
  script="$1"
  shift
  run_status=0
  env HOME="$case_home" XDG_STATE_HOME="$case_state" TMPDIR="$case_tmp" \
    TERRAPOD_MACOS_APPLICATIONS_DIR="$case_apps" \
    TERRAPOD_MACHINE_ARCH=arm64 TERRAPOD_DARWIN_TRANSLATED=0 \
    EGO_STUB_LOG="$case_log" PATH="$stub_bin:/usr/bin:/bin" \
    "$@" sh "$script" >"$case_root/output" 2>&1 || run_status=$?
  case_calls="$(cat "$case_log")"
}

# Writes an app bundle with the given bundle ID and prints nothing.
write_app() {
  mkdir -p "$1/Contents"
  printf '%s\n' "$2" >"$1/Contents/Info.plist"
}

write_marker() {
  env HOME="$case_home" XDG_STATE_HOME="$case_state" sh -c \
    '. "$1"; terrapod_install_warning_write "$2" "Install needs attention" "Rerun tpod apply."' \
    sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh" "$1"
}

assert_path_absent() {
  [ ! -e "$1" ] || fail "$2"
  pass "$2"
}

# --- Fresh installation ---------------------------------------------------------

new_case fresh
run_script "$enabled_script"
assert_status "$run_status" 0 "a fresh ego lite install exits 0"
assert_contains "$case_calls" "curl" "a fresh install downloads the DMG"
assert_contains "$case_calls" "$arm64_url" "an Apple Silicon Mac downloads the ARM64 DMG"
assert_file_contains "$case_apps/ego lite.app/Contents/Info.plist" "$ego_lite_bundle_id" \
  "a fresh install places the app in the system Applications directory"
assert_not_contains "$case_calls" "APP-OPENED" "a fresh install never opens the app"
assert_not_contains "$case_calls" "SUDO-USED" "a fresh install never asks for sudo"
assert_contains "$case_calls" "hdiutil detach" "a fresh install detaches the mounted DMG"
assert_path_absent "$case_marker" "a fresh install records no install warning"
assert_equals "$(ls -A "$case_tmp")" "" "a fresh install leaves no temporary files"
assert_equals "$(LC_ALL=C ls -A "$case_apps")" "Unrelated.app
ego lite.app" "a fresh install adds only the app to the Applications directory"

# --- Architecture selection -----------------------------------------------------

new_case intel
run_script "$enabled_script" TERRAPOD_MACHINE_ARCH=x86_64
assert_contains "$case_calls" "$x64_url" "an Intel Mac downloads the x64 DMG"
assert_not_contains "$case_calls" "$arm64_url" "an Intel Mac does not download the ARM64 DMG"

new_case rosetta
run_script "$enabled_script" TERRAPOD_MACHINE_ARCH=x86_64 TERRAPOD_DARWIN_TRANSLATED=1
assert_contains "$case_calls" "$arm64_url" "an Apple Silicon Mac running under Rosetta still downloads the ARM64 DMG"

new_case unknown_arch
run_script "$enabled_script" TERRAPOD_MACHINE_ARCH=riscv64
assert_status "$run_status" 0 "an unknown architecture does not fail the apply"
assert_not_contains "$case_calls" "curl" "an unknown architecture downloads nothing"
assert_file_contains "$case_marker" "ego lite (unsupported architecture)" "an unknown architecture records why in its install warning"

# --- Existing installations -----------------------------------------------------

new_case existing_system
write_app "$case_apps/ego lite.app" "$ego_lite_bundle_id"
printf '%s\n' keep >"$case_apps/ego lite.app/Contents/version-marker"
run_script "$enabled_script"
assert_status "$run_status" 0 "an existing system install exits 0"
assert_not_contains "$case_calls" "curl" "an existing system install downloads nothing"
assert_not_contains "$case_calls" "hdiutil" "an existing system install mounts nothing"
assert_not_contains "$case_calls" "ditto" "an existing system install copies nothing"
assert_not_contains "$case_calls" "APP-OPENED" "an existing system install is not opened"
assert_file_exists "$case_apps/ego lite.app/Contents/version-marker" "an existing system install stays unchanged"

new_case existing_user
write_app "$case_home/Applications/ego lite.app" "$ego_lite_bundle_id"
run_script "$enabled_script"
assert_status "$run_status" 0 "an existing user install exits 0"
assert_not_contains "$case_calls" "curl" "an existing user install downloads nothing"
assert_path_absent "$case_apps/ego lite.app" "an existing user install is not duplicated into the system Applications directory"

new_case repeat
run_script "$enabled_script"
: >"$case_log"
run_script "$enabled_script"
assert_status "$run_status" 0 "a repeated apply exits 0"
assert_not_contains "$case_calls" "curl" "a repeated apply downloads nothing"
assert_not_contains "$case_calls" "ditto" "a repeated apply does not replace the app"

# --- Group selection and profile ------------------------------------------------

new_case disabled
write_marker ego-lite
run_script "$disabled_script"
assert_status "$run_status" 0 "a disabled browsers group exits 0"
assert_equals "$case_calls" "" "a disabled browsers group runs no install command"
assert_path_absent "$case_apps/ego lite.app" "a disabled browsers group installs no app"
assert_path_absent "$case_marker" "disabling the browsers group clears an obsolete ego lite warning"

new_case disabled_existing
write_app "$case_apps/ego lite.app" "$ego_lite_bundle_id"
run_script "$disabled_script"
assert_file_exists "$case_apps/ego lite.app/Contents/Info.plist" "disabling the browsers group leaves an installed app in place"

vps_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableMacosAppGroupBrowsers":true}'
assert_equals "$(render_template "$vps_data" "$installer_template")" "" \
  "the VPS Shell Profile renders no ego lite installer even with the setting enabled"

# --- Failures -------------------------------------------------------------------

# A recorded failure keeps the apply going, names the app, leaves the rest of
# the machine alone and cleans up after itself.
assert_recorded_failure() {
  label="$1"
  assert_status "$run_status" 0 "$label does not fail the apply"
  assert_file_contains "$case_marker" "ego lite" "$label records a warning naming ego lite"
  assert_file_contains "$case_marker" "rerun tpod apply" "$label tells the user how to retry"
  assert_file_exists "$case_apps/Unrelated.app/Contents/Info.plist" "$label leaves unrelated apps in place"
  assert_path_absent "$case_apps/ego lite.app" "$label installs no app"
  assert_equals "$(LC_ALL=C ls -A "$case_apps")" "Unrelated.app" "$label leaves nothing behind in the Applications directory"
  assert_not_contains "$case_calls" "APP-OPENED" "$label opens nothing"
}

new_case download_failure
run_script "$enabled_script" EGO_CURL_FAIL=1
assert_recorded_failure "a download failure"
assert_file_contains "$case_marker" "ego lite (download)" "a download failure names the failed step"
assert_equals "$(ls -A "$case_tmp")" "" "a download failure leaves no temporary files"

new_case mount_failure
run_script "$enabled_script" EGO_ATTACH_FAIL=1
assert_recorded_failure "a mount failure"
assert_file_contains "$case_marker" "ego lite (DMG mount)" "a mount failure names the failed step"
assert_not_contains "$case_calls" "hdiutil detach" "a mount failure detaches nothing"
assert_equals "$(ls -A "$case_tmp")" "" "a mount failure leaves no temporary files"

new_case wrong_identity
run_script "$enabled_script" EGO_DMG_BUNDLE_ID=com.example.impostor
assert_recorded_failure "a DMG holding another application"
assert_file_contains "$case_marker" "ego lite (DMG identity check)" "a DMG holding another application names the failed step"
assert_contains "$case_calls" "hdiutil detach" "a DMG holding another application is still detached"
assert_not_contains "$case_calls" "ditto" "a DMG holding another application is never copied"

new_case copy_failure
run_script "$enabled_script" EGO_DITTO_FAIL=1
assert_recorded_failure "a copy failure"
assert_file_contains "$case_marker" "ego lite (copy)" "a copy failure names the failed step"
assert_contains "$case_calls" "hdiutil detach" "a copy failure still detaches the DMG"

new_case publish_failure
run_script "$enabled_script" EGO_PUBLISH_FAIL=1
assert_recorded_failure "a failed move into the Applications directory"

new_case unwritable_applications
run_script "$enabled_script" TERRAPOD_MACOS_APPLICATIONS_DIR="$case_root/missing-applications"
assert_status "$run_status" 0 "an unusable Applications directory does not fail the apply"
assert_file_exists "$case_marker" "an unusable Applications directory records an install warning"
assert_not_contains "$case_calls" "curl" "an unusable Applications directory downloads nothing"
assert_not_contains "$case_calls" "SUDO-USED" "an unusable Applications directory does not escalate to sudo"

# Something else already holds the destination. Terrapod never replaces it, and
# reports the problem before spending a download on it.
new_case occupied_destination
write_app "$case_apps/ego lite.app" com.example.other
run_script "$enabled_script"
assert_status "$run_status" 0 "an occupied destination does not fail the apply"
assert_file_contains "$case_marker" "move it aside" "an occupied destination tells the user how to free the path"
assert_not_contains "$case_calls" "curl" "an occupied destination downloads nothing"
assert_file_contains "$case_apps/ego lite.app/Contents/Info.plist" com.example.other "an occupied destination stays unchanged"

# The app is in place, so the failed detach is visible now and cleared by the
# next apply, which finds the app and has nothing left to do.
new_case detach_failure
run_script "$enabled_script" EGO_DETACH_FAIL=1
assert_status "$run_status" 0 "a detach failure does not fail the apply"
assert_file_exists "$case_apps/ego lite.app/Contents/Info.plist" "a detach failure keeps the installed app"
assert_file_contains "$case_marker" "ego lite DMG cleanup" "a detach failure blames the cleanup, not the installed app"
assert_contains "$case_calls" "-force" "a detach failure retries with force"
assert_equals "$(LC_ALL=C ls -A "$case_apps")" "Unrelated.app
ego lite.app" "a detach failure leaves no staging directory in the Applications directory"
run_script "$enabled_script"
assert_path_absent "$case_marker" "the apply after a detach failure clears its warning"

new_case warning_write_failure
: >"$case_root/not-a-directory"
case_state="$case_root/not-a-directory/state"
run_script "$enabled_script" EGO_CURL_FAIL=1
assert_status "$run_status" 1 "a failure that cannot be recorded fails the apply"

new_case retry
write_marker ego-lite
run_script "$enabled_script" EGO_CURL_FAIL=1
assert_file_exists "$case_marker" "a failed apply keeps the warning visible"
: >"$case_log"
run_script "$enabled_script"
assert_status "$run_status" 0 "a retry after a failure exits 0"
assert_contains "$case_calls" "curl" "a later apply retries the missing app"
assert_file_exists "$case_apps/ego lite.app/Contents/Info.plist" "a retry installs the app"
assert_path_absent "$case_marker" "a successful retry clears the warning"

new_case unrelated_warning
write_marker jetendard-font
run_script "$enabled_script"
assert_file_exists "$case_state/terrapod/install-warnings/jetendard-font" "a successful install leaves other warning categories alone"
