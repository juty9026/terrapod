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

assert_contains() {
  haystack="$1"
  needle="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_line() {
  haystack="$1"
  expected_line="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | grep -Fx "$expected_line" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

mkdir -p "$tmp_dir/home"

terrapod="$repo_root/dot_local/bin/executable_terrapod"

managed_targets="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    managed
)"

assert_line \
  "$managed_targets" \
  ".local/bin/terrapod" \
  "chezmoi manages Terrapod as the primary user-facing command"

terrapod_target="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    target-path dot_local/bin/executable_terrapod
)"
expected_terrapod_target="$tmp_dir/home/.local/bin/terrapod"

if [ "$terrapod_target" != "$expected_terrapod_target" ]; then
  fail "chezmoi installs Terrapod at ~/.local/bin/terrapod; expected '$expected_terrapod_target', got '$terrapod_target'"
fi

pass "chezmoi installs Terrapod at ~/.local/bin/terrapod"

if [ ! -f "$terrapod" ]; then
  fail "Terrapod command source exists"
fi

pass "Terrapod command source exists"

if [ ! -x "$terrapod" ]; then
  fail "Terrapod command source is executable"
fi

pass "Terrapod command source is executable"

sh -n "$terrapod" || fail "Terrapod command is valid POSIX shell"
pass "Terrapod command is valid POSIX shell"

help_output="$(sh "$terrapod" help)"

assert_contains \
  "$help_output" \
  "Terrapod - Dotfiles Management Tool" \
  "Terrapod help names the Dotfiles Management Tool"

assert_contains \
  "$help_output" \
  "Usage:" \
  "Terrapod help shows usage"

assert_contains \
  "$help_output" \
  "Commands:" \
  "Terrapod help shows command list"

default_output="$(sh "$terrapod")"

assert_contains \
  "$default_output" \
  "Terrapod - Dotfiles Management Tool" \
  "Terrapod with no arguments shows help"

dash_help_output="$(sh "$terrapod" --help)"

assert_contains \
  "$dash_help_output" \
  "Terrapod - Dotfiles Management Tool" \
  "Terrapod --help shows help"

if sh "$terrapod" frobnicate >"$tmp_dir/unknown.out" 2>"$tmp_dir/unknown.err"; then
  fail "unknown Terrapod subcommands fail"
fi

unknown_error="$(cat "$tmp_dir/unknown.err")"

assert_contains \
  "$unknown_error" \
  "terrapod: unknown command: frobnicate" \
  "unknown Terrapod subcommands name the rejected command"

assert_contains \
  "$unknown_error" \
  "Run 'terrapod help' for usage." \
  "unknown Terrapod subcommands point to help"
