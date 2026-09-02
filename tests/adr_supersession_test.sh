#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
adr_dir="$repo_root/docs/adr"

# Every "ADR NNNN" an ADR names must resolve to a file, so a renumbered or
# deleted record cannot leave a dangling cross-reference behind.
for adr in "$adr_dir"/*.md; do
  adr_name="${adr##*/}"

  for referenced in $(sed -n 's/.*ADR \([0-9][0-9][0-9][0-9]\).*/\1/p' "$adr" | sort -u); do
    [ -n "$(find "$adr_dir" -name "$referenced-*.md")" ] ||
      fail "$adr_name references an existing ADR (missing: ADR $referenced)"
  done
done
pass "every ADR cross-reference resolves to an existing record"

# ADR 0008 has always recorded that it supersedes ADR 0006; this is the reverse
# direction, so a reader landing on 0006 is not left believing it is current.
adr_0006="$adr_dir/0006-use-official-installers-for-ai-cli-tools.md"
grep -F 'ADR 0008 supersedes this decision' "$adr_0006" >/dev/null ||
  fail "ADR 0006 records that ADR 0008 superseded its package source"
pass "ADR 0006 records that ADR 0008 superseded its package source"

grep -F 'ADR 0017' "$adr_0006" >/dev/null ||
  fail "ADR 0006 records the Claude Code vendor installer ADR 0017 restored"
pass "ADR 0006 records the Claude Code vendor installer ADR 0017 restored"

grep -F "ADR 0006's vendor-installer choice" "$adr_dir/0008-use-homebrew-for-optional-ai-cli-tools.md" >/dev/null ||
  fail "ADR 0008 still records the supersession ADR 0006 points back to"
pass "ADR 0008 still records the supersession ADR 0006 points back to"

# ADR 0017 moved Claude Code off the cask, so ADR 0008's upgrade guidance must
# not name `claude-code` while still covering the two casks that remain.
adr_0008="$adr_dir/0008-use-homebrew-for-optional-ai-cli-tools.md"
adr_0008_upgrade="$(grep -F 'brew upgrade --cask' "$adr_0008")"
assert_not_contains "$adr_0008_upgrade" 'claude-code' \
  "ADR 0008's upgrade command does not name the claude-code cask"
assert_contains "$adr_0008_upgrade" 'brew upgrade --cask codex antigravity-cli' \
  "ADR 0008's upgrade command still covers the codex and antigravity-cli casks"
assert_file_contains "$adr_0008" 'ADR 0017' \
  "ADR 0008 points Claude Code upgrades at the vendor installer ADR 0017 records"
