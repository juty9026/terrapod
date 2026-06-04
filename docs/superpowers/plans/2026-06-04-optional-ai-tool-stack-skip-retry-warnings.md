# Optional AI Tool Stack Skip Retry Warnings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the **Optional AI Tool Stack** installer marker-aware, idempotent, partial-failure recoverable, and constrained to official vendor installers.

**Architecture:** Keep the existing before-apply AI CLI installer template, but give it a small internal per-tool runner that checks command availability on Terrapod's expected command lookup path before downloading installers. Failed tools are accumulated into one `optional-ai-cli-tools` install warning marker, while successful tools remain installed and are skipped on later applies.

**Tech Stack:** POSIX shell, chezmoi templates, `gh`, shell regression tests.

---

## File Structure

- Modify: `dot_local/lib/terrapod/install-warnings.sh`
  - Rename the Optional AI Tool Stack warning category slug from `ai-cli-tools` to `optional-ai-cli-tools`.
  - Keep category validation and listing behavior explicit and stable.
- Modify: `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`
  - Define one `AI_CLI_WARNING_CATEGORY`.
  - Define Terrapod's expected AI CLI command lookup path with managed user-bin and platform tool locations.
  - Skip already available `agy`, `claude`, and `codex` commands using command availability only.
  - Continue through per-tool installer failures and write one warning marker listing failed tool names.
  - Use official installer URLs and Codex `CODEX_NON_INTERACTIVE=1`.
- Modify: `tests/terrapod_command_test.sh`
  - Update install warning category tests and marker expectations to `optional-ai-cli-tools`.
- Modify: `tests/chezmoiignore_test.sh`
  - Update disabled-stack marker cleanup expectations.
  - Add rendered-installer behavior tests for expected PATH skip checks, partial failure retry, marker content, Codex unattended env, official URLs, no token injection, no prompt piping, and no installer patching.
- Create: `docs/superpowers/plans/2026-06-04-optional-ai-tool-stack-skip-retry-warnings.md`
  - This implementation plan.

## Assumptions

- The blocker #96 is closed, so the shared install warning helper contract is available.
- `optional-ai-cli-tools` is the desired stable category slug because issue #103 and `CONTEXT.md` name that category explicitly.
- This change does not add upgrade/version checks; an available command satisfies the skip check regardless of version or package-manager provenance.
- Installer failures for this optional stack should not make chezmoi apply fail; they should write a warning marker and leave recovery to `tpod apply` plus `tpod doctor`.
- The issue's doctor acceptance is scoped to the disabled stack sentence: when the **Optional AI Tool Stack** is disabled, missing `agy`, `claude`, or `codex` must not fail `tpod doctor`. When the stack is enabled and tools are missing, keep the existing `tpod doctor` non-zero readiness behavior.

---

### Task 1: Rename Optional AI CLI Warning Category

**Files:**
- Modify: `dot_local/lib/terrapod/install-warnings.sh`
- Modify: `tests/terrapod_command_test.sh`
- Modify: `tests/chezmoiignore_test.sh`
- Modify: `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`

- [ ] **Step 1: Update failing category expectations**

In `tests/terrapod_command_test.sh`, change the category list and marker setup assertions from `ai-cli-tools` to `optional-ai-cli-tools`:

```sh
expected_marker_categories="$(printf '%s\n' homebrew-core homebrew-desktop-apps ubuntu-bootstrap shell-integrations mise-tools optional-ai-cli-tools)"

HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c \
  '. "$1"; terrapod_install_warning_write optional-ai-cli-tools "AI CLI tool install needs attention" "Rerun tpod apply after network access is restored."' \
  sh "$install_warnings_lib"

expected_marker_list="$(printf '%s\n' homebrew-core optional-ai-cli-tools)"
```

In `tests/chezmoiignore_test.sh`, change disabled cleanup expectations:

```sh
assert_contains_text "$disabled_ai_cli_tools_cleanup" "clear_install_warning optional-ai-cli-tools" "disabled Optional AI Tool Stack renders stale marker cleanup"

HOME="$ai_marker_home" XDG_STATE_HOME="$ai_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write optional-ai-cli-tools "Optional AI CLI tool install needs attention" "Rerun tpod apply after network access is restored."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"
```

- [ ] **Step 2: Run tests to verify category expectations fail**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: FAIL because `install-warnings.sh` still recognizes `ai-cli-tools`, not `optional-ai-cli-tools`.

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: FAIL because the rendered AI CLI installer still clears/writes `ai-cli-tools`.

- [ ] **Step 3: Rename category in helper and installer template**

In `dot_local/lib/terrapod/install-warnings.sh`, replace the category slug:

```sh
terrapod_install_warning_categories() {
  printf '%s\n' \
    homebrew-core \
    homebrew-desktop-apps \
    ubuntu-bootstrap \
    shell-integrations \
    mise-tools \
    optional-ai-cli-tools
}

terrapod_install_warning_is_category() {
  case "$1" in
    homebrew-core|homebrew-desktop-apps|ubuntu-bootstrap|shell-integrations|mise-tools|optional-ai-cli-tools)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}
```

In `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`, introduce and use:

```sh
AI_CLI_WARNING_CATEGORY=optional-ai-cli-tools
```

Then replace direct `ai-cli-tools` marker calls with `"$AI_CLI_WARNING_CATEGORY"`.

- [ ] **Step 4: Run tests to verify category rename passes**

Run:

```bash
sh tests/terrapod_command_test.sh
sh tests/chezmoiignore_test.sh
```

Expected: PASS for marker category and disabled cleanup checks. Other failures should be investigated before proceeding.

- [ ] **Step 5: Commit**

```bash
git add dot_local/lib/terrapod/install-warnings.sh .chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl tests/terrapod_command_test.sh tests/chezmoiignore_test.sh docs/superpowers/plans/2026-06-04-optional-ai-tool-stack-skip-retry-warnings.md
git commit -m "test: rename optional ai cli warning marker"
```

---

### Task 2: Add Installer Skip, Retry, and Safety Coverage

**Files:**
- Modify: `tests/chezmoiignore_test.sh`

- [ ] **Step 1: Add expected PATH skip behavior test**

After the existing static AI installer assertions in `tests/chezmoiignore_test.sh`, add a rendered-installer test that places `agy`, `claude`, and `codex` only under `$HOME/.local/bin`, runs the rendered installer with a minimal external `PATH`, and asserts `curl` is not called:

```sh
ai_skip_home="$tmp_dir/ai-skip-home"
ai_skip_state="$tmp_dir/ai-skip-state"
ai_skip_log="$tmp_dir/ai-skip-curl.log"
mkdir -p "$ai_skip_home/.local/bin"
for command_name in agy claude codex; do
  write_stub "$ai_skip_home/.local/bin/$command_name" 'exit 0'
done
write_stub "$ai_skip_home/.local/bin/curl" \
  'printf "%s\n" "$*" >>"$AI_SKIP_LOG"' \
  'exit 97'

ai_cli_tools_installer_script="$tmp_dir/ai-cli-tools-installer.sh"
printf '%s\n' "$ai_cli_tools_installer" >"$ai_cli_tools_installer_script"
sh -n "$ai_cli_tools_installer_script" || fail "rendered Optional AI Tool Stack installer should be valid sh"

if ! HOME="$ai_skip_home" XDG_STATE_HOME="$ai_skip_state" AI_SKIP_LOG="$ai_skip_log" PATH="/usr/bin:/bin" sh "$ai_cli_tools_installer_script" >"$tmp_dir/ai-skip.out" 2>"$tmp_dir/ai-skip.err"; then
  fail "Optional AI Tool Stack installer skips commands found on expected PATH"
fi
if [ -e "$ai_skip_log" ]; then
  fail "Optional AI Tool Stack installer does not download installers for commands already on expected PATH"
fi
if [ -e "$ai_skip_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "Optional AI Tool Stack installer does not leave warning marker after full skip"
fi
pass "Optional AI Tool Stack installer skips commands found on expected PATH"
```

- [ ] **Step 2: Add partial failure retry test**

Add a fake installer harness in `tests/chezmoiignore_test.sh` that:

- First run installs `agy` and `codex`, fails `claude`, exits successfully, and writes one `optional-ai-cli-tools` marker mentioning `claude`.
- Second run starts with `agy` and `codex` already present, succeeds only `claude`, skips `agy`/`codex`, and clears the marker.

Use shell snippets like:

```sh
write_stub "$ai_retry_home/.local/bin/bash" 'exec /bin/sh "$@"'
write_stub "$ai_retry_home/.local/bin/curl" \
  'url=' \
  'output=' \
  'while [ "$#" -gt 0 ]; do' \
  '  case "$1" in' \
  '    -o) shift; output="$1" ;;' \
  '    http*) url="$1" ;;' \
  '  esac' \
  '  shift' \
  'done' \
  'printf "%s\n" "$url" >>"$AI_RETRY_LOG"' \
  'case "$url" in' \
  '  https://antigravity.google/cli/install.sh)' \
  '    printf "%s\n" "#!/bin/sh" "printf \"%s\\n\" agy >>\"$AI_RETRY_RUN_LOG\"" "mkdir -p \"\\$HOME/.local/bin\"" "printf \"%s\\n\" \"#!/bin/sh\" \"exit 0\" >\"\\$HOME/.local/bin/agy\"" "chmod +x \"\\$HOME/.local/bin/agy\"" >"$output" ;;' \
  '  https://claude.ai/install.sh)' \
  '    if [ "${AI_RETRY_CLAUDE_FAIL:-}" = "1" ]; then' \
  '      printf "%s\n" "#!/bin/sh" "printf \"%s\\n\" claude-fail >>\"$AI_RETRY_RUN_LOG\"" "exit 42" >"$output"' \
  '    else' \
  '      printf "%s\n" "#!/bin/sh" "printf \"%s\\n\" claude >>\"$AI_RETRY_RUN_LOG\"" "mkdir -p \"\\$HOME/.local/bin\"" "printf \"%s\\n\" \"#!/bin/sh\" \"exit 0\" >\"\\$HOME/.local/bin/claude\"" "chmod +x \"\\$HOME/.local/bin/claude\"" >"$output"' \
  '    fi ;;' \
  '  https://chatgpt.com/codex/install.sh)' \
  '    printf "%s\n" "#!/bin/sh" "printf \"codex env:%s path:%s\\n\" \"\\$CODEX_NON_INTERACTIVE\" \"\\$PATH\" >>\"$AI_RETRY_RUN_LOG\"" "mkdir -p \"\\$HOME/.local/bin\"" "printf \"%s\\n\" \"#!/bin/sh\" \"exit 0\" >\"\\$HOME/.local/bin/codex\"" "chmod +x \"\\$HOME/.local/bin/codex\"" >"$output" ;;' \
  '  *) exit 88 ;;' \
  'esac'
```

Assert after the first run:

```sh
if ! HOME="$ai_retry_home" XDG_STATE_HOME="$ai_retry_state" AI_RETRY_CLAUDE_FAIL=1 AI_RETRY_LOG="$ai_retry_log" AI_RETRY_RUN_LOG="$ai_retry_run_log" PATH="/usr/bin:/bin" sh "$ai_cli_tools_installer_script" >"$tmp_dir/ai-retry-first.out" 2>"$tmp_dir/ai-retry-first.err"; then
  fail "partial Optional AI Tool Stack failure records a marker without failing apply"
fi
if [ ! -x "$ai_retry_home/.local/bin/agy" ] || [ ! -x "$ai_retry_home/.local/bin/codex" ]; then
  fail "partial Optional AI Tool Stack failure leaves successful tools installed"
fi
if [ ! -f "$ai_retry_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "partial Optional AI Tool Stack failure writes optional-ai-cli-tools marker"
fi
ai_retry_marker="$(cat "$ai_retry_state/terrapod/install-warnings/optional-ai-cli-tools")"
assert_contains_text "$ai_retry_marker" "claude" "partial Optional AI Tool Stack marker includes failed tool names"
assert_not_contains_text "$ai_retry_marker" "agy, codex" "partial Optional AI Tool Stack marker does not list successful tools as failed"
assert_contains_text "$(cat "$ai_retry_run_log")" "codex env:1" "Codex installer runs with CODEX_NON_INTERACTIVE=1"
```

Assert after the second run:

```sh
: >"$ai_retry_log"
if ! HOME="$ai_retry_home" XDG_STATE_HOME="$ai_retry_state" AI_RETRY_CLAUDE_FAIL=0 AI_RETRY_LOG="$ai_retry_log" AI_RETRY_RUN_LOG="$ai_retry_run_log" PATH="/usr/bin:/bin" sh "$ai_cli_tools_installer_script" >"$tmp_dir/ai-retry-second.out" 2>"$tmp_dir/ai-retry-second.err"; then
  fail "Optional AI Tool Stack retry succeeds after installing only missing tools"
fi
second_retry_urls="$(cat "$ai_retry_log")"
assert_contains_text "$second_retry_urls" "https://claude.ai/install.sh" "retry downloads the missing Claude installer"
assert_not_contains_text "$second_retry_urls" "https://antigravity.google/cli/install.sh" "retry skips already installed agy"
assert_not_contains_text "$second_retry_urls" "https://chatgpt.com/codex/install.sh" "retry skips already installed codex"
if [ -e "$ai_retry_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "successful Optional AI Tool Stack retry clears warning marker"
fi
pass "Optional AI Tool Stack retry installs only missing expected commands"
```

- [ ] **Step 3: Add platform expected PATH coverage**

Add static rendered-script assertions so the test closes both managed user-bin and platform tool path requirements without writing to real platform directories:

```sh
assert_contains_text "$ai_cli_tools_installer" 'AI_CLI_EXPECTED_PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"' \
  "Optional AI Tool Stack installer includes macOS managed user-bin and platform tool lookup path"
assert_contains_text "$ai_cli_tools_installer" 'AI_CLI_EXPECTED_PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"' \
  "Optional AI Tool Stack installer includes Linux managed user-bin and platform tool lookup path"
assert_contains_text "$development_workspace_ai_installer" 'AI_CLI_EXPECTED_PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"' \
  "Optional Development Workspace AI installer includes macOS expected lookup path"
assert_contains_text "$development_workspace_ai_installer" 'AI_CLI_EXPECTED_PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"' \
  "Optional Development Workspace AI installer includes Linux expected lookup path"
```

- [ ] **Step 4: Add official-only safety assertions**

Extend the static assertions around `ai_cli_tools_installer` and `development_workspace_ai_installer`:

```sh
for forbidden_text in \
  "GITHUB_TOKEN" \
  "Authorization:" \
  "api.github.com" \
  "sed -i" \
  "apply_patch" \
  "patch " \
  "yes |" \
  "printf 'y" \
  "printf \"y" \
  "| sh" \
  "| bash"
do
  assert_not_contains_text "$ai_cli_tools_installer" "$forbidden_text" "enableAiCliTools does not inject tokens, patch installers, or pipe prompt answers: $forbidden_text"
  assert_not_contains_text "$development_workspace_ai_installer" "$forbidden_text" "enableDevelopmentWorkspace does not inject tokens, patch installers, or pipe prompt answers: $forbidden_text"
done
```

Keep the existing official URL assertions:

```sh
https://antigravity.google/cli/install.sh
https://claude.ai/install.sh
https://chatgpt.com/codex/install.sh
```

- [ ] **Step 5: Run tests to verify new behavior coverage fails**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: FAIL because the installer still downloads all AI installers, exits at the first failure, and does not accumulate failed tool names.

- [ ] **Step 6: Commit**

```bash
git add tests/chezmoiignore_test.sh
git commit -m "test: cover optional ai cli retry behavior"
```

---

### Task 3: Implement Idempotent Official AI CLI Installer

**Files:**
- Modify: `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`

- [ ] **Step 1: Add expected lookup path helpers**

Add helpers near the top of the enabled branch:

```sh
AI_CLI_WARNING_CATEGORY=optional-ai-cli-tools

case "$(uname -s 2>/dev/null || printf unknown)" in
  Darwin)
    AI_CLI_EXPECTED_PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
    ;;
  *)
    AI_CLI_EXPECTED_PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
    ;;
esac

command_available_on_expected_path() {
  command_name="$1"
  PATH="$AI_CLI_EXPECTED_PATH" command -v "$command_name" >/dev/null 2>&1
}
```

- [ ] **Step 2: Add failure accumulation helpers**

Replace hard `exit 1` paths in optional AI installation with accumulated failures:

```sh
failed_tools=

append_failed_tool() {
  command_name="$1"

  case ", $failed_tools, " in
    *", $command_name, "*)
      return 0
      ;;
  esac

  if [ -z "$failed_tools" ]; then
    failed_tools="$command_name"
  else
    failed_tools="$failed_tools, $command_name"
  fi
}

finish_ai_cli_installers() {
  if [ -n "$failed_tools" ]; then
    mark_install_warning \
      "$AI_CLI_WARNING_CATEGORY" \
      "Optional AI CLI tool install needs attention: $failed_tools" \
      "Failed tools: $failed_tools. Review installer output, fix network or installer requirements, then rerun tpod apply."
    return 0
  fi

  clear_install_warning "$AI_CLI_WARNING_CATEGORY"
}
```

- [ ] **Step 3: Replace installer runner with per-command skip and retry logic**

Use one runner for all three official installers:

```sh
run_installer() {
  label="$1"
  command_name="$2"
  shell_name="$3"
  installer_url="$4"
  mode="${5:-}"

  if command_available_on_expected_path "$command_name"; then
    echo "Skipping $label; $command_name is already available on Terrapod's expected PATH."
    return 0
  fi

  if ! command_available_on_expected_path curl; then
    echo "curl is required to install $label." >&2
    append_failed_tool "$command_name"
    return 0
  fi

  if ! command_available_on_expected_path "$shell_name"; then
    echo "$shell_name is required to install $label." >&2
    append_failed_tool "$command_name"
    return 0
  fi

  installer_path="$(mktemp "${TMPDIR:-/tmp}/terrapod-ai-installer.XXXXXX")"
  installer_paths="$installer_paths $installer_path"

  echo "Installing $label through official installer: $installer_url"
  if ! PATH="$AI_CLI_EXPECTED_PATH" curl -fsSL "$installer_url" -o "$installer_path"; then
    append_failed_tool "$command_name"
    return 0
  fi

  if [ "$mode" = "codex" ]; then
    if ! CODEX_NON_INTERACTIVE=1 PATH="$AI_CLI_EXPECTED_PATH" "$shell_name" "$installer_path"; then
      append_failed_tool "$command_name"
      return 0
    fi
  else
    if ! PATH="$AI_CLI_EXPECTED_PATH" "$shell_name" "$installer_path"; then
      append_failed_tool "$command_name"
      return 0
    fi
  fi

  if ! command_available_on_expected_path "$command_name"; then
    echo "$label installer completed but $command_name is not available on Terrapod's expected PATH." >&2
    append_failed_tool "$command_name"
  fi
}

run_installer "Antigravity CLI" agy bash "https://antigravity.google/cli/install.sh"
run_installer "Claude Code" claude bash "https://claude.ai/install.sh"
run_installer "Codex" codex sh "https://chatgpt.com/codex/install.sh" codex
finish_ai_cli_installers
```

Remove the old `require_command` and `run_codex_installer` functions.

- [ ] **Step 4: Run behavior tests**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: PASS for rendered installer skip/retry/security checks.

- [ ] **Step 5: Commit**

```bash
git add .chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl
git commit -m "fix: make optional ai cli installer retryable"
```

---

### Task 4: Verify Command Surface and Apply/Doctor Behavior

**Files:**
- Modify only if tests expose a real regression:
  - `dot_local/bin/executable_terrapod`
  - `tests/terrapod_command_test.sh`

- [ ] **Step 1: Run command regression tests**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: PASS with `optional-ai-cli-tools` appearing in marker category/list/status/doctor/apply output where install warnings are summarized. Preserve the existing enabled-stack readiness behavior: `tpod doctor` should still fail when the **Optional AI Tool Stack** is enabled and `agy`, `claude`, or `codex` are missing. Also preserve the existing disabled-stack behavior: `tpod doctor` should not fail merely because disabled Optional AI Tool Stack tools are absent.

- [ ] **Step 2: Run syntax checks for changed shell files**

Run:

```bash
sh -n dot_local/lib/terrapod/install-warnings.sh
sh -n dot_local/bin/executable_terrapod
chezmoi execute-template --override-data '{"chezmoi":{"os":"linux","sourceDir":"."},"enableAiCliTools":true,"enableDevelopmentWorkspace":false}' --file .chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl > /tmp/terrapod-ai-cli-tools-check.sh
sh -n /tmp/terrapod-ai-cli-tools-check.sh
```

Expected: all commands exit 0.

- [ ] **Step 3: Run full repository shell tests**

Run:

```bash
for test_script in tests/*_test.sh tests/*.zsh; do
  case "$test_script" in
    *.zsh) zsh "$test_script" ;;
    *) sh "$test_script" ;;
  esac
done
```

Expected: PASS. If a pre-existing unrelated failure appears, capture the failing script and output, then rerun the focused changed tests to confirm this issue's behavior.

- [ ] **Step 4: Commit any test-driven fixes**

If Step 1-3 required additional fixes:

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh tests/chezmoiignore_test.sh dot_local/lib/terrapod/install-warnings.sh .chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl
git commit -m "fix: align optional ai cli warning checks"
```

If no additional fixes were needed, do not create an empty commit.

---

### Task 5: Publish Ready-for-Review PR

**Files:**
- No code changes expected.

- [ ] **Step 1: Verify final status and diff**

Run:

```bash
git status -sb
git log --oneline --decorate -5
git diff --stat origin/main...HEAD
```

Expected: only issue #103 files changed and all changes committed.

- [ ] **Step 2: Run final focused tests**

Run:

```bash
sh tests/chezmoiignore_test.sh
sh tests/terrapod_command_test.sh
```

Expected: PASS.

- [ ] **Step 3: Push branch**

```bash
git push -u origin codex/issue-103-optional-ai-tool-stack
```

- [ ] **Step 4: Create ready-for-review PR**

Create a non-draft PR against `main` with a title like:

```text
Optional AI Tool Stack skip retry warnings
```

PR body must include:

- What changed: expected PATH skip checks, per-tool retry, `optional-ai-cli-tools` marker.
- Why: issue #103 idempotency and recovery requirements.
- Impact: existing `agy`, `claude`, or `codex` commands are skipped; partial failures retry only missing commands.
- Validation: focused tests and full shell test loop results.
