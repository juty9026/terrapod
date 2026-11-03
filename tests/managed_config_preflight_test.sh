#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir

. "$repo_root/dot_local/lib/terrapod/config-toml.sh"
terrapod="$repo_root/dot_local/bin/executable_terrapod"
mkdir "$tmp_dir/bin"
write_gum_stub "$tmp_dir/bin/gum"
: >"$tmp_dir/responses"

assert_setup_rejected() {
  destination="$1"
  reason="$2"
  snapshot_root="$3"
  before="$(find "$snapshot_root" -print | sort)"
  : >"$tmp_dir/gum.log"
  status=0
  PATH="$tmp_dir/bin:$PATH" TERRAPOD_PROFILE=macos-terminal \
    TERRAPOD_CHEZMOI_CONFIG="$destination" TERRAPOD_GUM_LOG="$tmp_dir/gum.log" \
    TERRAPOD_GUM_RESPONSES="$tmp_dir/responses" \
    sh "$terrapod" setup >"$tmp_dir/out" 2>"$tmp_dir/err" </dev/null || status=$?
  assert_status "$status" 1 "setup rejects $reason"
  assert_file_contains "$tmp_dir/err" "$reason" "setup explains $reason"
  assert_file_contains "$tmp_dir/err" "$destination" "setup names the rejected destination"
  assert_equals "$(cat "$tmp_dir/gum.log")" "" "setup rejects $reason before any gum invocation"
  assert_equals "$(find "$snapshot_root" -print | sort)" "$before" "setup rejection creates no directories, backups, or staging files"
}

mkdir -p "$tmp_dir/non-regular/destination"
assert_setup_rejected "$tmp_dir/non-regular/destination" 'not a regular file' "$tmp_dir/non-regular"

mkdir "$tmp_dir/links"
printf '%s\n' '[data]' 'enableEditorStack = false' >"$tmp_dir/links/target"
chmod 600 "$tmp_dir/links/target"
cp "$tmp_dir/links/target" "$tmp_dir/target-before"
ln -s target "$tmp_dir/links/config"
assert_setup_rejected "$tmp_dir/links/config" 'not a regular file' "$tmp_dir/links"
[ -L "$tmp_dir/links/config" ] || fail "setup preserves the config symlink"
cmp -s "$tmp_dir/links/target" "$tmp_dir/target-before" || fail "setup preserves the symlink target"
assert_equals "$(file_mode "$tmp_dir/links/target")" 600 "setup preserves the symlink target mode"
ln -s absent "$tmp_dir/links/dangling"
assert_setup_rejected "$tmp_dir/links/dangling" 'not a regular file' "$tmp_dir/links"
[ -L "$tmp_dir/links/dangling" ] && [ ! -e "$tmp_dir/links/absent" ] || fail "setup preserves the dangling link and absent target"

mkdir "$tmp_dir/syntax"
for syntax in inline multiline-string multiline-array; do
  case "$syntax" in
    inline) text='data = { enableEditorStack = false }'; reason='unsupported inline data table' ;;
    multiline-string) text='notes = """kept"""'; reason='unsupported multiline string' ;;
    multiline-array) text='notes = [
  [section]
]'; reason='unsupported multiline array' ;;
  esac
  printf '%s\n' "$text" >"$tmp_dir/syntax/config"
  chmod 600 "$tmp_dir/syntax/config"
  cp "$tmp_dir/syntax/config" "$tmp_dir/syntax-before"
  assert_setup_rejected "$tmp_dir/syntax/config" "$reason" "$tmp_dir/syntax"
  cmp -s "$tmp_dir/syntax/config" "$tmp_dir/syntax-before" || fail "setup preserves unsupported config contents"
  assert_equals "$(file_mode "$tmp_dir/syntax/config")" 600 "setup preserves unsupported config mode"
done

mkdir "$tmp_dir/unreadable"
printf '%s\n' '[data]' 'enableEditorStack = false' >"$tmp_dir/unreadable/config"
cp "$tmp_dir/unreadable/config" "$tmp_dir/unreadable-before"
chmod 000 "$tmp_dir/unreadable/config"
if [ -r "$tmp_dir/unreadable/config" ]; then
  skip "setup rejects unreadable configs (this user can read mode 000)"
else
  assert_setup_rejected "$tmp_dir/unreadable/config" unreadable "$tmp_dir/unreadable"
  assert_equals "$(file_mode "$tmp_dir/unreadable/config")" 0 "setup preserves unreadable config mode"
fi
chmod 600 "$tmp_dir/unreadable/config"
cmp -s "$tmp_dir/unreadable/config" "$tmp_dir/unreadable-before" || fail "setup preserves unreadable config contents"

mkdir "$tmp_dir/locked"
printf '%s\n' '[data]' 'enableEditorStack = false' >"$tmp_dir/locked/config"
chmod 600 "$tmp_dir/locked/config"
cp "$tmp_dir/locked/config" "$tmp_dir/locked-before"
chmod 500 "$tmp_dir/locked"
if [ -w "$tmp_dir/locked" ]; then
  skip "setup rejects unwritable parents and ancestors (this user can write mode 500)"
else
  assert_setup_rejected "$tmp_dir/locked/config" 'not a writable, searchable directory' "$tmp_dir/locked"
  assert_equals "$(file_mode "$tmp_dir/locked/config")" 600 "setup preserves config mode in an unwritable parent"
  assert_setup_rejected "$tmp_dir/locked/missing/parents/config" 'not a writable, searchable directory' "$tmp_dir/locked"
fi
chmod 700 "$tmp_dir/locked"
cmp -s "$tmp_dir/locked/config" "$tmp_dir/locked-before" || fail "setup preserves config in an unwritable parent"

mkdir "$tmp_dir/valid"
before="$(find "$tmp_dir/valid" -print)"
managed_setup_config_write_preflight "$tmp_dir/valid/missing/parents/config"
assert_equals "$(find "$tmp_dir/valid" -print)" "$before" "successful preflight creates no missing directories or artifacts"

# Read completeness does not imply rewrite permissions, and a rewrite does
# not require valid old Managed Setting values.
printf '%s\n' '[data]' 'enableEditorStack = "false"' >"$tmp_dir/valid/config"
managed_setup_config_write_preflight "$tmp_dir/valid/config"
assert_equals "$(cat "$tmp_dir/valid/config")" '[data]
enableEditorStack = "false"' "preflight permits replacing invalid values without changing them"

# BSD immutable flags prohibit replacement/staging even with ordinary mode
# bits granting access. Restore flags even when an assertion fails.
mkdir "$tmp_dir/immutable"
printf '%s\n' '[data]' 'enableEditorStack = false' >"$tmp_dir/immutable/config"
for flagged_path in "$tmp_dir/immutable/config" "$tmp_dir/immutable"; do
  if ! command -v chflags >/dev/null 2>&1 || ! chflags uchg "$flagged_path" 2>/dev/null; then
    skip "setup detects immutable file/directory flags (chflags unavailable or unsupported)"
    break
  fi
  (
    trap 'chflags nouchg "$flagged_path"' EXIT
    assert_setup_rejected "$tmp_dir/immutable/config" 'immutable or append-only' "$tmp_dir/immutable"
    if [ -d "$flagged_path" ]; then
      assert_setup_rejected "$tmp_dir/immutable/missing/config" 'immutable or append-only' "$tmp_dir/immutable"
    fi
  )
done
