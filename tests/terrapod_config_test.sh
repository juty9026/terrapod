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

assert_contains() {
  file="$1"
  needle="$2"
  message="$3"

  if ! grep -F "$needle" "$file" >/dev/null; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  file="$1"
  needle="$2"
  message="$3"

  if grep -F "$needle" "$file" >/dev/null; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    fail "$message"
  fi

  pass "$message"
}

extract_data_section() {
  file="$1"

  awk '
    function is_data_section(line) {
      return line ~ "^[[:space:]]*(\\[data\\]|\\[\"data\"\\]|\\[\047data\047\\])[[:space:]]*($|#)"
    }

    function is_section(line) {
      return line ~ /^[[:space:]]*(\[[^]]+\]|\[\[[^]]+\]\])[[:space:]]*($|#)/
    }

    {
      if (is_data_section($0)) {
        in_data = 1
        print
        next
      }

      if (in_data && is_section($0)) {
        exit
      }

      if (in_data) {
        print
      }
    }
  ' "$file"
}

assert_data_key_once_with_value() {
  file="$1"
  key="$2"
  expected_value="$3"
  message="$4"
  expected_line="$key = $expected_value"
  data_section="$(extract_data_section "$file")"

  actual_count="$(
    printf '%s\n' "$data_section" |
      grep -E "^[[:space:]]*(\"$key\"|'$key'|$key)[[:space:]]*=" |
      wc -l |
      tr -d ' '
  )"

  if [ "$actual_count" -ne 1 ]; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    printf '%s\n' "data section:" >&2
    printf '%s\n' "$data_section" | sed 's/^/  /' >&2
    fail "$message; expected one $key entry in [data], found $actual_count"
  fi

  if ! printf '%s\n' "$data_section" | grep -Fx "$expected_line" >/dev/null; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    printf '%s\n' "data section:" >&2
    printf '%s\n' "$data_section" | sed 's/^/  /' >&2
    fail "$message; expected $expected_line in [data]"
  fi

  pass "$message"
}

assert_lines_after_header_not_contains() {
  file="$1"
  header="$2"
  needle="$3"
  message="$4"

  if awk -v header="$header" '
    found {
      print
    }

    $0 == header {
      found = 1
    }
  ' "$file" | grep -F "$needle" >/dev/null; then
    printf '%s\n' "file contents:" >&2
    sed 's/^/  /' "$file" >&2
    fail "$message"
  fi

  pass "$message"
}

file_mode() {
  path="$1"

  if mode="$(stat -c %a "$path" 2>/dev/null)"; then
    printf '%s\n' "$mode"
    return 0
  fi

  stat -f %Lp "$path"
}

assert_backup_count() {
  config_file="$1"
  expected_count="$2"
  message="$3"

  set -- "$config_file".terrapod-backup-*
  if [ "$1" = "$config_file.terrapod-backup-*" ]; then
    actual_count=0
  else
    actual_count=$#
  fi

  if [ "$actual_count" -ne "$expected_count" ]; then
    fail "$message; expected $expected_count backup(s), found $actual_count"
  fi

  pass "$message"
}

assert_single_backup_matches() {
  config_file="$1"
  expected_file="$2"
  message="$3"

  set -- "$config_file".terrapod-backup-*
  if [ "$1" = "$config_file.terrapod-backup-*" ]; then
    fail "$message; expected one backup, found 0"
  fi

  if [ "$#" -ne 1 ]; then
    fail "$message; expected one backup, found $#"
  fi

  if ! cmp -s "$1" "$expected_file"; then
    printf '%s\n' "expected backup contents:" >&2
    sed 's/^/  /' "$expected_file" >&2
    printf '%s\n' "actual backup contents:" >&2
    sed 's/^/  /' "$1" >&2
    fail "$message"
  fi

  pass "$message"
}

assert_no_terrapod_temp_files() {
  config_file="$1"
  message="$2"
  config_dir="$(dirname -- "$config_file")"
  found_file="$tmp_dir/found-temp-files"

  find "$config_dir" \
    \( -name '.terrapod-config.*' \
    -o -name '.terrapod-data.*' \
    -o -name "$(basename -- "$config_file").terrapod-tmp-*" \
    -o -name "$(basename -- "$config_file").terrapod-data-*" \) \
    -print >"$found_file"

  if [ -s "$found_file" ]; then
    printf '%s\n' "unexpected temp files:" >&2
    sed 's/^/  /' "$found_file" >&2
    fail "$message"
  fi

  pass "$message"
}

assert_no_terrapod_artifacts_near_path() {
  path="$1"
  message="$2"
  path_dir="$(dirname -- "$path")"
  path_base="$(basename -- "$path")"
  found_file="$tmp_dir/found-artifacts"

  find "$path_dir" \
    \( -name '.terrapod-config.*' \
    -o -name '.terrapod-data.*' \
    -o -name "$path_base.terrapod-backup-*" \
    -o -name "$path_base.terrapod-tmp-*" \
    -o -name "$path_base.terrapod-data-*" \) \
    -print >"$found_file"

  if [ -s "$found_file" ]; then
    printf '%s\n' "unexpected Terrapod artifacts:" >&2
    sed 's/^/  /' "$found_file" >&2
    fail "$message"
  fi

  pass "$message"
}

run_terrapod_configure() {
  preset="$1"
  input="${2:-}"
  home_dir="$3"
  xdg_config_home="$4"

  if [ -n "$input" ]; then
    printf '%s\n' "$input" |
      TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" configure "$preset"
  else
    TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" configure "$preset" </dev/null
  fi
}

terrapod="$repo_root/dot_local/bin/executable_terrapod"

new_home="$tmp_dir/new-home"
new_xdg="$tmp_dir/new-xdg"
new_config="$new_xdg/chezmoi/chezmoi.toml"
mkdir -p "$new_home"

run_terrapod_configure minimal "" "$new_home" "$new_xdg"

if [ ! -f "$new_config" ]; then
  fail "minimal Preset creates a chezmoi config file"
fi
pass "minimal Preset creates a chezmoi config file"

assert_contains "$new_config" "[data]" "new config contains a data section"
assert_data_key_once_with_value "$new_config" "enableEditorStack" "false" "minimal Preset disables Optional Editor Stack exactly once in data"
assert_data_key_once_with_value "$new_config" "enableAiCliTools" "false" "minimal Preset disables Optional AI Tool Stack exactly once in data"
assert_data_key_once_with_value "$new_config" "enableDevelopmentWorkspace" "false" "minimal Preset disables Optional Development Workspace exactly once in data"
assert_data_key_once_with_value "$new_config" "enableMacosDesktopApps" "false" "minimal Preset disables macOS App Groups through the desktop-app boundary exactly once in data"
assert_not_contains "$new_config" "terrapodPreset" "minimal Preset stores concrete values instead of a dynamic Preset"
assert_backup_count "$new_config" 0 "new config creation does not create a backup"

workstation_home="$tmp_dir/workstation-home"
workstation_xdg="$tmp_dir/workstation-xdg"
workstation_config="$workstation_xdg/chezmoi/chezmoi.toml"
mkdir -p "$workstation_home"

run_terrapod_configure workstation "" "$workstation_home" "$workstation_xdg"

if [ ! -f "$workstation_config" ]; then
  fail "workstation Preset creates a chezmoi config file"
fi
pass "workstation Preset creates a chezmoi config file"

assert_data_key_once_with_value "$workstation_config" "enableEditorStack" "true" "workstation Preset enables Optional Editor Stack exactly once in data"
assert_data_key_once_with_value "$workstation_config" "enableAiCliTools" "true" "workstation Preset enables Optional AI Tool Stack exactly once in data"
assert_data_key_once_with_value "$workstation_config" "enableDevelopmentWorkspace" "true" "workstation Preset enables Optional Development Workspace exactly once in data"
assert_data_key_once_with_value "$workstation_config" "enableMacosDesktopApps" "true" "workstation Preset enables macOS App Groups through the desktop-app boundary exactly once in data"
assert_not_contains "$workstation_config" "terrapodPreset" "workstation Preset stores concrete values instead of a dynamic Preset"
assert_backup_count "$workstation_config" 0 "workstation config creation does not create a backup"

default_env_home="$tmp_dir/default-env-home"
default_env_xdg="$tmp_dir/default-env-xdg"
default_env_config="$default_env_xdg/chezmoi/chezmoi.toml"
escape_config="$tmp_dir/escape.toml"
mkdir -p "$default_env_home"

export TERRAPOD_CHEZMOI_CONFIG="$escape_config"
run_terrapod_configure minimal "" "$default_env_home" "$default_env_xdg"
unset TERRAPOD_CHEZMOI_CONFIG

if [ ! -f "$default_env_config" ]; then
  fail "default-path configure ignores exported TERRAPOD_CHEZMOI_CONFIG"
fi
pass "default-path configure ignores exported TERRAPOD_CHEZMOI_CONFIG"

if [ -e "$escape_config" ]; then
  fail "default-path configure does not write to exported TERRAPOD_CHEZMOI_CONFIG"
fi
pass "default-path configure does not write to exported TERRAPOD_CHEZMOI_CONFIG"

existing_home="$tmp_dir/existing-home"
existing_xdg="$tmp_dir/existing-xdg"
existing_config="$existing_xdg/chezmoi/chezmoi.toml"
mkdir -p "$existing_home" "$(dirname "$existing_config")"

cat >"$existing_config" <<'TOML'
[sourceState]
branch = "main"

[data]
email = "minu@example.com"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosDesktopApps = true
terrapodPreset = "minimal"

[edit]
command = "nvim"
TOML
cp "$existing_config" "$tmp_dir/existing-before.toml"

run_terrapod_configure development "y" "$existing_home" "$existing_xdg"

assert_contains "$existing_config" "branch = \"main\"" "existing update preserves unrelated sourceState values"
assert_contains "$existing_config" "email = \"minu@example.com\"" "existing update preserves unrelated data values"
assert_contains "$existing_config" "command = \"nvim\"" "existing update preserves unrelated later sections"
assert_data_key_once_with_value "$existing_config" "enableEditorStack" "true" "development Preset enables Optional Editor Stack exactly once in data"
assert_data_key_once_with_value "$existing_config" "enableAiCliTools" "true" "development Preset enables Optional AI Tool Stack exactly once in data"
assert_data_key_once_with_value "$existing_config" "enableDevelopmentWorkspace" "true" "development Preset enables Optional Development Workspace exactly once in data"
assert_data_key_once_with_value "$existing_config" "enableMacosDesktopApps" "false" "development Preset leaves macOS App Groups disabled exactly once in data"
assert_not_contains "$existing_config" "terrapodPreset" "existing update removes stale dynamic Preset key"
assert_single_backup_matches "$existing_config" "$tmp_dir/existing-before.toml" "existing update creates one backup before changing managed keys"

quoted_table_home="$tmp_dir/quoted-table-home"
quoted_table_xdg="$tmp_dir/quoted-table-xdg"
quoted_table_config="$quoted_table_xdg/chezmoi/chezmoi.toml"
mkdir -p "$quoted_table_home" "$(dirname "$quoted_table_config")"

cat >"$quoted_table_config" <<'TOML'
["data"]
keepQuotedTable = "preserve"
enableEditorStack = false
terrapodPreset = "minimal"

[sourceState]
branch = "main"
TOML

run_terrapod_configure development "y" "$quoted_table_home" "$quoted_table_xdg"

assert_contains "$quoted_table_config" "[\"data\"]" "quoted data table header is preserved"
assert_not_contains "$quoted_table_config" "[data]" "quoted data table update does not append a duplicate bare data table"
assert_contains "$quoted_table_config" "keepQuotedTable = \"preserve\"" "quoted data table update preserves unrelated data values"
assert_contains "$quoted_table_config" "branch = \"main\"" "quoted data table update preserves later sections"
assert_data_key_once_with_value "$quoted_table_config" "enableEditorStack" "true" "quoted data table update writes Editor Stack in data"
assert_data_key_once_with_value "$quoted_table_config" "enableAiCliTools" "true" "quoted data table update writes AI Tool Stack in data"
assert_data_key_once_with_value "$quoted_table_config" "enableDevelopmentWorkspace" "true" "quoted data table update writes Development Workspace in data"
assert_data_key_once_with_value "$quoted_table_config" "enableMacosDesktopApps" "false" "quoted data table update writes macOS desktop-app boundary in data"
assert_not_contains "$quoted_table_config" "terrapodPreset" "quoted data table update removes stale dynamic Preset key"

quoted_home="$tmp_dir/quoted-home"
quoted_xdg="$tmp_dir/quoted-xdg"
quoted_config="$quoted_xdg/chezmoi/chezmoi.toml"
mkdir -p "$quoted_home" "$(dirname "$quoted_config")"

cat >"$quoted_config" <<'TOML'
[data]
'enableEditorStack' = false
'enableAiCliTools' = false
'enableDevelopmentWorkspace' = false
'enableMacosDesktopApps' = true
'terrapodPreset' = "minimal"
keepLiteral = "preserve"
TOML

run_terrapod_configure development "y" "$quoted_home" "$quoted_xdg"

assert_data_key_once_with_value "$quoted_config" "enableEditorStack" "true" "quoted managed key update writes one Editor Stack value in data"
assert_data_key_once_with_value "$quoted_config" "enableAiCliTools" "true" "quoted managed key update writes one AI Tool Stack value in data"
assert_data_key_once_with_value "$quoted_config" "enableDevelopmentWorkspace" "true" "quoted managed key update writes one Development Workspace value in data"
assert_data_key_once_with_value "$quoted_config" "enableMacosDesktopApps" "false" "quoted managed key update writes one macOS desktop-app boundary value in data"
assert_not_contains "$quoted_config" "\"enableEditorStack\"" "quoted managed key update removes quoted Editor Stack key"
assert_not_contains "$quoted_config" "\"enableAiCliTools\"" "quoted managed key update removes quoted AI Tool Stack key"
assert_not_contains "$quoted_config" "\"enableDevelopmentWorkspace\"" "quoted managed key update removes quoted Development Workspace key"
assert_not_contains "$quoted_config" "\"enableMacosDesktopApps\"" "quoted managed key update removes quoted macOS desktop-app boundary key"
assert_not_contains "$quoted_config" "'enableEditorStack'" "quoted managed key update removes literal Editor Stack key"
assert_not_contains "$quoted_config" "'enableAiCliTools'" "quoted managed key update removes literal AI Tool Stack key"
assert_not_contains "$quoted_config" "'enableDevelopmentWorkspace'" "quoted managed key update removes literal Development Workspace key"
assert_not_contains "$quoted_config" "'enableMacosDesktopApps'" "quoted managed key update removes literal macOS desktop-app boundary key"
assert_not_contains "$quoted_config" "terrapodPreset" "quoted managed key update removes stale dynamic Preset key"
assert_contains "$quoted_config" "keepLiteral = \"preserve\"" "quoted managed key update preserves unrelated literal data values"

array_home="$tmp_dir/array-home"
array_xdg="$tmp_dir/array-xdg"
array_config="$array_xdg/chezmoi/chezmoi.toml"
mkdir -p "$array_home" "$(dirname "$array_config")"

cat >"$array_config" <<'TOML'
[data]
keepMe = "yes"
enableEditorStack = false

[[merge.command]]
name = "external"
enableEditorStack = "do-not-touch"
TOML
chmod 600 "$array_config"

run_terrapod_configure development "y" "$array_home" "$array_xdg"

assert_contains "$array_config" "keepMe = \"yes\"" "array-table update preserves unrelated data values"
assert_data_key_once_with_value "$array_config" "enableEditorStack" "true" "array-table update writes Editor Stack only in data"
assert_data_key_once_with_value "$array_config" "enableAiCliTools" "true" "array-table update writes AI Tool Stack only in data"
assert_data_key_once_with_value "$array_config" "enableDevelopmentWorkspace" "true" "array-table update writes Development Workspace only in data"
assert_data_key_once_with_value "$array_config" "enableMacosDesktopApps" "false" "array-table update writes macOS desktop-app boundary only in data"
assert_contains "$array_config" "[[merge.command]]" "array-table update preserves TOML array table"
assert_contains "$array_config" "enableEditorStack = \"do-not-touch\"" "array-table update preserves same-named external key"
assert_lines_after_header_not_contains "$array_config" "[[merge.command]]" "enableAiCliTools = true" "array-table update does not append AI Tool Stack under array table"
assert_lines_after_header_not_contains "$array_config" "[[merge.command]]" "enableDevelopmentWorkspace = true" "array-table update does not append Development Workspace under array table"
assert_lines_after_header_not_contains "$array_config" "[[merge.command]]" "enableMacosDesktopApps = false" "array-table update does not append macOS desktop-app boundary under array table"

array_mode="$(file_mode "$array_config")"
if [ "$array_mode" != "600" ]; then
  fail "existing update preserves config file mode; expected 600, got $array_mode"
fi
pass "existing update preserves config file mode"

assert_no_terrapod_temp_files "$array_config" "successful array-table update cleans Terrapod temp files"

fake_stat_home="$tmp_dir/fake-stat-home"
fake_stat_xdg="$tmp_dir/fake-stat-xdg"
fake_stat_config="$fake_stat_xdg/chezmoi/chezmoi.toml"
fake_stat_bin="$tmp_dir/fake-stat-bin"
mkdir -p "$fake_stat_home" "$(dirname "$fake_stat_config")" "$fake_stat_bin"

cat >"$fake_stat_config" <<'TOML'
[data]
enableEditorStack = false
TOML
chmod 600 "$fake_stat_config"

cat >"$fake_stat_bin/stat" <<'SH'
#!/bin/sh

if [ "${1:-}" = "-f" ]; then
  printf '%s\n' "GNU stat filesystem output that is not a file mode"
  exit 1
fi

if [ "${1:-}" = "-c" ] && [ "${2:-}" = "%a" ]; then
  printf '%s\n' "600"
  exit 0
fi

printf '%s\n' "unexpected stat invocation: $*" >&2
exit 2
SH
chmod +x "$fake_stat_bin/stat"

if ! printf '%s\n' "y" |
  PATH="$fake_stat_bin:$PATH" HOME="$fake_stat_home" XDG_CONFIG_HOME="$fake_stat_xdg" sh "$terrapod" configure development >"$tmp_dir/fake-stat.out" 2>"$tmp_dir/fake-stat.err"; then
  printf '%s\n' "stdout:" >&2
  sed 's/^/  /' "$tmp_dir/fake-stat.out" >&2
  printf '%s\n' "stderr:" >&2
  sed 's/^/  /' "$tmp_dir/fake-stat.err" >&2
  fail "existing update ignores stdout from failing GNU stat -f fallback"
fi
pass "existing update ignores stdout from failing GNU stat -f fallback"

fake_stat_mode="$(
  PATH="$fake_stat_bin:$PATH"
  file_mode "$fake_stat_config"
)"
if [ "$fake_stat_mode" != "600" ]; then
  fail "existing update preserves mode with GNU-first stat fallback; expected 600, got $fake_stat_mode"
fi
pass "existing update preserves mode with GNU-first stat fallback"

decline_home="$tmp_dir/decline-home"
decline_xdg="$tmp_dir/decline-xdg"
decline_config="$decline_xdg/chezmoi/chezmoi.toml"
mkdir -p "$decline_home" "$(dirname "$decline_config")"

cat >"$decline_config" <<'TOML'
[data]
enableEditorStack = false
keepMe = "yes"
TOML
cp "$decline_config" "$tmp_dir/decline-before.toml"

if run_terrapod_configure development "n" "$decline_home" "$decline_xdg" >"$tmp_dir/decline.out" 2>"$tmp_dir/decline.err"; then
  fail "existing config update can be declined"
fi
pass "existing config update can be declined"

if ! cmp -s "$decline_config" "$tmp_dir/decline-before.toml"; then
  fail "declined config update leaves existing config unchanged"
fi
pass "declined config update leaves existing config unchanged"

assert_contains "$tmp_dir/decline.err" "Update Terrapod-managed data keys" "existing config update asks before writing"
assert_backup_count "$decline_config" 0 "declined config update does not create a backup"

directory_home="$tmp_dir/directory-home"
directory_xdg="$tmp_dir/directory-xdg"
directory_config="$directory_xdg/chezmoi/chezmoi.toml"
mkdir -p "$directory_home" "$directory_config"

if TERRAPOD_CHEZMOI_CONFIG="$directory_config" HOME="$directory_home" XDG_CONFIG_HOME="$directory_xdg" sh "$terrapod" configure minimal >"$tmp_dir/directory.out" 2>"$tmp_dir/directory.err" </dev/null; then
  fail "config path that is a directory fails explicitly"
fi
pass "config path that is a directory fails explicitly"

assert_contains "$tmp_dir/directory.err" "not a regular file" "directory config path explains it is not usable"
assert_not_contains "$tmp_dir/directory.out" "Configured Terrapod Preset" "directory config path does not report success"
assert_no_terrapod_temp_files "$directory_config" "directory config path leaves no Terrapod temp files"

symlink_home="$tmp_dir/symlink-home"
symlink_xdg="$tmp_dir/symlink-xdg"
symlink_dir="$tmp_dir/symlink-config-dir"
symlink_target="$symlink_dir/target.toml"
symlink_config="$symlink_dir/symlink_config.toml"
mkdir -p "$symlink_home" "$symlink_dir"

cat >"$symlink_target" <<'TOML'
[data]
enableEditorStack = false
TOML
chmod 600 "$symlink_target"
cp "$symlink_target" "$tmp_dir/symlink-target-before.toml"
symlink_target_mode="$(file_mode "$symlink_target")"
ln -s "$symlink_target" "$symlink_config"

if TERRAPOD_CHEZMOI_CONFIG="$symlink_config" HOME="$symlink_home" XDG_CONFIG_HOME="$symlink_xdg" sh "$terrapod" configure development >"$tmp_dir/symlink.out" 2>"$tmp_dir/symlink.err" </dev/null; then
  fail "config path that is a symlink to a regular file fails explicitly"
fi
pass "config path that is a symlink to a regular file fails explicitly"

assert_contains "$tmp_dir/symlink.err" "not a regular file" "symlink config path explains it is not usable"
assert_not_contains "$tmp_dir/symlink.err" "Update Terrapod-managed data keys" "symlink config path does not prompt before failing"
assert_not_contains "$tmp_dir/symlink.err" "config update cancelled" "symlink config path does not report a declined update"

if [ ! -L "$symlink_config" ]; then
  fail "symlink config path remains a symlink after failed update"
fi
pass "symlink config path remains a symlink after failed update"

if ! cmp -s "$symlink_target" "$tmp_dir/symlink-target-before.toml"; then
  fail "symlink config target remains unchanged after failed update"
fi
pass "symlink config target remains unchanged after failed update"

symlink_target_mode_after="$(file_mode "$symlink_target")"
if [ "$symlink_target_mode_after" != "$symlink_target_mode" ]; then
  fail "symlink config target mode remains unchanged; expected $symlink_target_mode, got $symlink_target_mode_after"
fi
pass "symlink config target mode remains unchanged"

assert_no_terrapod_artifacts_near_path "$symlink_config" "symlink config path leaves no Terrapod artifacts"
assert_no_terrapod_artifacts_near_path "$symlink_target" "symlink config target leaves no Terrapod artifacts"

dangling_home="$tmp_dir/dangling-home"
dangling_xdg="$tmp_dir/dangling-xdg"
dangling_dir="$tmp_dir/dangling-config-dir"
dangling_config="$dangling_dir/dangling_config.toml"
dangling_target="$dangling_dir/missing-target.toml"
mkdir -p "$dangling_home" "$dangling_dir"
ln -s "$dangling_target" "$dangling_config"

if TERRAPOD_CHEZMOI_CONFIG="$dangling_config" HOME="$dangling_home" XDG_CONFIG_HOME="$dangling_xdg" sh "$terrapod" configure minimal >"$tmp_dir/dangling.out" 2>"$tmp_dir/dangling.err" </dev/null; then
  fail "config path that is a dangling symlink fails explicitly"
fi
pass "config path that is a dangling symlink fails explicitly"

assert_contains "$tmp_dir/dangling.err" "not a regular file" "dangling symlink config path explains it is not usable"

if [ ! -L "$dangling_config" ]; then
  fail "dangling symlink config path remains a symlink after failed update"
fi
pass "dangling symlink config path remains a symlink after failed update"

if [ -e "$dangling_target" ]; then
  fail "dangling symlink target remains missing after failed update"
fi
pass "dangling symlink target remains missing after failed update"

assert_no_terrapod_artifacts_near_path "$dangling_config" "dangling symlink config path leaves no Terrapod artifacts"

invalid_home="$tmp_dir/invalid-home"
invalid_xdg="$tmp_dir/invalid-xdg"
invalid_config="$invalid_xdg/chezmoi/chezmoi.toml"
mkdir -p "$invalid_home" "$(dirname "$invalid_config")"

cat >"$invalid_config" <<'TOML'
[data]
enableEditorStack = false
keepMe = "yes"
TOML
cp "$invalid_config" "$tmp_dir/invalid-before.toml"

if HOME="$invalid_home" XDG_CONFIG_HOME="$invalid_xdg" sh "$terrapod" configure typo >"$tmp_dir/invalid.out" 2>"$tmp_dir/invalid.err" </dev/null; then
  fail "invalid Preset fails before prompting"
else
  invalid_status="$?"
fi

if [ "$invalid_status" -ne 64 ]; then
  fail "invalid Preset exits with usage status 64; got $invalid_status"
fi
pass "invalid Preset exits with usage status 64"

assert_contains "$tmp_dir/invalid.err" "unknown Preset: typo" "invalid Preset names rejected Preset"
assert_not_contains "$tmp_dir/invalid.err" "Update Terrapod-managed data keys" "invalid Preset does not prompt before failing"

if ! cmp -s "$invalid_config" "$tmp_dir/invalid-before.toml"; then
  fail "invalid Preset leaves existing config unchanged"
fi
pass "invalid Preset leaves existing config unchanged"

assert_backup_count "$invalid_config" 0 "invalid Preset does not create a backup"
assert_no_terrapod_temp_files "$invalid_config" "invalid Preset leaves no Terrapod temp files"
