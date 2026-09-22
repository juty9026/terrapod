# Common case setup, command fakes, and execution capture for Terrapod tests.
# POSIX shell; source this after tests/lib/harness.sh.

make_case_dir() {
  test_support_name="$1"
  case_dir="$tmp_dir/$test_support_name"
  case_bin="$case_dir/bin"
  case_home="$case_dir/home"
  case_xdg_config_home="$case_dir/xdg-config"
  case_xdg_data_home="$case_dir/xdg-data"
  case_call_log="$case_dir/calls.log"
  case_stdout_file="$case_dir/stdout"
  case_stderr_file="$case_dir/stderr"
  mkdir -p "$case_bin" "$case_home" "$case_xdg_config_home" "$case_xdg_data_home"
  : >"$case_call_log"
  printf '%s\n' "$case_dir"
}

run_case_command() {
  case_status=0
  HOME="$case_home" \
    XDG_CONFIG_HOME="$case_xdg_config_home" \
    XDG_DATA_HOME="$case_xdg_data_home" \
    TERRAPOD_STUB_CALL_LOG="$case_call_log" \
    "$@" >"$case_stdout_file" 2>"$case_stderr_file" || case_status=$?
  case_stdout="$(cat "$case_stdout_file")"
  case_stderr="$(cat "$case_stderr_file")"
}

capture_case_command() {
  test_support_case_dir="$1"
  test_support_input="$2"
  shift 2
  case_stdout_file="$test_support_case_dir/stdout"
  case_stderr_file="$test_support_case_dir/stderr"
  case_status=0
  printf '%s' "$test_support_input" | "$@" >"$case_stdout_file" 2>"$case_stderr_file" || case_status=$?
  case_stdout="$(cat "$case_stdout_file")"
  case_stderr="$(cat "$case_stderr_file")"
}

write_stub() {
  test_support_stub="$1"
  shift
  mkdir -p "${test_support_stub%/*}"
  : >"$test_support_stub"
  case "${1-}" in
    '#!'*) ;;
    *) printf '%s\n' '#!/bin/sh' >>"$test_support_stub" ;;
  esac
  while [ "$#" -gt 0 ]; do
    printf '%s\n' "$1" >>"$test_support_stub"
    shift
  done
  chmod +x "$test_support_stub"
}

write_uname_stub() {
  test_support_path="$1"
  if [ -d "$test_support_path/bin" ]; then
    test_support_path="$test_support_path/bin/uname"
  fi
  test_support_kernel="$2"
  test_support_machine="${3:-}"
  if [ -z "$test_support_machine" ]; then
    case "$test_support_kernel" in
      Darwin) test_support_machine=arm64 ;;
      *) test_support_machine=x86_64 ;;
    esac
  fi
  write_stub "$test_support_path" \
    'case "${1-}" in' \
    "  -s) printf '%s\\n' '$test_support_kernel' ;;" \
    "  -m) printf '%s\\n' '$test_support_machine' ;;" \
    "  '') printf '%s\\n' '$test_support_kernel' ;;" \
    '  *) exit 64 ;;' \
    'esac'
}

write_gum_responses() {
  test_support_responses="$1"
  shift
  : >"$test_support_responses"
  for test_support_response do
    printf '%s\n' "$test_support_response" >>"$test_support_responses"
  done
}

write_gum_stub() {
  test_support_path="$1"
  test_support_choose_policy="${2:-cancel}"
  test_support_confirm_policy="${3:-cancel}"
  write_stub "$test_support_path" \
    "TERRAPOD_GUM_EMPTY_CHOOSE_POLICY='$test_support_choose_policy'" \
    "TERRAPOD_GUM_EMPTY_CONFIRM_POLICY='$test_support_confirm_policy'" \
    'set -eu' \
    'log_file="${TERRAPOD_GUM_LOG:-${TERRAPOD_STUB_CALL_LOG:?}}"' \
    'responses_file="${TERRAPOD_GUM_RESPONSES:?}"' \
    'printf "%s" "gum args:" >>"$log_file"' \
    'for arg do printf "%s" " $arg" >>"$log_file"; done' \
    'printf "\\n" >>"$log_file"' \
    'if [ "${1:-}" = "--version" ]; then printf "%s\\n" "gum test stub"; exit 0; fi' \
    'next_response() { response="$(sed -n "1p" "$responses_file")"; sed "1d" "$responses_file" >"$responses_file.tmp"; mv "$responses_file.tmp" "$responses_file"; }' \
    'confirm_default_status() { for arg do case "$arg" in --default=true) return 0 ;; --default=false) return 1 ;; esac; done; return 1; }' \
    'case "${1:-}" in' \
    '  choose)' \
    '    if [ "${2:-}" = "--help" ]; then printf "%s\\n" "Usage: gum choose [<options> ...] [flags]" "      --label-delimiter=\"\""; exit 0; fi' \
    '    while IFS= read -r option; do printf "%s\\n" "gum stdin: $option" >>"$log_file"; done' \
    '    next_response' \
    '    if [ -z "$response" ] && [ "$TERRAPOD_GUM_EMPTY_CHOOSE_POLICY" = cancel ]; then exit 130; fi' \
    '    case "$response" in __CANCEL__) exit 130 ;; __ERROR__) printf "%s\\n" "simulated gum operational failure" >&2; exit 2 ;; esac' \
    '    printf "%s\\n" "$response"' \
    '    ;;' \
    '  confirm)' \
    '    next_response' \
    '    case "$response" in' \
    '      "") if [ "$TERRAPOD_GUM_EMPTY_CONFIRM_POLICY" = default ]; then shift; confirm_default_status "$@"; else exit 130; fi ;;' \
    '      yes|y|true|enabled) exit 0 ;; no|n|false|disabled) exit 1 ;; __CANCEL__) exit 130 ;;' \
    '      __ERROR__) printf "%s\\n" "simulated gum operational failure" >&2; exit 2 ;;' \
    '      *) printf "%s\\n" "unexpected gum confirm response: $response" >&2; exit 2 ;;' \
    '    esac' \
    '    ;;' \
    '  style) shift; for arg do case "$arg" in --*) ;; *) printf "%s\\n" "$arg" ;; esac; done ;;' \
    '  *) printf "%s\\n" "unexpected gum command: ${1:-}" >&2; exit 2 ;;' \
    'esac'
}

write_brew_bundle_stub() {
  test_support_path="$1"
  write_stub "$test_support_path" \
    'log_file="${MACOS_BREW_LOG:-${TERRAPOD_STUB_CALL_LOG:?}}"' \
    'printf "%s\\n" "brew args:$*" >>"$log_file"' \
    'bundle_file=' \
    'for arg do case "$arg" in --file=*) bundle_file="${arg#--file=}" ;; esac; done' \
    'bundle_has() { [ -n "$bundle_file" ] && grep -Eq "^$1 \\\"$2\\\"(,[[:space:]]|$)" "$bundle_file" 2>/dev/null; }' \
    'case "${1:-}" in' \
    '  --prefix) printf "%s\\n" "${MACOS_BREW_PREFIX:-/opt/homebrew}" ;;' \
    '  shellenv) case "${MACOS_BREW_SHELLENV_MODE:-success}" in command-failure) exit 41 ;; eval-failure) printf "%s\\n" false ;; *) printf "%s\\n" : ;; esac ;;' \
    '  analytics) exit 0 ;;' \
    '  bundle)' \
    '    [ "${MACOS_BREW_DRAIN_STDIN:-0}" != 1 ] || cat >/dev/null' \
    '    [ "${MACOS_BREW_ECHO_OUTPUT:-0}" != 1 ] || printf "%s\\n" "visible brew bundle output: $*"' \
    '    for formula in ${MACOS_BREW_FAIL_FORMULAE:-}; do bundle_has brew "$formula" && exit 42; done' \
    '    for cask in ${MACOS_BREW_FAIL_CASKS:-}; do bundle_has cask "$cask" && exit 42; done' \
    '    if [ "${MACOS_BREW_FAIL_CORE_BULK:-0}" = 1 ] && bundle_has brew mise && bundle_has brew btop; then exit 42; fi' \
    '    if [ "${MACOS_BREW_FAIL_DESKTOP_BULK:-0}" = 1 ] && grep -Fx "# Rendered opt-in macOS Desktop App Stack." "$bundle_file" >/dev/null 2>&1; then exit 42; fi' \
    '    if [ "${MACOS_BREW_FAIL_BULK:-0}" = 1 ] && grep -Fx "tap \"homebrew/cask\"" "$bundle_file" >/dev/null 2>&1; then exit 42; fi' \
    '    [ -z "${MACOS_BREW_INSTALLED_FILE:-}" ] || : >"$MACOS_BREW_INSTALLED_FILE"' \
    '    ;;' \
    '  *) exit 64 ;;' \
    'esac'
}

assert_call_log_contains() {
  assert_file_contains "$1" "$2" "$3"
}

write_restricted_path() {
  test_support_path="$1"
  shift
  mkdir -p "$test_support_path"
  for test_support_command do
    test_support_command_path="$(command -v "$test_support_command" 2>/dev/null || true)"
    if [ -z "$test_support_command_path" ]; then
      fail "restricted PATH setup requires $test_support_command"
    fi
    ln -s "$test_support_command_path" "$test_support_path/$test_support_command"
  done
}

shell_quote() {
  printf "'"
  printf '%s' "$1" | sed "s/'/'\\\\''/g"
  printf "'"
}

run_in_pty() {
  test_support_command_text="$1"
  if script --version >/dev/null 2>&1; then
    script -q -e -c "$test_support_command_text" /dev/null
  else
    script -q /dev/null sh -c "$test_support_command_text"
  fi
}
