#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir

make_case_dir example >/dev/null
assert_file_exists "$case_bin" "case setup creates an isolated bin directory"
assert_file_exists "$case_home" "case setup creates an isolated home directory"
assert_file_exists "$case_xdg_config_home" "case setup creates an isolated XDG config directory"
assert_file_exists "$case_xdg_data_home" "case setup creates an isolated XDG data directory"
[ "$case_call_log" = "$case_dir/calls.log" ] || fail "case setup publishes the common call log path"
pass "case setup publishes the common call log path"

write_stub "$case_bin/arguments" 'for arg do printf "%s\n" "$arg"; done' 'printf "%s\n" problem >&2' 'exit 23'
run_case_command "$case_bin/arguments" 'one two' three
assert_status "$case_status" 23 "case runner preserves a failing exit status under set -e"
assert_file_contains "$case_stdout_file" 'one two' "case runner captures stdout separately"
assert_file_contains "$case_stderr_file" problem "case runner captures stderr separately"

first_case_home="$case_home"
first_case_log="$case_call_log"
make_case_dir second >/dev/null
[ "$case_home" != "$first_case_home" ] || fail "case setup isolates HOME between cases"
pass "case setup isolates HOME between cases"
[ "$case_call_log" != "$first_case_log" ] || fail "case setup isolates call logs between cases"
pass "case setup isolates call logs between cases"
[ ! -s "$case_call_log" ] || fail "a new case starts with an empty call log"
pass "a new case starts with an empty call log"

capture_case_command "$case_dir" input sh -c 'read value; printf "out:%s\n" "$value"; printf "err\n" >&2; exit 17'
assert_status "$case_status" 17 "input case runner preserves the command status"
assert_equals "$case_stdout" out:input "input case runner exposes captured stdout"
assert_equals "$case_stderr" err "input case runner exposes captured stderr"

write_uname_stub "$case_bin/uname" Linux aarch64
assert_equals "$("$case_bin/uname" -s)" Linux "uname fake reports the configured kernel"
assert_equals "$("$case_bin/uname" -m)" aarch64 "uname fake reports the configured machine"

bundle="$case_dir/Brewfile"
printf '%s\n' 'brew "plain"' 'brew "optioned", args: ["with-feature"]' 'brew "plain-extra"' 'cask "desktop", greedy: true' >"$bundle"
write_brew_bundle_stub "$case_bin/brew"
MACOS_BREW_LOG="$case_call_log" MACOS_BREW_FAIL_FORMULAE=optioned "$case_bin/brew" bundle --file="$bundle" >/dev/null 2>&1 &&
  fail "brew fake matches an optioned formula"
pass "brew fake matches an optioned formula"
MACOS_BREW_LOG="$case_call_log" MACOS_BREW_FAIL_FORMULAE=plain "$case_bin/brew" bundle --file="$bundle" >/dev/null 2>&1 &&
  fail "brew fake matches a plain formula"
pass "brew fake matches a plain formula"
MACOS_BREW_LOG="$case_call_log" MACOS_BREW_FAIL_FORMULAE=missing "$case_bin/brew" bundle --file="$bundle"
pass "brew fake does not match another package"
MACOS_BREW_LOG="$case_call_log" MACOS_BREW_FAIL_CASKS=desktop "$case_bin/brew" bundle --file="$bundle" >/dev/null 2>&1 &&
  fail "brew fake matches an optioned cask"
pass "brew fake matches an optioned cask"

responses="$case_dir/gum.responses"
gum_log="$case_dir/gum.log"
write_gum_stub "$case_bin/gum" cancel default
write_gum_responses "$responses" ''
TERRAPOD_GUM_LOG="$gum_log" TERRAPOD_GUM_RESPONSES="$responses" "$case_bin/gum" choose item >/dev/null 2>&1 &&
  fail "gum fake applies the configured empty choose cancellation policy"
[ "$?" -eq 130 ] || fail "empty choose response exits 130"
pass "gum fake applies the configured empty choose cancellation policy"
write_gum_responses "$responses" ''
TERRAPOD_GUM_LOG="$gum_log" TERRAPOD_GUM_RESPONSES="$responses" "$case_bin/gum" confirm --default=true
pass "gum fake applies the confirm default for an empty response"
write_gum_stub "$case_bin/gum-cancel" cancel cancel
write_gum_responses "$responses" ''
TERRAPOD_GUM_LOG="$gum_log" TERRAPOD_GUM_RESPONSES="$responses" "$case_bin/gum-cancel" confirm --default=true >/dev/null 2>&1 &&
  fail "gum fake applies the configured empty confirm cancellation policy"
[ "$?" -eq 130 ] || fail "empty confirm cancellation exits 130"
pass "gum fake applies the configured empty confirm cancellation policy"
write_gum_responses "$responses" __ERROR__
TERRAPOD_GUM_LOG="$gum_log" TERRAPOD_GUM_RESPONSES="$responses" "$case_bin/gum" confirm >/dev/null 2>&1 &&
  fail "gum fake exposes operational errors"
[ "$?" -eq 2 ] || fail "gum operational error exits 2"
pass "gum fake exposes operational errors"

assert_call_log_contains "$case_call_log" 'brew args:bundle' "common call log assertion observes command arguments"
