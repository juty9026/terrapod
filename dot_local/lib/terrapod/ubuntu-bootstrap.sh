#!/bin/sh

TERRAPOD_UBUNTU_BOOTSTRAP_LOADED=1
TERRAPOD_UBUNTU_BOOTSTRAP_SUMMARY="Ubuntu bootstrap needs attention"
TERRAPOD_UBUNTU_BOOTSTRAP_GUIDANCE=

terrapod_ubuntu_bootstrap_run() {
  os_id="$1"
  version_id="$2"
  TERRAPOD_UBUNTU_BOOTSTRAP_GUIDANCE=

  if [ "$os_id" != "ubuntu" ] || [ "$version_id" != "24.04" ]; then
    printf '%s\n' "Unsupported Linux release: ${os_id:-unknown} ${version_id:-unknown}. This dotfiles repo supports Ubuntu 24.04 LTS for the VPS Shell Profile." >&2
    TERRAPOD_UBUNTU_BOOTSTRAP_GUIDANCE="Run Terrapod on Ubuntu 24.04, or use tpod doctor for current platform guidance."
    return 1
  fi

  if [ "$(id -u)" -eq 0 ]; then
    sudo_cmd=""
  elif command -v sudo >/dev/null 2>&1; then
    sudo_cmd="sudo"
  else
    printf '%s\n' "sudo is required to install Ubuntu bootstrap packages." >&2
    TERRAPOD_UBUNTU_BOOTSTRAP_GUIDANCE="Install sudo or run as root, then rerun tpod apply."
    return 1
  fi

  export DEBIAN_FRONTEND=noninteractive

  if ! $sudo_cmd apt-get update -y; then
    TERRAPOD_UBUNTU_BOOTSTRAP_GUIDANCE="Review APT update output, fix package repository access, then rerun tpod apply."
    return 1
  fi

  if ! $sudo_cmd apt-get install -y \
    build-essential \
    ca-certificates \
    curl \
    file \
    git \
    libbz2-dev \
    libffi-dev \
    liblzma-dev \
    libncursesw5-dev \
    libreadline-dev \
    libsqlite3-dev \
    libssl-dev \
    libxml2-dev \
    libxmlsec1-dev \
    procps \
    tk-dev \
    unzip \
    xz-utils \
    zlib1g-dev \
    zsh; then
    TERRAPOD_UBUNTU_BOOTSTRAP_GUIDANCE="Review APT install output for system and Homebrew prerequisites, then rerun tpod apply."
    return 1
  fi

  target_user="$(id -un)"
  zsh_path="/usr/bin/zsh"
  current_shell="$(getent passwd "$target_user" | cut -d: -f7)"

  if [ "$current_shell" != "$zsh_path" ]; then
    printf '%s\n' "Setting login shell for $target_user to $zsh_path"
    if ! $sudo_cmd chsh -s "$zsh_path" "$target_user"; then
      printf '%s\n' "Could not change the login shell automatically. Run this manually after setup: chsh -s \"$zsh_path\"" >&2
      TERRAPOD_UBUNTU_BOOTSTRAP_GUIDANCE="Run chsh -s /usr/bin/zsh after fixing shell permission issues."
      return 1
    fi
  fi

  return 0
}

# The install body shared by the bootstrap-ubuntu installer and its retry: it
# declares the ubuntu-bootstrap category, runs the bootstrap, and records or
# clears the marker. It always ends the script through the install warning
# policy layer, so call it from the script's own shell, not inside a command
# substitution or pipeline. The caller loads install-warnings.sh and
# install-warning-script.sh first.
terrapod_ubuntu_bootstrap_install() {
  declare_install_warning_category ubuntu-bootstrap "$TERRAPOD_UBUNTU_BOOTSTRAP_SUMMARY"

  if terrapod_ubuntu_bootstrap_run "$1" "$2"; then
    finish_install_warning_category
  fi

  fail_install_warning_category "$TERRAPOD_UBUNTU_BOOTSTRAP_GUIDANCE"
}
