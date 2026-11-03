#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir

library="$repo_root/dot_local/lib/terrapod/config-toml.sh"
. "$library"

settings="$(
  printf '%s\n' 'profile = "macos-terminal"'
  managed_setting_keys | while IFS= read -r key; do
    printf '%s = true\n' "$key"
  done
)"
destination="$tmp_dir/new/chezmoi.toml"
write_managed_settings "$destination" "$settings"
managed_setup_config_load "$destination"
assert_equals "$managed_setup_config_state" complete "a direct library write creates a complete config"
assert_equals "$(managed_setup_stored_profile)" macos-terminal "a direct library write preserves the profile"
for key in $(managed_setting_keys); do
  managed_setting_enabled "$key" || fail "a direct library write enables $key"
done
pass "all schema settings round trip through a direct library write"
assert_equals "$(find "$tmp_dir/new" -name "*.terrapod-backup-*" -print)" "" "a new config needs no backup"

# Fail local filesystem operations after preflight. Each case uses its own
# destination so backup names remain independent of clock resolution.
for operation in mkdir mktemp cp stat tr awk chmod mv; do
  case_dir="$tmp_dir/failure-$operation"
  mkdir -p "$case_dir/bin" "$case_dir/config-dir"
  destination="$case_dir/config-dir/config"
  printf '%s\n' '[data]' 'profile = "macos-terminal"' >"$destination"
  chmod 600 "$destination"
  cp "$destination" "$case_dir/before"
  {
    printf '%s\n' '#!/bin/sh'
    if [ "$operation" = mktemp ]; then
      printf '%s\n' 'case "$1" in */.terrapod-data.*) exit 73 ;; esac'
      printf 'exec "%s" "$@"\n' "$(command -v mktemp)"
    else
      printf '%s\n' 'exit 73'
    fi
  } >"$case_dir/bin/$operation"
  chmod +x "$case_dir/bin/$operation"
  if (PATH="$case_dir/bin:$PATH" write_managed_settings "$destination" "$settings"); then
    fail "$operation failure must stop a direct write"
  fi
  cmp -s "$destination" "$case_dir/before" || fail "$operation failure preserves the previous config"
  assert_equals "$(file_mode "$destination")" 600 "$operation failure preserves the previous mode"
  assert_equals "$(find "$case_dir/config-dir" -name '.terrapod-*' -print)" "" "$operation failure cleans staging files"
done

for signal in HUP INT TERM; do
  case_dir="$tmp_dir/signal-$signal"
  mkdir -p "$case_dir/bin" "$case_dir/config-dir"
  destination="$case_dir/config-dir/config"
  printf '%s\n' '[data]' 'profile = "macos-terminal"' >"$destination"
  chmod 600 "$destination"
  cp "$destination" "$case_dir/before"
  cat >"$case_dir/bin/mv" <<'SH'
#!/bin/sh
parent_pid="$(ps -o ppid= -p "$$" | tr -d ' ')"
kill -"$WRITE_TEST_SIGNAL" "$parent_pid"
exit 73
SH
  chmod +x "$case_dir/bin/mv"
  if (WRITE_TEST_SIGNAL="$signal" PATH="$case_dir/bin:$PATH" write_managed_settings "$destination" "$settings"); then
    fail "$signal interruption must stop a direct write"
  fi
  cmp -s "$destination" "$case_dir/before" || fail "$signal interruption preserves the previous config"
  assert_equals "$(file_mode "$destination")" 600 "$signal interruption preserves the previous mode"
  assert_equals "$(find "$case_dir/config-dir" -name '.terrapod-*' -print)" "" "$signal interruption cleans staging files"
done

# Reuse a replacement failure for the caller-handler checks below.
mkdir "$tmp_dir/fail-bin"
cat >"$tmp_dir/fail-bin/mv" <<'SH'
#!/bin/sh
exit 73
SH
chmod +x "$tmp_dir/fail-bin/mv"
disabled_settings="$(
  printf '%s\n' 'profile = "vps-shell"'
  managed_setting_keys | while IFS= read -r key; do
    printf '%s = false\n' "$key"
  done
)"
for dialect in table quoted-header literal-header dotted; do
  destination="$tmp_dir/$dialect.toml"
  case "$dialect" in
    table) header='[data] # managed'; prefix= ;;
    quoted-header) header=' [ "data" ] # managed'; prefix= ;;
    literal-header) header="[ 'data' ] # managed"; prefix= ;;
    dotted) header=; prefix="'data' . " ;;
  esac
  {
    printf '%s\n' '# unrelated root comment' 'sourceDir = "/keep/source"'
    [ -z "$header" ] || printf '%s\n' "$header"
    printf '%s"profile" = "macos-terminal" # old profile\n' "$prefix"
    printf "%s'enableEditorStack' = \"false\" # replace rejected value\n" "$prefix"
    printf '%senableAiCliTools = true # old value\n' "$prefix"
    printf '%senableMacosDesktopApps = true\n' "$prefix"
    printf '%sterrapodPreset = "development"\n' "$prefix"
    printf '%scustom = "keep this value" # unrelated data\n' "$prefix"
    printf '%s\n' '[other]' 'profile = "keep this profile"' 'enableEditorStack = "keep this setting"'
  } >"$destination"
  chmod 600 "$destination"
  cp "$destination" "$tmp_dir/$dialect.before"
  write_managed_settings "$destination" "$disabled_settings"
  managed_setup_config_load "$destination"
  assert_equals "$managed_setup_config_state" complete "$dialect round trip is complete"
  assert_equals "$(managed_setup_stored_profile)" vps-shell "$dialect round trip updates the profile"
  for key in $(managed_setting_keys); do
    if managed_setting_enabled "$key"; then fail "$dialect round trip disables $key"; fi
  done
  assert_file_contains "$destination" 'sourceDir = "/keep/source"' "$dialect preserves unrelated root content"
  assert_file_contains "$destination" 'custom = "keep this value" # unrelated data' "$dialect preserves unrelated data and comments"
  assert_file_contains "$destination" 'profile = "keep this profile"' "$dialect preserves the unrelated table profile"
  assert_file_contains "$destination" 'enableEditorStack = "keep this setting"' "$dialect preserves an unrelated table setting"
  assert_file_not_contains "$destination" enableMacosDesktopApps "$dialect removes retired settings"
  assert_file_not_contains "$destination" terrapodPreset "$dialect removes the retired Preset key"
  assert_equals "$(file_mode "$destination")" 600 "$dialect preserves file mode"
  set -- "$destination".terrapod-backup-*
  assert_equals "$#" 1 "$dialect creates exactly one backup"
  cmp -s "$1" "$tmp_dir/$dialect.before" || fail "$dialect backup matches the original bytes"
  pass "$dialect backup matches the original bytes"
done

# Replacing a readable, read-only file is allowed when its parent permits
# backup and atomic replacement. A preflight must judge those operations.
destination="$tmp_dir/read-only.toml"
printf '%s\n' '[data]' 'profile = "macos-terminal"' >"$destination"
chmod 400 "$destination"
managed_setup_config_write_preflight "$destination"
printf '%s\n' "$disabled_settings" >"$tmp_dir/settings"
cat >"$tmp_dir/write-in-terminal" <<'SH'
#!/bin/sh
set -eu
. "$1"
write_managed_settings "$2" "$(cat "$3")"
SH
terminal_command="sh $(shell_quote "$tmp_dir/write-in-terminal") $(shell_quote "$library") $(shell_quote "$destination") $(shell_quote "$tmp_dir/settings")"
printf 'n\n' | run_in_pty "$terminal_command" >"$tmp_dir/read-only-terminal.log" 2>&1
managed_setup_config_load "$destination"
assert_equals "$managed_setup_config_state" complete "a readable config can be replaced in a writable parent"
assert_equals "$(file_mode "$destination")" 400 "replacing a read-only config preserves its mode"
assert_file_not_contains "$tmp_dir/read-only-terminal.log" 'override ' "a library write requires no extra overwrite prompt"

# Sourcing and writes preserve caller handlers under both supported shells.
for writer_shell in sh dash; do
  if ! command -v "$writer_shell" >/dev/null 2>&1; then
    skip "direct library trap checks under $writer_shell (not installed)"
    continue
  fi
  mkdir "$tmp_dir/traps-$writer_shell"
  "$writer_shell" -c '
    set -eu
    library="$1"; work="$2"; settings="$3"; fail_bin="$4"
    trap '\''printf "%s\n" EXIT >>"$work/handlers"'\'' EXIT
    trap '\''printf "%s\n" HUP >>"$work/handlers"'\'' HUP
    trap '\''printf "%s\n" INT >>"$work/handlers"'\'' INT
    trap '\''printf "%s\n" TERM >>"$work/handlers"'\'' TERM
    before="$(trap)"
    . "$library"
    [ "$(trap)" = "$before" ] || exit 11
    [ -z "$(ls -A "$work")" ] || exit 12
    write_managed_settings "$work/config" "$settings"
    [ "$(trap)" = "$before" ] || exit 13
    if (PATH="$fail_bin:$PATH" write_managed_settings "$work/config" "$settings"); then exit 14; fi
    [ "$(trap)" = "$before" ] || exit 15
    if write_managed_settings "$work" "$settings" 2>/dev/null; then exit 16; fi
    [ "$(trap)" = "$before" ] || exit 17
    kill -HUP "$$"
    kill -INT "$$"
    kill -TERM "$$"
  ' "$writer_shell" "$library" "$tmp_dir/traps-$writer_shell" "$settings" "$tmp_dir/fail-bin"
  assert_equals "$(cat "$tmp_dir/traps-$writer_shell/handlers")" 'HUP
INT
TERM
EXIT' "$writer_shell preserves caller handlers when sourcing, succeeding, and failing"
done
