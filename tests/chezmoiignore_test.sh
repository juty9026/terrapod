#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

managed_source_paths() {
  data="$1"
  chezmoi \
    --source "$repo_root" \
    --override-data "$data" \
    managed \
    --path-style source-relative
}

render_template() {
  data="$1"
  file="$2"

  chezmoi execute-template \
    --override-data "$data" \
    --file "$repo_root/$file"
}

render_managed_file() {
  data="$1"
  destination="$2"

  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    --override-data "$data" \
    cat "$tmp_dir/home/$destination"
}

assert_managed_paths_exclude_prefix() {
  managed_paths="$1"
  prefix="$2"
  message="$3"

  if printf '%s\n' "$managed_paths" | grep -E "^${prefix}(/|$)" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_managed_paths_include_prefix() {
  managed_paths="$1"
  prefix="$2"
  message="$3"

  if ! printf '%s\n' "$managed_paths" | grep -E "^${prefix}(/|$)" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

managed_tests="$(
  chezmoi \
    --source "$repo_root" \
    managed \
    --path-style source-relative |
    grep '^tests/' || true
)"

if [ -n "$managed_tests" ]; then
  fail "development tests should not be managed by chezmoi: $managed_tests"
fi

pass "development tests are ignored by chezmoi"

ubuntu_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false}'
ubuntu_managed="$(managed_source_paths "$ubuntu_data")"
macos_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false}'
macos_managed="$(managed_source_paths "$macos_data")"
macos_ai_cli_tools_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":true,"enableDevelopmentWorkspace":false}'
macos_ai_cli_tools_managed="$(managed_source_paths "$macos_ai_cli_tools_data")"
macos_development_workspace_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":true}'
macos_development_workspace_managed="$(managed_source_paths "$macos_development_workspace_data")"
editor_stack_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":true,"enableAiCliTools":false,"enableDevelopmentWorkspace":false}'
editor_stack_managed="$(managed_source_paths "$editor_stack_data")"
ai_cli_tools_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":true,"enableDevelopmentWorkspace":false}'
ai_cli_tools_managed="$(managed_source_paths "$ai_cli_tools_data")"
development_workspace_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":true}'
development_workspace_managed="$(managed_source_paths "$development_workspace_data")"

macos_only_entries="
.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl
.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl
dot_config/cmux
dot_config/ghostty
dot_config/private_karabiner
dot_hammerspoon
dot_zprofile
"

for entry in $macos_only_entries; do
  if printf '%s\n' "$ubuntu_managed" | grep -Fx "$entry" >/dev/null; then
    fail "Ubuntu VPS should not manage macOS-only entry: $entry"
  fi
done

pass "Ubuntu VPS ignores macOS-only entries"

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  ".chezmoiscripts/run_onchange_after_40-remove-legacy-npm-ai-tools.sh.tmpl" \
  "macOS default ignores legacy AI tool uninstall script"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  "dot_config/nvim" \
  "Ubuntu VPS ignores Optional Editor Stack entries by default"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  "dot_config/zellij/layouts/dev.kdl" \
  "Ubuntu VPS ignores Optional Development Workspace layout by default"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl" \
  "Ubuntu VPS ignores Optional AI Tool Stack installer by default"

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  "dot_config/nvim" \
  "macOS ignores Optional Editor Stack entries by default"

assert_managed_paths_exclude_prefix \
  "$macos_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "macOS default ignores Antigravity PATH snippet"

assert_managed_paths_exclude_prefix \
  "$ai_cli_tools_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "Linux enableAiCliTools ignores Antigravity PATH snippet"

assert_managed_paths_include_prefix \
  "$macos_ai_cli_tools_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "macOS enableAiCliTools includes Antigravity PATH snippet"

assert_managed_paths_include_prefix \
  "$macos_development_workspace_managed" \
  "dot_config/zsh/path.d/antigravity.zsh.tmpl" \
  "macOS enableDevelopmentWorkspace includes Antigravity PATH snippet"

assert_managed_paths_include_prefix \
  "$editor_stack_managed" \
  "dot_config/nvim" \
  "enableEditorStack includes Optional Editor Stack entries"

assert_managed_paths_include_prefix \
  "$development_workspace_managed" \
  "dot_config/nvim" \
  "enableDevelopmentWorkspace includes Optional Editor Stack entries"

assert_managed_paths_exclude_prefix \
  "$ai_cli_tools_managed" \
  "dot_config/nvim" \
  "enableAiCliTools alone ignores Optional Editor Stack entries"

assert_managed_paths_exclude_prefix \
  "$ai_cli_tools_managed" \
  "dot_config/zellij/layouts/dev.kdl" \
  "enableAiCliTools alone ignores Optional Development Workspace layout"

assert_managed_paths_include_prefix \
  "$development_workspace_managed" \
  "dot_config/zellij/layouts/dev.kdl" \
  "enableDevelopmentWorkspace includes Optional Development Workspace layout"

assert_managed_paths_include_prefix \
  "$ai_cli_tools_managed" \
  ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl" \
  "enableAiCliTools includes Optional AI Tool Stack installer"

assert_managed_paths_include_prefix \
  "$development_workspace_managed" \
  ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl" \
  "enableDevelopmentWorkspace includes Optional AI Tool Stack installer"

ubuntu_mise_config="$(render_template "$ubuntu_data" "dot_config/mise/config.toml.tmpl")"

if ! printf '%s\n' "$ubuntu_mise_config" | grep -F '"aqua:neovim/neovim" = "latest"' >/dev/null; then
  fail "Ubuntu VPS keeps plain Neovim in the Core Shell Stack"
fi

pass "Ubuntu VPS keeps plain Neovim in the Core Shell Stack"

if ! printf '%s\n' "$ubuntu_mise_config" | grep -F '"aqua:cli/cli" = "latest"' >/dev/null; then
  fail "Ubuntu VPS installs GitHub CLI gh in the Core Shell Stack"
fi

pass "Ubuntu VPS installs GitHub CLI gh in the Core Shell Stack"

mise_tools_installer="$(render_template "$ubuntu_data" ".chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl")"

if ! printf '%s\n' "$mise_tools_installer" |
  grep -E '^# mise-config-sha256=[0-9a-f]{64}$' >/dev/null; then
  fail "mise tool installer tracks rendered mise config changes"
fi

pass "mise tool installer tracks rendered mise config changes"

ai_cli_tools_installer="$(render_template "$ai_cli_tools_data" ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl")"
development_workspace_ai_installer="$(render_template "$development_workspace_data" ".chezmoiscripts/run_onchange_after_60-install-ai-cli-tools.sh.tmpl")"

for package in \
  "@anthropic-ai/claude-code" \
  "@google/gemini-cli" \
  "@openai/codex"
do
  if ! printf '%s\n' "$ai_cli_tools_installer" | grep -F "$package" >/dev/null; then
    fail "enableAiCliTools renders Optional AI Tool Stack installer"
  fi

  if ! printf '%s\n' "$development_workspace_ai_installer" | grep -F "$package" >/dev/null; then
    fail "enableDevelopmentWorkspace renders Optional AI Tool Stack installer"
  fi
done

pass "enableAiCliTools renders Optional AI Tool Stack installer"
pass "enableDevelopmentWorkspace renders Optional AI Tool Stack installer"

development_workspace_zellij_layout="$(render_managed_file "$development_workspace_data" ".config/zellij/layouts/dev.kdl")"

for pane in CLAUDE CODEX GEMINI; do
  if ! printf '%s\n' "$development_workspace_zellij_layout" |
    grep -E "pane name=\"${pane}\" .*start_suspended=true" >/dev/null; then
    fail "enableDevelopmentWorkspace starts assistant panes suspended"
  fi
done

pass "enableDevelopmentWorkspace starts assistant panes suspended"

if ! printf '%s\n' "$development_workspace_zellij_layout" |
  grep -A2 'pane name="GEMINI" command="gemini"' |
  grep -F 'args "--yolo"' >/dev/null; then
  fail "enableDevelopmentWorkspace passes yolo mode to the Gemini pane"
fi

pass "enableDevelopmentWorkspace passes yolo mode to the Gemini pane"
