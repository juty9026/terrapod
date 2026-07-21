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

write_gum_stub() {
  path="$1"

  cat >"$path" <<'SH'
#!/bin/sh
set -eu

log_file="${TERRAPOD_GUM_LOG:?}"
responses_file="${TERRAPOD_GUM_RESPONSES:?}"

printf '%s' "gum args:" >>"$log_file"
for arg do
  printf '%s' " $arg" >>"$log_file"
done
printf '\n' >>"$log_file"

if [ "${1:-}" = "--version" ]; then
  printf '%s\n' "gum test stub"
  exit 0
fi

next_response() {
  response="$(sed -n '1p' "$responses_file")"
  sed '1d' "$responses_file" >"$responses_file.tmp"
  mv "$responses_file.tmp" "$responses_file"

  if [ -z "$response" ]; then
    exit 130
  fi

  printf '%s\n' "$response"
}

case "${1:-}" in
  choose)
    if [ "${2:-}" = "--help" ]; then
      printf '%s\n' "Usage: gum choose [<options> ...] [flags]"
      printf '%s\n' "      --label-delimiter=\"\""
      exit 0
    fi

    while IFS= read -r option; do
      printf '%s\n' "gum stdin: $option" >>"$log_file"
    done

    response="$(next_response)"
    if [ "$response" = "__CANCEL__" ]; then
      exit 130
    fi
    if [ "$response" = "__ERROR__" ]; then
      printf '%s\n' "simulated gum operational failure" >&2
      exit 2
    fi
    printf '%s\n' "$response"
    ;;
  confirm)
    response="$(next_response)"
    case "$response" in
      yes|y|true|enabled)
        exit 0
        ;;
      no|n|false|disabled)
        exit 1
        ;;
      __CANCEL__)
        exit 130
        ;;
      __ERROR__)
        printf '%s\n' "simulated gum operational failure" >&2
        exit 2
        ;;
      *)
        printf '%s\n' "unexpected gum confirm response: $response" >&2
        exit 2
        ;;
    esac
    ;;
  style)
    shift
    for arg do
      case "$arg" in
        --*)
          ;;
        *)
          printf '%s\n' "$arg"
          ;;
      esac
    done
    ;;
  *)
    printf '%s\n' "unexpected gum command: ${1:-}" >&2
    exit 2
    ;;
esac
SH

  chmod +x "$path"
}

write_gum_responses() {
  responses_file="$1"
  shift

  : >"$responses_file"
  for response do
    printf '%s\n' "$response" >>"$responses_file"
  done
}

write_old_gum_stub() {
  path="$1"

  cat >"$path" <<'SH'
#!/bin/sh
set -eu

log_file="${TERRAPOD_GUM_LOG:?}"

printf '%s' "gum args:" >>"$log_file"
for arg do
  printf '%s' " $arg" >>"$log_file"
done
printf '\n' >>"$log_file"

if [ "${1:-}" = "--version" ]; then
  printf '%s\n' "gum version 0.14.0"
  exit 0
fi

if [ "${1:-}" = "choose" ] && [ "${2:-}" = "--help" ]; then
  printf '%s\n' "Usage: gum choose [<options> ...] [flags]"
  printf '%s\n' "      --header=\"Choose:\""
  exit 0
fi

case "${1:-}" in
  choose)
    printf '%s\n' "unknown flag: --label-delimiter" >&2
    exit 2
    ;;
  style)
    shift
    for arg do
      case "$arg" in
        --*)
          ;;
        *)
          printf '%s\n' "$arg"
          ;;
      esac
    done
    ;;
  *)
    printf '%s\n' "unexpected old gum command: ${1:-}" >&2
    exit 2
    ;;
esac
SH

  chmod +x "$path"
}

write_no_gum_path() {
  path="$1"
  shift

  mkdir -p "$path"

  for command_name do
    command_path="$(command -v "$command_name" 2>/dev/null || true)"
    if [ -z "$command_path" ]; then
      fail "no-gum PATH setup requires $command_name"
    fi

    ln -s "$command_path" "$path/$command_name"
  done

  if PATH="$path" command -v gum >/dev/null 2>&1; then
    fail "no-gum PATH setup should hide gum"
  fi
}

shell_quote() {
  printf "'"
  printf '%s' "$1" | sed "s/'/'\\\\''/g"
  printf "'"
}

run_setup_in_pty() {
  profile="$1"
  term="$2"
  home_dir="$3"
  xdg_config_home="$4"

  if script --version >/dev/null 2>&1; then
    command_text="env TERM=$(shell_quote "$term") TERRAPOD_PROFILE=$(shell_quote "$profile") TERRAPOD_CHEZMOI_CONFIG= HOME=$(shell_quote "$home_dir") XDG_CONFIG_HOME=$(shell_quote "$xdg_config_home") sh $(shell_quote "$terrapod") setup"
    script -q -e -c "$command_text" /dev/null
  else
    script -q /dev/null env TERM="$term" TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG= HOME="$home_dir" XDG_CONFIG_HOME="$xdg_config_home" sh "$terrapod" setup
  fi
}

run_command_in_pty() {
  term="$1"
  no_color_mode="$2"
  shift 2

  command_text="TERM=$(shell_quote "$term"); export TERM; TERRAPOD_PROFILE=macos-terminal; export TERRAPOD_PROFILE"
  case "$no_color_mode" in
    unset)
      command_text="$command_text; unset NO_COLOR"
      ;;
    empty)
      command_text="$command_text; NO_COLOR=; export NO_COLOR"
      ;;
    set)
      command_text="$command_text; NO_COLOR=1; export NO_COLOR"
      ;;
    *)
      fail "unknown NO_COLOR mode: $no_color_mode"
      ;;
  esac

  command_text="$command_text;"
  for arg do
    command_text="$command_text $(shell_quote "$arg")"
  done

  if script --version >/dev/null 2>&1; then
    script -q -e -c "$command_text" /dev/null
  else
    script -q /dev/null sh -c "$command_text"
  fi
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

assert_no_ansi_escape() {
  haystack="$1"
  message="$2"
  escape_char="$(printf '\033')"

  if printf '%s\n' "$haystack" | grep -F "$escape_char" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_has_ansi_escape() {
  haystack="$1"
  message="$2"
  escape_char="$(printf '\033')"

  if ! printf '%s\n' "$haystack" | grep -F "$escape_char" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains_ansi_before() {
  haystack="$1"
  text="$2"
  message="$3"
  escape_char="$(printf '\033')"

  if printf '%s\n' "$haystack" | grep -F "$escape_char$text" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains_ansi_after() {
  haystack="$1"
  text="$2"
  message="$3"
  escape_char="$(printf '\033')"

  if printf '%s\n' "$haystack" | grep -F "$text$escape_char" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains_colored_phrase() {
  haystack="$1"
  text="$2"
  message="$3"

  if printf '%s\n' "$haystack" | grep -F "m$text" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_no_rich_setup_emoji() {
  haystack="$1"
  message="$2"

  if printf '%s\n' "$haystack" | grep -E '🌱|✨|▸' >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_no_routine_emoji() {
  haystack="$1"
  message="$2"

  if printf '%s\n' "$haystack" | grep -E '🌱|✨|▸|✅|⚠|❌|🚀|🔴|🟢|🟡' >/dev/null; then
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
  earlier="$2"
  later="$3"
  message="$4"

  earlier_line="$(
    printf '%s\n' "$haystack" |
      awk -v needle="$earlier" 'index($0, needle) { print NR; exit }'
  )"
  later_line="$(
    printf '%s\n' "$haystack" |
      awk -v needle="$later" 'index($0, needle) { print NR; exit }'
  )"

  if [ -z "$earlier_line" ] || [ -z "$later_line" ] || [ "$earlier_line" -ge "$later_line" ]; then
    fail "$message"
  fi

  pass "$message"
}

run_terrapod_setup_command() {
  profile="$1"
  responses_file="$2"
  home_dir="$3"
  xdg_config_home="$4"
  output_file="$5"
  gum_log="$6"

  TERRAPOD_GUM_RESPONSES="$responses_file" TERRAPOD_GUM_LOG="$gum_log" \
    PATH="$tmp_dir/bin:/usr/bin:/bin" \
    run_setup_in_pty "$profile" xterm "$home_dir" "$xdg_config_home" >"$output_file" 2>&1
}

assert_call_args() {
  call_file="$1"
  message="$2"
  shift 2

  expected_file="$tmp_dir/expected.args"
  : >"$expected_file"

  for arg do
    printf '%s\n' "$arg" >>"$expected_file"
  done

  if ! cmp -s "$expected_file" "$call_file"; then
    printf '%s\n' "expected args:" >&2
    sed 's/^/  /' "$expected_file" >&2
    printf '%s\n' "actual args:" >&2
    sed 's/^/  /' "$call_file" >&2
    fail "$message"
  fi

  pass "$message"
}

write_success_stub() {
  name="$1"
  write_stub "$tmp_dir/bin/$name" \
    'exit 0'
}

write_failing_command_stub() {
  path="$1"
  call_file="$2"
  write_stub "$path" \
    'printf "%s\n" "$0 $*" >>"'"$call_file"'"' \
    'exit 90'
}

write_brew_bundle_stub() {
  path="$1"

  write_stub "$path" \
    'printf "%s\n" "brew args:$*" >>"$MACOS_BREW_LOG"' \
    'bundle_file=' \
    'for arg do' \
    '  case "$arg" in' \
    '    --file=*) bundle_file="${arg#--file=}" ;;' \
    '  esac' \
    'done' \
    'case "$1" in' \
    '  --prefix) printf "%s\n" "${MACOS_BREW_PREFIX:-/opt/homebrew}"; exit 0 ;;' \
    '  shellenv) printf "%s\n" ":" ;;' \
    '  analytics) exit 0 ;;' \
    '  bundle)' \
    '    if [ "${MACOS_BREW_ECHO_OUTPUT:-}" = "1" ]; then' \
    '      printf "%s\n" "visible brew bundle output: $*"' \
    '    fi' \
    '    for formula in ${MACOS_BREW_FAIL_FORMULAE:-}; do' \
    '      if [ -n "$bundle_file" ] && grep -Fx "brew \"$formula\"" "$bundle_file" >/dev/null 2>&1; then' \
    '        exit 42' \
    '      fi' \
    '    done' \
    '    if [ "${MACOS_BREW_FAIL_CORE_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "brew \"mise\"" "$bundle_file" >/dev/null 2>&1 && grep -Fx "brew \"btop\"" "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    for cask in ${MACOS_BREW_FAIL_CASKS:-}; do' \
    '      if [ -n "$bundle_file" ] && grep -Fx "cask \"$cask\"" "$bundle_file" >/dev/null 2>&1; then' \
    '        exit 42' \
    '      fi' \
    '    done' \
    '    if [ "${MACOS_BREW_FAIL_DESKTOP_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "# Rendered opt-in macOS Desktop App Stack." "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    if [ "${MACOS_BREW_FAIL_BULK:-}" = "1" ] && [ -n "$bundle_file" ] && grep -Fx "tap \"homebrew/cask\"" "$bundle_file" >/dev/null 2>&1; then' \
    '      exit 42' \
    '    fi' \
    '    exit 0' \
    '    ;;' \
    '  *) exit 64 ;;' \
    'esac'
}

write_os_release() {
  path="$1"
  id="$2"
  version_id="$3"
  pretty_name="$4"

  cat >"$path" <<EOF
ID=$id
VERSION_ID="$version_id"
PRETTY_NAME="$pretty_name"
EOF
}

copy_desktop_apply_source_fixture() {
  source_dir="$1"

  # Keep this fixture minimal so real chezmoi apply only runs the Homebrew path under test.
  mkdir -p \
    "$source_dir/.chezmoiscripts" \
    "$source_dir/dot_local/bin" \
    "$source_dir/dot_local/lib/terrapod"

  cp "$repo_root/Brewfile" "$source_dir/Brewfile"
  cp "$repo_root/Brewfile.macos-desktop-apps.tmpl" "$source_dir/Brewfile.macos-desktop-apps.tmpl"
  cp "$repo_root/.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl" "$source_dir/.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl"
  cp "$repo_root/.chezmoiscripts/run_before_01-retry-homebrew-desktop-apps.sh.tmpl" "$source_dir/.chezmoiscripts/run_before_01-retry-homebrew-desktop-apps.sh.tmpl"
  cp "$repo_root/.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl" "$source_dir/.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl"
  cp "$terrapod" "$source_dir/dot_local/bin/executable_terrapod"
  cp "$tpod_source" "$source_dir/dot_local/bin/symlink_tpod"
  cp "$repo_root/dot_local/lib/terrapod/homebrew-core-bundle.sh" "$source_dir/dot_local/lib/terrapod/homebrew-core-bundle.sh"
  cp "$install_warnings_lib" "$source_dir/dot_local/lib/terrapod/install-warnings.sh"
}

write_desktop_apply_config() {
  config_file="$1"
  source_dir="$2"
  dest_dir="$3"
  terminal_apps="$4"
  launcher="$5"

  cat >"$config_file" <<EOF
sourceDir = "$source_dir"
destDir = "$dest_dir"

[data]
profile = "macos-terminal"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = $terminal_apps
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = $launcher
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
EOF
}

status_doctor_path() {
  name="$1"
  shift

  isolated_path="$tmp_dir/status-doctor-path-$name"
  rm -rf "$isolated_path"
  mkdir -p "$isolated_path"

  awk_path="$(command -v awk 2>/dev/null || true)"
  if [ -z "$awk_path" ]; then
    fail "status/doctor tests require awk"
  fi
  ln -s "$awk_path" "$isolated_path/awk"

  for command_name do
    if [ "$command_name" = brew ]; then
      write_stub "$isolated_path/$command_name" \
        'if [ "${1:-}" = "--prefix" ]; then printf "%s\n" "${0%/*}"; fi' \
        'exit 0'
    else
      write_stub "$isolated_path/$command_name" \
        'exit 0'
    fi
  done

  printf '%s\n' "$isolated_path"
}

assert_no_terrapod_artifacts_under() {
  dir="$1"
  message="$2"
  found_file="$tmp_dir/found-terrapod-artifacts"

  : >"$found_file"
  if [ -d "$dir" ]; then
    find "$dir" \
      \( -name '.terrapod-config.*' \
      -o -name '.terrapod-data.*' \
      -o -name '*.terrapod-backup-*' \
      -o -name '*.terrapod-tmp-*' \
      -o -name '*.terrapod-data-*' \) \
      -print >"$found_file"
  fi

  if [ -s "$found_file" ]; then
    printf '%s\n' "unexpected Terrapod artifacts:" >&2
    sed 's/^/  /' "$found_file" >&2
    fail "$message"
  fi

  pass "$message"
}

mkdir -p "$tmp_dir/bin" "$tmp_dir/home"
system_path="$PATH"
write_gum_stub "$tmp_dir/bin/gum"
no_gum_path="$tmp_dir/no-gum-bin"
write_no_gum_path "$no_gum_path" sh

terrapod="$repo_root/dot_local/bin/executable_terrapod"
tpod_source="$repo_root/dot_local/bin/symlink_tpod"
install_warnings_lib="$repo_root/dot_local/lib/terrapod/install-warnings.sh"

managed_targets="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    managed
)"

assert_line \
  "$managed_targets" \
  ".local/bin/terrapod" \
  "chezmoi manages Terrapod as the primary user-facing command"

assert_line \
  "$managed_targets" \
  ".local/bin/tpod" \
  "chezmoi manages tpod as an alias to Terrapod"

assert_line \
  "$managed_targets" \
  ".local/lib/terrapod/install-warnings.sh" \
  "chezmoi manages the shared install warning marker library"

terrapod_target="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    target-path dot_local/bin/executable_terrapod
)"
expected_terrapod_target="$tmp_dir/home/.local/bin/terrapod"

if [ "$terrapod_target" != "$expected_terrapod_target" ]; then
  fail "chezmoi installs Terrapod at ~/.local/bin/terrapod; expected '$expected_terrapod_target', got '$terrapod_target'"
fi

pass "chezmoi installs Terrapod at ~/.local/bin/terrapod"

tpod_target="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    target-path dot_local/bin/symlink_tpod
)"
expected_tpod_target="$tmp_dir/home/.local/bin/tpod"

if [ "$tpod_target" != "$expected_tpod_target" ]; then
  fail "chezmoi installs tpod at ~/.local/bin/tpod; expected '$expected_tpod_target', got '$tpod_target'"
fi

pass "chezmoi installs tpod at ~/.local/bin/tpod"

if [ ! -f "$terrapod" ]; then
  fail "Terrapod command source exists"
fi

pass "Terrapod command source exists"

if [ ! -x "$terrapod" ]; then
  fail "Terrapod command source is executable"
fi

pass "Terrapod command source is executable"

if [ ! -f "$tpod_source" ]; then
  fail "tpod alias source exists"
fi

pass "tpod alias source exists"

IFS= read -r tpod_source_line <"$tpod_source"

if [ "$tpod_source_line" != "terrapod" ]; then
  fail "tpod alias points to Terrapod"
fi

pass "tpod alias points to Terrapod"

if [ ! -f "$install_warnings_lib" ]; then
  fail "shared install warning marker library source exists"
fi

pass "shared install warning marker library source exists"

sh -n "$install_warnings_lib" || fail "shared install warning marker library is valid POSIX shell"
pass "shared install warning marker library is valid POSIX shell"

sh -n "$terrapod" || fail "Terrapod command is valid POSIX shell"
pass "Terrapod command is valid POSIX shell"

marker_home="$tmp_dir/marker-home"
marker_xdg_state="$tmp_dir/marker-xdg-state"
marker_dotfiles_probe="$tmp_dir/marker-dotfiles-probe"
mkdir -p "$marker_home" "$marker_dotfiles_probe"

default_marker_dir="$(
  HOME="$marker_home" XDG_STATE_HOME= sh -c '. "$1"; terrapod_install_warning_dir' sh "$install_warnings_lib"
)"
expected_default_marker_dir="$marker_home/.local/state/terrapod/install-warnings"

if [ "$default_marker_dir" != "$expected_default_marker_dir" ]; then
  fail "install warning markers default to HOME-local XDG state; expected '$expected_default_marker_dir', got '$default_marker_dir'"
fi
pass "install warning markers default to HOME-local XDG state"

xdg_marker_dir="$(
  HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c '. "$1"; terrapod_install_warning_dir' sh "$install_warnings_lib"
)"
expected_xdg_marker_dir="$marker_xdg_state/terrapod/install-warnings"

if [ "$xdg_marker_dir" != "$expected_xdg_marker_dir" ]; then
  fail "install warning markers honor XDG_STATE_HOME; expected '$expected_xdg_marker_dir', got '$xdg_marker_dir'"
fi
pass "install warning markers honor XDG_STATE_HOME"

marker_categories="$(
  sh -c '. "$1"; terrapod_install_warning_categories' sh "$install_warnings_lib"
)"
expected_marker_categories="$(printf '%s\n' homebrew-core homebrew-desktop-apps ubuntu-bootstrap shell-integrations mise-tools optional-ai-cli-tools)"

if [ "$marker_categories" != "$expected_marker_categories" ]; then
  printf '%s\n' "expected marker categories:" >&2
  printf '%s\n' "$expected_marker_categories" | sed 's/^/  /' >&2
  printf '%s\n' "actual marker categories:" >&2
  printf '%s\n' "$marker_categories" | sed 's/^/  /' >&2
  fail "install warning marker categories are explicit stable slugs"
fi
pass "install warning marker categories are explicit stable slugs"

HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "Run tpod apply after fixing the Homebrew bundle error."' \
  sh "$install_warnings_lib"

homebrew_core_marker="$marker_xdg_state/terrapod/install-warnings/homebrew-core"
if [ ! -f "$homebrew_core_marker" ]; then
  fail "install warning marker write creates the category file under XDG state"
fi
pass "install warning marker write creates the category file under XDG state"

assert_not_contains \
  "$(find "$repo_root" -path '*/install-warnings/*' -print)" \
  "$repo_root" \
  "install warning markers are not written under the Terrapod source repository"

assert_not_contains \
  "$(find "$marker_dotfiles_probe" -path '*/install-warnings/*' -print)" \
  "$marker_dotfiles_probe" \
  "install warning markers are not written under managed dotfiles"

homebrew_core_marker_text="$(cat "$homebrew_core_marker")"
assert_line "$homebrew_core_marker_text" "category='homebrew-core'" "install warning marker schema stores category as shell-friendly key/value data"
assert_line "$homebrew_core_marker_text" "summary='Homebrew core install needs attention'" "install warning marker schema stores summary as shell-friendly key/value data"
assert_line "$homebrew_core_marker_text" "guidance='Run tpod apply after fixing the Homebrew bundle error.'" "install warning marker schema stores guidance as shell-friendly key/value data"

updated_at_line="$(
  printf '%s\n' "$homebrew_core_marker_text" |
    awk -F= '$1 == "updated_at" { print $2 }'
)"
if ! printf '%s\n' "$updated_at_line" | grep -E "^'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z'$" >/dev/null; then
  fail "install warning marker updated_at is a UTC ISO 8601 timestamp ending in Z"
fi
pass "install warning marker updated_at is a UTC ISO 8601 timestamp ending in Z"

HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c \
  '. "$1"; terrapod_install_warning_write optional-ai-cli-tools "AI CLI tool install needs attention" "Rerun tpod apply after network access is restored."' \
  sh "$install_warnings_lib"

marker_list="$(
  HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c '. "$1"; terrapod_install_warning_list' sh "$install_warnings_lib"
)"
expected_marker_list="$(printf '%s\n' homebrew-core optional-ai-cli-tools)"
if [ "$marker_list" != "$expected_marker_list" ]; then
  printf '%s\n' "expected marker list:" >&2
  printf '%s\n' "$expected_marker_list" | sed 's/^/  /' >&2
  printf '%s\n' "actual marker list:" >&2
  printf '%s\n' "$marker_list" | sed 's/^/  /' >&2
  fail "install warning marker list returns existing category markers in stable order"
fi
pass "install warning marker list returns existing category markers in stable order"

read_summary="$(
  HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c '. "$1"; terrapod_install_warning_value homebrew-core summary' sh "$install_warnings_lib"
)"
if [ "$read_summary" != "Homebrew core install needs attention" ]; then
  fail "install warning marker value reader parses expected fields without sourcing marker files"
fi
pass "install warning marker value reader parses expected fields without sourcing marker files"

printf '%s\n' "category='homebrew-core'" "summary='stale'" "guidance='stale'" "updated_at='2000-01-01T00:00:00Z'" "extra='stale'" >"$homebrew_core_marker"
HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install recovered with a later warning" "Inspect the latest Homebrew output."' \
  sh "$install_warnings_lib"
homebrew_core_marker_text="$(cat "$homebrew_core_marker")"
assert_contains "$homebrew_core_marker_text" "summary='Homebrew core install recovered with a later warning'" "install warning marker write atomically replaces the category file content"
assert_not_contains "$homebrew_core_marker_text" "extra='stale'" "install warning marker write does not leave stale content from previous marker files"
if find "$marker_xdg_state/terrapod/install-warnings" -name '.homebrew-core.*' -print | grep . >/dev/null; then
  fail "install warning marker write leaves no temporary category files behind"
fi
pass "install warning marker write leaves no temporary category files behind"

HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c '. "$1"; terrapod_install_warning_clear homebrew-core' sh "$install_warnings_lib"
if [ -e "$homebrew_core_marker" ]; then
  fail "install warning marker clear removes the matching category file"
fi
pass "install warning marker clear removes the matching category file"

if [ ! -f "$marker_xdg_state/terrapod/install-warnings/optional-ai-cli-tools" ]; then
  fail "install warning marker clear does not remove other category files"
fi
pass "install warning marker clear does not remove other category files"

if HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c '. "$1"; terrapod_install_warning_write unknown-category "bad" "bad"' sh "$install_warnings_lib" 2>/dev/null; then
  fail "install warning marker write rejects unknown categories"
fi
pass "install warning marker write rejects unknown categories"

if HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" sh -c '. "$1"; terrapod_install_warning_write ai-cli-tools "bad" "bad"' sh "$install_warnings_lib" 2>/dev/null; then
  fail "install warning marker write rejects the legacy AI CLI category slug"
fi
pass "install warning marker write rejects the legacy AI CLI category slug"

legacy_marker_home="$tmp_dir/legacy-marker-home"
legacy_marker_state="$tmp_dir/legacy-marker-state"
legacy_marker_dir="$legacy_marker_state/terrapod/install-warnings"
legacy_ai_cli_marker="$legacy_marker_dir/ai-cli-tools"
mkdir -p "$legacy_marker_dir" "$legacy_marker_home"
printf '%s\n' \
  "category='ai-cli-tools'" \
  "summary='Legacy AI CLI tool install needs attention'" \
  "guidance='Rerun tpod apply after network access is restored.'" \
  "updated_at='2026-01-01T00:00:00Z'" \
  >"$legacy_ai_cli_marker"

legacy_marker_list="$(
  HOME="$legacy_marker_home" XDG_STATE_HOME="$legacy_marker_state" sh -c '. "$1"; terrapod_install_warning_list' sh "$install_warnings_lib"
)"
if [ "$legacy_marker_list" != "optional-ai-cli-tools" ]; then
  fail "install warning marker list exposes legacy AI CLI markers through the stable category slug"
fi
pass "install warning marker list exposes legacy AI CLI markers through the stable category slug"

legacy_marker_read="$(
  HOME="$legacy_marker_home" XDG_STATE_HOME="$legacy_marker_state" sh -c '. "$1"; terrapod_install_warning_read optional-ai-cli-tools' sh "$install_warnings_lib"
)"
assert_contains "$legacy_marker_read" "category='optional-ai-cli-tools'" "install warning marker read normalizes legacy AI CLI marker categories"
assert_not_contains "$legacy_marker_read" "category='ai-cli-tools'" "install warning marker read does not expose legacy AI CLI marker categories"

legacy_marker_category="$(
  HOME="$legacy_marker_home" XDG_STATE_HOME="$legacy_marker_state" sh -c '. "$1"; terrapod_install_warning_value optional-ai-cli-tools category' sh "$install_warnings_lib"
)"
if [ "$legacy_marker_category" != "optional-ai-cli-tools" ]; then
  fail "install warning marker category value normalizes legacy AI CLI marker files"
fi
pass "install warning marker category value normalizes legacy AI CLI marker files"

legacy_marker_summary="$(
  HOME="$legacy_marker_home" XDG_STATE_HOME="$legacy_marker_state" sh -c '. "$1"; terrapod_install_warning_value optional-ai-cli-tools summary' sh "$install_warnings_lib"
)"
if [ "$legacy_marker_summary" != "Legacy AI CLI tool install needs attention" ]; then
  fail "install warning marker value falls back to legacy AI CLI marker files"
fi
pass "install warning marker value falls back to legacy AI CLI marker files"

HOME="$legacy_marker_home" XDG_STATE_HOME="$legacy_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write optional-ai-cli-tools "Current AI CLI tool install needs attention" "Rerun tpod apply after network access is restored."' \
  sh "$install_warnings_lib"
if [ ! -f "$legacy_marker_dir/optional-ai-cli-tools" ] || [ -e "$legacy_ai_cli_marker" ]; then
  fail "install warning marker write replaces legacy AI CLI marker files with the stable marker"
fi
pass "install warning marker write replaces legacy AI CLI marker files with the stable marker"
current_marker_summary="$(
  HOME="$legacy_marker_home" XDG_STATE_HOME="$legacy_marker_state" sh -c '. "$1"; terrapod_install_warning_value optional-ai-cli-tools summary' sh "$install_warnings_lib"
)"
if [ "$current_marker_summary" != "Current AI CLI tool install needs attention" ]; then
  fail "install warning marker value prefers stable AI CLI marker files over legacy marker files"
fi
pass "install warning marker value prefers stable AI CLI marker files over legacy marker files"

HOME="$legacy_marker_home" XDG_STATE_HOME="$legacy_marker_state" sh -c '. "$1"; terrapod_install_warning_clear optional-ai-cli-tools' sh "$install_warnings_lib"
if [ -e "$legacy_marker_dir/optional-ai-cli-tools" ] || [ -e "$legacy_ai_cli_marker" ]; then
  fail "install warning marker clear removes stable and legacy AI CLI marker files"
fi
pass "install warning marker clear removes stable and legacy AI CLI marker files"

HOME="$legacy_marker_home" XDG_STATE_HOME="$legacy_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write optional-ai-cli-tools "AI CLI tool install needs attention" "Rerun tpod apply after network access is restored."' \
  sh "$install_warnings_lib"
if [ ! -f "$legacy_marker_dir/optional-ai-cli-tools" ] || [ -e "$legacy_ai_cli_marker" ]; then
  fail "install warning marker write stores Optional AI CLI warnings only under the stable category slug"
fi
pass "install warning marker write stores Optional AI CLI warnings only under the stable category slug"

fake_warning_bin="$tmp_dir/fake-warning-bin"
fake_warning_calls="$tmp_dir/fake-warning.calls"
mkdir -p "$fake_warning_bin"
write_stub "$fake_warning_bin/terrapod_install_warning_list" \
  'printf "%s\n" "list" >>"$FAKE_INSTALL_WARNING_CALLS"' \
  'printf "%s\n" homebrew-core'
write_stub "$fake_warning_bin/terrapod_install_warning_value" \
  'printf "%s\n" "value $*" >>"$FAKE_INSTALL_WARNING_CALLS"' \
  'printf "%s\n" spoofed'
write_stub "$fake_warning_bin/terrapod_install_warning_write" \
  'printf "%s\n" "write $*" >>"$FAKE_INSTALL_WARNING_CALLS"'

fake_ai_cli_home="$tmp_dir/fake-ai-cli-home"
mkdir -p "$fake_ai_cli_home/.local/bin"
write_stub "$fake_warning_bin/brew" \
  'case "$1" in' \
  '  shellenv) printf "%s\n" ":" ;;' \
  '  bundle) exit 0 ;;' \
  '  *) exit 64 ;;' \
  'esac'

fake_ai_cli_installer="$tmp_dir/fake-ai-cli-installer.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux","sourceDir":"/missing-terrapod-source"},"enableAiCliTools":true}' \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl" \
  >"$fake_ai_cli_installer"

if ! HOME="$fake_ai_cli_home" FAKE_INSTALL_WARNING_CALLS="$fake_warning_calls" PATH="$fake_warning_bin:/usr/bin:/bin" /bin/sh "$fake_ai_cli_installer" >"$tmp_dir/fake-ai-cli-installer.out" 2>"$tmp_dir/fake-ai-cli-installer.err"; then
  fail "rendered installer fixture succeeds when the Homebrew AI CLI bundle succeeds and the shared library is missing"
fi

if [ -e "$fake_warning_calls" ]; then
  fail "installer scripts ignore PATH fake install warning helpers when the shared library is not loaded"
fi
pass "installer scripts ignore PATH fake install warning helpers when the shared library is not loaded"

fake_ai_cli_failure_home="$tmp_dir/fake-ai-cli-failure-home"
mkdir -p "$fake_ai_cli_failure_home/.local/bin"
write_stub "$fake_ai_cli_failure_home/.local/bin/brew" \
  'case "$1" in' \
  '  shellenv) printf "%s\n" ":" ;;' \
  '  bundle) exit 42 ;;' \
  '  *) exit 64 ;;' \
  'esac'

fake_ai_cli_failure_installer="$tmp_dir/fake-ai-cli-failure-installer.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux","sourceDir":"/missing-terrapod-source"},"enableAiCliTools":true}' \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl" \
  >"$fake_ai_cli_failure_installer"

fake_ai_cli_failure_status=0
HOME="$fake_ai_cli_failure_home" PATH="$fake_ai_cli_failure_home/.local/bin:/usr/bin:/bin" /bin/sh "$fake_ai_cli_failure_installer" >"$tmp_dir/fake-ai-cli-failure.out" 2>"$tmp_dir/fake-ai-cli-failure.err" || fake_ai_cli_failure_status=$?
if [ "$fake_ai_cli_failure_status" -eq 0 ]; then
  fail "rendered installer fixture fails when optional AI CLI failures cannot be recorded without the shared library"
fi
pass "rendered installer fixture fails when optional AI CLI failures cannot be recorded without the shared library"

fake_ai_cli_warning_source="$tmp_dir/fake-ai-cli-warning-source"
fake_ai_cli_write_failure_home="$tmp_dir/fake-ai-cli-write-failure-home"
mkdir -p "$fake_ai_cli_warning_source/dot_local/lib/terrapod" "$fake_ai_cli_write_failure_home/.local/bin"
printf '%s\n' \
  'TERRAPOD_INSTALL_WARNINGS_LOADED=1' \
  'terrapod_install_warning_write() {' \
  '  printf "%s\n" "write failed:$*" >&2' \
  '  return 1' \
  '}' \
  'terrapod_install_warning_clear() {' \
  '  return 0' \
  '}' \
  >"$fake_ai_cli_warning_source/dot_local/lib/terrapod/install-warnings.sh"
write_stub "$fake_ai_cli_write_failure_home/.local/bin/brew" \
  'case "$1" in' \
  '  shellenv) printf "%s\n" ":" ;;' \
  '  bundle) exit 42 ;;' \
  '  *) exit 64 ;;' \
  'esac'

fake_ai_cli_write_failure_installer="$tmp_dir/fake-ai-cli-write-failure-installer.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data "{\"chezmoi\":{\"os\":\"linux\",\"sourceDir\":\"$fake_ai_cli_warning_source\"},\"enableAiCliTools\":true}" \
  --file "$repo_root/.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl" \
  >"$fake_ai_cli_write_failure_installer"

fake_ai_cli_write_failure_status=0
HOME="$fake_ai_cli_write_failure_home" PATH="$fake_ai_cli_write_failure_home/.local/bin:/usr/bin:/bin" /bin/sh "$fake_ai_cli_write_failure_installer" >"$tmp_dir/fake-ai-cli-write-failure.out" 2>"$tmp_dir/fake-ai-cli-write-failure.err" || fake_ai_cli_write_failure_status=$?
if [ "$fake_ai_cli_write_failure_status" -eq 0 ]; then
  fail "rendered installer fixture fails when optional AI CLI failures cannot be recorded after marker write failure"
fi
pass "rendered installer fixture fails when optional AI CLI failures cannot be recorded after marker write failure"

help_output="$(TERRAPOD_PROFILE=macos-terminal sh "$terrapod" help)"
help_with_marker_output="$(
  HOME="$marker_home" XDG_STATE_HOME="$marker_xdg_state" TERRAPOD_PROFILE=macos-terminal sh "$terrapod" help
)"
assert_not_contains "$help_with_marker_output" "Install warnings" "Terrapod help does not read or summarize install warning markers"
assert_not_contains "$help_with_marker_output" "Warning:" "Terrapod help remains warning-free when install warning markers exist"

ln -s "$terrapod" "$tmp_dir/bin/tpod"
tpod_help_output="$(TERRAPOD_PROFILE=macos-terminal PATH="$tmp_dir/bin:/usr/bin:/bin" "$tmp_dir/bin/tpod" help)"

if [ "$tpod_help_output" != "$help_output" ]; then
  fail "tpod shows the same help as Terrapod"
fi

pass "tpod shows the same help as Terrapod"

assert_contains \
  "$help_output" \
  "Terrapod - a small landing pod for your dotfiles" \
  "Terrapod help introduces the landing pod product promise"

assert_contains \
  "$help_output" \
  "Uses chezmoi underneath; keeps package-manager upgrades outside its scope." \
  "Terrapod help states chezmoi and package-manager boundaries"

assert_contains \
  "$help_output" \
  "Usage:" \
  "Terrapod help shows usage"

assert_contains \
  "$help_output" \
  "Commands:" \
  "Terrapod help shows command list"

assert_contains \
  "$help_output" \
  "tpod [help|--help|-h]" \
  "Terrapod help documents short help command"

assert_contains \
  "$help_output" \
  "tpod setup" \
  "Terrapod help documents short setup command"

assert_contains \
  "$help_output" \
  "tpod configure <minimal|development|workstation>" \
  "Terrapod help documents short Preset configuration"

assert_contains \
  "$help_output" \
  "tpod status" \
  "Terrapod help documents short status command"

assert_contains \
  "$help_output" \
  "tpod doctor" \
  "Terrapod help documents short doctor command"

assert_contains \
  "$help_output" \
  "tpod diff" \
  "Terrapod help documents short diff command"

assert_contains \
  "$help_output" \
  "tpod apply" \
  "Terrapod help documents short apply command"

assert_contains \
  "$help_output" \
  "tpod update" \
  "Terrapod help documents short update command"

assert_contains \
  "$help_output" \
  "tpod chezmoi -- <args...>" \
  "Terrapod help documents the raw chezmoi escape hatch"

assert_contains \
  "$help_output" \
  "terrapod also works as the full command." \
  "Terrapod help documents the full command note"

assert_not_contains \
  "$help_output" \
  "terrapod setup" \
  "Terrapod help does not use old setup copyable command"

assert_not_contains \
  "$help_output" \
  "terrapod configure <minimal|development|workstation>" \
  "Terrapod help does not use old configure copyable command"

assert_contains \
  "$help_output" \
  "tpod apply" \
  "Terrapod help examples lead with the short apply command"

assert_not_contains \
  "$help_output" \
  "terrapod status" \
  "Terrapod help does not use old status copyable command"

assert_not_contains \
  "$help_output" \
  "terrapod doctor" \
  "Terrapod help does not use old doctor copyable command"

assert_not_contains \
  "$help_output" \
  "terrapod diff" \
  "Terrapod help does not use old diff copyable command"

assert_not_contains \
  "$help_output" \
  "terrapod apply" \
  "Terrapod help does not use old apply copyable command"

assert_not_contains \
  "$help_output" \
  "terrapod update" \
  "Terrapod help does not use old update copyable command"

assert_not_contains \
  "$help_output" \
  "terrapod chezmoi -- <args...>" \
  "Terrapod help does not use old raw chezmoi copyable command"

assert_contains \
  "$help_output" \
  "setup" \
  "Terrapod help describes setup"

assert_contains \
  "$help_output" \
  "status" \
  "Terrapod help documents status"

assert_contains \
  "$help_output" \
  "doctor" \
  "Terrapod help documents doctor"

assert_no_ansi_escape "$help_output" "captured Terrapod help is plain without ANSI escapes"
assert_no_routine_emoji "$help_output" "captured Terrapod help has no routine emoji"

tty_help_output="$(run_command_in_pty xterm unset sh "$terrapod" help)"
assert_has_ansi_escape "$tty_help_output" "TTY Terrapod help uses ANSI visual treatment when supported"

tty_status_output="$(run_command_in_pty xterm unset sh "$terrapod" status)"
assert_has_ansi_escape "$tty_status_output" "TTY Terrapod status uses ANSI visual treatment when supported"
assert_not_contains_ansi_before "$tty_status_output" "macOS Terminal Profile" "TTY Terrapod status keeps profile value neutral"
assert_not_contains_ansi_after "$tty_status_output" "macOS Terminal Profile" "TTY Terrapod status does not color through profile value"
assert_not_contains_colored_phrase "$tty_status_output" "Profile: macOS Terminal Profile" "TTY Terrapod status does not color profile label and value as one phrase"

no_color_tty_help_output="$(run_command_in_pty xterm set sh "$terrapod" help)"
assert_no_ansi_escape "$no_color_tty_help_output" "TTY Terrapod help is plain when NO_COLOR is set"

empty_no_color_tty_help_output="$(run_command_in_pty xterm empty sh "$terrapod" help)"
assert_no_ansi_escape "$empty_no_color_tty_help_output" "TTY Terrapod help is plain when NO_COLOR is set to an empty value"

dumb_help_output="$(TERM=dumb TERRAPOD_PROFILE=macos-terminal sh "$terrapod" help)"
assert_no_ansi_escape "$dumb_help_output" "TERM=dumb Terrapod help is plain without ANSI escapes"

macos_help_output="$(TERRAPOD_PROFILE=macos-terminal sh "$terrapod" help)"

assert_contains \
  "$macos_help_output" \
  "tpod configure <minimal|development|workstation>" \
  "macOS Terminal Profile help exposes workstation Preset"

vps_help_output="$(TERRAPOD_PROFILE=vps-shell sh "$terrapod" help)"

assert_contains \
  "$vps_help_output" \
  "tpod configure <minimal|development>" \
  "VPS Shell Profile help hides workstation Preset"

assert_not_contains \
  "$vps_help_output" \
  "workstation" \
  "VPS Shell Profile help does not mention workstation anywhere"

if PATH="/usr/bin:/bin" TERRAPOD_PROFILE=vps-shell TERRAPOD_CHEZMOI_CONFIG= HOME="$tmp_dir/home" XDG_CONFIG_HOME="$tmp_dir/xdg" sh "$terrapod" configure workstation >"$tmp_dir/vps-workstation.out" 2>"$tmp_dir/vps-workstation.err"; then
  fail "VPS Shell Profile rejects workstation Preset"
fi
pass "VPS Shell Profile rejects workstation Preset"

if [ -e "$tmp_dir/xdg/chezmoi/chezmoi.toml" ]; then
  fail "VPS Shell Profile rejects workstation Preset before writing config"
fi
pass "VPS Shell Profile rejects workstation Preset before writing config"

assert_no_terrapod_artifacts_under \
  "$tmp_dir/xdg/chezmoi" \
  "VPS Shell Profile workstation rejection leaves no Terrapod artifacts"

vps_workstation_error="$(cat "$tmp_dir/vps-workstation.err")"

assert_contains \
  "$vps_workstation_error" \
  "workstation Preset is only available for the macOS Terminal Profile" \
  "VPS Shell Profile explains workstation Preset rejection"

assert_not_contains \
  "$vps_workstation_error" \
  "gum is required" \
  "VPS Shell Profile no-gum workstation rejection does not require gum"

setup_home="$tmp_dir/setup-home"
setup_xdg="$tmp_dir/setup-xdg"
setup_output="$tmp_dir/setup.out"
setup_responses="$tmp_dir/setup.responses"
setup_gum_log="$tmp_dir/setup-gum.log"
setup_config="$setup_xdg/chezmoi/chezmoi.toml"
mkdir -p "$setup_home"
write_gum_responses "$setup_responses" development yes yes yes yes yes yes yes

if ! run_terrapod_setup_command macos-terminal "$setup_responses" "$setup_home" "$setup_xdg" "$setup_output" "$setup_gum_log"; then
  sed 's/^/  /' "$setup_output" >&2
  fail "macOS Terminal Profile setup uses gum prompts before final confirmation and completes with development"
fi
pass "macOS Terminal Profile setup uses gum prompts before final confirmation and completes with development"

setup_output_text="$(cat "$setup_output")"
setup_gum_log_text="$(cat "$setup_gum_log")"
assert_contains "$setup_output_text" "🌱 Terrapod Setup" "gum setup prints a rich setup heading"
assert_contains "$setup_output_text" "Profile  macOS Terminal Profile" "gum setup shows detected macOS profile in aligned setup context"
assert_contains "$setup_output_text" "Choose a Preset" "gum setup labels the Preset choice section"
assert_not_contains "$setup_output_text" "Preset guide:" "gum setup does not print a separate Preset guide"
assert_contains "$setup_output_text" "Settings to write:" "gum setup shows concrete settings summary"
assert_contains "$setup_output_text" "Customize Terrapod settings." "gum setup offers sequential setting customization"
assert_not_contains "$setup_output_text" "Option guide:" "gum setup does not print a separate option guide"
assert_contains "$setup_output_text" "Optional Development Workspace" "gum setup shows Optional Development Workspace setting title"
assert_contains "$setup_output_text" "  Dev Zellij layouts." "gum setup describes Optional Development Workspace under its title"
assert_contains "$setup_output_text" "  Includes:" "gum setup groups workspace inclusions"
assert_contains "$setup_output_text" "    - Optional Editor Stack" "gum setup lists Optional Editor Stack as included by workspace"
assert_contains "$setup_output_text" "    - Optional AI Tool Stack" "gum setup lists Optional AI Tool Stack as included by workspace"
assert_not_contains "$setup_output_text" "Optional Development Workspace: Dev Zellij layouts; also includes Editor and AI tool stacks." "gum setup no longer prints workspace description before the setting title"
assert_not_contains "$setup_output_text" "Optional Editor Stack: included by Optional Development Workspace" "gum setup no longer repeats workspace-included Editor Stack as a standalone message"
assert_not_contains "$setup_output_text" "Optional AI Tool Stack: included by Optional Development Workspace" "gum setup no longer repeats workspace-included AI Tool Stack as a standalone message"
assert_contains "$setup_output_text" "terminal-apps" "gum setup leads terminal-apps App Group prompt with the group name"
assert_contains "$setup_output_text" "  Installs Ghostty." "gum setup describes terminal-apps under its group name"
assert_contains "$setup_output_text" "automation" "gum setup leads automation App Group prompt with the group name"
assert_contains "$setup_output_text" "  Installs Hammerspoon, Karabiner-Elements, and Scroll Reverser." "gum setup describes automation under its group name"
assert_contains "$setup_output_text" "development-apps" "gum setup leads development-apps App Group prompt with the group name"
assert_contains "$setup_output_text" "  Installs Zed and Orca ADE." "gum setup lists Zed and Orca ADE in the development-apps App Group"
assert_contains "$setup_output_text" "  Trusts only the fully-qualified stablyai/orca/orca cask, not the entire stablyai/orca tap." "gum setup discloses Orca's cask-specific trust boundary"
assert_contains "$setup_output_text" "enableEditorStack = true" "gum setup summary includes concrete Editor Stack setting"
assert_contains "$setup_output_text" "enableMacosAppGroupMonitoring = true" "gum setup summary includes concrete macOS App Group setting"
assert_contains "$setup_output_text" "enableMacosAppGroupDevelopmentApps = true" "gum setup summary includes concrete development-apps App Group setting"
assert_contains "$setup_output_text" "Configured Terrapod Preset 'development'" "gum setup reports successful configuration"
assert_contains "$setup_gum_log_text" "gum args: style" "gum setup uses gum style for setup-only presentation"
assert_contains "$setup_gum_log_text" "gum args: choose" "gum setup uses gum choose for Preset selection"
assert_contains "$setup_gum_log_text" "gum stdin: minimal      Core shell/runtime baseline:minimal" "gum setup offers minimal Preset with nearby explanation"
assert_contains "$setup_gum_log_text" "gum stdin: development  Coding machine with editor, AI CLI, workspace:development" "gum setup offers development Preset with nearby explanation"
assert_contains "$setup_gum_log_text" "gum stdin: workstation  macOS workstation with development setup and app groups:workstation" "gum setup offers workstation Preset with nearby explanation"
assert_contains "$setup_gum_log_text" "gum args: confirm Enable Optional Development Workspace?" "gum setup asks stable Enable question for Optional Development Workspace"
assert_contains "$setup_gum_log_text" " --affirmative Keep enabled" "gum setup uses Keep enabled action for enabled Preset-proposed values"
assert_contains "$setup_gum_log_text" " --negative Disable" "gum setup uses Disable action for enabled Preset-proposed values"
assert_contains "$setup_gum_log_text" "gum args: confirm Enable terminal-apps?" "gum setup asks stable Enable question for terminal-apps"
assert_contains "$setup_gum_log_text" " --affirmative Enable" "gum setup uses Enable action for disabled Preset-proposed values"
assert_contains "$setup_gum_log_text" " --negative Keep disabled" "gum setup uses Keep disabled action for disabled Preset-proposed values"
assert_contains "$setup_gum_log_text" "gum args: confirm Enable development-apps?" "gum setup asks stable Enable question for development-apps"
assert_contains "$setup_gum_log_text" "gum args: confirm Write these Terrapod settings" "gum setup asks final confirmation with gum confirm"
assert_first_occurrence_before "$setup_output_text" "Profile  macOS Terminal Profile" "Customize Terrapod settings." "gum setup shows profile before settings customization"
assert_first_occurrence_before "$setup_output_text" "Choose a Preset" "Customize Terrapod settings." "gum setup presents Preset selection before customization"
assert_first_occurrence_before "$setup_output_text" "Optional Development Workspace" "    - Optional Editor Stack" "gum setup groups included stacks under Optional Development Workspace"
assert_first_occurrence_before "$setup_output_text" "Customize Terrapod settings." "Settings to write:" "gum setup shows customized settings before summary"

if [ ! -f "$setup_config" ]; then
  fail "gum setup writes config after final confirmation"
fi
pass "gum setup writes config after final confirmation"

dumb_setup_home="$tmp_dir/dumb-setup-home"
dumb_setup_xdg="$tmp_dir/dumb-setup-xdg"
dumb_setup_output="$tmp_dir/dumb-setup.out"
mkdir -p "$dumb_setup_home"

if TERRAPOD_GUM_RESPONSES="$tmp_dir/dumb.responses" TERRAPOD_GUM_LOG="$tmp_dir/dumb-gum.log" \
  PATH="$tmp_dir/bin:/usr/bin:/bin" \
  run_setup_in_pty macos-terminal dumb "$dumb_setup_home" "$dumb_setup_xdg" >"$dumb_setup_output" 2>&1; then
  sed 's/^/  /' "$dumb_setup_output" >&2
  fail "TERM=dumb setup fails instead of falling back to plain prompts"
fi
pass "TERM=dumb setup fails instead of falling back to plain prompts"

dumb_setup_output_text="$(cat "$dumb_setup_output")"
assert_contains "$dumb_setup_output_text" "requires an interactive terminal supported by gum" "TERM=dumb setup explains unsupported terminal environment"

vps_setup_home="$tmp_dir/vps-setup-home"
vps_setup_xdg="$tmp_dir/vps-setup-xdg"
vps_setup_output="$tmp_dir/vps-setup.out"
vps_setup_responses="$tmp_dir/vps-setup.responses"
vps_setup_gum_log="$tmp_dir/vps-setup-gum.log"
mkdir -p "$vps_setup_home"
write_gum_responses "$vps_setup_responses" workstation

if run_terrapod_setup_command vps-shell "$vps_setup_responses" "$vps_setup_home" "$vps_setup_xdg" "$vps_setup_output" "$vps_setup_gum_log"; then
  fail "VPS Shell Profile setup rejects workstation Preset"
fi
pass "VPS Shell Profile setup rejects workstation Preset"

vps_setup_output_text="$(cat "$vps_setup_output")"
assert_contains "$vps_setup_output_text" "Profile  VPS Shell Profile" "VPS setup shows detected profile before rejection"
assert_contains "$vps_setup_output_text" "workstation Preset is only available for the macOS Terminal Profile" "VPS setup explains workstation rejection"
vps_setup_gum_log_text="$(cat "$vps_setup_gum_log")"
assert_contains "$vps_setup_gum_log_text" "gum args: choose" "VPS setup uses gum choose for Preset selection"
assert_contains "$vps_setup_gum_log_text" "gum stdin: minimal      Core shell/runtime baseline:minimal" "VPS setup offers minimal Preset with nearby explanation"
assert_contains "$vps_setup_gum_log_text" "gum stdin: development  Coding machine with editor, AI CLI, workspace:development" "VPS setup offers development Preset with nearby explanation"
assert_not_contains "$vps_setup_gum_log_text" "workstation" "VPS setup does not offer workstation Preset"

if [ -e "$vps_setup_xdg/chezmoi/chezmoi.toml" ]; then
  fail "VPS rejected setup does not write config"
fi
pass "VPS rejected setup does not write config"

assert_no_terrapod_artifacts_under "$vps_setup_xdg" "VPS rejected setup leaves no Terrapod artifacts"

preset_cancel_home="$tmp_dir/preset-cancel-home"
preset_cancel_xdg="$tmp_dir/preset-cancel-xdg"
preset_cancel_output="$tmp_dir/preset-cancel.out"
preset_cancel_responses="$tmp_dir/preset-cancel.responses"
preset_cancel_gum_log="$tmp_dir/preset-cancel-gum.log"
preset_cancel_config="$preset_cancel_xdg/chezmoi/chezmoi.toml"
mkdir -p "$preset_cancel_home"
write_gum_responses "$preset_cancel_responses" __CANCEL__

if run_terrapod_setup_command macos-terminal "$preset_cancel_responses" "$preset_cancel_home" "$preset_cancel_xdg" "$preset_cancel_output" "$preset_cancel_gum_log"; then
  fail "Preset selection cancellation exits non-zero"
fi
pass "Preset selection cancellation exits non-zero"

preset_cancel_output_text="$(cat "$preset_cancel_output")"
assert_contains "$preset_cancel_output_text" "setup cancelled" "Preset selection cancellation preserves setup cancellation guidance"
assert_not_contains "$preset_cancel_output_text" "no Terrapod Preset selected" "Preset selection cancellation is not reported as missing selection"

if [ -e "$preset_cancel_config" ]; then
  fail "Preset selection cancellation does not write config"
fi
pass "Preset selection cancellation does not write config"

assert_no_terrapod_artifacts_under "$preset_cancel_xdg" "Preset selection cancellation leaves no Terrapod artifacts"

gum_error_home="$tmp_dir/gum-error-home"
gum_error_xdg="$tmp_dir/gum-error-xdg"
gum_error_output="$tmp_dir/gum-error.out"
gum_error_responses="$tmp_dir/gum-error.responses"
gum_error_gum_log="$tmp_dir/gum-error-gum.log"
gum_error_config="$gum_error_xdg/chezmoi/chezmoi.toml"
mkdir -p "$gum_error_home"
write_gum_responses "$gum_error_responses" minimal __ERROR__

if run_terrapod_setup_command macos-terminal "$gum_error_responses" "$gum_error_home" "$gum_error_xdg" "$gum_error_output" "$gum_error_gum_log"; then
  fail "setup fails when gum returns an operational error"
fi
pass "setup fails when gum returns an operational error"

gum_error_output_text="$(cat "$gum_error_output")"
assert_contains "$gum_error_output_text" "gum failed during Terrapod Setup" "gum operational error explains gum failure"
assert_contains "$gum_error_output_text" "rerun 'tpod setup'" "gum operational error gives rerun guidance with tpod"
assert_not_contains "$gum_error_output_text" "rerun 'terrapod setup'" "gum operational error avoids old rerun command"
assert_not_contains "$gum_error_output_text" "setup cancelled" "gum operational error is not reported as cancellation"

if [ -e "$gum_error_config" ]; then
  fail "gum operational error does not write config"
fi
pass "gum operational error does not write config"

assert_no_terrapod_artifacts_under "$gum_error_xdg" "gum operational error leaves no Terrapod artifacts"

missing_gum_home="$tmp_dir/missing-gum-home"
missing_gum_xdg="$tmp_dir/missing-gum-xdg"
missing_gum_output="$tmp_dir/missing-gum.out"
mkdir -p "$missing_gum_home"

if printf '%s\n' "workstation" |
  PATH="$no_gum_path" TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG= HOME="$missing_gum_home" XDG_CONFIG_HOME="$missing_gum_xdg" sh "$terrapod" setup >"$missing_gum_output" 2>&1; then
  fail "setup fails when gum is missing"
fi
pass "setup fails when gum is missing"

missing_gum_output_text="$(cat "$missing_gum_output")"
assert_contains "$missing_gum_output_text" "gum is required before Terrapod Setup can run" "missing gum setup explains gum requirement"
assert_contains "$missing_gum_output_text" "Install gum, then rerun Terrapod Setup" "missing gum setup gives rerun guidance"
assert_not_contains "$missing_gum_output_text" "Configured Terrapod Preset" "missing gum setup does not fall back to configure success"

if [ -e "$missing_gum_xdg/chezmoi/chezmoi.toml" ]; then
  fail "missing gum setup does not write config"
fi
pass "missing gum setup does not write config"

assert_no_terrapod_artifacts_under "$missing_gum_xdg" "missing gum setup leaves no Terrapod artifacts"

old_gum_home="$tmp_dir/old-gum-home"
old_gum_xdg="$tmp_dir/old-gum-xdg"
old_gum_output="$tmp_dir/old-gum.out"
old_gum_log="$tmp_dir/old-gum.log"
old_gum_path="$tmp_dir/old-gum-bin"
mkdir -p "$old_gum_home" "$old_gum_path"
write_old_gum_stub "$old_gum_path/gum"

if TERRAPOD_GUM_RESPONSES="$tmp_dir/old-gum.responses" TERRAPOD_GUM_LOG="$old_gum_log" \
  PATH="$old_gum_path:/usr/bin:/bin" \
  run_setup_in_pty macos-terminal xterm "$old_gum_home" "$old_gum_xdg" >"$old_gum_output" 2>&1; then
  sed 's/^/  /' "$old_gum_output" >&2
  fail "setup rejects gum without choose label support"
fi
pass "setup rejects gum without choose label support"

old_gum_output_text="$(cat "$old_gum_output")"
old_gum_log_text="$(cat "$old_gum_log")"
assert_contains "$old_gum_output_text" "gum is too old for Terrapod Setup" "old gum setup explains the unsupported gum capability"
assert_contains "$old_gum_output_text" "Upgrade gum, then rerun Terrapod Setup" "old gum setup gives upgrade guidance"
assert_contains "$old_gum_output_text" "Homebrew: HOMEBREW_NO_AUTO_UPDATE=1 brew upgrade gum || HOMEBREW_NO_AUTO_UPDATE=1 brew install gum" "old gum setup gives explicit Homebrew upgrade guidance"
assert_contains "$old_gum_output_text" "APT: sudo apt-get update && sudo apt-get install --only-upgrade gum" "old gum setup gives explicit APT upgrade guidance"
assert_contains "$old_gum_output_text" "replace the older gum earlier on PATH" "old gum setup explains manually installed gum shadowing"
assert_not_contains "$old_gum_output_text" "rerun the installer" "old gum setup does not suggest rerunning the installer"
assert_contains "$old_gum_log_text" "gum args: choose --help" "old gum setup checks choose label support"
assert_not_contains "$old_gum_log_text" "gum args: choose --label-delimiter" "old gum setup fails before using label-delimiter"

if [ -e "$old_gum_xdg/chezmoi/chezmoi.toml" ]; then
  fail "old gum setup does not write config"
fi
pass "old gum setup does not write config"

assert_no_terrapod_artifacts_under "$old_gum_xdg" "old gum setup leaves no Terrapod artifacts"

noninteractive_setup_home="$tmp_dir/noninteractive-setup-home"
noninteractive_setup_xdg="$tmp_dir/noninteractive-setup-xdg"
noninteractive_setup_output="$tmp_dir/noninteractive-setup.out"
mkdir -p "$noninteractive_setup_home"

if TERRAPOD_GUM_RESPONSES="$tmp_dir/noninteractive.responses" TERRAPOD_GUM_LOG="$tmp_dir/noninteractive-gum.log" \
  PATH="$tmp_dir/bin:/usr/bin:/bin" TERM=xterm TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG= HOME="$noninteractive_setup_home" XDG_CONFIG_HOME="$noninteractive_setup_xdg" sh "$terrapod" setup >"$noninteractive_setup_output" 2>&1 </dev/null; then
  fail "setup fails when stdin is not interactive"
fi
pass "setup fails when stdin is not interactive"

noninteractive_setup_output_text="$(cat "$noninteractive_setup_output")"
assert_contains "$noninteractive_setup_output_text" "requires an interactive terminal supported by gum" "non-interactive setup explains terminal requirement"

assert_contains \
  "$help_output" \
  "Expand a Preset into concrete chezmoi data values." \
  "Terrapod help describes Preset configuration"

assert_contains \
  "$help_output" \
  "Run raw chezmoi for advanced maintenance." \
  "Terrapod help describes raw chezmoi as advanced maintenance"

default_output="$(sh "$terrapod")"

assert_contains \
  "$default_output" \
  "Terrapod - a small landing pod for your dotfiles" \
  "Terrapod with no arguments shows help"

dash_help_output="$(sh "$terrapod" --help)"

assert_contains \
  "$dash_help_output" \
  "Terrapod - a small landing pod for your dotfiles" \
  "Terrapod --help shows help"

if sh "$terrapod" frobnicate >"$tmp_dir/unknown.out" 2>"$tmp_dir/unknown.err"; then
  fail "unknown Terrapod subcommands fail"
fi

unknown_error="$(cat "$tmp_dir/unknown.err")"

assert_contains \
  "$unknown_error" \
  "terrapod: unknown command: frobnicate" \
  "unknown Terrapod subcommands name the rejected command"

assert_contains \
  "$unknown_error" \
  "Run 'tpod help' for usage." \
  "unknown Terrapod subcommands point to help"

export CHEZMOI_CALL_FILE="$tmp_dir/chezmoi.args"

write_stub "$tmp_dir/bin/chezmoi" \
  ': >"$CHEZMOI_CALL_FILE"' \
  'for arg do' \
  '  printf "%s\n" "$arg" >>"$CHEZMOI_CALL_FILE"' \
  'done' \
  'exit 0'

export PATH="$tmp_dir/bin:/usr/bin:/bin"

sh "$terrapod" chezmoi -- status --include files

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod passes raw arguments to chezmoi after --" \
  status --include files

"$tmp_dir/bin/tpod" chezmoi -- diff

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "tpod uses the same raw chezmoi escape hatch as Terrapod" \
  diff

if sh "$terrapod" chezmoi status >"$tmp_dir/missing-separator.out" 2>"$tmp_dir/missing-separator.err"; then
  fail "raw chezmoi escape hatch requires -- separator"
fi

missing_separator_error="$(cat "$tmp_dir/missing-separator.err")"

assert_contains \
  "$missing_separator_error" \
  "terrapod: raw chezmoi commands must be separated with '--'" \
  "Terrapod explains missing raw chezmoi separator"

if sh "$terrapod" chezmoi -- >"$tmp_dir/missing-command.out" 2>"$tmp_dir/missing-command.err"; then
  fail "raw chezmoi escape hatch requires a command after --"
fi

missing_command_error="$(cat "$tmp_dir/missing-command.err")"

assert_contains \
  "$missing_command_error" \
  "terrapod: raw chezmoi command is required" \
  "Terrapod explains missing raw chezmoi command"

status_config="$tmp_dir/status-macos.toml"
cat >"$status_config" <<'TOML'
[data]
profile = "macos-terminal"
enableEditorStack = true
enableAiCliTools = true
enableDevelopmentWorkspace = true
enableMacosAppGroupTerminalApps = true
enableMacosAppGroupAutomation = true
enableMacosAppGroupLauncher = true
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = true
TOML

macos_status_path="$(status_doctor_path macos chezmoi git zsh mise brew nvim agy claude codex zellij ghostty op)"

macos_status_output="$(
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$status_config" PATH="$macos_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$macos_status_output" "Terrapod status" "Terrapod status prints a command heading"
assert_contains "$macos_status_output" "Profile: macOS Terminal Profile" "Terrapod status reports macOS Terminal Profile context"
assert_contains "$macos_status_output" "Config: $status_config (present)" "Terrapod status reports explicit config path"
assert_contains "$macos_status_output" "Optional Editor Stack         : enabled (rich Neovim configuration)" "Terrapod status reports enabled Optional Editor Stack state"
assert_contains "$macos_status_output" "Optional AI Tool Stack        : enabled (tools available: agy, claude, codex)" "Terrapod status reports enabled Optional AI Tool Stack tool state"
assert_contains "$macos_status_output" "Optional Development Workspace: enabled (development Zellij layouts)" "Terrapod status reports enabled Optional Development Workspace state"
assert_contains "$macos_status_output" "terminal-apps                 : enabled (Ghostty)" "Terrapod status reports enabled Ghostty-only terminal-apps macOS App Group"
assert_contains "$macos_status_output" "automation                    : enabled (Hammerspoon, Karabiner-Elements, and Scroll Reverser)" "Terrapod status reports enabled automation macOS App Group"
assert_contains "$macos_status_output" "launcher                      : enabled (Raycast and 1Password CLI)" "Terrapod status reports enabled launcher macOS App Group"
assert_contains "$macos_status_output" "monitoring                    : disabled" "Terrapod status reports disabled monitoring macOS App Group"
assert_contains "$macos_status_output" "development-apps              : enabled (Zed and Orca ADE)" "Terrapod status lists Zed and Orca ADE in the enabled development-apps App Group"
assert_contains "$macos_status_output" "chezmoi                       : available" "Terrapod status reports chezmoi availability"
assert_contains "$macos_status_output" "brew                          : available" "Terrapod status reports macOS Bootstrap Package Manager availability"
assert_contains "$macos_status_output" "Warnings: none" "Terrapod status reports no warnings when enabled tools are present"
assert_not_contains "$macos_status_output" "Warning:" "Terrapod status emits no warning lines when enabled tools are present"
assert_no_ansi_escape "$help_output" "Terrapod help does not use setup ANSI presentation"
assert_no_rich_setup_emoji "$help_output" "Terrapod help does not use setup emoji presentation"
assert_no_ansi_escape "$macos_status_output" "Terrapod status does not use setup ANSI presentation"
assert_no_rich_setup_emoji "$macos_status_output" "Terrapod status does not use setup emoji presentation"
assert_no_routine_emoji "$macos_status_output" "captured Terrapod status has no routine emoji"

status_marker_state="$tmp_dir/status-marker-state"
HOME="$tmp_dir/status-marker-home" XDG_STATE_HOME="$status_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "Run tpod apply after fixing the Homebrew bundle error."' \
  sh "$install_warnings_lib"

if ! status_marker_output="$(
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$status_config" XDG_STATE_HOME="$status_marker_state" PATH="$macos_status_path" \
    /bin/sh "$terrapod" status
)"; then
  fail "Terrapod status exits successfully when install warning markers remain"
fi

assert_contains "$status_marker_output" "Install warnings              : present (homebrew-core)" "Terrapod status summarizes install warning marker presence"
assert_not_contains "$status_marker_output" "Run tpod apply after fixing the Homebrew bundle error." "Terrapod status does not expand install warning marker details"

rm -f "$fake_warning_calls"
if ! status_missing_lib_output="$(
  FAKE_INSTALL_WARNING_CALLS="$fake_warning_calls" TERRAPOD_INSTALL_WARNINGS_LIB="$tmp_dir/missing-install-warnings-lib" TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$status_config" PATH="$fake_warning_bin:$macos_status_path" \
    /bin/sh "$terrapod" status
)"; then
  fail "Terrapod status exits successfully when the install warning library is unavailable"
fi
assert_contains "$status_missing_lib_output" "Install warnings              : none" "Terrapod status ignores PATH fake install warning helpers when the shared library is unavailable"
if [ -e "$fake_warning_calls" ]; then
  fail "Terrapod status does not execute PATH fake install warning helpers"
fi
pass "Terrapod status does not execute PATH fake install warning helpers"

tty_macos_status_output="$(
  run_command_in_pty xterm unset env TERRAPOD_CHEZMOI_CONFIG="$status_config" PATH="$macos_status_path" /bin/sh "$terrapod" status
)"
assert_has_ansi_escape "$tty_macos_status_output" "TTY Terrapod status with config uses ANSI visual treatment"
assert_not_contains_colored_phrase "$tty_macos_status_output" "Profile: macOS Terminal Profile" "TTY Terrapod status keeps profile value out of the section style"
assert_not_contains_colored_phrase "$tty_macos_status_output" "Config: $status_config" "TTY Terrapod status keeps config path neutral"
assert_not_contains_colored_phrase "$tty_macos_status_output" "Optional Editor Stack         : enabled (rich Neovim configuration)" "TTY Terrapod status does not color optional-stack detail as one phrase"
assert_not_contains_colored_phrase "$tty_macos_status_output" "Optional AI Tool Stack        : enabled (tools available: agy, claude, codex)" "TTY Terrapod status does not color optional-stack tool list as one phrase"
assert_not_contains_colored_phrase "$tty_macos_status_output" "chezmoi                       : available" "TTY Terrapod status does not color tool names with state words"

status_dotted_config="$tmp_dir/status-dotted.toml"
cat >"$status_dotted_config" <<'TOML'
data.profile = "macos-terminal"
data.enableEditorStack = false
data.enableAiCliTools = false
data.enableDevelopmentWorkspace = true
data.enableMacosAppGroupAutomation = false
data.enableMacosAppGroupLauncher = false
data.enableMacosAppGroupMonitoring = false
data.enableMacosAppGroupTerminalApps = true
data.enableMacosAppGroupDevelopmentApps = true
TOML

dotted_status_path="$(status_doctor_path dotted chezmoi git zsh mise brew nvim agy claude codex zellij)"

dotted_status_output="$(
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$status_dotted_config" PATH="$dotted_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$dotted_status_output" "Optional Editor Stack         : enabled (rich Neovim configuration)" "Terrapod status reads root dotted data keys for effective editor stack state"
assert_contains "$dotted_status_output" "Optional AI Tool Stack        : enabled (tools available: agy, claude, codex)" "Terrapod status reads root dotted data keys for effective AI stack state"
assert_contains "$dotted_status_output" "terminal-apps                 : enabled (Ghostty)" "Terrapod status reads root dotted data keys for Ghostty-only macOS App Groups"
assert_contains "$dotted_status_output" "development-apps              : enabled (Zed and Orca ADE)" "Terrapod status reads root dotted data keys for Zed and Orca ADE in development-apps"
assert_contains "$dotted_status_output" "Warnings: none" "Terrapod status has no warnings for root dotted data keys when tools are present"

status_ubuntu_config="$tmp_dir/status-ubuntu.toml"
status_ubuntu_os_release="$tmp_dir/status-ubuntu-os-release"
cat >"$status_ubuntu_config" <<'TOML'
[data]
profile = "vps-shell"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
TOML
write_os_release "$status_ubuntu_os_release" ubuntu 24.04 "Ubuntu 24.04 LTS"

ubuntu_status_path="$(status_doctor_path ubuntu chezmoi git zsh mise nvim zellij apt)"
write_stub "$ubuntu_status_path/uname" 'printf "%s\n" "Linux"'

ubuntu_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$ubuntu_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$ubuntu_status_output" "Profile: VPS Shell Profile" "Terrapod status reports VPS Shell Profile context on Ubuntu 24.04"
assert_contains "$ubuntu_status_output" "Optional Editor Stack         : disabled" "Terrapod status reports disabled Optional Editor Stack without treating nvim as missing"
assert_contains "$ubuntu_status_output" "Optional AI Tool Stack        : disabled" "Terrapod status reports disabled Optional AI Tool Stack without missing-tool warnings"
assert_contains "$ubuntu_status_output" "Optional Development Workspace: disabled" "Terrapod status reports disabled Optional Development Workspace without missing-tool warnings"
assert_contains "$ubuntu_status_output" "macOS App Groups: not applicable for VPS Shell Profile" "Terrapod status omits macOS App Group details on VPS Shell Profile"
assert_contains "$ubuntu_status_output" "apt                           : available" "Terrapod status reports Ubuntu Bootstrap Package Manager availability"
assert_contains "$ubuntu_status_output" "Warnings: none" "Terrapod status has no warnings for disabled optional stacks"
assert_not_contains "$ubuntu_status_output" "Warning:" "Terrapod status emits no warning lines for disabled optional stacks"
assert_not_contains "$ubuntu_status_output" "brew                          : missing" "disabled Ubuntu Optional AI Tool Stack does not require Homebrew"
assert_not_contains "$ubuntu_status_output" "missing tools: nvim" "Terrapod status distinguishes disabled Optional Editor Stack from missing tools"
assert_not_contains "$ubuntu_status_output" "missing tools: agy" "Terrapod status distinguishes disabled Optional AI Tool Stack from missing tools"

status_incomplete_vps_config="$tmp_dir/status-incomplete-vps.toml"
cat >"$status_incomplete_vps_config" <<'TOML'
[data]
profile = "vps-shell"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
TOML

if ! status_incomplete_vps_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_incomplete_vps_config" PATH="$ubuntu_status_path" \
    /bin/sh "$terrapod" status
)"; then
  fail "Terrapod status exits successfully when managed setup config is incomplete"
fi

assert_contains "$status_incomplete_vps_output" "Config: $status_incomplete_vps_config (present; incomplete managed setup config)" "Terrapod status reports incomplete managed setup config in the Config section"
assert_contains "$status_incomplete_vps_output" "enableMacosAppGroupDevelopmentApps" "Terrapod status identifies missing managed setup keys even on VPS"
assert_contains "$status_incomplete_vps_output" "Run 'tpod setup' or 'tpod configure <minimal|development>' to complete the managed setup config." "Terrapod status guides incomplete config recovery with tpod setup or configure"

status_unsafe_multiline_config="$tmp_dir/status-unsafe-multiline.toml"
cat >"$status_unsafe_multiline_config" <<'TOML'
[data]
notes = """
profile = "vps-shell"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
"""
TOML

if ! status_unsafe_multiline_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_unsafe_multiline_config" PATH="$ubuntu_status_path" \
    /bin/sh "$terrapod" status
)"; then
  fail "Terrapod status exits successfully when managed setup config contains unsupported multiline strings"
fi

assert_contains "$status_unsafe_multiline_output" "Config: $status_unsafe_multiline_config (unsupported managed setup config)" "Terrapod status reports unsupported multiline config in the Config section"
assert_contains "$status_unsafe_multiline_output" "unsupported multiline string" "Terrapod status explains unsupported multiline config"
assert_not_contains "$status_unsafe_multiline_output" "Config: $status_unsafe_multiline_config (present)" "Terrapod status does not trust managed keys inside multiline strings"

status_unreadable_config="$tmp_dir/status-unreadable.toml"
cat >"$status_unreadable_config" <<'TOML'
[data]
profile = "vps-shell"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
TOML
chmod 000 "$status_unreadable_config"

if ! status_unreadable_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_unreadable_config" PATH="$ubuntu_status_path" \
    /bin/sh "$terrapod" status
)"; then
  chmod 644 "$status_unreadable_config"
  fail "Terrapod status exits successfully when the managed setup config is unreadable"
fi
chmod 644 "$status_unreadable_config"

assert_contains "$status_unreadable_output" "Config: $status_unreadable_config (unreadable managed setup config)" "Terrapod status reports an unreadable managed setup config in the Config section"
assert_contains "$status_unreadable_output" "Fix the config path or permissions so it is a readable regular file." "Terrapod status gives path or permission guidance for unreadable config"
assert_not_contains "$status_unreadable_output" "Missing managed setup keys" "Terrapod status does not report missing managed setup keys for unreadable config"
assert_not_contains "$status_unreadable_output" "managed setup config is incomplete" "Terrapod status does not misreport unreadable config as incomplete managed setup keys"

core_missing_status_path="$(status_doctor_path core-missing-status chezmoi git zsh mise apt)"
write_stub "$core_missing_status_path/uname" 'printf "%s\n" "Linux"'

core_missing_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$core_missing_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$core_missing_status_output" "Optional Editor Stack         : disabled" "Terrapod status keeps disabled Optional Editor Stack separate from missing core Neovim"
assert_contains "$core_missing_status_output" "Optional Development Workspace: disabled" "Terrapod status keeps disabled Optional Development Workspace separate from missing core Zellij"
assert_contains "$core_missing_status_output" "nvim                          : missing" "Terrapod status reports missing plain Neovim as a key tool"
assert_contains "$core_missing_status_output" "zellij                        : missing" "Terrapod status reports missing Zellij as a key tool"
assert_contains "$core_missing_status_output" "Warning: missing key tools: nvim, zellij" "Terrapod status warns about missing core Neovim and Zellij even when optional stacks are disabled"

status_missing_config="$tmp_dir/status-missing.toml"
cat >"$status_missing_config" <<'TOML'
[data]
enableEditorStack = true
enableAiCliTools = true
enableDevelopmentWorkspace = true
TOML

missing_status_path="$(status_doctor_path missing chezmoi git zsh mise nvim zellij apt)"
write_stub "$missing_status_path/uname" 'printf "%s\n" "Linux"'

missing_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_missing_config" PATH="$missing_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$missing_status_output" "Optional Editor Stack         : enabled (rich Neovim configuration)" "Terrapod status reports enabled Optional Editor Stack as rich config state"
assert_contains "$missing_status_output" "Optional AI Tool Stack        : enabled (missing tools: agy, claude, codex)" "Terrapod status reports missing tools only for enabled Optional AI Tool Stack"
assert_contains "$missing_status_output" "Optional Development Workspace: enabled (development Zellij layouts)" "Terrapod status reports enabled Optional Development Workspace as layout state"
assert_contains "$missing_status_output" "brew                          : missing" "enabled Ubuntu Optional AI Tool Stack requires Homebrew"
assert_contains "$missing_status_output" "Warning: missing key tools: brew" "enabled Ubuntu Optional AI Tool Stack warns when Homebrew is missing"
assert_contains "$missing_status_output" "Warning: Optional AI Tool Stack is enabled but missing tools: agy, claude, codex" "Terrapod status warns for enabled missing AI tools"

status_shadow_config="$tmp_dir/status-shadow.toml"
cat >"$status_shadow_config" <<'TOML'
[data]
profile = "vps-shell"
enableEditorStack = false
enableAiCliTools = true
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
TOML

status_shadow_path="$(status_doctor_path shadow chezmoi git zsh mise nvim zellij apt brew agy claude codex)"
status_shadow_legacy="$tmp_dir/status-shadow-legacy"
mkdir -p "$status_shadow_legacy"
mv "$status_shadow_path/claude" "$status_shadow_legacy/claude"
write_stub "$status_shadow_path/brew" \
  'case "$1" in' \
  '  --prefix) prefix="${0%/*}"; printf "%s\n" "$prefix" ;;' \
  '  *) exit 0 ;;' \
  'esac'
write_stub "$status_shadow_path/uname" 'printf "%s\n" "Linux"'

status_shadow_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_shadow_config" PATH="$status_shadow_legacy:$status_shadow_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$status_shadow_output" "Warning: Optional AI Tool Stack has non-Homebrew commands shadowing managed casks: claude" "Terrapod status reports legacy Claude shadowing the Homebrew cask"

status_broken_prefix_path="$(status_doctor_path broken-prefix chezmoi git zsh mise nvim zellij apt brew agy claude codex)"
write_stub "$status_broken_prefix_path/brew" \
  'if [ "${1:-}" = "--prefix" ]; then exit 1; fi' \
  'exit 0'
write_stub "$status_broken_prefix_path/uname" 'printf "%s\n" "Linux"'

status_broken_prefix_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_shadow_config" PATH="$status_broken_prefix_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$status_broken_prefix_output" "Warning: Optional AI Tool Stack cannot verify Homebrew command ownership because 'brew --prefix' failed" "Terrapod status reports an unusable Homebrew prefix"

status_workspace_bundle_config="$tmp_dir/status-workspace-bundle.toml"
cat >"$status_workspace_bundle_config" <<'TOML'
[data]
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = true
TOML

workspace_bundle_status_path="$(status_doctor_path workspace-bundle chezmoi git zsh mise nvim zellij apt)"
write_stub "$workspace_bundle_status_path/uname" 'printf "%s\n" "Linux"'

workspace_bundle_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_workspace_bundle_config" PATH="$workspace_bundle_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$workspace_bundle_status_output" "Optional Editor Stack         : enabled (rich Neovim configuration)" "Terrapod status treats Optional Development Workspace as enabling Optional Editor Stack"
assert_contains "$workspace_bundle_status_output" "Optional AI Tool Stack        : enabled (missing tools: agy, claude, codex)" "Terrapod status treats Optional Development Workspace as enabling Optional AI Tool Stack"
assert_contains "$workspace_bundle_status_output" "Optional Development Workspace: enabled (development Zellij layouts)" "Terrapod status reports enabled Optional Development Workspace"
assert_contains "$workspace_bundle_status_output" "Warning: Optional AI Tool Stack is enabled but missing tools: agy, claude, codex" "Terrapod status warns when workspace-enabled Optional AI Tool Stack tools are missing"

status_unsupported_os_release="$tmp_dir/status-unsupported-os-release"
write_os_release "$status_unsupported_os_release" debian 12 "Debian GNU/Linux 12"

unsupported_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_unsupported_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$ubuntu_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$unsupported_status_output" "Profile: unsupported profile" "Terrapod status reports unsupported Linux as unsupported"
assert_contains "$unsupported_status_output" "Warning: unsupported Linux release: Debian GNU/Linux 12. Terrapod supports Ubuntu 24.04 for the VPS Shell Profile." "Terrapod status explains unsupported Linux"

doctor_config="$tmp_dir/doctor-ok.toml"
doctor_os_release="$tmp_dir/doctor-os-release"
cat >"$doctor_config" <<'TOML'
[data]
profile = "vps-shell"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
TOML
write_os_release "$doctor_os_release" ubuntu 24.04 "Ubuntu 24.04 LTS"

doctor_ok_path="$(status_doctor_path doctor-ok chezmoi git zsh mise nvim zellij apt)"
write_stub "$doctor_ok_path/uname" 'printf "%s\n" "Linux"'

if ! doctor_ok_output="$(
  TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_ok_path" \
    /bin/sh "$terrapod" doctor
)"; then
  fail "Terrapod doctor succeeds when VPS prerequisites are present and optional stacks are disabled"
fi

assert_contains "$doctor_ok_output" "Terrapod doctor" "Terrapod doctor prints a command heading"
assert_contains "$doctor_ok_output" "ok - Profile is supported: VPS Shell Profile" "Terrapod doctor validates supported Ubuntu profile"
assert_contains "$doctor_ok_output" "ok - chezmoi is available" "Terrapod doctor validates chezmoi availability"
assert_contains "$doctor_ok_output" "ok - nvim is available" "Terrapod doctor validates plain Neovim as a Core Shell Stack tool"
assert_contains "$doctor_ok_output" "ok - zellij is available" "Terrapod doctor validates Zellij as a Core Shell Stack tool"
assert_contains "$doctor_ok_output" "ok - apt is available" "Terrapod doctor validates Ubuntu Bootstrap Package Manager availability"
assert_contains "$doctor_ok_output" "ok - Optional Editor Stack is disabled" "Terrapod doctor treats disabled Optional Editor Stack as valid"
assert_contains "$doctor_ok_output" "ok - Optional AI Tool Stack is disabled" "Terrapod doctor treats disabled Optional AI Tool Stack as valid"
assert_not_contains "$doctor_ok_output" "brew is missing" "disabled Ubuntu Optional AI Tool Stack doctor does not require Homebrew"
assert_contains "$doctor_ok_output" "ok - Optional Development Workspace is disabled" "Terrapod doctor treats disabled Optional Development Workspace as valid"
assert_contains "$doctor_ok_output" "Guidance: none" "Terrapod doctor prints no guidance when checks pass"
assert_no_ansi_escape "$doctor_ok_output" "captured Terrapod doctor is plain without ANSI escapes"
assert_no_routine_emoji "$doctor_ok_output" "captured Terrapod doctor has no routine emoji"

if ! doctor_shadow_output="$(
  TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_shadow_config" PATH="$status_shadow_legacy:$status_shadow_path" \
    /bin/sh "$terrapod" doctor
)"; then
  fail "Terrapod doctor keeps legacy AI CLI shadowing as a non-fatal warning"
fi

assert_contains "$doctor_shadow_output" "ok - brew is available" "enabled Ubuntu Optional AI Tool Stack doctor requires Homebrew"
assert_contains "$doctor_shadow_output" "warn - Optional AI Tool Stack has non-Homebrew commands shadowing managed casks: claude" "Terrapod doctor reports legacy Claude shadowing the Homebrew cask"
assert_contains "$doctor_shadow_output" "Remove each legacy command with its original installer method, open a new shell, and rerun tpod apply." "Terrapod doctor gives non-destructive legacy cleanup guidance"

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_shadow_config" PATH="$status_broken_prefix_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-broken-prefix.out" 2>"$tmp_dir/doctor-broken-prefix.err"; then
  fail "Terrapod doctor fails when the enabled Optional AI Tool Stack cannot resolve the Homebrew prefix"
fi

doctor_broken_prefix_output="$(cat "$tmp_dir/doctor-broken-prefix.out")"
assert_contains "$doctor_broken_prefix_output" "warn - Optional AI Tool Stack cannot resolve the Homebrew prefix" "Terrapod doctor reports an unusable Homebrew prefix"
assert_contains "$doctor_broken_prefix_output" "Repair Homebrew until 'brew --prefix' succeeds, open a new shell, and rerun tpod apply." "Terrapod doctor gives Homebrew prefix recovery guidance"

tty_doctor_output="$(
  run_command_in_pty xterm unset env TERRAPOD_PROFILE= TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_ok_path" /bin/sh "$terrapod" doctor
)"
assert_has_ansi_escape "$tty_doctor_output" "TTY Terrapod doctor uses ANSI visual treatment"

doctor_missing_key_path="$(status_doctor_path doctor-missing-key git zsh mise nvim zellij apt)"
write_stub "$doctor_missing_key_path/uname" 'printf "%s\n" "Linux"'

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_missing_key_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-missing-key.out" 2>"$tmp_dir/doctor-missing-key.err"; then
  fail "Terrapod doctor fails when a required key tool is missing"
fi

doctor_missing_key_output="$(cat "$tmp_dir/doctor-missing-key.out")"

assert_contains "$doctor_missing_key_output" "warn - chezmoi is missing" "Terrapod doctor warns when required chezmoi is missing"
assert_contains "$doctor_missing_key_output" "Install or apply the configured Core Shell Stack so 'chezmoi' is available on PATH." "Terrapod doctor gives actionable guidance for missing key tools"

if TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_incomplete_vps_config" PATH="$doctor_ok_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-incomplete-config.out" 2>"$tmp_dir/doctor-incomplete-config.err"; then
  fail "Terrapod doctor fails when managed setup config is incomplete"
fi

doctor_incomplete_config_output="$(cat "$tmp_dir/doctor-incomplete-config.out")"

assert_contains "$doctor_incomplete_config_output" "warn - managed setup config is incomplete" "Terrapod doctor marks incomplete managed setup config as a failed check"
assert_contains "$doctor_incomplete_config_output" "enableMacosAppGroupDevelopmentApps" "Terrapod doctor reports missing managed setup keys even on VPS"
assert_contains "$doctor_incomplete_config_output" "Run 'tpod setup' or 'tpod configure <minimal|development>' to complete the managed setup config." "Terrapod doctor guides incomplete config recovery with tpod setup or configure"

if TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_unsafe_multiline_config" PATH="$doctor_ok_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-unsafe-multiline.out" 2>"$tmp_dir/doctor-unsafe-multiline.err"; then
  fail "Terrapod doctor fails when managed setup config contains unsupported multiline strings"
fi

doctor_unsafe_multiline_output="$(cat "$tmp_dir/doctor-unsafe-multiline.out")"

assert_contains "$doctor_unsafe_multiline_output" "warn - unsupported multiline string" "Terrapod doctor warns for unsupported multiline config"
assert_not_contains "$doctor_unsafe_multiline_output" "Managed setup config is complete" "Terrapod doctor does not trust managed keys inside multiline strings"

doctor_unreadable_config="$tmp_dir/doctor-unreadable.toml"
cat >"$doctor_unreadable_config" <<'TOML'
[data]
profile = "vps-shell"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
TOML
chmod 000 "$doctor_unreadable_config"

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_unreadable_config" PATH="$doctor_ok_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-unreadable.out" 2>"$tmp_dir/doctor-unreadable.err"; then
  chmod 644 "$doctor_unreadable_config"
  fail "Terrapod doctor fails when the managed setup config is unreadable"
fi
chmod 644 "$doctor_unreadable_config"

doctor_unreadable_output="$(cat "$tmp_dir/doctor-unreadable.out")"

assert_contains "$doctor_unreadable_output" "warn - config path is not a readable regular file: $doctor_unreadable_config" "Terrapod doctor reports unreadable config as a failed check"
assert_contains "$doctor_unreadable_output" "Fix the config path or permissions so it is a readable regular file." "Terrapod doctor gives path or permission guidance for unreadable config"
assert_not_contains "$doctor_unreadable_output" "managed setup config is incomplete" "Terrapod doctor does not misreport unreadable config as incomplete managed setup keys"

doctor_missing_core_path="$(status_doctor_path doctor-missing-core chezmoi git zsh mise apt)"
write_stub "$doctor_missing_core_path/uname" 'printf "%s\n" "Linux"'

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_missing_core_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-missing-core.out" 2>"$tmp_dir/doctor-missing-core.err"; then
  fail "Terrapod doctor fails when Core Shell Stack Neovim or Zellij is missing"
fi

doctor_missing_core_output="$(cat "$tmp_dir/doctor-missing-core.out")"

assert_contains "$doctor_missing_core_output" "warn - nvim is missing" "Terrapod doctor warns when plain Neovim is missing even if Optional Editor Stack is disabled"
assert_contains "$doctor_missing_core_output" "warn - zellij is missing" "Terrapod doctor warns when Zellij is missing even if Optional Development Workspace is disabled"
assert_contains "$doctor_missing_core_output" "ok - Optional Editor Stack is disabled" "Terrapod doctor keeps disabled Optional Editor Stack separate from missing core Neovim"
assert_contains "$doctor_missing_core_output" "ok - Optional Development Workspace is disabled" "Terrapod doctor keeps disabled Optional Development Workspace separate from missing core Zellij"

doctor_missing_config="$tmp_dir/doctor-missing.toml"
cat >"$doctor_missing_config" <<'TOML'
[data]
enableEditorStack = true
enableAiCliTools = true
enableDevelopmentWorkspace = true
TOML

doctor_missing_path="$(status_doctor_path doctor-missing chezmoi git zsh mise nvim zellij apt)"
write_stub "$doctor_missing_path/uname" 'printf "%s\n" "Linux"'

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_missing_config" PATH="$doctor_missing_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-missing.out" 2>"$tmp_dir/doctor-missing.err"; then
  fail "Terrapod doctor fails when enabled optional stack tools are missing"
fi

doctor_missing_output="$(cat "$tmp_dir/doctor-missing.out")"

assert_contains "$doctor_missing_output" "ok - Optional Editor Stack is enabled (rich Neovim configuration)" "Terrapod doctor reports enabled Optional Editor Stack as rich config state"
assert_contains "$doctor_missing_output" "warn - Optional AI Tool Stack is enabled but missing tools: agy, claude, codex" "Terrapod doctor warns about missing enabled AI tools"
assert_contains "$doctor_missing_output" "ok - Optional Development Workspace is enabled (development Zellij layouts)" "Terrapod doctor reports enabled Optional Development Workspace as layout state"
assert_contains "$doctor_missing_output" "Run tpod chezmoi -- apply after enabling Optional AI Tool Stack, or install/apply the configured tools before relying on them." "Terrapod doctor gives actionable missing optional-stack guidance with tpod"
assert_not_contains "$doctor_missing_output" "Run terrapod chezmoi -- apply" "Terrapod doctor avoids old optional-stack guidance command"

doctor_unsupported_os_release="$tmp_dir/doctor-unsupported-os-release"
write_os_release "$doctor_unsupported_os_release" debian 12 "Debian GNU/Linux 12"

doctor_unsupported_path="$(status_doctor_path doctor-unsupported chezmoi git zsh mise nvim zellij apt)"
write_stub "$doctor_unsupported_path/uname" 'printf "%s\n" "Linux"'

if TERRAPOD_OS_RELEASE_FILE="$doctor_unsupported_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_unsupported_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-unsupported.out" 2>"$tmp_dir/doctor-unsupported.err"; then
  fail "Terrapod doctor fails on unsupported Linux"
fi

doctor_unsupported_output="$(cat "$tmp_dir/doctor-unsupported.out")"
assert_contains "$doctor_unsupported_output" "warn - unsupported Linux release: Debian GNU/Linux 12. Terrapod supports Ubuntu 24.04 for the VPS Shell Profile." "Terrapod doctor explains unsupported Linux"

doctor_marker_state="$tmp_dir/doctor-marker-state"
HOME="$tmp_dir/doctor-marker-home" XDG_STATE_HOME="$doctor_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write ubuntu-bootstrap "Ubuntu bootstrap needs attention" "Review APT output, fix package repository access, then rerun tpod apply."' \
  sh "$install_warnings_lib"
HOME="$tmp_dir/doctor-marker-home" XDG_STATE_HOME="$doctor_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-desktop-apps "Homebrew desktop app install needs attention" "Review Homebrew cask output for failed casks: ghostty, raycast; App Groups: terminal-apps, launcher, fix app installation access, then rerun tpod apply."' \
  sh "$install_warnings_lib"
doctor_marker_file="$doctor_marker_state/terrapod/install-warnings/ubuntu-bootstrap"
doctor_marker_before="$(cat "$doctor_marker_file")"

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" XDG_STATE_HOME="$doctor_marker_state" PATH="$doctor_ok_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-marker.out" 2>"$tmp_dir/doctor-marker.err"; then
  fail "Terrapod doctor fails when unresolved install warning markers remain"
fi

doctor_marker_output="$(cat "$tmp_dir/doctor-marker.out")"
doctor_marker_after="$(cat "$doctor_marker_file")"

assert_contains "$doctor_marker_output" "warn - install warning marker remains: ubuntu-bootstrap" "Terrapod doctor reports install warning marker categories"
assert_contains "$doctor_marker_output" "warn - install warning marker remains: homebrew-desktop-apps" "Terrapod doctor reports homebrew desktop app warning marker categories"
assert_contains "$doctor_marker_output" "Summary: Ubuntu bootstrap needs attention" "Terrapod doctor reports install warning marker summary"
assert_contains "$doctor_marker_output" "Summary: Homebrew desktop app install needs attention" "Terrapod doctor reports homebrew desktop app warning marker summary"
assert_contains "$doctor_marker_output" "Guidance: Review APT output, fix package repository access, then rerun tpod apply." "Terrapod doctor reports install warning marker guidance"
assert_contains "$doctor_marker_output" "Guidance: Review Homebrew cask output for failed casks: ghostty, raycast; App Groups: terminal-apps, launcher, fix app installation access, then rerun tpod apply." "Terrapod doctor reports homebrew desktop app cask and App Group guidance"
assert_contains "$doctor_marker_output" "Updated: " "Terrapod doctor reports install warning marker updated_at"
if [ "$doctor_marker_after" != "$doctor_marker_before" ]; then
  fail "Terrapod doctor is read-only for install warning markers"
fi
pass "Terrapod doctor is read-only for install warning markers"

rm -f "$fake_warning_calls"
if ! doctor_missing_lib_output="$(
  FAKE_INSTALL_WARNING_CALLS="$fake_warning_calls" TERRAPOD_INSTALL_WARNINGS_LIB="$tmp_dir/missing-install-warnings-lib" TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$fake_warning_bin:$doctor_ok_path" \
    /bin/sh "$terrapod" doctor
)"; then
  fail "Terrapod doctor succeeds when the install warning library is unavailable and no other checks fail"
fi
assert_contains "$doctor_missing_lib_output" "ok - No install warning marker reader is installed" "Terrapod doctor reports unavailable marker reader without using PATH fake helpers"
assert_not_contains "$doctor_missing_lib_output" "spoofed" "Terrapod doctor does not trust PATH fake install warning helper output"
if [ -e "$fake_warning_calls" ]; then
  fail "Terrapod doctor does not execute PATH fake install warning helpers"
fi
pass "Terrapod doctor does not execute PATH fake install warning helpers"

doctor_broad_upgrade_calls="$tmp_dir/doctor-broad-upgrade.calls"
rm -f "$doctor_broad_upgrade_calls"
doctor_broad_upgrade_path="$(status_doctor_path doctor-broad-upgrade chezmoi git zsh nvim agy claude codex zellij)"
for command_name in brew apt sudo mise npm; do
  write_failing_command_stub "$doctor_broad_upgrade_path/$command_name" "$doctor_broad_upgrade_calls"
done
write_stub "$doctor_broad_upgrade_path/uname" 'printf "%s\n" "Darwin"'

if TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_broad_upgrade_path" \
  /bin/sh "$terrapod" status >/dev/null 2>/dev/null; then
  :
fi

if TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_broad_upgrade_path" \
  /bin/sh "$terrapod" doctor >/dev/null 2>/dev/null; then
  :
fi

if [ -e "$doctor_broad_upgrade_calls" ]; then
  printf '%s\n' "unexpected broad upgrade command calls from status/doctor:" >&2
  sed 's/^/  /' "$doctor_broad_upgrade_calls" >&2
  fail "Terrapod status and doctor do not call brew, apt, sudo, mise, or npm upgrade flows"
fi

pass "Terrapod status and doctor do not call brew, apt, sudo, mise, or npm upgrade flows"

export CHEZMOI_CALL_FILE="$tmp_dir/chezmoi-update.args"
export CHEZMOI_INVOKED_FILE="$tmp_dir/chezmoi-update.invoked"
export BROAD_UPGRADE_CALL_FILE="$tmp_dir/broad-upgrade.calls"

write_stub "$tmp_dir/bin/chezmoi" \
  'printf "%s\n" invoked >"$CHEZMOI_INVOKED_FILE"' \
  ': >"$CHEZMOI_CALL_FILE"' \
  'for arg do' \
  '  printf "%s\n" "$arg" >>"$CHEZMOI_CALL_FILE"' \
  'done' \
  'exit 0'

write_stub "$tmp_dir/bin/brew" \
  'printf "%s\n" "brew $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/apt" \
  'printf "%s\n" "apt $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/sudo" \
  'printf "%s\n" "sudo $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/mise" \
  'printf "%s\n" "mise $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/npm" \
  'printf "%s\n" "npm $*" >>"$BROAD_UPGRADE_CALL_FILE"' \
  'exit 90'

write_stub "$tmp_dir/bin/uname" \
  'printf "%s\n" "Darwin"'

update_home="$tmp_dir/update-home"
update_xdg="$tmp_dir/update-xdg"
update_config="$update_xdg/chezmoi/chezmoi.toml"
mkdir -p "$update_home" "$(dirname "$update_config")"

cat >"$update_config" <<'TOML'
[data]
profile = "macos-terminal"
enableEditorStack = true
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
TOML

if ! HOME="$update_home" XDG_CONFIG_HOME="$update_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" update >"$tmp_dir/update.out" 2>"$tmp_dir/update.err"; then
  printf '%s\n' "update stdout:" >&2
  sed 's/^/  /' "$tmp_dir/update.out" >&2
  printf '%s\n' "update stderr:" >&2
  sed 's/^/  /' "$tmp_dir/update.err" >&2
  fail "Terrapod update runs successfully"
fi

update_output="$(cat "$tmp_dir/update.out")"

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod update delegates source update semantics to chezmoi update" \
  update --exclude scripts

assert_contains \
  "$update_output" \
  "Terrapod update" \
  "Terrapod update prints command context"

assert_contains \
  "$update_output" \
  "Profile: macOS Terminal Profile" \
  "Terrapod update prints profile context"

assert_contains \
  "$update_output" \
  "Config: $update_config (present)" \
  "Terrapod update prints managed config context"

assert_contains \
  "$update_output" \
  "Delegating source update to: chezmoi update --exclude scripts" \
  "Terrapod update explains the delegated command"

assert_no_ansi_escape "$update_output" "captured Terrapod update is plain without ANSI escapes"
assert_no_routine_emoji "$update_output" "captured Terrapod update has no routine emoji"

tty_update_output="$(
  run_command_in_pty xterm unset env HOME="$update_home" XDG_CONFIG_HOME="$update_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" sh "$terrapod" update
)"
assert_has_ansi_escape "$tty_update_output" "TTY Terrapod update uses ANSI visual treatment"
assert_not_contains_colored_phrase "$tty_update_output" "Delegating source update to: chezmoi update --exclude scripts" "TTY Terrapod update keeps delegated command text neutral"

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod update does not call brew, apt, sudo, mise, or npm upgrade flows"
fi

pass "Terrapod update does not call brew, apt, sudo, mise, or npm upgrade flows"

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if TERRAPOD_PROFILE=vps-shell TERRAPOD_CHEZMOI_CONFIG="$status_incomplete_vps_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" update >"$tmp_dir/update-incomplete.out" 2>"$tmp_dir/update-incomplete.err"; then
  fail "Terrapod update fails before chezmoi update when managed setup config is incomplete"
fi

update_incomplete_output="$(cat "$tmp_dir/update-incomplete.out")"
update_incomplete_error="$(cat "$tmp_dir/update-incomplete.err")"

assert_contains "$update_incomplete_output" "Config: $status_incomplete_vps_config (present; incomplete managed setup config)" "Terrapod update reports incomplete managed setup config in the Config section"
assert_not_contains "$update_incomplete_output" "Delegating source update to:" "Terrapod update does not announce delegation when managed setup config is incomplete"
assert_contains "$update_incomplete_error" "managed setup config is incomplete" "Terrapod update explains incomplete managed setup config"
assert_contains "$update_incomplete_error" "enableMacosAppGroupDevelopmentApps" "Terrapod update reports missing managed setup keys even on VPS"
assert_contains "$update_incomplete_error" "Run 'tpod setup' or 'tpod configure <minimal|development>' to complete the managed setup config." "Terrapod update guides incomplete config recovery with tpod setup or configure"

if [ -e "$CHEZMOI_INVOKED_FILE" ]; then
  fail "Terrapod update rejects incomplete config before calling chezmoi update"
fi

if [ -e "$CHEZMOI_CALL_FILE" ]; then
  fail "Terrapod update rejects incomplete config before recording chezmoi update arguments"
fi

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if TERRAPOD_PROFILE=vps-shell TERRAPOD_CHEZMOI_CONFIG="$status_unsafe_multiline_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" update >"$tmp_dir/update-unsafe-multiline.out" 2>"$tmp_dir/update-unsafe-multiline.err"; then
  fail "Terrapod update fails before chezmoi update when managed setup config contains unsupported multiline strings"
fi

update_unsafe_multiline_output="$(cat "$tmp_dir/update-unsafe-multiline.out")"
update_unsafe_multiline_error="$(cat "$tmp_dir/update-unsafe-multiline.err")"

assert_contains "$update_unsafe_multiline_output" "Config: $status_unsafe_multiline_config (unsupported managed setup config)" "Terrapod update reports unsupported multiline config in the Config section"
assert_not_contains "$update_unsafe_multiline_output" "Delegating source update to:" "Terrapod update does not announce delegation when managed setup config contains unsupported multiline strings"
assert_contains "$update_unsafe_multiline_error" "unsupported multiline string" "Terrapod update explains unsupported multiline config"

if [ -e "$CHEZMOI_INVOKED_FILE" ]; then
  fail "Terrapod update rejects unsupported multiline config before calling chezmoi update"
fi

if [ -e "$CHEZMOI_CALL_FILE" ]; then
  fail "Terrapod update rejects unsupported multiline config before recording chezmoi update arguments"
fi

override_config="$tmp_dir/override-chezmoi.toml"

cat >"$override_config" <<'TOML'
[data]
profile = "macos-terminal"
enableEditorStack = false
enableAiCliTools = true
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
TOML

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if ! HOME="$update_home" XDG_CONFIG_HOME="$update_xdg" TERRAPOD_CHEZMOI_CONFIG="$override_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" update >"$tmp_dir/update-override.out" 2>"$tmp_dir/update-override.err"; then
  printf '%s\n' "override update stdout:" >&2
  sed 's/^/  /' "$tmp_dir/update-override.out" >&2
  printf '%s\n' "override update stderr:" >&2
  sed 's/^/  /' "$tmp_dir/update-override.err" >&2
  fail "Terrapod update runs successfully with an explicit config override"
fi

override_update_output="$(cat "$tmp_dir/update-override.out")"

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod update passes explicit config overrides to chezmoi update" \
  --config "$override_config" update --exclude scripts

assert_contains \
  "$override_update_output" \
  "Config: $override_config (present)" \
  "Terrapod update prints explicit config override context"

assert_contains \
  "$override_update_output" \
  "Delegating source update to: chezmoi --config $override_config update --exclude scripts" \
  "Terrapod update explains delegated explicit config command"

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls with explicit config override:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod update with explicit config override does not call broad upgrade flows"
fi

pass "Terrapod update with explicit config override does not call broad upgrade flows"

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if HOME="$update_home" XDG_CONFIG_HOME="$update_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" update --aggressive >"$tmp_dir/update-extra.out" 2>"$tmp_dir/update-extra.err"; then
  fail "Terrapod update rejects extra arguments"
fi

update_extra_error="$(cat "$tmp_dir/update-extra.err")"

assert_contains \
  "$update_extra_error" \
  "terrapod: update accepts no arguments" \
  "Terrapod update explains rejected extra arguments"

if [ -e "$CHEZMOI_INVOKED_FILE" ]; then
  fail "Terrapod update rejects extra arguments before calling chezmoi"
fi

pass "Terrapod update rejects extra arguments before calling chezmoi"

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls after rejected extra arguments:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod update rejects extra arguments before broad upgrade flows"
fi

pass "Terrapod update rejects extra arguments before broad upgrade flows"

export CHEZMOI_CALL_FILE="$tmp_dir/chezmoi-diff.args"
export CHEZMOI_INVOKED_FILE="$tmp_dir/chezmoi-diff.invoked"

write_stub "$tmp_dir/bin/chezmoi" \
  'printf "%s\n" invoked >"$CHEZMOI_INVOKED_FILE"' \
  ': >"$CHEZMOI_CALL_FILE"' \
  'for arg do' \
  '  printf "%s\n" "$arg" >>"$CHEZMOI_CALL_FILE"' \
  'done' \
  'printf "%s\n" "stub diff output"' \
  'exit 0'

write_stub "$tmp_dir/bin/uname" \
  'printf "%s\n" "Darwin"'

diff_home="$tmp_dir/diff-home"
diff_xdg="$tmp_dir/diff-xdg"
diff_config="$diff_xdg/chezmoi/chezmoi.toml"
mkdir -p "$diff_home" "$(dirname "$diff_config")"

cat >"$diff_config" <<'TOML'
[data]
profile = "macos-terminal"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = true
enableMacosAppGroupTerminalApps = true
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = true
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = true
TOML

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" diff >"$tmp_dir/diff.out" 2>"$tmp_dir/diff.err"; then
  printf '%s\n' "diff stdout:" >&2
  sed 's/^/  /' "$tmp_dir/diff.out" >&2
  printf '%s\n' "diff stderr:" >&2
  sed 's/^/  /' "$tmp_dir/diff.err" >&2
  fail "Terrapod diff runs successfully"
fi

diff_output="$(cat "$tmp_dir/diff.out")"

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod diff delegates declared-state diff behavior to chezmoi diff" \
  diff

assert_contains \
  "$diff_output" \
  "Terrapod diff" \
  "Terrapod diff prints command context"

assert_contains \
  "$diff_output" \
  "Profile: macOS Terminal Profile" \
  "Terrapod diff prints active profile context"

assert_contains \
  "$diff_output" \
  "Config: $diff_config (present)" \
  "Terrapod diff prints config context"

assert_contains \
  "$diff_output" \
  "Optional stacks:" \
  "Terrapod diff prints optional stack section header"

assert_contains \
  "$diff_output" \
  "Optional Editor Stack         : enabled" \
  "Terrapod diff prints effective enabled Optional Editor Stack state"

assert_contains \
  "$diff_output" \
  "Optional AI Tool Stack        : enabled" \
  "Terrapod diff prints effective enabled Optional AI Tool Stack state"

assert_contains \
  "$diff_output" \
  "Optional Development Workspace: enabled" \
  "Terrapod diff prints enabled Optional Development Workspace state"

assert_contains \
  "$diff_output" \
  "macOS App Groups:" \
  "Terrapod diff prints macOS App Groups section header"

assert_contains \
  "$diff_output" \
  "terminal-apps                 : enabled" \
  "Terrapod diff prints enabled terminal-apps macOS App Group state"

assert_contains \
  "$diff_output" \
  "automation                    : disabled" \
  "Terrapod diff prints disabled automation macOS App Group state"

assert_contains \
  "$diff_output" \
  "launcher                      : enabled" \
  "Terrapod diff prints enabled launcher macOS App Group state"

assert_contains \
  "$diff_output" \
  "monitoring                    : disabled" \
  "Terrapod diff prints disabled monitoring macOS App Group state"

assert_contains \
  "$diff_output" \
  "development-apps              : enabled" \
  "Terrapod diff prints enabled development-apps macOS App Group state"

assert_contains \
  "$diff_output" \
  "Delegating declared-state diff to: chezmoi diff" \
  "Terrapod diff explains the delegated command"

assert_contains \
  "$diff_output" \
  "stub diff output" \
  "Terrapod diff includes delegated chezmoi diff output"

assert_no_ansi_escape "$diff_output" "captured Terrapod diff is plain without ANSI escapes"
assert_no_routine_emoji "$diff_output" "captured Terrapod diff has no routine emoji"

tty_diff_output="$(
  run_command_in_pty xterm unset env HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" sh "$terrapod" diff
)"
assert_has_ansi_escape "$tty_diff_output" "TTY Terrapod diff uses ANSI visual treatment"

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls during diff:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod diff does not call brew, apt, sudo, mise, or npm upgrade flows"
fi

pass "Terrapod diff does not call brew, apt, sudo, mise, or npm upgrade flows"

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$diff_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" diff --verbose >"$tmp_dir/diff-extra.out" 2>"$tmp_dir/diff-extra.err"; then
  fail "Terrapod diff rejects extra arguments"
fi

diff_extra_error="$(cat "$tmp_dir/diff-extra.err")"

assert_contains \
  "$diff_extra_error" \
  "terrapod: diff accepts no arguments" \
  "Terrapod diff explains rejected extra arguments"

if [ -e "$CHEZMOI_INVOKED_FILE" ]; then
  fail "Terrapod diff rejects extra arguments before calling chezmoi"
fi

pass "Terrapod diff rejects extra arguments before calling chezmoi"

rm -f "$CHEZMOI_CALL_FILE" "$CHEZMOI_INVOKED_FILE"

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$diff_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" diff >"$tmp_dir/diff-override.out" 2>"$tmp_dir/diff-override.err"; then
  printf '%s\n' "override diff stdout:" >&2
  sed 's/^/  /' "$tmp_dir/diff-override.out" >&2
  printf '%s\n' "override diff stderr:" >&2
  sed 's/^/  /' "$tmp_dir/diff-override.err" >&2
  fail "Terrapod diff runs successfully with an explicit config override"
fi

diff_override_output="$(cat "$tmp_dir/diff-override.out")"

assert_call_args \
  "$CHEZMOI_CALL_FILE" \
  "Terrapod diff passes explicit config overrides to chezmoi diff" \
  --config "$diff_config" diff

assert_contains \
  "$diff_override_output" \
  "Delegating declared-state diff to: chezmoi --config $diff_config diff" \
  "Terrapod diff explains delegated explicit config command"

export CHEZMOI_APPLY_ARGS_FILE="$tmp_dir/chezmoi-apply.args"
export CHEZMOI_APPLY_INVOKED_FILE="$tmp_dir/chezmoi-apply.invoked"
export CHEZMOI_MANAGED_ARGS_FILE="$tmp_dir/chezmoi-managed.args"

write_stub "$tmp_dir/bin/chezmoi" \
  'command_name=' \
  'for arg do' \
  '  case "$arg" in' \
  '    apply|managed)' \
  '      command_name="$arg"' \
  '      ;;' \
  '  esac' \
  'done' \
  'case "$command_name" in' \
  '  apply)' \
  '    printf "%s\n" invoked >"$CHEZMOI_APPLY_INVOKED_FILE"' \
  '    : >"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" "stub apply output"' \
  '    exit 0' \
  '    ;;' \
  '  managed)' \
  '    : >"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" ".local/bin/terrapod"' \
  '    printf "%s\n" ".local/bin/tpod"' \
  '    exit 0' \
  '    ;;' \
  'esac' \
  'printf "%s\n" "unexpected chezmoi command: $*" >&2' \
  'exit 91'

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"
rm -f "$BROAD_UPGRADE_CALL_FILE"

apply_incomplete_path="$(status_doctor_path apply-incomplete chezmoi git zsh mise nvim zellij apt)"
write_stub "$apply_incomplete_path/uname" 'printf "%s\n" "Linux"'
write_stub "$apply_incomplete_path/chezmoi" \
  'command_name=' \
  'for arg do' \
  '  case "$arg" in' \
  '    apply|managed)' \
  '      command_name="$arg"' \
  '      ;;' \
  '  esac' \
  'done' \
  'case "$command_name" in' \
  '  apply)' \
  '    printf "%s\n" invoked >"$CHEZMOI_APPLY_INVOKED_FILE"' \
  '    exit 0' \
  '    ;;' \
  '  managed)' \
  '    : >"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    exit 0' \
  '    ;;' \
  'esac' \
  'exit 0'

if TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_incomplete_vps_config" PATH="$apply_incomplete_path" \
  /bin/sh "$terrapod" apply >"$tmp_dir/apply-incomplete.out" 2>"$tmp_dir/apply-incomplete.err"; then
  fail "Terrapod apply fails before chezmoi apply when managed setup config is incomplete"
fi

apply_incomplete_error="$(cat "$tmp_dir/apply-incomplete.err")"

assert_contains "$apply_incomplete_error" "managed setup config is incomplete" "Terrapod apply explains incomplete managed setup config"
assert_contains "$apply_incomplete_error" "enableMacosAppGroupDevelopmentApps" "Terrapod apply reports missing managed setup keys even on VPS"
assert_contains "$apply_incomplete_error" "Run 'tpod setup' or 'tpod configure <minimal|development>' to complete the managed setup config." "Terrapod apply guides incomplete config recovery with tpod setup or configure"

if [ -e "$CHEZMOI_APPLY_INVOKED_FILE" ]; then
  fail "Terrapod apply rejects incomplete config before calling chezmoi apply"
fi

if [ -e "$CHEZMOI_MANAGED_ARGS_FILE" ]; then
  fail "Terrapod apply rejects incomplete config before post-apply validation"
fi

apply_missing_home="$tmp_dir/apply-missing-home"
apply_missing_xdg="$tmp_dir/apply-missing-xdg"
apply_missing_config="$apply_missing_xdg/chezmoi/chezmoi.toml"
mkdir -p "$apply_missing_home" "$apply_missing_xdg"
rm -f "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if HOME="$apply_missing_home" XDG_CONFIG_HOME="$apply_missing_xdg" PATH="$apply_incomplete_path" \
  /bin/sh "$terrapod" apply >"$tmp_dir/apply-missing.out" 2>"$tmp_dir/apply-missing.err"; then
  fail "Terrapod apply fails when the managed setup config is missing"
fi

apply_missing_output="$(cat "$tmp_dir/apply-missing.out")"
apply_missing_error="$(cat "$tmp_dir/apply-missing.err")"

assert_contains "$apply_missing_output" "Config:" "Terrapod apply prints a Config section for missing managed setup config"
assert_contains "$apply_missing_output" "(missing; incomplete managed setup config)" "Terrapod apply reports missing managed setup config in the Config section"
assert_contains "$apply_missing_output" "Preflight: config file is missing" "Terrapod apply reports a missing config file before completeness guidance"
assert_not_contains "$apply_missing_output" "chezmoi defaults apply" "Terrapod apply does not say chezmoi defaults apply for missing config"
assert_not_contains "$apply_missing_output" "chezmoi defaults will apply" "Terrapod apply does not say chezmoi defaults will apply for missing config"
assert_contains "$apply_missing_error" "managed setup config is incomplete" "Terrapod apply explains missing managed setup config"
assert_contains "$apply_missing_error" "Run 'tpod setup' or 'tpod configure <minimal|development>' to complete the managed setup config." "Terrapod apply guides missing config recovery with tpod setup or configure"

if [ -e "$CHEZMOI_APPLY_INVOKED_FILE" ]; then
  fail "Terrapod apply rejects missing config before calling chezmoi apply"
fi

if [ -e "$CHEZMOI_MANAGED_ARGS_FILE" ]; then
  fail "Terrapod apply rejects missing config before post-apply validation"
fi

apply_inline_config="$tmp_dir/apply-inline-table.toml"
cat >"$apply_inline_config" <<'TOML'
data = { profile = "vps-shell", enableEditorStack = false, enableAiCliTools = false, enableDevelopmentWorkspace = false, enableMacosAppGroupTerminalApps = false, enableMacosAppGroupAutomation = false, enableMacosAppGroupLauncher = false, enableMacosAppGroupMonitoring = false, enableMacosAppGroupDevelopmentApps = false }
TOML
rm -f "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$apply_inline_config" PATH="$apply_incomplete_path" \
  /bin/sh "$terrapod" apply >"$tmp_dir/apply-inline-table.out" 2>"$tmp_dir/apply-inline-table.err"; then
  fail "Terrapod apply rejects inline data table config before chezmoi apply"
fi

apply_inline_error="$(cat "$tmp_dir/apply-inline-table.err")"

assert_contains "$apply_inline_error" "unsupported inline data table" "Terrapod apply reports unsupported inline data table config"
assert_not_contains "$apply_inline_error" "managed setup config is incomplete" "Terrapod apply does not misreport inline data table config as missing managed setup keys"

if [ -e "$CHEZMOI_APPLY_INVOKED_FILE" ]; then
  fail "Terrapod apply rejects inline data table config before calling chezmoi apply"
fi

if [ -e "$CHEZMOI_MANAGED_ARGS_FILE" ]; then
  fail "Terrapod apply rejects inline data table config before post-apply validation"
fi

rm -f "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_unsafe_multiline_config" PATH="$apply_incomplete_path" \
  /bin/sh "$terrapod" apply >"$tmp_dir/apply-unsafe-multiline.out" 2>"$tmp_dir/apply-unsafe-multiline.err"; then
  fail "Terrapod apply rejects unsupported multiline config before chezmoi apply"
fi

apply_unsafe_multiline_output="$(cat "$tmp_dir/apply-unsafe-multiline.out")"
apply_unsafe_multiline_error="$(cat "$tmp_dir/apply-unsafe-multiline.err")"

assert_contains "$apply_unsafe_multiline_output" "Config: $status_unsafe_multiline_config (unsupported managed setup config)" "Terrapod apply reports unsupported multiline config in the Config section"
assert_contains "$apply_unsafe_multiline_error" "unsupported multiline string" "Terrapod apply reports unsupported multiline config"
assert_not_contains "$apply_unsafe_multiline_error" "managed setup config is incomplete" "Terrapod apply does not misreport unsupported multiline config as missing managed setup keys"

if [ -e "$CHEZMOI_APPLY_INVOKED_FILE" ]; then
  fail "Terrapod apply rejects unsupported multiline config before calling chezmoi apply"
fi

if [ -e "$CHEZMOI_MANAGED_ARGS_FILE" ]; then
  fail "Terrapod apply rejects unsupported multiline config before post-apply validation"
fi

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"
rm -f "$BROAD_UPGRADE_CALL_FILE"

apply_marker_state="$tmp_dir/apply-marker-state"
HOME="$tmp_dir/apply-marker-home" XDG_STATE_HOME="$apply_marker_state" sh -c \
  '. "$1"; terrapod_install_warning_write mise-tools "mise tool install needs attention" "Run tpod apply after fixing mise tool installation output."' \
  sh "$install_warnings_lib"

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" XDG_STATE_HOME="$apply_marker_state" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply.out" 2>"$tmp_dir/apply.err"; then
  printf '%s\n' "apply stdout:" >&2
  sed 's/^/  /' "$tmp_dir/apply.out" >&2
  printf '%s\n' "apply stderr:" >&2
  sed 's/^/  /' "$tmp_dir/apply.err" >&2
  fail "Terrapod apply runs successfully"
fi

apply_output="$(cat "$tmp_dir/apply.out")"

assert_call_args \
  "$CHEZMOI_APPLY_ARGS_FILE" \
  "Terrapod apply delegates declared-state apply behavior to chezmoi apply" \
  apply

assert_call_args \
  "$CHEZMOI_MANAGED_ARGS_FILE" \
  "Terrapod apply validates managed targets with chezmoi managed" \
  managed

assert_contains \
  "$apply_output" \
  "Terrapod apply" \
  "Terrapod apply prints command context"

assert_contains \
  "$apply_output" \
  "Profile: macOS Terminal Profile" \
  "Terrapod apply prints active profile context"

assert_contains \
  "$apply_output" \
  "Config: $diff_config (present)" \
  "Terrapod apply prints config context"

assert_contains \
  "$apply_output" \
  "Optional stacks:" \
  "Terrapod apply prints optional stack section header"

assert_contains \
  "$apply_output" \
  "Optional Editor Stack         : enabled" \
  "Terrapod apply prints effective enabled Optional Editor Stack state"

assert_contains \
  "$apply_output" \
  "Optional AI Tool Stack        : enabled" \
  "Terrapod apply prints effective enabled Optional AI Tool Stack state"

assert_contains \
  "$apply_output" \
  "Optional Development Workspace: enabled" \
  "Terrapod apply prints enabled Optional Development Workspace state"

assert_contains \
  "$apply_output" \
  "terminal-apps                 : enabled" \
  "Terrapod apply prints enabled terminal-apps macOS App Group state"

assert_contains \
  "$apply_output" \
  "automation                    : disabled" \
  "Terrapod apply prints disabled automation macOS App Group state"

assert_contains \
  "$apply_output" \
  "launcher                      : enabled" \
  "Terrapod apply prints enabled launcher macOS App Group state"

assert_contains \
  "$apply_output" \
  "monitoring                    : disabled" \
  "Terrapod apply prints disabled monitoring macOS App Group state"

assert_contains \
  "$apply_output" \
  "development-apps              : enabled" \
  "Terrapod apply prints enabled development-apps macOS App Group state"

assert_contains \
  "$apply_output" \
  "Preflight: chezmoi is available" \
  "Terrapod apply confirms chezmoi preflight"

assert_contains \
  "$apply_output" \
  "Preflight: config file is readable" \
  "Terrapod apply confirms readable config preflight"

assert_contains \
  "$apply_output" \
  "Delegating declared-state apply to: chezmoi apply" \
  "Terrapod apply explains the delegated command"

assert_contains \
  "$apply_output" \
  "stub apply output" \
  "Terrapod apply includes delegated chezmoi apply output"

assert_contains \
  "$apply_output" \
  "Post-apply validation: Terrapod command is managed" \
  "Terrapod apply validates the Terrapod command managed target"

assert_contains \
  "$apply_output" \
  "Post-apply validation: tpod alias is managed" \
  "Terrapod apply validates the tpod alias managed target"

assert_contains \
  "$apply_output" \
  "Remaining install warnings:" \
  "Terrapod apply surfaces remaining install warning markers after apply"

assert_contains \
  "$apply_output" \
  "mise-tools: mise tool install needs attention" \
  "Terrapod apply prints remaining install warning marker summaries without failing solely because markers remain"

assert_no_ansi_escape "$apply_output" "captured Terrapod apply is plain without ANSI escapes"
assert_no_routine_emoji "$apply_output" "captured Terrapod apply has no routine emoji"

tty_apply_output="$(
  run_command_in_pty xterm unset env HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" sh "$terrapod" apply
)"
assert_has_ansi_escape "$tty_apply_output" "TTY Terrapod apply uses ANSI visual treatment"

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls during apply:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod apply does not call brew, apt, sudo, mise, or npm upgrade flows"
fi

pass "Terrapod apply does not call brew, apt, sudo, mise, or npm upgrade flows"

real_chezmoi="$(PATH="$system_path" command -v chezmoi 2>/dev/null || true)"
if [ -z "$real_chezmoi" ]; then
  fail "desktop apply recalculation test requires chezmoi"
fi

desktop_apply_source="$tmp_dir/desktop-apply-source"
desktop_apply_home="$tmp_dir/desktop-apply-home"
desktop_apply_state="$tmp_dir/desktop-apply-state"
desktop_apply_bin="$tmp_dir/desktop-apply-bin"
desktop_apply_log="$tmp_dir/desktop-apply-brew.log"
desktop_apply_config="$tmp_dir/desktop-apply.toml"
mkdir -p "$desktop_apply_home" "$desktop_apply_bin"
copy_desktop_apply_source_fixture "$desktop_apply_source"
ln -s "$real_chezmoi" "$desktop_apply_bin/chezmoi"
write_brew_bundle_stub "$desktop_apply_bin/brew"

write_desktop_apply_config "$desktop_apply_config" "$desktop_apply_source" "$desktop_apply_home" true true

if ! HOME="$desktop_apply_home" XDG_STATE_HOME="$desktop_apply_state" TERRAPOD_CHEZMOI_CONFIG="$desktop_apply_config" MACOS_BREW_LOG="$desktop_apply_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty raycast" PATH="$desktop_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/desktop-apply-first.out" 2>"$tmp_dir/desktop-apply-first.err"; then
  printf '%s\n' "desktop apply first stdout:" >&2
  sed 's/^/  /' "$tmp_dir/desktop-apply-first.out" >&2
  printf '%s\n' "desktop apply first stderr:" >&2
  sed 's/^/  /' "$tmp_dir/desktop-apply-first.err" >&2
  fail "Terrapod apply succeeds when desktop App Group casks fail with a marker"
fi

desktop_apply_marker="$desktop_apply_state/terrapod/install-warnings/homebrew-desktop-apps"
desktop_apply_marker_text="$(cat "$desktop_apply_marker")"
assert_contains "$desktop_apply_marker_text" "failed casks: ghostty, raycast" "Terrapod apply records failed casks from enabled terminal and launcher groups"
assert_contains "$desktop_apply_marker_text" "App Groups: terminal-apps, launcher" "Terrapod apply records failed App Groups from enabled terminal and launcher groups"

if ! HOME="$desktop_apply_home" XDG_STATE_HOME="$desktop_apply_state" TERRAPOD_CHEZMOI_CONFIG="$desktop_apply_config" MACOS_BREW_LOG="$desktop_apply_log" PATH="$desktop_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/desktop-apply-retry-success.out" 2>"$tmp_dir/desktop-apply-retry-success.err"; then
  printf '%s\n' "desktop apply retry success stdout:" >&2
  sed 's/^/  /' "$tmp_dir/desktop-apply-retry-success.out" >&2
  printf '%s\n' "desktop apply retry success stderr:" >&2
  sed 's/^/  /' "$tmp_dir/desktop-apply-retry-success.err" >&2
  fail "Terrapod apply retries desktop App Group failures with unchanged settings"
fi

if [ -e "$desktop_apply_marker" ]; then
  fail "Terrapod apply clears a desktop App Group marker after unchanged-settings retry succeeds"
fi
pass "Terrapod apply clears a desktop App Group marker after unchanged-settings retry succeeds"

if ! HOME="$desktop_apply_home" XDG_STATE_HOME="$desktop_apply_state" TERRAPOD_CHEZMOI_CONFIG="$desktop_apply_config" MACOS_BREW_LOG="$desktop_apply_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty raycast" PATH="$desktop_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/desktop-apply-recreate.out" 2>"$tmp_dir/desktop-apply-recreate.err"; then
  printf '%s\n' "desktop apply recreate stdout:" >&2
  sed 's/^/  /' "$tmp_dir/desktop-apply-recreate.out" >&2
  printf '%s\n' "desktop apply recreate stderr:" >&2
  sed 's/^/  /' "$tmp_dir/desktop-apply-recreate.err" >&2
  fail "Terrapod apply recreates desktop App Group marker before disabled-group recalculation"
fi

write_desktop_apply_config "$desktop_apply_config" "$desktop_apply_source" "$desktop_apply_home" true false

if ! HOME="$desktop_apply_home" XDG_STATE_HOME="$desktop_apply_state" TERRAPOD_CHEZMOI_CONFIG="$desktop_apply_config" MACOS_BREW_LOG="$desktop_apply_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_CASKS="ghostty raycast" PATH="$desktop_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/desktop-apply-terminal-only.out" 2>"$tmp_dir/desktop-apply-terminal-only.err"; then
  printf '%s\n' "desktop apply terminal-only stdout:" >&2
  sed 's/^/  /' "$tmp_dir/desktop-apply-terminal-only.out" >&2
  printf '%s\n' "desktop apply terminal-only stderr:" >&2
  sed 's/^/  /' "$tmp_dir/desktop-apply-terminal-only.err" >&2
  fail "Terrapod apply succeeds when disabled launcher failures are recalculated away"
fi

desktop_apply_marker_text="$(cat "$desktop_apply_marker")"
assert_contains "$desktop_apply_marker_text" "failed casks: ghostty" "Terrapod apply retains enabled terminal-apps failure after App Group settings change"
assert_contains "$desktop_apply_marker_text" "App Groups: terminal-apps" "Terrapod apply retains enabled terminal-apps group after App Group settings change"
assert_not_contains "$desktop_apply_marker_text" "raycast" "Terrapod apply removes disabled launcher cask from marker content"
assert_not_contains "$desktop_apply_marker_text" "launcher" "Terrapod apply removes disabled launcher group from marker content"

core_apply_source="$tmp_dir/core-apply-source"
core_apply_home="$tmp_dir/core-apply-home"
core_apply_state="$tmp_dir/core-apply-state"
core_apply_bin="$tmp_dir/core-apply-bin"
core_apply_log="$tmp_dir/core-apply-brew.log"
core_apply_config="$tmp_dir/core-apply.toml"
core_apply_prefix="$tmp_dir/core-apply-prefix"
mkdir -p "$core_apply_home" "$core_apply_bin" "$core_apply_prefix"
chmod 555 "$core_apply_prefix"
copy_desktop_apply_source_fixture "$core_apply_source"
ln -s "$real_chezmoi" "$core_apply_bin/chezmoi"
write_brew_bundle_stub "$core_apply_bin/brew"
write_desktop_apply_config "$core_apply_config" "$core_apply_source" "$core_apply_home" false false

if ! HOME="$core_apply_home" XDG_STATE_HOME="$core_apply_state" TERRAPOD_CHEZMOI_CONFIG="$core_apply_config" MACOS_BREW_LOG="$core_apply_log" MACOS_BREW_PREFIX="$core_apply_prefix" MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_FORMULAE="gum" PATH="$core_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/core-apply-first.out" 2>"$tmp_dir/core-apply-first.err"; then
  printf '%s\n' "core apply first stdout:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-first.out" >&2
  printf '%s\n' "core apply first stderr:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-first.err" >&2
  fail "Terrapod apply succeeds when core Homebrew bundle fails with a marker"
fi
chmod 755 "$core_apply_prefix"

core_apply_marker="$core_apply_state/terrapod/install-warnings/homebrew-core"
core_apply_marker_text="$(cat "$core_apply_marker")"
assert_contains "$core_apply_marker_text" "failed formulae: gum" "Terrapod apply records failed core formula detail"
assert_contains "$core_apply_marker_text" "Homebrew prefix is not writable: $core_apply_prefix" "Terrapod apply records shared-prefix permission guidance"
assert_not_contains "$core_apply_marker_text" "chown" "Terrapod apply core guidance avoids broad ownership command guidance"

if ! HOME="$core_apply_home" XDG_STATE_HOME="$core_apply_state" TERRAPOD_CHEZMOI_CONFIG="$core_apply_config" MACOS_BREW_LOG="$core_apply_log" PATH="$core_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/core-apply-retry-success.out" 2>"$tmp_dir/core-apply-retry-success.err"; then
  printf '%s\n' "core apply retry success stdout:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-retry-success.out" >&2
  printf '%s\n' "core apply retry success stderr:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-retry-success.err" >&2
  fail "Terrapod apply retries core Homebrew marker and succeeds"
fi

if [ -e "$core_apply_marker" ]; then
  fail "Terrapod apply clears a core Homebrew marker after retry succeeds"
fi
pass "Terrapod apply clears a core Homebrew marker after retry succeeds"

HOME="$core_apply_home" XDG_STATE_HOME="$core_apply_state" sh -c \
  '. "$1"; terrapod_install_warning_write homebrew-core "Homebrew core install needs attention" "old apply core warning."' \
  sh "$install_warnings_lib"

if ! HOME="$core_apply_home" XDG_STATE_HOME="$core_apply_state" TERRAPOD_CHEZMOI_CONFIG="$core_apply_config" MACOS_BREW_LOG="$core_apply_log" MACOS_BREW_FAIL_CORE_BULK=1 MACOS_BREW_FAIL_CASKS="font-d2coding" PATH="$core_apply_bin:/usr/bin:/bin" \
  /bin/sh "$terrapod" apply >"$tmp_dir/core-apply-retry-failure.out" 2>"$tmp_dir/core-apply-retry-failure.err"; then
  printf '%s\n' "core apply retry failure stdout:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-retry-failure.out" >&2
  printf '%s\n' "core apply retry failure stderr:" >&2
  sed 's/^/  /' "$tmp_dir/core-apply-retry-failure.err" >&2
  fail "Terrapod apply replaces a core Homebrew marker after retry fails"
fi

core_apply_marker_text="$(cat "$core_apply_marker")"
assert_contains "$core_apply_marker_text" "failed casks: font-d2coding" "Terrapod apply replaces core marker with current failed cask detail"
assert_not_contains "$core_apply_marker_text" "old apply core warning" "Terrapod apply removes stale core marker guidance after failed retry"

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply --dry-run >"$tmp_dir/apply-extra.out" 2>"$tmp_dir/apply-extra.err"; then
  fail "Terrapod apply rejects extra arguments"
fi

apply_extra_error="$(cat "$tmp_dir/apply-extra.err")"

assert_contains \
  "$apply_extra_error" \
  "terrapod: apply accepts no arguments" \
  "Terrapod apply explains rejected extra arguments"

if [ -e "$CHEZMOI_APPLY_INVOKED_FILE" ]; then
  fail "Terrapod apply rejects extra arguments before calling chezmoi apply"
fi

pass "Terrapod apply rejects extra arguments before calling chezmoi apply"

write_stub "$tmp_dir/bin/chezmoi" \
  'command_name=' \
  'for arg do' \
  '  case "$arg" in' \
  '    apply|managed)' \
  '      command_name="$arg"' \
  '      ;;' \
  '  esac' \
  'done' \
  'case "$command_name" in' \
  '  apply)' \
  '    printf "%s\n" invoked >"$CHEZMOI_APPLY_INVOKED_FILE"' \
  '    . "$TERRAPOD_APPLY_FAILURE_MARKER_LIB"' \
  '    terrapod_install_warning_write mise-tools "mise tool install needs attention" "Run tpod apply after fixing mise tool installation output."' \
  '    printf "%s\n" "simulated apply failure" >&2' \
  '    exit 91' \
  '    ;;' \
  'esac' \
  'printf "%s\n" "unexpected chezmoi command: $*" >&2' \
  'exit 92'

apply_failure_marker_state="$tmp_dir/apply-failure-marker-state"

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" XDG_STATE_HOME="$apply_failure_marker_state" TERRAPOD_APPLY_FAILURE_MARKER_LIB="$install_warnings_lib" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-failure.out" 2>"$tmp_dir/apply-failure.err"; then
  fail "Terrapod apply fails when delegated chezmoi apply fails"
fi

apply_failure_output="$(cat "$tmp_dir/apply-failure.out")"
apply_failure_error="$(cat "$tmp_dir/apply-failure.err")"

assert_contains \
  "$apply_failure_error" \
  "terrapod: chezmoi apply failed; fix the error above, then rerun 'tpod apply'." \
  "Terrapod apply failure guidance uses tpod"

assert_not_contains \
  "$apply_failure_error" \
  "rerun 'terrapod apply'" \
  "Terrapod apply failure avoids old rerun command"

assert_contains \
  "$apply_failure_output" \
  "Remaining install warnings:" \
  "Terrapod apply surfaces remaining install warning markers when delegated chezmoi apply fails"

assert_contains \
  "$apply_failure_output" \
  "mise-tools: mise tool install needs attention" \
  "Terrapod apply prints marker summaries when delegated chezmoi apply fails"

write_stub "$tmp_dir/bin/chezmoi" \
  'command_name=' \
  'for arg do' \
  '  case "$arg" in' \
  '    apply|managed)' \
  '      command_name="$arg"' \
  '      ;;' \
  '  esac' \
  'done' \
  'case "$command_name" in' \
  '  apply)' \
  '    printf "%s\n" invoked >"$CHEZMOI_APPLY_INVOKED_FILE"' \
  '    : >"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" "stub apply output"' \
  '    exit 0' \
  '    ;;' \
  '  managed)' \
  '    : >"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" ".local/bin/terrapod"' \
  '    printf "%s\n" ".local/bin/tpod"' \
  '    exit 0' \
  '    ;;' \
  'esac' \
  'printf "%s\n" "unexpected chezmoi command: $*" >&2' \
  'exit 91'

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$diff_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-override.out" 2>"$tmp_dir/apply-override.err"; then
  printf '%s\n' "override apply stdout:" >&2
  sed 's/^/  /' "$tmp_dir/apply-override.out" >&2
  printf '%s\n' "override apply stderr:" >&2
  sed 's/^/  /' "$tmp_dir/apply-override.err" >&2
  fail "Terrapod apply runs successfully with an explicit config override"
fi

apply_override_output="$(cat "$tmp_dir/apply-override.out")"

assert_call_args \
  "$CHEZMOI_APPLY_ARGS_FILE" \
  "Terrapod apply passes explicit config overrides to chezmoi apply" \
  --config "$diff_config" apply

assert_call_args \
  "$CHEZMOI_MANAGED_ARGS_FILE" \
  "Terrapod apply passes explicit config overrides to chezmoi managed" \
  --config "$diff_config" managed

assert_contains \
  "$apply_override_output" \
  "Delegating declared-state apply to: chezmoi --config $diff_config apply" \
  "Terrapod apply explains delegated explicit config command"

symlink_config_target="$tmp_dir/symlink-target-chezmoi.toml"
symlink_config="$tmp_dir/symlink-chezmoi.toml"
cp "$diff_config" "$symlink_config_target"
ln -s "$symlink_config_target" "$symlink_config"

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$symlink_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-symlink.out" 2>"$tmp_dir/apply-symlink.err"; then
  printf '%s\n' "symlink config apply stdout:" >&2
  sed 's/^/  /' "$tmp_dir/apply-symlink.out" >&2
  printf '%s\n' "symlink config apply stderr:" >&2
  sed 's/^/  /' "$tmp_dir/apply-symlink.err" >&2
  fail "Terrapod apply accepts a symlink to a regular config file"
fi

apply_symlink_output="$(cat "$tmp_dir/apply-symlink.out")"

assert_call_args \
  "$CHEZMOI_APPLY_ARGS_FILE" \
  "Terrapod apply delegates with a symlink config path" \
  --config "$symlink_config" apply

assert_call_args \
  "$CHEZMOI_MANAGED_ARGS_FILE" \
  "Terrapod apply validates managed targets with a symlink config path" \
  --config "$symlink_config" managed

assert_contains \
  "$apply_symlink_output" \
  "Preflight: config file is readable" \
  "Terrapod apply treats symlinked config files as readable"

write_stub "$tmp_dir/bin/chezmoi" \
  'command_name=' \
  'for arg do' \
  '  case "$arg" in' \
  '    apply|managed)' \
  '      command_name="$arg"' \
  '      ;;' \
  '  esac' \
  'done' \
  'case "$command_name" in' \
  '  apply)' \
  '    printf "%s\n" invoked >"$CHEZMOI_APPLY_INVOKED_FILE"' \
  '    : >"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_APPLY_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" "stub apply output"' \
  '    exit 0' \
  '    ;;' \
  '  managed)' \
  '    : >"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    for arg do' \
  '      printf "%s\n" "$arg" >>"$CHEZMOI_MANAGED_ARGS_FILE"' \
  '    done' \
  '    printf "%s\n" ".local/bin/terrapod"' \
  '    exit 0' \
  '    ;;' \
  'esac' \
  'printf "%s\n" "unexpected chezmoi command: $*" >&2' \
  'exit 91'

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" XDG_STATE_HOME="$apply_marker_state" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-validation.out" 2>"$tmp_dir/apply-validation.err"; then
  fail "Terrapod apply fails when tpod alias is not managed"
fi

apply_validation_output="$(cat "$tmp_dir/apply-validation.out")"
apply_validation_error="$(cat "$tmp_dir/apply-validation.err")"

assert_contains \
  "$apply_validation_error" \
  "terrapod: post-apply validation failed: tpod alias is not managed (.local/bin/tpod missing)" \
  "Terrapod apply explains missing tpod alias managed target"

assert_contains \
  "$apply_validation_error" \
  "Run 'tpod chezmoi -- managed' to inspect managed targets, then rerun 'tpod apply'." \
  "Terrapod apply gives actionable post-apply validation guidance with tpod"

assert_not_contains \
  "$apply_validation_error" \
  "Run 'terrapod chezmoi -- managed'" \
  "Terrapod apply avoids old post-apply validation inspection command"

assert_contains \
  "$apply_validation_output" \
  "Remaining install warnings:" \
  "Terrapod apply still surfaces install warning markers when post-apply validation fails"

rm -f "$CHEZMOI_APPLY_ARGS_FILE" "$CHEZMOI_APPLY_INVOKED_FILE" "$CHEZMOI_MANAGED_ARGS_FILE"

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" TERRAPOD_CHEZMOI_CONFIG="$diff_config" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-override-validation.out" 2>"$tmp_dir/apply-override-validation.err"; then
  fail "Terrapod apply fails when tpod alias is not managed with an explicit config override"
fi

apply_override_validation_error="$(cat "$tmp_dir/apply-override-validation.err")"

assert_contains \
  "$apply_override_validation_error" \
  "terrapod: post-apply validation failed: tpod alias is not managed (.local/bin/tpod missing)" \
  "Terrapod apply explains missing tpod alias managed target with an explicit config override"

assert_contains \
  "$apply_override_validation_error" \
  "Run 'tpod chezmoi -- --config $diff_config managed' to inspect managed targets, then rerun 'tpod apply'." \
  "Terrapod apply gives config-aware post-apply validation guidance with tpod"

assert_not_contains \
  "$apply_override_validation_error" \
  "Run 'terrapod chezmoi -- --config $diff_config managed'" \
  "Terrapod apply avoids old config-aware post-apply validation command"
