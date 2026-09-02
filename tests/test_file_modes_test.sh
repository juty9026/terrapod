#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"

# Every test file carries a shebang and is meant to run directly as well as
# through tests/run, so it has to hold the executable bit. The harness is
# sourced rather than executed and deliberately does not.
for test_file in "$repo_root"/tests/*_test.sh "$repo_root"/tests/*_test.zsh; do
  [ -f "$test_file" ] || continue
  if [ ! -x "$test_file" ]; then
    fail "${test_file##*/} is a test file and should be executable (chmod +x)"
  fi
done
pass "every tests/*_test.sh and tests/*_test.zsh file is executable"

for runner in "$repo_root/tests/run" "$repo_root/tests/homebrew_ubuntu_smoke.sh"; do
  if [ ! -x "$runner" ]; then
    fail "${runner##*/} is invoked directly and should be executable (chmod +x)"
  fi
done
pass "tests/run and tests/homebrew_ubuntu_smoke.sh are executable"

if [ -x "$repo_root/tests/lib/harness.sh" ]; then
  fail "tests/lib/harness.sh is sourced, not executed, and should not be executable"
fi
pass "tests/lib/harness.sh stays a source-only file"
