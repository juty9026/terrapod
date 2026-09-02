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

managed_setup_keys() {
  printf '%s\n' \
    profile \
    enableEditorStack \
    enableAiCliTools \
    enableDevelopmentWorkspace \
    enableMacosAppGroupTerminalApps \
    enableMacosAppGroupAutomation \
    enableMacosAppGroupLauncher \
    enableMacosAppGroupMonitoring \
    enableMacosAppGroupDevelopmentApps \
    enableMacosAppGroupMobileDev
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
