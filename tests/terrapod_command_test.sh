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

help_output="$(sh "$terrapod" help)"

ln -s "$terrapod" "$tmp_dir/bin/tpod"
tpod_help_output="$(PATH="$tmp_dir/bin:/usr/bin:/bin" "$tmp_dir/bin/tpod" help)"

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
  update

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
  "Delegating repository update to: chezmoi update" \
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
  --config "$override_config" update

assert_contains \
  "$override_update_output" \
  "Config: $override_config (present)" \
  "Terrapod update prints explicit config override context"

assert_contains \
  "$override_update_output" \
  "Delegating repository update to: chezmoi --config $override_config update" \
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
