#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
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

write_stub() {
  path="$1"
  shift
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' "$@"
  } >"$path"
  chmod +x "$path"
}

mkdir -p "$tmp_dir/bin" "$tmp_dir/home"

rendered="$tmp_dir/bootstrap-ubuntu.sh"
chezmoi execute-template \
  --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl" \
  >"$rendered"

sh -n "$rendered" || fail "rendered Ubuntu bootstrap script should be valid sh"
pass "rendered Ubuntu bootstrap script is valid sh"

write_stub "$tmp_dir/bin/id" \
  'case "$1" in' \
  '  -u) printf "%s\n" 1000 ;;' \
  '  -un) printf "%s\n" testuser ;;' \
  '  *) exit 64 ;;' \
  'esac'

write_stub "$tmp_dir/bin/sudo" \
  'exec "$@"'

write_stub "$tmp_dir/bin/apt-get" \
  'exit 0'

write_stub "$tmp_dir/bin/install" \
  'exit 0'

write_stub "$tmp_dir/bin/curl" \
  'printf "%s\n" mise-key'

write_stub "$tmp_dir/bin/tee" \
  'cat >/dev/null' \
  'exit 0'

write_stub "$tmp_dir/bin/getent" \
  'if [ "$1" = passwd ] && [ "$2" = testuser ]; then' \
  '  printf "%s\n" "testuser:x:1000:1000::/home/testuser:/bin/bash"' \
  '  exit 0' \
  'fi' \
  'exit 2'

write_stub "$tmp_dir/bin/zsh" \
  'exit 0'

write_stub "$tmp_dir/bin/chsh" \
  'printf "%s\n" "$*" >"$CHSH_TEST_LOG"'

export PATH="$tmp_dir/bin:/usr/bin:/bin"
export HOME="$tmp_dir/home"
export CHSH_TEST_LOG="$tmp_dir/chsh.log"

sh "$rendered" >"$tmp_dir/bootstrap.out" 2>"$tmp_dir/bootstrap.err"

expected="-s $tmp_dir/bin/zsh testuser"
actual="$(cat "$CHSH_TEST_LOG" 2>/dev/null || true)"

if [ "$actual" != "$expected" ]; then
  fail "Ubuntu bootstrap should set the login shell to zsh; expected '$expected', got '${actual:-<no chsh call>}'"
fi

pass "Ubuntu bootstrap sets the login shell to zsh"
