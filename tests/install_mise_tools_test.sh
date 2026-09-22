#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"
macos_mise_tools_installer="$(render_template "$macos_data" ".chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl")"
macos_mise_missing_script="$tmp_dir/macos-mise-missing.sh"
render_template_with_homebrew_prefix_provider \
  "$macos_data" \
  '.chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl' \
  "$tmp_dir/missing-macos-prefix" >"$macos_mise_missing_script"
sh -n "$macos_mise_missing_script" || fail "macOS mise tool installer missing-mise test script should be valid sh"
pass "macOS mise tool installer missing-mise test script is valid sh"

macos_brew_bin="$tmp_dir/macos-brew-bin"
macos_brew_log="$tmp_dir/macos-brew.log"
mkdir -p "$macos_brew_bin"
write_stub "$macos_brew_bin/brew" \
  'printf "%s\n" "brew args:$*" >>"$MACOS_BREW_LOG"' \
  'case "$1" in' \
  '  shellenv)' \
  '    case "${MACOS_BREW_SHELLENV_MODE:-success}" in' \
  '      command-failure) exit 41 ;;' \
  '      eval-failure) printf "%s\n" "false" ;;' \
  '      *) printf "%s\n" ":" ;;' \
  '    esac' \
  '    ;;' \
  '  analytics) exit 0 ;;' \
  '  bundle) exit 0 ;;' \
  '  *) exit 64 ;;' \
  'esac'

mise_missing_without_core_home="$tmp_dir/mise-missing-without-core-home"
mise_missing_without_core_state="$tmp_dir/mise-missing-without-core-state"
mkdir -p "$mise_missing_without_core_home"
mise_missing_without_core_status=0
HOME="$mise_missing_without_core_home" XDG_STATE_HOME="$mise_missing_without_core_state" PATH="/usr/bin:/bin" \
  sh "$macos_mise_missing_script" >"$tmp_dir/mise-missing-without-core.out" 2>"$tmp_dir/mise-missing-without-core.err" ||
  mise_missing_without_core_status=$?
if [ "$mise_missing_without_core_status" -ne 0 ]; then
  fail "macOS mise tool installer records a recoverable warning when mise is missing without a homebrew-core marker"
fi
mise_missing_without_core_marker="$mise_missing_without_core_state/terrapod/install-warnings/mise-tools"
if [ ! -f "$mise_missing_without_core_marker" ]; then
  fail "macOS mise tool installer records a mise-tools marker when mise is missing without a homebrew-core marker"
fi
pass "macOS mise tool installer records a recoverable mise-tools warning when mise is missing without a homebrew-core marker"

mise_missing_with_core_home="$tmp_dir/mise-missing-with-core-home"
mise_missing_with_core_state="$tmp_dir/mise-missing-with-core-state"
mkdir -p "$mise_missing_with_core_home"
HOME="$mise_missing_with_core_home" XDG_STATE_HOME="$mise_missing_with_core_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "Install failed formulae, then rerun tpod apply."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

mise_missing_with_core_status=0
HOME="$mise_missing_with_core_home" XDG_STATE_HOME="$mise_missing_with_core_state" PATH="/usr/bin:/bin" \
  sh "$macos_mise_missing_script" >"$tmp_dir/mise-missing-with-core.out" 2>"$tmp_dir/mise-missing-with-core.err" ||
  mise_missing_with_core_status=$?
if [ "$mise_missing_with_core_status" -ne 0 ]; then
  printf '%s\n' "mise missing with core stdout:" >&2
  sed 's/^/  /' "$tmp_dir/mise-missing-with-core.out" >&2
  printf '%s\n' "mise missing with core stderr:" >&2
  sed 's/^/  /' "$tmp_dir/mise-missing-with-core.err" >&2
  fail "macOS mise tool installer exits 0 when missing mise is covered by a homebrew-core marker"
fi
pass "macOS mise tool installer exits 0 when missing mise is covered by a homebrew-core marker"

if [ ! -f "$mise_missing_with_core_state/terrapod/install-warnings/homebrew-core" ]; then
  fail "macOS mise tool installer keeps the existing homebrew-core marker"
fi
if [ ! -f "$mise_missing_with_core_state/terrapod/install-warnings/mise-tools" ]; then
  fail "macOS mise tool installer records its own warning when standard Homebrew mise is missing"
fi
pass "macOS mise tool installer records missing standard Homebrew mise independently"

mise_tools_installer="$(render_template "$ubuntu_data" ".chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl")"

assert_contains "$mise_tools_installer" 'mise_bin="$(standard_mise_path || true)"' "mise installer resolves mise from a standard Homebrew prefix"
assert_contains "$mise_tools_installer" '"$mise_bin" install --yes -C "$HOME"' "Ubuntu runtime install invokes the resolved Homebrew mise"
assert_not_contains "$mise_tools_installer" '/usr/bin/mise' "Ubuntu runtime install never falls back to APT mise"

# run_after_20 is not a run_onchange_ script, so a source checksum in it gates
# nothing and only reads as if the script were change-tracked.
if printf '%s\n' "$mise_tools_installer" |
  grep -E '^# mise-config-sha256=[0-9a-f]{64}$' >/dev/null; then
  fail "mise tool installer carries no vestigial rendered-config checksum"
fi

pass "mise tool installer carries no vestigial rendered-config checksum"

mise_tools_prefix="$tmp_dir/mise-tools-prefix"
mise_tools_installer_script="$tmp_dir/mise-tools-installer.sh"
render_template_with_homebrew_prefix_provider \
  "$ubuntu_data" \
  '.chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl' \
  "$mise_tools_prefix" >"$mise_tools_installer_script"
sh -n "$mise_tools_installer_script" || fail "mise tool installer script should be valid sh"
pass "mise tool installer script should be valid sh"

mise_tools_bin="$mise_tools_prefix/bin"
mise_tools_state="$tmp_dir/mise-tools-state"
mise_tools_home="$tmp_dir/mise-tools-home"
mise_tools_log="$tmp_dir/mise-tools.log"
mkdir -p "$mise_tools_bin" "$mise_tools_home"
write_stub "$mise_tools_bin/mise" \
  'printf "%s\n" "/home/linuxbrew/.linuxbrew/bin/mise args:$*" >>"$MISE_TOOLS_LOG"' \
  'case "$1" in' \
  '  install)' \
  '    exit "${MISE_TOOLS_INSTALL_STATUS:-0}"' \
  '    ;;' \
  '  exec)' \
  '    shift' \
  '    while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do' \
  '      shift' \
  '    done' \
  '    if [ "$#" -gt 0 ]; then' \
  '      shift' \
  '    fi' \
  '    case "$*" in' \
  '      "sh -c command -v corepack")' \
  '        if [ "${MISE_TOOLS_COREPACK_PRESENT:-1}" = "1" ]; then' \
  '          exit 0' \
  '        fi' \
  '        exit 1' \
  '        ;;' \
  '      "corepack enable")' \
  '        exit "${MISE_TOOLS_COREPACK_STATUS:-0}"' \
  '        ;;' \
  '    esac' \
  '    ;;' \
  'esac' \
  'exit 0'

mise_tools_install_failure_status=0
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  MISE_TOOLS_INSTALL_STATUS=17 \
  TERRAPOD_MACHINE_ARCH=aarch64 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script" || mise_tools_install_failure_status=$?
if [ "$mise_tools_install_failure_status" -ne 0 ]; then
  fail "mise tool installer should continue after recording a mise install warning"
fi
mise_tools_marker="$mise_tools_state/terrapod/install-warnings/mise-tools"
if [ ! -f "$mise_tools_marker" ]; then
  fail "mise tool installer should write a mise-tools warning marker after mise install failure"
fi
mise_tools_marker_text="$(cat "$mise_tools_marker")"
assert_contains "$mise_tools_marker_text" "summary='mise tool install needs attention'" "mise install failure marker keeps the expected summary"
assert_contains "$mise_tools_marker_text" "Failed step(s): mise install" "mise install failure marker records failed mise install step"
assert_contains "$mise_tools_marker_text" "GITHUB_TOKEN" "mise install failure marker suggests GitHub token recovery"
assert_contains "$mise_tools_marker_text" "gh auth login" "mise install failure marker suggests GitHub auth recovery"
mise_tools_log_text="$(cat "$mise_tools_log")"
assert_contains "$mise_tools_log_text" "/home/linuxbrew/.linuxbrew/bin/mise args:install --yes -C $mise_tools_home" "Ubuntu runtime install uses Linuxbrew mise"
assert_not_contains "$mise_tools_log_text" "/usr/bin/mise" "Ubuntu runtime install never falls back to APT mise"
assert_contains "$mise_tools_log_text" "mise args:exec --yes -C $mise_tools_home -- sh -c command -v corepack" "mise install failure still checks corepack availability"
assert_contains "$mise_tools_log_text" "mise args:exec --yes -C $mise_tools_home -- corepack enable" "mise install failure still attempts corepack enable"

HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  TERRAPOD_MACHINE_ARCH=aarch64 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script"
if [ -e "$mise_tools_marker" ]; then
  fail "mise tool installer should clear stale mise-tools marker after a successful rerun"
fi
pass "mise tool installer clears stale mise-tools marker after successful rerun"

mkdir -p "$mise_tools_state/terrapod/install-warnings"
printf '%s\n' \
  "category='mise-tools'" \
  "summary='mise tool install needs attention'" \
  "guidance='stale guidance'" \
  "updated_at='2026-01-01T00:00:00Z'" \
  >"$mise_tools_marker"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  MISE_TOOLS_COREPACK_STATUS=23 \
  TERRAPOD_MACHINE_ARCH=aarch64 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script"
mise_tools_marker_text="$(cat "$mise_tools_marker")"
assert_contains "$mise_tools_marker_text" "summary='mise tool install needs attention'" "mise tool installer replacement marker keeps the expected summary"
assert_contains "$mise_tools_marker_text" "Failed step(s): corepack enable" "mise tool installer replaces stale marker with corepack enable failure"
assert_not_contains "$mise_tools_marker_text" "stale guidance" "mise tool installer replaces stale marker guidance"
assert_contains "$mise_tools_marker_text" "updated_at='" "mise tool installer replacement marker includes update timestamp"

: >"$mise_tools_log"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  TERRAPOD_MACHINE_ARCH=aarch64 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script"
if [ -e "$mise_tools_marker" ]; then
  fail "mise tool reconciliation should clear stale mise-tools marker after a successful apply"
fi
pass "mise tool reconciliation clears stale mise-tools marker after a successful apply"
mise_tools_retry_log_text="$(cat "$mise_tools_log")"
assert_contains "$mise_tools_retry_log_text" "mise args:install --yes -C $mise_tools_home" "mise tool reconciliation attempts mise install when marker exists"

: >"$mise_tools_log"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  TERRAPOD_MACHINE_ARCH=aarch64 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script"
if [ ! -s "$mise_tools_log" ]; then
  fail "mise tool reconciliation should run when no marker exists"
fi
pass "mise tool reconciliation runs when no marker exists"

HOME="$mise_tools_home" XDG_STATE_HOME="$mise_tools_state" sh -c \
  '. "$1"; terrapod_install_warning_write mise-tools "mise tool install needs attention" "Previous mise warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"
: >"$mise_tools_log"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  TERRAPOD_MACHINE_ARCH=aarch64 \
  MISE_TOOLS_INSTALL_STATUS=17 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script"
if [ ! -f "$mise_tools_marker" ]; then
  fail "mise tool reconciliation should keep a warning marker when apply still fails"
fi
pass "mise tool reconciliation keeps a warning marker when apply still fails"
mise_tools_marker_text="$(cat "$mise_tools_marker")"
assert_contains "$mise_tools_marker_text" "Failed step(s): mise install" "mise tool reconciliation replacement marker records failed mise install step"

: >"$mise_tools_log"
HOME="$mise_tools_home" \
  XDG_STATE_HOME="$mise_tools_state" \
  MISE_TOOLS_LOG="$mise_tools_log" \
  TERRAPOD_MACHINE_ARCH=aarch64 \
  PATH="$mise_tools_bin:/usr/bin:/bin" \
  sh "$mise_tools_installer_script"
if [ -e "$mise_tools_marker" ]; then
  fail "mise tool reconciliation should clear warning marker after recovery"
fi
pass "mise tool reconciliation clears warning marker after recovery"
