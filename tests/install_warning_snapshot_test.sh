#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

HOME="$tmp_dir/home"
export HOME
mkdir -p "$HOME"
. "$repo_root/dot_local/lib/terrapod/install-warnings.sh"
snapshot_dir="$tmp_dir/snapshot"

terrapod_install_warning_write mise-tools "existing warning" "existing guidance"
terrapod_install_warning_snapshot "$snapshot_dir"
change_status=0
terrapod_install_warning_change_status "$snapshot_dir" || change_status="$?"
assert_status "$change_status" 0 "warning library reports no change for an unchanged marker"

terrapod_install_warning_write mise-tools "new warning" "new guidance"
change_status=0
terrapod_install_warning_change_status "$snapshot_dir" || change_status="$?"
assert_status "$change_status" 1 "warning library reports a rewritten marker"

marker="$HOME/.local/state/terrapod/install-warnings/mise-tools"
chmod 000 "$marker"
change_status=0
terrapod_install_warning_change_status "$snapshot_dir" || change_status="$?"
chmod 600 "$marker"
assert_status "$change_status" 2 "warning library reports an unreadable marker as unknown"

terrapod_install_warning_write mise-tools "preexisting warning" "preexisting guidance"
chmod 000 "$marker"
unknown_snapshot_dir="$tmp_dir/unknown-snapshot"
terrapod_install_warning_snapshot "$unknown_snapshot_dir"
chmod 600 "$marker"
terrapod_install_warning_clear mise-tools
change_status=0
terrapod_install_warning_change_status "$unknown_snapshot_dir" || change_status="$?"
assert_status "$change_status" 2 "warning library retains an unreadable pre-apply state after the marker is cleared"
