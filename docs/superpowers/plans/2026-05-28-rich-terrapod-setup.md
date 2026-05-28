# Rich Terrapod Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add progressive rich terminal presentation to Terrapod Setup prompts while preserving the existing plain setup fallback and concrete saved settings.

**Architecture:** Keep config rendering and writing unchanged. Add setup-only presentation detection, rich input/state transition helpers, and rich prompt wrappers inside the existing POSIX shell command. The rich path returns the same Preset name and `render_setup_data` output shape as the plain path, so downstream summary, confirmation, and config writes stay shared.

**Tech Stack:** POSIX `sh`, ANSI terminal sequences only inside setup rich prompt helpers, existing shell integration tests under `tests/`.

---

## File Structure

- Modify: `dot_local/bin/executable_terrapod`
  - Add setup presentation detection helpers.
  - Split existing plain prompt helpers into explicit plain helpers plus wrapper functions.
  - Add rich Preset selection state transitions and rich setting customization state transitions.
  - Keep `render_setup_data`, `show_setup_settings_summary`, `confirm_setup_write`, and `write_setup_settings` unchanged.
- Modify: `tests/terrapod_command_test.sh`
  - Add assertions that auto mode keeps the plain fallback for piped setup input and `TERM=dumb`.
  - Add rich forced-mode command-output coverage that checks for setup-only rich presentation markers without asserting full frames.
  - Add routine output guard coverage for no ANSI color/emoji in non-setup commands.
- Modify: `tests/terrapod_config_test.sh`
  - Add rich forced-mode config equivalence tests for Preset navigation and concrete setting customization.
  - Add rich state transition tests through the command surface with line-oriented key commands.
- Create: `docs/superpowers/plans/2026-05-28-rich-terrapod-setup.md`
  - This plan.

---

### Task 1: Add Rich Setup Detection and Plain Fallback Tests

**Files:**
- Modify: `tests/terrapod_command_test.sh`
- Test: `tests/terrapod_command_test.sh`

- [ ] **Step 1: Add ANSI and emoji guard helpers**

Add these helpers after `assert_not_contains()`:

```sh
assert_no_ansi_escape() {
  haystack="$1"
  message="$2"
  escape_char="$(printf '\033')"

  if printf '%s\n' "$haystack" | grep -F "$escape_char" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_no_rich_setup_emoji() {
  haystack="$1"
  message="$2"

  if printf '%s\n' "$haystack" | grep -E '🌱|✨|▸' >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}
```

- [ ] **Step 2: Add a setup runner that can set presentation mode**

Add this helper after `run_terrapod_setup_command()`:

```sh
run_terrapod_setup_command_with_presentation() {
  presentation="$1"
  profile="$2"
  input="$3"
  home_dir="$4"
  xdg_config_home="$5"
  output_file="$6"

  printf '%s' "$input" |
    TERRAPOD_SETUP_PRESENTATION="$presentation" TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" setup >"$output_file" 2>&1
}
```

- [ ] **Step 3: Add plain fallback assertions for non-TTY setup**

Add these assertions immediately after the existing plain macOS setup happy-path block:

```sh
assert_no_ansi_escape "$setup_output_text" "auto setup fallback does not use ANSI color with piped input"
assert_no_rich_setup_emoji "$setup_output_text" "auto setup fallback does not use rich setup emoji with piped input"
```

- [ ] **Step 4: Add plain fallback assertions for dumb terminals**

Add this block after the assertions from Step 3:

```sh
dumb_setup_home="$tmp_dir/dumb-setup-home"
dumb_setup_xdg="$tmp_dir/dumb-setup-xdg"
dumb_setup_output="$tmp_dir/dumb-setup.out"
mkdir -p "$dumb_setup_home"

if ! printf '%s' 'minimal
n
n
n
n
n
n
n
y
' |
  TERM=dumb TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG= HOME="$dumb_setup_home" XDG_CONFIG_HOME="$dumb_setup_xdg" sh "$terrapod" setup >"$dumb_setup_output" 2>&1; then
  sed 's/^/  /' "$dumb_setup_output" >&2
  fail "TERM=dumb setup uses plain fallback"
fi
pass "TERM=dumb setup uses plain fallback"

dumb_setup_output_text="$(cat "$dumb_setup_output")"
assert_contains "$dumb_setup_output_text" "Choose Terrapod Preset (minimal|development|workstation):" "TERM=dumb setup keeps plain Preset prompt"
assert_no_ansi_escape "$dumb_setup_output_text" "TERM=dumb setup does not use ANSI color"
assert_no_rich_setup_emoji "$dumb_setup_output_text" "TERM=dumb setup does not use rich setup emoji"
```

- [ ] **Step 5: Add routine output guard assertions**

Add these assertions after `macos_status_output` is captured:

```sh
assert_no_ansi_escape "$help_output" "Terrapod help does not use setup ANSI presentation"
assert_no_rich_setup_emoji "$help_output" "Terrapod help does not use setup emoji presentation"
assert_no_ansi_escape "$macos_status_output" "Terrapod status does not use setup ANSI presentation"
assert_no_rich_setup_emoji "$macos_status_output" "Terrapod status does not use setup emoji presentation"
```

- [ ] **Step 6: Run the command tests and verify the new tests fail**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: FAIL because rich detection and setup presentation helpers do not exist yet.

- [ ] **Step 7: Commit**

Do not commit yet. Keep this red test work with Task 2 implementation.

---

### Task 2: Add Rich Setup Presentation Helpers

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_command_test.sh`

- [ ] **Step 1: Rename existing prompt functions to plain implementations**

Rename the current `prompt_for_setup_preset()` to `prompt_for_setup_preset_plain()` and current `prompt_for_setup_settings()` to `prompt_for_setup_settings_plain()`.

Then add wrapper functions with the original names:

```sh
prompt_for_setup_preset() {
  profile="$1"

  if setup_rich_presentation_enabled; then
    prompt_for_setup_preset_rich "$profile"
  else
    prompt_for_setup_preset_plain "$profile"
  fi
}

prompt_for_setup_settings() {
  preset="$1"
  profile="$2"

  if setup_rich_presentation_enabled; then
    prompt_for_setup_settings_rich "$preset" "$profile"
  else
    prompt_for_setup_settings_plain "$preset" "$profile"
  fi
}
```

- [ ] **Step 2: Add setup rich detection helpers**

Add these helpers before the setup prompt functions:

```sh
setup_rich_presentation_enabled() {
  case "${TERRAPOD_SETUP_PRESENTATION:-auto}" in
    rich)
      return 0
      ;;
    plain)
      return 1
      ;;
    auto|"")
      ;;
    *)
      fatal "unsupported TERRAPOD_SETUP_PRESENTATION: ${TERRAPOD_SETUP_PRESENTATION:-}"
      ;;
  esac

  [ -t 0 ] || return 1
  [ -t 2 ] || return 1
  [ -n "${TERM:-}" ] || return 1
  [ "${TERM:-}" != "dumb" ] || return 1
  [ -z "${CI:-}" ] || return 1

  return 0
}

setup_rich_color_enabled() {
  setup_rich_presentation_enabled || return 1
  [ -z "${NO_COLOR:-}" ] || return 1
  return 0
}

setup_rich_text() {
  color_code="$1"
  text="$2"

  if setup_rich_color_enabled; then
    printf '\033[%sm%s\033[0m' "$color_code" "$text"
  else
    printf '%s' "$text"
  fi
}
```

- [ ] **Step 3: Add rich Preset state helpers**

Add these helpers after `available_preset_args()`:

```sh
available_preset_at_index() {
  profile="$1"
  wanted_index="$2"
  index=1

  for preset in $(known_presets); do
    if is_preset_available_for_profile "$preset" "$profile"; then
      if [ "$index" -eq "$wanted_index" ]; then
        printf '%s\n' "$preset"
        return 0
      fi
      index=$((index + 1))
    fi
  done

  return 1
}

available_preset_count() {
  profile="$1"
  count=0

  for preset in $(known_presets); do
    if is_preset_available_for_profile "$preset" "$profile"; then
      count=$((count + 1))
    fi
  done

  printf '%s\n' "$count"
}

rich_next_index() {
  current="$1"
  count="$2"

  if [ "$current" -ge "$count" ]; then
    printf '%s\n' 1
  else
    printf '%s\n' "$((current + 1))"
  fi
}

rich_previous_index() {
  current="$1"
  count="$2"

  if [ "$current" -le 1 ]; then
    printf '%s\n' "$count"
  else
    printf '%s\n' "$((current - 1))"
  fi
}

rich_preset_index_for_input() {
  profile="$1"
  input="$2"
  count="$(available_preset_count "$profile")"

  case "$input" in
    1|2|3)
      if [ "$input" -le "$count" ]; then
        printf '%s\n' "$input"
        return 0
      fi
      return 1
      ;;
  esac

  index=1
  for preset in $(known_presets); do
    if is_preset_available_for_profile "$preset" "$profile"; then
      if [ "$input" = "$preset" ]; then
        printf '%s\n' "$index"
        return 0
      fi
      index=$((index + 1))
    fi
  done

  return 1
}
```

- [ ] **Step 4: Add rich Preset prompt**

Add this function near `prompt_for_setup_preset_plain()`:

```sh
prompt_for_setup_preset_rich() {
  profile="$1"
  count="$(available_preset_count "$profile")"
  selected=1

  while :; do
    printf '%s\n' "$(setup_rich_text 36 '🌱 Terrapod Setup') Preset" >&2
    index=1
    for preset in $(known_presets); do
      if is_preset_available_for_profile "$preset" "$profile"; then
        marker=" "
        if [ "$index" -eq "$selected" ]; then
          marker="$(setup_rich_text 32 '▸')"
        fi
        printf '%s %s. %s\n' "$marker" "$index" "$preset" >&2
        index=$((index + 1))
      fi
    done

    printf '%s ' "$(setup_rich_text 35 'Preset [j/k, number/name, Enter]')" >&2
    if ! IFS= read -r answer; then
      fatal "no Terrapod Preset selected"
    fi

    case "$answer" in
      "")
        available_preset_at_index "$profile" "$selected"
        return
        ;;
      j|J|down|Down|DOWN|"$(printf '\033[B')")
        selected="$(rich_next_index "$selected" "$count")"
        ;;
      k|K|up|Up|UP|"$(printf '\033[A')")
        selected="$(rich_previous_index "$selected" "$count")"
        ;;
      *)
        if next_selected="$(rich_preset_index_for_input "$profile" "$answer")"; then
          available_preset_at_index "$profile" "$next_selected"
          return
        fi
        printf '%s\n' "terrapod: choose a Preset with j/k, number, or name" >&2
        ;;
    esac
  done
}
```

- [ ] **Step 5: Run the command tests and verify detection/presentation tests pass**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: PASS for plain fallback and routine output guards. Rich forced-mode customization tests are added in Task 3.

- [ ] **Step 6: Commit**

Do not commit yet. Keep this implementation with Task 3 tests and implementation.

---

### Task 3: Add Rich Setting State Transitions and Config Equivalence Tests

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Modify: `tests/terrapod_config_test.sh`
- Modify: `tests/terrapod_command_test.sh`
- Test: `tests/terrapod_config_test.sh`, `tests/terrapod_command_test.sh`

- [ ] **Step 1: Add rich setup runner helper in config tests**

Add this helper after `run_terrapod_setup()`:

```sh
run_terrapod_setup_rich() {
  profile="$1"
  input="$2"
  home_dir="$3"
  xdg_config_home="$4"

  printf '%s' "$input" |
    TERRAPOD_SETUP_PRESENTATION=rich TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" setup
}
```

- [ ] **Step 2: Add rich/plain concrete settings equivalence test**

Add this block after the existing `setup_custom_workspace_config` assertions:

```sh
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
assert_data_key_once_with_value "$rich_equivalent_config" "enableEditorStack" "true" "rich setup writes included Optional Editor Stack"
assert_data_key_once_with_value "$rich_equivalent_config" "enableAiCliTools" "true" "rich setup writes included Optional AI Tool Stack"
assert_data_key_once_with_value "$rich_equivalent_config" "enableDevelopmentWorkspace" "true" "rich setup writes enabled Optional Development Workspace"
assert_data_key_once_with_value "$rich_equivalent_config" "enableMacosAppGroupTerminalApps" "false" "rich setup writes customized terminal-apps App Group"
assert_data_key_once_with_value "$rich_equivalent_config" "enableMacosAppGroupAutomation" "true" "rich setup writes customized automation App Group"
assert_data_key_once_with_value "$rich_equivalent_config" "enableMacosAppGroupLauncher" "false" "rich setup writes customized launcher App Group"
assert_data_key_once_with_value "$rich_equivalent_config" "enableMacosAppGroupMonitoring" "true" "rich setup writes customized monitoring App Group"
```

- [ ] **Step 3: Add rich Preset navigation state transition test**

Add this block after the rich equivalence test:

```sh
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
```

- [ ] **Step 4: Add rich VPS non-applicability test**

Add this block after the rich navigation test:

```sh
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
```

- [ ] **Step 5: Add rich forced-mode output coverage in command tests**

Add this block after the dumb terminal fallback test:

```sh
rich_setup_home="$tmp_dir/rich-setup-home"
rich_setup_xdg="$tmp_dir/rich-setup-xdg"
rich_setup_output="$tmp_dir/rich-setup.out"
mkdir -p "$rich_setup_home"

if ! run_terrapod_setup_command_with_presentation rich macos-terminal '1

y
' "$rich_setup_home" "$rich_setup_xdg" "$rich_setup_output"; then
  sed 's/^/  /' "$rich_setup_output" >&2
  fail "forced rich setup uses rich prompt path"
fi
pass "forced rich setup uses rich prompt path"

rich_setup_output_text="$(cat "$rich_setup_output")"
assert_contains "$rich_setup_output_text" "Terrapod Setup" "rich setup shows setup-only rich heading"
assert_contains "$rich_setup_output_text" "Preset [j/k, number/name, Enter]" "rich setup shows keyboard Preset controls"
assert_contains "$rich_setup_output_text" "Settings [j/k, t, y/n, Enter]" "rich setup shows keyboard setting controls"
```

- [ ] **Step 6: Add rich setting field helpers in the command**

Add these helpers near `render_setup_data()`:

```sh
rich_setup_field_count() {
  profile="$1"

  if is_enabled "$setup_enableDevelopmentWorkspace"; then
    if [ "$profile" = "macos-terminal" ]; then
      printf '%s\n' 5
    else
      printf '%s\n' 1
    fi
    return
  fi

  if [ "$profile" = "macos-terminal" ]; then
    printf '%s\n' 7
  else
    printf '%s\n' 3
  fi
}

rich_setup_field_name() {
  profile="$1"
  index="$2"

  if is_enabled "$setup_enableDevelopmentWorkspace"; then
    case "$index" in
      1) printf '%s\n' "enableDevelopmentWorkspace" ;;
      2) printf '%s\n' "enableMacosAppGroupTerminalApps" ;;
      3) printf '%s\n' "enableMacosAppGroupAutomation" ;;
      4) printf '%s\n' "enableMacosAppGroupLauncher" ;;
      5) printf '%s\n' "enableMacosAppGroupMonitoring" ;;
      *) return 1 ;;
    esac
    return
  fi

  case "$index" in
    1) printf '%s\n' "enableDevelopmentWorkspace" ;;
    2) printf '%s\n' "enableEditorStack" ;;
    3) printf '%s\n' "enableAiCliTools" ;;
    4) printf '%s\n' "enableMacosAppGroupTerminalApps" ;;
    5) printf '%s\n' "enableMacosAppGroupAutomation" ;;
    6) printf '%s\n' "enableMacosAppGroupLauncher" ;;
    7) printf '%s\n' "enableMacosAppGroupMonitoring" ;;
    *) return 1 ;;
  esac
}

rich_setup_field_label() {
  case "$1" in
    enableDevelopmentWorkspace) printf '%s\n' "Optional Development Workspace" ;;
    enableEditorStack) printf '%s\n' "Optional Editor Stack" ;;
    enableAiCliTools) printf '%s\n' "Optional AI Tool Stack" ;;
    enableMacosAppGroupTerminalApps) printf '%s\n' "terminal-apps macOS App Group" ;;
    enableMacosAppGroupAutomation) printf '%s\n' "automation macOS App Group" ;;
    enableMacosAppGroupLauncher) printf '%s\n' "launcher macOS App Group" ;;
    enableMacosAppGroupMonitoring) printf '%s\n' "monitoring macOS App Group" ;;
    *) return 1 ;;
  esac
}

rich_setup_field_value() {
  eval "printf '%s\n' \"\$setup_$1\""
}

rich_set_setup_field_value() {
  field="$1"
  value="$2"

  eval "setup_$field=\$value"

  if [ "$field" = "enableDevelopmentWorkspace" ] && is_enabled "$value"; then
    setup_enableEditorStack=true
    setup_enableAiCliTools=true
  fi
}
```

- [ ] **Step 7: Add rich settings prompt**

Add this function near `prompt_for_setup_settings_plain()`:

```sh
prompt_for_setup_settings_rich() {
  preset="$1"
  profile="$2"

  load_setup_defaults "$preset"
  selected=1

  while :; do
    count="$(rich_setup_field_count "$profile")"
    if [ "$selected" -gt "$count" ]; then
      selected="$count"
    fi

    printf '%s\n' "$(setup_rich_text 36 '✨ Setup settings')" >&2

    index=1
    while [ "$index" -le "$count" ]; do
      field="$(rich_setup_field_name "$profile" "$index")"
      label="$(rich_setup_field_label "$field")"
      value="$(rich_setup_field_value "$field")"
      marker=" "
      if [ "$index" -eq "$selected" ]; then
        marker="$(setup_rich_text 32 '▸')"
      fi
      printf '%s %s [%s]\n' "$marker" "$label" "$(bool_state_label "$value")" >&2
      index=$((index + 1))
    done

    if is_enabled "$setup_enableDevelopmentWorkspace"; then
      printf '%s\n' "  Optional Editor Stack: included by Optional Development Workspace" >&2
      printf '%s\n' "  Optional AI Tool Stack: included by Optional Development Workspace" >&2
    fi

    if [ "$profile" != "macos-terminal" ]; then
      printf '%s\n' "  macOS App Groups: not applicable for $(profile_context_label)" >&2
    fi

    printf '%s ' "$(setup_rich_text 35 'Settings [j/k, t, y/n, Enter]')" >&2
    if ! IFS= read -r answer; then
      break
    fi

    case "$answer" in
      "")
        break
        ;;
      j|J|down|Down|DOWN|"$(printf '\033[B')")
        selected="$(rich_next_index "$selected" "$count")"
        ;;
      k|K|up|Up|UP|"$(printf '\033[A')")
        selected="$(rich_previous_index "$selected" "$count")"
        ;;
      t|T|" ")
        field="$(rich_setup_field_name "$profile" "$selected")"
        value="$(rich_setup_field_value "$field")"
        if is_enabled "$value"; then
          rich_set_setup_field_value "$field" false
        else
          rich_set_setup_field_value "$field" true
        fi
        ;;
      y|Y|yes|YES|true|TRUE|on|ON|1|enabled|ENABLED)
        field="$(rich_setup_field_name "$profile" "$selected")"
        rich_set_setup_field_value "$field" true
        ;;
      n|N|no|NO|false|FALSE|off|OFF|0|disabled|DISABLED)
        field="$(rich_setup_field_name "$profile" "$selected")"
        rich_set_setup_field_value "$field" false
        ;;
      *)
        printf '%s\n' "terrapod: use j/k, t, y/n, or Enter for setup settings" >&2
        ;;
    esac
  done

  if is_enabled "$setup_enableDevelopmentWorkspace"; then
    setup_enableEditorStack=true
    setup_enableAiCliTools=true
  fi

  if [ "$profile" != "macos-terminal" ]; then
    setup_enableMacosAppGroupTerminalApps=false
    setup_enableMacosAppGroupAutomation=false
    setup_enableMacosAppGroupLauncher=false
    setup_enableMacosAppGroupMonitoring=false
  fi

  render_setup_data
}
```

- [ ] **Step 8: Run focused tests**

Run:

```bash
sh tests/terrapod_command_test.sh
sh tests/terrapod_config_test.sh
```

Expected: PASS.

- [ ] **Step 9: Commit**

Commit the implementation and tests:

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh tests/terrapod_config_test.sh docs/superpowers/plans/2026-05-28-rich-terrapod-setup.md
git commit -m "feat: add rich Terrapod setup prompts"
```

---

### Task 4: Final Verification

**Files:**
- Test: all shell and zsh tests

- [ ] **Step 1: Run syntax check**

Run:

```bash
sh -n dot_local/bin/executable_terrapod
sh -n install.sh
```

Expected: both commands exit 0.

- [ ] **Step 2: Run all tests**

Run:

```bash
for test_file in tests/*.sh; do
  sh "$test_file"
done
for test_file in tests/*.zsh; do
  zsh "$test_file"
done
```

Expected: all tests print `ok - ...` and exit 0.

- [ ] **Step 3: Inspect diff scope**

Run:

```bash
git diff --stat
git diff -- dot_local/bin/executable_terrapod tests/terrapod_command_test.sh tests/terrapod_config_test.sh docs/superpowers/plans/2026-05-28-rich-terrapod-setup.md
```

Expected: diff is limited to rich setup prompt behavior, tests, and this plan.

---

## Self-Review

- Spec coverage: The plan covers rich interactive detection, plain fallback, keyboard-driven Preset selection, keyboard-friendly setting customization, setup-only color/emoji, rich/plain concrete setting equivalence, rich input/state tests without full-screen frame assertions, and existing plain setup tests.
- Placeholder scan: No TBD/TODO/fill-in markers remain.
- Type and name consistency: Function names consistently use `setup_rich_*`, `prompt_for_setup_*_plain`, `prompt_for_setup_*_rich`, and existing `setup_enable...` state variables.
