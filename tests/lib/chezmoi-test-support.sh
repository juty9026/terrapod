# Callers provide repo_root and tmp_dir; preparation happens only on request.
# Keep helper config separate from any config a caller is exercising.
run_test_chezmoi() (
  source="$1"
  destination="$2"
  shift 2

  mkdir -p "$tmp_dir/chezmoi-test"
  : >"$tmp_dir/chezmoi-test/config.toml"
  chezmoi \
    --config "$tmp_dir/chezmoi-test/config.toml" \
    --source "$source" \
    --destination "$destination" \
    "$@"
)

managed_source_paths() {
  run_test_chezmoi "$repo_root" "$tmp_dir/home" \
    --override-data "$1" managed --path-style source-relative
}

managed_target_paths() {
  managed_target_paths_from_source "$1" "$repo_root"
}

managed_target_paths_from_source() {
  run_test_chezmoi "$2" "$tmp_dir/home" --override-data "$1" managed
}

render_template() {
  render_template_from_source "$1" "$2" "$repo_root"
}

render_template_from_source() {
  run_test_chezmoi "$3" "$tmp_dir/home" \
    execute-template --override-data "$1" --file "$3/$2"
}

render_template_with_homebrew_prefix_provider() (
  data="$1"
  file="$2"
  prefix="$3"
  source="$tmp_dir/homebrew-prefix-source-${file##*/}"

  rm -rf "$source"
  mkdir -p "$source"
  cp -R "$repo_root/." "$source"
  printf '%s\n' \
    '#!/bin/sh' \
    'TERRAPOD_HOMEBREW_PREFIX_LOADED=1' \
    'terrapod_standard_homebrew_prefix_for_os() {' \
    "  printf '%s\\n' '$prefix'" \
    '}' \
    >"$source/dot_local/lib/terrapod/homebrew-prefix.sh"

  render_template_from_source "$data" "$file" "$source"
)

render_managed_file() {
  run_test_chezmoi "$repo_root" "$tmp_dir/home" \
    --override-data "$1" cat "$tmp_dir/home/$2"
}

# Shared profile inputs used by rendering and managed-path tests.
ubuntu_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false}'
macos_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false}'
macos_terminal_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":true,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false}'
macos_automation_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":true,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false}'
macos_launcher_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":true,"enableMacosAppGroupMonitoring":false}'
macos_terminal_launcher_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":true,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":true,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false}'
macos_monitoring_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":true}'
macos_development_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":true}'
macos_mobile_dev_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false,"enableMacosAppGroupMobileDev":true,"enableMacosAppGroupBrowsers":false}'
macos_ai_cli_tools_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":true,"enableDevelopmentWorkspace":false}'
macos_development_workspace_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":true}'
ai_cli_tools_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":true,"enableDevelopmentWorkspace":false}'
development_workspace_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":true}'
