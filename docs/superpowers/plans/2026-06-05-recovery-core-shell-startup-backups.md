# Recovery-Core Shell Startup Backups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make first-run recovery-core shell startup overwrite safe by backing up different existing `.zshenv`, `.zprofile`, and `.zshrc` files before a bounded forced chezmoi apply, while keeping routine `tpod apply` interactive.

**Architecture:** Keep the new behavior inside the POSIX first-run installer after **Terrapod Setup** and before full declared-state apply. Use `chezmoi cat` to render each recovery-core target for exact comparison, copy only different existing user content to timestamped sibling backups, then run `chezmoi apply --force` only for the three shell startup targets. Leave `dot_local/bin/executable_terrapod` routine `apply` unchanged so day-to-day `tpod apply` continues to delegate to normal interactive chezmoi behavior.

**Tech Stack:** POSIX `sh`, `chezmoi cat`, `chezmoi apply --force`, `cmp`, `cp`, `date`, existing shell tests in `tests/terrapod_installer_test.sh`, existing config command tests in `tests/terrapod_config_test.sh`.

---

## Assumptions

- Issue #98 already installed or repaired the recovery-core command surface; this issue adds the shell startup portion of the same first-run recovery-core phase.
- Recovery-core shell startup files are exactly `.zshenv`, `.zprofile`, and `.zshrc`, corresponding to source files `dot_zshenv.tmpl`, `dot_zprofile`, and `dot_zshrc.tmpl`.
- Backup comparison must use rendered target content from `chezmoi cat`, not source template bytes.
- Missing target files do not need backups because no user content exists.
- Existing target files identical to rendered target state do not need backups.
- Backup retention is passive: Terrapod creates and reports backup paths, but does not merge, delete, or later prune those files.
- Vendor-installer shell edits are not migrated automatically. Guidance should point users to the backup files and the managed zsh extension point loaded from `~/.config/zsh/path.d/*.zsh`.
- Routine `tpod apply` remains normal `chezmoi apply`; no `--force`, backup, or shell-startup special case is added to `dot_local/bin/executable_terrapod`.

## File Structure

- Modify `install.sh`
  - Add recovery-core shell startup constants/helpers near the existing recovery-core command surface helpers.
  - Add backup comparison using `"$chezmoi_bin" cat "$target"` plus `cmp -s`.
  - Add a bounded forced apply using `"$chezmoi_bin" apply --force "$HOME/.zshenv" "$HOME/.zprofile" "$HOME/.zshrc"`.
  - Call shell startup recovery after `apply_recovery_core_command_surface` and before `run_initial_apply`.
- Modify `tests/terrapod_installer_test.sh`
  - Add `cmp` and `date` to the constrained safe PATH.
  - Teach the `chezmoi` stub to render shell startup targets through `cat` and to handle bounded `apply --force` target writes.
  - Add focused first-run cases for backup creation, backup omission, target limiting, reporting, and no automatic cleanup.
- Modify `tests/terrapod_config_test.sh`
  - Extend the existing no-shell-startup-backup assertion to include `.zshenv.terrapod-backup-*`.
- No change to `dot_local/bin/executable_terrapod`
  - Routine `tpod apply` should remain `run_chezmoi_command apply` without `--force`.
- Optional docs only if tests show user-facing output needs a stable prose home:
  - `README.md`
  - `README.ko.md`

## Task 1: Installer Tests for Bounded Shell Startup Backups

**Files:**
- Modify: `tests/terrapod_installer_test.sh`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Add command dependencies to safe PATH**

Change the safe command loop near the top of `tests/terrapod_installer_test.sh` from:

```sh
for command_name in awk cat chmod cp grep ln mkdir mktemp readlink rm; do
```

to:

```sh
for command_name in awk cat chmod cmp cp date grep ln mkdir mktemp readlink rm; do
```

- [ ] **Step 2: Add shell startup content helpers in the test harness**

Add these helpers after `assert_no_stub_calls`:

```sh
managed_shell_startup_content() {
  target="$1"

  case "${target##*/}" in
    .zshenv)
      printf '%s\n' "managed zshenv"
      ;;
    .zprofile)
      printf '%s\n' "managed zprofile"
      ;;
    .zshrc)
      printf '%s\n' "managed zshrc"
      ;;
    *)
      return 1
      ;;
  esac
}

single_shell_backup_path() {
  target="$1"
  message="$2"

  set -- "$target".terrapod-backup-*
  if [ "$1" = "$target.terrapod-backup-*" ]; then
    fail "$message; expected one backup, found 0"
  fi
  if [ "$#" -ne 1 ]; then
    fail "$message; expected one backup, found $#"
  fi

  printf '%s\n' "$1"
}

assert_shell_backup_path_is_timestamped() {
  target="$1"
  backup_path="$2"
  message="$3"

  suffix="${backup_path#"$target.terrapod-backup-"}"
  timestamp="${suffix%-*}"
  pid="${suffix##*-}"

  case "$timestamp" in
    [0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z)
      ;;
    *)
      fail "$message; backup path is not timestamped: $backup_path"
      ;;
  esac

  case "$pid" in
    ""|*[!0-9]*)
      fail "$message; backup path does not include numeric process suffix: $backup_path"
      ;;
  esac

  pass "$message"
}

assert_single_shell_backup_matches() {
  target="$1"
  expected_content="$2"
  message="$3"
  backup_path="$(single_shell_backup_path "$target" "$message")"

  if ! cmp -s "$backup_path" "$expected_content"; then
    printf '%s\n' "expected backup contents:" >&2
    sed 's/^/  /' "$expected_content" >&2
    printf '%s\n' "actual backup contents:" >&2
    sed 's/^/  /' "$backup_path" >&2
    fail "$message"
  fi

  pass "$message"
}

assert_no_shell_backup_for() {
  target="$1"
  message="$2"

  set -- "$target".terrapod-backup-*
  if [ "$1" != "$target.terrapod-backup-*" ]; then
    fail "$message"
  fi

  pass "$message"
}
```

- [ ] **Step 3: Teach the chezmoi stub to render recovery-core targets**

In `write_chezmoi_flow_stub`, add a `cat)` branch before `apply)`:

```sh
  cat)
    target="${2-}"
    case "${target##*/}" in
      .zshenv)
        printf '%s\n' "managed zshenv"
        ;;
      .zprofile)
        printf '%s\n' "managed zprofile"
        ;;
      .zshrc)
        printf '%s\n' "managed zshrc"
        ;;
      *)
        printf '%s\n' "unexpected chezmoi cat target:$target" >>"$log_file"
        exit 64
        ;;
    esac
    ;;
```

- [ ] **Step 4: Teach the chezmoi stub to handle bounded forced apply**

At the start of the existing `apply)` branch in `write_chezmoi_flow_stub`, before the full-apply command-surface stub body, add:

```sh
    if [ "${1-}" = "--force" ]; then
      shift
      while [ "$#" -gt 0 ]; do
        target="$1"
        case "${target##*/}" in
          .zshenv)
            printf '%s\n' "managed zshenv" >"$target"
            ;;
          .zprofile)
            printf '%s\n' "managed zprofile" >"$target"
            ;;
          .zshrc)
            printf '%s\n' "managed zshrc" >"$target"
            ;;
          *)
            printf '%s\n' "unexpected recovery-core apply target:$target" >>"$log_file"
            exit 64
            ;;
        esac
        shift
      done
      exit 0
    fi
```

- [ ] **Step 5: Write the backup creation test**

Append after the existing recovery-core command-surface success tests:

```sh
shell_backup_case="$(make_case_dir shell-startup-backups)"
prepare_resumable_macos_case "$shell_backup_case"
write_complete_setup_config "$shell_backup_case/xdg-config/chezmoi/chezmoi.toml"
printf '%s\n' "user zshenv" >"$shell_backup_case/home/.zshenv"
printf '%s\n' "user zprofile" >"$shell_backup_case/home/.zprofile"
printf '%s\n' "user zshrc" >"$shell_backup_case/home/.zshrc"
printf '%s\n' "unmanaged bashrc" >"$shell_backup_case/home/.bashrc"
printf '%s\n' "user zshenv" >"$shell_backup_case/expected-zshenv"
printf '%s\n' "user zprofile" >"$shell_backup_case/expected-zprofile"
printf '%s\n' "user zshrc" >"$shell_backup_case/expected-zshrc"
shell_backup_log="$shell_backup_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$shell_backup_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$shell_backup_case"
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "different shell startup files are backed up before first-run forced apply"
shell_backup_stdout="$(cat "$shell_backup_case/stdout")"
shell_backup_log_text="$(cat "$shell_backup_log")"
assert_contains "$shell_backup_log_text" "chezmoi args:cat $shell_backup_case/home/.zshenv" "recovery-core compares rendered .zshenv"
assert_contains "$shell_backup_log_text" "chezmoi args:cat $shell_backup_case/home/.zprofile" "recovery-core compares rendered .zprofile"
assert_contains "$shell_backup_log_text" "chezmoi args:cat $shell_backup_case/home/.zshrc" "recovery-core compares rendered .zshrc"
assert_contains "$shell_backup_log_text" "chezmoi args:apply --force $shell_backup_case/home/.zshenv $shell_backup_case/home/.zprofile $shell_backup_case/home/.zshrc" "recovery-core force apply is bounded to shell startup targets"
assert_first_occurrence_before "$shell_backup_log_text" "terrapod args:setup" "chezmoi args:apply --force" "shell startup force apply runs after Terrapod Setup"
assert_first_occurrence_before "$shell_backup_log_text" "chezmoi args:apply --force" "tpod args:help" "shell startup force apply runs before final help validation"
assert_single_shell_backup_matches "$shell_backup_case/home/.zshenv" "$shell_backup_case/expected-zshenv" "different .zshenv content is backed up"
assert_single_shell_backup_matches "$shell_backup_case/home/.zprofile" "$shell_backup_case/expected-zprofile" "different .zprofile content is backed up"
assert_single_shell_backup_matches "$shell_backup_case/home/.zshrc" "$shell_backup_case/expected-zshrc" "different .zshrc content is backed up"
zshenv_backup="$(single_shell_backup_path "$shell_backup_case/home/.zshenv" "find .zshenv backup path")"
zprofile_backup="$(single_shell_backup_path "$shell_backup_case/home/.zprofile" "find .zprofile backup path")"
zshrc_backup="$(single_shell_backup_path "$shell_backup_case/home/.zshrc" "find .zshrc backup path")"
assert_shell_backup_path_is_timestamped "$shell_backup_case/home/.zshenv" "$zshenv_backup" ".zshenv backup path is timestamped"
assert_shell_backup_path_is_timestamped "$shell_backup_case/home/.zprofile" "$zprofile_backup" ".zprofile backup path is timestamped"
assert_shell_backup_path_is_timestamped "$shell_backup_case/home/.zshrc" "$zshrc_backup" ".zshrc backup path is timestamped"
assert_no_shell_backup_for "$shell_backup_case/home/.bashrc" "non-recovery shell startup file is not backed up"
assert_contains "$(cat "$shell_backup_case/home/.zshenv")" "managed zshenv" "forced apply writes managed .zshenv"
assert_contains "$(cat "$shell_backup_case/home/.zprofile")" "managed zprofile" "forced apply writes managed .zprofile"
assert_contains "$(cat "$shell_backup_case/home/.zshrc")" "managed zshrc" "forced apply writes managed .zshrc"
assert_contains "$shell_backup_stdout" "Shell startup backups created:" "installer reports shell startup backup heading"
assert_contains "$shell_backup_stdout" "$zshenv_backup" "installer reports exact .zshenv backup path"
assert_contains "$shell_backup_stdout" "$zprofile_backup" "installer reports exact .zprofile backup path"
assert_contains "$shell_backup_stdout" "$zshrc_backup" "installer reports exact .zshrc backup path"
assert_contains "$shell_backup_stdout" "Terrapod does not merge or delete these backups automatically." "installer explains backup retention"
assert_contains "$shell_backup_stdout" "Review backups for vendor-installer shell startup edits" "installer explains vendor edits are not migrated"
assert_contains "$shell_backup_stdout" "$shell_backup_case/home/.config/zsh/path.d/*.zsh" "installer points to the managed zsh extension point"
```

- [ ] **Step 6: Write the backup omission test**

Append after the backup creation test:

```sh
shell_no_backup_case="$(make_case_dir shell-startup-no-backups)"
prepare_resumable_macos_case "$shell_no_backup_case"
write_complete_setup_config "$shell_no_backup_case/xdg-config/chezmoi/chezmoi.toml"
printf '%s\n' "managed zprofile" >"$shell_no_backup_case/home/.zprofile"
printf '%s\n' "managed zshrc" >"$shell_no_backup_case/home/.zshrc"
shell_no_backup_log="$shell_no_backup_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$shell_no_backup_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$shell_no_backup_case"
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "missing and identical shell startup files do not create backups"
shell_no_backup_stdout="$(cat "$shell_no_backup_case/stdout")"
assert_no_shell_backup_for "$shell_no_backup_case/home/.zshenv" "missing .zshenv does not create a backup"
assert_no_shell_backup_for "$shell_no_backup_case/home/.zprofile" "identical .zprofile does not create a backup"
assert_no_shell_backup_for "$shell_no_backup_case/home/.zshrc" "identical .zshrc does not create a backup"
assert_not_contains "$shell_no_backup_stdout" "Shell startup backups created:" "installer omits backup report when no backups are created"
```

- [ ] **Step 7: Run the installer test and confirm it fails**

Run:

```bash
tests/terrapod_installer_test.sh
```

Expected: FAIL because `install.sh` has not yet called `chezmoi cat`, created shell startup backups, or run bounded `chezmoi apply --force`.

## Task 2: Implement Recovery-Core Shell Startup Apply

**Files:**
- Modify: `install.sh`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Add shell startup helpers**

Add these helpers after `validate_recovery_core_command_surface`:

```sh
shell_startup_backup_timestamp() {
  date -u +%Y%m%dT%H%M%SZ
}

append_line() {
  current="$1"
  line="$2"

  if [ -n "$current" ]; then
    printf '%s\n%s\n' "$current" "$line"
  else
    printf '%s\n' "$line"
  fi
}

backup_shell_startup_if_different() {
  chezmoi_bin="$1"
  target="$2"

  [ -f "$target" ] || return 0

  rendered_file="$(mktemp)" ||
    fatal "failed to create temporary file for shell startup comparison"
  if ! "$chezmoi_bin" cat "$target" >"$rendered_file"; then
    rm -f "$rendered_file"
    fatal "failed to render managed shell startup file before backup: $target"
  fi

  if cmp -s "$target" "$rendered_file"; then
    rm -f "$rendered_file"
    return 0
  fi
  rm -f "$rendered_file"

  backup_file="$target.terrapod-backup-$(shell_startup_backup_timestamp)-$$"
  cp "$target" "$backup_file" ||
    fatal "failed to back up shell startup file before first-run overwrite: $target"
  printf '%s\n' "$backup_file"
}

backup_recovery_core_shell_startup_files() {
  chezmoi_bin="$1"
  backup_paths=""

  for target in "$HOME/.zshenv" "$HOME/.zprofile" "$HOME/.zshrc"; do
    if backup_path="$(backup_shell_startup_if_different "$chezmoi_bin" "$target")"; then
      if [ -n "$backup_path" ]; then
        backup_paths="$(append_line "$backup_paths" "$backup_path")"
      fi
    else
      return 1
    fi
  done

  printf '%s' "$backup_paths"
}
```

- [ ] **Step 2: Add backup reporting guidance**

Add:

```sh
report_shell_startup_backups() {
  backup_paths="$1"

  [ -n "$backup_paths" ] || return 0

  printf '%s\n' "terrapod installer: Shell startup backups created:"
  printf '%s\n' "$backup_paths" | while IFS= read -r backup_path; do
    printf '%s\n' "terrapod installer:   $backup_path"
  done
  printf '%s\n' "terrapod installer: Terrapod does not merge or delete these backups automatically."
  printf '%s\n' "terrapod installer: Review backups for vendor-installer shell startup edits; Terrapod does not migrate them automatically."
  printf '%s\n' "terrapod installer: Move machine-local PATH or shell snippets into $HOME/.config/zsh/path.d/*.zsh before relying on managed shell startup files."
}
```

- [ ] **Step 3: Add bounded forced apply helper**

Add:

```sh
apply_recovery_core_shell_startup_files() {
  chezmoi_bin="$1"

  backup_paths="$(backup_recovery_core_shell_startup_files "$chezmoi_bin")"
  report_shell_startup_backups "$backup_paths"

  "$chezmoi_bin" apply --force "$HOME/.zshenv" "$HOME/.zprofile" "$HOME/.zshrc" ||
    fatal "failed to apply recovery-core shell startup files"
}
```

- [ ] **Step 4: Wire the helper into first-run only**

In `main`, call the helper after command surface recovery and before full initial apply:

```sh
  apply_recovery_core_command_surface "$profile" "$source_dir" "$local_bin_dir"
  apply_recovery_core_shell_startup_files "$chezmoi_bin"
  run_initial_apply "$chezmoi_bin"
```

Do not change `run_initial_apply`; it should remain:

```sh
"$chezmoi_bin" apply || fatal "chezmoi apply failed"
```

Do not change `dot_local/bin/executable_terrapod` routine `run_apply`; it should continue to call:

```sh
run_chezmoi_command apply
```

- [ ] **Step 5: Run the focused installer test**

Run:

```bash
tests/terrapod_installer_test.sh
```

Expected: PASS.

## Task 3: Config-Only Regression Coverage

**Files:**
- Modify: `tests/terrapod_config_test.sh`
- Test: `tests/terrapod_config_test.sh`

- [ ] **Step 1: Extend the no shell startup backup assertion**

In `assert_no_shell_startup_backups_under`, include `.zshenv` in the search expression:

```sh
      \( -name '.zshenv.terrapod-backup-*' \
      -o -name '.zshrc.terrapod-backup-*' \
      -o -name '.zprofile.terrapod-backup-*' \
      -o -name '.bashrc.terrapod-backup-*' \
      -o -name '.bash_profile.terrapod-backup-*' \
      -o -name '.profile.terrapod-backup-*' \) \
```

Existing calls already assert `terrapod configure` and `terrapod setup` do not create shell startup backups. This small extension covers `.zshenv` explicitly.

- [ ] **Step 2: Run the config test**

Run:

```bash
tests/terrapod_config_test.sh
```

Expected: PASS.

## Task 4: Routine Apply and Documentation Validation

**Files:**
- Inspect only unless a test failure requires a minimal edit:
  - `dot_local/bin/executable_terrapod`
  - `README.md`
  - `README.ko.md`
- Test:
  - `tests/terrapod_command_test.sh`
  - `tests/readme_korean_test.sh`

- [ ] **Step 1: Verify routine apply still delegates normally**

Run:

```bash
rg -n 'run_chezmoi_command apply|apply --force|Shell startup backups created|terrapod-backup' dot_local/bin/executable_terrapod install.sh
```

Expected:
- `dot_local/bin/executable_terrapod` still has `run_chezmoi_command apply`.
- `dot_local/bin/executable_terrapod` has no shell startup backup code and no `apply --force`.
- `install.sh` is the only file with shell startup backup and `apply --force` behavior.

- [ ] **Step 2: Run routine command tests**

Run:

```bash
tests/terrapod_command_test.sh
```

Expected: PASS. This preserves routine `tpod apply` behavior and command-surface validation.

- [ ] **Step 3: Decide whether README edits are necessary**

If installer output tests already cover backup paths, retention, vendor edit non-migration, and extension-point guidance, do not edit README. If reviewer finds user-facing guidance too transient, add a concise paragraph to both `README.md` and `README.ko.md` under first-run setup:

English:

```md
During first-run only, Terrapod force-applies the managed zsh startup files
`.zshenv`, `.zprofile`, and `.zshrc` after Terrapod Setup. If those files
already contain different user content, the installer leaves timestamped
`.terrapod-backup-*` files beside them and prints the backup paths. Terrapod
does not merge or delete those backups automatically; review them manually and
move machine-local PATH snippets into `~/.config/zsh/path.d/*.zsh`, which is
loaded by the managed `.zshenv`.
```

Korean:

```md
first-run에서만 Terrapod은 Terrapod Setup 이후 managed zsh startup file인
`.zshenv`, `.zprofile`, `.zshrc`를 force-apply합니다. 해당 file에 다른 user
content가 있으면 installer는 같은 위치에 timestamp가 붙은
`.terrapod-backup-*` file을 남기고 backup path를 출력합니다. Terrapod은 이
backup을 자동 merge/delete하지 않습니다. backup을 직접 확인하고 machine-local
PATH snippet은 managed `.zshenv`가 load하는 `~/.config/zsh/path.d/*.zsh`로
옮기세요.
```

- [ ] **Step 4: Run README parity test if docs changed**

Run only if README files are edited:

```bash
tests/readme_korean_test.sh
```

Expected: PASS.

## Final Verification

- [ ] Run all touched test scripts:

```bash
tests/terrapod_installer_test.sh
tests/terrapod_config_test.sh
tests/terrapod_command_test.sh
```

- [ ] Run shell syntax checks:

```bash
sh -n install.sh
sh -n tests/terrapod_installer_test.sh
sh -n tests/terrapod_config_test.sh
sh -n dot_local/bin/executable_terrapod
```

- [ ] Confirm the diff is scoped to Issue #99:

```bash
git diff -- install.sh tests/terrapod_installer_test.sh tests/terrapod_config_test.sh docs/superpowers/plans/2026-06-05-recovery-core-shell-startup-backups.md
git diff --stat
```
