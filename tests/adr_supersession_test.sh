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

# Supersession is recorded in prose on both sides: the superseding ADR says
# "This decision supersedes ADR NNNN's ..." and the superseded one answers
# "ADR MMMM supersedes this decision's ...". Every sentence that speaks of
# supersession therefore has to be matched by a mention of this ADR in each
# other ADR that sentence names, whichever side of the link it sits on.
#
# Sentences are cut at ". " after the file is flattened to one line, so a
# claim wrapped over several lines is still read as one sentence.
supersession_links() {
  tr '\n' ' ' <"$1" | awk '
    {
      count = split($0, sentences, /\. /)
      for (i = 1; i <= count; i++) {
        sentence = sentences[i]
        if (sentence !~ /supersed/) continue
        while (match(sentence, /ADR [0-9][0-9][0-9][0-9]/)) {
          print substr(sentence, RSTART + 4, 4)
          sentence = substr(sentence, RSTART + RLENGTH)
        }
      }
    }'
}

link_count=0
for adr in "$adr_dir"/*.md; do
  adr_name="${adr##*/}"
  adr_number="${adr_name%%-*}"

  for other in $(supersession_links "$adr" | sort -u); do
    [ "$other" != "$adr_number" ] || continue
    link_count=$((link_count + 1))
    other_adr="$(find "$adr_dir" -name "$other-*.md")"

    grep -F "ADR $adr_number" "$other_adr" >/dev/null ||
      fail "ADR $other names ADR $adr_number back ($adr_name links them in a supersession sentence)"
    pass "ADR $other names ADR $adr_number back"
  done
done

[ "$link_count" -gt 0 ] ||
  fail "at least one supersession sentence was found (the sentence scan is not silently empty)"
pass "at least one supersession sentence was found"

# ADR 0017 supersedes ADR 0008, not ADR 0006, so the invariant above does not
# reach this: a reader of 0006 still has to learn that a vendor installer came
# back for Claude Code.
assert_file_contains "$adr_dir/0006-use-official-installers-for-ai-cli-tools.md" 'ADR 0017' \
  "ADR 0006 records the Claude Code vendor installer ADR 0017 restored"

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
