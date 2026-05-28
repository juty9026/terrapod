#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

write_stub() {
  path="$1"
  shift
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' "$@"
  } >"$path"
  chmod +x "$path"
}

assert_contains() {
  haystack="$1"
  needle="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  haystack="$1"
  needle="$2"
  message="$3"

  if printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_line() {
  haystack="$1"
  expected_line="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | grep -Fx "$expected_line" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_call_args() {
  call_file="$1"
  message="$2"
  shift 2

  expected_file="$tmp_dir/expected.args"
  : >"$expected_file"

  for arg do
    printf '%s\n' "$arg" >>"$expected_file"
  done

  if ! cmp -s "$expected_file" "$call_file"; then
    printf '%s\n' "expected args:" >&2
    sed 's/^/  /' "$expected_file" >&2
    printf '%s\n' "actual args:" >&2
    sed 's/^/  /' "$call_file" >&2
    fail "$message"
  fi

  pass "$message"
}

write_success_stub() {
  name="$1"
  write_stub "$tmp_dir/bin/$name" \
    'exit 0'
}

write_failing_command_stub() {
  path="$1"
  call_file="$2"
  write_stub "$path" \
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

status_doctor_path() {
  name="$1"
  shift

  isolated_path="$tmp_dir/status-doctor-path-$name"
  rm -rf "$isolated_path"
  mkdir -p "$isolated_path"

  awk_path="$(command -v awk 2>/dev/null || true)"
  if [ -z "$awk_path" ]; then
    fail "status/doctor tests require awk"
  fi
  ln -s "$awk_path" "$isolated_path/awk"

  for command_name do
    write_stub "$isolated_path/$command_name" \
      'exit 0'
  done

  printf '%s\n' "$isolated_path"
}

assert_no_terrapod_artifacts_under() {
  dir="$1"
  message="$2"
  found_file="$tmp_dir/found-terrapod-artifacts"

  : >"$found_file"
  if [ -d "$dir" ]; then
    find "$dir" \
      \( -name '.terrapod-config.*' \
      -o -name '.terrapod-data.*' \
      -o -name '*.terrapod-backup-*' \
      -o -name '*.terrapod-tmp-*' \
      -o -name '*.terrapod-data-*' \) \
      -print >"$found_file"
  fi

  if [ -s "$found_file" ]; then
    printf '%s\n' "unexpected Terrapod artifacts:" >&2
    sed 's/^/  /' "$found_file" >&2
    fail "$message"
  fi

  pass "$message"
}

mkdir -p "$tmp_dir/bin" "$tmp_dir/home"

terrapod="$repo_root/dot_local/bin/executable_terrapod"
tpod_source="$repo_root/dot_local/bin/symlink_tpod"

managed_targets="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    managed
)"

assert_line \
  "$managed_targets" \
  ".local/bin/terrapod" \
  "chezmoi manages Terrapod as the primary user-facing command"

assert_line \
  "$managed_targets" \
  ".local/bin/tpod" \
  "chezmoi manages tpod as an alias to Terrapod"

terrapod_target="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    target-path dot_local/bin/executable_terrapod
)"
expected_terrapod_target="$tmp_dir/home/.local/bin/terrapod"

if [ "$terrapod_target" != "$expected_terrapod_target" ]; then
  fail "chezmoi installs Terrapod at ~/.local/bin/terrapod; expected '$expected_terrapod_target', got '$terrapod_target'"
fi

pass "chezmoi installs Terrapod at ~/.local/bin/terrapod"

tpod_target="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    target-path dot_local/bin/symlink_tpod
)"
expected_tpod_target="$tmp_dir/home/.local/bin/tpod"

if [ "$tpod_target" != "$expected_tpod_target" ]; then
  fail "chezmoi installs tpod at ~/.local/bin/tpod; expected '$expected_tpod_target', got '$tpod_target'"
fi

pass "chezmoi installs tpod at ~/.local/bin/tpod"

if [ ! -f "$terrapod" ]; then
  fail "Terrapod command source exists"
fi

pass "Terrapod command source exists"

if [ ! -x "$terrapod" ]; then
  fail "Terrapod command source is executable"
fi

pass "Terrapod command source is executable"

if [ ! -f "$tpod_source" ]; then
  fail "tpod alias source exists"
fi

pass "tpod alias source exists"

IFS= read -r tpod_source_line <"$tpod_source"

if [ "$tpod_source_line" != "terrapod" ]; then
  fail "tpod alias points to Terrapod"
fi

pass "tpod alias points to Terrapod"

sh -n "$terrapod" || fail "Terrapod command is valid POSIX shell"
pass "Terrapod command is valid POSIX shell"

help_output="$(TERRAPOD_PROFILE=macos-terminal sh "$terrapod" help)"

ln -s "$terrapod" "$tmp_dir/bin/tpod"
tpod_help_output="$(TERRAPOD_PROFILE=macos-terminal PATH="$tmp_dir/bin:/usr/bin:/bin" "$tmp_dir/bin/tpod" help)"

if [ "$tpod_help_output" != "$help_output" ]; then
  fail "tpod shows the same help as Terrapod"
fi

pass "tpod shows the same help as Terrapod"

assert_contains \
  "$help_output" \
  "Terrapod - Dotfiles Management Tool" \
  "Terrapod help names the Dotfiles Management Tool"

assert_contains \
  "$help_output" \
  "Usage:" \
  "Terrapod help shows usage"

assert_contains \
  "$help_output" \
  "Commands:" \
  "Terrapod help shows command list"

assert_contains \
  "$help_output" \
  "terrapod chezmoi -- <args...>" \
  "Terrapod help documents the raw chezmoi escape hatch"

assert_contains \
  "$help_output" \
  "terrapod configure <minimal|development|workstation>" \
  "Terrapod help documents Preset configuration"

assert_contains \
  "$help_output" \
  "terrapod status" \
  "Terrapod help documents status"

assert_contains \
  "$help_output" \
  "terrapod doctor" \
  "Terrapod help documents doctor"

macos_help_output="$(TERRAPOD_PROFILE=macos-terminal sh "$terrapod" help)"

assert_contains \
  "$macos_help_output" \
  "terrapod configure <minimal|development|workstation>" \
  "macOS Terminal Profile help exposes workstation Preset"

vps_help_output="$(TERRAPOD_PROFILE=vps-shell sh "$terrapod" help)"

assert_contains \
  "$vps_help_output" \
  "terrapod configure <minimal|development>" \
  "VPS Shell Profile help hides workstation Preset"

assert_not_contains \
  "$vps_help_output" \
  "workstation" \
  "VPS Shell Profile help does not mention workstation anywhere"

if TERRAPOD_PROFILE=vps-shell HOME="$tmp_dir/home" XDG_CONFIG_HOME="$tmp_dir/xdg" sh "$terrapod" configure workstation >"$tmp_dir/vps-workstation.out" 2>"$tmp_dir/vps-workstation.err"; then
  fail "VPS Shell Profile rejects workstation Preset"
fi
pass "VPS Shell Profile rejects workstation Preset"

if [ -e "$tmp_dir/xdg/chezmoi/chezmoi.toml" ]; then
  fail "VPS Shell Profile rejects workstation Preset before writing config"
fi
pass "VPS Shell Profile rejects workstation Preset before writing config"

assert_no_terrapod_artifacts_under \
  "$tmp_dir/xdg/chezmoi" \
  "VPS Shell Profile workstation rejection leaves no Terrapod artifacts"

vps_workstation_error="$(cat "$tmp_dir/vps-workstation.err")"

assert_contains \
  "$vps_workstation_error" \
  "workstation Preset is only available for the macOS Terminal Profile" \
  "VPS Shell Profile explains workstation Preset rejection"

assert_contains \
  "$help_output" \
  "Expand a Preset into concrete chezmoi data values." \
  "Terrapod help describes Preset configuration"

assert_contains \
  "$help_output" \
  "Run raw chezmoi for advanced maintenance." \
  "Terrapod help describes raw chezmoi as advanced maintenance"

default_output="$(sh "$terrapod")"

assert_contains \
  "$default_output" \
  "Terrapod - Dotfiles Management Tool" \
  "Terrapod with no arguments shows help"

dash_help_output="$(sh "$terrapod" --help)"

assert_contains \
  "$dash_help_output" \
  "Terrapod - Dotfiles Management Tool" \
  "Terrapod --help shows help"

if sh "$terrapod" frobnicate >"$tmp_dir/unknown.out" 2>"$tmp_dir/unknown.err"; then
  fail "unknown Terrapod subcommands fail"
fi

unknown_error="$(cat "$tmp_dir/unknown.err")"

assert_contains \
  "$unknown_error" \
  "terrapod: unknown command: frobnicate" \
  "unknown Terrapod subcommands name the rejected command"

assert_contains \
  "$unknown_error" \
  "Run 'terrapod help' for usage." \
  "unknown Terrapod subcommands point to help"

export CHEZMOI_CALL_FILE="$tmp_dir/chezmoi.args"

write_stub "$tmp_dir/bin/chezmoi" \
  ': >"$CHEZMOI_CALL_FILE"' \
  'for arg do' \
  '  printf "%s\n" "$arg" >>"$CHEZMOI_CALL_FILE"' \
  'done' \
  'exit 0'

export PATH="$tmp_dir/bin:/usr/bin:/bin"

sh "$terrapod" chezmoi -- status --include files

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod passes raw arguments to chezmoi after --" \
  status --include files

"$tmp_dir/bin/tpod" chezmoi -- diff

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "tpod uses the same raw chezmoi escape hatch as Terrapod" \
  diff

if sh "$terrapod" chezmoi status >"$tmp_dir/missing-separator.out" 2>"$tmp_dir/missing-separator.err"; then
  fail "raw chezmoi escape hatch requires -- separator"
fi

missing_separator_error="$(cat "$tmp_dir/missing-separator.err")"

assert_contains \
  "$missing_separator_error" \
  "terrapod: raw chezmoi commands must be separated with '--'" \
  "Terrapod explains missing raw chezmoi separator"

if sh "$terrapod" chezmoi -- >"$tmp_dir/missing-command.out" 2>"$tmp_dir/missing-command.err"; then
  fail "raw chezmoi escape hatch requires a command after --"
fi

missing_command_error="$(cat "$tmp_dir/missing-command.err")"

assert_contains \
  "$missing_command_error" \
  "terrapod: raw chezmoi command is required" \
  "Terrapod explains missing raw chezmoi command"

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

macos_status_path="$(status_doctor_path macos chezmoi git zsh mise brew nvim gemini claude codex zellij ghostty cmux op)"

macos_status_output="$(
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$status_config" PATH="$macos_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$macos_status_output" "Terrapod status" "Terrapod status prints a command heading"
assert_contains "$macos_status_output" "Profile: macOS Terminal Profile" "Terrapod status reports macOS Terminal Profile context"
assert_contains "$macos_status_output" "Config: $status_config (present)" "Terrapod status reports explicit config path"
assert_contains "$macos_status_output" "Optional Editor Stack: enabled (rich Neovim configuration)" "Terrapod status reports enabled Optional Editor Stack state"
assert_contains "$macos_status_output" "Optional AI Tool Stack: enabled (tools available: gemini, claude, codex)" "Terrapod status reports enabled Optional AI Tool Stack tool state"
assert_contains "$macos_status_output" "Optional Development Workspace: enabled (development Zellij layouts)" "Terrapod status reports enabled Optional Development Workspace state"
assert_contains "$macos_status_output" "terminal-apps: enabled (Ghostty and cmux)" "Terrapod status reports enabled terminal-apps macOS App Group"
assert_contains "$macos_status_output" "automation: disabled" "Terrapod status reports disabled automation macOS App Group"
assert_contains "$macos_status_output" "launcher: enabled (Raycast and 1Password CLI)" "Terrapod status reports enabled launcher macOS App Group"
assert_contains "$macos_status_output" "monitoring: disabled" "Terrapod status reports disabled monitoring macOS App Group"
assert_contains "$macos_status_output" "chezmoi: available" "Terrapod status reports chezmoi availability"
assert_contains "$macos_status_output" "brew: available" "Terrapod status reports macOS Bootstrap Package Manager availability"
assert_contains "$macos_status_output" "Warnings: none" "Terrapod status reports no warnings when enabled tools are present"
assert_not_contains "$macos_status_output" "Warning:" "Terrapod status emits no warning lines when enabled tools are present"

status_dotted_config="$tmp_dir/status-dotted.toml"
cat >"$status_dotted_config" <<'TOML'
data.enableEditorStack = false
data.enableAiCliTools = false
data.enableDevelopmentWorkspace = true
data.enableMacosAppGroupTerminalApps = true
TOML

dotted_status_path="$(status_doctor_path dotted chezmoi git zsh mise brew nvim gemini claude codex zellij)"

dotted_status_output="$(
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$status_dotted_config" PATH="$dotted_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$dotted_status_output" "Optional Editor Stack: enabled (rich Neovim configuration)" "Terrapod status reads root dotted data keys for effective editor stack state"
assert_contains "$dotted_status_output" "Optional AI Tool Stack: enabled (tools available: gemini, claude, codex)" "Terrapod status reads root dotted data keys for effective AI stack state"
assert_contains "$dotted_status_output" "terminal-apps: enabled (Ghostty and cmux)" "Terrapod status reads root dotted data keys for macOS App Groups"
assert_contains "$dotted_status_output" "Warnings: none" "Terrapod status has no warnings for root dotted data keys when tools are present"

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

ubuntu_status_path="$(status_doctor_path ubuntu chezmoi git zsh mise nvim zellij apt)"
write_stub "$ubuntu_status_path/uname" 'printf "%s\n" "Linux"'

ubuntu_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$ubuntu_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$ubuntu_status_output" "Profile: VPS Shell Profile" "Terrapod status reports VPS Shell Profile context on Ubuntu 24.04"
assert_contains "$ubuntu_status_output" "Optional Editor Stack: disabled" "Terrapod status reports disabled Optional Editor Stack without treating nvim as missing"
assert_contains "$ubuntu_status_output" "Optional AI Tool Stack: disabled" "Terrapod status reports disabled Optional AI Tool Stack without missing-tool warnings"
assert_contains "$ubuntu_status_output" "Optional Development Workspace: disabled" "Terrapod status reports disabled Optional Development Workspace without missing-tool warnings"
assert_contains "$ubuntu_status_output" "macOS App Groups: not applicable for VPS Shell Profile" "Terrapod status omits macOS App Group details on VPS Shell Profile"
assert_contains "$ubuntu_status_output" "apt: available" "Terrapod status reports Ubuntu Bootstrap Package Manager availability"
assert_contains "$ubuntu_status_output" "Warnings: none" "Terrapod status has no warnings for disabled optional stacks"
assert_not_contains "$ubuntu_status_output" "Warning:" "Terrapod status emits no warning lines for disabled optional stacks"
assert_not_contains "$ubuntu_status_output" "missing tools: nvim" "Terrapod status distinguishes disabled Optional Editor Stack from missing tools"
assert_not_contains "$ubuntu_status_output" "missing tools: gemini" "Terrapod status distinguishes disabled Optional AI Tool Stack from missing tools"

core_missing_status_path="$(status_doctor_path core-missing-status chezmoi git zsh mise apt)"
write_stub "$core_missing_status_path/uname" 'printf "%s\n" "Linux"'

core_missing_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$core_missing_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$core_missing_status_output" "Optional Editor Stack: disabled" "Terrapod status keeps disabled Optional Editor Stack separate from missing core Neovim"
assert_contains "$core_missing_status_output" "Optional Development Workspace: disabled" "Terrapod status keeps disabled Optional Development Workspace separate from missing core Zellij"
assert_contains "$core_missing_status_output" "nvim: missing" "Terrapod status reports missing plain Neovim as a key tool"
assert_contains "$core_missing_status_output" "zellij: missing" "Terrapod status reports missing Zellij as a key tool"
assert_contains "$core_missing_status_output" "Warning: missing key tools: nvim, zellij" "Terrapod status warns about missing core Neovim and Zellij even when optional stacks are disabled"

status_missing_config="$tmp_dir/status-missing.toml"
cat >"$status_missing_config" <<'TOML'
[data]
enableEditorStack = true
enableAiCliTools = true
enableDevelopmentWorkspace = true
TOML

missing_status_path="$(status_doctor_path missing chezmoi git zsh mise nvim zellij apt)"
write_stub "$missing_status_path/uname" 'printf "%s\n" "Linux"'

missing_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_missing_config" PATH="$missing_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$missing_status_output" "Optional Editor Stack: enabled (rich Neovim configuration)" "Terrapod status reports enabled Optional Editor Stack as rich config state"
assert_contains "$missing_status_output" "Optional AI Tool Stack: enabled (missing tools: gemini, claude, codex)" "Terrapod status reports missing tools only for enabled Optional AI Tool Stack"
assert_contains "$missing_status_output" "Optional Development Workspace: enabled (development Zellij layouts)" "Terrapod status reports enabled Optional Development Workspace as layout state"
assert_contains "$missing_status_output" "Warning: Optional AI Tool Stack is enabled but missing tools: gemini, claude, codex" "Terrapod status warns for enabled missing AI tools"

status_workspace_bundle_config="$tmp_dir/status-workspace-bundle.toml"
cat >"$status_workspace_bundle_config" <<'TOML'
[data]
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = true
TOML

workspace_bundle_status_path="$(status_doctor_path workspace-bundle chezmoi git zsh mise nvim zellij apt)"
write_stub "$workspace_bundle_status_path/uname" 'printf "%s\n" "Linux"'

workspace_bundle_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_workspace_bundle_config" PATH="$workspace_bundle_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$workspace_bundle_status_output" "Optional Editor Stack: enabled (rich Neovim configuration)" "Terrapod status treats Optional Development Workspace as enabling Optional Editor Stack"
assert_contains "$workspace_bundle_status_output" "Optional AI Tool Stack: enabled (missing tools: gemini, claude, codex)" "Terrapod status treats Optional Development Workspace as enabling Optional AI Tool Stack"
assert_contains "$workspace_bundle_status_output" "Optional Development Workspace: enabled (development Zellij layouts)" "Terrapod status reports enabled Optional Development Workspace"
assert_contains "$workspace_bundle_status_output" "Warning: Optional AI Tool Stack is enabled but missing tools: gemini, claude, codex" "Terrapod status warns when workspace-enabled Optional AI Tool Stack tools are missing"

status_unsupported_os_release="$tmp_dir/status-unsupported-os-release"
write_os_release "$status_unsupported_os_release" debian 12 "Debian GNU/Linux 12"

unsupported_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_unsupported_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$ubuntu_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$unsupported_status_output" "Profile: unsupported profile" "Terrapod status reports unsupported Linux as unsupported"
assert_contains "$unsupported_status_output" "Warning: unsupported Linux release: Debian GNU/Linux 12. Terrapod supports Ubuntu 24.04 for the VPS Shell Profile." "Terrapod status explains unsupported Linux"

doctor_config="$tmp_dir/doctor-ok.toml"
doctor_os_release="$tmp_dir/doctor-os-release"
cat >"$doctor_config" <<'TOML'
[data]
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
TOML
write_os_release "$doctor_os_release" ubuntu 24.04 "Ubuntu 24.04 LTS"

doctor_ok_path="$(status_doctor_path doctor-ok chezmoi git zsh mise nvim zellij apt)"
write_stub "$doctor_ok_path/uname" 'printf "%s\n" "Linux"'

if ! doctor_ok_output="$(
  TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_ok_path" \
    /bin/sh "$terrapod" doctor
)"; then
  fail "Terrapod doctor succeeds when VPS prerequisites are present and optional stacks are disabled"
fi

assert_contains "$doctor_ok_output" "Terrapod doctor" "Terrapod doctor prints a command heading"
assert_contains "$doctor_ok_output" "ok - Profile is supported: VPS Shell Profile" "Terrapod doctor validates supported Ubuntu profile"
assert_contains "$doctor_ok_output" "ok - chezmoi is available" "Terrapod doctor validates chezmoi availability"
assert_contains "$doctor_ok_output" "ok - nvim is available" "Terrapod doctor validates plain Neovim as a Core Shell Stack tool"
assert_contains "$doctor_ok_output" "ok - zellij is available" "Terrapod doctor validates Zellij as a Core Shell Stack tool"
assert_contains "$doctor_ok_output" "ok - apt is available" "Terrapod doctor validates Ubuntu Bootstrap Package Manager availability"
assert_contains "$doctor_ok_output" "ok - Optional Editor Stack is disabled" "Terrapod doctor treats disabled Optional Editor Stack as valid"
assert_contains "$doctor_ok_output" "ok - Optional AI Tool Stack is disabled" "Terrapod doctor treats disabled Optional AI Tool Stack as valid"
assert_contains "$doctor_ok_output" "ok - Optional Development Workspace is disabled" "Terrapod doctor treats disabled Optional Development Workspace as valid"
assert_contains "$doctor_ok_output" "Guidance: none" "Terrapod doctor prints no guidance when checks pass"

doctor_missing_key_path="$(status_doctor_path doctor-missing-key git zsh mise nvim zellij apt)"
write_stub "$doctor_missing_key_path/uname" 'printf "%s\n" "Linux"'

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_missing_key_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-missing-key.out" 2>"$tmp_dir/doctor-missing-key.err"; then
  fail "Terrapod doctor fails when a required key tool is missing"
fi

doctor_missing_key_output="$(cat "$tmp_dir/doctor-missing-key.out")"

assert_contains "$doctor_missing_key_output" "warn - chezmoi is missing" "Terrapod doctor warns when required chezmoi is missing"
assert_contains "$doctor_missing_key_output" "Install or apply the configured Core Shell Stack so 'chezmoi' is available on PATH." "Terrapod doctor gives actionable guidance for missing key tools"

doctor_missing_core_path="$(status_doctor_path doctor-missing-core chezmoi git zsh mise apt)"
write_stub "$doctor_missing_core_path/uname" 'printf "%s\n" "Linux"'

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_missing_core_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-missing-core.out" 2>"$tmp_dir/doctor-missing-core.err"; then
  fail "Terrapod doctor fails when Core Shell Stack Neovim or Zellij is missing"
fi

doctor_missing_core_output="$(cat "$tmp_dir/doctor-missing-core.out")"

assert_contains "$doctor_missing_core_output" "warn - nvim is missing" "Terrapod doctor warns when plain Neovim is missing even if Optional Editor Stack is disabled"
assert_contains "$doctor_missing_core_output" "warn - zellij is missing" "Terrapod doctor warns when Zellij is missing even if Optional Development Workspace is disabled"
assert_contains "$doctor_missing_core_output" "ok - Optional Editor Stack is disabled" "Terrapod doctor keeps disabled Optional Editor Stack separate from missing core Neovim"
assert_contains "$doctor_missing_core_output" "ok - Optional Development Workspace is disabled" "Terrapod doctor keeps disabled Optional Development Workspace separate from missing core Zellij"

doctor_missing_config="$tmp_dir/doctor-missing.toml"
cat >"$doctor_missing_config" <<'TOML'
[data]
enableEditorStack = true
enableAiCliTools = true
enableDevelopmentWorkspace = true
TOML

doctor_missing_path="$(status_doctor_path doctor-missing chezmoi git zsh mise nvim zellij apt)"
write_stub "$doctor_missing_path/uname" 'printf "%s\n" "Linux"'

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_missing_config" PATH="$doctor_missing_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-missing.out" 2>"$tmp_dir/doctor-missing.err"; then
  fail "Terrapod doctor fails when enabled optional stack tools are missing"
fi

doctor_missing_output="$(cat "$tmp_dir/doctor-missing.out")"

assert_contains "$doctor_missing_output" "ok - Optional Editor Stack is enabled (rich Neovim configuration)" "Terrapod doctor reports enabled Optional Editor Stack as rich config state"
assert_contains "$doctor_missing_output" "warn - Optional AI Tool Stack is enabled but missing tools: gemini, claude, codex" "Terrapod doctor warns about missing enabled AI tools"
assert_contains "$doctor_missing_output" "ok - Optional Development Workspace is enabled (development Zellij layouts)" "Terrapod doctor reports enabled Optional Development Workspace as layout state"
assert_contains "$doctor_missing_output" "Run terrapod chezmoi -- apply after enabling Optional AI Tool Stack, or install/apply the configured tools before relying on them." "Terrapod doctor gives actionable missing optional-stack guidance"

doctor_unsupported_os_release="$tmp_dir/doctor-unsupported-os-release"
write_os_release "$doctor_unsupported_os_release" debian 12 "Debian GNU/Linux 12"

doctor_unsupported_path="$(status_doctor_path doctor-unsupported chezmoi git zsh mise nvim zellij apt)"
write_stub "$doctor_unsupported_path/uname" 'printf "%s\n" "Linux"'

if TERRAPOD_OS_RELEASE_FILE="$doctor_unsupported_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_unsupported_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-unsupported.out" 2>"$tmp_dir/doctor-unsupported.err"; then
  fail "Terrapod doctor fails on unsupported Linux"
fi

doctor_unsupported_output="$(cat "$tmp_dir/doctor-unsupported.out")"
assert_contains "$doctor_unsupported_output" "warn - unsupported Linux release: Debian GNU/Linux 12. Terrapod supports Ubuntu 24.04 for the VPS Shell Profile." "Terrapod doctor explains unsupported Linux"

doctor_broad_upgrade_calls="$tmp_dir/doctor-broad-upgrade.calls"
rm -f "$doctor_broad_upgrade_calls"
doctor_broad_upgrade_path="$(status_doctor_path doctor-broad-upgrade chezmoi git zsh nvim gemini claude codex zellij)"
for command_name in brew apt sudo mise npm; do
  write_failing_command_stub "$doctor_broad_upgrade_path/$command_name" "$doctor_broad_upgrade_calls"
done
write_stub "$doctor_broad_upgrade_path/uname" 'printf "%s\n" "Darwin"'

if TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_broad_upgrade_path" \
  /bin/sh "$terrapod" status >/dev/null 2>/dev/null; then
  :
fi

if TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_broad_upgrade_path" \
  /bin/sh "$terrapod" doctor >/dev/null 2>/dev/null; then
  :
fi

if [ -e "$doctor_broad_upgrade_calls" ]; then
  printf '%s\n' "unexpected broad upgrade command calls from status/doctor:" >&2
  sed 's/^/  /' "$doctor_broad_upgrade_calls" >&2
  fail "Terrapod status and doctor do not call brew, apt, sudo, mise, or npm upgrade flows"
fi

pass "Terrapod status and doctor do not call brew, apt, sudo, mise, or npm upgrade flows"

export CHEZMOI_CALL_FILE="$tmp_dir/chezmoi-update.args"
export CHEZMOI_INVOKED_FILE="$tmp_dir/chezmoi-update.invoked"
export BROAD_UPGRADE_CALL_FILE="$tmp_dir/broad-upgrade.calls"

write_stub "$tmp_dir/bin/chezmoi" \
  'printf "%s\n" invoked >"$CHEZMOI_INVOKED_FILE"' \
  ': >"$CHEZMOI_CALL_FILE"' \
  'for arg do' \
  '  printf "%s\n" "$arg" >>"$CHEZMOI_CALL_FILE"' \
  'done' \
  'exit 0'

write_stub "$tmp_dir/bin/brew" \
  'printf "%s\n" "brew $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/apt" \
  'printf "%s\n" "apt $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/sudo" \
  'printf "%s\n" "sudo $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/mise" \
  'printf "%s\n" "mise $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/npm" \
  'printf "%s\n" "npm $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/uname" \
  'printf "%s\n" "Darwin"'

update_home="$tmp_dir/update-home"
update_xdg="$tmp_dir/update-xdg"
update_config="$update_xdg/chezmoi/chezmoi.toml"
mkdir -p "$update_home" "$(dirname "$update_config")"

cat >"$update_config" <<'TOML'
[data]
enableEditorStack = true
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosDesktopApps = false
TOML

if ! HOME="$update_home" XDG_CONFIG_HOME="$update_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" update >"$tmp_dir/update.out" 2>"$tmp_dir/update.err"; then
  printf '%s\n' "update stdout:" >&2
  sed 's/^/  /' "$tmp_dir/update.out" >&2
  printf '%s\n' "update stderr:" >&2
  sed 's/^/  /' "$tmp_dir/update.err" >&2
  fail "Terrapod update runs successfully"
fi

update_output="$(cat "$tmp_dir/update.out")"

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod update delegates repository update semantics to chezmoi update" \
  update --exclude scripts

assert_contains \
  "$update_output" \
  "Terrapod update" \
  "Terrapod update prints command context"

assert_contains \
  "$update_output" \
  "Profile: macOS Terminal Profile" \
  "Terrapod update prints profile context"

assert_contains \
  "$update_output" \
  "Config: $update_config (present)" \
  "Terrapod update prints managed config context"

assert_contains \
  "$update_output" \
  "Delegating repository update to: chezmoi update --exclude scripts" \
  "Terrapod update explains the delegated command"

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod update does not call brew, apt, sudo, mise, or npm upgrade flows"
fi

pass "Terrapod update does not call brew, apt, sudo, mise, or npm upgrade flows"

override_config="$tmp_dir/override-chezmoi.toml"

cat >"$override_config" <<'TOML'
[data]
enableEditorStack = false
enableAiCliTools = true
enableDevelopmentWorkspace = false
enableMacosDesktopApps = false
TOML

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if ! HOME="$update_home" XDG_CONFIG_HOME="$update_xdg" TERRAPOD_CHEZMOI_CONFIG="$override_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" update >"$tmp_dir/update-override.out" 2>"$tmp_dir/update-override.err"; then
  printf '%s\n' "override update stdout:" >&2
  sed 's/^/  /' "$tmp_dir/update-override.out" >&2
  printf '%s\n' "override update stderr:" >&2
  sed 's/^/  /' "$tmp_dir/update-override.err" >&2
  fail "Terrapod update runs successfully with an explicit config override"
fi

override_update_output="$(cat "$tmp_dir/update-override.out")"

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod update passes explicit config overrides to chezmoi update" \
  --config "$override_config" update --exclude scripts

assert_contains \
  "$override_update_output" \
  "Config: $override_config (present)" \
  "Terrapod update prints explicit config override context"

assert_contains \
  "$override_update_output" \
  "Delegating repository update to: chezmoi --config $override_config update --exclude scripts" \
  "Terrapod update explains delegated explicit config command"

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls with explicit config override:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod update with explicit config override does not call broad upgrade flows"
fi

pass "Terrapod update with explicit config override does not call broad upgrade flows"

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if HOME="$update_home" XDG_CONFIG_HOME="$update_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" update --aggressive >"$tmp_dir/update-extra.out" 2>"$tmp_dir/update-extra.err"; then
  fail "Terrapod update rejects extra arguments"
fi

update_extra_error="$(cat "$tmp_dir/update-extra.err")"

assert_contains \
  "$update_extra_error" \
  "terrapod: update accepts no arguments" \
  "Terrapod update explains rejected extra arguments"

if [ -e "$CHEZMOI_INVOKED_FILE" ]; then
  fail "Terrapod update rejects extra arguments before calling chezmoi"
fi

pass "Terrapod update rejects extra arguments before calling chezmoi"

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls after rejected extra arguments:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod update rejects extra arguments before broad upgrade flows"
fi

pass "Terrapod update rejects extra arguments before broad upgrade flows"

export CHEZMOI_CALL_FILE="$tmp_dir/chezmoi-diff.args"
export CHEZMOI_INVOKED_FILE="$tmp_dir/chezmoi-diff.invoked"

write_stub "$tmp_dir/bin/chezmoi" \
  'printf "%s\n" invoked >"$CHEZMOI_INVOKED_FILE"' \
  ': >"$CHEZMOI_CALL_FILE"' \
  'for arg do' \
  '  printf "%s\n" "$arg" >>"$CHEZMOI_CALL_FILE"' \
  'done' \
  'printf "%s\n" "stub diff output"' \
  'exit 0'

write_stub "$tmp_dir/bin/uname" \
  'printf "%s\n" "Darwin"'

diff_home="$tmp_dir/diff-home"
diff_xdg="$tmp_dir/diff-xdg"
diff_config="$diff_xdg/chezmoi/chezmoi.toml"
mkdir -p "$diff_home" "$(dirname "$diff_config")"

cat >"$diff_config" <<'TOML'
[data]
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = true
enableMacosAppGroupTerminalApps = true
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = true
enableMacosAppGroupMonitoring = false
TOML

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" diff >"$tmp_dir/diff.out" 2>"$tmp_dir/diff.err"; then
  printf '%s\n' "diff stdout:" >&2
  sed 's/^/  /' "$tmp_dir/diff.out" >&2
  printf '%s\n' "diff stderr:" >&2
  sed 's/^/  /' "$tmp_dir/diff.err" >&2
  fail "Terrapod diff runs successfully"
fi

diff_output="$(cat "$tmp_dir/diff.out")"

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod diff delegates target-state diff behavior to chezmoi diff" \
  diff

assert_contains \
  "$diff_output" \
  "Terrapod diff" \
  "Terrapod diff prints command context"

assert_contains \
  "$diff_output" \
  "Profile: macOS Terminal Profile" \
  "Terrapod diff prints active profile context"

assert_contains \
  "$diff_output" \
  "Config: $diff_config (present)" \
  "Terrapod diff prints config context"

assert_contains \
  "$diff_output" \
  "Optional stacks:" \
  "Terrapod diff prints optional stack section header"

assert_contains \
  "$diff_output" \
  "Optional Editor Stack: enabled" \
  "Terrapod diff prints effective enabled Optional Editor Stack state"

assert_contains \
  "$diff_output" \
  "Optional AI Tool Stack: enabled" \
  "Terrapod diff prints effective enabled Optional AI Tool Stack state"

assert_contains \
  "$diff_output" \
  "Optional Development Workspace: enabled" \
  "Terrapod diff prints enabled Optional Development Workspace state"

assert_contains \
  "$diff_output" \
  "macOS App Groups:" \
  "Terrapod diff prints macOS App Groups section header"

assert_contains \
  "$diff_output" \
  "terminal-apps: enabled" \
  "Terrapod diff prints enabled terminal-apps macOS App Group state"

assert_contains \
  "$diff_output" \
  "automation: disabled" \
  "Terrapod diff prints disabled automation macOS App Group state"

assert_contains \
  "$diff_output" \
  "launcher: enabled" \
  "Terrapod diff prints enabled launcher macOS App Group state"

assert_contains \
  "$diff_output" \
  "monitoring: disabled" \
  "Terrapod diff prints disabled monitoring macOS App Group state"

assert_contains \
  "$diff_output" \
  "Delegating target-state diff to: chezmoi diff" \
  "Terrapod diff explains the delegated command"

assert_contains \
  "$diff_output" \
  "stub diff output" \
  "Terrapod diff includes delegated chezmoi diff output"

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$diff_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" diff --verbose >"$tmp_dir/diff-extra.out" 2>"$tmp_dir/diff-extra.err"; then
  fail "Terrapod diff rejects extra arguments"
fi

diff_extra_error="$(cat "$tmp_dir/diff-extra.err")"

assert_contains \
  "$diff_extra_error" \
  "terrapod: diff accepts no arguments" \
  "Terrapod diff explains rejected extra arguments"

if [ -e "$CHEZMOI_INVOKED_FILE" ]; then
  fail "Terrapod diff rejects extra arguments before calling chezmoi"
fi

pass "Terrapod diff rejects extra arguments before calling chezmoi"

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$diff_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" diff >"$tmp_dir/diff-override.out" 2>"$tmp_dir/diff-override.err"; then
  printf '%s\n' "override diff stdout:" >&2
  sed 's/^/  /' "$tmp_dir/diff-override.out" >&2
  printf '%s\n' "override diff stderr:" >&2
  sed 's/^/  /' "$tmp_dir/diff-override.err" >&2
  fail "Terrapod diff runs successfully with an explicit config override"
fi

diff_override_output="$(cat "$tmp_dir/diff-override.out")"

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod diff passes explicit config overrides to chezmoi diff" \
  --config "$diff_config" diff

assert_contains \
  "$diff_override_output" \
  "Delegating target-state diff to: chezmoi --config $diff_config diff" \
  "Terrapod diff explains delegated explicit config command"

export CHEZMOI_APPLY_ARGS_FILE="$tmp_dir/chezmoi-apply.args"
export CHEZMOI_APPLY_INVOKED_FILE="$tmp_dir/chezmoi-apply.invoked"
export CHEZMOI_MANAGED_ARGS_FILE="$tmp_dir/chezmoi-managed.args"

write_stub "$tmp_dir/bin/chezmoi" \
  'command_name=' \
  'for arg do' \
  '  case "$arg" in' \
  '    apply|managed)' \
  '      command_name="$arg"' \
  '      ;;' \
  '  esac' \
  'done' \
  'case "$command_name" in' \
  '  apply)' \
  '    printf "%s\n" invoked >"$CHEZMOI_APPLY_INVOKED_FILE"' \
  '    : >"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" "stub apply output"' \
  '    exit 0' \
  '    ;;' \
  '  managed)' \
  '    : >"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" ".local/bin/terrapod"' \
  '    printf "%s\n" ".local/bin/tpod"' \
  '    exit 0' \
  '    ;;' \
  'esac' \
  'printf "%s\n" "unexpected chezmoi command: $*" >&2' \
  'exit 91'

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply.out" 2>"$tmp_dir/apply.err"; then
  printf '%s\n' "apply stdout:" >&2
  sed 's/^/  /' "$tmp_dir/apply.out" >&2
  printf '%s\n' "apply stderr:" >&2
  sed 's/^/  /' "$tmp_dir/apply.err" >&2
  fail "Terrapod apply runs successfully"
fi

apply_output="$(cat "$tmp_dir/apply.out")"

assert_call_args \
  "$CHEZMOI_APPLY_ARGS_FILE" \
  "Terrapod apply delegates target-state apply behavior to chezmoi apply" \
  apply

assert_call_args \
  "$CHEZMOI_MANAGED_ARGS_FILE" \
  "Terrapod apply validates managed targets with chezmoi managed" \
  managed

assert_contains \
  "$apply_output" \
  "Terrapod apply" \
  "Terrapod apply prints command context"

assert_contains \
  "$apply_output" \
  "Profile: macOS Terminal Profile" \
  "Terrapod apply prints active profile context"

assert_contains \
  "$apply_output" \
  "Config: $diff_config (present)" \
  "Terrapod apply prints config context"

assert_contains \
  "$apply_output" \
  "Optional stacks:" \
  "Terrapod apply prints optional stack section header"

assert_contains \
  "$apply_output" \
  "Optional Editor Stack: enabled" \
  "Terrapod apply prints effective enabled Optional Editor Stack state"

assert_contains \
  "$apply_output" \
  "Optional AI Tool Stack: enabled" \
  "Terrapod apply prints effective enabled Optional AI Tool Stack state"

assert_contains \
  "$apply_output" \
  "Optional Development Workspace: enabled" \
  "Terrapod apply prints enabled Optional Development Workspace state"

assert_contains \
  "$apply_output" \
  "terminal-apps: enabled" \
  "Terrapod apply prints enabled terminal-apps macOS App Group state"

assert_contains \
  "$apply_output" \
  "automation: disabled" \
  "Terrapod apply prints disabled automation macOS App Group state"

assert_contains \
  "$apply_output" \
  "launcher: enabled" \
  "Terrapod apply prints enabled launcher macOS App Group state"

assert_contains \
  "$apply_output" \
  "monitoring: disabled" \
  "Terrapod apply prints disabled monitoring macOS App Group state"

assert_contains \
  "$apply_output" \
  "Preflight: chezmoi is available" \
  "Terrapod apply confirms chezmoi preflight"

assert_contains \
  "$apply_output" \
  "Preflight: config file is readable" \
  "Terrapod apply confirms readable config preflight"

assert_contains \
  "$apply_output" \
  "Delegating target-state apply to: chezmoi apply" \
  "Terrapod apply explains the delegated command"

assert_contains \
  "$apply_output" \
  "stub apply output" \
  "Terrapod apply includes delegated chezmoi apply output"

assert_contains \
  "$apply_output" \
  "Post-apply validation: Terrapod command is managed" \
  "Terrapod apply validates the Terrapod command managed target"

assert_contains \
  "$apply_output" \
  "Post-apply validation: tpod alias is managed" \
  "Terrapod apply validates the tpod alias managed target"

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply --dry-run >"$tmp_dir/apply-extra.out" 2>"$tmp_dir/apply-extra.err"; then
  fail "Terrapod apply rejects extra arguments"
fi

apply_extra_error="$(cat "$tmp_dir/apply-extra.err")"

assert_contains \
  "$apply_extra_error" \
  "terrapod: apply accepts no arguments" \
  "Terrapod apply explains rejected extra arguments"

if [ -e "$CHEZMOI_APPLY_INVOKED_FILE" ]; then
  fail "Terrapod apply rejects extra arguments before calling chezmoi apply"
fi

pass "Terrapod apply rejects extra arguments before calling chezmoi apply"

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$diff_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-override.out" 2>"$tmp_dir/apply-override.err"; then
  printf '%s\n' "override apply stdout:" >&2
  sed 's/^/  /' "$tmp_dir/apply-override.out" >&2
  printf '%s\n' "override apply stderr:" >&2
  sed 's/^/  /' "$tmp_dir/apply-override.err" >&2
  fail "Terrapod apply runs successfully with an explicit config override"
fi

apply_override_output="$(cat "$tmp_dir/apply-override.out")"

assert_call_args \
  "$CHEZMOI_APPLY_ARGS_FILE" \
  "Terrapod apply passes explicit config overrides to chezmoi apply" \
  --config "$diff_config" apply

assert_call_args \
  "$CHEZMOI_MANAGED_ARGS_FILE" \
  "Terrapod apply passes explicit config overrides to chezmoi managed" \
  --config "$diff_config" managed

assert_contains \
  "$apply_override_output" \
  "Delegating target-state apply to: chezmoi --config $diff_config apply" \
  "Terrapod apply explains delegated explicit config command"

symlink_config_target="$tmp_dir/symlink-target-chezmoi.toml"
symlink_config="$tmp_dir/symlink-chezmoi.toml"
cp "$diff_config" "$symlink_config_target"
ln -s "$symlink_config_target" "$symlink_config"

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$symlink_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-symlink.out" 2>"$tmp_dir/apply-symlink.err"; then
  printf '%s\n' "symlink config apply stdout:" >&2
  sed 's/^/  /' "$tmp_dir/apply-symlink.out" >&2
  printf '%s\n' "symlink config apply stderr:" >&2
  sed 's/^/  /' "$tmp_dir/apply-symlink.err" >&2
  fail "Terrapod apply accepts a symlink to a regular config file"
fi

apply_symlink_output="$(cat "$tmp_dir/apply-symlink.out")"

assert_call_args \
  "$CHEZMOI_APPLY_ARGS_FILE" \
  "Terrapod apply delegates with a symlink config path" \
  --config "$symlink_config" apply

assert_call_args \
  "$CHEZMOI_MANAGED_ARGS_FILE" \
  "Terrapod apply validates managed targets with a symlink config path" \
  --config "$symlink_config" managed

assert_contains \
  "$apply_symlink_output" \
  "Preflight: config file is readable" \
  "Terrapod apply treats symlinked config files as readable"

write_stub "$tmp_dir/bin/chezmoi" \
  'command_name=' \
  'for arg do' \
  '  case "$arg" in' \
  '    apply|managed)' \
  '      command_name="$arg"' \
  '      ;;' \
  '  esac' \
  'done' \
  'case "$command_name" in' \
  '  apply)' \
  '    printf "%s\n" invoked >"$CHEZMOI_APPLY_INVOKED_FILE"' \
  '    : >"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" "stub apply output"' \
  '    exit 0' \
  '    ;;' \
  '  managed)' \
  '    : >"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" ".local/bin/terrapod"' \
  '    exit 0' \
  '    ;;' \
  'esac' \
  'printf "%s\n" "unexpected chezmoi command: $*" >&2' \
  'exit 91'

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-validation.out" 2>"$tmp_dir/apply-validation.err"; then
  fail "Terrapod apply fails when tpod alias is not managed"
fi

apply_validation_error="$(cat "$tmp_dir/apply-validation.err")"

assert_contains \
  "$apply_validation_error" \
  "terrapod: post-apply validation failed: tpod alias is not managed (.local/bin/tpod missing)" \
  "Terrapod apply explains missing tpod alias managed target"

assert_contains \
  "$apply_validation_error" \
  "Run 'terrapod chezmoi -- managed' to inspect managed targets, then rerun 'terrapod apply'." \
  "Terrapod apply gives actionable post-apply validation guidance"

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$diff_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-override-validation.out" 2>"$tmp_dir/apply-override-validation.err"; then
  fail "Terrapod apply fails when tpod alias is not managed with an explicit config override"
fi

apply_override_validation_error="$(cat "$tmp_dir/apply-override-validation.err")"

assert_contains \
  "$apply_override_validation_error" \
  "terrapod: post-apply validation failed: tpod alias is not managed (.local/bin/tpod missing)" \
  "Terrapod apply explains missing tpod alias managed target with an explicit config override"

assert_contains \
  "$apply_override_validation_error" \
  "Run 'terrapod chezmoi -- --config $diff_config managed' to inspect managed targets, then rerun 'terrapod apply'." \
  "Terrapod apply gives config-aware post-apply validation guidance"
