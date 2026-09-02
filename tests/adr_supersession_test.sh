#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
adr_dir="$repo_root/docs/adr"

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

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
