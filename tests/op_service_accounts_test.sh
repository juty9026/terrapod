#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

home_dir="$tmp_dir/home with spaces"
mkdir -p "$home_dir/.config/zsh" "$tmp_dir/bin"
: >"$tmp_dir/chezmoi.toml"

apply_shell() {
  chezmoi --config "$tmp_dir/chezmoi.toml" --source "$repo_root" \
    --destination "$home_dir" --override-data "$1" \
    apply --exclude scripts -- "$home_dir/.zshrc" \
    "$home_dir/.config/zsh/1password-service-accounts.zsh"
}

run_zsh() {
  env -i HOME="$home_dir" PATH="$tmp_dir/bin:/usr/bin:/bin" TERM=dumb \
    zsh -d -ic "$1"
}

apply_shell '{"chezmoi":{"os":"darwin"}}'
actual="$(run_zsh 'whence -w opx opx-add' 2>"$tmp_dir/stderr")"
assert_equals "$actual" 'opx: function
opx-add: function' "managed interactive startup exposes both service account helpers"
assert_equals "$(cat "$tmp_dir/stderr")" "" "startup needs no CLI or registered tokens"
[ ! -e "$home_dir/.config/op" ] || fail "startup must not create token storage"
pass "startup leaves machine-local token storage absent"

cat >"$tmp_dir/bin/op" <<'STUB'
#!/bin/sh
printf '%s\n' "$OP_SERVICE_ACCOUNT_TOKEN" >"$HOME/selected-token"
printf '%s\000' "$@" >"$HOME/arguments"
cat >"$HOME/command-stdin"
printf '{"result":"unchanged"}\n'
printf 'CLI diagnostic\n' >&2
exit "${OP_TEST_STATUS:-37}"
STUB
chmod +x "$tmp_dir/bin/op"
mkdir -p "$home_dir/.config/op/sa"
printf '%s' dummy-selected-token >"$home_dir/.config/op/sa/team_1-test"
printf '%s' 'stdin bytes' >"$home_dir/input"

run_zsh '
  for previous in unset empty populated; do
    case $previous in
      unset) unset OP_SERVICE_ACCOUNT_TOKEN ;;
      empty) export OP_SERVICE_ACCOUNT_TOKEN= ;;
      populated) export OP_SERVICE_ACCOUNT_TOKEN=dummy-parent-token ;;
    esac
    opx team_1-test item get "two words" "" "*" "--format=json" <"$HOME/input" >"$HOME/stdout" 2>"$HOME/stderr"
    result=$?
    print -r -- "$result|${OP_SERVICE_ACCOUNT_TOKEN-unset}" >>"$HOME/caller-state"
  done
' >"$tmp_dir/output" 2>"$tmp_dir/stderr"
assert_equals "$(cat "$home_dir/caller-state")" '37|unset
37|
37|dummy-parent-token' "opx preserves CLI failure and unset, empty, and populated caller environments"
assert_equals "$(cat "$home_dir/selected-token")" dummy-selected-token "only the selected token reaches the child CLI"
assert_equals "$(od -An -tx1 "$home_dir/arguments" | tr -d ' \n')" \
  '6974656d006765740074776f20776f72647300002a002d2d666f726d61743d6a736f6e00' \
  "opx forwards individual arguments including spaces, empty strings, globs, and flags"
assert_equals "$(cat "$home_dir/command-stdin")" 'stdin bytes' "opx preserves CLI stdin"
assert_equals "$(cat "$home_dir/stdout")" '{"result":"unchanged"}' "opx leaves CLI stdout directly usable"
assert_equals "$(cat "$home_dir/stderr")" 'CLI diagnostic' "opx preserves CLI stderr"
assert_equals "$(cat "$tmp_dir/output" "$tmp_dir/stderr")" '' "opx adds no success output"
printf '{"result":"unchanged"}\n' >"$tmp_dir/expected-stdout"
printf 'CLI diagnostic\n' >"$tmp_dir/expected-stderr"
cmp "$home_dir/stdout" "$tmp_dir/expected-stdout" || fail "CLI stdout bytes must be unchanged"
cmp "$home_dir/stderr" "$tmp_dir/expected-stderr" || fail "CLI stderr bytes must be unchanged"
cmp "$home_dir/input" "$home_dir/command-stdin" || fail "CLI stdin bytes must be unchanged"
pass "CLI streams preserve exact bytes including trailing newlines"
for cli_status in 0 1 130; do
  result=0
  run_zsh "export OP_TEST_STATUS=$cli_status; opx team_1-test vault list" </dev/null >"$tmp_dir/output" 2>"$tmp_dir/stderr" || result=$?
  assert_status "$result" "$cli_status" "opx preserves CLI exit status $cli_status"
done

expect_failure() {
  command_text="$1"
  expected_status="$2"
  guidance="$3"
  rm -f "$home_dir/selected-token"
  result=0
  run_zsh "$command_text" >"$tmp_dir/output" 2>"$tmp_dir/stderr" || result=$?
  assert_status "$result" "$expected_status" "$command_text fails before executing op"
  assert_file_contains "$tmp_dir/stderr" "$guidance" "failure gives actionable guidance"
  [ ! -e "$home_dir/selected-token" ] || fail "preflight must not invoke op"
  assert_equals "$(cat "$tmp_dir/output")" '' "preflight leaves stdout empty"
}

expect_failure 'opx' 64 'usage: opx'
expect_failure 'opx team_1-test' 64 'usage: opx'
expect_failure 'opx-add' 64 'usage: opx-add'
expect_failure 'opx-add team_1-test extra' 64 'usage: opx-add'
for invalid in '' '../outside' '/absolute' 'a/b' '.' '..' '-leading' '_leading' 'two words' 'a.b' 'a*' 'é'; do
  expect_failure "opx '$invalid' item list" 64 'invalid account name'
  expect_failure "opx-add '$invalid'" 64 'invalid account name'
done
expect_failure 'opx missing item list' 1 'opx-add missing'
: >"$home_dir/.config/op/sa/empty"
expect_failure 'opx empty item list' 1 'no usable token'
printf '\n' >"$home_dir/.config/op/sa/newline"
expect_failure 'opx newline item list' 1 'no usable token'
mkdir "$home_dir/.config/op/sa/directory"
expect_failure 'opx directory item list' 1 'no usable token'
ln -s "$home_dir/.config/op/sa/team_1-test" "$home_dir/.config/op/sa/link"
expect_failure 'opx link item list' 1 'no usable token'
printf '%s' dummy-unreadable >"$home_dir/.config/op/sa/unreadable"
chmod 000 "$home_dir/.config/op/sa/unreadable"
if [ -r "$home_dir/.config/op/sa/unreadable" ]; then
  skip 'unreadable token requires a non-root test user'
else
  expect_failure 'opx unreadable item list' 1 'no usable token'
fi
rm "$tmp_dir/bin/op"
expect_failure 'opx team_1-test item list' 127 '1Password CLI (op) is not installed'

printf '%s\n' dummy-first-token | run_zsh 'opx-add new-account' >"$tmp_dir/output" 2>"$tmp_dir/stderr"
assert_equals "$(cat "$home_dir/.config/op/sa/new-account")" dummy-first-token "registration saves a token without needing op"
assert_file_not_contains "$tmp_dir/stderr" dummy-first-token "registration messages omit the token value"
assert_equals "$(cat "$tmp_dir/output")" '' "registration leaves stdout empty"
permissions="$(python3 - "$home_dir/.config/op/sa" <<'PY'
import pathlib, stat, sys
directory = pathlib.Path(sys.argv[1])
print(f'{stat.S_IMODE(directory.stat().st_mode):o}|{stat.S_IMODE((directory / "new-account").stat().st_mode):o}')
PY
)"
assert_equals "$permissions" '700|600' "token directory and file have private permissions"

result=0
printf 'no\n' | run_zsh 'opx-add new-account' >"$tmp_dir/output" 2>"$tmp_dir/stderr" || result=$?
assert_status "$result" 1 "declining replacement fails without changing registration"
assert_file_contains "$tmp_dir/stderr" 'Replace token' "existing registration requires explicit confirmation"
assert_equals "$(cat "$home_dir/.config/op/sa/new-account")" dummy-first-token "declining keeps the previous token bytes"

printf 'yes\ndummy-replacement\n' | run_zsh 'opx-add new-account' >"$tmp_dir/output" 2>"$tmp_dir/stderr"
assert_equals "$(cat "$home_dir/.config/op/sa/new-account")" dummy-replacement "affirmative confirmation permits replacement"
assert_file_not_contains "$tmp_dir/stderr" dummy-replacement "replacement messages omit the new token value"

run_registration_in_terminal() {
  python3 "$repo_root/tests/fixtures/op-service-account-input.py" \
    "$home_dir" "$tmp_dir/bin" "$1" "$2" >"$tmp_dir/terminal.json"
}

run_registration_in_terminal 'opx-add terminal-account' \
  '[["Paste token", "dummy-hidden-token\n", true]]'
assert_equals "$(jq -r .status "$tmp_dir/terminal.json")" 0 "registration succeeds through a real terminal"
assert_equals "$(jq -c .hidden "$tmp_dir/terminal.json")" '[true]' "terminal echo is disabled while entering a token"
assert_file_not_contains "$tmp_dir/terminal.json" dummy-hidden-token "terminal transcript never echoes the token"
assert_equals "$(cat "$home_dir/.config/op/sa/terminal-account")" dummy-hidden-token "terminal registration stores the entered token"

run_registration_in_terminal 'opx-add terminal-account' \
  '[["Replace token", "Y\n", false], ["Paste token", "dummy-terminal-replacement\n", true]]'
assert_equals "$(jq -r .status "$tmp_dir/terminal.json")" 0 "terminal confirmation permits replacement"
assert_equals "$(jq -c .hidden "$tmp_dir/terminal.json")" '[true]' "replacement token entry also disables terminal echo"
assert_file_not_contains "$tmp_dir/terminal.json" dummy-terminal-replacement "terminal replacement does not echo the token"

for steps in \
  '[["Replace token", "n\n", false]]' \
  '[["Replace token", "\n", false]]' \
  '[["Replace token", "\u0004", false]]' \
  '[["Replace token", "yes\n", false], ["Paste token", "\n", true]]' \
  '[["Replace token", "yes\n", false], ["Paste token", "\u0004", true]]' \
  '[["Replace token", "yes\n", false], ["Paste token", "\u0003", true]]'; do
  run_registration_in_terminal 'opx-add terminal-account' "$steps"
  [ "$(jq -r .status "$tmp_dir/terminal.json")" -ne 0 ] || fail "cancelled terminal input must fail"
  assert_equals "$(jq -r .pending "$tmp_dir/terminal.json")" 0 "cancellation reaches the intended public prompt"
  assert_equals "$(cat "$home_dir/.config/op/sa/terminal-account")" dummy-terminal-replacement "decline, empty input, EOF, and interrupt preserve the existing token"
  assert_file_not_contains "$tmp_dir/terminal.json" 'opx-add: saved' "cancelled registration does not report success"
done

for input in '' '\n' 'dummy-without-newline'; do
  result=0
  printf '%b' "$input" | run_zsh 'opx-add unregistered' >"$tmp_dir/output" 2>"$tmp_dir/stderr" || result=$?
  assert_status "$result" 1 "empty or incomplete initial token input fails"
  [ ! -e "$home_dir/.config/op/sa/unregistered" ] || fail "failed input must not create a token"
done

expect_failure 'opx-add link' 1 'unsafe token path'
expect_failure 'opx-add directory' 1 'unsafe token path'
ln -s "$home_dir/outside-token" "$home_dir/.config/op/sa/dangling"
expect_failure 'opx-add dangling' 1 'unsafe token path'
[ ! -e "$home_dir/outside-token" ] || fail "token symlinks must not write outside storage"
pass "symlink registration leaves unrelated files untouched"

cat >"$tmp_dir/bin/mv" <<'STUB'
#!/bin/sh
exit 73
STUB
chmod +x "$tmp_dir/bin/mv"
result=0
printf 'yes\ndummy-failed-save\n' | run_zsh 'opx-add new-account' >"$tmp_dir/output" 2>"$tmp_dir/stderr" || result=$?
assert_status "$result" 1 "a failed final save returns failure"
assert_equals "$(cat "$home_dir/.config/op/sa/new-account")" dummy-replacement "failed save preserves the previous token bytes"
assert_file_contains "$tmp_dir/stderr" 'could not save token' "failed save explains the storage failure"
assert_file_not_contains "$tmp_dir/stderr" 'opx-add: saved' "failed save never reports success"
assert_equals "$(find "$home_dir/.config/op/sa" -name '.token.*' -print)" '' "failed save cleans up staged credentials"
rm "$tmp_dir/bin/mv"

# A real storage permission failure occurs before replacement. Root bypasses it.
chmod 500 "$home_dir/.config/op/sa"
if [ -w "$home_dir/.config/op/sa" ]; then
  skip 'unwritable storage requires a non-root test user'
else
  # opx-add repairs its directory mode, so prevent that repair at the external
  # chmod boundary to exercise the filesystem's actual write refusal.
  cat >"$tmp_dir/bin/chmod" <<'STUB'
#!/bin/sh
exit 0
STUB
  chmod +x "$tmp_dir/bin/chmod"
  result=0
  printf 'yes\ndummy-unwritable\n' | run_zsh 'opx-add new-account' >"$tmp_dir/output" 2>"$tmp_dir/stderr" || result=$?
  assert_status "$result" 1 "unwritable storage returns failure"
  assert_equals "$(cat "$home_dir/.config/op/sa/new-account")" dummy-replacement "unwritable storage preserves prior registration"
  assert_file_not_contains "$tmp_dir/stderr" 'opx-add: saved' "unwritable storage does not claim success"
  rm "$tmp_dir/bin/chmod"
fi
chmod 700 "$home_dir/.config/op/sa"

mv "$home_dir/.config/op/sa" "$home_dir/kept-registrations"
ln -s "$home_dir/kept-registrations" "$home_dir/.config/op/sa"
expect_failure 'opx-add new-account' 1 'unsafe token path'
rm "$home_dir/.config/op/sa"
mv "$home_dir/kept-registrations" "$home_dir/.config/op/sa"

for data in \
  '{"chezmoi":{"os":"darwin"}}' \
  '{"chezmoi":{"os":"darwin"},"enableEditorStack":true,"enableAiCliTools":true,"enableDevelopmentWorkspace":true,"enableMacosAppGroupLauncher":true}' \
  '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
  '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":true,"enableAiCliTools":true,"enableDevelopmentWorkspace":true,"enableMacosAppGroupLauncher":true}'; do
  apply_shell "$data"
  actual="$(run_zsh 'whence -w opx opx-add' </dev/null 2>"$tmp_dir/stderr")"
  assert_equals "$actual" 'opx: function
opx-add: function' "both profiles expose helpers independently of optional selections"
  assert_equals "$(cat "$tmp_dir/stderr")" '' "both profiles start without op, prompts, or authentication"
  assert_equals "$(cat "$home_dir/.config/op/sa/new-account")" dummy-replacement "reapplying managed shell targets preserves unmanaged registrations"
done

mv "$home_dir/.config/zsh/1password-service-accounts.zsh" "$tmp_dir/helper-module"
actual="$(run_zsh 'print -r -- recovery-started' </dev/null 2>"$tmp_dir/stderr")"
assert_equals "$actual" recovery-started "a partially installed shell starts without the helper module"
assert_equals "$(cat "$tmp_dir/stderr")" '' "a missing helper module produces no startup error"
mv "$tmp_dir/helper-module" "$home_dir/.config/zsh/1password-service-accounts.zsh"
