#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
fail() { printf '%s\n' "not ok - $*" >&2; exit 1; }

managed_keys='profile|enableEditorStack|enableAiCliTools|enableDevelopmentWorkspace|enableMacosAppGroupTerminalApps|enableMacosAppGroupAutomation|enableMacosAppGroupLauncher|enableMacosAppGroupMonitoring|enableMacosAppGroupDevelopmentApps'
legacy_refs="$(
  find "$repo_root" -type f \( -name '*.tmpl' -o -name '.chezmoiignore' \) \
    -not -path "$repo_root/docs/*" -exec grep -nE "get \\. \"($managed_keys)\"" {} + 2>/dev/null || true
)"
[ -z "$legacy_refs" ] || fail "managed templates still read root data: $legacy_refs"

grep -F '.terrapod.enableDevelopmentWorkspace' "$repo_root/dot_zshrc.tmpl" >/dev/null ||
  fail "dot_zshrc does not read independent Terrapod data"
grep -F '.terrapod.enableEditorStack' "$repo_root/.chezmoiignore" >/dev/null ||
  fail ".chezmoiignore does not read independent Terrapod data"
grep -F 'get . "gitAllowedSigners"' "$repo_root/private_dot_ssh/allowed_signers.tmpl" >/dev/null ||
  fail "unrelated root chezmoi data was moved"

go test ./internal/config ./internal/setup

printf '%s\n' "ok - templates use independent Terrapod config data"
