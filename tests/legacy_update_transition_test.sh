#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $*" >&2
  exit 1
}

config="$tmp_dir/chezmoi.toml"
: >"$config"
legacy_data="$tmp_dir/legacy-data.json"
printf '%s\n' '{"profile":"macos-terminal"}' >"$legacy_data"

stub_bin="$tmp_dir/bin"
mkdir -p "$stub_bin"

cat >"$stub_bin/curl" <<'CURL'
#!/bin/sh
set -eu
[ "$#" -eq 4 ] || exit 90
[ "$1" = "-fsSL" ] || exit 91
[ "$2" = "https://github.com/juty9026/terrapod/releases/download/v1.0.0/install.sh" ] || exit 92
[ "$3" = "-o" ] || exit 93
printf '%s\n' "$2" >"$TERRAPOD_TEST_CURL_LOG"
[ "${TERRAPOD_TEST_RELEASE_AVAILABLE:-1}" = 1 ] || exit 22
cp "$TERRAPOD_TEST_INSTALLER_SOURCE" "$4"
CURL
chmod +x "$stub_bin/curl"

installer_stub="$tmp_dir/installer.sh"
cat >"$installer_stub" <<'INSTALLER'
#!/bin/sh
set -eu
for arg do
  printf '%s\n' "installer-arg:$arg"
done >>"$TERRAPOD_TEST_MIGRATION_LOG"
[ "${TERRAPOD_TEST_MIGRATION_STATUS:-0}" -eq 0 ] ||
  exit "$TERRAPOD_TEST_MIGRATION_STATUS"
[ "${TERRAPOD_TEST_REPLACE_BRIDGE:-1}" = 1 ] || exit 0
manager="$HOME/.local/bin/terrapod.new"
cat >"$manager" <<'MANAGER'
#!/bin/sh
set -eu
printf '%s\n' "manager-env:${TERRAPOD_TEST_FORWARDED_ENV:-}" >>"$TERRAPOD_TEST_MANAGER_LOG"
for arg do
  printf '%s\n' "manager-arg:$arg"
done >>"$TERRAPOD_TEST_MANAGER_LOG"
MANAGER
chmod +x "$manager"
mv "$manager" "$HOME/.local/bin/terrapod"
INSTALLER

materialize_bridge() {
  case_dir="$1"
  mkdir -p "$case_dir/home" "$case_dir/tmp"
  HOME="$case_dir/home" TMPDIR="$case_dir/tmp" \
    chezmoi --config "$config" --source "$repo_root" \
      --destination "$case_dir/home" --override-data-file "$legacy_data" \
      apply --exclude scripts >"$case_dir/first.stdout" 2>"$case_dir/first.stderr" ||
    fail "first legacy update materializes the transition bridge"
}

assert_bridge_retryable() {
  case_dir="$1"
  [ -x "$case_dir/home/.local/bin/terrapod" ] ||
    fail "$2 leaves the bridge executable"
  [ ! -e "$case_dir/manager.log" ] ||
    fail "$2 does not invoke the manager"
}

success_case="$tmp_dir/success"
materialize_bridge "$success_case"
first_stderr="$success_case/first.stderr"
migration_log="$success_case/migration.log"
manager_log="$success_case/manager.log"
grep -F 'Terrapod manager transition is ready.' "$first_stderr" >/dev/null ||
  fail "first legacy update prints transition readiness"
grep -F 'Run `tpod update` once more to complete it automatically.' "$first_stderr" >/dev/null ||
  fail "first legacy update prints the second-run command"
[ ! -e "$migration_log" ] ||
  fail "first legacy update does not run migration"

HOME="$success_case/home" TMPDIR="$success_case/tmp" PATH="$stub_bin:$PATH" \
  TERRAPOD_TEST_INSTALLER_SOURCE="$installer_stub" \
  TERRAPOD_TEST_CURL_LOG="$success_case/curl.log" \
  TERRAPOD_TEST_MIGRATION_LOG="$migration_log" \
  TERRAPOD_TEST_MANAGER_LOG="$manager_log" \
  TERRAPOD_TEST_FORWARDED_ENV="still-present" \
  "$success_case/home/.local/bin/tpod" update "two words" "" ||
  fail "second legacy invocation migrates and forwards to the manager"

grep -Fx 'https://github.com/juty9026/terrapod/releases/download/v1.0.0/install.sh' \
  "$success_case/curl.log" >/dev/null ||
  fail "bridge downloads the version-pinned v1.0.0 installer"
grep -Fx 'installer-arg:--migrate' "$migration_log" >/dev/null ||
  fail "bridge invokes migration internally"
grep -Fx 'manager-env:still-present' "$manager_log" >/dev/null ||
  fail "bridge preserves the environment"
grep -Fx 'manager-arg:update' "$manager_log" >/dev/null ||
  fail "bridge forwards update after migration"
grep -Fx 'manager-arg:two words' "$manager_log" >/dev/null ||
  fail "bridge preserves argument boundaries"
grep -Fx 'manager-arg:' "$manager_log" >/dev/null ||
  fail "bridge preserves empty arguments"

release_case="$tmp_dir/release-unavailable"
materialize_bridge "$release_case"
if HOME="$release_case/home" TMPDIR="$release_case/tmp" PATH="$stub_bin:$PATH" \
  TERRAPOD_TEST_INSTALLER_SOURCE="$installer_stub" \
  TERRAPOD_TEST_CURL_LOG="$release_case/curl.log" \
  TERRAPOD_TEST_MIGRATION_LOG="$release_case/migration.log" \
  TERRAPOD_TEST_MANAGER_LOG="$release_case/manager.log" \
  TERRAPOD_TEST_RELEASE_AVAILABLE=0 \
  "$release_case/home/.local/bin/tpod" update >"$release_case/stdout" 2>"$release_case/stderr"; then
  fail "unavailable release fails the transition"
fi
grep -F 'v1.0.0 is not available' "$release_case/stderr" >/dev/null ||
  fail "unavailable release explains that the command is retryable"
assert_bridge_retryable "$release_case" "unavailable release"

migration_case="$tmp_dir/migration-failure"
materialize_bridge "$migration_case"
if HOME="$migration_case/home" TMPDIR="$migration_case/tmp" PATH="$stub_bin:$PATH" \
  TERRAPOD_TEST_INSTALLER_SOURCE="$installer_stub" \
  TERRAPOD_TEST_CURL_LOG="$migration_case/curl.log" \
  TERRAPOD_TEST_MIGRATION_LOG="$migration_case/migration.log" \
  TERRAPOD_TEST_MANAGER_LOG="$migration_case/manager.log" \
  TERRAPOD_TEST_MIGRATION_STATUS=17 \
  "$migration_case/home/.local/bin/tpod" update >"$migration_case/stdout" 2>"$migration_case/stderr"; then
  fail "failed migration fails the transition"
fi
grep -F 'manager migration did not complete' "$migration_case/stderr" >/dev/null ||
  fail "failed migration explains that the command is retryable"
assert_bridge_retryable "$migration_case" "failed migration"

replacement_case="$tmp_dir/replacement-missing"
materialize_bridge "$replacement_case"
if HOME="$replacement_case/home" TMPDIR="$replacement_case/tmp" PATH="$stub_bin:$PATH" \
  TERRAPOD_TEST_INSTALLER_SOURCE="$installer_stub" \
  TERRAPOD_TEST_CURL_LOG="$replacement_case/curl.log" \
  TERRAPOD_TEST_MIGRATION_LOG="$replacement_case/migration.log" \
  TERRAPOD_TEST_MANAGER_LOG="$replacement_case/manager.log" \
  TERRAPOD_TEST_REPLACE_BRIDGE=0 \
  "$replacement_case/home/.local/bin/tpod" update >"$replacement_case/stdout" 2>"$replacement_case/stderr"; then
  fail "successful installer without replacement fails the transition"
fi
grep -F 'migration did not replace the legacy transition bridge' \
  "$replacement_case/stderr" >/dev/null ||
  fail "recursion guard identifies a missing launcher replacement"
assert_bridge_retryable "$replacement_case" "missing launcher replacement"

printf '%s\n' "ok - guided legacy update transition"
