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

  if printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
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

rendered="$tmp_dir/shell-integrations.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux"}}' \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_30-install-shell-integrations.sh.tmpl" \
  >"$rendered"

sh -n "$rendered" || fail "rendered shell integrations script should be valid sh"
pass "rendered shell integrations script is valid sh"

write_stub "$tmp_dir/bin/curl" \
  'printf "%s\n" "curl args:$*" >>"$SHELL_INTEGRATIONS_TEST_LOG"' \
  'exit "${SHELL_INTEGRATIONS_CURL_STATUS:-0}"'

write_stub "$tmp_dir/bin/git" \
  'printf "%s\n" "git args:$*" >>"$SHELL_INTEGRATIONS_TEST_LOG"' \
  'if [ "$1" = clone ] && [ "$2" = https://github.com/zdharma-continuum/zinit ]; then' \
  '  mkdir -p "$3"' \
  '  exit 0' \
  'fi' \
  'if [ "$1" = clone ] && [ "$2" = https://github.com/scmbreeze/scm_breeze.git ]; then' \
  '  mkdir -p "$3"' \
  '  printf "%s\n" "#!/bin/sh" "exit 0" >"$3/install.sh"' \
  '  chmod +x "$3/install.sh"' \
  '  exit 0' \
  'fi' \
  'exit 64'

export PATH="$tmp_dir/bin:/usr/bin:/bin"
export HOME="$tmp_dir/home"
export XDG_STATE_HOME="$tmp_dir/state"
export SHELL_INTEGRATIONS_TEST_LOG="$tmp_dir/shell-integrations.log"

HOME="$HOME" XDG_STATE_HOME="$XDG_STATE_HOME" sh -c \
  '. "$1"; terrapod_install_warning_write shell-integrations "Shell integration setup needs attention" "Previous shell integration warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

SHELL_INTEGRATIONS_CURL_STATUS=23
export SHELL_INTEGRATIONS_CURL_STATUS
if sh "$rendered" >"$tmp_dir/shell-integrations-curl-failure.out" 2>"$tmp_dir/shell-integrations-curl-failure.err"; then
  unset SHELL_INTEGRATIONS_CURL_STATUS
  fail "shell integrations should fail when the Oh My Zsh installer download fails"
fi
unset SHELL_INTEGRATIONS_CURL_STATUS

shell_integrations_marker="$XDG_STATE_HOME/terrapod/install-warnings/shell-integrations"
if [ ! -f "$shell_integrations_marker" ]; then
  fail "shell integrations should keep a warning marker when the Oh My Zsh installer download fails"
fi
pass "shell integrations keeps a warning marker when the Oh My Zsh installer download fails"

marker_text="$(cat "$shell_integrations_marker")"
assert_contains "$marker_text" "summary='Shell integration setup needs attention'" "shell integrations marker keeps the expected summary"
assert_contains "$marker_text" "Oh My Zsh" "shell integrations marker mentions the failed Oh My Zsh step"

test_log="$(cat "$SHELL_INTEGRATIONS_TEST_LOG")"
assert_contains "$test_log" "curl args:-fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh" "shell integrations attempts to download the Oh My Zsh installer"
assert_not_contains "$test_log" "git args:clone https://github.com/zdharma-continuum/zinit" "shell integrations stops before zinit when Oh My Zsh download fails"

rm -f "$shell_integrations_marker"
: >"$SHELL_INTEGRATIONS_TEST_LOG"
SHELL_INTEGRATIONS_CURL_STATUS=23
export SHELL_INTEGRATIONS_CURL_STATUS
if ! TERRAPOD_FIRST_RUN_APPLY=1 sh "$rendered" >"$tmp_dir/shell-integrations-first-run-curl-failure.out" 2>"$tmp_dir/shell-integrations-first-run-curl-failure.err"; then
  unset SHELL_INTEGRATIONS_CURL_STATUS
  fail "first-run shell integrations should continue when the Oh My Zsh installer download warning is recorded"
fi
unset SHELL_INTEGRATIONS_CURL_STATUS

if [ ! -f "$shell_integrations_marker" ]; then
  fail "first-run shell integrations should record a warning marker when the Oh My Zsh installer download fails"
fi
pass "first-run shell integrations records a warning marker when the Oh My Zsh installer download fails"
