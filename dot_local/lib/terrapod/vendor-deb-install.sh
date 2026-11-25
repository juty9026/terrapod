#!/bin/sh

# Shared steps of the installers that put one vendor .deb on the VPS Shell
# Profile through APT: Orca (ADR 0026) and Grok Bot (ADR 0027). Each chezmoi
# script inlines this beside install-warnings.sh and install-warning-script.sh
# and keeps its own vendor part: the index or manifest, its parsing, the digest
# algorithm and the file name validation. This is not a general .deb installer.
#
#   vendor_deb_is_installed        <package>
#   vendor_deb_require_privileges  <app name>
#   vendor_deb_make_work_dir       <slug>
#   vendor_deb_apt_install         <deb path>
#   vendor_deb_cleanup
#
# Every function returns non-zero on failure and leaves the step in progress in
# `vendor_deb_install_step`, so the caller's warning says why the app is
# missing. The vendor part sets that step itself. State lives in these globals,
# so call the functions from the script's own shell, never inside a command
# substitution. The script installs the cleanup traps:
#
#   trap 'vendor_deb_cleanup || true' EXIT
#   trap 'exit 1' HUP INT TERM

vendor_deb_install_step=
vendor_deb_work_dir=
vendor_deb_sudo=

vendor_deb_is_installed() {
  # A removed package that kept its configuration reports config-files, which
  # is not installed.
  [ "$(dpkg-query -W -f='${db:Status-Status}' "$1" 2>/dev/null)" = installed ]
}

# Privileges come before any download because a machine that cannot run APT
# must not cost a package download of hundreds of megabytes.
vendor_deb_require_privileges() {
  if [ "$(id -u)" -eq 0 ]; then
    vendor_deb_sudo=""
  elif command -v sudo >/dev/null 2>&1; then
    vendor_deb_sudo="sudo"
  else
    printf '%s\n' "sudo is required to install $1." >&2
    vendor_deb_install_step="sudo is required; install sudo or run as root"
    return 1
  fi
}

vendor_deb_make_work_dir() {
  vendor_deb_install_step="work directory"
  vendor_deb_work_dir="$(mktemp -d "${TMPDIR:-/tmp}/terrapod-$1.XXXXXX")" || return 1
}

vendor_deb_cleanup() {
  [ -n "$vendor_deb_work_dir" ] || return 0

  vendor_deb_cleanup_dir="$vendor_deb_work_dir"
  vendor_deb_work_dir=
  rm -rf "$vendor_deb_cleanup_dir"
}

# The package indexes can be stale on a machine whose bootstrap did not rerun,
# and the desktop dependencies of the package come from them. `sudo` resets the
# environment, so the non-interactive frontend travels through `env`.
vendor_deb_apt_install() {
  vendor_deb_install_step="apt update"
  $vendor_deb_sudo env DEBIAN_FRONTEND=noninteractive apt-get update -y || return 1

  vendor_deb_install_step="apt install"
  $vendor_deb_sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y "$1" || return 1
}
