# Assertion vocabulary and temporary-directory preamble shared by every
# Terrapod test file.
#
# Written in POSIX shell and sourced by both tests/*_test.sh and
# tests/*_test.zsh, so the two interpreters read one set of definitions
# instead of near-identical copies.
#
# Canonical assertion signatures:
#
#   assert_contains          <haystack> <needle> <message>
#   assert_not_contains      <haystack> <needle> <message>
#   assert_file_contains     <file>     <needle> <message>
#   assert_file_not_contains <file>     <needle> <message>
#
# The haystack forms take a string, the file forms take a path, and neither
# infers which one it was handed. That is deliberate: the copies these replaced
# disagreed about whether the first argument was text or a filename, so an
# assertion copied between test files could silently change meaning.
#
# Helper working variables carry a harness_ prefix so sourcing this file never
# shadows a name the test itself holds. The one deliberate exception is
# make_tmp_dir, which fills $tmp_dir because that is the name every test reads.

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

# Reported instead of an assertion when a case cannot hold on this platform.
# tests/run counts these lines, so a skip stays visible in a green CI log.
skip() {
  printf '%s\n' "skip - $1"
}

# Scratch directory for the test, removed however the test ends. The trap is
# armed here rather than inside make_tmp_dir because zsh runs an EXIT trap set
# inside a function as soon as that function returns.
tmp_dir=""

harness_remove_tmp_dir() {
  if [ -n "$tmp_dir" ]; then
    rm -rf "$tmp_dir"
  fi
}

trap harness_remove_tmp_dir EXIT INT TERM

make_tmp_dir() {
  tmp_dir="$(mktemp -d)"
}

# Failure diagnostics: what was looked for, then the text or file it was looked
# for in, indented so it cannot be misread as an ok/not ok line.
harness_report_text() {
  printf '%s\n' "$1" >&2
  printf '%s\n' "$2" | sed 's/^/  /' >&2
}

harness_report_file() {
  printf '%s\n' "$1" >&2
  sed 's/^/  /' "$2" >&2
}

assert_contains() {
  harness_haystack="$1"
  harness_needle="$2"
  harness_message="$3"

  if ! printf '%s\n' "$harness_haystack" | grep -F -e "$harness_needle" >/dev/null; then
    harness_report_text "missing: $harness_needle" "$harness_haystack"
    fail "$harness_message"
  fi

  pass "$harness_message"
}

assert_not_contains() {
  harness_haystack="$1"
  harness_needle="$2"
  harness_message="$3"

  if printf '%s\n' "$harness_haystack" | grep -F -e "$harness_needle" >/dev/null; then
    harness_report_text "unexpected: $harness_needle" "$harness_haystack"
    fail "$harness_message"
  fi

  pass "$harness_message"
}

assert_file_contains() {
  harness_file="$1"
  harness_needle="$2"
  harness_message="$3"

  if ! grep -F -e "$harness_needle" "$harness_file" >/dev/null; then
    harness_report_file "missing from $harness_file: $harness_needle" "$harness_file"
    fail "$harness_message"
  fi

  pass "$harness_message"
}

assert_file_not_contains() {
  harness_file="$1"
  harness_needle="$2"
  harness_message="$3"

  if grep -F -e "$harness_needle" "$harness_file" >/dev/null; then
    harness_report_file "unexpected in $harness_file: $harness_needle" "$harness_file"
    fail "$harness_message"
  fi

  pass "$harness_message"
}
