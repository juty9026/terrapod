#!/bin/sh

# Tool paths are derived from the selected prefix. Prefix policy lives in a
# provider so tests can substitute only that policy without copying this logic.
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

terrapod_standard_homebrew_os_for_profile() {
  case "$1" in
    macos-terminal) printf '%s\n' darwin ;;
    vps-shell) printf '%s\n' linux ;;
    *) return 1 ;;
  esac
}

terrapod_standard_homebrew_prefix_for_profile() {
  os="$(terrapod_standard_homebrew_os_for_profile "$1")" || return 1
  terrapod_standard_homebrew_prefix_for_os "$os"
}

terrapod_standard_homebrew_tool_path() {
  os="$1"
  tool="$2"
  prefix="$(terrapod_standard_homebrew_prefix_for_os "$os")" || return 1
  printf '%s/bin/%s\n' "$prefix" "$tool"
}

terrapod_standard_homebrew_brew_path() {
  terrapod_standard_homebrew_tool_path "$1" brew
}

terrapod_standard_homebrew_mise_path() {
  terrapod_standard_homebrew_tool_path "$1" mise
}

terrapod_standard_homebrew_gh_path() {
  terrapod_standard_homebrew_tool_path "$1" gh
}
