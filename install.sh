#!/bin/sh
set -eu

DEFAULT_SOURCE_REPO="https://github.com/juty9026/terrapod.git"

fatal() {
  printf '%s\n' "terrapod installer: $*" >&2
  exit 1
}

user_local_bin_dir() {
  printf '%s\n' "$HOME/.local/bin"
}

default_source_dir() {
  if [ "${XDG_DATA_HOME:-}" ]; then
    printf '%s\n' "$XDG_DATA_HOME/chezmoi"
  else
    printf '%s\n' "$HOME/.local/share/chezmoi"
  fi
}

profile_label() {
  case "$1" in
    macos-terminal)
      printf '%s\n' "macOS Terminal Profile"
      ;;
    vps-shell)
      printf '%s\n' "VPS Shell Profile"
      ;;
    *)
      fatal "unknown profile: $1"
      ;;
  esac
}

read_os_release_value() {
  key="$1"
  os_release_file="${TERRAPOD_OS_RELEASE_FILE:-/etc/os-release}"

  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      "$key="*)
        value="${line#*=}"
        case "$value" in
          \"*\")
            value="${value#\"}"
            value="${value%\"}"
            ;;
        esac
        printf '%s\n' "$value"
        return 0
        ;;
    esac
  done <"$os_release_file"

  return 1
}

detect_profile() {
  kernel_name="$(uname -s)"

  case "$kernel_name" in
    Darwin)
      printf '%s\n' "macos-terminal"
      ;;
    Linux)
      if ! linux_id="$(read_os_release_value ID)"; then
        fatal "Unsupported Linux release. Supported Linux release: Ubuntu 24.04 LTS"
      fi
      if ! linux_version_id="$(read_os_release_value VERSION_ID)"; then
        fatal "Unsupported Linux release. Supported Linux release: Ubuntu 24.04 LTS"
      fi

      if [ "$linux_id" = "ubuntu" ] && [ "$linux_version_id" = "24.04" ]; then
        printf '%s\n' "vps-shell"
        return 0
      fi

      fatal "Unsupported Linux release: ID=$linux_id VERSION_ID=$linux_version_id. Supported Linux release: Ubuntu 24.04 LTS"
      ;;
    *)
      fatal "Unsupported platform: $kernel_name. Supported platforms: Darwin macOS Terminal Profile and Ubuntu 24.04 LTS VPS Shell Profile"
      ;;
  esac
}

machine_arch() {
  if [ -n "${TERRAPOD_MACHINE_ARCH:-}" ]; then
    printf '%s\n' "$TERRAPOD_MACHINE_ARCH"
  else
    uname -m
  fi
}

expected_homebrew_path() {
  profile="$1"
  arch="$2"

  if [ -n "${TERRAPOD_EXPECTED_HOMEBREW_PATH:-}" ]; then
    printf '%s\n' "$TERRAPOD_EXPECTED_HOMEBREW_PATH"
    return 0
  fi

  case "$profile:$arch" in
    vps-shell:x86_64|vps-shell:aarch64)
      printf '%s\n' /home/linuxbrew/.linuxbrew/bin/brew
      ;;
    macos-terminal:arm64|macos-terminal:aarch64)
      printf '%s\n' /opt/homebrew/bin/brew
      ;;
    macos-terminal:x86_64)
      printf '%s\n' /usr/local/bin/brew
      ;;
    vps-shell:*)
      fatal "Unsupported CPU architecture: $arch. Supported architectures: x86_64, aarch64."
      ;;
    *)
      fatal "Unsupported CPU architecture: $arch for profile $profile."
      ;;
  esac
}

reject_nonstandard_homebrew() {
  expected_brew="$1"
  discovered_brew=""

  if command -v brew >/dev/null 2>&1; then
    discovered_brew="$(command -v brew)"
  elif [ -n "${TERRAPOD_HOMEBREW_CANDIDATE_PATHS:-}" ]; then
    old_ifs="$IFS"
    IFS=:
    for candidate in $TERRAPOD_HOMEBREW_CANDIDATE_PATHS; do
      if [ -x "$candidate" ]; then
        discovered_brew="$candidate"
        break
      fi
    done
    IFS="$old_ifs"
  fi

  if [ -n "$discovered_brew" ] && [ "$discovered_brew" != "$expected_brew" ]; then
    fatal "Homebrew exists outside the supported prefix: $discovered_brew. Move or uninstall that Homebrew before installing the supported prefix at ${expected_brew%/bin/brew}."
  fi
}

require_non_root_linux_user() {
  if [ "$1" = "vps-shell" ] && [ "$(id -u)" -eq 0 ]; then
    fatal "Run the Terrapod installer as the non-root management user with sudo access; Homebrew does not support installation as root."
  fi
}

ensure_user_local_bin() {
  bin_dir="$1"

  mkdir -p "$bin_dir" || fatal "failed to create local bin directory: $bin_dir"
  case ":${PATH:-}:" in
    *":$bin_dir:"*)
      ;;
    *)
      PATH="$bin_dir${PATH:+:$PATH}"
      export PATH
      ;;
  esac
}

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

reject_unresumable_source_dir() {
  source_dir="$1"

  fatal "chezmoi source directory already exists but is not a resumable Terrapod Source Repository checkout: $source_dir. Move it aside before first-run install, or run Terrapod from a checked-out juty9026/terrapod source repository."
}

terrapod_help_output_is_valid() {
  help_output="$1"

  printf '%s\n' "$help_output" | grep -F "Terrapod - a small landing pod for your dotfiles" >/dev/null 2>&1 &&
    printf '%s\n' "$help_output" | grep -F "Usage:" >/dev/null 2>&1 &&
    printf '%s\n' "$help_output" | grep -F "tpod apply" >/dev/null 2>&1
}

command_help_is_terrapod() {
  command_path="$1"
  profile="$2"

  [ -x "$command_path" ] || return 1
  if ! help_output="$(TERRAPOD_PROFILE="$profile" "$command_path" help 2>/dev/null)"; then
    return 1
  fi

  terrapod_help_output_is_valid "$help_output"
}

installed_command_surface_is_valid() {
  local_bin_dir="$1"
  profile="$2"
  terrapod_bin="$local_bin_dir/terrapod"
  tpod_bin="$local_bin_dir/tpod"

  command_help_is_terrapod "$terrapod_bin" "$profile" &&
    command_help_is_terrapod "$tpod_bin" "$profile"
}

path_points_to_terrapod_source_command() {
  command_path="$1"
  source_dir="$2"
  expected_source="$source_dir/dot_local/bin/executable_terrapod"

  [ -L "$command_path" ] || return 1
  target="$(readlink "$command_path")" || return 1
  case "$target" in
    /*)
      target_path="$target"
      ;;
    *)
      target_path="${command_path%/*}/$target"
      ;;
  esac

  target_dir="${target_path%/*}"
  target_base="${target_path##*/}"
  if ! resolved_dir="$(CDPATH= cd -P -- "$target_dir" 2>/dev/null && pwd -P)"; then
    return 1
  fi
  resolved_target="$resolved_dir/$target_base"

  expected_dir="${expected_source%/*}"
  expected_base="${expected_source##*/}"
  if ! resolved_expected_dir="$(CDPATH= cd -P -- "$expected_dir" 2>/dev/null && pwd -P)"; then
    return 1
  fi
  resolved_expected="$resolved_expected_dir/$expected_base"

  [ "$resolved_target" = "$resolved_expected" ]
}

path_points_to_installed_tpod_alias() {
  command_path="$1"

  [ "${command_path##*/}" = "tpod" ] || return 1
  [ -L "$command_path" ] || return 1
  target="$(readlink "$command_path")" || return 1
  case "$target" in
    /*)
      target_path="$target"
      ;;
    *)
      target_path="${command_path%/*}/$target"
      ;;
  esac

  command_dir="${command_path%/*}"
  if ! resolved_command_dir="$(CDPATH= cd -P -- "$command_dir" 2>/dev/null && pwd -P)"; then
    return 1
  fi

  target_dir="${target_path%/*}"
  target_base="${target_path##*/}"
  if ! resolved_target_dir="$(CDPATH= cd -P -- "$target_dir" 2>/dev/null && pwd -P)"; then
    return 1
  fi
  resolved_target="$resolved_target_dir/$target_base"

  [ "$resolved_target" = "$resolved_command_dir/terrapod" ]
}

file_points_to_terrapod_source_command() {
  command_path="$1"
  source_dir="$2"
  expected_exec="exec \"$source_dir/dot_local/bin/executable_terrapod\" \"\$@\""

  [ -L "$command_path" ] && return 1
  [ -f "$command_path" ] || return 1
  awk -v expected_exec="$expected_exec" '
    NR == 1 {
      if ($0 != "#!/bin/sh") {
        exit 1
      }
      next
    }

    NR == 2 {
      if ($0 == expected_exec) {
        found = 1
        next
      }
      exit 1
    }

    $0 !~ /^[[:space:]]*$/ {
      found = 0
      exit 1
    }

    END { exit found ? 0 : 1 }
  ' "$command_path"
}

file_looks_like_terrapod_command() {
  command_path="$1"

  [ -L "$command_path" ] && return 1
  [ -f "$command_path" ] || return 1
  awk '
    NR == 1 {
      if ($0 != "#!/bin/sh") {
        exit 1
      }
      found_shebang = 1
    }

    index($0, "Terrapod - a small landing pod for your dotfiles") {
      found_title = 1
    }

    index($0, "Usage:") {
      found_usage = 1
    }

    index($0, "tpod apply") {
      found_apply = 1
    }

    index($0, "help|--help|-h") {
      found_help = 1
    }

    END {
      exit found_shebang && found_title && found_usage && found_apply && found_help ? 0 : 1
    }
  ' "$command_path"
}

command_surface_path_is_repairable() {
  command_path="$1"
  source_dir="$2"
  profile="$3"

  if [ -L "$command_path" ]; then
    path_points_to_terrapod_source_command "$command_path" "$source_dir" ||
      path_points_to_installed_tpod_alias "$command_path"
    return $?
  fi

  [ -e "$command_path" ] || return 0

  if file_points_to_terrapod_source_command "$command_path" "$source_dir"; then
    return 0
  fi

  if [ ! -x "$command_path" ] && file_looks_like_terrapod_command "$command_path"; then
    return 0
  fi

  command_help_is_terrapod "$command_path" "$profile"
}

reject_command_surface_conflict() {
  command_path="$1"

  fatal "non-Terrapod command already exists at $command_path. Move or remove it, then rerun the Terrapod installer."
}

ensure_command_surface_path_repairable() {
  command_path="$1"
  source_dir="$2"
  profile="$3"

  if ! command_surface_path_is_repairable "$command_path" "$source_dir" "$profile"; then
    reject_command_surface_conflict "$command_path"
  fi
}

print_already_installed_guidance() {
  local_bin_dir="$1"

  printf '%s\n' "Terrapod is already installed."
  printf '%s\n' "Routine commands:"
  printf '%s\n' "  $local_bin_dir/tpod status"
  printf '%s\n' "  $local_bin_dir/tpod apply"
  printf '%s\n' "  $local_bin_dir/tpod help"
}

vps_sudo_cmd() {
  if command -v sudo >/dev/null 2>&1; then
    printf '%s\n' "sudo"
  else
    fatal "Ubuntu Homebrew prerequisites are required before Terrapod Setup. Install sudo so Terrapod can prepare Homebrew with apt-get, then rerun the installer."
  fi
}

warn_low_linuxbrew_disk_space() {
  [ "$1" = "vps-shell" ] || return 0
  if [ -n "${TERRAPOD_AVAILABLE_KB:-}" ]; then
    available_kb="$TERRAPOD_AVAILABLE_KB"
  else
    available_kb="$(df -Pk /home | awk 'NR == 2 { print $4 }')"
  fi
  case "$available_kb" in *[!0-9]*|'') return 0 ;; esac
  if [ "$available_kb" -lt 3145728 ]; then
    printf '%s\n' "terrapod installer: warning: less than 3 GiB is available for Linuxbrew; installation will continue and may need additional free space." >&2
  fi
}

ensure_source_repo_prerequisites() {
  profile="$1"
  [ "$profile" = "vps-shell" ] || return 0
  sudo_cmd="$(vps_sudo_cmd)"
  $sudo_cmd apt-get update -y || fatal "failed to update APT metadata before Homebrew bootstrap"
  $sudo_cmd apt-get install -y build-essential ca-certificates curl file git procps ||
    fatal "failed to install Ubuntu Homebrew prerequisites: build-essential, ca-certificates, curl, file, git, procps"
}

ensure_homebrew() {
  profile="$1"
  expected_brew="$(expected_homebrew_path "$profile" "$(machine_arch)")"
  reject_nonstandard_homebrew "$expected_brew"
  if [ ! -x "$expected_brew" ]; then
    installer="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-install.XXXXXX")" || fatal "failed to create Homebrew installer temporary file"
    if ! curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh -o "$installer"; then
      rm -f "$installer"
      fatal "failed to download the official Homebrew installer"
    fi
    if ! NONINTERACTIVE=1 /bin/bash "$installer" >&2; then
      rm -f "$installer"
      fatal "official Homebrew installer failed before Terrapod Setup"
    fi
    rm -f "$installer"
  fi
  [ -x "$expected_brew" ] || fatal "Homebrew install finished, but brew was not found at $expected_brew"
  printf '%s\n' "$expected_brew"
}

prepare_brew_bootstrap_tools() {
  brew_bin="$1"
  HOMEBREW_NO_AUTO_UPDATE=1 "$brew_bin" install chezmoi gum >&2 ||
    fatal "failed to install chezmoi and gum with Homebrew before Terrapod Setup"
  chezmoi_bin="${brew_bin%/brew}/chezmoi"
  [ -x "$chezmoi_bin" ] || fatal "Homebrew did not install chezmoi at $chezmoi_bin"
  command -v gum >/dev/null 2>&1 || fatal "Homebrew did not make gum available before Terrapod Setup"
}

initialize_source_repository() {
  chezmoi_bin="$1"

  "$chezmoi_bin" init "$DEFAULT_SOURCE_REPO" || fatal "chezmoi init failed"
}

checked_out_terrapod() {
  source_dir="$1"

  terrapod_source="$source_dir/dot_local/bin/executable_terrapod"
  if [ ! -x "$terrapod_source" ]; then
    fatal "checked-out Terrapod executable is missing: $terrapod_source"
  fi

  printf '%s\n' "$terrapod_source"
}

chezmoi_config_file() {
  if [ "${XDG_CONFIG_HOME:-}" ]; then
    printf '%s\n' "$XDG_CONFIG_HOME/chezmoi/chezmoi.toml"
  else
    printf '%s\n' "$HOME/.config/chezmoi/chezmoi.toml"
  fi
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

reject_unsupported_managed_config_syntax() {
  config_file="$1"

  if problem_message="$(unsupported_managed_config_problem_message "$config_file")"; then
    fatal "$problem_message"
  fi
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
    enableMacosAppGroupDevelopmentApps
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

toml_string_value_matches() {
  value="$1"
  expected="$2"

  [ "$value" = "\"$expected\"" ] || [ "$value" = "'$expected'" ]
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

print_setup_recovery() {
  profile="$1"
  source_dir="$2"
  brew_bin=""

  if [ "$profile" = "macos-terminal" ]; then
    brew_bin="$(find_homebrew || true)"
  fi

  printf '%s\n' "terrapod installer: Terrapod Setup did not complete." >&2
  printf '%s\n' "terrapod installer: Resume Terrapod Setup from the checked-out source repository:" >&2
  if [ -n "$brew_bin" ]; then
    printf '%s\n' "terrapod installer:   cd \"$source_dir\" && eval \"\$(\"$brew_bin\" shellenv)\" && TERRAPOD_PROFILE=\"$profile\" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup" >&2
  else
    printf '%s\n' "terrapod installer:   cd \"$source_dir\" && TERRAPOD_PROFILE=\"$profile\" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup" >&2
  fi
}

find_homebrew() {
  if command -v brew >/dev/null 2>&1; then
    command -v brew
    return 0
  fi

  if [ "${TERRAPOD_HOMEBREW_CANDIDATE_PATHS+x}" ]; then
    homebrew_candidate_paths="$TERRAPOD_HOMEBREW_CANDIDATE_PATHS"
  else
    homebrew_candidate_paths="/opt/homebrew/bin/brew /usr/local/bin/brew"
  fi

  for brew_path in $homebrew_candidate_paths; do
    if [ -x "$brew_path" ]; then
      printf '%s\n' "$brew_path"
      return 0
    fi
  done

  return 1
}

mark_install_warning_from_source() {
  source_dir="$1"
  category="$2"
  summary="$3"
  guidance="$4"
  install_warnings_lib="$source_dir/dot_local/lib/terrapod/install-warnings.sh"
  TERRAPOD_INSTALL_WARNINGS_LOADED=

  if [ -f "$install_warnings_lib" ]; then
    . "$install_warnings_lib"
  fi

  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" = "1" ]; then
    terrapod_install_warning_write "$category" "$summary" "$guidance" || true
  fi
}

clear_install_warning_from_source() {
  source_dir="$1"
  category="$2"
  install_warnings_lib="$source_dir/dot_local/lib/terrapod/install-warnings.sh"
  TERRAPOD_INSTALL_WARNINGS_LOADED=

  if [ -f "$install_warnings_lib" ]; then
    . "$install_warnings_lib"
  fi

  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" = "1" ]; then
    terrapod_install_warning_clear "$category" || true
  fi
}

load_install_warnings_from_source() {
  source_dir="$1"
  install_warnings_lib="$source_dir/dot_local/lib/terrapod/install-warnings.sh"
  TERRAPOD_INSTALL_WARNINGS_LOADED=

  if [ -f "$install_warnings_lib" ]; then
    . "$install_warnings_lib"
  fi

  [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" = "1" ]
}

snapshot_install_warnings_from_source() {
  source_dir="$1"
  snapshot_dir="$2"

  mkdir -p "$snapshot_dir" || return 1
  load_install_warnings_from_source "$source_dir" || return 0

  for category in $(terrapod_install_warning_categories); do
    terrapod_install_warning_read "$category" >"$snapshot_dir/$category" 2>/dev/null || true
  done
}

install_warning_markers_changed_since_snapshot() {
  source_dir="$1"
  snapshot_dir="$2"
  changed=false

  load_install_warnings_from_source "$source_dir" || return 1

  for category in $(terrapod_install_warning_categories); do
    current_file="$snapshot_dir/current-$category"
    if terrapod_install_warning_read "$category" >"$current_file" 2>/dev/null; then
      if [ ! -f "$snapshot_dir/$category" ] || ! cmp -s "$snapshot_dir/$category" "$current_file"; then
        changed=true
      fi
    fi
    rm -f "$current_file"
  done

  [ "$changed" = "true" ]
}

run_terrapod_setup() {
  profile="$1"
  source_dir="$2"
  terrapod_source="$(checked_out_terrapod "$source_dir")"

  if TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= "$terrapod_source" setup; then
    return 0
  fi

  print_setup_recovery "$profile" "$source_dir"
  return 1
}

ensure_first_run_setup() {
  profile="$1"
  source_dir="$2"
  chezmoi_bin="$3"
  config_file="$(chezmoi_config_file)"

  managed_setup_config_path_is_usable_for_resume "$config_file"
  reject_unsupported_managed_config_syntax "$config_file"

  if managed_setup_config_complete "$config_file" "$profile"; then
    printf '%s\n' "terrapod installer: Reusing complete managed Terrapod Setup config: $config_file"
    return 0
  fi

  run_terrapod_setup "$profile" "$source_dir"
}

apply_recovery_core_command_surface() {
  profile="$1"
  source_dir="$2"
  local_bin_dir="$3"
  terrapod_source="$(checked_out_terrapod "$source_dir")"
  terrapod_target="$local_bin_dir/terrapod"
  tpod_target="$local_bin_dir/tpod"

  ensure_command_surface_path_repairable "$terrapod_target" "$source_dir" "$profile"
  ensure_command_surface_path_repairable "$tpod_target" "$source_dir" "$profile"

  rm -f "$terrapod_target" "$tpod_target" ||
    fatal "failed to repair Terrapod command surface under $local_bin_dir"
  cp "$terrapod_source" "$terrapod_target" ||
    fatal "failed to install Terrapod command at $terrapod_target"
  chmod +x "$terrapod_target" ||
    fatal "failed to make Terrapod command executable: $terrapod_target"
  ln -s terrapod "$tpod_target" ||
    fatal "failed to install tpod alias at $tpod_target"

  validate_recovery_core_command_surface "$profile" "$local_bin_dir"
}

validate_recovery_core_command_surface() {
  profile="$1"
  local_bin_dir="$2"
  terrapod_bin="$local_bin_dir/terrapod"
  tpod_bin="$local_bin_dir/tpod"

  [ -x "$terrapod_bin" ] ||
    fatal "terrapod was not installed at $terrapod_bin after recovery-core apply"
  [ -x "$tpod_bin" ] ||
    fatal "tpod was not installed at $tpod_bin after recovery-core apply"
  TERRAPOD_PROFILE="$profile" "$tpod_bin" help >/dev/null 2>&1 ||
    fatal "tpod help failed after recovery-core apply"
}

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

copy_shell_startup_backup() {
  target="$1"
  backup_file="$target.terrapod-backup-$(shell_startup_backup_timestamp)-$$"

  cp -P "$target" "$backup_file" ||
    fatal "failed to back up shell startup file before first-run overwrite: $target"
  printf '%s\n' "$backup_file"
}

backup_shell_startup_if_different() {
  chezmoi_bin="$1"
  target="$2"

  if [ -L "$target" ]; then
    copy_shell_startup_backup "$target"
    return 0
  fi

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
  else
    cmp_status="$?"
  fi
  rm -f "$rendered_file"
  if [ "$cmp_status" -ne 1 ]; then
    fatal "failed to compare shell startup file before backup: $target"
  fi

  copy_shell_startup_backup "$target"
}

backup_recovery_core_shell_startup_files() {
  chezmoi_bin="$1"
  profile="$2"
  backup_paths=""

  for target in "$HOME/.zshenv" "$HOME/.zprofile" "$HOME/.zshrc"; do
    if [ "$target" = "$HOME/.zprofile" ] && [ "$profile" != "macos-terminal" ]; then
      continue
    fi
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

apply_recovery_core_shell_startup_files() {
  profile="$1"
  chezmoi_bin="$2"

  if ! backup_paths="$(backup_recovery_core_shell_startup_files "$chezmoi_bin" "$profile")"; then
    fatal "failed to back up recovery-core shell startup files"
  fi
  report_shell_startup_backups "$backup_paths"

  if [ "$profile" = "macos-terminal" ]; then
    "$chezmoi_bin" apply --force "$HOME/.zshenv" "$HOME/.zprofile" "$HOME/.zshrc" ||
      fatal "failed to apply recovery-core shell startup files"
  else
    "$chezmoi_bin" apply --force "$HOME/.zshenv" "$HOME/.zshrc" ||
      fatal "failed to apply recovery-core shell startup files"
  fi
}

run_initial_apply() {
  chezmoi_bin="$1"
  source_dir="$2"
  marker_snapshot_dir="$(mktemp -d)" ||
    fatal "failed to create install-warning snapshot directory"

  snapshot_install_warnings_from_source "$source_dir" "$marker_snapshot_dir" ||
    fatal "failed to snapshot install warning markers"

  if ! TERRAPOD_FIRST_RUN_APPLY=1 "$chezmoi_bin" apply; then
    rm -rf "$marker_snapshot_dir"
    fatal "chezmoi apply failed"
  fi

  if install_warning_markers_changed_since_snapshot "$source_dir" "$marker_snapshot_dir"; then
    rm -rf "$marker_snapshot_dir"
    return 2
  fi

  rm -rf "$marker_snapshot_dir"
  return 0
}

show_first_run_help() {
  profile="$1"
  local_bin_dir="$2"
  tpod_bin="$local_bin_dir/tpod"

  if [ ! -x "$tpod_bin" ]; then
    fatal "tpod was not installed at $tpod_bin after initial apply"
  fi

  TERRAPOD_PROFILE="$profile" "$tpod_bin" help || fatal "tpod help failed after initial apply"
}

print_first_run_tpod_availability() {
  local_bin_dir="$1"

  printf '\n'
  printf '%s\n' "Terrapod command availability:"
  printf '%s\n' "  If this shell has not reloaded Terrapod's managed PATH yet, plain 'tpod' may not resolve."
  printf '%s\n' "  Use this absolute command now: $local_bin_dir/tpod"
  printf '%s\n' "  Open a new terminal or refresh your login shell before relying on plain 'tpod'."
}

print_first_run_clean_completion() {
  printf '\n'
  printf '%s\n' "Terrapod first-run apply complete."
}

print_first_run_warning_completion() {
  local_bin_dir="$1"

  printf '\n'
  printf '%s\n' "Terrapod first-run apply completed with warnings."
  printf '%s\n' "Warning:"
  printf '%s\n' "  Terrapod installed and the recovery core is valid, but machine profile readiness needs attention."
  printf '%s\n' "  Review the full apply output above, then run:"
  printf '%s\n' "  $local_bin_dir/tpod doctor"
}

main() {
  profile="$(detect_profile)"
  label="$(profile_label "$profile")"
  local_bin_dir="$(user_local_bin_dir)"
  source_dir="$(default_source_dir)"

  printf '%s\n' "Terrapod first-run installer"
  printf '%s\n' "Profile: $label"
  printf '%s\n' "Source repository: $DEFAULT_SOURCE_REPO"

  ensure_user_local_bin "$local_bin_dir"
  source_already_present=false
  if source_dir_exists "$source_dir"; then
    if ! source_is_resumable_terrapod_checkout "$source_dir"; then
      reject_unresumable_source_dir "$source_dir"
    fi
    source_already_present=true
  fi

  if [ "$source_already_present" = "true" ] && installed_command_surface_is_valid "$local_bin_dir" "$profile"; then
    print_already_installed_guidance "$local_bin_dir"
    return 0
  fi

  require_non_root_linux_user "$profile"
  ensure_source_repo_prerequisites "$profile"
  warn_low_linuxbrew_disk_space "$profile"
  brew_bin="$(ensure_homebrew "$profile")"
  if ! brew_shellenv="$("$brew_bin" shellenv)"; then
    fatal "failed to evaluate Homebrew shellenv"
  fi
  eval "$brew_shellenv" || fatal "failed to evaluate Homebrew shellenv"
  prepare_brew_bootstrap_tools "$brew_bin"
  chezmoi_bin="${brew_bin%/brew}/chezmoi"
  if [ "$source_already_present" = "false" ]; then
    initialize_source_repository "$chezmoi_bin"
  fi
  ensure_first_run_setup "$profile" "$source_dir" "$chezmoi_bin"
  apply_recovery_core_command_surface "$profile" "$source_dir" "$local_bin_dir"
  apply_recovery_core_shell_startup_files "$profile" "$chezmoi_bin"
  initial_apply_status=0
  run_initial_apply "$chezmoi_bin" "$source_dir" || initial_apply_status="$?"
  show_first_run_help "$profile" "$local_bin_dir"
  print_first_run_tpod_availability "$local_bin_dir"

  case "$initial_apply_status" in
    0)
      print_first_run_clean_completion
      ;;
    2)
      print_first_run_warning_completion "$local_bin_dir"
      ;;
    *)
      fatal "unexpected initial apply status: $initial_apply_status"
      ;;
  esac
}

main "$@"
