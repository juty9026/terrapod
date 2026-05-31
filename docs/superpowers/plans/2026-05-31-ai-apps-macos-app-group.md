# ai-apps macOS App Group Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `ai-apps` macOS App Group that installs Claude Desktop, Codex Desktop, Antigravity 2.0, and Antigravity IDE through Homebrew casks without making the Optional Development Workspace own desktop apps.

**Architecture:** Extend the existing macOS App Group pattern with one new machine-local data key, `enableMacosAppGroupAiApps`. The key participates in setup/config/status/diff/apply rendering only for macOS profiles, joins workstation defaults, stays disabled for `development`, and feeds the rendered Homebrew cask bundle. The desktop Codex cask is `codex-app`; the existing `codex` cask is a CLI cask and must not be used here.

**Tech Stack:** POSIX `sh`, chezmoi templates, Homebrew casks, Markdown docs, existing shell tests under `tests/`.

---

## File Structure

- Modify: `Brewfile.macos-desktop-apps.tmpl`
  - Add the `ai-apps` App Group casks: `claude`, `codex-app`, `antigravity`, and `antigravity-ide`.
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`
  - Include `enableMacosAppGroupAiApps` in the aggregate desktop app bundle gate.
- Modify: `.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl`
  - Include `enableMacosAppGroupAiApps` in the aggregate desktop app bundle checksum gate.
- Modify: `dot_local/bin/executable_terrapod`
  - Add the new data key to setup prompts, rich setup fields, config rendering, preset defaults, status, diff/apply context, and managed-key rewrites.
  - Keep `development` separate from `ai-apps`; enable the key only in `workstation` defaults.
  - Hide macOS App Group detail rows from non-macOS profiles by reporting them as not applicable.
- Modify: `tests/chezmoiignore_test.sh`
  - Add cask rendering and Desktop App Stack bundle tests for `ai-apps`.
- Modify: `tests/terrapod_config_test.sh`
  - Add config write/setup coverage for the new data key across presets and macOS/VPS setup paths.
- Modify: `tests/terrapod_command_test.sh`
  - Add setup/status/diff/apply output coverage for `ai-apps`, plus VPS non-applicability coverage.
- Modify: `tests/readme_optional_stack_profiles_test.sh`
  - Add README assertions for the new key and casks.
- Modify: `README.md`
  - Document the `ai-apps` App Group and cask names.
- Modify: `README.ko.md`
  - Mirror the English documentation in Korean while preserving heading parity.

---

### Task 1: Add Failing Template Tests for the ai-apps Casks

**Files:**
- Modify: `tests/chezmoiignore_test.sh`
- Test: `tests/chezmoiignore_test.sh`

- [ ] **Step 1: Add ai-apps data fixtures**

In `tests/chezmoiignore_test.sh`, after `macos_monitoring_apps_data=...`, add:

```sh
macos_ai_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupAiApps":true}'
```

After `monitoring_apps_brewfile="$(render_template "$macos_monitoring_apps_data" "Brewfile.macos-desktop-apps.tmpl")"`, add:

```sh
ai_apps_brewfile="$(render_template "$macos_ai_apps_data" "Brewfile.macos-desktop-apps.tmpl")"
macos_ai_apps_bootstrap="$(render_template "$macos_ai_apps_data" ".chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl")"
macos_ai_apps_karabiner_opener="$(render_template "$macos_ai_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
```

- [ ] **Step 2: Add default exclusion assertions**

After the existing default cask exclusion assertions for `istat-menus`, add:

```sh
assert_not_contains_text "$macos_brewfile" 'cask "claude"' "macOS default does not render Claude Desktop"
assert_not_contains_text "$macos_brewfile" 'cask "codex-app"' "macOS default does not render Codex Desktop"
assert_not_contains_text "$macos_brewfile" 'cask "codex"' "macOS default does not render Codex CLI as a desktop app"
assert_not_contains_text "$macos_brewfile" 'cask "antigravity"' "macOS default does not render Antigravity 2.0"
assert_not_contains_text "$macos_brewfile" 'cask "antigravity-ide"' "macOS default does not render Antigravity IDE"
```

- [ ] **Step 3: Add ai-apps cask rendering assertions**

After the existing monitoring group assertion, add:

```sh
assert_contains_text "$ai_apps_brewfile" 'cask "claude"' "ai-apps group renders Claude Desktop"
assert_contains_text "$ai_apps_brewfile" 'cask "codex-app"' "ai-apps group renders Codex Desktop cask"
assert_not_contains_text "$ai_apps_brewfile" 'cask "codex"' "ai-apps group does not render Codex CLI cask"
assert_contains_text "$ai_apps_brewfile" 'cask "antigravity"' "ai-apps group renders Antigravity 2.0"
assert_contains_text "$ai_apps_brewfile" 'cask "antigravity-ide"' "ai-apps group renders Antigravity IDE"
```

- [ ] **Step 4: Add bundle gate assertions**

After the existing `terminal-apps group runs macOS Desktop App Stack Brewfile` assertion, add:

```sh
assert_contains_text \
  "$macos_ai_apps_bootstrap" \
  "terrapod-macos-desktop-apps" \
  "ai-apps group renders macOS Desktop App Stack Brewfile"

assert_contains_text \
  "$macos_ai_apps_bootstrap" \
  'brew bundle --no-upgrade --file="$desktop_brewfile"' \
  "ai-apps group runs macOS Desktop App Stack Brewfile"
```

After the existing Karabiner checksum assertions, add:

```sh
assert_contains_text \
  "$macos_ai_apps_karabiner_opener" \
  "macOS Desktop App Stack enabled: true" \
  "Karabiner opener tracks enabled ai-apps Desktop App Stack state"

assert_contains_text \
  "$macos_ai_apps_karabiner_opener" \
  "macOS Desktop App Stack Brewfile checksum" \
  "Karabiner opener tracks ai-apps Desktop App Stack Brewfile changes"
```

- [ ] **Step 5: Run the focused test and verify it fails**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: FAIL because `enableMacosAppGroupAiApps` does not yet gate any casks or script checksums.

- [ ] **Step 6: Commit**

Do not commit this red test alone. Keep it with Task 2's implementation.

---

### Task 2: Implement the ai-apps Data Key and Cask Rendering

**Files:**
- Modify: `Brewfile.macos-desktop-apps.tmpl`
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`
- Modify: `.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl`
- Modify: `dot_local/bin/executable_terrapod`
- Modify: `tests/terrapod_config_test.sh`
- Test: `tests/chezmoiignore_test.sh`
- Test: `tests/terrapod_config_test.sh`

- [ ] **Step 1: Add the cask group to the desktop Brewfile template**

Append this block to `Brewfile.macos-desktop-apps.tmpl`:

```gotemplate
{{ if default false (get . "enableMacosAppGroupAiApps") -}}
# ai-apps macOS App Group
cask "claude"
cask "codex-app"
cask "antigravity"
cask "antigravity-ide"
{{ end -}}
```

- [ ] **Step 2: Add ai-apps to both aggregate template gates**

In both `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl` and `.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl`, change the `$enableMacosAppGroups` declaration to:

```gotemplate
{{- $enableMacosAppGroups := or
  (default false (get . "enableMacosAppGroupTerminalApps"))
  (default false (get . "enableMacosAppGroupAutomation"))
  (default false (get . "enableMacosAppGroupLauncher"))
  (default false (get . "enableMacosAppGroupMonitoring"))
  (default false (get . "enableMacosAppGroupAiApps"))
-}}
```

- [ ] **Step 3: Extend Terrapod setup prompts**

In `dot_local/bin/executable_terrapod`, update plain setup so macOS profiles prompt and non-macOS profiles reset the new key:

```sh
setup_enableMacosAppGroupAiApps="$(prompt_setup_bool "ai-apps macOS App Group" "$setup_enableMacosAppGroupAiApps")"
```

```sh
setup_enableMacosAppGroupAiApps=false
```

Update rich setup field counts and names so macOS setup includes the new key. When `enableDevelopmentWorkspace` is enabled, macOS field count becomes `6`; otherwise it becomes `8`. Add this field mapping:

```sh
6) printf '%s\n' "enableMacosAppGroupAiApps" ;;
```

for the workspace-enabled macOS branch, and this mapping:

```sh
8) printf '%s\n' "enableMacosAppGroupAiApps" ;;
```

for the full macOS branch. Add this label:

```sh
enableMacosAppGroupAiApps) printf '%s\n' "ai-apps macOS App Group" ;;
```

- [ ] **Step 4: Extend Terrapod config rendering and presets**

Change `render_settings_data()` to print the eighth key:

```sh
enableMacosAppGroupAiApps = $8
```

Update `render_preset_data()` to use:

```sh
minimal)
  render_settings_data false false false false false false false false
  ;;
development)
  render_settings_data true true true false false false false false
  ;;
workstation)
  render_settings_data true true true true true true true true
  ;;
```

Add `setup_enableMacosAppGroupAiApps=false` to `minimal` and `development` defaults, and `setup_enableMacosAppGroupAiApps=true` to `workstation` defaults. Pass `"$setup_enableMacosAppGroupAiApps"` as the eighth argument in `render_setup_data()`.

- [ ] **Step 5: Extend managed-key rewriting**

In `write_managed_settings()`'s `is_managed_name()`, include:

```awk
name == "enableMacosAppGroupAiApps" ||
```

before the legacy `enableMacosDesktopApps` key.

- [ ] **Step 6: Add config test expectations**

In `tests/terrapod_config_test.sh`, add assertions for `enableMacosAppGroupAiApps` beside the other macOS App Group assertions:

```sh
assert_data_key_once_with_value "$new_config" "enableMacosAppGroupAiApps" "false" "minimal Preset disables ai-apps macOS App Group exactly once in data"
assert_data_key_once_with_value "$development_config" "enableMacosAppGroupAiApps" "false" "development Preset disables ai-apps macOS App Group in a new config"
assert_data_key_once_with_value "$workstation_config" "enableMacosAppGroupAiApps" "true" "workstation Preset enables ai-apps macOS App Group exactly once in data"
assert_data_key_once_with_value "$setup_workstation_config" "enableMacosAppGroupAiApps" "true" "confirmed setup enables ai-apps macOS App Group exactly once in data"
```

For custom macOS setup blocks, add `enableMacosAppGroupAiApps` to the input sequence and assert the chosen value. For VPS setup/config blocks, assert it is written as `false`:

```sh
assert_data_key_once_with_value "$setup_vps_config" "enableMacosAppGroupAiApps" "false" "VPS setup writes ai-apps App Group disabled"
```

Also add the new key to existing quoted/spaced/dotted/array/signers config update assertions so managed-key rewrite coverage remains complete.

- [ ] **Step 7: Run focused tests**

Run:

```bash
sh tests/chezmoiignore_test.sh
sh tests/terrapod_config_test.sh
```

Expected: PASS.

- [ ] **Step 8: Commit**

Run:

```bash
git add Brewfile.macos-desktop-apps.tmpl .chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl .chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl dot_local/bin/executable_terrapod tests/chezmoiignore_test.sh tests/terrapod_config_test.sh
git commit -m "feat: add ai-apps macOS app group"
```

---

### Task 3: Add Command Output Coverage and macOS/VPS Reporting Behavior

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Modify: `tests/terrapod_command_test.sh`
- Test: `tests/terrapod_command_test.sh`

- [ ] **Step 1: Add command test expectations**

In `tests/terrapod_command_test.sh`, add `enableMacosAppGroupAiApps` to macOS status/diff/apply config fixtures beside the other macOS App Group keys:

```toml
enableMacosAppGroupAiApps = true
```

Add this status assertion near the other App Group assertions:

```sh
assert_contains "$macos_status_output" "ai-apps: enabled (Claude Desktop, Codex Desktop, Antigravity 2.0, and Antigravity IDE)" "Terrapod status reports enabled ai-apps macOS App Group"
```

Add this setup assertion near the setup summary assertions:

```sh
assert_contains "$setup_output_text" "enableMacosAppGroupAiApps = true" "plain setup summary includes concrete ai-apps App Group setting"
```

Add these diff/apply assertions beside the other App Group context assertions:

```sh
assert_contains \
  "$diff_output" \
  "ai-apps: enabled" \
  "Terrapod diff prints enabled ai-apps macOS App Group state"

assert_contains \
  "$apply_output" \
  "ai-apps: enabled" \
  "Terrapod apply prints enabled ai-apps macOS App Group state"
```

- [ ] **Step 2: Add VPS non-applicability assertions**

Add a VPS diff fixture that runs with `TERRAPOD_PROFILE=vps-shell` or Ubuntu 24.04 detection and assert:

```sh
assert_contains \
  "$vps_diff_output" \
  "macOS App Groups: not applicable for VPS Shell Profile" \
  "Terrapod diff reports macOS App Groups as not applicable on VPS"

assert_not_contains \
  "$vps_diff_output" \
  "ai-apps:" \
  "Terrapod diff does not report ai-apps as a VPS setting"
```

Add the same pair for `apply` output:

```sh
assert_contains \
  "$vps_apply_output" \
  "macOS App Groups: not applicable for VPS Shell Profile" \
  "Terrapod apply reports macOS App Groups as not applicable on VPS"

assert_not_contains \
  "$vps_apply_output" \
  "ai-apps:" \
  "Terrapod apply does not report ai-apps as a VPS setting"
```

- [ ] **Step 3: Run the focused test and verify it fails**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: FAIL until the command output implementation is updated.

- [ ] **Step 4: Implement command output**

Update `print_macos_app_group_status()` to include:

```sh
if is_enabled "$(config_data_bool "$config_file" enableMacosAppGroupAiApps)"; then
  printf '%s\n' "  ai-apps: enabled (Claude Desktop, Codex Desktop, Antigravity 2.0, and Antigravity IDE)"
else
  printf '%s\n' "  ai-apps: disabled"
fi
```

Replace `show_stack_context()` with a profile-aware signature:

```sh
show_stack_context() {
  config_file="$1"
  profile="$2"

  printf '%s\n' "Optional stacks:"
  printf '%s\n' "Optional Editor Stack: $(effective_optional_stack_state_label "$config_file" enableEditorStack)"
  printf '%s\n' "Optional AI Tool Stack: $(effective_optional_stack_state_label "$config_file" enableAiCliTools)"
  printf '%s\n' "Optional Development Workspace: $(config_bool_state_label "$config_file" enableDevelopmentWorkspace)"

  if [ "$profile" != "macos-terminal" ]; then
    printf '%s\n' "macOS App Groups: not applicable for $(profile_context_label)"
    return
  fi

  printf '%s\n' "macOS App Groups:"
  printf '%s\n' "terminal-apps: $(config_bool_state_label "$config_file" enableMacosAppGroupTerminalApps)"
  printf '%s\n' "automation: $(config_bool_state_label "$config_file" enableMacosAppGroupAutomation)"
  printf '%s\n' "launcher: $(config_bool_state_label "$config_file" enableMacosAppGroupLauncher)"
  printf '%s\n' "monitoring: $(config_bool_state_label "$config_file" enableMacosAppGroupMonitoring)"
  printf '%s\n' "ai-apps: $(config_bool_state_label "$config_file" enableMacosAppGroupAiApps)"
}
```

Then update `show_diff_context()` and `show_apply_context()` to compute `profile="$(current_profile)"` and call:

```sh
show_stack_context "$config_file" "$profile"
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: PASS, including the existing broad-upgrade guards.

- [ ] **Step 6: Commit**

Run:

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh
git commit -m "test: cover ai-apps terrapod output"
```

---

### Task 4: Document ai-apps in English and Korean README Files

**Files:**
- Modify: `README.md`
- Modify: `README.ko.md`
- Modify: `tests/readme_optional_stack_profiles_test.sh`
- Test: `tests/readme_optional_stack_profiles_test.sh`
- Test: `tests/readme_korean_test.sh`

- [ ] **Step 1: Add README test assertions**

In `tests/readme_optional_stack_profiles_test.sh`, add `enableMacosAppGroupAiApps` to the loop of documented macOS App Group keys.

Add these row assertions after the monitoring assertions:

```sh
assert_key_row_contains '`enableMacosAppGroupAiApps`' 'ai-apps' \
  "README documents ai-apps group on its option row"
assert_key_row_contains '`enableMacosAppGroupAiApps`' 'Claude Desktop' \
  "README documents Claude Desktop on the ai-apps option row"
assert_key_row_contains '`enableMacosAppGroupAiApps`' 'Codex Desktop' \
  "README documents Codex Desktop on the ai-apps option row"
assert_key_row_contains '`enableMacosAppGroupAiApps`' 'codex-app' \
  "README documents Codex Desktop cask token on the ai-apps option row"
assert_key_row_contains '`enableMacosAppGroupAiApps`' 'Antigravity 2.0' \
  "README documents Antigravity 2.0 on the ai-apps option row"
assert_key_row_contains '`enableMacosAppGroupAiApps`' 'Antigravity IDE' \
  "README documents Antigravity IDE on the ai-apps option row"
```

- [ ] **Step 2: Run README tests and verify they fail**

Run:

```bash
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
```

Expected: `tests/readme_optional_stack_profiles_test.sh` FAILS until the README is updated; heading parity should remain PASS.

- [ ] **Step 3: Update English README**

In `README.md`, extend the macOS App Group bullet list with:

```markdown
- `ai-apps`: Claude Desktop, Codex Desktop (`codex-app`), Antigravity 2.0, and Antigravity IDE.
```

Add this table row after monitoring:

```markdown
| `enableMacosAppGroupAiApps` | `false` | Installs the ai-apps macOS App Group: Claude Desktop, Codex Desktop (`codex-app`), Antigravity 2.0, and Antigravity IDE. |
```

- [ ] **Step 4: Update Korean README**

In `README.ko.md`, extend the macOS App Group bullet list with:

```markdown
- `ai-apps`: Claude Desktop, Codex Desktop(`codex-app`), Antigravity 2.0, Antigravity IDE.
```

Add this table row after monitoring:

```markdown
| `enableMacosAppGroupAiApps` | `false` | ai-apps macOS App Group인 Claude Desktop, Codex Desktop(`codex-app`), Antigravity 2.0, Antigravity IDE를 설치합니다. |
```

- [ ] **Step 5: Run README tests**

Run:

```bash
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
git add README.md README.ko.md tests/readme_optional_stack_profiles_test.sh
git commit -m "docs: document ai-apps app group"
```

---

### Task 5: Final Regression

**Files:**
- No new files.
- Test: full shell test suite.

- [ ] **Step 1: Run the full test suite**

Run:

```bash
set -eu
for test_file in tests/*.sh; do
  sh "$test_file"
done
zsh tests/zshrc_zoxide_test.zsh
```

Expected: PASS.

- [ ] **Step 2: Inspect the final diff**

Run:

```bash
git status -sb
git log --oneline --decorate -5
git diff origin/main...HEAD --stat
```

Expected: only Issue #83 implementation, tests, docs, and this plan are included.

- [ ] **Step 3: Handoff to finishing workflow**

Use `superpowers:finishing-a-development-branch`, then publish a ready-for-review PR with `github:yeet` as requested.

---

## Self-Review

- Spec coverage: The plan covers the new data key, Homebrew casks, Codex Desktop cask token, workstation/development preset separation, VPS non-applicability, setup/status/diff/apply output, README/Korean README docs, tests, and broad-upgrade regression through the existing command test guard.
- Placeholder scan: No placeholder sections remain.
- Type consistency: The new key is consistently named `enableMacosAppGroupAiApps` across templates, Terrapod config rendering, tests, and docs.
