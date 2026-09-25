#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"

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
macos_mobile_dev_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false,"enableMacosAppGroupMobileDev":true}'
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

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  "gh-extensions.tmpl" \
  "macOS does not manage the rendered GitHub CLI Extension Set list"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  "gh-extensions.tmpl" \
  "Ubuntu does not manage the rendered GitHub CLI Extension Set list"

for entry in \
  .chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl \
  dot_local/lib/terrapod/homebrew-core-bundle.sh
do
  printf '%s\n' "$ubuntu_managed" | grep -Fx "$entry" >/dev/null ||
    fail "Ubuntu manages cross-profile Homebrew entry: $entry"
done
pass "Ubuntu manages cross-profile Homebrew core state"

# .chezmoiignore names target paths, so an entry only means anything if some
# source script actually renders to it. A dead entry rots into false safety: the
# list looks complete while a macOS-only script that is genuinely missing from it
# goes unnoticed.
chezmoiscript_target_name() {
  script_target_name="${1##*/}"
  script_target_name="${script_target_name%.tmpl}"

  case "$script_target_name" in
    run_onchange_before_*) script_target_name="${script_target_name#run_onchange_before_}" ;;
    run_onchange_after_*) script_target_name="${script_target_name#run_onchange_after_}" ;;
    run_onchange_*) script_target_name="${script_target_name#run_onchange_}" ;;
    run_before_*) script_target_name="${script_target_name#run_before_}" ;;
    run_after_*) script_target_name="${script_target_name#run_after_}" ;;
    run_*) script_target_name="${script_target_name#run_}" ;;
  esac

  printf '%s\n' "$script_target_name"
}

chezmoiscript_target_names=""
for script_source in "$repo_root"/.chezmoiscripts/*; do
  chezmoiscript_target_names="$chezmoiscript_target_names$(chezmoiscript_target_name "$script_source")
"
done

ignored_chezmoiscripts="$(grep -E '^\.chezmoiscripts/' "$repo_root/.chezmoiignore" || true)"

if [ -z "$ignored_chezmoiscripts" ]; then
  fail ".chezmoiignore still names chezmoi scripts to check"
fi
pass ".chezmoiignore still names chezmoi scripts to check"

for ignored_chezmoiscript in $ignored_chezmoiscripts; do
  ignored_chezmoiscript_name="${ignored_chezmoiscript#.chezmoiscripts/}"

  if ! printf '%s' "$chezmoiscript_target_names" | grep -Fx -- "$ignored_chezmoiscript_name" >/dev/null; then
    printf '%s\n' "known .chezmoiscripts target names:" >&2
    printf '%s' "$chezmoiscript_target_names" | sed 's/^/  /' >&2
    fail ".chezmoiignore entry resolves to a source script: $ignored_chezmoiscript"
  fi
  pass ".chezmoiignore entry resolves to a source script: $ignored_chezmoiscript"
done

macos_only_entries="
.chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl
.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl
.chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl
dot_local/lib/terrapod/executable_jetendard-font
dot_local/lib/terrapod/executable_jetendard-settings
dot_local/lib/terrapod/jetendard-font-install.sh
dot_local/lib/terrapod/jetendard-font-status.sh
dot_config/ghostty
dot_config/private_karabiner
dot_hammerspoon
dot_zprofile.tmpl
"

for entry in $macos_only_entries; do
  if printf '%s\n' "$ubuntu_managed" | grep -Fx "$entry" >/dev/null; then
    fail "Ubuntu VPS should not manage macOS-only entry: $entry"
  fi

  if ! printf '%s\n' "$macos_managed" | grep -Fx "$entry" >/dev/null; then
    fail "macOS should manage macOS-only entry: $entry"
  fi
done

pass "Ubuntu VPS ignores macOS-only entries"

linux_only_entries="
dot_local/lib/terrapod/ubuntu-bootstrap.sh
"

for entry in $linux_only_entries; do
  if printf '%s\n' "$macos_managed" | grep -Fx "$entry" >/dev/null; then
    fail "macOS should not manage VPS-only entry: $entry"
  fi

  if ! printf '%s\n' "$ubuntu_managed" | grep -Fx "$entry" >/dev/null; then
    fail "Ubuntu VPS should manage VPS-only entry: $entry"
  fi
done

pass "macOS ignores VPS-only entries"
assert_managed_paths_exclude_prefix \
  "$macos_managed_targets" \
  "Brewfile.macos-desktop-apps" \
  "macOS default does not manage rendered macOS Desktop App Stack Brewfile target"

assert_managed_paths_exclude_prefix \
  "$macos_terminal_apps_managed_targets" \
  "Brewfile.macos-desktop-apps" \
  "terminal-apps group does not manage rendered macOS Desktop App Stack Brewfile target"

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

assert_managed_paths_include_prefix \
  "$macos_managed" \
  ".chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl" \
  "macOS default applies Jetendard app settings"

assert_managed_paths_include_prefix \
  "$macos_development_apps_managed" \
  ".chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl" \
  "development-apps selection applies the same user-scoped Jetendard settings"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl" \
  "Ubuntu excludes Jetendard app settings"

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
  ".chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl" \
  "Ubuntu VPS includes Optional AI Tool Stack warning cleanup by default"

assert_managed_paths_include_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl" \
  "Ubuntu VPS includes always-run mise tool reconciliation"

assert_managed_paths_include_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_before_20-install-gh-extensions.sh.tmpl" \
  "Ubuntu VPS includes always-run GitHub CLI Extension Set install hook"

assert_managed_paths_include_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_before_30-install-shell-integrations.sh.tmpl" \
  "Ubuntu VPS includes always-run shell integration install hook"

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  "dot_config/nvim" \
  "macOS ignores Optional Editor Stack entries by default"

assert_managed_paths_include_prefix \
  "$macos_managed" \
  ".chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl" \
  "macOS includes always-run mise tool reconciliation"

assert_managed_paths_include_prefix \
  "$macos_managed" \
  ".chezmoiscripts/run_before_20-install-gh-extensions.sh.tmpl" \
  "macOS includes always-run GitHub CLI Extension Set install hook"

assert_managed_paths_include_prefix \
  "$macos_managed" \
  ".chezmoiscripts/run_before_30-install-shell-integrations.sh.tmpl" \
  "macOS includes always-run shell integration install hook"

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
  ".chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl" \
  "enableAiCliTools includes Optional AI Tool Stack installer"

assert_managed_paths_include_prefix \
  "$development_workspace_managed" \
  ".chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl" \
  "enableDevelopmentWorkspace includes Optional AI Tool Stack installer"
