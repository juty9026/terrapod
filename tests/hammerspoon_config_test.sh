#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
hammerspoon_config="$repo_root/dot_hammerspoon/init.lua"

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

  if ! grep -F "$needle" "$hammerspoon_config" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  needle="$1"
  message="$2"

  if grep -F "$needle" "$hammerspoon_config" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_contains \
  '{ key = "1", label = "Orca", bundleID = "com.stablyai.orca" }' \
  "Hammerspoon launcher binds Orca to 1"

assert_contains \
  '{ key = "2", label = "Zed", bundleID = "dev.zed.Zed" }' \
  "Hammerspoon launcher binds Zed to 2"

assert_not_contains \
  'com.openai.codex' \
  "Hammerspoon launcher no longer includes ChatGPT"

assert_not_contains \
  'com.anthropic.claudefordesktop' \
  "Hammerspoon launcher no longer includes Claude Desktop"

assert_not_contains \
  '{ key = "4", label = "Orca", bundleID = "com.stablyai.orca" }' \
  "Hammerspoon launcher no longer binds Orca to 4"

assert_not_contains \
  'com.google.antigravity' \
  "Hammerspoon launcher no longer includes Antigravity desktop apps"

assert_not_contains \
  'ChatGPT Atlas' \
  "Hammerspoon launcher no longer includes ChatGPT Atlas label"

assert_not_contains \
  'com.openai.atlas' \
  "Hammerspoon launcher no longer includes ChatGPT Atlas bundle ID"

assert_not_contains \
  '{ key = "c", label = "Codex", bundleID = "com.openai.codex" }' \
  "Hammerspoon launcher no longer binds Codex Desktop to c"

assert_not_contains \
  '{ key = "i", label = "Antigravity", bundleID = "com.google.antigravity" }' \
  "Hammerspoon launcher no longer binds Antigravity 2.0 to i"
