#!/bin/sh
# Runtime behavior of dot_hammerspoon/init.lua: the leader-key modal and the
# Ghostty input-source retry loop.
#
# tests/hammerspoon_config_test.sh reads the appBindings table as text. This
# file instead loads the real init.lua into a Lua interpreter with the `hs`
# global replaced by tests/fixtures/hammerspoon_hs_stub.lua, which records
# every Hammerspoon call it receives. Each scenario below drives init.lua
# through that stub (key presses, app activations, timer flushes) and asserts
# on the recorded trace.
#
# The interpreter is optional: without lua5.4, lua or luajit on PATH the whole
# file reports a single skip so tests/run stays green but says so.
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
hammerspoon_config="$repo_root/dot_hammerspoon/init.lua"
hs_stub="$repo_root/tests/fixtures/hammerspoon_hs_stub.lua"
make_tmp_dir

lua_bin=""
for candidate in lua5.4 lua luajit; do
  if command -v "$candidate" >/dev/null 2>&1; then
    lua_bin="$(command -v "$candidate")"
    break
  fi
done

if [ -z "$lua_bin" ]; then
  skip "Hammerspoon runtime behavior (needs lua5.4, lua or luajit on PATH)"
  exit 0
fi

# Writes stdin to $tmp_dir/<name>.lua, runs it with the stub and init.lua
# paths as arg[1] and arg[2], and prints its stdout. A Lua error fails the
# test here so a broken scenario never reads as a missing trace line.
run_scenario() {
  scenario_name="$1"
  scenario_file="$tmp_dir/$scenario_name.lua"
  cat >"$scenario_file"

  if ! "$lua_bin" "$scenario_file" "$hs_stub" "$hammerspoon_config" >"$tmp_dir/$scenario_name.out" 2>"$tmp_dir/$scenario_name.err"; then
    printf '%s\n' "scenario $scenario_name failed under $lua_bin:" >&2
    sed 's/^/  /' "$tmp_dir/$scenario_name.err" >&2
    fail "Hammerspoon runtime scenario $scenario_name runs to completion"
  fi

  cat "$tmp_dir/$scenario_name.out"
}

count_lines() {
  printf '%s\n' "$1" | grep -F -c -e "$2" || true
}

assert_count() {
  haystack="$1"
  needle="$2"
  expected="$3"
  message="$4"
  actual="$(count_lines "$haystack" "$needle")"

  if [ "$actual" -ne "$expected" ]; then
    printf 'expected %s occurrences of %s, found %s in:\n' "$expected" "$needle" "$actual" >&2
    printf '%s\n' "$haystack" | sed 's/^/  /' >&2
    fail "$message"
  fi

  pass "$message"
}

# Every scenario starts the same way: load the stub, load init.lua on top of
# it, then discard the load-time trace so assertions see only what the
# scenario itself triggers.
scenario_preamble='
local harness = dofile(arg[1])
dofile(arg[2])
local loadTrace = harness.trace
harness.trace = {}
'

# --- Load-time side effects ---------------------------------------------------

load_trace="$(run_scenario load <<EOF
$scenario_preamble
for _, line in ipairs(loadTrace) do
  print(line)
end
EOF
)"

assert_contains "$load_trace" "hotkey.bind none f18" "init.lua binds the leader key on load"
assert_contains "$load_trace" "hotkey.bind alt+cmd+ctrl+shift t" "init.lua binds the hyper chord for Ghostty on load"
assert_contains "$load_trace" "modal.bind none t" "init.lua binds the Ghostty key inside the leader modal on load"
assert_contains "$load_trace" "application.watcher.start" "init.lua starts the application watcher on load"
assert_contains "$load_trace" "window.filter.subscribe windowFocused" "init.lua subscribes to Ghostty window focus on load"
assert_not_contains "$load_trace" "modal.enter" "loading init.lua does not enter the leader modal"
assert_not_contains "$load_trace" "timer.doAfter" "loading init.lua does not schedule an input-source retry"

# --- Leader-key modal -----------------------------------------------------------

modal_trace="$(run_scenario modal_launch <<EOF
$scenario_preamble
harness.addApp("com.mitchellh.ghostty")
harness.press("f18")
harness.press("t")
harness.release("f18")
harness.printTrace()
EOF
)"

cat >"$tmp_dir/modal_launch.expected" <<'TRACE'
press none f18
modal.enter
press none t (modal)
app.activate com.mitchellh.ghostty
modal.exit
release none f18
TRACE

if ! diff -u "$tmp_dir/modal_launch.expected" "$tmp_dir/modal_launch.out" >"$tmp_dir/modal_launch.diff"; then
  cat "$tmp_dir/modal_launch.diff" >&2
  fail "holding the leader and pressing an app key enters the modal, focuses the app and exits the modal"
fi
pass "holding the leader and pressing an app key enters the modal, focuses the app and exits the modal"

assert_count "$modal_trace" "modal.exit" 1 "releasing the leader after an app key does not exit the modal a second time"

modal_release_trace="$(run_scenario modal_release <<EOF
$scenario_preamble
harness.press("f18")
harness.press("f18")
harness.release("f18")
harness.release("f18")
harness.printTrace()
EOF
)"

assert_count "$modal_release_trace" "modal.enter" 1 "pressing the leader while the modal is active does not enter it again"
assert_count "$modal_release_trace" "modal.exit" 1 "releasing the leader without an app key exits the modal once"

modal_launch_missing_trace="$(run_scenario modal_launch_missing <<EOF
$scenario_preamble
harness.press("f18")
harness.press("s")
harness.printTrace()
EOF
)"

assert_contains "$modal_launch_missing_trace" "application.launchOrFocusByBundleID com.tinyspeck.slackmacgap" "an app key for an app that is not running launches it by bundle ID"
assert_contains "$modal_launch_missing_trace" "alert.show Could not launch Slack" "a failed launch shows an alert naming the app"
assert_contains "$modal_launch_missing_trace" "modal.exit" "the modal exits after a failed launch"

modal_hidden_trace="$(run_scenario modal_hidden <<EOF
$scenario_preamble
harness.addApp("notion.id", { hidden = true })
harness.press("f18")
harness.press("n")
harness.printTrace()
EOF
)"

assert_contains "$modal_hidden_trace" "app.unhide notion.id" "an app key for a hidden app unhides it"
assert_contains "$modal_hidden_trace" "app.activate notion.id" "an app key for a hidden app activates it"

modal_inert_trace="$(run_scenario modal_inert <<EOF
$scenario_preamble
local bound, err = pcall(harness.press, "t")
print(bound and "bound" or "unbound: " .. tostring(err))
harness.printTrace()
EOF
)"

assert_contains "$modal_inert_trace" "unbound: " "an app key without the leader held is not bound"
assert_not_contains "$modal_inert_trace" "app.activate" "an app key without the leader held launches nothing"

hyper_trace="$(run_scenario hyper <<EOF
$scenario_preamble
harness.addApp("com.mitchellh.ghostty")
harness.press("t", { "ctrl", "alt", "cmd", "shift" })
harness.printTrace()
EOF
)"

assert_contains "$hyper_trace" "app.activate com.mitchellh.ghostty" "the hyper chord focuses the app without the leader"
assert_not_contains "$hyper_trace" "modal.enter" "the hyper chord does not enter the leader modal"

# --- Ghostty input-source retry -------------------------------------------------

# The stub's input source starts on a Korean layout, so every retry scenario
# begins with a switch pending. Both switch calls are made to fail here.
retry_failing_trace="$(run_scenario retry_failing <<EOF
$scenario_preamble
harness.setSourceSucceeds = false
harness.setLayoutSucceeds = false
local ghostty = harness.addApp("com.mitchellh.ghostty")
harness.activateApp(ghostty)
harness.flushTimers()
print("pending timers: " .. harness.pendingTimers())
harness.printTrace()
EOF
)"

cat >"$tmp_dir/retry_failing.expected" <<'TRACE'
timer.doAfter 0.00
timer.doAfter 0.08
timer.doAfter 0.18
timer.doAfter 0.35
TRACE

grep '^timer.doAfter' "$tmp_dir/retry_failing.out" >"$tmp_dir/retry_failing.timers" || true
if ! diff -u "$tmp_dir/retry_failing.expected" "$tmp_dir/retry_failing.timers" >"$tmp_dir/retry_failing.diff"; then
  cat "$tmp_dir/retry_failing.diff" >&2
  fail "activating Ghostty schedules one retry per configured delay and nothing more"
fi
pass "activating Ghostty schedules one retry per configured delay and nothing more"

assert_count "$retry_failing_trace" "keycodes.currentSourceID set com.apple.keylayout.ABC" 4 "a switch that keeps failing is requested once per retry"
assert_count "$retry_failing_trace" "keycodes.setLayout ABC" 4 "a failed source switch falls back to the ABC layout on every retry"
assert_contains "$retry_failing_trace" "pending timers: 0" "a switch that keeps failing stops after the last retry"

retry_success_trace="$(run_scenario retry_success <<EOF
$scenario_preamble
local ghostty = harness.addApp("com.mitchellh.ghostty")
harness.activateApp(ghostty)
harness.flushTimers()
print("pending timers: " .. harness.pendingTimers())
harness.printTrace()
EOF
)"

assert_count "$retry_success_trace" "keycodes.currentSourceID set com.apple.keylayout.ABC" 1 "a switch that succeeds is requested once"
assert_not_contains "$retry_success_trace" "keycodes.setLayout" "a successful source switch does not fall back to the ABC layout"
assert_count "$retry_success_trace" "timer.fire" 4 "later retries still fire after a successful switch"
assert_contains "$retry_success_trace" "pending timers: 0" "no retry is left pending after a successful switch"

retry_fallback_trace="$(run_scenario retry_fallback <<EOF
$scenario_preamble
harness.setSourceSucceeds = false
local ghostty = harness.addApp("com.mitchellh.ghostty")
harness.activateApp(ghostty)
harness.flushTimers()
harness.printTrace()
EOF
)"

assert_count "$retry_fallback_trace" "keycodes.currentSourceID set com.apple.keylayout.ABC" 1 "a switch whose fallback succeeds is requested once"
assert_count "$retry_fallback_trace" "keycodes.setLayout ABC" 1 "the ABC layout fallback is requested once when it succeeds"

retry_already_english_trace="$(run_scenario retry_already_english <<EOF
$scenario_preamble
harness.currentSourceID = "com.apple.keylayout.ABC"
local ghostty = harness.addApp("com.mitchellh.ghostty")
harness.activateApp(ghostty)
harness.flushTimers()
harness.printTrace()
EOF
)"

assert_not_contains "$retry_already_english_trace" "keycodes.currentSourceID set" "no switch is requested when Ghostty is already on ABC"
assert_not_contains "$retry_already_english_trace" "keycodes.setLayout" "no fallback is requested when Ghostty is already on ABC"

retry_other_app_trace="$(run_scenario retry_other_app <<EOF
$scenario_preamble
harness.setSourceSucceeds = false
harness.setLayoutSucceeds = false
local ghostty = harness.addApp("com.mitchellh.ghostty")
local slack = harness.addApp("com.tinyspeck.slackmacgap")
harness.activateApp(ghostty)
harness.activateApp(slack)
harness.flushTimers()
print("pending timers: " .. harness.pendingTimers())
harness.printTrace()
EOF
)"

assert_count "$retry_other_app_trace" "timer.stop" 4 "activating another app cancels every pending Ghostty retry"
assert_not_contains "$retry_other_app_trace" "keycodes.currentSourceID set" "no switch is requested once another app is activated"
assert_contains "$retry_other_app_trace" "pending timers: 0" "no retry is left pending once another app is activated"

retry_focus_lost_trace="$(run_scenario retry_focus_lost <<EOF
$scenario_preamble
harness.setSourceSucceeds = false
harness.setLayoutSucceeds = false
local ghostty = harness.addApp("com.mitchellh.ghostty")
local slack = harness.addApp("com.tinyspeck.slackmacgap")
harness.activateApp(ghostty)
harness.flushTimers(1)
harness.frontmost = slack
harness.flushTimers()
print("pending timers: " .. harness.pendingTimers())
harness.printTrace()
EOF
)"

assert_count "$retry_focus_lost_trace" "keycodes.currentSourceID set com.apple.keylayout.ABC" 1 "a retry that finds Ghostty no longer frontmost requests no switch"
assert_count "$retry_focus_lost_trace" "timer.stop" 2 "a retry that finds Ghostty no longer frontmost cancels the remaining retries"
assert_contains "$retry_focus_lost_trace" "pending timers: 0" "no retry is left pending once Ghostty loses focus mid-way"

retry_refocus_trace="$(run_scenario retry_refocus <<EOF
$scenario_preamble
harness.setSourceSucceeds = false
harness.setLayoutSucceeds = false
local ghostty = harness.addApp("com.mitchellh.ghostty")
harness.activateApp(ghostty)
harness.focusWindow("windowFocused")
harness.flushTimers()
harness.printTrace()
EOF
)"

assert_count "$retry_refocus_trace" "timer.doAfter" 8 "a Ghostty window focus after activation schedules a fresh set of retries"
assert_count "$retry_refocus_trace" "timer.stop" 4 "a Ghostty window focus after activation cancels the earlier retries"
assert_count "$retry_refocus_trace" "keycodes.currentSourceID set com.apple.keylayout.ABC" 4 "overlapping Ghostty focus events never double the retry count"
