#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

managed_source_paths() {
  data="$1"
  chezmoi \
    --source "$repo_root" \
    --override-data "$data" \
    managed \
    --path-style source-relative
}

managed_tests="$(
  chezmoi \
    --source "$repo_root" \
    managed \
    --path-style source-relative |
    grep '^tests/' || true
)"

if [ -n "$managed_tests" ]; then
  fail "development tests should not be managed by chezmoi: $managed_tests"
fi

pass "development tests are ignored by chezmoi"

ubuntu_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}'
ubuntu_managed="$(managed_source_paths "$ubuntu_data")"

macos_only_entries="
.chezmoiscripts/run_onchange_after_40-remove-legacy-npm-ai-tools.sh.tmpl
.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl
.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl
dot_config/cmux
dot_config/ghostty
dot_config/private_karabiner
dot_hammerspoon
dot_zprofile
"

for entry in $macos_only_entries; do
  if printf '%s\n' "$ubuntu_managed" | grep -Fx "$entry" >/dev/null; then
    fail "Ubuntu VPS should not manage macOS-only entry: $entry"
  fi
done

pass "Ubuntu VPS ignores macOS-only entries"
