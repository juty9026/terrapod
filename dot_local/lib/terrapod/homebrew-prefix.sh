#!/bin/sh

TERRAPOD_HOMEBREW_PREFIX_LOADED=1

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
