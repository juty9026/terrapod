#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"
macos_terminal_apps_bootstrap="$(render_template "$macos_terminal_apps_data" ".chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl")"
ai_cli_tools_installer="$(render_template "$ai_cli_tools_data" ".chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl")"
macos_ai_cli_tools_installer="$(render_template "$macos_ai_cli_tools_data" ".chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl")"
development_workspace_ai_installer="$(render_template "$development_workspace_data" ".chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl")"
disabled_ai_cli_tools_cleanup="$(render_template "$ubuntu_data" ".chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl")"
ai_cli_tools_brewfile="$(render_template "$ai_cli_tools_data" "Brewfile.ai-cli-tools.tmpl")"
development_workspace_ai_brewfile="$(render_template "$development_workspace_data" "Brewfile.ai-cli-tools.tmpl")"
disabled_ai_cli_tools_brewfile="$(render_template "$ubuntu_data" "Brewfile.ai-cli-tools.tmpl")"
macos_ai_cli_tools_brewfile="$(render_template "$macos_ai_cli_tools_data" "Brewfile.ai-cli-tools.tmpl")"
macos_development_workspace_ai_brewfile="$(render_template "$macos_development_workspace_data" "Brewfile.ai-cli-tools.tmpl")"

assert_not_contains "$ai_cli_tools_installer" "bootstrap_linux_homebrew" "AI installer no longer owns Linuxbrew bootstrap"
assert_not_contains "$ai_cli_tools_installer" "raw.githubusercontent.com/Homebrew/install" "AI installer never downloads Homebrew"
assert_not_contains "$ai_cli_tools_installer" "HOMEBREW_NO_AUTO_UPDATE=1" "Ubuntu AI installer runs no Homebrew bundle"
assert_not_contains "$ai_cli_tools_installer" "install_ai_cli_bundle" "Ubuntu AI installer renders no Homebrew bundle step"
assert_not_contains "$ai_cli_tools_installer" 'cask "codex"' "Ubuntu AI installer renders no macOS-only casks"
assert_contains "$ai_cli_tools_installer" 'finish_install_warning_category' "Ubuntu AI installer clears stale optional AI CLI markers through the policy layer"
assert_contains "$macos_ai_cli_tools_installer" 'terrapod_standard_homebrew_brew_path "darwin"' "macOS AI installer derives brew from the standard Homebrew prefix"
assert_contains "$macos_ai_cli_tools_installer" "HOMEBREW_NO_AUTO_UPDATE=1" "AI bundle disables Homebrew auto-update"
assert_contains "$macos_terminal_apps_bootstrap" "HOMEBREW_NO_AUTO_UPDATE=1" "desktop bundle disables Homebrew auto-update"

for rendered_brewfile in "$macos_ai_cli_tools_brewfile" "$macos_development_workspace_ai_brewfile"; do
  assert_contains "$rendered_brewfile" 'cask "antigravity-cli"' "Optional AI Tool Stack declares Antigravity CLI cask"
  assert_contains "$rendered_brewfile" 'cask "codex"' "Optional AI Tool Stack declares Codex CLI cask"
  assert_not_contains "$rendered_brewfile" 'cask "claude-code"' "Optional AI Tool Stack no longer declares a Claude Code cask"
done
assert_equals "$disabled_ai_cli_tools_brewfile" "" "disabled Optional AI Tool Stack renders no Homebrew casks"
assert_equals "$ai_cli_tools_brewfile" "" "Ubuntu Optional AI Tool Stack renders no Homebrew casks"
assert_equals "$development_workspace_ai_brewfile" "" "Ubuntu Optional Development Workspace renders no AI Homebrew casks"

assert_contains "$disabled_ai_cli_tools_cleanup" "AI_CLI_WARNING_CATEGORY=optional-ai-cli-tools" "disabled Optional AI Tool Stack renders optional AI CLI warning category"
assert_contains "$disabled_ai_cli_tools_cleanup" 'finish_install_warning_category' "disabled Optional AI Tool Stack renders policy-layer stale marker cleanup"
assert_not_contains "$disabled_ai_cli_tools_cleanup" "raw.githubusercontent.com/Homebrew/install" "disabled Optional AI Tool Stack cleanup does not render Homebrew installer URL"

disabled_ai_cli_tools_cleanup_script="$tmp_dir/disabled-ai-cli-tools-cleanup.sh"
printf '%s\n' "$disabled_ai_cli_tools_cleanup" >"$disabled_ai_cli_tools_cleanup_script"
sh -n "$disabled_ai_cli_tools_cleanup_script" || fail "disabled Optional AI Tool Stack cleanup script should be valid sh"
pass "disabled Optional AI Tool Stack cleanup script is valid sh"

ai_marker_state="$tmp_dir/ai-marker-state"
ai_marker_home="$tmp_dir/ai-marker-home"
mkdir -p "$ai_marker_home"
HOME="$ai_marker_home" XDG_STATE_HOME="$ai_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write optional-ai-cli-tools "Optional AI CLI tool install needs attention" "Rerun tpod apply after network access is restored."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"
HOME="$ai_marker_home" XDG_STATE_HOME="$ai_marker_state" sh "$disabled_ai_cli_tools_cleanup_script"
if [ -e "$ai_marker_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "disabled Optional AI Tool Stack cleanup clears stale optional-ai-cli-tools marker"
fi
pass "disabled Optional AI Tool Stack cleanup clears stale optional AI CLI markers"

ai_cli_tools_installer_script="$tmp_dir/ai-cli-tools-installer.sh"
macos_ai_cli_tools_installer_script="$tmp_dir/macos-ai-cli-tools-installer.sh"
render_template_with_homebrew_prefix_provider \
  "$ai_cli_tools_data" \
  '.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl' \
  "$tmp_dir/linux-ai-prefix" >"$ai_cli_tools_installer_script"
macos_ai_prefix="$tmp_dir/macos-ai-prefix"
render_template_with_homebrew_prefix_provider \
  "$macos_ai_cli_tools_data" \
  '.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl' \
  "$macos_ai_prefix" >"$macos_ai_cli_tools_installer_script"
sh -n "$ai_cli_tools_installer_script" || fail "enabled Optional AI Tool Stack installer script should be valid sh"
sh -n "$macos_ai_cli_tools_installer_script" || fail "macOS Optional AI Tool Stack installer script should be valid sh"
pass "enabled Optional AI Tool Stack installer scripts are valid sh"

write_ai_brew_stub() {
  path="$1"
  write_stub "$path" \
    'printf "%s\n" "brew args:$*" >>"$AI_BREW_LOG"' \
    'printf "%s\n" "brew path:$0" >>"$AI_BREW_LOG"' \
    'printf "%s\n" "brew auto-update:${HOMEBREW_NO_AUTO_UPDATE:-}" >>"$AI_BREW_LOG"' \
    'case "$1" in' \
    '  shellenv)' \
    '    [ "${AI_BREW_SHELLENV_FAIL:-0}" = "0" ] || exit 41' \
    '    printf "export PATH=\"%s:$PATH\"\n" "$AI_BREW_BIN"' \
    '    ;;' \
    '  bundle)' \
    '    bundle_file=' \
    '    for arg do case "$arg" in --file=*) bundle_file="$(printf "%s" "$arg" | cut -d= -f2-)" ;; esac; done' \
    '    [ -n "$bundle_file" ] || exit 64' \
    '    grep -Fx "cask \"antigravity-cli\"" "$bundle_file" >/dev/null || exit 65' \
    '    grep -Fx "cask \"codex\"" "$bundle_file" >/dev/null || exit 66' \
    '    [ "$AI_BREW_FAIL" = "0" ] || exit 42' \
    '    ;;' \
    '  *) exit 64 ;;' \
    'esac'
}

write_claude_installer_stubs() {
  stub_dir="$1"
  write_stub "$stub_dir/curl" \
    'log="${CLAUDE_INSTALLER_LOG:-/dev/null}"' \
    'printf "%s\n" "curl args:$*" >>"$log"' \
    '[ "${CLAUDE_INSTALLER_CURL_FAIL:-0}" = "0" ] || exit 4' \
    'output=' \
    'while [ "$#" -gt 0 ]; do' \
    '  if [ "$1" = "-o" ]; then output="$2"; shift 2; else shift; fi' \
    'done' \
    '[ -n "$output" ] || exit 2' \
    'printf "%s\n" "#!/bin/bash" "exit 0" >"$output"'
  write_stub "$stub_dir/bash" \
    'log="${CLAUDE_INSTALLER_LOG:-/dev/null}"' \
    'printf "%s\n" "bash args:$*" >>"$log"' \
    '[ "${CLAUDE_INSTALLER_FAIL:-0}" = "0" ] || exit 3' \
    'if [ "${CLAUDE_INSTALLER_SKIP_BINARY:-0}" != "0" ]; then' \
    '  printf "%s\n" "bash skipped binary" >>"$log"' \
    '  exit 0' \
    'fi' \
    'mkdir -p "$HOME/.local/bin"' \
    'printf "%s\n" "#!/bin/sh" "exit 0" >"$HOME/.local/bin/claude"' \
    'chmod +x "$HOME/.local/bin/claude"'
}

macos_ai_brew_bin="$macos_ai_prefix/bin"
mkdir -p "$macos_ai_brew_bin"
write_ai_brew_stub "$macos_ai_brew_bin/brew"
write_stub "$macos_ai_brew_bin/uname" 'printf "%s\n" "${AI_UNAME_ARCH:-arm64}"'
write_claude_installer_stubs "$macos_ai_brew_bin"

run_macos_ai_arch_case() {
  arch="$1"
  expected_brew_bin="$2"
  case_dir="$tmp_dir/macos-ai-$arch"
  case_home="$case_dir/home"
  case_state="$case_dir/state"
  case_log="$case_dir/brew.log"
  mkdir -p "$case_home"

  HOME="$case_home" XDG_STATE_HOME="$case_state" \
    AI_UNAME_ARCH="$arch" AI_BREW_BIN="$expected_brew_bin" AI_BREW_LOG="$case_log" AI_BREW_FAIL=0 \
    PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script"
  case_log_text="$(cat "$case_log")"
  assert_contains "$case_log_text" "brew path:$expected_brew_bin/brew" "macOS $arch Optional AI Tool Stack selects its standard Homebrew prefix"
  assert_contains "$case_log_text" "brew args:shellenv" "macOS $arch Optional AI Tool Stack loads Homebrew shellenv"
  assert_contains "$case_log_text" "brew args:bundle --no-upgrade --file=" "macOS $arch Optional AI Tool Stack installs the common no-upgrade bundle"
  assert_contains "$case_log_text" "brew auto-update:1" "macOS $arch Optional AI Tool Stack disables Homebrew auto-update"
}

run_macos_ai_arch_case arm64 "$macos_ai_brew_bin"
run_macos_ai_arch_case x86_64 "$macos_ai_brew_bin"

macos_ai_brew_home="$tmp_dir/macos-ai-brew-home"
macos_ai_brew_state="$tmp_dir/macos-ai-brew-state"
macos_ai_brew_log="$tmp_dir/macos-ai-brew.log"
mkdir -p "$macos_ai_brew_home"

ai_cli_shellenv_failure_status=0
HOME="$macos_ai_brew_home" XDG_STATE_HOME="$macos_ai_brew_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$macos_ai_brew_log" \
  AI_BREW_FAIL=0 AI_BREW_SHELLENV_FAIL=1 PATH="$macos_ai_brew_bin:/usr/bin:/bin" \
  sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 || ai_cli_shellenv_failure_status=$?
if [ "$ai_cli_shellenv_failure_status" -ne 0 ]; then
  fail "Optional AI Tool Stack should continue apply when Homebrew shellenv fails"
fi
if [ ! -f "$macos_ai_brew_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "Optional AI Tool Stack records an optional-ai-cli-tools warning when Homebrew shellenv fails"
fi
if grep -F "brew args:bundle" "$macos_ai_brew_log" >/dev/null; then
  fail "Optional AI Tool Stack stops before bundle when Homebrew shellenv fails"
fi
pass "Optional AI Tool Stack records a warning and stops before bundle when Homebrew shellenv fails"

for vendor_url in \
  "https://antigravity.google/cli/install.sh" \
  "https://chatgpt.com/codex/install.sh"
do
  assert_not_contains "$ai_cli_tools_installer" "$vendor_url" "Optional AI Tool Stack no longer renders vendor installer URL: $vendor_url"
  assert_not_contains "$macos_ai_cli_tools_installer" "$vendor_url" "macOS Optional AI Tool Stack no longer renders vendor installer URL: $vendor_url"
done

assert_contains "$macos_ai_cli_tools_installer" "https://claude.ai/install.sh" \
  "macOS Optional AI Tool Stack renders the Claude Code installer URL"
assert_not_contains "$ai_cli_tools_installer" "https://claude.ai/install.sh" \
  "Ubuntu Optional AI Tool Stack renders no Claude Code installer URL"
assert_contains "$macos_ai_cli_tools_installer" 'bash "$claude_code_installer" </dev/null' \
  "Claude Code installer runs under bash with stdin detached"

linux_ai_brew_bin="$tmp_dir/linux-ai-brew-bin"
linux_ai_brew_home="$tmp_dir/linux-ai-brew-home"
linux_ai_brew_state="$tmp_dir/linux-ai-brew-state"
linux_ai_brew_log="$tmp_dir/linux-ai-brew.log"
linux_ai_brew_template="$tmp_dir/linux-ai-brew-template"
mkdir -p "$linux_ai_brew_bin" "$linux_ai_brew_home"
write_ai_brew_stub "$linux_ai_brew_template"
cp "$linux_ai_brew_template" "$linux_ai_brew_bin/brew"
write_stub "$linux_ai_brew_bin/uname" \
  'case "${1:-}" in' \
  '  -m) printf "%s\n" x86_64 ;;' \
  '  *) printf "%s\n" Linux ;;' \
  'esac'
HOME="$linux_ai_brew_home" XDG_STATE_HOME="$linux_ai_brew_state" sh -c \
  '. "$1"; terrapod_install_warning_write optional-ai-cli-tools "Optional AI CLI tool install needs attention" "Rerun tpod apply after network access is restored."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"
HOME="$linux_ai_brew_home" XDG_STATE_HOME="$linux_ai_brew_state" \
  AI_BREW_BIN="$linux_ai_brew_bin" AI_BREW_LOG="$linux_ai_brew_log" AI_BREW_FAIL=0 \
  PATH="$linux_ai_brew_bin:/usr/bin:/bin" sh "$ai_cli_tools_installer_script"
if [ -e "$linux_ai_brew_log" ]; then
  fail "Ubuntu Optional AI Tool Stack should never run Homebrew"
fi
pass "Ubuntu Optional AI Tool Stack never runs Homebrew"
if [ -e "$linux_ai_brew_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "Ubuntu Optional AI Tool Stack should clear stale optional-ai-cli-tools markers"
fi
pass "Ubuntu Optional AI Tool Stack clears stale optional AI CLI markers"

ai_cli_failure_state="$tmp_dir/ai-cli-failure-state"
ai_cli_failure_home="$tmp_dir/ai-cli-failure-home"
ai_cli_failure_log="$tmp_dir/ai-cli-failure.log"
mkdir -p "$ai_cli_failure_home"
ai_cli_failure_status=0
HOME="$ai_cli_failure_home" XDG_STATE_HOME="$ai_cli_failure_state" \
  AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$ai_cli_failure_log" AI_BREW_FAIL=1 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  ai_cli_failure_status=$?
if [ "$ai_cli_failure_status" -ne 0 ]; then
  fail "routine Optional AI Tool Stack bundle failure should continue apply after recording a warning"
fi
ai_cli_failure_marker="$ai_cli_failure_state/terrapod/install-warnings/optional-ai-cli-tools"
if [ ! -f "$ai_cli_failure_marker" ]; then
  fail "Optional AI Tool Stack bundle failure records optional-ai-cli-tools marker"
fi
pass "routine Optional AI Tool Stack bundle failure records a warning and exits zero"

HOME="$ai_cli_failure_home" XDG_STATE_HOME="$ai_cli_failure_state" \
  AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$ai_cli_failure_log" AI_BREW_FAIL=1 \
  TERRAPOD_FIRST_RUN_APPLY=1 PATH="$macos_ai_brew_bin:/usr/bin:/bin" \
  sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  fail "first-run Optional AI Tool Stack bundle failure remains recoverable"
pass "first-run Optional AI Tool Stack bundle failure records a warning and exits zero"

ai_cli_marker_write_failure_home="$tmp_dir/ai-cli-marker-write-failure-home"
ai_cli_marker_write_failure_parent="$tmp_dir/ai-cli-marker-write-failure-parent"
ai_cli_marker_write_failure_log="$tmp_dir/ai-cli-marker-write-failure.log"
mkdir -p "$ai_cli_marker_write_failure_home"
: >"$ai_cli_marker_write_failure_parent"
ai_cli_marker_write_failure_status=0
HOME="$ai_cli_marker_write_failure_home" XDG_STATE_HOME="$ai_cli_marker_write_failure_parent/state" \
  AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$ai_cli_marker_write_failure_log" AI_BREW_FAIL=1 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  ai_cli_marker_write_failure_status=$?
if [ "$ai_cli_marker_write_failure_status" -eq 0 ]; then
  fail "Optional AI Tool Stack should fail when the optional-ai-cli-tools marker cannot be written"
fi
pass "Optional AI Tool Stack fails when the optional-ai-cli-tools marker cannot be written"

HOME="$ai_cli_failure_home" XDG_STATE_HOME="$ai_cli_failure_state" \
  AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$ai_cli_failure_log" AI_BREW_FAIL=0 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script"
if [ -e "$ai_cli_failure_marker" ]; then
  fail "successful Optional AI Tool Stack retry clears warning marker"
fi
pass "successful Optional AI Tool Stack retry clears warning marker"

claude_fresh_home="$tmp_dir/claude-fresh-home"
claude_fresh_state="$tmp_dir/claude-fresh-state"
claude_fresh_brew_log="$tmp_dir/claude-fresh-brew.log"
claude_fresh_installer_log="$tmp_dir/claude-fresh-installer.log"
mkdir -p "$claude_fresh_home"
HOME="$claude_fresh_home" XDG_STATE_HOME="$claude_fresh_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_fresh_brew_log" AI_BREW_FAIL=0 \
  CLAUDE_INSTALLER_LOG="$claude_fresh_installer_log" \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script"
assert_contains "$(cat "$claude_fresh_installer_log")" "curl args:" \
  "Optional AI Tool Stack downloads the Claude Code installer when Claude Code is absent"
assert_contains "$(cat "$claude_fresh_installer_log")" "bash args:" \
  "Optional AI Tool Stack runs the Claude Code installer when Claude Code is absent"
if [ ! -x "$claude_fresh_home/.local/bin/claude" ]; then
  fail "Optional AI Tool Stack installs the canonical Claude Code executable"
fi
pass "Optional AI Tool Stack installs the canonical Claude Code executable"
if [ -e "$claude_fresh_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "a successful Optional AI Tool Stack apply records no warning"
fi
pass "a successful Optional AI Tool Stack apply records no warning"

claude_present_home="$tmp_dir/claude-present-home"
claude_present_state="$tmp_dir/claude-present-state"
claude_present_brew_log="$tmp_dir/claude-present-brew.log"
claude_present_installer_log="$tmp_dir/claude-present-installer.log"
mkdir -p "$claude_present_home/.local/bin"
write_stub "$claude_present_home/.local/bin/claude" 'exit 0'
HOME="$claude_present_home" XDG_STATE_HOME="$claude_present_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_present_brew_log" AI_BREW_FAIL=0 \
  CLAUDE_INSTALLER_LOG="$claude_present_installer_log" \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script"
if [ -e "$claude_present_installer_log" ]; then
  fail "an existing Claude Code install should not rerun the vendor installer"
fi
pass "an existing Claude Code install does not rerun the vendor installer"

claude_failure_home="$tmp_dir/claude-failure-home"
claude_failure_state="$tmp_dir/claude-failure-state"
claude_failure_brew_log="$tmp_dir/claude-failure-brew.log"
mkdir -p "$claude_failure_home"
claude_failure_status=0
HOME="$claude_failure_home" XDG_STATE_HOME="$claude_failure_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_failure_brew_log" AI_BREW_FAIL=0 \
  CLAUDE_INSTALLER_FAIL=1 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  claude_failure_status=$?
if [ "$claude_failure_status" -ne 0 ]; then
  fail "a Claude Code install failure should continue apply after recording a warning"
fi
claude_failure_marker="$claude_failure_state/terrapod/install-warnings/optional-ai-cli-tools"
if [ ! -f "$claude_failure_marker" ]; then
  fail "a Claude Code install failure records the optional-ai-cli-tools marker"
fi
assert_contains "$(cat "$claude_failure_marker")" "Claude Code" \
  "a Claude Code install failure names Claude Code in its marker"
assert_not_contains "$(cat "$claude_failure_marker")" "Homebrew bundle" \
  "a Claude Code install failure does not name the Homebrew bundle in its marker"
pass "a Claude Code install failure records a warning and exits zero"

claude_skip_binary_home="$tmp_dir/claude-skip-binary-home"
claude_skip_binary_state="$tmp_dir/claude-skip-binary-state"
claude_skip_binary_brew_log="$tmp_dir/claude-skip-binary-brew.log"
claude_skip_binary_installer_log="$tmp_dir/claude-skip-binary-installer.log"
mkdir -p "$claude_skip_binary_home"
claude_skip_binary_status=0
HOME="$claude_skip_binary_home" XDG_STATE_HOME="$claude_skip_binary_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_skip_binary_brew_log" AI_BREW_FAIL=0 \
  CLAUDE_INSTALLER_LOG="$claude_skip_binary_installer_log" CLAUDE_INSTALLER_SKIP_BINARY=1 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  claude_skip_binary_status=$?
if [ "$claude_skip_binary_status" -ne 0 ]; then
  fail "a vendor installer that exits zero without installing the canonical executable should continue apply after recording a warning"
fi
if [ -x "$claude_skip_binary_home/.local/bin/claude" ]; then
  fail "the CLAUDE_INSTALLER_SKIP_BINARY stub should not create the canonical executable"
fi
claude_skip_binary_marker="$claude_skip_binary_state/terrapod/install-warnings/optional-ai-cli-tools"
if [ ! -f "$claude_skip_binary_marker" ]; then
  fail "a vendor installer that exits zero without the canonical executable records the optional-ai-cli-tools marker"
fi
assert_contains "$(cat "$claude_skip_binary_marker")" "Claude Code" \
  "a vendor installer that exits zero without the canonical executable names Claude Code in its marker"
pass "a vendor installer that exits zero without installing the canonical executable records a warning and exits zero"

claude_curl_failure_home="$tmp_dir/claude-curl-failure-home"
claude_curl_failure_state="$tmp_dir/claude-curl-failure-state"
claude_curl_failure_brew_log="$tmp_dir/claude-curl-failure-brew.log"
claude_curl_failure_installer_log="$tmp_dir/claude-curl-failure-installer.log"
mkdir -p "$claude_curl_failure_home"
claude_curl_failure_status=0
HOME="$claude_curl_failure_home" XDG_STATE_HOME="$claude_curl_failure_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_curl_failure_brew_log" AI_BREW_FAIL=0 \
  CLAUDE_INSTALLER_LOG="$claude_curl_failure_installer_log" CLAUDE_INSTALLER_CURL_FAIL=1 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  claude_curl_failure_status=$?
if [ "$claude_curl_failure_status" -ne 0 ]; then
  fail "a Claude Code installer download failure should continue apply after recording a warning"
fi
if grep -F "bash args:" "$claude_curl_failure_installer_log" >/dev/null 2>&1; then
  fail "a Claude Code installer download failure should not run the downloaded script"
fi
claude_curl_failure_marker="$claude_curl_failure_state/terrapod/install-warnings/optional-ai-cli-tools"
if [ ! -f "$claude_curl_failure_marker" ]; then
  fail "a Claude Code installer download failure records the optional-ai-cli-tools marker"
fi
assert_contains "$(cat "$claude_curl_failure_marker")" "Claude Code" \
  "a Claude Code installer download failure names Claude Code in its marker"
pass "a Claude Code installer download failure records a warning and exits zero"

claude_both_failure_home="$tmp_dir/claude-both-failure-home"
claude_both_failure_state="$tmp_dir/claude-both-failure-state"
claude_both_failure_brew_log="$tmp_dir/claude-both-failure-brew.log"
mkdir -p "$claude_both_failure_home"
claude_both_failure_status=0
HOME="$claude_both_failure_home" XDG_STATE_HOME="$claude_both_failure_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_both_failure_brew_log" AI_BREW_FAIL=1 \
  CLAUDE_INSTALLER_FAIL=1 \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  claude_both_failure_status=$?
if [ "$claude_both_failure_status" -ne 0 ]; then
  fail "a Homebrew bundle failure together with a Claude Code install failure should continue apply after recording a warning"
fi
claude_both_failure_marker="$claude_both_failure_state/terrapod/install-warnings/optional-ai-cli-tools"
if [ ! -f "$claude_both_failure_marker" ]; then
  fail "a Homebrew bundle failure together with a Claude Code install failure records the optional-ai-cli-tools marker"
fi
claude_both_failure_marker_text="$(cat "$claude_both_failure_marker")"
assert_contains "$claude_both_failure_marker_text" "Homebrew bundle" \
  "a combined install failure names the Homebrew bundle in its marker"
assert_contains "$claude_both_failure_marker_text" "Claude Code" \
  "a combined install failure names Claude Code in its marker"
if ! grep -F "Failed: Homebrew bundle, Claude Code." "$claude_both_failure_marker" >/dev/null; then
  fail "a combined install failure names both sources on a single line"
fi
pass "a combined install failure records both sources on a single-line marker"

claude_bundle_failure_home="$tmp_dir/claude-bundle-failure-home"
claude_bundle_failure_state="$tmp_dir/claude-bundle-failure-state"
claude_bundle_failure_brew_log="$tmp_dir/claude-bundle-failure-brew.log"
claude_bundle_failure_installer_log="$tmp_dir/claude-bundle-failure-installer.log"
mkdir -p "$claude_bundle_failure_home"
claude_bundle_failure_status=0
HOME="$claude_bundle_failure_home" XDG_STATE_HOME="$claude_bundle_failure_state" \
  AI_UNAME_ARCH=arm64 AI_BREW_BIN="$macos_ai_brew_bin" AI_BREW_LOG="$claude_bundle_failure_brew_log" AI_BREW_FAIL=1 \
  CLAUDE_INSTALLER_LOG="$claude_bundle_failure_installer_log" \
  PATH="$macos_ai_brew_bin:/usr/bin:/bin" sh "$macos_ai_cli_tools_installer_script" >/dev/null 2>&1 ||
  claude_bundle_failure_status=$?
if [ "$claude_bundle_failure_status" -ne 0 ]; then
  fail "a Homebrew bundle failure should continue apply after recording a warning"
fi
assert_contains "$(cat "$claude_bundle_failure_installer_log")" "bash args:" \
  "a Homebrew bundle failure does not skip the Claude Code step"
claude_bundle_failure_marker="$claude_bundle_failure_state/terrapod/install-warnings/optional-ai-cli-tools"
if [ ! -f "$claude_bundle_failure_marker" ]; then
  fail "a Homebrew bundle failure records the optional-ai-cli-tools marker"
fi
assert_contains "$(cat "$claude_bundle_failure_marker")" "Homebrew bundle" \
  "a Homebrew bundle failure names the bundle in its marker"

if [ -e "$repo_root/dot_config/zsh/path.d/antigravity.zsh.tmpl" ]; then
  fail "legacy Antigravity app-bundle PATH snippet is no longer managed"
fi

pass "legacy Antigravity app-bundle PATH snippet is no longer managed"
