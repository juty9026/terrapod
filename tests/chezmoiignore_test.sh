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

assert_contains_text() {
  text="$1"
  needle="$2"
  message="$3"

  if ! printf '%s\n' "$text" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains_text() {
  text="$1"
  needle="$2"
  message="$3"

  if printf '%s\n' "$text" | grep -F "$needle" >/dev/null; then
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
dot_config/cmux
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
assert_contains_text "$terminal_apps_brewfile" 'cask "cmux"' "terminal-apps group renders cmux"
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

if awk '/^[[:space:]]*cask[[:space:]]+"/ && $0 !~ /^[[:space:]]*cask[[:space:]]+"font-(jetbrains-mono-nerd-font|d2coding)"$/ { found=1 } END { exit found ? 0 : 1 }' "$repo_root/Brewfile"; then
  fail "core Brewfile casks are terminal font casks only"
fi

pass "core Brewfile casks are terminal font casks only"

for app_config in \
  ".config/ghostty/config" \
  ".config/cmux/settings.json" \
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

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  ".chezmoiscripts/run_onchange_after_40-remove-legacy-npm-ai-tools.sh.tmpl" \
  "macOS default ignores legacy AI tool uninstall script"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  "dot_config/nvim" \
  "Ubuntu VPS ignores Optional Editor Stack entries by default"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  "dot_config/zellij/layouts/dev.kdl" \
  "Ubuntu VPS ignores Optional Development Workspace layout by default"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl" \
  "Ubuntu VPS ignores Optional AI Tool Stack installer by default"

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  "dot_config/nvim" \
  "macOS ignores Optional Editor Stack entries by default"

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "macOS default ignores Antigravity PATH snippet"

assert_managed_paths_exclude_prefix \
  "$ai_cli_tools_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "Linux enableAiCliTools ignores Antigravity PATH snippet"

assert_managed_paths_include_prefix \
  "$macos_ai_cli_tools_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "macOS enableAiCliTools includes Antigravity PATH snippet"

assert_managed_paths_include_prefix \
  "$macos_development_workspace_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "macOS enableDevelopmentWorkspace includes Antigravity PATH snippet"

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
  ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl" \
  "enableAiCliTools includes Optional AI Tool Stack installer"

assert_managed_paths_include_prefix \
  "$development_workspace_managed" \
  ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl" \
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

ai_cli_tools_installer="$(render_template "$ai_cli_tools_data" ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl")"
development_workspace_ai_installer="$(render_template "$development_workspace_data" ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl")"

for package in \
  "@anthropic-ai/claude-code" \
  "@google/gemini-cli" \
  "@openai/codex"
do
  if ! printf '%s\n' "$ai_cli_tools_installer" | grep -F "$package" >/dev/null; then
    fail "enableAiCliTools renders Optional AI Tool Stack installer"
  fi

  if ! printf '%s\n' "$development_workspace_ai_installer" | grep -F "$package" >/dev/null; then
    fail "enableDevelopmentWorkspace renders Optional AI Tool Stack installer"
  fi
done

pass "enableAiCliTools renders Optional AI Tool Stack installer"
pass "enableDevelopmentWorkspace renders Optional AI Tool Stack installer"

development_workspace_zellij_layout="$(render_managed_file "$development_workspace_data" ".config/zellij/layouts/dev.kdl")"

for pane in CLAUDE CODEX GEMINI; do
  if ! printf '%s\n' "$development_workspace_zellij_layout" |
    grep -E "pane name=\"${pane}\" .*start_suspended=true" >/dev/null; then
    fail "enableDevelopmentWorkspace starts assistant panes suspended"
  fi
done

pass "enableDevelopmentWorkspace starts assistant panes suspended"

if ! printf '%s\n' "$development_workspace_zellij_layout" |
  grep -A2 'pane name="GEMINI" command="gemini"' |
  grep -F 'args "--yolo"' >/dev/null; then
  fail "enableDevelopmentWorkspace passes yolo mode to the Gemini pane"
fi

pass "enableDevelopmentWorkspace passes yolo mode to the Gemini pane"
