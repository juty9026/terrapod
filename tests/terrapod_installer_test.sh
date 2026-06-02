#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
safe_path_dir="$tmp_dir/safe-bin"

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

mkdir -p "$safe_path_dir"
for command_name in cat chmod cp mkdir mktemp rm; do
  command_path="$(command -v "$command_name")"
  ln -s "$command_path" "$safe_path_dir/$command_name"
done

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

assert_line() {
  haystack="$1"
  expected_line="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | grep -Fx "$expected_line" >/dev/null; then
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

assert_all_chezmoi_paths_equal() {
  haystack="$1"
  expected_path="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | awk -v expected="chezmoi path:$expected_path" '
    /^chezmoi path:/ {
      count++
      if ($0 != expected) {
        printf "%s\n", $0 >"/dev/stderr"
        bad = 1
      }
    }
    END { exit !(count > 0 && bad != 1) }
  '; then
    fail "$message"
  fi

  pass "$message"
}

install_script="$repo_root/install.sh"

if [ ! -f "$install_script" ]; then
  fail "install.sh exists"
fi

pass "install.sh exists"

if [ ! -x "$install_script" ]; then
  fail "install.sh is executable"
fi

pass "install.sh is executable"

sh -n "$install_script" || fail "install.sh is valid POSIX shell"
pass "install.sh is valid POSIX shell"

readme_file="$repo_root/README.md"
if [ ! -f "$readme_file" ]; then
  fail "README.md exists"
fi

pass "README.md exists"
readme_text="$(cat "$readme_file")"
assert_contains "$readme_text" "Terrapod first-run installer" "README documents the Terrapod first-run installer"
assert_contains "$readme_text" 'sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"' "README documents the full installer command"
assert_contains "$readme_text" "https://github.com/juty9026/terrapod.git" "README documents the HTTPS source repository"
assert_not_contains "$readme_text" "juty9026/dotfiles" "README does not document the unsupported legacy repository slug"
assert_contains "$readme_text" "You do not need to install \`chezmoi\` manually before running this installer." "README states chezmoi is not a first-run prerequisite"
assert_contains "$readme_text" "After the initial apply completes, the installer prints" "README documents post-apply help output"
assert_contains "$readme_text" "\`tpod help\` so the short day-to-day command is immediately visible." "README documents tpod help after first-run apply"
assert_contains "$readme_text" "Use \`tpod\` as the day-to-day management command after bootstrap." "README documents tpod as the day-to-day management command"
assert_contains "$readme_text" "\`terrapod\` remains the full command and brand name." "README keeps Terrapod as the full command and brand"
assert_contains "$readme_text" "terrapod chezmoi -- status" "README documents raw chezmoi access through Terrapod"
assert_contains "$readme_text" "Direct chezmoi use remains an advanced escape hatch." "README keeps direct chezmoi as an advanced escape hatch"
assert_not_contains "$readme_text" "--non-interactive" "README keeps non-interactive installer options out of scope"
assert_not_contains "$readme_text" "repository rename" "README keeps repository renaming out of scope"
assert_not_contains "$readme_text" "log-output" "README keeps broader log-output design out of scope"

agents_file="$repo_root/AGENTS.md"
if [ ! -f "$agents_file" ]; then
  fail "AGENTS.md exists"
fi

pass "AGENTS.md exists"
agents_text="$(cat "$agents_file")"
assert_contains "$agents_text" "juty9026/terrapod" "AGENTS.md documents the renamed GitHub issue tracker repository"
assert_not_contains "$agents_text" "juty9026/dotfiles" "AGENTS.md does not document the unsupported legacy repository slug"

issue_tracker_file="$repo_root/docs/agents/issue-tracker.md"
if [ ! -f "$issue_tracker_file" ]; then
  fail "issue tracker agent doc exists"
fi

pass "issue tracker agent doc exists"
issue_tracker_text="$(cat "$issue_tracker_file")"
assert_contains "$issue_tracker_text" "GitHub repository: \`juty9026/terrapod\`" "issue tracker doc uses the renamed GitHub repository"
assert_not_contains "$issue_tracker_text" "juty9026/dotfiles" "issue tracker doc does not document the unsupported legacy repository slug"

chezmoiignore_file="$repo_root/.chezmoiignore"
if [ ! -f "$chezmoiignore_file" ]; then
  fail ".chezmoiignore exists"
fi

pass ".chezmoiignore exists"

if ! grep -Fx "install.sh" "$chezmoiignore_file" >/dev/null; then
  fail "install.sh is ignored by chezmoi"
fi

pass "install.sh is not managed into the home directory"

assert_status() {
  actual="$1"
  expected="$2"
  message="$3"

  if [ "$actual" -ne "$expected" ]; then
    fail "$message"
  fi

  pass "$message"
}

assert_failure() {
  actual="$1"
  message="$2"

  if [ "$actual" -eq 0 ]; then
    fail "$message"
  fi

  pass "$message"
}

assert_no_stub_calls() {
  log_file="$1"
  message="$2"

  if [ -s "$log_file" ]; then
    printf '%s\n' "stubbed command calls:" >&2
    sed 's/^/  /' "$log_file" >&2
    fail "$message"
  fi

  pass "$message"
}

make_case_dir() {
  name="$1"
  case_dir="$tmp_dir/$name"
  mkdir -p \
    "$case_dir/bin" \
    "$case_dir/home" \
    "$case_dir/xdg-data" \
    "$case_dir/xdg-config"
  printf '%s\n' "$case_dir"
}

write_uname_stub() {
  case_dir="$1"
  kernel_name="$2"
  stub="$case_dir/bin/uname"

  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' "printf '%s\n' '$kernel_name'"
  } >"$stub"
  chmod +x "$stub"
}

write_os_release() {
  case_dir="$1"
  shift
  os_release_file="$case_dir/os-release"

  : >"$os_release_file"
  while [ "$#" -gt 0 ]; do
    printf '%s\n' "$1" >>"$os_release_file"
    shift
  done

  printf '%s\n' "$os_release_file"
}

write_command_call_stubs() {
  case_dir="$1"
  shift

  while [ "$#" -gt 0 ]; do
    command_name="$1"
    stub="$case_dir/bin/$command_name"
    {
      printf '%s\n' '#!/bin/sh'
      printf '%s\n' "printf '%s\n' '$command_name' >>\"\${TERRAPOD_STUB_CALL_LOG:?}\""
      printf '%s\n' 'exit 42'
    } >"$stub"
    chmod +x "$stub"
    shift
  done
}

write_ubuntu_package_stubs() {
  case_dir="$1"

  cat >"$case_dir/bin/id" <<'EOF'
#!/bin/sh
set -eu

case "${1-}" in
  -u)
    printf '%s\n' 1000
    ;;
  *)
    exit 64
    ;;
esac
EOF
  chmod +x "$case_dir/bin/id"

  cat >"$case_dir/bin/sudo" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "sudo args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
if [ "${TERRAPOD_SUDO_STUB_STATUS:-0}" != "0" ]; then
  exit "$TERRAPOD_SUDO_STUB_STATUS"
fi
exec "$@"
EOF
  chmod +x "$case_dir/bin/sudo"

  cat >"$case_dir/bin/apt-get" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "apt-get args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
case "${1-}" in
  update|install)
    if [ "$*" = "install -y gum" ] && [ "${TERRAPOD_APT_FAIL_INSTALL_GUM:-0}" = "1" ]; then
      exit 17
    fi
    exit 0
    ;;
  *)
    exit 64
    ;;
esac
EOF
  chmod +x "$case_dir/bin/apt-get"

  cat >"$case_dir/bin/install" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "install args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
exit 0
EOF
  chmod +x "$case_dir/bin/install"

  cat >"$case_dir/bin/curl" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "curl args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
case "$*" in
  *https://repo.charm.sh/apt/gpg.key*)
    if [ "${TERRAPOD_CHARM_KEY_STUB_STATUS:-0}" != "0" ]; then
      exit "$TERRAPOD_CHARM_KEY_STUB_STATUS"
    fi
    output_file=""
    want_output_file="no"
    for arg do
      if [ "$want_output_file" = "yes" ]; then
        output_file="$arg"
        want_output_file="no"
        continue
      fi
      if [ "$arg" = "-o" ]; then
        want_output_file="yes"
      fi
    done
    if [ -n "$output_file" ]; then
      printf '%s\n' "fake charm key" >"$output_file"
    else
      printf '%s\n' "fake charm key"
    fi
    ;;
  *)
    printf '%s\n' "unexpected curl args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
    exit 64
    ;;
esac
EOF
  chmod +x "$case_dir/bin/curl"

  cat >"$case_dir/bin/gpg" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "gpg args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
cat >/dev/null
if [ "${TERRAPOD_GPG_STUB_STATUS:-0}" != "0" ]; then
  exit "$TERRAPOD_GPG_STUB_STATUS"
fi
exit 0
EOF
  chmod +x "$case_dir/bin/gpg"

  cat >"$case_dir/bin/tee" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "tee args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
while IFS= read -r line || [ -n "$line" ]; do
  printf '%s\n' "tee stdin:$line" >>"${TERRAPOD_STUB_CALL_LOG:?}"
done
exit 0
EOF
  chmod +x "$case_dir/bin/tee"
}

write_non_root_id_stub() {
  case_dir="$1"

  cat >"$case_dir/bin/id" <<'EOF'
#!/bin/sh
set -eu

case "${1-}" in
  -u)
    printf '%s\n' 1000
    ;;
  *)
    exit 64
    ;;
esac
EOF
  chmod +x "$case_dir/bin/id"
}

write_chezmoi_flow_stub() {
  stub="$1"

  cat >"$stub" <<'EOF'
#!/bin/sh
set -eu

log_file="${TERRAPOD_STUB_CALL_LOG:?}"

printf '%s\n' "chezmoi path:$0" >>"$log_file"
printf '%s\n' "chezmoi args:$*" >>"$log_file"
case ":${PATH:-}:" in
  *":$HOME/.local/bin:"*)
    printf '%s\n' "chezmoi path_has_local_bin:yes" >>"$log_file"
    ;;
  *)
    printf '%s\n' "chezmoi path_has_local_bin:no" >>"$log_file"
    ;;
esac

case "${1-}" in
  init)
    printf '%s\n' "chezmoi init_repo:${2-}" >>"$log_file"
    source_dir="${XDG_DATA_HOME:-$HOME/.local/share}/chezmoi"
    mkdir -p "$source_dir/dot_local/bin"
    cat >"$source_dir/dot_local/bin/executable_terrapod" <<'TERRAPOD_STUB'
#!/bin/sh
set -eu

log_file="${TERRAPOD_STUB_CALL_LOG:?}"

printf '%s\n' "terrapod path:$0" >>"$log_file"
printf '%s\n' "terrapod args:$*" >>"$log_file"
printf '%s\n' "terrapod TERRAPOD_PROFILE:${TERRAPOD_PROFILE-}" >>"$log_file"
printf '%s\n' "terrapod TERRAPOD_CHEZMOI_CONFIG:${TERRAPOD_CHEZMOI_CONFIG-unset}" >>"$log_file"
case ":${PATH:-}:" in
  *":$HOME/.local/bin:"*)
    printf '%s\n' "terrapod path_has_local_bin:yes" >>"$log_file"
    ;;
  *)
    printf '%s\n' "terrapod path_has_local_bin:no" >>"$log_file"
    ;;
esac

case "${1-}" in
  setup)
    setup_stdin_line_number=0
    while IFS= read -r setup_stdin_line || [ -n "$setup_stdin_line" ]; do
      setup_stdin_line_number=$((setup_stdin_line_number + 1))
      printf '%s\n' "terrapod setup stdin $setup_stdin_line_number:$setup_stdin_line" >>"$log_file"
    done
    printf '%s\n' "terrapod setup stdin lines:$setup_stdin_line_number" >>"$log_file"

    setup_status="${TERRAPOD_SETUP_STUB_STATUS:-0}"
    if [ "$setup_status" != "0" ]; then
      printf '%s\n' "${TERRAPOD_SETUP_STUB_MESSAGE:-terrapod setup failed}" >&2
      exit "$setup_status"
    fi
    ;;
  configure)
    ;;
esac
TERRAPOD_STUB
    chmod +x "$source_dir/dot_local/bin/executable_terrapod"
    ;;
  apply)
    mkdir -p "$HOME/.local/bin"
    cat >"$HOME/.local/bin/tpod" <<'TPOD_STUB'
#!/bin/sh
set -eu

printf '%s\n' "tpod path:$0" >>"${TERRAPOD_STUB_CALL_LOG:?}"
printf '%s\n' "tpod args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
case ":${PATH:-}:" in
  *":$HOME/.local/bin:"*)
    printf '%s\n' "tpod path_has_local_bin:yes" >>"${TERRAPOD_STUB_CALL_LOG:?}"
    ;;
  *)
    printf '%s\n' "tpod path_has_local_bin:no" >>"${TERRAPOD_STUB_CALL_LOG:?}"
    ;;
esac

case "${1-}" in
  help|--help|-h)
    printf '%s\n' "tpod help output"
    ;;
  *)
    printf '%s\n' "unexpected tpod command:${1-}" >>"${TERRAPOD_STUB_CALL_LOG:?}"
    exit 64
    ;;
esac
TPOD_STUB
    chmod +x "$HOME/.local/bin/tpod"
    ;;
  *)
    printf '%s\n' "unexpected chezmoi command:${1-}" >>"$log_file"
    exit 64
    ;;
esac
EOF
  chmod +x "$stub"
}

write_installer_download_stubs() {
  case_dir="$1"

  cat >"$case_dir/bin/curl" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "curl args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
case "$*" in
  *get.chezmoi.io*)
    printf '%s\n' "# fake chezmoi installer from get.chezmoi.io"
    ;;
  *raw.githubusercontent.com/Homebrew/install/HEAD/install.sh*)
    homebrew_installer_status="${TERRAPOD_HOMEBREW_INSTALLER_STUB_STATUS:-0}"
    if [ "$homebrew_installer_status" != "0" ]; then
      exit "$homebrew_installer_status"
    fi

    cat <<'HOMEBREW_INSTALLER_STUB'
#!/bin/sh
set -eu

log_file="${TERRAPOD_STUB_CALL_LOG:?}"
brew_stub="${TERRAPOD_HOMEBREW_INSTALL_STUB_BREW:?}"
brew_stub_dir="${brew_stub%/*}"

printf '%s\n' "homebrew installer ran" >>"$log_file"
mkdir -p "$brew_stub_dir"
cat >"$brew_stub" <<'BREW_STUB'
#!/bin/sh
set -eu

log_file="${TERRAPOD_STUB_CALL_LOG:?}"
printf '%s\n' "brew args:$*" >>"$log_file"

case "${1-}" in
  shellenv)
    case "$0" in
      */*) stub_dir="${0%/*}" ;;
      *) stub_dir=. ;;
    esac
    printf '%s\n' "PATH=\"$stub_dir:\$PATH\"; export PATH"
    exit 0
    ;;
  install)
    printf '%s\n' "brew install HOMEBREW_NO_AUTO_UPDATE:${HOMEBREW_NO_AUTO_UPDATE-unset}" >>"$log_file"
    if [ "${2-}" != "gum" ]; then
      printf '%s\n' "unexpected brew install target:${2-}" >>"$log_file"
      exit 64
    fi
    case "$0" in
      */*) stub_dir="${0%/*}" ;;
      *) stub_dir=. ;;
    esac
    {
      printf '%s\n' '#!/bin/sh'
      printf '%s\n' 'exit 0'
    } >"$stub_dir/gum"
    chmod +x "$stub_dir/gum"
    ;;
  *)
    printf '%s\n' "unexpected brew command:${1-}" >>"$log_file"
    exit 64
    ;;
esac
BREW_STUB
chmod +x "$brew_stub"
HOMEBREW_INSTALLER_STUB
    ;;
  *)
    printf '%s\n' "unexpected curl args:$*" >>"${TERRAPOD_STUB_CALL_LOG:?}"
    exit 64
    ;;
esac
EOF
  chmod +x "$case_dir/bin/curl"

  cat >"$case_dir/bin/sh" <<'EOF'
#!/bin/sh
set -eu

log_file="${TERRAPOD_STUB_CALL_LOG:?}"
stdin_capture="${TERRAPOD_INSTALLER_STDIN_CAPTURE:?}"
script_capture="${TERRAPOD_INSTALLER_SCRIPT_CAPTURE:?}"
stub_template="${TERRAPOD_CHEZMOI_STUB_TEMPLATE:?}"

printf '%s\n' "sh args:$*" >>"$log_file"
for arg do
  printf '%s\n' "sh arg:$arg" >>"$log_file"
done

cat >"$stdin_capture"
: >"$script_capture"
if [ "${1-}" = "-c" ]; then
  printf '%s\n' "${2-}" >"$script_capture"
fi

bin_dir=""
want_bin="no"
for arg do
  if [ "$want_bin" = "yes" ]; then
    bin_dir="$arg"
    want_bin="no"
    continue
  fi

  if [ "$arg" = "-b" ]; then
    want_bin="yes"
  fi
done

if [ -z "$bin_dir" ]; then
  printf '%s\n' "missing -b bin dir" >>"$log_file"
  exit 64
fi

printf '%s\n' "sh selected_bin:$bin_dir" >>"$log_file"
mkdir -p "$bin_dir"
cp "$stub_template" "$bin_dir/chezmoi"
chmod +x "$bin_dir/chezmoi"
EOF
  chmod +x "$case_dir/bin/sh"
}

write_gum_command_stub() {
  case_dir="$1"
  stub="$case_dir/bin/gum"

  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' 'exit 0'
  } >"$stub"
  chmod +x "$stub"
}

write_macos_brew_gum_stubs() {
  case_dir="$1"
  gum_install_status="${2:-0}"
  stub="${3:-$case_dir/bin/brew}"
  shellenv_status="${4:-0}"
  stub_dir="${stub%/*}"
  mkdir -p "$stub_dir"

  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' 'set -eu'
    printf '%s\n' 'log_file="${TERRAPOD_STUB_CALL_LOG:?}"'
    printf '%s\n' 'printf "%s\n" "brew args:$*" >>"$log_file"'
    printf '%s\n' 'case "${1-}" in'
    printf '%s\n' '  shellenv)'
    printf '%s\n' '    case "$0" in'
    printf '%s\n' '      */*) stub_dir="${0%/*}" ;;'
    printf '%s\n' '      *) stub_dir=. ;;'
    printf '%s\n' '    esac'
    printf '%s\n' "    if [ '$shellenv_status' != '0' ]; then"
    printf '%s\n' "      exit '$shellenv_status'"
    printf '%s\n' '    fi'
    printf '%s\n' '    printf "%s\n" "PATH=\"$stub_dir:\$PATH\"; export PATH"'
    printf '%s\n' '    exit 0'
    printf '%s\n' '    ;;'
    printf '%s\n' '  install)'
    printf '%s\n' '    if [ "${2-}" != "gum" ]; then'
    printf '%s\n' '      printf "%s\n" "unexpected brew install target:${2-}" >>"$log_file"'
    printf '%s\n' '      exit 64'
    printf '%s\n' '    fi'
    printf '%s\n' '    printf "%s\n" "brew install HOMEBREW_NO_AUTO_UPDATE:${HOMEBREW_NO_AUTO_UPDATE-unset}" >>"$log_file"'
    printf '%s\n' "    if [ '$gum_install_status' != '0' ]; then"
    printf '%s\n' "      exit '$gum_install_status'"
    printf '%s\n' '    fi'
    printf '%s\n' '    case "$0" in'
    printf '%s\n' '      */*) stub_dir="${0%/*}" ;;'
    printf '%s\n' '      *) stub_dir=. ;;'
    printf '%s\n' '    esac'
    printf '%s\n' '    {'
    printf '%s\n' "      printf '%s\n' '#!/bin/sh'"
    printf '%s\n' "      printf '%s\n' 'exit 0'"
    printf '%s\n' '    } >"$stub_dir/gum"'
    printf '%s\n' '    chmod +x "$stub_dir/gum"'
    printf '%s\n' '    ;;'
    printf '%s\n' '  *)'
    printf '%s\n' '    printf "%s\n" "unexpected brew command:${1-}" >>"$log_file"'
    printf '%s\n' '    exit 64'
    printf '%s\n' '    ;;'
    printf '%s\n' 'esac'
  } >"$stub"
  chmod +x "$stub"
}

run_installer_case() {
  case_dir="$1"
  input_text="
"
  stdout_file="$case_dir/stdout"
  stderr_file="$case_dir/stderr"

  if [ "$#" -gt 1 ]; then
    input_text="$2"
  fi

  if printf '%s' "$input_text" | PATH="$case_dir/bin:$safe_path_dir" \
    HOME="$case_dir/home" \
    XDG_DATA_HOME="$case_dir/xdg-data" \
    XDG_CONFIG_HOME="$case_dir/xdg-config" \
    "$install_script" >"$stdout_file" 2>"$stderr_file"; then
    installer_status=0
  else
    installer_status=$?
  fi
}

darwin_case="$(make_case_dir darwin-profile)"
write_uname_stub "$darwin_case" "Darwin"
write_command_call_stubs "$darwin_case" "curl" "wget" "git" "sh"
write_gum_command_stub "$darwin_case"
mkdir -p "$darwin_case/home/.local/bin"
write_chezmoi_flow_stub "$darwin_case/home/.local/bin/chezmoi"
darwin_log="$darwin_case/command-calls"
TERRAPOD_STUB_CALL_LOG="$darwin_log"
export TERRAPOD_STUB_CALL_LOG
darwin_input='minimal
'
run_installer_case "$darwin_case" "$darwin_input"
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "Darwin profile is supported"
darwin_stdout="$(cat "$darwin_case/stdout")"
assert_contains "$darwin_stdout" "Profile: macOS Terminal Profile" "Darwin profile label is printed"
if [ ! -d "$darwin_case/home/.local/bin" ]; then
  fail "Darwin installer creates user local bin directory"
fi
pass "Darwin installer creates user local bin directory"

ubuntu_case="$(make_case_dir ubuntu-profile)"
write_uname_stub "$ubuntu_case" "Linux"
ubuntu_os_release="$(write_os_release "$ubuntu_case" "ID=ubuntu" 'VERSION_ID="24.04"')"
write_command_call_stubs "$ubuntu_case" "curl" "wget" "git" "gum" "sh"
mkdir -p "$ubuntu_case/home/.local/bin"
write_chezmoi_flow_stub "$ubuntu_case/home/.local/bin/chezmoi"
TERRAPOD_OS_RELEASE_FILE="$ubuntu_os_release"
TERRAPOD_STUB_CALL_LOG="$ubuntu_case/command-calls"
export TERRAPOD_OS_RELEASE_FILE
export TERRAPOD_STUB_CALL_LOG
ubuntu_input='development
'
run_installer_case "$ubuntu_case" "$ubuntu_input"
unset TERRAPOD_OS_RELEASE_FILE
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "Ubuntu 24.04 profile is supported"
ubuntu_stdout="$(cat "$ubuntu_case/stdout")"
assert_contains "$ubuntu_stdout" "Profile: VPS Shell Profile" "Ubuntu 24.04 profile label is printed"

ubuntu_missing_git_case="$(make_case_dir ubuntu-missing-git)"
write_uname_stub "$ubuntu_missing_git_case" "Linux"
ubuntu_missing_git_os_release="$(write_os_release "$ubuntu_missing_git_case" "ID=ubuntu" 'VERSION_ID="24.04"')"
write_command_call_stubs "$ubuntu_missing_git_case" "curl" "wget" "git" "sh"
rm -f "$ubuntu_missing_git_case/bin/git"
write_ubuntu_package_stubs "$ubuntu_missing_git_case"
mkdir -p "$ubuntu_missing_git_case/home/.local/bin"
write_chezmoi_flow_stub "$ubuntu_missing_git_case/home/.local/bin/chezmoi"
ubuntu_missing_git_log="$ubuntu_missing_git_case/command-calls"
TERRAPOD_OS_RELEASE_FILE="$ubuntu_missing_git_os_release"
TERRAPOD_STUB_CALL_LOG="$ubuntu_missing_git_log"
export TERRAPOD_OS_RELEASE_FILE
export TERRAPOD_STUB_CALL_LOG
ubuntu_missing_git_input='development
'
run_installer_case "$ubuntu_missing_git_case" "$ubuntu_missing_git_input"
unset TERRAPOD_OS_RELEASE_FILE
unset TERRAPOD_STUB_CALL_LOG
assert_status "$installer_status" 0 "Ubuntu installer installs source prerequisites when git is missing"
ubuntu_missing_git_log_text="$(cat "$ubuntu_missing_git_log")"
assert_contains "$ubuntu_missing_git_log_text" "sudo args:apt-get update -y" "Ubuntu missing git case may use interactive sudo for package metadata"
assert_contains "$ubuntu_missing_git_log_text" "apt-get args:update -y" "Ubuntu missing git case updates package metadata"
assert_contains "$ubuntu_missing_git_log_text" "sudo args:apt-get install -y ca-certificates curl git gpg" "Ubuntu missing git case may use interactive sudo for bootstrap packages"
assert_contains "$ubuntu_missing_git_log_text" "apt-get args:install -y ca-certificates curl git gpg" "Ubuntu missing git case installs source and bootstrap UI dependencies"
assert_contains "$ubuntu_missing_git_log_text" "install args:-dm 755 /etc/apt/keyrings" "Ubuntu missing git case creates an APT keyring directory"
assert_contains "$ubuntu_missing_git_log_text" "curl args:-fsSL https://repo.charm.sh/apt/gpg.key -o " "Ubuntu missing git case fetches the Charm APT signing key"
assert_contains "$ubuntu_missing_git_log_text" "gpg args:--dearmor --yes -o /etc/apt/keyrings/charm.gpg " "Ubuntu missing git case dearmors the Charm APT signing key"
assert_contains "$ubuntu_missing_git_log_text" "tee args:/etc/apt/sources.list.d/charm.list" "Ubuntu missing git case writes the Charm APT source list"
assert_contains "$ubuntu_missing_git_log_text" "tee stdin:deb [signed-by=/etc/apt/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *" "Ubuntu missing git case pins the Charm repository to its keyring"
assert_contains "$ubuntu_missing_git_log_text" "apt-get args:install -y gum" "Ubuntu missing git case installs gum through APT"
assert_first_occurrence_before "$ubuntu_missing_git_log_text" "apt-get args:install -y ca-certificates curl git gpg" "chezmoi args:init https://github.com/juty9026/terrapod.git" "Ubuntu missing git case installs git before chezmoi init"
assert_first_occurrence_before "$ubuntu_missing_git_log_text" "tee args:/etc/apt/sources.list.d/charm.list" "apt-get args:install -y gum" "Ubuntu missing git case adds the Charm repository before installing gum"
assert_first_occurrence_before "$ubuntu_missing_git_log_text" "apt-get args:install -y gum" "terrapod args:setup" "Ubuntu missing git case installs gum before Terrapod Setup"
assert_first_occurrence_before "$ubuntu_missing_git_log_text" "apt-get args:install -y gum" "chezmoi args:apply" "Ubuntu missing git case installs gum before initial apply"

ubuntu_missing_sudo_case="$(make_case_dir ubuntu-missing-sudo)"
write_uname_stub "$ubuntu_missing_sudo_case" "Linux"
ubuntu_missing_sudo_os_release="$(write_os_release "$ubuntu_missing_sudo_case" "ID=ubuntu" 'VERSION_ID="24.04"')"
write_command_call_stubs "$ubuntu_missing_sudo_case" "curl" "wget" "sh"
write_non_root_id_stub "$ubuntu_missing_sudo_case"
mkdir -p "$ubuntu_missing_sudo_case/home/.local/bin"
write_chezmoi_flow_stub "$ubuntu_missing_sudo_case/home/.local/bin/chezmoi"
ubuntu_missing_sudo_log="$ubuntu_missing_sudo_case/command-calls"
: >"$ubuntu_missing_sudo_log"
TERRAPOD_OS_RELEASE_FILE="$ubuntu_missing_sudo_os_release"
TERRAPOD_STUB_CALL_LOG="$ubuntu_missing_sudo_log"
export TERRAPOD_OS_RELEASE_FILE
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$ubuntu_missing_sudo_case" 'development
'
unset TERRAPOD_OS_RELEASE_FILE
unset TERRAPOD_STUB_CALL_LOG
assert_failure "$installer_status" "Ubuntu missing sudo makes installer exit unsuccessfully before Setup"
ubuntu_missing_sudo_stderr="$(cat "$ubuntu_missing_sudo_case/stderr")"
ubuntu_missing_sudo_log_text="$(cat "$ubuntu_missing_sudo_log")"
assert_contains "$ubuntu_missing_sudo_stderr" "Install sudo so Terrapod can prepare git and gum with apt-get" "Ubuntu missing sudo guidance mentions installing sudo"
assert_contains "$ubuntu_missing_sudo_stderr" "install git and gum manually before rerunning the installer" "Ubuntu missing sudo guidance mentions manual git and gum preparation"
assert_not_contains "$ubuntu_missing_sudo_log_text" "chezmoi args:init" "Ubuntu missing sudo stops before source repository initialization"
assert_not_contains "$ubuntu_missing_sudo_log_text" "terrapod args:setup" "Ubuntu missing sudo stops before Terrapod Setup"
assert_not_contains "$ubuntu_missing_sudo_log_text" "chezmoi args:apply" "Ubuntu missing sudo stops before initial apply"

ubuntu_sudo_failure_case="$(make_case_dir ubuntu-sudo-failure)"
write_uname_stub "$ubuntu_sudo_failure_case" "Linux"
ubuntu_sudo_failure_os_release="$(write_os_release "$ubuntu_sudo_failure_case" "ID=ubuntu" 'VERSION_ID="24.04"')"
write_command_call_stubs "$ubuntu_sudo_failure_case" "curl" "wget" "git" "sh"
rm -f "$ubuntu_sudo_failure_case/bin/git"
write_ubuntu_package_stubs "$ubuntu_sudo_failure_case"
mkdir -p "$ubuntu_sudo_failure_case/home/.local/bin"
write_chezmoi_flow_stub "$ubuntu_sudo_failure_case/home/.local/bin/chezmoi"
ubuntu_sudo_failure_log="$ubuntu_sudo_failure_case/command-calls"
TERRAPOD_OS_RELEASE_FILE="$ubuntu_sudo_failure_os_release"
TERRAPOD_STUB_CALL_LOG="$ubuntu_sudo_failure_log"
TERRAPOD_SUDO_STUB_STATUS=17
export TERRAPOD_OS_RELEASE_FILE
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_SUDO_STUB_STATUS
run_installer_case "$ubuntu_sudo_failure_case" 'development
'
unset TERRAPOD_OS_RELEASE_FILE
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_SUDO_STUB_STATUS
assert_failure "$installer_status" "Ubuntu sudo failure makes installer exit unsuccessfully before Setup"
ubuntu_sudo_failure_stderr="$(cat "$ubuntu_sudo_failure_case/stderr")"
ubuntu_sudo_failure_log_text="$(cat "$ubuntu_sudo_failure_log")"
assert_contains "$ubuntu_sudo_failure_log_text" "sudo args:apt-get update -y" "Ubuntu sudo failure attempts prerequisite preparation with sudo"
assert_contains "$ubuntu_sudo_failure_stderr" "failed to update APT metadata before installing Ubuntu bootstrap prerequisites" "Ubuntu sudo failure explains prerequisite preparation failure"
assert_contains "$ubuntu_sudo_failure_stderr" "Check sudo permissions and rerun the Terrapod installer before Terrapod Setup" "Ubuntu sudo failure gives sudo recovery guidance"
assert_not_contains "$ubuntu_sudo_failure_log_text" "chezmoi args:init" "Ubuntu sudo failure stops before source repository initialization"
assert_not_contains "$ubuntu_sudo_failure_log_text" "terrapod args:setup" "Ubuntu sudo failure stops before Terrapod Setup"
assert_not_contains "$ubuntu_sudo_failure_log_text" "chezmoi args:apply" "Ubuntu sudo failure stops before initial apply"

ubuntu_charm_repo_failure_case="$(make_case_dir ubuntu-charm-repo-failure)"
write_uname_stub "$ubuntu_charm_repo_failure_case" "Linux"
ubuntu_charm_repo_failure_os_release="$(write_os_release "$ubuntu_charm_repo_failure_case" "ID=ubuntu" 'VERSION_ID="24.04"')"
write_command_call_stubs "$ubuntu_charm_repo_failure_case" "wget" "git" "sh"
write_ubuntu_package_stubs "$ubuntu_charm_repo_failure_case"
mkdir -p "$ubuntu_charm_repo_failure_case/home/.local/bin"
write_chezmoi_flow_stub "$ubuntu_charm_repo_failure_case/home/.local/bin/chezmoi"
ubuntu_charm_repo_failure_log="$ubuntu_charm_repo_failure_case/command-calls"
TERRAPOD_OS_RELEASE_FILE="$ubuntu_charm_repo_failure_os_release"
TERRAPOD_STUB_CALL_LOG="$ubuntu_charm_repo_failure_log"
TERRAPOD_GPG_STUB_STATUS=17
export TERRAPOD_OS_RELEASE_FILE
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_GPG_STUB_STATUS
run_installer_case "$ubuntu_charm_repo_failure_case" 'development
'
unset TERRAPOD_OS_RELEASE_FILE
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_GPG_STUB_STATUS
assert_failure "$installer_status" "Ubuntu Charm repository failure makes installer exit unsuccessfully"
ubuntu_charm_repo_failure_stderr="$(cat "$ubuntu_charm_repo_failure_case/stderr")"
ubuntu_charm_repo_failure_log_text="$(cat "$ubuntu_charm_repo_failure_log")"
assert_contains "$ubuntu_charm_repo_failure_stderr" "failed to install the Charm APT signing key" "Ubuntu Charm repository failure explains the failed trust boundary"
assert_contains "$ubuntu_charm_repo_failure_stderr" "rerun the Terrapod installer before Terrapod Setup" "Ubuntu Charm repository failure gives setup recovery guidance"
assert_not_contains "$ubuntu_charm_repo_failure_log_text" "terrapod args:setup" "Ubuntu Charm repository failure stops before Terrapod Setup"
assert_not_contains "$ubuntu_charm_repo_failure_log_text" "chezmoi args:apply" "Ubuntu Charm repository failure stops before initial apply"

ubuntu_charm_key_fetch_failure_case="$(make_case_dir ubuntu-charm-key-fetch-failure)"
write_uname_stub "$ubuntu_charm_key_fetch_failure_case" "Linux"
ubuntu_charm_key_fetch_failure_os_release="$(write_os_release "$ubuntu_charm_key_fetch_failure_case" "ID=ubuntu" 'VERSION_ID="24.04"')"
write_command_call_stubs "$ubuntu_charm_key_fetch_failure_case" "wget" "git" "sh"
write_ubuntu_package_stubs "$ubuntu_charm_key_fetch_failure_case"
mkdir -p "$ubuntu_charm_key_fetch_failure_case/home/.local/bin"
write_chezmoi_flow_stub "$ubuntu_charm_key_fetch_failure_case/home/.local/bin/chezmoi"
ubuntu_charm_key_fetch_failure_log="$ubuntu_charm_key_fetch_failure_case/command-calls"
TERRAPOD_OS_RELEASE_FILE="$ubuntu_charm_key_fetch_failure_os_release"
TERRAPOD_STUB_CALL_LOG="$ubuntu_charm_key_fetch_failure_log"
TERRAPOD_CHARM_KEY_STUB_STATUS=17
export TERRAPOD_OS_RELEASE_FILE
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_CHARM_KEY_STUB_STATUS
run_installer_case "$ubuntu_charm_key_fetch_failure_case" 'development
'
unset TERRAPOD_OS_RELEASE_FILE
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_CHARM_KEY_STUB_STATUS
assert_failure "$installer_status" "Ubuntu Charm key fetch failure makes installer exit unsuccessfully"
ubuntu_charm_key_fetch_failure_stderr="$(cat "$ubuntu_charm_key_fetch_failure_case/stderr")"
ubuntu_charm_key_fetch_failure_log_text="$(cat "$ubuntu_charm_key_fetch_failure_log")"
assert_contains "$ubuntu_charm_key_fetch_failure_stderr" "failed to fetch the Charm APT signing key" "Ubuntu Charm key fetch failure explains the failed download"
assert_contains "$ubuntu_charm_key_fetch_failure_stderr" "rerun the Terrapod installer before Terrapod Setup" "Ubuntu Charm key fetch failure gives setup recovery guidance"
assert_contains "$ubuntu_charm_key_fetch_failure_log_text" "curl args:-fsSL https://repo.charm.sh/apt/gpg.key -o " "Ubuntu Charm key fetch failure attempts to fetch the signing key"
assert_not_contains "$ubuntu_charm_key_fetch_failure_log_text" "apt-get args:install -y gum" "Ubuntu Charm key fetch failure does not install gum"
assert_not_contains "$ubuntu_charm_key_fetch_failure_log_text" "terrapod args:setup" "Ubuntu Charm key fetch failure stops before Terrapod Setup"
assert_not_contains "$ubuntu_charm_key_fetch_failure_log_text" "chezmoi args:apply" "Ubuntu Charm key fetch failure stops before initial apply"

ubuntu_gum_failure_case="$(make_case_dir ubuntu-gum-failure)"
write_uname_stub "$ubuntu_gum_failure_case" "Linux"
ubuntu_gum_failure_os_release="$(write_os_release "$ubuntu_gum_failure_case" "ID=ubuntu" 'VERSION_ID="24.04"')"
write_command_call_stubs "$ubuntu_gum_failure_case" "wget" "git" "sh"
write_ubuntu_package_stubs "$ubuntu_gum_failure_case"
mkdir -p "$ubuntu_gum_failure_case/home/.local/bin"
write_chezmoi_flow_stub "$ubuntu_gum_failure_case/home/.local/bin/chezmoi"
ubuntu_gum_failure_log="$ubuntu_gum_failure_case/command-calls"
TERRAPOD_OS_RELEASE_FILE="$ubuntu_gum_failure_os_release"
TERRAPOD_STUB_CALL_LOG="$ubuntu_gum_failure_log"
TERRAPOD_APT_FAIL_INSTALL_GUM=1
export TERRAPOD_OS_RELEASE_FILE
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_APT_FAIL_INSTALL_GUM
run_installer_case "$ubuntu_gum_failure_case" 'development
'
unset TERRAPOD_OS_RELEASE_FILE
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_APT_FAIL_INSTALL_GUM
assert_failure "$installer_status" "Ubuntu gum install failure makes installer exit unsuccessfully"
ubuntu_gum_failure_stderr="$(cat "$ubuntu_gum_failure_case/stderr")"
ubuntu_gum_failure_log_text="$(cat "$ubuntu_gum_failure_log")"
assert_contains "$ubuntu_gum_failure_stderr" "failed to install gum from the Charm APT repository" "Ubuntu gum install failure explains the failed package install"
assert_contains "$ubuntu_gum_failure_stderr" "rerun the Terrapod installer before Terrapod Setup" "Ubuntu gum install failure gives setup recovery guidance"
assert_contains "$ubuntu_gum_failure_log_text" "apt-get args:install -y gum" "Ubuntu gum install failure attempts to install gum"
assert_not_contains "$ubuntu_gum_failure_log_text" "terrapod args:setup" "Ubuntu gum install failure stops before Terrapod Setup"
assert_not_contains "$ubuntu_gum_failure_log_text" "chezmoi args:apply" "Ubuntu gum install failure stops before initial apply"

first_run_case="$(make_case_dir first-run-macos)"
write_uname_stub "$first_run_case" "Darwin"
write_chezmoi_flow_stub "$first_run_case/chezmoi-template"
write_installer_download_stubs "$first_run_case"
write_command_call_stubs "$first_run_case" "wget" "git"
write_macos_brew_gum_stubs "$first_run_case" 0
first_run_log="$first_run_case/command-calls"
first_run_stdin_capture="$first_run_case/installer-stdin"
first_run_script_capture="$first_run_case/installer-script"
TERRAPOD_STUB_CALL_LOG="$first_run_log"
TERRAPOD_INSTALLER_STDIN_CAPTURE="$first_run_stdin_capture"
TERRAPOD_INSTALLER_SCRIPT_CAPTURE="$first_run_script_capture"
TERRAPOD_CHEZMOI_STUB_TEMPLATE="$first_run_case/chezmoi-template"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_INSTALLER_STDIN_CAPTURE
export TERRAPOD_INSTALLER_SCRIPT_CAPTURE
export TERRAPOD_CHEZMOI_STUB_TEMPLATE
first_run_input='workstation





y
'
run_installer_case "$first_run_case" "$first_run_input"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_INSTALLER_STDIN_CAPTURE
unset TERRAPOD_INSTALLER_SCRIPT_CAPTURE
unset TERRAPOD_CHEZMOI_STUB_TEMPLATE
assert_status "$installer_status" 0 "stubbed macOS workstation first-run flow completes"
first_run_stdout="$(cat "$first_run_case/stdout")"
assert_contains "$first_run_stdout" "Terrapod first-run apply complete." "first-run completion is printed"
first_run_log_text="$(cat "$first_run_log")"
assert_contains "$first_run_log_text" "curl args:-fsLS get.chezmoi.io" "chezmoi installer is downloaded from get.chezmoi.io"
assert_line "$first_run_log_text" "sh arg:--" "chezmoi installer receives --"
assert_line "$first_run_log_text" "sh arg:-b" "chezmoi installer receives -b"
assert_line "$first_run_log_text" "sh arg:$first_run_case/home/.local/bin" "chezmoi installer receives user local bin"
first_run_installer_payload="$(
  cat "$first_run_stdin_capture" 2>/dev/null
  cat "$first_run_script_capture" 2>/dev/null
)"
assert_contains "$first_run_installer_payload" "fake chezmoi installer from get.chezmoi.io" "chezmoi installer script is captured"
if [ ! -x "$first_run_case/home/.local/bin/chezmoi" ]; then
  fail "stubbed chezmoi installer creates user-local chezmoi"
fi
pass "stubbed chezmoi installer creates user-local chezmoi"
assert_all_chezmoi_paths_equal "$first_run_log_text" "$first_run_case/home/.local/bin/chezmoi" "only user-local chezmoi is invoked"
assert_contains "$first_run_log_text" "chezmoi args:init https://github.com/juty9026/terrapod.git" "chezmoi init receives source repository"
if [ ! -x "$first_run_case/xdg-data/chezmoi/dot_local/bin/executable_terrapod" ]; then
  fail "chezmoi init creates checked-out Terrapod executable"
fi
pass "chezmoi init creates checked-out Terrapod executable"
assert_contains "$first_run_log_text" "terrapod TERRAPOD_PROFILE:macos-terminal" "setup receives macOS Terrapod profile"
assert_contains "$first_run_log_text" "terrapod TERRAPOD_CHEZMOI_CONFIG:" "setup receives an empty Terrapod chezmoi config override"
assert_contains "$first_run_log_text" "brew args:shellenv" "macOS first-run setup UI bootstrap evaluates Homebrew shellenv"
assert_contains "$first_run_log_text" "brew args:install gum" "macOS first-run setup UI bootstrap installs gum with Homebrew when gum is missing"
assert_contains "$first_run_log_text" "brew install HOMEBREW_NO_AUTO_UPDATE:1" "macOS first-run setup UI bootstrap disables Homebrew auto-update while installing gum"
assert_contains "$first_run_log_text" "terrapod args:setup" "checked-out Terrapod Setup runs"
assert_contains "$first_run_log_text" "terrapod setup stdin 1:workstation" "checked-out Terrapod Setup receives Preset input"
assert_contains "$first_run_log_text" "terrapod setup stdin 7:y" "checked-out Terrapod Setup receives final confirmation input"
assert_contains "$first_run_log_text" "terrapod setup stdin lines:7" "checked-out Terrapod Setup receives the full workstation setup input"
assert_not_contains "$first_run_log_text" "terrapod args:configure" "first-run installer does not bypass setup with configure"
assert_first_occurrence_before "$first_run_log_text" "chezmoi args:init https://github.com/juty9026/terrapod.git" "terrapod args:setup" "setup runs after source repository initialization"
assert_first_occurrence_before "$first_run_log_text" "brew args:install gum" "terrapod args:setup" "macOS setup UI gum bootstrap runs before Terrapod Setup"
assert_first_occurrence_before "$first_run_log_text" "terrapod args:setup" "chezmoi args:apply" "setup runs before chezmoi apply"
assert_first_occurrence_before "$first_run_log_text" "brew args:install gum" "chezmoi args:apply" "macOS setup UI gum bootstrap runs before initial apply"
assert_contains "$first_run_log_text" "chezmoi args:apply" "chezmoi apply runs after setup"
assert_first_occurrence_before "$first_run_log_text" "chezmoi args:apply" "tpod args:help" "first-run installer shows tpod help after initial apply"
assert_contains "$first_run_log_text" "tpod path:$first_run_case/home/.local/bin/tpod" "first-run installer invokes installed tpod from user local bin"
assert_contains "$first_run_log_text" "tpod path_has_local_bin:yes" "tpod help receives PATH containing user local bin"
assert_contains "$first_run_stdout" "tpod help output" "first-run installer prints tpod help after initial apply"
assert_contains "$first_run_log_text" "chezmoi path_has_local_bin:yes" "child command PATH contains user local bin"
assert_not_contains "$first_run_log_text" "brew args:upgrade" "first-run installer does not run broad Homebrew upgrades"
assert_not_contains "$first_run_log_text" "brew args:bundle" "first-run installer leaves Brewfile bundle to initial apply"

homebrew_missing_case="$(make_case_dir homebrew-missing-macos)"
write_uname_stub "$homebrew_missing_case" "Darwin"
write_chezmoi_flow_stub "$homebrew_missing_case/chezmoi-template"
write_installer_download_stubs "$homebrew_missing_case"
write_command_call_stubs "$homebrew_missing_case" "wget" "git"
homebrew_missing_log="$homebrew_missing_case/command-calls"
homebrew_missing_stdin_capture="$homebrew_missing_case/installer-stdin"
homebrew_missing_script_capture="$homebrew_missing_case/installer-script"
homebrew_missing_brew="$homebrew_missing_case/homebrew/bin/brew"
TERRAPOD_STUB_CALL_LOG="$homebrew_missing_log"
TERRAPOD_INSTALLER_STDIN_CAPTURE="$homebrew_missing_stdin_capture"
TERRAPOD_INSTALLER_SCRIPT_CAPTURE="$homebrew_missing_script_capture"
TERRAPOD_CHEZMOI_STUB_TEMPLATE="$homebrew_missing_case/chezmoi-template"
TERRAPOD_HOMEBREW_CANDIDATE_PATHS="$homebrew_missing_brew"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_INSTALLER_STDIN_CAPTURE
export TERRAPOD_INSTALLER_SCRIPT_CAPTURE
export TERRAPOD_CHEZMOI_STUB_TEMPLATE
export TERRAPOD_HOMEBREW_CANDIDATE_PATHS
homebrew_missing_input='minimal
'
run_installer_case "$homebrew_missing_case" "$homebrew_missing_input"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_INSTALLER_STDIN_CAPTURE
unset TERRAPOD_INSTALLER_SCRIPT_CAPTURE
unset TERRAPOD_CHEZMOI_STUB_TEMPLATE
unset TERRAPOD_HOMEBREW_CANDIDATE_PATHS
assert_failure "$installer_status" "macOS first-run fails before Setup when Homebrew is missing"
homebrew_missing_stderr="$(cat "$homebrew_missing_case/stderr")"
homebrew_missing_log_text="$(cat "$homebrew_missing_log")"
assert_not_contains "$homebrew_missing_log_text" "curl args:-fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh" "Homebrew missing case does not download the Homebrew installer"
assert_not_contains "$homebrew_missing_log_text" "homebrew installer ran" "Homebrew missing case does not run the Homebrew installer"
assert_not_contains "$homebrew_missing_log_text" "brew args:shellenv" "Homebrew missing case cannot evaluate Homebrew shellenv"
assert_not_contains "$homebrew_missing_log_text" "brew args:install gum" "Homebrew missing case cannot install gum with Homebrew"
assert_not_contains "$homebrew_missing_log_text" "terrapod args:setup" "Homebrew missing case stops before Terrapod Setup"
assert_not_contains "$homebrew_missing_log_text" "chezmoi args:apply" "Homebrew missing case stops before initial apply"
assert_contains "$homebrew_missing_stderr" "Homebrew was not found" "Homebrew missing case explains missing Homebrew"
assert_contains "$homebrew_missing_stderr" "Install Homebrew from https://brew.sh, follow its shellenv instructions, then run: HOMEBREW_NO_AUTO_UPDATE=1 brew install gum" "Homebrew missing case guidance mentions manual Homebrew and gum preparation"
assert_contains "$homebrew_missing_stderr" "cd \"$homebrew_missing_case/xdg-data/chezmoi\" && TERRAPOD_PROFILE=\"macos-terminal\" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup && \"$homebrew_missing_case/home/.local/bin/chezmoi\" apply" "Homebrew missing case prints setup and initial apply recovery command"
assert_not_contains "$homebrew_missing_stderr" "Rerun the installer" "Homebrew missing case does not suggest rerunning the source-guarded installer"

homebrew_shellenv_failure_case="$(make_case_dir homebrew-shellenv-failure)"
write_uname_stub "$homebrew_shellenv_failure_case" "Darwin"
write_chezmoi_flow_stub "$homebrew_shellenv_failure_case/chezmoi-template"
write_installer_download_stubs "$homebrew_shellenv_failure_case"
write_command_call_stubs "$homebrew_shellenv_failure_case" "wget" "git"
write_macos_brew_gum_stubs "$homebrew_shellenv_failure_case" 0 "$homebrew_shellenv_failure_case/bin/brew" 31
homebrew_shellenv_failure_log="$homebrew_shellenv_failure_case/command-calls"
homebrew_shellenv_failure_stdin_capture="$homebrew_shellenv_failure_case/installer-stdin"
homebrew_shellenv_failure_script_capture="$homebrew_shellenv_failure_case/installer-script"
TERRAPOD_STUB_CALL_LOG="$homebrew_shellenv_failure_log"
TERRAPOD_INSTALLER_STDIN_CAPTURE="$homebrew_shellenv_failure_stdin_capture"
TERRAPOD_INSTALLER_SCRIPT_CAPTURE="$homebrew_shellenv_failure_script_capture"
TERRAPOD_CHEZMOI_STUB_TEMPLATE="$homebrew_shellenv_failure_case/chezmoi-template"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_INSTALLER_STDIN_CAPTURE
export TERRAPOD_INSTALLER_SCRIPT_CAPTURE
export TERRAPOD_CHEZMOI_STUB_TEMPLATE
homebrew_shellenv_failure_input='minimal
'
run_installer_case "$homebrew_shellenv_failure_case" "$homebrew_shellenv_failure_input"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_INSTALLER_STDIN_CAPTURE
unset TERRAPOD_INSTALLER_SCRIPT_CAPTURE
unset TERRAPOD_CHEZMOI_STUB_TEMPLATE
assert_failure "$installer_status" "Homebrew shellenv failure makes installer exit unsuccessfully"
homebrew_shellenv_failure_stderr="$(cat "$homebrew_shellenv_failure_case/stderr")"
homebrew_shellenv_failure_log_text="$(cat "$homebrew_shellenv_failure_log")"
assert_contains "$homebrew_shellenv_failure_log_text" "brew args:shellenv" "Homebrew shellenv failure attempts to evaluate Homebrew shellenv"
assert_not_contains "$homebrew_shellenv_failure_log_text" "brew args:install gum" "Homebrew shellenv failure stops before gum install"
assert_not_contains "$homebrew_shellenv_failure_log_text" "terrapod args:setup" "Homebrew shellenv failure stops before Terrapod Setup"
assert_not_contains "$homebrew_shellenv_failure_log_text" "chezmoi args:apply" "Homebrew shellenv failure stops before initial apply"
assert_contains "$homebrew_shellenv_failure_stderr" "failed to evaluate brew shellenv" "Homebrew shellenv failure explains the failed step"
homebrew_shellenv_failure_prepare_command='eval "$("'"$homebrew_shellenv_failure_case"'/bin/brew" shellenv)" && HOMEBREW_NO_AUTO_UPDATE=1 "'"$homebrew_shellenv_failure_case"'/bin/brew" install gum'
assert_contains "$homebrew_shellenv_failure_stderr" "$homebrew_shellenv_failure_prepare_command" "Homebrew shellenv failure guidance includes known Homebrew shellenv and gum install command"
homebrew_shellenv_failure_resume_command='cd "'"$homebrew_shellenv_failure_case"'/xdg-data/chezmoi" && eval "$("'"$homebrew_shellenv_failure_case"'/bin/brew" shellenv)" && TERRAPOD_PROFILE="macos-terminal" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup && "'"$homebrew_shellenv_failure_case"'/home/.local/bin/chezmoi" apply'
assert_contains "$homebrew_shellenv_failure_stderr" "$homebrew_shellenv_failure_resume_command" "Homebrew shellenv failure resume command keeps Homebrew shellenv active through setup and apply"
assert_not_contains "$homebrew_shellenv_failure_stderr" "Rerun the installer" "Homebrew shellenv failure does not suggest rerunning the source-guarded installer"

gum_bootstrap_failure_case="$(make_case_dir gum-bootstrap-failure)"
write_uname_stub "$gum_bootstrap_failure_case" "Darwin"
write_chezmoi_flow_stub "$gum_bootstrap_failure_case/chezmoi-template"
write_installer_download_stubs "$gum_bootstrap_failure_case"
write_command_call_stubs "$gum_bootstrap_failure_case" "wget" "git"
write_macos_brew_gum_stubs "$gum_bootstrap_failure_case" 23
gum_bootstrap_failure_log="$gum_bootstrap_failure_case/command-calls"
gum_bootstrap_failure_stdin_capture="$gum_bootstrap_failure_case/installer-stdin"
gum_bootstrap_failure_script_capture="$gum_bootstrap_failure_case/installer-script"
TERRAPOD_STUB_CALL_LOG="$gum_bootstrap_failure_log"
TERRAPOD_INSTALLER_STDIN_CAPTURE="$gum_bootstrap_failure_stdin_capture"
TERRAPOD_INSTALLER_SCRIPT_CAPTURE="$gum_bootstrap_failure_script_capture"
TERRAPOD_CHEZMOI_STUB_TEMPLATE="$gum_bootstrap_failure_case/chezmoi-template"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_INSTALLER_STDIN_CAPTURE
export TERRAPOD_INSTALLER_SCRIPT_CAPTURE
export TERRAPOD_CHEZMOI_STUB_TEMPLATE
gum_bootstrap_failure_input='minimal
'
run_installer_case "$gum_bootstrap_failure_case" "$gum_bootstrap_failure_input"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_INSTALLER_STDIN_CAPTURE
unset TERRAPOD_INSTALLER_SCRIPT_CAPTURE
unset TERRAPOD_CHEZMOI_STUB_TEMPLATE
assert_failure "$installer_status" "gum bootstrap failure makes installer exit unsuccessfully"
gum_bootstrap_failure_stderr="$(cat "$gum_bootstrap_failure_case/stderr")"
gum_bootstrap_failure_log_text="$(cat "$gum_bootstrap_failure_log")"
assert_contains "$gum_bootstrap_failure_log_text" "brew args:shellenv" "gum bootstrap failure evaluates Homebrew shellenv"
assert_contains "$gum_bootstrap_failure_log_text" "brew args:install gum" "gum bootstrap failure attempts to install gum"
assert_contains "$gum_bootstrap_failure_log_text" "brew install HOMEBREW_NO_AUTO_UPDATE:1" "gum bootstrap failure attempts gum install with Homebrew auto-update disabled"
assert_first_occurrence_before "$gum_bootstrap_failure_log_text" "chezmoi args:init https://github.com/juty9026/terrapod.git" "brew args:install gum" "gum bootstrap failure initializes source before setup UI dependency bootstrap"
assert_not_contains "$gum_bootstrap_failure_log_text" "terrapod args:setup" "gum bootstrap failure stops before Terrapod Setup"
assert_not_contains "$gum_bootstrap_failure_log_text" "chezmoi args:apply" "gum bootstrap failure stops before initial apply"
assert_contains "$gum_bootstrap_failure_stderr" "gum is required before Terrapod Setup can run" "gum bootstrap failure explains the setup UI dependency"
assert_contains "$gum_bootstrap_failure_stderr" "Prepare gum with Homebrew:" "gum bootstrap failure gives actionable Homebrew guidance"
gum_bootstrap_failure_prepare_command='eval "$("'"$gum_bootstrap_failure_case"'/bin/brew" shellenv)" && HOMEBREW_NO_AUTO_UPDATE=1 "'"$gum_bootstrap_failure_case"'/bin/brew" install gum'
assert_contains "$gum_bootstrap_failure_stderr" "$gum_bootstrap_failure_prepare_command" "gum bootstrap failure guidance includes known Homebrew shellenv and gum install command"
assert_not_contains "$gum_bootstrap_failure_stderr" "Rerun the installer" "gum bootstrap failure does not suggest rerunning the source-guarded installer"
gum_bootstrap_failure_resume_command='cd "'"$gum_bootstrap_failure_case"'/xdg-data/chezmoi" && eval "$("'"$gum_bootstrap_failure_case"'/bin/brew" shellenv)" && TERRAPOD_PROFILE="macos-terminal" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup && "'"$gum_bootstrap_failure_case"'/home/.local/bin/chezmoi" apply'
assert_contains "$gum_bootstrap_failure_stderr" "$gum_bootstrap_failure_resume_command" "gum bootstrap failure resume command keeps Homebrew shellenv active through setup and apply"

setup_failure_case="$(make_case_dir setup-failure)"
write_uname_stub "$setup_failure_case" "Darwin"
write_chezmoi_flow_stub "$setup_failure_case/chezmoi-template"
write_installer_download_stubs "$setup_failure_case"
write_command_call_stubs "$setup_failure_case" "wget" "git"
write_macos_brew_gum_stubs "$setup_failure_case" 0
setup_failure_log="$setup_failure_case/command-calls"
setup_failure_stdin_capture="$setup_failure_case/installer-stdin"
setup_failure_script_capture="$setup_failure_case/installer-script"
TERRAPOD_STUB_CALL_LOG="$setup_failure_log"
TERRAPOD_INSTALLER_STDIN_CAPTURE="$setup_failure_stdin_capture"
TERRAPOD_INSTALLER_SCRIPT_CAPTURE="$setup_failure_script_capture"
TERRAPOD_CHEZMOI_STUB_TEMPLATE="$setup_failure_case/chezmoi-template"
TERRAPOD_SETUP_STUB_STATUS=17
TERRAPOD_SETUP_STUB_MESSAGE="simulated Terrapod Setup failure"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_INSTALLER_STDIN_CAPTURE
export TERRAPOD_INSTALLER_SCRIPT_CAPTURE
export TERRAPOD_CHEZMOI_STUB_TEMPLATE
export TERRAPOD_SETUP_STUB_STATUS
export TERRAPOD_SETUP_STUB_MESSAGE
setup_failure_input='minimal
n
n
n
n
n
n
n
y
'
run_installer_case "$setup_failure_case" "$setup_failure_input"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_INSTALLER_STDIN_CAPTURE
unset TERRAPOD_INSTALLER_SCRIPT_CAPTURE
unset TERRAPOD_CHEZMOI_STUB_TEMPLATE
unset TERRAPOD_SETUP_STUB_STATUS
unset TERRAPOD_SETUP_STUB_MESSAGE
assert_failure "$installer_status" "setup failure makes installer exit unsuccessfully"
setup_failure_stdout="$(cat "$setup_failure_case/stdout")"
setup_failure_stderr="$(cat "$setup_failure_case/stderr")"
setup_failure_log_text="$(cat "$setup_failure_log")"
assert_contains "$setup_failure_log_text" "brew args:install gum" "setup failure case bootstraps gum with Homebrew before setup"
assert_first_occurrence_before "$setup_failure_log_text" "brew args:install gum" "terrapod args:setup" "setup failure case prepares gum before Terrapod Setup"
assert_contains "$setup_failure_log_text" "terrapod args:setup" "setup failure case runs checked-out Terrapod Setup"
assert_contains "$setup_failure_log_text" "terrapod setup stdin 1:minimal" "setup failure case forwards Preset input to Terrapod Setup"
assert_contains "$setup_failure_log_text" "terrapod setup stdin lines:9" "setup failure case forwards full minimal setup input to Terrapod Setup"
assert_first_occurrence_before "$setup_failure_log_text" "chezmoi args:init https://github.com/juty9026/terrapod.git" "terrapod args:setup" "setup failure case initializes source before setup"
assert_not_contains "$setup_failure_log_text" "chezmoi args:apply" "setup failure case does not run initial apply"
assert_not_contains "$setup_failure_stdout" "Terrapod first-run apply complete." "setup failure case does not print first-run completion"
assert_contains "$setup_failure_stderr" "simulated Terrapod Setup failure" "setup failure case preserves setup error output"
assert_contains "$setup_failure_stderr" "Terrapod Setup did not complete." "setup failure case explains setup did not complete"
assert_contains "$setup_failure_stderr" "Resume Terrapod Setup from the checked-out source repository:" "setup failure case prints recovery heading"
setup_failure_resume_command='cd "'"$setup_failure_case"'/xdg-data/chezmoi" && eval "$("'"$setup_failure_case"'/bin/brew" shellenv)" && TERRAPOD_PROFILE="macos-terminal" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup'
assert_contains "$setup_failure_stderr" "$setup_failure_resume_command" "setup failure case resume command keeps Homebrew shellenv active through setup"

setup_cancel_case="$(make_case_dir setup-cancel)"
write_uname_stub "$setup_cancel_case" "Darwin"
write_chezmoi_flow_stub "$setup_cancel_case/chezmoi-template"
write_installer_download_stubs "$setup_cancel_case"
write_command_call_stubs "$setup_cancel_case" "wget" "git"
write_macos_brew_gum_stubs "$setup_cancel_case" 0
setup_cancel_log="$setup_cancel_case/command-calls"
setup_cancel_stdin_capture="$setup_cancel_case/installer-stdin"
setup_cancel_script_capture="$setup_cancel_case/installer-script"
TERRAPOD_STUB_CALL_LOG="$setup_cancel_log"
TERRAPOD_INSTALLER_STDIN_CAPTURE="$setup_cancel_stdin_capture"
TERRAPOD_INSTALLER_SCRIPT_CAPTURE="$setup_cancel_script_capture"
TERRAPOD_CHEZMOI_STUB_TEMPLATE="$setup_cancel_case/chezmoi-template"
TERRAPOD_SETUP_STUB_STATUS=1
TERRAPOD_SETUP_STUB_MESSAGE="terrapod: setup cancelled"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_INSTALLER_STDIN_CAPTURE
export TERRAPOD_INSTALLER_SCRIPT_CAPTURE
export TERRAPOD_CHEZMOI_STUB_TEMPLATE
export TERRAPOD_SETUP_STUB_STATUS
export TERRAPOD_SETUP_STUB_MESSAGE
setup_cancel_input='development





n
'
run_installer_case "$setup_cancel_case" "$setup_cancel_input"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_INSTALLER_STDIN_CAPTURE
unset TERRAPOD_INSTALLER_SCRIPT_CAPTURE
unset TERRAPOD_CHEZMOI_STUB_TEMPLATE
unset TERRAPOD_SETUP_STUB_STATUS
unset TERRAPOD_SETUP_STUB_MESSAGE
assert_failure "$installer_status" "setup cancellation makes installer exit unsuccessfully"
setup_cancel_stdout="$(cat "$setup_cancel_case/stdout")"
setup_cancel_stderr="$(cat "$setup_cancel_case/stderr")"
setup_cancel_log_text="$(cat "$setup_cancel_log")"
assert_contains "$setup_cancel_log_text" "brew args:install gum" "setup cancellation case bootstraps gum with Homebrew before setup"
assert_first_occurrence_before "$setup_cancel_log_text" "brew args:install gum" "terrapod args:setup" "setup cancellation case prepares gum before Terrapod Setup"
assert_contains "$setup_cancel_log_text" "terrapod args:setup" "setup cancellation case runs checked-out Terrapod Setup"
assert_contains "$setup_cancel_log_text" "terrapod setup stdin 1:development" "setup cancellation case forwards Preset input to Terrapod Setup"
assert_contains "$setup_cancel_log_text" "terrapod setup stdin 7:n" "setup cancellation case forwards final cancellation input to Terrapod Setup"
assert_contains "$setup_cancel_log_text" "terrapod setup stdin lines:7" "setup cancellation case forwards full development setup input to Terrapod Setup"
assert_not_contains "$setup_cancel_log_text" "chezmoi args:apply" "setup cancellation case does not run initial apply"
assert_not_contains "$setup_cancel_stdout" "Terrapod first-run apply complete." "setup cancellation case does not print first-run completion"
assert_contains "$setup_cancel_stderr" "terrapod: setup cancelled" "setup cancellation case preserves setup cancellation output"
assert_contains "$setup_cancel_stderr" "Terrapod Setup did not complete." "setup cancellation case explains setup did not complete"
setup_cancel_resume_command='cd "'"$setup_cancel_case"'/xdg-data/chezmoi" && eval "$("'"$setup_cancel_case"'/bin/brew" shellenv)" && TERRAPOD_PROFILE="macos-terminal" TERRAPOD_CHEZMOI_CONFIG= ./dot_local/bin/executable_terrapod setup'
assert_contains "$setup_cancel_stderr" "$setup_cancel_resume_command" "setup cancellation case resume command keeps Homebrew shellenv active through setup"

system_chezmoi_case="$(make_case_dir system-chezmoi)"
write_uname_stub "$system_chezmoi_case" "Darwin"
write_command_call_stubs "$system_chezmoi_case" "chezmoi" "wget" "git"
write_gum_command_stub "$system_chezmoi_case"
write_chezmoi_flow_stub "$system_chezmoi_case/chezmoi-template"
write_installer_download_stubs "$system_chezmoi_case"
system_chezmoi_log="$system_chezmoi_case/command-calls"
system_chezmoi_stdin_capture="$system_chezmoi_case/installer-stdin"
system_chezmoi_script_capture="$system_chezmoi_case/installer-script"
TERRAPOD_STUB_CALL_LOG="$system_chezmoi_log"
TERRAPOD_INSTALLER_STDIN_CAPTURE="$system_chezmoi_stdin_capture"
TERRAPOD_INSTALLER_SCRIPT_CAPTURE="$system_chezmoi_script_capture"
TERRAPOD_CHEZMOI_STUB_TEMPLATE="$system_chezmoi_case/chezmoi-template"
export TERRAPOD_STUB_CALL_LOG
export TERRAPOD_INSTALLER_STDIN_CAPTURE
export TERRAPOD_INSTALLER_SCRIPT_CAPTURE
export TERRAPOD_CHEZMOI_STUB_TEMPLATE
system_chezmoi_input='minimal
'
run_installer_case "$system_chezmoi_case" "$system_chezmoi_input"
unset TERRAPOD_STUB_CALL_LOG
unset TERRAPOD_INSTALLER_STDIN_CAPTURE
unset TERRAPOD_INSTALLER_SCRIPT_CAPTURE
unset TERRAPOD_CHEZMOI_STUB_TEMPLATE
assert_status "$installer_status" 0 "installer targets user-local chezmoi even when PATH contains chezmoi"
system_chezmoi_log_text="$(cat "$system_chezmoi_log")"
assert_contains "$system_chezmoi_log_text" "curl args:-fsLS get.chezmoi.io" "system chezmoi case still delegates installation to get.chezmoi.io"
assert_all_chezmoi_paths_equal "$system_chezmoi_log_text" "$system_chezmoi_case/home/.local/bin/chezmoi" "system chezmoi case invokes only user-local chezmoi"
if printf '%s\n' "$system_chezmoi_log_text" | grep -Fx "chezmoi" >/dev/null; then
  fail "PATH chezmoi is not invoked"
fi
pass "PATH chezmoi is not invoked"

debian_case="$(make_case_dir debian-profile)"
write_uname_stub "$debian_case" "Linux"
debian_os_release="$(write_os_release "$debian_case" "ID=debian" 'VERSION_ID="12"')"
debian_log="$debian_case/command-calls"
write_command_call_stubs "$debian_case" "curl" "wget" "git" "chezmoi" "sh"
TERRAPOD_OS_RELEASE_FILE="$debian_os_release"
TERRAPOD_STUB_CALL_LOG="$debian_log"
export TERRAPOD_OS_RELEASE_FILE
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$debian_case"
unset TERRAPOD_OS_RELEASE_FILE
unset TERRAPOD_STUB_CALL_LOG
assert_failure "$installer_status" "Debian profile is rejected"
debian_stderr="$(cat "$debian_case/stderr")"
assert_contains "$debian_stderr" "Supported Linux release: Ubuntu 24.04 LTS" "Debian rejection explains supported Linux release"
assert_no_stub_calls "$debian_log" "Debian rejection runs before network or chezmoi commands"

source_guard_case="$(make_case_dir source-guard)"
write_uname_stub "$source_guard_case" "Darwin"
mkdir -p "$source_guard_case/xdg-data/chezmoi"
source_guard_log="$source_guard_case/command-calls"
write_command_call_stubs "$source_guard_case" "curl" "wget" "git" "chezmoi" "sh"
TERRAPOD_STUB_CALL_LOG="$source_guard_log"
export TERRAPOD_STUB_CALL_LOG
run_installer_case "$source_guard_case"
unset TERRAPOD_STUB_CALL_LOG
assert_failure "$installer_status" "existing source directory is rejected"
source_guard_stderr="$(cat "$source_guard_case/stderr")"
assert_contains "$source_guard_stderr" "chezmoi source directory already exists" "existing source directory rejection is explained"
assert_contains "$source_guard_stderr" "Move it aside" "existing source directory rejection gives recovery guidance"
assert_no_stub_calls "$source_guard_log" "existing source guard runs before network or chezmoi commands"
