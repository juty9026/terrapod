#!/bin/sh

# Successful installation fingerprints live outside install-warnings: pruning
# warnings must never turn an unchanged apply into another installation.
# Publish in a separate shell so cleanup traps do not replace a caller's traps.
terrapod_install_source_save() (
  install_source_path="$1"
  install_source_value="$2"
  umask 077
  mkdir -p "${install_source_path%/*}" || exit 1
  [ ! -d "$install_source_path" ] || exit 1
  install_source_staging="$(mktemp "${install_source_path%/*}/.success.XXXXXX")" || exit 1
  trap 'rm -f "$install_source_staging"' EXIT
  trap 'exit 1' HUP INT TERM
  printf '%s\n' "$install_source_value" >"$install_source_staging" || exit 1
  mv -f "$install_source_staging" "$install_source_path" || exit 1
)

# One always-run entry point per category calls this once per apply:
#   terrapod_install_if_changed <category> <summary> <fingerprint> <body> [args]
# The body terminates its shell through the install warning policy. Exit 0
# alone does not mean success: a recorded failure or failed marker clear leaves
# a warning and must not advance the successful fingerprint.
terrapod_install_if_changed() {
  install_source_category="$1"
  install_source_summary="$2"
  install_source_fingerprint="$3"
  shift 3
  install_source_path="${XDG_STATE_HOME:-$HOME/.local/state}/terrapod/install-sources/$install_source_category"

  if ! terrapod_install_warning_existing_path "$install_source_category" >/dev/null 2>&1 &&
    printf '%s\n' "$install_source_fingerprint" | cmp -s - "$install_source_path"; then
    return 0
  fi

  # Keep this a standalone command under set -e, so fatal body failures and
  # interruptions stop here rather than publishing successful state.
  ( "$@" )
  if terrapod_install_warning_existing_path "$install_source_category" >/dev/null 2>&1; then
    return 0
  fi

  if ! terrapod_install_source_save "$install_source_path" "$install_source_fingerprint"; then
    printf 'terrapod: could not save successful installation state for %s\n' "$install_source_category" >&2
    declare_install_warning_category "$install_source_category" "$install_source_summary"
    fail_install_warning_category "Restore write access to ${install_source_path%/*}, then rerun tpod apply."
  fi
}
