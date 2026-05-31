# Remove cmux from macOS Desktop App Stack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove cmux from Terrapod's declared macOS Desktop App Stack while leaving existing user-installed cmux apps and settings untouched.

**Architecture:** Keep the existing `enableMacosAppGroupTerminalApps` setting and macOS App Group flow, but change the terminal-apps group contents from Ghostty+cmux to Ghostty only. Stop managing cmux user settings by excluding `.config/cmux` from managed paths and removing the tracked cmux source file; do not add any uninstall, cleanup, or deletion behavior.

**Tech Stack:** POSIX shell, chezmoi templates and ignore rules, shell-based regression tests, Markdown documentation.

---

## File Structure

- Modify `Brewfile.macos-desktop-apps.tmpl` so the terminal-apps macOS App Group renders only `cask "ghostty"`.
- Modify `.chezmoiignore` so `.config/cmux` and `.config/cmux/**` are ignored for every profile, including macOS.
- Delete `dot_config/cmux/private_settings.json` so Terrapod no longer carries cmux settings as declared state.
- Modify `dot_local/bin/executable_terrapod` so status output says `terminal-apps: enabled (Ghostty)`.
- Modify `tests/chezmoiignore_test.sh` to expect no cmux cask, to verify cmux settings are excluded from managed targets, and to keep automation/launcher/monitoring behavior covered.
- Modify `tests/terrapod_command_test.sh` to expect Ghostty-only status output while leaving broad-upgrade guard tests intact.
- Modify `tests/readme_optional_stack_profiles_test.sh` to remove the cmux README expectation and keep the Ghostty terminal-apps expectation.
- Modify `README.md`, `README.ko.md`, and `CONTEXT.md` so user-facing docs and domain docs describe terminal-apps as Ghostty-only.

## Task 1: Desktop App Template and Managed Path Tests

**Files:**
- Modify: `Brewfile.macos-desktop-apps.tmpl`
- Modify: `.chezmoiignore`
- Delete: `dot_config/cmux/private_settings.json`
- Modify: `tests/chezmoiignore_test.sh`

- [ ] **Step 1: Write failing Brewfile and managed-path expectations**

In `tests/chezmoiignore_test.sh`, replace the terminal-apps cmux expectation:

```sh
assert_contains_text "$terminal_apps_brewfile" 'cask "ghostty"' "terminal-apps group renders Ghostty"
assert_not_contains_text "$terminal_apps_brewfile" 'cask "cmux"' "terminal-apps group does not render cmux"
assert_not_contains_text "$terminal_apps_brewfile" 'cask "hammerspoon"' "terminal-apps group does not render automation casks"
```

Replace the user-scoped app config loop so cmux is no longer expected as a managed target:

```sh
for app_config in \
  ".config/ghostty/config" \
  ".config/karabiner/karabiner.json" \
  ".hammerspoon/init.lua"
do
  if ! printf '%s\n' "$macos_managed_targets" | grep -Fx "$app_config" >/dev/null; then
    fail "macOS default manages user-scoped app config: $app_config"
  fi

  if ! printf '%s\n' "$macos_terminal_apps_managed_targets" | grep -Fx "$app_config" >/dev/null; then
    fail "terminal-apps group manages user-scoped app config: $app_config"
  fi
done

pass "user-scoped macOS app config remains managed regardless of app group selection"
```

Immediately after that loop, add explicit cmux exclusion assertions:

```sh
assert_managed_paths_exclude_prefix \
  "$macos_managed_targets" \
  ".config/cmux" \
  "macOS default does not manage cmux settings"

assert_managed_paths_exclude_prefix \
  "$macos_terminal_apps_managed_targets" \
  ".config/cmux" \
  "terminal-apps group does not manage cmux settings"
```

- [ ] **Step 2: Run the targeted test to verify it fails**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: FAIL on `terminal-apps group does not render cmux` or `terminal-apps group does not manage cmux settings`, proving the new assertions catch the existing cmux declaration.

- [ ] **Step 3: Remove cmux from the desktop app template**

Change `Brewfile.macos-desktop-apps.tmpl` from:

```gotemplate
# Rendered opt-in macOS Desktop App Stack.
{{ if default false (get . "enableMacosAppGroupTerminalApps") -}}
# terminal-apps macOS App Group
cask "ghostty"
cask "cmux"
{{ end -}}
```

to:

```gotemplate
# Rendered opt-in macOS Desktop App Stack.
{{ if default false (get . "enableMacosAppGroupTerminalApps") -}}
# terminal-apps macOS App Group
cask "ghostty"
{{ end -}}
```

- [ ] **Step 4: Stop managing cmux settings without deleting user settings**

Add this unconditional block near the top of `.chezmoiignore`, after repository-only entries and before optional stack blocks:

```gotemplate
# cmux settings are intentionally unmanaged; existing local settings are left untouched.
.config/cmux
.config/cmux/**
```

Delete the tracked source file:

```bash
git rm dot_config/cmux/private_settings.json
```

Do not add any `.chezmoiscripts` cleanup, uninstall command, `rm -rf`, `brew uninstall`, or cmux migration flow.

- [ ] **Step 5: Run the targeted test to verify it passes**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: PASS, including these lines:

```text
ok - terminal-apps group renders Ghostty
ok - terminal-apps group does not render cmux
ok - macOS default does not manage cmux settings
ok - terminal-apps group does not manage cmux settings
```

- [ ] **Step 6: Commit**

Run:

```bash
git add .chezmoiignore Brewfile.macos-desktop-apps.tmpl tests/chezmoiignore_test.sh dot_config/cmux/private_settings.json
git commit -m "Remove cmux from managed desktop app stack"
```

## Task 2: Terrapod Status Output

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Modify: `tests/terrapod_command_test.sh`

- [ ] **Step 1: Write failing status expectations**

In `tests/terrapod_command_test.sh`, replace both status assertions that expect `Ghostty and cmux`:

```sh
assert_contains "$macos_status_output" "terminal-apps: enabled (Ghostty)" "Terrapod status reports enabled Ghostty-only terminal-apps macOS App Group"
```

and:

```sh
assert_contains "$dotted_status_output" "terminal-apps: enabled (Ghostty)" "Terrapod status reads root dotted data keys for Ghostty-only macOS App Groups"
```

Update the fake status path for the macOS case from:

```sh
macos_status_path="$(status_doctor_path macos chezmoi git zsh mise brew nvim gemini claude codex zellij ghostty cmux op)"
```

to:

```sh
macos_status_path="$(status_doctor_path macos chezmoi git zsh mise brew nvim gemini claude codex zellij ghostty op)"
```

- [ ] **Step 2: Run the targeted test to verify it fails**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: FAIL on a status assertion because the command still prints `terminal-apps: enabled (Ghostty and cmux)`.

- [ ] **Step 3: Update Terrapod status copy**

In `dot_local/bin/executable_terrapod`, change the enabled terminal-apps line inside `print_macos_app_group_status` from:

```sh
printf '%s\n' "  terminal-apps: enabled (Ghostty and cmux)"
```

to:

```sh
printf '%s\n' "  terminal-apps: enabled (Ghostty)"
```

- [ ] **Step 4: Run the targeted test to verify it passes**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: PASS, including the updated Ghostty-only status assertions and the existing broad-upgrade guard assertions.

- [ ] **Step 5: Commit**

Run:

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh
git commit -m "Report terminal app group as Ghostty only"
```

## Task 3: Documentation and README Tests

**Files:**
- Modify: `README.md`
- Modify: `README.ko.md`
- Modify: `CONTEXT.md`
- Modify: `tests/readme_optional_stack_profiles_test.sh`

- [ ] **Step 1: Write failing README expectation updates**

In `tests/readme_optional_stack_profiles_test.sh`, keep the terminal-apps and Ghostty row expectations:

```sh
assert_key_row_contains '`enableMacosAppGroupTerminalApps`' 'terminal-apps' \
  "README documents terminal-apps group on its option row"
assert_key_row_contains '`enableMacosAppGroupTerminalApps`' 'Ghostty' \
  "README documents Ghostty on the terminal-apps option row"
```

Remove this cmux expectation:

```sh
assert_key_row_contains '`enableMacosAppGroupTerminalApps`' 'cmux' \
  "README documents cmux on the terminal-apps option row"
```

Add a README-wide cmux absence assertion after the Ghostty row expectation:

```sh
assert_not_contains 'cmux' \
  "README no longer documents cmux as part of the macOS Desktop App Stack"
```

Add the helper function near the other README assertion helpers:

```sh
assert_not_contains() {
  needle="$1"
  message="$2"

  if grep -F "$needle" "$readme" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}
```

- [ ] **Step 2: Run the README test to verify it fails**

Run:

```bash
sh tests/readme_optional_stack_profiles_test.sh
```

Expected: FAIL on `README no longer documents cmux as part of the macOS Desktop App Stack`.

- [ ] **Step 3: Update English README**

In `README.md`, change:

```markdown
- `terminal-apps`: Ghostty and cmux.
```

to:

```markdown
- `terminal-apps`: Ghostty.
```

Change:

```markdown
| `enableMacosAppGroupTerminalApps` | `false` | Installs the terminal-apps macOS App Group: Ghostty and cmux. |
```

to:

```markdown
| `enableMacosAppGroupTerminalApps` | `false` | Installs the terminal-apps macOS App Group: Ghostty. |
```

- [ ] **Step 4: Update Korean README**

In `README.ko.md`, change:

```markdown
- `terminal-apps`: Ghostty와 cmux.
```

to:

```markdown
- `terminal-apps`: Ghostty.
```

Change:

```markdown
| `enableMacosAppGroupTerminalApps` | `false` | terminal-apps macOS App Group인 Ghostty와 cmux를 설치합니다. |
```

to:

```markdown
| `enableMacosAppGroupTerminalApps` | `false` | terminal-apps macOS App Group인 Ghostty를 설치합니다. |
```

- [ ] **Step 5: Update domain docs**

In `CONTEXT.md`, change:

```markdown
- The terminal-apps **macOS App Group** contains Ghostty and cmux.
```

to:

```markdown
- The terminal-apps **macOS App Group** contains Ghostty.
```

- [ ] **Step 6: Run documentation tests**

Run:

```bash
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
```

Expected: both PASS.

- [ ] **Step 7: Commit**

Run:

```bash
git add README.md README.ko.md CONTEXT.md tests/readme_optional_stack_profiles_test.sh
git commit -m "Document terminal apps as Ghostty only"
```

## Task 4: Final Regression and Non-Destructive Guard

**Files:**
- Verify all changed files.

- [ ] **Step 1: Search for remaining cmux management references**

Run:

```bash
rg -n "cmux|cask \"cmux\"|\\.config/cmux|dot_config/cmux" .
```

Expected: only intentional references remain in `.chezmoiignore`, `tests/chezmoiignore_test.sh`, and this plan file. No `Brewfile.macos-desktop-apps.tmpl`, README, status output, or tracked cmux settings file reference should remain.

- [ ] **Step 2: Verify no cleanup or uninstall flow was introduced**

Run:

```bash
rg -n "cmux.*(rm|remove|delete|cleanup|uninstall)|brew uninstall.*cmux|rm -rf.*cmux|dot_config/cmux" .chezmoiscripts dot_local install.sh tests
```

Expected: no matches except test assertions mentioning `dot_config/cmux` exclusion if present. There must be no production cleanup command for cmux.

- [ ] **Step 3: Run the full regression suite**

Run:

```bash
for test in tests/*_test.sh; do sh "$test"; done
for test in tests/*_test.zsh; do zsh "$test"; done
```

Expected: all tests PASS.

- [ ] **Step 4: Review git diff**

Run:

```bash
git diff --stat
git diff -- . ':!docs/superpowers/plans/2026-05-31-remove-cmux-from-macos-desktop-app-stack.md'
```

Expected: diff only removes cmux from declared app/settings management and updates docs/tests/status copy; it does not add any cmux deletion, uninstall, or migration command.

- [ ] **Step 5: Commit the implementation plan if still uncommitted**

Run:

```bash
git add docs/superpowers/plans/2026-05-31-remove-cmux-from-macos-desktop-app-stack.md
git commit -m "Plan cmux desktop stack removal"
```

Skip this commit if the plan has already been committed as part of an earlier commit.

## Self-Review

- Spec coverage: This plan removes cmux from rendered terminal-apps casks, stops managing cmux settings, avoids cleanup/uninstall flows, updates status/setup-facing documentation and README copy, adds managed-path coverage for cmux exclusion, preserves other App Group assertions, and keeps the broad-upgrade guard in the full command regression suite.
- Placeholder scan: No `TBD`, `TODO`, deferred edge handling, or unnamed tests remain.
- Type consistency: The plan consistently uses the existing `enableMacosAppGroupTerminalApps` key, `terminal-apps` group label, `.config/cmux` target path, `dot_config/cmux/private_settings.json` source path, and Ghostty-only wording.
