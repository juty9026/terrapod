#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"

helper="$repo_root/dot_local/lib/terrapod/executable_headless-services"
stub_bin="$tmp_dir/bin"
mkdir -p "$stub_bin"
cat >"$stub_bin/systemctl" <<'PY'
#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys

home = Path.home()
args = sys.argv[1:]
with (home / "systemctl.log").open("a") as log:
    log.write(" ".join(args) + "\n")
if os.environ.get("BUS_UNAVAILABLE"):
    sys.exit(1)
manager = "user" if "--user" in args else "system"
args = [arg for arg in args if arg != "--user"]
state_path = home / "state.json"
states = json.loads(state_path.read_text()) if state_path.exists() else {}
if args[0] == "show":
    unit = args[1]
    key = manager + ":" + unit
    target = home / ".config/systemd/user" / unit
    properties = states.get(key, {})
    prop = args[2].split("=", 1)[1]
    defaults = {"LoadState": "loaded" if manager == "user" and target.exists() else "not-found",
                "FragmentPath": str(target), "ActiveState": "inactive"}
    print(properties.get(prop, defaults[prop]))
elif args[0] == "start":
    states["user:" + args[1]] = {"ActiveState": "active"}
    state_path.write_text(json.dumps(states))
elif args[0] not in ("daemon-reload", "enable"):
    sys.exit(2)
PY
cat >"$stub_bin/loginctl" <<'SH'
#!/bin/sh
printf '%s\n' "${LINGER:-yes}"
SH
for command in orca-ide grok-bot xvfb-run xauth; do
  cat >"$stub_bin/$command" <<'PY'
#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys
Path.home().joinpath("argv.json").write_text(json.dumps(sys.argv))
sys.exit(int(os.environ.get("APP_EXIT", "0")))
PY
done
chmod 755 "$stub_bin"/*

new_case() {
  case_home="$tmp_dir/$1"
  mkdir -p "$case_home/.config/terrapod"
  config="$case_home/.config/terrapod/headless-services.json"
  unit_dir="$case_home/.config/systemd/user"
}

run_helper() {
  status=0
  HOME="$case_home" PATH="$stub_bin:$PATH" python3 "$helper" "$@" >"$case_home/stdout" 2>"$case_home/stderr" || status=$?
}

new_case disabled
run_helper apply
assert_status "$status" 0 "a missing host config is a successful opt-out"
[ ! -e "$case_home/systemctl.log" ] || fail "opt-out contacted systemd"
pass "opt-out makes no systemd calls"

new_case enabled
printf '%s\n' '{"orca":{"enabled":true},"grokBot":{"enabled":true}}' >"$config"
run_helper apply
assert_status "$status" 0 "both opted-in services install and start"
assert_file_exists "$unit_dir/terrapod-orca.service" "Orca has a user unit"
assert_file_exists "$unit_dir/terrapod-grok-bot.service" "Grok Bot has a user unit"
assert_file_contains "$unit_dir/terrapod-orca.service" 'KillMode=process' "Orca preserves upstream terminal-daemon stop behavior"
assert_file_contains "$unit_dir/terrapod-grok-bot.service" 'Restart=always' "Grok Bot restarts after closing"
run_helper apply
assert_status "$status" 0 "a repeated apply succeeds"
assert_equals "$(awk '/--user start / {n++} END {print n+0}' "$case_home/systemctl.log")" 2 \
  "repeated apply starts each stopped service once"
assert_file_not_contains "$case_home/systemctl.log" restart "apply never restarts a service"
assert_file_not_contains "$case_home/systemctl.log" stop "apply never stops a service"

printf '%s\n' '{"orca":{"enabled":true,"port":7000},"grokBot":{"enabled":true,"display":143}}' >"$config"
printf '# old managed content\n' >>"$unit_dir/terrapod-orca.service"
run_helper apply
assert_status "$status" 0 "apply updates unit content while the app remains active"
assert_equals "$(awk '/--user start / {n++} END {print n+0}' "$case_home/systemctl.log")" 2 \
  "changed configuration leaves running apps untouched"
run_helper run-orca
assert_status "$status" 0 "Orca runtime invokes its installed launcher"
assert_file_contains "$case_home/argv.json" '"--no-pairing"' "Orca never emits a service pairing offer"
assert_file_contains "$case_home/argv.json" '"--refuse-public"' "Orca explicitly refuses public acceptance"
assert_file_contains "$case_home/argv.json" '"7000"' "Orca runtime reads the host-local port"
assert_file_not_contains "$case_home/argv.json" '"--bind"' "Orca uses its upstream listener behavior for on-demand pairing"
run_helper run-grok-bot
assert_status "$status" 0 "Grok Bot runtime invokes xvfb-run"
assert_file_contains "$case_home/argv.json" '"143"' "Grok Bot uses its configured display"
assert_file_contains "$case_home/argv.json" '-nolisten tcp' "the virtual X server does not listen on TCP"
assert_file_contains "$case_home/argv.json" 'Xauthority' "VNC can authenticate to the running display"
assert_equals "$(python3 -c 'import os,sys; print(oct(os.stat(sys.argv[1]).st_mode & 0o777))' "$case_home/.local/state/terrapod/grok-bot")" 0o700 \
  "Grok Bot keeps the X cookie directory private"
assert_equals "$(python3 -c 'import os,sys; print(oct(os.stat(sys.argv[1]).st_mode & 0o777))' "$case_home/.local/state/terrapod/grok-bot/Xauthority")" 0o600 \
  "Grok Bot keeps the X cookie file private"
APP_EXIT=23 run_helper run-orca
assert_status "$status" 23 "the runtime forwards the application's exit status"
unset APP_EXIT

printf '%s\n' '{}' >"$config"
cp "$case_home/systemctl.log" "$case_home/before.log"
run_helper apply
assert_status "$status" 0 "opting out does not stop existing services"
cmp "$case_home/systemctl.log" "$case_home/before.log" || fail "opt-out changed service state"
pass "opt-out leaves existing services for explicit manual removal"

for invalid in \
  '{"orca":{"enabled":"true"}}' \
  '{"orca":{"port":true}}' \
  '{"orca":{"port":0}}' \
  '{"orca":{"bind":"host.example"}}' \
  '{"grokBot":{"display":99}}' \
  '{"grokBot":{"display":-1}}' \
  '{"grokBot":{"display":142,"extra":true}}' \
  '{"unknown":{}}' \
  '[]' \
  'not JSON'; do
  new_case invalid
  printf '%s\n' "$invalid" >"$config"
  run_helper apply
  assert_status "$status" 1 "invalid host config is rejected before service changes"
done
[ ! -e "$case_home/systemctl.log" ] || fail "invalid config contacted systemd"

for legacy in 'system:orca-remote.service' 'user:orca-serve.service' 'user:grok-bot.service' 'system:terrapod-orca.service'; do
  new_case "legacy-${legacy%%:*}-${legacy#*:}"
  printf '%s\n' '{"orca":{"enabled":true},"grokBot":{"enabled":true}}' >"$config"
  printf '{"%s":{"LoadState":"loaded"}}\n' "$legacy" >"$case_home/state.json"
  run_helper apply
  assert_status "$status" 1 "existing $legacy blocks duplicate services"
  if [ "${legacy#*:}" = grok-bot.service ]; then
    blocked=terrapod-grok-bot.service
    healthy=terrapod-orca.service
  else
    blocked=terrapod-orca.service
    healthy=terrapod-grok-bot.service
  fi
  [ ! -e "$unit_dir/$blocked" ] || fail "legacy conflict wrote a replacement unit"
  assert_file_contains "$case_home/systemctl.log" "--user start $healthy" "a legacy conflict does not block the other app"
  assert_file_not_contains "$case_home/systemctl.log" "--user enable $blocked" "a legacy conflict enables no replacement service"
done

new_case unmanaged
printf '%s\n' '{"grokBot":{"enabled":true}}' >"$config"
mkdir -p "$unit_dir"
printf '%s\n' 'my unit' >"$unit_dir/terrapod-grok-bot.service"
run_helper apply
assert_status "$status" 1 "an unmanaged same-name unit is refused"
assert_equals "$(cat "$unit_dir/terrapod-grok-bot.service")" 'my unit' "an unmanaged same-name unit is preserved"
rm "$unit_dir/terrapod-grok-bot.service"
ln -s /dev/null "$unit_dir/terrapod-grok-bot.service"
run_helper apply
assert_status "$status" 1 "a masked unit is preserved"

new_case foreign
printf '%s\n' '{"orca":{"enabled":true}}' >"$config"
printf '%s\n' '{"user:terrapod-orca.service":{"LoadState":"loaded","FragmentPath":"/usr/lib/systemd/user/terrapod-orca.service"}}' >"$case_home/state.json"
run_helper apply
assert_status "$status" 1 "a unit from another search path is preserved"

new_case prerequisites
printf '%s\n' '{"orca":{"enabled":true}}' >"$config"
LINGER=no run_helper apply
assert_status "$status" 1 "lingering requires an explicit administrator action"
unset LINGER
[ ! -d "$unit_dir" ] || fail "missing lingering wrote a unit"
BUS_UNAVAILABLE=1 run_helper apply
assert_status "$status" 1 "an unavailable user manager cannot be mistaken for a stopped service"
unset BUS_UNAVAILABLE
[ ! -d "$unit_dir" ] || fail "unavailable user manager wrote a unit"

new_case missing-xvfb
printf '%s\n' '{"orca":{"enabled":true},"grokBot":{"enabled":true}}' >"$config"
missing_bin="$tmp_dir/missing-bin"
mkdir -p "$missing_bin"
for command in systemctl loginctl orca-ide grok-bot xauth; do
  ln -s "$stub_bin/$command" "$missing_bin/$command"
done
ln -s "$(command -v python3)" "$missing_bin/python3"
status=0
HOME="$case_home" PATH="$missing_bin" python3 "$helper" apply >"$case_home/stdout" 2>"$case_home/stderr" || status=$?
assert_status "$status" 1 "missing Xvfb is reported as an opt-in failure"
assert_file_contains "$case_home/systemctl.log" '--user start terrapod-orca.service' \
  "missing Xvfb does not block Orca"
[ ! -e "$unit_dir/terrapod-grok-bot.service" ] || fail "missing Xvfb wrote a Grok Bot unit"

template=.chezmoiscripts/run_after_87-headless-services.sh.tmpl
script="$tmp_dir/apply.sh"
render_template "$ubuntu_data" "$template" >"$script"
sh -n "$script" || fail "the service hook is valid sh"
assert_equals "$(render_template "$macos_data" "$template")" '' "macOS renders no service hook"
assert_not_contains "$(managed_target_paths "$macos_data")" '/.local/lib/terrapod/headless-services' \
  "macOS does not manage the Linux helper"

new_case hook
mkdir -p "$case_home/.local/lib/terrapod"
cp "$helper" "$case_home/.local/lib/terrapod/headless-services"
printf '%s\n' '{"grokBot":{"enabled":true,"display":99}}' >"$config"
status=0
HOME="$case_home" XDG_STATE_HOME="$case_home/.local/state" PATH="$stub_bin:$PATH" sh "$script" >"$case_home/hook-output" 2>&1 || status=$?
if [ "$status" -ne 0 ]; then
  cat "$case_home/hook-output"
  fail "the service hook continues after recording a failed opt-in"
fi
assert_file_exists "$case_home/.local/state/terrapod/install-warnings/headless-services" \
  "a failed opt-in records a nonfatal apply warning"
printf '%s\n' '{}' >"$config"
HOME="$case_home" XDG_STATE_HOME="$case_home/.local/state" PATH="$stub_bin:$PATH" sh "$script" >"$case_home/hook-output" 2>&1
[ ! -e "$case_home/.local/state/terrapod/install-warnings/headless-services" ] || fail "successful opt-out did not clear the warning"
pass "successful opt-out clears the apply warning"
