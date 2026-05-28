# Plain Terrapod Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a plain `terrapod setup` command that prompts for a valid Preset, shows the concrete settings summary, confirms, and writes machine-local Terrapod settings through shared Preset behavior.

**Architecture:** Keep the implementation in the existing POSIX shell command file. Add setup-only prompt/summary/confirmation helpers that reuse current profile, Preset policy, `render_preset_data`, and managed config writing. Cover behavior through the existing shell integration tests.

**Tech Stack:** POSIX `sh`, `awk`, existing shell integration tests under `tests/`.

---

## File Structure

- Modify `dot_local/bin/executable_terrapod`: add `setup` help text, setup prompt helpers, settings summary rendering, setup write flow, `run_setup`, and command dispatch.
- Modify `tests/terrapod_command_test.sh`: add user-visible command tests for help, macOS setup prompt/summary/confirmation, and VPS workstation rejection.
- Modify `tests/terrapod_config_test.sh`: add setup config-write and cancellation tests beside existing configure tests.
- Create `docs/superpowers/specs/2026-05-28-plain-terrapod-setup-design.md`: compact design record for Issue #56.
- Create `docs/superpowers/plans/2026-05-28-plain-terrapod-setup.md`: this implementation plan.

---

### Task 1: Add User-Visible Setup Command Tests

**Files:**
- Modify: `tests/terrapod_command_test.sh`
- Test: `tests/terrapod_command_test.sh`

- [ ] **Step 1: Add combined-output helper functions**

Add these helper functions after `assert_line()`:

```sh
assert_first_occurrence_before() {
  haystack="$1"
  earlier="$2"
  later="$3"
  message="$4"

  earlier_line="$(
    printf '%s\n' "$haystack" |
      awk -v needle="$earlier" 'index($0, needle) { print NR; exit }'
  )"
  later_line="$(
    printf '%s\n' "$haystack" |
      awk -v needle="$later" 'index($0, needle) { print NR; exit }'
  )"

  if [ -z "$earlier_line" ] || [ -z "$later_line" ] || [ "$earlier_line" -ge "$later_line" ]; then
    fail "$message"
  fi

  pass "$message"
}

run_terrapod_setup_command() {
  profile="$1"
  input="$2"
  home_dir="$3"
  xdg_config_home="$4"
  output_file="$5"

  if printf '%s' "$input" |
    TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" setup >"$output_file" 2>&1; then
    return 0
  fi

  return "$?"
}
```

- [ ] **Step 2: Add help assertion**

Add this assertion after the existing help command assertions:

```sh
assert_contains \
  "$help_output" \
  "terrapod setup" \
  "Terrapod help lists setup"

assert_contains \
  "$help_output" \
  "setup" \
  "Terrapod help describes plain setup"
```

- [ ] **Step 3: Add macOS happy-path user-visible test**

Add this block near the existing profile-specific command tests:

```sh
setup_home="$tmp_dir/setup-home"
setup_xdg="$tmp_dir/setup-xdg"
setup_output="$tmp_dir/setup.out"
setup_config="$setup_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_home"

if ! run_terrapod_setup_command macos-terminal 'workstation
y
' "$setup_home" "$setup_xdg" "$setup_output"; then
  sed 's/^/  /' "$setup_output" >&2
  fail "macOS Terminal Profile setup completes with workstation"
fi
pass "macOS Terminal Profile setup completes with workstation"

setup_output_text="$(cat "$setup_output")"
assert_contains "$setup_output_text" "Terrapod setup" "plain setup prints a command heading"
assert_contains "$setup_output_text" "Profile: macOS Terminal Profile" "plain setup shows detected macOS profile"
assert_contains "$setup_output_text" "Choose Terrapod Preset (minimal|development|workstation):" "plain setup shows macOS Preset choices"
assert_contains "$setup_output_text" "Settings to write:" "plain setup shows concrete settings summary"
assert_contains "$setup_output_text" "enableEditorStack = true" "plain setup summary includes concrete Editor Stack setting"
assert_contains "$setup_output_text" "enableMacosAppGroupMonitoring = true" "plain setup summary includes concrete macOS App Group setting"
assert_contains "$setup_output_text" "Write these Terrapod settings" "plain setup asks for final confirmation"
assert_contains "$setup_output_text" "Configured Terrapod Preset 'workstation'" "plain setup reports successful configuration"
assert_first_occurrence_before "$setup_output_text" "Profile: macOS Terminal Profile" "Choose Terrapod Preset" "plain setup shows profile before Preset selection"
assert_first_occurrence_before "$setup_output_text" "Settings to write:" "Write these Terrapod settings" "plain setup shows summary before final confirmation"

if [ ! -f "$setup_config" ]; then
  fail "plain setup writes config after final confirmation"
fi
pass "plain setup writes config after final confirmation"
```

- [ ] **Step 4: Add VPS workstation rejection test**

Add this block near the existing VPS `configure workstation` rejection test:

```sh
vps_setup_home="$tmp_dir/vps-setup-home"
vps_setup_xdg="$tmp_dir/vps-setup-xdg"
vps_setup_output="$tmp_dir/vps-setup.out"
mkdir -p "$vps_setup_home"

if run_terrapod_setup_command vps-shell 'workstation
y
' "$vps_setup_home" "$vps_setup_xdg" "$vps_setup_output"; then
  fail "VPS Shell Profile setup rejects workstation Preset"
fi
pass "VPS Shell Profile setup rejects workstation Preset"

vps_setup_output_text="$(cat "$vps_setup_output")"
assert_contains "$vps_setup_output_text" "Profile: VPS Shell Profile" "VPS setup shows detected profile before rejection"
assert_contains "$vps_setup_output_text" "Choose Terrapod Preset (minimal|development):" "VPS setup hides workstation from choices"
assert_contains "$vps_setup_output_text" "workstation Preset is only available for the macOS Terminal Profile" "VPS setup explains workstation rejection"

if [ -e "$vps_setup_xdg/chezmoi/chezmoi.toml" ]; then
  fail "VPS rejected setup does not write config"
fi
pass "VPS rejected setup does not write config"

assert_no_terrapod_artifacts_under "$vps_setup_xdg" "VPS rejected setup leaves no Terrapod artifacts"
```

- [ ] **Step 5: Run test to verify it fails**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: FAIL because `terrapod setup` is not yet in help or dispatch.

- [ ] **Step 6: Commit**

Do not commit yet. Task 1 is intentionally red and will be committed with the implementation after Task 3.

---

### Task 2: Add Setup Config-Write and Cancellation Tests

**Files:**
- Modify: `tests/terrapod_config_test.sh`
- Test: `tests/terrapod_config_test.sh`

- [ ] **Step 1: Add setup test runner helper**

Add this helper after `run_terrapod_configure()`:

```sh
run_terrapod_setup() {
  profile="$1"
  input="$2"
  home_dir="$3"
  xdg_config_home="$4"

  printf '%s' "$input" |
    TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" setup
}
```

- [ ] **Step 2: Add confirmed setup concrete settings test**

Add this block after the workstation `configure` new-config test:

```sh
setup_workstation_home="$tmp_dir/setup-workstation-home"
setup_workstation_xdg="$tmp_dir/setup-workstation-xdg"
setup_workstation_config="$setup_workstation_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_workstation_home"

run_terrapod_setup macos-terminal 'workstation
y
' "$setup_workstation_home" "$setup_workstation_xdg"

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
```

- [ ] **Step 3: Add cancellation-before-confirmation test for a new config**

Add this block after the confirmed setup test:

```sh
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
```

- [ ] **Step 4: Add cancellation-before-confirmation test for an existing config**

Add this block after the new-config cancellation test:

```sh
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
```

- [ ] **Step 5: Run test to verify it fails**

Run:

```bash
sh tests/terrapod_config_test.sh
```

Expected: FAIL because `terrapod setup` is not implemented.

- [ ] **Step 6: Commit**

Do not commit yet. Task 2 is intentionally red and will be committed with the implementation after Task 3.

---

### Task 3: Implement Plain Setup

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_command_test.sh`, `tests/terrapod_config_test.sh`

- [ ] **Step 1: Add help text**

Update `show_help()` so usage includes `terrapod setup`, commands include a setup description, and examples include `terrapod setup`:

```sh
Usage:
  terrapod [help|--help|-h]
  terrapod setup
  terrapod configure <$preset_args>
  terrapod status
  terrapod doctor
  terrapod diff
  terrapod apply
  terrapod update
  terrapod chezmoi -- <args...>

Commands:
  help                                  Show this help.
  setup                                 Run plain interactive Terrapod Setup.
  configure <$preset_args>
                                        Expand a Preset into concrete chezmoi data values.

Examples:
  terrapod setup
  terrapod configure development
```

- [ ] **Step 2: Add setup prompt and summary helpers**

Add these functions after `profile_context_label()`:

```sh
prompt_for_setup_preset() {
  profile="$1"
  preset_args="$(available_preset_args "$profile")"

  printf 'Choose Terrapod Preset (%s): ' "$preset_args" >&2
  if ! IFS= read -r preset; then
    fatal "no Terrapod Preset selected"
  fi

  if [ -z "$preset" ]; then
    fatal "no Terrapod Preset selected"
  fi

  validate_preset_for_profile "$preset" "$profile"
  printf '%s\n' "$preset"
}

show_preset_settings_summary() {
  preset="$1"

  printf '%s\n' "Settings to write:"
  render_preset_data "$preset" | sed 's/^/  /'
}

confirm_setup_write() {
  config_file="$1"

  printf 'Write these Terrapod settings to %s? [y/N] ' "$config_file" >&2
  IFS= read -r answer || answer=

  case "$answer" in
    y|Y|yes|YES)
      return 0
      ;;
    *)
      printf '%s\n' "terrapod: setup cancelled" >&2
      return 1
      ;;
  esac
}
```

- [ ] **Step 3: Add setup write wrapper and runner**

Add these functions before `run_configure()`:

```sh
write_setup_settings() {
  config_file="$1"
  preset="$2"

  validate_preset "$preset"
  reject_unsupported_managed_config "$config_file"
  write_managed_config "$config_file" "$preset"
}

run_setup() {
  if [ "$#" -ne 0 ]; then
    fail_usage "setup accepts no arguments"
  fi

  profile="$(current_profile)"
  config_file="$(chezmoi_config_file)"

  printf '%s\n' "Terrapod setup"
  printf '%s\n' "Profile: $(profile_context_label)"
  show_config_context "$config_file"

  preset="$(prompt_for_setup_preset "$profile")"
  show_preset_settings_summary "$preset"

  if ! confirm_setup_write "$config_file"; then
    return 1
  fi

  write_setup_settings "$config_file" "$preset"
  printf '%s\n' "Configured Terrapod Preset '$preset' in $config_file"
}
```

- [ ] **Step 4: Add command dispatch**

Add a `setup` branch before `configure` in the command dispatcher:

```sh
  setup)
    shift
    run_setup "$@"
    ;;
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
sh tests/terrapod_command_test.sh
sh tests/terrapod_config_test.sh
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh tests/terrapod_config_test.sh docs/superpowers/specs/2026-05-28-plain-terrapod-setup-design.md docs/superpowers/plans/2026-05-28-plain-terrapod-setup.md
git commit -m "feat: add plain terrapod setup"
```

---

### Task 4: Final Verification

**Files:**
- Verify: all `tests/*.sh` and `tests/*.zsh`

- [ ] **Step 1: Run command test**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: PASS.

- [ ] **Step 2: Run config test**

Run:

```bash
sh tests/terrapod_config_test.sh
```

Expected: PASS.

- [ ] **Step 3: Run full suite**

Run:

```bash
for test_file in tests/*.sh; do
  sh "$test_file"
done
for test_file in tests/*.zsh; do
  zsh "$test_file"
done
```

Expected: PASS.

- [ ] **Step 4: Review diff scope**

Run:

```bash
git diff --stat origin/main...HEAD
git diff origin/main...HEAD -- dot_local/bin/executable_terrapod tests/terrapod_command_test.sh tests/terrapod_config_test.sh docs/superpowers/specs/2026-05-28-plain-terrapod-setup-design.md docs/superpowers/plans/2026-05-28-plain-terrapod-setup.md
```

Expected: diff only touches the planned files and implements Issue #56.
