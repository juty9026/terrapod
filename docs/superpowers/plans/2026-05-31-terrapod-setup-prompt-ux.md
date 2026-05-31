# Terrapod Setup Prompt UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make gum-backed Terrapod Setup setting prompts read as Preset-proposed setting review instead of abstract Yes/No questions.

**Architecture:** Keep the existing gum-backed sequential confirmation flow and final concrete key/value summary. Change only the presentation helpers in `dot_local/bin/executable_terrapod` so each setting renders as a quiet setting block followed by a stable `Enable <setting>?` confirmation with dynamic action labels. Update shell tests to assert the new prompt copy, action labels, and grouped workspace inclusions.

**Tech Stack:** POSIX shell, gum confirm/choose/style, existing shell test suite.

---

### Task 1: Rework Terrapod Setup Setting Prompts

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Modify: `tests/terrapod_command_test.sh`
- Modify: `tests/terrapod_config_test.sh`
- Modify: `CONTEXT.md`
- Test: `tests/terrapod_command_test.sh`
- Test: `tests/terrapod_config_test.sh`

- [ ] **Step 1: Confirm the domain decision is present**

Run:

```bash
sed -n '127,138p' CONTEXT.md
```

Expected: the output includes all of these decisions:

```text
Terrapod Setup customization should feel like reviewing and adjusting concrete settings proposed by the selected Preset
gum-backed Terrapod Setup confirmations should use action labels that expose the current Preset-proposed value
gum-backed Terrapod Setup setting prompts should show the setting name first
gum-backed Terrapod Setup setting confirmation prompts should keep a stable `Enable <setting>?` shape
Rich Terrapod Setup presentation should reserve colored emphasis for section headings
Optional Development Workspace presents Optional Editor Stack and Optional AI Tool Stack as a grouped inclusion list
macOS App Groups section ... leading with the group name
final Terrapod Setup settings summary should continue showing the concrete machine-local key/value settings
```

- [ ] **Step 2: Update tests for the new prompt contract**

In `tests/terrapod_command_test.sh`, replace assertions that expect the old description-before-title and generic confirmation labels with assertions for the new contract.

Required assertions:

```sh
assert_contains "$setup_output_text" "Optional Development Workspace" "gum setup shows Optional Development Workspace setting title"
assert_contains "$setup_output_text" "  Dev Zellij layouts." "gum setup describes Optional Development Workspace under its title"
assert_contains "$setup_output_text" "  Includes:" "gum setup groups workspace inclusions"
assert_contains "$setup_output_text" "    - Optional Editor Stack" "gum setup lists Optional Editor Stack as included by workspace"
assert_contains "$setup_output_text" "    - Optional AI Tool Stack" "gum setup lists Optional AI Tool Stack as included by workspace"
assert_not_contains "$setup_output_text" "Optional Development Workspace: Dev Zellij layouts; also includes Editor and AI tool stacks." "gum setup no longer prints workspace description before the setting title"
assert_not_contains "$setup_output_text" "Optional Editor Stack: included by Optional Development Workspace" "gum setup no longer repeats workspace-included Editor Stack as a standalone message"
assert_not_contains "$setup_output_text" "Optional AI Tool Stack: included by Optional Development Workspace" "gum setup no longer repeats workspace-included AI Tool Stack as a standalone message"
assert_contains "$setup_output_text" "terminal-apps" "gum setup leads terminal-apps App Group prompt with the group name"
assert_contains "$setup_output_text" "  Installs Ghostty." "gum setup describes terminal-apps under its group name"
assert_contains "$setup_output_text" "ai-apps" "gum setup leads ai-apps App Group prompt with the group name"
assert_contains "$setup_output_text" "  Installs Claude Desktop, Codex Desktop, Antigravity 2.0, and Antigravity IDE." "gum setup describes ai-apps under its group name"
assert_contains "$setup_gum_log_text" "gum args: confirm Enable Optional Development Workspace?" "gum setup asks stable Enable question for Optional Development Workspace"
assert_contains "$setup_gum_log_text" "--affirmative Keep enabled" "gum setup uses Keep enabled action for enabled Preset-proposed values"
assert_contains "$setup_gum_log_text" "--negative Disable" "gum setup uses Disable action for enabled Preset-proposed values"
assert_contains "$setup_gum_log_text" "gum args: confirm Enable terminal-apps?" "gum setup asks stable Enable question for terminal-apps"
assert_contains "$setup_gum_log_text" "--affirmative Enable" "gum setup uses Enable action for disabled Preset-proposed values"
assert_contains "$setup_gum_log_text" "--negative Keep disabled" "gum setup uses Keep disabled action for disabled Preset-proposed values"
```

Keep existing assertions that the final settings summary contains concrete key/value lines such as:

```sh
assert_contains "$setup_output_text" "enableEditorStack = true" "gum setup summary includes concrete Editor Stack setting"
assert_contains "$setup_output_text" "enableMacosAppGroupMonitoring = true" "gum setup summary includes concrete macOS App Group setting"
```

In `tests/terrapod_config_test.sh`, replace old assertions that expect legacy description text:

```sh
assert_contains "$tmp_dir/gum-equivalent.out" "terminal-apps macOS App Group (Ghostty): Ghostty." "gum setup labels terminal-apps macOS App Group as Ghostty-only"
assert_contains "$tmp_dir/setup-custom-workspace.out" "Optional Editor Stack: included by Optional Development Workspace" "workspace-enabled setup presents Optional Editor Stack as included"
assert_contains "$tmp_dir/setup-custom-workspace.out" "Optional AI Tool Stack: included by Optional Development Workspace" "workspace-enabled setup presents Optional AI Tool Stack as included"
```

with assertions for:

```sh
assert_contains "$tmp_dir/gum-equivalent.out" "terminal-apps" "gum setup labels terminal-apps App Group with the group name"
assert_contains "$tmp_dir/gum-equivalent.out" "  Installs Ghostty." "gum setup describes terminal-apps App Group as Ghostty-only"
assert_contains "$tmp_dir/setup-custom-workspace.out" "  Includes:" "workspace-enabled setup groups included optional stacks"
assert_contains "$tmp_dir/setup-custom-workspace.out" "    - Optional Editor Stack" "workspace-enabled setup lists Optional Editor Stack under workspace"
assert_contains "$tmp_dir/setup-custom-workspace.out" "    - Optional AI Tool Stack" "workspace-enabled setup lists Optional AI Tool Stack under workspace"
```

- [ ] **Step 3: Run focused tests and verify they fail before implementation**

Run:

```bash
sh tests/terrapod_command_test.sh
sh tests/terrapod_config_test.sh
```

Expected: at least one failure in each focused test file because `dot_local/bin/executable_terrapod` still uses the old prompt presentation.

- [ ] **Step 4: Implement prompt block helpers**

In `dot_local/bin/executable_terrapod`, replace `setup_option_description()` / `show_setup_option_description()` with helpers that render setting blocks:

```sh
setup_option_title() {
  case "$1" in
    "Optional Development Workspace")
      printf '%s\n' "Optional Development Workspace"
      ;;
    "Optional Editor Stack")
      printf '%s\n' "Optional Editor Stack"
      ;;
    "Optional AI Tool Stack")
      printf '%s\n' "Optional AI Tool Stack"
      ;;
    "terminal-apps macOS App Group (Ghostty)")
      printf '%s\n' "terminal-apps"
      ;;
    "automation macOS App Group")
      printf '%s\n' "automation"
      ;;
    "launcher macOS App Group")
      printf '%s\n' "launcher"
      ;;
    "monitoring macOS App Group")
      printf '%s\n' "monitoring"
      ;;
    "ai-apps macOS App Group")
      printf '%s\n' "ai-apps"
      ;;
    *)
      return 1
      ;;
  esac
}

setup_option_prompt_name() {
  setup_option_title "$1"
}

show_setup_option_block() {
  label="$1"

  title="$(setup_option_title "$label")" || return 0
  printf '%s\n' "$title"

  case "$label" in
    "Optional Development Workspace")
      printf '%s\n' "  Dev Zellij layouts."
      printf '%s\n' "  Includes:"
      printf '%s\n' "    - Optional Editor Stack"
      printf '%s\n' "    - Optional AI Tool Stack"
      ;;
    "Optional Editor Stack")
      printf '%s\n' "  Rich Neovim configuration."
      ;;
    "Optional AI Tool Stack")
      printf '%s\n' "  Antigravity CLI, Claude Code, and Codex."
      ;;
    "terminal-apps macOS App Group (Ghostty)")
      printf '%s\n' "  Installs Ghostty."
      ;;
    "automation macOS App Group")
      printf '%s\n' "  Installs Hammerspoon and Karabiner-Elements."
      ;;
    "launcher macOS App Group")
      printf '%s\n' "  Installs Raycast and 1Password CLI."
      ;;
    "monitoring macOS App Group")
      printf '%s\n' "  Installs iStat Menus."
      ;;
    "ai-apps macOS App Group")
      printf '%s\n' "  Installs Claude Desktop, Codex Desktop, Antigravity 2.0, and Antigravity IDE."
      ;;
  esac
  printf '%s\n' ""
}
```

Add a helper for dynamic confirmation labels:

```sh
setup_confirm_actions() {
  current="$1"

  if is_enabled "$current"; then
    setup_affirmative="Keep enabled"
    setup_negative="Disable"
  else
    setup_affirmative="Enable"
    setup_negative="Keep disabled"
  fi
}
```

Update `prompt_setup_bool()` to render the block first, ask a stable Enable question, and pass gum action labels:

```sh
prompt_setup_bool() {
  label="$1"
  current="$2"

  show_setup_option_block "$label" >&2
  prompt_name="$(setup_option_prompt_name "$label" || printf '%s\n' "$label")"
  setup_confirm_actions "$current"

  if gum confirm "Enable $prompt_name?" \
    --default="$current" \
    --affirmative "$setup_affirmative" \
    --negative "$setup_negative"; then
    printf '%s\n' "true"
    return
  else
    status="$?"
  fi

  if [ "$status" -eq 1 ]; then
    printf '%s\n' "false"
    return
  fi

  if [ "$status" -eq 130 ]; then
    fatal "setup cancelled"
  fi

  fatal_gum_setup_failure
}
```

Update the workspace-enabled branch in `prompt_for_setup_settings()` by deleting the two `show_setup_option_description` calls and the two standalone included-setting `printf` lines. The workspace prompt block now owns the inclusion list.

- [ ] **Step 5: Run focused tests and verify they pass**

Run:

```bash
sh tests/terrapod_command_test.sh
sh tests/terrapod_config_test.sh
```

Expected: both test files pass.

- [ ] **Step 6: Run final targeted regression**

Run:

```bash
sh -n dot_local/bin/executable_terrapod
git diff --check
```

Expected: both commands pass with no output.

- [ ] **Step 7: Commit**

Run:

```bash
git add CONTEXT.md docs/superpowers/plans/2026-05-31-terrapod-setup-prompt-ux.md dot_local/bin/executable_terrapod tests/terrapod_command_test.sh tests/terrapod_config_test.sh
git commit -m "Improve Terrapod Setup setting prompts"
```

Expected: commit succeeds.

## Self-Review

- Spec coverage: The plan covers setting block order, stable Enable prompts, dynamic action labels, quiet setting titles, workspace inclusion grouping, macOS App Group naming, and preserving final key/value summaries.
- Placeholder scan: No TBD/TODO/fill-in markers remain.
- Type consistency: The plan consistently uses existing setting labels as internal identifiers and derived display names for prompt text.
