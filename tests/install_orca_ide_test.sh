#!/bin/sh
# Behavior of the rendered Orca installer. Each case runs the real rendered
# chezmoi script under an isolated home, state directory and temporary
# directory, with the machine architecture, dpkg, the network, sudo and APT
# stubbed. Nothing here downloads, installs or removes a real package.
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"

installer_template=".chezmoiscripts/run_after_85-install-orca-ide.sh.tmpl"
installer_script="$tmp_dir/installer.sh"
render_template "$ubuntu_data" "$installer_template" >"$installer_script"
sh -n "$installer_script" || fail "rendered Orca installer is valid sh"
pass "rendered Orca installer is valid sh"

assert_equals "$(render_template "$macos_data" "$installer_template")" "" \
  "the macOS Terminal Profile renders no Orca installer"

release_url="https://github.com/stablyai/orca/releases/latest/download"
version="1.4.274"
arm64_deb="orca-ide_${version}_arm64.deb"
amd64_deb="orca-ide_${version}_amd64.deb"

# The base64 sha512 digest an electron-updater manifest carries for a file.
manifest_digest() {
  python3 -I -c 'import base64, hashlib, sys; print(base64.b64encode(hashlib.sha512(open(sys.argv[1], "rb").read()).digest()).decode())' "$1"
}

test_sh="$(command -v sh)"
stub_bin="$tmp_dir/stub-bin"
write_stub "$stub_bin/uname" \
  'printf "%s\n" "${ORCA_STUB_MACHINE:-aarch64}"'
write_stub "$stub_bin/id" \
  'printf "%s\n" "${ORCA_STUB_UID:-1000}"'
write_stub "$stub_bin/dpkg-query" \
  'printf "%s\n" "dpkg-query $*" >>"$ORCA_STUB_LOG"' \
  '[ -n "${ORCA_STUB_DPKG_STATUS:-}" ] || exit 1' \
  'printf "%s\n" "$ORCA_STUB_DPKG_STATUS"'
write_stub "$stub_bin/curl" \
  'printf "%s\n" "curl $*" >>"$ORCA_STUB_LOG"' \
  'while [ "$#" -gt 0 ]; do' \
  '  case "$1" in' \
  '    -o) output="$2"; shift 2 ;;' \
  '    --retry) shift 2 ;;' \
  '    -*) shift ;;' \
  '    *) url="$1"; shift ;;' \
  '  esac' \
  'done' \
  'source_file="$ORCA_SERVE_DIR/${url##*/}"' \
  '[ -f "$source_file" ] || exit 22' \
  'cp "$source_file" "$output"'
write_stub "$stub_bin/sudo" \
  'printf "%s\n" "sudo $*" >>"$ORCA_STUB_LOG"' \
  'exec "$@"'
write_stub "$stub_bin/apt-get" \
  'printf "%s\n" "APT $* (DEBIAN_FRONTEND=${DEBIAN_FRONTEND:-unset})" >>"$ORCA_STUB_LOG"' \
  'case "$1" in' \
  '  update) [ "${ORCA_APT_FAIL:-}" != update ] || exit 100 ;;' \
  '  install)' \
  '    for deb_arg do :; done' \
  '    printf "%s\n" "APT-DEB $(cat "$deb_arg")" >>"$ORCA_STUB_LOG"' \
  '    [ "${ORCA_APT_FAIL:-}" != install ] || exit 100' \
  '    ;;' \
  '  *) exit 64 ;;' \
  'esac'
# Linux ships sha512sum in coreutils. Where the host lacks it, wrap the
# equivalent tool so the script still hashes a real file.
if ! PATH=/usr/bin:/bin command -v sha512sum >/dev/null 2>&1; then
  write_stub "$stub_bin/sha512sum" "exec $(command -v shasum) -a 512 \"\$@\""
fi

# A PATH that holds every stub and the commands the installer needs, but no sudo.
nosudo_bin="$tmp_dir/nosudo-bin"
mkdir -p "$nosudo_bin"
for stub_path in "$stub_bin"/*; do
  [ "${stub_path##*/}" = sudo ] || ln -s "$stub_path" "$nosudo_bin/${stub_path##*/}"
done
write_restricted_path "$tmp_dir/nosudo-tools" awk base64 cat chmod cp date dirname env grep mkdir mktemp mv od rm sed tr
for tool_path in "$tmp_dir/nosudo-tools"/*; do
  [ -e "$nosudo_bin/${tool_path##*/}" ] || ln -s "$tool_path" "$nosudo_bin/${tool_path##*/}"
done

# Writes a release manifest in the electron-updater shape: the AppImage and the
# rpm carry other digests so only the .deb entry yields the right one.
write_manifest() {
  manifest_path="$1"
  deb_url="$2"
  deb_digest="$3"
  printf '%s\n' \
    "version: $version" \
    'files:' \
    '  - url: orca-linux.AppImage' \
    '    sha512: AppImageDigestAppImageDigestAppImageDigestAppImageDigestAppImageDigestAppImageDigest==' \
    '    size: 280599071' \
    '    blockMapSize: 294931' \
    "  - url: $deb_url" \
    "    sha512: $deb_digest" \
    '    size: 211759148' \
    "  - url: orca-ide-$version.aarch64.rpm" \
    '    sha512: RpmDigestRpmDigestRpmDigestRpmDigestRpmDigestRpmDigestRpmDigestRpmDigestRpmDigestRpm==' \
    '    size: 179842161' \
    'path: orca-linux.AppImage' \
    'sha512: AppImageDigestAppImageDigestAppImageDigestAppImageDigestAppImageDigestAppImageDigest==' \
    "releaseDate: '2026-11-24T22:39:02.883Z'" \
    >"$manifest_path"
}

new_case() {
  case_root="$tmp_dir/$1"
  case_home="$case_root/home"
  case_state="$case_root/state"
  case_tmp="$case_root/tmp"
  case_log="$case_root/calls.log"
  case_serve="$case_root/serve"
  case_marker="$case_state/terrapod/install-warnings/orca-ide"
  mkdir -p "$case_home" "$case_state" "$case_tmp" "$case_serve"
  : >"$case_log"

  printf '%s\n' "fake arm64 package" >"$case_serve/$arm64_deb"
  printf '%s\n' "fake amd64 package" >"$case_serve/$amd64_deb"
  write_manifest "$case_serve/latest-linux-arm64.yml" "$arm64_deb" "$(manifest_digest "$case_serve/$arm64_deb")"
  write_manifest "$case_serve/latest-linux.yml" "$amd64_deb" "$(manifest_digest "$case_serve/$amd64_deb")"
}

# Runs the rendered script for the current case. Extra arguments are
# environment assignments, such as a stub failure switch.
run_script() {
  run_status=0
  env HOME="$case_home" XDG_STATE_HOME="$case_state" TMPDIR="$case_tmp" \
    PATH="$stub_bin:/usr/bin:/bin" ORCA_STUB_LOG="$case_log" ORCA_SERVE_DIR="$case_serve" \
    "$@" "$test_sh" "$installer_script" >"$case_root/output" 2>&1 || run_status=$?
  case_calls="$(sed "s#$case_tmp/terrapod-orca-ide\\.[A-Za-z0-9]*#<work>#g" "$case_log")"
}

write_marker() {
  env HOME="$case_home" XDG_STATE_HOME="$case_state" sh -c \
    '. "$1"; terrapod_install_warning_write "$2" "Install needs attention" "Rerun tpod apply."' \
    sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh" "$1"
}

assert_path_absent() {
  [ ! -e "$1" ] || fail "$2"
  pass "$2"
}

call_line() {
  printf '%s\n' "$case_calls" | grep -n -F -- "$1" | head -n 1 | cut -d: -f1
}

# --- Fresh installation ---------------------------------------------------------

new_case fresh
run_script
assert_status "$run_status" 0 "a missing Orca installs and exits 0"
assert_contains "$case_calls" "curl -fsSL --retry 3 $release_url/latest-linux-arm64.yml" \
  "an arm64 machine reads the arm64 release manifest"
assert_contains "$case_calls" "curl -fsSL --retry 3 $release_url/$arm64_deb" \
  "an arm64 machine downloads the arm64 package the manifest names"
assert_not_contains "$case_calls" "latest-linux.yml" "an arm64 machine ignores the x64 manifest"
assert_not_contains "$case_calls" "$amd64_deb" "an arm64 machine does not download the amd64 package"
assert_contains "$case_calls" "APT install -y <work>/$arm64_deb (DEBIAN_FRONTEND=noninteractive)" \
  "APT installs the downloaded package by absolute path without prompting"
assert_contains "$case_calls" "APT-DEB fake arm64 package" "APT installs the package that was downloaded"
assert_contains "$case_calls" "sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y" \
  "a non-root install runs APT under sudo and keeps the non-interactive frontend"
[ "$(call_line "APT update")" -lt "$(call_line "APT install")" ] ||
  fail "APT refreshes its package indexes before installing"
pass "APT refreshes its package indexes before installing"
assert_path_absent "$case_marker" "a successful install records no warning"
assert_equals "$(ls -A "$case_tmp")" "" "a successful install leaves no temporary files"

new_case arm64_spelling
run_script ORCA_STUB_MACHINE=arm64
assert_contains "$case_calls" "$release_url/latest-linux-arm64.yml" "a machine reporting arm64 reads the arm64 manifest"

new_case x86_64
run_script ORCA_STUB_MACHINE=x86_64
assert_status "$run_status" 0 "an x86_64 install exits 0"
assert_contains "$case_calls" "curl -fsSL --retry 3 $release_url/latest-linux.yml" \
  "an x86_64 machine reads the x64 release manifest"
assert_contains "$case_calls" "$release_url/$amd64_deb" "an x86_64 machine downloads the amd64 package"
assert_not_contains "$case_calls" "latest-linux-arm64.yml" "an x86_64 machine ignores the arm64 manifest"
assert_contains "$case_calls" "APT-DEB fake amd64 package" "an x86_64 machine installs the amd64 package"

new_case root_user
run_script ORCA_STUB_UID=0
assert_status "$run_status" 0 "a root install exits 0"
assert_not_contains "$case_calls" "sudo " "a root install does not call sudo"
assert_contains "$case_calls" "APT install -y <work>/$arm64_deb (DEBIAN_FRONTEND=noninteractive)" \
  "a root install still installs without prompting"

new_case removed_with_config
run_script ORCA_STUB_DPKG_STATUS=config-files
assert_contains "$case_calls" "APT-DEB fake arm64 package" "a removed package that kept its configuration is installed again"

# --- Existing installations -----------------------------------------------------

new_case installed
write_marker orca-ide
run_script ORCA_STUB_DPKG_STATUS=installed
assert_status "$run_status" 0 "an installed Orca exits 0"
assert_not_contains "$case_calls" "curl" "an installed Orca does no release lookup"
assert_not_contains "$case_calls" "APT" "an installed Orca runs no APT command"
assert_not_contains "$case_calls" "sudo" "an installed Orca never asks for sudo"
assert_path_absent "$case_marker" "an installed Orca clears an obsolete warning"

new_case installed_other_arch
run_script ORCA_STUB_DPKG_STATUS=installed ORCA_STUB_MACHINE=riscv64
assert_status "$run_status" 0 "an installed Orca needs no supported architecture"
assert_path_absent "$case_marker" "an installed Orca on an unknown architecture records no warning"

# --- Failures -------------------------------------------------------------------

# A recorded failure keeps the apply going, names Orca and the failed step, and
# leaves nothing behind.
assert_recorded_failure() {
  label="$1"
  step="$2"
  assert_status "$run_status" 0 "$label does not fail the apply"
  assert_file_contains "$case_marker" "Orca ($step)" "$label records a warning naming the failed step"
  assert_file_contains "$case_marker" "rerun tpod apply" "$label tells the user how to retry"
  assert_equals "$(ls -A "$case_tmp")" "" "$label leaves no temporary files"
}

new_case unsupported_architecture
run_script ORCA_STUB_MACHINE=riscv64
assert_recorded_failure "an unsupported architecture" "unsupported architecture"
assert_not_contains "$case_calls" "curl" "an unsupported architecture downloads nothing"

new_case missing_sudo
run_script PATH="$nosudo_bin"
assert_recorded_failure "a missing sudo" "sudo is required; install sudo or run as root"
assert_not_contains "$case_calls" "curl" "a missing sudo downloads nothing"

new_case manifest_unreachable
rm "$case_serve/latest-linux-arm64.yml"
run_script
assert_recorded_failure "an unreachable manifest" "release manifest download"
assert_not_contains "$case_calls" "$arm64_deb" "an unreachable manifest downloads no package"
assert_not_contains "$case_calls" "APT" "an unreachable manifest runs no APT command"

new_case manifest_without_deb
printf '%s\n' "version: $version" 'files:' '  - url: orca-linux.AppImage' '    sha512: AAAA==' 'path: orca-linux.AppImage' \
  >"$case_serve/latest-linux-arm64.yml"
run_script
assert_recorded_failure "a manifest without a .deb" "release manifest"
assert_not_contains "$case_calls" "APT" "a manifest without a .deb runs no APT command"

new_case manifest_with_path
write_manifest "$case_serve/latest-linux-arm64.yml" "../orca-ide_${version}_arm64.deb" "AAAA=="
run_script
assert_recorded_failure "a package name holding a path" "release manifest"
assert_not_contains "$case_calls" "orca-ide_" "a package name holding a path downloads nothing"

new_case manifest_other_arch
write_manifest "$case_serve/latest-linux-arm64.yml" "$amd64_deb" "$(manifest_digest "$case_serve/$amd64_deb")"
run_script
assert_recorded_failure "a package for another architecture" "release manifest"
assert_not_contains "$case_calls" "APT" "a package for another architecture runs no APT command"

new_case package_unreachable
rm "$case_serve/$arm64_deb"
run_script
assert_recorded_failure "an unreachable package" "download"
assert_not_contains "$case_calls" "APT" "an unreachable package runs no APT command"

new_case checksum_mismatch
printf '%s\n' "tampered package" >"$case_serve/$arm64_deb"
run_script
assert_recorded_failure "a checksum mismatch" "checksum"
assert_not_contains "$case_calls" "APT" "a checksum mismatch runs no APT command"
assert_not_contains "$case_calls" "sudo" "a checksum mismatch never asks for sudo"

new_case unreadable_digest
write_manifest "$case_serve/latest-linux-arm64.yml" "$arm64_deb" "not*base64"
run_script
assert_recorded_failure "an unreadable manifest digest" "checksum"
assert_not_contains "$case_calls" "APT" "an unreadable manifest digest runs no APT command"

new_case apt_update_failure
run_script ORCA_APT_FAIL=update
assert_recorded_failure "an APT update failure" "apt update"
assert_not_contains "$case_calls" "APT install" "an APT update failure installs nothing"

new_case apt_install_failure
run_script ORCA_APT_FAIL=install
assert_recorded_failure "an APT install failure" "apt install"

new_case warning_write_failure
: >"$case_root/not-a-directory"
case_state="$case_root/not-a-directory/state"
run_script ORCA_STUB_MACHINE=riscv64
assert_status "$run_status" 1 "a failure that cannot be recorded fails the apply"

# --- Recovery -------------------------------------------------------------------

new_case retry
run_script ORCA_APT_FAIL=install
assert_file_exists "$case_marker" "a failed apply keeps the warning visible"
: >"$case_log"
run_script
assert_status "$run_status" 0 "a retry after a failure exits 0"
assert_contains "$case_calls" "APT-DEB fake arm64 package" "a later apply retries the missing Orca"
assert_path_absent "$case_marker" "a successful retry clears the warning"

new_case unrelated_warning
write_marker jetendard-font
run_script
assert_file_exists "$case_state/terrapod/install-warnings/jetendard-font" \
  "a successful install leaves other warning categories alone"
