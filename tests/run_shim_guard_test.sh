#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

# tests/run globs test files relative to its own location, so a copy under an
# empty tests/ directory exercises the runner's startup checks without running
# this suite again inside itself.
fake_repo="$tmp_dir/repo"
mkdir -p "$fake_repo/tests"
cp "$repo_root/tests/run" "$fake_repo/tests/run"

# Stand-ins for a real interpreter and for a mise shim. Neither is a python;
# both record being executed, which the guarded run must never do.
real_bin="$tmp_dir/real-bin"
shim_home="$tmp_dir/home"
default_shims="$shim_home/.local/share/mise/shims"
custom_data_dir="$tmp_dir/mise-data"
custom_shims="$custom_data_dir/shims"
executed_log="$tmp_dir/executed.log"
: >"$executed_log"

write_python3_stub() {
  mkdir -p "$1"
  cat >"$1/python3" <<STUB
#!/bin/sh
printf '%s\\n' "$1/python3" >>"$executed_log"
STUB
  chmod +x "$1/python3"
}

write_python3_stub "$real_bin"
write_python3_stub "$default_shims"
write_python3_stub "$custom_shims"

# The runner needs the usual utilities; only the python3 lookup is under test.
run_runner() {
  set +e
  env -u MISE_DATA_DIR -u TERRAPOD_MISE_SHIMS_DIR HOME="$shim_home" "$@" \
    sh "$fake_repo/tests/run" >"$tmp_dir/stdout" 2>"$tmp_dir/stderr"
  runner_status=$?
  set -e
}

guidance='PATH="$(mise where python)/bin:$PATH" tests/run'

run_runner PATH="$default_shims:$PATH"
if [ "$runner_status" != 2 ]; then
  fail "tests/run refuses a python3 under the default mise shim directory (exit $runner_status)"
fi
pass "tests/run refuses a python3 under the default mise shim directory"
assert_file_contains "$tmp_dir/stderr" "$default_shims/python3" \
  "the refusal names the shim python3 resolved to"
assert_file_contains "$tmp_dir/stderr" "$guidance" \
  "the refusal carries the PATH prefix AGENTS.md documents"
assert_file_not_contains "$tmp_dir/stdout" "test files passed" \
  "a refused run reaches no test file"

run_runner PATH="$custom_shims:$PATH" MISE_DATA_DIR="$custom_data_dir"
if [ "$runner_status" != 2 ]; then
  fail "tests/run honours MISE_DATA_DIR when locating the shim directory (exit $runner_status)"
fi
pass "tests/run honours MISE_DATA_DIR when locating the shim directory"

run_runner PATH="$custom_shims:$PATH" TERRAPOD_MISE_SHIMS_DIR="$custom_shims"
if [ "$runner_status" != 2 ]; then
  fail "tests/run honours TERRAPOD_MISE_SHIMS_DIR like the executable-selection library (exit $runner_status)"
fi
pass "tests/run honours TERRAPOD_MISE_SHIMS_DIR like the executable-selection library"

if [ -s "$executed_log" ]; then
  fail "a refused run never executes the shim; ran: $(cat "$executed_log")"
fi
pass "a refused run never executes the shim"

# A shim directory that exists but sits behind a real interpreter on PATH is
# fine, and so is a machine without mise at all; both start the suite quietly.
run_runner PATH="$real_bin:$default_shims:$PATH"
if [ "$runner_status" != 0 ]; then
  fail "tests/run starts when a real python3 is ahead of the shims (exit $runner_status): $(cat "$tmp_dir/stderr")"
fi
pass "tests/run starts when a real python3 is ahead of the shims"
assert_file_contains "$tmp_dir/stdout" "0 test files passed" \
  "a run that passes the guard proceeds to the test files"
assert_file_not_contains "$tmp_dir/stderr" "mise shim" \
  "a run that passes the guard says nothing about shims"

rm -rf "$shim_home/.local"
run_runner PATH="$real_bin:$PATH"
if [ "$runner_status" != 0 ]; then
  fail "tests/run starts on a machine without mise (exit $runner_status): $(cat "$tmp_dir/stderr")"
fi
pass "tests/run starts on a machine without mise"
assert_not_contains "$(cat "$tmp_dir/stderr")" "mise" \
  "a machine without mise sees no shim guidance"
