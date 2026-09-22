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

# High-level interface for a non-blocking installer script that handles exactly
# one install warning category. The script states the category and what it
# attempts; the marker-and-success contract lives here, so no script decides
# for itself when to record, clear or exit.
#
#   declare_install_warning_category <category> <summary>
#   note_failed_install_warning_item <item>
#   fail_install_warning_category    <guidance>
#   finish_install_warning_category  [<guidance format with one %s>]
#
# The two terminal calls always end the script. They exit 0 once the marker is
# recorded, or once the category succeeded, and exit 1 only when the marker
# could not be written. A failed clear never fails the script. `exit` runs an
# EXIT trap the script installed, so cleanup still happens. Call them from the
# script's own shell, not inside a command substitution or pipeline, or the exit
# only leaves that subshell.

INSTALL_WARNING_CATEGORY=
INSTALL_WARNING_SUMMARY=
INSTALL_WARNING_FAILED_ITEMS=

# Calling this again for the same category replaces its summary, for a script
# that records different summaries depending on how it failed.
declare_install_warning_category() {
  INSTALL_WARNING_CATEGORY="$1"
  INSTALL_WARNING_SUMMARY="$2"
}

# The script keeps attempting its remaining items after noting a failed one.
note_failed_install_warning_item() {
  if [ -n "$INSTALL_WARNING_FAILED_ITEMS" ]; then
    INSTALL_WARNING_FAILED_ITEMS="$INSTALL_WARNING_FAILED_ITEMS, $1"
  else
    INSTALL_WARNING_FAILED_ITEMS="$1"
  fi
}

fail_install_warning_category() {
  mark_install_warning "$INSTALL_WARNING_CATEGORY" "$INSTALL_WARNING_SUMMARY" "$1"
  exit_after_install_warning
}

# The format carries the failed names into the guidance, because each category
# words the prefix in front of them differently. It is only read when an item
# failed, so a script that has no failures to report may leave it out.
finish_install_warning_category() {
  if [ -n "$INSTALL_WARNING_FAILED_ITEMS" ]; then
    # shellcheck disable=SC2059
    finish_guidance="$(printf "$1" "$INSTALL_WARNING_FAILED_ITEMS")"
    fail_install_warning_category "$finish_guidance"
  fi

  clear_install_warning "$INSTALL_WARNING_CATEGORY"
  exit 0
}
