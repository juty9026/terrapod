#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM
fail() { printf '%s\n' "not ok - $*" >&2; exit 1; }

config="$tmp_dir/chezmoi.toml"
: >"$config"
data="$tmp_dir/data.json"
cat >"$data" <<'JSON'
{"terrapod":{"profile":"vps-shell","enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false,"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false},"gitAllowedSigners":[]}
JSON

managed="$tmp_dir/managed"
chezmoi --config "$config" --source "$repo_root" --override-data-file "$data" \
  managed --exclude scripts --path-style source-relative >"$managed"

grep -Fx 'dot_zshrc.tmpl' "$managed" >/dev/null || fail "zshrc is not managed"
if grep -E '^(tests|docs|\\.chezmoiscripts)(/|$)|^(Brewfile|dot_local/bin/(executable_terrapod|symlink_tpod))$' "$managed" >/dev/null; then
  fail "legacy or development source is managed"
fi

if grep -F '.chezmoiscripts/' "$repo_root/.chezmoiignore" >/dev/null; then
  fail "obsolete script-specific ignore entries remain"
fi

printf '%s\n' "ok - chezmoi uses independent data and excludes scripts"
