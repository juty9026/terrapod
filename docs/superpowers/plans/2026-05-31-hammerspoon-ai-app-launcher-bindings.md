# Hammerspoon AI App Launcher Bindings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update the Hammerspoon app launcher so the ai-apps macOS App Group apps use the agreed shortcut keys and ChatGPT Atlas is no longer launchable.

**Architecture:** Keep the existing `appBindings` table as the single source for direct hyper-key bindings, leader modal bindings, and help text. Add a focused shell test that treats the Lua config as a managed dotfile and verifies the exact launcher rows and removed Atlas identifiers. No new Hammerspoon abstraction is needed.

**Tech Stack:** Hammerspoon Lua config, POSIX shell tests, `grep`/`sed`.

---

## Verified Bundle ID Inputs

- Codex Desktop: installed `/Applications/Codex.app` has `CFBundleIdentifier` `com.openai.codex`; Homebrew cask `codex-app` also quits `com.openai.codex`.
- Claude Desktop: Homebrew cask `claude` quits `com.anthropic.claudefordesktop` and installs `Claude.app`.
- Antigravity 2.0: installed `/Applications/Antigravity.app` has `CFBundleIdentifier` `com.google.antigravity`; Homebrew cask `antigravity` also quits `com.google.antigravity`.
- Antigravity IDE: installed `/Applications/Antigravity IDE.app` has `CFBundleIdentifier` `com.google.antigravity-ide`; Homebrew cask `antigravity-ide` also quits `com.google.antigravity-ide`.

## File Structure

- Create `tests/hammerspoon_config_test.sh`: focused Hammerspoon launcher regression tests using existing shell test style.
- Modify `dot_hammerspoon/init.lua`: update only the `appBindings` rows for AI desktop apps and remove ChatGPT Atlas.

## Tasks

### Task 1: Add Hammerspoon Launcher Regression Test

**Files:**
- Create: `tests/hammerspoon_config_test.sh`
- Read: `dot_hammerspoon/init.lua`

- [ ] **Step 1: Write the failing test**

Create `tests/hammerspoon_config_test.sh` with this exact content:

```sh
#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
hammerspoon_config="$repo_root/dot_hammerspoon/init.lua"

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

assert_contains() {
  needle="$1"
  message="$2"

  if ! grep -F "$needle" "$hammerspoon_config" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  needle="$1"
  message="$2"

  if grep -F "$needle" "$hammerspoon_config" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_contains \
  '{ key = "1", label = "Codex Desktop", bundleID = "com.openai.codex" }' \
  "Hammerspoon launcher binds Codex Desktop to 1"

assert_contains \
  '{ key = "2", label = "Claude Desktop", bundleID = "com.anthropic.claudefordesktop" }' \
  "Hammerspoon launcher binds Claude Desktop to 2"

assert_contains \
  '{ key = "3", label = "Antigravity 2.0", bundleID = "com.google.antigravity" }' \
  "Hammerspoon launcher binds Antigravity 2.0 to 3"

assert_contains \
  '{ key = "i", label = "Antigravity IDE", bundleID = "com.google.antigravity-ide" }' \
  "Hammerspoon launcher binds Antigravity IDE to i"

assert_not_contains \
  'ChatGPT Atlas' \
  "Hammerspoon launcher no longer includes ChatGPT Atlas label"

assert_not_contains \
  'com.openai.atlas' \
  "Hammerspoon launcher no longer includes ChatGPT Atlas bundle ID"

assert_not_contains \
  '{ key = "c", label = "Codex", bundleID = "com.openai.codex" }' \
  "Hammerspoon launcher no longer binds Codex Desktop to c"

assert_not_contains \
  '{ key = "i", label = "Antigravity", bundleID = "com.google.antigravity" }' \
  "Hammerspoon launcher no longer binds Antigravity 2.0 to i"
```

- [ ] **Step 2: Run the focused test to verify it fails**

Run:

```bash
sh tests/hammerspoon_config_test.sh
```

Expected: FAIL on `Hammerspoon launcher binds Codex Desktop to 1`, proving the test catches the missing new launcher row.

### Task 2: Update Hammerspoon App Bindings

**Files:**
- Modify: `dot_hammerspoon/init.lua`
- Test: `tests/hammerspoon_config_test.sh`

- [ ] **Step 1: Replace the AI launcher rows**

In `dot_hammerspoon/init.lua`, replace the current ChatGPT Atlas, Codex, and Antigravity rows in `appBindings`:

```lua
  { key = "a", label = "ChatGPT Atlas", bundleID = "com.openai.atlas" },
  { key = "b", label = "Google Chrome", bundleID = "com.google.Chrome" },
  { key = "c", label = "Codex", bundleID = "com.openai.codex" },
  { key = "i", label = "Antigravity", bundleID = "com.google.antigravity" },
```

with:

```lua
  { key = "b", label = "Google Chrome", bundleID = "com.google.Chrome" },
  { key = "1", label = "Codex Desktop", bundleID = "com.openai.codex" },
  { key = "2", label = "Claude Desktop", bundleID = "com.anthropic.claudefordesktop" },
  { key = "3", label = "Antigravity 2.0", bundleID = "com.google.antigravity" },
  { key = "i", label = "Antigravity IDE", bundleID = "com.google.antigravity-ide" },
```

- [ ] **Step 2: Run the focused test to verify it passes**

Run:

```bash
sh tests/hammerspoon_config_test.sh
```

Expected: PASS for all eight assertions.

- [ ] **Step 3: Run nearby managed-file tests**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: PASS. This guards that `dot_hammerspoon` is still managed only for the macOS profile and remains excluded for Ubuntu.

### Task 3: Full Verification and Publish-Ready Commit

**Files:**
- Verify: `dot_hammerspoon/init.lua`
- Verify: `tests/hammerspoon_config_test.sh`
- Verify: `docs/superpowers/plans/2026-05-31-hammerspoon-ai-app-launcher-bindings.md`

- [ ] **Step 1: Run the full local test suite**

Run:

```bash
for test_file in tests/*.sh; do sh "$test_file"; done
for test_file in tests/*.zsh; do zsh "$test_file"; done
```

Expected: PASS for all tests, including the new `tests/hammerspoon_config_test.sh`.

- [ ] **Step 2: Review the final diff**

Run:

```bash
git diff -- dot_hammerspoon/init.lua tests/hammerspoon_config_test.sh docs/superpowers/plans/2026-05-31-hammerspoon-ai-app-launcher-bindings.md
```

Expected: the diff only adds the plan and Hammerspoon test, removes Atlas from `appBindings`, and adds the four requested AI desktop app launcher rows.

- [ ] **Step 3: Commit the scoped changes**

Run:

```bash
git add dot_hammerspoon/init.lua tests/hammerspoon_config_test.sh docs/superpowers/plans/2026-05-31-hammerspoon-ai-app-launcher-bindings.md
git commit -m "Update Hammerspoon AI launcher bindings"
```

Expected: one commit containing the scoped implementation and plan.

- [ ] **Step 4: Open a ready-for-review PR**

Push the branch and open a non-draft PR against the repository default branch with a body that mentions the bundle ID confirmation sources and the focused/full verification commands.

## Self-Review

- Spec coverage: The plan covers all Issue #85 acceptance criteria: four agreed shortcuts, Atlas removal, bundle ID confirmation, Hammerspoon config tests, and coordination with the ai-apps macOS App Group app names/casks.
- Placeholder scan: No placeholders remain.
- Type consistency: Labels, keys, bundle IDs, file paths, and test command names are consistent across tasks.
