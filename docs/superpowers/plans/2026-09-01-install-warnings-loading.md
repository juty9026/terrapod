# Install Warnings Loading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the four ad-hoc ways eleven entry points load `install-warnings.sh` with three deliberate rules, so a missing library can no longer silently disable warning recording, warning reporting, or the retry path.

**Architecture:** Always-run chezmoi scripts inline the library at compile time via `{{ include }}`. The two `run_onchange_` scripts source it by path, unguarded, under `set -e` — inlining would put the library into their rendered-content hash and re-run an `apt` bootstrap and a GitHub font download on every library edit. `tpod` resolves the library lazily at each call site. `install.sh` loads it once from the checkout and treats absence as fatal. The `mark`/`clear`/`exit` wrapper trio, currently duplicated five times with drift, moves into one new library.

**Tech Stack:** POSIX `sh`, chezmoi templates (Go text/template), shell test scripts under `tests/` run as `sh tests/<name>.sh`.

**Spec:** `docs/superpowers/specs/2026-09-01-install-warnings-loading-design.md`

## Global Constraints

- Every shell file is POSIX `sh`. No bashisms, no `local`.
- Every chezmoi script under `.chezmoiscripts/` runs `set -eu`.
- Existing behavior is preserved except where a task states otherwise. Exit codes, printed strings, and marker contents do not change.
- `mark_install_warning` must never return non-zero. Callers run under `set -e`, and a failing bare call would kill the script before its `exit_after_install_warning` line runs.
- Marker category names are fixed by `terrapod_install_warning_categories()`; no task adds or removes one.
- Run a test file with `sh tests/<name>.sh`. It prints `ok - …` per assertion and exits non-zero on the first `not ok`.
- The full relevant suite for this plan is: `tests/terrapod_command_test.sh`, `tests/terrapod_installer_test.sh`, `tests/chezmoiignore_test.sh`, `tests/bootstrap_ubuntu_test.sh`, `tests/shell_integrations_test.sh`. `chezmoiignore_test.sh` takes about 11 seconds; the others are comparable.

---

## Spec Correction Applied In This Plan

The spec's "Fixture source directories gain a file" section says all three synthetic source trees need `install-warning-script.sh`. That is too broad, and this plan narrows it.

Only `copy_desktop_apply_source_fixture` (`tests/terrapod_command_test.sh:551-575`) materializes `.chezmoiscripts/` and runs a real `chezmoi apply` against it, so only it needs the new file. `tests/terrapod_installer_test.sh:640` and `:816` build fixtures for a **stub** chezmoi; they never render a template, and they copy `install-warnings.sh` only so `install.sh`'s own loader finds it.

Task 5 makes the one fixture change. Task 3 confirms the installer fixtures already satisfy the stricter `install.sh` loader.

---

## File Structure

**Created:**
- `dot_local/lib/terrapod/install-warning-script.sh` — installer policy layer: the `INSTALL_WARNING_RECORDED` flag and the five functions over it. Sourced or inlined only by `.chezmoiscripts/`.
- `docs/adr/0016-load-install-warnings-by-a-chosen-rule.md` — records the three loading rules and the accepted first-run prune no-op.

**Modified:**
- `dot_local/bin/executable_terrapod` — one-shot load decision becomes a lazy loader.
- `install.sh` — three guarded loaders become one unconditional load; two dead functions deleted.
- `.chezmoiscripts/run_before_01-retry-ubuntu-bootstrap.sh.tmpl`
- `.chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl`
- `.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl`
- `.chezmoiscripts/run_before_30-install-shell-integrations.sh.tmpl`
- `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl`
- `.chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl`
- `.chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl`
- `.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl`
- `.chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl`
- `tests/terrapod_command_test.sh`, `tests/chezmoiignore_test.sh`

**Unchanged on purpose:** `dot_local/lib/terrapod/install-warnings.sh`. Its `TERRAPOD_INSTALL_WARNINGS_LOADED=1` assignment stays; only the guards that read it are deleted. `.chezmoiignore` gains nothing — both libraries deploy, following `homebrew-core-bundle.sh`.

---

### Task 1: Add the installer policy library

Creates the shared trio so later tasks have something to inline. No consumer changes yet, so the suite must stay green.

**Files:**
- Create: `dot_local/lib/terrapod/install-warning-script.sh`
- Modify: `tests/terrapod_command_test.sh` (near `:846-848` and `:916`)

**Interfaces:**
- Consumes: `terrapod_install_warning_write`, `terrapod_install_warning_clear` from `dot_local/lib/terrapod/install-warnings.sh`.
- Produces: global `INSTALL_WARNING_RECORDED`; functions `mark_install_warning category summary guidance` (always returns 0), `install_warning_recorded` (0 when the last mark succeeded), `exit_after_install_warning` (exits 0 or 1), `continue_after_core_install_warning` (returns 0 or exits 1), `clear_install_warning category` (always returns 0).

- [ ] **Step 1: Write the failing tests**

Find the block in `tests/terrapod_command_test.sh` that begins `assert_line \` with `".local/lib/terrapod/install-warnings.sh" \` (around line 845). Add immediately after that block:

```sh
assert_line \
  "$managed_targets" \
  ".local/lib/terrapod/install-warning-script.sh" \
  "chezmoi manages the shared install warning script helper library"
```

Then find `sh -n "$install_warnings_lib" || fail "shared install warning marker library is valid POSIX shell"` (around line 916) and add after it:

```sh
install_warning_script_lib="$repo_root/dot_local/lib/terrapod/install-warning-script.sh"

if [ ! -f "$install_warning_script_lib" ]; then
  fail "shared install warning script helper library exists"
fi
pass "shared install warning script helper library exists"

sh -n "$install_warning_script_lib" || fail "shared install warning script helper library is valid POSIX shell"
pass "shared install warning script helper library is valid POSIX shell"
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `sh tests/terrapod_command_test.sh`
Expected: FAIL with `not ok - chezmoi manages the shared install warning script helper library`

- [ ] **Step 3: Create the library**

Create `dot_local/lib/terrapod/install-warning-script.sh`:

```sh
#!/bin/sh

# Installer-side policy over the install warning markers. Chezmoi scripts inline
# or source this beside install-warnings.sh; tpod does not use it.
#
# There is deliberately no TERRAPOD_..._LOADED sentinel here. Nothing may branch
# on whether this file loaded — that branch is the defect this library removes.

INSTALL_WARNING_RECORDED=0

# Records a warning marker. Never fails: callers run under `set -e`, and a
# non-zero return here would kill the script before it reaches its exit policy.
mark_install_warning() {
  category="$1"
  summary="$2"
  guidance="$3"
  INSTALL_WARNING_RECORDED=0

  if terrapod_install_warning_write "$category" "$summary" "$guidance"; then
    INSTALL_WARNING_RECORDED=1
  fi

  return 0
}

install_warning_recorded() {
  [ "$INSTALL_WARNING_RECORDED" -eq 1 ]
}

# The user was told what went wrong, so the apply continues. If we could not
# even record the warning, fail loudly instead of failing silently.
exit_after_install_warning() {
  if install_warning_recorded; then
    exit 0
  fi

  exit 1
}

continue_after_core_install_warning() {
  if ! install_warning_recorded; then
    exit 1
  fi

  return 0
}

clear_install_warning() {
  terrapod_install_warning_clear "$1" || true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `sh tests/terrapod_command_test.sh`
Expected: PASS, including the three new `ok -` lines.

- [ ] **Step 5: Confirm nothing else broke**

Run: `sh tests/chezmoiignore_test.sh`
Expected: PASS. The new file has no profile scope, so it must not appear in the `macos_only_entries` or `linux_only_entries` lists (`:238-276`).

- [ ] **Step 6: Commit**

```bash
git add dot_local/lib/terrapod/install-warning-script.sh tests/terrapod_command_test.sh
git commit -m "Add shared install warning script helper library"
```

---

### Task 2: Make tpod resolve the library lazily

Fixes symptom 1: during the first-run apply, `tpod` starts before `~/.local/lib/terrapod/install-warnings.sh` exists and disables warning reporting for the whole run.

**Files:**
- Modify: `dot_local/bin/executable_terrapod:25-27`, `:1244-1246`, `:1519-1521`, `:1660-1663`, `:1685-1687`
- Test: `tests/terrapod_command_test.sh`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `ensure_install_warnings_loaded` (returns 0 when `terrapod_install_warning_*` are callable, 1 otherwise) in `executable_terrapod`. No other file uses it.

- [ ] **Step 1: Write the failing test**

Append to `tests/terrapod_command_test.sh`, before its final `pass`/exit. This mirrors the first-run sequence: the library path is absent when `tpod` starts, and the delegated apply creates it.

```sh
lazy_home="$tmp_dir/lazy-lib-home"
lazy_state="$lazy_home/.local/state"
lazy_lib="$tmp_dir/lazy-lib/install-warnings.sh"
lazy_bin="$tmp_dir/lazy-lib-bin"
lazy_config="$tmp_dir/lazy-lib-chezmoi.toml"
mkdir -p "$lazy_home" "$lazy_state" "$lazy_bin" "$tmp_dir/lazy-lib"
: >"$lazy_config"

if [ -e "$lazy_lib" ]; then
  fail "lazy library fixture starts without the install warning library"
fi

# The stub apply creates the library mid-run, exactly as the first real apply does.
write_stub "$lazy_bin/chezmoi" \
  'if [ "${1-}" = "apply" ]; then' \
  '  mkdir -p "$(dirname "$LAZY_LIB_TARGET")"' \
  '  cp "$LAZY_LIB_SOURCE" "$LAZY_LIB_TARGET"' \
  '  HOME="$LAZY_HOME" XDG_STATE_HOME="$LAZY_STATE" sh -c '"'"'. "$1"; terrapod_install_warning_write mise-tools "mise tool install needs attention" "Rerun tpod apply."'"'"' sh "$LAZY_LIB_TARGET"' \
  '  printf "%s\n" "stub apply output"' \
  'fi' \
  'exit 0'

lazy_output="$(
  LAZY_LIB_TARGET="$lazy_lib" \
  LAZY_LIB_SOURCE="$repo_root/dot_local/lib/terrapod/install-warnings.sh" \
  LAZY_HOME="$lazy_home" \
  LAZY_STATE="$lazy_state" \
  HOME="$lazy_home" \
  XDG_STATE_HOME="$lazy_state" \
  TERRAPOD_INSTALL_WARNINGS_LIB="$lazy_lib" \
  TERRAPOD_PROFILE=macos-terminal \
  TERRAPOD_CHEZMOI_CONFIG="$lazy_config" \
  PATH="$lazy_bin:$PATH" \
    "$terrapod" apply 2>&1
)" || true

assert_contains \
  "$lazy_output" \
  "Remaining install warnings:" \
  "Terrapod apply reports warnings written by an apply that also installed the marker library"

assert_contains \
  "$lazy_output" \
  "mise-tools: mise tool install needs attention" \
  "Terrapod apply reads the marker library that appeared during the delegated apply"
```

If `write_stub` or `assert_contains` is not defined at that point in the file, move the block above the first use of the helper it needs; both are defined near the top of the file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `sh tests/terrapod_command_test.sh`
Expected: FAIL with `not ok - Terrapod apply reports warnings written by an apply that also installed the marker library`. The library existed by the time `print_remaining_install_warnings` ran, but the start-of-script decision had already disabled it.

- [ ] **Step 3: Replace the one-shot decision with a lazy loader**

In `dot_local/bin/executable_terrapod`, delete lines 25-27:

```sh
TERRAPOD_INSTALL_WARNINGS_LOADED=
if [ -f "$install_warnings_lib" ]; then
  . "$install_warnings_lib"
fi
```

Leave the `TERRAPOD_HOMEBREW_PREFIX_LOADED` block that follows it untouched. Add this function immediately after the `homebrew_prefix_lib` block, before `run_jetendard_check`:

```sh
# Resolved per call rather than once at startup: during the first-run apply the
# library target does not exist yet when tpod starts, but does by the time the
# post-apply readers run.
ensure_install_warnings_loaded() {
  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" = "1" ]; then
    return 0
  fi
  if [ ! -f "$install_warnings_lib" ]; then
    return 1
  fi

  . "$install_warnings_lib"
}
```

Do not collapse this to `[ … ] && return 0`. Under the `set -eu` at line 2, a failing left operand makes the whole AND-list fail and kills `tpod`.

- [ ] **Step 4: Convert the four call sites**

In `install_warning_categories_csv` (around `:1244`), replace:

```sh
  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" != "1" ]; then
    return 0
  fi
```

with:

```sh
  if ! ensure_install_warnings_loaded; then
    return 0
  fi
```

In `prune_retired_install_warnings` (around `:1519`), replace:

```sh
  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" != "1" ]; then
    return
  fi
```

with:

```sh
  if ! ensure_install_warnings_loaded; then
    return
  fi
```

In `doctor_check_install_warnings` (around `:1660`), replace:

```sh
  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" != "1" ]; then
    doctor_ok "No install warning marker reader is installed"
    return
  fi
```

with:

```sh
  if ! ensure_install_warnings_loaded; then
    doctor_ok "No install warning marker reader is installed"
    return
  fi
```

In `print_remaining_install_warnings` (around `:1685`), replace:

```sh
  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" != "1" ]; then
    return
  fi
```

with:

```sh
  if ! ensure_install_warnings_loaded; then
    return
  fi
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `sh tests/terrapod_command_test.sh`
Expected: PASS, including the two new assertions.

The existing cases that point `TERRAPOD_INSTALL_WARNINGS_LIB` at `$tmp_dir/missing-install-warnings-lib` (`:2037`, `:2669`) must still pass: the file is absent at call time too, so `ensure_install_warnings_loaded` returns 1 and the same early return runs.

- [ ] **Step 6: Commit**

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh
git commit -m "Resolve the install warning library lazily in tpod"
```

---

### Task 3: Load the library once in install.sh

Removes two never-called functions and the guard that let a broken checkout skip the snapshot deciding whether the first run reports warnings.

**Files:**
- Modify: `install.sh:986-1028`, `:1035`, `:1052`, after `:1353`
- Test: `tests/terrapod_installer_test.sh`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `load_install_warnings_from_source source_dir` — sources the library from the checkout, returns non-zero if the file is missing. Callers in `install.sh` only.

- [ ] **Step 1: Write the failing test**

Append to `tests/terrapod_installer_test.sh`, after the existing fixture helpers are defined. It asserts the installer refuses a checkout with no library instead of silently continuing.

```sh
missing_lib_source="$tmp_dir/missing-lib-source"
mkdir -p "$missing_lib_source/dot_local/lib/terrapod"

missing_lib_output="$(
  sh -c '
    . "$1"
    load_install_warnings_from_source "$2"
  ' sh "$repo_root/install.sh" "$missing_lib_source" 2>&1
)" && fail "install.sh rejects a checkout with no install warning library"
pass "install.sh rejects a checkout with no install warning library"
```

If sourcing `install.sh` runs `main` and makes that shape unworkable, assert on the code instead — both statements must hold:

```sh
if grep -n "mark_install_warning_from_source" "$repo_root/install.sh" >/dev/null; then
  fail "install.sh no longer defines the never-called mark_install_warning_from_source"
fi
pass "install.sh no longer defines the never-called mark_install_warning_from_source"

if grep -n "load_install_warnings_from_source .* || return 0" "$repo_root/install.sh" >/dev/null; then
  fail "install.sh no longer skips the marker snapshot when the library is missing"
fi
pass "install.sh no longer skips the marker snapshot when the library is missing"
```

`install.sh` ends with `main "$@"`, so the second shape is the expected one. Use it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `sh tests/terrapod_installer_test.sh`
Expected: FAIL with `not ok - install.sh no longer defines the never-called mark_install_warning_from_source`

- [ ] **Step 3: Delete the dead functions**

In `install.sh`, delete `mark_install_warning_from_source` (`:986-1001`) and `clear_install_warning_from_source` (`:1003-1016`) in full. Confirm first that nothing calls them:

```bash
grep -n "mark_install_warning_from_source\|clear_install_warning_from_source" install.sh
```

Only the two definition lines should appear.

- [ ] **Step 4: Make the remaining loader unconditional**

Replace `load_install_warnings_from_source` (`:1018-1028`) with:

```sh
load_install_warnings_from_source() {
  source_dir="$1"
  install_warnings_lib="$source_dir/dot_local/lib/terrapod/install-warnings.sh"

  [ -f "$install_warnings_lib" ] || return 1

  . "$install_warnings_lib"
}
```

In `snapshot_install_warnings_from_source`, delete the line:

```sh
  load_install_warnings_from_source "$source_dir" || return 0
```

In `install_warning_markers_changed_since_snapshot`, delete the line:

```sh
  load_install_warnings_from_source "$source_dir" || return 1
```

- [ ] **Step 5: Load once from main**

In `main`, immediately after the `ensure_first_run_setup "$profile" "$source_dir" "$chezmoi_bin"` line (`:1353`), insert:

```sh
  load_install_warnings_from_source "$source_dir" ||
    fatal "failed to load the install warning library from $source_dir"
```

This runs after the checkout is guaranteed and before `apply_recovery_core_command_surface`, so both snapshot helpers called from `run_initial_apply` find the functions defined.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `sh tests/terrapod_installer_test.sh`
Expected: PASS. The existing fixtures at `:640` and `:816` already copy `install-warnings.sh` into their source directories, which is what the stricter loader now requires.

- [ ] **Step 7: Commit**

```bash
git add install.sh tests/terrapod_installer_test.sh
git commit -m "Load the install warning library once in install.sh"
```

---

### Task 4: Inline the library into three always-run scripts

Converts the straightforward trio users. `run_before_10` is deliberately excluded — it carries two extra complications and gets its own task.

**Files:**
- Modify: `.chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl:8-42`
- Modify: `.chezmoiscripts/run_before_30-install-shell-integrations.sh.tmpl:7-49`
- Modify: `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl:8-32`, `:46-60`
- Modify: `tests/terrapod_command_test.sh:1252-1334`

**Interfaces:**
- Consumes: `mark_install_warning`, `install_warning_recorded`, `exit_after_install_warning`, `clear_install_warning` from Task 1's `install-warning-script.sh`.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Convert run_after_20**

In `.chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl`, replace lines 8-42 — the `install_warnings_lib=` block through the closing `}` of `clear_install_warning` — with:

```
{{ include "dot_local/lib/terrapod/install-warnings.sh" }}

{{ include "dot_local/lib/terrapod/install-warning-script.sh" }}
```

Leave `set -eu` at `:4` and the `homebrew-prefix.sh` include at `:6` alone. The call sites at `:58-62`, `:65`, and `:74-78` are unchanged: they already use `mark_install_warning …` followed by `exit_after_install_warning`.

- [ ] **Step 2: Convert run_before_30**

In `.chezmoiscripts/run_before_30-install-shell-integrations.sh.tmpl`, replace lines 7-41 — the `install_warnings_lib=` block through the closing `}` of `clear_install_warning` — with:

```
{{ include "dot_local/lib/terrapod/install-warnings.sh" }}

{{ include "dot_local/lib/terrapod/install-warning-script.sh" }}
```

Then replace `shell_integrations_warning_exists` (`:43-49`) with:

```sh
shell_integrations_warning_exists() {
  terrapod_install_warning_existing_path shell-integrations >/dev/null 2>&1
}
```

It stays in this script: it is bound to one category, not to `INSTALL_WARNING_RECORDED`.

- [ ] **Step 3: Convert run_before_60**

In `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl`, replace lines 8-32 — the `install_warnings_lib=` block through the closing `}` of `clear_install_warning` — with:

```
{{ include "dot_local/lib/terrapod/install-warnings.sh" }}

{{ include "dot_local/lib/terrapod/install-warning-script.sh" }}
```

Move `AI_CLI_WARNING_CATEGORY=optional-ai-cli-tools` from `:7` to **below** the includes, so the head of the file reads:

```
{{- if or (eq .chezmoi.os "darwin") (eq .chezmoi.os "linux") -}}
#!/bin/sh
set -eu

{{ include "dot_local/lib/terrapod/homebrew-prefix.sh" }}

{{ include "dot_local/lib/terrapod/install-warnings.sh" }}

{{ include "dot_local/lib/terrapod/install-warning-script.sh" }}

AI_CLI_WARNING_CATEGORY=optional-ai-cli-tools
```

The position matters: Step 4 anchors a test stub to that line, and the stub must land *after* the inlined definitions to override them.

This script has the one call site that read the old return value. Replace `finish_ai_cli_install` (`:46-60`) with:

```sh
finish_ai_cli_install() {
  if [ "$1" -ne 0 ]; then
    mark_install_warning \
      "$AI_CLI_WARNING_CATEGORY" \
      "Optional AI CLI tool install needs attention" \
      "Homebrew setup or the AI CLI bundle failed. Review Homebrew output and network access, then rerun tpod apply."

    if ! install_warning_recorded; then
      echo "Failed to record Optional AI CLI tool install warning." >&2
      return 1
    fi

    return 0
  fi

  clear_install_warning "$AI_CLI_WARNING_CATEGORY"
}
```

- [ ] **Step 4: Repair the four run_before_60 tests built on a missing library**

`tests/terrapod_command_test.sh:1252-1334` holds four assertions that render `run_before_60` with `"sourceDir":"/missing-terrapod-source"` so the library cannot load. Inlining reads from `--source "$repo_root"`, not from the `sourceDir` override, so that premise no longer exists. Two of them pass by accident and two break.

First, retire the two whose meaning is now false. At `:1268-1275`, change the two messages and the `sourceDir`:

```sh
chezmoi execute-template \
  --source "$repo_root" \
  --override-data "{\"chezmoi\":{\"os\":\"darwin\",\"sourceDir\":\"$repo_root\"},\"enableAiCliTools\":true}" \
  --file "$repo_root/.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl" \
  | sed "s#/opt/homebrew/bin/brew#$fake_warning_bin/brew#g" \
  >"$fake_ai_cli_installer"

if ! HOME="$fake_ai_cli_home" FAKE_INSTALL_WARNING_CALLS="$fake_warning_calls" TERRAPOD_MACHINE_ARCH=aarch64 PATH="$fake_warning_bin:/usr/bin:/bin" /bin/sh "$fake_ai_cli_installer" >"$tmp_dir/fake-ai-cli-installer.out" 2>"$tmp_dir/fake-ai-cli-installer.err"; then
  fail "rendered installer fixture succeeds when the Homebrew AI CLI bundle succeeds"
fi

if [ -e "$fake_warning_calls" ]; then
  fail "installer scripts ignore PATH fake install warning helpers"
fi
pass "installer scripts ignore PATH fake install warning helpers"
```

Second, delete the block at `:1277-1299` in full — from `fake_ai_cli_failure_home="$tmp_dir/fake-ai-cli-failure-home"` through its `pass "rendered installer fixture fails when optional AI CLI failures cannot be recorded without the shared library"`. It asserts the script fails when the library is missing, which inlining makes unreachable. The next block covers the case that still matters: the library loads and the marker write fails.

Third, rewrite that next block (`:1301-1334`) to plant its stub by appending after the include rather than by placing a file in a source directory the include no longer reads. Replace the whole block with:

```sh
fake_ai_cli_write_failure_home="$tmp_dir/fake-ai-cli-write-failure-home"
fake_ai_cli_warning_stub="$tmp_dir/fake-ai-cli-warning-stub.sh"
mkdir -p "$fake_ai_cli_write_failure_home/.local/bin"
cat >"$fake_ai_cli_warning_stub" <<'STUB'
terrapod_install_warning_write() {
  printf "%s\n" "write failed:$*" >&2
  return 1
}
terrapod_install_warning_clear() {
  return 0
}
STUB
write_stub "$fake_ai_cli_write_failure_home/.local/bin/brew" \
  'case "$1" in' \
  '  shellenv) printf "%s\n" ":" ;;' \
  '  bundle) exit 42 ;;' \
  '  *) exit 64 ;;' \
  'esac'

# The script inlines install-warnings.sh, so the stub is appended after the
# category assignment to override the real definitions.
fake_ai_cli_write_failure_installer="$tmp_dir/fake-ai-cli-write-failure-installer.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"darwin"},"enableAiCliTools":true}' \
  --file "$repo_root/.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl" \
  | sed \
    -e "s#/opt/homebrew/bin/brew#$fake_ai_cli_write_failure_home/.local/bin/brew#g" \
    -e "/^AI_CLI_WARNING_CATEGORY=/r $fake_ai_cli_warning_stub" \
  >"$fake_ai_cli_write_failure_installer"

fake_ai_cli_write_failure_status=0
HOME="$fake_ai_cli_write_failure_home" TERRAPOD_MACHINE_ARCH=aarch64 PATH="$fake_ai_cli_write_failure_home/.local/bin:/usr/bin:/bin" /bin/sh "$fake_ai_cli_write_failure_installer" >"$tmp_dir/fake-ai-cli-write-failure.out" 2>"$tmp_dir/fake-ai-cli-write-failure.err" || fake_ai_cli_write_failure_status=$?
if [ "$fake_ai_cli_write_failure_status" -eq 0 ]; then
  fail "rendered installer fixture fails when optional AI CLI failures cannot be recorded after marker write failure"
fi
pass "rendered installer fixture fails when optional AI CLI failures cannot be recorded after marker write failure"
```

The `r` command inserts the stub after `AI_CLI_WARNING_CATEGORY=`, which Step 3 placed below the includes — so the stub's definitions override the inlined ones. Confirm the ordering before running the test:

```bash
chezmoi execute-template --source . \
  --override-data '{"chezmoi":{"os":"darwin"},"enableAiCliTools":true}' \
  --file .chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl \
  | grep -n 'terrapod_install_warning_write() {\|^AI_CLI_WARNING_CATEGORY='
```

Expected: the `terrapod_install_warning_write() {` line number is smaller than the `AI_CLI_WARNING_CATEGORY=` one. If it is not, Step 3's reordering was not applied.

- [ ] **Step 5: Verify all three render as valid POSIX shell**

```bash
for f in run_after_20-install-mise-tools run_before_30-install-shell-integrations run_before_60-install-ai-cli-tools; do
  chezmoi execute-template --source . \
    --override-data '{"chezmoi":{"os":"darwin"},"enableAiCliTools":true}' \
    --file ".chezmoiscripts/$f.sh.tmpl" | sh -n && echo "ok $f"
done
```

Expected: `ok` for all three. A rendered script that still contains `install_warnings_lib=` means an include line did not replace the block.

- [ ] **Step 6: Run the affected tests**

Run: `sh tests/shell_integrations_test.sh && sh tests/terrapod_command_test.sh && sh tests/chezmoiignore_test.sh`
Expected: PASS. `chezmoiignore_test.sh` exercises the Optional AI Tool Stack warning paths that Step 3 rewrote, and `terrapod_command_test.sh` runs the four assertions Step 4 repaired.

- [ ] **Step 7: Commit**

```bash
git add .chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl \
        .chezmoiscripts/run_before_30-install-shell-integrations.sh.tmpl \
        .chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl \
        tests/terrapod_command_test.sh
git commit -m "Inline the install warning libraries into three always-run scripts"
```

---

### Task 5: Inline both libraries into run_before_10

The largest consumer. It also guards `homebrew-core-bundle.sh` the same way, and it has the one call site that relied on `mark_install_warning` returning non-zero.

**Files:**
- Modify: `.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl:17-68`, `:300-325`, `:343-348`
- Modify: `tests/terrapod_command_test.sh:551-575`

**Interfaces:**
- Consumes: the five functions from Task 1.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Add the new library to the apply fixture**

`copy_desktop_apply_source_fixture` in `tests/terrapod_command_test.sh` builds a source tree and runs a real `chezmoi apply` against it. `{{ include }}` reads from that tree at render time, so the render fails without the new file. Find:

```sh
  cp "$install_warnings_lib" "$source_dir/dot_local/lib/terrapod/install-warnings.sh"
```

and add after it:

```sh
  cp "$repo_root/dot_local/lib/terrapod/install-warning-script.sh" \
    "$source_dir/dot_local/lib/terrapod/install-warning-script.sh"
```

- [ ] **Step 2: Replace the two loader blocks and the trio**

Replace lines 17-68 — from `install_warnings_lib=` through the closing `}` of `clear_install_warning` — with:

```
{{ include "dot_local/lib/terrapod/install-warnings.sh" }}

{{ include "dot_local/lib/terrapod/install-warning-script.sh" }}

{{ include "dot_local/lib/terrapod/homebrew-core-bundle.sh" }}
```

This drops `TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED` along with `TERRAPOD_INSTALL_WARNINGS_LOADED`; the library can no longer be missing, so neither guard has anything to decide.

- [ ] **Step 3: Delete the core-bundle fallback branch**

The `else` branch at `:314-324` exists only for the case where `homebrew-core-bundle.sh` failed to load, and it runs a bare `brew bundle` without the library's guidance text. That case is now impossible. Replace the whole block at `:300-325`:

```sh
if [ -f "$core_brewfile" ]; then
  if [ "${TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED:-}" = "1" ]; then
    if terrapod_homebrew_core_run_bundle "$core_brewfile"; then
      clear_install_warning homebrew-core
    else
      if [ -z "${TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT:-}" ]; then
        TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output, fix package access, then rerun tpod apply."
      fi
      mark_install_warning \
        homebrew-core \
        "Homebrew core install needs attention" \
        "$TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT"
      continue_after_core_install_warning
    fi
  else
    if HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade --file="$core_brewfile"; then
      clear_install_warning homebrew-core
    else
      mark_install_warning \
        homebrew-core \
        "Homebrew core install needs attention" \
        "Review Homebrew core bundle output, fix package access, then rerun tpod apply."
      continue_after_core_install_warning
    fi
  fi
fi
```

with:

```sh
if [ -f "$core_brewfile" ]; then
  if terrapod_homebrew_core_run_bundle "$core_brewfile"; then
    clear_install_warning homebrew-core
  else
    if [ -z "${TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT:-}" ]; then
      TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output, fix package access, then rerun tpod apply."
    fi
    mark_install_warning \
      homebrew-core \
      "Homebrew core install needs attention" \
      "$TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT"
    continue_after_core_install_warning
  fi
fi
```

- [ ] **Step 4: Convert the desktop-apps call site**

At `:343-348` the desktop-apps branch relies on the old non-zero return. Replace:

```sh
    mark_install_warning \
      homebrew-desktop-apps \
      "Homebrew desktop app install needs attention" \
      "$desktop_app_failure_guidance_text" ||
      exit 1
    exit 0
```

with:

```sh
    mark_install_warning \
      homebrew-desktop-apps \
      "Homebrew desktop app install needs attention" \
      "$desktop_app_failure_guidance_text"
    exit_after_install_warning
```

The exit codes are identical: recorded exits 0, not recorded exits 1.

The remaining `mark_install_warning` call sites (`:254`, `:262`, `:274`, `:282`, `:288`) already pair with `exit_after_install_warning` and need no change.

- [ ] **Step 5: Verify the render**

```bash
chezmoi execute-template --source . \
  --override-data '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":true}' \
  --file .chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl | sh -n && echo ok
```

Expected: `ok`.

- [ ] **Step 6: Run the affected tests**

Run: `sh tests/terrapod_command_test.sh && sh tests/chezmoiignore_test.sh`
Expected: PASS. `terrapod_command_test.sh` runs the real apply fixture from Step 1; `chezmoiignore_test.sh:284-296` renders `run_before_10` under six data sets.

- [ ] **Step 7: Commit**

```bash
git add .chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl tests/terrapod_command_test.sh
git commit -m "Inline the install warning and Homebrew core bundle libraries into run_before_10"
```

---

### Task 6: Inline the library into the Ubuntu retry script

`run_before_01` is the variant that exits 0 when the library is missing, reporting success for a retry it never attempted.

**Files:**
- Modify: `.chezmoiscripts/run_before_01-retry-ubuntu-bootstrap.sh.tmpl:5-9`
- Test: `tests/bootstrap_ubuntu_test.sh`

**Interfaces:**
- Consumes: `terrapod_install_warning_existing_path`, `terrapod_install_warning_clear`, `terrapod_install_warning_write` from the marker API. It does not use the policy layer.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Replace the guard with an include**

Replace lines 5-9:

```sh
install_warnings_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warnings.sh"
if [ ! -f "$install_warnings_lib" ]; then
  exit 0
fi
. "$install_warnings_lib"
```

with:

```
{{ include "dot_local/lib/terrapod/install-warnings.sh" }}
```

Leave `set -eu` at `:3` and the `ubuntu_bootstrap_lib` source at `:15-16` alone — the latter is already unguarded under `set -e`, which is the behavior this change is spreading.

- [ ] **Step 2: Verify the render**

```bash
chezmoi execute-template --source . \
  --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
  --file .chezmoiscripts/run_before_01-retry-ubuntu-bootstrap.sh.tmpl | sh -n && echo ok
```

Expected: `ok`.

- [ ] **Step 3: Run the affected test**

Run: `sh tests/bootstrap_ubuntu_test.sh`
Expected: PASS. `:286` and `:295` render this template and exercise the retry path.

- [ ] **Step 4: Commit**

```bash
git add .chezmoiscripts/run_before_01-retry-ubuntu-bootstrap.sh.tmpl
git commit -m "Inline the install warning library into the Ubuntu bootstrap retry"
```

---

### Task 7: Inline the library into the two always-run Jetendard scripts

Fixes symptom 3. These run `set -u` without `set -e`, so a failed source leaves undefined functions that return 127 — read as "no marker" in the retry, and as a post-success failure in the settings script. Also repairs the test fixture seam that inlining breaks.

**Files:**
- Modify: `.chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl:2-7`
- Modify: `.chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl:2-15`
- Modify: `tests/chezmoiignore_test.sh:403-411`, and append a new case

**Interfaces:**
- Consumes: the marker API only.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Fix the fixture seam first**

`render_jetendard_adapter_fixture` swaps the stub library in by rewriting the `^warnings_lib=` line. Inlining deletes that line, so the `sed` would silently no-op and the real library would run. Switch to inserting the stub *after* the library, using the surviving `font_helper=` / `settings_helper=` lines as anchors — shell function redefinition then wins. Replace `:403-411`:

```sh
render_jetendard_adapter_fixture() {
  rendered="$1"
  destination="$2"
  printf '%s\n' "$rendered" |
    sed \
      -e "s#^warnings_lib=.*#warnings_lib=\"$jetendard_warnings_stub\"#" \
      -e "s#^font_helper=.*#font_helper=\"$jetendard_helper_stub\"#" \
      -e "s#^settings_helper=.*#settings_helper=\"$jetendard_helper_stub\"#" \
      >"$destination"
}
```

with:

```sh
# The adapters inline install-warnings.sh, so the stub is appended after the
# helper assignment to override the real definitions rather than replacing a path.
render_jetendard_adapter_fixture() {
  rendered="$1"
  destination="$2"
  printf '%s\n' "$rendered" |
    sed \
      -e "s#^warnings_lib=.*#warnings_lib=\"$jetendard_warnings_stub\"#" \
      -e "s#^font_helper=.*#font_helper=\"$jetendard_helper_stub\"#" \
      -e "s#^settings_helper=.*#settings_helper=\"$jetendard_helper_stub\"#" \
      -e "/^font_helper=/r $jetendard_warnings_stub" \
      -e "/^settings_helper=/r $jetendard_warnings_stub" \
      >"$destination"
}
```

The two `s#^warnings_lib=...#` and path substitutions stay because `run_onchange_after_65` still has a `warnings_lib=` line after Task 8.

- [ ] **Step 2: Write the failing test**

Append to `tests/chezmoiignore_test.sh`, after the existing Jetendard adapter cases (after the `:437` block). It pins symptom 3: with `python3` gone, the settings script records the warning and exits 0.

```sh
jetendard_no_python_bin="$tmp_dir/jetendard-no-python-bin"
mkdir -p "$jetendard_no_python_bin"

: >"$jetendard_adapter_log"
if ! JETENDARD_ADAPTER_LOG="$jetendard_adapter_log" \
  JETENDARD_MARKER_EXISTS=0 \
  JETENDARD_CLEAR_FAIL=0 \
  PATH="$jetendard_no_python_bin" \
  sh "$jetendard_settings_fixture" >/dev/null 2>&1; then
  fail "Jetendard settings adapter records a warning and succeeds without python3"
fi
pass "Jetendard settings adapter records a warning and succeeds without python3"

assert_text_equals \
  "$(cat "$jetendard_adapter_log")" \
  'write' \
  "Jetendard settings adapter writes exactly one warning when python3 is missing"
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `sh tests/chezmoiignore_test.sh`
Expected: FAIL. Today `run_after_70` has no `set -e`; the real library is not stubbed after Step 1's change until the script is inlined, so the assertions do not hold yet.

- [ ] **Step 4: Convert run_before_02**

Replace lines 2-7 of `.chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl`:

```sh
#!/bin/sh
set -u

warnings_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warnings.sh"
font_helper="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/executable_jetendard-font"
. "$warnings_lib"
```

with:

```
#!/bin/sh
set -eu

{{ include "dot_local/lib/terrapod/install-warnings.sh" }}

font_helper="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/executable_jetendard-font"
```

The `font_helper=` line must stay last of the two so the fixture's `r` command inserts the stub after the library.

- [ ] **Step 5: Convert run_after_70**

Replace lines 2-7 of `.chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl` the same way, keeping `settings_helper=` after the include:

```
#!/bin/sh
set -eu

{{ include "dot_local/lib/terrapod/install-warnings.sh" }}

settings_helper="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/executable_jetendard-settings"
```

Then fix the `$?` capture, which `set -e` would otherwise abort on. Replace:

```sh
python3 "$settings_helper" apply
settings_status="$?"
```

with:

```sh
settings_status=0
python3 "$settings_helper" apply || settings_status="$?"
```

Leave the `case "$settings_status" in` block and all three branches unchanged.

- [ ] **Step 6: Verify both renders**

```bash
for f in run_before_02-retry-jetendard-font run_after_70-apply-jetendard-settings; do
  chezmoi execute-template --source . \
    --override-data '{"chezmoi":{"os":"darwin"}}' \
    --file ".chezmoiscripts/$f.sh.tmpl" | sh -n && echo "ok $f"
done
```

Expected: `ok` for both.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `sh tests/chezmoiignore_test.sh`
Expected: PASS, including the three original adapter assertions at `:421-437` and the two new ones.

- [ ] **Step 8: Commit**

```bash
git add .chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl \
        .chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl \
        tests/chezmoiignore_test.sh
git commit -m "Inline the install warning library into the always-run Jetendard scripts"
```

---

### Task 8: Make the two run_onchange_ scripts fail loudly instead of inlining

These are the exception. Chezmoi triggers `run_onchange_` scripts on rendered-content change, so inlining would put the library's bytes in the hash and re-run an `apt` bootstrap and a full GitHub font download on every library edit — seven such edits between `4d3a9dd` and `f314327`. Removing the guard and adding `set -e` fixes the silent failure without the hash coupling.

**Files:**
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl:5-42`
- Modify: `.chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl:3`
- Test: `tests/bootstrap_ubuntu_test.sh`, `tests/chezmoiignore_test.sh`

**Interfaces:**
- Consumes: the marker API in both; the policy layer in `run_onchange_before_00` only.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Write the failing test**

Append to `tests/bootstrap_ubuntu_test.sh`. It asserts that a source tree with no library stops the script instead of no-op'ing its warning writes.

```sh
missing_lib_source="$tmp_dir/onchange-missing-lib"
mkdir -p "$missing_lib_source"

rendered_missing_lib="$tmp_dir/onchange-missing-lib.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data "{\"chezmoi\":{\"os\":\"linux\",\"sourceDir\":\"$missing_lib_source\",\"osRelease\":{\"id\":\"ubuntu\",\"versionID\":\"24.04\"}}}" \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl" \
  >"$rendered_missing_lib"

if sh "$rendered_missing_lib" >/dev/null 2>&1; then
  fail "Ubuntu bootstrap stops when the install warning library is missing"
fi
pass "Ubuntu bootstrap stops when the install warning library is missing"
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `sh tests/bootstrap_ubuntu_test.sh`
Expected: FAIL with `not ok - Ubuntu bootstrap stops when the install warning library is missing`. Today the guard swallows the missing file and the script proceeds with no-op warning writes.

- [ ] **Step 3: Convert run_onchange_before_00**

Replace lines 5-42 of `.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl` — the `install_warnings_lib=` block through the closing `}` of `clear_install_warning`, but keeping the `ubuntu_bootstrap_lib` lines at `:11-13` — so the head of the file reads:

```sh
#!/bin/sh
set -eu

# Sourced by path rather than inlined: inlining would put this library into the
# run_onchange_ content hash, re-running the apt bootstrap on every library edit.
. "{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warnings.sh"
. "{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warning-script.sh"

# Ubuntu bootstrap helper checksum: {{ include "dot_local/lib/terrapod/ubuntu-bootstrap.sh" | sha256sum }}
ubuntu_bootstrap_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/ubuntu-bootstrap.sh"
. "$ubuntu_bootstrap_lib"
```

The call sites at `:49-53` and `:56` are unchanged.

Note the existing `sha256sum` comment stays: coupling this script to `ubuntu-bootstrap.sh`, its actual subject, is deliberate and predates this change.

- [ ] **Step 4: Convert run_onchange_after_65**

In `.chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl`, change line 3 from `set -u` to `set -eu`, and add the explanatory comment above the existing source:

```sh
#!/bin/sh
set -eu

# Sourced by path rather than inlined: inlining would put this library into the
# run_onchange_ content hash, re-downloading the font on every library edit.
warnings_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warnings.sh"
font_helper="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/executable_jetendard-font"
# Jetendard font helper checksum: {{ include "dot_local/lib/terrapod/executable_jetendard-font" | sha256sum }}
. "$warnings_lib"
```

The source was already unguarded; `set -e` is what turns its 127 into a stop. The `warnings_lib=` line stays, so Task 7's fixture substitution keeps covering this adapter.

- [ ] **Step 5: Verify both renders**

```bash
chezmoi execute-template --source . \
  --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
  --file .chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl | sh -n && echo ok-00
chezmoi execute-template --source . \
  --override-data '{"chezmoi":{"os":"darwin"}}' \
  --file .chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl | sh -n && echo ok-65
```

Expected: `ok-00` and `ok-65`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `sh tests/bootstrap_ubuntu_test.sh && sh tests/chezmoiignore_test.sh`
Expected: PASS, including the new assertion and the three Jetendard adapter assertions.

- [ ] **Step 7: Commit**

```bash
git add .chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl \
        .chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl \
        tests/bootstrap_ubuntu_test.sh
git commit -m "Stop the run_onchange scripts when the install warning library is missing"
```

---

### Task 9: Pin the loading-rule split and record the decision

The nine templates now follow two rules. This task adds the test that keeps the split from drifting and writes the ADR that explains why it is two rules rather than one.

**Files:**
- Modify: `tests/chezmoiignore_test.sh` (append)
- Create: `docs/adr/0016-load-install-warnings-by-a-chosen-rule.md`

**Interfaces:**
- Consumes: the final state of all nine templates from Tasks 4-8.
- Produces: nothing.

- [ ] **Step 1: Write the failing test**

Append to `tests/chezmoiignore_test.sh`:

```sh
inlined_warning_scripts="
.chezmoiscripts/run_before_01-retry-ubuntu-bootstrap.sh.tmpl
.chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl
.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl
.chezmoiscripts/run_before_30-install-shell-integrations.sh.tmpl
.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl
.chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl
.chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl
"

path_sourced_warning_scripts="
.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl
.chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl
"

warning_script_data() {
  case "$1" in
    *run_before_01*|*run_onchange_before_00*)
      printf '%s' "$ubuntu_data"
      ;;
    *)
      printf '%s' "$macos_data"
      ;;
  esac
}

for warning_script in $inlined_warning_scripts; do
  rendered_warning_script="$(render_template "$(warning_script_data "$warning_script")" "$warning_script")"

  assert_contains_text \
    "$rendered_warning_script" \
    "terrapod_install_warning_write() {" \
    "always-run script inlines the marker library: $warning_script"

  assert_not_contains_text \
    "$rendered_warning_script" \
    'if [ -f "$install_warnings_lib" ]; then' \
    "always-run script keeps no install warning loader guard: $warning_script"
done

for warning_script in $path_sourced_warning_scripts; do
  rendered_warning_script="$(render_template "$(warning_script_data "$warning_script")" "$warning_script")"

  assert_not_contains_text \
    "$rendered_warning_script" \
    "terrapod_install_warning_write() {" \
    "run_onchange script keeps the marker library out of its content hash: $warning_script"

  assert_contains_text \
    "$rendered_warning_script" \
    "/dot_local/lib/terrapod/install-warnings.sh" \
    "run_onchange script sources the marker library by path: $warning_script"

  assert_not_contains_text \
    "$rendered_warning_script" \
    'if [ -f "$install_warnings_lib" ]; then' \
    "run_onchange script keeps no install warning loader guard: $warning_script"
done
```

Place it after `ubuntu_data` and `macos_data` are defined (`:194-196`) and after `render_template` (`:54`). Appending at the end of the file satisfies both.

- [ ] **Step 2: Run the test to verify it passes**

Run: `sh tests/chezmoiignore_test.sh`
Expected: PASS. Unlike earlier tasks this test is written last and should pass immediately — it pins state Tasks 4-8 already produced. If any assertion fails, a template was missed; fix the template, not the test.

- [ ] **Step 3: Verify the test would catch a regression**

Temporarily change `run_after_20` back to a path source, run the test, confirm it fails with `not ok - always-run script inlines the marker library`, then revert:

```bash
git stash && sh tests/chezmoiignore_test.sh; git stash pop
```

Skip this step only if the working tree is not clean enough to stash safely; in that case verify by hand-editing one template and reverting it.

- [ ] **Step 4: Write the ADR**

Create `docs/adr/0016-load-install-warnings-by-a-chosen-rule.md`:

```markdown
# Load install warnings by a chosen rule per entry point

Every consumer of `install-warnings.sh` loads it by one of three rules, chosen
for that entry point rather than by habit. No rule can degrade quietly: the
library is either compiled in, or its absence stops the script.

Always-run chezmoi scripts inline the library with `{{ include }}`. Compile-time
inlining is the default because it is the only rule under which the library
cannot be missing, so no guard is needed and none can be reintroduced.

The two `run_onchange_` scripts are the exception. They source the library by
path, unguarded, under `set -e`. Chezmoi triggers `run_onchange_` scripts on
rendered-content change, so inlining would put the library's bytes into the
hash and re-run the work on every library edit — and neither piece of work is
cheap. `install()` in `executable_jetendard-font` has no installed-state
short-circuit: it always calls the GitHub releases API and downloads the full
archive. `ubuntu-bootstrap.sh` always runs `apt-get update` and escalates
through `sudo` for a non-root user. Both need network; one can raise an
unexpected password prompt. Any future `run_onchange_` script that records
install warnings follows this rule.

`tpod` resolves the library lazily at each call site. A decision made once at
process start is always wrong during the first-run apply, because the library
target is created by that very apply.

`install.sh` cannot use `{{ include }}` — it is a `curl | sh` bootstrap, not a
chezmoi template. It loads the library once from the checkout and treats absence
as fatal, since a checkout missing the library is a broken clone.

`TERRAPOD_INSTALL_WARNINGS_LOADED` is a load memo for `tpod` and nothing else.
It never again decides whether warnings work.

## Considered Options

- Inline everywhere, including the two `run_onchange_` scripts: rejected on the
  cost above. `install-warnings.sh` changed seven times between `4d3a9dd` and
  `f314327`, and its most common edit — a new name in
  `terrapod_install_warning_categories()` — happens whenever a new install step
  gains a warning category. Paying an `apt` run and a font download on every
  machine for that is a worse trade than one documented exception.
- Move `prune_retired_install_warnings` after the apply delegation so the
  first-run prune works: rejected because ADR 0014 fixes that ordering so the
  directory stays consistent when apply fails, and reversing it on unrelated
  grounds is not this change's business.
- Give `tpod` a `chezmoi source-path` fallback: rejected as machinery for a
  state no code path produces. `tpod` already resolves the library relative to
  its own directory, which covers running from a checkout; the only remaining
  gap needs markers written by an apply that never wrote the library.

## Consequences

- The first-run prune remains a no-op, because it runs before the apply that
  installs the library. A fresh machine has no markers to prune, so this costs
  nothing. It is not a bug.
- `mark_install_warning` never returns non-zero. Callers run under `set -e`, so
  a failing bare call would kill the script before its exit policy runs. Code
  that needs the outcome calls `install_warning_recorded`.
- Editing `install-warnings.sh` does not re-run any `run_onchange_` script. A
  future author who inlines it into one silently reintroduces that cost; the
  loading-rule assertions in `tests/chezmoiignore_test.sh` fail if they do.
- `install-warning-script.sh` deploys to `~/.local/lib/terrapod/` even though
  only chezmoi scripts use it, following `homebrew-core-bundle.sh`. Making it
  source-tree-only would need an unconditional `.chezmoiignore` entry, a shape
  this repo does not otherwise use.
- `run_before_10` no longer has a fallback branch that runs a bare `brew bundle`
  when `homebrew-core-bundle.sh` fails to load. That branch existed only for a
  state inlining makes impossible.
```

- [ ] **Step 5: Run the full suite**

Run each of these and confirm all pass:

```bash
sh tests/terrapod_command_test.sh
sh tests/terrapod_installer_test.sh
sh tests/chezmoiignore_test.sh
sh tests/bootstrap_ubuntu_test.sh
sh tests/shell_integrations_test.sh
```

Expected: PASS for all five.

- [ ] **Step 6: Confirm no loader guard survives anywhere**

```bash
grep -rn 'TERRAPOD_INSTALL_WARNINGS_LOADED' --exclude-dir=.git --exclude-dir=docs .
```

Expected: exactly two hits — the assignment in `dot_local/lib/terrapod/install-warnings.sh:3`, and the memo read inside `ensure_install_warnings_loaded` in `dot_local/bin/executable_terrapod`. Any other hit is a missed guard.

- [ ] **Step 7: Commit**

```bash
git add tests/chezmoiignore_test.sh docs/adr/0016-load-install-warnings-by-a-chosen-rule.md
git commit -m "Pin the install warning loading rules and record ADR 0016"
```

---

## Verification Against The Issue

After Task 9, each symptom in #153 has a test that fails without its fix:

| Symptom | Fixed by | Pinned by |
|---|---|---|
| 1. `tpod` decides once at startup; first apply reports nothing | Task 2 | `terrapod_command_test.sh`, lazy-library apply case |
| 2. Five scripts no-op silently; five drifted trio copies | Tasks 1, 4, 5 | `chezmoiignore_test.sh` loading-rule assertions |
| 2b. `run_before_01` exits 0 for a retry it never ran | Task 6 | `chezmoiignore_test.sh` loading-rule assertions |
| 3. Jetendard 127 continues past a failed source | Tasks 7, 8 | `chezmoiignore_test.sh` no-python3 settings case; `bootstrap_ubuntu_test.sh` missing-library case |
| Dead code in `install.sh`; snapshot skipped silently | Task 3 | `terrapod_installer_test.sh` |
