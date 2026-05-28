# First-Run Terrapod Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Change the first-run installer so it initializes the Terrapod Source Repository, runs checked-out Terrapod Setup, and only applies declared state after setup succeeds.

**Architecture:** Keep the installer as a POSIX `sh` script and split the first-run flow into source initialization, setup orchestration, and apply steps. Reuse the checked-out `dot_local/bin/executable_terrapod` as the setup entrypoint and print installer-owned recovery guidance when setup exits non-zero. Cover behavior in the existing installer shell integration test with stubs that prove command order and setup failure handling.

**Tech Stack:** POSIX `sh`, existing shell integration tests under `tests/`, GitHub issue #58 acceptance criteria.

---

## File Structure

- Modify `install.sh`: remove installer-owned Preset selection, add checked-out setup orchestration and recovery guidance, and run `chezmoi apply` only after setup succeeds.
- Modify `tests/terrapod_installer_test.sh`: update the first-run stubs and assertions to expect `terrapod setup`; add setup failure and setup cancellation cases that prove no apply or completion message is emitted.
- Create `docs/superpowers/plans/2026-05-28-first-run-terrapod-setup.md`: this implementation plan for Issue #58.

---

### Task 1: Add Successful Setup Orchestration Tests

**Files:**
- Modify: `tests/terrapod_installer_test.sh`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Extend the checked-out Terrapod stub to model setup**

In `write_chezmoi_flow_stub()`, replace the embedded `TERRAPOD_STUB` body after the PATH logging block with this case block so `setup` consumes and records forwarded installer stdin, succeeds by default, and optionally fails when a test exports `TERRAPOD_SETUP_STUB_STATUS`:

```sh
case "${1-}" in
  setup)
    setup_stdin_line_number=0
    while IFS= read -r setup_stdin_line || [ -n "$setup_stdin_line" ]; do
      setup_stdin_line_number=$((setup_stdin_line_number + 1))
      printf '%s\n' "terrapod setup stdin $setup_stdin_line_number:$setup_stdin_line" >>"$log_file"
    done
    printf '%s\n' "terrapod setup stdin lines:$setup_stdin_line_number" >>"$log_file"

    setup_status="${TERRAPOD_SETUP_STUB_STATUS:-0}"
    if [ "$setup_status" != "0" ]; then
      printf '%s\n' "${TERRAPOD_SETUP_STUB_MESSAGE:-terrapod setup failed}" >&2
      exit "$setup_status"
    fi
    ;;
  configure)
    ;;
esac
```

- [ ] **Step 2: Update the happy-path first-run input**

In the `first_run_case` block, replace:

```sh
first_run_input='workstation
'
```

with:

```sh
first_run_input='workstation





y
'
```

This represents the real `terrapod setup` interaction for the workstation Preset: Preset selection, default customization answers, and final confirmation.

- [ ] **Step 3: Replace configure assertions with setup assertions**

In the `first_run_case` assertions, replace:

```sh
assert_contains "$first_run_log_text" "terrapod TERRAPOD_PROFILE:macos-terminal" "configure receives macOS Terrapod profile"
assert_contains "$first_run_log_text" "terrapod args:configure workstation" "configure receives workstation preset"
assert_first_occurrence_before "$first_run_log_text" "terrapod args:configure workstation" "chezmoi args:apply" "configure runs before chezmoi apply"
assert_contains "$first_run_log_text" "chezmoi args:apply" "chezmoi apply runs"
```

with:

```sh
assert_contains "$first_run_log_text" "terrapod TERRAPOD_PROFILE:macos-terminal" "setup receives macOS Terrapod profile"
assert_contains "$first_run_log_text" "terrapod TERRAPOD_CHEZMOI_CONFIG:" "setup receives an empty Terrapod chezmoi config override"
assert_contains "$first_run_log_text" "terrapod args:setup" "checked-out Terrapod Setup runs"
assert_contains "$first_run_log_text" "terrapod setup stdin 1:workstation" "checked-out Terrapod Setup receives Preset input"
assert_contains "$first_run_log_text" "terrapod setup stdin 7:y" "checked-out Terrapod Setup receives final confirmation input"
assert_contains "$first_run_log_text" "terrapod setup stdin lines:7" "checked-out Terrapod Setup receives the full workstation setup input"
assert_not_contains "$first_run_log_text" "terrapod args:configure" "first-run installer does not bypass setup with configure"
assert_first_occurrence_before "$first_run_log_text" "chezmoi args:init https://github.com/juty9026/terrapod.git" "terrapod args:setup" "setup runs after source repository initialization"
assert_first_occurrence_before "$first_run_log_text" "terrapod args:setup" "chezmoi args:apply" "setup runs before chezmoi apply"
assert_contains "$first_run_log_text" "chezmoi args:apply" "chezmoi apply runs after setup"
```

- [ ] **Step 4: Run the focused installer test and verify it fails**

Run:

```bash
sh tests/terrapod_installer_test.sh
```

Expected: FAIL before implementation because the installer still invokes `terrapod configure <preset>` instead of `terrapod setup`.

- [ ] **Step 5: Do not commit yet**

Leave these red tests unstaged. They will be committed with the implementation after Task 3 passes.

---

### Task 2: Add Setup Failure and Cancellation Tests

**Files:**
- Modify: `tests/terrapod_installer_test.sh`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Add a setup failure case**

After the successful `first_run_case` assertions and before `system_chezmoi_case`, add:

```sh
setup_failure_case="$(make_case_dir setup-failure)"
write_uname_stub "$setup_failure_case" "Darwin"
write_chezmoi_flow_stub "$setup_failure_case/chezmoi-template"
write_installer_download_stubs "$setup_failure_case"
write_command_call_stubs "$setup_failure_case" "wget" "git"
setup_failure_log="$setup_failure_case/command-calls"
setup_failure_stdin_capture="$setup_failure_case/installer-stdin"
setup_failure_script_capture="$setup_failure_case/installer-script"
TERRAPOD_STUB_CALL_LOG="$setup_failure_log"
TERRAPOD_INSTALLER_STDIN_CAPTURE="$setup_failure_stdin_capture"
TERRAPOD_INSTALLER_SCRIPT_CAPTURE="$setup_failure_script_capture"
TERRAPOD_CHEZMOI_STUB_TEMPLATE="$setup_failure_case/chezmoi-template"
TERRAPOD_SETUP_STUB_STATUS=17
TERRAPOD_SETUP_STUB_MESSAGE="simulated Terrapod Setup failure"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_INSTALLER_STDIN_CAPTURE
export TERRAPOD_INSTALLER_SCRIPT_CAPTURE
export TERRAPOD_CHEZMOI_STUB_TEMPLATE
export TERRAPOD_SETUP_STUB_STATUS
export TERRAPOD_SETUP_STUB_MESSAGE
setup_failure_input='minimal
n
n
n
n
n
n
n
y
'
run_installer_case "$setup_failure_case" "$setup_failure_input"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_INSTALLER_STDIN_CAPTURE
unset TERRAPOD_INSTALLER_SCRIPT_CAPTURE
unset TERRAPOD_CHEZMOI_STUB_TEMPLATE
unset TERRAPOD_SETUP_STUB_STATUS
unset TERRAPOD_SETUP_STUB_MESSAGE
assert_failure "$installer_status" "setup failure makes installer exit unsuccessfully"
setup_failure_stdout="$(cat "$setup_failure_case/stdout")"
setup_failure_stderr="$(cat "$setup_failure_case/stderr")"
setup_failure_log_text="$(cat "$setup_failure_log")"
assert_contains "$setup_failure_log_text" "terrapod args:setup" "setup failure case runs checked-out Terrapod Setup"
assert_contains "$setup_failure_log_text" "terrapod setup stdin 1:minimal" "setup failure case forwards Preset input to Terrapod Setup"
assert_contains "$setup_failure_log_text" "terrapod setup stdin lines:9" "setup failure case forwards full minimal setup input to Terrapod Setup"
assert_first_occurrence_before "$setup_failure_log_text" "chezmoi args:init https://github.com/juty9026/terrapod.git" "terrapod args:setup" "setup failure case initializes source before setup"
assert_not_contains "$setup_failure_log_text" "chezmoi args:apply" "setup failure case does not run initial apply"
assert_not_contains "$setup_failure_stdout" "Terrapod first-run apply complete." "setup failure case does not print first-run completion"
assert_contains "$setup_failure_stderr" "simulated Terrapod Setup failure" "setup failure case preserves setup error output"
assert_contains "$setup_failure_stderr" "Terrapod Setup did not complete." "setup failure case explains setup did not complete"
assert_contains "$setup_failure_stderr" "Resume Terrapod Setup from the checked-out source repository:" "setup failure case prints recovery heading"
assert_contains "$setup_failure_stderr" "cd \"$setup_failure_case/xdg-data/chezmoi\" && TERRAPOD_PROFILE=\"macos-terminal\" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup" "setup failure case prints resume command"
```

- [ ] **Step 2: Add a setup cancellation case**

Immediately after the setup failure case, add:

```sh
setup_cancel_case="$(make_case_dir setup-cancel)"
write_uname_stub "$setup_cancel_case" "Darwin"
write_chezmoi_flow_stub "$setup_cancel_case/chezmoi-template"
write_installer_download_stubs "$setup_cancel_case"
write_command_call_stubs "$setup_cancel_case" "wget" "git"
setup_cancel_log="$setup_cancel_case/command-calls"
setup_cancel_stdin_capture="$setup_cancel_case/installer-stdin"
setup_cancel_script_capture="$setup_cancel_case/installer-script"
TERRAPOD_STUB_CALL_LOG="$setup_cancel_log"
TERRAPOD_INSTALLER_STDIN_CAPTURE="$setup_cancel_stdin_capture"
TERRAPOD_INSTALLER_SCRIPT_CAPTURE="$setup_cancel_script_capture"
TERRAPOD_CHEZMOI_STUB_TEMPLATE="$setup_cancel_case/chezmoi-template"
TERRAPOD_SETUP_STUB_STATUS=1
TERRAPOD_SETUP_STUB_MESSAGE="terrapod: setup cancelled"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_INSTALLER_STDIN_CAPTURE
export TERRAPOD_INSTALLER_SCRIPT_CAPTURE
export TERRAPOD_CHEZMOI_STUB_TEMPLATE
export TERRAPOD_SETUP_STUB_STATUS
export TERRAPOD_SETUP_STUB_MESSAGE
setup_cancel_input='development





n
'
run_installer_case "$setup_cancel_case" "$setup_cancel_input"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_INSTALLER_STDIN_CAPTURE
unset TERRAPOD_INSTALLER_SCRIPT_CAPTURE
unset TERRAPOD_CHEZMOI_STUB_TEMPLATE
unset TERRAPOD_SETUP_STUB_STATUS
unset TERRAPOD_SETUP_STUB_MESSAGE
assert_failure "$installer_status" "setup cancellation makes installer exit unsuccessfully"
setup_cancel_stdout="$(cat "$setup_cancel_case/stdout")"
setup_cancel_stderr="$(cat "$setup_cancel_case/stderr")"
setup_cancel_log_text="$(cat "$setup_cancel_log")"
assert_contains "$setup_cancel_log_text" "terrapod args:setup" "setup cancellation case runs checked-out Terrapod Setup"
assert_contains "$setup_cancel_log_text" "terrapod setup stdin 1:development" "setup cancellation case forwards Preset input to Terrapod Setup"
assert_contains "$setup_cancel_log_text" "terrapod setup stdin 7:n" "setup cancellation case forwards final cancellation input to Terrapod Setup"
assert_contains "$setup_cancel_log_text" "terrapod setup stdin lines:7" "setup cancellation case forwards full development setup input to Terrapod Setup"
assert_not_contains "$setup_cancel_log_text" "chezmoi args:apply" "setup cancellation case does not run initial apply"
assert_not_contains "$setup_cancel_stdout" "Terrapod first-run apply complete." "setup cancellation case does not print first-run completion"
assert_contains "$setup_cancel_stderr" "terrapod: setup cancelled" "setup cancellation case preserves setup cancellation output"
assert_contains "$setup_cancel_stderr" "Terrapod Setup did not complete." "setup cancellation case explains setup did not complete"
assert_contains "$setup_cancel_stderr" "cd \"$setup_cancel_case/xdg-data/chezmoi\" && TERRAPOD_PROFILE=\"macos-terminal\" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup" "setup cancellation case prints resume command"
```

- [ ] **Step 3: Run the focused installer test and verify it fails**

Run:

```bash
sh tests/terrapod_installer_test.sh
```

Expected: FAIL before implementation because the installer currently treats `terrapod configure` as the setup step and can still apply after setup-specific failures.

- [ ] **Step 4: Do not commit yet**

Leave these red tests unstaged. They will be committed with the implementation after Task 3 passes.

---

### Task 3: Implement Installer Setup Orchestration

**Files:**
- Modify: `install.sh`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Remove installer-owned Preset selection**

Delete the entire `choose_preset()` function from `install.sh`. The checked-out `terrapod setup` command owns Preset selection and concrete setting customization.

- [ ] **Step 2: Replace `run_initial_apply()` with smaller first-run helpers**

Replace the existing `run_initial_apply()` function:

```sh
run_initial_apply() {
  chezmoi_bin="$1"
  profile="$2"
  source_dir="$3"
  preset="$4"

  "$chezmoi_bin" init "$DEFAULT_SOURCE_REPO" || fatal "chezmoi init failed"

  terrapod_source="$source_dir/dot_local/bin/executable_terrapod"
  if [ ! -x "$terrapod_source" ]; then
    fatal "checked-out Terrapod executable is missing: $terrapod_source"
  fi

  TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= "$terrapod_source" configure "$preset" \
    || fatal "Terrapod configure failed"
  "$chezmoi_bin" apply || fatal "chezmoi apply failed"
}
```

with:

```sh
initialize_source_repository() {
  chezmoi_bin="$1"

  "$chezmoi_bin" init "$DEFAULT_SOURCE_REPO" || fatal "chezmoi init failed"
}

checked_out_terrapod() {
  source_dir="$1"
  terrapod_source="$source_dir/dot_local/bin/executable_terrapod"

  if [ ! -x "$terrapod_source" ]; then
    fatal "checked-out Terrapod executable is missing: $terrapod_source"
  fi

  printf '%s\n' "$terrapod_source"
}

print_setup_recovery() {
  profile="$1"
  source_dir="$2"

  printf '%s\n' "terrapod installer: Terrapod Setup did not complete." >&2
  printf '%s\n' "terrapod installer: Resume Terrapod Setup from the checked-out source repository:" >&2
  printf '%s\n' "terrapod installer:   cd \"$source_dir\" && TERRAPOD_PROFILE=\"$profile\" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup" >&2
}

run_terrapod_setup() {
  profile="$1"
  source_dir="$2"
  terrapod_source="$(checked_out_terrapod "$source_dir")"

  if TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= "$terrapod_source" setup; then
    return 0
  fi

  print_setup_recovery "$profile" "$source_dir"
  return 1
}

run_initial_apply() {
  chezmoi_bin="$1"

  "$chezmoi_bin" apply || fatal "chezmoi apply failed"
}
```

- [ ] **Step 3: Update `main()` flow**

In `main()`, replace:

```sh
preset="$(choose_preset "$profile")"
ensure_source_repo_prerequisites "$profile"
run_initial_apply "$chezmoi_bin" "$profile" "$source_dir" "$preset"
```

with:

```sh
ensure_source_repo_prerequisites "$profile"
initialize_source_repository "$chezmoi_bin"
run_terrapod_setup "$profile" "$source_dir"
run_initial_apply "$chezmoi_bin"
```

Keep `ensure_user_local_bin`, `reject_existing_source_dir`, `install_chezmoi_if_needed`, supported profile detection, and Ubuntu source prerequisites in their existing order.

- [ ] **Step 4: Run the focused installer test and verify it passes**

Run:

```bash
sh tests/terrapod_installer_test.sh
```

Expected: PASS.

- [ ] **Step 5: Commit**

Commit the tests, implementation, and plan:

```bash
git add install.sh tests/terrapod_installer_test.sh docs/superpowers/plans/2026-05-28-first-run-terrapod-setup.md
git commit -m "Invoke Terrapod Setup during first-run install"
```

---

### Task 4: Full Regression Verification

**Files:**
- Test: `tests/*_test.sh`

- [ ] **Step 1: Run all shell tests**

Run:

```bash
for test_script in tests/*_test.sh; do
  printf 'Running %s\n' "$test_script"
  sh "$test_script" || exit 1
done
```

Expected: PASS for every test script.

- [ ] **Step 2: Inspect final diff**

Run:

```bash
git status -sb
git diff --stat HEAD
git diff HEAD -- install.sh tests/terrapod_installer_test.sh docs/superpowers/plans/2026-05-28-first-run-terrapod-setup.md
```

Expected: working tree clean if Task 3 committed successfully; diff commands show no unstaged changes against `HEAD`.

- [ ] **Step 3: Self-review Issue #58 acceptance criteria**

Check the implementation against each criterion:

```text
- first-run installer invokes Terrapod Setup after source repo initialization and before initial apply
- initial apply runs only after Terrapod Setup succeeds
- setup failure exits unsuccessfully and omits first-run completion message
- setup cancellation exits unsuccessfully and omits first-run completion message
- setup failure/cancellation prints a resume command for checked-out Terrapod Setup
- existing guarantees remain intact: user-local chezmoi, profile detection, source guard before network/apply, Ubuntu git prerequisites before init
- tests cover successful setup orchestration, setup failure guidance, and setup-before-apply command order
```

Expected: every item maps to either `install.sh` flow or an assertion in `tests/terrapod_installer_test.sh`.
