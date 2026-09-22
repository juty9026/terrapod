#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"

# Unit tests for the install warning policy layer, install-warning-script.sh.
# Each case runs a script body under `set -eu`, the way the chezmoi scripts do,
# with both libraries loaded, so a terminal call's exit is observed as the exit
# status of a real shell rather than inferred from the code.
warnings_lib="$repo_root/dot_local/lib/terrapod/install-warnings.sh"
policy_lib="$repo_root/dot_local/lib/terrapod/install-warning-script.sh"

make_tmp_dir

sh -n "$policy_lib" || fail "install warning policy layer is valid POSIX shell"
pass "install warning policy layer is valid POSIX shell"

case_number=0

# Runs $1 as a script body. Sets $case_state (the XDG state dir the body wrote
# into), $case_status, $case_stdout and $case_stderr.
run_case_with_marker_state() {
  case_body="$1"
  marker_state="$2"
  case_number=$((case_number + 1))
  case_home="$tmp_dir/case-$case_number-home"
  case_state="$tmp_dir/case-$case_number-state"
  mkdir -p "$case_home"

  if [ "$marker_state" = unwritable ]; then
    : >"$case_state"
  fi

  case_status=0
  HOME="$case_home" XDG_STATE_HOME="$case_state" sh -c \
    'set -eu; . "$1"; . "$2"; '"$case_body" \
    sh "$warnings_lib" "$policy_lib" \
    >"$tmp_dir/case-$case_number.out" 2>"$tmp_dir/case-$case_number.err" || case_status=$?
  case_stdout="$(cat "$tmp_dir/case-$case_number.out")"
  case_stderr="$(cat "$tmp_dir/case-$case_number.err")"
}

run_case() {
  run_case_with_marker_state "$1" writable
}

# Same as run_case, but $case_state's parent is a regular file, so the marker
# directory cannot be created and no marker can be written.
run_case_with_unwritable_marker_dir() {
  run_case_with_marker_state "$1" unwritable
}

marker_file() {
  printf '%s\n' "$case_state/terrapod/install-warnings/$1"
}

marker_value() {
  HOME="$case_home" XDG_STATE_HOME="$case_state" sh -c \
    '. "$1"; terrapod_install_warning_value "$2" "$3"' \
    sh "$warnings_lib" "$1" "$2"
}

assert_status() {
  [ "$case_status" -eq "$1" ] || fail "$2 (expected exit $1, got $case_status; stderr: $case_stderr)"
  pass "$2"
}

# --- Accumulate, then finish with failures -----------------------------------

run_case '
declare_install_warning_category mise-tools "mise tool install needs attention"
note_failed_install_warning_item "mise install"
echo after-note
note_failed_install_warning_item "corepack enable"
finish_install_warning_category "Failed step(s): %s. Rerun tpod apply."
echo unreachable
'
assert_status 0 "finishing a category with failed items exits 0 once the marker is recorded"
assert_contains "$case_stdout" "after-note" "noting a failed item does not stop the script"
assert_not_contains "$case_stdout" "unreachable" "finishing a category is terminal"
[ -f "$(marker_file mise-tools)" ] || fail "finishing with failures records the marker"
pass "finishing with failures records the marker"
assert_contains "$(marker_value mise-tools summary)" "mise tool install needs attention" "the marker carries the declared summary"
assert_contains "$(marker_value mise-tools guidance)" "Failed step(s): mise install, corepack enable. Rerun tpod apply." \
  "the guidance format receives the failed names joined with a comma and a space"

# A `%` in an item name is data, not part of the guidance format.
run_case '
declare_install_warning_category gh-extensions "GitHub CLI Extension Set install needs attention"
note_failed_install_warning_item "owner/100%s-repo"
finish_install_warning_category "Failed to install: %s. Rerun."
'
assert_contains "$(marker_value gh-extensions guidance)" "Failed to install: owner/100%s-repo. Rerun." \
  "a failed item name is never read as a printf format"

# --- Finish with no failures --------------------------------------------------

run_case '
declare_install_warning_category mise-tools "mise tool install needs attention"
terrapod_install_warning_write mise-tools "mise tool install needs attention" "stale"
finish_install_warning_category "Failed step(s): %s."
echo unreachable
'
assert_status 0 "finishing a category with no failed items exits 0"
assert_not_contains "$case_stdout" "unreachable" "finishing a successful category is terminal"
[ ! -e "$(marker_file mise-tools)" ] || fail "finishing with no failures clears the stale marker"
pass "finishing with no failures clears the stale marker"

# Nothing failed, so the guidance format is not needed.
run_case '
declare_install_warning_category jetendard-settings "Jetendard app settings need attention"
finish_install_warning_category
'
assert_status 0 "finishing a category with no failed items needs no guidance format"

# --- Fail the category now ----------------------------------------------------

run_case '
declare_install_warning_category mise-tools "mise tool install needs attention"
note_failed_install_warning_item "ignored once the category fails now"
fail_install_warning_category "Install the mandatory Homebrew core bundle, then rerun tpod apply."
echo unreachable
'
assert_status 0 "failing a category exits 0 once the marker is recorded"
assert_not_contains "$case_stdout" "unreachable" "failing a category is terminal"
assert_contains "$(marker_value mise-tools guidance)" "Install the mandatory Homebrew core bundle, then rerun tpod apply." \
  "failing a category records the guidance as given"
assert_not_contains "$(marker_value mise-tools guidance)" "ignored once" \
  "failing a category does not append accumulated failed items"

# Declaring again replaces the summary, which is how one script records two
# summaries for its one category.
run_case '
declare_install_warning_category jetendard-settings "Jetendard app settings need attention"
declare_install_warning_category jetendard-settings "Jetendard Orca setting is deferred"
fail_install_warning_category "Quit Orca, then rerun tpod apply."
'
assert_contains "$(marker_value jetendard-settings summary)" "Jetendard Orca setting is deferred" \
  "declaring the category again replaces its summary"

# --- The marker cannot be written --------------------------------------------

run_case_with_unwritable_marker_dir '
declare_install_warning_category mise-tools "mise tool install needs attention"
fail_install_warning_category "Rerun tpod apply."
echo unreachable
'
assert_status 1 "failing a category exits 1 when the marker cannot be written"
assert_not_contains "$case_stdout" "unreachable" "a failed marker write still ends the script"

run_case_with_unwritable_marker_dir '
declare_install_warning_category mise-tools "mise tool install needs attention"
note_failed_install_warning_item "mise install"
finish_install_warning_category "Failed step(s): %s."
echo unreachable
'
assert_status 1 "finishing a category with failures exits 1 when the marker cannot be written"

run_case '
declare_install_warning_category not-a-registered-category "Never registered"
fail_install_warning_category "Rerun tpod apply."
'
assert_status 1 "failing an unregistered category exits 1 because no marker can exist for it"

run_case '
declare_install_warning_category not-a-registered-category "Never registered"
note_failed_install_warning_item "thing"
finish_install_warning_category "Failed: %s."
'
assert_status 1 "finishing an unregistered category with failures exits 1"

# --- The marker cannot be cleared --------------------------------------------

run_case '
declare_install_warning_category mise-tools "mise tool install needs attention"
terrapod_install_warning_clear() { return 1; }
finish_install_warning_category "Failed step(s): %s."
echo unreachable
'
assert_status 0 "finishing a successful category exits 0 when the marker cannot be cleared"
assert_not_contains "$case_stdout" "unreachable" "finishing a category is terminal even when the clear fails"

# --- Cleanup traps still run --------------------------------------------------

for terminal_call in \
  'finish_install_warning_category "Failed: %s."' \
  'fail_install_warning_category "Rerun tpod apply."'
do
  run_case '
declare_install_warning_category mise-tools "mise tool install needs attention"
note_failed_install_warning_item "mise install"
trap '"'"'echo cleanup-ran'"'"' EXIT
'"$terminal_call"'
'
  assert_status 0 "a script with an EXIT trap exits 0 through: $terminal_call"
  assert_contains "$case_stdout" "cleanup-ran" "the script's EXIT trap runs when the helper exits: $terminal_call"
done

run_case_with_unwritable_marker_dir '
declare_install_warning_category mise-tools "mise tool install needs attention"
trap '"'"'echo cleanup-ran'"'"' EXIT
fail_install_warning_category "Rerun tpod apply."
'
assert_status 1 "an EXIT trap does not turn a failed marker write into success"
assert_contains "$case_stdout" "cleanup-ran" "the script's EXIT trap runs when the marker cannot be written"

# --- The low-level calls keep their behavior ---------------------------------

run_case_with_unwritable_marker_dir '
mark_install_warning mise-tools "mise tool install needs attention" "Rerun."
echo survived
if install_warning_recorded; then echo recorded; else echo not-recorded; fi
'
assert_status 0 "mark_install_warning never returns non-zero, even when the marker cannot be written"
assert_contains "$case_stdout" "not-recorded" "install_warning_recorded reports an unwritten marker"

run_case '
mark_install_warning mise-tools "mise tool install needs attention" "Rerun."
if install_warning_recorded; then echo recorded; else echo not-recorded; fi
terrapod_install_warning_clear() { return 1; }
clear_install_warning mise-tools
echo survived-failed-clear
'
assert_contains "$case_stdout" "recorded" "install_warning_recorded reports a written marker"
assert_contains "$case_stdout" "survived-failed-clear" "clear_install_warning swallows a clear failure"
