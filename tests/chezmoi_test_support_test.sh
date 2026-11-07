#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

# Record at the command seam while preserving real chezmoi behavior.
call_log="$tmp_dir/chezmoi-calls"
: >"$call_log"
chezmoi() {
  printf '%s\n' "$*" >>"$call_log"
  command chezmoi "$@"
}

chezmoi_config="$tmp_dir/chezmoi.toml"
printf '# existing caller config\n[data]\nname = "sentinel"\n' >"$tmp_dir/chezmoi.toml"
cp "$tmp_dir/chezmoi.toml" "$tmp_dir/expected-config"
. "$repo_root/tests/lib/chezmoi-test-support.sh"
assert_equals "$(cat "$call_log")" "" "sourcing support does not execute chezmoi"
cmp -s "$tmp_dir/chezmoi.toml" "$tmp_dir/expected-config" ||
  fail "sourcing support preserves existing config bytes"
pass "sourcing support preserves existing config bytes"

rendered="$(render_template "$macos_ai_cli_tools_data" Brewfile.ai-cli-tools.tmpl)"
assert_equals "$(wc -l <"$call_log" | tr -d ' ')" "1" \
  "one template request executes chezmoi once"
assert_file_contains "$call_log" 'execute-template' "template request executes rendering"
assert_file_not_contains "$call_log" ' managed' "template request does not discover managed paths"
assert_file_not_contains "$call_log" "$tmp_dir/chezmoi.toml" \
  "helper execution uses config isolated from caller config"
assert_equals "$rendered" 'cask "antigravity-cli"
cask "codex"' "recorded rendering returns the real macOS AI tool declarations"
cmp -s "$tmp_dir/chezmoi.toml" "$tmp_dir/expected-config" ||
  fail "rendering preserves existing caller config bytes"
pass "rendering preserves existing caller config bytes"
