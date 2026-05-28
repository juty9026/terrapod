# Shared Preset Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Share Terrapod Preset validation, Preset expansion, and safe machine-local config writing so `configure <Preset>` and a future human-facing Setup workflow can call the same behavior.

**Architecture:** Keep the implementation inside `dot_local/bin/executable_terrapod`, because Terrapod is currently a single POSIX shell command. Extract small shell functions around Preset availability, config safety checks, and the high-level “write this Preset into this config file” workflow; keep the concrete Preset data in `render_preset_data` as the single expansion source. Preserve `configure <Preset>` as the only public behavior in this slice.

**Tech Stack:** POSIX `sh`, `awk`, existing shell integration tests under `tests/`.

---

## File Structure

- Modify: `dot_local/bin/executable_terrapod`
  - Add shared Preset availability helpers used by both help text and validation.
  - Add `reject_unsupported_managed_config` to centralize unsafe-config rejection.
  - Add `write_preset_settings` to provide the reusable Preset-to-settings workflow for future setup.
  - Add `run_configure` so the command dispatch stays thin.
- Modify: `tests/terrapod_config_test.sh`
  - Add a behavior-focused characterization for the `development` Preset creating a new config with the same concrete settings expected from existing updates.
- Create: `docs/superpowers/plans/2026-05-28-shared-preset-settings.md`
  - This plan.

---

### Task 1: Characterize Development Preset Expansion On New Configs

**Files:**
- Modify: `tests/terrapod_config_test.sh`
- Test: `tests/terrapod_config_test.sh`

- [ ] **Step 1: Add the behavior-focused test**

Insert this block after the existing minimal Preset assertions and before the `workstation_home=...` block:

```sh
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
```

- [ ] **Step 2: Run the focused test**

Run:

```bash
sh tests/terrapod_config_test.sh
```

Expected: PASS. This is a characterization test for behavior that already exists before the refactor.

- [ ] **Step 3: Commit the test**

Run:

```bash
git add tests/terrapod_config_test.sh
git commit -m "test: characterize development preset config creation"
```

Expected: commit succeeds.

---

### Task 2: Extract Shared Preset Validation Helpers

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_command_test.sh`, `tests/terrapod_config_test.sh`

- [ ] **Step 1: Replace `available_preset_args` and validation helpers**

Replace the existing `available_preset_args`, `validate_preset`, and `validate_preset_for_profile` definitions with the following functions. Keep `render_preset_data` unchanged.

```sh
known_presets() {
  printf '%s\n' "minimal" "development" "workstation"
}

is_known_preset() {
  case "$1" in
    minimal|development|workstation)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

is_preset_available_for_profile() {
  preset="$1"
  profile="$2"

  case "$preset" in
    minimal|development)
      return 0
      ;;
    workstation)
      [ "$profile" = "macos-terminal" ]
      return
      ;;
    *)
      return 1
      ;;
  esac
}

available_preset_args() {
  profile="$1"
  preset_args=

  for preset in $(known_presets); do
    if is_preset_available_for_profile "$preset" "$profile"; then
      if [ -z "$preset_args" ]; then
        preset_args="$preset"
      else
        preset_args="$preset_args|$preset"
      fi
    fi
  done

  printf '%s\n' "$preset_args"
}

validate_preset() {
  preset="$1"

  if ! is_known_preset "$preset"; then
    fail_usage "unknown Preset: $preset"
  fi
}

validate_preset_for_profile() {
  preset="$1"
  profile="$2"

  validate_preset "$preset"

  if ! is_preset_available_for_profile "$preset" "$profile"; then
    fail_usage "workstation Preset is only available for the macOS Terminal Profile"
  fi
}
```

- [ ] **Step 2: Remove the unknown-Preset branch from `render_preset_data`**

In `render_preset_data`, change the final branch from:

```sh
    *)
      fail_usage "unknown Preset: $preset"
      ;;
```

to:

```sh
    *)
      return 1
      ;;
```

`validate_preset` now owns the user-facing unknown Preset error. `render_preset_data` remains the single concrete expansion source.

- [ ] **Step 3: Run focused command/config tests**

Run:

```bash
sh tests/terrapod_command_test.sh
sh tests/terrapod_config_test.sh
```

Expected: both pass. In particular:
- `macOS Terminal Profile help exposes workstation Preset`
- `VPS Shell Profile help hides workstation Preset`
- `invalid Preset exits with usage status 64`
- `invalid Preset names rejected Preset`

- [ ] **Step 4: Commit shared validation helpers**

Run:

```bash
git add dot_local/bin/executable_terrapod
git commit -m "refactor: share preset validation helpers"
```

Expected: commit succeeds.

---

### Task 3: Extract Reusable Preset-To-Config Write Workflow

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_config_test.sh`

- [ ] **Step 1: Add a shared unsafe-config rejection wrapper**

Insert this function immediately after `reject_unusable_config_path`:

```sh
reject_unsupported_managed_config() {
  config_file="$1"

  reject_unusable_config_path "$config_file"
  reject_unsupported_multiline_strings "$config_file"
  reject_section_like_multiline_arrays "$config_file"
  reject_unsupported_inline_data_table "$config_file"
}
```

- [ ] **Step 2: Use the wrapper inside `write_managed_config`**

Replace these lines near the top of `write_managed_config`:

```sh
  reject_unusable_config_path "$config_file"
  reject_unsupported_multiline_strings "$config_file"
  reject_section_like_multiline_arrays "$config_file"
  reject_unsupported_inline_data_table "$config_file"
```

with:

```sh
  reject_unsupported_managed_config "$config_file"
```

- [ ] **Step 3: Add `write_preset_settings`**

Insert this function after `write_managed_config`:

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

This function is the future setup workflow hook: given a Preset and config path, it validates the Preset, rejects unsafe configs before prompting, preserves unrelated config values through `write_managed_config`, and writes only concrete managed settings.

- [ ] **Step 4: Run the focused config test**

Run:

```bash
sh tests/terrapod_config_test.sh
```

Expected: PASS. Important preserved behaviors include:
- existing unrelated config values are preserved
- unsupported inline data tables are rejected before prompting
- multiline strings and section-like multiline arrays are rejected before rewriting
- symlink and directory config paths are rejected without artifacts

- [ ] **Step 5: Commit shared config write workflow**

Run:

```bash
git add dot_local/bin/executable_terrapod
git commit -m "refactor: share preset config writes"
```

Expected: commit succeeds.

---

### Task 4: Route `configure` Through The Shared Workflow

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_command_test.sh`, `tests/terrapod_config_test.sh`

- [ ] **Step 1: Add `run_configure`**

Insert this function after `write_preset_settings`:

```sh
run_configure() {
  if [ "$#" -eq 0 ]; then
    fail_usage "Preset is required"
  fi

  if [ "$#" -gt 1 ]; then
    fail_usage "configure accepts exactly one Preset"
  fi

  preset="$1"
  profile="$(current_profile)"
  config_file="$(chezmoi_config_file)"

  validate_preset_for_profile "$preset" "$profile"
  write_preset_settings "$config_file" "$preset"
  printf '%s\n' "Configured Terrapod Preset '$preset' in $config_file"
}
```

- [ ] **Step 2: Replace the command dispatch body for `configure`**

Replace the whole `configure)` branch body:

```sh
  configure)
    shift

    preset="${1:-}"
    if [ -z "$preset" ]; then
      fail_usage "Preset is required"
    fi

    if [ "$#" -gt 1 ]; then
      fail_usage "configure accepts exactly one Preset"
    fi

    profile="$(current_profile)"
    validate_preset_for_profile "$preset" "$profile"

    config_file="$(chezmoi_config_file)"
    reject_unusable_config_path "$config_file"
    reject_unsupported_multiline_strings "$config_file"
    reject_section_like_multiline_arrays "$config_file"
    reject_unsupported_inline_data_table "$config_file"
    confirm_existing_config_update "$config_file"
    write_managed_config "$config_file" "$preset"
    printf '%s\n' "Configured Terrapod Preset '$preset' in $config_file"
    ;;
```

with:

```sh
  configure)
    shift
    run_configure "$@"
    ;;
```

- [ ] **Step 3: Run focused command/config tests**

Run:

```bash
sh tests/terrapod_command_test.sh
sh tests/terrapod_config_test.sh
```

Expected: both pass. This verifies the script-friendly `configure <Preset>` command still accepts and rejects the same Preset/profile combinations and writes the same concrete settings.

- [ ] **Step 4: Commit configure routing**

Run:

```bash
git add dot_local/bin/executable_terrapod
git commit -m "refactor: route configure through preset workflow"
```

Expected: commit succeeds.

---

### Task 5: Full Regression Verification

**Files:**
- Verify all tracked changes.

- [ ] **Step 1: Run shell syntax check**

Run:

```bash
sh -n dot_local/bin/executable_terrapod
```

Expected: exits 0.

- [ ] **Step 2: Run the full test suite**

Run:

```bash
for test_file in tests/*.sh; do sh "$test_file"; done
for test_file in tests/*.zsh; do zsh "$test_file"; done
```

Expected: all tests pass.

- [ ] **Step 3: Inspect the final diff**

Run:

```bash
git diff --stat origin/main...HEAD
git diff --check
```

Expected:
- diff touches only `dot_local/bin/executable_terrapod`, `tests/terrapod_config_test.sh`, and this plan file
- `git diff --check` exits 0

- [ ] **Step 4: Final implementation commit if needed**

If any verification-only fixes were needed after Task 4, commit them:

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_config_test.sh docs/superpowers/plans/2026-05-28-shared-preset-settings.md
git commit -m "chore: verify shared preset workflow"
```

Expected: either no commit is needed, or the commit succeeds.

---

## Self-Review

- Spec coverage: The plan preserves supported Preset acceptance and unsupported profile rejection through focused command tests, preserves concrete Optional Stack and macOS App Group expansion through config tests, preserves unsafe-config and unrelated-config behavior through existing config tests, and exposes the shared behavior as `write_preset_settings` for future setup.
- Placeholder scan: No TBD/TODO/implement-later placeholders remain.
- Type/name consistency: Function names are consistent across tasks: `known_presets`, `is_known_preset`, `is_preset_available_for_profile`, `reject_unsupported_managed_config`, `write_preset_settings`, and `run_configure`.
