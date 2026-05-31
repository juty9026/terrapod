#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

assert_contains() {
  file="$1"
  needle="$2"
  message="$3"

  if ! grep -F "$needle" "$file" >/dev/null; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  file="$1"
  needle="$2"
  message="$3"

  if grep -F "$needle" "$file" >/dev/null; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    fail "$message"
  fi

  pass "$message"
}

extract_data_section() {
  file="$1"

  awk '
    function is_data_section(line) {
      return line ~ "^[[:space:]]*\\[[[:space:]]*(data|\"data\"|\047data\047)[[:space:]]*\\][[:space:]]*($|#)"
    }

    function is_section(line) {
      return line ~ /^[[:space:]]*(\[[^]]+\]|\[\[[^]]+\]\])[[:space:]]*($|#)/
    }

    {
      if (is_data_section($0)) {
        in_data = 1
        print
        next
      }

      if (in_data && is_section($0)) {
        exit
      }

      if (in_data) {
        print
      }
    }
  ' "$file"
}

assert_data_key_once_with_value() {
  file="$1"
  key="$2"
  expected_value="$3"
  message="$4"
  expected_line="$key = $expected_value"
  data_section="$(extract_data_section "$file")"

  actual_count="$(
    printf '%s\n' "$data_section" |
      grep -E "^[[:space:]]*(\"$key\"|'$key'|$key)[[:space:]]*=" |
      wc -l |
      tr -d ' '
  )"

  if [ "$actual_count" -ne 1 ]; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    printf '%s\n' "data section:" >&2
    printf '%s\n' "$data_section" | sed 's/^/  /' >&2
    fail "$message; expected one $key entry in [data], found $actual_count"
  fi

  if ! printf '%s\n' "$data_section" | grep -Fx "$expected_line" >/dev/null; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    printf '%s\n' "data section:" >&2
    printf '%s\n' "$data_section" | sed 's/^/  /' >&2
    fail "$message; expected $expected_line in [data]"
  fi

  pass "$message"
}

assert_lines_after_header_not_contains() {
  file="$1"
  header="$2"
  needle="$3"
  message="$4"

  if awk -v header="$header" '
    found {
      print
    }

    $0 == header {
      found = 1
    }
  ' "$file" | grep -F "$needle" >/dev/null; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    fail "$message"
  fi

  pass "$message"
}

file_mode() {
  path="$1"

  if mode="$(stat -c %a "$path" 2>/dev/null)"; then
    printf '%s\n' "$mode"
    return 0
  fi

  stat -f %Lp "$path"
}

assert_backup_count() {
  config_file="$1"
  expected_count="$2"
  message="$3"

  set -- "$config_file".terrapod-backup-*
  if [ "$1" = "$config_file.terrapod-backup-*" ]; then
    actual_count=0
  else
    actual_count=$#
  fi

  if [ "$actual_count" -ne "$expected_count" ]; then
    fail "$message; expected $expected_count backup(s), found $actual_count"
  fi

  pass "$message"
}

assert_single_backup_matches() {
  config_file="$1"
  expected_file="$2"
  message="$3"

  set -- "$config_file".terrapod-backup-*
  if [ "$1" = "$config_file.terrapod-backup-*" ]; then
    fail "$message; expected one backup, found 0"
  fi

  if [ "$#" -ne 1 ]; then
    fail "$message; expected one backup, found $#"
  fi

  if ! cmp -s "$1" "$expected_file"; then
    printf '%s\n' "expected backup contents:" >&2
    sed 's/^/  /' "$expected_file" >&2
    printf '%s\n' "actual backup contents:" >&2
    sed 's/^/  /' "$1" >&2
    fail "$message"
  fi

  pass "$message"
}

assert_no_terrapod_temp_files() {
  config_file="$1"
  message="$2"
  config_dir="$(dirname -- "$config_file")"
  found_file="$tmp_dir/found-temp-files"

  find "$config_dir" \
    \( -name '.terrapod-config.*' \
    -o -name '.terrapod-data.*' \
    -o -name "$(basename -- "$config_file").terrapod-tmp-*" \
    -o -name "$(basename -- "$config_file").terrapod-data-*" \) \
    -print >"$found_file"

  if [ -s "$found_file" ]; then
    printf '%s\n' "unexpected temp files:" >&2
    sed 's/^/  /' "$found_file" >&2
    fail "$message"
  fi

  pass "$message"
}

assert_no_terrapod_artifacts_near_path() {
  path="$1"
  message="$2"
  path_dir="$(dirname -- "$path")"
  path_base="$(basename -- "$path")"
  found_file="$tmp_dir/found-artifacts"

  if [ ! -d "$path_dir" ]; then
    pass "$message"
    return
  fi

  find "$path_dir" \
    \( -name '.terrapod-config.*' \
    -o -name '.terrapod-data.*' \
    -o -name "$path_base.terrapod-backup-*" \
    -o -name "$path_base.terrapod-tmp-*" \
    -o -name "$path_base.terrapod-data-*" \) \
    -print >"$found_file"

  if [ -s "$found_file" ]; then
    printf '%s\n' "unexpected Terrapod artifacts:" >&2
    sed 's/^/  /' "$found_file" >&2
    fail "$message"
  fi

  pass "$message"
}

run_terrapod_configure() {
  preset="$1"
  input="${2:-}"
  home_dir="$3"
  xdg_config_home="$4"

  if [ -n "$input" ]; then
    printf '%s\n' "$input" |
      TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" configure "$preset"
  else
    TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" configure "$preset" </dev/null
  fi
}

run_terrapod_setup() {
  profile="$1"
  input="$2"
  home_dir="$3"
  xdg_config_home="$4"

  printf '%s' "$input" |
    TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" setup
}

run_terrapod_setup_rich() {
  profile="$1"
  input="$2"
  home_dir="$3"
  xdg_config_home="$4"

  printf '%s' "$input" |
    TERRAPOD_SETUP_PRESENTATION=rich TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" setup
}

terrapod="$repo_root/dot_local/bin/executable_terrapod"

new_home="$tmp_dir/new-home"
new_xdg="$tmp_dir/new-xdg"
new_config="$new_xdg/chezmoi/chezmoi.toml"
mkdir -p "$new_home"

run_terrapod_configure minimal "" "$new_home" "$new_xdg"

if [ ! -f "$new_config" ]; then
  fail "minimal Preset creates a chezmoi config file"
fi
pass "minimal Preset creates a chezmoi config file"

assert_contains "$new_config" "[data]" "new config contains a data section"
assert_data_key_once_with_value "$new_config" "enableEditorStack" "false" "minimal Preset disables Optional Editor Stack exactly once in data"
assert_data_key_once_with_value "$new_config" "enableAiCliTools" "false" "minimal Preset disables Optional AI Tool Stack exactly once in data"
assert_data_key_once_with_value "$new_config" "enableDevelopmentWorkspace" "false" "minimal Preset disables Optional Development Workspace exactly once in data"
assert_data_key_once_with_value "$new_config" "enableMacosAppGroupTerminalApps" "false" "minimal Preset disables terminal-apps macOS App Group exactly once in data"
assert_data_key_once_with_value "$new_config" "enableMacosAppGroupAutomation" "false" "minimal Preset disables automation macOS App Group exactly once in data"
assert_data_key_once_with_value "$new_config" "enableMacosAppGroupLauncher" "false" "minimal Preset disables launcher macOS App Group exactly once in data"
assert_data_key_once_with_value "$new_config" "enableMacosAppGroupMonitoring" "false" "minimal Preset disables monitoring macOS App Group exactly once in data"
assert_not_contains "$new_config" "enableMacosDesktopApps" "minimal Preset does not write the legacy all-in desktop app toggle"
assert_not_contains "$new_config" "terrapodPreset" "minimal Preset stores concrete values instead of a dynamic Preset"
assert_backup_count "$new_config" 0 "new config creation does not create a backup"

development_home="$tmp_dir/development-home"
development_xdg="$tmp_dir/development-xdg"
development_config="$development_xdg/chezmoi/chezmoi.toml"
mkdir -p "$development_home"

run_terrapod_configure development "" "$development_home" "$development_xdg"

if [ ! -f "$development_config" ]; then
  fail "development Preset creates a chezmoi config file"
fi
pass "development Preset creates a chezmoi config file"

assert_data_key_once_with_value "$development_config" "enableEditorStack" "true" "development Preset enables Optional Editor Stack in a new config"
assert_data_key_once_with_value "$development_config" "enableAiCliTools" "true" "development Preset enables Optional AI Tool Stack in a new config"
assert_data_key_once_with_value "$development_config" "enableDevelopmentWorkspace" "true" "development Preset enables Optional Development Workspace in a new config"
assert_data_key_once_with_value "$development_config" "enableMacosAppGroupTerminalApps" "false" "development Preset disables terminal-apps macOS App Group in a new config"
assert_data_key_once_with_value "$development_config" "enableMacosAppGroupAutomation" "false" "development Preset disables automation macOS App Group in a new config"
assert_data_key_once_with_value "$development_config" "enableMacosAppGroupLauncher" "false" "development Preset disables launcher macOS App Group in a new config"
assert_data_key_once_with_value "$development_config" "enableMacosAppGroupMonitoring" "false" "development Preset disables monitoring macOS App Group in a new config"
assert_not_contains "$development_config" "enableMacosDesktopApps" "development Preset does not write the legacy all-in desktop app toggle"
assert_not_contains "$development_config" "terrapodPreset" "development Preset stores concrete values instead of a dynamic Preset"
assert_backup_count "$development_config" 0 "development config creation does not create a backup"

workstation_home="$tmp_dir/workstation-home"
workstation_xdg="$tmp_dir/workstation-xdg"
workstation_config="$workstation_xdg/chezmoi/chezmoi.toml"
mkdir -p "$workstation_home"

TERRAPOD_PROFILE=macos-terminal run_terrapod_configure workstation "" "$workstation_home" "$workstation_xdg"

if [ ! -f "$workstation_config" ]; then
  fail "workstation Preset creates a chezmoi config file"
fi
pass "workstation Preset creates a chezmoi config file"

assert_data_key_once_with_value "$workstation_config" "enableEditorStack" "true" "workstation Preset enables Optional Editor Stack exactly once in data"
assert_data_key_once_with_value "$workstation_config" "enableAiCliTools" "true" "workstation Preset enables Optional AI Tool Stack exactly once in data"
assert_data_key_once_with_value "$workstation_config" "enableDevelopmentWorkspace" "true" "workstation Preset enables Optional Development Workspace exactly once in data"
assert_data_key_once_with_value "$workstation_config" "enableMacosAppGroupTerminalApps" "true" "workstation Preset enables terminal-apps macOS App Group exactly once in data"
assert_data_key_once_with_value "$workstation_config" "enableMacosAppGroupAutomation" "true" "workstation Preset enables automation macOS App Group exactly once in data"
assert_data_key_once_with_value "$workstation_config" "enableMacosAppGroupLauncher" "true" "workstation Preset enables launcher macOS App Group exactly once in data"
assert_data_key_once_with_value "$workstation_config" "enableMacosAppGroupMonitoring" "true" "workstation Preset enables monitoring macOS App Group exactly once in data"
assert_not_contains "$workstation_config" "enableMacosDesktopApps" "workstation Preset does not write the legacy all-in desktop app toggle"
assert_not_contains "$workstation_config" "terrapodPreset" "workstation Preset stores concrete values instead of a dynamic Preset"
assert_backup_count "$workstation_config" 0 "workstation config creation does not create a backup"

setup_workstation_home="$tmp_dir/setup-workstation-home"
setup_workstation_xdg="$tmp_dir/setup-workstation-xdg"
setup_workstation_config="$setup_workstation_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_workstation_home"

if ! run_terrapod_setup macos-terminal 'workstation





y
' "$setup_workstation_home" "$setup_workstation_xdg" >"$tmp_dir/setup-workstation.out" 2>"$tmp_dir/setup-workstation.err"; then
  printf '%s\n' "setup stdout:" >&2
  sed 's/^/  /' "$tmp_dir/setup-workstation.out" >&2
  printf '%s\n' "setup stderr:" >&2
  sed 's/^/  /' "$tmp_dir/setup-workstation.err" >&2
  fail "confirmed setup accepts customization prompts before final confirmation"
fi
pass "confirmed setup accepts customization prompts before final confirmation"

if [ ! -f "$setup_workstation_config" ]; then
  fail "confirmed setup creates a chezmoi config file"
fi
pass "confirmed setup creates a chezmoi config file"

assert_data_key_once_with_value "$setup_workstation_config" "enableEditorStack" "true" "confirmed setup enables Optional Editor Stack exactly once in data"
assert_data_key_once_with_value "$setup_workstation_config" "enableAiCliTools" "true" "confirmed setup enables Optional AI Tool Stack exactly once in data"
assert_data_key_once_with_value "$setup_workstation_config" "enableDevelopmentWorkspace" "true" "confirmed setup enables Optional Development Workspace exactly once in data"
assert_data_key_once_with_value "$setup_workstation_config" "enableMacosAppGroupTerminalApps" "true" "confirmed setup enables terminal-apps macOS App Group exactly once in data"
assert_data_key_once_with_value "$setup_workstation_config" "enableMacosAppGroupAutomation" "true" "confirmed setup enables automation macOS App Group exactly once in data"
assert_data_key_once_with_value "$setup_workstation_config" "enableMacosAppGroupLauncher" "true" "confirmed setup enables launcher macOS App Group exactly once in data"
assert_data_key_once_with_value "$setup_workstation_config" "enableMacosAppGroupMonitoring" "true" "confirmed setup enables monitoring macOS App Group exactly once in data"
assert_not_contains "$setup_workstation_config" "enableMacosDesktopApps" "confirmed setup does not write the legacy all-in desktop app toggle"
assert_not_contains "$setup_workstation_config" "terrapodPreset" "confirmed setup stores concrete values instead of a dynamic Preset"
assert_backup_count "$setup_workstation_config" 0 "confirmed setup new config creation does not create a backup"

setup_custom_workspace_home="$tmp_dir/setup-custom-workspace-home"
setup_custom_workspace_xdg="$tmp_dir/setup-custom-workspace-xdg"
setup_custom_workspace_config="$setup_custom_workspace_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_custom_workspace_home"

if ! run_terrapod_setup macos-terminal 'minimal
y
n
y
n
y
y
' "$setup_custom_workspace_home" "$setup_custom_workspace_xdg" >"$tmp_dir/setup-custom-workspace.out" 2>"$tmp_dir/setup-custom-workspace.err"; then
  printf '%s\n' "setup stdout:" >&2
  sed 's/^/  /' "$tmp_dir/setup-custom-workspace.out" >&2
  printf '%s\n' "setup stderr:" >&2
  sed 's/^/  /' "$tmp_dir/setup-custom-workspace.err" >&2
  fail "macOS setup prompts for customization before final confirmation and customizes Optional Development Workspace and App Groups"
fi
pass "macOS setup prompts for customization before final confirmation and customizes Optional Development Workspace and App Groups"

assert_contains "$tmp_dir/setup-custom-workspace.err" "Optional Editor Stack: included by Optional Development Workspace" "workspace-enabled setup presents Optional Editor Stack as included"
assert_contains "$tmp_dir/setup-custom-workspace.err" "Optional AI Tool Stack: included by Optional Development Workspace" "workspace-enabled setup presents Optional AI Tool Stack as included"
assert_contains "$tmp_dir/setup-custom-workspace.out" "enableEditorStack = true" "workspace-enabled setup summary reflects included Optional Editor Stack"
assert_contains "$tmp_dir/setup-custom-workspace.out" "enableAiCliTools = true" "workspace-enabled setup summary reflects included Optional AI Tool Stack"
assert_contains "$tmp_dir/setup-custom-workspace.out" "enableDevelopmentWorkspace = true" "workspace-enabled setup summary reflects enabled Optional Development Workspace"
assert_contains "$tmp_dir/setup-custom-workspace.out" "enableMacosAppGroupTerminalApps = false" "macOS setup summary reflects customized terminal-apps App Group"
assert_contains "$tmp_dir/setup-custom-workspace.out" "enableMacosAppGroupAutomation = true" "macOS setup summary reflects customized automation App Group"
assert_contains "$tmp_dir/setup-custom-workspace.out" "enableMacosAppGroupLauncher = false" "macOS setup summary reflects customized launcher App Group"
assert_contains "$tmp_dir/setup-custom-workspace.out" "enableMacosAppGroupMonitoring = true" "macOS setup summary reflects customized monitoring App Group"

if [ ! -f "$setup_custom_workspace_config" ]; then
  fail "macOS customized setup creates a chezmoi config file"
fi
pass "macOS customized setup creates a chezmoi config file"

assert_data_key_once_with_value "$setup_custom_workspace_config" "enableEditorStack" "true" "workspace-enabled setup writes included Optional Editor Stack"
assert_data_key_once_with_value "$setup_custom_workspace_config" "enableAiCliTools" "true" "workspace-enabled setup writes included Optional AI Tool Stack"
assert_data_key_once_with_value "$setup_custom_workspace_config" "enableDevelopmentWorkspace" "true" "workspace-enabled setup writes enabled Optional Development Workspace"
assert_data_key_once_with_value "$setup_custom_workspace_config" "enableMacosAppGroupTerminalApps" "false" "workspace-enabled setup writes customized terminal-apps App Group"
assert_data_key_once_with_value "$setup_custom_workspace_config" "enableMacosAppGroupAutomation" "true" "workspace-enabled setup writes customized automation App Group"
assert_data_key_once_with_value "$setup_custom_workspace_config" "enableMacosAppGroupLauncher" "false" "workspace-enabled setup writes customized launcher App Group"
assert_data_key_once_with_value "$setup_custom_workspace_config" "enableMacosAppGroupMonitoring" "true" "workspace-enabled setup writes customized monitoring App Group"

rich_equivalent_home="$tmp_dir/rich-equivalent-home"
rich_equivalent_xdg="$tmp_dir/rich-equivalent-xdg"
rich_equivalent_config="$rich_equivalent_xdg/chezmoi/chezmoi.toml"
mkdir -p "$rich_equivalent_home"

if ! run_terrapod_setup_rich macos-terminal '1
t
j
n
j
y
j
n
j
y

y
' "$rich_equivalent_home" "$rich_equivalent_xdg" >"$tmp_dir/rich-equivalent.out" 2>"$tmp_dir/rich-equivalent.err"; then
  printf '%s\n' "rich setup stdout:" >&2
  sed 's/^/  /' "$tmp_dir/rich-equivalent.out" >&2
  printf '%s\n' "rich setup stderr:" >&2
  sed 's/^/  /' "$tmp_dir/rich-equivalent.err" >&2
  fail "rich setup customizes concrete settings and completes"
fi
pass "rich setup customizes concrete settings and completes"

assert_contains "$tmp_dir/rich-equivalent.err" "Terrapod Setup" "rich setup prints setup-only rich heading"
assert_contains "$tmp_dir/rich-equivalent.err" "terminal-apps macOS App Group (Ghostty)" "rich setup labels terminal-apps macOS App Group as Ghostty-only"
assert_data_key_once_with_value "$rich_equivalent_config" "enableEditorStack" "true" "rich setup writes included Optional Editor Stack"
assert_data_key_once_with_value "$rich_equivalent_config" "enableAiCliTools" "true" "rich setup writes included Optional AI Tool Stack"
assert_data_key_once_with_value "$rich_equivalent_config" "enableDevelopmentWorkspace" "true" "rich setup writes enabled Optional Development Workspace"
assert_data_key_once_with_value "$rich_equivalent_config" "enableMacosAppGroupTerminalApps" "false" "rich setup writes customized terminal-apps App Group"
assert_data_key_once_with_value "$rich_equivalent_config" "enableMacosAppGroupAutomation" "true" "rich setup writes customized automation App Group"
assert_data_key_once_with_value "$rich_equivalent_config" "enableMacosAppGroupLauncher" "false" "rich setup writes customized launcher App Group"
assert_data_key_once_with_value "$rich_equivalent_config" "enableMacosAppGroupMonitoring" "true" "rich setup writes customized monitoring App Group"

if ! cmp -s "$setup_custom_workspace_config" "$rich_equivalent_config"; then
  printf '%s\n' "plain setup config:" >&2
  sed 's/^/  /' "$setup_custom_workspace_config" >&2
  printf '%s\n' "rich setup config:" >&2
  sed 's/^/  /' "$rich_equivalent_config" >&2
  fail "rich setup writes the same concrete settings as equivalent plain setup choices"
fi
pass "rich setup writes the same concrete settings as equivalent plain setup choices"

rich_navigation_home="$tmp_dir/rich-navigation-home"
rich_navigation_xdg="$tmp_dir/rich-navigation-xdg"
rich_navigation_config="$rich_navigation_xdg/chezmoi/chezmoi.toml"
mkdir -p "$rich_navigation_home"

if ! run_terrapod_setup_rich macos-terminal 'j
j
k


y
' "$rich_navigation_home" "$rich_navigation_xdg" >"$tmp_dir/rich-navigation.out" 2>"$tmp_dir/rich-navigation.err"; then
  printf '%s\n' "rich navigation stdout:" >&2
  sed 's/^/  /' "$tmp_dir/rich-navigation.out" >&2
  printf '%s\n' "rich navigation stderr:" >&2
  sed 's/^/  /' "$tmp_dir/rich-navigation.err" >&2
  fail "rich Preset navigation selects development and completes"
fi
pass "rich Preset navigation selects development and completes"

assert_contains "$tmp_dir/rich-navigation.out" "Configured Terrapod Preset 'development'" "rich Preset navigation selects development"
assert_data_key_once_with_value "$rich_navigation_config" "enableEditorStack" "true" "rich navigation writes development Editor Stack setting"
assert_data_key_once_with_value "$rich_navigation_config" "enableAiCliTools" "true" "rich navigation writes development AI Tool Stack setting"
assert_data_key_once_with_value "$rich_navigation_config" "enableDevelopmentWorkspace" "true" "rich navigation writes development workspace setting"
assert_data_key_once_with_value "$rich_navigation_config" "enableMacosAppGroupTerminalApps" "false" "rich navigation keeps terminal-apps disabled for development"

rich_vps_home="$tmp_dir/rich-vps-home"
rich_vps_xdg="$tmp_dir/rich-vps-xdg"
rich_vps_config="$rich_vps_xdg/chezmoi/chezmoi.toml"
mkdir -p "$rich_vps_home"

if ! run_terrapod_setup_rich vps-shell 'minimal
j
y
j
n

y
' "$rich_vps_home" "$rich_vps_xdg" >"$tmp_dir/rich-vps.out" 2>"$tmp_dir/rich-vps.err"; then
  printf '%s\n' "rich VPS stdout:" >&2
  sed 's/^/  /' "$tmp_dir/rich-vps.out" >&2
  printf '%s\n' "rich VPS stderr:" >&2
  sed 's/^/  /' "$tmp_dir/rich-vps.err" >&2
  fail "rich VPS setup customizes optional stacks without macOS App Groups"
fi
pass "rich VPS setup customizes optional stacks without macOS App Groups"

rich_vps_combined="$(cat "$tmp_dir/rich-vps.out" "$tmp_dir/rich-vps.err")"
if printf '%s\n' "$rich_vps_combined" | grep -F "terminal-apps macOS App Group" >/dev/null; then
  fail "rich VPS setup does not show macOS App Group items"
fi
pass "rich VPS setup does not show macOS App Group items"

assert_data_key_once_with_value "$rich_vps_config" "enableEditorStack" "true" "rich VPS setup writes customized Optional Editor Stack"
assert_data_key_once_with_value "$rich_vps_config" "enableAiCliTools" "false" "rich VPS setup writes customized Optional AI Tool Stack"
assert_data_key_once_with_value "$rich_vps_config" "enableDevelopmentWorkspace" "false" "rich VPS setup writes disabled Optional Development Workspace"
assert_data_key_once_with_value "$rich_vps_config" "enableMacosAppGroupTerminalApps" "false" "rich VPS setup writes terminal-apps App Group disabled"
assert_data_key_once_with_value "$rich_vps_config" "enableMacosAppGroupAutomation" "false" "rich VPS setup writes automation App Group disabled"
assert_data_key_once_with_value "$rich_vps_config" "enableMacosAppGroupLauncher" "false" "rich VPS setup writes launcher App Group disabled"
assert_data_key_once_with_value "$rich_vps_config" "enableMacosAppGroupMonitoring" "false" "rich VPS setup writes monitoring App Group disabled"

setup_leaf_home="$tmp_dir/setup-leaf-home"
setup_leaf_xdg="$tmp_dir/setup-leaf-xdg"
setup_leaf_config="$setup_leaf_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_leaf_home"

if ! run_terrapod_setup macos-terminal 'development
n
n
y
y
n
y
n
y
' "$setup_leaf_home" "$setup_leaf_xdg" >"$tmp_dir/setup-leaf.out" 2>"$tmp_dir/setup-leaf.err"; then
  printf '%s\n' "setup stdout:" >&2
  sed 's/^/  /' "$tmp_dir/setup-leaf.out" >&2
  printf '%s\n' "setup stderr:" >&2
  sed 's/^/  /' "$tmp_dir/setup-leaf.err" >&2
  fail "workspace-disabled setup prompts for customization before final confirmation and customizes leaf stacks independently"
fi
pass "workspace-disabled setup prompts for customization before final confirmation and customizes leaf stacks independently"

assert_contains "$tmp_dir/setup-leaf.out" "enableEditorStack = false" "workspace-disabled setup summary reflects customized Optional Editor Stack"
assert_contains "$tmp_dir/setup-leaf.out" "enableAiCliTools = true" "workspace-disabled setup summary reflects customized Optional AI Tool Stack"
assert_contains "$tmp_dir/setup-leaf.out" "enableDevelopmentWorkspace = false" "workspace-disabled setup summary reflects disabled Optional Development Workspace"

if [ ! -f "$setup_leaf_config" ]; then
  fail "workspace-disabled customized setup creates a chezmoi config file"
fi
pass "workspace-disabled customized setup creates a chezmoi config file"

assert_data_key_once_with_value "$setup_leaf_config" "enableEditorStack" "false" "workspace-disabled setup writes customized Optional Editor Stack"
assert_data_key_once_with_value "$setup_leaf_config" "enableAiCliTools" "true" "workspace-disabled setup writes customized Optional AI Tool Stack"
assert_data_key_once_with_value "$setup_leaf_config" "enableDevelopmentWorkspace" "false" "workspace-disabled setup writes disabled Optional Development Workspace"
assert_data_key_once_with_value "$setup_leaf_config" "enableMacosAppGroupTerminalApps" "true" "workspace-disabled setup writes customized terminal-apps App Group"
assert_data_key_once_with_value "$setup_leaf_config" "enableMacosAppGroupAutomation" "false" "workspace-disabled setup writes customized automation App Group"
assert_data_key_once_with_value "$setup_leaf_config" "enableMacosAppGroupLauncher" "true" "workspace-disabled setup writes customized launcher App Group"
assert_data_key_once_with_value "$setup_leaf_config" "enableMacosAppGroupMonitoring" "false" "workspace-disabled setup writes customized monitoring App Group"

setup_vps_custom_home="$tmp_dir/setup-vps-custom-home"
setup_vps_custom_xdg="$tmp_dir/setup-vps-custom-xdg"
setup_vps_custom_config="$setup_vps_custom_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_vps_custom_home"

if ! run_terrapod_setup vps-shell 'minimal
n
y
n
y
' "$setup_vps_custom_home" "$setup_vps_custom_xdg" >"$tmp_dir/setup-vps-custom.out" 2>"$tmp_dir/setup-vps-custom.err"; then
  printf '%s\n' "setup stdout:" >&2
  sed 's/^/  /' "$tmp_dir/setup-vps-custom.out" >&2
  printf '%s\n' "setup stderr:" >&2
  sed 's/^/  /' "$tmp_dir/setup-vps-custom.err" >&2
  fail "VPS setup prompts for customization before final confirmation and customizes optional stacks without macOS App Groups"
fi
pass "VPS setup prompts for customization before final confirmation and customizes optional stacks without macOS App Groups"

setup_vps_custom_output="$(cat "$tmp_dir/setup-vps-custom.out" "$tmp_dir/setup-vps-custom.err")"
if printf '%s\n' "$setup_vps_custom_output" | grep -F "terminal-apps macOS App Group" >/dev/null; then
  fail "VPS setup does not prompt for terminal-apps macOS App Group"
fi
pass "VPS setup does not prompt for terminal-apps macOS App Group"

assert_contains "$tmp_dir/setup-vps-custom.err" "macOS App Groups: not applicable for VPS Shell Profile" "VPS setup explains macOS App Groups are not applicable"
assert_contains "$tmp_dir/setup-vps-custom.out" "enableEditorStack = true" "VPS setup summary reflects customized Optional Editor Stack"
assert_contains "$tmp_dir/setup-vps-custom.out" "enableAiCliTools = false" "VPS setup summary reflects customized Optional AI Tool Stack"
assert_contains "$tmp_dir/setup-vps-custom.out" "enableDevelopmentWorkspace = false" "VPS setup summary reflects disabled Optional Development Workspace"

if [ ! -f "$setup_vps_custom_config" ]; then
  fail "VPS customized setup creates a chezmoi config file"
fi
pass "VPS customized setup creates a chezmoi config file"

assert_data_key_once_with_value "$setup_vps_custom_config" "enableEditorStack" "true" "VPS setup writes customized Optional Editor Stack"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableAiCliTools" "false" "VPS setup writes customized Optional AI Tool Stack"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableDevelopmentWorkspace" "false" "VPS setup writes disabled Optional Development Workspace"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableMacosAppGroupTerminalApps" "false" "VPS setup writes terminal-apps App Group disabled"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableMacosAppGroupAutomation" "false" "VPS setup writes automation App Group disabled"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableMacosAppGroupLauncher" "false" "VPS setup writes launcher App Group disabled"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableMacosAppGroupMonitoring" "false" "VPS setup writes monitoring App Group disabled"

setup_vps_workstation_error_home="$tmp_dir/setup-vps-workstation-error-home"
setup_vps_workstation_error_xdg="$tmp_dir/setup-vps-workstation-error-xdg"
setup_vps_workstation_error_config="$setup_vps_workstation_error_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_vps_workstation_error_home"

if run_terrapod_setup vps-shell 'workstation
y
' "$setup_vps_workstation_error_home" "$setup_vps_workstation_error_xdg" >"$tmp_dir/setup-vps-workstation-error.out" 2>"$tmp_dir/setup-vps-workstation-error.err"; then
  fail "VPS setup workstation rejection exits non-zero"
fi
pass "VPS setup workstation rejection exits non-zero"

assert_contains "$tmp_dir/setup-vps-workstation-error.err" "workstation Preset is only available for the macOS Terminal Profile" "VPS setup workstation rejection writes the error to stderr"
assert_not_contains "$tmp_dir/setup-vps-workstation-error.out" "workstation Preset is only available for the macOS Terminal Profile" "VPS setup workstation rejection does not write the error to stdout"

if [ -e "$setup_vps_workstation_error_config" ]; then
  fail "VPS setup workstation rejection does not create a config"
fi
pass "VPS setup workstation rejection does not create a config"

assert_no_terrapod_artifacts_near_path "$setup_vps_workstation_error_config" "VPS setup workstation rejection leaves no Terrapod artifacts"

setup_invalid_bool_home="$tmp_dir/setup-invalid-bool-home"
setup_invalid_bool_xdg="$tmp_dir/setup-invalid-bool-xdg"
setup_invalid_bool_config="$setup_invalid_bool_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_invalid_bool_home"

if run_terrapod_setup macos-terminal 'minimal
maybe
' "$setup_invalid_bool_home" "$setup_invalid_bool_xdg" >"$tmp_dir/setup-invalid-bool.out" 2>"$tmp_dir/setup-invalid-bool.err"; then
  fail "setup invalid boolean answer exits non-zero"
fi
pass "setup invalid boolean answer exits non-zero"

assert_contains "$tmp_dir/setup-invalid-bool.err" "invalid answer for Optional Development Workspace; enter y or n" "setup invalid boolean answer writes the error to stderr"
assert_not_contains "$tmp_dir/setup-invalid-bool.out" "invalid answer for Optional Development Workspace; enter y or n" "setup invalid boolean answer does not write the error to stdout"

if [ -e "$setup_invalid_bool_config" ]; then
  fail "setup invalid boolean answer does not create a config"
fi
pass "setup invalid boolean answer does not create a config"

assert_no_terrapod_artifacts_near_path "$setup_invalid_bool_config" "setup invalid boolean answer leaves no Terrapod artifacts"

setup_cancel_home="$tmp_dir/setup-cancel-home"
setup_cancel_xdg="$tmp_dir/setup-cancel-xdg"
setup_cancel_config="$setup_cancel_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_cancel_home"

if run_terrapod_setup macos-terminal 'development





n
' "$setup_cancel_home" "$setup_cancel_xdg" >"$tmp_dir/setup-cancel.out" 2>"$tmp_dir/setup-cancel.err"; then
  fail "cancelled setup exits non-zero"
fi
pass "cancelled setup exits non-zero"

assert_contains "$tmp_dir/setup-cancel.err" "setup cancelled" "cancelled setup explains cancellation"

if [ -e "$setup_cancel_config" ]; then
  fail "cancelled setup does not create a new config"
fi
pass "cancelled setup does not create a new config"

assert_no_terrapod_artifacts_near_path "$setup_cancel_config" "cancelled setup leaves no Terrapod artifacts near new config path"

setup_existing_cancel_home="$tmp_dir/setup-existing-cancel-home"
setup_existing_cancel_xdg="$tmp_dir/setup-existing-cancel-xdg"
setup_existing_cancel_config="$setup_existing_cancel_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_existing_cancel_home" "$(dirname "$setup_existing_cancel_config")"

cat >"$setup_existing_cancel_config" <<'TOML'
[data]
email = "minu@example.com"
enableEditorStack = false
TOML
cp "$setup_existing_cancel_config" "$tmp_dir/setup-existing-cancel-before.toml"

if run_terrapod_setup macos-terminal 'development







' "$setup_existing_cancel_home" "$setup_existing_cancel_xdg" >"$tmp_dir/setup-existing-cancel.out" 2>"$tmp_dir/setup-existing-cancel.err"; then
  fail "empty final confirmation cancels setup"
fi
pass "empty final confirmation cancels setup"

assert_contains "$tmp_dir/setup-existing-cancel.err" "setup cancelled" "empty final confirmation explains cancellation"

if ! cmp -s "$setup_existing_cancel_config" "$tmp_dir/setup-existing-cancel-before.toml"; then
  fail "cancelled setup leaves existing config unchanged"
fi
pass "cancelled setup leaves existing config unchanged"

assert_backup_count "$setup_existing_cancel_config" 0 "cancelled setup does not create a backup for existing config"
assert_no_terrapod_temp_files "$setup_existing_cancel_config" "cancelled setup leaves no Terrapod temp files for existing config"

default_env_home="$tmp_dir/default-env-home"
default_env_xdg="$tmp_dir/default-env-xdg"
default_env_config="$default_env_xdg/chezmoi/chezmoi.toml"
escape_config="$tmp_dir/escape.toml"
mkdir -p "$default_env_home"

export TERRAPOD_CHEZMOI_CONFIG="$escape_config"
run_terrapod_configure minimal "" "$default_env_home" "$default_env_xdg"
unset TERRAPOD_CHEZMOI_CONFIG

if [ ! -f "$default_env_config" ]; then
  fail "default-path configure ignores exported TERRAPOD_CHEZMOI_CONFIG"
fi
pass "default-path configure ignores exported TERRAPOD_CHEZMOI_CONFIG"

if [ -e "$escape_config" ]; then
  fail "default-path configure does not write to exported TERRAPOD_CHEZMOI_CONFIG"
fi
pass "default-path configure does not write to exported TERRAPOD_CHEZMOI_CONFIG"

existing_home="$tmp_dir/existing-home"
existing_xdg="$tmp_dir/existing-xdg"
existing_config="$existing_xdg/chezmoi/chezmoi.toml"
mkdir -p "$existing_home" "$(dirname "$existing_config")"

cat >"$existing_config" <<'TOML'
[sourceState]
branch = "main"

[data]
email = "minu@example.com"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosDesktopApps = true
terrapodPreset = "minimal"

[edit]
command = "nvim"
TOML
cp "$existing_config" "$tmp_dir/existing-before.toml"

run_terrapod_configure development "y" "$existing_home" "$existing_xdg"

assert_contains "$existing_config" "branch = \"main\"" "existing update preserves unrelated sourceState values"
assert_contains "$existing_config" "email = \"minu@example.com\"" "existing update preserves unrelated data values"
assert_contains "$existing_config" "command = \"nvim\"" "existing update preserves unrelated later sections"
assert_data_key_once_with_value "$existing_config" "enableEditorStack" "true" "development Preset enables Optional Editor Stack exactly once in data"
assert_data_key_once_with_value "$existing_config" "enableAiCliTools" "true" "development Preset enables Optional AI Tool Stack exactly once in data"
assert_data_key_once_with_value "$existing_config" "enableDevelopmentWorkspace" "true" "development Preset enables Optional Development Workspace exactly once in data"
assert_data_key_once_with_value "$existing_config" "enableMacosAppGroupTerminalApps" "false" "development Preset disables terminal-apps macOS App Group exactly once in data"
assert_data_key_once_with_value "$existing_config" "enableMacosAppGroupAutomation" "false" "development Preset disables automation macOS App Group exactly once in data"
assert_data_key_once_with_value "$existing_config" "enableMacosAppGroupLauncher" "false" "development Preset disables launcher macOS App Group exactly once in data"
assert_data_key_once_with_value "$existing_config" "enableMacosAppGroupMonitoring" "false" "development Preset disables monitoring macOS App Group exactly once in data"
assert_not_contains "$existing_config" "enableMacosDesktopApps" "existing update removes the legacy all-in desktop app toggle"
assert_not_contains "$existing_config" "terrapodPreset" "existing update removes stale dynamic Preset key"
assert_single_backup_matches "$existing_config" "$tmp_dir/existing-before.toml" "existing update creates one backup before changing managed keys"

quoted_table_home="$tmp_dir/quoted-table-home"
quoted_table_xdg="$tmp_dir/quoted-table-xdg"
quoted_table_config="$quoted_table_xdg/chezmoi/chezmoi.toml"
mkdir -p "$quoted_table_home" "$(dirname "$quoted_table_config")"

cat >"$quoted_table_config" <<'TOML'
["data"]
keepQuotedTable = "preserve"
enableEditorStack = false
enableMacosDesktopApps = true
terrapodPreset = "minimal"

[sourceState]
branch = "main"
TOML

run_terrapod_configure development "y" "$quoted_table_home" "$quoted_table_xdg"

assert_contains "$quoted_table_config" "[\"data\"]" "quoted data table header is preserved"
assert_not_contains "$quoted_table_config" "[data]" "quoted data table update does not append a duplicate bare data table"
assert_contains "$quoted_table_config" "keepQuotedTable = \"preserve\"" "quoted data table update preserves unrelated data values"
assert_contains "$quoted_table_config" "branch = \"main\"" "quoted data table update preserves later sections"
assert_data_key_once_with_value "$quoted_table_config" "enableEditorStack" "true" "quoted data table update writes Editor Stack in data"
assert_data_key_once_with_value "$quoted_table_config" "enableAiCliTools" "true" "quoted data table update writes AI Tool Stack in data"
assert_data_key_once_with_value "$quoted_table_config" "enableDevelopmentWorkspace" "true" "quoted data table update writes Development Workspace in data"
assert_data_key_once_with_value "$quoted_table_config" "enableMacosAppGroupTerminalApps" "false" "quoted data table update writes terminal-apps App Group in data"
assert_data_key_once_with_value "$quoted_table_config" "enableMacosAppGroupAutomation" "false" "quoted data table update writes automation App Group in data"
assert_data_key_once_with_value "$quoted_table_config" "enableMacosAppGroupLauncher" "false" "quoted data table update writes launcher App Group in data"
assert_data_key_once_with_value "$quoted_table_config" "enableMacosAppGroupMonitoring" "false" "quoted data table update writes monitoring App Group in data"
assert_not_contains "$quoted_table_config" "enableMacosDesktopApps" "quoted data table update removes the legacy all-in desktop app toggle"
assert_not_contains "$quoted_table_config" "terrapodPreset" "quoted data table update removes stale dynamic Preset key"

spaced_table_home="$tmp_dir/spaced-table-home"
spaced_table_xdg="$tmp_dir/spaced-table-xdg"
spaced_table_config="$spaced_table_xdg/chezmoi/chezmoi.toml"
mkdir -p "$spaced_table_home" "$(dirname "$spaced_table_config")"

cat >"$spaced_table_config" <<'TOML'
[ data ]
keepSpacedTable = "preserve"
enableEditorStack = false
enableMacosDesktopApps = true
terrapodPreset = "minimal"

[sourceState]
branch = "main"
TOML

run_terrapod_configure development "y" "$spaced_table_home" "$spaced_table_xdg"

assert_contains "$spaced_table_config" "[ data ]" "spaced data table header is preserved"
assert_not_contains "$spaced_table_config" "[data]" "spaced data table update does not append a duplicate bare data table"
assert_contains "$spaced_table_config" "keepSpacedTable = \"preserve\"" "spaced data table update preserves unrelated data values"
assert_contains "$spaced_table_config" "branch = \"main\"" "spaced data table update preserves later sections"
assert_data_key_once_with_value "$spaced_table_config" "enableEditorStack" "true" "spaced data table update writes Editor Stack in data"
assert_data_key_once_with_value "$spaced_table_config" "enableAiCliTools" "true" "spaced data table update writes AI Tool Stack in data"
assert_data_key_once_with_value "$spaced_table_config" "enableDevelopmentWorkspace" "true" "spaced data table update writes Development Workspace in data"
assert_data_key_once_with_value "$spaced_table_config" "enableMacosAppGroupTerminalApps" "false" "spaced data table update writes terminal-apps App Group in data"
assert_data_key_once_with_value "$spaced_table_config" "enableMacosAppGroupAutomation" "false" "spaced data table update writes automation App Group in data"
assert_data_key_once_with_value "$spaced_table_config" "enableMacosAppGroupLauncher" "false" "spaced data table update writes launcher App Group in data"
assert_data_key_once_with_value "$spaced_table_config" "enableMacosAppGroupMonitoring" "false" "spaced data table update writes monitoring App Group in data"
assert_not_contains "$spaced_table_config" "enableMacosDesktopApps" "spaced data table update removes the legacy all-in desktop app toggle"
assert_not_contains "$spaced_table_config" "terrapodPreset" "spaced data table update removes stale dynamic Preset key"

dotted_home="$tmp_dir/dotted-home"
dotted_xdg="$tmp_dir/dotted-xdg"
dotted_config="$dotted_xdg/chezmoi/chezmoi.toml"
mkdir -p "$dotted_home" "$(dirname "$dotted_config")"

cat >"$dotted_config" <<'TOML'
data.email = "minu@example.com"
data.enableEditorStack = false
data.enableAiCliTools = false
data.enableDevelopmentWorkspace = false
data.enableMacosDesktopApps = true
data.terrapodPreset = "minimal"

[sourceState]
branch = "main"
TOML

run_terrapod_configure development "y" "$dotted_home" "$dotted_xdg"

assert_not_contains "$dotted_config" "[data]" "dotted data update does not append a duplicate data table"
assert_contains "$dotted_config" "data.email = \"minu@example.com\"" "dotted data update preserves unrelated data values"
assert_contains "$dotted_config" "branch = \"main\"" "dotted data update preserves later sections"
assert_contains "$dotted_config" "data.enableEditorStack = true" "dotted data update writes Editor Stack as a dotted data key"
assert_contains "$dotted_config" "data.enableAiCliTools = true" "dotted data update writes AI Tool Stack as a dotted data key"
assert_contains "$dotted_config" "data.enableDevelopmentWorkspace = true" "dotted data update writes Development Workspace as a dotted data key"
assert_contains "$dotted_config" "data.enableMacosAppGroupTerminalApps = false" "dotted data update writes terminal-apps App Group as a dotted data key"
assert_contains "$dotted_config" "data.enableMacosAppGroupAutomation = false" "dotted data update writes automation App Group as a dotted data key"
assert_contains "$dotted_config" "data.enableMacosAppGroupLauncher = false" "dotted data update writes launcher App Group as a dotted data key"
assert_contains "$dotted_config" "data.enableMacosAppGroupMonitoring = false" "dotted data update writes monitoring App Group as a dotted data key"
assert_not_contains "$dotted_config" "data.enableMacosDesktopApps" "dotted data update removes the legacy all-in desktop app toggle"
assert_not_contains "$dotted_config" "data.terrapodPreset" "dotted data update removes stale dynamic Preset key"

if ! HOME="$dotted_home" XDG_CONFIG_HOME="$dotted_xdg" chezmoi --config "$dotted_config" data >"$tmp_dir/dotted-data.out" 2>"$tmp_dir/dotted-data.err"; then
  printf '%s\n' "stderr:" >&2
  sed 's/^/  /' "$tmp_dir/dotted-data.err" >&2
  fail "dotted data update leaves a chezmoi-parseable config"
fi
pass "dotted data update leaves a chezmoi-parseable config"

quoted_home="$tmp_dir/quoted-home"
quoted_xdg="$tmp_dir/quoted-xdg"
quoted_config="$quoted_xdg/chezmoi/chezmoi.toml"
mkdir -p "$quoted_home" "$(dirname "$quoted_config")"

cat >"$quoted_config" <<'TOML'
[data]
'enableEditorStack' = false
'enableAiCliTools' = false
'enableDevelopmentWorkspace' = false
'enableMacosDesktopApps' = true
'terrapodPreset' = "minimal"
keepLiteral = "preserve"
TOML

run_terrapod_configure development "y" "$quoted_home" "$quoted_xdg"

assert_data_key_once_with_value "$quoted_config" "enableEditorStack" "true" "quoted managed key update writes one Editor Stack value in data"
assert_data_key_once_with_value "$quoted_config" "enableAiCliTools" "true" "quoted managed key update writes one AI Tool Stack value in data"
assert_data_key_once_with_value "$quoted_config" "enableDevelopmentWorkspace" "true" "quoted managed key update writes one Development Workspace value in data"
assert_data_key_once_with_value "$quoted_config" "enableMacosAppGroupTerminalApps" "false" "quoted managed key update writes one terminal-apps App Group value in data"
assert_data_key_once_with_value "$quoted_config" "enableMacosAppGroupAutomation" "false" "quoted managed key update writes one automation App Group value in data"
assert_data_key_once_with_value "$quoted_config" "enableMacosAppGroupLauncher" "false" "quoted managed key update writes one launcher App Group value in data"
assert_data_key_once_with_value "$quoted_config" "enableMacosAppGroupMonitoring" "false" "quoted managed key update writes one monitoring App Group value in data"
assert_not_contains "$quoted_config" "\"enableEditorStack\"" "quoted managed key update removes quoted Editor Stack key"
assert_not_contains "$quoted_config" "\"enableAiCliTools\"" "quoted managed key update removes quoted AI Tool Stack key"
assert_not_contains "$quoted_config" "\"enableDevelopmentWorkspace\"" "quoted managed key update removes quoted Development Workspace key"
assert_not_contains "$quoted_config" "\"enableMacosDesktopApps\"" "quoted managed key update removes quoted macOS desktop-app boundary key"
assert_not_contains "$quoted_config" "'enableEditorStack'" "quoted managed key update removes literal Editor Stack key"
assert_not_contains "$quoted_config" "'enableAiCliTools'" "quoted managed key update removes literal AI Tool Stack key"
assert_not_contains "$quoted_config" "'enableDevelopmentWorkspace'" "quoted managed key update removes literal Development Workspace key"
assert_not_contains "$quoted_config" "'enableMacosDesktopApps'" "quoted managed key update removes literal macOS desktop-app boundary key"
assert_not_contains "$quoted_config" "terrapodPreset" "quoted managed key update removes stale dynamic Preset key"
assert_contains "$quoted_config" "keepLiteral = \"preserve\"" "quoted managed key update preserves unrelated literal data values"

array_home="$tmp_dir/array-home"
array_xdg="$tmp_dir/array-xdg"
array_config="$array_xdg/chezmoi/chezmoi.toml"
mkdir -p "$array_home" "$(dirname "$array_config")"

cat >"$array_config" <<'TOML'
[data]
keepMe = "yes"
enableEditorStack = false

[[merge.command]]
name = "external"
enableEditorStack = "do-not-touch"
TOML
chmod 600 "$array_config"

run_terrapod_configure development "y" "$array_home" "$array_xdg"

assert_contains "$array_config" "keepMe = \"yes\"" "array-table update preserves unrelated data values"
assert_data_key_once_with_value "$array_config" "enableEditorStack" "true" "array-table update writes Editor Stack only in data"
assert_data_key_once_with_value "$array_config" "enableAiCliTools" "true" "array-table update writes AI Tool Stack only in data"
assert_data_key_once_with_value "$array_config" "enableDevelopmentWorkspace" "true" "array-table update writes Development Workspace only in data"
assert_data_key_once_with_value "$array_config" "enableMacosAppGroupTerminalApps" "false" "array-table update writes terminal-apps App Group only in data"
assert_data_key_once_with_value "$array_config" "enableMacosAppGroupAutomation" "false" "array-table update writes automation App Group only in data"
assert_data_key_once_with_value "$array_config" "enableMacosAppGroupLauncher" "false" "array-table update writes launcher App Group only in data"
assert_data_key_once_with_value "$array_config" "enableMacosAppGroupMonitoring" "false" "array-table update writes monitoring App Group only in data"
assert_contains "$array_config" "[[merge.command]]" "array-table update preserves TOML array table"
assert_contains "$array_config" "enableEditorStack = \"do-not-touch\"" "array-table update preserves same-named external key"
assert_lines_after_header_not_contains "$array_config" "[[merge.command]]" "enableAiCliTools = true" "array-table update does not append AI Tool Stack under array table"
assert_lines_after_header_not_contains "$array_config" "[[merge.command]]" "enableDevelopmentWorkspace = true" "array-table update does not append Development Workspace under array table"
assert_lines_after_header_not_contains "$array_config" "[[merge.command]]" "enableMacosAppGroupTerminalApps = false" "array-table update does not append terminal-apps App Group under array table"
assert_lines_after_header_not_contains "$array_config" "[[merge.command]]" "enableMacosAppGroupAutomation = false" "array-table update does not append automation App Group under array table"
assert_lines_after_header_not_contains "$array_config" "[[merge.command]]" "enableMacosAppGroupLauncher = false" "array-table update does not append launcher App Group under array table"
assert_lines_after_header_not_contains "$array_config" "[[merge.command]]" "enableMacosAppGroupMonitoring = false" "array-table update does not append monitoring App Group under array table"

array_mode="$(file_mode "$array_config")"
if [ "$array_mode" != "600" ]; then
  fail "existing update preserves config file mode; expected 600, got $array_mode"
fi
pass "existing update preserves config file mode"

assert_no_terrapod_temp_files "$array_config" "successful array-table update cleans Terrapod temp files"

signers_home="$tmp_dir/signers-home"
signers_xdg="$tmp_dir/signers-xdg"
signers_config="$signers_xdg/chezmoi/chezmoi.toml"
mkdir -p "$signers_home" "$(dirname "$signers_config")"

cat >"$signers_config" <<'TOML'
[data]
gitAllowedSigners = [
  "name@company.com ssh-ed25519 AAAA_COMPANY_PUBLIC_KEY company",
]
enableEditorStack = false
enableAiCliTools = false

[sourceState]
branch = "main"
TOML

run_terrapod_configure development "y" "$signers_home" "$signers_xdg"

assert_contains "$signers_config" "gitAllowedSigners = [" "documented signer array update preserves array header"
assert_contains "$signers_config" "name@company.com ssh-ed25519 AAAA_COMPANY_PUBLIC_KEY company" "documented signer array update preserves signer value"
assert_data_key_once_with_value "$signers_config" "enableEditorStack" "true" "documented signer array update writes Editor Stack in data"
assert_data_key_once_with_value "$signers_config" "enableAiCliTools" "true" "documented signer array update writes AI Tool Stack in data"
assert_data_key_once_with_value "$signers_config" "enableDevelopmentWorkspace" "true" "documented signer array update writes Development Workspace in data"
assert_data_key_once_with_value "$signers_config" "enableMacosAppGroupTerminalApps" "false" "documented signer array update writes terminal-apps App Group in data"
assert_data_key_once_with_value "$signers_config" "enableMacosAppGroupAutomation" "false" "documented signer array update writes automation App Group in data"
assert_data_key_once_with_value "$signers_config" "enableMacosAppGroupLauncher" "false" "documented signer array update writes launcher App Group in data"
assert_data_key_once_with_value "$signers_config" "enableMacosAppGroupMonitoring" "false" "documented signer array update writes monitoring App Group in data"
assert_contains "$signers_config" "branch = \"main\"" "documented signer array update preserves later sections"

multiline_array_home="$tmp_dir/multiline-array-home"
multiline_array_xdg="$tmp_dir/multiline-array-xdg"
multiline_array_config="$multiline_array_xdg/chezmoi/chezmoi.toml"
mkdir -p "$multiline_array_home" "$(dirname "$multiline_array_config")"

cat >"$multiline_array_config" <<'TOML'
[data]
keepMe = "yes"
matrix = [
[1, 2]
]
enableEditorStack = false
enableAiCliTools = false

[sourceState]
branch = "main"
TOML
cp "$multiline_array_config" "$tmp_dir/multiline-array-before.toml"

if printf '%s\n' "y" |
  HOME="$multiline_array_home" XDG_CONFIG_HOME="$multiline_array_xdg" sh "$terrapod" configure development >"$tmp_dir/multiline-array.out" 2>"$tmp_dir/multiline-array.err"; then
  fail "section-like multiline array config is rejected before rewriting"
fi
pass "section-like multiline array config is rejected before rewriting"

assert_contains "$tmp_dir/multiline-array.err" "unsupported multiline array" "section-like multiline array rejection explains unsupported format"
assert_not_contains "$tmp_dir/multiline-array.err" "Update Terrapod-managed data keys" "section-like multiline array rejection does not prompt before failing"
assert_not_contains "$tmp_dir/multiline-array.out" "Configured Terrapod Preset" "section-like multiline array rejection does not report success"

if ! cmp -s "$multiline_array_config" "$tmp_dir/multiline-array-before.toml"; then
  fail "section-like multiline array rejection leaves existing config unchanged"
fi
pass "section-like multiline array rejection leaves existing config unchanged"

assert_backup_count "$multiline_array_config" 0 "section-like multiline array rejection does not create a backup"
assert_no_terrapod_temp_files "$multiline_array_config" "section-like multiline array rejection leaves no Terrapod temp files"

fake_stat_home="$tmp_dir/fake-stat-home"
fake_stat_xdg="$tmp_dir/fake-stat-xdg"
fake_stat_config="$fake_stat_xdg/chezmoi/chezmoi.toml"
fake_stat_bin="$tmp_dir/fake-stat-bin"
mkdir -p "$fake_stat_home" "$(dirname "$fake_stat_config")" "$fake_stat_bin"

cat >"$fake_stat_config" <<'TOML'
[data]
enableEditorStack = false
TOML
chmod 600 "$fake_stat_config"

cat >"$fake_stat_bin/stat" <<'SH'
#!/bin/sh

if [ "${1:-}" = "-f" ]; then
  printf '%s\n' "GNU stat filesystem output that is not a file mode"
  exit 1
fi

if [ "${1:-}" = "-c" ] && [ "${2:-}" = "%a" ]; then
  printf '%s\n' "600"
  exit 0
fi

printf '%s\n' "unexpected stat invocation: $*" >&2
exit 2
SH
chmod +x "$fake_stat_bin/stat"

if ! printf '%s\n' "y" |
  PATH="$fake_stat_bin:$PATH" HOME="$fake_stat_home" XDG_CONFIG_HOME="$fake_stat_xdg" sh "$terrapod" configure development >"$tmp_dir/fake-stat.out" 2>"$tmp_dir/fake-stat.err"; then
  printf '%s\n' "stdout:" >&2
  sed 's/^/  /' "$tmp_dir/fake-stat.out" >&2
  printf '%s\n' "stderr:" >&2
  sed 's/^/  /' "$tmp_dir/fake-stat.err" >&2
  fail "existing update ignores stdout from failing GNU stat -f fallback"
fi
pass "existing update ignores stdout from failing GNU stat -f fallback"

fake_stat_mode="$(
  PATH="$fake_stat_bin:$PATH"
  file_mode "$fake_stat_config"
)"
if [ "$fake_stat_mode" != "600" ]; then
  fail "existing update preserves mode with GNU-first stat fallback; expected 600, got $fake_stat_mode"
fi
pass "existing update preserves mode with GNU-first stat fallback"

inline_table_home="$tmp_dir/inline-table-home"
inline_table_xdg="$tmp_dir/inline-table-xdg"
inline_table_config="$inline_table_xdg/chezmoi/chezmoi.toml"
mkdir -p "$inline_table_home" "$(dirname "$inline_table_config")"

cat >"$inline_table_config" <<'TOML'
data = { email = "minu@example.com", enableEditorStack = false }

[sourceState]
branch = "main"
TOML
cp "$inline_table_config" "$tmp_dir/inline-table-before.toml"

if printf '%s\n' "y" |
  HOME="$inline_table_home" XDG_CONFIG_HOME="$inline_table_xdg" sh "$terrapod" configure development >"$tmp_dir/inline-table.out" 2>"$tmp_dir/inline-table.err"; then
  fail "inline data table config is rejected instead of rewritten"
fi
pass "inline data table config is rejected instead of rewritten"

assert_contains "$tmp_dir/inline-table.err" "unsupported inline data table" "inline data table rejection explains unsupported format"
assert_not_contains "$tmp_dir/inline-table.err" "Update Terrapod-managed data keys" "inline data table rejection does not prompt before failing"
assert_not_contains "$tmp_dir/inline-table.out" "Configured Terrapod Preset" "inline data table rejection does not report success"

if ! cmp -s "$inline_table_config" "$tmp_dir/inline-table-before.toml"; then
  fail "inline data table rejection leaves existing config unchanged"
fi
pass "inline data table rejection leaves existing config unchanged"

assert_backup_count "$inline_table_config" 0 "inline data table rejection does not create a backup"
assert_no_terrapod_temp_files "$inline_table_config" "inline data table rejection leaves no Terrapod temp files"

multiline_string_home="$tmp_dir/multiline-string-home"
multiline_string_xdg="$tmp_dir/multiline-string-xdg"
multiline_string_config="$multiline_string_xdg/chezmoi/chezmoi.toml"
mkdir -p "$multiline_string_home" "$(dirname "$multiline_string_config")"

cat >"$multiline_string_config" <<'TOML'
notes = '''
[data]
enableEditorStack = false
keep this line in the string
'''

[sourceState]
branch = "main"
TOML
cp "$multiline_string_config" "$tmp_dir/multiline-string-before.toml"

if printf '%s\n' "y" |
  HOME="$multiline_string_home" XDG_CONFIG_HOME="$multiline_string_xdg" sh "$terrapod" configure development >"$tmp_dir/multiline-string.out" 2>"$tmp_dir/multiline-string.err"; then
  fail "multiline string with data-like content is rejected instead of rewritten"
fi
pass "multiline string with data-like content is rejected instead of rewritten"

assert_contains "$tmp_dir/multiline-string.err" "unsupported multiline string" "multiline string rejection explains unsupported format"
assert_not_contains "$tmp_dir/multiline-string.err" "Update Terrapod-managed data keys" "multiline string rejection does not prompt before failing"
assert_not_contains "$tmp_dir/multiline-string.out" "Configured Terrapod Preset" "multiline string rejection does not report success"

if ! cmp -s "$multiline_string_config" "$tmp_dir/multiline-string-before.toml"; then
  fail "multiline string rejection leaves existing config unchanged"
fi
pass "multiline string rejection leaves existing config unchanged"

assert_backup_count "$multiline_string_config" 0 "multiline string rejection does not create a backup"
assert_no_terrapod_temp_files "$multiline_string_config" "multiline string rejection leaves no Terrapod temp files"

dotted_multiline_home="$tmp_dir/dotted-multiline-home"
dotted_multiline_xdg="$tmp_dir/dotted-multiline-xdg"
dotted_multiline_config="$dotted_multiline_xdg/chezmoi/chezmoi.toml"
mkdir -p "$dotted_multiline_home" "$(dirname "$dotted_multiline_config")"

cat >"$dotted_multiline_config" <<'TOML'
data.email = "minu@example.com"
data.enableEditorStack = false
notes = '''
[profile]
keep this line in the string
'''
TOML
cp "$dotted_multiline_config" "$tmp_dir/dotted-multiline-before.toml"

if printf '%s\n' "y" |
  HOME="$dotted_multiline_home" XDG_CONFIG_HOME="$dotted_multiline_xdg" sh "$terrapod" configure development >"$tmp_dir/dotted-multiline.out" 2>"$tmp_dir/dotted-multiline.err"; then
  fail "dotted data update rejects multiline strings before injecting managed keys"
fi
pass "dotted data update rejects multiline strings before injecting managed keys"

assert_contains "$tmp_dir/dotted-multiline.err" "unsupported multiline string" "dotted multiline rejection explains unsupported format"
assert_not_contains "$tmp_dir/dotted-multiline.err" "Update Terrapod-managed data keys" "dotted multiline rejection does not prompt before failing"
assert_not_contains "$tmp_dir/dotted-multiline.out" "Configured Terrapod Preset" "dotted multiline rejection does not report success"

if ! cmp -s "$dotted_multiline_config" "$tmp_dir/dotted-multiline-before.toml"; then
  fail "dotted multiline rejection leaves existing config unchanged"
fi
pass "dotted multiline rejection leaves existing config unchanged"

assert_backup_count "$dotted_multiline_config" 0 "dotted multiline rejection does not create a backup"
assert_no_terrapod_temp_files "$dotted_multiline_config" "dotted multiline rejection leaves no Terrapod temp files"

dotted_array_multiline_home="$tmp_dir/dotted-array-multiline-home"
dotted_array_multiline_xdg="$tmp_dir/dotted-array-multiline-xdg"
dotted_array_multiline_config="$dotted_array_multiline_xdg/chezmoi/chezmoi.toml"
mkdir -p "$dotted_array_multiline_home" "$(dirname "$dotted_array_multiline_config")"

cat >"$dotted_array_multiline_config" <<'TOML'
data.email = "minu@example.com"
data.enableEditorStack = false
notes = [
'''
[profile]
keep this line in the string
'''
]
TOML
cp "$dotted_array_multiline_config" "$tmp_dir/dotted-array-multiline-before.toml"

if printf '%s\n' "y" |
  HOME="$dotted_array_multiline_home" XDG_CONFIG_HOME="$dotted_array_multiline_xdg" sh "$terrapod" configure development >"$tmp_dir/dotted-array-multiline.out" 2>"$tmp_dir/dotted-array-multiline.err"; then
  fail "dotted data update rejects multiline strings in arrays before injecting managed keys"
fi
pass "dotted data update rejects multiline strings in arrays before injecting managed keys"

assert_contains "$tmp_dir/dotted-array-multiline.err" "unsupported multiline string" "dotted array multiline rejection explains unsupported format"
assert_not_contains "$tmp_dir/dotted-array-multiline.err" "Update Terrapod-managed data keys" "dotted array multiline rejection does not prompt before failing"
assert_not_contains "$tmp_dir/dotted-array-multiline.out" "Configured Terrapod Preset" "dotted array multiline rejection does not report success"

if ! cmp -s "$dotted_array_multiline_config" "$tmp_dir/dotted-array-multiline-before.toml"; then
  fail "dotted array multiline rejection leaves existing config unchanged"
fi
pass "dotted array multiline rejection leaves existing config unchanged"

assert_backup_count "$dotted_array_multiline_config" 0 "dotted array multiline rejection does not create a backup"
assert_no_terrapod_temp_files "$dotted_array_multiline_config" "dotted array multiline rejection leaves no Terrapod temp files"

decline_home="$tmp_dir/decline-home"
decline_xdg="$tmp_dir/decline-xdg"
decline_config="$decline_xdg/chezmoi/chezmoi.toml"
mkdir -p "$decline_home" "$(dirname "$decline_config")"

cat >"$decline_config" <<'TOML'
[data]
enableEditorStack = false
keepMe = "yes"
TOML
cp "$decline_config" "$tmp_dir/decline-before.toml"

if run_terrapod_configure development "n" "$decline_home" "$decline_xdg" >"$tmp_dir/decline.out" 2>"$tmp_dir/decline.err"; then
  fail "existing config update can be declined"
fi
pass "existing config update can be declined"

if ! cmp -s "$decline_config" "$tmp_dir/decline-before.toml"; then
  fail "declined config update leaves existing config unchanged"
fi
pass "declined config update leaves existing config unchanged"

assert_contains "$tmp_dir/decline.err" "Update Terrapod-managed data keys" "existing config update asks before writing"
assert_backup_count "$decline_config" 0 "declined config update does not create a backup"

directory_home="$tmp_dir/directory-home"
directory_xdg="$tmp_dir/directory-xdg"
directory_config="$directory_xdg/chezmoi/chezmoi.toml"
mkdir -p "$directory_home" "$directory_config"

if TERRAPOD_CHEZMOI_CONFIG="$directory_config" HOME="$directory_home" XDG_CONFIG_HOME="$directory_xdg" sh "$terrapod" configure minimal >"$tmp_dir/directory.out" 2>"$tmp_dir/directory.err" </dev/null; then
  fail "config path that is a directory fails explicitly"
fi
pass "config path that is a directory fails explicitly"

assert_contains "$tmp_dir/directory.err" "not a regular file" "directory config path explains it is not usable"
assert_not_contains "$tmp_dir/directory.out" "Configured Terrapod Preset" "directory config path does not report success"
assert_no_terrapod_temp_files "$directory_config" "directory config path leaves no Terrapod temp files"

symlink_home="$tmp_dir/symlink-home"
symlink_xdg="$tmp_dir/symlink-xdg"
symlink_dir="$tmp_dir/symlink-config-dir"
symlink_target="$symlink_dir/target.toml"
symlink_config="$symlink_dir/symlink_config.toml"
mkdir -p "$symlink_home" "$symlink_dir"

cat >"$symlink_target" <<'TOML'
[data]
enableEditorStack = false
TOML
chmod 600 "$symlink_target"
cp "$symlink_target" "$tmp_dir/symlink-target-before.toml"
symlink_target_mode="$(file_mode "$symlink_target")"
ln -s "$symlink_target" "$symlink_config"

if TERRAPOD_CHEZMOI_CONFIG="$symlink_config" HOME="$symlink_home" XDG_CONFIG_HOME="$symlink_xdg" sh "$terrapod" configure development >"$tmp_dir/symlink.out" 2>"$tmp_dir/symlink.err" </dev/null; then
  fail "config path that is a symlink to a regular file fails explicitly"
fi
pass "config path that is a symlink to a regular file fails explicitly"

assert_contains "$tmp_dir/symlink.err" "not a regular file" "symlink config path explains it is not usable"
assert_not_contains "$tmp_dir/symlink.err" "Update Terrapod-managed data keys" "symlink config path does not prompt before failing"
assert_not_contains "$tmp_dir/symlink.err" "config update cancelled" "symlink config path does not report a declined update"

if [ ! -L "$symlink_config" ]; then
  fail "symlink config path remains a symlink after failed update"
fi
pass "symlink config path remains a symlink after failed update"

if ! cmp -s "$symlink_target" "$tmp_dir/symlink-target-before.toml"; then
  fail "symlink config target remains unchanged after failed update"
fi
pass "symlink config target remains unchanged after failed update"

symlink_target_mode_after="$(file_mode "$symlink_target")"
if [ "$symlink_target_mode_after" != "$symlink_target_mode" ]; then
  fail "symlink config target mode remains unchanged; expected $symlink_target_mode, got $symlink_target_mode_after"
fi
pass "symlink config target mode remains unchanged"

assert_no_terrapod_artifacts_near_path "$symlink_config" "symlink config path leaves no Terrapod artifacts"
assert_no_terrapod_artifacts_near_path "$symlink_target" "symlink config target leaves no Terrapod artifacts"

dangling_home="$tmp_dir/dangling-home"
dangling_xdg="$tmp_dir/dangling-xdg"
dangling_dir="$tmp_dir/dangling-config-dir"
dangling_config="$dangling_dir/dangling_config.toml"
dangling_target="$dangling_dir/missing-target.toml"
mkdir -p "$dangling_home" "$dangling_dir"
ln -s "$dangling_target" "$dangling_config"

if TERRAPOD_CHEZMOI_CONFIG="$dangling_config" HOME="$dangling_home" XDG_CONFIG_HOME="$dangling_xdg" sh "$terrapod" configure minimal >"$tmp_dir/dangling.out" 2>"$tmp_dir/dangling.err" </dev/null; then
  fail "config path that is a dangling symlink fails explicitly"
fi
pass "config path that is a dangling symlink fails explicitly"

assert_contains "$tmp_dir/dangling.err" "not a regular file" "dangling symlink config path explains it is not usable"

if [ ! -L "$dangling_config" ]; then
  fail "dangling symlink config path remains a symlink after failed update"
fi
pass "dangling symlink config path remains a symlink after failed update"

if [ -e "$dangling_target" ]; then
  fail "dangling symlink target remains missing after failed update"
fi
pass "dangling symlink target remains missing after failed update"

assert_no_terrapod_artifacts_near_path "$dangling_config" "dangling symlink config path leaves no Terrapod artifacts"

empty_preset_home="$tmp_dir/empty-preset-home"
empty_preset_xdg="$tmp_dir/empty-preset-xdg"
empty_preset_config="$empty_preset_xdg/chezmoi/chezmoi.toml"
mkdir -p "$empty_preset_home" "$(dirname "$empty_preset_config")"

cat >"$empty_preset_config" <<'TOML'
[data]
enableEditorStack = false
keepMe = "yes"
TOML
cp "$empty_preset_config" "$tmp_dir/empty-preset-before.toml"

if HOME="$empty_preset_home" XDG_CONFIG_HOME="$empty_preset_xdg" sh "$terrapod" configure "" >"$tmp_dir/empty-preset.out" 2>"$tmp_dir/empty-preset.err" </dev/null; then
  fail "empty Preset fails before prompting"
else
  empty_preset_status="$?"
fi

if [ "$empty_preset_status" -ne 64 ]; then
  fail "empty Preset exits with usage status 64; got $empty_preset_status"
fi
pass "empty Preset exits with usage status 64"

assert_contains "$tmp_dir/empty-preset.err" "Preset is required" "empty Preset reports required Preset"
assert_not_contains "$tmp_dir/empty-preset.err" "unknown Preset" "empty Preset does not report unknown Preset"
assert_not_contains "$tmp_dir/empty-preset.err" "Update Terrapod-managed data keys" "empty Preset does not prompt before failing"
assert_not_contains "$tmp_dir/empty-preset.out" "Update Terrapod-managed data keys" "empty Preset does not prompt on stdout before failing"
assert_not_contains "$tmp_dir/empty-preset.out" "Configured Terrapod Preset" "empty Preset does not report success"

if ! cmp -s "$empty_preset_config" "$tmp_dir/empty-preset-before.toml"; then
  fail "empty Preset leaves existing config unchanged"
fi
pass "empty Preset leaves existing config unchanged"

assert_backup_count "$empty_preset_config" 0 "empty Preset does not create a backup"
assert_no_terrapod_temp_files "$empty_preset_config" "empty Preset leaves no Terrapod temp files"

invalid_home="$tmp_dir/invalid-home"
invalid_xdg="$tmp_dir/invalid-xdg"
invalid_config="$invalid_xdg/chezmoi/chezmoi.toml"
mkdir -p "$invalid_home" "$(dirname "$invalid_config")"

cat >"$invalid_config" <<'TOML'
[data]
enableEditorStack = false
keepMe = "yes"
TOML
cp "$invalid_config" "$tmp_dir/invalid-before.toml"

if HOME="$invalid_home" XDG_CONFIG_HOME="$invalid_xdg" sh "$terrapod" configure typo >"$tmp_dir/invalid.out" 2>"$tmp_dir/invalid.err" </dev/null; then
  fail "invalid Preset fails before prompting"
else
  invalid_status="$?"
fi

if [ "$invalid_status" -ne 64 ]; then
  fail "invalid Preset exits with usage status 64; got $invalid_status"
fi
pass "invalid Preset exits with usage status 64"

assert_contains "$tmp_dir/invalid.err" "unknown Preset: typo" "invalid Preset names rejected Preset"
assert_not_contains "$tmp_dir/invalid.err" "Update Terrapod-managed data keys" "invalid Preset does not prompt before failing"

if ! cmp -s "$invalid_config" "$tmp_dir/invalid-before.toml"; then
  fail "invalid Preset leaves existing config unchanged"
fi
pass "invalid Preset leaves existing config unchanged"

assert_backup_count "$invalid_config" 0 "invalid Preset does not create a backup"
assert_no_terrapod_temp_files "$invalid_config" "invalid Preset leaves no Terrapod temp files"
