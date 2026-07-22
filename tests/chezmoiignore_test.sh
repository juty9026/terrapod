#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
chezmoi_config="$tmp_dir/chezmoi.toml"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

: >"$chezmoi_config"

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

managed_source_paths() {
  data="$1"
  chezmoi \
    --config "$chezmoi_config" \
    --source "$repo_root" \
    --override-data "$data" \
    managed \
    --path-style source-relative
}

managed_target_paths() {
  data="$1"
  chezmoi \
    --config "$chezmoi_config" \
    --source "$repo_root" \
    --override-data "$data" \
    managed
}

managed_target_paths_from_source() {
  data="$1"
  source="$2"

  chezmoi \
    --config "$chezmoi_config" \
    --source "$source" \
    --override-data "$data" \
    managed
}

render_template() {
  data="$1"
  file="$2"

  chezmoi \
    --config "$chezmoi_config" \
    --source "$repo_root" \
    execute-template \
    --override-data "$data" \
    --file "$repo_root/$file"
}

render_managed_file() {
  data="$1"
  destination="$2"

  chezmoi \
    --config "$chezmoi_config" \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    --override-data "$data" \
    cat "$tmp_dir/home/$destination"
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

assert_contains_text() {
  text="$1"
  needle="$2"
  message="$3"

  if ! printf '%s\n' "$text" | grep -F -- "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains_text() {
  text="$1"
  needle="$2"
  message="$3"

  if printf '%s\n' "$text" | grep -F -- "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_texts_differ() {
  left="$1"
  right="$2"
  message="$3"

  if [ "$left" = "$right" ]; then
    fail "$message"
  fi

  pass "$message"
}

assert_text_equals() {
  actual="$1"
  expected="$2"
  message="$3"

  if [ "$actual" != "$expected" ]; then
    printf '%s\n' "expected:" >&2
    printf '%s\n' "$expected" | sed 's/^/  /' >&2
    printf '%s\n' "actual:" >&2
    printf '%s\n' "$actual" | sed 's/^/  /' >&2
    fail "$message"
  fi

  pass "$message"
}

assert_managed_paths_exclude_prefix() {
  managed_paths="$1"
  prefix="$2"
  message="$3"

  if printf '%s\n' "$managed_paths" | grep -E "^${prefix}(/|$)" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_managed_paths_include_prefix() {
  managed_paths="$1"
  prefix="$2"
  message="$3"

  if ! printf '%s\n' "$managed_paths" | grep -E "^${prefix}(/|$)" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

managed_tests="$(
  chezmoi \
    --config "$chezmoi_config" \
    --source "$repo_root" \
    managed \
    --path-style source-relative |
    grep '^tests/' || true
)"

if [ -n "$managed_tests" ]; then
  fail "development tests should not be managed by chezmoi: $managed_tests"
fi

pass "development tests are ignored by chezmoi"

managed_repository_docs="$(
  chezmoi \
    --config "$chezmoi_config" \
    --source "$repo_root" \
    managed \
    --path-style source-relative |
    grep -E '^(README(\.ko)?\.md|AGENTS\.md|CONTEXT\.md|docs/)' || true
)"

if [ -n "$managed_repository_docs" ]; then
  fail "repository documentation should not be managed by chezmoi: $managed_repository_docs"
fi

pass "repository documentation is ignored by chezmoi"

ubuntu_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false}'
ubuntu_managed="$(managed_source_paths "$ubuntu_data")"
macos_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false}'
macos_managed="$(managed_source_paths "$macos_data")"
macos_managed_targets="$(managed_target_paths "$macos_data")"
macos_terminal_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":true,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false}'
macos_automation_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":true,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false}'
macos_launcher_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":true,"enableMacosAppGroupMonitoring":false}'
macos_terminal_launcher_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":true,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":true,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false}'
macos_monitoring_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":true}'
macos_development_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":true}'
macos_terminal_apps_managed_targets="$(managed_target_paths "$macos_terminal_apps_data")"
macos_development_apps_managed="$(managed_source_paths "$macos_development_apps_data")"
macos_ai_cli_tools_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":true,"enableDevelopmentWorkspace":false}'
macos_ai_cli_tools_managed="$(managed_source_paths "$macos_ai_cli_tools_data")"
macos_development_workspace_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":true}'
macos_development_workspace_managed="$(managed_source_paths "$macos_development_workspace_data")"
editor_stack_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":true,"enableAiCliTools":false,"enableDevelopmentWorkspace":false}'
editor_stack_managed="$(managed_source_paths "$editor_stack_data")"
ai_cli_tools_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":true,"enableDevelopmentWorkspace":false}'
ai_cli_tools_managed="$(managed_source_paths "$ai_cli_tools_data")"
development_workspace_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":true}'
development_workspace_managed="$(managed_source_paths "$development_workspace_data")"

assert_managed_paths_exclude_prefix \
  "$macos_ai_cli_tools_managed" \
  "Brewfile.ai-cli-tools.tmpl" \
  "macOS does not manage the rendered AI CLI tools Brewfile"

assert_managed_paths_exclude_prefix \
  "$ai_cli_tools_managed" \
  "Brewfile.ai-cli-tools.tmpl" \
  "Ubuntu does not manage the rendered AI CLI tools Brewfile"

for entry in \
  .chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl \
  .chezmoiscripts/run_before_11-retry-homebrew-core.sh.tmpl \
  dot_local/lib/terrapod/homebrew-core-bundle.sh
do
  printf '%s\n' "$ubuntu_managed" | grep -Fx "$entry" >/dev/null ||
    fail "Ubuntu manages cross-profile Homebrew entry: $entry"
done
pass "Ubuntu manages cross-profile Homebrew core state"

printf '%s\n' "$ubuntu_managed" | grep -Fx '.chezmoiscripts/run_before_12-retry-homebrew-macos-platform.sh.tmpl' >/dev/null &&
  fail "Ubuntu must not manage macOS platform retry state"
pass "Ubuntu excludes macOS platform retry state"

macos_only_entries="
.chezmoiscripts/run_before_12-retry-homebrew-macos-platform.sh.tmpl
.chezmoiscripts/run_before_13-retry-homebrew-desktop-apps.sh.tmpl
.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl
dot_config/ghostty
dot_config/private_karabiner
dot_hammerspoon
dot_zprofile
"

for entry in $macos_only_entries; do
  if printf '%s\n' "$ubuntu_managed" | grep -Fx "$entry" >/dev/null; then
    fail "Ubuntu VPS should not manage macOS-only entry: $entry"
  fi
done

pass "Ubuntu VPS ignores macOS-only entries"

macos_brewfile="$(render_template "$macos_data" "Brewfile.macos-desktop-apps.tmpl")"
terminal_apps_brewfile="$(render_template "$macos_terminal_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
automation_apps_brewfile="$(render_template "$macos_automation_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
launcher_apps_brewfile="$(render_template "$macos_launcher_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
monitoring_apps_brewfile="$(render_template "$macos_monitoring_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
development_apps_brewfile="$(render_template "$macos_development_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
ubuntu_homebrew_bootstrap="$(render_template "$ubuntu_data" ".chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl")"
macos_bootstrap="$(render_template "$macos_data" ".chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl")"
macos_terminal_apps_bootstrap="$(render_template "$macos_terminal_apps_data" ".chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl")"
macos_terminal_launcher_apps_bootstrap="$(render_template "$macos_terminal_launcher_apps_data" ".chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl")"
macos_development_apps_bootstrap="$(render_template "$macos_development_apps_data" ".chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl")"
macos_development_workspace_bootstrap="$(render_template "$macos_development_workspace_data" ".chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl")"
macos_core_retry="$(render_template "$macos_data" ".chezmoiscripts/run_before_11-retry-homebrew-core.sh.tmpl")"
ubuntu_core_retry="$(render_template "$ubuntu_data" ".chezmoiscripts/run_before_11-retry-homebrew-core.sh.tmpl")"
macos_platform_retry="$(render_template "$macos_data" ".chezmoiscripts/run_before_12-retry-homebrew-macos-platform.sh.tmpl")"
macos_desktop_retry="$(render_template "$macos_data" ".chezmoiscripts/run_before_13-retry-homebrew-desktop-apps.sh.tmpl")"
macos_terminal_apps_desktop_retry="$(render_template "$macos_terminal_apps_data" ".chezmoiscripts/run_before_13-retry-homebrew-desktop-apps.sh.tmpl")"
macos_mise_tools_installer="$(render_template "$macos_data" ".chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl")"
macos_karabiner_opener="$(render_template "$macos_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
macos_terminal_apps_karabiner_opener="$(render_template "$macos_terminal_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
macos_automation_apps_karabiner_opener="$(render_template "$macos_automation_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
macos_development_apps_karabiner_opener="$(render_template "$macos_development_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"

assert_contains_text \
  "$macos_bootstrap" \
  'terrapod_homebrew_core_run_bundle "$core_brewfile"' \
  "macOS bootstrap always runs the core Brewfile through the core bundle helper"

assert_contains_text "$ubuntu_homebrew_bootstrap" 'core_brewfile="' "Ubuntu renders the mandatory CLI bundle"
assert_not_contains_text "$ubuntu_homebrew_bootstrap" 'Brewfile.macos"' "Ubuntu excludes the macOS platform bundle"
assert_contains_text "$macos_bootstrap" 'macos_brewfile="' "macOS renders the platform bundle"
assert_contains_text "$macos_bootstrap" 'homebrew-macos-platform' "macOS uses a separate platform warning category"
assert_contains_text "$ubuntu_homebrew_bootstrap" 'HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade' "Ubuntu bundle apply disables automatic updates"
assert_not_contains_text "$ubuntu_homebrew_bootstrap" 'linux:arm64' "Ubuntu Homebrew bootstrap rejects the arm64 identifier"
assert_not_contains_text "$ubuntu_core_retry" 'linux:arm64' "Ubuntu Homebrew retry rejects the arm64 identifier"
assert_contains_text "$ubuntu_core_retry" 'linux:x86_64|linux:aarch64)' "Ubuntu Homebrew retry accepts exactly the supported Linux architecture identifiers"

for bundle_source in \
  "$repo_root/.chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl" \
  "$repo_root/.chezmoiscripts/run_before_13-retry-homebrew-desktop-apps.sh.tmpl" \
  "$repo_root/dot_local/lib/terrapod/homebrew-core-bundle.sh"
do
  unguarded_bundle_calls="$(grep 'brew bundle --no-upgrade' "$bundle_source" | grep -v 'HOMEBREW_NO_AUTO_UPDATE=1' || true)"
  if [ -n "$unguarded_bundle_calls" ]; then
    fail "every Homebrew bundle call disables automatic updates: $bundle_source"
  fi
done
pass "every Homebrew bundle call disables automatic updates"

assert_not_contains_text \
  "$macos_bootstrap" \
  "Brewfile.macos-desktop-apps" \
  "macOS bootstrap default skips macOS Desktop App Stack Brewfile"

assert_not_contains_text \
  "$macos_bootstrap" \
  "terrapod-macos-desktop-apps" \
  "macOS bootstrap default skips macOS Desktop App Stack temp Brewfile"

assert_not_contains_text \
  "$macos_bootstrap" \
  'brew bundle --no-upgrade --file="$desktop_brewfile"' \
  "macOS bootstrap default skips macOS Desktop App Stack bundle"

assert_contains_text \
  "$macos_bootstrap" \
  "clear_install_warning homebrew-desktop-apps" \
  "macOS bootstrap default renders macOS Desktop App Stack warning cleanup"

macos_bootstrap_script="$tmp_dir/macos-bootstrap-default.sh"
printf '%s\n' "$macos_bootstrap" >"$macos_bootstrap_script"
sh -n "$macos_bootstrap_script" || fail "macOS bootstrap default cleanup script should be valid sh"
pass "macOS bootstrap default cleanup script is valid sh"

macos_core_retry_script="$tmp_dir/macos-core-retry.sh"
printf '%s\n' "$macos_core_retry" >"$macos_core_retry_script"
sh -n "$macos_core_retry_script" || fail "macOS core retry script should be valid sh"
pass "macOS core retry script is valid sh"

macos_platform_retry_script="$tmp_dir/macos-platform-retry.sh"
printf '%s\n' "$macos_platform_retry" >"$macos_platform_retry_script"
sh -n "$macos_platform_retry_script" || fail "macOS platform retry script should be valid sh"
pass "macOS platform retry script is valid sh"

macos_desktop_retry_script="$tmp_dir/macos-desktop-retry-default.sh"
printf '%s\n' "$macos_desktop_retry" >"$macos_desktop_retry_script"
sh -n "$macos_desktop_retry_script" || fail "macOS desktop retry default cleanup script should be valid sh"
pass "macOS desktop retry default cleanup script is valid sh"

macos_terminal_apps_desktop_retry_script="$tmp_dir/macos-terminal-desktop-retry.sh"
printf '%s\n' "$macos_terminal_apps_desktop_retry" >"$macos_terminal_apps_desktop_retry_script"
sh -n "$macos_terminal_apps_desktop_retry_script" || fail "macOS desktop retry App Group script should be valid sh"
pass "macOS desktop retry App Group script is valid sh"

macos_mise_missing_script="$tmp_dir/macos-mise-missing.sh"
printf '%s\n' "$macos_mise_tools_installer" |
  sed \
    -e "s#/opt/homebrew/bin/brew#$tmp_dir/missing-opt-homebrew-brew#g" \
    -e "s#/usr/local/bin/brew#$tmp_dir/missing-usr-local-brew#g" \
    -e "s#/opt/homebrew/bin/mise#$tmp_dir/missing-opt-homebrew-mise#g" \
    -e "s#/usr/local/bin/mise#$tmp_dir/missing-usr-local-mise#g" \
    >"$macos_mise_missing_script"
sh -n "$macos_mise_missing_script" || fail "macOS mise tool installer missing-mise test script should be valid sh"
pass "macOS mise tool installer missing-mise test script is valid sh"

macos_brew_bin="$tmp_dir/macos-brew-bin"
macos_brew_log="$tmp_dir/macos-brew.log"
mkdir -p "$macos_brew_bin"
write_stub "$macos_brew_bin/brew" \
  'printf "%s\n" "brew args:$*" >>"$MACOS_BREW_LOG"' \
  'case "$1" in' \
  '  shellenv) printf "%s\n" ":" ;;' \
  '  analytics) exit 0 ;;' \
  '  bundle) exit 0 ;;' \
  '  *) exit 64 ;;' \
  'esac'

write_brew_bundle_stub() {
  path="$1"

  write_stub "$path" \
    'printf "%s\n" "brew args:$*" >>"$MACOS_BREW_LOG"' \
    'bundle_file=' \
    'for arg do' \
    '  case "$arg" in' \
    '    --file=*) bundle_file="${arg#--file=}" ;;' \
    '  esac' \
    'done' \
    'case "$1" in' \
    '  --prefix) printf "%s\n" "${MACOS_BREW_PREFIX:-/opt/homebrew}"; exit 0 ;;' \
    '  shellenv) printf "%s\n" ":" ;;' \
    '  analytics) exit 0 ;;' \
    '  bundle)' \
    '    if [ "${MACOS_BREW_ECHO_OUTPUT:-}" = "1" ]; then' \
    '      printf "%s\n" "visible brew bundle output: $*"' \
    '    fi' \
    '    for formula in ${MACOS_BREW_FAIL_FORMULAE:-}; do' \
    '      if [ -n "$bundle_file" ] && grep -Fx "brew \"$formula\"" "$bundle_file" >/dev/null 2>&1; then' \
    '        exit 42' \
    '      fi' \
    '    done' \
    '    if [ "${MACOS_BREW_FAIL_CORE_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "brew \"mise\"" "$bundle_file" >/dev/null 2>&1 && grep -Fx "brew \"btop\"" "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    for cask in ${MACOS_BREW_FAIL_CASKS:-}; do' \
    '      if [ -n "$bundle_file" ] && grep -Fx "cask \"$cask\"" "$bundle_file" >/dev/null 2>&1; then' \
    '        exit 42' \
    '      fi' \
    '    done' \
    '    if [ "${MACOS_BREW_FAIL_DESKTOP_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "# Rendered opt-in macOS Desktop App Stack." "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    if [ "${MACOS_BREW_FAIL_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "tap \"homebrew/cask\"" "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    exit 0' \
    '    ;;' \
    '  *) exit 64 ;;' \
    'esac'
}

run_linux_homebrew_arch_case() {
  arch="$1"
  expected_status="$2"
  case_dir="$tmp_dir/linux-homebrew-arch-$arch"
  case_bin="$case_dir/bin"
  case_script="$case_dir/bootstrap.sh"
  case_log="$case_dir/commands.log"
  case_state="$case_dir/state"
  case_home="$case_dir/home"
  mkdir -p "$case_bin" "$case_home"

  write_stub "$case_bin/uname" "printf '%s\\n' '$arch'"
  write_stub "$case_bin/curl" \
    'printf "%s\n" "curl args:$*" >>"$LINUX_HOMEBREW_ARCH_LOG"' \
    'exit 97'
  write_stub "$case_bin/brew" \
    'printf "%s\n" "brew args:$*" >>"$LINUX_HOMEBREW_ARCH_LOG"' \
    'case "$1" in' \
    '  shellenv) printf "%s\n" ":" ;;' \
    '  analytics|bundle) exit 0 ;;' \
    '  --prefix) printf "%s\n" "$LINUX_HOMEBREW_ARCH_PREFIX" ;;' \
    '  *) exit 64 ;;' \
    'esac'

  printf '%s\n' "$ubuntu_homebrew_bootstrap" |
    sed "s#/home/linuxbrew/.linuxbrew/bin/brew#$case_bin/brew#g" >"$case_script"
  sh -n "$case_script" || fail "Ubuntu Homebrew $arch bootstrap test script is valid sh"

  case_status=0
  HOME="$case_home" \
    XDG_STATE_HOME="$case_state" \
    TERRAPOD_FIRST_RUN_APPLY=1 \
    LINUX_HOMEBREW_ARCH_LOG="$case_log" \
    LINUX_HOMEBREW_ARCH_PREFIX="$case_dir/prefix" \
    PATH="$case_bin:/usr/bin:/bin" \
    sh "$case_script" >"$case_dir/stdout" 2>"$case_dir/stderr" || case_status=$?

  if [ "$expected_status" = success ]; then
    if [ "$case_status" -ne 0 ]; then
      fail "Ubuntu Homebrew bootstrap accepts $arch"
    fi
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

  printf '%s\n' "$ubuntu_homebrew_bootstrap" |
    sed "s#/home/linuxbrew/.linuxbrew/bin/brew#$case_dir/missing-brew#g" >"$case_script"
  sh -n "$case_script" || fail "Ubuntu Homebrew $case_name space test script is valid sh"

  case_status=0
  HOME="$case_home" \
    XDG_STATE_HOME="$case_state" \
    TERRAPOD_FIRST_RUN_APPLY=1 \
    LINUX_HOMEBREW_SPACE_LOG="$case_log" \
    LINUX_HOMEBREW_AVAILABLE_KB="$available_kb" \
    PATH="$case_bin:/usr/bin:/bin" \
    sh "$case_script" >"$case_dir/stdout" 2>"$case_dir/stderr" || case_status=$?

  if [ "$case_status" -eq 0 ]; then
    fail "Ubuntu Homebrew $case_name space case reaches the intentionally failing installer download"
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

replace_standard_brew_path() {
  input_file="$1"
  output_file="$2"
  replacement="$3"

  sed \
    -e "s#/opt/homebrew/bin/brew#$replacement#g" \
    -e "s#/usr/local/bin/brew#$replacement#g" \
    "$input_file" >"$output_file"
}

macos_bootstrap_with_stub="$tmp_dir/macos-bootstrap-default-with-stub.sh"
replace_standard_brew_path "$macos_bootstrap_script" "$macos_bootstrap_with_stub" "$macos_brew_bin/brew"
macos_bootstrap_script="$macos_bootstrap_with_stub"

macos_core_retry_with_stub="$tmp_dir/macos-core-retry-with-stub.sh"
replace_standard_brew_path "$macos_core_retry_script" "$macos_core_retry_with_stub" "$macos_brew_bin/brew"
macos_core_retry_script="$macos_core_retry_with_stub"

macos_platform_retry_with_stub="$tmp_dir/macos-platform-retry-with-stub.sh"
replace_standard_brew_path "$macos_platform_retry_script" "$macos_platform_retry_with_stub" "$macos_brew_bin/brew"
macos_platform_retry_script="$macos_platform_retry_with_stub"

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
printf '%s\n' "$macos_bootstrap" | sed \
  -e "s#/opt/homebrew/bin/brew#$tmp_dir/missing-opt-homebrew-brew#g" \
  -e "s#/usr/local/bin/brew#$tmp_dir/missing-usr-local-brew#g" \
  >"$homebrew_installer_failure_script"
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

if HOME="$homebrew_installer_failure_home" XDG_STATE_HOME="$homebrew_installer_failure_state" HOMEBREW_INSTALLER_FAILURE_LOG="$homebrew_installer_failure_log" PATH="$homebrew_installer_failure_bin:/usr/bin:/bin" \
  sh "$homebrew_installer_failure_script" >"$tmp_dir/homebrew-installer-failure.out" 2>"$tmp_dir/homebrew-installer-failure.err"; then
  fail "macOS bootstrap fails when the Homebrew installer command fails"
fi

homebrew_installer_failure_marker="$homebrew_installer_failure_state/terrapod/install-warnings/homebrew-core"
if [ ! -f "$homebrew_installer_failure_marker" ]; then
  fail "macOS bootstrap records homebrew-core marker when the Homebrew installer command fails"
fi
pass "macOS bootstrap records homebrew-core marker when the Homebrew installer command fails"

homebrew_installer_failure_marker_text="$(cat "$homebrew_installer_failure_marker")"
assert_contains_text "$homebrew_installer_failure_marker_text" "summary='Homebrew core install needs attention'" "macOS bootstrap Homebrew installer failure marker keeps the expected summary"
assert_contains_text "$homebrew_installer_failure_marker_text" "guidance='Install Homebrew from https://brew.sh, then rerun tpod apply.'" "macOS bootstrap Homebrew installer failure marker keeps recovery guidance"

homebrew_first_run_failure_state="$tmp_dir/homebrew-first-run-failure-state"
homebrew_first_run_failure_home="$tmp_dir/homebrew-first-run-failure-home"
homebrew_first_run_failure_log="$tmp_dir/homebrew-first-run-failure.log"
mkdir -p "$homebrew_first_run_failure_home"

if HOME="$homebrew_first_run_failure_home" XDG_STATE_HOME="$homebrew_first_run_failure_state" HOMEBREW_INSTALLER_FAILURE_LOG="$homebrew_first_run_failure_log" PATH="$homebrew_installer_failure_bin:/usr/bin:/bin" \
  TERRAPOD_FIRST_RUN_APPLY=1 sh "$homebrew_installer_failure_script" >"$tmp_dir/homebrew-first-run-failure.out" 2>"$tmp_dir/homebrew-first-run-failure.err"; then
  fail "first-run macOS bootstrap hard-fails when the Homebrew installer command fails"
fi
pass "first-run macOS bootstrap hard-fails when the Homebrew installer command fails"

homebrew_first_run_failure_marker="$homebrew_first_run_failure_state/terrapod/install-warnings/homebrew-core"
if [ ! -f "$homebrew_first_run_failure_marker" ]; then
  fail "first-run macOS bootstrap records homebrew-core marker when the Homebrew installer command fails"
fi
pass "first-run macOS bootstrap records homebrew-core marker when the Homebrew installer command fails"

homebrew_first_run_download_bin="$tmp_dir/homebrew-first-run-download-bin"
homebrew_first_run_download_state="$tmp_dir/homebrew-first-run-download-state"
homebrew_first_run_download_home="$tmp_dir/homebrew-first-run-download-home"
homebrew_first_run_download_log="$tmp_dir/homebrew-first-run-download.log"
mkdir -p "$homebrew_first_run_download_bin" "$homebrew_first_run_download_home"
write_stub "$homebrew_first_run_download_bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$HOMEBREW_INSTALLER_FAILURE_LOG"' \
  'exit 42'

if HOME="$homebrew_first_run_download_home" XDG_STATE_HOME="$homebrew_first_run_download_state" HOMEBREW_INSTALLER_FAILURE_LOG="$homebrew_first_run_download_log" PATH="$homebrew_first_run_download_bin:/usr/bin:/bin" \
  TERRAPOD_FIRST_RUN_APPLY=1 sh "$homebrew_installer_failure_script" >"$tmp_dir/homebrew-first-run-download.out" 2>"$tmp_dir/homebrew-first-run-download.err"; then
  fail "first-run macOS bootstrap hard-fails when the Homebrew installer download fails"
fi
if [ ! -f "$homebrew_first_run_download_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "first-run macOS bootstrap records homebrew-core marker when the Homebrew installer download fails"
fi
pass "first-run macOS bootstrap hard-fails and records a marker when the Homebrew installer download fails"

homebrew_first_run_not_found_bin="$tmp_dir/homebrew-first-run-not-found-bin"
homebrew_first_run_not_found_state="$tmp_dir/homebrew-first-run-not-found-state"
homebrew_first_run_not_found_home="$tmp_dir/homebrew-first-run-not-found-home"
homebrew_first_run_not_found_log="$tmp_dir/homebrew-first-run-not-found.log"
mkdir -p "$homebrew_first_run_not_found_bin" "$homebrew_first_run_not_found_home"
write_stub "$homebrew_first_run_not_found_bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$HOMEBREW_INSTALLER_FAILURE_LOG"' \
  'output_file=' \
  'while [ "$#" -gt 0 ]; do' \
  '  if [ "$1" = -o ]; then shift; output_file="$1"; fi' \
  '  shift' \
  'done' \
  'printf "%s\n" "exit 0" >"$output_file"'

if HOME="$homebrew_first_run_not_found_home" XDG_STATE_HOME="$homebrew_first_run_not_found_state" HOMEBREW_INSTALLER_FAILURE_LOG="$homebrew_first_run_not_found_log" PATH="$homebrew_first_run_not_found_bin:/usr/bin:/bin" \
  TERRAPOD_FIRST_RUN_APPLY=1 sh "$homebrew_installer_failure_script" >"$tmp_dir/homebrew-first-run-not-found.out" 2>"$tmp_dir/homebrew-first-run-not-found.err"; then
  fail "first-run macOS bootstrap hard-fails when brew is not found after installation"
fi
if [ ! -f "$homebrew_first_run_not_found_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "first-run macOS bootstrap records homebrew-core marker when brew is not found after installation"
fi
pass "first-run macOS bootstrap hard-fails and records a marker when brew is not found after installation"

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

assert_contains_text "$(cat "$tmp_dir/core-detail.out")" "visible brew bundle output:" "core Homebrew failure preserves visible brew output"
core_detail_marker="$core_detail_state/terrapod/install-warnings/homebrew-core"
if [ ! -f "$core_detail_marker" ]; then
  fail "core Homebrew bundle failure records a homebrew-core marker"
fi
pass "core Homebrew bundle failure records a homebrew-core marker"

core_detail_marker_text="$(cat "$core_detail_marker")"
assert_contains_text "$core_detail_marker_text" "category='homebrew-core'" "core marker keeps one stable category"
assert_contains_text "$core_detail_marker_text" "summary='Homebrew core install needs attention'" "core marker keeps stable summary"
assert_contains_text "$core_detail_marker_text" "failed formulae: gum" "core marker guidance includes reliable failed formula names"
assert_contains_text "$core_detail_marker_text" "Homebrew prefix is not writable: $core_detail_prefix" "core marker guidance identifies unwritable shared prefix"
assert_not_contains_text "$core_detail_marker_text" "btop" "core marker excludes successful formula names"
assert_not_contains_text "$core_detail_marker_text" "chown" "core marker avoids broad ownership command guidance"

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
assert_contains_text "$core_fallback_marker_text" "Review Homebrew core bundle output, fix package access, then rerun tpod apply." "core marker falls back to visible-output rerun guidance"
assert_not_contains_text "$core_fallback_marker_text" "failed formulae:" "core fallback marker avoids invented formula detail"
assert_not_contains_text "$core_fallback_marker_text" "failed casks:" "core fallback marker avoids invented cask detail"

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
  sh "$macos_core_retry_script" >"$tmp_dir/core-retry-success.out" 2>"$tmp_dir/core-retry-success.err"; then
  fail "successful core retry succeeds"
fi

if [ -e "$core_retry_success_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "successful core retry clears homebrew-core marker"
fi
pass "successful core retry clears homebrew-core marker"

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
  sh "$macos_core_retry_script" >"$tmp_dir/core-retry-failure.out" 2>"$tmp_dir/core-retry-failure.err"; then
  fail "failed core retry records a replacement marker and exits successfully"
fi

core_retry_failure_marker_text="$(cat "$core_retry_failure_state/terrapod/install-warnings/homebrew-core")"
assert_contains_text "$core_retry_failure_marker_text" "failed formulae: mise" "failed core retry replaces marker with current failed formula detail"
assert_not_contains_text "$core_retry_failure_marker_text" "old core retry warning" "failed core retry replaces stale marker guidance"

platform_retry_state="$tmp_dir/platform-retry-state"
platform_retry_home="$tmp_dir/platform-retry-home"
platform_retry_log="$tmp_dir/platform-retry-brew.log"
platform_retry_bin="$tmp_dir/platform-retry-bin"
mkdir -p "$platform_retry_home" "$platform_retry_bin"
write_brew_bundle_stub "$platform_retry_bin/brew"
HOME="$platform_retry_home" XDG_STATE_HOME="$platform_retry_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-macos-platform "Homebrew macOS platform install needs attention" "old platform retry warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$platform_retry_home" XDG_STATE_HOME="$platform_retry_state" MACOS_BREW_LOG="$platform_retry_log" MACOS_BREW_FAIL_CASKS="font-d2coding" PATH="$platform_retry_bin:/usr/bin:/bin" \
  sh "$macos_platform_retry_script" >"$tmp_dir/platform-retry-failure.out" 2>"$tmp_dir/platform-retry-failure.err"; then
  fail "failed macOS platform retry records a replacement marker and exits successfully"
fi

platform_retry_marker="$platform_retry_state/terrapod/install-warnings/homebrew-macos-platform"
platform_retry_marker_text="$(cat "$platform_retry_marker")"
assert_contains_text "$platform_retry_marker_text" "failed casks: font-d2coding" "failed macOS platform retry records the failed font cask"
assert_not_contains_text "$platform_retry_marker_text" "old platform retry warning" "failed macOS platform retry replaces stale guidance"

if ! HOME="$platform_retry_home" XDG_STATE_HOME="$platform_retry_state" MACOS_BREW_LOG="$platform_retry_log" PATH="$platform_retry_bin:/usr/bin:/bin" \
  sh "$macos_platform_retry_script" >"$tmp_dir/platform-retry-success.out" 2>"$tmp_dir/platform-retry-success.err"; then
  fail "successful macOS platform retry succeeds"
fi
if [ -e "$platform_retry_marker" ]; then
  fail "successful macOS platform retry clears its marker"
fi
pass "successful macOS platform retry clears its marker"
assert_contains_text "$core_retry_failure_marker_text" "updated_at='" "failed core retry replacement marker keeps updated_at"

mise_missing_without_core_home="$tmp_dir/mise-missing-without-core-home"
mise_missing_without_core_state="$tmp_dir/mise-missing-without-core-state"
mkdir -p "$mise_missing_without_core_home"
mise_missing_without_core_status=0
HOME="$mise_missing_without_core_home" XDG_STATE_HOME="$mise_missing_without_core_state" PATH="/usr/bin:/bin" \
  sh "$macos_mise_missing_script" >"$tmp_dir/mise-missing-without-core.out" 2>"$tmp_dir/mise-missing-without-core.err" ||
  mise_missing_without_core_status=$?
if [ "$mise_missing_without_core_status" -ne 0 ]; then
  fail "macOS mise tool installer records a recoverable warning when mise is missing without a homebrew-core marker"
fi
mise_missing_without_core_marker="$mise_missing_without_core_state/terrapod/install-warnings/mise-tools"
if [ ! -f "$mise_missing_without_core_marker" ]; then
  fail "macOS mise tool installer records a mise-tools marker when mise is missing without a homebrew-core marker"
fi
pass "macOS mise tool installer records a recoverable mise-tools warning when mise is missing without a homebrew-core marker"

mise_missing_with_core_home="$tmp_dir/mise-missing-with-core-home"
mise_missing_with_core_state="$tmp_dir/mise-missing-with-core-state"
mkdir -p "$mise_missing_with_core_home"
HOME="$mise_missing_with_core_home" XDG_STATE_HOME="$mise_missing_with_core_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "Install failed formulae, then rerun tpod apply."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

mise_missing_with_core_status=0
HOME="$mise_missing_with_core_home" XDG_STATE_HOME="$mise_missing_with_core_state" PATH="/usr/bin:/bin" \
  sh "$macos_mise_missing_script" >"$tmp_dir/mise-missing-with-core.out" 2>"$tmp_dir/mise-missing-with-core.err" ||
  mise_missing_with_core_status=$?
if [ "$mise_missing_with_core_status" -ne 0 ]; then
  printf '%s\n' "mise missing with core stdout:" >&2
  sed 's/^/  /' "$tmp_dir/mise-missing-with-core.out" >&2
  printf '%s\n' "mise missing with core stderr:" >&2
  sed 's/^/  /' "$tmp_dir/mise-missing-with-core.err" >&2
  fail "macOS mise tool installer exits 0 when missing mise is covered by a homebrew-core marker"
fi
pass "macOS mise tool installer exits 0 when missing mise is covered by a homebrew-core marker"

if [ ! -f "$mise_missing_with_core_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "macOS mise tool installer keeps the existing homebrew-core marker"
fi
if [ ! -f "$mise_missing_with_core_state/terrapod/install-warnings/mise-tools" ]; then
  fail "macOS mise tool installer records its own warning when standard Homebrew mise is missing"
fi
pass "macOS mise tool installer records missing standard Homebrew mise independently"

assert_managed_paths_exclude_prefix \
  "$macos_managed_targets" \
  "Brewfile.macos-desktop-apps" \
  "macOS default does not manage rendered macOS Desktop App Stack Brewfile target"

assert_managed_paths_exclude_prefix \
  "$macos_terminal_apps_managed_targets" \
  "Brewfile.macos-desktop-apps" \
  "terminal-apps group does not manage rendered macOS Desktop App Stack Brewfile target"

assert_not_contains_text "$macos_brewfile" 'cask "ghostty"' "macOS default does not render Ghostty"
assert_not_contains_text "$macos_brewfile" 'cask "cmux"' "macOS default does not render cmux"
assert_not_contains_text "$macos_brewfile" 'cask "hammerspoon"' "macOS default does not render Hammerspoon"
assert_not_contains_text "$macos_brewfile" 'cask "karabiner-elements"' "macOS default does not render Karabiner-Elements"
assert_not_contains_text "$macos_brewfile" 'cask "scroll-reverser"' "macOS default does not render Scroll Reverser"
assert_not_contains_text "$macos_brewfile" 'cask "raycast"' "macOS default does not render Raycast"
assert_not_contains_text "$macos_brewfile" 'cask "1password-cli"' "macOS default does not render 1Password CLI"
assert_not_contains_text "$macos_brewfile" 'cask "istat-menus"' "macOS default does not render iStat Menus"
assert_not_contains_text "$macos_brewfile" 'cask "claude"' "macOS default does not render Claude Desktop"
assert_not_contains_text "$macos_brewfile" 'cask "codex-app"' "macOS default does not render the unified ChatGPT desktop app"
assert_not_contains_text "$macos_brewfile" 'cask "chatgpt"' "macOS default does not render the legacy ChatGPT cask"
assert_not_contains_text "$macos_brewfile" 'cask "codex"' "macOS default does not render Codex CLI as a desktop app"
assert_not_contains_text "$macos_brewfile" 'cask "antigravity"' "macOS default does not render Antigravity 2.0"
assert_not_contains_text "$macos_brewfile" 'cask "antigravity-ide"' "macOS default does not render Antigravity IDE"
assert_not_contains_text "$macos_brewfile" 'cask "stablyai/orca/orca"' "macOS default does not render Orca"

assert_contains_text "$terminal_apps_brewfile" 'cask "ghostty"' "terminal-apps group renders Ghostty"
assert_not_contains_text "$terminal_apps_brewfile" 'cask "cmux"' "terminal-apps group does not render cmux"
assert_not_contains_text "$terminal_apps_brewfile" 'cask "hammerspoon"' "terminal-apps group does not render automation casks"

assert_contains_text "$automation_apps_brewfile" 'cask "hammerspoon"' "automation group renders Hammerspoon"
assert_contains_text "$automation_apps_brewfile" 'cask "karabiner-elements"' "automation group renders Karabiner-Elements"
assert_contains_text "$automation_apps_brewfile" 'cask "scroll-reverser"' "automation group renders Scroll Reverser"

assert_contains_text "$launcher_apps_brewfile" 'cask "raycast"' "launcher group renders Raycast"
assert_contains_text "$launcher_apps_brewfile" 'cask "1password-cli"' "launcher group renders 1Password CLI"

assert_contains_text "$monitoring_apps_brewfile" 'cask "istat-menus"' "monitoring group renders iStat Menus"

assert_contains_text "$development_apps_brewfile" 'cask "zed"' "development-apps group renders Zed"
assert_contains_text "$development_apps_brewfile" 'cask "stablyai/orca/orca", trusted: true' "development-apps group trusts only Orca's fully-qualified vendor cask"
for removed_cask in claude codex-app chatgpt antigravity antigravity-ide; do
  assert_not_contains_text "$development_apps_brewfile" "cask \"$removed_cask\"" "development-apps group excludes removed desktop cask: $removed_cask"
done
development_apps_casks="$(
  printf '%s\n' "$development_apps_brewfile" |
    awk '/^[[:space:]]*cask[[:space:]]+"/ { print }'
)"
expected_development_apps_casks='cask "zed"
cask "stablyai/orca/orca", trusted: true'
assert_text_equals \
  "$development_apps_casks" \
  "$expected_development_apps_casks" \
  "development-apps group renders exactly the expected casks"

development_apps_bootstrap_script="$tmp_dir/macos-development-apps-bootstrap.sh"
printf '%s\n' "$macos_development_apps_bootstrap" | sed \
  -e "s#/opt/homebrew/bin/brew#$tmp_dir/development-apps-failure-bin/brew#g" \
  -e "s#/usr/local/bin/brew#$tmp_dir/development-apps-failure-bin/brew#g" \
  >"$development_apps_bootstrap_script"
sh -n "$development_apps_bootstrap_script" || fail "development-apps bootstrap script should be valid sh"
pass "development-apps bootstrap script is valid sh"

development_apps_failure_bin="$tmp_dir/development-apps-failure-bin"
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
assert_contains_text "$development_apps_failure_marker_text" "failed casks: stablyai/orca/orca" "Orca failure attribution preserves its fully-qualified cask source"
assert_contains_text "$development_apps_failure_marker_text" "App Groups: development-apps" "Orca failure attribution identifies the development-apps group"

terminal_launcher_bootstrap_script="$tmp_dir/macos-terminal-launcher-bootstrap.sh"
printf '%s\n' "$macos_terminal_launcher_apps_bootstrap" | sed \
  -e "s#/opt/homebrew/bin/brew#$tmp_dir/terminal-launcher-bin/brew#g" \
  -e "s#/usr/local/bin/brew#$tmp_dir/terminal-launcher-bin/brew#g" \
  >"$terminal_launcher_bootstrap_script"
sh -n "$terminal_launcher_bootstrap_script" || fail "terminal and launcher bootstrap script should be valid sh"
pass "terminal and launcher bootstrap script is valid sh"

terminal_launcher_bin="$tmp_dir/terminal-launcher-bin"
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
assert_contains_text "$terminal_launcher_marker_text" "category='homebrew-desktop-apps'" "desktop app marker keeps one stable category"
assert_contains_text "$terminal_launcher_marker_text" "summary='Homebrew desktop app install needs attention'" "desktop app marker keeps stable summary"
assert_contains_text "$terminal_launcher_marker_text" "failed casks: ghostty, raycast" "desktop app marker guidance includes only casks whose single-cask bundle failed"
assert_contains_text "$terminal_launcher_marker_text" "App Groups: terminal-apps, launcher" "desktop app marker guidance includes enabled App Groups"
assert_not_contains_text "$terminal_launcher_marker_text" "1password-cli" "desktop app marker excludes casks whose single-cask bundle succeeded"

core_then_desktop_bin="$tmp_dir/core-then-desktop-bin"
core_then_desktop_state="$tmp_dir/core-then-desktop-state"
core_then_desktop_home="$tmp_dir/core-then-desktop-home"
core_then_desktop_log="$tmp_dir/core-then-desktop-brew.log"
mkdir -p "$core_then_desktop_bin" "$core_then_desktop_home"
write_brew_bundle_stub "$core_then_desktop_bin/brew"

if ! HOME="$core_then_desktop_home" XDG_STATE_HOME="$core_then_desktop_state" MACOS_BREW_LOG="$core_then_desktop_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="font-d2coding ghostty raycast" PATH="$core_then_desktop_bin:/usr/bin:/bin" \
  sh "$terminal_launcher_bootstrap_script" >"$tmp_dir/core-then-desktop.out" 2>"$tmp_dir/core-then-desktop.err"; then
  printf '%s\n' "core then desktop stdout:" >&2
  sed 's/^/  /' "$tmp_dir/core-then-desktop.out" >&2
  printf '%s\n' "core then desktop stderr:" >&2
  sed 's/^/  /' "$tmp_dir/core-then-desktop.err" >&2
  fail "macOS bootstrap records platform and desktop app warnings in one App Groups run"
fi

core_then_desktop_core_marker="$core_then_desktop_state/terrapod/install-warnings/homebrew-macos-platform"
core_then_desktop_desktop_marker="$core_then_desktop_state/terrapod/install-warnings/homebrew-desktop-apps"
if [ ! -f "$core_then_desktop_core_marker" ]; then
  fail "macOS bootstrap keeps homebrew-macos-platform marker when desktop App Groups also need attention"
fi
if [ ! -f "$core_then_desktop_desktop_marker" ]; then
  fail "macOS bootstrap continues to desktop App Groups after recording a homebrew-core marker"
fi
pass "macOS bootstrap records platform and desktop app warnings in one App Groups run"

core_then_desktop_core_text="$(cat "$core_then_desktop_core_marker")"
core_then_desktop_desktop_text="$(cat "$core_then_desktop_desktop_marker")"
assert_contains_text "$core_then_desktop_core_text" "failed casks: font-d2coding" "combined bootstrap platform marker keeps failed font detail"
assert_contains_text "$core_then_desktop_desktop_text" "failed casks: ghostty, raycast" "combined bootstrap desktop marker keeps failed cask detail"

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

desktop_retry_marker_failure_bin="$tmp_dir/desktop-retry-marker-failure-bin"
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

if HOME="$desktop_retry_marker_failure_home" XDG_STATE_HOME="$desktop_retry_marker_failure_state" DESKTOP_RETRY_MARKER_FAILURE_DIR="$desktop_retry_marker_failure_dir" MACOS_BREW_LOG="$desktop_retry_marker_failure_log" PATH="$desktop_retry_marker_failure_bin:/usr/bin:/bin" \
  sh "$macos_terminal_apps_desktop_retry_script" >"$tmp_dir/desktop-retry-marker-failure.out" 2>"$tmp_dir/desktop-retry-marker-failure.err"; then
  fail "macOS desktop retry failure blocks when the warning marker cannot be recorded"
fi
pass "macOS desktop retry failure blocks when the warning marker cannot be recorded"

bulk_only_bootstrap_script="$tmp_dir/macos-bulk-only-bootstrap.sh"
printf '%s\n' "$macos_terminal_launcher_apps_bootstrap" | sed \
  -e "s#/opt/homebrew/bin/brew#$tmp_dir/bulk-only-bin/brew#g" \
  -e "s#/usr/local/bin/brew#$tmp_dir/bulk-only-bin/brew#g" \
  >"$bulk_only_bootstrap_script"
sh -n "$bulk_only_bootstrap_script" || fail "bulk-only desktop bootstrap script should be valid sh"
pass "bulk-only desktop bootstrap script is valid sh"

bulk_only_bin="$tmp_dir/bulk-only-bin"
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
assert_contains_text "$bulk_only_marker_text" "Review Homebrew desktop app bundle output" "desktop app marker falls back when bulk fails but single-cask attribution succeeds"
assert_not_contains_text "$bulk_only_marker_text" "failed casks:" "desktop app bulk-only fallback avoids invented cask detail"
assert_not_contains_text "$bulk_only_marker_text" "App Groups:" "desktop app bulk-only fallback avoids invented App Group detail"

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
  sed "s#$tmp_dir/terminal-launcher-bin/brew#$tmp_dir/fallback-bin/brew#g" >"$fallback_bootstrap_script"
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
assert_contains_text "$fallback_marker_text" "Review Homebrew desktop app bundle output" "desktop app marker falls back to bulk bundle guidance when casks are not reliable"
assert_not_contains_text "$fallback_marker_text" "failed casks:" "desktop app fallback marker avoids invented cask detail"
assert_not_contains_text "$fallback_marker_text" "App Groups:" "desktop app fallback marker avoids invented App Group detail"

terminal_only_bootstrap_script="$tmp_dir/macos-terminal-only-bootstrap.sh"
printf '%s\n' "$macos_terminal_apps_bootstrap" | sed \
  -e "s#/opt/homebrew/bin/brew#$tmp_dir/terminal-only-bin/brew#g" \
  -e "s#/usr/local/bin/brew#$tmp_dir/terminal-only-bin/brew#g" \
  >"$terminal_only_bootstrap_script"
sh -n "$terminal_only_bootstrap_script" || fail "terminal-only bootstrap script should be valid sh"
pass "terminal-only bootstrap script is valid sh"

terminal_only_bin="$tmp_dir/terminal-only-bin"
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
assert_contains_text "$terminal_only_marker_text" "failed casks: ghostty" "enabled terminal-apps failure remains in desktop app marker"
assert_contains_text "$terminal_only_marker_text" "App Groups: terminal-apps" "enabled terminal-apps group remains in desktop app marker"
assert_not_contains_text "$terminal_only_marker_text" "raycast" "disabled launcher cask is removed from desktop app marker"
assert_not_contains_text "$terminal_only_marker_text" "1password-cli" "disabled launcher CLI cask is removed from desktop app marker"
assert_not_contains_text "$terminal_only_marker_text" "launcher" "disabled launcher group is removed from desktop app marker"

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

assert_contains_text \
  "$macos_terminal_apps_bootstrap" \
  "terrapod-macos-desktop-apps" \
  "terminal-apps group renders macOS Desktop App Stack Brewfile"

assert_contains_text \
  "$macos_terminal_apps_bootstrap" \
  'run_desktop_app_bundle "$desktop_brewfile"' \
  "terminal-apps group runs macOS Desktop App Stack installer"

assert_contains_text \
  "$macos_development_apps_bootstrap" \
  "terrapod-macos-desktop-apps" \
  "development-apps group renders macOS Desktop App Stack Brewfile"

assert_contains_text \
  "$macos_development_apps_bootstrap" \
  'run_desktop_app_bundle "$desktop_brewfile"' \
  "development-apps group runs macOS Desktop App Stack installer"

assert_not_contains_text \
  "$macos_development_workspace_bootstrap" \
  "Brewfile.macos-desktop-apps" \
  "enableDevelopmentWorkspace does not imply macOS Desktop App Stack Brewfile"

assert_contains_text \
  "$macos_karabiner_opener" \
  "macOS Desktop App Stack enabled: false" \
  "Karabiner opener tracks disabled macOS Desktop App Stack state"

assert_not_contains_text \
  "$macos_karabiner_opener" \
  "macOS Desktop App Stack Brewfile checksum" \
  "Karabiner opener default skips macOS Desktop App Stack Brewfile checksum"

assert_contains_text \
  "$macos_automation_apps_karabiner_opener" \
  "macOS Desktop App Stack enabled: true" \
  "Karabiner opener tracks enabled macOS Desktop App Stack state"

assert_contains_text \
  "$macos_automation_apps_karabiner_opener" \
  "macOS Desktop App Stack Brewfile checksum" \
  "Karabiner opener tracks macOS Desktop App Stack Brewfile changes"

assert_contains_text \
  "$macos_development_apps_karabiner_opener" \
  "macOS Desktop App Stack enabled: true" \
  "Karabiner opener tracks enabled development-apps Desktop App Stack state"

assert_contains_text \
  "$macos_development_apps_karabiner_opener" \
  "macOS Desktop App Stack Brewfile checksum" \
  "Karabiner opener tracks development-apps Desktop App Stack Brewfile changes"

assert_texts_differ \
  "$macos_terminal_apps_karabiner_opener" \
  "$macos_automation_apps_karabiner_opener" \
  "Karabiner opener tracks different macOS Desktop App Group combinations"

for cask in \
  font-jetbrains-mono-nerd-font \
  font-d2coding
do
  if ! grep -Fx "cask \"$cask\"" "$repo_root/Brewfile.macos" >/dev/null; then
    fail "macOS platform Brewfile contains expected terminal font cask: $cask"
  fi
done

pass "macOS platform Brewfile contains expected terminal font casks"

if ! grep -Fx 'brew "gum"' "$repo_root/Brewfile" >/dev/null; then
  fail "core Brewfile declares gum as the setup UI dependency"
fi

pass "core Brewfile declares gum as the setup UI dependency"

if grep -E '^[[:space:]]*cask[[:space:]]+"' "$repo_root/Brewfile" >/dev/null; then
  fail "cross-profile core Brewfile excludes macOS-only casks"
fi

pass "cross-profile core Brewfile excludes macOS-only casks"

for app_config in \
  ".config/ghostty/config" \
  ".config/karabiner/karabiner.json" \
  ".hammerspoon/init.lua"
do
  if ! printf '%s\n' "$macos_managed_targets" | grep -Fx "$app_config" >/dev/null; then
    fail "macOS default manages user-scoped app config: $app_config"
  fi

  if ! printf '%s\n' "$macos_terminal_apps_managed_targets" | grep -Fx "$app_config" >/dev/null; then
    fail "terminal-apps group manages user-scoped app config: $app_config"
  fi
done

pass "user-scoped macOS app config remains managed regardless of app group selection"

cmux_fixture_source="$tmp_dir/cmux-fixture-source"
mkdir -p "$cmux_fixture_source/dot_config/cmux"
cp "$repo_root/.chezmoiignore" "$cmux_fixture_source/.chezmoiignore"
printf '{}\n' >"$cmux_fixture_source/dot_config/cmux/private_settings.json"

cmux_fixture_macos_managed_targets="$(managed_target_paths_from_source "$macos_data" "$cmux_fixture_source")"
cmux_fixture_terminal_apps_managed_targets="$(managed_target_paths_from_source "$macos_terminal_apps_data" "$cmux_fixture_source")"

assert_managed_paths_exclude_prefix \
  "$cmux_fixture_macos_managed_targets" \
  ".config/cmux" \
  "macOS default ignore rules exclude future cmux settings sources"

assert_managed_paths_exclude_prefix \
  "$cmux_fixture_terminal_apps_managed_targets" \
  ".config/cmux" \
  "terminal-apps ignore rules exclude future cmux settings sources"

assert_managed_paths_exclude_prefix \
  "$macos_managed_targets" \
  ".config/cmux" \
  "macOS default does not manage cmux settings"

assert_managed_paths_exclude_prefix \
  "$macos_terminal_apps_managed_targets" \
  ".config/cmux" \
  "terminal-apps group does not manage cmux settings"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  "dot_config/nvim" \
  "Ubuntu VPS ignores Optional Editor Stack entries by default"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  "dot_config/zellij/layouts/dev.kdl" \
  "Ubuntu VPS ignores Optional Development Workspace layout by default"

assert_managed_paths_include_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl" \
  "Ubuntu VPS includes Optional AI Tool Stack warning cleanup by default"

assert_managed_paths_include_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_after_21-retry-mise-tools.sh.tmpl" \
  "Ubuntu VPS includes marker-gated mise tool retry hook"

assert_managed_paths_include_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_before_31-retry-shell-integrations.sh.tmpl" \
  "Ubuntu VPS includes marker-gated shell integration retry hook"

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  "dot_config/nvim" \
  "macOS ignores Optional Editor Stack entries by default"

assert_managed_paths_include_prefix \
  "$macos_managed" \
  ".chezmoiscripts/run_after_21-retry-mise-tools.sh.tmpl" \
  "macOS includes marker-gated mise tool retry hook"

assert_managed_paths_include_prefix \
  "$macos_managed" \
  ".chezmoiscripts/run_before_31-retry-shell-integrations.sh.tmpl" \
  "macOS includes marker-gated shell integration retry hook"

if [ -e "$repo_root/dot_config/zsh/path.d/antigravity.zsh.tmpl" ]; then
  fail "legacy Antigravity app-bundle PATH snippet is no longer managed"
fi

pass "legacy Antigravity app-bundle PATH snippet is no longer managed"

assert_managed_paths_exclude_prefix \
  "$macos_development_apps_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "macOS development-apps group does not restore legacy Antigravity PATH snippet"

assert_managed_paths_include_prefix \
  "$editor_stack_managed" \
  "dot_config/nvim" \
  "enableEditorStack includes Optional Editor Stack entries"

assert_managed_paths_include_prefix \
  "$development_workspace_managed" \
  "dot_config/nvim" \
  "enableDevelopmentWorkspace includes Optional Editor Stack entries"

assert_managed_paths_exclude_prefix \
  "$ai_cli_tools_managed" \
  "dot_config/nvim" \
  "enableAiCliTools alone ignores Optional Editor Stack entries"

assert_managed_paths_exclude_prefix \
  "$ai_cli_tools_managed" \
  "dot_config/zellij/layouts/dev.kdl" \
  "enableAiCliTools alone ignores Optional Development Workspace layout"

assert_managed_paths_include_prefix \
  "$development_workspace_managed" \
  "dot_config/zellij/layouts/dev.kdl" \
  "enableDevelopmentWorkspace includes Optional Development Workspace layout"

assert_managed_paths_include_prefix \
  "$ai_cli_tools_managed" \
  ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl" \
  "enableAiCliTools includes Optional AI Tool Stack installer"

assert_managed_paths_include_prefix \
  "$development_workspace_managed" \
  ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl" \
  "enableDevelopmentWorkspace includes Optional AI Tool Stack installer"

ubuntu_mise_config="$(render_template "$ubuntu_data" "dot_config/mise/config.toml.tmpl")"

if printf '%s\n' "$ubuntu_mise_config" | grep -F '"aqua:neovim/neovim" = "latest"' >/dev/null; then
  fail "Ubuntu VPS removes duplicate mise-managed Neovim"
fi

pass "Ubuntu VPS removes duplicate mise-managed Neovim"

if printf '%s\n' "$ubuntu_mise_config" | grep -F '"aqua:cli/cli" = "latest"' >/dev/null; then
  fail "Ubuntu VPS removes duplicate mise-managed GitHub CLI"
fi

pass "Ubuntu VPS removes duplicate mise-managed GitHub CLI"

for formula in neovim gh; do
  if ! grep -Fx "brew \"$formula\"" "$repo_root/Brewfile" >/dev/null; then
    fail "cross-profile Brewfile declares migrated formula: $formula"
  fi
done
pass "cross-profile Brewfile declares migrated Neovim and GitHub CLI"

mise_tools_installer="$(render_template "$ubuntu_data" ".chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl")"
mise_tools_retry="$(render_template "$ubuntu_data" ".chezmoiscripts/run_after_21-retry-mise-tools.sh.tmpl")"

assert_contains_text "$mise_tools_installer" 'mise_bin="$(standard_mise_path || true)"' "mise installer resolves mise from a standard Homebrew prefix"
assert_contains_text "$mise_tools_installer" '"$mise_bin" install --yes -C "$HOME"' "Ubuntu runtime install invokes the resolved Homebrew mise"
assert_not_contains_text "$mise_tools_installer" '/usr/bin/mise' "Ubuntu runtime install never falls back to APT mise"

if ! printf '%s\n' "$mise_tools_installer" |
  grep -E '^# mise-config-sha256=[0-9a-f]{64}$' >/dev/null; then
  fail "mise tool installer tracks rendered mise config changes"
fi

pass "mise tool installer tracks rendered mise config changes"

mise_tools_installer_script="$tmp_dir/mise-tools-installer.sh"
printf '%s\n' "$mise_tools_installer" |
  sed "s#/home/linuxbrew/.linuxbrew/bin/mise#$tmp_dir/mise-tools-bin/mise#g" \
  >"$mise_tools_installer_script"
sh -n "$mise_tools_installer_script" || fail "mise tool installer script should be valid sh"
pass "mise tool installer script should be valid sh"

mise_tools_retry_script="$tmp_dir/mise-tools-retry.sh"
printf '%s\n' "$mise_tools_retry" |
  sed "s#/home/linuxbrew/.linuxbrew/bin/mise#$tmp_dir/mise-tools-bin/mise#g" \
  >"$mise_tools_retry_script"
sh -n "$mise_tools_retry_script" || fail "mise tool retry script should be valid sh"
pass "mise tool retry script should be valid sh"

mise_tools_bin="$tmp_dir/mise-tools-bin"
mise_tools_state="$tmp_dir/mise-tools-state"
mise_tools_home="$tmp_dir/mise-tools-home"
mise_tools_log="$tmp_dir/mise-tools.log"
mkdir -p "$mise_tools_bin" "$mise_tools_home"
write_stub "$mise_tools_bin/mise" \
  'printf "%s\n" "/home/linuxbrew/.linuxbrew/bin/mise args:$*" >>"$MISE_TOOLS_LOG"' \
  'case "$1" in' \
  '  install)' \
  '    exit "${MISE_TOOLS_INSTALL_STATUS:-0}"' \
  '    ;;' \
  '  exec)' \
  '    shift' \
  '    while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do' \
  '      shift' \
  '    done' \
  '    if [ "$#" -gt 0 ]; then' \
  '      shift' \
  '    fi' \
  '    case "$*" in' \
  '      "sh -c command -v corepack")' \
  '        if [ "${MISE_TOOLS_COREPACK_PRESENT:-1}" = "1" ]; then' \
  '          exit 0' \
  '        fi' \
  '        exit 1' \
  '        ;;' \
  '      "corepack enable")' \
  '        exit "${MISE_TOOLS_COREPACK_STATUS:-0}"' \
  '        ;;' \
  '    esac' \
  '    ;;' \
  'esac' \
  'exit 0'

mise_tools_install_failure_status=0
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  MISE_TOOLS_INSTALL_STATUS=17 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script" || mise_tools_install_failure_status=$?
if [ "$mise_tools_install_failure_status" -ne 0 ]; then
  fail "mise tool installer should continue after recording a mise install warning"
fi
mise_tools_marker="$mise_tools_state/terrapod/install-warnings/mise-tools"
if [ ! -f "$mise_tools_marker" ]; then
  fail "mise tool installer should write a mise-tools warning marker after mise install failure"
fi
mise_tools_marker_text="$(cat "$mise_tools_marker")"
assert_contains_text "$mise_tools_marker_text" "summary='mise tool install needs attention'" "mise install failure marker keeps the expected summary"
assert_contains_text "$mise_tools_marker_text" "Failed step(s): mise install" "mise install failure marker records failed mise install step"
assert_contains_text "$mise_tools_marker_text" "GITHUB_TOKEN" "mise install failure marker suggests GitHub token recovery"
assert_contains_text "$mise_tools_marker_text" "gh auth login" "mise install failure marker suggests GitHub auth recovery"
mise_tools_log_text="$(cat "$mise_tools_log")"
assert_contains_text "$mise_tools_log_text" "/home/linuxbrew/.linuxbrew/bin/mise args:install --yes -C $mise_tools_home" "Ubuntu runtime install uses Linuxbrew mise"
assert_not_contains_text "$mise_tools_log_text" "/usr/bin/mise" "Ubuntu runtime install never falls back to APT mise"
assert_contains_text "$mise_tools_log_text" "mise args:exec --yes -C $mise_tools_home -- sh -c command -v corepack" "mise install failure still checks corepack availability"
assert_contains_text "$mise_tools_log_text" "mise args:exec --yes -C $mise_tools_home -- corepack enable" "mise install failure still attempts corepack enable"

HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script"
if [ -e "$mise_tools_marker" ]; then
  fail "mise tool installer should clear stale mise-tools marker after a successful rerun"
fi
pass "mise tool installer clears stale mise-tools marker after successful rerun"

mkdir -p "$mise_tools_state/terrapod/install-warnings"
printf '%s\n' \
  "category='mise-tools'" \
  "summary='mise tool install needs attention'" \
  "guidance='stale guidance'" \
  "updated_at='2026-01-01T00:00:00Z'" \
  >"$mise_tools_marker"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  MISE_TOOLS_COREPACK_STATUS=23 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script"
mise_tools_marker_text="$(cat "$mise_tools_marker")"
assert_contains_text "$mise_tools_marker_text" "summary='mise tool install needs attention'" "mise tool installer replacement marker keeps the expected summary"
assert_contains_text "$mise_tools_marker_text" "Failed step(s): corepack enable" "mise tool installer replaces stale marker with corepack enable failure"
assert_not_contains_text "$mise_tools_marker_text" "stale guidance" "mise tool installer replaces stale marker guidance"
assert_contains_text "$mise_tools_marker_text" "updated_at='" "mise tool installer replacement marker includes update timestamp"

: >"$mise_tools_log"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_retry_script"
if [ -e "$mise_tools_marker" ]; then
  fail "mise tool retry should clear stale mise-tools marker after a successful retry"
fi
pass "mise tool retry clears stale mise-tools marker after a successful retry"
mise_tools_retry_log_text="$(cat "$mise_tools_log")"
assert_contains_text "$mise_tools_retry_log_text" "mise args:install --yes -C $mise_tools_home" "mise tool retry attempts mise install when marker exists"

: >"$mise_tools_log"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  MISE_TOOLS_INSTALL_STATUS=17 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_retry_script"
if [ -s "$mise_tools_log" ]; then
  fail "mise tool retry should be a no-op when no marker exists"
fi
pass "mise tool retry is a no-op when no marker exists"

HOME="$mise_tools_home" XDG_STATE_HOME="$mise_tools_state" sh -c \
  '. "$1"; terrapod_install_warning_write mise-tools "mise tool install needs attention" "Previous mise warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"
: >"$mise_tools_log"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  MISE_TOOLS_INSTALL_STATUS=17 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_retry_script"
if [ ! -f "$mise_tools_marker" ]; then
  fail "mise tool retry should keep a warning marker when retry still fails"
fi
pass "mise tool retry keeps a warning marker when retry still fails"
mise_tools_marker_text="$(cat "$mise_tools_marker")"
assert_contains_text "$mise_tools_marker_text" "Failed step(s): mise install" "mise tool retry replacement marker records failed mise install step"

: >"$mise_tools_log"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_retry_script"
if [ -e "$mise_tools_marker" ]; then
  fail "mise tool retry should clear warning marker after recovery"
fi
pass "mise tool retry clears warning marker after recovery"

ai_cli_tools_installer="$(render_template "$ai_cli_tools_data" ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl")"
macos_ai_cli_tools_installer="$(render_template "$macos_ai_cli_tools_data" ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl")"
development_workspace_ai_installer="$(render_template "$development_workspace_data" ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl")"
disabled_ai_cli_tools_cleanup="$(render_template "$ubuntu_data" ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl")"
ai_cli_tools_brewfile="$(render_template "$ai_cli_tools_data" "Brewfile.ai-cli-tools.tmpl")"
development_workspace_ai_brewfile="$(render_template "$development_workspace_data" "Brewfile.ai-cli-tools.tmpl")"
disabled_ai_cli_tools_brewfile="$(render_template "$ubuntu_data" "Brewfile.ai-cli-tools.tmpl")"

for rendered_brewfile in "$ai_cli_tools_brewfile" "$development_workspace_ai_brewfile"; do
  assert_contains_text "$rendered_brewfile" 'cask "antigravity-cli"' "Optional AI Tool Stack declares Antigravity CLI cask"
  assert_contains_text "$rendered_brewfile" 'cask "claude-code"' "Optional AI Tool Stack declares Claude Code cask"
  assert_contains_text "$rendered_brewfile" 'cask "codex"' "Optional AI Tool Stack declares Codex CLI cask"
done
assert_text_equals "$disabled_ai_cli_tools_brewfile" "" "disabled Optional AI Tool Stack renders no Homebrew casks"

assert_contains_text "$disabled_ai_cli_tools_cleanup" "AI_CLI_WARNING_CATEGORY=optional-ai-cli-tools" "disabled Optional AI Tool Stack renders optional AI CLI warning category"
assert_contains_text "$disabled_ai_cli_tools_cleanup" 'clear_install_warning "$AI_CLI_WARNING_CATEGORY"' "disabled Optional AI Tool Stack renders stale marker cleanup"
assert_not_contains_text "$disabled_ai_cli_tools_cleanup" "raw.githubusercontent.com/Homebrew/install" "disabled Optional AI Tool Stack cleanup does not render Homebrew installer URL"

disabled_ai_cli_tools_cleanup_script="$tmp_dir/disabled-ai-cli-tools-cleanup.sh"
printf '%s\n' "$disabled_ai_cli_tools_cleanup" >"$disabled_ai_cli_tools_cleanup_script"
sh -n "$disabled_ai_cli_tools_cleanup_script" || fail "disabled Optional AI Tool Stack cleanup script should be valid sh"
pass "disabled Optional AI Tool Stack cleanup script is valid sh"

ai_marker_state="$tmp_dir/ai-marker-state"
ai_marker_home="$tmp_dir/ai-marker-home"
mkdir -p "$ai_marker_home"
HOME="$ai_marker_home" XDG_STATE_HOME="$ai_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write optional-ai-cli-tools "Optional AI CLI tool install needs attention" "Rerun tpod apply after network access is restored."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"
HOME="$ai_marker_home" XDG_STATE_HOME="$ai_marker_state" sh "$disabled_ai_cli_tools_cleanup_script"
if [ -e "$ai_marker_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "disabled Optional AI Tool Stack cleanup clears stale optional-ai-cli-tools marker"
fi
pass "disabled Optional AI Tool Stack cleanup clears stale optional AI CLI markers"

ai_cli_tools_installer_script="$tmp_dir/ai-cli-tools-installer.sh"
macos_ai_cli_tools_installer_script="$tmp_dir/macos-ai-cli-tools-installer.sh"
printf '%s\n' "$ai_cli_tools_installer" |
  sed \
    -e "s#/opt/homebrew/bin/brew#$tmp_dir/missing-opt-homebrew-brew#g" \
    -e "s#/usr/local/bin/brew#$tmp_dir/missing-usr-local-brew#g" \
    -e "s#/home/linuxbrew/.linuxbrew/bin/brew#$tmp_dir/missing-linuxbrew-brew#g" \
    >"$ai_cli_tools_installer_script"
printf '%s\n' "$macos_ai_cli_tools_installer" >"$macos_ai_cli_tools_installer_script"
sh -n "$ai_cli_tools_installer_script" || fail "enabled Optional AI Tool Stack installer script should be valid sh"
sh -n "$macos_ai_cli_tools_installer_script" || fail "macOS Optional AI Tool Stack installer script should be valid sh"
pass "enabled Optional AI Tool Stack installer scripts are valid sh"

write_ai_brew_stub() {
  path="$1"
  write_stub "$path" \
    'printf "%s\n" "brew args:$*" >>"$AI_BREW_LOG"' \
    'case "$1" in' \
    '  shellenv) printf "export PATH=\"%s:$PATH\"\n" "$AI_BREW_BIN" ;;' \
    '  bundle)' \
    '    bundle_file=' \
    '    for arg do case "$arg" in --file=*) bundle_file="$(printf "%s" "$arg" | cut -d= -f2-)" ;; esac; done' \
    '    [ -n "$bundle_file" ] || exit 64' \
    '    grep -Fx "cask \"antigravity-cli\"" "$bundle_file" >/dev/null || exit 65' \
    '    grep -Fx "cask \"claude-code\"" "$bundle_file" >/dev/null || exit 66' \
    '    grep -Fx "cask \"codex\"" "$bundle_file" >/dev/null || exit 67' \
    '    [ "$AI_BREW_FAIL" = "0" ] || exit 42' \
    '    ;;' \
    '  *) exit 64 ;;' \
    'esac'
}

macos_ai_brew_bin="$tmp_dir/macos-ai-brew-bin"
macos_ai_brew_home="$tmp_dir/macos-ai-brew-home"
macos_ai_brew_state="$tmp_dir/macos-ai-brew-state"
macos_ai_brew_log="$tmp_dir/macos-ai-brew.log"
mkdir -p "$macos_ai_brew_bin" "$macos_ai_brew_home"
write_ai_brew_stub "$macos_ai_brew_bin/brew"
HOME="$macos_ai_brew_home" XDG_STATE_HOME="$macos_ai_brew_state" \
  AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$macos_ai_brew_log" AI_BREW_FAIL=0 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script"
macos_ai_brew_log_text="$(cat "$macos_ai_brew_log")"
assert_contains_text "$macos_ai_brew_log_text" "brew args:shellenv" "macOS Optional AI Tool Stack loads Homebrew shellenv"
assert_contains_text "$macos_ai_brew_log_text" "brew args:bundle --no-upgrade --file=" "macOS Optional AI Tool Stack installs the common no-upgrade bundle"

for vendor_url in \
  "https://antigravity.google/cli/install.sh" \
  "https://claude.ai/install.sh" \
  "https://chatgpt.com/codex/install.sh"
do
  assert_not_contains_text "$ai_cli_tools_installer" "$vendor_url" "Optional AI Tool Stack no longer renders vendor installer URL: $vendor_url"
done

linux_ai_brew_bin="$tmp_dir/linux-ai-brew-bin"
linux_ai_brew_home="$tmp_dir/linux-ai-brew-home"
linux_ai_brew_state="$tmp_dir/linux-ai-brew-state"
linux_ai_brew_log="$tmp_dir/linux-ai-brew.log"
linux_ai_curl_log="$tmp_dir/linux-ai-curl.log"
linux_ai_brew_template="$tmp_dir/linux-ai-brew-template"
mkdir -p "$linux_ai_brew_bin" "$linux_ai_brew_home"
write_ai_brew_stub "$linux_ai_brew_template"
write_stub "$linux_ai_brew_bin/uname" 'printf "%s\n" Linux'
write_stub "$linux_ai_brew_bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$AI_CURL_LOG"' \
  'output=' \
  'while [ "$#" -gt 0 ]; do' \
  '  if [ "$1" = "-o" ]; then shift; output="$1"; fi' \
  '  shift' \
  'done' \
  '[ -n "$output" ] || exit 2' \
  'printf "%s\n" "#!/bin/sh" "cp \"$AI_BREW_TEMPLATE\" \"$AI_BREW_BIN/brew\"" "chmod +x \"$AI_BREW_BIN/brew\"" >"$output"'
HOME="$linux_ai_brew_home" XDG_STATE_HOME="$linux_ai_brew_state" \
  AI_BREW_BIN="$linux_ai_brew_bin" AI_BREW_LOG="$linux_ai_brew_log" AI_BREW_FAIL=0 \
  AI_BREW_TEMPLATE="$linux_ai_brew_template" AI_CURL_LOG="$linux_ai_curl_log" \
  PATH="$linux_ai_brew_bin:/usr/bin:/bin" sh "$ai_cli_tools_installer_script"
assert_contains_text "$(cat "$linux_ai_curl_log")" "https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh" "Ubuntu Optional AI Tool Stack downloads only the official Homebrew installer"
assert_contains_text "$(cat "$linux_ai_brew_log")" "brew args:bundle --no-upgrade --file=" "Ubuntu Optional AI Tool Stack installs the common no-upgrade bundle"

ai_cli_failure_state="$tmp_dir/ai-cli-failure-state"
ai_cli_failure_home="$tmp_dir/ai-cli-failure-home"
ai_cli_failure_log="$tmp_dir/ai-cli-failure.log"
mkdir -p "$ai_cli_failure_home"
ai_cli_failure_status=0
HOME="$ai_cli_failure_home" XDG_STATE_HOME="$ai_cli_failure_state" \
  AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$ai_cli_failure_log" AI_BREW_FAIL=1 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  ai_cli_failure_status=$?
if [ "$ai_cli_failure_status" -eq 0 ]; then
  fail "routine Optional AI Tool Stack bundle failure exits non-zero after recording a warning"
fi
ai_cli_failure_marker="$ai_cli_failure_state/terrapod/install-warnings/optional-ai-cli-tools"
if [ ! -f "$ai_cli_failure_marker" ]; then
  fail "Optional AI Tool Stack bundle failure records optional-ai-cli-tools marker"
fi
pass "routine Optional AI Tool Stack bundle failure records a warning and exits non-zero"

HOME="$ai_cli_failure_home" XDG_STATE_HOME="$ai_cli_failure_state" \
  AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$ai_cli_failure_log" AI_BREW_FAIL=1 \
  TERRAPOD_FIRST_RUN_APPLY=1 PATH="$macos_ai_brew_bin:/usr/bin:/bin" \
  sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  fail "first-run Optional AI Tool Stack bundle failure remains recoverable"
pass "first-run Optional AI Tool Stack bundle failure records a warning and exits zero"

HOME="$ai_cli_failure_home" XDG_STATE_HOME="$ai_cli_failure_state" \
  AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$ai_cli_failure_log" AI_BREW_FAIL=0 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script"
if [ -e "$ai_cli_failure_marker" ]; then
  fail "successful Optional AI Tool Stack retry clears warning marker"
fi
pass "successful Optional AI Tool Stack retry clears warning marker"

development_workspace_zellij_layout="$(render_managed_file "$development_workspace_data" ".config/zellij/layouts/dev.kdl")"

for pane in CLAUDE CODEX ANTIGRAVITY; do
  if ! printf '%s\n' "$development_workspace_zellij_layout" |
    grep -E "pane name=\"${pane}\" .*start_suspended=true" >/dev/null; then
    fail "enableDevelopmentWorkspace starts assistant panes suspended"
  fi
done

pass "enableDevelopmentWorkspace starts assistant panes suspended"

if printf '%s\n' "$development_workspace_zellij_layout" | grep -F 'command="gemini"' >/dev/null; then
  fail "enableDevelopmentWorkspace no longer launches Gemini CLI"
fi

pass "enableDevelopmentWorkspace no longer launches Gemini CLI"

if ! printf '%s\n' "$development_workspace_zellij_layout" |
  grep -A2 'pane name="ANTIGRAVITY" command="agy"' |
  grep -F 'args "--dangerously-skip-permissions"' >/dev/null; then
  fail "enableDevelopmentWorkspace passes supported permission skip mode to the Antigravity pane"
fi

pass "enableDevelopmentWorkspace passes supported permission skip mode to the Antigravity pane"
