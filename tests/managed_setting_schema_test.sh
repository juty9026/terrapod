#!/bin/sh
set -eu

# Structure tests for the Managed Setting schema. The schema row in the managed
# config reader is the one place a setting is declared; these tests keep every
# other site in the command, the reader, and the chezmoi templates derived from
# it. Behavior of the derived output (Setup, configure, status) is covered by
# terrapod_config_test.sh and terrapod_command_test.sh.

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

terrapod="$repo_root/dot_local/bin/executable_terrapod"
reader="$repo_root/dot_local/lib/terrapod/config-toml.sh"
desktop_apps_template="$repo_root/Brewfile.macos-desktop-apps.tmpl"
app_group_partial="$repo_root/.chezmoitemplates/any-macos-app-group-enabled"
reconcile_script="$repo_root/.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl"
karabiner_script="$repo_root/.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl"

# The command answers through its own test hook, so this list is whatever the
# schema derives and not a second copy of it.
print_setup_keys() {
  TERRAPOD_PRINT_MANAGED_SETUP_KEYS=1 /bin/sh "$1"
}

setup_keys="$(print_setup_keys "$terrapod")"
[ -n "$setup_keys" ] || fail "managed setup keys are readable from the Terrapod command"
schema_keys="$(printf '%s\n' "$setup_keys" | grep -Fxv profile)"
pass "managed setup keys are readable from the Terrapod command"

# --- Key literals live on their schema row ---------------------------------
#
# Every occurrence of a Managed Setting key in the command or the reader, by
# the top-level function that holds it. The schema function holds each key
# exactly once (its row); any other function has to be on the allowlist below,
# with the reason it needs the literal.

key_sites() {
  site_file="$1"
  site_key="$2"

  awk -v key="$site_key" '
    /^[A-Za-z_][A-Za-z0-9_]*\(\) *\{/ { fn = $0; sub(/\(.*/, "", fn) }
    index($0, key) { print (fn == "" ? "(top level)" : fn) }
    /^}/ { fn = "" }
  ' "$site_file"
}

# file|function|key|reason. The file is "terrapod" (the command) or "reader".
# Each row is behavior that names a specific setting, not a place the setting
# is declared, so it cannot be derived from a row.
allowed_sites_file="$tmp_dir/allowed-sites"
cat >"$allowed_sites_file" <<'ALLOWLIST'
terrapod|prompt_for_setup_settings|enableDevelopmentWorkspace|Workspace implication block (ADR 0002): the Workspace answer decides the two stacks it bundles
terrapod|prompt_for_setup_settings|enableEditorStack|Workspace implication block (ADR 0002)
terrapod|prompt_for_setup_settings|enableAiCliTools|Workspace implication block (ADR 0002)
terrapod|show_setup_option_block|enableDevelopmentWorkspace|per-key Setup notes: the Includes list of the Optional Development Workspace
terrapod|show_setup_option_block|enableEditorStack|per-key Setup notes: the Includes list of the Optional Development Workspace
terrapod|show_setup_option_block|enableAiCliTools|per-key Setup notes: the Includes list of the Optional Development Workspace
terrapod|show_setup_option_block|enableMacosAppGroupDevelopmentApps|per-key Setup notes: cask trust note
terrapod|show_setup_option_block|enableMacosAppGroupMobileDev|per-key Setup notes: environment, trust, and scope notes
terrapod|ai_cli_tools_supported_profile|enableAiCliTools|profile applicability of the AI Tool Stack, asked of the schema
terrapod|run_executable_selection|enableAiCliTools|the executable selection helper takes the evaluated AI Tool Stack state as an argument
terrapod|run_executable_selection|enableMacosAppGroupLauncher|the executable selection helper takes the launcher state as an argument
terrapod|run_status|enableEditorStack|status formats this setting from the shared readiness result
terrapod|run_status|enableAiCliTools|status formats this setting and checks agy, claude, and codex from the shared readiness result
terrapod|run_status|enableDevelopmentWorkspace|status formats this setting from the shared readiness result
terrapod|show_stack_context|enableEditorStack|apply and diff context format this setting from the shared readiness result
terrapod|show_stack_context|enableAiCliTools|apply and diff context format this setting from the shared readiness result
terrapod|show_stack_context|enableDevelopmentWorkspace|apply and diff context format this setting from the shared readiness result
terrapod|run_doctor|enableEditorStack|doctor formats this setting from the shared readiness result
terrapod|run_doctor|enableAiCliTools|doctor formats this setting from the shared readiness result
terrapod|run_doctor|enableDevelopmentWorkspace|doctor formats this setting from the shared readiness result
terrapod|effective_optional_stack_enabled|enableDevelopmentWorkspace|the Workspace turns on every optional stack it bundles
ALLOWLIST

allowed_sites="$tmp_dir/allowed-sites.keys"
cut -d'|' -f1-3 "$allowed_sites_file" | sort -u >"$allowed_sites"

actual_sites="$tmp_dir/actual-sites"
: >"$actual_sites"
schema_function_problems="$tmp_dir/schema-function-problems"
: >"$schema_function_problems"

for key in $schema_keys; do
  for site in terrapod reader; do
    case "$site" in
      terrapod) site_path="$terrapod" ;;
      reader) site_path="$reader" ;;
    esac

    key_sites "$site_path" "$key" >"$tmp_dir/key-sites"

    if [ "$site" = "reader" ]; then
      schema_row_count="$(grep -Fxc managed_setting_schema "$tmp_dir/key-sites" || true)"
      if [ "$schema_row_count" -ne 1 ]; then
        printf '%s\n' "$key appears $schema_row_count times in managed_setting_schema (want exactly one row)" >>"$schema_function_problems"
      fi
      grep -Fxv managed_setting_schema "$tmp_dir/key-sites" >"$tmp_dir/key-sites.other" || true
      mv "$tmp_dir/key-sites.other" "$tmp_dir/key-sites"
    fi

    sort -u "$tmp_dir/key-sites" | while IFS= read -r function_name; do
      [ -n "$function_name" ] || continue
      printf '%s|%s|%s\n' "$site" "$function_name" "$key"
    done >>"$actual_sites"
  done
done
sort -u "$actual_sites" -o "$actual_sites"

if [ -s "$schema_function_problems" ]; then
  cat "$schema_function_problems" >&2
  fail "every Managed Setting key is declared once, on its schema row"
fi
pass "every Managed Setting key is declared once, on its schema row"

unlisted_sites="$(grep -Fxv -f "$allowed_sites" "$actual_sites" || true)"
if [ -n "$unlisted_sites" ]; then
  printf '%s\n' "$unlisted_sites" | sed 's/^/  /' >&2
  fail "Managed Setting key literals outside the schema row are on the allowlist (file|function|key)"
fi
pass "Managed Setting key literals outside the schema row are on the allowlist"

stale_entries="$(grep -Fxv -f "$actual_sites" "$allowed_sites" || true)"
if [ -n "$stale_entries" ]; then
  printf '%s\n' "$stale_entries" | sed 's/^/  /' >&2
  fail "every allowlist entry still names a key literal that exists"
fi
pass "every allowlist entry still names a key literal that exists"

missing_reason="$(awk -F'|' 'NF < 4 || $4 == "" { print }' "$allowed_sites_file")"
[ -z "$missing_reason" ] || fail "every allowlist entry states a reason"
pass "every allowlist entry states a reason"

# --- Preset names in the schema are Presets the command knows --------------
#
# The Preset column is free text in a schema row; a typo there would silently
# leave a setting off everywhere. Every name must be one configure accepts.

schema_preset_names="$(
  sh -c '. "$1"; managed_setting_schema' sh "$reader" |
    awk -F'|' '{ n = split($5, names, ","); for (i = 1; i <= n; i++) print names[i] }' |
    sort -u
)"
[ -n "$schema_preset_names" ] || fail "the schema lists at least one Preset"

for preset_name in $schema_preset_names; do
  preset_config="$tmp_dir/preset-$preset_name.toml"
  if ! TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$preset_config" HOME="$tmp_dir" \
    /bin/sh "$terrapod" configure "$preset_name" >/dev/null 2>&1; then
    fail "the schema names a Preset the command knows: $preset_name"
  fi
done
pass "every Preset named in the schema is a Preset the command knows"

case " $schema_preset_names " in
  *" minimal "*) fail "minimal appears in no schema row" ;;
esac
pass "minimal appears in no schema row"

# --- Retired keys are data, not literals -----------------------------------

retired_keys="enableMacosAppGroupAiApps enableMacosDesktopApps terrapodPreset"
retired_reader_keys="$(
  sh -c '. "$1"; retired_managed_setting_keys' sh "$reader"
)"

for retired_key in $retired_keys; do
  assert_contains "$retired_reader_keys" "$retired_key" \
    "the reader lists retired key $retired_key"
  assert_file_not_contains "$terrapod" "$retired_key" \
    "the command does not hardcode retired key $retired_key"
done

# --- No eval, no positional settings renderer ------------------------------

assert_file_not_contains "$terrapod" 'eval ' "the command does not use eval"
assert_file_not_contains "$reader" 'eval ' "the reader does not use eval"
assert_file_not_contains "$terrapod" '${10}' \
  "the command has no settings renderer taking positional per-setting parameters"

# --- Templates stay in step with the schema --------------------------------

app_group_keys_of() {
  grep -o 'enableMacosAppGroup[A-Za-z]*' "$1" | sort -u
}

schema_app_group_keys="$(printf '%s\n' "$schema_keys" | grep '^enableMacosAppGroup' | sort -u)"

[ -f "$app_group_partial" ] || fail "the any-macOS-App-Group partial exists at .chezmoitemplates/any-macos-app-group-enabled"

if [ "$(app_group_keys_of "$desktop_apps_template")" != "$schema_app_group_keys" ]; then
  fail "the desktop-app Brewfile template block set equals the schema's macOS App Groups"
fi
pass "the desktop-app Brewfile template block set equals the schema's macOS App Groups"

if [ "$(app_group_keys_of "$app_group_partial")" != "$schema_app_group_keys" ]; then
  fail "the any-macOS-App-Group partial names exactly the schema's macOS App Groups"
fi
pass "the any-macOS-App-Group partial names exactly the schema's macOS App Groups"

for gate_script in "$reconcile_script" "$karabiner_script"; do
  gate_name="${gate_script##*/}"
  assert_file_contains "$gate_script" 'any-macos-app-group-enabled' \
    "$gate_name includes the any-macOS-App-Group partial"
  assert_file_not_contains "$gate_script" 'get . "enableMacosAppGroup' \
    "$gate_name does not repeat the macOS App Group list"
done

# --- Adding a group is adding one row --------------------------------------
#
# A copy of the reader with one more macOS App Group row must reach the key
# list, Preset expansion, and status without touching the command.

extra_lib_dir="$tmp_dir/extra-lib"
mkdir -p "$extra_lib_dir"
awk '
  /^ROWS$/ && !added {
    print "enableMacosAppGroupSchemaProbe|macos-app-group|schema-probe|macos-terminal|workstation|Probe and Fixture"
    added = 1
  }
  { print }
' "$reader" >"$extra_lib_dir/config-toml.sh"

probe_keys="$(TERRAPOD_CONFIG_TOML_LIB="$extra_lib_dir/config-toml.sh" print_setup_keys "$terrapod")"
assert_contains "$probe_keys" "enableMacosAppGroupSchemaProbe" \
  "a new schema row joins the managed setup keys"

probe_home="$tmp_dir/probe-home"
mkdir -p "$probe_home"
probe_config="$probe_home/chezmoi.toml"

TERRAPOD_CONFIG_TOML_LIB="$extra_lib_dir/config-toml.sh" \
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$probe_config" HOME="$probe_home" \
  /bin/sh "$terrapod" configure workstation >/dev/null
assert_file_contains "$probe_config" "enableMacosAppGroupSchemaProbe = true" \
  "a new schema row is enabled by the Presets it lists"

probe_status="$(
  TERRAPOD_CONFIG_TOML_LIB="$extra_lib_dir/config-toml.sh" \
    TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$probe_config" HOME="$probe_home" \
    PATH="/usr/bin:/bin" /bin/sh "$terrapod" status 2>&1 || true
)"
assert_contains "$probe_status" "schema-probe" \
  "a new schema row is reported by status"
assert_contains "$probe_status" "Probe and Fixture" \
  "status describes a macOS App Group from its schema summary"

TERRAPOD_CONFIG_TOML_LIB="$extra_lib_dir/config-toml.sh" \
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$probe_config" HOME="$probe_home" \
  /bin/sh "$terrapod" configure development --yes >/dev/null
assert_file_contains "$probe_config" "enableMacosAppGroupSchemaProbe = false" \
  "a new schema row stays disabled for the Presets it omits"

probe_vps_config="$probe_home/vps.toml"
TERRAPOD_CONFIG_TOML_LIB="$extra_lib_dir/config-toml.sh" \
  TERRAPOD_PROFILE=vps-shell TERRAPOD_CHEZMOI_CONFIG="$probe_vps_config" HOME="$probe_home" \
  /bin/sh "$terrapod" configure development >/dev/null
assert_file_contains "$probe_vps_config" "enableMacosAppGroupSchemaProbe = false" \
  "a schema row that does not apply to the profile is written false"
