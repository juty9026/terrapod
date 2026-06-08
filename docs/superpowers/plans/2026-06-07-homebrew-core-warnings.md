# Homebrew Core Warnings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Make Homebrew core Brewfile bundle failures marker-backed readiness warnings that preserve command output, keep `tpod apply` recoverable, and provide cautious shared-prefix permission guidance.

**Architecture:** Move the Homebrew core bundle attribution logic into a small source-side shell helper so both the `run_onchange` bootstrap script and a new marker-gated `run_before` retry script can share the same behavior. Run the core Brewfile once in bulk to preserve real Homebrew output, then retry parsed Terrapod-declared formula/cask entries one at a time only to identify reliable failed item names. If attribution is not reliable, write generic core bundle guidance that points users to visible output and `tpod apply`. Never run ownership repair and never print broad ownership-change commands.

**Tech Stack:** POSIX `sh`, chezmoi templates, Homebrew `brew bundle`, Terrapod install-warning marker helpers, shell tests in `tests/chezmoiignore_test.sh` and `tests/terrapod_command_test.sh`.

---

## File Structure

- Create `dot_local/lib/terrapod/homebrew-core-bundle.sh`
  - Parse Terrapod's core `Brewfile` for `brew "..."` and `cask "..."` entries.
  - Run bulk `brew bundle --no-upgrade` first.
  - On failure, run single-item bundles to identify failed formula/cask names only when command exit status confirms them.
  - Build one-line marker guidance with fallback detail and optional unwritable-prefix guidance.
- Modify `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`
  - Source the new helper.
  - Use the helper for core Brewfile bundle failures.
  - Exit 0 after recording a `homebrew-core` marker for core bundle failures.
- Create `.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl`
  - Run only when a `homebrew-core` marker exists.
  - Retry the core Brewfile, clear marker on success, and replace marker on failure.
- Modify `.chezmoiignore`
  - Keep the rendered macOS-only core retry hook out of Ubuntu/VPS management.
- Modify `tests/chezmoiignore_test.sh`
  - Render the new retry script.
  - Extend Homebrew stubs to simulate core bulk, formula, cask, output, and prefix failures.
  - Cover successful core bundle, failed marker creation, reliable detail, fallback detail, permission guidance, clear, and replace.
- Modify `tests/terrapod_command_test.sh`
  - Include the new helper and retry script in the minimal real-chezmoi apply fixture.
  - Cover `tpod apply` success with core marker creation, rerun clear, and failed rerun replace.

---

### Task 1: Add Shared Homebrew Core Bundle Helper

**Files:**
- Create: `dot_local/lib/terrapod/homebrew-core-bundle.sh`
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`

- [x] **Step 1: Create the helper with parser, attribution, fallback, and permission guidance**

Create `dot_local/lib/terrapod/homebrew-core-bundle.sh`:

```sh
#!/bin/sh

TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED=1
TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT=

terrapod_homebrew_core_join_lines_with_commas() {
  awk '
    NF {
      if (joined == "") {
        joined = $0
      } else {
        joined = joined ", " $0
      }
    }

    END {
      print joined
    }
  ' "$1"
}

terrapod_homebrew_core_item_records() {
  brewfile="$1"

  awk '
    /^[[:space:]]*brew[[:space:]]+"/ {
      item = $0
      sub(/^[[:space:]]*brew[[:space:]]+"/, "", item)
      sub(/".*$/, "", item)
      if (item != "") {
        printf "formula\t%s\n", item
      }
      next
    }

    /^[[:space:]]*cask[[:space:]]+"/ {
      item = $0
      sub(/^[[:space:]]*cask[[:space:]]+"/, "", item)
      sub(/".*$/, "", item)
      if (item != "") {
        printf "cask\t%s\n", item
      }
    }
  ' "$brewfile"
}

terrapod_homebrew_core_permission_guidance() {
  prefix="$(brew --prefix 2>/dev/null || true)"

  if [ -n "$prefix" ] && [ -e "$prefix" ] && [ ! -w "$prefix" ]; then
    printf '%s\n' "Homebrew prefix is not writable: $prefix. Fix Homebrew permissions for your user or ask the prefix owner/admin; avoid broad ownership changes."
    return
  fi

  if [ -n "$prefix" ]; then
    printf '%s\n' "If this was a permissions failure, check Homebrew permissions under $prefix without broad ownership changes."
    return
  fi

  printf '%s\n' "If this was a permissions failure, check Homebrew prefix permissions without broad ownership changes."
}

terrapod_homebrew_core_cleanup_temps() {
  [ -z "${core_records_file:-}" ] || rm -f "$core_records_file"
  [ -z "${failed_formulae_file:-}" ] || rm -f "$failed_formulae_file"
  [ -z "${failed_casks_file:-}" ] || rm -f "$failed_casks_file"
  [ -z "${single_core_brewfile:-}" ] || rm -f "$single_core_brewfile"
}

terrapod_homebrew_core_failure_guidance_from_files() {
  detail=

  if [ -s "$failed_formulae_file" ]; then
    failed_formulae="$(terrapod_homebrew_core_join_lines_with_commas "$failed_formulae_file")" || return 1
    detail="failed formulae: $failed_formulae"
  fi

  if [ -s "$failed_casks_file" ]; then
    failed_casks="$(terrapod_homebrew_core_join_lines_with_commas "$failed_casks_file")" || return 1
    if [ -n "$detail" ]; then
      detail="$detail; failed casks: $failed_casks"
    else
      detail="failed casks: $failed_casks"
    fi
  fi

  permission_guidance="$(terrapod_homebrew_core_permission_guidance)"

  if [ -n "$detail" ]; then
    TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output for $detail. $permission_guidance Then rerun tpod apply."
  else
    TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output, fix package access, then rerun tpod apply. $permission_guidance"
  fi
}

terrapod_homebrew_core_run_bundle() {
  brewfile="$1"
  TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output, fix package access, then rerun tpod apply."

  if brew bundle --no-upgrade --file="$brewfile"; then
    return 0
  fi

  core_records_file=
  failed_formulae_file=
  failed_casks_file=
  single_core_brewfile=

  core_records_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-core-records.XXXXXX")" || return 1
  failed_formulae_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-core-failed-formulae.XXXXXX")" || {
    terrapod_homebrew_core_cleanup_temps
    return 1
  }
  failed_casks_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-core-failed-casks.XXXXXX")" || {
    terrapod_homebrew_core_cleanup_temps
    return 1
  }

  if ! terrapod_homebrew_core_item_records "$brewfile" >"$core_records_file"; then
    terrapod_homebrew_core_cleanup_temps
    return 1
  fi

  if [ ! -s "$core_records_file" ]; then
    terrapod_homebrew_core_failure_guidance_from_files || {
      terrapod_homebrew_core_cleanup_temps
      return 1
    }
    terrapod_homebrew_core_cleanup_temps
    return 1
  fi

  tab="$(printf '\t')"
  while IFS="$tab" read -r item_kind item_name; do
    single_core_brewfile="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-core-item.XXXXXX")" || {
      terrapod_homebrew_core_cleanup_temps
      return 1
    }

    case "$item_kind" in
      formula)
        printf 'brew "%s"\n' "$item_name" >"$single_core_brewfile" || {
          terrapod_homebrew_core_cleanup_temps
          return 1
        }
        if ! brew bundle --no-upgrade --file="$single_core_brewfile"; then
          printf '%s\n' "$item_name" >>"$failed_formulae_file" || {
            terrapod_homebrew_core_cleanup_temps
            return 1
          }
        fi
        ;;
      cask)
        printf 'cask "%s"\n' "$item_name" >"$single_core_brewfile" || {
          terrapod_homebrew_core_cleanup_temps
          return 1
        }
        if ! brew bundle --no-upgrade --file="$single_core_brewfile"; then
          printf '%s\n' "$item_name" >>"$failed_casks_file" || {
            terrapod_homebrew_core_cleanup_temps
            return 1
          }
        fi
        ;;
    esac

    rm -f "$single_core_brewfile"
    single_core_brewfile=
  done <"$core_records_file"

  terrapod_homebrew_core_failure_guidance_from_files || {
    terrapod_homebrew_core_cleanup_temps
    return 1
  }

  terrapod_homebrew_core_cleanup_temps
  return 1
}
```

- [x] **Step 2: Source the helper from the Homebrew bootstrap template**

In `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`, after the install-warning helper load block, add:

```sh
homebrew_core_bundle_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/homebrew-core-bundle.sh"
TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED=
if [ -f "$homebrew_core_bundle_lib" ]; then
  . "$homebrew_core_bundle_lib"
fi
```

- [x] **Step 3: Add an always-nonblocking exit helper for known marker-backed core bundle failures**

Add this function after `exit_after_install_warning`:

```sh
exit_after_nonblocking_install_warning() {
  if [ "$INSTALL_WARNING_RECORDED" -eq 1 ]; then
    exit 0
  fi

  exit 1
}
```

- [x] **Step 4: Replace direct core `brew bundle` handling with helper-backed marker writing**

Replace the existing core Brewfile block with:

```sh
if [ -f "$core_brewfile" ]; then
  if [ "${TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED:-}" = "1" ] &&
    terrapod_homebrew_core_run_bundle "$core_brewfile"; then
    clear_install_warning homebrew-core
  else
    if [ -z "${TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT:-}" ]; then
      TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output, fix package access, then rerun tpod apply."
    fi
    mark_install_warning \
      homebrew-core \
      "Homebrew core install needs attention" \
      "$TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT"
    exit_after_nonblocking_install_warning
  fi
fi
```

- [x] **Step 5: Run the focused rendered script tests and observe failures until Task 3 tests exist**

Run:

```bash
sh -n dot_local/lib/terrapod/homebrew-core-bundle.sh
sh tests/chezmoiignore_test.sh
```

Expected before test updates: syntax check passes; existing tests still pass.

---

### Task 2: Add Marker-Gated Core Retry Hook

**Files:**
- Create: `.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl`
- Modify: `.chezmoiignore`
- Modify: `tests/chezmoiignore_test.sh`

- [x] **Step 1: Create the retry template**

Create `.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl`:

```sh
{{- if eq .chezmoi.os "darwin" -}}
#!/bin/sh
set -eu

install_warnings_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warnings.sh"
TERRAPOD_INSTALL_WARNINGS_LOADED=
if [ -f "$install_warnings_lib" ]; then
  . "$install_warnings_lib"
fi

homebrew_core_bundle_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/homebrew-core-bundle.sh"
TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED=
if [ -f "$homebrew_core_bundle_lib" ]; then
  . "$homebrew_core_bundle_lib"
fi

if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" != "1" ] ||
  [ "${TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED:-}" != "1" ]; then
  exit 0
fi

mark_install_warning() {
  category="$1"
  summary="$2"
  guidance="$3"

  terrapod_install_warning_write "$category" "$summary" "$guidance"
}

clear_install_warning() {
  category="$1"

  terrapod_install_warning_clear "$category" || true
}

core_marker_path="$(terrapod_install_warning_path homebrew-core 2>/dev/null || true)"
if [ -z "$core_marker_path" ] || [ ! -f "$core_marker_path" ]; then
  exit 0
fi

find_brew() {
  if command -v brew >/dev/null 2>&1; then
    command -v brew
    return
  fi

  if [ -x /opt/homebrew/bin/brew ]; then
    printf '%s\n' /opt/homebrew/bin/brew
    return
  fi

  if [ -x /usr/local/bin/brew ]; then
    printf '%s\n' /usr/local/bin/brew
    return
  fi

  return 1
}

brew_bin="$(find_brew || true)"
if [ -z "$brew_bin" ]; then
  mark_install_warning \
    homebrew-core \
    "Homebrew core install needs attention" \
    "Install Homebrew from https://brew.sh, then rerun tpod apply." ||
    exit 1
  exit 0
fi

if ! brew_shellenv="$("$brew_bin" shellenv)"; then
  mark_install_warning \
    homebrew-core \
    "Homebrew core install needs attention" \
    "Fix Homebrew shellenv, then rerun tpod apply." ||
    exit 1
  exit 0
fi
eval "$brew_shellenv"
brew analytics off >/dev/null 2>&1 || true

core_brewfile="{{ .chezmoi.sourceDir }}/Brewfile"
if [ ! -f "$core_brewfile" ]; then
  clear_install_warning homebrew-core
  exit 0
fi

if terrapod_homebrew_core_run_bundle "$core_brewfile"; then
  clear_install_warning homebrew-core
else
  if [ -z "${TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT:-}" ]; then
    TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output, fix package access, then rerun tpod apply."
  fi
  mark_install_warning \
    homebrew-core \
    "Homebrew core install needs attention" \
    "$TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT" ||
    exit 1
fi
{{- end }}
```

- [x] **Step 2: Add retry script to macOS-only managed-entry assertions**

In `.chezmoiignore`, add the rendered target path inside the `{{ if ne .chezmoi.os "darwin" }}` block:

```text
.chezmoiscripts/01-retry-homebrew-core.sh
```

In `tests/chezmoiignore_test.sh`, add this line to `macos_only_entries`:

```sh
.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl
```

- [x] **Step 3: Render and syntax-check the retry script in tests**

Near existing `macos_desktop_retry` rendering, add:

```sh
macos_core_retry="$(render_template "$macos_data" ".chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl")"
```

After the bootstrap script syntax check, add:

```sh
macos_core_retry_script="$tmp_dir/macos-core-retry.sh"
printf '%s\n' "$macos_core_retry" >"$macos_core_retry_script"
sh -n "$macos_core_retry_script" || fail "macOS core retry script should be valid sh"
pass "macOS core retry script is valid sh"
```

- [x] **Step 4: Run the rendered-script test and verify the new template renders**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected before Task 3 behavior tests: PASS.

---

### Task 3: Add Rendered Homebrew Core Warning Tests

**Files:**
- Modify: `tests/chezmoiignore_test.sh`

- [x] **Step 1: Extend `write_brew_bundle_stub` for core failures, output, and prefix**

In the `write_brew_bundle_stub` helper, add support for:

```sh
'  --prefix) printf "%s\n" "${MACOS_BREW_PREFIX:-/opt/homebrew}"; exit 0 ;;' \
```

inside the `case "$1" in` block, and inside the `bundle)` branch before cask handling:

```sh
'    if [ "${MACOS_BREW_ECHO_OUTPUT:-}" = "1" ]; then' \
'      printf "%s\n" "visible brew bundle output: $*"' \
'    fi' \
'    for formula in ${MACOS_BREW_FAIL_FORMULAE:-}; do' \
'      if [ -n "$bundle_file" ] && grep -Fx "brew \"$formula\"" "$bundle_file" >/dev/null 2>&1; then' \
'        exit 42' \
'      fi' \
'    done' \
'    if [ "${MACOS_BREW_FAIL_CORE_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "brew \"mise\"" "$bundle_file" >/dev/null 2>&1; then' \
'      exit 42' \
'    fi' \
```

- [x] **Step 2: Add stale core marker clear test for successful core bundle**

After the Homebrew installer failure tests and before desktop App Group tests, add:

```sh
core_success_bin="$tmp_dir/core-success-bin"
core_success_state="$tmp_dir/core-success-state"
core_success_home="$tmp_dir/core-success-home"
core_success_log="$tmp_dir/core-success-brew.log"
mkdir -p "$core_success_bin" "$core_success_home"
write_brew_bundle_stub "$core_success_bin/brew"

HOME="$core_success_home" XDG_STATE_HOME="$core_success_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "stale core warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$core_success_home" XDG_STATE_HOME="$core_success_state" MACOS_BREW_LOG="$core_success_log" PATH="$core_success_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-success.out" 2>"$tmp_dir/core-success.err"; then
  fail "successful core Homebrew bundle succeeds"
fi

if [ -e "$core_success_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "successful core Homebrew bundle clears stale homebrew-core marker"
fi
pass "successful core Homebrew bundle clears stale homebrew-core marker"
```

- [x] **Step 3: Add reliable formula/cask detail and visible-output preservation test**

Add:

```sh
core_detail_bin="$tmp_dir/core-detail-bin"
core_detail_state="$tmp_dir/core-detail-state"
core_detail_home="$tmp_dir/core-detail-home"
core_detail_log="$tmp_dir/core-detail-brew.log"
core_detail_prefix="$tmp_dir/core-detail-prefix"
mkdir -p "$core_detail_bin" "$core_detail_home" "$core_detail_prefix"
chmod 555 "$core_detail_prefix"
write_brew_bundle_stub "$core_detail_bin/brew"

if ! HOME="$core_detail_home" XDG_STATE_HOME="$core_detail_state" MACOS_BREW_LOG="$core_detail_log" MACOS_BREW_PREFIX="$core_detail_prefix" MACOS_BREW_ECHO_OUTPUT=1 MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_FORMULAE="gum" MACOS_BREW_FAIL_CASKS="font-d2coding" PATH="$core_detail_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-detail.out" 2>"$tmp_dir/core-detail.err"; then
  fail "core Homebrew bundle failure records a marker and does not block bootstrap script"
fi
chmod 755 "$core_detail_prefix"

assert_contains_text "$(cat "$tmp_dir/core-detail.out")" "visible brew bundle output:" "core Homebrew failure preserves visible brew output"
core_detail_marker="$core_detail_state/terrapod/install-warnings/homebrew-core"
if [ ! -f "$core_detail_marker" ]; then
  fail "core Homebrew bundle failure records a homebrew-core marker"
fi
pass "core Homebrew bundle failure records a homebrew-core marker"

core_detail_marker_text="$(cat "$core_detail_marker")"
assert_contains_text "$core_detail_marker_text" "category='homebrew-core'" "core marker keeps one stable category"
assert_contains_text "$core_detail_marker_text" "summary='Homebrew core install needs attention'" "core marker keeps stable summary"
assert_contains_text "$core_detail_marker_text" "failed formulae: gum" "core marker guidance includes reliable failed formula names"
assert_contains_text "$core_detail_marker_text" "failed casks: font-d2coding" "core marker guidance includes reliable failed cask names"
assert_contains_text "$core_detail_marker_text" "Homebrew prefix is not writable: $core_detail_prefix" "core marker guidance identifies unwritable shared prefix"
assert_not_contains_text "$core_detail_marker_text" "btop" "core marker excludes successful formula names"
assert_not_contains_text "$core_detail_marker_text" "chown" "core marker avoids broad ownership command guidance"
```

- [x] **Step 4: Add bulk-only fallback test**

Add:

```sh
core_fallback_bin="$tmp_dir/core-fallback-bin"
core_fallback_state="$tmp_dir/core-fallback-state"
core_fallback_home="$tmp_dir/core-fallback-home"
core_fallback_log="$tmp_dir/core-fallback-brew.log"
mkdir -p "$core_fallback_bin" "$core_fallback_home"
write_brew_bundle_stub "$core_fallback_bin/brew"

if ! HOME="$core_fallback_home" XDG_STATE_HOME="$core_fallback_state" MACOS_BREW_LOG="$core_fallback_log" MACOS_BREW_FAIL_CORE_BULK=1 PATH="$core_fallback_bin:/usr/bin:/bin" \
  sh "$macos_bootstrap_script" >"$tmp_dir/core-fallback.out" 2>"$tmp_dir/core-fallback.err"; then
  fail "core Homebrew bulk-only failure records a marker and does not block bootstrap script"
fi

core_fallback_marker_text="$(cat "$core_fallback_state/terrapod/install-warnings/homebrew-core")"
assert_contains_text "$core_fallback_marker_text" "Review Homebrew core bundle output, fix package access, then rerun tpod apply." "core marker falls back to visible-output rerun guidance"
assert_not_contains_text "$core_fallback_marker_text" "failed formulae:" "core fallback marker avoids invented formula detail"
assert_not_contains_text "$core_fallback_marker_text" "failed casks:" "core fallback marker avoids invented cask detail"
```

- [x] **Step 5: Add retry clear and failed retry replace tests**

Add:

```sh
core_retry_success_bin="$tmp_dir/core-retry-success-bin"
core_retry_success_state="$tmp_dir/core-retry-success-state"
core_retry_success_home="$tmp_dir/core-retry-success-home"
core_retry_success_log="$tmp_dir/core-retry-success-brew.log"
mkdir -p "$core_retry_success_bin" "$core_retry_success_home"
write_brew_bundle_stub "$core_retry_success_bin/brew"
HOME="$core_retry_success_home" XDG_STATE_HOME="$core_retry_success_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "stale core retry warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$core_retry_success_home" XDG_STATE_HOME="$core_retry_success_state" MACOS_BREW_LOG="$core_retry_success_log" PATH="$core_retry_success_bin:/usr/bin:/bin" \
  sh "$macos_core_retry_script" >"$tmp_dir/core-retry-success.out" 2>"$tmp_dir/core-retry-success.err"; then
  fail "successful core retry succeeds"
fi

if [ -e "$core_retry_success_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "successful core retry clears homebrew-core marker"
fi
pass "successful core retry clears homebrew-core marker"

core_retry_failure_bin="$tmp_dir/core-retry-failure-bin"
core_retry_failure_state="$tmp_dir/core-retry-failure-state"
core_retry_failure_home="$tmp_dir/core-retry-failure-home"
core_retry_failure_log="$tmp_dir/core-retry-failure-brew.log"
mkdir -p "$core_retry_failure_bin" "$core_retry_failure_home"
write_brew_bundle_stub "$core_retry_failure_bin/brew"
HOME="$core_retry_failure_home" XDG_STATE_HOME="$core_retry_failure_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "old core retry warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$core_retry_failure_home" XDG_STATE_HOME="$core_retry_failure_state" MACOS_BREW_LOG="$core_retry_failure_log" MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_FORMULAE="mise" PATH="$core_retry_failure_bin:/usr/bin:/bin" \
  sh "$macos_core_retry_script" >"$tmp_dir/core-retry-failure.out" 2>"$tmp_dir/core-retry-failure.err"; then
  fail "failed core retry records a replacement marker and exits successfully"
fi

core_retry_failure_marker_text="$(cat "$core_retry_failure_state/terrapod/install-warnings/homebrew-core")"
assert_contains_text "$core_retry_failure_marker_text" "failed formulae: mise" "failed core retry replaces marker with current failed formula detail"
assert_not_contains_text "$core_retry_failure_marker_text" "old core retry warning" "failed core retry replaces stale marker guidance"
assert_contains_text "$core_retry_failure_marker_text" "updated_at='" "failed core retry replacement marker keeps updated_at"
```

- [x] **Step 6: Run rendered-script tests and verify they pass**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: PASS.

---

### Task 4: Add Real `tpod apply` Core Marker Regression Tests

**Files:**
- Modify: `tests/terrapod_command_test.sh`

- [x] **Step 1: Extend test Homebrew stub with core behavior**

In `write_brew_bundle_stub`, add the same `--prefix`, `MACOS_BREW_ECHO_OUTPUT`, `MACOS_BREW_FAIL_FORMULAE`, and `MACOS_BREW_FAIL_CORE_BULK` handling from Task 3.

- [x] **Step 2: Include the new retry script and helper in the minimal apply fixture**

In `copy_desktop_apply_source_fixture`, add:

```sh
  cp "$repo_root/.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl" "$source_dir/.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl"
  cp "$repo_root/dot_local/lib/terrapod/homebrew-core-bundle.sh" "$source_dir/dot_local/lib/terrapod/homebrew-core-bundle.sh"
```

- [x] **Step 3: Add core apply failure, clear, and replace tests**

After the desktop apply marker tests, add:

```sh
core_apply_source="$tmp_dir/core-apply-source"
core_apply_home="$tmp_dir/core-apply-home"
core_apply_state="$tmp_dir/core-apply-state"
core_apply_bin="$tmp_dir/core-apply-bin"
core_apply_log="$tmp_dir/core-apply-brew.log"
core_apply_config="$tmp_dir/core-apply.toml"
core_apply_prefix="$tmp_dir/core-apply-prefix"
mkdir -p "$core_apply_home" "$core_apply_bin" "$core_apply_prefix"
chmod 555 "$core_apply_prefix"
copy_desktop_apply_source_fixture "$core_apply_source"
ln -s "$real_chezmoi" "$core_apply_bin/chezmoi"
write_brew_bundle_stub "$core_apply_bin/brew"
write_desktop_apply_config "$core_apply_config" "$core_apply_source" "$core_apply_home" false false

if ! HOME="$core_apply_home" XDG_STATE_HOME="$core_apply_state" TERRAPOD_CHEZMOI_CONFIG="$core_apply_config" MACOS_BREW_LOG="$core_apply_log" MACOS_BREW_PREFIX="$core_apply_prefix" MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_FORMULAE="gum" PATH="$core_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/core-apply-first.out" 2>"$tmp_dir/core-apply-first.err"; then
  printf '%s\n' "core apply first stdout:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-first.out" >&2
  printf '%s\n' "core apply first stderr:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-first.err" >&2
  fail "Terrapod apply succeeds when core Homebrew bundle fails with a marker"
fi
chmod 755 "$core_apply_prefix"

core_apply_marker="$core_apply_state/terrapod/install-warnings/homebrew-core"
core_apply_marker_text="$(cat "$core_apply_marker")"
assert_contains "$core_apply_marker_text" "failed formulae: gum" "Terrapod apply records failed core formula detail"
assert_contains "$core_apply_marker_text" "Homebrew prefix is not writable: $core_apply_prefix" "Terrapod apply records shared-prefix permission guidance"
assert_not_contains "$core_apply_marker_text" "chown" "Terrapod apply core guidance avoids broad ownership command guidance"

if ! HOME="$core_apply_home" XDG_STATE_HOME="$core_apply_state" TERRAPOD_CHEZMOI_CONFIG="$core_apply_config" MACOS_BREW_LOG="$core_apply_log" PATH="$core_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/core-apply-retry-success.out" 2>"$tmp_dir/core-apply-retry-success.err"; then
  printf '%s\n' "core apply retry success stdout:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-retry-success.out" >&2
  printf '%s\n' "core apply retry success stderr:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-retry-success.err" >&2
  fail "Terrapod apply retries core Homebrew marker and succeeds"
fi

if [ -e "$core_apply_marker" ]; then
  fail "Terrapod apply clears a core Homebrew marker after retry succeeds"
fi
pass "Terrapod apply clears a core Homebrew marker after retry succeeds"

HOME="$core_apply_home" XDG_STATE_HOME="$core_apply_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "old apply core warning."' \
  sh "$install_warnings_lib"

if ! HOME="$core_apply_home" XDG_STATE_HOME="$core_apply_state" TERRAPOD_CHEZMOI_CONFIG="$core_apply_config" MACOS_BREW_LOG="$core_apply_log" MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_CASKS="font-d2coding" PATH="$core_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/core-apply-retry-failure.out" 2>"$tmp_dir/core-apply-retry-failure.err"; then
  printf '%s\n' "core apply retry failure stdout:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-retry-failure.out" >&2
  printf '%s\n' "core apply retry failure stderr:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-retry-failure.err" >&2
  fail "Terrapod apply replaces a core Homebrew marker after retry fails"
fi

core_apply_marker_text="$(cat "$core_apply_marker")"
assert_contains "$core_apply_marker_text" "failed casks: font-d2coding" "Terrapod apply replaces core marker with current failed cask detail"
assert_not_contains "$core_apply_marker_text" "old apply core warning" "Terrapod apply removes stale core marker guidance after failed retry"
```

- [x] **Step 4: Run focused command tests**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: PASS.

---

### Task 5: Full Verification And Commit

**Files:**
- Verify all modified files.

- [x] **Step 1: Run syntax checks**

Run:

```bash
sh -n dot_local/lib/terrapod/homebrew-core-bundle.sh
tmp_dir="$(mktemp -d)"
chezmoi --config "$tmp_dir/chezmoi.toml" --source "$PWD" execute-template --override-data '{"chezmoi":{"os":"darwin"}}' --file "$PWD/.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl" >"$tmp_dir/core-retry.sh"
chezmoi --config "$tmp_dir/chezmoi.toml" --source "$PWD" execute-template --override-data '{"chezmoi":{"os":"darwin"}}' --file "$PWD/.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl" >"$tmp_dir/bootstrap-homebrew.sh"
sh -n "$tmp_dir/core-retry.sh"
sh -n "$tmp_dir/bootstrap-homebrew.sh"
rm -rf "$tmp_dir"
```

Expected: PASS.

- [x] **Step 2: Run focused tests**

Run:

```bash
sh tests/chezmoiignore_test.sh
sh tests/terrapod_command_test.sh
```

Expected: PASS.

- [x] **Step 3: Run the full shell test suite**

Run:

```bash
for t in tests/*.sh; do sh "$t"; done
zsh tests/zshrc_zoxide_test.zsh
```

Expected: PASS.

- [x] **Step 4: Review diff for forbidden guidance**

Run:

```bash
rg -n "chown|chmod -R|sudo chown|ownership repair|Homebrew prefix" .chezmoiscripts dot_local/lib tests
```

Expected: no automatic repair commands; `chown` appears only in tests asserting it is absent.

- [x] **Step 5: Commit**

Run:

```bash
git add .chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl .chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl dot_local/lib/terrapod/homebrew-core-bundle.sh tests/chezmoiignore_test.sh tests/terrapod_command_test.sh docs/superpowers/plans/2026-06-07-homebrew-core-warnings.md
git commit -m "Make Homebrew core failures warning-backed"
```

Expected: commit succeeds.

---

## Self-Review

- Spec coverage: The tasks cover marker creation, non-blocking core bundle exit, successful clear, failed replace with `updated_at`, reliable formula/cask detail, fallback detail, cautious shared-prefix guidance, and tests for rendered scripts plus real `tpod apply`.
- Placeholder scan: No placeholder markers, unresolved follow-up notes, or undefined commands remain in this plan.
- Type/signature consistency: The helper exposes `terrapod_homebrew_core_run_bundle` and `TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT`; both bootstrap and retry scripts use those exact names.
