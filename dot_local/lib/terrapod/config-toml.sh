#!/bin/sh

# Shared reader and writer for the Terrapod-managed chezmoi TOML config. Both the
# first-run installer and the tpod command surface parse the same file with
# the same rules, so the parser lives here instead of being copied into each.
# Sourcing performs no writes and installs no traps. Read problems are verdicts;
# write operations return failure with a diagnostic instead of calling fatal().

TERRAPOD_CONFIG_TOML_LOADED=1

# The path chezmoi itself would use. Callers that hand chezmoi an explicit
# --config are the ones that may honour an override; see
# terrapod_chezmoi_config_file.
terrapod_default_chezmoi_config_file() {
  if [ -n "${XDG_CONFIG_HOME:-}" ]; then
    printf '%s\n' "$XDG_CONFIG_HOME/chezmoi/chezmoi.toml"
    return
  fi

  printf '%s\n' "$HOME/.config/chezmoi/chezmoi.toml"
}

terrapod_chezmoi_config_file() {
  if [ -n "${TERRAPOD_CHEZMOI_CONFIG:-}" ]; then
    printf '%s\n' "$TERRAPOD_CHEZMOI_CONFIG"
    return
  fi

  terrapod_default_chezmoi_config_file
}

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

# The Managed Setting schema: the one place a setting is declared. Terrapod
# Setup, Preset expansion, the config writer, status, and the installer's
# completeness check all derive from these rows, so adding a macOS App Group
# starts and mostly ends here. The reader owns it because Setup and the
# installer run before the full apply (ADR 0019).
#
# One row per Managed Setting, 'key|kind|name|profile|presets|summary|readiness-detail':
#   kind     optional-stack, macos-app-group, or shell-environment
#   name     the display name Setup, status, and doctor show
#   profile  the one machine profile the setting applies to, or "all". A
#            setting that does not apply is not offered by Setup and is
#            written false everywhere, but its key is still written
#   presets  comma-separated Presets that enable it; listed explicitly, since
#            Presets are neither ordered nor nested and minimal is in no row
#   summary  the line Setup opens its explanation with. A macOS App Group
#            summary is its app list: Setup says "Installs <summary>." and
#            status reports the group by the same text. An optional stack
#            summary is the sentence Setup opens with. A shell environment
#            preference uses its summary in all three places
#   readiness-detail  optional explicit wording for status and doctor, falling
#            back to summary when omitted; independent of Setup capitalization
#
# Row order is the write order of the TOML and the managed_setup_keys order.
#
# The rows are printed by builtins alone: status runs under a PATH that may hold
# no external commands, and an external command here would silently blank it.
managed_setting_schema() {
  while IFS= read -r schema_row; do
    printf '%s\n' "$schema_row"
  done <<'ROWS'
enableEditorStack|optional-stack|Optional Editor Stack|all|development,workstation|Rich Neovim configuration|rich Neovim configuration
enableAiCliTools|optional-stack|Optional AI Tool Stack|macos-terminal|development,workstation|Antigravity CLI, Claude Code, and Codex
enableDevelopmentWorkspace|optional-stack|Optional Development Workspace|all|development,workstation|Dev Zellij layouts|development Zellij layouts
enableMacosAppGroupTerminalApps|macos-app-group|terminal-apps|macos-terminal|workstation|Ghostty, D2Coding, Hack Nerd Font, JetBrains Mono Nerd Font, and Noto Sans CJK KR
enableMacosAppGroupAutomation|macos-app-group|automation|macos-terminal|workstation|Hammerspoon, Karabiner-Elements, and Scroll Reverser
enableMacosAppGroupLauncher|macos-app-group|launcher|macos-terminal|workstation|Raycast and 1Password CLI
enableMacosAppGroupMonitoring|macos-app-group|monitoring|macos-terminal|workstation|iStat Menus
enableMacosAppGroupDevelopmentApps|macos-app-group|development-apps|macos-terminal|workstation|Zed, Orca ADE, and OrbStack
enableMacosAppGroupMobileDev|macos-app-group|mobile-dev|macos-terminal|workstation|Android Studio and Maestro
enableMacosAppGroupBrowsers|macos-app-group|browsers|macos-terminal|workstation|Aside and Google Chrome
enableMacosAppGroupAiApps|macos-app-group|ai-apps|macos-terminal|workstation|Claude, ChatGPT (including Codex), and Gemini
enableColimaDockerHost|shell-environment|Colima Docker Host Selection|macos-terminal||DOCKER_HOST for the default Colima profile; no runtime installation or startup
ROWS
}

# Keys that earlier releases wrote and the writer must still strip. They are
# data so the writer's managed-name check is a superset of the current keys by
# construction.
retired_managed_setting_keys() {
  printf '%s\n' \
    enableMacosDesktopApps \
    terrapodPreset
}

# Schema keys in row order, without the machine profile.
managed_setting_keys() {
  managed_setting_schema | while IFS='|' read -r schema_key schema_rest; do
    printf '%s\n' "$schema_key"
  done
}

managed_setup_keys() {
  printf '%s\n' profile
  managed_setting_keys
}

managed_setting_keys_of_kind() {
  managed_setting_schema | while IFS='|' read -r schema_key schema_kind schema_rest; do
    if [ "$schema_kind" = "$1" ]; then
      printf '%s\n' "$schema_key"
    fi
  done
}

# managed_setting_field <key> kind|name|profile|presets|summary|readiness-detail. Prints nothing
# for an unknown key. It reads every row rather than stopping at the match: a
# reader that closes the pipe early makes the schema's printf fail with EPIPE,
# and dash reports that as an I/O error when SIGPIPE is ignored.
managed_setting_field() {
  wanted_key="$1"
  wanted_field="$2"

  managed_setting_schema | while IFS='|' read -r schema_key schema_kind schema_name schema_profile schema_presets schema_summary schema_detail; do
    if [ "$schema_key" = "$wanted_key" ]; then
      case "$wanted_field" in
        kind) printf '%s\n' "$schema_kind" ;;
        name) printf '%s\n' "$schema_name" ;;
        profile) printf '%s\n' "$schema_profile" ;;
        presets) printf '%s\n' "$schema_presets" ;;
        readiness-detail) printf '%s\n' "${schema_detail:-$schema_summary}" ;;
        summary) printf '%s\n' "$schema_summary" ;;
      esac
    fi
  done
}

managed_setting_applies_to_profile() {
  applicable_profile="$(managed_setting_field "$1" profile)"

  [ -n "$applicable_profile" ] || return 1
  [ "$applicable_profile" = "all" ] || [ "$applicable_profile" = "$2" ]
}

# Whether a Preset lists the setting. Independent of the machine profile; a
# caller that expands a Preset also asks managed_setting_applies_to_profile.
managed_setting_enabled_by_preset() {
  [ -n "$2" ] || return 1

  case ",$(managed_setting_field "$1" presets)," in
    *",$2,"*) return 0 ;;
    *) return 1 ;;
  esac
}

# Shared awk grammar for the reader, syntax guards, and conservative upsert.
managed_config_awk_grammar() {
  printf '%s\n' '
    function strip_space(value) {
      sub(/^[[:space:]]*/, "", value)
      sub(/[[:space:]]*$/, "", value)
      return value
    }

    function strip_comment(value) {
      sub(/[[:space:]]*#.*/, "", value)
      return value
    }

    function unquote_key(value, quote) {
      value = strip_space(value)
      quote = substr(value, 1, 1)

      if ((quote == "\"" || quote == "\047") && substr(value, length(value), 1) == quote) {
        return substr(value, 2, length(value) - 2)
      }

      return value
    }

    function is_comment(line) {
      return line ~ "^[[:space:]]*#"
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

    function is_root_dotted_data_key(line) {
      return line ~ "^[[:space:]]*(data|\"data\"|\047data\047)[[:space:]]*\\."
    }

    function assignment_key_name(line, key) {
      key = line
      sub(/^[[:space:]]*/, "", key)
      sub(/[[:space:]]*=.*/, "", key)
      return unquote_key(key)
    }

    function dotted_data_key_name(line, key) {
      key = line
      sub(/^[[:space:]]*/, "", key)
      sub(/[[:space:]]*=.*/, "", key)
      sub("^[[:space:]]*(data|\"data\"|\047data\047)[[:space:]]*\\.[[:space:]]*", "", key)
      return unquote_key(key)
    }

    function assignment_value(line, value) {
      value = line
      sub(/^[^=]*=/, "", value)
      return strip_space(strip_comment(value))
    }

    # A TOML string token, "..." or literal, as its plain text; else "".
    function string_value(value, quote) {
      quote = substr(value, 1, 1)

      if (length(value) >= 2 && (quote == "\"" || quote == "\047") && substr(value, length(value), 1) == quote) {
        return substr(value, 2, length(value) - 2)
      }

      return ""
    }

  '
}

# The one pass over the managed [data] table. It prints, for each managed setup
# key in the order handed to it, one line:
#   missing <key>          the key is absent
#   invalid <key>          a Managed Setting whose value is not a bare boolean
#   value <key>=<value>    the interpreted value: true or false for a Managed
#                          Setting, the plain string for a quoted profile
# A profile that is not a quoted string prints no value line, so it never
# matches a detected profile. The profile is not otherwise judged.
managed_setup_config_scan() {
  awk -v setting_keys="$2" "$(managed_config_awk_grammar)"'
    function remember(key, value) {
      if (key in wanted) {
        raw[key] = value
        found[key] = 1
      }
    }

    BEGIN {
      in_root = 1
      key_count = split("profile " setting_keys, ordered_keys, " ")
      for (i = 1; i <= key_count; i++) {
        wanted[ordered_keys[i]] = 1
      }
    }

    {
      if (is_comment($0)) {
        next
      }

      if (in_root && is_root_dotted_data_key($0)) {
        remember(dotted_data_key_name($0), assignment_value($0))
        next
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

      if (in_data && is_key_assignment($0)) {
        remember(assignment_key_name($0), assignment_value($0))
      }
    }

    END {
      for (i = 1; i <= key_count; i++) {
        key = ordered_keys[i]

        if (!(key in found)) {
          print "missing " key
        } else if (key == "profile") {
          profile = string_value(raw[key])
          if (profile != "") {
            print "value profile=" profile
          }
        } else if (raw[key] == "true" || raw[key] == "false") {
          print "value " key "=" raw[key]
        } else {
          print "invalid " key
        }
      }
    }
  ' "$1"
}

unsupported_managed_setting_value_message() {
  case "$1" in
    *", "*) printf '%s\n' "unsupported Managed Setting values for $1 in config; rewrite them as bare true or false before running Terrapod commands: $2" ;;
    *) printf '%s\n' "unsupported Managed Setting value for $1 in config; rewrite it as bare true or false before running Terrapod commands: $2" ;;
  esac
}

config_has_unsupported_inline_data_table() {
  config_file="$1"

  if [ ! -f "$config_file" ]; then
    return 1
  fi

  awk "$(managed_config_awk_grammar)"'
    function is_inline_data_table(line) {
      return line ~ "^[[:space:]]*(data|\"data\"|\047data\047)[[:space:]]*=[[:space:]]*\\{"
    }

    {
      if (is_section($0)) {
        exit
      }

      if (is_inline_data_table($0)) {
        found = 1
      }
    }

    END {
      exit found ? 0 : 1
    }
  ' "$config_file"
}

config_has_unsupported_multiline_strings() {
  config_file="$1"

  if [ ! -f "$config_file" ]; then
    return 1
  fi

  awk "$(managed_config_awk_grammar)"'
    BEGIN {
      multiline_literal = sprintf("%c%c%c", 39, 39, 39)
      multiline_basic = "\"\"\""
    }

    function has_multiline_string_marker(line) {
      return !is_comment(line) && (index(line, multiline_basic) > 0 || index(line, multiline_literal) > 0)
    }

    {
      if (has_multiline_string_marker($0)) {
        found = 1
      }
    }

    END {
      exit found ? 0 : 1
    }
  ' "$config_file"
}

config_has_section_like_multiline_arrays() {
  config_file="$1"

  if [ ! -f "$config_file" ]; then
    return 1
  fi

  awk "$(managed_config_awk_grammar)"'
    function array_balance_delta(line, start, i, ch, in_basic_string, in_literal_string, escaped, balance) {
      for (i = start; i <= length(line); i++) {
        ch = substr(line, i, 1)

        if (in_basic_string) {
          if (escaped) {
            escaped = 0
          } else if (ch == "\\") {
            escaped = 1
          } else if (ch == "\"") {
            in_basic_string = 0
          }
          continue
        }

        if (in_literal_string) {
          if (ch == "\047") {
            in_literal_string = 0
          }
          continue
        }

        if (ch == "#") {
          break
        }

        if (ch == "\"") {
          in_basic_string = 1
          continue
        }

        if (ch == "\047") {
          in_literal_string = 1
          continue
        }

        if (ch == "[") {
          balance++
        } else if (ch == "]") {
          balance--
        }
      }

      return balance
    }

    function multiline_array_balance(line, i, ch, after_equals, saw_value) {
      if (is_comment(line)) {
        return 0
      }

      for (i = 1; i <= length(line); i++) {
        ch = substr(line, i, 1)

        if (!after_equals) {
          if (ch == "=") {
            after_equals = 1
          }
          continue
        }

        if (ch == "#") {
          break
        }

        if (!saw_value) {
          if (ch ~ /[[:space:]]/) {
            continue
          }

          if (ch != "[") {
            return 0
          }

          saw_value = 1
          return array_balance_delta(line, i)
        }
      }

      return 0
    }

    {
      if (in_multiline_array) {
        if (is_section($0)) {
          found = 1
        }

        array_balance += array_balance_delta($0, 1)
        if (array_balance <= 0) {
          in_multiline_array = 0
          array_balance = 0
        }
        next
      }

      array_balance = multiline_array_balance($0)
      if (array_balance > 0) {
        in_multiline_array = 1
      }
    }

    END {
      exit found ? 0 : 1
    }
  ' "$config_file"
}

unsupported_managed_config_problem_message() {
  config_file="$1"

  if config_has_unsupported_multiline_strings "$config_file"; then
    printf '%s\n' "unsupported multiline string in config; rewrite multiline values before running Terrapod commands: $config_file"
    return 0
  fi

  if config_has_section_like_multiline_arrays "$config_file"; then
    printf '%s\n' "unsupported multiline array with section-like entries in config; rewrite that array before running Terrapod commands: $config_file"
    return 0
  fi

  if config_has_unsupported_inline_data_table "$config_file"; then
    printf '%s\n' "unsupported inline data table in config; rewrite data = {...} as a [data] table before running Terrapod commands: $config_file"
    return 0
  fi

  return 1
}

# Reads the managed Terrapod Setup config at the path it is handed, once, and
# leaves the answer in globals: the one place the file state, then unsupported
# syntax or values, then missing keys ladder is climbed, and the one place a
# Managed Setting value is interpreted. Call it directly, not in $(...), when
# the values are needed too: a subshell drops the globals. It never calls
# fatal(). The file is read a fixed number of times, whatever the schema holds.
#
# managed_setup_config_state is one of:
#   missing      no file; every managed setup key is missing
#   non-regular  not a regular file (a directory, or a dangling symlink)
#   unreadable   a regular file the caller cannot read
#   unsupported  syntax the reader cannot parse safely, or a Managed Setting
#                value that is not a bare true or false
#   incomplete   parseable, but a managed setup key is absent
#   complete     `profile` and every current Managed Setting key are present,
#                and every Managed Setting value is a bare boolean
# managed_setup_config_detail is the problem message for unsupported, the
# missing keys (", "-joined, in schema order) for missing and incomplete, else
# empty. Read the values with managed_setting_enabled and
# managed_setup_stored_profile.
#
# The value rule is schema-based, like completeness: a key that does not apply
# to the stored profile is judged the same way. chezmoi's templates read the
# same keys with Go truthiness, where the string "false" is on; tpod apply stops
# on unsupported before it delegates, so they never see a rejected value.
#
# complete does not compare the stored profile. The first-run installer
# additionally reruns Terrapod Setup when it differs from the detected one; that
# is its rule, so the reader does not take a profile.
managed_setup_config_load() {
  load_config_file="$1"
  managed_setup_config_state=
  managed_setup_config_detail=
  managed_setup_config_values="
"
  load_setting_keys=
  for load_key in $(managed_setting_keys); do
    load_setting_keys="${load_setting_keys:+$load_setting_keys }$load_key"
  done

  managed_setup_config_state="$(config_file_state "$load_config_file")"
  case "$managed_setup_config_state" in
    missing)
      for load_key in $(managed_setup_keys); do
        managed_setup_config_detail="${managed_setup_config_detail:+$managed_setup_config_detail, }$load_key"
      done
      return 0
      ;;
    non-regular|unreadable)
      return 0
      ;;
  esac

  load_missing_keys=
  load_invalid_keys=
  while IFS= read -r load_line; do
    case "$load_line" in
      "missing "*)
        load_missing_keys="${load_missing_keys:+$load_missing_keys, }${load_line#missing }"
        ;;
      "invalid "*)
        load_invalid_keys="${load_invalid_keys:+$load_invalid_keys, }${load_line#invalid }"
        ;;
      "value "*)
        managed_setup_config_values="$managed_setup_config_values${load_line#value }
"
        ;;
    esac
  done <<EOF
$(managed_setup_config_scan "$load_config_file" "$load_setting_keys")
EOF

  if managed_setup_config_detail="$(unsupported_managed_config_problem_message "$load_config_file")"; then
    managed_setup_config_state=unsupported
  elif [ -n "$load_invalid_keys" ]; then
    managed_setup_config_state=unsupported
    managed_setup_config_detail="$(unsupported_managed_setting_value_message "$load_invalid_keys" "$load_config_file")"
  elif [ -n "$load_missing_keys" ]; then
    managed_setup_config_state=incomplete
    managed_setup_config_detail="$load_missing_keys"
  else
    managed_setup_config_state=complete
    managed_setup_config_detail=
  fi
}

# Whether the loaded config turns the Managed Setting on. Absent and rejected
# values, and every value of a file that was not read, are off. A file with
# unsupported syntax still answers with what the scan read, as status always
# has. Parameter expansion only, like the verdict accessors.
managed_setting_enabled() {
  case "$managed_setup_config_values" in
    *"
$1=true
"*) return 0 ;;
    *) return 1 ;;
  esac
}

# The loaded config's stored profile as a plain string, quotes removed. Fails
# when the profile is absent or not a quoted string.
managed_setup_stored_profile() {
  case "$managed_setup_config_values" in
    *"
profile="*)
      stored_profile="${managed_setup_config_values#*"
profile="}"
      printf '%s\n' "${stored_profile%%"
"*}"
      ;;
    *) return 1 ;;
  esac
}

# The verdict on the managed config at the path it is handed, as two lines: the
# state, then the detail (see managed_setup_config_load). Callers that need no
# values capture it once with $(...) and decide how to print it and what exit
# status it implies.
managed_setup_config_verdict() {
  managed_setup_config_load "$1"
  managed_setup_loaded_verdict
}

managed_setup_loaded_verdict() {
  printf '%s\n%s\n' "$managed_setup_config_state" "$managed_setup_config_detail"
}

# Accessors for a captured verdict. $(...) drops the trailing newlines, so an
# empty detail leaves a verdict with no second line at all. Parameter expansion
# only: status runs under a PATH that may hold no external commands.
managed_setup_verdict_state() {
  printf '%s\n' "${1%%
*}"
}

managed_setup_verdict_detail() {
  case "$1" in
    *"
"*) printf '%s\n' "${1#*
}" ;;
    *) printf '\n' ;;
  esac
}

# Parent lookup uses builtins so a missing-config preflight still works when
# PATH contains only a shell (for example, before reporting missing gum).
managed_config_parent_directory() (
  parent_path="$1"
  while [ "${parent_path%/}" != "$parent_path" ]; do
    parent_path="${parent_path%/}"
  done
  case "$parent_path" in
    */*)
      parent_path="${parent_path%/*}"
      while [ "${parent_path%/}" != "$parent_path" ]; do
        parent_path="${parent_path%/}"
      done
      printf '%s\n' "${parent_path:-/}"
      ;;
    *) printf '%s\n' . ;;
  esac
)

# BSD flags can prohibit replacement even when ordinary access checks pass.
# stat is already used to preserve config mode. GNU stat has no %Sf file-flag
# query, so an unsupported query is ignored; actual operations still must
# succeed. This never adds a flag/permission requirement to the read API.
managed_config_write_flags_preflight() (
  if write_flags="$(stat -f %Sf "$1" 2>/dev/null)"; then
    case ",$write_flags," in
      *,uchg,*|*,schg,*|*,uappnd,*|*,sappnd,*)
        printf 'terrapod: config destination is immutable or append-only (%s): %s\n' "$1" "$2" >&2
        return 1
        ;;
    esac
  fi
)

# A separate, read-only write capability check. A complete read verdict does
# not imply this succeeds: regular-file symlinks are readable, but never
# writable through this API. Invalid managed values are replaceable; only
# unsupported syntax blocks a rewrite. No fatal() contract is needed here.
# Returns zero when eligible, else one with the reason and path on stderr.
managed_config_write_path_preflight() (
  config_file="$1"

  if [ -L "$config_file" ] || { [ -e "$config_file" ] && [ ! -f "$config_file" ]; }; then
    printf 'terrapod: config path is not a regular file: %s\n' "$config_file" >&2
    return 1
  fi
  if [ -f "$config_file" ] && [ ! -r "$config_file" ]; then
    printf 'terrapod: config file is unreadable: %s\n' "$config_file" >&2
    return 1
  fi
  if [ -f "$config_file" ]; then
    managed_config_write_flags_preflight "$config_file" "$config_file" || return 1
  fi

  # Creating parents and staging/backing up/replacing all require write and
  # search permission on the parent, or its nearest existing ancestor.
  config_parent="$(managed_config_parent_directory "$config_file")" || return 1
  while [ ! -e "$config_parent" ] && [ ! -L "$config_parent" ]; do
    config_parent="$(managed_config_parent_directory "$config_parent")" || return 1
  done
  managed_config_write_flags_preflight "$config_parent" "$config_file" || return 1
  if [ ! -d "$config_parent" ] || [ ! -w "$config_parent" ] || [ ! -x "$config_parent" ]; then
    printf 'terrapod: config parent is not a writable, searchable directory (%s): %s\n' "$config_parent" "$config_file" >&2
    return 1
  fi
)

# The public preflight adds syntax eligibility to the filesystem checks.
managed_setup_config_write_preflight() (
  managed_config_write_path_preflight "$1" || return 1
  config_file="$1"

  if problem_message="$(unsupported_managed_config_problem_message "$config_file")"; then
    printf 'terrapod: %s\n' "$problem_message" >&2
    return 1
  fi
)

file_mode() {
  path="$1"

  if mode="$(stat -c %a "$path" 2>/dev/null)"; then
    printf '%s\n' "$mode"
    return 0
  fi

  stat -f %Lp "$path"
}

backup_existing_config() {
  config_file="$1"

  if [ ! -f "$config_file" ]; then
    return 0
  fi

  backup_stamp="$(date +%Y%m%d%H%M%S)" || return 1
  backup_file="$config_file.terrapod-backup-$backup_stamp-$$"
  cp "$config_file" "$backup_file"
}

cleanup_managed_config_temp() {
  rm -f "${tmp_file:-}" "${managed_data_file:-}"
}

write_managed_settings() (
  config_file="$1"
  settings_data="$2"
  config_dir="$(managed_config_parent_directory "$config_file")" || exit 1
  tmp_file=
  managed_data_file=

  managed_setup_config_write_preflight "$config_file" || exit 1

  mkdir -p "$config_dir" || exit 1
  trap cleanup_managed_config_temp EXIT
  trap 'cleanup_managed_config_temp; exit 1' HUP INT TERM

  tmp_file="$(mktemp "$config_dir/.terrapod-config.XXXXXX")" || exit 1
  managed_data_file="$(mktemp "$config_dir/.terrapod-data.XXXXXX")" || exit 1
  printf '%s\n' "$settings_data" >"$managed_data_file" || exit 1
  backup_existing_config "$config_file" || exit 1

  if [ -f "$config_file" ]; then
    config_mode="$(file_mode "$config_file")" || exit 1
    # Every key the writer owns: the current keys, which it replaces, and the
    # retired ones, which it only strips. Space-separated because the awk
    # shipped with macOS rejects a newline in a -v value.
    managed_names="$( { managed_setup_keys; retired_managed_setting_keys; } | tr '\n' ' ')" || exit 1

    awk -v managed_data_file="$managed_data_file" -v managed_names="$managed_names" "$(managed_config_awk_grammar)"'
      function is_managed_name(name) {
        return name in managed_name_set
      }

      function is_managed_key(line) {
        return is_key_assignment(line) && is_managed_name(assignment_key_name(line))
      }

      function is_root_dotted_managed_key(line) {
        return is_root_dotted_data_key(line) && is_managed_name(dotted_data_key_name(line))
      }

      function print_managed_data(prefix, line, result) {
        if (!printed_managed_data) {
          while ((result = (getline line < managed_data_file)) > 0) {
            print prefix line
          }
          if (result < 0) {
            exit 1
          }
          close(managed_data_file)
          printed_managed_data = 1
        }
      }

      BEGIN {
        in_root = 1

        managed_name_count = split(managed_names, managed_name_list, " ")
        for (i = 1; i <= managed_name_count; i++) {
          managed_name_set[managed_name_list[i]] = 1
        }
      }

      {
        if (in_root && is_root_dotted_managed_key($0)) {
          saw_root_data = 1
          next
        }

        if (in_root && is_root_dotted_data_key($0)) {
          saw_root_data = 1
          print
          next
        }

        if (is_data_section($0)) {
          in_root = 0
          in_data = 1
          saw_data = 1
          print
          next
        }

        if (in_root && is_section($0)) {
          if (saw_root_data) {
            print_managed_data("data.")
          }
          in_root = 0
          print
          next
        }

        if (in_data && is_section($0)) {
          print_managed_data()
          in_data = 0
          print
          next
        }

        if (in_data && is_managed_key($0)) {
          next
        }

        print
      }

      END {
        if (in_data) {
          print_managed_data()
        } else if (!saw_data) {
          if (saw_root_data) {
            print_managed_data("data.")
          } else {
            if (NR > 0) {
              print ""
            }
            print "[data]"
            print_managed_data()
          }
        }
      }
    ' "$config_file" >"$tmp_file" || exit 1

    chmod "$config_mode" "$tmp_file" || exit 1
  else
    {
      printf '%s\n' "[data]"
      cat "$managed_data_file"
    } >"$tmp_file" || exit 1
  fi

  # Permissions or the destination type can change while staging. In
  # particular, mv must never interpret a newly created directory as a target
  # directory and strand a staged config inside it.
  managed_config_write_path_preflight "$config_file" || exit 1
  # Consent belongs to setup/configure. Suppress mv's mode-based prompt for
  # a readable, read-only destination whose parent permits replacement.
  mv -f "$tmp_file" "$config_file" || exit 1
)
