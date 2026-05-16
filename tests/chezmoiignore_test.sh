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
