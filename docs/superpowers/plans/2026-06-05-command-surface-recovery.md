# Command Surface Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add first-run recovery-core command surface behavior so Terrapod installs or repairs `~/.local/bin/terrapod` and `~/.local/bin/tpod`, validates `~/.local/bin/tpod help`, and stops on unsafe command-name conflicts.

**Architecture:** Keep the change inside the POSIX first-run installer. Add conservative ownership helpers before the apply flow, install the command surface directly from the checked-out Terrapod Source Repository before full `chezmoi apply`, and validate both command files before warning-capable completion can happen. Extend the existing shell installer test harness rather than adding a new test runner.

**Tech Stack:** POSIX `sh`, `awk`, `grep`, `cp`, `ln`, `readlink`, `rm`, existing shell tests in `tests/terrapod_installer_test.sh`.

---

## Assumptions

- Issue #98 is scoped to first-run installer recovery-core command files, not routine `tpod apply` post-apply validation.
- `~/.local/bin/tpod` should be a symlink to `terrapod`, matching `dot_local/bin/symlink_tpod`.
- A clear Terrapod Source Repository pointer means an existing symlink target canonicalizes to `$source_dir/dot_local/bin/executable_terrapod`; relative symlink targets must be resolved from the command file's parent directory.
- A clear installed alias pointer means an existing `tpod` symlink canonicalizes to sibling `terrapod`, because recovery-core itself installs `~/.local/bin/tpod -> terrapod`.
- An existing executable whose `help` output contains Terrapod's canonical help markers counts as Terrapod-owned; arbitrary successful `help` output does not.
- Broken symlinks and exact wrapper files that clearly exec `$source_dir/dot_local/bin/executable_terrapod` are repairable; ambiguous regular files are conflicts.

## File Structure

- Modify `install.sh`
  - Add command surface ownership helpers near `installed_tpod_help_works`.
  - Replace the current already-installed shortcut with full command surface validation for `terrapod`, `tpod`, and canonical Terrapod help output.
  - Add recovery-core command surface install and validation helpers near `run_initial_apply`.
  - Call recovery-core command surface apply after setup and before full `chezmoi apply`.
- Modify `tests/terrapod_installer_test.sh`
  - Add constrained `PATH` commands needed by the installer helpers.
  - Extend source-side and installed command stubs so recovery-core validation can run `tpod help` before full apply.
  - Add tests around resumable first-run repair/conflict behavior.
- Create this plan file at `docs/superpowers/plans/2026-06-05-command-surface-recovery.md`.

## Task 1: Test Command Surface Recovery Cases

**Files:**
- Modify: `tests/terrapod_installer_test.sh:22-27`
- Modify: `tests/terrapod_installer_test.sh:430-584`
- Modify: `tests/terrapod_installer_test.sh:1783-1816`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Add installer helper commands to the constrained PATH**

The installer will use `grep`, `ln`, and `readlink`. Add them to the command loop near the top of `tests/terrapod_installer_test.sh`:

```sh
for command_name in awk cat chmod cp grep ln mkdir mktemp readlink rm; do
  command_path="$(command -v "$command_name")"
  ln -s "$command_path" "$safe_path_dir/$command_name"
done
```

- [ ] **Step 2: Teach source-side Terrapod stubs to support help**

Update both source command stubs:

- the `TERRAPOD_STUB` created inside `write_chezmoi_flow_stub`'s `init)` branch
- `write_terrapod_command_stub`

Add this branch before each `*)` case:

```sh
  help|--help|-h)
    if [ "${TPOD_HELP_STUB_STATUS:-0}" != "0" ]; then
      exit "$TPOD_HELP_STUB_STATUS"
    fi
    printf '%s\n' "Terrapod - a small landing pod for your dotfiles"
    printf '%s\n' "Usage:"
    printf '%s\n' "  tpod apply"
    ;;
```

This matters because recovery-core apply copies `$source_dir/dot_local/bin/executable_terrapod` before full `chezmoi apply`; the copied command must be able to satisfy `~/.local/bin/tpod help`.

- [ ] **Step 3: Extend the apply stub to install both command names**

In `write_chezmoi_flow_stub`, replace the `apply)` body that only writes `"$HOME/.local/bin/tpod"` with a stub that writes `terrapod` and symlinks `tpod`:

```sh
  apply)
    mkdir -p "$HOME/.local/bin"
    cat >"$HOME/.local/bin/terrapod" <<'TERRAPOD_INSTALLED_STUB'
#!/bin/sh
set -eu

command_name="${0##*/}"
printf '%s\n' "$command_name path:$0" >>"${TERRAPOD_STUB_CALL_LOG:?}"
printf '%s\n' "$command_name args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"

case "${1-}" in
  help|--help|-h)
    if [ "${TPOD_HELP_STUB_STATUS:-0}" != "0" ]; then
      exit "$TPOD_HELP_STUB_STATUS"
    fi
    printf '%s\n' "Terrapod - a small landing pod for your dotfiles"
    printf '%s\n' "Usage:"
    printf '%s\n' "  tpod apply"
    ;;
  *)
    printf '%s\n' "unexpected $command_name command:${1-}" >>"${TERRAPOD_STUB_CALL_LOG:?}"
    exit 64
    ;;
esac
TERRAPOD_INSTALLED_STUB
    chmod +x "$HOME/.local/bin/terrapod"
    ln -sf terrapod "$HOME/.local/bin/tpod"
    ;;
```

- [ ] **Step 4: Add installed command-surface helper stubs**

Add helpers after `write_installed_tpod_stub`:

```sh
write_installed_terrapod_command_stub() {
  path="$1"
  status="$2"

  mkdir -p "$(dirname "$path")"
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' 'set -eu'
    printf '%s\n' 'printf "%s\n" "terrapod-owned path:$0" >>"${TERRAPOD_STUB_CALL_LOG:?}"'
    printf '%s\n' 'printf "%s\n" "terrapod-owned args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"'
    printf '%s\n' "if [ '$status' != '0' ]; then"
    printf '%s\n' "  exit '$status'"
    printf '%s\n' 'fi'
    printf '%s\n' 'case "${1-}" in'
    printf '%s\n' '  help|--help|-h)'
    printf '%s\n' '    printf "%s\n" "Terrapod - a small landing pod for your dotfiles"'
    printf '%s\n' '    printf "%s\n" "Usage:"'
    printf '%s\n' '    printf "%s\n" "  tpod apply"'
    printf '%s\n' '    ;;'
    printf '%s\n' '  *) exit 64 ;;'
    printf '%s\n' 'esac'
  } >"$path"
  chmod +x "$path"
}

write_non_terrapod_command_stub() {
  path="$1"

  mkdir -p "$(dirname "$path")"
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' 'set -eu'
    printf '%s\n' 'printf "%s\n" "external command"'
  } >"$path"
  chmod +x "$path"
}

write_source_pointer_command_file() {
  path="$1"
  source_dir="$2"

  mkdir -p "$(dirname "$path")"
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' "exec \"$source_dir/dot_local/bin/executable_terrapod\" \"\$@\""
  } >"$path"
}
```

- [ ] **Step 5: Update already-installed validation test**

Change the existing `already_installed_case` setup from only writing `tpod` to installing a complete command surface:

```sh
write_installed_terrapod_command_stub "$already_installed_case/home/.local/bin/terrapod" 0
ln -sf terrapod "$already_installed_case/home/.local/bin/tpod"
```

Then update the assertions:

```sh
assert_contains "$already_installed_log_text" "terrapod-owned args:help" "already installed detection validates canonical Terrapod help"
assert_contains "$already_installed_log_text" "terrapod-owned path:$already_installed_case/home/.local/bin/terrapod" "already installed detection validates terrapod directly"
assert_contains "$already_installed_log_text" "terrapod-owned path:$already_installed_case/home/.local/bin/tpod" "already installed detection validates tpod directly"
assert_not_contains "$already_installed_log_text" "terrapod args:setup" "already installed case does not rerun setup"
assert_not_contains "$already_installed_log_text" "chezmoi args:apply" "already installed case does not automatically apply"
```

Add a guard case showing an incomplete installed surface does not short-circuit and is treated through recovery-core conflict rules:

```sh
incomplete_installed_surface_case="$(make_case_dir incomplete-installed-surface)"
prepare_resumable_macos_case "$incomplete_installed_surface_case"
write_complete_setup_config "$incomplete_installed_surface_case/xdg-config/chezmoi/chezmoi.toml"
write_installed_tpod_stub "$incomplete_installed_surface_case/home/.local/bin/tpod" 0
incomplete_installed_surface_log="$incomplete_installed_surface_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$incomplete_installed_surface_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$incomplete_installed_surface_case"
unset TERRAPOD_STUB_CALL_LOG
assert_failure "$installer_status" "tpod-only installed surface is not treated as already installed"
incomplete_installed_surface_stderr="$(cat "$incomplete_installed_surface_case/stderr")"
assert_contains "$incomplete_installed_surface_stderr" "$incomplete_installed_surface_case/home/.local/bin/tpod" "tpod-only installed surface is handled as an unsafe conflict"

external_terrapod_installed_surface_case="$(make_case_dir external-terrapod-installed-surface)"
prepare_resumable_macos_case "$external_terrapod_installed_surface_case"
write_complete_setup_config "$external_terrapod_installed_surface_case/xdg-config/chezmoi/chezmoi.toml"
write_non_terrapod_command_stub "$external_terrapod_installed_surface_case/home/.local/bin/terrapod"
write_installed_terrapod_command_stub "$external_terrapod_installed_surface_case/home/.local/bin/tpod" 0
external_terrapod_installed_surface_log="$external_terrapod_installed_surface_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$external_terrapod_installed_surface_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$external_terrapod_installed_surface_case"
unset TERRAPOD_STUB_CALL_LOG
assert_failure "$installer_status" "external terrapod with Terrapod tpod does not short-circuit already-installed detection"
external_terrapod_installed_surface_stderr="$(cat "$external_terrapod_installed_surface_case/stderr")"
assert_contains "$external_terrapod_installed_surface_stderr" "$external_terrapod_installed_surface_case/home/.local/bin/terrapod" "external terrapod conflict is still reported"
```

- [ ] **Step 6: Write failing recovery and conflict tests**

Append after the existing `broken_tpod_case` assertions:

```sh
missing_command_surface_case="$(make_case_dir missing-command-surface-repair)"
prepare_resumable_macos_case "$missing_command_surface_case"
write_complete_setup_config "$missing_command_surface_case/xdg-config/chezmoi/chezmoi.toml"
missing_command_surface_log="$missing_command_surface_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$missing_command_surface_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$missing_command_surface_case"
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "missing command surface is installed during recovery-core apply"
missing_command_surface_log_text="$(cat "$missing_command_surface_log")"
assert_first_occurrence_before "$missing_command_surface_log_text" "terrapod args:help" "chezmoi args:apply" "recovery-core validation happens before full apply"
assert_contains "$missing_command_surface_log_text" "tpod args:help" "installed command surface is validated with tpod help"
assert_contains "$missing_command_surface_log_text" "chezmoi args:apply" "missing command surface still continues to full apply after recovery-core validation"

dangling_symlink_conflict_case="$(make_case_dir dangling-symlink-command-conflict)"
prepare_resumable_macos_case "$dangling_symlink_conflict_case"
write_complete_setup_config "$dangling_symlink_conflict_case/xdg-config/chezmoi/chezmoi.toml"
ln -s "$dangling_symlink_conflict_case/missing-terrapod" "$dangling_symlink_conflict_case/home/.local/bin/terrapod"
dangling_symlink_conflict_log="$dangling_symlink_conflict_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$dangling_symlink_conflict_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$dangling_symlink_conflict_case"
unset TERRAPOD_STUB_CALL_LOG
assert_failure "$installer_status" "ambiguous dangling symlink command conflict stops installation"
dangling_symlink_conflict_stderr="$(cat "$dangling_symlink_conflict_case/stderr")"
assert_contains "$dangling_symlink_conflict_stderr" "$dangling_symlink_conflict_case/home/.local/bin/terrapod" "dangling symlink conflict guidance identifies path"

installed_tpod_alias_repair_case="$(make_case_dir installed-tpod-alias-repair)"
prepare_resumable_macos_case "$installed_tpod_alias_repair_case"
write_complete_setup_config "$installed_tpod_alias_repair_case/xdg-config/chezmoi/chezmoi.toml"
ln -s terrapod "$installed_tpod_alias_repair_case/home/.local/bin/tpod"
installed_tpod_alias_repair_log="$installed_tpod_alias_repair_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$installed_tpod_alias_repair_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$installed_tpod_alias_repair_case"
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "installed tpod alias is repairable when terrapod is missing"
installed_tpod_alias_repair_log_text="$(cat "$installed_tpod_alias_repair_log")"
assert_contains "$installed_tpod_alias_repair_log_text" "tpod args:help" "installed tpod alias repair validates installed tpod"
assert_contains "$installed_tpod_alias_repair_log_text" "chezmoi args:apply" "installed tpod alias repair continues to full apply"

terrapod_owned_repair_case="$(make_case_dir terrapod-owned-command-repair)"
prepare_resumable_macos_case "$terrapod_owned_repair_case"
write_complete_setup_config "$terrapod_owned_repair_case/xdg-config/chezmoi/chezmoi.toml"
write_installed_terrapod_command_stub "$terrapod_owned_repair_case/home/.local/bin/terrapod" 0
ln -sf terrapod "$terrapod_owned_repair_case/home/.local/bin/tpod"
terrapod_owned_repair_log="$terrapod_owned_repair_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$terrapod_owned_repair_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$terrapod_owned_repair_case"
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "Terrapod-owned command files are repairable"
terrapod_owned_repair_log_text="$(cat "$terrapod_owned_repair_log")"
assert_contains "$terrapod_owned_repair_log_text" "terrapod-owned args:help" "Terrapod-owned repair checks existing help before overwrite"
assert_contains "$terrapod_owned_repair_log_text" "tpod args:help" "Terrapod-owned repair validates installed tpod after recovery-core apply"

source_pointer_repair_case="$(make_case_dir source-pointer-command-repair)"
prepare_resumable_macos_case "$source_pointer_repair_case"
write_complete_setup_config "$source_pointer_repair_case/xdg-config/chezmoi/chezmoi.toml"
ln -s "$source_pointer_repair_case/xdg-data/chezmoi/dot_local/bin/executable_terrapod" "$source_pointer_repair_case/home/.local/bin/terrapod"
ln -sf terrapod "$source_pointer_repair_case/home/.local/bin/tpod"
source_pointer_repair_log="$source_pointer_repair_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$source_pointer_repair_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$source_pointer_repair_case"
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "source-pointer command files are repairable"

source_pointer_file_repair_case="$(make_case_dir source-pointer-file-command-repair)"
prepare_resumable_macos_case "$source_pointer_file_repair_case"
write_complete_setup_config "$source_pointer_file_repair_case/xdg-config/chezmoi/chezmoi.toml"
write_source_pointer_command_file "$source_pointer_file_repair_case/home/.local/bin/terrapod" "$source_pointer_file_repair_case/xdg-data/chezmoi"
write_source_pointer_command_file "$source_pointer_file_repair_case/home/.local/bin/tpod" "$source_pointer_file_repair_case/xdg-data/chezmoi"
source_pointer_file_repair_log="$source_pointer_file_repair_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$source_pointer_file_repair_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$source_pointer_file_repair_case"
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "source-pointer regular command files are repairable"

non_terrapod_conflict_case="$(make_case_dir non-terrapod-command-conflict)"
prepare_resumable_macos_case "$non_terrapod_conflict_case"
write_complete_setup_config "$non_terrapod_conflict_case/xdg-config/chezmoi/chezmoi.toml"
write_non_terrapod_command_stub "$non_terrapod_conflict_case/home/.local/bin/terrapod"
non_terrapod_conflict_log="$non_terrapod_conflict_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$non_terrapod_conflict_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$non_terrapod_conflict_case"
unset TERRAPOD_STUB_CALL_LOG
assert_failure "$installer_status" "non-Terrapod terrapod command conflict stops installation"
non_terrapod_conflict_stderr="$(cat "$non_terrapod_conflict_case/stderr")"
non_terrapod_conflict_log_text="$(cat "$non_terrapod_conflict_log" 2>/dev/null || true)"
assert_contains "$non_terrapod_conflict_stderr" "$non_terrapod_conflict_case/home/.local/bin/terrapod" "non-Terrapod conflict guidance identifies path"
assert_contains "$non_terrapod_conflict_stderr" "Move or remove it, then rerun the Terrapod installer." "non-Terrapod conflict guidance asks user to move or remove"
assert_not_contains "$non_terrapod_conflict_log_text" "chezmoi args:apply" "non-Terrapod conflict stops before full apply"

non_terrapod_tpod_conflict_case="$(make_case_dir non-terrapod-tpod-command-conflict)"
prepare_resumable_macos_case "$non_terrapod_tpod_conflict_case"
write_complete_setup_config "$non_terrapod_tpod_conflict_case/xdg-config/chezmoi/chezmoi.toml"
write_non_terrapod_command_stub "$non_terrapod_tpod_conflict_case/home/.local/bin/tpod"
non_terrapod_tpod_conflict_log="$non_terrapod_tpod_conflict_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$non_terrapod_tpod_conflict_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$non_terrapod_tpod_conflict_case"
unset TERRAPOD_STUB_CALL_LOG
assert_failure "$installer_status" "non-Terrapod tpod command conflict stops installation"
non_terrapod_tpod_conflict_stderr="$(cat "$non_terrapod_tpod_conflict_case/stderr")"
non_terrapod_tpod_conflict_log_text="$(cat "$non_terrapod_tpod_conflict_log" 2>/dev/null || true)"
assert_contains "$non_terrapod_tpod_conflict_stderr" "$non_terrapod_tpod_conflict_case/home/.local/bin/tpod" "non-Terrapod tpod conflict guidance identifies path"
assert_contains "$non_terrapod_tpod_conflict_stderr" "Move or remove it, then rerun the Terrapod installer." "non-Terrapod tpod conflict guidance asks user to move or remove"
assert_not_contains "$non_terrapod_tpod_conflict_log_text" "chezmoi args:apply" "non-Terrapod tpod conflict stops before full apply"

ambiguous_tpod_conflict_case="$(make_case_dir ambiguous-tpod-command-conflict)"
prepare_resumable_macos_case "$ambiguous_tpod_conflict_case"
write_complete_setup_config "$ambiguous_tpod_conflict_case/xdg-config/chezmoi/chezmoi.toml"
cat >"$ambiguous_tpod_conflict_case/home/.local/bin/tpod" <<'AMBIGUOUS_TPOD'
not a script
AMBIGUOUS_TPOD
ambiguous_tpod_conflict_log="$ambiguous_tpod_conflict_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$ambiguous_tpod_conflict_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$ambiguous_tpod_conflict_case"
unset TERRAPOD_STUB_CALL_LOG
assert_failure "$installer_status" "ambiguous tpod command conflict stops installation"
ambiguous_tpod_conflict_stderr="$(cat "$ambiguous_tpod_conflict_case/stderr")"
assert_contains "$ambiguous_tpod_conflict_stderr" "$ambiguous_tpod_conflict_case/home/.local/bin/tpod" "ambiguous command conflict guidance identifies path"

tpod_help_failure_case="$(make_case_dir tpod-help-failure)"
prepare_resumable_macos_case "$tpod_help_failure_case"
write_complete_setup_config "$tpod_help_failure_case/xdg-config/chezmoi/chezmoi.toml"
TPOD_HELP_STUB_STATUS=17
export TPOD_HELP_STUB_STATUS
tpod_help_failure_log="$tpod_help_failure_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$tpod_help_failure_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$tpod_help_failure_case"
unset TERRAPOD_STUB_CALL_LOG
unset TPOD_HELP_STUB_STATUS
assert_failure "$installer_status" "tpod help failure is a hard recovery-core failure"
tpod_help_failure_stderr="$(cat "$tpod_help_failure_case/stderr")"
tpod_help_failure_log_text="$(cat "$tpod_help_failure_log" 2>/dev/null || true)"
assert_contains "$tpod_help_failure_stderr" "tpod help failed after recovery-core apply" "tpod help failure explains recovery-core validation failure"
assert_not_contains "$tpod_help_failure_log_text" "chezmoi args:apply" "tpod help failure stops before full apply"
```

- [ ] **Step 7: Run test to verify it fails**

Run: `sh tests/terrapod_installer_test.sh`

Expected: FAIL because `install.sh` does not yet install or validate recovery-core command surface before full apply and does not reject command-name conflicts.

## Task 2: Implement Conservative Command Ownership And Recovery-Core Apply

**Files:**
- Modify: `install.sh:153-170`
- Modify: `install.sh:880-929`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Add help-output validation helpers**

Replace `installed_tpod_help_works` with helpers that validate canonical Terrapod help output:

```sh
terrapod_help_output_is_valid() {
  help_output="$1"

  printf '%s\n' "$help_output" | grep -F "Terrapod - a small landing pod for your dotfiles" >/dev/null 2>&1 &&
    printf '%s\n' "$help_output" | grep -F "Usage:" >/dev/null 2>&1 &&
    printf '%s\n' "$help_output" | grep -F "tpod apply" >/dev/null 2>&1
}

command_help_is_terrapod() {
  command_path="$1"
  profile="$2"

  [ -x "$command_path" ] || return 1
  if ! help_output="$(TERRAPOD_PROFILE="$profile" "$command_path" help 2>/dev/null)"; then
    return 1
  fi

  terrapod_help_output_is_valid "$help_output"
}

installed_command_surface_is_valid() {
  local_bin_dir="$1"
  profile="$2"
  terrapod_bin="$local_bin_dir/terrapod"
  tpod_bin="$local_bin_dir/tpod"

  command_help_is_terrapod "$terrapod_bin" "$profile" &&
    command_help_is_terrapod "$tpod_bin" "$profile"
}
```

- [ ] **Step 2: Add pointer and conflict helpers**

Add after the help helpers:

```sh
path_points_to_terrapod_source_command() {
  command_path="$1"
  source_dir="$2"
  expected_source="$source_dir/dot_local/bin/executable_terrapod"

  [ -L "$command_path" ] || return 1
  target="$(readlink "$command_path")" || return 1
  case "$target" in
    /*)
      target_path="$target"
      ;;
    *)
      target_path="${command_path%/*}/$target"
      ;;
  esac

  target_dir="${target_path%/*}"
  target_base="${target_path##*/}"
  if ! resolved_dir="$(CDPATH= cd -P -- "$target_dir" 2>/dev/null && pwd -P)"; then
    return 1
  fi
  resolved_target="$resolved_dir/$target_base"

  [ "$resolved_target" = "$expected_source" ]
}

path_points_to_installed_tpod_alias() {
  command_path="$1"

  [ "${command_path##*/}" = "tpod" ] || return 1
  [ -L "$command_path" ] || return 1
  target="$(readlink "$command_path")" || return 1
  case "$target" in
    /*)
      target_path="$target"
      ;;
    *)
      target_path="${command_path%/*}/$target"
      ;;
  esac

  command_dir="${command_path%/*}"
  if ! resolved_command_dir="$(CDPATH= cd -P -- "$command_dir" 2>/dev/null && pwd -P)"; then
    return 1
  fi

  target_dir="${target_path%/*}"
  target_base="${target_path##*/}"
  if ! resolved_target_dir="$(CDPATH= cd -P -- "$target_dir" 2>/dev/null && pwd -P)"; then
    return 1
  fi
  resolved_target="$resolved_target_dir/$target_base"

  [ "$resolved_target" = "$resolved_command_dir/terrapod" ]
}

file_points_to_terrapod_source_command() {
  command_path="$1"
  source_dir="$2"
  expected_exec="exec \"$source_dir/dot_local/bin/executable_terrapod\" \"\$@\""

  [ -L "$command_path" ] && return 1
  [ -f "$command_path" ] || return 1
  awk -v expected_exec="$expected_exec" '
    NR == 1 {
      if ($0 != "#!/bin/sh") {
        exit 1
      }
      next
    }

    NR == 2 {
      if ($0 == expected_exec) {
        found = 1
        next
      }
      exit 1
    }

    $0 !~ /^[[:space:]]*$/ {
      found = 0
      exit 1
    }

    END { exit found ? 0 : 1 }
  ' "$command_path"
}

command_surface_path_is_repairable() {
  command_path="$1"
  source_dir="$2"
  profile="$3"

  if [ -L "$command_path" ]; then
    path_points_to_terrapod_source_command "$command_path" "$source_dir" ||
      path_points_to_installed_tpod_alias "$command_path"
    return $?
  fi

  [ -e "$command_path" ] || return 0

  if file_points_to_terrapod_source_command "$command_path" "$source_dir"; then
    return 0
  fi

  command_help_is_terrapod "$command_path" "$profile"
}

reject_command_surface_conflict() {
  command_path="$1"

  fatal "non-Terrapod command already exists at $command_path. Move or remove it, then rerun the Terrapod installer."
}

ensure_command_surface_path_repairable() {
  command_path="$1"
  source_dir="$2"
  profile="$3"

  if ! command_surface_path_is_repairable "$command_path" "$source_dir" "$profile"; then
    reject_command_surface_conflict "$command_path"
  fi
}
```

The test plan adds `grep`, `ln`, and `readlink` to the constrained `PATH`; do not rely on unlisted external commands.

- [ ] **Step 3: Update the already-installed shortcut**

In `main`, replace:

```sh
  if [ "$source_already_present" = "true" ] && installed_tpod_help_works "$local_bin_dir" "$profile"; then
```

with:

```sh
  if [ "$source_already_present" = "true" ] && installed_command_surface_is_valid "$local_bin_dir" "$profile"; then
```

This prevents `tpod`-only, arbitrary `help`, or missing `terrapod` states from bypassing recovery-core conflict checks.

- [ ] **Step 4: Add recovery-core command surface apply and validation**

Replace `run_initial_apply` area with:

```sh
apply_recovery_core_command_surface() {
  profile="$1"
  source_dir="$2"
  local_bin_dir="$3"
  terrapod_source="$(checked_out_terrapod "$source_dir")"
  terrapod_target="$local_bin_dir/terrapod"
  tpod_target="$local_bin_dir/tpod"

  ensure_command_surface_path_repairable "$terrapod_target" "$source_dir" "$profile"
  ensure_command_surface_path_repairable "$tpod_target" "$source_dir" "$profile"

  rm -f "$terrapod_target" "$tpod_target" ||
    fatal "failed to repair Terrapod command surface under $local_bin_dir"
  cp "$terrapod_source" "$terrapod_target" ||
    fatal "failed to install Terrapod command at $terrapod_target"
  chmod +x "$terrapod_target" ||
    fatal "failed to make Terrapod command executable: $terrapod_target"
  ln -s terrapod "$tpod_target" ||
    fatal "failed to install tpod alias at $tpod_target"

  validate_recovery_core_command_surface "$profile" "$local_bin_dir"
}

validate_recovery_core_command_surface() {
  profile="$1"
  local_bin_dir="$2"
  terrapod_bin="$local_bin_dir/terrapod"
  tpod_bin="$local_bin_dir/tpod"

  [ -x "$terrapod_bin" ] ||
    fatal "terrapod was not installed at $terrapod_bin after recovery-core apply"
  [ -x "$tpod_bin" ] ||
    fatal "tpod was not installed at $tpod_bin after recovery-core apply"
  TERRAPOD_PROFILE="$profile" "$tpod_bin" help >/dev/null 2>&1 ||
    fatal "tpod help failed after recovery-core apply"
}
```

- [ ] **Step 5: Call recovery-core before full apply**

In `main`, insert this before `run_initial_apply "$chezmoi_bin"`:

```sh
  apply_recovery_core_command_surface "$profile" "$source_dir" "$local_bin_dir"
```

The order should be:

```sh
  ensure_first_run_setup "$profile" "$source_dir" "$chezmoi_bin"
  apply_recovery_core_command_surface "$profile" "$source_dir" "$local_bin_dir"
  run_initial_apply "$chezmoi_bin"
  show_first_run_help "$profile" "$local_bin_dir"
```

- [ ] **Step 6: Run focused tests**

Run: `sh -n install.sh && sh tests/terrapod_installer_test.sh`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add install.sh tests/terrapod_installer_test.sh docs/superpowers/plans/2026-06-05-command-surface-recovery.md
git commit -m "feat: add command surface recovery core"
```

## Task 3: Final Validation And Issue Handoff

**Files:**
- Verify: `install.sh`
- Verify: `tests/terrapod_installer_test.sh`
- Verify: `docs/superpowers/plans/2026-06-05-command-surface-recovery.md`

- [ ] **Step 1: Run all relevant shell tests**

Run:

```bash
sh -n install.sh
sh tests/terrapod_installer_test.sh
sh tests/terrapod_command_test.sh
```

Expected: PASS.

- [ ] **Step 2: Inspect diff scope**

Run:

```bash
git status -sb
git diff --stat origin/main...HEAD
git diff --check
```

Expected: only `install.sh`, `tests/terrapod_installer_test.sh`, and this plan file changed; no whitespace errors.

- [ ] **Step 3: Final review**

Dispatch a code reviewer for the full diff with Issue #98 acceptance criteria. Expected: no blocker findings.

- [ ] **Step 4: Publish ready-for-review PR**

Use `github:yeet` flow:

```bash
gh --version
gh auth status
git status -sb
git push -u origin feat/issue-98-command-surface-recovery
```

Create a ready-for-review PR against `main` with a body that covers:

- recovery-core command surface installation and validation
- non-Terrapod conflict handling and guidance
- test coverage for repair, conflict, ambiguous file, and `tpod help` hard failure
