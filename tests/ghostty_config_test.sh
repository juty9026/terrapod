#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
ghostty_config="$repo_root/dot_config/ghostty/config"

features_line="$(
  sed -n 's/^shell-integration-features[[:space:]]*=[[:space:]]*//p' "$ghostty_config" |
    tail -n 1
)"

if [ -z "$features_line" ]; then
  fail "Ghostty shell integration features should be configured"
fi

case ",$features_line," in
  *,ssh-env,*) pass "Ghostty SSH sessions fall back to a compatible TERM when needed" ;;
  *) fail "Ghostty shell integration should enable ssh-env" ;;
esac

case ",$features_line," in
  *,ssh-terminfo,*) pass "Ghostty SSH sessions install xterm-ghostty terminfo when possible" ;;
  *) fail "Ghostty shell integration should enable ssh-terminfo" ;;
esac
