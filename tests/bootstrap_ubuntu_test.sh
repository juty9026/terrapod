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

assert_contains() {
  haystack="$1"
  needle="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  haystack="$1"
  needle="$2"
  message="$3"

  if printf '%s\n' "$haystack" | grep -F -e "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_first_occurrence_before() {
  haystack="$1"
  first="$2"
  second="$3"
  message="$4"

  if ! printf '%s\n' "$haystack" | awk -v first="$first" -v second="$second" '
    first_line == 0 && index($0, first) { first_line = NR }
    second_line == 0 && index($0, second) { second_line = NR }
    END { exit !(first_line > 0 && second_line > 0 && first_line < second_line) }
  '; then
    fail "$message"
  fi

  pass "$message"
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
  'printf "%s\n" "apt-get args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'exit 0'

write_stub "$tmp_dir/bin/install" \
  'printf "%s\n" "install args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'exit 0'

write_stub "$tmp_dir/bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'case "$*" in' \
  '  *mise.en.dev*) printf "%s\n" mise-key ;;' \
  '  *repo.charm.sh*)' \
  '    if [ "${BOOTSTRAP_CHARM_KEY_CURL_STATUS:-0}" != "0" ]; then' \
  '      exit "$BOOTSTRAP_CHARM_KEY_CURL_STATUS"' \
  '    fi' \
  '    output_file=""' \
  '    want_output_file="no"' \
  '    for arg do' \
  '      if [ "$want_output_file" = "yes" ]; then' \
  '        output_file="$arg"' \
  '        want_output_file="no"' \
  '        continue' \
  '      fi' \
  '      if [ "$arg" = "-o" ]; then' \
  '        want_output_file="yes"' \
  '      fi' \
  '    done' \
  '    if [ -n "$output_file" ]; then' \
  '      printf "%s\n" charm-key >"$output_file"' \
  '    else' \
  '      printf "%s\n" charm-key' \
  '    fi' \
  '    ;;' \
  '  *) exit 64 ;;' \
  'esac'

write_stub "$tmp_dir/bin/gpg" \
  'printf "%s\n" "gpg args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'cat >/dev/null'

write_stub "$tmp_dir/bin/tee" \
  'printf "%s\n" "tee args:$*" >>"$BOOTSTRAP_TEST_LOG"' \
  'while IFS= read -r line || [ -n "$line" ]; do' \
  '  printf "%s\n" "tee stdin:$line" >>"$BOOTSTRAP_TEST_LOG"' \
  'done'

write_stub "$tmp_dir/bin/getent" \
  'if [ "$1" = passwd ] && [ "$2" = testuser ]; then' \
  '  printf "%s\n" "testuser:x:1000:1000::/home/testuser:/bin/bash"' \
  '  exit 0' \
  'fi' \
  'exit 2'

write_stub "$tmp_dir/bin/zsh" \
  'printf "%s\n" "PATH zsh should not be used for login shell changes" >&2' \
  'exit 1'

write_stub "$tmp_dir/bin/chsh" \
  'printf "%s\n" "$*" >"$CHSH_TEST_LOG"'

export PATH="$tmp_dir/bin:/usr/bin:/bin"
export HOME="$tmp_dir/home"
export CHSH_TEST_LOG="$tmp_dir/chsh.log"
export BOOTSTRAP_TEST_LOG="$tmp_dir/bootstrap.log"

sh "$rendered" >"$tmp_dir/bootstrap.out" 2>"$tmp_dir/bootstrap.err"

expected="-s /usr/bin/zsh testuser"
actual="$(cat "$CHSH_TEST_LOG" 2>/dev/null || true)"

if [ "$actual" != "$expected" ]; then
  fail "Ubuntu bootstrap should set the login shell to zsh; expected '$expected', got '${actual:-<no chsh call>}'"
fi

pass "Ubuntu bootstrap sets the login shell to zsh"

bootstrap_log="$(cat "$BOOTSTRAP_TEST_LOG")"
assert_contains "$bootstrap_log" "install args:-dm 755 /etc/apt/keyrings" "Ubuntu bootstrap creates an APT keyring directory"
assert_contains "$bootstrap_log" "curl args:-fSs https://mise.en.dev/gpg-key.pub" "Ubuntu bootstrap still fetches the mise APT signing key"
assert_contains "$bootstrap_log" "curl args:-fsSL https://repo.charm.sh/apt/gpg.key -o " "Ubuntu bootstrap fetches the Charm APT signing key"
assert_contains "$bootstrap_log" "gpg args:--dearmor --yes -o /etc/apt/keyrings/charm.gpg " "Ubuntu bootstrap dearmors the Charm APT signing key"
assert_contains "$bootstrap_log" "tee stdin:deb [signed-by=/etc/apt/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *" "Ubuntu bootstrap pins the Charm repository to its keyring"
assert_contains "$bootstrap_log" "apt-get args:install -y gum" "Ubuntu bootstrap installs gum through APT"
assert_contains "$bootstrap_log" "apt-get args:install -y mise" "Ubuntu bootstrap still installs mise through APT"
assert_first_occurrence_before "$bootstrap_log" "tee stdin:deb [signed-by=/etc/apt/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *" "apt-get args:install -y gum" "Ubuntu bootstrap adds the Charm repository before installing gum"

: >"$BOOTSTRAP_TEST_LOG"
BOOTSTRAP_CHARM_KEY_CURL_STATUS=17
export BOOTSTRAP_CHARM_KEY_CURL_STATUS
if sh "$rendered" >"$tmp_dir/bootstrap-curl-failure.out" 2>"$tmp_dir/bootstrap-curl-failure.err"; then
  unset BOOTSTRAP_CHARM_KEY_CURL_STATUS
  fail "Ubuntu bootstrap should fail when the Charm signing key download fails"
fi
unset BOOTSTRAP_CHARM_KEY_CURL_STATUS

bootstrap_curl_failure_log="$(cat "$BOOTSTRAP_TEST_LOG")"
assert_contains "$bootstrap_curl_failure_log" "curl args:-fsSL https://repo.charm.sh/apt/gpg.key -o " "Ubuntu bootstrap Charm key failure attempts to fetch the signing key"
assert_not_contains "$bootstrap_curl_failure_log" "apt-get args:install -y gum" "Ubuntu bootstrap Charm key failure stops before installing gum"
pass "Ubuntu bootstrap fails when the Charm signing key download fails"
