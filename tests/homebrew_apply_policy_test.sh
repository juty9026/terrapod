#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
cd "$repo_root"

if ! awk '
  FILENAME == "dot_local/lib/terrapod/homebrew-install.sh" { next }
  {
    command = $0
    sub(/^[[:space:]]*/, "", command)
    while (sub(/^(if|elif|then|else|command|exec|env|!|[[:alpha:]_][[:alnum:]_]*=[^[:space:]]*)[[:space:]]+/, "", command)) {}
    if (command ~ /^"?(brew|\/[^[:space:]"]*\/brew|\$\{?[[:alnum:]_]*brew[[:alnum:]_]*\}?)"?[[:space:]]+(install|bundle)([[:space:];]|$)/) {
      printf "%s:%d: use terrapod_homebrew_install from homebrew-install.sh\n", FILENAME, FNR
      failed = 1
    }
  }
  END { exit failed }
' .chezmoiscripts/*.sh.tmpl dot_local/lib/terrapod/*.sh; then
  fail "tpod apply routes Homebrew package installation through the shared policy"
fi
pass "tpod apply routes Homebrew package installation through the shared policy"
