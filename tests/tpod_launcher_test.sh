#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
launcher="$repo_root/scripts/tpod-launcher.sh"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

[ -x "$launcher" ] || fail "launcher is executable"
sh -n "$launcher" || fail "launcher is valid POSIX shell"
pass "launcher is executable POSIX shell"

case_dir="$tmp_dir/case"
home_dir="$case_dir/home"
data_home="$case_dir/data"
release_dir="$data_home/terrapod/releases/1.2.3"
bin_dir="$release_dir/bin"
stub_dir="$case_dir/stubs"
mkdir -p "$home_dir" "$bin_dir" "$stub_dir"

cat >"$stub_dir/id" <<'EOF'
#!/bin/sh
printf '%s\n' "${TPOD_TEST_UID:-501}"
EOF
chmod +x "$stub_dir/id"
cat >"$stub_dir/curl" <<'EOF'
#!/bin/sh
printf '%s\n' called >>"${TPOD_TEST_CURL_LOG:?}"
exit 99
EOF
chmod +x "$stub_dir/curl"
curl_log="$case_dir/curl.log"
: >"$curl_log"

cat >"$bin_dir/tpod" <<'EOF'
#!/bin/sh
printf '%s\n' "argc:$#"
for argument in "$@"; do
  printf '%s\n' "arg:$argument"
done
printf '%s\n' "marker:${TPOD_TEST_MARKER-}"
EOF
chmod +x "$bin_dir/tpod"
ln -s releases/1.2.3 "$data_home/terrapod/current"

output="$(
  HOME="$home_dir" XDG_DATA_HOME="$data_home" TPOD_TEST_MARKER="kept" \
    PATH="$stub_dir:/usr/bin:/bin" "$launcher" status "two words" ""
)"
expected='argc:3
arg:status
arg:two words
arg:
marker:kept'
[ "$output" = "$expected" ] || fail "launcher preserves exact arguments and environment"
pass "launcher preserves exact arguments and environment"

rm "$data_home/terrapod/current"
if HOME="$home_dir" XDG_DATA_HOME="$data_home" TPOD_TEST_CURL_LOG="$curl_log" PATH="$stub_dir:/usr/bin:/bin" \
  "$launcher" status >"$case_dir/missing.out" 2>"$case_dir/missing.err"; then
  fail "missing active binary fails"
fi
expected_repair='sh -c "$(curl -fsLS https://github.com/juty9026/terrapod/releases/download/v1.0.0/install.sh)" -- --repair'
grep -Fx "$expected_repair" "$case_dir/missing.err" >/dev/null ||
  fail "missing active binary prints the exact versioned repair command"
pass "missing active binary prints the exact versioned repair command"
if [ -s "$curl_log" ]; then
  fail "status never downloads implicitly"
fi
pass "status never downloads implicitly"

ln -s releases/1.2.3 "$data_home/terrapod/current"
chmod -x "$bin_dir/tpod"
if HOME="$home_dir" XDG_DATA_HOME="$data_home" PATH="$stub_dir:/usr/bin:/bin" \
  "$launcher" help >/dev/null 2>"$case_dir/broken.err"; then
  fail "non-executable active binary fails"
fi
grep -Fx "$expected_repair" "$case_dir/broken.err" >/dev/null ||
  fail "broken active binary prints repair guidance"
pass "broken active binary prints repair guidance"
chmod +x "$bin_dir/tpod"

outside="$case_dir/outside"
mkdir -p "$outside/bin"
cp "$bin_dir/tpod" "$outside/bin/tpod"
rm "$data_home/terrapod/current"
ln -s "$outside" "$data_home/terrapod/current"
if HOME="$home_dir" XDG_DATA_HOME="$data_home" PATH="$stub_dir:/usr/bin:/bin" \
  "$launcher" help >/dev/null 2>"$case_dir/escape.err"; then
  fail "active symlink escape is rejected"
fi
pass "active symlink escape is rejected"

rm "$data_home/terrapod/current"
ln -s releases/1.2.3 "$data_home/terrapod/current"
output="$(
  HOME="$home_dir" XDG_DATA_HOME="$data_home" TPOD_TEST_UID=0 \
    PATH="$stub_dir:/usr/bin:/bin" "$launcher" help
)"
printf '%s\n' "$output" | grep -Fx "arg:help" >/dev/null ||
  fail "PATH id cannot spoof the trusted root check"
grep -F 'uid="$(/usr/bin/id -u)"' "$launcher" >/dev/null ||
  fail "launcher uses the trusted absolute id path"
pass "launcher root check cannot be bypassed through PATH"
