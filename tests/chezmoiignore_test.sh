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
macos_monitoring_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":true}'
macos_ai_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupAiApps":true}'
macos_terminal_apps_managed_targets="$(managed_target_paths "$macos_terminal_apps_data")"
macos_ai_apps_managed="$(managed_source_paths "$macos_ai_apps_data")"
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

macos_only_entries="
.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl
.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl
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
ai_apps_brewfile="$(render_template "$macos_ai_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
macos_bootstrap="$(render_template "$macos_data" ".chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl")"
macos_terminal_apps_bootstrap="$(render_template "$macos_terminal_apps_data" ".chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl")"
macos_ai_apps_bootstrap="$(render_template "$macos_ai_apps_data" ".chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl")"
macos_development_workspace_bootstrap="$(render_template "$macos_development_workspace_data" ".chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl")"
macos_karabiner_opener="$(render_template "$macos_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
macos_terminal_apps_karabiner_opener="$(render_template "$macos_terminal_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
macos_automation_apps_karabiner_opener="$(render_template "$macos_automation_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
macos_ai_apps_karabiner_opener="$(render_template "$macos_ai_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"

assert_contains_text \
  "$macos_bootstrap" \
  'brew bundle --no-upgrade --file="$core_brewfile"' \
  "macOS bootstrap always runs the core Brewfile"

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
sed \
  -e "s#/opt/homebrew/bin/brew#$tmp_dir/missing-opt-homebrew-brew#g" \
  -e "s#/usr/local/bin/brew#$tmp_dir/missing-usr-local-brew#g" \
  "$macos_bootstrap_script" >"$homebrew_installer_failure_script"
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
assert_not_contains_text "$macos_brewfile" 'cask "raycast"' "macOS default does not render Raycast"
assert_not_contains_text "$macos_brewfile" 'cask "1password-cli"' "macOS default does not render 1Password CLI"
assert_not_contains_text "$macos_brewfile" 'cask "istat-menus"' "macOS default does not render iStat Menus"
assert_not_contains_text "$macos_brewfile" 'cask "claude"' "macOS default does not render Claude Desktop"
assert_not_contains_text "$macos_brewfile" 'cask "codex-app"' "macOS default does not render Codex Desktop"
assert_not_contains_text "$macos_brewfile" 'cask "codex"' "macOS default does not render Codex CLI as a desktop app"
assert_not_contains_text "$macos_brewfile" 'cask "antigravity"' "macOS default does not render Antigravity 2.0"
assert_not_contains_text "$macos_brewfile" 'cask "antigravity-ide"' "macOS default does not render Antigravity IDE"

assert_contains_text "$terminal_apps_brewfile" 'cask "ghostty"' "terminal-apps group renders Ghostty"
assert_not_contains_text "$terminal_apps_brewfile" 'cask "cmux"' "terminal-apps group does not render cmux"
assert_not_contains_text "$terminal_apps_brewfile" 'cask "hammerspoon"' "terminal-apps group does not render automation casks"

assert_contains_text "$automation_apps_brewfile" 'cask "hammerspoon"' "automation group renders Hammerspoon"
assert_contains_text "$automation_apps_brewfile" 'cask "karabiner-elements"' "automation group renders Karabiner-Elements"

assert_contains_text "$launcher_apps_brewfile" 'cask "raycast"' "launcher group renders Raycast"
assert_contains_text "$launcher_apps_brewfile" 'cask "1password-cli"' "launcher group renders 1Password CLI"

assert_contains_text "$monitoring_apps_brewfile" 'cask "istat-menus"' "monitoring group renders iStat Menus"

assert_contains_text "$ai_apps_brewfile" 'cask "claude"' "ai-apps group renders Claude Desktop"
assert_contains_text "$ai_apps_brewfile" 'cask "codex-app"' "ai-apps group renders Codex Desktop cask"
assert_not_contains_text "$ai_apps_brewfile" 'cask "codex"' "ai-apps group does not render Codex CLI cask"
assert_contains_text "$ai_apps_brewfile" 'cask "antigravity"' "ai-apps group renders Antigravity 2.0"
assert_contains_text "$ai_apps_brewfile" 'cask "antigravity-ide"' "ai-apps group renders Antigravity IDE"
ai_apps_casks="$(
  printf '%s\n' "$ai_apps_brewfile" |
    awk '/^[[:space:]]*cask[[:space:]]+"/ { print }'
)"
expected_ai_apps_casks='cask "claude"
cask "codex-app"
cask "antigravity"
cask "antigravity-ide"'
assert_text_equals \
  "$ai_apps_casks" \
  "$expected_ai_apps_casks" \
  "ai-apps group renders exactly the expected casks"

assert_contains_text \
  "$macos_terminal_apps_bootstrap" \
  "terrapod-macos-desktop-apps" \
  "terminal-apps group renders macOS Desktop App Stack Brewfile"

assert_contains_text \
  "$macos_terminal_apps_bootstrap" \
  'brew bundle --no-upgrade --file="$desktop_brewfile"' \
  "terminal-apps group runs macOS Desktop App Stack Brewfile"

assert_contains_text \
  "$macos_ai_apps_bootstrap" \
  "terrapod-macos-desktop-apps" \
  "ai-apps group renders macOS Desktop App Stack Brewfile"

assert_contains_text \
  "$macos_ai_apps_bootstrap" \
  'brew bundle --no-upgrade --file="$desktop_brewfile"' \
  "ai-apps group runs macOS Desktop App Stack Brewfile"

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
  "$macos_ai_apps_karabiner_opener" \
  "macOS Desktop App Stack enabled: true" \
  "Karabiner opener tracks enabled ai-apps Desktop App Stack state"

assert_contains_text \
  "$macos_ai_apps_karabiner_opener" \
  "macOS Desktop App Stack Brewfile checksum" \
  "Karabiner opener tracks ai-apps Desktop App Stack Brewfile changes"

assert_texts_differ \
  "$macos_terminal_apps_karabiner_opener" \
  "$macos_automation_apps_karabiner_opener" \
  "Karabiner opener tracks different macOS Desktop App Group combinations"

for cask in \
  font-jetbrains-mono-nerd-font \
  font-d2coding
do
  if ! grep -Fx "cask \"$cask\"" "$repo_root/Brewfile" >/dev/null; then
    fail "core Brewfile contains expected terminal font cask: $cask"
  fi
done

pass "core Brewfile contains expected terminal font casks"

if ! grep -Fx 'brew "gum"' "$repo_root/Brewfile" >/dev/null; then
  fail "core Brewfile declares gum as the setup UI dependency"
fi

pass "core Brewfile declares gum as the setup UI dependency"

if awk '/^[[:space:]]*cask[[:space:]]+"/ && $0 !~ /^[[:space:]]*cask[[:space:]]+"font-(jetbrains-mono-nerd-font|d2coding)"$/ { found=1 } END { exit found ? 0 : 1 }' "$repo_root/Brewfile"; then
  fail "core Brewfile casks are terminal font casks only"
fi

pass "core Brewfile casks are terminal font casks only"

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

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  "dot_config/nvim" \
  "macOS ignores Optional Editor Stack entries by default"

if [ -e "$repo_root/dot_config/zsh/path.d/antigravity.zsh.tmpl" ]; then
  fail "legacy Antigravity app-bundle PATH snippet is no longer managed"
fi

pass "legacy Antigravity app-bundle PATH snippet is no longer managed"

assert_managed_paths_exclude_prefix \
  "$macos_ai_apps_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "macOS ai-apps group does not restore legacy Antigravity PATH snippet"

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

if ! printf '%s\n' "$ubuntu_mise_config" | grep -F '"aqua:neovim/neovim" = "latest"' >/dev/null; then
  fail "Ubuntu VPS keeps plain Neovim in the Core Shell Stack"
fi

pass "Ubuntu VPS keeps plain Neovim in the Core Shell Stack"

if ! printf '%s\n' "$ubuntu_mise_config" | grep -F '"aqua:cli/cli" = "latest"' >/dev/null; then
  fail "Ubuntu VPS installs GitHub CLI gh in the Core Shell Stack"
fi

pass "Ubuntu VPS installs GitHub CLI gh in the Core Shell Stack"

mise_tools_installer="$(render_template "$ubuntu_data" ".chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl")"

if ! printf '%s\n' "$mise_tools_installer" |
  grep -E '^# mise-config-sha256=[0-9a-f]{64}$' >/dev/null; then
  fail "mise tool installer tracks rendered mise config changes"
fi

pass "mise tool installer tracks rendered mise config changes"

ai_cli_tools_installer="$(render_template "$ai_cli_tools_data" ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl")"
development_workspace_ai_installer="$(render_template "$development_workspace_data" ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl")"
disabled_ai_cli_tools_cleanup="$(render_template "$ubuntu_data" ".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl")"

assert_contains_text "$disabled_ai_cli_tools_cleanup" "AI_CLI_WARNING_CATEGORY=optional-ai-cli-tools" "disabled Optional AI Tool Stack renders optional AI CLI warning category"
assert_contains_text "$disabled_ai_cli_tools_cleanup" 'clear_install_warning "$AI_CLI_WARNING_CATEGORY"' "disabled Optional AI Tool Stack renders stale marker cleanup"
assert_not_contains_text "$disabled_ai_cli_tools_cleanup" "https://chatgpt.com/codex/install.sh" "disabled Optional AI Tool Stack cleanup does not render installer URLs"

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

if [ ! -f "$ai_marker_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "test setup should create an optional-ai-cli-tools warning marker"
fi
printf '%s\n' \
  "category='ai-cli-tools'" \
  "summary='Legacy Optional AI CLI tool install needs attention'" \
  "guidance='Rerun tpod apply after network access is restored.'" \
  "updated_at='2026-01-01T00:00:00Z'" \
  >"$ai_marker_state/terrapod/install-warnings/ai-cli-tools"
if [ ! -f "$ai_marker_state/terrapod/install-warnings/ai-cli-tools" ]; then
  fail "test setup should create a legacy ai-cli-tools warning marker"
fi

HOME="$ai_marker_home" XDG_STATE_HOME="$ai_marker_state" sh "$disabled_ai_cli_tools_cleanup_script"
if [ -e "$ai_marker_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "disabled Optional AI Tool Stack cleanup should clear stale optional-ai-cli-tools marker"
fi
if [ -e "$ai_marker_state/terrapod/install-warnings/ai-cli-tools" ]; then
  fail "disabled Optional AI Tool Stack cleanup should clear stale legacy ai-cli-tools marker"
fi
pass "disabled Optional AI Tool Stack cleanup clears stale optional AI CLI markers"

ai_cli_tools_installer_script="$tmp_dir/ai-cli-tools-installer.sh"
printf '%s\n' "$ai_cli_tools_installer" >"$ai_cli_tools_installer_script"
sh -n "$ai_cli_tools_installer_script" || fail "enabled Optional AI Tool Stack installer script should be valid sh"
pass "enabled Optional AI Tool Stack installer script is valid sh"

ai_cli_skip_home="$tmp_dir/ai-cli-skip-home"
ai_cli_skip_state="$tmp_dir/ai-cli-skip-state"
ai_cli_skip_curl_log="$tmp_dir/ai-cli-skip-curl.log"
mkdir -p "$ai_cli_skip_home/.local/bin"
for command_name in agy claude codex; do
  write_stub "$ai_cli_skip_home/.local/bin/$command_name" \
    'exit 0'
done
write_stub "$ai_cli_skip_home/.local/bin/curl" \
  'printf "%s\n" "curl called:$*" >>"$AI_CLI_SKIP_CURL_LOG"' \
  'exit 7'

ai_cli_skip_status=0
HOME="$ai_cli_skip_home" \
  XDG_STATE_HOME="$ai_cli_skip_state" \
  AI_CLI_SKIP_CURL_LOG="$ai_cli_skip_curl_log" \
  TMPDIR="$tmp_dir" \
  PATH="/usr/bin:/bin" \
  http_proxy="http://127.0.0.1:9" \
  https_proxy="http://127.0.0.1:9" \
  all_proxy="http://127.0.0.1:9" \
  HTTP_PROXY="http://127.0.0.1:9" \
  HTTPS_PROXY="http://127.0.0.1:9" \
  ALL_PROXY="http://127.0.0.1:9" \
  sh "$ai_cli_tools_installer_script" >/dev/null 2>"$tmp_dir/ai-cli-skip.err" || ai_cli_skip_status=$?
if [ "$ai_cli_skip_status" -ne 0 ]; then
  fail "enabled Optional AI Tool Stack installer exits 0 when all tools already exist under HOME-local PATH"
fi
pass "enabled Optional AI Tool Stack installer exits 0 when all tools already exist under HOME-local PATH"

if [ -s "$ai_cli_skip_curl_log" ]; then
  fail "enabled Optional AI Tool Stack installer skips curl when all tools already exist under HOME-local PATH"
fi
pass "enabled Optional AI Tool Stack installer skips curl when all tools already exist under HOME-local PATH"

if [ -e "$ai_cli_skip_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "enabled Optional AI Tool Stack installer leaves no optional-ai-cli-tools marker when all tools are already installed"
fi
pass "enabled Optional AI Tool Stack installer leaves no optional-ai-cli-tools marker when all tools are already installed"

ai_cli_retry_home="$tmp_dir/ai-cli-retry-home"
ai_cli_retry_state="$tmp_dir/ai-cli-retry-state"
ai_cli_retry_url_log="$tmp_dir/ai-cli-retry-url.log"
ai_cli_retry_run_log="$tmp_dir/ai-cli-retry-run.log"
ai_cli_retry_antigravity_installer="$tmp_dir/ai-cli-retry-antigravity-installer.sh"
ai_cli_retry_claude_installer="$tmp_dir/ai-cli-retry-claude-installer.sh"
ai_cli_retry_codex_installer="$tmp_dir/ai-cli-retry-codex-installer.sh"
mkdir -p "$ai_cli_retry_home/.local/bin"

write_stub "$ai_cli_retry_antigravity_installer" \
  'printf "%s\n" "run:antigravity" >>"$AI_CLI_RETRY_RUN_LOG"' \
  'mkdir -p "$HOME/.local/bin"' \
  'printf "%s\n" "#!/bin/sh" "exit 0" >"$HOME/.local/bin/agy"' \
  'chmod +x "$HOME/.local/bin/agy"'
write_stub "$ai_cli_retry_claude_installer" \
  'printf "%s\n" "run:claude" >>"$AI_CLI_RETRY_RUN_LOG"' \
  'if [ "${AI_CLI_RETRY_CLAUDE_FAIL:-}" = "1" ]; then exit 42; fi' \
  'mkdir -p "$HOME/.local/bin"' \
  'printf "%s\n" "#!/bin/sh" "exit 0" >"$HOME/.local/bin/claude"' \
  'chmod +x "$HOME/.local/bin/claude"'
write_stub "$ai_cli_retry_codex_installer" \
  'printf "%s\n" "run:codex" >>"$AI_CLI_RETRY_RUN_LOG"' \
  'printf "%s\n" "codex:noninteractive=${CODEX_NON_INTERACTIVE:-}" >>"$AI_CLI_RETRY_RUN_LOG"' \
  'printf "%s\n" "codex:path=$PATH" >>"$AI_CLI_RETRY_RUN_LOG"' \
  'mkdir -p "$HOME/.local/bin"' \
  'printf "%s\n" "#!/bin/sh" "exit 0" >"$HOME/.local/bin/codex"' \
  'chmod +x "$HOME/.local/bin/codex"'

write_stub "$ai_cli_retry_home/.local/bin/bash" \
  'printf "%s\n" "bash:$1" >>"$AI_CLI_RETRY_RUN_LOG"' \
  'exec sh "$@"'
write_stub "$ai_cli_retry_home/.local/bin/curl" \
  'url=' \
  'output=' \
  'while [ "$#" -gt 0 ]; do' \
  '  case "$1" in' \
  '    -o)' \
  '      shift' \
  '      output="${1:-}"' \
  '      ;;' \
  '    https://*)' \
  '      url="$1"' \
  '      ;;' \
  '  esac' \
  '  shift' \
  'done' \
  'if [ -z "$url" ] || [ -z "$output" ]; then' \
  '  printf "%s\n" "bad curl args" >>"$AI_CLI_RETRY_RUN_LOG"' \
  '  exit 2' \
  'fi' \
  'printf "%s\n" "$url" >>"$AI_CLI_RETRY_URL_LOG"' \
  'case "$url" in' \
  '  https://antigravity.google/cli/install.sh)' \
  '    cp "$AI_CLI_RETRY_ANTIGRAVITY_INSTALLER" "$output"' \
  '    ;;' \
  '  https://claude.ai/install.sh)' \
  '    cp "$AI_CLI_RETRY_CLAUDE_INSTALLER" "$output"' \
  '    ;;' \
  '  https://chatgpt.com/codex/install.sh)' \
  '    cp "$AI_CLI_RETRY_CODEX_INSTALLER" "$output"' \
  '    ;;' \
  '  *)' \
  '    printf "%s\n" "unexpected url:$url" >>"$AI_CLI_RETRY_RUN_LOG"' \
  '    exit 3' \
  '    ;;' \
  'esac'

ai_cli_retry_status=0
HOME="$ai_cli_retry_home" \
  XDG_STATE_HOME="$ai_cli_retry_state" \
  AI_CLI_RETRY_URL_LOG="$ai_cli_retry_url_log" \
  AI_CLI_RETRY_RUN_LOG="$ai_cli_retry_run_log" \
  AI_CLI_RETRY_ANTIGRAVITY_INSTALLER="$ai_cli_retry_antigravity_installer" \
  AI_CLI_RETRY_CLAUDE_INSTALLER="$ai_cli_retry_claude_installer" \
  AI_CLI_RETRY_CODEX_INSTALLER="$ai_cli_retry_codex_installer" \
  AI_CLI_RETRY_CLAUDE_FAIL=1 \
  TMPDIR="$tmp_dir" \
  PATH="$ai_cli_retry_home/.local/bin:/usr/bin:/bin" \
  sh "$ai_cli_tools_installer_script" >/dev/null 2>"$tmp_dir/ai-cli-retry-first.err" || ai_cli_retry_status=$?
if [ "$ai_cli_retry_status" -ne 0 ]; then
  fail "enabled Optional AI Tool Stack installer exits 0 while recording partial AI CLI installer failures"
fi
pass "enabled Optional AI Tool Stack installer exits 0 while recording partial AI CLI installer failures"

if [ ! -x "$ai_cli_retry_home/.local/bin/agy" ]; then
  fail "enabled Optional AI Tool Stack installer keeps successful Antigravity install after Claude failure"
fi
if [ ! -x "$ai_cli_retry_home/.local/bin/codex" ]; then
  fail "enabled Optional AI Tool Stack installer keeps running Codex after Claude failure"
fi
if [ -x "$ai_cli_retry_home/.local/bin/claude" ]; then
  fail "enabled Optional AI Tool Stack installer does not mark failed Claude install as present"
fi
pass "enabled Optional AI Tool Stack installer records successful and failed AI CLI installs independently"

ai_cli_retry_first_urls="$(cat "$ai_cli_retry_url_log")"
assert_contains_text "$ai_cli_retry_first_urls" "https://antigravity.google/cli/install.sh" "enabled Optional AI Tool Stack first run downloads official Antigravity installer"
assert_contains_text "$ai_cli_retry_first_urls" "https://claude.ai/install.sh" "enabled Optional AI Tool Stack first run downloads official Claude installer"
assert_contains_text "$ai_cli_retry_first_urls" "https://chatgpt.com/codex/install.sh" "enabled Optional AI Tool Stack first run downloads official Codex installer"

ai_cli_retry_first_runs="$(cat "$ai_cli_retry_run_log")"
assert_contains_text "$ai_cli_retry_first_runs" "run:antigravity" "enabled Optional AI Tool Stack first run executes Antigravity installer"
assert_contains_text "$ai_cli_retry_first_runs" "run:claude" "enabled Optional AI Tool Stack first run executes Claude installer"
assert_contains_text "$ai_cli_retry_first_runs" "run:codex" "enabled Optional AI Tool Stack first run continues to Codex after Claude failure"
ai_cli_retry_first_bash_runs="$(printf '%s\n' "$ai_cli_retry_first_runs" | grep -c '^bash:' || true)"
if [ "$ai_cli_retry_first_bash_runs" -ne 2 ]; then
  fail "enabled Optional AI Tool Stack first run executes Antigravity and Claude installers with bash"
fi
pass "enabled Optional AI Tool Stack first run executes Antigravity and Claude installers with bash"
assert_contains_text "$ai_cli_retry_first_runs" "codex:noninteractive=1" "enabled Optional AI Tool Stack first run keeps Codex noninteractive"
case "$(uname -s)" in
  Darwin)
    ai_cli_retry_expected_path="$ai_cli_retry_home/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
    ;;
  *)
    ai_cli_retry_expected_path="$ai_cli_retry_home/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
    ;;
esac
assert_contains_text "$ai_cli_retry_first_runs" "codex:path=$ai_cli_retry_expected_path" "enabled Optional AI Tool Stack first run passes host-expected PATH to Codex"

ai_cli_retry_marker_dir="$ai_cli_retry_state/terrapod/install-warnings"
ai_cli_retry_marker="$ai_cli_retry_marker_dir/optional-ai-cli-tools"
if [ ! -f "$ai_cli_retry_marker" ]; then
  fail "enabled Optional AI Tool Stack installer writes optional-ai-cli-tools marker for partial failures"
fi
ai_cli_retry_marker_files="$(find "$ai_cli_retry_marker_dir" -type f -print | sort)"
assert_text_equals "$ai_cli_retry_marker_files" "$ai_cli_retry_marker" "enabled Optional AI Tool Stack installer writes one optional-ai-cli-tools marker for partial failures"
ai_cli_retry_marker_text="$(cat "$ai_cli_retry_marker")"
assert_contains_text "$ai_cli_retry_marker_text" "claude" "enabled Optional AI Tool Stack partial failure marker mentions Claude only"
assert_not_contains_text "$ai_cli_retry_marker_text" "agy" "enabled Optional AI Tool Stack partial failure marker omits successful agy command"
assert_not_contains_text "$ai_cli_retry_marker_text" "antigravity" "enabled Optional AI Tool Stack partial failure marker omits successful Antigravity"
assert_not_contains_text "$ai_cli_retry_marker_text" "Antigravity" "enabled Optional AI Tool Stack partial failure marker omits successful Antigravity label"
assert_not_contains_text "$ai_cli_retry_marker_text" "codex" "enabled Optional AI Tool Stack partial failure marker omits successful Codex"
assert_not_contains_text "$ai_cli_retry_marker_text" "Codex" "enabled Optional AI Tool Stack partial failure marker omits successful Codex label"

: >"$ai_cli_retry_url_log"
: >"$ai_cli_retry_run_log"
ai_cli_retry_second_status=0
HOME="$ai_cli_retry_home" \
  XDG_STATE_HOME="$ai_cli_retry_state" \
  AI_CLI_RETRY_URL_LOG="$ai_cli_retry_url_log" \
  AI_CLI_RETRY_RUN_LOG="$ai_cli_retry_run_log" \
  AI_CLI_RETRY_ANTIGRAVITY_INSTALLER="$ai_cli_retry_antigravity_installer" \
  AI_CLI_RETRY_CLAUDE_INSTALLER="$ai_cli_retry_claude_installer" \
  AI_CLI_RETRY_CODEX_INSTALLER="$ai_cli_retry_codex_installer" \
  AI_CLI_RETRY_CLAUDE_FAIL=0 \
  TMPDIR="$tmp_dir" \
  PATH="$ai_cli_retry_home/.local/bin:/usr/bin:/bin" \
  sh "$ai_cli_tools_installer_script" >/dev/null 2>"$tmp_dir/ai-cli-retry-second.err" || ai_cli_retry_second_status=$?
if [ "$ai_cli_retry_second_status" -ne 0 ]; then
  fail "enabled Optional AI Tool Stack retry exits 0 after recovering failed Claude install"
fi
pass "enabled Optional AI Tool Stack retry exits 0 after recovering failed Claude install"

if [ ! -x "$ai_cli_retry_home/.local/bin/claude" ]; then
  fail "enabled Optional AI Tool Stack retry installs Claude after previous failure"
fi
pass "enabled Optional AI Tool Stack retry installs Claude after previous failure"

ai_cli_retry_second_urls="$(cat "$ai_cli_retry_url_log" 2>/dev/null || true)"
assert_text_equals "$ai_cli_retry_second_urls" "https://claude.ai/install.sh" "enabled Optional AI Tool Stack retry downloads only Claude after partial failure"
ai_cli_retry_second_runs="$(cat "$ai_cli_retry_run_log" 2>/dev/null || true)"
assert_contains_text "$ai_cli_retry_second_runs" "run:claude" "enabled Optional AI Tool Stack retry executes Claude after partial failure"
ai_cli_retry_second_bash_runs="$(printf '%s\n' "$ai_cli_retry_second_runs" | grep -c '^bash:' || true)"
if [ "$ai_cli_retry_second_bash_runs" -ne 1 ]; then
  fail "enabled Optional AI Tool Stack retry executes only Claude installer with bash"
fi
pass "enabled Optional AI Tool Stack retry executes only Claude installer with bash"
assert_not_contains_text "$ai_cli_retry_second_runs" "run:antigravity" "enabled Optional AI Tool Stack retry skips pre-existing Antigravity"
assert_not_contains_text "$ai_cli_retry_second_runs" "run:codex" "enabled Optional AI Tool Stack retry skips pre-existing Codex"

if [ -e "$ai_cli_retry_marker" ]; then
  fail "enabled Optional AI Tool Stack retry clears optional-ai-cli-tools marker after recovery"
fi
pass "enabled Optional AI Tool Stack retry clears optional-ai-cli-tools marker after recovery"

for installer_url in \
  "https://antigravity.google/cli/install.sh" \
  "https://claude.ai/install.sh" \
  "https://chatgpt.com/codex/install.sh"
do
  assert_contains_text "$ai_cli_tools_installer" "$installer_url" "enableAiCliTools renders official AI CLI installer URL: $installer_url"
  assert_contains_text "$development_workspace_ai_installer" "$installer_url" "enableDevelopmentWorkspace renders official AI CLI installer URL: $installer_url"
done

for legacy_text in \
  "@anthropic-ai/claude-code" \
  "@google/gemini-cli" \
  "@openai/codex" \
  "npm install -g" \
  "npm uninstall" \
  "--skip-path" \
  "--skip-aliases"
do
  assert_not_contains_text "$ai_cli_tools_installer" "$legacy_text" "enableAiCliTools does not render legacy npm AI CLI management: $legacy_text"
  assert_not_contains_text "$development_workspace_ai_installer" "$legacy_text" "enableDevelopmentWorkspace does not render legacy npm AI CLI management: $legacy_text"
done

for expected_path_assignment in \
  'AI_CLI_EXPECTED_PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"' \
  'AI_CLI_EXPECTED_PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"'
do
  assert_contains_text "$ai_cli_tools_installer" "$expected_path_assignment" "enableAiCliTools renders expected AI CLI installer PATH assignment: $expected_path_assignment"
  assert_contains_text "$development_workspace_ai_installer" "$expected_path_assignment" "enableDevelopmentWorkspace renders expected AI CLI installer PATH assignment: $expected_path_assignment"
done

for unsafe_installer_text in \
  "GITHUB_TOKEN" \
  "Authorization:" \
  "api.github.com" \
  "sed -i" \
  "apply_patch" \
  "yes |" \
  "| sh" \
  "| bash"
do
  assert_not_contains_text "$ai_cli_tools_installer" "$unsafe_installer_text" "enableAiCliTools renders official-only AI CLI installer without unsafe automation text: $unsafe_installer_text"
  assert_not_contains_text "$development_workspace_ai_installer" "$unsafe_installer_text" "enableDevelopmentWorkspace renders official-only AI CLI installer without unsafe automation text: $unsafe_installer_text"
done

assert_contains_text "$ai_cli_tools_installer" 'CODEX_NON_INTERACTIVE=1' \
  "enableAiCliTools runs Codex installer noninteractively"
assert_contains_text "$development_workspace_ai_installer" 'CODEX_NON_INTERACTIVE=1' \
  "enableDevelopmentWorkspace runs Codex installer noninteractively"

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
