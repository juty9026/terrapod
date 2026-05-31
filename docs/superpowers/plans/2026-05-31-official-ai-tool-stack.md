# Official AI Tool Stack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Switch Terrapod's **Optional AI Tool Stack** from Gemini CLI/npm installation to Antigravity CLI, Claude Code, and Codex installed through official vendor installer scripts.

**Architecture:** Keep the existing `enableAiCliTools` setting and effective-stack logic, but change the declared binary set to `agy`, `claude`, and `codex`. Run AI CLI vendor installers as a before-apply chezmoi script so any vendor shell-profile edits are overwritten by Terrapod's managed shell files, and remove the legacy Antigravity app-bundle PATH snippet from management.

**Tech Stack:** POSIX shell, chezmoi templates, Zellij KDL layout, Markdown documentation, shell regression tests.

---

## File Structure

- Modify: `tests/chezmoiignore_test.sh`
  - Verifies the rendered AI CLI installer uses official vendor installer URLs, does not render global npm package installation, no longer manages legacy Antigravity app-bundle PATH snippets, and changes the development workspace pane from Gemini to Antigravity CLI.
- Modify: `tests/terrapod_command_test.sh`
  - Verifies `terrapod status` and `terrapod doctor` expect `agy`, `claude`, and `codex`, and keep disabled-stack behavior/broad-upgrade guards intact.
- Modify: `tests/readme_optional_stack_profiles_test.sh`
  - Verifies README describes Antigravity CLI, official installers, and non-destructive legacy npm behavior.
- Delete: `dot_config/zsh/path.d/antigravity.zsh.tmpl`
  - Removes legacy Antigravity Desktop and Antigravity IDE app-bundle PATH snippets from Terrapod management.
- Rename: `.chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl` to `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`
  - Installs Antigravity CLI, Claude Code, and Codex through official scripts before final managed shell files are applied.
- Modify: `.chezmoiignore`
  - Points Optional AI Tool Stack ignore rules at the before-apply installer and removes the deleted Antigravity PATH snippet rule.
- Modify: `dot_config/zellij/layouts/dev.kdl`
  - Replaces the Gemini pane with an Antigravity CLI pane using `agy --dangerously-skip-permissions`.
- Modify: `dot_local/bin/executable_terrapod`
  - Changes AI stack status/warning/doctor checks to `agy claude codex`.
- Modify: `README.md`, `README.ko.md`, `CONTEXT.md`
  - Updates user-facing and domain language for the new AI CLI stack while preserving existing heading parity.

---

### Task 1: Write Failing Regression Tests

**Files:**
- Modify: `tests/chezmoiignore_test.sh`
- Modify: `tests/terrapod_command_test.sh`
- Modify: `tests/readme_optional_stack_profiles_test.sh`

- [ ] **Step 1: Update installer and management tests first**

Change the Optional AI Tool Stack installer path assertions in `tests/chezmoiignore_test.sh` from:

```sh
".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl"
```

to:

```sh
".chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl"
```

Replace the Antigravity PATH snippet include/exclude assertions with this file-absence check:

```sh
if [ -e "$repo_root/dot_config/zsh/path.d/antigravity.zsh.tmpl" ]; then
  fail "legacy Antigravity app-bundle PATH snippet is no longer managed"
fi

pass "legacy Antigravity app-bundle PATH snippet is no longer managed"
```

Replace the npm package loop with official installer URL assertions:

```sh
for installer_url in \
  "https://antigravity.google/cli/install.sh" \
  "https://claude.ai/install.sh" \
  "https://chatgpt.com/codex/install.sh"
do
  assert_contains_text "$ai_cli_tools_installer" "$installer_url" "enableAiCliTools renders official AI CLI installer URL: $installer_url"
  assert_contains_text "$development_workspace_ai_installer" "$installer_url" "enableDevelopmentWorkspace renders official AI CLI installer URL: $installer_url"
done

for legacy_text in \
  "@anthropic-ai/claude-code" \
  "@google/gemini-cli" \
  "@openai/codex" \
  "npm install -g" \
  "npm uninstall"
do
  assert_not_contains_text "$ai_cli_tools_installer" "$legacy_text" "enableAiCliTools does not render legacy npm AI CLI management: $legacy_text"
  assert_not_contains_text "$development_workspace_ai_installer" "$legacy_text" "enableDevelopmentWorkspace does not render legacy npm AI CLI management: $legacy_text"
done
```

Replace the assistant pane expectations with:

```sh
for pane in CLAUDE CODEX ANTIGRAVITY; do
  if ! printf '%s\n' "$development_workspace_zellij_layout" |
    grep -E "pane name=\"${pane}\" .*start_suspended=true" >/dev/null; then
    fail "enableDevelopmentWorkspace starts assistant panes suspended"
  fi
done

pass "enableDevelopmentWorkspace starts assistant panes suspended"

if printf '%s\n' "$development_workspace_zellij_layout" | grep -F 'command="gemini"' >/dev/null; then
  fail "enableDevelopmentWorkspace no longer launches Gemini CLI"
fi

pass "enableDevelopmentWorkspace no longer launches Gemini CLI"

if ! printf '%s\n' "$development_workspace_zellij_layout" |
  grep -A2 'pane name="ANTIGRAVITY" command="agy"' |
  grep -F 'args "--dangerously-skip-permissions"' >/dev/null; then
  fail "enableDevelopmentWorkspace passes supported permission skip mode to the Antigravity pane"
fi

pass "enableDevelopmentWorkspace passes supported permission skip mode to the Antigravity pane"
```

- [ ] **Step 2: Update status/doctor tests first**

In `tests/terrapod_command_test.sh`, change status paths that currently stub `gemini claude codex` to stub `agy claude codex`, for example:

```sh
macos_status_path="$(status_doctor_path macos chezmoi git zsh mise brew nvim agy claude codex zellij ghostty cmux op)"
```

Change expected AI stack output strings from:

```text
Optional AI Tool Stack: enabled (tools available: gemini, claude, codex)
Optional AI Tool Stack: enabled (missing tools: gemini, claude, codex)
Warning: Optional AI Tool Stack is enabled but missing tools: gemini, claude, codex
warn - Optional AI Tool Stack is enabled but missing tools: gemini, claude, codex
```

to:

```text
Optional AI Tool Stack: enabled (tools available: agy, claude, codex)
Optional AI Tool Stack: enabled (missing tools: agy, claude, codex)
Warning: Optional AI Tool Stack is enabled but missing tools: agy, claude, codex
warn - Optional AI Tool Stack is enabled but missing tools: agy, claude, codex
```

Change the disabled-stack negative assertion to:

```sh
assert_not_contains "$ubuntu_status_output" "missing tools: agy" "Terrapod status distinguishes disabled Optional AI Tool Stack from missing tools"
```

- [ ] **Step 3: Update README behavior tests first**

Add these assertions to `tests/readme_optional_stack_profiles_test.sh` near the existing `enableAiCliTools` checks:

```sh
assert_key_row_contains '`enableAiCliTools`' 'Antigravity CLI, Claude Code, and Codex' \
  "README documents the new Optional AI Tool Stack membership"
assert_key_row_contains '`enableAiCliTools`' 'official vendor installers' \
  "README documents official AI CLI installers"
assert_contains 'Existing npm-installed AI CLIs are left unmanaged; Terrapod does not uninstall or warn merely because they remain on a machine.' \
  "README documents non-destructive legacy npm AI CLI migration"
```

- [ ] **Step 4: Run tests to verify they fail**

Run:

```sh
sh tests/chezmoiignore_test.sh
```

Expected: FAIL because `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl` does not exist yet, installer content still uses npm packages, and the layout still contains Gemini.

Run:

```sh
sh tests/terrapod_command_test.sh
```

Expected: FAIL because `terrapod status` and `terrapod doctor` still report `gemini, claude, codex`.

Run:

```sh
sh tests/readme_optional_stack_profiles_test.sh
```

Expected: FAIL because README still describes npm-installed Gemini CLI, Claude Code, and Codex.

---

### Task 2: Implement Official Installer and Management Boundary

**Files:**
- Delete: `dot_config/zsh/path.d/antigravity.zsh.tmpl`
- Rename: `.chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl` to `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`
- Modify: `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`
- Modify: `.chezmoiignore`

- [ ] **Step 1: Delete the legacy Antigravity PATH snippet**

Remove `dot_config/zsh/path.d/antigravity.zsh.tmpl` entirely. The existing `dot_zshenv.tmpl` already puts `~/.local/bin` on PATH, which is where Antigravity CLI's official installer places `agy`.

- [ ] **Step 2: Rename the AI installer script**

Rename:

```text
.chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl
```

to:

```text
.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl
```

This makes the vendor installer scripts run before chezmoi writes the final managed shell files.

- [ ] **Step 3: Replace npm/mise installer content**

Replace the renamed installer with:

```sh
{{- if and (or (eq .chezmoi.os "darwin") (eq .chezmoi.os "linux")) (or (default false (get . "enableAiCliTools")) (default false (get . "enableDevelopmentWorkspace"))) -}}
#!/bin/sh
set -eu

installer_paths=

cleanup_installers() {
  for installer_path in $installer_paths; do
    rm -f "$installer_path"
  done
}

trap cleanup_installers EXIT HUP INT TERM

require_command() {
  command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "$command_name is required to install optional AI CLI tools." >&2
    exit 1
  fi
}

run_installer() {
  label="$1"
  shell_name="$2"
  installer_url="$3"
  shift 3

  installer_path="$(mktemp "${TMPDIR:-/tmp}/terrapod-ai-installer.XXXXXX")"
  installer_paths="$installer_paths $installer_path"

  echo "Installing $label through official installer: $installer_url"
  curl -fsSL "$installer_url" -o "$installer_path"
  "$shell_name" "$installer_path" "$@"
}

require_command curl
require_command bash

run_installer "Antigravity CLI" bash "https://antigravity.google/cli/install.sh" --skip-path --skip-aliases
run_installer "Claude Code" bash "https://claude.ai/install.sh"
run_installer "Codex" sh "https://chatgpt.com/codex/install.sh"
{{- end }}
```

- [ ] **Step 4: Update `.chezmoiignore`**

Change:

```text
.chezmoiscripts/60-install-ai-cli-tools.sh
```

only if the target path needs to remain the same; otherwise leave the target path unchanged because both before/after source names map to the same script target. Remove this entire block because the source file is gone:

```gotemplate
{{ if not (and (eq .chezmoi.os "darwin") (or (default false (get . "enableAiCliTools")) (default false (get . "enableDevelopmentWorkspace")))) }}
# Antigravity PATH snippet is opt-in for macOS AI/development profiles.
.config/zsh/path.d/antigravity.zsh
{{ end }}
```

- [ ] **Step 5: Run focused installer tests**

Run:

```sh
sh tests/chezmoiignore_test.sh
```

Expected: still FAIL until Task 3 updates the development workspace layout, but installer URL and deleted PATH snippet assertions pass.

---

### Task 3: Update Runtime Validation and Development Workspace

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Modify: `dot_config/zellij/layouts/dev.kdl`

- [ ] **Step 1: Change status warning expected tools**

In `dot_local/bin/executable_terrapod`, change:

```sh
missing="$(missing_tools gemini claude codex)"
```

to:

```sh
missing="$(missing_tools agy claude codex)"
```

- [ ] **Step 2: Change status tool listing**

Change:

```sh
print_stack_status "Optional AI Tool Stack" "$(effective_ai_cli_tools_enabled "$config_file")" gemini claude codex
```

to:

```sh
print_stack_status "Optional AI Tool Stack" "$(effective_ai_cli_tools_enabled "$config_file")" agy claude codex
```

- [ ] **Step 3: Change doctor expected tools**

Change:

```sh
gemini claude codex
```

in the `doctor_check_optional_stack "Optional AI Tool Stack"` call to:

```sh
agy claude codex
```

- [ ] **Step 4: Replace the Gemini pane**

In `dot_config/zellij/layouts/dev.kdl`, replace:

```kdl
                pane name="GEMINI" command="gemini" start_suspended=true {
                    args "--yolo"
                }
```

with:

```kdl
                pane name="ANTIGRAVITY" command="agy" start_suspended=true {
                    args "--dangerously-skip-permissions"
                }
```

- [ ] **Step 5: Run focused validation tests**

Run:

```sh
sh tests/terrapod_command_test.sh
```

Expected: status/doctor AI CLI expectations pass; if the pre-existing `Killed: 9` baseline apply failure repeats, capture the failing section and continue with the remaining focused tests.

Run:

```sh
sh tests/chezmoiignore_test.sh
```

Expected: PASS after Task 2 and Task 3.

---

### Task 4: Update Documentation and Domain Language

**Files:**
- Modify: `README.md`
- Modify: `README.ko.md`
- Modify: `CONTEXT.md`

- [ ] **Step 1: Update Canonical README AI row**

Change the `enableAiCliTools` row in `README.md` to:

```markdown
| `enableAiCliTools` | `false` | Installs Antigravity CLI, Claude Code, and Codex through official vendor installers. Existing npm-installed AI CLIs are left unmanaged. |
```

- [ ] **Step 2: Add non-destructive migration sentence**

After the existing optional stack opt-out sentence in `README.md`, add:

```markdown
Existing npm-installed AI CLIs are left unmanaged; Terrapod does not uninstall or warn merely because they remain on a machine.
```

- [ ] **Step 3: Update Korean README AI row and migration sentence**

Change the `enableAiCliTools` row in `README.ko.md` to:

```markdown
| `enableAiCliTools` | `false` | official vendor installer를 통해 Antigravity CLI, Claude Code, Codex를 설치합니다. 기존 npm-installed AI CLI는 unmanaged 상태로 둡니다. |
```

After the Korean optional stack opt-out sentence, add:

```markdown
기존 npm-installed AI CLI는 unmanaged 상태로 남겨 둡니다. Terrapod은 해당 도구가 machine에 남아 있다는 이유만으로 uninstall하거나 warning을 내지 않습니다.
```

- [ ] **Step 4: Update CONTEXT.md**

Change:

```markdown
- Gemini CLI, Claude Code, and Codex belong to the **Optional AI Tool Stack**, not the **Core Shell Stack**.
```

to:

```markdown
- Antigravity CLI, Claude Code, and Codex belong to the **Optional AI Tool Stack**, not the **Core Shell Stack**.
- Existing npm-installed AI CLIs are unmanaged legacy tools; Terrapod does not uninstall or warn merely because they remain on a machine.
```

- [ ] **Step 5: Run README tests**

Run:

```sh
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
```

Expected: PASS.

---

### Task 5: Full Verification and PR Preparation

**Files:**
- Review all changed files with `git diff`
- No new production files beyond the renamed installer and plan

- [ ] **Step 1: Search for stale Gemini/npm installer declarations**

Run:

```sh
rg -n "gemini|GEMINI|@google/gemini-cli|@anthropic-ai/claude-code|npm install -g|npm uninstall|run_onchange_after_60-install-ai-cli-tools|dot_config/zsh/path.d/antigravity" . -g '!docs/superpowers/plans/**'
```

Expected: no stale Optional AI Tool Stack declarations remain. Mentions in unrelated docs should be reviewed and either updated or justified.

- [ ] **Step 2: Verify broad-upgrade guards**

Run:

```sh
sh tests/terrapod_command_test.sh
```

Expected: PASS, or the same pre-existing `Killed: 9` apply failure captured with narrower preceding status/doctor assertions passing.

- [ ] **Step 3: Run the full test suite**

Run:

```sh
for test in tests/*_test.sh tests/*_test.zsh; do
  printf '==> %s\n' "$test"
  case "$test" in
    *.zsh) zsh "$test" ;;
    *) sh "$test" ;;
  esac
done
```

Expected: PASS. If `tests/terrapod_command_test.sh` repeats the baseline `Killed: 9` failure, rerun the failing test once and document the exact failure as environmental if it is unchanged from baseline.

- [ ] **Step 4: Commit and publish**

After tests are verified, run:

```sh
git status -sb
git add .chezmoiignore .chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl dot_config/zellij/layouts/dev.kdl dot_local/bin/executable_terrapod README.md README.ko.md CONTEXT.md tests/chezmoiignore_test.sh tests/terrapod_command_test.sh tests/readme_optional_stack_profiles_test.sh docs/superpowers/plans/2026-05-31-official-ai-tool-stack.md
git add -u .chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl dot_config/zsh/path.d/antigravity.zsh.tmpl
git commit -m "Switch optional AI stack to official installers"
git push -u origin feat/issue-82-official-ai-tool-stack
```

Open a ready-for-review PR against the repository default branch with a body that summarizes the new AI CLI stack, official installer paths, non-destructive legacy behavior, and verification commands.

---

## Self-Review

- Spec coverage: The plan maps every Issue #82 acceptance criterion to tests and implementation: stack membership, official installers, no npm uninstall/warnings, before-apply shell ownership, removal of legacy Antigravity PATH snippets, Zellij panes, status/doctor, README/Korean README, tests, and broad-upgrade guard verification.
- Placeholder scan: No `TBD`, `TODO`, "implement later", or unspecified edge handling remains.
- Type/name consistency: The expected binary names are consistently `agy`, `claude`, and `codex`; the Zellij pane name is consistently `ANTIGRAVITY`; the installer file is consistently `run_onchange_before_60-install-ai-cli-tools.sh.tmpl`.
