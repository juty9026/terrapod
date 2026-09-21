#!/bin/sh

# Shared reader for the Terrapod-managed chezmoi TOML config. Both the
# first-run installer and the tpod command surface parse the same file with
# the same rules, so the parser lives here instead of being copied into each.
# Callers must define fatal() before any function here can report a problem.

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
# One row per Managed Setting, 'key|kind|name|profile|presets|summary':
#   kind     optional-stack or macos-app-group
#   name     the display name Setup, status, and doctor show
#   profile  the one machine profile the setting applies to, or "all". A
#            setting that does not apply is not offered by Setup and is
#            written false everywhere, but its key is still written
#   presets  comma-separated Presets that enable it; listed explicitly, since
#            Presets are neither ordered nor nested and minimal is in no row
#   summary  the line Setup opens its explanation with. A macOS App Group
#            summary is its app list: Setup says "Installs <summary>." and
#            status reports the group by the same text. An optional stack
#            summary is the sentence Setup opens with; its status and doctor
#            details are hand-written, and worded differently
#
# Row order is the write order of the TOML and the managed_setup_keys order.
#
# The rows are printed by builtins alone: status runs under a PATH that may hold
# no external commands, and an external command here would silently blank it.
managed_setting_schema() {
  while IFS= read -r schema_row; do
    printf '%s\n' "$schema_row"
  done <<'ROWS'
enableEditorStack|optional-stack|Optional Editor Stack|all|development,workstation|Rich Neovim configuration
enableAiCliTools|optional-stack|Optional AI Tool Stack|macos-terminal|development,workstation|Antigravity CLI, Claude Code, and Codex
enableDevelopmentWorkspace|optional-stack|Optional Development Workspace|all|development,workstation|Dev Zellij layouts
enableMacosAppGroupTerminalApps|macos-app-group|terminal-apps|macos-terminal|workstation|Ghostty, D2Coding, Hack Nerd Font, JetBrains Mono Nerd Font, and Noto Sans CJK KR
enableMacosAppGroupAutomation|macos-app-group|automation|macos-terminal|workstation|Hammerspoon, Karabiner-Elements, and Scroll Reverser
enableMacosAppGroupLauncher|macos-app-group|launcher|macos-terminal|workstation|Raycast and 1Password CLI
enableMacosAppGroupMonitoring|macos-app-group|monitoring|macos-terminal|workstation|iStat Menus
enableMacosAppGroupDevelopmentApps|macos-app-group|development-apps|macos-terminal|workstation|Zed, Orca ADE, and OrbStack
enableMacosAppGroupMobileDev|macos-app-group|mobile-dev|macos-terminal|workstation|Android Studio and Maestro
ROWS
}

# Keys that earlier releases wrote and the writer must still strip. They are
# data so the writer's managed-name check is a superset of the current keys by
# construction.
retired_managed_setting_keys() {
  printf '%s\n' \
    enableMacosAppGroupAiApps \
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

# managed_setting_field <key> kind|name|profile|presets|summary. Prints nothing
# for an unknown key. It reads every row rather than stopping at the match: a
# reader that closes the pipe early makes the schema's printf fail with EPIPE,
# and dash reports that as an I/O error when SIGPIPE is ignored.
managed_setting_field() {
  wanted_key="$1"
  wanted_field="$2"

  managed_setting_schema | while IFS='|' read -r schema_key schema_kind schema_name schema_profile schema_presets schema_summary; do
    if [ "$schema_key" = "$wanted_key" ]; then
      case "$wanted_field" in
        kind) printf '%s\n' "$schema_kind" ;;
        name) printf '%s\n' "$schema_name" ;;
        profile) printf '%s\n' "$schema_profile" ;;
        presets) printf '%s\n' "$schema_presets" ;;
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

config_data_value() {
  config_file="$1"
  key="$2"

  if [ ! -f "$config_file" ]; then
    return 1
  fi

  awk -v wanted_key="$key" '
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

    BEGIN {
      in_root = 1
      found = 0
    }

    {
      if (is_comment($0)) {
        next
      }

      if (in_root && is_root_dotted_data_key($0)) {
        if (dotted_data_key_name($0) == wanted_key) {
          result = assignment_value($0)
          found = 1
        }
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

      if (in_data && is_key_assignment($0) && assignment_key_name($0) == wanted_key) {
        result = assignment_value($0)
        found = 1
      }
    }

    END {
      if (!found) {
        exit 1
      }

      print result
    }
  ' "$config_file"
}

config_data_key_present() {
  config_data_value "$1" "$2" >/dev/null 2>&1
}

config_has_unsupported_inline_data_table() {
  config_file="$1"

  if [ ! -f "$config_file" ]; then
    return 1
  fi

  awk '
    function is_section(line) {
      return line ~ /^[[:space:]]*(\[[^]]+\]|\[\[[^]]+\]\])[[:space:]]*($|#)/
    }

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

  awk '
    BEGIN {
      multiline_literal = sprintf("%c%c%c", 39, 39, 39)
      multiline_basic = "\"\"\""
    }

    function is_comment(line) {
      return line ~ "^[[:space:]]*#"
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

  awk '
    function is_comment(line) {
      return line ~ "^[[:space:]]*#"
    }

    function is_section(line) {
      return line ~ /^[[:space:]]*(\[[^]]+\]|\[\[[^]]+\]\])[[:space:]]*($|#)/
    }

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

reject_unsupported_managed_config_syntax() {
  config_file="$1"

  if problem_message="$(unsupported_managed_config_problem_message "$config_file")"; then
    fatal "$problem_message"
  fi
}
