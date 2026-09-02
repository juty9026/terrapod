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

user_local_terrapod_lib_dir() {
  printf '%s\n' "$HOME/.local/lib/terrapod"
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

darwin_hardware_arch() {
  process_arch="$1"
  if [ "$process_arch" = x86_64 ] &&
    [ "$(sysctl -in sysctl.proc_translated 2>/dev/null || true)" = "1" ]; then
    printf '%s\n' arm64
  else
    printf '%s\n' "$process_arch"
  fi
}

expected_homebrew_path() {
  profile="$1"
  arch="$2"

  if [ -n "${TERRAPOD_EXPECTED_HOMEBREW_PATH:-}" ]; then
    printf '%s\n' "$TERRAPOD_EXPECTED_HOMEBREW_PATH"
    return 0
  fi

  if [ "$profile" = macos-terminal ]; then
    arch="$(darwin_hardware_arch "$arch")"
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

# TERRAPOD_HOMEBREW_CANDIDATE_PATHS overrides the Homebrew search list. Entries
# are colon-separated like PATH; an empty value means there are no candidates.
first_executable_homebrew_candidate() {
  candidate_paths="${TERRAPOD_HOMEBREW_CANDIDATE_PATHS-/opt/homebrew/bin/brew:/usr/local/bin/brew}"
  old_ifs="$IFS"
  IFS=:
  for candidate in $candidate_paths; do
    if [ -n "$candidate" ] && [ -x "$candidate" ]; then
      printf '%s\n' "$candidate"
      break
    fi
  done
  IFS="$old_ifs"
}

reject_nonstandard_homebrew() {
  expected_brew="$1"
  discovered_brew=""

  # The installer invokes the standard brew by absolute path. A legacy brew on
  # PATH must not make a valid standard-prefix installation look unsupported.
  if [ "${TERRAPOD_TEST_BREW_ABSENT:-0}" != 1 ] && [ -x "$expected_brew" ]; then
    return 0
  fi

  if command -v brew >/dev/null 2>&1; then
    discovered_brew="$(command -v brew)"
  elif [ -n "${TERRAPOD_HOMEBREW_CANDIDATE_PATHS:-}" ]; then
    discovered_brew="$(first_executable_homebrew_candidate)"
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

# Checkouts made before the dot_zprofile -> dot_zprofile.tmpl rename still carry
# the untemplated name, so accept either one when judging resumability.
source_has_zprofile_template() {
  source_dir="$1"

  [ -e "$source_dir/dot_zprofile.tmpl" ] || [ -e "$source_dir/dot_zprofile" ]
}

source_has_recovery_core_files() {
  source_dir="$1"

  [ -x "$source_dir/dot_local/bin/executable_terrapod" ] &&
    [ -e "$source_dir/dot_local/bin/symlink_tpod" ] &&
    [ -e "$source_dir/dot_local/lib/terrapod/config-toml.sh" ] &&
    [ -e "$source_dir/dot_zshenv.tmpl" ] &&
    source_has_zprofile_template "$source_dir" &&
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

  brew_path="$(first_executable_homebrew_candidate)"
  [ -n "$brew_path" ] || return 1
  printf '%s\n' "$brew_path"
}

load_config_toml_from_source() {
  source_dir="$1"
  config_toml_lib="$source_dir/dot_local/lib/terrapod/config-toml.sh"

  [ -f "$config_toml_lib" ] || return 1

  . "$config_toml_lib"
}

load_install_warnings_from_source() {
  source_dir="$1"
  install_warnings_lib="$source_dir/dot_local/lib/terrapod/install-warnings.sh"

  [ -f "$install_warnings_lib" ] || return 1

  . "$install_warnings_lib"
}

snapshot_install_warnings_from_source() {
  source_dir="$1"
  snapshot_dir="$2"

  mkdir -p "$snapshot_dir" || return 1

  for category in $(terrapod_install_warning_categories); do
    if terrapod_install_warning_read "$category" >"$snapshot_dir/$category" 2>/dev/null; then
      marker_path="$(terrapod_install_warning_existing_path "$category" 2>/dev/null || true)"
      if [ -n "$marker_path" ]; then
        ln "$marker_path" "$snapshot_dir/$category.identity" 2>/dev/null || true
      fi
    fi
  done
}

# Reports 0 when no marker changed, 1 when at least one did, and 2 when the
# answer could not be determined: the marker library is not loaded, or a
# marker exists but cannot be read. Reporting either of those as "nothing
# changed" would hide fresh warnings behind a clean completion message.
install_warning_marker_change_status() {
  source_dir="$1"
  snapshot_dir="$2"
  changed=false

  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" != "1" ]; then
    return 2
  fi

  for category in $(terrapod_install_warning_categories); do
    marker_path="$(terrapod_install_warning_existing_path "$category" 2>/dev/null || true)"
    if [ -z "$marker_path" ]; then
      continue
    fi

    current_file="$snapshot_dir/current-$category"
    if ! terrapod_install_warning_read "$category" >"$current_file" 2>/dev/null; then
      rm -f "$current_file"
      return 2
    fi

    if [ ! -f "$snapshot_dir/$category" ] ||
      ! cmp -s "$snapshot_dir/$category" "$current_file" ||
      {
        [ -f "$snapshot_dir/$category.identity" ] &&
          [ ! "$snapshot_dir/$category.identity" -ef "$marker_path" ]
      }; then
      changed=true
    fi
    rm -f "$current_file"
  done

  if [ "$changed" = "true" ]; then
    return 1
  fi

  return 0
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
  # Deliberately the default path, not TERRAPOD_CHEZMOI_CONFIG: the installer
  # runs chezmoi without --config and hands Terrapod Setup an empty override,
  # so honouring one here would read a config the installer never applies.
  config_file="$(terrapod_default_chezmoi_config_file)"

  managed_setup_config_path_is_usable_for_resume "$config_file"
  reject_unsupported_managed_config_syntax "$config_file"

  if managed_setup_config_complete "$config_file" "$profile"; then
    printf '%s\n' "terrapod installer: Reusing complete managed Terrapod Setup config: $config_file"
    return 0
  fi

  run_terrapod_setup "$profile" "$source_dir"
}

fail_command_surface_repair() {
  staged_path="$1"
  message="$2"

  rm -f "$staged_path"
  fatal "$message"
}

apply_recovery_core_command_surface() {
  profile="$1"
  source_dir="$2"
  local_bin_dir="$3"
  terrapod_source="$(checked_out_terrapod "$source_dir")"
  terrapod_target="$local_bin_dir/terrapod"
  tpod_target="$local_bin_dir/tpod"
  terrapod_staged="$terrapod_target.terrapod-new"
  tpod_staged="$tpod_target.terrapod-new"
  config_toml_source="$source_dir/dot_local/lib/terrapod/config-toml.sh"
  config_toml_dir="$(user_local_terrapod_lib_dir)"
  config_toml_target="$config_toml_dir/config-toml.sh"
  config_toml_staged="$config_toml_target.terrapod-new"

  [ -f "$config_toml_source" ] ||
    fatal "checked-out Terrapod config reader is missing: $config_toml_source"

  ensure_command_surface_path_repairable "$terrapod_target" "$source_dir" "$profile"
  ensure_command_surface_path_repairable "$tpod_target" "$source_dir" "$profile"

  rm -f "$terrapod_staged" "$tpod_staged" ||
    fatal "failed to clear staged Terrapod command files under $local_bin_dir"
  cp "$terrapod_source" "$terrapod_staged" ||
    fail_command_surface_repair "$terrapod_staged" "failed to install Terrapod command at $terrapod_target"
  chmod +x "$terrapod_staged" ||
    fail_command_surface_repair "$terrapod_staged" "failed to make Terrapod command executable: $terrapod_target"
  mv -f "$terrapod_staged" "$terrapod_target" ||
    fail_command_surface_repair "$terrapod_staged" "failed to install Terrapod command at $terrapod_target"
  ln -s terrapod "$tpod_staged" ||
    fail_command_surface_repair "$tpod_staged" "failed to install tpod alias at $tpod_target"
  mv -f "$tpod_staged" "$tpod_target" ||
    fail_command_surface_repair "$tpod_staged" "failed to install tpod alias at $tpod_target"

  # The installed command cannot read the managed config without this reader,
  # so it ships with the command rather than waiting for the full apply.
  mkdir -p "$config_toml_dir" ||
    fatal "failed to create Terrapod library directory: $config_toml_dir"
  rm -f "$config_toml_staged" ||
    fatal "failed to clear the staged Terrapod config reader under $config_toml_dir"
  cp "$config_toml_source" "$config_toml_staged" ||
    fail_command_surface_repair "$config_toml_staged" "failed to install the Terrapod config reader at $config_toml_target"
  mv -f "$config_toml_staged" "$config_toml_target" ||
    fail_command_surface_repair "$config_toml_staged" "failed to install the Terrapod config reader at $config_toml_target"

  validate_recovery_core_command_surface "$profile" "$local_bin_dir"
}

validate_recovery_core_command_surface() {
  profile="$1"
  local_bin_dir="$2"
  terrapod_bin="$local_bin_dir/terrapod"
  tpod_bin="$local_bin_dir/tpod"
  config_toml_bin="$(user_local_terrapod_lib_dir)/config-toml.sh"

  [ -x "$terrapod_bin" ] ||
    fatal "terrapod was not installed at $terrapod_bin after recovery-core apply"
  [ -x "$tpod_bin" ] ||
    fatal "tpod was not installed at $tpod_bin after recovery-core apply"
  [ -f "$config_toml_bin" ] ||
    fatal "the Terrapod config reader was not installed at $config_toml_bin after recovery-core apply"
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

# Returns 0 for a clean apply, 2 when install warning markers changed, and 3
# when the marker state could not be read.
run_initial_apply() {
  profile="$1"
  source_dir="$2"
  local_bin_dir="$3"
  tpod_bin="$local_bin_dir/tpod"
  marker_snapshot_dir="$(mktemp -d)" ||
    fatal "failed to create install-warning snapshot directory"
  trap 'rm -rf "$marker_snapshot_dir"' EXIT
  trap 'rm -rf "$marker_snapshot_dir"; exit 1' INT TERM

  snapshot_install_warnings_from_source "$source_dir" "$marker_snapshot_dir" ||
    fatal "failed to snapshot install warning markers"

  if ! TERRAPOD_PROFILE="$profile" TERRAPOD_FIRST_RUN_APPLY=1 "$tpod_bin" apply; then
    fatal "installed tpod apply failed"
  fi

  marker_change_status=0
  install_warning_marker_change_status "$source_dir" "$marker_snapshot_dir" ||
    marker_change_status="$?"

  rm -rf "$marker_snapshot_dir"
  trap - EXIT INT TERM

  case "$marker_change_status" in
    0)
      return 0
      ;;
    1)
      return 2
      ;;
    *)
      return 3
      ;;
  esac
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

print_first_run_unknown_marker_completion() {
  local_bin_dir="$1"

  printf '\n'
  printf '%s\n' "Terrapod first-run apply completed with an unknown warning state."
  printf '%s\n' "Warning:"
  printf '%s\n' "  Terrapod installed and the recovery core is valid, but install warning markers could not be read,"
  printf '%s\n' "  so new machine profile readiness warnings could not be detected."
  printf '%s\n' "  Review the full apply output above, then run:"
  printf '%s\n' "  $local_bin_dir/tpod doctor"
}

main() {
  profile="$(detect_profile)"
  if [ "${TERRAPOD_PRINT_EXPECTED_HOMEBREW_PATH:-}" = 1 ]; then
    expected_homebrew_path "$profile" "$(machine_arch)"
    return
  fi
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
  load_config_toml_from_source "$source_dir" ||
    fatal "failed to load the managed config reader from $source_dir"
  ensure_first_run_setup "$profile" "$source_dir" "$chezmoi_bin"
  load_install_warnings_from_source "$source_dir" ||
    fatal "failed to load the install warning library from $source_dir"
  apply_recovery_core_command_surface "$profile" "$source_dir" "$local_bin_dir"
  apply_recovery_core_shell_startup_files "$profile" "$chezmoi_bin"
  initial_apply_status=0
  run_initial_apply "$profile" "$source_dir" "$local_bin_dir" || initial_apply_status="$?"
  show_first_run_help "$profile" "$local_bin_dir"
  print_first_run_tpod_availability "$local_bin_dir"

  case "$initial_apply_status" in
    0)
      print_first_run_clean_completion
      ;;
    2)
      print_first_run_warning_completion "$local_bin_dir"
      ;;
    3)
      print_first_run_unknown_marker_completion "$local_bin_dir"
      ;;
    *)
      fatal "unexpected initial apply status: $initial_apply_status"
      ;;
  esac
}

main "$@"
