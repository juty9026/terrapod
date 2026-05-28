# Custom Setup Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Terrapod Setup treat a Preset as a starting point, customize concrete optional stack and applicable macOS App Group settings, show the customized summary, and write only after final confirmation.

**Architecture:** Keep the implementation inside the existing POSIX shell Terrapod command. Reuse the current Preset expansion and conservative config writer by adding a second write path that accepts already-customized managed data, while `configure <Preset>` continues to render and write Preset defaults unchanged. The interactive setup flow loads Preset defaults into shell variables, prompts for concrete settings, applies Optional Development Workspace precedence, and renders the final managed data only after all customization prompts are complete.

**Tech Stack:** POSIX `sh`, `awk`, existing shell integration tests under `tests/`.

---

## File Structure

- Modify: `dot_local/bin/executable_terrapod`
  - Add `render_settings_data` so both Presets and setup-customized values render the same managed keys.
  - Add setup default-loading, boolean prompting, and customized setup data rendering helpers.
  - Split config writing into `write_managed_settings` for arbitrary managed data and keep `write_managed_config` as the Preset wrapper.
  - Update `run_setup` to prompt for customization before the final confirmation and write customized data after confirmation.
- Modify: `tests/terrapod_config_test.sh`
  - Update existing setup tests for the new customization prompts.
  - Add coverage for Optional Development Workspace precedence, independent leaf-stack customization, macOS App Group customization, VPS non-applicability, customized summary output, and cancellation safety.
- Modify: `tests/terrapod_command_test.sh`
  - Update the plain setup output expectations for the new customization prompts.
- Create: `docs/superpowers/plans/2026-05-28-custom-setup-settings.md`
  - This plan.

---

### Task 1: Add Setup Customization Tests

**Files:**
- Modify: `tests/terrapod_config_test.sh`
- Modify: `tests/terrapod_command_test.sh`
- Test: `tests/terrapod_config_test.sh`, `tests/terrapod_command_test.sh`

- [ ] **Step 1: Update existing setup input fixtures**

In `tests/terrapod_config_test.sh`, update the existing confirmed workstation setup input from:

```sh
run_terrapod_setup macos-terminal 'workstation
y
' "$setup_workstation_home" "$setup_workstation_xdg"
```

to:

```sh
run_terrapod_setup macos-terminal 'workstation





y
' "$setup_workstation_home" "$setup_workstation_xdg"
```

This keeps the workstation defaults for Optional Development Workspace and all four macOS App Groups, then confirms the final write.

Update the existing cancelled setup input from:

```sh
if run_terrapod_setup macos-terminal 'development
n
' "$setup_cancel_home" "$setup_cancel_xdg" >"$tmp_dir/setup-cancel.out" 2>"$tmp_dir/setup-cancel.err"; then
```

to:

```sh
if run_terrapod_setup macos-terminal 'development





n
' "$setup_cancel_home" "$setup_cancel_xdg" >"$tmp_dir/setup-cancel.out" 2>"$tmp_dir/setup-cancel.err"; then
```

Update the existing empty-confirmation input from:

```sh
if run_terrapod_setup macos-terminal 'development

' "$setup_existing_cancel_home" "$setup_existing_cancel_xdg" >"$tmp_dir/setup-existing-cancel.out" 2>"$tmp_dir/setup-existing-cancel.err"; then
```

to:

```sh
if run_terrapod_setup macos-terminal 'development






' "$setup_existing_cancel_home" "$setup_existing_cancel_xdg" >"$tmp_dir/setup-existing-cancel.out" 2>"$tmp_dir/setup-existing-cancel.err"; then
```

In `tests/terrapod_command_test.sh`, update the existing macOS setup command input from:

```sh
if ! run_terrapod_setup_command macos-terminal 'workstation
y
' "$setup_home" "$setup_xdg" "$setup_output"; then
```

to:

```sh
if ! run_terrapod_setup_command macos-terminal 'workstation





y
' "$setup_home" "$setup_xdg" "$setup_output"; then
```

- [ ] **Step 2: Add macOS workspace-precedence and App Group customization coverage**

Insert this block in `tests/terrapod_config_test.sh` after the existing confirmed workstation setup assertions and before `setup_cancel_home=...`:

```sh
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
  fail "macOS setup customizes Optional Development Workspace and App Groups"
fi
pass "macOS setup customizes Optional Development Workspace and App Groups"

setup_custom_workspace_output="$(cat "$tmp_dir/setup-custom-workspace.out" "$tmp_dir/setup-custom-workspace.err")"
assert_contains "$tmp_dir/setup-custom-workspace.out" "Optional Editor Stack: included by Optional Development Workspace" "workspace-enabled setup presents Optional Editor Stack as included"
assert_contains "$tmp_dir/setup-custom-workspace.out" "Optional AI Tool Stack: included by Optional Development Workspace" "workspace-enabled setup presents Optional AI Tool Stack as included"
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
```

- [ ] **Step 3: Add independent leaf-stack customization coverage**

Insert this block immediately after the block from Step 2:

```sh
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
  fail "workspace-disabled setup customizes leaf stacks independently"
fi
pass "workspace-disabled setup customizes leaf stacks independently"

assert_contains "$tmp_dir/setup-leaf.out" "enableEditorStack = false" "workspace-disabled setup summary reflects customized Optional Editor Stack"
assert_contains "$tmp_dir/setup-leaf.out" "enableAiCliTools = true" "workspace-disabled setup summary reflects customized Optional AI Tool Stack"
assert_contains "$tmp_dir/setup-leaf.out" "enableDevelopmentWorkspace = false" "workspace-disabled setup summary reflects disabled Optional Development Workspace"

assert_data_key_once_with_value "$setup_leaf_config" "enableEditorStack" "false" "workspace-disabled setup writes customized Optional Editor Stack"
assert_data_key_once_with_value "$setup_leaf_config" "enableAiCliTools" "true" "workspace-disabled setup writes customized Optional AI Tool Stack"
assert_data_key_once_with_value "$setup_leaf_config" "enableDevelopmentWorkspace" "false" "workspace-disabled setup writes disabled Optional Development Workspace"
assert_data_key_once_with_value "$setup_leaf_config" "enableMacosAppGroupTerminalApps" "true" "workspace-disabled setup writes customized terminal-apps App Group"
assert_data_key_once_with_value "$setup_leaf_config" "enableMacosAppGroupAutomation" "false" "workspace-disabled setup writes customized automation App Group"
assert_data_key_once_with_value "$setup_leaf_config" "enableMacosAppGroupLauncher" "true" "workspace-disabled setup writes customized launcher App Group"
assert_data_key_once_with_value "$setup_leaf_config" "enableMacosAppGroupMonitoring" "false" "workspace-disabled setup writes customized monitoring App Group"
```

- [ ] **Step 4: Add VPS non-applicability coverage**

Insert this block immediately after the block from Step 3:

```sh
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
  fail "VPS setup customizes optional stacks without macOS App Groups"
fi
pass "VPS setup customizes optional stacks without macOS App Groups"

setup_vps_custom_output="$(cat "$tmp_dir/setup-vps-custom.out" "$tmp_dir/setup-vps-custom.err")"
if printf '%s\n' "$setup_vps_custom_output" | grep -F "terminal-apps macOS App Group" >/dev/null; then
  fail "VPS setup does not prompt for terminal-apps macOS App Group"
fi
pass "VPS setup does not prompt for terminal-apps macOS App Group"

assert_contains "$tmp_dir/setup-vps-custom.out" "macOS App Groups: not applicable for VPS Shell Profile" "VPS setup explains macOS App Groups are not applicable"
assert_contains "$tmp_dir/setup-vps-custom.out" "enableEditorStack = true" "VPS setup summary reflects customized Optional Editor Stack"
assert_contains "$tmp_dir/setup-vps-custom.out" "enableAiCliTools = false" "VPS setup summary reflects customized Optional AI Tool Stack"
assert_contains "$tmp_dir/setup-vps-custom.out" "enableDevelopmentWorkspace = false" "VPS setup summary reflects disabled Optional Development Workspace"

assert_data_key_once_with_value "$setup_vps_custom_config" "enableEditorStack" "true" "VPS setup writes customized Optional Editor Stack"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableAiCliTools" "false" "VPS setup writes customized Optional AI Tool Stack"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableDevelopmentWorkspace" "false" "VPS setup writes disabled Optional Development Workspace"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableMacosAppGroupTerminalApps" "false" "VPS setup writes terminal-apps App Group disabled"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableMacosAppGroupAutomation" "false" "VPS setup writes automation App Group disabled"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableMacosAppGroupLauncher" "false" "VPS setup writes launcher App Group disabled"
assert_data_key_once_with_value "$setup_vps_custom_config" "enableMacosAppGroupMonitoring" "false" "VPS setup writes monitoring App Group disabled"
```

- [ ] **Step 5: Update command test assertions for customization prompts**

In `tests/terrapod_command_test.sh`, after this assertion:

```sh
assert_contains "$setup_output_text" "Settings to write:" "plain setup shows concrete settings summary"
```

add:

```sh
assert_contains "$setup_output_text" "Customize Terrapod settings." "plain setup offers concrete setting customization"
assert_contains "$setup_output_text" "Optional Development Workspace [enabled]:" "plain setup prompts for Optional Development Workspace"
assert_contains "$setup_output_text" "Optional Editor Stack: included by Optional Development Workspace" "plain setup presents workspace-included Optional Editor Stack"
assert_contains "$setup_output_text" "terminal-apps macOS App Group [enabled]:" "plain setup prompts for terminal-apps macOS App Group"
assert_first_occurrence_before "$setup_output_text" "Choose Terrapod Preset" "Customize Terrapod settings." "plain setup customizes settings after Preset selection"
assert_first_occurrence_before "$setup_output_text" "Customize Terrapod settings." "Settings to write:" "plain setup shows customized settings before summary"
```

- [ ] **Step 6: Run focused tests and verify they fail**

Run:

```bash
sh tests/terrapod_config_test.sh
sh tests/terrapod_command_test.sh
```

Expected: FAIL before implementation because Terrapod Setup does not yet prompt for concrete setting customization.

- [ ] **Step 7: Commit the failing tests**

Run:

```bash
git add tests/terrapod_config_test.sh tests/terrapod_command_test.sh
git commit -m "test: cover setup concrete setting customization"
```

Expected: commit succeeds.

---

### Task 2: Add Customized Setup Data Rendering

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_config_test.sh`, `tests/terrapod_command_test.sh`

- [ ] **Step 1: Replace `render_preset_data` with shared rendering helpers**

Replace the existing `render_preset_data` function with:

```sh
render_settings_data() {
  cat <<EOF
enableEditorStack = $1
enableAiCliTools = $2
enableDevelopmentWorkspace = $3
enableMacosAppGroupTerminalApps = $4
enableMacosAppGroupAutomation = $5
enableMacosAppGroupLauncher = $6
enableMacosAppGroupMonitoring = $7
EOF
}

render_preset_data() {
  preset="$1"

  case "$preset" in
    minimal)
      render_settings_data false false false false false false false
      ;;
    development)
      render_settings_data true true true false false false false
      ;;
    workstation)
      render_settings_data true true true true true true true
      ;;
    *)
      return 1
      ;;
  esac
}
```

- [ ] **Step 2: Add setup default-loading and rendering helpers**

Add these functions immediately after `render_preset_data`:

```sh
load_setup_defaults() {
  preset="$1"

  case "$preset" in
    minimal)
      setup_enableEditorStack=false
      setup_enableAiCliTools=false
      setup_enableDevelopmentWorkspace=false
      setup_enableMacosAppGroupTerminalApps=false
      setup_enableMacosAppGroupAutomation=false
      setup_enableMacosAppGroupLauncher=false
      setup_enableMacosAppGroupMonitoring=false
      ;;
    development)
      setup_enableEditorStack=true
      setup_enableAiCliTools=true
      setup_enableDevelopmentWorkspace=true
      setup_enableMacosAppGroupTerminalApps=false
      setup_enableMacosAppGroupAutomation=false
      setup_enableMacosAppGroupLauncher=false
      setup_enableMacosAppGroupMonitoring=false
      ;;
    workstation)
      setup_enableEditorStack=true
      setup_enableAiCliTools=true
      setup_enableDevelopmentWorkspace=true
      setup_enableMacosAppGroupTerminalApps=true
      setup_enableMacosAppGroupAutomation=true
      setup_enableMacosAppGroupLauncher=true
      setup_enableMacosAppGroupMonitoring=true
      ;;
    *)
      return 1
      ;;
  esac
}

render_setup_data() {
  render_settings_data \
    "$setup_enableEditorStack" \
    "$setup_enableAiCliTools" \
    "$setup_enableDevelopmentWorkspace" \
    "$setup_enableMacosAppGroupTerminalApps" \
    "$setup_enableMacosAppGroupAutomation" \
    "$setup_enableMacosAppGroupLauncher" \
    "$setup_enableMacosAppGroupMonitoring"
}
```

- [ ] **Step 3: Run syntax and focused tests**

Run:

```bash
sh -n dot_local/bin/executable_terrapod
sh tests/terrapod_config_test.sh
sh tests/terrapod_command_test.sh
```

Expected: syntax passes, tests still fail on the missing setup customization prompts.

- [ ] **Step 4: Commit shared data rendering**

Run:

```bash
git add dot_local/bin/executable_terrapod
git commit -m "refactor: render setup settings from concrete values"
```

Expected: commit succeeds.

---

### Task 3: Implement Setup Customization Prompts

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_config_test.sh`, `tests/terrapod_command_test.sh`

- [ ] **Step 1: Add boolean setup prompt helpers**

Add these functions after `confirm_setup_write`:

```sh
prompt_setup_bool() {
  label="$1"
  current="$2"

  printf '%s [%s]: ' "$label" "$(bool_state_label "$current")" >&2
  IFS= read -r answer || answer=

  case "$answer" in
    "")
      printf '%s\n' "$current"
      ;;
    y|Y|yes|YES|true|TRUE|on|ON|1|enabled|ENABLED)
      printf '%s\n' "true"
      ;;
    n|N|no|NO|false|FALSE|off|OFF|0|disabled|DISABLED)
      printf '%s\n' "false"
      ;;
    *)
      fatal "invalid answer for $label; enter y or n"
      ;;
  esac
}

prompt_for_setup_settings() {
  preset="$1"
  profile="$2"

  load_setup_defaults "$preset"

  printf '%s\n' "Customize Terrapod settings." >&2
  printf '%s\n' "Press Enter to keep the value shown in brackets." >&2

  setup_enableDevelopmentWorkspace="$(prompt_setup_bool "Optional Development Workspace" "$setup_enableDevelopmentWorkspace")"

  if is_enabled "$setup_enableDevelopmentWorkspace"; then
    setup_enableEditorStack=true
    setup_enableAiCliTools=true
    printf '%s\n' "Optional Editor Stack: included by Optional Development Workspace" >&2
    printf '%s\n' "Optional AI Tool Stack: included by Optional Development Workspace" >&2
  else
    setup_enableEditorStack="$(prompt_setup_bool "Optional Editor Stack" "$setup_enableEditorStack")"
    setup_enableAiCliTools="$(prompt_setup_bool "Optional AI Tool Stack" "$setup_enableAiCliTools")"
  fi

  if [ "$profile" = "macos-terminal" ]; then
    printf '%s\n' "macOS App Groups:" >&2
    setup_enableMacosAppGroupTerminalApps="$(prompt_setup_bool "terminal-apps macOS App Group" "$setup_enableMacosAppGroupTerminalApps")"
    setup_enableMacosAppGroupAutomation="$(prompt_setup_bool "automation macOS App Group" "$setup_enableMacosAppGroupAutomation")"
    setup_enableMacosAppGroupLauncher="$(prompt_setup_bool "launcher macOS App Group" "$setup_enableMacosAppGroupLauncher")"
    setup_enableMacosAppGroupMonitoring="$(prompt_setup_bool "monitoring macOS App Group" "$setup_enableMacosAppGroupMonitoring")"
  else
    setup_enableMacosAppGroupTerminalApps=false
    setup_enableMacosAppGroupAutomation=false
    setup_enableMacosAppGroupLauncher=false
    setup_enableMacosAppGroupMonitoring=false
    printf '%s\n' "macOS App Groups: not applicable for $(profile_context_label)" >&2
  fi

  render_setup_data
}
```

- [ ] **Step 2: Replace the setup summary helper**

Replace `show_preset_settings_summary`:

```sh
show_preset_settings_summary() {
  preset="$1"

  printf '%s\n' "Settings to write:"
  render_preset_data "$preset" | sed 's/^/  /'
}
```

with:

```sh
show_setup_settings_summary() {
  settings_data="$1"

  printf '%s\n' "Settings to write:"
  printf '%s\n' "$settings_data" | sed 's/^/  /'
}
```

- [ ] **Step 3: Update `run_setup` to customize before final confirmation**

Replace this block in `run_setup`:

```sh
  preset="$(prompt_for_setup_preset "$profile")"
  show_preset_settings_summary "$preset"

  if ! confirm_setup_write "$config_file"; then
    return 1
  fi

  write_setup_settings "$config_file" "$preset"
  printf '%s\n' "Configured Terrapod Preset '$preset' in $config_file"
```

with:

```sh
  preset="$(prompt_for_setup_preset "$profile")"
  setup_settings_data="$(prompt_for_setup_settings "$preset" "$profile")"
  show_setup_settings_summary "$setup_settings_data"

  if ! confirm_setup_write "$config_file"; then
    return 1
  fi

  write_setup_settings "$config_file" "$setup_settings_data"
  printf '%s\n' "Configured Terrapod Preset '$preset' in $config_file"
```

- [ ] **Step 4: Run focused tests and observe the writer failure**

Run:

```bash
sh -n dot_local/bin/executable_terrapod
sh tests/terrapod_config_test.sh
sh tests/terrapod_command_test.sh
```

Expected: syntax passes. Tests may still fail because `write_setup_settings` still expects a Preset instead of customized managed data.

- [ ] **Step 5: Commit setup prompting**

Run:

```bash
git add dot_local/bin/executable_terrapod
git commit -m "feat: prompt setup for concrete settings"
```

Expected: commit succeeds.

---

### Task 4: Write Customized Setup Settings

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_config_test.sh`, `tests/terrapod_command_test.sh`

- [ ] **Step 1: Split the managed config writer**

Replace the start of `write_managed_config`:

```sh
write_managed_config() {
  config_file="$1"
  preset="$2"
  config_dir="$(dirname -- "$config_file")"
  tmp_file=
  managed_data_file=

  reject_unsupported_managed_config "$config_file"

  mkdir -p "$config_dir"
  trap cleanup_managed_config_temp EXIT
  trap 'cleanup_managed_config_temp; exit 1' HUP INT TERM

  tmp_file="$(mktemp "$config_dir/.terrapod-config.XXXXXX")"
  managed_data_file="$(mktemp "$config_dir/.terrapod-data.XXXXXX")"
  render_preset_data "$preset" >"$managed_data_file"
  backup_existing_config "$config_file"
```

with:

```sh
write_managed_settings() {
  config_file="$1"
  settings_data="$2"
  config_dir="$(dirname -- "$config_file")"
  tmp_file=
  managed_data_file=

  reject_unsupported_managed_config "$config_file"

  mkdir -p "$config_dir"
  trap cleanup_managed_config_temp EXIT
  trap 'cleanup_managed_config_temp; exit 1' HUP INT TERM

  tmp_file="$(mktemp "$config_dir/.terrapod-config.XXXXXX")"
  managed_data_file="$(mktemp "$config_dir/.terrapod-data.XXXXXX")"
  printf '%s\n' "$settings_data" >"$managed_data_file"
  backup_existing_config "$config_file"
```

Then rename the closing function from `write_managed_config` to `write_managed_settings` by leaving the rest of the body unchanged through:

```sh
  trap - EXIT HUP INT TERM
}
```

Immediately after that closing brace, add:

```sh
write_managed_config() {
  config_file="$1"
  preset="$2"

  validate_preset "$preset"
  write_managed_settings "$config_file" "$(render_preset_data "$preset")"
}
```

- [ ] **Step 2: Update setup writing to accept customized data**

Replace `write_setup_settings`:

```sh
write_setup_settings() {
  config_file="$1"
  preset="$2"

  validate_preset "$preset"
  reject_unsupported_managed_config "$config_file"
  write_managed_config "$config_file" "$preset"
}
```

with:

```sh
write_setup_settings() {
  config_file="$1"
  settings_data="$2"

  write_managed_settings "$config_file" "$settings_data"
}
```

Keep `write_preset_settings` as the `configure <Preset>` path:

```sh
write_preset_settings() {
  config_file="$1"
  preset="$2"

  validate_preset "$preset"
  reject_unsupported_managed_config "$config_file"
  confirm_existing_config_update "$config_file"
  write_managed_config "$config_file" "$preset"
}
```

- [ ] **Step 3: Run focused tests and verify they pass**

Run:

```bash
sh -n dot_local/bin/executable_terrapod
sh tests/terrapod_config_test.sh
sh tests/terrapod_command_test.sh
```

Expected: PASS.

- [ ] **Step 4: Commit customized setup writes**

Run:

```bash
git add dot_local/bin/executable_terrapod
git commit -m "feat: write customized setup settings"
```

Expected: commit succeeds.

---

### Task 5: Final Verification

**Files:**
- Test: all shell tests

- [ ] **Step 1: Run the full shell test suite**

Run:

```bash
for test in tests/*_test.sh tests/*_test.zsh; do
  printf '== %s ==\n' "$test"
  case "$test" in
    *.zsh) zsh "$test" ;;
    *) sh "$test" ;;
  esac || exit $?
done
```

Expected: PASS.

- [ ] **Step 2: Review the final diff**

Run:

```bash
git diff origin/main...HEAD --stat
git diff origin/main...HEAD -- dot_local/bin/executable_terrapod tests/terrapod_config_test.sh tests/terrapod_command_test.sh docs/superpowers/plans/2026-05-28-custom-setup-settings.md
```

Expected: diff contains only the plan, setup customization tests, and Terrapod setup implementation.

- [ ] **Step 3: Commit any verification-only fixes**

If Step 1 or Step 2 required small fixes, run:

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_config_test.sh tests/terrapod_command_test.sh docs/superpowers/plans/2026-05-28-custom-setup-settings.md
git commit -m "test: verify setup customization flow"
```

Expected: no commit is needed if all previous tasks already committed a clean final state.

---

## Self-Review

- Spec coverage: The plan covers setup customization after Preset selection, Optional Development Workspace enable/disable, workspace-included leaf stack presentation, independent leaf-stack customization when workspace is disabled, macOS App Group customization, VPS App Group non-applicability, customized final summary, customized config writes, cancellation without partial writes, and tests for the required cases.
- Placeholder scan: No TODO/TBD placeholders remain; each code-changing step includes concrete code.
- Type and name consistency: The managed key names stay exactly aligned with existing `render_preset_data`, config writer removal rules, and tests.
