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
  '{ key = "1", label = "Codex Desktop", bundleID = "com.openai.codex" }' \
  "Hammerspoon launcher binds Codex Desktop to 1"

assert_contains \
  '{ key = "2", label = "Claude Desktop", bundleID = "com.anthropic.claudefordesktop" }' \
  "Hammerspoon launcher binds Claude Desktop to 2"

assert_contains \
  '{ key = "3", label = "Antigravity 2.0", bundleID = "com.google.antigravity" }' \
  "Hammerspoon launcher binds Antigravity 2.0 to 3"

assert_contains \
  '{ key = "i", label = "Antigravity IDE", bundleID = "com.google.antigravity-ide" }' \
  "Hammerspoon launcher binds Antigravity IDE to i"

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
