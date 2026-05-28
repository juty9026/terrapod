# Terrapod Status And Doctor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `terrapod status` and `terrapod doctor` commands that explain the current machine through repository domain language without running broad package upgrade commands.

**Architecture:** Keep implementation inside the existing POSIX shell Terrapod entry point. Add small helpers for supported profile detection, conservative managed `[data]` reads, tool availability summaries, status rendering, and doctor validation. Extend the existing shell test file with stubbed `uname`, `chezmoi`, package-manager, and tool commands.

**Tech Stack:** POSIX `sh`, `awk`, existing shell test scripts, `chezmoi` only in tests that already depend on it.

---

## File Structure

- Modify `dot_local/bin/executable_terrapod`: add `status` and `doctor` command dispatch plus shared helper functions.
- Modify `tests/terrapod_command_test.sh`: add command tests for help text, macOS status, Ubuntu 24.04 status, unsupported Linux status, disabled optional stacks, enabled optional stacks with missing tools, doctor validation, and broad-upgrade non-invocation.
- Leave `tests/terrapod_config_test.sh` unchanged: config-writing behavior already covers Preset expansion and managed key persistence.

## Task 1: Add failing status tests

**Files:**
- Modify: `tests/terrapod_command_test.sh`
- Test: `tests/terrapod_command_test.sh`

- [ ] **Step 1: Add status test helpers after `assert_call_args`**

```sh
write_success_stub() {
  name="$1"
  write_stub "$tmp_dir/bin/$name" \
    'exit 0'
}

write_failing_command_stub() {
  name="$1"
  call_file="$2"
  write_stub "$tmp_dir/bin/$name" \
    'printf "%s\n" "$0 $*" >>"'"$call_file"'"' \
    'exit 90'
}

write_os_release() {
  path="$1"
  id="$2"
  version_id="$3"
  pretty_name="$4"

  cat >"$path" <<EOF
ID=$id
VERSION_ID="$version_id"
PRETTY_NAME="$pretty_name"
EOF
}
```

- [ ] **Step 2: Update the help assertions**

Add these assertions after the existing update help assertions:

```sh
assert_contains \
  "$help_output" \
  "terrapod status" \
  "Terrapod help documents status"

assert_contains \
  "$help_output" \
  "terrapod doctor" \
  "Terrapod help documents doctor"
```

- [ ] **Step 3: Add macOS status test before the update tests**

```sh
status_config="$tmp_dir/status-macos.toml"
cat >"$status_config" <<'TOML'
[data]
enableEditorStack = true
enableAiCliTools = true
enableDevelopmentWorkspace = true
enableMacosAppGroupTerminalApps = true
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = true
enableMacosAppGroupMonitoring = false
TOML

for tool in chezmoi git zsh mise brew nvim gemini claude codex zellij ghostty cmux op; do
  write_success_stub "$tool"
done

macos_status_output="$(
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$status_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
    sh "$terrapod" status
)"

assert_contains "$macos_status_output" "Terrapod status" "Terrapod status prints a command heading"
assert_contains "$macos_status_output" "Profile: macOS Terminal Profile" "Terrapod status reports macOS Terminal Profile context"
assert_contains "$macos_status_output" "Config: $status_config (present)" "Terrapod status reports explicit config path"
assert_contains "$macos_status_output" "Optional Editor Stack: enabled (tools available: nvim)" "Terrapod status reports enabled Optional Editor Stack tool state"
assert_contains "$macos_status_output" "Optional AI Tool Stack: enabled (tools available: gemini, claude, codex)" "Terrapod status reports enabled Optional AI Tool Stack tool state"
assert_contains "$macos_status_output" "Optional Development Workspace: enabled (tools available: zellij)" "Terrapod status reports enabled Optional Development Workspace tool state"
assert_contains "$macos_status_output" "terminal-apps: enabled (Ghostty and cmux)" "Terrapod status reports enabled terminal-apps macOS App Group"
assert_contains "$macos_status_output" "automation: disabled" "Terrapod status reports disabled automation macOS App Group"
assert_contains "$macos_status_output" "launcher: enabled (Raycast and 1Password CLI)" "Terrapod status reports enabled launcher macOS App Group"
assert_contains "$macos_status_output" "monitoring: disabled" "Terrapod status reports disabled monitoring macOS App Group"
assert_contains "$macos_status_output" "chezmoi: available" "Terrapod status reports chezmoi availability"
assert_contains "$macos_status_output" "brew: available" "Terrapod status reports macOS Bootstrap Package Manager availability"
assert_contains "$macos_status_output" "Warnings: none" "Terrapod status reports no warnings when enabled tools are present"
```

- [ ] **Step 4: Add Ubuntu disabled-stack status test**

```sh
status_ubuntu_config="$tmp_dir/status-ubuntu.toml"
status_ubuntu_os_release="$tmp_dir/status-ubuntu-os-release"
cat >"$status_ubuntu_config" <<'TOML'
[data]
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
TOML
write_os_release "$status_ubuntu_os_release" ubuntu 24.04 "Ubuntu 24.04 LTS"
write_stub "$tmp_dir/bin/uname" 'printf "%s\n" "Linux"'
write_success_stub apt

ubuntu_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
    sh "$terrapod" status
)"

assert_contains "$ubuntu_status_output" "Profile: VPS Shell Profile" "Terrapod status reports VPS Shell Profile context on Ubuntu 24.04"
assert_contains "$ubuntu_status_output" "Optional Editor Stack: disabled" "Terrapod status reports disabled Optional Editor Stack without treating nvim as missing"
assert_contains "$ubuntu_status_output" "Optional AI Tool Stack: disabled" "Terrapod status reports disabled Optional AI Tool Stack without missing-tool warnings"
assert_contains "$ubuntu_status_output" "Optional Development Workspace: disabled" "Terrapod status reports disabled Optional Development Workspace without missing-tool warnings"
assert_contains "$ubuntu_status_output" "macOS App Groups: not applicable for VPS Shell Profile" "Terrapod status omits macOS App Group details on VPS Shell Profile"
assert_contains "$ubuntu_status_output" "apt: available" "Terrapod status reports Ubuntu Bootstrap Package Manager availability"
assert_contains "$ubuntu_status_output" "Warnings: none" "Terrapod status has no warnings for disabled optional stacks"
assert_not_contains "$ubuntu_status_output" "missing tools: nvim" "Terrapod status distinguishes disabled Optional Editor Stack from missing tools"
assert_not_contains "$ubuntu_status_output" "missing tools: gemini" "Terrapod status distinguishes disabled Optional AI Tool Stack from missing tools"
```

- [ ] **Step 5: Add enabled-missing-tools and unsupported-Linux status tests**

```sh
status_missing_config="$tmp_dir/status-missing.toml"
cat >"$status_missing_config" <<'TOML'
[data]
enableEditorStack = true
enableAiCliTools = true
enableDevelopmentWorkspace = true
TOML

rm -f "$tmp_dir/bin/nvim" "$tmp_dir/bin/gemini" "$tmp_dir/bin/claude" "$tmp_dir/bin/codex" "$tmp_dir/bin/zellij"

missing_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_missing_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
    sh "$terrapod" status
)"

assert_contains "$missing_status_output" "Optional Editor Stack: enabled (missing tools: nvim)" "Terrapod status reports missing tools only for enabled Optional Editor Stack"
assert_contains "$missing_status_output" "Optional AI Tool Stack: enabled (missing tools: gemini, claude, codex)" "Terrapod status reports missing tools only for enabled Optional AI Tool Stack"
assert_contains "$missing_status_output" "Optional Development Workspace: enabled (missing tools: zellij)" "Terrapod status reports missing tools only for enabled Optional Development Workspace"
assert_contains "$missing_status_output" "Warning: Optional AI Tool Stack is enabled but missing tools: gemini, claude, codex" "Terrapod status warns for enabled missing AI tools"

status_unsupported_os_release="$tmp_dir/status-unsupported-os-release"
write_os_release "$status_unsupported_os_release" debian 12 "Debian GNU/Linux 12"

unsupported_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_unsupported_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
    sh "$terrapod" status
)"

assert_contains "$unsupported_status_output" "Profile: unsupported profile" "Terrapod status reports unsupported Linux as unsupported"
assert_contains "$unsupported_status_output" "Warning: unsupported Linux release: Debian GNU/Linux 12. Terrapod supports Ubuntu 24.04 for the VPS Shell Profile." "Terrapod status explains unsupported Linux"
```

- [ ] **Step 6: Run status tests and verify they fail**

Run: `sh tests/terrapod_command_test.sh`

Expected: FAIL because `status` is not yet implemented and help text does not mention it.

## Task 2: Implement status command

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_command_test.sh`

- [ ] **Step 1: Add `status` and `doctor` to help text**

Change the help command list to include:

```sh
  terrapod status
  terrapod doctor
```

Change the command descriptions to include:

```sh
  status                                Explain this machine's Terrapod profile, config, stack, and tool state.
  doctor                                Validate Terrapod prerequisites and report actionable guidance.
```

Change examples to include:

```sh
  terrapod status
  terrapod doctor
```

- [ ] **Step 2: Add supported OS-release helpers after `chezmoi_config_file`**

```sh
os_release_file() {
  printf '%s\n' "${TERRAPOD_OS_RELEASE_FILE:-/etc/os-release}"
}

os_release_value() {
  key="$1"
  file="$(os_release_file)"

  if [ ! -f "$file" ]; then
    return 1
  fi

  awk -F= -v wanted="$key" '
    $1 == wanted {
      value = $0
      sub("^[^=]*=", "", value)
      sub(/^"/, "", value)
      sub(/"$/, "", value)
      print value
      found = 1
      exit
    }
    END {
      exit found ? 0 : 1
    }
  ' "$file"
}

is_supported_ubuntu_release() {
  [ "$(os_release_value ID 2>/dev/null || printf unknown)" = "ubuntu" ] &&
    [ "$(os_release_value VERSION_ID 2>/dev/null || printf unknown)" = "24.04" ]
}

unsupported_profile_reason() {
  case "$(uname -s 2>/dev/null || printf unknown)" in
    Linux)
      pretty_name="$(os_release_value PRETTY_NAME 2>/dev/null || printf 'unknown Linux release')"
      printf '%s\n' "unsupported Linux release: $pretty_name. Terrapod supports Ubuntu 24.04 for the VPS Shell Profile."
      ;;
    *)
      printf '%s\n' "unsupported operating system: $(uname -s 2>/dev/null || printf unknown). Terrapod supports macOS and Ubuntu 24.04."
      ;;
  esac
}
```

- [ ] **Step 3: Update `current_profile` Linux detection**

Replace the Linux branch with:

```sh
    Linux)
      if is_supported_ubuntu_release; then
        printf '%s\n' "vps-shell"
      else
        printf '%s\n' "unsupported"
      fi
      ;;
```

- [ ] **Step 4: Add config bool and tool summary helpers after `profile_context_label`**

```sh
config_bool() {
  key="$1"
  config_file="$(chezmoi_config_file)"

  if [ ! -f "$config_file" ]; then
    printf '%s\n' "false"
    return
  fi

  awk -v wanted="$key" '
    function strip_space(value) {
      sub(/^[[:space:]]*/, "", value)
      sub(/[[:space:]]*$/, "", value)
      return value
    }

    function strip_comment(value) {
      sub(/[[:space:]]*#.*/, "", value)
      return strip_space(value)
    }

    function unquote_key(value, quote) {
      value = strip_space(value)
      quote = substr(value, 1, 1)
      if ((quote == "\"" || quote == "\047") && substr(value, length(value), 1) == quote) {
        return substr(value, 2, length(value) - 2)
      }
      return value
    }

    function is_data_section(line) {
      return line ~ "^[[:space:]]*\\[[[:space:]]*(data|\"data\"|\047data\047)[[:space:]]*\\][[:space:]]*($|#)"
    }

    function is_section(line) {
      return line ~ /^[[:space:]]*(\[[^]]+\]|\[\[[^]]+\]\])[[:space:]]*($|#)/
    }

    function is_key_assignment(line) {
      return line ~ "^[[:space:]]*(\"[^\"]+\"|\047[^\047]+\047|[A-Za-z0-9_-]+)[[:space:]]*="
    }

    function assignment_key_name(line, key) {
      key = line
      sub(/^[[:space:]]*/, "", key)
      sub(/[[:space:]]*=.*/, "", key)
      return unquote_key(key)
    }

    function assignment_value(line, value) {
      value = line
      sub(/^[^=]*=/, "", value)
      return strip_comment(value)
    }

    function dotted_data_key_name(line, key) {
      key = line
      sub(/^[[:space:]]*/, "", key)
      sub(/[[:space:]]*=.*/, "", key)
      sub("^[[:space:]]*(data|\"data\"|\047data\047)[[:space:]]*\\.[[:space:]]*", "", key)
      return unquote_key(key)
    }

    function is_root_dotted_data_key(line) {
      return line ~ "^[[:space:]]*(data|\"data\"|\047data\047)[[:space:]]*\\."
    }

    BEGIN {
      in_root = 1
      found = "false"
    }

    {
      if (in_root && is_root_dotted_data_key($0) && dotted_data_key_name($0) == wanted) {
        found = assignment_value($0)
      }

      if (is_data_section($0)) {
        in_root = 0
        in_data = 1
        next
      }

      if (is_section($0)) {
        in_root = 0
        in_data = 0
        next
      }

      if (in_data && is_key_assignment($0) && assignment_key_name($0) == wanted) {
        found = assignment_value($0)
      }
    }

    END {
      if (found == "true") {
        print "true"
      } else {
        print "false"
      }
    }
  ' "$config_file"
}

is_enabled() {
  [ "$1" = "true" ]
}

effective_editor_stack_enabled() {
  if is_enabled "$(config_bool enableEditorStack)" || is_enabled "$(config_bool enableDevelopmentWorkspace)"; then
    printf '%s\n' "true"
  else
    printf '%s\n' "false"
  fi
}

effective_ai_cli_tools_enabled() {
  if is_enabled "$(config_bool enableAiCliTools)" || is_enabled "$(config_bool enableDevelopmentWorkspace)"; then
    printf '%s\n' "true"
  else
    printf '%s\n' "false"
  fi
}

join_tools() {
  joined=
  for tool do
    if [ -z "$joined" ]; then
      joined="$tool"
    else
      joined="$joined, $tool"
    fi
  done
  printf '%s\n' "$joined"
}

missing_tools() {
  missing=
  for tool do
    if ! command -v "$tool" >/dev/null 2>&1; then
      if [ -z "$missing" ]; then
        missing="$tool"
      else
        missing="$missing, $tool"
      fi
    fi
  done
  printf '%s\n' "$missing"
}

tool_phrase() {
  missing="$(missing_tools "$@")"
  if [ -n "$missing" ]; then
    printf '%s\n' "missing tools: $missing"
  else
    printf '%s\n' "tools available: $(join_tools "$@")"
  fi
}

print_stack_status() {
  label="$1"
  enabled="$2"
  shift 2

  if is_enabled "$enabled"; then
    printf '  %s: enabled (%s)\n' "$label" "$(tool_phrase "$@")"
  else
    printf '  %s: disabled\n' "$label"
  fi
}
```

- [ ] **Step 5: Add status render helpers**

```sh
print_config_context() {
  config_file="$(chezmoi_config_file)"
  if [ -f "$config_file" ]; then
    printf '%s\n' "Config: $config_file (present)"
  else
    printf '%s\n' "Config: $config_file (missing; defaults apply)"
  fi
}

print_macos_app_group_status() {
  profile="$1"

  if [ "$profile" != "macos-terminal" ]; then
    printf '%s\n' "macOS App Groups: not applicable for $(profile_context_label)"
    return
  fi

  printf '%s\n' "macOS App Groups:"
  if is_enabled "$(config_bool enableMacosAppGroupTerminalApps)"; then
    printf '%s\n' "  terminal-apps: enabled (Ghostty and cmux)"
  else
    printf '%s\n' "  terminal-apps: disabled"
  fi
  if is_enabled "$(config_bool enableMacosAppGroupAutomation)"; then
    printf '%s\n' "  automation: enabled (Hammerspoon and Karabiner-Elements)"
  else
    printf '%s\n' "  automation: disabled"
  fi
  if is_enabled "$(config_bool enableMacosAppGroupLauncher)"; then
    printf '%s\n' "  launcher: enabled (Raycast and 1Password CLI)"
  else
    printf '%s\n' "  launcher: disabled"
  fi
  if is_enabled "$(config_bool enableMacosAppGroupMonitoring)"; then
    printf '%s\n' "  monitoring: enabled (iStat Menus)"
  else
    printf '%s\n' "  monitoring: disabled"
  fi
}

print_key_tool_status() {
  profile="$1"

  printf '%s\n' "Key tools:"
  for tool in chezmoi git zsh mise; do
    if command -v "$tool" >/dev/null 2>&1; then
      printf '  %s: available\n' "$tool"
    else
      printf '  %s: missing\n' "$tool"
    fi
  done

  case "$profile" in
    macos-terminal)
      if command -v brew >/dev/null 2>&1; then
        printf '%s\n' "  brew: available"
      else
        printf '%s\n' "  brew: missing"
      fi
      ;;
    vps-shell)
      if command -v apt >/dev/null 2>&1; then
        printf '%s\n' "  apt: available"
      else
        printf '%s\n' "  apt: missing"
      fi
      ;;
  esac
}

print_status_warnings() {
  profile="$1"
  printed=0

  if [ "$profile" = "unsupported" ]; then
    printf '%s\n' "Warning: $(unsupported_profile_reason)"
    printed=1
  fi

  if is_enabled "$(effective_editor_stack_enabled)"; then
    missing="$(missing_tools nvim)"
    if [ -n "$missing" ]; then
      printf '%s\n' "Warning: Optional Editor Stack is enabled but missing tools: $missing"
      printed=1
    fi
  fi

  if is_enabled "$(effective_ai_cli_tools_enabled)"; then
    missing="$(missing_tools gemini claude codex)"
    if [ -n "$missing" ]; then
      printf '%s\n' "Warning: Optional AI Tool Stack is enabled but missing tools: $missing"
      printed=1
    fi
  fi

  if is_enabled "$(config_bool enableDevelopmentWorkspace)"; then
    missing="$(missing_tools zellij)"
    if [ -n "$missing" ]; then
      printf '%s\n' "Warning: Optional Development Workspace is enabled but missing tools: $missing"
      printed=1
    fi
  fi

  if [ "$printed" -eq 0 ]; then
    printf '%s\n' "Warnings: none"
  fi
}

run_status() {
  if [ "$#" -ne 0 ]; then
    fail_usage "status accepts no arguments"
  fi

  profile="$(current_profile)"

  printf '%s\n' "Terrapod status"
  printf '%s\n' "Profile: $(profile_context_label)"
  print_config_context
  printf '%s\n' "Optional stacks:"
  print_stack_status "Optional Editor Stack" "$(effective_editor_stack_enabled)" nvim
  print_stack_status "Optional AI Tool Stack" "$(effective_ai_cli_tools_enabled)" gemini claude codex
  print_stack_status "Optional Development Workspace" "$(config_bool enableDevelopmentWorkspace)" zellij
  print_macos_app_group_status "$profile"
  print_key_tool_status "$profile"
  print_status_warnings "$profile"
}
```

- [ ] **Step 6: Dispatch `status`**

Add this branch before `update)`:

```sh
  status)
    shift
    run_status "$@"
    ;;
```

- [ ] **Step 7: Run status tests and verify they pass**

Run: `sh tests/terrapod_command_test.sh`

Expected: PASS through the new status section and existing command tests.

## Task 3: Add failing doctor tests

**Files:**
- Modify: `tests/terrapod_command_test.sh`
- Test: `tests/terrapod_command_test.sh`

- [ ] **Step 1: Add doctor success test after status tests**

```sh
doctor_config="$tmp_dir/doctor-ok.toml"
doctor_os_release="$tmp_dir/doctor-os-release"
cat >"$doctor_config" <<'TOML'
[data]
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
TOML
write_os_release "$doctor_os_release" ubuntu 24.04 "Ubuntu 24.04 LTS"

for tool in chezmoi git zsh mise apt; do
  write_success_stub "$tool"
done

if ! doctor_ok_output="$(
  TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
    sh "$terrapod" doctor
)"; then
  fail "Terrapod doctor succeeds when VPS prerequisites are present and optional stacks are disabled"
fi

assert_contains "$doctor_ok_output" "Terrapod doctor" "Terrapod doctor prints a command heading"
assert_contains "$doctor_ok_output" "ok - Profile is supported: VPS Shell Profile" "Terrapod doctor validates supported Ubuntu profile"
assert_contains "$doctor_ok_output" "ok - chezmoi is available" "Terrapod doctor validates chezmoi availability"
assert_contains "$doctor_ok_output" "ok - apt is available" "Terrapod doctor validates Ubuntu Bootstrap Package Manager availability"
assert_contains "$doctor_ok_output" "ok - Optional AI Tool Stack is disabled" "Terrapod doctor treats disabled Optional AI Tool Stack as valid"
assert_contains "$doctor_ok_output" "Guidance: none" "Terrapod doctor prints no guidance when checks pass"
```

- [ ] **Step 2: Add doctor missing-tool test**

```sh
doctor_missing_config="$tmp_dir/doctor-missing.toml"
cat >"$doctor_missing_config" <<'TOML'
[data]
enableEditorStack = true
enableAiCliTools = true
enableDevelopmentWorkspace = true
TOML

rm -f "$tmp_dir/bin/nvim" "$tmp_dir/bin/gemini" "$tmp_dir/bin/claude" "$tmp_dir/bin/codex" "$tmp_dir/bin/zellij"

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_missing_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" doctor >"$tmp_dir/doctor-missing.out" 2>"$tmp_dir/doctor-missing.err"; then
  fail "Terrapod doctor fails when enabled optional stack tools are missing"
fi

doctor_missing_output="$(cat "$tmp_dir/doctor-missing.out")"

assert_contains "$doctor_missing_output" "warn - Optional Editor Stack is enabled but missing tools: nvim" "Terrapod doctor warns about missing enabled editor tools"
assert_contains "$doctor_missing_output" "warn - Optional AI Tool Stack is enabled but missing tools: gemini, claude, codex" "Terrapod doctor warns about missing enabled AI tools"
assert_contains "$doctor_missing_output" "warn - Optional Development Workspace is enabled but missing tools: zellij" "Terrapod doctor warns about missing enabled workspace tools"
assert_contains "$doctor_missing_output" "Run terrapod chezmoi -- apply after enabling Optional AI Tool Stack, or install/apply the configured tools before relying on them." "Terrapod doctor gives actionable missing optional-stack guidance"
```

- [ ] **Step 3: Add doctor unsupported Linux and broad-upgrade tests**

```sh
doctor_unsupported_os_release="$tmp_dir/doctor-unsupported-os-release"
write_os_release "$doctor_unsupported_os_release" debian 12 "Debian GNU/Linux 12"

if TERRAPOD_OS_RELEASE_FILE="$doctor_unsupported_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" doctor >"$tmp_dir/doctor-unsupported.out" 2>"$tmp_dir/doctor-unsupported.err"; then
  fail "Terrapod doctor fails on unsupported Linux"
fi

doctor_unsupported_output="$(cat "$tmp_dir/doctor-unsupported.out")"
assert_contains "$doctor_unsupported_output" "warn - unsupported Linux release: Debian GNU/Linux 12. Terrapod supports Ubuntu 24.04 for the VPS Shell Profile." "Terrapod doctor explains unsupported Linux"

doctor_broad_upgrade_calls="$tmp_dir/doctor-broad-upgrade.calls"
rm -f "$doctor_broad_upgrade_calls"
for command_name in brew apt sudo mise npm; do
  write_failing_command_stub "$command_name" "$doctor_broad_upgrade_calls"
done

TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" status >/dev/null

if TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" doctor >/dev/null; then
  :
fi

if [ -e "$doctor_broad_upgrade_calls" ]; then
  printf '%s\n' "unexpected broad upgrade command calls from status/doctor:" >&2
  sed 's/^/  /' "$doctor_broad_upgrade_calls" >&2
  fail "Terrapod status and doctor do not call brew, apt, sudo, mise, or npm upgrade flows"
fi

pass "Terrapod status and doctor do not call brew, apt, sudo, mise, or npm upgrade flows"
```

- [ ] **Step 4: Run doctor tests and verify they fail**

Run: `sh tests/terrapod_command_test.sh`

Expected: FAIL because `doctor` is not yet implemented.

## Task 4: Implement doctor command

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Test: `tests/terrapod_command_test.sh`

- [ ] **Step 1: Add doctor output helpers after `run_status`**

```sh
doctor_failed=0
doctor_guidance_printed=0

doctor_ok() {
  printf '  ok - %s\n' "$1"
}

doctor_warn() {
  printf '  warn - %s\n' "$1"
  doctor_failed=1
}

doctor_guidance() {
  if [ "$doctor_guidance_printed" -eq 0 ]; then
    printf '%s\n' "Guidance:"
    doctor_guidance_printed=1
  fi
  printf '  - %s\n' "$1"
}

doctor_check_command() {
  tool="$1"
  if command -v "$tool" >/dev/null 2>&1; then
    doctor_ok "$tool is available"
  else
    doctor_warn "$tool is missing"
    doctor_guidance "Install or apply the configured Core Shell Stack so '$tool' is available on PATH."
  fi
}

doctor_check_optional_stack() {
  label="$1"
  enabled="$2"
  guidance="$3"
  shift 3

  if ! is_enabled "$enabled"; then
    doctor_ok "$label is disabled"
    return
  fi

  missing="$(missing_tools "$@")"
  if [ -n "$missing" ]; then
    doctor_warn "$label is enabled but missing tools: $missing"
    doctor_guidance "$guidance"
  else
    doctor_ok "$label is enabled and tools are available: $(join_tools "$@")"
  fi
}
```

- [ ] **Step 2: Add `run_doctor` after the doctor helpers**

```sh
run_doctor() {
  if [ "$#" -ne 0 ]; then
    fail_usage "doctor accepts no arguments"
  fi

  doctor_failed=0
  doctor_guidance_printed=0
  profile="$(current_profile)"

  printf '%s\n' "Terrapod doctor"
  printf '%s\n' "Profile: $(profile_context_label)"
  print_config_context
  printf '%s\n' "Checks:"

  if [ "$profile" = "unsupported" ]; then
    doctor_warn "$(unsupported_profile_reason)"
    doctor_guidance "Run Terrapod on macOS or Ubuntu 24.04, or use 'terrapod chezmoi -- <args>' as an advanced escape hatch on unsupported systems."
  else
    doctor_ok "Profile is supported: $(profile_context_label)"
  fi

  for tool in chezmoi git zsh mise; do
    doctor_check_command "$tool"
  done

  case "$profile" in
    macos-terminal)
      doctor_check_command brew
      ;;
    vps-shell)
      doctor_check_command apt
      ;;
  esac

  doctor_check_optional_stack \
    "Optional Editor Stack" \
    "$(effective_editor_stack_enabled)" \
    "Run terrapod chezmoi -- apply after enabling Optional Editor Stack, or install/apply the configured editor tools before relying on them." \
    nvim

  doctor_check_optional_stack \
    "Optional AI Tool Stack" \
    "$(effective_ai_cli_tools_enabled)" \
    "Run terrapod chezmoi -- apply after enabling Optional AI Tool Stack, or install/apply the configured tools before relying on them." \
    gemini claude codex

  doctor_check_optional_stack \
    "Optional Development Workspace" \
    "$(config_bool enableDevelopmentWorkspace)" \
    "Run terrapod chezmoi -- apply after enabling Optional Development Workspace, or install/apply the configured workspace tools before relying on them." \
    zellij

  if [ "$doctor_guidance_printed" -eq 0 ]; then
    printf '%s\n' "Guidance: none"
  fi

  if [ "$doctor_failed" -ne 0 ]; then
    return 1
  fi
}
```

- [ ] **Step 3: Dispatch `doctor`**

Add this branch before `update)`:

```sh
  doctor)
    shift
    run_doctor "$@"
    ;;
```

- [ ] **Step 4: Run command tests and verify they pass**

Run: `sh tests/terrapod_command_test.sh`

Expected: PASS.

## Task 5: Full verification and final commit

**Files:**
- Verify: all `tests/*.sh` and `tests/*.zsh`

- [ ] **Step 1: Run syntax check**

Run: `sh -n dot_local/bin/executable_terrapod`

Expected: PASS with no output.

- [ ] **Step 2: Run focused tests**

Run: `sh tests/terrapod_command_test.sh`

Expected: PASS.

- [ ] **Step 3: Run full shell test suite**

Run:

```sh
for test_script in tests/*.sh; do
  [ -f "$test_script" ] || continue
  sh "$test_script"
done
for test_script in tests/*.zsh; do
  [ -f "$test_script" ] || continue
  zsh "$test_script"
done
```

Expected: PASS for every script.

- [ ] **Step 4: Review diff against base**

Run: `git diff --stat origin/main...HEAD && git diff origin/main...HEAD -- dot_local/bin/executable_terrapod tests/terrapod_command_test.sh docs/superpowers/plans/2026-05-28-terrapod-status-doctor.md`

Expected: diff includes only the Terrapod command, command tests, and this plan.

- [ ] **Step 5: Commit once per the requested workflow**

Run:

```sh
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh docs/superpowers/plans/2026-05-28-terrapod-status-doctor.md
git commit -m "Add Terrapod status and doctor"
```

Expected: commit succeeds on branch `feat/issue-36-terrapod-status-doctor`.

---

## Execution Notes

- `origin/main` gained `terrapod diff` and `terrapod apply` while this work was in progress, so the final implementation preserves those wrappers and adds `status`/`doctor` alongside them.
- The final status/doctor implementation uses the existing config-aware helpers (`config_data_bool "$config_file" ...` and `show_config_context`) rather than the standalone sketch names shown in the planning snippets.
- Doctor guidance points users at `terrapod chezmoi -- apply` so the command stays informational and avoids broad package upgrade flows.
