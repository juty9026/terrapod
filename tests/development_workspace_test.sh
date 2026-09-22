#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"
development_workspace_zellij_layout="$(render_managed_file "$development_workspace_data" ".config/zellij/layouts/dev.kdl")"

for pane in CLAUDE CODEX ANTIGRAVITY; do
  if ! printf '%s\n' "$development_workspace_zellij_layout" |
    grep -E "pane name=\"${pane}\" .*start_suspended=true" >/dev/null; then
    fail "enableDevelopmentWorkspace starts assistant panes suspended"
  fi
done

pass "enableDevelopmentWorkspace starts assistant panes suspended"

if printf '%s\n' "$development_workspace_zellij_layout" | grep -F 'command="gemini"' >/dev/null; then
  fail "enableDevelopmentWorkspace no longer launches Gemini CLI"
fi

pass "enableDevelopmentWorkspace no longer launches Gemini CLI"

if ! printf '%s\n' "$development_workspace_zellij_layout" |
  grep -A2 'pane name="ANTIGRAVITY" command="agy"' |
  grep -F 'args "--dangerously-skip-permissions"' >/dev/null; then
  fail "enableDevelopmentWorkspace passes supported permission skip mode to the Antigravity pane"
fi

pass "enableDevelopmentWorkspace passes supported permission skip mode to the Antigravity pane"
