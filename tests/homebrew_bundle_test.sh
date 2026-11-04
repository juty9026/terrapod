#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
. "$repo_root/dot_local/lib/terrapod/config-toml.sh"
. "$repo_root/dot_local/lib/terrapod/homebrew-bundle.sh"
make_tmp_dir

mkdir -p "$tmp_dir/bin" "$tmp_dir/scratch"
write_brew_bundle_stub "$tmp_dir/bin/brew"
export MACOS_BREW_LOG="$tmp_dir/brew.log"
export TMPDIR="$tmp_dir/scratch"
brewfile="$tmp_dir/Brewfile"
cat >"$brewfile" <<'BREWFILE'
# terrapod-macos-app-group: enableMacosAppGroupMobileDev
brew "example/tap/tool", trusted: true
cask "studio"
BREWFILE

status=0
MACOS_BREW_FAIL_FORMULAE=example/tap/tool MACOS_BREW_FAIL_CASKS=studio \
  terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$brewfile" arm64 || status=$?
assert_status "$status" 1 "a failed bundle returns failure to its caller"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" \
  'failed formulae: example/tap/tool; failed casks: studio; App Groups: mobile-dev' \
  "the bundle step attributes mixed failures and deduplicates their group"
assert_equals "$TMPDIR" "$tmp_dir/scratch" "the bundle step preserves its caller's TMPDIR"
assert_equals "$(ls -A "$TMPDIR")" "" "the bundle step owns and removes its scratch files"

unset MACOS_BREW_FAIL_FORMULAE MACOS_BREW_FAIL_CASKS
terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$brewfile" arm64 || fail "a successful bundle succeeds"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" "" "success clears previous failure attribution"
assert_equals "$(ls -A "$TMPDIR")" "" "success removes internal scratch files"

# A single failed declaration still carries its original options. The log is
# the Brewfile consumed by the external package manager, not internal records.
write_stub "$tmp_dir/bin/options-brew" \
  'for arg do case "$arg" in --file=*) file="${arg#--file=}" ;; esac; done' \
  'cat "$file" >>"$OPTIONS_LOG"' \
  'printf "%s\n" "$HOMEBREW_NO_AUTO_UPDATE $*" >>"$OPTIONS_ARGS_LOG"' \
  'exit 42'
printf '%s\n' 'brew "example/tap/tool", trusted: true' >"$brewfile"
export OPTIONS_LOG="$tmp_dir/options.log" OPTIONS_ARGS_LOG="$tmp_dir/options-args.log"
status=0
terrapod_homebrew_bundle_step "$tmp_dir/bin/options-brew" "$brewfile" arm64 || status=$?
assert_status "$status" 1 "a failed formula returns failure"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" 'failed formulae: example/tap/tool' "a failed formula without a group has no invented group"
assert_equals "$(cat "$OPTIONS_LOG")" "$(printf '%s\n' 'brew "example/tap/tool", trusted: true' 'brew "example/tap/tool", trusted: true')" "bulk and retry preserve declaration options"
assert_equals "$(grep -c '^1 bundle --no-upgrade --file=' "$OPTIONS_ARGS_LOG")" 2 "bulk and retry disable auto-update and upgrades"

cat >"$brewfile" <<'BREWFILE'
cask "ghostty"
# terrapod-macos-app-group: enableMacosAppGroupLauncher
cask "raycast"
BREWFILE
export MACOS_BREW_FAIL_CASKS=ghostty MACOS_BREW_DRAIN_STDIN=1
status=0
terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$brewfile" arm64 </dev/null || status=$?
assert_status "$status" 1 "a failed headerless cask returns failure"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" 'failed casks: ghostty' "only observed failures contribute names and groups"
export MACOS_BREW_FAIL_CASKS='ghostty raycast'
terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$brewfile" arm64 </dev/null && fail "stdin-draining brew must still fail"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" 'failed casks: ghostty, raycast; App Groups: launcher' "stdin-draining brew cannot hide later failures"
unset MACOS_BREW_FAIL_CASKS MACOS_BREW_DRAIN_STDIN

# An unavailable group never inherits the preceding group's display name, nor
# does a real setting from another kind become a macOS App Group.
for unavailable_key in enableMissingGroup enableEditorStack ''; do
  cat >"$brewfile" <<BREWFILE
# terrapod-macos-app-group: enableMacosAppGroupLauncher
cask "raycast"
# terrapod-macos-app-group: $unavailable_key
cask "studio"
BREWFILE
  MACOS_BREW_FAIL_CASKS=studio \
    terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$brewfile" arm64 &&
    fail "an unresolvable group must preserve the observed failure"
  assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" 'failed casks: studio' \
    "unresolvable group data ($unavailable_key) preserves item attribution without an invented name"
done

# Consumers without the schema still get reliable item-level attribution.
missing_schema_detail="$(
  MACOS_BREW_FAIL_CASKS=raycast /bin/sh -c '
    . "$1"
    terrapod_homebrew_bundle_step "$2" "$3" arm64 && exit 99
    printf "%s\n" "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL"
  ' sh "$repo_root/dot_local/lib/terrapod/homebrew-bundle.sh" "$tmp_dir/bin/brew" "$brewfile"
)"
assert_equals "$missing_schema_detail" 'failed casks: raycast' \
  "an unavailable schema preserves reliable failures without group names"

# The fake fails an explicitly chosen invocation, independent of package names
# or Brewfile comments. Retries then succeed, but the bulk outcome stays failed.
for fixture in with-items without-items; do
  case "$fixture" in
    with-items) printf '%s\n' 'brew "tool"' >"$brewfile" ;;
    without-items) printf '%s\n' 'tap "example/tap"' >"$brewfile" ;;
  esac
  : >"$MACOS_BREW_LOG"
  export MACOS_BREW_FAIL_BUNDLE_CALLS=1
  status=0
  terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$brewfile" arm64 || status=$?
  assert_status "$status" 1 "$fixture bulk failure remains a failure"
  assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" 'Homebrew bundle' "$fixture bulk failure uses the common unattributed fallback"
  assert_equals "$(ls -A "$TMPDIR")" "" "$fixture bulk failure removes internal files"
done
unset MACOS_BREW_FAIL_BUNDLE_CALLS

status=0
terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$tmp_dir/missing" arm64 2>"$tmp_dir/setup-error" || status=$?
assert_status "$status" 1 "unreadable input remains a failure"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" 'Homebrew bundle' "input setup failure provides usable fallback detail"
assert_equals "$(ls -A "$TMPDIR")" "" "input setup failure removes scratch files"

saved_tmpdir="$TMPDIR"
export TMPDIR="$tmp_dir/missing-directory"
status=0
terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$brewfile" arm64 2>"$tmp_dir/temp-error" || status=$?
assert_status "$status" 1 "unavailable temporary storage remains a failure"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" 'Homebrew bundle' "temporary storage failure provides fallback detail"
export TMPDIR="$saved_tmpdir"

# Failure to process detector records must never become a successful install.
write_stub "$tmp_dir/bin/awk" 'exit 42'
saved_path="$PATH"
export PATH="$tmp_dir/bin:$PATH"
printf '%s\n' 'cask "studio"' >"$brewfile"
export MACOS_BREW_FAIL_CASKS=studio
status=0
terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$brewfile" arm64 || status=$?
assert_status "$status" 1 "record processing errors remain failures"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" 'Homebrew bundle' "record processing errors retain recovery detail"
assert_equals "$(ls -A "$TMPDIR")" "" "record processing errors remove internal files"
rm "$tmp_dir/bin/awk"
unset MACOS_BREW_FAIL_CASKS

cat >"$brewfile" <<'BREWFILE'
# terrapod-macos-app-group: enableMacosAppGroupAiApps
cask "claude"
cask "chatgpt"
cask "google-gemini"
BREWFILE
original_brewfile="$(cat "$brewfile")"
write_stub "$tmp_dir/bin/sw_vers" 'printf "%s\n" 15.0'
terrapod_homebrew_bundle_step "$tmp_dir/bin/brew" "$brewfile" x86_64 >"$tmp_dir/exclusion-output" || fail "Gemini exclusion alone succeeds"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" "" "inapplicable Gemini alone creates no failure"
assert_contains "$(cat "$tmp_dir/exclusion-output")" 'Skipping google-gemini: not applicable' "the shared step reports Gemini exclusion"
assert_equals "$(cat "$brewfile")" "$original_brewfile" "applicability preserves the caller's source Brewfile"
assert_equals "$(ls -A "$TMPDIR")" "" "applicability removes internal files"
export PATH="$saved_path"
