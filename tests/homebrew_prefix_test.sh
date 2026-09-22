#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

paths_lib="$repo_root/dot_local/lib/terrapod/homebrew-paths.sh"
prefix_lib="$repo_root/dot_local/lib/terrapod/homebrew-prefix.sh"

standard_path() {
  os="$1"
  arch="$2"
  translated="$3"
  tool="$4"

  TERRAPOD_MACHINE_ARCH="$arch" TERRAPOD_DARWIN_TRANSLATED="$translated" \
    sh -c '. "$1"; . "$2"; "terrapod_standard_homebrew_$3_path" "$4"' \
    sh "$paths_lib" "$prefix_lib" "$tool" "$os"
}

assert_equals "$(standard_path darwin arm64 0 brew)" /opt/homebrew/bin/brew \
  "Apple Silicon derives brew from the standard prefix"
assert_equals "$(standard_path darwin x86_64 0 mise)" /usr/local/bin/mise \
  "Intel Mac derives mise from the standard prefix"
assert_equals "$(standard_path darwin x86_64 1 gh)" /opt/homebrew/bin/gh \
  "Rosetta derives gh from the Apple Silicon standard prefix"
assert_equals "$(standard_path linux x86_64 0 brew)" /home/linuxbrew/.linuxbrew/bin/brew \
  "Linux x86_64 derives brew from the standard prefix"
assert_equals "$(standard_path linux aarch64 0 mise)" /home/linuxbrew/.linuxbrew/bin/mise \
  "Linux aarch64 derives mise from the standard prefix"

set +e
unsupported_output="$(standard_path linux arm64 0 brew 2>/dev/null)"
unsupported_status=$?
set -e
assert_status "$unsupported_status" 1 "unsupported Linux architecture fails prefix resolution"
assert_equals "$unsupported_output" "" "unsupported Linux architecture returns no tool path"

set +e
unsupported_os_output="$(standard_path freebsd x86_64 0 gh 2>/dev/null)"
unsupported_os_status=$?
set -e
assert_status "$unsupported_os_status" 1 "unsupported operating system fails prefix resolution"
assert_equals "$unsupported_os_output" "" "unsupported operating system returns no tool path"

fake_provider="$tmp_dir/homebrew-prefix.sh"
printf '%s\n' \
  '#!/bin/sh' \
  'TERRAPOD_HOMEBREW_PREFIX_LOADED=1' \
  'terrapod_standard_homebrew_prefix_for_os() {' \
  "  printf '%s\\n' '$tmp_dir/prefix'" \
  '}' \
  >"$fake_provider"

fake_path="$(sh -c '. "$1"; . "$2"; terrapod_standard_homebrew_gh_path linux' sh "$paths_lib" "$fake_provider")"
assert_equals "$fake_path" "$tmp_dir/prefix/bin/gh" \
  "a test prefix provider reuses the production gh path derivation"
