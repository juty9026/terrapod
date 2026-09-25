#!/bin/sh

# The install body shared by the Jetendard font installer and its retry. It is
# its own library so the installer can checksum it: an edit here re-runs the
# installer, while guidance edits in jetendard-font-status.sh do not.
#
#   terrapod_jetendard_font_install <font_helper>
#
# Declares the jetendard-font category, runs the helper's install subcommand,
# and records or clears the marker. It always ends the script through the
# install warning policy layer, so call it from the script's own shell, not
# inside a command substitution or pipeline. The caller loads
# install-warnings.sh, install-warning-script.sh and jetendard-font-status.sh
# first.
terrapod_jetendard_font_install() {
  font_helper="$1"

  declare_install_warning_category jetendard-font "Jetendard font install needs attention"

  if command -v python3 >/dev/null 2>&1; then
    if python3 "$font_helper" install; then
      printf '%s\n' "Jetendard is ready. Restart Ghostty or Zed if an existing window still uses a cached font."
      finish_install_warning_category
    else
      # The helper's exit status names the failure; anything else is generic.
      font_status=$?
    fi
  else
    font_status=127
  fi

  fail_install_warning_category "$(terrapod_jetendard_font_guidance "$font_status")"
}
