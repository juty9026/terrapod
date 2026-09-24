#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"
macos_brewfile="$(render_template "$macos_data" "Brewfile.macos-desktop-apps.tmpl")"
linux_macos_desktop_apps_brewfile="$(render_template "$ubuntu_data" "Brewfile.macos-desktop-apps.tmpl")"
terminal_apps_brewfile="$(render_template "$macos_terminal_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
automation_apps_brewfile="$(render_template "$macos_automation_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
launcher_apps_brewfile="$(render_template "$macos_launcher_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
monitoring_apps_brewfile="$(render_template "$macos_monitoring_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
development_apps_brewfile="$(render_template "$macos_development_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
mobile_dev_brewfile="$(render_template "$macos_mobile_dev_data" "Brewfile.macos-desktop-apps.tmpl")"
macos_bootstrap="$(render_template "$macos_data" ".chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl")"
macos_terminal_apps_bootstrap="$(render_template "$macos_terminal_apps_data" ".chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl")"
macos_terminal_launcher_apps_bootstrap="$(render_template "$macos_terminal_launcher_apps_data" ".chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl")"
macos_development_apps_bootstrap="$(render_template "$macos_development_apps_data" ".chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl")"
macos_mobile_dev_bootstrap="$(render_template "$macos_mobile_dev_data" ".chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl")"
macos_development_workspace_bootstrap="$(render_template "$macos_development_workspace_data" ".chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl")"
# Each fake brew logs "brew auto-update:<value>" for every bundle call, so a
# run proves that every bundle it made had Homebrew auto-update disabled.
assert_bundle_calls_disable_auto_update() {
  bundle_log="$1"
  bundle_message="$2"
  bundle_calls="$(grep -c '^brew auto-update:' "$bundle_log" || true)"
  guarded_bundle_calls="$(grep -cx 'brew auto-update:1' "$bundle_log" || true)"
  if [ "$bundle_calls" -eq 0 ]; then
    fail "$bundle_message: no brew bundle call was recorded"
  fi
  if [ "$guarded_bundle_calls" -ne "$bundle_calls" ]; then
    grep '^brew auto-update:' "$bundle_log" | sed 's/^/  /' >&2
    fail "$bundle_message"
  fi
  pass "$bundle_message"
}

assert_not_contains \
  "$macos_bootstrap" \
  "Brewfile.macos-desktop-apps" \
  "macOS bootstrap default skips macOS Desktop App Stack Brewfile"

assert_not_contains \
  "$macos_bootstrap" \
  "terrapod-macos-desktop-apps" \
  "macOS bootstrap default skips macOS Desktop App Stack temp Brewfile"

assert_not_contains \
  "$macos_bootstrap" \
  'brew bundle --no-upgrade --file="$desktop_brewfile"' \
  "macOS bootstrap default skips macOS Desktop App Stack bundle"

assert_contains \
  "$macos_bootstrap" \
  "clear_install_warning homebrew-desktop-apps" \
  "macOS bootstrap default renders macOS Desktop App Stack warning cleanup"

macos_bootstrap_script="$tmp_dir/macos-bootstrap-default.sh"
render_template_with_homebrew_prefix_provider \
  "$macos_data" \
  '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
  "$tmp_dir/macos-brew-prefix" >"$macos_bootstrap_script"
sh -n "$macos_bootstrap_script" || fail "macOS bootstrap default cleanup script should be valid sh"
pass "macOS bootstrap default cleanup script is valid sh"

macos_brew_bin="$tmp_dir/macos-brew-prefix/bin"
macos_brew_log="$tmp_dir/macos-brew.log"
mkdir -p "$macos_brew_bin"
write_stub "$macos_brew_bin/brew" \
  'printf "%s\n" "brew args:$*" >>"$MACOS_BREW_LOG"' \
  'case "$1" in' \
  '  shellenv)' \
  '    case "${MACOS_BREW_SHELLENV_MODE:-success}" in' \
  '      command-failure) exit 41 ;;' \
  '      eval-failure) printf "%s\n" "false" ;;' \
  '      *) printf "%s\n" ":" ;;' \
  '    esac' \
  '    ;;' \
  '  analytics) exit 0 ;;' \
  '  bundle) exit 0 ;;' \
  '  *) exit 64 ;;' \
  'esac'

run_linux_homebrew_arch_case() {
  arch="$1"
  expected_status="$2"
  case_dir="$tmp_dir/linux-homebrew-arch-$arch"
  case_bin="$case_dir/bin"
  case_script="$case_dir/bootstrap.sh"
  case_log="$case_dir/commands.log"
  case_state="$case_dir/state"
  case_home="$case_dir/home"
  case_prefix="$case_dir/prefix"
  mkdir -p "$case_bin" "$case_prefix/bin" "$case_home"

  write_stub "$case_bin/uname" "printf '%s\\n' '$arch'"
  write_stub "$case_bin/curl" \
    'printf "%s\n" "curl args:$*" >>"$LINUX_HOMEBREW_ARCH_LOG"' \
    'exit 97'
  write_stub "$case_prefix/bin/brew" \
    'printf "%s\n" "brew args:$*" >>"$LINUX_HOMEBREW_ARCH_LOG"' \
    'case "$1" in' \
    '  shellenv) printf "export PATH=\"%s/bin:\$PATH\"\n" "$LINUX_HOMEBREW_ARCH_PREFIX" ;;' \
    '  analytics) exit 0 ;;' \
    '  bundle) printf "%s\n" "brew auto-update:${HOMEBREW_NO_AUTO_UPDATE:-}" >>"$LINUX_HOMEBREW_ARCH_LOG" ;;' \
    '  --prefix) printf "%s\n" "$LINUX_HOMEBREW_ARCH_PREFIX" ;;' \
    '  *) exit 64 ;;' \
    'esac'

  render_template_with_homebrew_prefix_provider \
    "$ubuntu_data" \
    '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
    "$case_prefix" >"$case_script"
  sh -n "$case_script" || fail "Ubuntu Homebrew $arch bootstrap test script is valid sh"

  case_status=0
  HOME="$case_home" \
    XDG_STATE_HOME="$case_state" \
    LINUX_HOMEBREW_ARCH_LOG="$case_log" \
    LINUX_HOMEBREW_ARCH_PREFIX="$case_prefix" \
    PATH="$case_bin:/usr/bin:/bin" \
    sh "$case_script" >"$case_dir/stdout" 2>"$case_dir/stderr" || case_status=$?

  if [ "$expected_status" = success ]; then
    if [ "$case_status" -ne 0 ]; then
      sed 's/^/stdout: /' "$case_dir/stdout" >&2
      sed 's/^/stderr: /' "$case_dir/stderr" >&2
      fail "Ubuntu Homebrew bootstrap accepts $arch"
    fi
    assert_bundle_calls_disable_auto_update "$case_log" "Ubuntu $arch core bundle disables Homebrew auto-update"
  elif [ "$case_status" -eq 0 ]; then
    fail "Ubuntu Homebrew bootstrap rejects $arch"
  fi

  if [ -f "$case_log" ] && grep -F 'raw.githubusercontent.com/Homebrew/install' "$case_log" >/dev/null; then
    fail "Ubuntu Homebrew $arch architecture check runs before the installer download"
  fi
  pass "Ubuntu Homebrew bootstrap handles $arch before installer download"
}

run_linux_homebrew_arch_case x86_64 success
run_linux_homebrew_arch_case aarch64 success
run_linux_homebrew_arch_case arm64 failure
run_linux_homebrew_arch_case i686 failure
run_linux_homebrew_arch_case unknown failure

run_linux_homebrew_space_case() {
  available_kb="$1"
  expected_warning="$2"
  case_name="$3"
  case_dir="$tmp_dir/linux-homebrew-space-$case_name"
  case_bin="$case_dir/bin"
  case_script="$case_dir/bootstrap.sh"
  case_log="$case_dir/commands.log"
  case_state="$case_dir/state"
  case_home="$case_dir/home"
  mkdir -p "$case_bin" "$case_home"

  write_stub "$case_bin/uname" 'printf "%s\n" x86_64'
  write_stub "$case_bin/df" \
    'printf "%s\n" "df args:$*" >>"$LINUX_HOMEBREW_SPACE_LOG"' \
    'printf "%s\n" "Filesystem 1024-blocks Used Available Capacity Mounted on"' \
    'printf "%s\n" "/dev/test 9999999 1 $LINUX_HOMEBREW_AVAILABLE_KB 1% /"'
  write_stub "$case_bin/curl" \
    'printf "%s\n" "curl args:$*" >>"$LINUX_HOMEBREW_SPACE_LOG"' \
    'exit 97'

  render_template_with_homebrew_prefix_provider \
    "$ubuntu_data" \
    '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
    "$case_dir/missing-prefix" >"$case_script"
  sh -n "$case_script" || fail "Ubuntu Homebrew $case_name space test script is valid sh"

  case_status=0
  HOME="$case_home" \
    XDG_STATE_HOME="$case_state" \
    LINUX_HOMEBREW_SPACE_LOG="$case_log" \
    LINUX_HOMEBREW_AVAILABLE_KB="$available_kb" \
    PATH="$case_bin:/usr/bin:/bin" \
    sh "$case_script" >"$case_dir/stdout" 2>"$case_dir/stderr" || case_status=$?

  if [ "$case_status" -ne 0 ]; then
    fail "Ubuntu Homebrew $case_name space case continues apply after the intentionally failing installer download"
  fi
  if [ ! -f "$case_state/terrapod/install-warnings/homebrew-core" ]; then
    fail "Ubuntu Homebrew $case_name space case records a homebrew-core warning for the failed installer download"
  fi
  if ! grep -F 'raw.githubusercontent.com/Homebrew/install' "$case_log" >/dev/null; then
    fail "Ubuntu Homebrew $case_name space check continues to the installer download"
  fi

  if [ "$expected_warning" = yes ]; then
    if ! grep -F 'Warning: less than 3 GiB is available for /home/linuxbrew; Homebrew installation will continue.' "$case_dir/stderr" >/dev/null; then
      fail "Ubuntu Homebrew warns when available space is below 3 GiB"
    fi
  elif grep -F 'Warning: less than 3 GiB is available for /home/linuxbrew' "$case_dir/stderr" >/dev/null; then
    fail "Ubuntu Homebrew does not warn when available space is at least 3 GiB"
  fi
  pass "Ubuntu Homebrew handles $case_name available space without blocking installation"
}

run_linux_homebrew_space_case 3145727 yes low
run_linux_homebrew_space_case 3145728 no sufficient

macos_marker_state="$tmp_dir/macos-marker-state"
macos_marker_home="$tmp_dir/macos-marker-home"
mkdir -p "$macos_marker_home"
HOME="$macos_marker_home" XDG_STATE_HOME="$macos_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-desktop-apps "Homebrew desktop app install needs attention" "Rerun tpod apply after disabling macOS App Groups."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if [ ! -f "$macos_marker_state/terrapod/install-warnings/homebrew-desktop-apps" ]; then
  fail "test setup should create a homebrew-desktop-apps warning marker"
fi

HOME="$macos_marker_home" XDG_STATE_HOME="$macos_marker_state" MACOS_BREW_LOG="$macos_brew_log" PATH="$macos_brew_bin:/usr/bin:/bin" sh "$macos_bootstrap_script"
if [ -e "$macos_marker_state/terrapod/install-warnings/homebrew-desktop-apps" ]; then
  fail "macOS bootstrap default cleanup should clear stale homebrew-desktop-apps marker"
fi
pass "macOS bootstrap default cleanup clears stale homebrew-desktop-apps marker"

homebrew_installer_failure_script="$tmp_dir/macos-bootstrap-homebrew-installer-failure.sh"
render_template_with_homebrew_prefix_provider \
  "$macos_data" \
  '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
  "$tmp_dir/missing-prefix" >"$homebrew_installer_failure_script"
sh -n "$homebrew_installer_failure_script" || fail "macOS bootstrap no-Homebrew test script should be valid sh"

homebrew_installer_failure_bin="$tmp_dir/homebrew-installer-failure-bin"
homebrew_installer_failure_state="$tmp_dir/homebrew-installer-failure-state"
homebrew_installer_failure_home="$tmp_dir/homebrew-installer-failure-home"
homebrew_installer_failure_log="$tmp_dir/homebrew-installer-failure.log"
mkdir -p "$homebrew_installer_failure_bin" "$homebrew_installer_failure_home"
write_stub "$homebrew_installer_failure_bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$HOMEBREW_INSTALLER_FAILURE_LOG"' \
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
  'if [ -n "$output_file" ]; then' \
  '  printf "%s\n" "echo simulated Homebrew installer failure >&2" "exit 42" >"$output_file"' \
  'else' \
  '  printf "%s\n" "echo simulated Homebrew installer failure >&2" "exit 42"' \
  'fi'

if ! HOME="$homebrew_installer_failure_home" XDG_STATE_HOME="$homebrew_installer_failure_state" HOMEBREW_INSTALLER_FAILURE_LOG="$homebrew_installer_failure_log" PATH="$homebrew_installer_failure_bin:/usr/bin:/bin" \
  sh "$homebrew_installer_failure_script" >"$tmp_dir/homebrew-installer-failure.out" 2>"$tmp_dir/homebrew-installer-failure.err"; then
  fail "macOS bootstrap should continue routine apply when the Homebrew installer command fails"
fi
pass "macOS bootstrap continues routine apply when the Homebrew installer command fails"

homebrew_installer_failure_marker="$homebrew_installer_failure_state/terrapod/install-warnings/homebrew-core"
if [ ! -f "$homebrew_installer_failure_marker" ]; then
  fail "macOS bootstrap records homebrew-core marker when the Homebrew installer command fails"
fi
pass "macOS bootstrap records homebrew-core marker when the Homebrew installer command fails"

homebrew_installer_failure_marker_text="$(cat "$homebrew_installer_failure_marker")"
assert_contains "$homebrew_installer_failure_marker_text" "summary='Homebrew core install needs attention'" "macOS bootstrap Homebrew installer failure marker keeps the expected summary"
assert_contains "$homebrew_installer_failure_marker_text" "guidance='Install Homebrew from https://brew.sh, then rerun tpod apply.'" "macOS bootstrap Homebrew installer failure marker keeps recovery guidance"

homebrew_download_failure_bin="$tmp_dir/homebrew-download-failure-bin"
homebrew_download_failure_state="$tmp_dir/homebrew-download-failure-state"
homebrew_download_failure_home="$tmp_dir/homebrew-download-failure-home"
homebrew_download_failure_log="$tmp_dir/homebrew-download-failure.log"
mkdir -p "$homebrew_download_failure_bin" "$homebrew_download_failure_home"
write_stub "$homebrew_download_failure_bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$HOMEBREW_INSTALLER_FAILURE_LOG"' \
  'exit 42'

if ! HOME="$homebrew_download_failure_home" XDG_STATE_HOME="$homebrew_download_failure_state" HOMEBREW_INSTALLER_FAILURE_LOG="$homebrew_download_failure_log" PATH="$homebrew_download_failure_bin:/usr/bin:/bin" \
  sh "$homebrew_installer_failure_script" >"$tmp_dir/homebrew-download-failure.out" 2>"$tmp_dir/homebrew-download-failure.err"; then
  fail "macOS bootstrap should continue when the Homebrew installer download fails"
fi
if [ ! -f "$homebrew_download_failure_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "macOS bootstrap records homebrew-core marker when the Homebrew installer download fails"
fi
pass "macOS bootstrap continues and records a marker when the Homebrew installer download fails"

homebrew_missing_after_install_bin="$tmp_dir/homebrew-missing-after-install-bin"
homebrew_missing_after_install_state="$tmp_dir/homebrew-missing-after-install-state"
homebrew_missing_after_install_home="$tmp_dir/homebrew-missing-after-install-home"
homebrew_missing_after_install_log="$tmp_dir/homebrew-missing-after-install.log"
mkdir -p "$homebrew_missing_after_install_bin" "$homebrew_missing_after_install_home"
write_stub "$homebrew_missing_after_install_bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$HOMEBREW_INSTALLER_FAILURE_LOG"' \
  'output_file=' \
  'while [ "$#" -gt 0 ]; do' \
  '  if [ "$1" = -o ]; then shift; output_file="$1"; fi' \
  '  shift' \
  'done' \
  'printf "%s\n" "exit 0" >"$output_file"'

if ! HOME="$homebrew_missing_after_install_home" XDG_STATE_HOME="$homebrew_missing_after_install_state" HOMEBREW_INSTALLER_FAILURE_LOG="$homebrew_missing_after_install_log" PATH="$homebrew_missing_after_install_bin:/usr/bin:/bin" \
  sh "$homebrew_installer_failure_script" >"$tmp_dir/homebrew-missing-after-install.out" 2>"$tmp_dir/homebrew-missing-after-install.err"; then
  fail "macOS bootstrap should continue when brew is not found after installation"
fi
if [ ! -f "$homebrew_missing_after_install_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "macOS bootstrap records homebrew-core marker when brew is not found after installation"
fi
pass "macOS bootstrap continues and records a marker when brew is not found after installation"

homebrew_marker_write_failure_home="$tmp_dir/homebrew-marker-write-failure-home"
homebrew_marker_write_failure_parent="$tmp_dir/homebrew-marker-write-failure-parent"
homebrew_marker_write_failure_log="$tmp_dir/homebrew-marker-write-failure.log"
mkdir -p "$homebrew_marker_write_failure_home"
: >"$homebrew_marker_write_failure_parent"
if HOME="$homebrew_marker_write_failure_home" XDG_STATE_HOME="$homebrew_marker_write_failure_parent/state" HOMEBREW_INSTALLER_FAILURE_LOG="$homebrew_marker_write_failure_log" PATH="$homebrew_installer_failure_bin:/usr/bin:/bin" \
  sh "$homebrew_installer_failure_script" >"$tmp_dir/homebrew-marker-write-failure.out" 2>"$tmp_dir/homebrew-marker-write-failure.err"; then
  fail "macOS bootstrap should fail when the homebrew-core marker cannot be written"
fi
pass "macOS bootstrap fails when the homebrew-core marker cannot be written"

core_success_bin="$tmp_dir/core-success-bin"
core_success_state="$tmp_dir/core-success-state"
core_success_home="$tmp_dir/core-success-home"
core_success_log="$tmp_dir/core-success-brew.log"
mkdir -p "$core_success_bin" "$core_success_home"
write_brew_bundle_stub "$core_success_bin/brew"

HOME="$core_success_home" XDG_STATE_HOME="$core_success_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "stale core warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$core_success_home" XDG_STATE_HOME="$core_success_state" MACOS_BREW_LOG="$core_success_log" PATH="$core_success_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-success.out" 2>"$tmp_dir/core-success.err"; then
  fail "successful core Homebrew bundle succeeds"
fi

if [ -e "$core_success_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "successful core Homebrew bundle clears stale homebrew-core marker"
fi
pass "successful core Homebrew bundle clears stale homebrew-core marker"
assert_bundle_calls_disable_auto_update "$core_success_log" "macOS core bundle disables Homebrew auto-update"

for shellenv_mode in command-failure eval-failure; do
  shellenv_failure_state="$tmp_dir/core-shellenv-$shellenv_mode-state"
  shellenv_failure_home="$tmp_dir/core-shellenv-$shellenv_mode-home"
  shellenv_failure_log="$tmp_dir/core-shellenv-$shellenv_mode-brew.log"
  shellenv_failure_bin="$tmp_dir/core-shellenv-$shellenv_mode-bin"
  mkdir -p "$shellenv_failure_home" "$shellenv_failure_bin"
  write_brew_bundle_stub "$shellenv_failure_bin/brew"

  if ! HOME="$shellenv_failure_home" XDG_STATE_HOME="$shellenv_failure_state" \
    MACOS_BREW_LOG="$shellenv_failure_log" MACOS_BREW_SHELLENV_MODE="$shellenv_mode" \
    PATH="$shellenv_failure_bin:/usr/bin:/bin" \
    sh "$macos_bootstrap_script" >"$tmp_dir/core-shellenv-$shellenv_mode.out" 2>"$tmp_dir/core-shellenv-$shellenv_mode.err"; then
    fail "core Homebrew $shellenv_mode records a nonblocking warning"
  fi

  shellenv_failure_marker="$shellenv_failure_state/terrapod/install-warnings/homebrew-core"
  if [ ! -f "$shellenv_failure_marker" ]; then
    fail "core Homebrew $shellenv_mode records a homebrew-core marker"
  fi
  assert_contains "$(cat "$shellenv_failure_marker")" \
    "Fix Homebrew shellenv, then rerun tpod apply." \
    "core Homebrew $shellenv_mode marker provides actionable recovery guidance"
  if grep -F "brew args:bundle" "$shellenv_failure_log" >/dev/null; then
    fail "core Homebrew $shellenv_mode stops before bundle execution"
  fi
  pass "core Homebrew $shellenv_mode is category-scoped"
done

shellenv_marker_failure_home="$tmp_dir/core-shellenv-marker-failure-home"
shellenv_marker_failure_state="$tmp_dir/core-shellenv-marker-failure-state"
shellenv_marker_failure_bin="$tmp_dir/core-shellenv-marker-failure-bin"
mkdir -p "$shellenv_marker_failure_home" "$shellenv_marker_failure_bin"
: >"$shellenv_marker_failure_state"
write_brew_bundle_stub "$shellenv_marker_failure_bin/brew"
if HOME="$shellenv_marker_failure_home" XDG_STATE_HOME="$shellenv_marker_failure_state" \
  MACOS_BREW_LOG="$tmp_dir/core-shellenv-marker-failure.log" MACOS_BREW_SHELLENV_MODE=command-failure \
  PATH="$shellenv_marker_failure_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-shellenv-marker-failure.out" 2>"$tmp_dir/core-shellenv-marker-failure.err"; then
  fail "core Homebrew shellenv failure is hard when its warning marker cannot be written"
fi
pass "core Homebrew shellenv failure is hard only when its warning marker cannot be written"

core_detail_bin="$tmp_dir/core-detail-bin"
core_detail_state="$tmp_dir/core-detail-state"
core_detail_home="$tmp_dir/core-detail-home"
core_detail_log="$tmp_dir/core-detail-brew.log"
core_detail_prefix="$tmp_dir/core-detail-prefix"
mkdir -p "$core_detail_bin" "$core_detail_home" "$core_detail_prefix"
chmod 555 "$core_detail_prefix"
write_brew_bundle_stub "$core_detail_bin/brew"

if ! HOME="$core_detail_home" XDG_STATE_HOME="$core_detail_state" MACOS_BREW_LOG="$core_detail_log" MACOS_BREW_PREFIX="$core_detail_prefix" MACOS_BREW_ECHO_OUTPUT=1 MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_FORMULAE="gum" PATH="$core_detail_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-detail.out" 2>"$tmp_dir/core-detail.err"; then
  fail "core Homebrew bundle failure records a marker and does not block bootstrap script"
fi
chmod 755 "$core_detail_prefix"

assert_contains "$(cat "$tmp_dir/core-detail.out")" "visible brew bundle output:" "core Homebrew failure preserves visible brew output"
core_detail_marker="$core_detail_state/terrapod/install-warnings/homebrew-core"
if [ ! -f "$core_detail_marker" ]; then
  fail "core Homebrew bundle failure records a homebrew-core marker"
fi
pass "core Homebrew bundle failure records a homebrew-core marker"

core_detail_marker_text="$(cat "$core_detail_marker")"
assert_contains "$core_detail_marker_text" "category='homebrew-core'" "core marker keeps one stable category"
assert_contains "$core_detail_marker_text" "summary='Homebrew core install needs attention'" "core marker keeps stable summary"
assert_contains "$core_detail_marker_text" "failed formulae: gum" "core marker guidance includes reliable failed formula names"
assert_not_contains "$core_detail_marker_text" "failed casks:" "core marker guidance contains no cask detail after core casks are removed"
assert_contains "$core_detail_marker_text" "Homebrew prefix is not writable: $core_detail_prefix" "core marker guidance identifies unwritable shared prefix"
assert_not_contains "$core_detail_marker_text" "btop" "core marker excludes successful formula names"
assert_not_contains "$core_detail_marker_text" "chown" "core marker avoids broad ownership command guidance"

core_fallback_bin="$tmp_dir/core-fallback-bin"
core_fallback_state="$tmp_dir/core-fallback-state"
core_fallback_home="$tmp_dir/core-fallback-home"
core_fallback_log="$tmp_dir/core-fallback-brew.log"
mkdir -p "$core_fallback_bin" "$core_fallback_home"
write_brew_bundle_stub "$core_fallback_bin/brew"

if ! HOME="$core_fallback_home" XDG_STATE_HOME="$core_fallback_state" MACOS_BREW_LOG="$core_fallback_log" MACOS_BREW_FAIL_CORE_BULK=1 PATH="$core_fallback_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-fallback.out" 2>"$tmp_dir/core-fallback.err"; then
  fail "core Homebrew bulk-only failure records a marker and does not block bootstrap script"
fi

core_fallback_marker_text="$(cat "$core_fallback_state/terrapod/install-warnings/homebrew-core")"
assert_contains "$core_fallback_marker_text" "Review Homebrew core bundle output, fix package access, then rerun tpod apply." "core marker falls back to visible-output rerun guidance"
assert_not_contains "$core_fallback_marker_text" "failed formulae:" "core fallback marker avoids invented formula detail"
assert_not_contains "$core_fallback_marker_text" "failed casks:" "core fallback marker avoids invented cask detail"

core_retry_success_bin="$tmp_dir/core-retry-success-bin"
core_retry_success_state="$tmp_dir/core-retry-success-state"
core_retry_success_home="$tmp_dir/core-retry-success-home"
core_retry_success_log="$tmp_dir/core-retry-success-brew.log"
mkdir -p "$core_retry_success_bin" "$core_retry_success_home"
write_brew_bundle_stub "$core_retry_success_bin/brew"
HOME="$core_retry_success_home" XDG_STATE_HOME="$core_retry_success_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "stale core retry warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$core_retry_success_home" XDG_STATE_HOME="$core_retry_success_state" MACOS_BREW_LOG="$core_retry_success_log" PATH="$core_retry_success_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-retry-success.out" 2>"$tmp_dir/core-retry-success.err"; then
  fail "successful core reconciliation succeeds"
fi

if [ -e "$core_retry_success_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "successful core reconciliation clears homebrew-core marker"
fi
pass "successful core reconciliation clears homebrew-core marker"

core_retry_failure_bin="$tmp_dir/core-retry-failure-bin"
core_retry_failure_state="$tmp_dir/core-retry-failure-state"
core_retry_failure_home="$tmp_dir/core-retry-failure-home"
core_retry_failure_log="$tmp_dir/core-retry-failure-brew.log"
mkdir -p "$core_retry_failure_bin" "$core_retry_failure_home"
write_brew_bundle_stub "$core_retry_failure_bin/brew"
HOME="$core_retry_failure_home" XDG_STATE_HOME="$core_retry_failure_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "old core retry warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$core_retry_failure_home" XDG_STATE_HOME="$core_retry_failure_state" MACOS_BREW_LOG="$core_retry_failure_log" MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_FORMULAE="mise" PATH="$core_retry_failure_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-retry-failure.out" 2>"$tmp_dir/core-retry-failure.err"; then
  fail "failed core reconciliation records a replacement marker and exits successfully"
fi

core_retry_failure_marker_text="$(cat "$core_retry_failure_state/terrapod/install-warnings/homebrew-core")"
assert_contains "$core_retry_failure_marker_text" "failed formulae: mise" "failed core reconciliation replaces marker with current failed formula detail"
assert_not_contains "$core_retry_failure_marker_text" "old core retry warning" "failed core reconciliation replaces stale marker guidance"

assert_contains "$core_retry_failure_marker_text" "updated_at='" "failed core reconciliation replacement marker keeps updated_at"

# A cask post-install step that reads stdin must not consume the per-item record
# list, which would silently skip every package after the first.
core_stdin_bin="$tmp_dir/core-stdin-bin"
core_stdin_state="$tmp_dir/core-stdin-state"
core_stdin_home="$tmp_dir/core-stdin-home"
core_stdin_log="$tmp_dir/core-stdin-brew.log"
mkdir -p "$core_stdin_bin" "$core_stdin_home"
write_brew_bundle_stub "$core_stdin_bin/brew"

if ! HOME="$core_stdin_home" XDG_STATE_HOME="$core_stdin_state" MACOS_BREW_LOG="$core_stdin_log" MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_FORMULAE="bat zoxide" MACOS_BREW_DRAIN_STDIN=1 PATH="$core_stdin_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-stdin.out" 2>"$tmp_dir/core-stdin.err" </dev/null; then
  fail "core reconciliation survives a brew bundle that reads stdin"
fi

core_stdin_marker_text="$(cat "$core_stdin_state/terrapod/install-warnings/homebrew-core")"
assert_contains "$core_stdin_marker_text" "failed formulae: bat, zoxide" "core retry keeps reading records when brew bundle consumes stdin"

mise_missing_without_core_home="$tmp_dir/mise-missing-without-core-home"
mise_missing_without_core_state="$tmp_dir/mise-missing-without-core-state"
mkdir -p "$mise_missing_without_core_home"
mise_missing_without_core_status=0
assert_not_contains "$macos_brewfile" 'cask "ghostty"' "macOS default does not render Ghostty"
assert_not_contains "$macos_brewfile" 'cask "font-d2coding"' "macOS default does not render D2Coding"
assert_not_contains "$macos_brewfile" 'cask "font-hack-nerd-font"' "macOS default does not render Hack Nerd Font"
assert_not_contains "$macos_brewfile" 'cask "font-jetbrains-mono-nerd-font"' "macOS default does not render JetBrains Mono Nerd Font"
assert_not_contains "$macos_brewfile" 'cask "font-noto-sans-cjk-kr"' "macOS default does not render Noto Sans CJK KR"
assert_not_contains "$macos_brewfile" 'cask "cmux"' "macOS default does not render cmux"
assert_not_contains "$macos_brewfile" 'cask "hammerspoon"' "macOS default does not render Hammerspoon"
assert_not_contains "$macos_brewfile" 'cask "karabiner-elements"' "macOS default does not render Karabiner-Elements"
assert_not_contains "$macos_brewfile" 'cask "scroll-reverser"' "macOS default does not render Scroll Reverser"
assert_not_contains "$macos_brewfile" 'cask "raycast"' "macOS default does not render Raycast"
assert_not_contains "$macos_brewfile" 'cask "1password-cli"' "macOS default does not render 1Password CLI"
assert_not_contains "$macos_brewfile" 'cask "istat-menus"' "macOS default does not render iStat Menus"
assert_not_contains "$macos_brewfile" 'cask "claude"' "macOS default does not render Claude Desktop"
assert_not_contains "$macos_brewfile" 'cask "codex-app"' "macOS default does not render the unified ChatGPT desktop app"
assert_not_contains "$macos_brewfile" 'cask "chatgpt"' "macOS default does not render the legacy ChatGPT cask"
assert_not_contains "$macos_brewfile" 'cask "codex"' "macOS default does not render Codex CLI as a desktop app"
assert_not_contains "$macos_brewfile" 'cask "antigravity"' "macOS default does not render Antigravity 2.0"
assert_not_contains "$macos_brewfile" 'cask "antigravity-ide"' "macOS default does not render Antigravity IDE"
assert_not_contains "$macos_brewfile" 'cask "stablyai/orca/orca"' "macOS default does not render Orca"
assert_not_contains "$macos_brewfile" 'cask "orbstack"' "macOS default does not render OrbStack"

assert_not_contains "$linux_macos_desktop_apps_brewfile" 'cask "' \
  "Linux renders no macOS Desktop App Stack casks, including the terminal-apps fonts"

assert_contains "$terminal_apps_brewfile" 'cask "ghostty"' "terminal-apps group renders Ghostty"
assert_contains "$terminal_apps_brewfile" 'cask "font-d2coding"' "terminal-apps group renders D2Coding"
assert_contains "$terminal_apps_brewfile" 'cask "font-hack-nerd-font"' "terminal-apps group renders Hack Nerd Font"
assert_contains "$terminal_apps_brewfile" 'cask "font-jetbrains-mono-nerd-font"' "terminal-apps group renders JetBrains Mono Nerd Font"
assert_contains "$terminal_apps_brewfile" 'cask "font-noto-sans-cjk-kr"' "terminal-apps group renders Noto Sans CJK KR"
assert_not_contains "$terminal_apps_brewfile" 'cask "cmux"' "terminal-apps group does not render cmux"
assert_not_contains "$terminal_apps_brewfile" 'cask "hammerspoon"' "terminal-apps group does not render automation casks"

assert_contains "$automation_apps_brewfile" 'cask "hammerspoon"' "automation group renders Hammerspoon"
assert_contains "$automation_apps_brewfile" 'cask "karabiner-elements"' "automation group renders Karabiner-Elements"
assert_contains "$automation_apps_brewfile" 'cask "scroll-reverser"' "automation group renders Scroll Reverser"

assert_contains "$launcher_apps_brewfile" 'cask "raycast"' "launcher group renders Raycast"
assert_contains "$launcher_apps_brewfile" 'cask "1password-cli"' "launcher group renders 1Password CLI"

assert_contains "$monitoring_apps_brewfile" 'cask "istat-menus"' "monitoring group renders iStat Menus"

assert_contains "$development_apps_brewfile" 'cask "zed"' "development-apps group renders Zed"
assert_contains "$development_apps_brewfile" 'cask "stablyai/orca/orca", trusted: true' "development-apps group trusts only Orca's fully-qualified vendor cask"
assert_contains "$development_apps_brewfile" 'cask "orbstack"' "development-apps group renders OrbStack"
for removed_cask in claude codex-app chatgpt antigravity antigravity-ide; do
  assert_not_contains "$development_apps_brewfile" "cask \"$removed_cask\"" "development-apps group excludes removed desktop cask: $removed_cask"
done
development_apps_casks="$(
  printf '%s\n' "$development_apps_brewfile" |
    awk '/^[[:space:]]*cask[[:space:]]+"/ { print }'
)"
expected_development_apps_casks='cask "zed"
cask "stablyai/orca/orca", trusted: true
cask "orbstack"'
assert_equals \
  "$development_apps_casks" \
  "$expected_development_apps_casks" \
  "development-apps group renders exactly the expected casks"

assert_contains "$mobile_dev_brewfile" 'cask "android-studio"' "mobile-dev group renders Android Studio"
assert_contains "$mobile_dev_brewfile" 'brew "mobile-dev-inc/tap/maestro", trusted: true' "mobile-dev group trusts only the fully-qualified Maestro formula, not the entire mobile-dev-inc/tap tap"
assert_not_contains "$mobile_dev_brewfile" 'cask "maestro"' "mobile-dev group does not render the unrelated homebrew-cask maestro"
assert_not_contains "$mobile_dev_brewfile" 'tap "mobile-dev-inc/tap"' "mobile-dev group taps on demand through the fully-qualified token instead of trusting the whole tap"
assert_not_contains "$mobile_dev_brewfile" 'android-platform-tools' "mobile-dev group leaves platform tools to the Android SDK"
assert_not_contains "$mobile_dev_brewfile" 'cask "temurin"' "mobile-dev group resolves Java through the Android Studio bundled runtime instead of a declared JDK"
assert_not_contains "$mobile_dev_brewfile" 'cask "zed"' "mobile-dev group does not render development-apps casks"

mobile_dev_packages="$(
  printf '%s\n' "$mobile_dev_brewfile" |
    awk '/^[[:space:]]*(cask|brew)[[:space:]]+"/ { print }'
)"
expected_mobile_dev_packages='cask "android-studio"
brew "mobile-dev-inc/tap/maestro", trusted: true'
assert_equals \
  "$mobile_dev_packages" \
  "$expected_mobile_dev_packages" \
  "mobile-dev group renders exactly the expected packages"

assert_not_contains "$macos_brewfile" 'cask "android-studio"' "macOS default does not render Android Studio"
assert_not_contains "$macos_brewfile" 'mobile-dev-inc/tap/maestro' "macOS default does not render Maestro"
assert_not_contains "$development_apps_brewfile" 'cask "android-studio"' "development-apps group does not render Android Studio"

mobile_dev_zshenv="$(render_managed_file "$macos_mobile_dev_data" ".zshenv")"
macos_default_zshenv="$(render_managed_file "$macos_data" ".zshenv")"

assert_contains "$mobile_dev_zshenv" 'export ANDROID_HOME="$HOME/Library/Android/sdk"' "mobile-dev group exports ANDROID_HOME"
assert_contains "$mobile_dev_zshenv" 'export ANDROID_SDK_ROOT="$ANDROID_HOME"' "mobile-dev group exports ANDROID_SDK_ROOT"
assert_contains "$mobile_dev_zshenv" 'export JAVA_HOME="$android_studio_jbr"' "mobile-dev group resolves JAVA_HOME to the Android Studio bundled runtime"
assert_contains "$mobile_dev_zshenv" 'if [[ -d "$android_studio_jbr" ]]; then' "JAVA_HOME is guarded so a failed Android Studio install cannot break java"
assert_contains "$mobile_dev_zshenv" 'if [[ -d "$ANDROID_HOME/platform-tools" ]]; then' "platform-tools joins PATH only when the SDK provides it"
assert_not_contains "$mobile_dev_zshenv" '.maestro/bin' "Maestro resolves through the Homebrew prefix instead of its vendor install directory"
assert_not_contains "$mobile_dev_zshenv" 'mise activate' "the Android environment does not repeat the mise activation the managed zshrc already runs"
assert_not_contains "$macos_default_zshenv" 'ANDROID_HOME' "macOS default does not render the Android environment"

mobile_dev_android_line="$(printf '%s\n' "$mobile_dev_zshenv" | grep -n 'export ANDROID_HOME' | head -1 | cut -d: -f1)"
mobile_dev_override_line="$(printf '%s\n' "$mobile_dev_zshenv" | grep -n 'zsh/path.d' | head -1 | cut -d: -f1)"
if [ -z "$mobile_dev_android_line" ] || [ -z "$mobile_dev_override_line" ]; then
  fail "rendered .zshenv should contain both the Android environment and the machine-local override loop"
fi
if [ "$mobile_dev_android_line" -ge "$mobile_dev_override_line" ]; then
  fail "the Android environment must render before the machine-local override loop so explicit overrides still run last"
fi
pass "the Android environment renders before the machine-local override loop"

development_apps_zprofile="$(render_managed_file "$macos_development_apps_data" ".zprofile")"
macos_default_zprofile="$(render_managed_file "$macos_data" ".zprofile")"

assert_contains \
  "$development_apps_zprofile" \
  '. "$HOME/.orbstack/shell/init.zsh"' \
  "development-apps group renders the OrbStack shell integration in .zprofile"
assert_contains \
  "$development_apps_zprofile" \
  '[ -r "$HOME/.orbstack/shell/init.zsh" ]' \
  "OrbStack shell integration is guarded by a readability check so a missing cask cannot break login shells"
assert_not_contains \
  "$macos_default_zprofile" \
  '.orbstack/shell/init.zsh' \
  "macOS default does not render the OrbStack shell integration"

development_apps_bootstrap_script="$tmp_dir/macos-development-apps-bootstrap.sh"
render_template_with_homebrew_prefix_provider "$macos_development_apps_data" \
  '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
  "$tmp_dir/development-apps-failure-prefix" >"$development_apps_bootstrap_script"
sh -n "$development_apps_bootstrap_script" || fail "development-apps bootstrap script should be valid sh"
pass "development-apps bootstrap script is valid sh"

package_records_probe="$tmp_dir/package-records-probe.sh"
package_records_brewfile="$tmp_dir/package-records-brewfile"
cat >"$package_records_brewfile" <<'PROBE_BREWFILE'
# development-apps macOS App Group
cask "zed"
cask "stablyai/orca/orca", trusted: true
# mobile-dev macOS App Group
cask "android-studio"
brew "mobile-dev-inc/tap/maestro", trusted: true
PROBE_BREWFILE

sed -n '/^terrapod_homebrew_bundle_records()/,/^}/p' \
  "$repo_root/dot_local/lib/terrapod/homebrew-bundle.sh" \
  >"$package_records_probe"
printf '%s\n' 'terrapod_homebrew_bundle_records "$1"' >>"$package_records_probe"

package_records_output="$(sh "$package_records_probe" "$package_records_brewfile")"

expected_package_records="$(printf '%s\n' \
  'cask	zed	development-apps	cask "zed"' \
  'cask	stablyai/orca/orca	development-apps	cask "stablyai/orca/orca", trusted: true' \
  'cask	android-studio	mobile-dev	cask "android-studio"' \
  'brew	mobile-dev-inc/tap/maestro	mobile-dev	brew "mobile-dev-inc/tap/maestro", trusted: true')"

assert_equals \
  "$package_records_output" \
  "$expected_package_records" \
  "bundle records carry kind, name, group, and the verbatim declaration for casks and tap formulae"

headerless_records_brewfile="$tmp_dir/package-records-headerless-brewfile"
cat >"$headerless_records_brewfile" <<'PROBE_BREWFILE'
# Rendered opt-in macOS Desktop App Stack.
cask "ghostty"
# launcher macOS App Group
cask "raycast"
PROBE_BREWFILE

headerless_records_output="$(sh "$package_records_probe" "$headerless_records_brewfile")"

expected_headerless_records="$(printf '%s\n' \
  'cask	ghostty	-	cask "ghostty"' \
  'cask	raycast	launcher	cask "raycast"')"

assert_equals \
  "$headerless_records_output" \
  "$expected_headerless_records" \
  "a declaration before the first macOS App Group header keeps a non-empty group field"

assert_contains \
  "$macos_development_apps_bootstrap" \
  'read -r terrapod_bundle_kind terrapod_bundle_name terrapod_bundle_group terrapod_bundle_declaration' \
  "per-package retry reads the declaration field alongside the group and token"
assert_contains \
  "$macos_development_apps_bootstrap" \
  '>"$terrapod_bundle_item_file"' \
  "per-package retry writes the recorded declaration so options such as trusted: true survive"

core_options_bin="$tmp_dir/core-options-bin"
core_options_log="$tmp_dir/core-options.log"
core_options_brewfile="$tmp_dir/core-options.Brewfile"
mkdir -p "$core_options_bin"
printf '%s\n' 'brew "example/tap/tool", trusted: true' >"$core_options_brewfile"
write_stub "$core_options_bin/brew" \
  'case "$1" in' \
  '  bundle)' \
  '    for arg do case "$arg" in --file=*) file="${arg#--file=}" ;; esac; done' \
  '    cat "$file" >>"$CORE_OPTIONS_LOG"' \
  '    exit 42 ;;' \
  '  --prefix) printf "%s\n" /missing-prefix ;;' \
  'esac'
core_options_guidance="$(CORE_OPTIONS_LOG="$core_options_log" PATH="$core_options_bin:/usr/bin:/bin" \
  sh -c '. "$1"; . "$2"; terrapod_homebrew_core_run_bundle "$3" || printf "%s\n" "$TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT"' \
  sh "$repo_root/dot_local/lib/terrapod/homebrew-bundle.sh" \
  "$repo_root/dot_local/lib/terrapod/homebrew-core-bundle.sh" "$core_options_brewfile")"
assert_contains "$core_options_guidance" "failed formulae: example/tap/tool" "core retry names a failed tap formula"
assert_equals "$(wc -l <"$core_options_log" | tr -d ' ')" "2" "core bundle retries one declaration after bulk failure"
assert_file_contains "$core_options_log" 'brew "example/tap/tool", trusted: true' "core retry preserves Brewfile declaration options"

development_apps_failure_bin="$tmp_dir/development-apps-failure-prefix/bin"
development_apps_failure_state="$tmp_dir/development-apps-failure-state"
development_apps_failure_home="$tmp_dir/development-apps-failure-home"
development_apps_failure_log="$tmp_dir/development-apps-failure-brew.log"
mkdir -p "$development_apps_failure_bin" "$development_apps_failure_home"
write_brew_bundle_stub "$development_apps_failure_bin/brew"

if ! HOME="$development_apps_failure_home" XDG_STATE_HOME="$development_apps_failure_state" MACOS_BREW_LOG="$development_apps_failure_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="stablyai/orca/orca" PATH="$development_apps_failure_bin:/usr/bin:/bin" \
  sh "$development_apps_bootstrap_script" >"$tmp_dir/development-apps-failure.out" 2>"$tmp_dir/development-apps-failure.err"; then
  fail "Orca desktop app bundle failure does not block bootstrap script"
fi

development_apps_failure_marker="$development_apps_failure_state/terrapod/install-warnings/homebrew-desktop-apps"
if [ ! -f "$development_apps_failure_marker" ]; then
  fail "Orca desktop app bundle failure records a homebrew-desktop-apps marker"
fi
pass "Orca desktop app bundle failure records a homebrew-desktop-apps marker"

development_apps_failure_marker_text="$(cat "$development_apps_failure_marker")"
assert_contains "$development_apps_failure_marker_text" "failed casks: stablyai/orca/orca" "Orca failure attribution preserves its fully-qualified cask source"
assert_contains "$development_apps_failure_marker_text" "App Groups: development-apps" "Orca failure attribution identifies the development-apps group"

mobile_dev_bootstrap_script="$tmp_dir/macos-mobile-dev-bootstrap.sh"
render_template_with_homebrew_prefix_provider "$macos_mobile_dev_data" \
  '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
  "$tmp_dir/mobile-dev-failure-prefix" >"$mobile_dev_bootstrap_script"
sh -n "$mobile_dev_bootstrap_script" || fail "mobile-dev bootstrap script should be valid sh"
pass "mobile-dev bootstrap script is valid sh"

mobile_dev_failure_bin="$tmp_dir/mobile-dev-failure-prefix/bin"
mobile_dev_failure_state="$tmp_dir/mobile-dev-failure-state"
mobile_dev_failure_home="$tmp_dir/mobile-dev-failure-home"
mobile_dev_failure_log="$tmp_dir/mobile-dev-failure-brew.log"
mkdir -p "$mobile_dev_failure_bin" "$mobile_dev_failure_home"
write_brew_bundle_stub "$mobile_dev_failure_bin/brew"

if ! HOME="$mobile_dev_failure_home" XDG_STATE_HOME="$mobile_dev_failure_state" MACOS_BREW_LOG="$mobile_dev_failure_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_FORMULAE="mobile-dev-inc/tap/maestro" PATH="$mobile_dev_failure_bin:/usr/bin:/bin" \
  sh "$mobile_dev_bootstrap_script" >"$tmp_dir/mobile-dev-failure.out" 2>"$tmp_dir/mobile-dev-failure.err"; then
  fail "Maestro desktop app bundle failure does not block bootstrap script"
fi

mobile_dev_failure_marker="$mobile_dev_failure_state/terrapod/install-warnings/homebrew-desktop-apps"
if [ ! -f "$mobile_dev_failure_marker" ]; then
  fail "Maestro formula failure records a homebrew-desktop-apps marker"
fi
pass "Maestro formula failure records a homebrew-desktop-apps marker"

mobile_dev_failure_marker_text="$(cat "$mobile_dev_failure_marker")"
assert_contains "$mobile_dev_failure_marker_text" "mobile-dev-inc/tap/maestro" \
  "a failed tap formula is named in the desktop app warning marker"
assert_contains "$mobile_dev_failure_marker_text" "Review Homebrew desktop app bundle output for failed formulae:" \
  "a failed tap formula points to bundle output rather than cask output"
assert_contains "$mobile_dev_failure_marker_text" "App Groups: mobile-dev" \
  "a failed tap formula is attributed to its macOS App Group"

terminal_launcher_bootstrap_script="$tmp_dir/macos-terminal-launcher-bootstrap.sh"
render_template_with_homebrew_prefix_provider "$macos_terminal_launcher_apps_data" \
  '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
  "$tmp_dir/terminal-launcher-prefix" >"$terminal_launcher_bootstrap_script"
sh -n "$terminal_launcher_bootstrap_script" || fail "terminal and launcher bootstrap script should be valid sh"
pass "terminal and launcher bootstrap script is valid sh"

terminal_launcher_bin="$tmp_dir/terminal-launcher-prefix/bin"
terminal_launcher_state="$tmp_dir/terminal-launcher-state"
terminal_launcher_home="$tmp_dir/terminal-launcher-home"
terminal_launcher_log="$tmp_dir/terminal-launcher-brew.log"
mkdir -p "$terminal_launcher_bin" "$terminal_launcher_home"
write_brew_bundle_stub "$terminal_launcher_bin/brew"

if ! HOME="$terminal_launcher_home" XDG_STATE_HOME="$terminal_launcher_state" MACOS_BREW_LOG="$terminal_launcher_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty raycast" PATH="$terminal_launcher_bin:/usr/bin:/bin" \
  sh "$terminal_launcher_bootstrap_script" >"$tmp_dir/terminal-launcher.out" 2>"$tmp_dir/terminal-launcher.err"; then
  fail "macOS desktop app bundle failure does not block bootstrap script"
fi

terminal_launcher_marker="$terminal_launcher_state/terrapod/install-warnings/homebrew-desktop-apps"
if [ ! -f "$terminal_launcher_marker" ]; then
  fail "macOS desktop app bundle failure records a homebrew-desktop-apps marker"
fi
pass "macOS desktop app bundle failure records a homebrew-desktop-apps marker"

terminal_launcher_marker_text="$(cat "$terminal_launcher_marker")"
assert_contains "$terminal_launcher_marker_text" "category='homebrew-desktop-apps'" "desktop app marker keeps one stable category"
assert_contains "$terminal_launcher_marker_text" "summary='Homebrew desktop app install needs attention'" "desktop app marker keeps stable summary"
assert_contains "$terminal_launcher_marker_text" "failed casks: ghostty, raycast" "desktop app marker guidance includes only casks whose single-cask bundle failed"
assert_contains "$terminal_launcher_marker_text" "App Groups: terminal-apps, launcher" "desktop app marker guidance includes enabled App Groups"
assert_not_contains "$terminal_launcher_marker_text" "1password-cli" "desktop app marker excludes casks whose single-cask bundle succeeded"

# Same stdin hazard as the core retry loop: ghostty is the first record, so a
# stdin-reading brew would hide the later raycast failure entirely.
desktop_stdin_bin="$tmp_dir/desktop-stdin-bin"
desktop_stdin_state="$tmp_dir/desktop-stdin-state"
desktop_stdin_home="$tmp_dir/desktop-stdin-home"
desktop_stdin_log="$tmp_dir/desktop-stdin-brew.log"
mkdir -p "$desktop_stdin_bin" "$desktop_stdin_home"
write_brew_bundle_stub "$desktop_stdin_bin/brew"

if ! HOME="$desktop_stdin_home" XDG_STATE_HOME="$desktop_stdin_state" MACOS_BREW_LOG="$desktop_stdin_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty raycast" MACOS_BREW_DRAIN_STDIN=1 PATH="$desktop_stdin_bin:/usr/bin:/bin" \
  sh "$terminal_launcher_bootstrap_script" >"$tmp_dir/desktop-stdin.out" 2>"$tmp_dir/desktop-stdin.err" </dev/null; then
  fail "desktop app reconciliation survives a brew bundle that reads stdin"
fi

desktop_stdin_marker_text="$(cat "$desktop_stdin_state/terrapod/install-warnings/homebrew-desktop-apps")"
assert_contains "$desktop_stdin_marker_text" "failed casks: ghostty, raycast" "desktop app retry keeps reading records when brew bundle consumes stdin"

core_then_desktop_bin="$tmp_dir/core-then-desktop-bin"
core_then_desktop_state="$tmp_dir/core-then-desktop-state"
core_then_desktop_home="$tmp_dir/core-then-desktop-home"
core_then_desktop_log="$tmp_dir/core-then-desktop-brew.log"
mkdir -p "$core_then_desktop_bin" "$core_then_desktop_home"
write_brew_bundle_stub "$core_then_desktop_bin/brew"

if ! HOME="$core_then_desktop_home" XDG_STATE_HOME="$core_then_desktop_state" MACOS_BREW_LOG="$core_then_desktop_log" MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_FORMULAE="mise" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty raycast" PATH="$core_then_desktop_bin:/usr/bin:/bin" \
  sh "$terminal_launcher_bootstrap_script" >"$tmp_dir/core-then-desktop.out" 2>"$tmp_dir/core-then-desktop.err"; then
  printf '%s\n' "core then desktop stdout:" >&2
  sed 's/^/  /' "$tmp_dir/core-then-desktop.out" >&2
  printf '%s\n' "core then desktop stderr:" >&2
  sed 's/^/  /' "$tmp_dir/core-then-desktop.err" >&2
  fail "macOS bootstrap records core and desktop app warnings in one App Groups run"
fi

core_then_desktop_core_marker="$core_then_desktop_state/terrapod/install-warnings/homebrew-core"
core_then_desktop_desktop_marker="$core_then_desktop_state/terrapod/install-warnings/homebrew-desktop-apps"
if [ ! -f "$core_then_desktop_core_marker" ]; then
  fail "macOS bootstrap keeps homebrew-core marker when desktop App Groups also need attention"
fi
if [ ! -f "$core_then_desktop_desktop_marker" ]; then
  fail "macOS bootstrap continues to desktop App Groups after recording a homebrew-core marker"
fi
pass "macOS bootstrap records core and desktop app warnings in one App Groups run"

core_then_desktop_core_text="$(cat "$core_then_desktop_core_marker")"
core_then_desktop_desktop_text="$(cat "$core_then_desktop_desktop_marker")"
assert_contains "$core_then_desktop_core_text" "failed formulae: mise" "combined bootstrap core marker keeps failed formula detail"
assert_contains "$core_then_desktop_desktop_text" "failed casks: ghostty, raycast" "combined bootstrap desktop marker keeps failed cask detail"
assert_call_log_contains "$core_then_desktop_log" "terrapod-macos-desktop-apps" "combined bootstrap runs the optional desktop bundle"
assert_bundle_calls_disable_auto_update "$core_then_desktop_log" "macOS core and optional desktop bundles, including per-item retries, disable Homebrew auto-update"

terminal_launcher_marker_failure_bin="$tmp_dir/terminal-launcher-marker-failure-bin"
terminal_launcher_marker_failure_state="$tmp_dir/terminal-launcher-marker-failure-state"
terminal_launcher_marker_failure_home="$tmp_dir/terminal-launcher-marker-failure-home"
terminal_launcher_marker_failure_log="$tmp_dir/terminal-launcher-marker-failure-brew.log"
mkdir -p "$terminal_launcher_marker_failure_bin" "$terminal_launcher_marker_failure_home"
write_brew_bundle_stub "$terminal_launcher_marker_failure_bin/brew"
: >"$terminal_launcher_marker_failure_state"

if HOME="$terminal_launcher_marker_failure_home" XDG_STATE_HOME="$terminal_launcher_marker_failure_state" MACOS_BREW_LOG="$terminal_launcher_marker_failure_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty" PATH="$terminal_launcher_marker_failure_bin:/usr/bin:/bin" \
  sh "$terminal_launcher_bootstrap_script" >"$tmp_dir/terminal-launcher-marker-failure.out" 2>"$tmp_dir/terminal-launcher-marker-failure.err"; then
  fail "macOS desktop app bundle failure blocks when the warning marker cannot be recorded"
fi
pass "macOS desktop app bundle failure blocks when the warning marker cannot be recorded"

desktop_retry_marker_failure_bin="$tmp_dir/desktop-retry-marker-failure-prefix/bin"
desktop_retry_marker_failure_state="$tmp_dir/desktop-retry-marker-failure-state"
desktop_retry_marker_failure_home="$tmp_dir/desktop-retry-marker-failure-home"
desktop_retry_marker_failure_log="$tmp_dir/desktop-retry-marker-failure-brew.log"
desktop_retry_marker_failure_dir="$desktop_retry_marker_failure_state/terrapod/install-warnings"
mkdir -p "$desktop_retry_marker_failure_bin" "$desktop_retry_marker_failure_home"
mkdir -p "$desktop_retry_marker_failure_dir"
HOME="$desktop_retry_marker_failure_home" XDG_STATE_HOME="$desktop_retry_marker_failure_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-desktop-apps "Homebrew desktop app install needs attention" "Retry marker write failure setup."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"
write_stub "$desktop_retry_marker_failure_bin/brew" \
  'printf "%s\n" "brew args:$*" >>"$MACOS_BREW_LOG"' \
  'case "$1" in' \
  '  shellenv) printf "%s\n" ":" ;;' \
  '  analytics) exit 0 ;;' \
  '  bundle)' \
  '    rm -rf "$DESKTOP_RETRY_MARKER_FAILURE_DIR"' \
  '    : >"$DESKTOP_RETRY_MARKER_FAILURE_DIR"' \
  '    exit 42' \
  '    ;;' \
  '  *) exit 64 ;;' \
  'esac'

desktop_retry_marker_failure_script="$tmp_dir/desktop-retry-marker-failure.sh"
render_template_with_homebrew_prefix_provider "$macos_terminal_launcher_apps_data" \
  '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
  "$tmp_dir/desktop-retry-marker-failure-prefix" >"$desktop_retry_marker_failure_script"

if HOME="$desktop_retry_marker_failure_home" XDG_STATE_HOME="$desktop_retry_marker_failure_state" DESKTOP_RETRY_MARKER_FAILURE_DIR="$desktop_retry_marker_failure_dir" MACOS_BREW_LOG="$desktop_retry_marker_failure_log" PATH="$desktop_retry_marker_failure_bin:/usr/bin:/bin" \
  sh "$desktop_retry_marker_failure_script" >"$tmp_dir/desktop-retry-marker-failure.out" 2>"$tmp_dir/desktop-retry-marker-failure.err"; then
  fail "macOS desktop reconciliation failure blocks when the warning marker cannot be recorded"
fi
pass "macOS desktop reconciliation failure blocks when the warning marker cannot be recorded"

bulk_only_bootstrap_script="$tmp_dir/macos-bulk-only-bootstrap.sh"
render_template_with_homebrew_prefix_provider "$macos_terminal_launcher_apps_data" \
  '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
  "$tmp_dir/bulk-only-prefix" >"$bulk_only_bootstrap_script"
sh -n "$bulk_only_bootstrap_script" || fail "bulk-only desktop bootstrap script should be valid sh"
pass "bulk-only desktop bootstrap script is valid sh"

bulk_only_bin="$tmp_dir/bulk-only-prefix/bin"
bulk_only_state="$tmp_dir/bulk-only-state"
bulk_only_home="$tmp_dir/bulk-only-home"
bulk_only_log="$tmp_dir/bulk-only-brew.log"
mkdir -p "$bulk_only_bin" "$bulk_only_home"
write_brew_bundle_stub "$bulk_only_bin/brew"

if ! HOME="$bulk_only_home" XDG_STATE_HOME="$bulk_only_state" MACOS_BREW_LOG="$bulk_only_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 PATH="$bulk_only_bin:/usr/bin:/bin" \
  sh "$bulk_only_bootstrap_script" >"$tmp_dir/bulk-only.out" 2>"$tmp_dir/bulk-only.err"; then
  fail "macOS desktop app bulk-only bundle failure does not block bootstrap script"
fi

bulk_only_marker_text="$(cat "$bulk_only_state/terrapod/install-warnings/homebrew-desktop-apps")"
assert_contains "$bulk_only_marker_text" "Review Homebrew desktop app bundle output" "desktop app marker falls back when bulk fails but single-cask attribution succeeds"
assert_not_contains "$bulk_only_marker_text" "failed casks:" "desktop app bulk-only fallback avoids invented cask detail"
assert_not_contains "$bulk_only_marker_text" "App Groups:" "desktop app bulk-only fallback avoids invented App Group detail"

fallback_bootstrap_script="$tmp_dir/macos-desktop-fallback-bootstrap.sh"
awk '
  $0 == "BREWFILE" && in_brewfile == 1 {
    print "# rendered desktop stack without reliable cask detail"
    print "tap \"homebrew/cask\""
    print "BREWFILE"
    in_brewfile = 0
    next
  }
  in_brewfile == 1 { next }
  $0 == "cat >\"$desktop_brewfile\" <<'\''BREWFILE'\''" {
    print
    in_brewfile = 1
    next
  }
  { print }
' "$terminal_launcher_bootstrap_script" |
  sed "s#$tmp_dir/terminal-launcher-prefix/bin/brew#$tmp_dir/fallback-bin/brew#g" >"$fallback_bootstrap_script"
sh -n "$fallback_bootstrap_script" || fail "fallback desktop bootstrap script should be valid sh"
pass "fallback desktop bootstrap script is valid sh"

fallback_bin="$tmp_dir/fallback-bin"
fallback_state="$tmp_dir/fallback-state"
fallback_home="$tmp_dir/fallback-home"
fallback_log="$tmp_dir/fallback-brew.log"
mkdir -p "$fallback_bin" "$fallback_home"
write_brew_bundle_stub "$fallback_bin/brew"

if ! HOME="$fallback_home" XDG_STATE_HOME="$fallback_state" MACOS_BREW_LOG="$fallback_log" MACOS_BREW_FAIL_BULK=1 PATH="$fallback_bin:/usr/bin:/bin" \
  sh "$fallback_bootstrap_script" >"$tmp_dir/fallback.out" 2>"$tmp_dir/fallback.err"; then
  fail "macOS desktop app fallback bundle failure does not block bootstrap script"
fi

fallback_marker_text="$(cat "$fallback_state/terrapod/install-warnings/homebrew-desktop-apps")"
assert_contains "$fallback_marker_text" "Review Homebrew desktop app bundle output" "desktop app marker falls back to bulk bundle guidance when casks are not reliable"
assert_not_contains "$fallback_marker_text" "failed casks:" "desktop app fallback marker avoids invented cask detail"
assert_not_contains "$fallback_marker_text" "App Groups:" "desktop app fallback marker avoids invented App Group detail"

headerless_bootstrap_script="$tmp_dir/macos-desktop-headerless-bootstrap.sh"
awk '
  $0 == "BREWFILE" && in_brewfile == 1 {
    print "# Rendered opt-in macOS Desktop App Stack."
    print "cask \"ghostty\""
    print "# launcher macOS App Group"
    print "cask \"raycast\""
    print "BREWFILE"
    in_brewfile = 0
    next
  }
  in_brewfile == 1 { next }
  $0 == "cat >\"$desktop_brewfile\" <<'\''BREWFILE'\''" {
    print
    in_brewfile = 1
    next
  }
  { print }
' "$terminal_launcher_bootstrap_script" |
  sed "s#$tmp_dir/terminal-launcher-prefix/bin/brew#$tmp_dir/headerless-bin/brew#g" >"$headerless_bootstrap_script"
sh -n "$headerless_bootstrap_script" || fail "headerless desktop bootstrap script should be valid sh"
pass "headerless desktop bootstrap script is valid sh"

headerless_bin="$tmp_dir/headerless-bin"
headerless_state="$tmp_dir/headerless-state"
headerless_home="$tmp_dir/headerless-home"
headerless_log="$tmp_dir/headerless-brew.log"
mkdir -p "$headerless_bin" "$headerless_home"
write_brew_bundle_stub "$headerless_bin/brew"

# The stub fails a cask by grepping the single-package Brewfile it is handed, so
# a collapsed record that writes an empty Brewfile makes the cask trivially
# "succeed" and hides the failure entirely.
if ! HOME="$headerless_home" XDG_STATE_HOME="$headerless_state" MACOS_BREW_LOG="$headerless_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty" PATH="$headerless_bin:/usr/bin:/bin" \
  sh "$headerless_bootstrap_script" >"$tmp_dir/headerless.out" 2>"$tmp_dir/headerless.err"; then
  fail "headerless desktop app bundle failure does not block bootstrap script"
fi

headerless_marker="$headerless_state/terrapod/install-warnings/homebrew-desktop-apps"
if [ ! -f "$headerless_marker" ]; then
  fail "headerless desktop app bundle failure records a homebrew-desktop-apps marker"
fi
pass "headerless desktop app bundle failure records a homebrew-desktop-apps marker"

headerless_marker_text="$(cat "$headerless_marker")"
assert_contains "$headerless_marker_text" "failed casks: ghostty" \
  "a cask declared before the first macOS App Group header is still retried and attributed"
assert_not_contains "$headerless_marker_text" "App Groups:" \
  "a cask with no macOS App Group contributes no App Group detail"
assert_not_contains "$headerless_marker_text" "raycast" \
  "a grouped cask whose single-cask bundle succeeds stays out of the marker"

terminal_only_bootstrap_script="$tmp_dir/macos-terminal-only-bootstrap.sh"
render_template_with_homebrew_prefix_provider "$macos_terminal_apps_data" \
  '.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl' \
  "$tmp_dir/terminal-only-prefix" >"$terminal_only_bootstrap_script"
sh -n "$terminal_only_bootstrap_script" || fail "terminal-only bootstrap script should be valid sh"
pass "terminal-only bootstrap script is valid sh"

terminal_only_bin="$tmp_dir/terminal-only-prefix/bin"
terminal_only_state="$tmp_dir/terminal-only-state"
terminal_only_home="$tmp_dir/terminal-only-home"
terminal_only_log="$tmp_dir/terminal-only-brew.log"
mkdir -p "$terminal_only_bin" "$terminal_only_home"
write_brew_bundle_stub "$terminal_only_bin/brew"

HOME="$terminal_only_home" XDG_STATE_HOME="$terminal_only_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-desktop-apps "Homebrew desktop app install needs attention" "Review Homebrew cask output for failed casks: ghostty, raycast, 1password-cli; App Groups: terminal-apps, launcher, then rerun tpod apply."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$terminal_only_home" XDG_STATE_HOME="$terminal_only_state" MACOS_BREW_LOG="$terminal_only_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty" PATH="$terminal_only_bin:/usr/bin:/bin" \
  sh "$terminal_only_bootstrap_script" >"$tmp_dir/terminal-only.out" 2>"$tmp_dir/terminal-only.err"; then
  fail "terminal-only desktop app bundle failure does not block bootstrap script"
fi

terminal_only_marker_text="$(cat "$terminal_only_state/terrapod/install-warnings/homebrew-desktop-apps")"
assert_contains "$terminal_only_marker_text" "failed casks: ghostty" "enabled terminal-apps failure remains in desktop app marker"
assert_contains "$terminal_only_marker_text" "App Groups: terminal-apps" "enabled terminal-apps group remains in desktop app marker"
assert_not_contains "$terminal_only_marker_text" "raycast" "disabled launcher cask is removed from desktop app marker"
assert_not_contains "$terminal_only_marker_text" "1password-cli" "disabled launcher CLI cask is removed from desktop app marker"
assert_not_contains "$terminal_only_marker_text" "launcher" "disabled launcher group is removed from desktop app marker"

terminal_success_bin="$tmp_dir/terminal-success-bin"
terminal_success_state="$tmp_dir/terminal-success-state"
terminal_success_home="$tmp_dir/terminal-success-home"
terminal_success_log="$tmp_dir/terminal-success-brew.log"
mkdir -p "$terminal_success_bin" "$terminal_success_home"
write_brew_bundle_stub "$terminal_success_bin/brew"

HOME="$terminal_success_home" XDG_STATE_HOME="$terminal_success_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-desktop-apps "Homebrew desktop app install needs attention" "Review Homebrew cask output for failed casks: ghostty; App Groups: terminal-apps, then rerun tpod apply."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$terminal_success_home" XDG_STATE_HOME="$terminal_success_state" MACOS_BREW_LOG="$terminal_success_log" PATH="$terminal_success_bin:/usr/bin:/bin" \
  sh "$terminal_only_bootstrap_script" >"$tmp_dir/terminal-success.out" 2>"$tmp_dir/terminal-success.err"; then
  fail "successful terminal-only desktop app rerun succeeds"
fi

if [ -e "$terminal_success_state/terrapod/install-warnings/homebrew-desktop-apps" ]; then
  fail "successful desktop app rerun clears homebrew-desktop-apps marker"
fi
pass "successful desktop app rerun clears homebrew-desktop-apps marker"

assert_contains \
  "$macos_terminal_apps_bootstrap" \
  "terrapod-macos-desktop-apps" \
  "terminal-apps group renders macOS Desktop App Stack Brewfile"

assert_contains \
  "$macos_terminal_apps_bootstrap" \
  'run_desktop_app_bundle "$desktop_brewfile"' \
  "terminal-apps group runs macOS Desktop App Stack installer"

assert_contains \
  "$macos_development_apps_bootstrap" \
  "terrapod-macos-desktop-apps" \
  "development-apps group renders macOS Desktop App Stack Brewfile"

assert_contains \
  "$macos_development_apps_bootstrap" \
  'run_desktop_app_bundle "$desktop_brewfile"' \
  "development-apps group runs macOS Desktop App Stack installer"

assert_not_contains \
  "$macos_development_workspace_bootstrap" \
  "Brewfile.macos-desktop-apps" \
  "enableDevelopmentWorkspace does not imply macOS Desktop App Stack Brewfile"
