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

reject_existing_source_dir() {
  source_dir="$1"

  if [ -e "$source_dir" ] || [ -L "$source_dir" ]; then
    fatal "chezmoi source directory already exists: $source_dir. Move it aside before first-run install, or use the existing checkout with Terrapod or chezmoi."
  fi
}

install_chezmoi_if_needed() {
  local_bin_dir="$1"
  chezmoi_path="$local_bin_dir/chezmoi"

  if [ -x "$chezmoi_path" ]; then
    printf '%s\n' "$chezmoi_path"
    return 0
  fi

  if ! installer_script="$(curl -fsLS get.chezmoi.io)"; then
    fatal "failed to download chezmoi installer"
  fi

  if ! sh -c "$installer_script" -- -b "$local_bin_dir" </dev/null >&2; then
    fatal "failed to install chezmoi"
  fi

  if [ ! -x "$chezmoi_path" ]; then
    fatal "chezmoi installer did not create executable: $chezmoi_path"
  fi

  printf '%s\n' "$chezmoi_path"
}

ensure_source_repo_prerequisites() {
  profile="$1"

  if [ "$profile" != "vps-shell" ]; then
    return 0
  fi

  if command -v git >/dev/null 2>&1; then
    return 0
  fi

  if [ "$(id -u)" -eq 0 ]; then
    sudo_cmd=""
  elif command -v sudo >/dev/null 2>&1; then
    sudo_cmd="sudo"
  else
    fatal "git is required before chezmoi can initialize the source repository. Install git, or install sudo so Terrapod can install git with apt-get."
  fi

  if ! $sudo_cmd apt-get update -y; then
    fatal "failed to update APT metadata before installing git"
  fi

  if ! $sudo_cmd apt-get install -y ca-certificates git; then
    fatal "failed to install git before initializing the source repository"
  fi
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

print_setup_recovery() {
  profile="$1"
  source_dir="$2"

  printf '%s\n' "terrapod installer: Terrapod Setup did not complete." >&2
  printf '%s\n' "terrapod installer: Resume Terrapod Setup from the checked-out source repository:" >&2
  printf '%s\n' "terrapod installer:   cd \"$source_dir\" && TERRAPOD_PROFILE=\"$profile\" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup" >&2
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

run_initial_apply() {
  chezmoi_bin="$1"

  "$chezmoi_bin" apply || fatal "chezmoi apply failed"
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
  reject_existing_source_dir "$source_dir"
  chezmoi_bin="$(install_chezmoi_if_needed "$local_bin_dir")"
  ensure_source_repo_prerequisites "$profile"
  initialize_source_repository "$chezmoi_bin"
  run_terrapod_setup "$profile" "$source_dir"
  run_initial_apply "$chezmoi_bin"

  printf '%s\n' "Terrapod first-run apply complete."
}

main "$@"
