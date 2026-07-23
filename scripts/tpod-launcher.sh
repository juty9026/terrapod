#!/bin/sh
set -eu

INSTALLER_VERSION="v1.0.0"
INSTALLER_URL="https://github.com/juty9026/terrapod/releases/download/$INSTALLER_VERSION/install.sh"

fatal() {
  printf '%s\n' "tpod launcher: $*" >&2
  exit 1
}

repair_guidance() {
  printf '%s\n' "tpod launcher: the active Terrapod Management Core is missing or broken." >&2
  printf '%s\n' 'sh -c "$(curl -fsLS '"$INSTALLER_URL"')" -- --repair' >&2
  exit 1
}

valid_release_version() {
  value="$1"
  old_ifs="$IFS"
  IFS=.
  set -- $value
  IFS="$old_ifs"
  [ "$#" -eq 3 ] || return 1
  for component in "$@"; do
    case "$component" in
      ''|*[!0-9]*|0[0-9]*) return 1 ;;
    esac
  done
  return 0
}

[ -x /usr/bin/id ] || fatal "trusted /usr/bin/id is unavailable"
uid="$(/usr/bin/id -u)" || fatal "failed to determine the effective user"
case "$uid" in
  0)
    fatal "refusing to run as root"
    ;;
  ''|*[!0-9]*)
    fatal "invalid effective user ID: $uid"
    ;;
esac

data_home="${XDG_DATA_HOME:-$HOME/.local/share}"
terrapod_data="$data_home/terrapod"
target="$terrapod_data/current/bin/tpod"

[ ! -L "$target" ] || repair_guidance
[ -f "$target" ] && [ -x "$target" ] || repair_guidance

if ! physical_data="$(CDPATH= cd -P -- "$terrapod_data" 2>/dev/null && pwd -P)"; then
  repair_guidance
fi
if ! physical_bin="$(CDPATH= cd -P -- "$terrapod_data/current/bin" 2>/dev/null && pwd -P)"; then
  repair_guidance
fi
physical_target="$physical_bin/tpod"
case "$physical_target" in
  "$physical_data"/releases/*/bin/tpod)
    ;;
  *)
    repair_guidance
    ;;
esac

release_relative="${physical_target#"$physical_data/releases/"}"
release_version="${release_relative%%/*}"
[ "$release_relative" = "$release_version/bin/tpod" ] || repair_guidance
case "$release_version" in
  ''|*[!0-9.]*|.*|*..*|*.)
    repair_guidance
    ;;
esac
valid_release_version "$release_version" || repair_guidance

[ ! -L "$physical_target" ] || repair_guidance
[ -f "$physical_target" ] && [ -x "$physical_target" ] || repair_guidance

exec "$physical_target" "$@"
