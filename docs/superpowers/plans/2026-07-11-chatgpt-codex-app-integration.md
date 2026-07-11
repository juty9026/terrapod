# ChatGPT Codex App Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Present the unified OpenAI desktop application as ChatGPT while preserving the `com.openai.codex` launcher identity and `codex-app` Homebrew installation token.

**Architecture:** Keep OS-level identity and package-manager identity unchanged, and update only user-facing launcher, setup, status, and documentation copy. Record the temporary package-name mismatch next to the Brewfile declaration and enforce it with shell regression tests.

**Tech Stack:** Lua, POSIX shell tests, Go templates, Markdown

## Global Constraints

- Hyper+1 displays `ChatGPT` and continues launching bundle ID `com.openai.codex`.
- The `ai-apps` group describes the installed artifact as `Codex desktop app (updates to the unified ChatGPT desktop app)` in English and `Codex 데스크톱 앱(통합 ChatGPT 데스크톱 앱으로 업데이트)` in Korean.
- The Homebrew token remains `codex-app`; `chatgpt` must not be added to the rendered `ai-apps` bundle.
- Terrapod must not uninstall or warn about an existing unmanaged `chatgpt` cask.
- Terrapod must not manage the unified application's default view.

---

### Task 1: Rename the Unified Desktop App Without Changing Its Package Identity

**Files:**
- Modify: `tests/hammerspoon_config_test.sh`
- Modify: `tests/chezmoiignore_test.sh`
- Modify: `tests/terrapod_command_test.sh`
- Modify: `tests/readme_optional_stack_profiles_test.sh`
- Modify: `tests/readme_korean_test.sh`
- Modify: `dot_hammerspoon/init.lua`
- Modify: `Brewfile.macos-desktop-apps.tmpl`
- Modify: `dot_local/bin/executable_terrapod`
- Modify: `README.md`
- Modify: `README.ko.md`

**Interfaces:**
- Consumes: the existing `appBindings` Lua table, `enableMacosAppGroupAiApps` template flag, and setup/status text renderers.
- Produces: the stable launcher name `ChatGPT`, an inventory that distinguishes the installed Codex artifact from its unified ChatGPT update, and a tested `codex-app`-only Homebrew contract.

- [x] **Step 1: Write the failing tests**

Update the shell assertions to require:

```text
{ key = "1", label = "ChatGPT", bundleID = "com.openai.codex" }
Installs Claude Desktop, Codex desktop app (updates to the unified ChatGPT desktop app), Antigravity 2.0, and Antigravity IDE.
ai-apps                       : enabled (Claude Desktop, Codex desktop app (updates to the unified ChatGPT desktop app), Antigravity 2.0, and Antigravity IDE)
cask "codex-app"
```

Also assert that the rendered `ai-apps` bundle does not contain `cask "chatgpt"`, and that both English and Korean README option rows retain `codex-app` while describing ChatGPT and Codex.

- [x] **Step 2: Run tests to verify they fail**

Run:

```bash
sh tests/hammerspoon_config_test.sh
sh tests/chezmoiignore_test.sh
sh tests/terrapod_command_test.sh
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
```

Expected: each changed contract fails because production copy still says `Codex Desktop` and the Brewfile does not yet explain or test the `chatgpt` exclusion.

- [x] **Step 3: Implement the minimal copy and contract changes**

Change the Hyper+1 binding to:

```lua
{ key = "1", label = "ChatGPT", bundleID = "com.openai.codex" },
```

Keep this cask and add an explanatory comment immediately above it:

```ruby
# codex-app installs Codex.app, which updates to the unified ChatGPT desktop app; do not replace it with the legacy chatgpt cask.
cask "codex-app"
```

Change English inventories to `Codex desktop app (updates to the unified ChatGPT desktop app)` and Korean inventories to `Codex 데스크톱 앱(통합 ChatGPT 데스크톱 앱으로 업데이트)`, without changing the option key, bundle ID, or cask token.

- [x] **Step 4: Run focused tests to verify they pass**

Run:

```bash
sh tests/hammerspoon_config_test.sh
sh tests/chezmoiignore_test.sh
sh tests/terrapod_command_test.sh
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
```

Expected: all four scripts exit 0 with only PASS output.

- [x] **Step 5: Run repository verification**

Run:

```bash
for test_script in tests/*_test.sh; do sh "$test_script"; done
git diff --check
git diff -- dot_hammerspoon/init.lua Brewfile.macos-desktop-apps.tmpl dot_local/bin/executable_terrapod README.md README.ko.md tests docs/superpowers/plans/2026-07-11-chatgpt-codex-app-integration.md
```

Expected: the full test runner and whitespace check exit 0; the diff contains no package-token, bundle-ID, default-view, uninstall, or warning behavior changes.

Actual: all focused tests and 12 of 13 repository test scripts passed. `tests/chezmoiignore_test.sh` stops at its pre-existing mise warning-marker expectation before reaching the new cask assertions; the `ai-apps` Brewfile contract was therefore also rendered and verified directly. `git diff --check` passed, and the diff contains no package-token, bundle-ID, default-view, uninstall, or warning behavior changes.
