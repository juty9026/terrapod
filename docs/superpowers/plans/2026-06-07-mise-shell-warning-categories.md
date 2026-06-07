# Mise And Shell Warning Categories Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `mise-tools` and `shell-integrations` installer failures marker-backed, non-blocking recovery categories for routine `tpod apply`, while clearing or replacing each marker on later reruns.

**Architecture:** Keep the shared install-warning marker storage in `dot_local/lib/terrapod/install-warnings.sh` unchanged. Update the two installer templates so reliable category failures are accumulated, written to one category marker, and return success after the marker write succeeds; successful reruns clear the category marker. Because successful `run_onchange_` scripts are not rerun by chezmoi when their content is unchanged, add marker-gated always-run retry hooks that no-op without a marker and retry only when a category marker exists. Tests render the templates through chezmoi and run isolated stubbed commands so no real installers or network calls execute.

**Tech Stack:** POSIX `sh`, chezmoi templates, shell test scripts under `tests/`, GitHub Issue #102.

---

## File Structure

- Modify: `.chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl`
  - Owns the `mise-tools` category.
  - Should collect failed steps, write one marker on failure, exit 0 after a successful marker write, and clear the marker when `mise install` and `corepack enable` both succeed.
- Create: `.chezmoiscripts/run_after_21-retry-mise-tools.sh.tmpl`
  - Always runs after apply, no-ops without a `mise-tools` marker, and retries the marker-backed recovery path on later `tpod apply` runs.
- Modify: `.chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl`
  - Owns the `shell-integrations` category.
  - Should collect failed shell integration names, keep attempting independent integrations when practical, write one marker on failure, exit 0 after a successful marker write, and clear the marker on full success.
- Create: `.chezmoiscripts/run_before_31-retry-shell-integrations.sh.tmpl`
  - Always runs during the before phase, no-ops without a `shell-integrations` marker, and retries the marker-backed recovery path on later `tpod apply` runs.
- Modify: `tests/chezmoiignore_test.sh`
  - Covers rendered `mise-tools` behavior, marker-gated retry behavior, and broad render assertions.
- Modify: `tests/shell_integrations_test.sh`
  - Covers rendered `shell-integrations` behavior, marker-gated retry behavior, and continuing after practical per-item failures.
- Create: `docs/superpowers/plans/2026-06-07-mise-shell-warning-categories.md`
  - This implementation plan.

No changes are expected in `dot_local/lib/terrapod/install-warnings.sh`: it already supports the `mise-tools` and `shell-integrations` categories, atomic replacement, `updated_at`, and marker clearing.

---

### Task 1: Render Failing Tests For `mise-tools`

**Files:**
- Modify: `tests/chezmoiignore_test.sh`

- [ ] **Step 1: Add a `mise` installer script fixture**

After the existing `mise_tools_installer` checksum assertion, add a rendered-script fixture:

```sh
mise_tools_installer_script="$tmp_dir/mise-tools-installer.sh"
printf '%s\n' "$mise_tools_installer" >"$mise_tools_installer_script"
sh -n "$mise_tools_installer_script" || fail "mise tool installer script should be valid sh"
pass "mise tool installer script is valid sh"
```

- [ ] **Step 2: Add stubbed `mise` behavior**

Append this setup after the rendered script fixture:

```sh
mise_tools_home="$tmp_dir/mise-tools-home"
mise_tools_state="$tmp_dir/mise-tools-state"
mise_tools_bin="$tmp_dir/mise-tools-bin"
mise_tools_log="$tmp_dir/mise-tools.log"
mkdir -p "$mise_tools_home" "$mise_tools_bin"

write_stub "$mise_tools_bin/mise" \
  'printf "%s\n" "mise args:$*" >>"$MISE_TOOLS_LOG"' \
  'case "$1" in' \
  '  install)' \
  '    exit "${MISE_TOOLS_INSTALL_STATUS:-0}"' \
  '    ;;' \
  '  exec)' \
  '    shift' \
  '    while [ "$#" -gt 0 ]; do' \
  '      case "$1" in' \
  '        --)' \
  '          shift' \
  '          break' \
  '          ;;' \
  '      esac' \
  '      shift' \
  '    done' \
  '    if [ "${1:-}" = sh ]; then' \
  '      if [ "${MISE_TOOLS_COREPACK_PRESENT:-1}" = "1" ]; then exit 0; else exit 1; fi' \
  '    fi' \
  '    if [ "${1:-}" = corepack ]; then' \
  '      exit "${MISE_TOOLS_COREPACK_STATUS:-0}"' \
  '    fi' \
  '    exit 64' \
  '    ;;' \
  'esac' \
  'exit 64'
```

- [ ] **Step 3: Test failure marker, successful exit, and GitHub rate-limit guidance**

Append this failure case:

```sh
MISE_TOOLS_INSTALL_STATUS=23
MISE_TOOLS_COREPACK_PRESENT=1
MISE_TOOLS_COREPACK_STATUS=0
export MISE_TOOLS_INSTALL_STATUS MISE_TOOLS_COREPACK_PRESENT MISE_TOOLS_COREPACK_STATUS

mise_tools_failure_status=0
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script" >"$tmp_dir/mise-tools-failure.out" 2>"$tmp_dir/mise-tools-failure.err" || mise_tools_failure_status=$?

unset MISE_TOOLS_INSTALL_STATUS MISE_TOOLS_COREPACK_PRESENT MISE_TOOLS_COREPACK_STATUS

if [ "$mise_tools_failure_status" -ne 0 ]; then
  fail "mise tool installer exits 0 after recording a mise-tools warning marker"
fi
pass "mise tool installer exits 0 after recording a mise-tools warning marker"

mise_tools_marker="$mise_tools_state/terrapod/install-warnings/mise-tools"
if [ ! -f "$mise_tools_marker" ]; then
  fail "mise tool installer writes a mise-tools marker after mise install failure"
fi
pass "mise tool installer writes a mise-tools marker after mise install failure"

mise_tools_marker_text="$(cat "$mise_tools_marker")"
assert_contains_text "$mise_tools_marker_text" "summary='mise tool install needs attention'" "mise tools marker keeps stable summary"
assert_contains_text "$mise_tools_marker_text" "Failed step(s): mise install" "mise tools marker guidance includes reliable failed step"
assert_contains_text "$mise_tools_marker_text" "GITHUB_TOKEN" "mise tools marker guidance mentions temporary GITHUB_TOKEN for GitHub rate limits"
assert_contains_text "$mise_tools_marker_text" "gh auth login" "mise tools marker guidance mentions GitHub CLI authentication for GitHub rate limits"
```

- [ ] **Step 4: Test successful rerun clears stale marker**

Append this rerun case:

```sh
: >"$mise_tools_log"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script" >"$tmp_dir/mise-tools-success.out" 2>"$tmp_dir/mise-tools-success.err"

if [ -e "$mise_tools_marker" ]; then
  fail "successful mise tool rerun clears mise-tools marker"
fi
pass "successful mise tool rerun clears mise-tools marker"
```

- [ ] **Step 5: Test failed rerun replaces marker content**

Append this replacement case:

```sh
HOME="$mise_tools_home" XDG_STATE_HOME="$mise_tools_state" sh -c \
  '. "$1"; terrapod_install_warning_write mise-tools "stale mise warning" "stale guidance."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

MISE_TOOLS_INSTALL_STATUS=0
MISE_TOOLS_COREPACK_PRESENT=1
MISE_TOOLS_COREPACK_STATUS=44
export MISE_TOOLS_INSTALL_STATUS MISE_TOOLS_COREPACK_PRESENT MISE_TOOLS_COREPACK_STATUS

mise_tools_corepack_status=0
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script" >"$tmp_dir/mise-tools-corepack.out" 2>"$tmp_dir/mise-tools-corepack.err" || mise_tools_corepack_status=$?

unset MISE_TOOLS_INSTALL_STATUS MISE_TOOLS_COREPACK_PRESENT MISE_TOOLS_COREPACK_STATUS

if [ "$mise_tools_corepack_status" -ne 0 ]; then
  fail "mise tool installer exits 0 after replacing a failed corepack marker"
fi
pass "mise tool installer exits 0 after replacing a failed corepack marker"

mise_tools_corepack_marker_text="$(cat "$mise_tools_marker")"
assert_contains_text "$mise_tools_corepack_marker_text" "Failed step(s): corepack enable" "failed mise rerun replaces marker with current failed step"
assert_not_contains_text "$mise_tools_corepack_marker_text" "stale guidance" "failed mise rerun replaces stale marker guidance"
assert_contains_text "$mise_tools_corepack_marker_text" "updated_at='" "failed mise rerun writes updated_at"
```

- [ ] **Step 6: Run the new test and verify it fails before implementation**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: FAIL because the current `mise-tools` script still exits non-zero after marker-backed routine failures.

- [ ] **Step 7: Commit the failing tests**

```bash
git add tests/chezmoiignore_test.sh docs/superpowers/plans/2026-06-07-mise-shell-warning-categories.md
git commit -m "test: cover mise warning category recovery"
```

---

### Task 2: Make `mise-tools` Marker-Backed And Non-Blocking

**Files:**
- Modify: `.chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl`
- Test: `tests/chezmoiignore_test.sh`

- [ ] **Step 1: Replace the first-run-only warning exit helper**

In `.chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl`, replace the existing `exit_after_install_warning` body with:

```sh
exit_after_install_warning() {
  if [ "$INSTALL_WARNING_RECORDED" -eq 1 ]; then
    exit 0
  fi

  exit 1
}
```

- [ ] **Step 2: Add failed-step accumulation helpers**

After `clear_install_warning`, add:

```sh
failed_mise_steps=

append_failed_mise_step() {
  if [ -n "$failed_mise_steps" ]; then
    failed_mise_steps="$failed_mise_steps, $1"
  else
    failed_mise_steps="$1"
  fi
}

finish_mise_tools() {
  if [ -n "$failed_mise_steps" ]; then
    mark_install_warning \
      mise-tools \
      "mise tool install needs attention" \
      "Failed step(s): $failed_mise_steps. Review mise output, fix tool installation issues, then rerun tpod apply. If mise aqua resolution hit GitHub API rate limits, export a temporary GITHUB_TOKEN or run gh auth login before rerunning."
    exit_after_install_warning
  fi

  clear_install_warning mise-tools
}
```

- [ ] **Step 3: Accumulate `mise install` failure instead of exiting immediately**

Replace the current `mise install` block:

```sh
if ! mise install --yes -C "$HOME"; then
  mark_install_warning \
    mise-tools \
    "mise tool install needs attention" \
    "Review mise install output, fix tool installation issues, then rerun tpod apply."
  exit_after_install_warning
fi
```

with:

```sh
if ! mise install --yes -C "$HOME"; then
  append_failed_mise_step "mise install"
fi
```

- [ ] **Step 4: Accumulate `corepack enable` failure**

Replace the current `corepack enable` block:

```sh
if mise exec --yes -C "$HOME" -- sh -c 'command -v corepack' >/dev/null 2>&1; then
  if ! mise exec --yes -C "$HOME" -- corepack enable; then
    mark_install_warning \
      mise-tools \
      "mise tool install needs attention" \
      "Review corepack enable output, then rerun tpod apply."
    exit_after_install_warning
  fi
fi

clear_install_warning mise-tools
```

with:

```sh
if mise exec --yes -C "$HOME" -- sh -c 'command -v corepack' >/dev/null 2>&1; then
  if ! mise exec --yes -C "$HOME" -- corepack enable; then
    append_failed_mise_step "corepack enable"
  fi
fi

finish_mise_tools
```

- [ ] **Step 5: Keep missing `mise` as a marker-backed category failure**

Leave `setup_mise` calls to `mark_install_warning ...; exit_after_install_warning` in place. With the new helper, a successful marker write exits 0 in routine and first-run applies; a marker-write failure exits 1.

- [ ] **Step 6: Run the targeted test**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: PASS for new `mise-tools` assertions and existing Optional AI / desktop app assertions.

- [ ] **Step 7: Commit the implementation**

```bash
git add .chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl tests/chezmoiignore_test.sh
git commit -m "fix: make mise warnings non-blocking"
```

---

### Task 3: Render Failing Tests For `shell-integrations`

**Files:**
- Modify: `tests/shell_integrations_test.sh`

- [ ] **Step 1: Update the routine curl-failure expectation**

Replace the current block that expects `sh "$rendered"` to fail on `SHELL_INTEGRATIONS_CURL_STATUS=23` with:

```sh
if ! sh "$rendered" >"$tmp_dir/shell-integrations-curl-failure.out" 2>"$tmp_dir/shell-integrations-curl-failure.err"; then
  unset SHELL_INTEGRATIONS_CURL_STATUS
  fail "shell integrations exits 0 after recording an Oh My Zsh warning marker"
fi
unset SHELL_INTEGRATIONS_CURL_STATUS
pass "shell integrations exits 0 after recording an Oh My Zsh warning marker"
```

- [ ] **Step 2: Update the remaining-item assertion**

Replace:

```sh
assert_not_contains "$test_log" "git args:clone https://github.com/zdharma-continuum/zinit" "shell integrations stops before zinit when Oh My Zsh download fails"
```

with:

```sh
assert_contains "$test_log" "git args:clone https://github.com/zdharma-continuum/zinit" "shell integrations continues to zinit after Oh My Zsh download fails"
assert_contains "$test_log" "git args:clone https://github.com/scmbreeze/scm_breeze.git" "shell integrations continues to SCM Breeze after Oh My Zsh download fails"
```

- [ ] **Step 3: Add failed rerun replacement coverage**

After the marker text assertions for the first failure, add:

```sh
first_marker_text="$marker_text"
: >"$SHELL_INTEGRATIONS_TEST_LOG"
SHELL_INTEGRATIONS_CURL_STATUS=0
export SHELL_INTEGRATIONS_CURL_STATUS
SHELL_INTEGRATIONS_ZINIT_STATUS=31
export SHELL_INTEGRATIONS_ZINIT_STATUS
if ! sh "$rendered" >"$tmp_dir/shell-integrations-zinit-failure.out" 2>"$tmp_dir/shell-integrations-zinit-failure.err"; then
  unset SHELL_INTEGRATIONS_CURL_STATUS SHELL_INTEGRATIONS_ZINIT_STATUS
  fail "shell integrations exits 0 after replacing a zinit warning marker"
fi
unset SHELL_INTEGRATIONS_CURL_STATUS SHELL_INTEGRATIONS_ZINIT_STATUS

replacement_marker_text="$(cat "$shell_integrations_marker")"
assert_contains "$replacement_marker_text" "zinit" "failed shell integration rerun replaces marker with current failed item"
assert_not_contains "$replacement_marker_text" "Oh My Zsh" "failed shell integration rerun replaces stale failed item detail"
assert_contains "$replacement_marker_text" "updated_at='" "failed shell integration rerun writes updated_at"
if [ "$replacement_marker_text" = "$first_marker_text" ]; then
  fail "failed shell integration rerun changes marker content"
fi
pass "failed shell integration rerun changes marker content"
```

- [ ] **Step 4: Add successful rerun clearing coverage**

After the replacement coverage, add:

```sh
: >"$SHELL_INTEGRATIONS_TEST_LOG"
if ! sh "$rendered" >"$tmp_dir/shell-integrations-success.out" 2>"$tmp_dir/shell-integrations-success.err"; then
  fail "successful shell integrations rerun exits 0"
fi

if [ -e "$shell_integrations_marker" ]; then
  fail "successful shell integrations rerun clears shell-integrations marker"
fi
pass "successful shell integrations rerun clears shell-integrations marker"
```

- [ ] **Step 5: Teach the git stub to simulate zinit failure**

In the `write_stub "$tmp_dir/bin/git"` block, replace the zinit clone handler:

```sh
if [ "$1" = clone ] && [ "$2" = https://github.com/zdharma-continuum/zinit ]; then
  mkdir -p "$3"
  exit 0
fi
```

with:

```sh
if [ "$1" = clone ] && [ "$2" = https://github.com/zdharma-continuum/zinit ]; then
  if [ "${SHELL_INTEGRATIONS_ZINIT_STATUS:-0}" != "0" ]; then
    exit "$SHELL_INTEGRATIONS_ZINIT_STATUS"
  fi
  mkdir -p "$3"
  exit 0
fi
```

- [ ] **Step 6: Run the new test and verify it fails before implementation**

Run:

```bash
sh tests/shell_integrations_test.sh
```

Expected: FAIL because the current script exits after the first Oh My Zsh failure and does not continue to zinit or SCM Breeze.

- [ ] **Step 7: Commit the failing tests**

```bash
git add tests/shell_integrations_test.sh
git commit -m "test: cover shell integration warning recovery"
```

---

### Task 4: Make `shell-integrations` Marker-Backed And Non-Blocking

**Files:**
- Modify: `.chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl`
- Test: `tests/shell_integrations_test.sh`

- [ ] **Step 1: Replace the first-run-only warning exit helper**

In `.chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl`, replace the existing `exit_after_install_warning` body with:

```sh
exit_after_install_warning() {
  if [ "$INSTALL_WARNING_RECORDED" -eq 1 ]; then
    exit 0
  fi

  exit 1
}
```

- [ ] **Step 2: Add failure accumulation helpers**

After `clear_install_warning`, add:

```sh
failed_shell_integrations=

append_failed_shell_integration() {
  if [ -n "$failed_shell_integrations" ]; then
    failed_shell_integrations="$failed_shell_integrations, $1"
  else
    failed_shell_integrations="$1"
  fi
}

finish_shell_integrations() {
  if [ -n "$failed_shell_integrations" ]; then
    mark_install_warning \
      shell-integrations \
      "Shell integration setup needs attention" \
      "Failed item(s): $failed_shell_integrations. Review installer output, fix network or installer requirements, then rerun tpod apply."
    exit_after_install_warning
  fi

  clear_install_warning shell-integrations
}
```

- [ ] **Step 3: Convert Oh My Zsh install to accumulated failure**

Replace the current Oh My Zsh block with:

```sh
if [ ! -d "$HOME/.oh-my-zsh" ]; then
  echo "Installing Oh My Zsh..."
  oh_my_zsh_installer="$(mktemp "${TMPDIR:-/tmp}/terrapod-oh-my-zsh-installer.XXXXXX")"
  installer_paths="$installer_paths $oh_my_zsh_installer"
  if curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh -o "$oh_my_zsh_installer"; then
    if ! sh "$oh_my_zsh_installer" --unattended --keep-zshrc; then
      append_failed_shell_integration "Oh My Zsh"
    fi
  else
    append_failed_shell_integration "Oh My Zsh"
  fi
fi
```

- [ ] **Step 4: Convert zinit install to accumulated failure**

Replace the current zinit block with:

```sh
if [ ! -d "$HOME/.local/share/zinit/zinit.git" ]; then
  echo "Installing zinit..."
  mkdir -p "$HOME/.local/share/zinit"
  if ! git clone https://github.com/zdharma-continuum/zinit "$HOME/.local/share/zinit/zinit.git"; then
    append_failed_shell_integration "zinit"
  fi
fi
```

- [ ] **Step 5: Convert SCM Breeze install to accumulated failure**

Replace the current SCM Breeze block with:

```sh
if [ ! -d "$HOME/.scm_breeze" ]; then
  echo "Installing SCM Breeze..."
  if git clone https://github.com/scmbreeze/scm_breeze.git "$HOME/.scm_breeze"; then
    if ! "$HOME/.scm_breeze/install.sh"; then
      append_failed_shell_integration "SCM Breeze"
    fi
  else
    append_failed_shell_integration "SCM Breeze"
  fi
fi
```

- [ ] **Step 6: Finish by writing or clearing the marker**

Replace the final line:

```sh
clear_install_warning shell-integrations
```

with:

```sh
finish_shell_integrations
```

- [ ] **Step 7: Run the targeted test**

Run:

```bash
sh tests/shell_integrations_test.sh
```

Expected: PASS.

- [ ] **Step 8: Commit the implementation**

```bash
git add .chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl tests/shell_integrations_test.sh
git commit -m "fix: make shell integration warnings non-blocking"
```

---

### Task 5: Integration Verification And PR Readiness

**Files:**
- Verify all modified files.

- [ ] **Step 1: Run syntax checks**

Run:

```bash
sh -n .chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl
sh -n .chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl
sh -n tests/chezmoiignore_test.sh
sh -n tests/shell_integrations_test.sh
```

Expected: PASS.

- [ ] **Step 2: Run targeted tests**

Run:

```bash
sh tests/chezmoiignore_test.sh
sh tests/shell_integrations_test.sh
sh tests/terrapod_command_test.sh
```

Expected: PASS.

- [ ] **Step 3: Run full test suite**

Run:

```bash
for test_script in tests/*.sh; do sh "$test_script" || exit $?; done
for test_script in tests/*.zsh; do zsh "$test_script" || exit $?; done
```

Expected: PASS.

- [ ] **Step 4: Review final diff**

Run:

```bash
git diff --stat
git diff -- .chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl .chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl tests/chezmoiignore_test.sh tests/shell_integrations_test.sh docs/superpowers/plans/2026-06-07-mise-shell-warning-categories.md
```

Expected: Diff only contains Issue #102 plan, tests, and the two installer-template changes.

- [ ] **Step 5: Commit any remaining changes**

If previous task commits were combined by the executor, make one final commit:

```bash
git add .chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl .chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl tests/chezmoiignore_test.sh tests/shell_integrations_test.sh docs/superpowers/plans/2026-06-07-mise-shell-warning-categories.md
git commit -m "fix: recover mise and shell warning categories"
```

Expected: Branch has committed changes ready to push.

---

## Self-Review

- Spec coverage: The plan covers marker writes for `mise-tools` and `shell-integrations`, non-blocking successful exits after marker writes, marker clearing on success, marker replacement on failed reruns, remaining item attempts for shell integrations, reliable failed-step detail, GitHub rate-limit guidance for mise, and tests for the requested cases.
- Placeholder scan: No `TBD`, `TODO`, or unspecified "add tests" placeholders remain.
- Type and name consistency: Category names are `mise-tools` and `shell-integrations`; helper names are local to each script and do not require shared library changes.
