# First-Run Source Resume Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the first-run installer resume from an existing Terrapod Source Repository checkout, reject unrelated chezmoi source directories, and exit safely on already installed Terrapod machines.

**Architecture:** Keep the installer as POSIX `sh` and add small source-state helpers around the existing `main()` flow. A source directory is resumable only when it has expected recovery-core source files and `.git/config` identifies `juty9026/terrapod`; an already installed machine is detected only by successful `~/.local/bin/tpod help`; incomplete resumes either reuse complete managed setup config or rerun Terrapod Setup before continuing to initial apply.

**Tech Stack:** POSIX `sh`, existing shell stubs in `tests/terrapod_installer_test.sh`, GitHub issue #97 acceptance criteria.

---

## Assumptions

- Repository identity can be conservatively checked by reading `.git/config` in the default source directory and accepting only exact HTTPS or SSH remotes for `github.com:juty9026/terrapod.git` or `github.com/juty9026/terrapod.git`.
- Recovery-core source files for resume are `dot_local/bin/executable_terrapod`, `dot_local/bin/symlink_tpod`, `dot_zshenv.tmpl`, `dot_zprofile`, and `dot_zshrc.tmpl`.
- Installer-side managed setup config completeness must mirror the existing `dot_local/bin/executable_terrapod` parser semantics for valid config shapes, including quoted `[data]`, root dotted `data.key = ...`, quoted keys, literal-string `profile` values, and comments.
- Missing or incomplete managed setup config reruns Terrapod Setup; present-but-unusable config paths hard-fail instead of being treated as setup input to overwrite.

## File Structure

- Modify `install.sh`: replace the hard source-directory guard with source-state classification, add `tpod help` installed detection, add setup config completeness helpers, and update `main()` to resume safely.
- Modify `tests/terrapod_installer_test.sh`: extend stubs and add issue #97 regression cases for unrelated sources, resumable Terrapod checkout, SSH origin, already installed state, broken command surface, complete setup config reuse, unsupported config syntax, and incomplete config rerun.
- Create `docs/superpowers/plans/2026-06-04-first-run-source-resume.md`: this implementation plan.

---

### Task 1: Add Installer Regression Tests

**Files:**
- Modify: `tests/terrapod_installer_test.sh`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Add source checkout helper fixtures**

Add helpers near `write_chezmoi_flow_stub()`:

```sh
write_terrapod_source_checkout() {
  source_dir="$1"
  mkdir -p "$source_dir/.git" "$source_dir/dot_local/bin"
  cat >"$source_dir/.git/config" <<'GITCONFIG'
[remote "origin"]
  url = https://github.com/juty9026/terrapod.git
GITCONFIG
  cp "$2" "$source_dir/dot_local/bin/executable_terrapod"
  chmod +x "$source_dir/dot_local/bin/executable_terrapod"
  : >"$source_dir/dot_local/bin/symlink_tpod"
  : >"$source_dir/dot_zshenv.tmpl"
  : >"$source_dir/dot_zprofile"
  : >"$source_dir/dot_zshrc.tmpl"
}
```

Expected: helper creates the exact source files that installer resume accepts.

- [ ] **Step 2: Add installed `tpod` stub helper**

Add:

```sh
write_installed_tpod_stub() {
  path="$1"
  status="$2"
  mkdir -p "$(dirname "$path")"
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' 'set -eu'
    printf '%s\n' 'printf "%s\n" "tpod path:$0" >>"${TERRAPOD_STUB_CALL_LOG:?}"'
    printf '%s\n' 'printf "%s\n" "tpod args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"'
    printf '%s\n' "if [ '$status' != '0' ]; then"
    printf '%s\n' "  exit '$status'"
    printf '%s\n' 'fi'
    printf '%s\n' 'case "${1-}" in'
    printf '%s\n' '  help|--help|-h) printf "%s\n" "installed tpod help output" ;;'
    printf '%s\n' '  *) exit 64 ;;'
    printf '%s\n' 'esac'
  } >"$path"
  chmod +x "$path"
}
```

Expected: tests can prove `tpod help`, not file existence, drives installed detection.

Do not use this environment-dependent shape, because the status is not reliably exported into the child process:

```sh
#!/bin/sh
set -eu
if [ "${TERRAPOD_INSTALLED_TPOD_STATUS:-0}" != "0" ]; then
  exit "$TERRAPOD_INSTALLED_TPOD_STATUS"
fi
```

- [ ] **Step 3: Add complete/incomplete config fixtures**

Add helpers:

```sh
write_complete_setup_config() {
  config_file="$1"
  mkdir -p "$(dirname "$config_file")"
  cat >"$config_file" <<'TOML'
[data]
profile = "macos-terminal"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupAiApps = false
TOML
}

write_incomplete_setup_config() {
  config_file="$1"
  mkdir -p "$(dirname "$config_file")"
  cat >"$config_file" <<'TOML'
[data]
profile = "macos-terminal"
enableEditorStack = false
TOML
}
```

Expected: first fixture skips setup on resume; second reruns setup.

- [ ] **Step 4: Add root dotted complete config fixture**

Add:

```sh
write_root_dotted_complete_setup_config() {
  config_file="$1"
  mkdir -p "$(dirname "$config_file")"
  cat >"$config_file" <<'TOML'
data.profile = "macos-terminal"
data.enableEditorStack = false
data.enableAiCliTools = false
data.enableDevelopmentWorkspace = false
data.enableMacosAppGroupTerminalApps = false
data.enableMacosAppGroupAutomation = false
data.enableMacosAppGroupLauncher = false
data.enableMacosAppGroupMonitoring = false
data.enableMacosAppGroupAiApps = false
TOML
}
```

Expected: installer accepts the same root dotted complete config shape already accepted by `tpod`.

- [ ] **Step 5: Add quoted complete config fixture**

Add:

```sh
write_quoted_complete_setup_config() {
  config_file="$1"
  mkdir -p "$(dirname "$config_file")"
  cat >"$config_file" <<'TOML'
["data"]
"profile" = "macos-terminal" # active profile
"enableEditorStack" = false
"enableAiCliTools" = false
"enableDevelopmentWorkspace" = false
"enableMacosAppGroupTerminalApps" = false
"enableMacosAppGroupAutomation" = false
"enableMacosAppGroupLauncher" = false
"enableMacosAppGroupMonitoring" = false
"enableMacosAppGroupAiApps" = false
TOML
}
```

Also add a complete config fixture with `profile = 'macos-terminal'`.

Expected: installer accepts quoted data section, quoted keys, trailing comments, and TOML literal-string `profile` values like the existing `tpod` parser.

- [ ] **Step 6: Add unrelated source rejection case**

Replace the existing `source_guard_case` body with a case that creates `$XDG_DATA_HOME/chezmoi/.git/config` pointing at another repository plus some arbitrary file.

Also install a working `~/.local/bin/tpod` stub and assert source rejection happens before installed detection:

```sh
write_installed_tpod_stub "$source_guard_case/home/.local/bin/tpod" 0
assert_not_contains "$source_guard_log_text" "tpod args:help" "unrelated source rejection happens before installed detection"
```

Run:

```bash
sh tests/terrapod_installer_test.sh
```

Expected: FAIL before implementation only after new assertions expect Terrapod-specific guidance and no network/chezmoi calls.

- [ ] **Step 7: Add near-miss Terrapod source rejection case**

Create a case whose `.git/config` points at `https://github.com/juty9026/terrapod.git`, but omit one recovery-core source file such as `dot_local/bin/symlink_tpod`.

Assert:

```sh
assert_failure "$installer_status" "Terrapod remote without recovery-core files is rejected"
assert_contains "$stderr_text" "not a resumable Terrapod Source Repository checkout" "near-miss source rejection explains missing resumable state"
assert_no_stub_calls "$log_file" "near-miss source guard runs before network or chezmoi commands"
```

Expected: FAIL before implementation because the existing source guard does not distinguish near-miss Terrapod source state.

- [ ] **Step 8: Add near-miss repository identity rejection case**

Create a case with every recovery-core source file present, but `.git/config` points at `https://github.com/juty9026/terrapod-fork.git`.

Assert:

```sh
assert_failure "$installer_status" "Terrapod-like fork source is rejected"
assert_contains "$stderr_text" "not a resumable Terrapod Source Repository checkout" "Terrapod-like fork rejection explains source identity mismatch"
assert_no_stub_calls "$log_file" "Terrapod-like fork source guard runs before network or chezmoi commands"
```

Expected: FAIL before implementation if repository identity matching is too broad.

- [ ] **Step 9: Add comment-only repository identity rejection case**

Create a case with every recovery-core source file present and this `.git/config`:

```gitconfig
[remote "origin"]
  url = https://github.com/juty9026/dotfiles.git
  # migrated to https://github.com/juty9026/terrapod.git later
```

Assert:

```sh
assert_failure "$installer_status" "legacy source with Terrapod comment is rejected"
assert_contains "$stderr_text" "not a resumable Terrapod Source Repository checkout" "comment-only Terrapod identity is rejected"
assert_no_stub_calls "$log_file" "comment-only identity rejection runs before network or chezmoi commands"
```

Expected: FAIL before implementation if repository identity matching scans non-remote-url text.

- [ ] **Step 10: Add origin fork with canonical upstream rejection case**

Create a case with every recovery-core source file present and this `.git/config`:

```gitconfig
[remote "origin"]
  url = https://github.com/juty9026/terrapod-fork.git
[remote "upstream"]
  url = https://github.com/juty9026/terrapod.git
```

Assert:

```sh
assert_failure "$installer_status" "fork origin with canonical upstream source is rejected"
assert_contains "$stderr_text" "not a resumable Terrapod Source Repository checkout" "canonical upstream does not make fork origin resumable"
assert_no_stub_calls "$log_file" "fork origin source guard runs before network or chezmoi commands"
```

Expected: FAIL before implementation if repository identity matching accepts any remote URL instead of `remote "origin"` only.

- [ ] **Step 11: Add resumable checkout with complete config case**

Create a case with:

- existing default source from `write_terrapod_source_checkout`
- existing user-local `chezmoi`
- `write_complete_setup_config "$case_dir/xdg-config/chezmoi/chezmoi.toml"`
- no installed `tpod`

Assert:

```sh
assert_status "$installer_status" 0 "resumable Terrapod checkout with complete setup config continues first-run"
assert_not_contains "$log_text" "chezmoi args:init" "resume does not reinitialize existing Terrapod source"
assert_not_contains "$log_text" "terrapod args:setup" "resume reuses complete managed setup config"
assert_contains "$log_text" "chezmoi args:apply" "resume runs initial apply after complete setup config"
assert_contains "$log_text" "tpod args:help" "resume validates installed command after apply"
```

Expected: FAIL before implementation because existing source directories hard-fail.

Also add an SSH origin variant by replacing `.git/config` with:

```gitconfig
[remote "origin"]
  url = git@github.com:juty9026/terrapod.git
```

Expected: installer accepts the exact supported SSH origin without rerunning setup.

- [ ] **Step 12: Add profile-mismatched complete config rerun case**

Create a Darwin/macOS case like Step 11, but write a complete config whose `profile = "vps-shell"`.

Assert:

```sh
assert_status "$installer_status" 0 "resumable Terrapod checkout with mismatched setup profile reruns setup"
assert_contains "$log_text" "terrapod TERRAPOD_PROFILE:macos-terminal" "profile mismatch resume keeps detected first-run setup profile"
assert_contains "$log_text" "terrapod args:setup" "profile mismatch resume reruns Terrapod Setup"
assert_first_occurrence_before "$log_text" "terrapod args:setup" "chezmoi args:apply" "profile mismatch resume continues to initial apply after setup"
```

Expected: FAIL before implementation if completeness checks only managed key presence.

- [ ] **Step 13: Add resumable checkout with root dotted complete config case**

Create a case like Step 11, but call `write_root_dotted_complete_setup_config`.

Assert:

```sh
assert_status "$installer_status" 0 "resumable Terrapod checkout accepts root dotted complete setup config"
assert_not_contains "$log_text" "terrapod args:setup" "root dotted complete setup config is reused"
assert_contains "$log_text" "chezmoi args:apply" "root dotted complete setup config continues to apply"
```

Expected: FAIL before implementation if installer parser is narrower than the existing `tpod` parser.

- [ ] **Step 14: Add resumable checkout with quoted complete config case**

Create a case like Step 11, but call `write_quoted_complete_setup_config`.

Assert:

```sh
assert_status "$installer_status" 0 "resumable Terrapod checkout accepts quoted complete setup config"
assert_not_contains "$log_text" "terrapod args:setup" "quoted complete setup config is reused"
assert_contains "$log_text" "chezmoi args:apply" "quoted complete setup config continues to apply"
```

Expected: FAIL before implementation if installer parser ignores quoted sections, quoted keys, or trailing comments.

Also add a case whose complete config uses `profile = 'macos-terminal'`.

Expected: FAIL before implementation if installer compares the raw `profile` token only against a double-quoted spelling.

- [ ] **Step 15: Add unsupported managed config syntax hard-fail cases**

Create a case like Step 11 with a config that includes every managed key plus a TOML multiline string:

```toml
[data]
profile = "macos-terminal"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupAiApps = false
notes = """
unsupported multiline value
"""
```

Assert:

```sh
assert_failure "$installer_status" "resume fails when managed setup config has unsupported multiline syntax"
assert_contains "$stderr_text" "unsupported multiline string in config" "unsupported multiline config gives syntax guidance"
assert_not_contains "$log_text" "terrapod args:setup" "unsupported multiline config is not treated as missing setup config"
assert_not_contains "$log_text" "chezmoi args:apply" "unsupported multiline config does not continue to apply"
```

Expected: FAIL before implementation if installer does not reuse the routine `tpod` unsupported config syntax preflight.

Also add equivalent resume hard-fail cases for root `data = { ... }` inline tables and section-like multiline arrays.

Expected: FAIL before implementation if installer treats unsupported syntax as incomplete setup input and reruns setup.

- [ ] **Step 16: Add resumable checkout with incomplete config case**

Create a case like Step 11, but call `write_incomplete_setup_config`.

Assert:

```sh
assert_status "$installer_status" 0 "resumable Terrapod checkout with incomplete setup config reruns setup"
assert_not_contains "$log_text" "chezmoi args:init" "incomplete resume does not reinitialize source"
assert_contains "$log_text" "terrapod TERRAPOD_PROFILE:macos-terminal" "incomplete resume keeps first-run setup profile"
assert_contains "$log_text" "terrapod TERRAPOD_CHEZMOI_CONFIG:" "incomplete resume keeps first-run setup config override"
assert_contains "$log_text" "terrapod args:setup" "incomplete resume reruns Terrapod Setup"
assert_first_occurrence_before "$log_text" "terrapod args:setup" "chezmoi args:apply" "incomplete resume continues to initial apply after setup"
```

Expected: FAIL before implementation.

- [ ] **Step 17: Add non-regular config hard-fail case**

Create a case with a valid existing Terrapod checkout, then create the config path as a directory:

```sh
mkdir -p "$case_dir/xdg-config/chezmoi/chezmoi.toml"
```

Assert:

```sh
assert_failure "$installer_status" "resume fails when managed setup config path is unusable"
assert_contains "$stderr_text" "config path is not a regular file" "unusable config path gives config guidance"
assert_not_contains "$log_text" "terrapod args:setup" "unusable config is not treated as missing setup config"
assert_not_contains "$log_text" "chezmoi args:apply" "unusable config does not continue to apply"
```

Expected: FAIL before implementation because current source guard stops earlier.

- [ ] **Step 18: Add unreadable config hard-fail case**

Create a case with a valid existing Terrapod checkout, then write the config file and remove read permission:

```sh
write_complete_setup_config "$case_dir/xdg-config/chezmoi/chezmoi.toml"
chmod 000 "$case_dir/xdg-config/chezmoi/chezmoi.toml"
```

Assert:

```sh
assert_failure "$installer_status" "resume fails when managed setup config is unreadable"
assert_contains "$stderr_text" "config path is not readable" "unreadable config path gives config guidance"
assert_not_contains "$log_text" "terrapod args:setup" "unreadable config is not treated as missing setup config"
assert_not_contains "$log_text" "chezmoi args:apply" "unreadable config does not continue to apply"
chmod 600 "$case_dir/xdg-config/chezmoi/chezmoi.toml"
```

Expected: FAIL before implementation because current source guard stops earlier. Restore permission before cleanup so the test cleanup can remove the file.

- [ ] **Step 19: Add already-installed valid command surface case**

Create a case with existing Terrapod checkout, complete config, installed `~/.local/bin/tpod` that exits 0 for `help`.

Assert:

```sh
assert_status "$installer_status" 0 "already installed Terrapod exits successfully"
assert_contains "$stdout_text" "Terrapod is already installed." "already installed case explains state"
assert_contains "$stdout_text" "$case_dir/home/.local/bin/tpod status" "already installed case guides status"
assert_contains "$stdout_text" "$case_dir/home/.local/bin/tpod apply" "already installed case guides routine apply"
assert_contains "$log_text" "tpod args:help" "already installed detection validates tpod help"
assert_not_contains "$log_text" "terrapod args:setup" "already installed case does not rerun setup"
assert_not_contains "$log_text" "chezmoi args:apply" "already installed case does not automatically apply"
```

Expected: FAIL before implementation.

- [ ] **Step 20: Add broken command surface resume case**

Create a case with existing Terrapod checkout, complete config, installed `~/.local/bin/tpod` that exits non-zero for `help`.

Assert:

```sh
assert_status "$installer_status" 0 "broken installed command surface resumes first-run"
assert_contains "$log_text" "tpod args:help" "broken command surface is tested with tpod help"
assert_contains "$log_text" "chezmoi args:apply" "broken command surface resumes apply instead of treating machine as installed"
assert_not_contains "$stdout_text" "Terrapod is already installed." "broken command surface is not reported as already installed"
```

Expected: FAIL before implementation.

- [ ] **Step 21: Commit tests after implementation passes**

Do not commit red tests yet. Commit after Task 3 is green:

```bash
git add tests/terrapod_installer_test.sh
git commit -m "test: cover first-run source resume"
```

---

### Task 2: Implement Source State And Installed Detection

**Files:**
- Modify: `install.sh`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Replace `reject_existing_source_dir` with source classifiers**

Add helpers near the old guard:

```sh
source_dir_exists() {
  [ -e "$1" ] || [ -L "$1" ]
}

source_has_recovery_core_files() {
  source_dir="$1"
  [ -x "$source_dir/dot_local/bin/executable_terrapod" ] &&
    [ -e "$source_dir/dot_local/bin/symlink_tpod" ] &&
    [ -e "$source_dir/dot_zshenv.tmpl" ] &&
    [ -e "$source_dir/dot_zprofile" ] &&
    [ -e "$source_dir/dot_zshrc.tmpl" ]
}

source_has_terrapod_repository_identity() {
  config_file="$1/.git/config"
  [ -f "$config_file" ] &&
    awk -F= '
      /^[[:space:]]*\[/ {
        in_origin = $0 ~ /^[[:space:]]*\[[[:space:]]*remote[[:space:]]+"origin"[[:space:]]*\][[:space:]]*($|#|;)/
      }

      !in_origin {
        next
      }

      /^[[:space:]]*url[[:space:]]*=/ {
        url = $0
        sub(/^[^=]*=/, "", url)
        sub(/^[[:space:]]*/, "", url)
        sub(/[[:space:]]*$/, "", url)
        if (url == "https://github.com/juty9026/terrapod.git" || url == "git@github.com:juty9026/terrapod.git") {
          found = 1
        }
      }
      END { exit found ? 0 : 1 }
    ' "$config_file"
}

source_is_resumable_terrapod_checkout() {
  source_has_recovery_core_files "$1" &&
    source_has_terrapod_repository_identity "$1"
}
```

Expected: these helpers return success only for exact `remote "origin"` `url = ...` entries for the Terrapod Source Repository and reject `terrapod-fork`, canonical `upstream` on fork origins, legacy `juty9026/dotfiles`, comments, or other prefix/suffix slug matches.

- [ ] **Step 2: Add unrelated source rejection guidance**

Add:

```sh
reject_unresumable_source_dir() {
  source_dir="$1"
  fatal "chezmoi source directory already exists but is not a resumable Terrapod Source Repository checkout: $source_dir. Move it aside before first-run install, or run Terrapod from a checked-out juty9026/terrapod source repository."
}
```

Expected: arbitrary existing chezmoi source directories still hard-fail before network or apply.

- [ ] **Step 3: Add `tpod help` installed validation**

Add:

```sh
installed_tpod_help_works() {
  local_bin_dir="$1"
  tpod_bin="$local_bin_dir/tpod"

  [ -x "$tpod_bin" ] &&
    TERRAPOD_PROFILE="$2" "$tpod_bin" help >/dev/null 2>&1
}

print_already_installed_guidance() {
  local_bin_dir="$1"
  printf '%s\n' "Terrapod is already installed."
  printf '%s\n' "Routine commands:"
  printf '%s\n' "  $local_bin_dir/tpod status"
  printf '%s\n' "  $local_bin_dir/tpod apply"
  printf '%s\n' "  $local_bin_dir/tpod help"
}
```

Expected: file existence alone is insufficient; failed `tpod help` falls through to resume.

- [ ] **Step 4: Update `main()` source handling**

In `main()`, replace the existing guard/init sequence with:

```sh
ensure_user_local_bin "$local_bin_dir"
source_already_present=false
if source_dir_exists "$source_dir"; then
  if ! source_is_resumable_terrapod_checkout "$source_dir"; then
    reject_unresumable_source_dir "$source_dir"
  fi
  source_already_present=true
fi

chezmoi_bin="$(install_chezmoi_if_needed "$local_bin_dir")"
ensure_source_repo_prerequisites "$profile"
if [ "$source_already_present" = "false" ]; then
  initialize_source_repository "$chezmoi_bin"
fi
```

Expected: new installs still initialize; resumable checkouts skip `chezmoi init`.

- [ ] **Step 5: Add already-installed early exit**

After source classification and before setup/apply:

```sh
if [ "$source_already_present" = "true" ] && installed_tpod_help_works "$local_bin_dir" "$profile"; then
  print_already_installed_guidance "$local_bin_dir"
  return 0
fi
```

Expected: rerunning first-run on an installed Terrapod machine exits without `tpod apply`.

- [ ] **Step 6: Run focused test**

Run:

```bash
sh tests/terrapod_installer_test.sh
```

Expected: source rejection, resume, and already-installed assertions pass once Task 3 is also implemented.

---

### Task 3: Gate Resume Setup By Managed Config Completeness

**Files:**
- Modify: `install.sh`
- Test: `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Add installer config path helper**

Add:

```sh
chezmoi_config_file() {
  if [ -n "${XDG_CONFIG_HOME:-}" ]; then
    printf '%s\n' "$XDG_CONFIG_HOME/chezmoi/chezmoi.toml"
  else
    printf '%s\n' "$HOME/.config/chezmoi/chezmoi.toml"
  fi
}
```

Expected: installer checks the same default managed setup config path used by `tpod`.

- [ ] **Step 2: Add config path state helper**

Add:

```sh
config_file_state() {
  config_file="$1"

  if [ -L "$config_file" ] || [ -e "$config_file" ]; then
    if [ ! -f "$config_file" ]; then
      printf '%s\n' "non-regular"
    elif [ ! -r "$config_file" ]; then
      printf '%s\n' "unreadable"
    else
      printf '%s\n' "readable"
    fi
  else
    printf '%s\n' "missing"
  fi
}
```

Expected: existing directory or unreadable config path is separated from missing config.

- [ ] **Step 3: Add managed key list and parser from `tpod`**

Add `managed_setup_keys()` from `dot_local/bin/executable_terrapod`, then copy the existing `config_data_value()` and `config_data_key_present()` implementations from `dot_local/bin/executable_terrapod` into `install.sh`.

The copied parser must support these existing valid forms:

```toml
[data]
profile = "macos-terminal"
"enableEditorStack" = false
```

```toml
data.profile = "macos-terminal"
data.enableEditorStack = false
```

config_data_key_present() {
  config_data_value "$1" "$2" >/dev/null 2>&1
}

toml_string_value_matches() {
  value="$1"
  expected="$2"

  [ "$value" = "\"$expected\"" ] || [ "$value" = "'$expected'" ]
}
```

Expected: installer completeness accepts the same complete managed config shapes as routine `tpod` commands.

- [ ] **Step 4: Add completeness and unusable-path checks**

Add:

```sh
reject_unsupported_managed_config_syntax() {
  config_file="$1"

  if problem_message="$(unsupported_managed_config_problem_message "$config_file")"; then
    fatal "$problem_message"
  fi
}

managed_setup_config_path_is_usable_for_resume() {
  config_file="$1"
  case "$(config_file_state "$config_file")" in
    missing|readable)
      return 0
      ;;
    non-regular)
      fatal "config path is not a regular file: $config_file"
      ;;
    unreadable)
      fatal "config path is not readable: $config_file"
      ;;
  esac
}

managed_setup_config_complete() {
  config_file="$1"
  expected_profile="$2"

  [ -f "$config_file" ] || return 1
  setup_profile="$(config_data_value "$config_file" profile)" || return 1
  toml_string_value_matches "$setup_profile" "$expected_profile" || return 1

  for key in $(managed_setup_keys); do
    config_data_key_present "$config_file" "$key" || return 1
  done
}
```

Copy `unsupported_managed_config_problem_message()`, `config_has_unsupported_inline_data_table()`, `config_has_unsupported_multiline_strings()`, and `config_has_section_like_multiline_arrays()` from `dot_local/bin/executable_terrapod` into `install.sh` so first-run resume uses the same unsupported config syntax contract as routine `tpod` commands.

Expected: missing config, incomplete config, or complete config for a different `profile` causes setup rerun; unusable present config and unsupported managed config syntax hard-fail.

- [ ] **Step 5: Add first-run setup gating helper**

Add:

```sh
ensure_first_run_setup() {
  profile="$1"
  source_dir="$2"
  config_file="$(chezmoi_config_file)"

  managed_setup_config_path_is_usable_for_resume "$config_file"
  reject_unsupported_managed_config_syntax "$config_file"

  if managed_setup_config_complete "$config_file" "$profile"; then
    printf '%s\n' "terrapod installer: Reusing complete managed Terrapod Setup config: $config_file"
    return 0
  fi

  run_terrapod_setup "$profile" "$source_dir"
}
```

Expected: complete config skips setup; incomplete config reruns setup in first-run context.

- [ ] **Step 6: Update `main()` setup call**

Replace:

```sh
run_terrapod_setup "$profile" "$source_dir"
```

with:

```sh
ensure_first_run_setup "$profile" "$source_dir"
```

Expected: new first-run with no config still runs setup; resume with complete config reuses it.

- [ ] **Step 7: Run focused installer tests**

Run:

```bash
sh tests/terrapod_installer_test.sh
```

Expected: all installer tests pass.

- [ ] **Step 8: Commit implementation**

```bash
git add install.sh tests/terrapod_installer_test.sh docs/superpowers/plans/2026-06-04-first-run-source-resume.md
git commit -m "fix: resume first-run from Terrapod source"
```

---

### Task 4: Final Verification And PR Prep

**Files:**
- Test: `install.sh`, `tests/terrapod_installer_test.sh`, `tests/terrapod_command_test.sh`

- [ ] **Step 1: Run syntax checks**

```bash
sh -n install.sh
sh -n tests/terrapod_installer_test.sh
```

Expected: both commands exit 0.

- [ ] **Step 2: Run focused regression tests**

```bash
sh tests/terrapod_installer_test.sh
sh tests/terrapod_command_test.sh
```

Expected: both scripts exit 0.

- [ ] **Step 3: Run full shell test suite**

```bash
for test_file in tests/*.sh tests/*.zsh; do
  case "$test_file" in
    *.zsh) zsh "$test_file" ;;
    *) sh "$test_file" ;;
  esac
done
```

Expected: every test exits 0.

- [ ] **Step 4: Check diff scope**

```bash
git diff --check
git status -sb
```

Expected: no whitespace errors; changed files are limited to `install.sh`, `tests/terrapod_installer_test.sh`, and this plan.

- [ ] **Step 5: Publish Ready for review PR**

Use `github:yeet` semantics but create a ready PR because the user requested Ready for review:

```bash
gh --version
gh auth status
git status -sb
git push -u origin fix/issue-97-first-run-source-resume
gh pr create --title "Fix first-run source resume detection" --body-file <tmp-body>
```

Expected: PR is created against the default branch and is not draft.
