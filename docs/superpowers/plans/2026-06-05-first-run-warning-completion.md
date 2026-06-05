# First-Run Warning Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Make first-run installation complete cleanly or with explicit marker-backed warning completion after the recovery core is valid.

**Architecture:** Keep recovery-core installation and validation as the hard boundary. Run the full declared-state apply with a first-run-only environment flag so known non-blocking installer scripts can record Terrapod install-warning markers and exit 0, allowing chezmoi to continue. Snapshot markers before the full apply and print warning completion only when the full apply succeeds and a marker is newly written or updated; any aggregate non-zero full apply remains a hard failure so unknown chezmoi/template/managed-file failures cannot be masked by an unrelated marker.

**Tech Stack:** POSIX `sh`, chezmoi apply stubs, Terrapod install-warning marker helper, shell test scripts.

---

## Scope And Assumptions

- Issue: GitHub Issue #100, "First-run warning completion and final output".
- Blockers #96, #98, and #99 are closed.
- Do not change routine `tpod apply` failure semantics; the marker-and-success behavior is gated by `TERRAPOD_FIRST_RUN_APPLY=1`.
- Recovery-core failures remain hard failures.
- Full apply output must not be captured or hidden; marker files store summary/guidance only.
- A pre-existing unchanged marker must not produce warning completion.
- Marker writes must carry a per-write freshness field so same-second rewrites of the same category/summary/guidance are still classified as changed.
- If `chezmoi apply` returns non-zero, `install.sh` treats it as hard failure even if some marker changed earlier in the same apply. This avoids hiding mixed failures; known first-run non-blocking installers must record a marker and exit 0.

## File Structure

- Modify: `install.sh`
  - Add install-warning snapshot/read helpers near existing marker helper functions.
  - Run full apply with `TERRAPOD_FIRST_RUN_APPLY=1`.
  - Return clean vs warning status based on marker changes after successful full apply.
  - Add final output helpers for `tpod` availability, clean completion, and warning completion.
- Modify: `dot_local/lib/terrapod/install-warnings.sh`
  - Add a per-write marker freshness field used by snapshot comparison.
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`
  - Gate warning-marker failures to exit 0 only during first-run full apply.
- Modify: `.chezmoiscripts/run_before_01-retry-homebrew-desktop-apps.sh.tmpl`
  - Keep desktop app warning retries non-blocking only when marker writes succeed.
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl`
  - Same first-run marker-and-success contract for `ubuntu-bootstrap`.
- Modify: `.chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl`
  - Same first-run marker-and-success contract for `shell-integrations`.
- Modify: `.chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl`
  - Same first-run marker-and-success contract for `mise-tools`.
- Modify: `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`
  - Same first-run marker-and-success contract for `optional-ai-cli-tools`.
- Modify: `tests/terrapod_installer_test.sh`
  - Extend fake source checkouts and full apply stub knobs.
  - Add first-run clean/warning/hard-failure/stale-marker/same-second rewrite output tests.
- Modify: relevant rendered script tests as needed:
  - `tests/chezmoiignore_test.sh`
  - `tests/bootstrap_ubuntu_test.sh`
  - `tests/shell_integrations_test.sh`
- Create: `docs/superpowers/plans/2026-06-05-first-run-warning-completion.md`
  - This plan.

---

### Task 1: Add First-Run Installer Regression Tests

**Files:**
- Modify: `tests/terrapod_installer_test.sh`

- [x] **Step 1: Make fake source checkouts include the warning helper**

Near `repo_root`, export the helper path:

```sh
install_warnings_lib_template="$repo_root/dot_local/lib/terrapod/install-warnings.sh"
export TERRAPOD_INSTALL_WARNINGS_LIB_TEMPLATE="$install_warnings_lib_template"
```

In `write_chezmoi_flow_stub`, after fake `init` writes `dot_local/bin/executable_terrapod`, copy the helper into the fake source checkout when the template path exists:

```sh
    if [ -f "${TERRAPOD_INSTALL_WARNINGS_LIB_TEMPLATE:-}" ]; then
      mkdir -p "$source_dir/dot_local/lib/terrapod"
      cp "$TERRAPOD_INSTALL_WARNINGS_LIB_TEMPLATE" "$source_dir/dot_local/lib/terrapod/install-warnings.sh"
    fi
```

In `write_terrapod_source_checkout`, always copy the real helper:

```sh
  mkdir -p "$source_dir/dot_local/lib/terrapod"
  cp "$repo_root/dot_local/lib/terrapod/install-warnings.sh" "$source_dir/dot_local/lib/terrapod/install-warnings.sh"
```

- [x] **Step 2: Add full apply stub controls**

Inside the non-`--force` `apply)` branch of `write_chezmoi_flow_stub`, before writing the installed command stub, add:

```sh
    if [ -n "${TERRAPOD_CHEZMOI_APPLY_STUB_STDOUT:-}" ]; then
      printf '%s\n' "$TERRAPOD_CHEZMOI_APPLY_STUB_STDOUT"
    fi
    if [ -n "${TERRAPOD_CHEZMOI_APPLY_STUB_STDERR:-}" ]; then
      printf '%s\n' "$TERRAPOD_CHEZMOI_APPLY_STUB_STDERR" >&2
    fi
    if [ -n "${TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_CATEGORY:-}" ] && [ -f "${TERRAPOD_INSTALL_WARNINGS_LIB_TEMPLATE:-}" ]; then
      . "$TERRAPOD_INSTALL_WARNINGS_LIB_TEMPLATE"
      terrapod_install_warning_write \
        "$TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_CATEGORY" \
        "${TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_SUMMARY:-mise tool install needs attention}" \
        "${TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_GUIDANCE:-Run tpod apply after fixing installer output.}"
    fi
    apply_status="${TERRAPOD_CHEZMOI_APPLY_STUB_STATUS:-0}"
    if [ "$apply_status" != "0" ]; then
      exit "$apply_status"
    fi
```

- [x] **Step 3: Add clean first-run output assertions**

In the existing `first_run_case` block, assert recovery-core ordering and clean final output:

```sh
assert_first_occurrence_before "$first_run_log_text" "terrapod path:$first_run_case/home/.local/bin/tpod" "chezmoi args:apply --force" "first-run validates recovery-core command surface before shell startup recovery apply"
assert_first_occurrence_before "$first_run_log_text" "chezmoi args:apply --force" "tpod args:help" "first-run applies recovery-core shell startup files before final help"
assert_contains "$first_run_stdout" "Terrapod command availability:" "clean first-run explains tpod availability"
assert_contains "$first_run_stdout" "Use this absolute command now: $first_run_case/home/.local/bin/tpod" "clean first-run prints absolute tpod command"
assert_contains "$first_run_stdout" "Open a new terminal or refresh your login shell before relying on plain 'tpod'." "clean first-run explains shell refresh guidance"
assert_not_contains "$first_run_stdout" "$first_run_case/home/.local/bin/tpod doctor" "clean first-run does not print doctor recovery guidance"
```

- [x] **Step 4: Add marker-backed warning completion test**

Add a resume case where full apply succeeds while writing a marker and emitting visible output:

```sh
warning_completion_case="$(make_case_dir warning-completion)"
prepare_resumable_macos_case "$warning_completion_case"
write_complete_setup_config "$warning_completion_case/xdg-config/chezmoi/chezmoi.toml"
warning_completion_log="$warning_completion_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$warning_completion_log"
TERRAPOD_CHEZMOI_APPLY_STUB_STDERR="simulated mise install failure"
TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_CATEGORY=mise-tools
TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_SUMMARY="mise tool install needs attention"
TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_GUIDANCE="Review mise output, then rerun tpod apply."
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_CHEZMOI_APPLY_STUB_STDERR
export TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_CATEGORY
export TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_SUMMARY
export TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_GUIDANCE
run_installer_case "$warning_completion_case"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_CHEZMOI_APPLY_STUB_STDERR
unset TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_CATEGORY
unset TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_SUMMARY
unset TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_GUIDANCE
assert_status "$installer_status" 0 "marker-backed full apply warning completes with installer status 0"
warning_completion_stdout="$(cat "$warning_completion_case/stdout")"
warning_completion_stderr="$(cat "$warning_completion_case/stderr")"
assert_contains "$warning_completion_stderr" "simulated mise install failure" "warning completion preserves full apply output"
assert_contains "$warning_completion_stdout" "Terrapod - a small landing pod for your dotfiles" "warning completion still prints tpod help"
assert_contains "$warning_completion_stdout" "Terrapod first-run apply completed with warnings." "warning completion prints distinct final status"
assert_contains "$warning_completion_stdout" "$warning_completion_case/home/.local/bin/tpod doctor" "warning completion prints absolute doctor recovery command"
```

- [x] **Step 5: Add unknown full apply hard failure test**

Add a resume case with non-zero full apply and no marker:

```sh
unknown_apply_failure_case="$(make_case_dir unknown-apply-failure)"
prepare_resumable_macos_case "$unknown_apply_failure_case"
write_complete_setup_config "$unknown_apply_failure_case/xdg-config/chezmoi/chezmoi.toml"
unknown_apply_failure_log="$unknown_apply_failure_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$unknown_apply_failure_log"
TERRAPOD_CHEZMOI_APPLY_STUB_STATUS=43
TERRAPOD_CHEZMOI_APPLY_STUB_STDERR="simulated template rendering failure"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_CHEZMOI_APPLY_STUB_STATUS
export TERRAPOD_CHEZMOI_APPLY_STUB_STDERR
run_installer_case "$unknown_apply_failure_case"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_CHEZMOI_APPLY_STUB_STATUS
unset TERRAPOD_CHEZMOI_APPLY_STUB_STDERR
assert_failure "$installer_status" "unknown full apply failure remains hard"
unknown_apply_failure_stdout="$(cat "$unknown_apply_failure_case/stdout")"
unknown_apply_failure_stderr="$(cat "$unknown_apply_failure_case/stderr")"
assert_contains "$unknown_apply_failure_stderr" "simulated template rendering failure" "unknown full apply failure preserves apply output"
assert_contains "$unknown_apply_failure_stderr" "terrapod installer: chezmoi apply failed" "unknown full apply failure keeps hard failure guidance"
assert_not_contains "$unknown_apply_failure_stdout" "completed with warnings" "unknown full apply failure does not print warning completion"
```

- [x] **Step 6: Add mixed marker plus non-zero hard failure test**

Add a resume case where the stub writes a marker and still exits non-zero:

```sh
mixed_apply_failure_case="$(make_case_dir mixed-apply-failure)"
prepare_resumable_macos_case "$mixed_apply_failure_case"
write_complete_setup_config "$mixed_apply_failure_case/xdg-config/chezmoi/chezmoi.toml"
mixed_apply_failure_log="$mixed_apply_failure_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$mixed_apply_failure_log"
TERRAPOD_CHEZMOI_APPLY_STUB_STATUS=45
TERRAPOD_CHEZMOI_APPLY_STUB_STDERR="simulated managed-file failure after marker"
TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_CATEGORY=mise-tools
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_CHEZMOI_APPLY_STUB_STATUS
export TERRAPOD_CHEZMOI_APPLY_STUB_STDERR
export TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_CATEGORY
run_installer_case "$mixed_apply_failure_case"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_CHEZMOI_APPLY_STUB_STATUS
unset TERRAPOD_CHEZMOI_APPLY_STUB_STDERR
unset TERRAPOD_CHEZMOI_APPLY_STUB_WARNING_CATEGORY
assert_failure "$installer_status" "non-zero full apply remains hard even when an unrelated marker changed"
mixed_apply_failure_stdout="$(cat "$mixed_apply_failure_case/stdout")"
mixed_apply_failure_stderr="$(cat "$mixed_apply_failure_case/stderr")"
assert_contains "$mixed_apply_failure_stderr" "simulated managed-file failure after marker" "mixed full apply failure preserves output"
assert_not_contains "$mixed_apply_failure_stdout" "completed with warnings" "mixed full apply failure does not print warning completion"
```

- [x] **Step 7: Add stale marker clean success test**

Create a marker before a successful full apply and assert clean completion:

```sh
stale_marker_success_case="$(make_case_dir stale-marker-success)"
prepare_resumable_macos_case "$stale_marker_success_case"
write_complete_setup_config "$stale_marker_success_case/xdg-config/chezmoi/chezmoi.toml"
HOME="$stale_marker_success_case/home" sh -c \
  '. "$1"; terrapod_install_warning_write mise-tools "stale warning" "stale guidance"' \
  sh "$install_warnings_lib_template"
stale_marker_success_log="$stale_marker_success_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$stale_marker_success_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$stale_marker_success_case"
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "unchanged stale marker does not prevent clean first-run completion"
stale_marker_success_stdout="$(cat "$stale_marker_success_case/stdout")"
assert_contains "$stale_marker_success_stdout" "Terrapod first-run apply complete." "stale marker success keeps clean completion"
assert_not_contains "$stale_marker_success_stdout" "$stale_marker_success_case/home/.local/bin/tpod doctor" "stale marker success does not print doctor recovery guidance"
```

- [x] **Step 8: Run tests and verify they fail before implementation**

Run: `sh tests/terrapod_installer_test.sh`

Expected: FAIL on new output/classification assertions before `install.sh` changes.

---

### Task 2: Add First-Run Marker-And-Success Contract To Known Scripts

**Files:**
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl`
- Modify: `.chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl`
- Modify: `.chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl`
- Modify: `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`
- Modify: `.chezmoiscripts/run_before_01-retry-homebrew-desktop-apps.sh.tmpl`
- Modify tests as needed: `tests/chezmoiignore_test.sh`, `tests/bootstrap_ubuntu_test.sh`, `tests/shell_integrations_test.sh`

- [x] **Step 1: Add a shared local pattern to each known warning script**

In each script except the AI CLI script, make `mark_install_warning` set a local success flag and add an exit helper:

```sh
INSTALL_WARNING_RECORDED=0

mark_install_warning() {
  category="$1"
  summary="$2"
  guidance="$3"
  INSTALL_WARNING_RECORDED=0

  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" = "1" ] &&
    terrapod_install_warning_write "$category" "$summary" "$guidance"; then
    INSTALL_WARNING_RECORDED=1
    return 0
  fi

  return 1
}

exit_after_install_warning() {
  if [ "${TERRAPOD_FIRST_RUN_APPLY:-}" = "1" ] && [ "$INSTALL_WARNING_RECORDED" -eq 1 ]; then
    exit 0
  fi

  exit 1
}
```

Keep `clear_install_warning` unchanged.

- [x] **Step 2: Replace warning exits in known scripts**

For every `mark_install_warning ...; exit 1` block in the known scripts, replace `exit 1` with:

```sh
    exit_after_install_warning
```

Preserve any cleanup that already happens before the warning marker is recorded.

- [x] **Step 3: Adjust Optional AI CLI finish path**

In `run_onchange_before_60-install-ai-cli-tools.sh.tmpl`, keep `mark_install_warning` returning write status and change the failed-tools branch:

```sh
    if [ "${TERRAPOD_FIRST_RUN_APPLY:-}" = "1" ]; then
      return 0
    fi
    return 1
```

only after `mark_install_warning` succeeds.

- [x] **Step 4: Add or update rendered script tests**

Keep existing routine tests expecting non-zero failures without `TERRAPOD_FIRST_RUN_APPLY`.

Add targeted first-run assertions:

```sh
TERRAPOD_FIRST_RUN_APPLY=1 sh "$rendered"
```

Expected for simulated known category failures: exit 0 and marker file exists.

Use the narrowest existing rendered-script tests:

- `tests/shell_integrations_test.sh`: Oh My Zsh download failure exits 0 with `TERRAPOD_FIRST_RUN_APPLY=1`.
- `tests/bootstrap_ubuntu_test.sh`: Charm signing key download failure exits 0 with `TERRAPOD_FIRST_RUN_APPLY=1`.
- `tests/chezmoiignore_test.sh`: Homebrew installer failure and Optional AI CLI partial failure exit 0 with `TERRAPOD_FIRST_RUN_APPLY=1`.
- `tests/chezmoiignore_test.sh`: Homebrew desktop app warning failures block when marker writes fail.

- [x] **Step 5: Run targeted rendered script tests**

Run:

```bash
sh tests/shell_integrations_test.sh
sh tests/bootstrap_ubuntu_test.sh
sh tests/chezmoiignore_test.sh
```

Expected: PASS.

---

### Task 3: Implement First-Run Warning Completion In `install.sh`

**Files:**
- Modify: `install.sh`

- [x] **Step 1: Add marker snapshot helpers**

Near `clear_install_warning_from_source`, add:

```sh
load_install_warnings_from_source() {
  source_dir="$1"
  install_warnings_lib="$source_dir/dot_local/lib/terrapod/install-warnings.sh"
  TERRAPOD_INSTALL_WARNINGS_LOADED=

  if [ -f "$install_warnings_lib" ]; then
    . "$install_warnings_lib"
  fi

  [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" = "1" ]
}

snapshot_install_warnings_from_source() {
  source_dir="$1"
  snapshot_dir="$2"

  mkdir -p "$snapshot_dir" || return 1
  load_install_warnings_from_source "$source_dir" || return 0

  for category in $(terrapod_install_warning_categories); do
    terrapod_install_warning_read "$category" >"$snapshot_dir/$category" 2>/dev/null || true
  done
}

install_warning_markers_changed_since_snapshot() {
  source_dir="$1"
  snapshot_dir="$2"
  changed=false

  load_install_warnings_from_source "$source_dir" || return 1

  for category in $(terrapod_install_warning_categories); do
    current_file="$snapshot_dir/current-$category"
    if terrapod_install_warning_read "$category" >"$current_file" 2>/dev/null; then
      if [ ! -f "$snapshot_dir/$category" ] || ! cmp -s "$snapshot_dir/$category" "$current_file"; then
        changed=true
      fi
    fi
    rm -f "$current_file"
  done

  [ "$changed" = "true" ]
}
```

- [x] **Step 2: Change `run_initial_apply` status contract**

Replace the current hard-fail-only function with:

```sh
run_initial_apply() {
  chezmoi_bin="$1"
  source_dir="$2"
  marker_snapshot_dir="$(mktemp -d)" ||
    fatal "failed to create install-warning snapshot directory"

  snapshot_install_warnings_from_source "$source_dir" "$marker_snapshot_dir" ||
    fatal "failed to snapshot install warning markers"

  if ! TERRAPOD_FIRST_RUN_APPLY=1 "$chezmoi_bin" apply; then
    rm -rf "$marker_snapshot_dir"
    fatal "chezmoi apply failed"
  fi

  if install_warning_markers_changed_since_snapshot "$source_dir" "$marker_snapshot_dir"; then
    rm -rf "$marker_snapshot_dir"
    return 2
  fi

  rm -rf "$marker_snapshot_dir"
  return 0
}
```

- [x] **Step 3: Add final output helpers**

Near `show_first_run_help`, add:

```sh
print_first_run_tpod_availability() {
  local_bin_dir="$1"

  printf '\n'
  printf '%s\n' "Terrapod command availability:"
  printf '%s\n' "  If this shell has not reloaded Terrapod's managed PATH yet, plain 'tpod' may not resolve."
  printf '%s\n' "  Use this absolute command now: $local_bin_dir/tpod"
  printf '%s\n' "  Open a new terminal or refresh your login shell before relying on plain 'tpod'."
}

print_first_run_clean_completion() {
  printf '\n'
  printf '%s\n' "Terrapod first-run apply complete."
}

print_first_run_warning_completion() {
  local_bin_dir="$1"

  printf '\n'
  printf '%s\n' "Terrapod first-run apply completed with warnings."
  printf '%s\n' "Warning:"
  printf '%s\n' "  Terrapod installed and the recovery core is valid, but machine profile readiness needs attention."
  printf '%s\n' "  Review the full apply output above, then run:"
  printf '%s\n' "  $local_bin_dir/tpod doctor"
}
```

- [x] **Step 4: Wire `main` to use clean vs warning completion**

Replace:

```sh
  run_initial_apply "$chezmoi_bin"
  show_first_run_help "$profile" "$local_bin_dir"

  printf '%s\n' "Terrapod first-run apply complete."
```

with:

```sh
  initial_apply_status=0
  run_initial_apply "$chezmoi_bin" "$source_dir" || initial_apply_status="$?"
  show_first_run_help "$profile" "$local_bin_dir"
  print_first_run_tpod_availability "$local_bin_dir"

  case "$initial_apply_status" in
    0)
      print_first_run_clean_completion
      ;;
    2)
      print_first_run_warning_completion "$local_bin_dir"
      ;;
    *)
      fatal "unexpected initial apply status: $initial_apply_status"
      ;;
  esac
```

- [x] **Step 5: Run targeted installer test**

Run: `sh tests/terrapod_installer_test.sh`

Expected: PASS.

---

### Task 4: Full Verification

**Files:**
- No new files unless a verification failure requires a fix.

- [x] **Step 1: Run shell syntax checks**

Run:

```bash
sh -n install.sh
sh -n dot_local/bin/executable_terrapod
sh -n dot_local/lib/terrapod/install-warnings.sh
```

Expected: all exit 0.

- [x] **Step 2: Run targeted tests**

Run:

```bash
sh tests/terrapod_installer_test.sh
sh tests/terrapod_command_test.sh
sh tests/shell_integrations_test.sh
sh tests/bootstrap_ubuntu_test.sh
sh tests/chezmoiignore_test.sh
```

Expected: PASS.

- [x] **Step 3: Run full shell test suite**

Run:

```bash
for test_file in tests/*_test.sh; do sh "$test_file"; done
```

Expected: PASS for every test file.

- [x] **Step 4: Review diff for scope**

Run:

```bash
git diff --stat
git diff -- install.sh .chezmoiscripts tests docs/superpowers/plans/2026-06-05-first-run-warning-completion.md
```

Expected: only Issue #100-related changes.

---

### Task 5: Commit And Publish Ready PR

**Files:**
- No source changes expected.

- [x] **Step 1: Commit intentionally**

After all tests pass:

```bash
git add install.sh .chezmoiscripts tests docs/superpowers/plans/2026-06-05-first-run-warning-completion.md
git commit -m "fix: add first-run warning completion"
```

- [x] **Step 2: Push branch**

Run:

```bash
git push -u origin feat/issue-100-first-run-warning-completion
```

Expected: branch pushed.

- [x] **Step 3: Create Ready for review PR**

Use GitHub tooling with base branch from the remote default branch. PR title:

```text
First-run warning completion and final output
```

PR body:

```markdown
## Summary
- validates recovery core before full first-run apply warning classification
- adds first-run marker-backed warning completion with absolute doctor guidance
- keeps unknown aggregate full apply failures hard and adds tpod availability guidance

## Test Plan
- [x] sh tests/terrapod_installer_test.sh
- [x] sh tests/terrapod_command_test.sh
- [x] sh tests/shell_integrations_test.sh
- [x] sh tests/bootstrap_ubuntu_test.sh
- [x] sh tests/chezmoiignore_test.sh
- [x] for test_file in tests/*_test.sh; do sh "$test_file"; done

Closes #100
```

Expected: PR is not draft and is ready for review.
