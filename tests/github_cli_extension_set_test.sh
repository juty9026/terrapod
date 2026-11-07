#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
. "$repo_root/tests/lib/managed-config-fixtures.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"

# Change an isolated declaration, then exercise both production entry points.
# Only the external gh command and Homebrew prefix provider are substituted.
source_dir="$tmp_dir/source"
mkdir -p "$source_dir"
cp -R "$repo_root/." "$source_dir"
. "$repo_root/dot_local/lib/terrapod/github-cli-extension-set.sh"
production_members="$(terrapod_github_cli_extension_set_members)"
declaration="$source_dir/dot_local/lib/terrapod/github-cli-extension-set.sh"
sed '/^GH_EXTENSIONS$/i\
fixture/gh-second
' "$declaration" >"$tmp_dir/declaration"
mv "$tmp_dir/declaration" "$declaration"
prefix="$tmp_dir/standard-homebrew"
mkdir -p "$prefix/bin" "$tmp_dir/bin" "$tmp_dir/home"
printf '%s\n' \
  '#!/bin/sh' \
  'TERRAPOD_HOMEBREW_PREFIX_LOADED=1' \
  'terrapod_standard_homebrew_prefix_for_os() {' \
  "  printf '%s\\n' '$prefix'" \
  '}' >"$source_dir/dot_local/lib/terrapod/homebrew-prefix.sh"

write_stub "$prefix/bin/gh" \
  'printf "standard:%s\n" "$*" >>"$GH_SET_LOG"' \
  'case "$1 $2" in' \
  '  "extension list") printf "%s\n" "$GH_SET_INSTALLED" ;;' \
  '  "extension install") [ "$3" != "${GH_SET_FAIL:-}" ] ;;' \
  '  *) exit 19 ;;' \
  'esac'
write_stub "$tmp_dir/bin/gh" \
  'printf "path:%s\n" "$*" >>"$GH_SET_LOG"' \
  '[ "$1 $2" = "extension list" ] || exit 19' \
  'printf "%s\n" "$GH_SET_INSTALLED"'
export GH_SET_LOG="$tmp_dir/gh.log"
initial_installed="$production_members
unmanaged/gh-extra
fixture/gh-second-suffix"
export GH_SET_INSTALLED="$initial_installed"
export HOME="$tmp_dir/home" XDG_STATE_HOME="$tmp_dir/state"
export PATH="$tmp_dir/bin:$PATH"

run_status() {
  TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG="$tmp_dir/config.toml" \
    sh "${status_command:-$source_dir/dot_local/bin/executable_terrapod}" status
}

for os in linux darwin; do
  case "$os" in
    linux) profile=vps-shell ;;
    darwin) profile=macos-terminal ;;
  esac
  render_complete_config "$profile" >"$tmp_dir/config.toml"
  GH_SET_INSTALLED="$initial_installed"
  installer="$tmp_dir/install-$os.sh"
  render_template_from_source "{\"chezmoi\":{\"os\":\"$os\"}}" \
    .chezmoiscripts/run_before_20-install-gh-extensions.sh.tmpl "$source_dir" >"$installer"
  sh -n "$installer"
  : >"$GH_SET_LOG"
  sh "$installer"
  assert_contains "$(cat "$GH_SET_LOG")" 'standard:extension install fixture/gh-second' \
    "$os installer sees the added declaration and rejects a partial repository match"
  assert_not_contains "$(cat "$GH_SET_LOG")" 'path:' "$os installer selects standard-prefix gh"
  assert_equals "$(sed -n 's/^standard:extension install //p' "$GH_SET_LOG")" fixture/gh-second \
    "$os installer preserves declared installed members and unmanaged extensions"

  : >"$GH_SET_LOG"
  output="$(run_status)"
  assert_contains "$output" 'GitHub CLI Extension Set      : missing (missing extensions: fixture/gh-second)' \
    "$os status sees the same added declaration and exact repository match"
  assert_contains "$(cat "$GH_SET_LOG")" 'path:extension list' "$os status selects invoking PATH gh"
  assert_not_contains "$(cat "$GH_SET_LOG")" 'standard:' "$os status does not select standard-prefix gh"

  # owner/repo can appear in any field, not only in a fixed output column.
  GH_SET_INSTALLED="$production_members
gh second ignored fixture/gh-second v1.0.0
unmanaged/gh-extra"
  : >"$GH_SET_LOG"
  sh "$installer"
  assert_not_contains "$(cat "$GH_SET_LOG")" 'extension install' \
    "$os installer recognizes an exact repository in a later field"
  output="$(run_status)"
  assert_contains "$output" "GitHub CLI Extension Set      : available (extensions available: $production_members, fixture/gh-second)" \
    "$os checkout status recognizes the same later-field repository"

  # Render the module through chezmoi to verify its deployed target, and run
  # the command with the same sibling lib/ layout as an installed machine.
  deployed="$tmp_dir/deployed-$os"
  mkdir -p "$deployed/.local/bin" "$deployed/.local/lib"
  cp -R "$source_dir/dot_local/lib/terrapod" "$deployed/.local/lib/terrapod"
  run_test_chezmoi "$source_dir" "$deployed" \
    --override-data "{\"chezmoi\":{\"os\":\"$os\"}}" \
    cat "$deployed/.local/lib/terrapod/github-cli-extension-set.sh" \
    >"$deployed/.local/lib/terrapod/github-cli-extension-set.sh"
  run_test_chezmoi "$source_dir" "$deployed" \
    --override-data "{\"chezmoi\":{\"os\":\"$os\"}}" \
    cat "$deployed/.local/bin/terrapod" >"$deployed/.local/bin/terrapod"
  status_command="$deployed/.local/bin/terrapod"
  output="$(run_status)"
  assert_contains "$output" "GitHub CLI Extension Set      : available (extensions available: $production_members, fixture/gh-second)" \
    "$os deployed status consumes the changed declaration"

  rm "$deployed/.local/lib/terrapod/github-cli-extension-set.sh"
  sh "$status_command" help >"$tmp_dir/help"
  assert_file_contains "$tmp_dir/help" 'Terrapod' "$os help works before the extension module arrives"
  output="$(run_status)"
  assert_contains "$output" 'extension checker is unavailable; run tpod apply' \
    "$os status explains a missing full-apply extension module"
  unset status_command
done

# With a third member after the failing second one, partial failure must not
# stop later attempts. Production members remain observed as installed.
sed '/^GH_EXTENSIONS$/i\
fixture/gh-third
' "$declaration" >"$tmp_dir/declaration"
mv "$tmp_dir/declaration" "$declaration"
export GH_SET_FAIL=fixture/gh-second
GH_SET_INSTALLED="$initial_installed"
for os in linux darwin; do
  installer="$tmp_dir/partial-$os.sh"
  render_template_from_source "{\"chezmoi\":{\"os\":\"$os\"}}" \
    .chezmoiscripts/run_before_20-install-gh-extensions.sh.tmpl "$source_dir" >"$installer"
  : >"$GH_SET_LOG"
  install_status=0
  sh "$installer" || install_status=$?
  assert_status "$install_status" 0 "$os partial install records a warning and exits zero"
  assert_equals "$(sed -n 's/^standard:extension install //p' "$GH_SET_LOG")" \
    'fixture/gh-second
fixture/gh-third' "$os partial install continues to later declarations"
  marker="$XDG_STATE_HOME/terrapod/install-warnings/gh-extensions"
  assert_file_contains "$marker" 'fixture/gh-second' "$os warning names the failed declaration"
  assert_file_not_contains "$marker" 'fixture/gh-third' "$os warning excludes successful declarations"
  assert_not_contains "$(cat "$GH_SET_LOG")" 'extension upgrade' "$os never upgrades extensions"
  assert_not_contains "$(cat "$GH_SET_LOG")" '--pin' "$os never pins extensions"

  # A non-directory state root forces a real marker write failure even as root.
  blocked_state="$tmp_dir/blocked-state-$os"
  : >"$blocked_state"
  install_status=0
  XDG_STATE_HOME="$blocked_state" sh "$installer" >"$tmp_dir/blocked-$os.log" 2>&1 || install_status=$?
  assert_status "$install_status" 1 "$os required warning write failure exits one"

  GH_SET_FAIL= sh "$installer"
  if [ -e "$marker" ]; then
    fail "$os successful rerun clears the partial failure warning"
  fi
  pass "$os successful rerun clears the partial failure warning"

  # Marker clearing is best-effort: replacing the marker with a non-empty
  # directory makes rm fail without relying on the invoking user's uid.
  mkdir -p "$marker/keep"
  install_status=0
  GH_SET_FAIL= sh "$installer" >"$tmp_dir/clear-$os.log" 2>&1 || install_status=$?
  assert_status "$install_status" 0 "$os warning clear failure remains non-fatal"
  rm -rf "$marker"

  mv "$prefix/bin/gh" "$prefix/bin/gh-unavailable"
  : >"$GH_SET_LOG"
  install_status=0
  sh "$installer" || install_status=$?
  assert_status "$install_status" 0 "$os missing standard-prefix gh records a warning and exits zero"
  assert_equals "$(cat "$GH_SET_LOG")" '' "$os missing standard-prefix gh does not fall back to PATH"
  assert_file_contains "$marker" 'Homebrew core bundle' "$os missing gh keeps Homebrew core guidance"
  mv "$prefix/bin/gh-unavailable" "$prefix/bin/gh"
done
