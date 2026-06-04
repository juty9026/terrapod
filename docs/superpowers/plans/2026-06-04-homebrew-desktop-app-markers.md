# Homebrew Desktop App Markers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** macOS Desktop App Stack Homebrew cask failures should write one App Group-aware `homebrew-desktop-apps` warning marker, avoid blocking `tpod` availability, and keep marker content aligned with currently enabled macOS App Groups.

**Architecture:** Keep the existing category-based marker contract. Run the rendered desktop Brewfile once with bulk `brew bundle --no-upgrade` to detect the actual category failure. If bulk fails, parse Terrapod's rendered `Brewfile.macos-desktop-apps.tmpl` only to identify the desired cask-to-App Group mapping, then run each parsed cask through its own single-cask `brew bundle --no-upgrade` invocation so failed cask names are based on command exit status rather than Homebrew output parsing. If single-cask attribution finds no failed casks, fall back to generic bulk desktop bundle guidance; desktop stack failures write the marker and exit successfully so the optional desktop stack does not block the command surface.

**Tech Stack:** POSIX `sh`, chezmoi templates, Homebrew `brew bundle`, shell tests in `tests/chezmoiignore_test.sh` and `tests/terrapod_command_test.sh`.

---

## Assumptions

- The reliable source for desired cask-to-App Group mapping is the rendered desktop Brewfile, not Homebrew output.
- A cask is recorded as failed only when its own single-cask `brew bundle --no-upgrade` invocation fails.
- If the bulk desktop bundle fails but no single-cask failure can be attributed reliably, Terrapod writes generic desktop bundle guidance.
- The `homebrew-desktop-apps` marker remains one category. Cask and App Group detail belongs in `guidance`, preserving the existing marker schema.
- Core Homebrew bundle failures remain blocking. Only the optional macOS Desktop App Stack failure becomes non-blocking.

## File Structure

- Modify `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`
  - Add a small parser for the rendered desktop Brewfile.
  - Use the parser to write App Group-aware marker guidance.
  - Return success after writing a desktop app marker so `tpod` availability is not blocked by optional desktop casks.
- Modify `tests/chezmoiignore_test.sh`
  - Add rendered-script behavior tests for cask/group detail, fallback detail, stale disabled-group removal, enabled-group retention, and successful rerun clear.
- Modify `tests/terrapod_command_test.sh`
  - Add `tpod doctor` assertions for a `homebrew-desktop-apps` marker that includes cask and App Group detail.

---

### Task 1: Add failing desktop bundle marker behavior tests

**Files:**
- Modify: `tests/chezmoiignore_test.sh`

- [x] **Step 1: Add test data for combined App Groups**

Add this data variable near the existing `macos_*_apps_data` variables:

```sh
macos_terminal_launcher_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":true,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":true,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupAiApps":false}'
```

- [x] **Step 2: Render the combined bootstrap script**

Add this near the existing rendered bootstrap variables:

```sh
macos_terminal_launcher_apps_bootstrap="$(render_template "$macos_terminal_launcher_apps_data" ".chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl")"
```

- [x] **Step 3: Add a reusable brew stub script fixture**

After the existing `macos_brew_bin` success stub test, add a helper in the test file:

```sh
write_brew_bundle_stub() {
  path="$1"

  write_stub "$path" \
    'printf "%s\n" "brew args:$*" >>"$MACOS_BREW_LOG"' \
    'bundle_file=' \
    'for arg do' \
    '  case "$arg" in' \
    '    --file=*) bundle_file="${arg#--file=}" ;;' \
    '  esac' \
    'done' \
    'case "$1" in' \
    '  shellenv) printf "%s\n" ":" ;;' \
    '  analytics) exit 0 ;;' \
    '  bundle)' \
    '    for cask in ${MACOS_BREW_FAIL_CASKS:-}; do' \
    '      if [ -n "$bundle_file" ] && grep -Fx "cask \"$cask\"" "$bundle_file" >/dev/null 2>&1; then' \
    '        exit 42' \
    '      fi' \
    '    done' \
    '    if [ "${MACOS_BREW_FAIL_DESKTOP_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "# Rendered opt-in macOS Desktop App Stack." "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    if [ "${MACOS_BREW_FAIL_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "tap \"homebrew/cask\"" "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    exit 0' \
    '    ;;' \
    '  *) exit 64 ;;' \
    'esac'
}
```

- [x] **Step 4: Add a failure test for cask and App Group detail**

Add this test after the existing desktop Brewfile rendering assertions:

```sh
terminal_launcher_bootstrap_script="$tmp_dir/macos-terminal-launcher-bootstrap.sh"
printf '%s\n' "$macos_terminal_launcher_apps_bootstrap" >"$terminal_launcher_bootstrap_script"
sh -n "$terminal_launcher_bootstrap_script" || fail "terminal and launcher bootstrap script should be valid sh"
pass "terminal and launcher bootstrap script is valid sh"

terminal_launcher_bin="$tmp_dir/terminal-launcher-bin"
terminal_launcher_state="$tmp_dir/terminal-launcher-state"
terminal_launcher_home="$tmp_dir/terminal-launcher-home"
terminal_launcher_log="$tmp_dir/terminal-launcher-brew.log"
mkdir -p "$terminal_launcher_bin" "$terminal_launcher_home"
write_brew_bundle_stub "$terminal_launcher_bin/brew"

if ! HOME="$terminal_launcher_home" XDG_STATE_HOME="$terminal_launcher_state" MACOS_BREW_LOG="$terminal_launcher_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty raycast" PATH="$terminal_launcher_bin:/usr/bin:/bin" \
  sh "$terminal_launcher_bootstrap_script" >"$tmp_dir/terminal-launcher.out" 2>"$tmp_dir/terminal-launcher.err"; then
  fail "macOS desktop app bundle failure does not block bootstrap script"
fi

terminal_launcher_marker="$terminal_launcher_state/terrapod/install-warnings/homebrew-desktop-apps"
if [ ! -f "$terminal_launcher_marker" ]; then
  fail "macOS desktop app bundle failure records a homebrew-desktop-apps marker"
fi
pass "macOS desktop app bundle failure records a homebrew-desktop-apps marker"

terminal_launcher_marker_text="$(cat "$terminal_launcher_marker")"
assert_contains_text "$terminal_launcher_marker_text" "category='homebrew-desktop-apps'" "desktop app marker keeps one stable category"
assert_contains_text "$terminal_launcher_marker_text" "summary='Homebrew desktop app install needs attention'" "desktop app marker keeps stable summary"
assert_contains_text "$terminal_launcher_marker_text" "failed casks: ghostty, raycast" "desktop app marker guidance includes only casks whose single-cask bundle failed"
assert_contains_text "$terminal_launcher_marker_text" "App Groups: terminal-apps, launcher" "desktop app marker guidance includes enabled App Groups"
assert_not_contains_text "$terminal_launcher_marker_text" "1password-cli" "desktop app marker excludes casks whose single-cask bundle succeeded"
```

- [x] **Step 5: Add a fallback-detail test**

Add this test after the detail test:

```sh
fallback_bootstrap_script="$tmp_dir/macos-desktop-fallback-bootstrap.sh"
awk '
  $0 == "BREWFILE" && in_brewfile == 1 {
    print "# rendered desktop stack without reliable cask detail"
    print "tap \"homebrew/cask\""
    print "BREWFILE"
    in_brewfile = 0
    next
  }
  in_brewfile == 1 { next }
  $0 == "cat >\"$desktop_brewfile\" <<'\''BREWFILE'\''" {
    print
    in_brewfile = 1
    next
  }
  { print }
' "$terminal_launcher_bootstrap_script" >"$fallback_bootstrap_script"
sh -n "$fallback_bootstrap_script" || fail "fallback desktop bootstrap script should be valid sh"
pass "fallback desktop bootstrap script is valid sh"

fallback_bin="$tmp_dir/fallback-bin"
fallback_state="$tmp_dir/fallback-state"
fallback_home="$tmp_dir/fallback-home"
fallback_log="$tmp_dir/fallback-brew.log"
mkdir -p "$fallback_bin" "$fallback_home"
write_brew_bundle_stub "$fallback_bin/brew"

if ! HOME="$fallback_home" XDG_STATE_HOME="$fallback_state" MACOS_BREW_LOG="$fallback_log" MACOS_BREW_FAIL_BULK=1 PATH="$fallback_bin:/usr/bin:/bin" \
  sh "$fallback_bootstrap_script" >"$tmp_dir/fallback.out" 2>"$tmp_dir/fallback.err"; then
  fail "macOS desktop app fallback bundle failure does not block bootstrap script"
fi

fallback_marker_text="$(cat "$fallback_state/terrapod/install-warnings/homebrew-desktop-apps")"
assert_contains_text "$fallback_marker_text" "Review Homebrew desktop app bundle output" "desktop app marker falls back to bulk bundle guidance when casks are not reliable"
assert_not_contains_text "$fallback_marker_text" "failed casks:" "desktop app fallback marker avoids invented cask detail"
assert_not_contains_text "$fallback_marker_text" "App Groups:" "desktop app fallback marker avoids invented App Group detail"
```

- [x] **Step 6: Add stale disabled-group removal and enabled retention test**

Add this test after the fallback test:

```sh
terminal_only_bootstrap_script="$tmp_dir/macos-terminal-only-bootstrap.sh"
printf '%s\n' "$macos_terminal_apps_bootstrap" >"$terminal_only_bootstrap_script"
sh -n "$terminal_only_bootstrap_script" || fail "terminal-only bootstrap script should be valid sh"
pass "terminal-only bootstrap script is valid sh"

terminal_only_bin="$tmp_dir/terminal-only-bin"
terminal_only_state="$tmp_dir/terminal-only-state"
terminal_only_home="$tmp_dir/terminal-only-home"
terminal_only_log="$tmp_dir/terminal-only-brew.log"
mkdir -p "$terminal_only_bin" "$terminal_only_home"
write_brew_bundle_stub "$terminal_only_bin/brew"

HOME="$terminal_only_home" XDG_STATE_HOME="$terminal_only_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-desktop-apps "Homebrew desktop app install needs attention" "Review Homebrew cask output for failed casks: ghostty, raycast, 1password-cli; App Groups: terminal-apps, launcher, then rerun tpod apply."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$terminal_only_home" XDG_STATE_HOME="$terminal_only_state" MACOS_BREW_LOG="$terminal_only_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty" PATH="$terminal_only_bin:/usr/bin:/bin" \
  sh "$terminal_only_bootstrap_script" >"$tmp_dir/terminal-only.out" 2>"$tmp_dir/terminal-only.err"; then
  fail "terminal-only desktop app bundle failure does not block bootstrap script"
fi

terminal_only_marker_text="$(cat "$terminal_only_state/terrapod/install-warnings/homebrew-desktop-apps")"
assert_contains_text "$terminal_only_marker_text" "failed casks: ghostty" "enabled terminal-apps failure remains in desktop app marker"
assert_contains_text "$terminal_only_marker_text" "App Groups: terminal-apps" "enabled terminal-apps group remains in desktop app marker"
assert_not_contains_text "$terminal_only_marker_text" "raycast" "disabled launcher cask is removed from desktop app marker"
assert_not_contains_text "$terminal_only_marker_text" "1password-cli" "disabled launcher CLI cask is removed from desktop app marker"
assert_not_contains_text "$terminal_only_marker_text" "launcher" "disabled launcher group is removed from desktop app marker"
```

- [x] **Step 7: Add successful rerun clear test**

Add this test after the stale disabled-group test:

```sh
terminal_success_bin="$tmp_dir/terminal-success-bin"
terminal_success_state="$tmp_dir/terminal-success-state"
terminal_success_home="$tmp_dir/terminal-success-home"
terminal_success_log="$tmp_dir/terminal-success-brew.log"
mkdir -p "$terminal_success_bin" "$terminal_success_home"
write_brew_bundle_stub "$terminal_success_bin/brew"

HOME="$terminal_success_home" XDG_STATE_HOME="$terminal_success_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-desktop-apps "Homebrew desktop app install needs attention" "Review Homebrew cask output for failed casks: ghostty; App Groups: terminal-apps, then rerun tpod apply."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

if ! HOME="$terminal_success_home" XDG_STATE_HOME="$terminal_success_state" MACOS_BREW_LOG="$terminal_success_log" PATH="$terminal_success_bin:/usr/bin:/bin" \
  sh "$terminal_only_bootstrap_script" >"$tmp_dir/terminal-success.out" 2>"$tmp_dir/terminal-success.err"; then
  fail "successful terminal-only desktop app rerun succeeds"
fi

if [ -e "$terminal_success_state/terrapod/install-warnings/homebrew-desktop-apps" ]; then
  fail "successful desktop app rerun clears homebrew-desktop-apps marker"
fi
pass "successful desktop app rerun clears homebrew-desktop-apps marker"
```

- [x] **Step 8: Run the new tests and verify failure**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: FAIL before implementation because the current desktop failure exits non-zero and marker guidance lacks cask/App Group detail.

- [x] **Step 9: Commit the failing tests**

Do not commit yet if this repo prefers one final commit; otherwise:

```bash
git add tests/chezmoiignore_test.sh
git commit -m "test: cover desktop app warning marker detail"
```

---

### Task 2: Implement App Group-aware desktop marker writing

**Files:**
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`

- [x] **Step 1: Add desktop cask mapping and helper functions**

Add this helper after `clear_install_warning()`:

```sh
join_lines_with_commas() {
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

desktop_app_cask_records() {
  brewfile="$1"

  awk '
    /^[[:space:]]*#[[:space:]]+.*[[:space:]]macOS App Group[[:space:]]*$/ {
      group = $0
      sub(/^[[:space:]]*#[[:space:]]+/, "", group)
      sub(/[[:space:]]+macOS App Group[[:space:]]*$/, "", group)
      next
    }

    /^[[:space:]]*cask[[:space:]]+"/ {
      cask = $0
      sub(/^[[:space:]]*cask[[:space:]]+"/, "", cask)
      sub(/".*$/, "", cask)
      if (cask != "") {
        printf "%s\t%s\n", group, cask
      }
    }
  ' "$brewfile"
}
```

- [x] **Step 2: Add a desktop bundle runner that records only actual failed casks**

Add this helper after `desktop_app_cask_records()`:

```sh
desktop_app_failure_guidance_text=

run_desktop_app_bundle() {
  brewfile="$1"
  desktop_app_failure_guidance_text="Review Homebrew desktop app bundle output, fix app installation access, then rerun tpod apply."

  if brew bundle --no-upgrade --file="$brewfile"; then
    return 0
  fi

  records_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-macos-desktop-records.XXXXXX")" || return 1
  failed_casks_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-macos-desktop-failed-casks.XXXXXX")" || {
    rm -f "$records_file"
    return 1
  }
  failed_groups_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-macos-desktop-failed-groups.XXXXXX")" || {
    rm -f "$records_file" "$failed_casks_file"
    return 1
  }

  desktop_app_cask_records "$brewfile" >"$records_file"

  if [ ! -s "$records_file" ]; then
    rm -f "$records_file" "$failed_casks_file" "$failed_groups_file"
    return 1
  fi

  tab="$(printf '\t')"
  while IFS="$tab" read -r app_group cask; do
    single_cask_brewfile="$(mktemp "${TMPDIR:-/tmp}/terrapod-macos-desktop-cask.XXXXXX")" || {
      rm -f "$records_file" "$failed_casks_file" "$failed_groups_file"
      return 1
    }
    printf 'cask "%s"\n' "$cask" >"$single_cask_brewfile"

    if ! brew bundle --no-upgrade --file="$single_cask_brewfile"; then
      printf '%s\n' "$cask" >>"$failed_casks_file"
      if [ -n "$app_group" ] && ! grep -Fx "$app_group" "$failed_groups_file" >/dev/null 2>&1; then
        printf '%s\n' "$app_group" >>"$failed_groups_file"
      fi
    fi

    rm -f "$single_cask_brewfile"
  done <"$records_file"

  rm -f "$records_file"

  if [ -s "$failed_casks_file" ]; then
    failed_casks="$(join_lines_with_commas "$failed_casks_file")"
    failed_groups="$(join_lines_with_commas "$failed_groups_file")"
    detail="failed casks: $failed_casks"
    if [ -n "$failed_groups" ]; then
      detail="$detail; App Groups: $failed_groups"
    fi
    desktop_app_failure_guidance_text="Review Homebrew cask output for $detail, fix app installation access, then rerun tpod apply."
    rm -f "$failed_casks_file" "$failed_groups_file"
    return 1
  fi

  rm -f "$failed_casks_file" "$failed_groups_file"
  return 1
}
```

- [x] **Step 3: Use the helper in the desktop bundle failure path**

Replace the existing desktop bundle `if brew bundle ...` block:

```sh
  if brew bundle --no-upgrade --file="$desktop_brewfile"; then
    clear_install_warning homebrew-desktop-apps
  else
    mark_install_warning \
      homebrew-desktop-apps \
      "Homebrew desktop app install needs attention" \
      "Review Homebrew cask output, fix app installation access, then rerun tpod apply."
    exit 1
  fi
```

with:

```sh
  if run_desktop_app_bundle "$desktop_brewfile"; then
    clear_install_warning homebrew-desktop-apps
  else
    mark_install_warning \
      homebrew-desktop-apps \
      "Homebrew desktop app install needs attention" \
      "$desktop_app_failure_guidance_text"
    exit 0
  fi
```

- [x] **Step 4: Run the targeted rendered script tests**

Run:

```bash
sh tests/chezmoiignore_test.sh
```

Expected: PASS.

- [x] **Step 5: Commit implementation with tests if using frequent commits**

```bash
git add .chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl tests/chezmoiignore_test.sh
git commit -m "fix: make desktop app warnings app group aware"
```

---

### Task 3: Add tpod apply recalculation coverage for desktop marker detail

**Files:**
- Modify: `tests/terrapod_command_test.sh`

- [x] **Step 1: Add a brew bundle stub helper to command tests**

Add this helper near the other command-test stubs:

```sh
write_brew_bundle_stub() {
  path="$1"

  write_stub "$path" \
    'printf "%s\n" "brew args:$*" >>"$MACOS_BREW_LOG"' \
    'bundle_file=' \
    'for arg do' \
    '  case "$arg" in' \
    '    --file=*) bundle_file="${arg#--file=}" ;;' \
    '  esac' \
    'done' \
    'case "$1" in' \
    '  shellenv) printf "%s\n" ":" ;;' \
    '  analytics) exit 0 ;;' \
    '  bundle)' \
    '    for cask in ${MACOS_BREW_FAIL_CASKS:-}; do' \
    '      if [ -n "$bundle_file" ] && grep -Fx "cask \"$cask\"" "$bundle_file" >/dev/null 2>&1; then' \
    '        exit 42' \
    '      fi' \
    '    done' \
    '    if [ "${MACOS_BREW_FAIL_DESKTOP_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "# Rendered opt-in macOS Desktop App Stack." "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    if [ "${MACOS_BREW_FAIL_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "tap \"homebrew/cask\"" "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    exit 0' \
    '    ;;' \
    '  *) exit 64 ;;' \
    'esac'
}
```

- [x] **Step 2: Add a real-chezmoi desktop apply fixture**

Add this helper near the apply tests:

```sh
copy_desktop_apply_source_fixture() {
  source_dir="$1"

  mkdir -p \
    "$source_dir/.chezmoiscripts" \
    "$source_dir/dot_local/bin" \
    "$source_dir/dot_local/lib/terrapod"

  cp "$repo_root/Brewfile" "$source_dir/Brewfile"
  cp "$repo_root/Brewfile.macos-desktop-apps.tmpl" "$source_dir/Brewfile.macos-desktop-apps.tmpl"
  cp "$repo_root/.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl" "$source_dir/.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl"
  cp "$terrapod" "$source_dir/dot_local/bin/executable_terrapod"
  cp "$tpod_source" "$source_dir/dot_local/bin/symlink_tpod"
  cp "$install_warnings_lib" "$source_dir/dot_local/lib/terrapod/install-warnings.sh"
}
```

- [x] **Step 3: Add a config writer for App Group combinations**

Add this helper near `copy_desktop_apply_source_fixture()`:

```sh
write_desktop_apply_config() {
  config_file="$1"
  source_dir="$2"
  dest_dir="$3"
  terminal_apps="$4"
  launcher="$5"

  cat >"$config_file" <<EOF
sourceDir = "$source_dir"
destDir = "$dest_dir"

[data]
profile = "macos-terminal"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = $terminal_apps
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = $launcher
enableMacosAppGroupMonitoring = false
enableMacosAppGroupAiApps = false
EOF
}
```

- [x] **Step 4: Add a real `tpod apply` recalculation test**

Add this test after the existing successful apply marker surfacing test:

```sh
real_chezmoi="$(command -v chezmoi 2>/dev/null || true)"
if [ -z "$real_chezmoi" ]; then
  fail "desktop apply recalculation test requires chezmoi"
fi

desktop_apply_source="$tmp_dir/desktop-apply-source"
desktop_apply_home="$tmp_dir/desktop-apply-home"
desktop_apply_state="$tmp_dir/desktop-apply-state"
desktop_apply_bin="$tmp_dir/desktop-apply-bin"
desktop_apply_log="$tmp_dir/desktop-apply-brew.log"
desktop_apply_config="$tmp_dir/desktop-apply.toml"
mkdir -p "$desktop_apply_home" "$desktop_apply_bin"
copy_desktop_apply_source_fixture "$desktop_apply_source"
ln -s "$real_chezmoi" "$desktop_apply_bin/chezmoi"
write_brew_bundle_stub "$desktop_apply_bin/brew"

write_desktop_apply_config "$desktop_apply_config" "$desktop_apply_source" "$desktop_apply_home" true true

if ! HOME="$desktop_apply_home" XDG_STATE_HOME="$desktop_apply_state" TERRAPOD_CHEZMOI_CONFIG="$desktop_apply_config" MACOS_BREW_LOG="$desktop_apply_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty raycast" PATH="$desktop_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/desktop-apply-first.out" 2>"$tmp_dir/desktop-apply-first.err"; then
  fail "Terrapod apply succeeds when desktop App Group casks fail with a marker"
fi

desktop_apply_marker="$desktop_apply_state/terrapod/install-warnings/homebrew-desktop-apps"
desktop_apply_marker_text="$(cat "$desktop_apply_marker")"
assert_contains "$desktop_apply_marker_text" "failed casks: ghostty, raycast" "Terrapod apply records failed casks from enabled terminal and launcher groups"
assert_contains "$desktop_apply_marker_text" "App Groups: terminal-apps, launcher" "Terrapod apply records failed App Groups from enabled terminal and launcher groups"

write_desktop_apply_config "$desktop_apply_config" "$desktop_apply_source" "$desktop_apply_home" true false

if ! HOME="$desktop_apply_home" XDG_STATE_HOME="$desktop_apply_state" TERRAPOD_CHEZMOI_CONFIG="$desktop_apply_config" MACOS_BREW_LOG="$desktop_apply_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty raycast" PATH="$desktop_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/desktop-apply-terminal-only.out" 2>"$tmp_dir/desktop-apply-terminal-only.err"; then
  fail "Terrapod apply succeeds when disabled launcher failures are recalculated away"
fi

desktop_apply_marker_text="$(cat "$desktop_apply_marker")"
assert_contains "$desktop_apply_marker_text" "failed casks: ghostty" "Terrapod apply retains enabled terminal-apps failure after App Group settings change"
assert_contains "$desktop_apply_marker_text" "App Groups: terminal-apps" "Terrapod apply retains enabled terminal-apps group after App Group settings change"
assert_not_contains "$desktop_apply_marker_text" "raycast" "Terrapod apply removes disabled launcher cask from marker content"
assert_not_contains "$desktop_apply_marker_text" "launcher" "Terrapod apply removes disabled launcher group from marker content"
```

- [x] **Step 5: Run command tests**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: PASS because `tpod apply` already surfaces marker guidance generically and does not fail solely because markers remain.

---

### Task 4: Add doctor readiness coverage for desktop marker detail

**Files:**
- Modify: `tests/terrapod_command_test.sh`

- [x] **Step 1: Add a `homebrew-desktop-apps` marker to the doctor marker test**

In the existing `doctor_marker_state` setup near the `ubuntu-bootstrap` marker, add:

```sh
HOME="$tmp_dir/doctor-marker-home" XDG_STATE_HOME="$doctor_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-desktop-apps "Homebrew desktop app install needs attention" "Review Homebrew cask output for failed casks: ghostty, raycast; App Groups: terminal-apps, launcher, fix app installation access, then rerun tpod apply."' \
  sh "$install_warnings_lib"
```

- [x] **Step 2: Assert doctor reports the desktop marker category**

Add after `doctor_marker_output="$(cat "$tmp_dir/doctor-marker.out")"`:

```sh
assert_contains "$doctor_marker_output" "warn - install warning marker remains: homebrew-desktop-apps" "Terrapod doctor reports homebrew desktop app warning marker categories"
assert_contains "$doctor_marker_output" "Summary: Homebrew desktop app install needs attention" "Terrapod doctor reports homebrew desktop app warning marker summary"
assert_contains "$doctor_marker_output" "Guidance: Review Homebrew cask output for failed casks: ghostty, raycast; App Groups: terminal-apps, launcher, fix app installation access, then rerun tpod apply." "Terrapod doctor reports homebrew desktop app cask and App Group guidance"
```

- [x] **Step 3: Run the targeted command tests and verify pass**

Run:

```bash
sh tests/terrapod_command_test.sh
```

Expected: PASS after implementation because `doctor` already surfaces marker guidance generically.

- [x] **Step 4: Commit if using frequent commits**

```bash
git add tests/terrapod_command_test.sh
git commit -m "test: cover desktop marker doctor readiness"
```

---

### Task 5: Full verification and issue cleanup

**Files:**
- Verify only unless failures require scoped fixes.

- [x] **Step 1: Run shell syntax checks**

Run syntax checks on the rendered bootstrap scripts through the tests, then direct shell files:

```bash
sh tests/chezmoiignore_test.sh
sh -n dot_local/lib/terrapod/install-warnings.sh
sh -n dot_local/bin/executable_terrapod
```

Expected: PASS.

- [x] **Step 2: Run targeted regression tests**

Run:

```bash
sh tests/chezmoiignore_test.sh
sh tests/terrapod_command_test.sh
```

Expected: PASS.

- [x] **Step 3: Run the full shell test suite**

Run:

```bash
for test_file in tests/*.sh; do sh "$test_file"; done
```

Expected: PASS.

- [x] **Step 4: Inspect diff for scope**

Run:

```bash
git status -sb
git diff -- .chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl tests/chezmoiignore_test.sh tests/terrapod_command_test.sh docs/superpowers/plans/2026-06-04-homebrew-desktop-app-markers.md
```

Expected: only issue #104 changes and the implementation plan are modified.

- [x] **Step 5: Final commit**

If earlier tasks were not committed separately, run:

```bash
git add .chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl tests/chezmoiignore_test.sh tests/terrapod_command_test.sh docs/superpowers/plans/2026-06-04-homebrew-desktop-app-markers.md
git commit -m "fix: make desktop app warnings app group aware"
```

Expected: clean working tree except ignored files.
