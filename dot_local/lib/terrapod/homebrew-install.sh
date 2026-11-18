#!/bin/sh

terrapod_homebrew_install() (
  brew_executable="$1"
  operation="$2"
  shift 2

  export HOMEBREW_NO_AUTO_UPDATE=1
  case "$operation" in
    install) "$brew_executable" install "$@" ;;
    bundle) "$brew_executable" bundle --no-upgrade --file="$1" ;;
    *)
      printf 'Unsupported Homebrew installation operation: %s\n' "$operation" >&2
      return 2
      ;;
  esac
)
