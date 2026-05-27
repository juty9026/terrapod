#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
readme="$repo_root/README.md"

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

assert_contains() {
  needle="$1"
  message="$2"

  if ! grep -F "$needle" "$readme" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_contains '| `enableEditorStack` | `false` |' \
  "README documents enableEditorStack default"
assert_contains '| `enableAiCliTools` | `false` |' \
  "README documents enableAiCliTools default"
assert_contains '| `enableDevelopmentWorkspace` | `false` |' \
  "README documents enableDevelopmentWorkspace default"
assert_contains '`enableMacosDesktopApps`' \
  "README documents enableMacosDesktopApps option"
if ! awk -F '|' '/`enableMacosDesktopApps`/ { gsub(/^[[:space:]]+|[[:space:]]+$/, "", $3); if ($3 == "`false`") found=1 } END { exit found ? 0 : 1 }' "$readme"; then
  fail "README documents enableMacosDesktopApps default"
fi

pass "README documents enableMacosDesktopApps default"

assert_contains 'All three optional stack profiles are disabled by default.' \
  "README states optional stack profiles are disabled by default"
assert_contains 'When `enableDevelopmentWorkspace` is `true`' \
  "README documents enableDevelopmentWorkspace behavior"
assert_contains 'Optional Editor Stack and Optional AI Tool Stack' \
  "README documents development workspace included stacks"
assert_contains 'macOS Desktop App Stack' \
  "README documents macOS Desktop App Stack"
assert_contains 'separate from `enableDevelopmentWorkspace`' \
  "README documents enableMacosDesktopApps separation from enableDevelopmentWorkspace"
assert_contains 'casks can affect shared applications' \
  "README documents why macOS Desktop App Stack remains separate"
assert_contains 'Opting out of an optional stack excludes its files from chezmoi management; it does not remove files already present on a machine.' \
  "README documents non-destructive optional stack opt-out"

assert_contains 'Minimal VPS' \
  "README includes a minimal VPS example"
assert_contains 'Editor-only machine' \
  "README includes an editor-only example"
assert_contains 'AI-only machine' \
  "README includes an AI-only example"
assert_contains 'Full development workspace machine' \
  "README includes a full development workspace example"
