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

vps_sudo_cmd() {
  if [ "$(id -u)" -eq 0 ]; then
    printf '%s\n' ""
  elif command -v sudo >/dev/null 2>&1; then
    printf '%s\n' "sudo"
  else
    fatal "Ubuntu bootstrap prerequisites are required before Terrapod Setup. Install sudo so Terrapod can prepare git and gum with apt-get, or install git and gum manually before rerunning the installer."
  fi
}

ensure_charm_apt_repository() {
  sudo_cmd="$1"

  if ! $sudo_cmd install -dm 755 /etc/apt/keyrings; then
    fatal "failed to create the APT keyring directory for the Charm repository. Check sudo permissions and rerun the Terrapod installer before Terrapod Setup."
  fi

  if ! charm_key_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-charm-key.XXXXXX")"; then
    fatal "failed to create a temporary file for the Charm APT signing key. Check temporary directory permissions and rerun the Terrapod installer before Terrapod Setup."
  fi

  if ! curl -fsSL https://repo.charm.sh/apt/gpg.key -o "$charm_key_file"; then
    rm -f "$charm_key_file"
    fatal "failed to fetch the Charm APT signing key for gum. Check network access to https://repo.charm.sh/apt/gpg.key and rerun the Terrapod installer before Terrapod Setup."
  fi

  if ! $sudo_cmd gpg --dearmor --yes -o /etc/apt/keyrings/charm.gpg "$charm_key_file"; then
    rm -f "$charm_key_file"
    fatal "failed to install the Charm APT signing key for gum. Check APT keyring permissions and rerun the Terrapod installer before Terrapod Setup."
  fi
  rm -f "$charm_key_file"

  if ! printf '%s\n' "deb [signed-by=/etc/apt/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *" | $sudo_cmd tee /etc/apt/sources.list.d/charm.list >/dev/null; then
    fatal "failed to write the Charm APT repository for gum. Check sudo permissions and rerun the Terrapod installer before Terrapod Setup."
  fi

  if ! $sudo_cmd apt-get update -y; then
    fatal "failed to update APT metadata after adding the Charm APT repository for gum. Check APT connectivity and rerun the Terrapod installer before Terrapod Setup."
  fi
}

ensure_source_repo_prerequisites() {
  profile="$1"

  if [ "$profile" != "vps-shell" ]; then
    return 0
  fi

  if command -v git >/dev/null 2>&1 && command -v gum >/dev/null 2>&1; then
    return 0
  fi

  sudo_cmd="$(vps_sudo_cmd)"
  bootstrap_packages="ca-certificates curl"
  if ! command -v git >/dev/null 2>&1; then
    bootstrap_packages="$bootstrap_packages git"
  fi
  bootstrap_packages="$bootstrap_packages gpg"

  if ! $sudo_cmd apt-get update -y; then
    fatal "failed to update APT metadata before installing Ubuntu bootstrap prerequisites"
  fi

  if ! $sudo_cmd apt-get install -y $bootstrap_packages; then
    fatal "failed to install Ubuntu bootstrap prerequisites before Terrapod Setup. Install ca-certificates, curl, git, and gpg, then rerun the Terrapod installer."
  fi

  if command -v gum >/dev/null 2>&1; then
    return 0
  fi

  ensure_charm_apt_repository "$sudo_cmd"

  if ! $sudo_cmd apt-get install -y gum; then
    fatal "failed to install gum from the Charm APT repository. Check APT output, fix repository/package access, and rerun the Terrapod installer before Terrapod Setup."
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

print_setup_ui_dependency_recovery() {
  profile="$1"
  source_dir="$2"
  chezmoi_bin="$3"
  brew_bin="$4"
  reason="$5"

  printf '%s\n' "terrapod installer: gum is required before Terrapod Setup can run." >&2
  printf '%s\n' "terrapod installer: Failed to prepare the macOS setup UI dependency with Homebrew: $reason" >&2
  if [ -n "$brew_bin" ]; then
    printf '%s\n' "terrapod installer: Prepare gum with Homebrew:" >&2
    printf '%s\n' "terrapod installer:   eval \"\$(\"$brew_bin\" shellenv)\" && HOMEBREW_NO_AUTO_UPDATE=1 \"$brew_bin\" install gum" >&2
  else
    printf '%s\n' "terrapod installer: Install Homebrew from https://brew.sh, follow its shellenv instructions, then run: HOMEBREW_NO_AUTO_UPDATE=1 brew install gum" >&2
  fi
  printf '%s\n' "terrapod installer: The source repository is already checked out. After gum is available, resume Terrapod Setup and continue the initial apply:" >&2
  if [ -n "$brew_bin" ]; then
    printf '%s\n' "terrapod installer:   cd \"$source_dir\" && eval \"\$(\"$brew_bin\" shellenv)\" && TERRAPOD_PROFILE=\"$profile\" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup && \"$chezmoi_bin\" apply" >&2
  else
    printf '%s\n' "terrapod installer:   cd \"$source_dir\" && TERRAPOD_PROFILE=\"$profile\" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup && \"$chezmoi_bin\" apply" >&2
  fi
}

prepare_setup_ui_dependency() {
  profile="$1"
  source_dir="$2"
  chezmoi_bin="$3"

  if [ "$profile" != "macos-terminal" ]; then
    return 0
  fi

  if command -v gum >/dev/null 2>&1; then
    return 0
  fi

  brew_bin="$(find_homebrew || true)"

  if [ -z "$brew_bin" ]; then
    printf '%s\n' "Installing Homebrew for Terrapod Setup UI dependency..."
    if ! homebrew_installer="$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"; then
      print_setup_ui_dependency_recovery "$profile" "$source_dir" "$chezmoi_bin" "" "failed to download the Homebrew installer"
      return 1
    fi

    if ! NONINTERACTIVE=1 /bin/bash -c "$homebrew_installer" </dev/null >&2; then
      print_setup_ui_dependency_recovery "$profile" "$source_dir" "$chezmoi_bin" "" "failed to install Homebrew"
      return 1
    fi

    brew_bin="$(find_homebrew || true)"
  fi

  if [ -z "$brew_bin" ]; then
    print_setup_ui_dependency_recovery "$profile" "$source_dir" "$chezmoi_bin" "" "Homebrew install finished, but brew was not found"
    return 1
  fi

  if ! brew_shellenv="$("$brew_bin" shellenv)"; then
    print_setup_ui_dependency_recovery "$profile" "$source_dir" "$chezmoi_bin" "$brew_bin" "failed to evaluate brew shellenv"
    return 1
  fi
  eval "$brew_shellenv"

  if command -v gum >/dev/null 2>&1; then
    return 0
  fi

  if ! HOMEBREW_NO_AUTO_UPDATE=1 "$brew_bin" install gum; then
    print_setup_ui_dependency_recovery "$profile" "$source_dir" "$chezmoi_bin" "$brew_bin" "failed to install gum"
    return 1
  fi

  if command -v gum >/dev/null 2>&1; then
    return 0
  fi

  print_setup_ui_dependency_recovery "$profile" "$source_dir" "$chezmoi_bin" "$brew_bin" "brew install gum finished, but gum was not found"
  return 1
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

show_first_run_help() {
  profile="$1"
  local_bin_dir="$2"
  tpod_bin="$local_bin_dir/tpod"

  if [ ! -x "$tpod_bin" ]; then
    fatal "tpod was not installed at $tpod_bin after initial apply"
  fi

  TERRAPOD_PROFILE="$profile" "$tpod_bin" help || fatal "tpod help failed after initial apply"
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
  prepare_setup_ui_dependency "$profile" "$source_dir" "$chezmoi_bin"
  run_terrapod_setup "$profile" "$source_dir"
  run_initial_apply "$chezmoi_bin"
  show_first_run_help "$profile" "$local_bin_dir"

  printf '%s\n' "Terrapod first-run apply complete."
}

main "$@"
