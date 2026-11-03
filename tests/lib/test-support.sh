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

# Responses are key=value lines: schema setting keys, preset, and write.
# Missing setting answers accept gum's actual --default; actions require consent.
write_gum_stub() {
  write_stub "$1" \
    'set -eu' \
    ". $(shell_quote "$repo_root/dot_local/lib/terrapod/config-toml.sh")" \
    ". $(shell_quote "$repo_root/tests/lib/gum-stub.sh")"
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
    '    printf "%s\\n" "brew auto-update:${HOMEBREW_NO_AUTO_UPDATE:-}" >>"$log_file"' \
    '    [ "${MACOS_BREW_DRAIN_STDIN:-0}" != 1 ] || cat >/dev/null' \
    '    [ "${MACOS_BREW_ECHO_OUTPUT:-0}" != 1 ] || printf "%s\\n" "visible brew bundle output: $*"' \
    '    for formula in ${MACOS_BREW_FAIL_FORMULAE:-}; do bundle_has brew "$formula" && exit 42; done' \
    '    for cask in ${MACOS_BREW_FAIL_CASKS:-}; do bundle_has cask "$cask" && exit 42; done' \
    '    bundle_call="$(grep -c "^brew auto-update:" "$log_file")"' \
    '    for failed_call in ${MACOS_BREW_FAIL_BUNDLE_CALLS:-}; do [ "$bundle_call" != "$failed_call" ] || exit 42; done' \
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

# Requires the managed-config reader. Observe real Setup's sequential gum calls.
# The schema supplies identities and
# applicability; the Workspace question/implication and final write order are
# explicit contracts, independent of the answer lookup in the gum stub.
assert_setup_questions() (
  test_support_log="$1"
  test_support_profile="$2"
  test_support_workspace="$3"
  test_support_expected="$(
    printf '%s\n' preset enableDevelopmentWorkspace
    if [ "$test_support_workspace" = false ]; then
      printf '%s\n' enableEditorStack
      if [ "$test_support_profile" = macos-terminal ]; then
        printf '%s\n' enableAiCliTools
      fi
    fi
    for test_support_kind in macos-app-group shell-environment; do
      managed_setting_schema | while IFS='|' read -r test_support_key test_support_row_kind test_support_name test_support_row_profile test_support_rest; do
        if [ "$test_support_kind" = "$test_support_row_kind" ] &&
          { [ "$test_support_row_profile" = all ] || [ "$test_support_row_profile" = "$test_support_profile" ]; }; then
          printf '%s\n' "$test_support_key"
        fi
      done
    done
    printf '%s\n' write
  )"
  test_support_actual="$(sed -n 's/^gum question: //p' "$test_support_log")"
  assert_equals "$test_support_actual" "$test_support_expected" "Setup asks each applicable question sequentially before final write ($test_support_profile, Workspace $test_support_workspace)"
)
