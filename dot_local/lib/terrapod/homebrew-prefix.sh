#!/bin/sh

TERRAPOD_HOMEBREW_PREFIX_LOADED=1

terrapod_homebrew_process_arch() {
  if [ -n "${TERRAPOD_MACHINE_ARCH:-}" ]; then
    printf '%s\n' "$TERRAPOD_MACHINE_ARCH"
  else
    uname -m 2>/dev/null
  fi
}

terrapod_darwin_is_translated() {
  case "${TERRAPOD_DARWIN_TRANSLATED:-}" in
    1) return 0 ;;
    0) return 1 ;;
  esac

  [ "$(sysctl -in sysctl.proc_translated 2>/dev/null || true)" = "1" ]
}

terrapod_homebrew_hardware_arch() {
  os="$1"
  process_arch="$(terrapod_homebrew_process_arch)" || return 1

  if [ "$os" = darwin ] && [ "$process_arch" = x86_64 ] && terrapod_darwin_is_translated; then
    printf '%s\n' arm64
  else
    printf '%s\n' "$process_arch"
  fi
}

terrapod_standard_homebrew_prefix_for_os() {
  os="$1"
  hardware_arch="$(terrapod_homebrew_hardware_arch "$os")" || return 1

  case "$os:$hardware_arch" in
    darwin:arm64|darwin:aarch64) printf '%s\n' /opt/homebrew ;;
    darwin:x86_64) printf '%s\n' /usr/local ;;
    linux:x86_64|linux:aarch64) printf '%s\n' /home/linuxbrew/.linuxbrew ;;
    *) return 1 ;;
  esac
}

terrapod_standard_homebrew_brew_path() {
  os="$1"
  hardware_arch="$(terrapod_homebrew_hardware_arch "$os")" || return 1
  case "$os:$hardware_arch" in
    darwin:arm64|darwin:aarch64) printf '%s\n' /opt/homebrew/bin/brew ;;
    darwin:x86_64) printf '%s\n' /usr/local/bin/brew ;;
    linux:x86_64|linux:aarch64) printf '%s\n' /home/linuxbrew/.linuxbrew/bin/brew ;;
    *) return 1 ;;
  esac
}

terrapod_standard_homebrew_mise_path() {
  os="$1"
  hardware_arch="$(terrapod_homebrew_hardware_arch "$os")" || return 1
  case "$os:$hardware_arch" in
    darwin:arm64|darwin:aarch64) printf '%s\n' /opt/homebrew/bin/mise ;;
    darwin:x86_64) printf '%s\n' /usr/local/bin/mise ;;
    linux:x86_64|linux:aarch64) printf '%s\n' /home/linuxbrew/.linuxbrew/bin/mise ;;
    *) return 1 ;;
  esac
}
