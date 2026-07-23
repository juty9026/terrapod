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
if grep -E '^(tests|docs|\\.chezmoiscripts)(/|$)|^Brewfile$' "$managed" >/dev/null; then
  fail "legacy or development source is managed"
fi
if grep -Fx 'dot_local/bin/executable_terrapod.tmpl' "$managed" >/dev/null; then
  fail "manager data manages the legacy terrapod bridge"
fi
if grep -Fx 'dot_local/bin/symlink_tpod' "$managed" >/dev/null; then
  fail "manager data manages the legacy tpod symlink"
fi

legacy_data="$tmp_dir/legacy-data.json"
printf '%s\n' '{"profile":"macos-terminal"}' >"$legacy_data"
legacy_managed="$tmp_dir/legacy-managed"
chezmoi --config "$config" --source "$repo_root" --override-data-file "$legacy_data" \
  managed --exclude scripts --path-style source-relative >"$legacy_managed"
grep -Fx 'dot_local/bin/executable_terrapod.tmpl' "$legacy_managed" >/dev/null ||
  fail "legacy flat data does not manage the terrapod bridge"
grep -Fx 'dot_local/bin/symlink_tpod' "$legacy_managed" >/dev/null ||
  fail "legacy flat data does not manage the tpod symlink"

if grep -F '.chezmoiscripts/' "$repo_root/.chezmoiignore" >/dev/null; then
  fail "obsolete script-specific ignore entries remain"
fi

printf '%s\n' "ok - chezmoi uses independent data and excludes scripts"
