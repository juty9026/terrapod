#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"

# Change only the schema's display name in a source fixture. Both status and
# the rendered installer must follow it, even before managed libraries deploy.
fixture_source="$tmp_dir/source"
mkdir -p "$fixture_source"
cp -R "$repo_root/." "$fixture_source"
fixture_reader="$fixture_source/dot_local/lib/terrapod/config-toml.sh"
sed 's/|macos-app-group|mobile-dev|/|macos-app-group|mobile-dev-renamed|/' \
  "$fixture_reader" >"$tmp_dir/reader"
mv "$tmp_dir/reader" "$fixture_reader"

fixture_home="$tmp_dir/home"
fixture_config="$tmp_dir/config.toml"
fixture_prefix="$tmp_dir/prefix"
mkdir -p "$fixture_home" "$fixture_prefix/bin"
write_brew_bundle_stub "$fixture_prefix/bin/brew"
write_stub "$fixture_source/dot_local/lib/terrapod/homebrew-prefix.sh" \
  'terrapod_standard_homebrew_prefix_for_os() {' \
  "  printf '%s\\n' '$fixture_prefix'" \
  '}'

TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$fixture_config" \
  HOME="$fixture_home" /bin/sh "$fixture_source/dot_local/bin/executable_terrapod" configure workstation >/dev/null
fixture_status="$(
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$fixture_config" \
    HOME="$fixture_home" PATH=/usr/bin:/bin \
    /bin/sh "$fixture_source/dot_local/bin/executable_terrapod" status 2>&1 || true
)"
assert_contains "$fixture_status" 'mobile-dev-renamed' "status follows a schema-only App Group rename"

fixture_script="$tmp_dir/reconcile.sh"
render_template_from_source "$macos_mobile_dev_data" \
  .chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl "$fixture_source" >"$fixture_script"
sh -n "$fixture_script" || fail "the renamed group's installer is valid sh"
HOME="$fixture_home" XDG_STATE_HOME="$tmp_dir/state" \
  MACOS_BREW_LOG="$tmp_dir/brew.log" MACOS_BREW_FAIL_FORMULAE=mobile-dev-inc/tap/maestro \
  PATH="$fixture_prefix/bin:/usr/bin:/bin" sh "$fixture_script" >"$tmp_dir/output" 2>&1 ||
  fail "an observed desktop failure records a warning and continues"
fixture_marker="$tmp_dir/state/terrapod/install-warnings/homebrew-desktop-apps"
assert_file_contains "$fixture_marker" 'failed formulae: mobile-dev-inc/tap/maestro' \
  "a schema-only rename preserves reliable formula attribution"
assert_file_contains "$fixture_marker" 'App Groups: mobile-dev-renamed' \
  "the desktop warning follows the same schema-only App Group rename as status"

. "$repo_root/dot_local/lib/terrapod/config-toml.sh"
. "$repo_root/dot_local/lib/terrapod/homebrew-bundle.sh"
write_stub "$tmp_dir/failing-brew" 'exit 42'

# Enable each setting alone through the actual template. This catches a block
# whose annotation disagrees with its gate, including missing identity data.
managed_setting_schema | while IFS='|' read -r key kind name rest; do
  [ "$kind" = macos-app-group ] || continue
  group_brewfile="$tmp_dir/$key.Brewfile"
  render_template "{\"chezmoi\":{\"os\":\"darwin\"},\"$key\":true}" \
    Brewfile.macos-desktop-apps.tmpl >"$group_brewfile"
  assert_file_contains "$group_brewfile" "# terrapod-macos-app-group: $key" \
    "$name carries the identity of its enabling setting"
  terrapod_homebrew_bundle_step "$tmp_dir/failing-brew" "$group_brewfile" arm64 >/dev/null &&
    fail "$name must report the observed failures"
  assert_contains "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" 'failed casks:' \
    "$name reports failures from its rendered declarations"
  assert_equals "${TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL##*; App Groups: }" "$name" \
    "$name resolves and deduplicates its schema name"
done

# Misleading prose next to real declarations must not become group data. Start
# with rendered input, then edit/remove only ordinary human-readable comments.
render_template "$macos_mobile_dev_data" Brewfile.macos-desktop-apps.tmpl >"$tmp_dir/rendered.Brewfile"
awk '
  /^(brew|cask) / { print "# moblie-dev macOS App Group" }
  { print }
' "$tmp_dir/rendered.Brewfile" >"$tmp_dir/edited.Brewfile"
sed '/^#/ { /terrapod-macos-app-group:/!d; }' "$tmp_dir/edited.Brewfile" >"$tmp_dir/removed.Brewfile"
for comments in edited removed; do
  MACOS_BREW_LOG="$tmp_dir/comments.log" MACOS_BREW_FAIL_FORMULAE=mobile-dev-inc/tap/maestro \
    terrapod_homebrew_bundle_step "$fixture_prefix/bin/brew" "$tmp_dir/$comments.Brewfile" arm64 &&
    fail "$comments comments must preserve the observed failure"
  assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" \
    'failed formulae: mobile-dev-inc/tap/maestro; App Groups: mobile-dev' \
    "$comments ordinary group comments cannot change attribution of rendered declarations"
done

render_template '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":true,"enableMacosAppGroupLauncher":true,"enableMacosAppGroupAutomation":true}' \
  Brewfile.macos-desktop-apps.tmpl >"$tmp_dir/multiple.Brewfile"
MACOS_BREW_LOG="$tmp_dir/multiple.log" MACOS_BREW_FAIL_CASKS='ghostty font-d2coding raycast istat-menus' \
  terrapod_homebrew_bundle_step "$fixture_prefix/bin/brew" "$tmp_dir/multiple.Brewfile" arm64 &&
  fail "multiple observed failures must return failure"
assert_equals "$TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL" \
  'failed casks: ghostty, font-d2coding, raycast; App Groups: terminal-apps, launcher' \
  "multiple failed groups are deduplicated while successful and disabled groups are absent"
