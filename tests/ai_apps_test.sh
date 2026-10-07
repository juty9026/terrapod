#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"

ai_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupAiApps":true}'
ai_apps_brewfile="$(render_template "$ai_apps_data" Brewfile.macos-desktop-apps.tmpl)"
for app in claude chatgpt google-gemini; do
  assert_contains "$ai_apps_brewfile" "cask \"$app\"" "ai-apps declares $app"
done

assert_not_contains "$ai_apps_brewfile" 'cask "codex-app"' "ai-apps uses the unified ChatGPT cask"
assert_not_contains "$ai_apps_brewfile" 'cask "codex"' "ai-apps does not select Codex CLI"
assert_not_contains "$ai_apps_brewfile" 'cask "zed"' "ai-apps stays independent of development-apps"
assert_not_contains "$(render_template "$macos_data" Brewfile.macos-desktop-apps.tmpl)" 'cask "claude"' "ai-apps is opt-in"
assert_contains "$(managed_source_paths "$ai_apps_data")" 'homebrew-bundle.sh' "ai-apps alone enables the desktop installer dependencies"
assert_not_contains "$(render_template '{"chezmoi":{"os":"linux"},"enableMacosAppGroupAiApps":true}' .chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl)" '# ai-apps macOS App Group' "VPS never installs AI desktop apps"

# Run the actual rendered installer with Homebrew stubbed. The Claude failure
# case also exercises individual retries after the bulk bundle fails.
run_ai_apps_case() {
  case_name="$1"
  case_arch="$2"
  case_version="$3"
  case_translated="$4"
  expect_gemini="$5"
  fail_claude="$6"
  case_dir="$tmp_dir/ai-$case_name"
  case_prefix="$case_dir/prefix"
  mkdir -p "$case_prefix/bin" "$case_dir/home"
  write_stub "$case_prefix/bin/sw_vers" "printf '%s\\n' '$case_version'"
  write_stub "$case_prefix/bin/brew" \
    'case "$1" in' \
    '  shellenv) printf "export PATH=\"%s/bin:$PATH\"\n" "$AI_APPS_PREFIX" ;;' \
    '  analytics) exit 0 ;;' \
    '  bundle)' \
    '    bundle_file=${3#--file=}' \
    '    count=$(grep -c "^cask " "$bundle_file" || true)' \
    '    [ "$count" -gt 0 ] || exit 0' \
    '    cat "$bundle_file" >>"$AI_APPS_LOG"' \
    '    [ "$HOMEBREW_NO_AUTO_UPDATE" = 1 ] || exit 99' \
    '    [ "$AI_APPS_FAIL_CLAUDE" = 1 ] || exit 0' \
    '    [ "$count" -le 1 ] || exit 1' \
    '    if [ "$AI_APPS_FAIL_CLAUDE" = 1 ] && grep -Fx '\''cask "claude"'\'' "$bundle_file" >/dev/null; then exit 1; fi' \
    '    ;;' \
    '  *) exit 64 ;;' \
    'esac'
  render_template_with_homebrew_prefix_provider "$ai_apps_data" \
    .chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl "$case_prefix" >"$case_dir/install.sh"
  sh -n "$case_dir/install.sh"
  AI_APPS_PREFIX="$case_prefix" AI_APPS_LOG="$case_dir/brew.log" AI_APPS_FAIL_CLAUDE="$fail_claude" \
    TERRAPOD_MACHINE_ARCH="$case_arch" TERRAPOD_DARWIN_TRANSLATED="$case_translated" \
    HOME="$case_dir/home" XDG_STATE_HOME="$case_dir/state" PATH="$case_prefix/bin:/usr/bin:/bin" \
    sh "$case_dir/install.sh" >"$case_dir/output" 2>"$case_dir/error" || {
      cat "$case_dir/error" >&2
      fail "$case_name installer completes"
    }
  assert_file_contains "$case_dir/brew.log" 'cask "claude"' "$case_name still attempts Claude"
  assert_file_contains "$case_dir/brew.log" 'cask "chatgpt"' "$case_name still attempts ChatGPT"
  if [ "$expect_gemini" = true ]; then
    assert_file_contains "$case_dir/brew.log" 'cask "google-gemini"' "$case_name installs Gemini"
    assert_file_not_contains "$case_dir/output" 'Skipping google-gemini' "$case_name does not skip supported Gemini"
  else
    assert_file_not_contains "$case_dir/brew.log" 'google-gemini' "$case_name excludes Gemini from bulk and retries"
    assert_file_contains "$case_dir/output" 'Skipping google-gemini: not applicable' "$case_name explains the exclusion"
  fi
  marker="$case_dir/state/terrapod/install-warnings/homebrew-desktop-apps"
  if [ "$fail_claude" = 1 ]; then
    assert_file_contains "$marker" 'failed casks: claude; App Groups: ai-apps' "$case_name attributes actual failure to ai-apps"
    assert_file_not_contains "$marker" 'google-gemini' "$case_name never records unsupported Gemini as a failure"
  elif [ -e "$marker" ]; then
    fail "$case_name leaves no install warning"
  else
    pass "$case_name leaves no install warning"
  fi
}

run_ai_apps_case apple-silicon-15 arm64 15.0 0 true 0
run_ai_apps_case apple-silicon-14 arm64 14.7 0 false 0
run_ai_apps_case intel-15 x86_64 15.0 0 false 0
run_ai_apps_case rosetta-15 x86_64 15.2 1 true 0
run_ai_apps_case future-macos arm64 27.0 0 true 0
run_ai_apps_case unknown-version arm64 unknown 0 false 0
run_ai_apps_case claude-failure x86_64 15.0 0 false 1

# The revived key is managed again; Presets deliberately choose a new concrete
# value, while routine readers honor an existing true selection.
for preset in minimal development workstation; do
  preset_config="$tmp_dir/ai-preset-$preset.toml"
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$preset_config" \
    HOME="$tmp_dir/home" sh "$repo_root/dot_local/bin/executable_terrapod" configure "$preset" >/dev/null
  expected=false
  [ "$preset" != workstation ] || expected=true
  assert_file_contains "$preset_config" "enableMacosAppGroupAiApps = $expected" "$preset proposes ai-apps $expected"
done

incomplete_config="$tmp_dir/ai-missing-key.toml"
sed '/^enableMacosAppGroupAiApps = /d' "$preset_config" >"$incomplete_config"
incomplete_status="$(TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$incomplete_config" \
  HOME="$tmp_dir/home" sh "$repo_root/dot_local/bin/executable_terrapod" status)"
assert_contains "$incomplete_status" 'enableMacosAppGroupAiApps' "configs without the restored key require setup"
