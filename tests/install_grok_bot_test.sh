#!/bin/sh
# Behavior of the rendered Grok Bot installer. Each case runs the real rendered
# chezmoi script under an isolated home, state directory and temporary
# directory, with the machine architecture, dpkg, the network, sudo and APT
# stubbed. Nothing here downloads, installs or removes a real package.
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"

installer_template=".chezmoiscripts/run_after_86-install-grok-bot.sh.tmpl"
installer_script="$tmp_dir/installer.sh"
render_template "$ubuntu_data" "$installer_template" >"$installer_script"
sh -n "$installer_script" || fail "rendered Grok Bot installer is valid sh"
pass "rendered Grok Bot installer is valid sh"

assert_equals "$(render_template "$macos_data" "$installer_template")" "" \
  "the macOS Terminal Profile renders no Grok Bot installer"

repository_url="https://downloads.cursor.com/aptrepo"
version="1.3.0"
arm64_pool="pool/grok-bot/g/gr/grok-bot_${version}_arm64.deb"
amd64_pool="pool/grok-bot/g/gr/grok-bot_${version}_amd64.deb"

file_digest() {
  python3 -I -c 'import hashlib, sys; print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())' "$1"
}

test_sh="$(command -v sh)"
stub_bin="$tmp_dir/stub-bin"
write_stub "$stub_bin/uname" \
  'printf "%s\n" "${GROK_STUB_MACHINE:-aarch64}"'
write_stub "$stub_bin/id" \
  'printf "%s\n" "${GROK_STUB_UID:-1000}"'
write_stub "$stub_bin/dpkg-query" \
  'printf "%s\n" "dpkg-query $*" >>"$GROK_STUB_LOG"' \
  '[ -n "${GROK_STUB_DPKG_STATUS:-}" ] || exit 1' \
  'printf "%s\n" "$GROK_STUB_DPKG_STATUS"'
# Serves a file by the path below the repository root. Both architectures name
# their index Packages, so a stub that served by file name would hide a wrong
# index.
write_stub "$stub_bin/curl" \
  'printf "%s\n" "curl $*" >>"$GROK_STUB_LOG"' \
  'while [ "$#" -gt 0 ]; do' \
  '  case "$1" in' \
  '    -o) output="$2"; shift 2 ;;' \
  '    --retry) shift 2 ;;' \
  '    -*) shift ;;' \
  '    *) url="$1"; shift ;;' \
  '  esac' \
  'done' \
  'relative="${url#https://downloads.cursor.com/aptrepo/}"' \
  '[ "$relative" != "$url" ] || exit 22' \
  'source_file="$GROK_SERVE_DIR/$relative"' \
  '[ -f "$source_file" ] || exit 22' \
  'cp "$source_file" "$output"'
write_stub "$stub_bin/sudo" \
  'printf "%s\n" "sudo $*" >>"$GROK_STUB_LOG"' \
  'exec "$@"'
write_stub "$stub_bin/apt-get" \
  'printf "%s\n" "APT $* (DEBIAN_FRONTEND=${DEBIAN_FRONTEND:-unset})" >>"$GROK_STUB_LOG"' \
  'case "$1" in' \
  '  update) [ "${GROK_APT_FAIL:-}" != update ] || exit 100 ;;' \
  '  install)' \
  '    for deb_arg do :; done' \
  '    printf "%s\n" "APT-DEB $(cat "$deb_arg")" >>"$GROK_STUB_LOG"' \
  '    [ "${GROK_APT_FAIL:-}" != install ] || exit 100' \
  '    ;;' \
  '  *) exit 64 ;;' \
  'esac'
# Linux ships sha256sum in coreutils. Where the host lacks it, wrap the
# equivalent tool so the script still hashes a real file.
if ! PATH=/usr/bin:/bin command -v sha256sum >/dev/null 2>&1; then
  write_stub "$stub_bin/sha256sum" "exec $(command -v shasum) -a 256 \"\$@\""
fi

# A PATH that holds every stub and the commands the installer needs, but no sudo.
nosudo_bin="$tmp_dir/nosudo-bin"
mkdir -p "$nosudo_bin"
for stub_path in "$stub_bin"/*; do
  [ "${stub_path##*/}" = sudo ] || ln -s "$stub_path" "$nosudo_bin/${stub_path##*/}"
done
write_restricted_path "$tmp_dir/nosudo-tools" awk cat chmod cp date dirname env grep mkdir mktemp mv rm sed tr
for tool_path in "$tmp_dir/nosudo-tools"/*; do
  [ -e "$nosudo_bin/${tool_path##*/}" ] || ln -s "$tool_path" "$nosudo_bin/${tool_path##*/}"
done

# Writes a Packages index in the vendor's shape for one architecture. An
# unrelated stanza leads it, so only the grok-bot stanza may yield the answer.
write_index() {
  index_path="$1"
  index_arch="$2"
  index_filename="$3"
  index_digest="$4"
  mkdir -p "${index_path%/*}"
  printf '%s\n' \
    'Package: grok-bot-helper' \
    "Version: $version" \
    "Architecture: $index_arch" \
    "Filename: pool/grok-bot/g/gr/grok-bot-helper_${version}_${index_arch}.deb" \
    "SHA256: $(printf '%064d' 0)" \
    '' \
    'Package: grok-bot' \
    "Version: $version" \
    'License: unknown' \
    "Architecture: $index_arch" \
    'Depends: libgtk-3-0, libnotify4, libnss3' \
    'Section: default' \
    "Filename: $index_filename" \
    'Size: 114013936' \
    'SHA1: eab82d38607c74fff0f9b6f1195520c17c31ae30' \
    "SHA256: $index_digest" \
    'MD5sum: 4dd3e7b023b5d6e2948e8bae60a524c4' \
    'Description: Grok Bot desktop agent' \
    >"$index_path"
}

index_of() {
  printf '%s\n' "$case_serve/dists/grok-bot/main/binary-$1/Packages"
}

new_case() {
  case_root="$tmp_dir/$1"
  case_home="$case_root/home"
  case_state="$case_root/state"
  case_tmp="$case_root/tmp"
  case_log="$case_root/calls.log"
  case_serve="$case_root/serve"
  case_marker="$case_state/terrapod/install-warnings/grok-bot"
  mkdir -p "$case_home" "$case_state" "$case_tmp" "$case_serve/pool/grok-bot/g/gr"
  : >"$case_log"

  printf '%s\n' "fake arm64 package" >"$case_serve/$arm64_pool"
  printf '%s\n' "fake amd64 package" >"$case_serve/$amd64_pool"
  write_index "$(index_of arm64)" arm64 "$arm64_pool" "$(file_digest "$case_serve/$arm64_pool")"
  write_index "$(index_of amd64)" amd64 "$amd64_pool" "$(file_digest "$case_serve/$amd64_pool")"
}

# Runs the rendered script for the current case. Extra arguments are
# environment assignments, such as a stub failure switch.
run_script() {
  run_status=0
  env HOME="$case_home" XDG_STATE_HOME="$case_state" TMPDIR="$case_tmp" \
    PATH="$stub_bin:/usr/bin:/bin" GROK_STUB_LOG="$case_log" GROK_SERVE_DIR="$case_serve" \
    "$@" "$test_sh" "$installer_script" >"$case_root/output" 2>&1 || run_status=$?
  case_calls="$(sed "s#$case_tmp/terrapod-grok-bot\\.[A-Za-z0-9]*#<work>#g" "$case_log")"
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
assert_status "$run_status" 0 "a missing Grok Bot installs and exits 0"
assert_contains "$case_calls" "curl -fsSL --retry 3 $repository_url/dists/grok-bot/main/binary-arm64/Packages" \
  "an arm64 machine reads the arm64 package index"
assert_contains "$case_calls" "curl -fsSL --retry 3 $repository_url/$arm64_pool" \
  "an arm64 machine downloads the arm64 package the index names"
assert_not_contains "$case_calls" "binary-amd64" "an arm64 machine ignores the amd64 index"
assert_not_contains "$case_calls" "_amd64.deb" "an arm64 machine does not download the amd64 package"
assert_not_contains "$case_calls" "grok-bot-helper" "the helper stanza of the index is never downloaded"
assert_contains "$case_calls" "APT install -y <work>/grok-bot_${version}_arm64.deb (DEBIAN_FRONTEND=noninteractive)" \
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
run_script GROK_STUB_MACHINE=arm64
assert_contains "$case_calls" "binary-arm64/Packages" "a machine reporting arm64 reads the arm64 index"

new_case x86_64
run_script GROK_STUB_MACHINE=x86_64
assert_status "$run_status" 0 "an x86_64 install exits 0"
assert_contains "$case_calls" "curl -fsSL --retry 3 $repository_url/dists/grok-bot/main/binary-amd64/Packages" \
  "an x86_64 machine reads the amd64 package index"
assert_contains "$case_calls" "$repository_url/$amd64_pool" "an x86_64 machine downloads the amd64 package"
assert_not_contains "$case_calls" "binary-arm64" "an x86_64 machine ignores the arm64 index"
assert_contains "$case_calls" "APT-DEB fake amd64 package" "an x86_64 machine installs the amd64 package"

new_case root_user
run_script GROK_STUB_UID=0
assert_status "$run_status" 0 "a root install exits 0"
assert_not_contains "$case_calls" "sudo " "a root install does not call sudo"
assert_contains "$case_calls" "APT install -y <work>/grok-bot_${version}_arm64.deb (DEBIAN_FRONTEND=noninteractive)" \
  "a root install still installs without prompting"

new_case removed_with_config
run_script GROK_STUB_DPKG_STATUS=config-files
assert_contains "$case_calls" "APT-DEB fake arm64 package" "a removed package that kept its configuration is installed again"

# --- Existing installations -----------------------------------------------------

new_case installed
write_marker grok-bot
run_script GROK_STUB_DPKG_STATUS=installed
assert_status "$run_status" 0 "an installed Grok Bot exits 0"
assert_not_contains "$case_calls" "curl" "an installed Grok Bot does no index lookup"
assert_not_contains "$case_calls" "APT" "an installed Grok Bot runs no APT command"
assert_not_contains "$case_calls" "sudo" "an installed Grok Bot never asks for sudo"
assert_path_absent "$case_marker" "an installed Grok Bot clears an obsolete warning"
assert_equals "$(cat "$case_root/output")" "" "an installed Grok Bot prints nothing"

new_case installed_other_arch
run_script GROK_STUB_DPKG_STATUS=installed GROK_STUB_MACHINE=riscv64
assert_status "$run_status" 0 "an installed Grok Bot needs no supported architecture"
assert_path_absent "$case_marker" "an installed Grok Bot on an unknown architecture records no warning"

# --- Failures -------------------------------------------------------------------

# A recorded failure keeps the apply going, names Grok Bot and the failed step,
# and leaves nothing behind.
assert_recorded_failure() {
  label="$1"
  step="$2"
  assert_status "$run_status" 0 "$label does not fail the apply"
  assert_file_contains "$case_marker" "Grok Bot ($step)" "$label records a warning naming the failed step"
  assert_file_contains "$case_marker" "rerun tpod apply" "$label tells the user how to retry"
  assert_equals "$(ls -A "$case_tmp")" "" "$label leaves no temporary files"
}

new_case unsupported_architecture
run_script GROK_STUB_MACHINE=riscv64
assert_recorded_failure "an unsupported architecture" "unsupported architecture"
assert_not_contains "$case_calls" "curl" "an unsupported architecture downloads nothing"

new_case missing_sudo
run_script PATH="$nosudo_bin"
assert_recorded_failure "a missing sudo" "sudo is required; install sudo or run as root"
assert_not_contains "$case_calls" "curl" "a missing sudo downloads nothing"

new_case index_unreachable
rm "$(index_of arm64)"
run_script
assert_recorded_failure "an unreachable package index" "package index download"
assert_not_contains "$case_calls" "_arm64.deb" "an unreachable index downloads no package"
assert_not_contains "$case_calls" "APT" "an unreachable index runs no APT command"

new_case index_without_grok_bot
printf '%s\n' 'Package: other' 'Architecture: arm64' "Filename: $arm64_pool" "SHA256: $(printf '%064d' 0)" \
  >"$(index_of arm64)"
run_script
assert_recorded_failure "an index without a grok-bot stanza" "package index"
assert_not_contains "$case_calls" "APT" "an index without a grok-bot stanza runs no APT command"

new_case index_other_arch
write_index "$(index_of arm64)" amd64 "$amd64_pool" "$(file_digest "$case_serve/$amd64_pool")"
run_script
assert_recorded_failure "a stanza for another architecture" "package index"
assert_not_contains "$case_calls" "APT" "a stanza for another architecture runs no APT command"

# Each rejected Filename would otherwise reach a URL or a path.
bad_filename_case() {
  case_name="$1"
  bad_filename="$2"
  new_case "$case_name"
  write_index "$(index_of arm64)" arm64 "$bad_filename" "$(file_digest "$case_serve/$arm64_pool")"
  run_script
  assert_recorded_failure "a package path $case_name" "package index"
  assert_not_contains "$case_calls" "$repository_url/pool" "a package path $case_name downloads nothing"
  assert_not_contains "$case_calls" "APT" "a package path $case_name runs no APT command"
}

bad_filename_case with_parent_directory "pool/grok-bot/../../grok-bot_${version}_arm64.deb"
bad_filename_case outside_the_pool "dists/grok-bot_${version}_arm64.deb"
bad_filename_case absolute "/pool/grok-bot/g/gr/grok-bot_${version}_arm64.deb"
bad_filename_case for_another_package "pool/grok-bot/g/gr/other_${version}_arm64.deb"
bad_filename_case for_another_architecture "$amd64_pool"
bad_filename_case with_a_query "pool/grok-bot/g/gr/grok-bot_${version}_arm64.deb?x=1"

new_case unreadable_digest
write_index "$(index_of arm64)" arm64 "$arm64_pool" "XYZ"
run_script
assert_recorded_failure "an unreadable index digest" "package index"
assert_not_contains "$case_calls" "$repository_url/pool" "an unreadable index digest downloads no package"

new_case package_unreachable
rm "$case_serve/$arm64_pool"
run_script
assert_recorded_failure "an unreachable package" "download"
assert_not_contains "$case_calls" "APT" "an unreachable package runs no APT command"

new_case checksum_mismatch
printf '%s\n' "tampered package" >"$case_serve/$arm64_pool"
run_script
assert_recorded_failure "a checksum mismatch" "checksum"
assert_not_contains "$case_calls" "APT" "a checksum mismatch runs no APT command"
assert_not_contains "$case_calls" "sudo" "a checksum mismatch never asks for sudo"

new_case apt_update_failure
run_script GROK_APT_FAIL=update
assert_recorded_failure "an APT update failure" "apt update"
assert_not_contains "$case_calls" "APT install" "an APT update failure installs nothing"

new_case apt_install_failure
run_script GROK_APT_FAIL=install
assert_recorded_failure "an APT install failure" "apt install"

new_case warning_write_failure
: >"$case_root/not-a-directory"
case_state="$case_root/not-a-directory/state"
run_script GROK_STUB_MACHINE=riscv64
assert_status "$run_status" 1 "a failure that cannot be recorded fails the apply"

# --- Recovery -------------------------------------------------------------------

new_case retry
run_script GROK_APT_FAIL=install
assert_file_exists "$case_marker" "a failed apply keeps the warning visible"
: >"$case_log"
run_script
assert_status "$run_status" 0 "a retry after a failure exits 0"
assert_contains "$case_calls" "APT-DEB fake arm64 package" "a later apply retries the missing Grok Bot"
assert_path_absent "$case_marker" "a successful retry clears the warning"

new_case unrelated_warning
write_marker jetendard-font
run_script
assert_file_exists "$case_state/terrapod/install-warnings/jetendard-font" \
  "a successful install leaves other warning categories alone"
