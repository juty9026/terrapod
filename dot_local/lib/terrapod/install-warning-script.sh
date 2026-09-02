#!/bin/sh

# Installer-side policy over the install warning markers. Chezmoi scripts inline
# or source this beside install-warnings.sh; tpod does not use it.
#
# There is deliberately no TERRAPOD_..._LOADED sentinel here. Nothing may branch
# on whether this file loaded — that branch is the defect this library removes.

INSTALL_WARNING_RECORDED=0

# Records a warning marker. Never fails: callers run under `set -e`, and a
# non-zero return here would kill the script before it reaches its exit policy.
mark_install_warning() {
  category="$1"
  summary="$2"
  guidance="$3"
  INSTALL_WARNING_RECORDED=0

  if terrapod_install_warning_write "$category" "$summary" "$guidance"; then
    INSTALL_WARNING_RECORDED=1
  fi

  return 0
}

install_warning_recorded() {
  [ "$INSTALL_WARNING_RECORDED" -eq 1 ]
}

# The user was told what went wrong, so the apply continues. If we could not
# even record the warning, fail loudly instead of failing silently.
exit_after_install_warning() {
  if install_warning_recorded; then
    exit 0
  fi

  exit 1
}

continue_after_core_install_warning() {
  if ! install_warning_recorded; then
    exit 1
  fi

  return 0
}

clear_install_warning() {
  terrapod_install_warning_clear "$1" || true
}
