#!/bin/sh
# Tracked text files are written in English (see the Language section of
# AGENTS.md). This test fails when any tracked file contains Korean text and
# names every offending file and line.
#
# It looks for Hangul only. It is not an ASCII rule: emoji, symbols, and Latin
# diacritics stay allowed. The Korean samples below are produced from octal
# escapes, so this file holds no Hangul and needs no exemption from its own
# scan.
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

# Prints "path:line: text" for every tracked file under the Git work tree $1
# that contains Hangul, and returns 1 when it found any. Files with a NUL byte
# are binary and skipped, as are tracked paths that are not regular files
# (symlinks, submodules) or that are missing from the work tree. Outside a Git
# work tree there is no tracked file list to trust, so the scan returns 2
# rather than passing over an empty one.
scan_tracked_korean() {
  if ! git -C "$1" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    printf '%s\n' "$1 is not a Git work tree; cannot list tracked files"
    return 2
  fi

  git -C "$1" ls-files -z | python3 -c '
import os
import re
import sys

root = sys.argv[1]
# Hangul Jamo, Compatibility Jamo, the parenthesized and circled Hangul in
# Enclosed CJK, Jamo Extended-A, Syllables, Jamo Extended-B, and the halfwidth
# Hangul forms.
hangul = re.compile(
    "[\u1100-\u11ff\u3130-\u318f\u3200-\u321e\u3260-\u327e\ua960-\ua97f\uac00-\ud7ff\uffa0-\uffdc]"
)

found = False
for path in sys.stdin.buffer.read().split(b"\0"):
    if not path:
        continue
    name = path.decode("utf-8", "replace")
    full = os.path.join(os.fsencode(root), path)
    if os.path.islink(full) or not os.path.isfile(full):
        continue
    with open(full, "rb") as handle:
        data = handle.read()
    if b"\0" in data:
        continue
    text = data.decode("utf-8", "replace")
    for number, line in enumerate(text.split("\n"), 1):
        if hangul.search(line):
            print(f"{name}:{number}: {line.strip()}")
            found = True

sys.exit(1 if found else 0)
' "$1"
}

new_fixture_repo() {
  mkdir -p "$tmp_dir/$1"
  git -C "$tmp_dir/$1" init --quiet
}

# A tracked file with Korean text fails the scan and is named in the report;
# an untracked one is outside the scan.
new_fixture_repo korean
# "Hangul" spelled in Hangul syllables (U+D55C U+AE00).
printf 'Status: \355\225\234\352\270\200\n' >"$tmp_dir/korean/notes.md"
printf 'Plain English.\n' >"$tmp_dir/korean/clean.md"
printf '\355\225\234\n' >"$tmp_dir/korean/untracked.md"
git -C "$tmp_dir/korean" add notes.md clean.md

status=0
report="$(scan_tracked_korean "$tmp_dir/korean")" || status=$?
assert_status "$status" 1 "a tracked file with Korean text fails the language check"
assert_contains "$report" "notes.md:1:" "the language check names the offending file and line"
assert_not_contains "$report" "clean.md" "the language check does not report English-only files"
assert_not_contains "$report" "untracked.md" "the language check scans tracked files only"

# A lone Compatibility Jamo (U+314E) is Korean text as well.
new_fixture_repo jamo
printf 'Key: \343\205\216\n' >"$tmp_dir/jamo/key.txt"
git -C "$tmp_dir/jamo" add key.txt

status=0
report="$(scan_tracked_korean "$tmp_dir/jamo")" || status=$?
assert_status "$status" 1 "a tracked file with a lone Hangul jamo fails the language check"
assert_contains "$report" "key.txt:1:" "the language check names the file holding the jamo"

# Emoji, symbols, and Latin diacritics are legitimate non-ASCII content.
new_fixture_repo symbols
# U+1F310 globe, U+2139 U+FE0F information, U+2192 arrow, U+2014 em dash,
# U+00E9 e-acute, U+00FC u-umlaut.
printf '\360\237\214\220 Language \342\204\271\357\270\217 \342\206\222 caf\303\251 \342\200\224 f\303\274r\n' \
  >"$tmp_dir/symbols/symbols.md"
# A binary file is skipped even if its bytes happen to decode as Hangul.
printf '\000\355\225\234' >"$tmp_dir/symbols/blob.bin"
git -C "$tmp_dir/symbols" add symbols.md blob.bin

status=0
report="$(scan_tracked_korean "$tmp_dir/symbols")" || status=$?
assert_status "$status" 0 "emoji, symbols, diacritics, and binary files pass the language check"

# Without a Git work tree the scan cannot pass by default.
mkdir -p "$tmp_dir/not-a-repo"
status=0
report="$(
  export GIT_CEILING_DIRECTORIES="$tmp_dir"
  scan_tracked_korean "$tmp_dir/not-a-repo"
)" || status=$?
assert_status "$status" 2 "the language check refuses a directory that is not a Git work tree"

status=0
report="$(scan_tracked_korean "$repo_root")" || status=$?
if [ "$status" -ne 0 ]; then
  harness_report_text "tracked files with Korean text:" "$report"
fi
assert_status "$status" 0 "no tracked text file contains Korean"
