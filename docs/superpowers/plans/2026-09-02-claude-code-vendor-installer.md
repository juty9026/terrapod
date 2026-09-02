# Claude Code Vendor Installer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Install Claude Code through its vendor-published install script instead of the `claude-code` Homebrew cask, because the cask lags vendor releases.

**Architecture:** The **Optional AI Tool Stack** gains a second package source on the **macOS Terminal Profile**. `run_before_60-install-ai-cli-tools.sh.tmpl` keeps its Homebrew bundle step and gains an independent Claude Code step that runs the vendor script only when `$HOME/.local/bin/claude` is absent, so the no-upgrade contract holds and Claude Code's own updater owns freshness. `executable-selection` gains a fourth provider, `claude-installer`, so status and doctor keep reporting every declaration through one path. The stack keeps one warning category, one status line, and its macOS-only scope.

**Tech Stack:** POSIX `sh`, chezmoi templates (Go text/template), shell test scripts under `tests/` run as `sh tests/<name>.sh`.

**Spec:** `docs/superpowers/specs/2026-09-02-claude-code-vendor-installer-design.md`

## Global Constraints

- Every shell file in this repository is POSIX `sh`. No bashisms, no `local`. The one place `bash` appears is as the *interpreter Terrapod invokes* for the downloaded vendor script — never as the interpreter of a Terrapod file.
- Every chezmoi script under `.chezmoiscripts/` runs `set -eu`.
- The vendor installer URL is exactly `https://claude.ai/install.sh`.
- The canonical Claude Code executable is exactly `$HOME/.local/bin/claude`.
- The new executable-selection provider token is exactly `claude-installer`. The package token stays `claude-code`. The provider label is exactly `the Claude Code installer`.
- `mark_install_warning` must never return non-zero; callers run under `set -e`.
- Install warning marker values stay single-line.
- The warning category stays `optional-ai-cli-tools`. No task adds or removes a category.
- The stack stays scoped to the **macOS Terminal Profile**. No task makes it applicable on the **VPS Shell Profile**.
- Terrapod never removes an alternate installation. No task adds uninstall guidance or provenance-specific advice.
- Run a test file with `sh tests/<name>.sh`. It prints `ok - …` per assertion and exits non-zero on the first `not ok`.
- The full relevant suite for this plan is: `tests/chezmoiignore_test.sh` (~11s), `tests/executable_selection_test.sh`, `tests/homebrew_manifests_test.sh`, `tests/terrapod_command_test.sh`, `tests/readme_korean_test.sh`, `tests/readme_optional_stack_profiles_test.sh`.

---

## Task Ordering Rationale

Tasks are ordered so every intermediate commit leaves a coherent machine state.

Task 1 adds the vendor install step while the cask is still declared: a machine gets Claude Code from both sources, which is harmless. Task 2 points executable selection at the vendor path, which Task 1 already populates. Only then does Task 3 stop declaring the cask. Reversing this order would leave a commit where doctor looks for a cask nothing installs.

---

## File Structure

**Created:**
- `docs/adr/0017-install-claude-code-through-its-vendor-installer.md` — records the package-source decision for Claude Code alone.

**Modified:**
- `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl` — gains the Claude Code install step and per-source failure naming.
- `dot_local/lib/terrapod/executable_executable-selection` — gains the `claude-installer` provider.
- `Brewfile.ai-cli-tools.tmpl` — drops `cask "claude-code"`.
- `CONTEXT.md` — glossary entry plus four changed and three new rules.
- `README.md`, `README.ko.md` — upgrade command, one sentence about self-updating, `enableAiCliTools` row.

**Test files modified:**
- `tests/chezmoiignore_test.sh` — vendor installer stubs and coverage; cask assertions removed in Task 3.
- `tests/executable_selection_test.sh` — `claude-installer` provider coverage.
- `tests/homebrew_manifests_test.sh` — expected cask list.
- `tests/terrapod_command_test.sh` — stubbed executable-selection strings.
- `tests/readme_korean_test.sh`, `tests/readme_optional_stack_profiles_test.sh` — expected README strings.

---

### Task 1: Install Claude Code through the vendor installer

**Files:**
- Modify: `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl`
- Test: `tests/chezmoiignore_test.sh:1925-2148`

**Interfaces:**
- Consumes: `mark_install_warning`, `install_warning_recorded`, `clear_install_warning` from `dot_local/lib/terrapod/install-warning-script.sh`; `terrapod_standard_homebrew_brew_path` from `homebrew-prefix.sh`. All already inlined by this template.
- Produces: a machine-side guarantee that `$HOME/.local/bin/claude` exists after a successful apply on the **macOS Terminal Profile**. Task 2 depends on that path.

The vendor script is fetched at apply time, so tests must never reach the network. The stubs added in Step 1 sit in the stub directory the harness already puts first on `PATH`, which insulates every existing case in this file as well as the new ones.

- [ ] **Step 1: Add vendor installer stubs to the test harness**

In `tests/chezmoiignore_test.sh`, add this helper immediately after the closing `}` of `write_ai_brew_stub` (currently ends near line 2013):

```sh
write_claude_installer_stubs() {
  stub_dir="$1"
  write_stub "$stub_dir/curl" \
    'log="${CLAUDE_INSTALLER_LOG:-/dev/null}"' \
    'printf "%s\n" "curl args:$*" >>"$log"' \
    'output=' \
    'while [ "$#" -gt 0 ]; do' \
    '  if [ "$1" = "-o" ]; then output="$2"; shift 2; else shift; fi' \
    'done' \
    '[ -n "$output" ] || exit 2' \
    'printf "%s\n" "#!/bin/bash" "exit 0" >"$output"'
  write_stub "$stub_dir/bash" \
    'log="${CLAUDE_INSTALLER_LOG:-/dev/null}"' \
    'printf "%s\n" "bash args:$*" >>"$log"' \
    '[ "${CLAUDE_INSTALLER_FAIL:-0}" = "0" ] || exit 3' \
    'mkdir -p "$HOME/.local/bin"' \
    'printf "%s\n" "#!/bin/sh" "exit 0" >"$HOME/.local/bin/claude"' \
    'chmod +x "$HOME/.local/bin/claude"'
}
```

Then, immediately after the existing two `write_ai_brew_stub` calls and the `uname` stub (currently lines 2016-2019, ending with `write_stub "$macos_ai_brew_bin/uname" ...`), add:

```sh
write_claude_installer_stubs "$macos_ai_brew_bin"
```

Only `$macos_ai_brew_bin` needs stubs: every macOS case runs with `PATH="$macos_ai_brew_bin:/usr/bin:/bin"`, and the Ubuntu rendering has no Claude Code step.

- [ ] **Step 2: Write the failing tests**

In `tests/chezmoiignore_test.sh`, replace the existing vendor-URL loop (currently lines 2065-2072) with this, which keeps the two casks' URLs banned and asserts the Claude Code URL renders on macOS only:

```sh
for vendor_url in \
  "https://antigravity.google/cli/install.sh" \
  "https://chatgpt.com/codex/install.sh"
do
  assert_not_contains_text "$ai_cli_tools_installer" "$vendor_url" "Optional AI Tool Stack no longer renders vendor installer URL: $vendor_url"
  assert_not_contains_text "$macos_ai_cli_tools_installer" "$vendor_url" "macOS Optional AI Tool Stack no longer renders vendor installer URL: $vendor_url"
done

assert_contains_text "$macos_ai_cli_tools_installer" "https://claude.ai/install.sh" \
  "macOS Optional AI Tool Stack renders the Claude Code installer URL"
assert_not_contains_text "$ai_cli_tools_installer" "https://claude.ai/install.sh" \
  "Ubuntu Optional AI Tool Stack renders no Claude Code installer URL"
assert_contains_text "$macos_ai_cli_tools_installer" 'bash "$claude_code_installer" </dev/null' \
  "Claude Code installer runs under bash with stdin detached"
```

Then append the behavioral cases at the end of the AI section, immediately after the `pass "successful Optional AI Tool Stack retry clears warning marker"` line (currently line 2148):

```sh
claude_fresh_home="$tmp_dir/claude-fresh-home"
claude_fresh_state="$tmp_dir/claude-fresh-state"
claude_fresh_brew_log="$tmp_dir/claude-fresh-brew.log"
claude_fresh_installer_log="$tmp_dir/claude-fresh-installer.log"
mkdir -p "$claude_fresh_home"
HOME="$claude_fresh_home" XDG_STATE_HOME="$claude_fresh_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_fresh_brew_log" AI_BREW_FAIL=0 \
  CLAUDE_INSTALLER_LOG="$claude_fresh_installer_log" \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script"
assert_contains_text "$(cat "$claude_fresh_installer_log")" "curl args:" \
  "Optional AI Tool Stack downloads the Claude Code installer when Claude Code is absent"
assert_contains_text "$(cat "$claude_fresh_installer_log")" "bash args:" \
  "Optional AI Tool Stack runs the Claude Code installer when Claude Code is absent"
if [ ! -x "$claude_fresh_home/.local/bin/claude" ]; then
  fail "Optional AI Tool Stack installs the canonical Claude Code executable"
fi
pass "Optional AI Tool Stack installs the canonical Claude Code executable"
if [ -e "$claude_fresh_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "a successful Optional AI Tool Stack apply records no warning"
fi
pass "a successful Optional AI Tool Stack apply records no warning"

claude_present_home="$tmp_dir/claude-present-home"
claude_present_state="$tmp_dir/claude-present-state"
claude_present_brew_log="$tmp_dir/claude-present-brew.log"
claude_present_installer_log="$tmp_dir/claude-present-installer.log"
mkdir -p "$claude_present_home/.local/bin"
write_stub "$claude_present_home/.local/bin/claude" 'exit 0'
HOME="$claude_present_home" XDG_STATE_HOME="$claude_present_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_present_brew_log" AI_BREW_FAIL=0 \
  CLAUDE_INSTALLER_LOG="$claude_present_installer_log" \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script"
if [ -e "$claude_present_installer_log" ]; then
  fail "an existing Claude Code install should not rerun the vendor installer"
fi
pass "an existing Claude Code install does not rerun the vendor installer"

claude_failure_home="$tmp_dir/claude-failure-home"
claude_failure_state="$tmp_dir/claude-failure-state"
claude_failure_brew_log="$tmp_dir/claude-failure-brew.log"
mkdir -p "$claude_failure_home"
claude_failure_status=0
HOME="$claude_failure_home" XDG_STATE_HOME="$claude_failure_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_failure_brew_log" AI_BREW_FAIL=0 \
  CLAUDE_INSTALLER_FAIL=1 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  claude_failure_status=$?
if [ "$claude_failure_status" -ne 0 ]; then
  fail "a Claude Code install failure should continue apply after recording a warning"
fi
claude_failure_marker="$claude_failure_state/terrapod/install-warnings/optional-ai-cli-tools"
if [ ! -f "$claude_failure_marker" ]; then
  fail "a Claude Code install failure records the optional-ai-cli-tools marker"
fi
assert_contains_text "$(cat "$claude_failure_marker")" "Claude Code" \
  "a Claude Code install failure names Claude Code in its marker"
pass "a Claude Code install failure records a warning and exits zero"

claude_bundle_failure_home="$tmp_dir/claude-bundle-failure-home"
claude_bundle_failure_state="$tmp_dir/claude-bundle-failure-state"
claude_bundle_failure_brew_log="$tmp_dir/claude-bundle-failure-brew.log"
claude_bundle_failure_installer_log="$tmp_dir/claude-bundle-failure-installer.log"
mkdir -p "$claude_bundle_failure_home"
claude_bundle_failure_status=0
HOME="$claude_bundle_failure_home" XDG_STATE_HOME="$claude_bundle_failure_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_bundle_failure_brew_log" AI_BREW_FAIL=1 \
  CLAUDE_INSTALLER_LOG="$claude_bundle_failure_installer_log" \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  claude_bundle_failure_status=$?
if [ "$claude_bundle_failure_status" -ne 0 ]; then
  fail "a Homebrew bundle failure should continue apply after recording a warning"
fi
assert_contains_text "$(cat "$claude_bundle_failure_installer_log")" "bash args:" \
  "a Homebrew bundle failure does not skip the Claude Code step"
claude_bundle_failure_marker="$claude_bundle_failure_state/terrapod/install-warnings/optional-ai-cli-tools"
assert_contains_text "$(cat "$claude_bundle_failure_marker")" "Homebrew bundle" \
  "a Homebrew bundle failure names the bundle in its marker"
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `sh tests/chezmoiignore_test.sh`
Expected: FAIL at `not ok - macOS Optional AI Tool Stack renders the Claude Code installer URL`, because the template has no Claude Code step yet.

- [ ] **Step 4: Add the Claude Code step to the installer template**

In `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl`, inside the `{{ else }}` branch, replace everything from `ai_cli_brewfile=` down to the final `exit 0` with:

```sh
ai_cli_brewfile=
claude_code_installer=
failed_ai_cli_tools=

CLAUDE_CODE_CANONICAL_EXECUTABLE="$HOME/.local/bin/claude"
CLAUDE_CODE_INSTALLER_URL=https://claude.ai/install.sh

cleanup_ai_cli_install() {
  [ -z "$ai_cli_brewfile" ] || rm -f "$ai_cli_brewfile"
  [ -z "$claude_code_installer" ] || rm -f "$claude_code_installer"
}

trap cleanup_ai_cli_install EXIT HUP INT TERM

append_failed_ai_cli_tool() {
  if [ -z "$failed_ai_cli_tools" ]; then
    failed_ai_cli_tools="$1"
  else
    failed_ai_cli_tools="$failed_ai_cli_tools, $1"
  fi
}

finish_ai_cli_install() {
  if [ -n "$failed_ai_cli_tools" ]; then
    mark_install_warning \
      "$AI_CLI_WARNING_CATEGORY" \
      "Optional AI CLI tool install needs attention" \
      "Failed: $failed_ai_cli_tools. Review the installer output and network access, then rerun tpod apply."

    if ! install_warning_recorded; then
      echo "Failed to record Optional AI CLI tool install warning." >&2
      return 1
    fi

    return 0
  fi

  clear_install_warning "$AI_CLI_WARNING_CATEGORY"
}

standard_brew_path() {
  terrapod_standard_homebrew_brew_path "{{ .chezmoi.os }}"
}

setup_brew_environment() {
  brew_bin="$(standard_brew_path || true)"
  if [ -z "$brew_bin" ] || [ ! -x "$brew_bin" ]; then
    echo "Mandatory standard-prefix Homebrew is unavailable for the Optional AI Tool Stack." >&2
    return 1
  fi
  brew_shellenv="$("$brew_bin" shellenv)" || return 1
  eval "$brew_shellenv"
}

install_ai_cli_bundle() {
  setup_brew_environment || return 1
  ai_cli_brewfile="$(mktemp "${TMPDIR:-/tmp}/terrapod-ai-cli-brewfile.XXXXXX")" || return 1
  cat >"$ai_cli_brewfile" <<'BREWFILE'
{{ includeTemplate "Brewfile.ai-cli-tools.tmpl" . }}
BREWFILE

  HOMEBREW_NO_AUTO_UPDATE=1 "$brew_bin" bundle --no-upgrade --file="$ai_cli_brewfile"
}

install_claude_code() {
  # The vendor installer always downloads the latest build, so running it on an
  # existing install would upgrade Claude Code on every apply. Terrapod restores
  # a missing install only; Claude Code's own updater owns version freshness.
  [ ! -x "$CLAUDE_CODE_CANONICAL_EXECUTABLE" ] || return 0

  echo "Installing Claude Code..."
  claude_code_installer="$(mktemp "${TMPDIR:-/tmp}/terrapod-claude-code-installer.XXXXXX")" || return 1
  curl -fsSL "$CLAUDE_CODE_INSTALLER_URL" -o "$claude_code_installer" || return 1

  # bash, not sh: the vendor script declares #!/bin/bash and uses [[ =~ ]],
  # BASH_REMATCH, and $'...' quoting.
  # </dev/null: the vendor script ends by running `claude install`, which has no
  # unattended flag and can wait on a terminal UI. First-run apply runs in an
  # interactive terminal, so an install that prompts would stall it.
  bash "$claude_code_installer" </dev/null || return 1

  [ -x "$CLAUDE_CODE_CANONICAL_EXECUTABLE" ]
}

# Optional AI Tool Stack Brewfile checksum: {{ includeTemplate "Brewfile.ai-cli-tools.tmpl" . | sha256sum }}
install_ai_cli_bundle || append_failed_ai_cli_tool "Homebrew bundle"
install_claude_code || append_failed_ai_cli_tool "Claude Code"

if ! finish_ai_cli_install; then
  exit 1
fi
exit 0
```

The two steps are separate `||` statements, so neither short-circuits the other under `set -e`. `finish_ai_cli_install` now takes no argument and decides from `$failed_ai_cli_tools`, which is what lets one marker name both sources.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `sh tests/chezmoiignore_test.sh`
Expected: PASS, including `ok - a Claude Code install failure names Claude Code in its marker` and every pre-existing AI assertion.

- [ ] **Step 6: Commit**

```bash
git add .chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl tests/chezmoiignore_test.sh
git commit -m "Install Claude Code through its vendor installer"
```

---

### Task 2: Report Claude Code through a vendor installer provider

**Files:**
- Modify: `dot_local/lib/terrapod/executable_executable-selection:74-80` (`optional_records`), `:117-137` (`provider_has_package`), `:139-145` (`provider_label`), `:147-161` (`expected_executable`)
- Test: `tests/executable_selection_test.sh`, `tests/terrapod_command_test.sh`

**Interfaces:**
- Consumes: `$HOME/.local/bin/claude`, the canonical executable Task 1 installs.
- Produces: the record `claude-installer|claude-code|claude` and the failure text `claude-code is not installed through the Claude Code installer`. Task 4's CONTEXT.md rules and Task 5's README row describe this provider.

- [ ] **Step 1: Write the failing tests**

In `tests/executable_selection_test.sh`, add this assertion immediately after the existing `assert_contains "$ai_enabled_output" "failure - antigravity-cli is not installed through Homebrew Cask"` block (currently ends line 227):

```sh
assert_contains "$ai_enabled_output" "failure - claude-code is not installed through the Claude Code installer" \
  "enabled Optional AI Tool Stack checks Claude Code against its vendor installer"
```

Then append a focused block for the canonical and advisory outcomes at the end of that same AI section, immediately before the `status_output="$(run_selection status)"` line (currently line 229):

```sh
claude_home="$tmp_dir/claude-home"
claude_path_dir="$tmp_dir/claude-path"
mkdir -p "$claude_path_dir"
printf '%s\n' claude-code >"$inventory/claude-installer"
write_executable "$claude_home/.local/bin/claude"
ln -s "$claude_home/.local/bin/claude" "$claude_path_dir/claude"

run_claude_selection() {
  HOME="$claude_home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    PATH="$claude_path_dir:/usr/bin:/bin" \
    "$selection" doctor macos-terminal true false 2>&1 || true
}

claude_canonical_output="$(run_claude_selection)"
assert_not_contains "$claude_canonical_output" "claude-code" \
  "a canonical Claude Code install raises no executable selection concern"

claude_shadow_dir="$tmp_dir/claude-shadow"
mkdir -p "$claude_shadow_dir"
write_executable "$claude_shadow_dir/claude"
rm -f "$claude_path_dir/claude"
ln -s "$claude_shadow_dir/claude" "$claude_path_dir/claude"
claude_shadow_output="$(run_claude_selection)"
assert_contains "$claude_shadow_output" "advisory - claude-code resolves to $claude_shadow_dir/claude" \
  "a shadowed Claude Code install is advisory"
assert_contains "$claude_shadow_output" "canonical: $claude_home/.local/bin/claude" \
  "the Claude Code advisory names the vendor installer path"

rm -f "$inventory/claude-installer"
claude_missing_output="$(run_claude_selection)"
assert_contains "$claude_missing_output" "failure - claude-code is not installed through the Claude Code installer" \
  "an absent Claude Code install names the vendor installer"
```

The `assert_not_contains ... "claude-code"` check works because the other declarations in this run raise their own concerns naming their own packages; only a Claude Code concern would print the token `claude-code`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `sh tests/executable_selection_test.sh`
Expected: FAIL at `not ok - enabled Optional AI Tool Stack checks Claude Code against its vendor installer (missing: failure - claude-code is not installed through the Claude Code installer)`, because the record still says `homebrew-cask`.

- [ ] **Step 3: Add the provider**

In `dot_local/lib/terrapod/executable_executable-selection`, change the Claude Code line in `optional_records`:

```sh
optional_records() {
  if [ "$ai_enabled" = true ]; then
    printf '%s\n' \
      "homebrew-cask|antigravity-cli|agy" \
      "claude-installer|claude-code|claude" \
      "homebrew-cask|codex|codex"
  fi

  if [ "$profile" = macos-terminal ] && [ "$launcher_enabled" = true ]; then
    printf '%s\n' "homebrew-cask|1password-cli|op"
  fi
}
```

Add a branch to `provider_has_package`, between the `homebrew-cask)` and `mise)` branches:

```sh
    claude-installer)
      [ -x "$HOME/.local/bin/claude" ]
      ;;
```

Add a branch to `provider_label`:

```sh
    claude-installer) printf '%s\n' "the Claude Code installer" ;;
```

Add a branch to `expected_executable`:

```sh
    claude-installer)
      printf '%s/.local/bin/%s\n' "$HOME" "$command"
      ;;
```

No new environment seam: this path derives from `$HOME` alone, and the tests already override `HOME`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `sh tests/executable_selection_test.sh`
Expected: PASS, including `ok - the Claude Code advisory names the vendor installer path`.

- [ ] **Step 5: Update the stubbed strings in the command tests**

`tests/terrapod_command_test.sh` feeds executable-selection output to `tpod` as opaque strings, so these do not fail — but they would leave the suite describing Claude Code as a Homebrew cask. Make three edits.

At line 2360-2367, change the canonical path in the stub and the assertion label:

```sh
    TERRAPOD_EXECUTABLE_SELECTION_OUTPUT="  advisory - claude-code resolves to $status_shadow_legacy/claude
             canonical: $tmp_dir/status-shadow-canonical/.local/bin/claude
             Adjust PATH or remove the other installation manually, then rerun 'tpod doctor'." \
```

```sh
assert_contains "$status_shadow_output" "advisory - claude-code resolves to $status_shadow_legacy/claude" "Terrapod status reports a legacy Claude shadowing the Claude Code installer"
```

At line 2480-2490, make the same two changes:

```sh
    TERRAPOD_EXECUTABLE_SELECTION_OUTPUT="  advisory - claude-code resolves to $doctor_ai_shadow_legacy/claude
             canonical: $tmp_dir/doctor-ai-shadow-canonical/.local/bin/claude
             Adjust PATH or remove the other installation manually, then rerun 'tpod doctor'." \
```

```sh
assert_contains "$doctor_shadow_output" "advisory - claude-code resolves to $doctor_ai_shadow_legacy/claude" "Terrapod doctor reports a legacy Claude shadowing the Claude Code installer"
```

At line 2602, change the stubbed failure line:

```sh
if TERRAPOD_EXECUTABLE_SELECTION_OUTPUT="  failure - antigravity-cli is not installed through Homebrew Cask
  failure - claude-code is not installed through the Claude Code installer
  failure - codex is not installed through Homebrew Cask" \
```

- [ ] **Step 6: Run the command tests to verify they pass**

Run: `sh tests/terrapod_command_test.sh`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add dot_local/lib/terrapod/executable_executable-selection tests/executable_selection_test.sh tests/terrapod_command_test.sh
git commit -m "Check Claude Code against its vendor installer"
```

---

### Task 3: Stop declaring the Claude Code cask

**Files:**
- Modify: `Brewfile.ai-cli-tools.tmpl`
- Test: `tests/homebrew_manifests_test.sh:53-60`, `tests/chezmoiignore_test.sh:1939`, `:1945-1949`, `:2008`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: a two-cask manifest. Nothing later depends on it.

- [ ] **Step 1: Write the failing test**

In `tests/homebrew_manifests_test.sh`, remove `cask "claude-code"` from the expected list (currently lines 56-59):

```sh
printf '%s\n' \
  'cask "antigravity-cli"' \
  'cask "codex"' >"$expected_ai_casks"
```

In the same file, change the two messages that wrap this comparison from
`exactly the three macOS casks` to `exactly the two macOS casks` — the `fail`
call inside the `if ! cmp -s` block and the `pass` call after it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `sh tests/homebrew_manifests_test.sh`
Expected: FAIL at `not ok - Optional AI Tool Stack declares exactly the three macOS casks`, with a diff showing the extra `cask "claude-code"` line.

- [ ] **Step 3: Remove the cask from the manifest**

`Brewfile.ai-cli-tools.tmpl` becomes:

```
{{- if and (eq .chezmoi.os "darwin") (or (default false (get . "enableAiCliTools")) (default false (get . "enableDevelopmentWorkspace"))) -}}
cask "antigravity-cli"
cask "codex"
{{- end -}}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `sh tests/homebrew_manifests_test.sh`
Expected: PASS, printing `ok - Optional AI Tool Stack declares exactly the two macOS casks`.

- [ ] **Step 5: Update the remaining cask assertions**

In `tests/chezmoiignore_test.sh`, line 1939 names a cask that no longer exists anywhere. Point it at a cask that still does:

```sh
assert_not_contains_text "$ai_cli_tools_installer" 'cask "codex"' "Ubuntu AI installer renders no macOS-only casks"
```

In the `for rendered_brewfile` loop (currently lines 1945-1949), delete the Claude Code line so the loop reads:

```sh
for rendered_brewfile in "$macos_ai_cli_tools_brewfile" "$macos_development_workspace_ai_brewfile"; do
  assert_contains_text "$rendered_brewfile" 'cask "antigravity-cli"' "Optional AI Tool Stack declares Antigravity CLI cask"
  assert_contains_text "$rendered_brewfile" 'cask "codex"' "Optional AI Tool Stack declares Codex CLI cask"
  assert_not_contains_text "$rendered_brewfile" 'cask "claude-code"' "Optional AI Tool Stack no longer declares a Claude Code cask"
done
```

In `write_ai_brew_stub` (currently line 2008), delete the line `'  grep -Fx "cask \"claude-code\"" "$bundle_file" >/dev/null || exit 66' \` so the stub checks only the two remaining casks.

- [ ] **Step 6: Run both suites to verify they pass**

Run: `sh tests/homebrew_manifests_test.sh && sh tests/chezmoiignore_test.sh`
Expected: PASS for both.

- [ ] **Step 7: Commit**

```bash
git add Brewfile.ai-cli-tools.tmpl tests/homebrew_manifests_test.sh tests/chezmoiignore_test.sh
git commit -m "Drop the Claude Code cask from the AI CLI manifest"
```

---

### Task 4: Record the decision in the domain context and an ADR

**Files:**
- Create: `docs/adr/0017-install-claude-code-through-its-vendor-installer.md`
- Modify: `CONTEXT.md:31-33` (glossary neighborhood), `:182`, `:184`, `:185`, `:237`

**Interfaces:**
- Consumes: the provider token and canonical path fixed by Task 2.
- Produces: the **Claude Code Installer** domain term that Task 5's README wording follows.

There is no test for documentation prose here; the check is that the text matches the code the previous tasks landed.

- [ ] **Step 1: Add the glossary entry**

In `CONTEXT.md`, insert this entry immediately after the **Optional AI Tool Stack** entry (currently ends line 33), keeping the existing bold-term and `_Avoid_` format:

```markdown
**Claude Code Installer**:
The vendor-published install script at `https://claude.ai/install.sh` that owns Claude Code on the **macOS Terminal Profile**.
_Avoid_: claude-code cask, Terrapod installer
```

The `Terrapod installer` avoidance matters: this repository's own bootstrap script is also named `install.sh`.

- [ ] **Step 2: Change the four existing rules**

Line 182 becomes:

```markdown
- The **Optional AI Tool Stack** installs Homebrew casks `antigravity-cli` and `codex` and installs Claude Code through the **Claude Code Installer**, on the **macOS Terminal Profile** only; Homebrew publishes those two as casks, which Linux Homebrew does not install.
```

Line 184 becomes:

```markdown
- `Brewfile.ai-cli-tools.tmpl` is the canonical declaration for the **Optional AI Tool Stack**'s Homebrew packages and renders empty outside the **macOS Terminal Profile**.
```

Line 185 becomes:

```markdown
- The **Modern CLI Provider** installs the 20 mandatory formulae declared in `Brewfile` on both supported profiles and owns the two Optional AI Tool Stack casks on macOS when that stack is enabled.
```

Line 237 becomes:

```markdown
- The optional AI CLI tools warning marker keeps one category for the **Optional AI Tool Stack** across both of its package sources but includes the failed tool names in its summary or guidance fields.
```

- [ ] **Step 3: Add the three new rules**

Insert these immediately after the amended line 185:

```markdown
- The **Claude Code Installer** is the canonical provider for Claude Code; its canonical executable is `$HOME/.local/bin/claude`.
- `tpod apply` runs the **Claude Code Installer** only when that canonical executable is absent. Claude Code's own updater owns version freshness, occupying the same place as the no-upgrade contract for Homebrew packages.
- Claude Code stays scoped to the **macOS Terminal Profile** even though the **Claude Code Installer** supports Linux; stack applicability is a profile decision, not a consequence of the package source.
```

- [ ] **Step 4: Write ADR 0017**

Create `docs/adr/0017-install-claude-code-through-its-vendor-installer.md` following the existing ADR format — a title, a decision statement, a supersession paragraph, `## Considered Options`, and `## Consequences`:

```markdown
# Install Claude Code through its vendor installer

The **Optional AI Tool Stack** installs Claude Code through the **Claude Code
Installer**, the vendor-published script at `https://claude.ai/install.sh`,
instead of the `claude-code` Homebrew cask. `antigravity-cli` and `codex` remain
Homebrew casks. The canonical Claude Code executable is `$HOME/.local/bin/claude`.

The Homebrew cask lags vendor releases, and Claude Code releases often enough for
that lag to show in daily use.

This decision supersedes ADR 0008's package-source choice for Claude Code alone;
ADR 0008 stays in force for the other two casks, and Homebrew remains the
**Modern CLI Provider**. It also corrects ADR 0015's stated reason as it applies
to Claude Code: the stack stays scoped to the **macOS Terminal Profile**, but for
Claude Code that scope is now a profile decision rather than a consequence of
Homebrew not installing casks on Linux. ADR 0010's no-upgrade contract and ADR
0012's advisory-only migration policy remain in force.

## Considered Options

- Keep the cask and accept the release lag: rejected because the lag is the
  problem being solved.
- Move the whole stack back to vendor installers: rejected because ADR 0008's
  single declared source still holds for `antigravity-cli` and `codex`, whose
  release cadence does not create the same pressure.
- Run the vendor installer on every apply: rejected because the script always
  downloads the latest build, which would make apply an upgrade and break ADR
  0010's `brew bundle --no-upgrade` contract.
- Extend the stack to the **VPS Shell Profile** now that its installer supports
  Linux: rejected because it confuses a package-source change with a profile
  decision.

## Consequences

- `Brewfile.ai-cli-tools.tmpl` declares two casks; the **Optional AI Tool Stack**
  draws from two package sources on the **macOS Terminal Profile**.
- `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl` runs the vendor
  installer only when `$HOME/.local/bin/claude` is absent. Claude Code's own
  updater owns version freshness.
- The Homebrew bundle step and the Claude Code step run independently; either
  failure records the single `optional-ai-cli-tools` warning, naming the failed
  source, and apply continues.
- `executable-selection` gains the `claude-installer` provider. A missing install
  reports `claude-code is not installed through the Claude Code installer`.
- A machine that still has the `claude-code` cask keeps resolving `claude` to the
  cask copy, because `dot_zshenv.tmpl` evaluates Homebrew's `shellenv` after
  prepending `$HOME/.local/bin`. Terrapod reports the provenance-neutral advisory
  and removes nothing, per ADR 0012.
- `claude install` may edit shell startup files. `run_before_60` runs before
  chezmoi writes the managed `.zshenv` and `.zshrc`, so those edits are
  overwritten; nothing is lost, because the managed PATH already includes
  `$HOME/.local/bin`.
- Intentional cask upgrades use `brew upgrade --cask codex antigravity-cli`.
```

- [ ] **Step 5: Verify no test regressed**

Run: `sh tests/terrapod_command_test.sh && sh tests/chezmoiignore_test.sh`
Expected: PASS. Neither suite reads `CONTEXT.md` or `docs/adr/`, so this is a guard against an accidental edit elsewhere.

- [ ] **Step 6: Commit**

```bash
git add CONTEXT.md docs/adr/0017-install-claude-code-through-its-vendor-installer.md
git commit -m "Record the Claude Code vendor installer decision"
```

---

### Task 5: Update both READMEs

**Files:**
- Modify: `README.md:255-262`, `:316`; `README.ko.md:242-249`, `:294`
- Test: `tests/readme_optional_stack_profiles_test.sh:99-102`, `:291-292`; `tests/readme_korean_test.sh:191-192`

**Interfaces:**
- Consumes: the **Claude Code Installer** term from Task 4.
- Produces: nothing later depends on it.

- [ ] **Step 1: Write the failing tests**

In `tests/readme_optional_stack_profiles_test.sh`, replace the two Homebrew-cask expectations. At lines 99-102:

```sh
assert_key_row_contains '`enableAiCliTools`' 'Antigravity CLI, Claude Code, and Codex' \
  "README documents the new Optional AI Tool Stack membership"
assert_key_row_contains '`enableAiCliTools`' 'Homebrew casks `antigravity-cli` and `codex`' \
  "README documents Homebrew-owned Optional AI Tool Stack members"
assert_key_row_contains '`enableAiCliTools`' 'Claude Code through its official installer' \
  "README documents the vendor-installed Optional AI Tool Stack member"
```

At line 291-292:

```sh
assert_contains 'brew upgrade --cask codex antigravity-cli' \
  "README documents targeted AI CLI upgrades"
assert_contains 'Claude Code updates itself and is not part of that command.' \
  "README documents who owns Claude Code freshness"
```

In `tests/readme_korean_test.sh` at lines 191-192:

```sh
assert_contains "$korean_readme" 'brew upgrade --cask codex antigravity-cli' \
  "README.ko.md documents targeted AI CLI upgrades"
assert_contains "$korean_readme" 'Claude Code는 자체적으로 업데이트하며 이 명령의 대상이 아닙니다.' \
  "README.ko.md documents who owns Claude Code freshness"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `sh tests/readme_optional_stack_profiles_test.sh`
Expected: FAIL at `not ok - README documents Homebrew-owned Optional AI Tool Stack members`.

- [ ] **Step 3: Update `README.md`**

Both new sentences must stay on one physical line each: the README tests match
literal substrings, and a wrapped sentence would never match.

Replace the upgrade block (currently lines 255-262) with:

````markdown
Intentional CLI upgrades are explicit Homebrew operations. Upgrade all
Homebrew-managed CLIs with `brew update` and `brew upgrade`, or target only the
AI CLI casks when that is the intended scope.
Claude Code updates itself and is not part of that command.

```sh
brew update
brew upgrade --cask codex antigravity-cli
```
````

Replace the `enableAiCliTools` table row (line 316) with:

```markdown
| `enableAiCliTools` | `false` | Installs Antigravity CLI, Claude Code, and Codex: Antigravity CLI and Codex through Homebrew casks `antigravity-cli` and `codex`, and Claude Code through its official installer. macOS Terminal Profile only; ignored on the VPS Shell Profile. |
```

- [ ] **Step 4: Update `README.ko.md`**

Replace the upgrade block (currently lines 242-249) with:

````markdown
CLI upgrade는 명시적인 Homebrew operation으로만 수행합니다. 모든 Homebrew-managed
CLI를 올리려면 `brew update`와 `brew upgrade`를 사용하고, AI CLI cask만 의도한 경우
대상을 지정합니다.
Claude Code는 자체적으로 업데이트하며 이 명령의 대상이 아닙니다.

```sh
brew update
brew upgrade --cask codex antigravity-cli
```
````

Replace the `enableAiCliTools` table row (line 294) with:

```markdown
| `enableAiCliTools` | `false` | Antigravity CLI, Claude Code, Codex를 설치합니다. Antigravity CLI와 Codex는 Homebrew cask `antigravity-cli`, `codex`로, Claude Code는 공식 installer로 설치합니다. macOS Terminal Profile 전용이며 VPS Shell Profile에서는 무시됩니다. |
```

- [ ] **Step 5: Run the README tests to verify they pass**

Run: `sh tests/readme_optional_stack_profiles_test.sh && sh tests/readme_korean_test.sh`
Expected: PASS for both.

- [ ] **Step 6: Run the full relevant suite**

Run:

```bash
for suite in chezmoiignore executable_selection homebrew_manifests terrapod_command readme_korean readme_optional_stack_profiles; do
  printf '== %s ==\n' "$suite"
  sh "tests/${suite}_test.sh" >/dev/null || { printf 'FAILED: %s\n' "$suite"; break; }
done
```

Expected: every suite prints its header and none prints `FAILED`.

- [ ] **Step 7: Commit**

```bash
git add README.md README.ko.md tests/readme_optional_stack_profiles_test.sh tests/readme_korean_test.sh
git commit -m "Document the Claude Code vendor installer in both READMEs"
```
