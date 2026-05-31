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

assert_no_rich_setup_emoji() {
  haystack="$1"
  message="$2"

  if printf '%s\n' "$haystack" | grep -E '🌱|✨|▸' >/dev/null; then
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
    write_stub "$isolated_path/$command_name" \
      'exit 0'
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
write_gum_stub "$tmp_dir/bin/gum"
no_gum_path="$tmp_dir/no-gum-bin"
write_no_gum_path "$no_gum_path" sh

terrapod="$repo_root/dot_local/bin/executable_terrapod"
tpod_source="$repo_root/dot_local/bin/symlink_tpod"

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

sh -n "$terrapod" || fail "Terrapod command is valid POSIX shell"
pass "Terrapod command is valid POSIX shell"

help_output="$(TERRAPOD_PROFILE=macos-terminal sh "$terrapod" help)"

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
  "terrapod chezmoi -- <args...>" \
  "Terrapod help documents the raw chezmoi escape hatch"

assert_contains \
  "$help_output" \
  "terrapod configure <minimal|development|workstation>" \
  "Terrapod help documents Preset configuration"

assert_contains \
  "$help_output" \
  "terrapod setup" \
  "Terrapod help lists setup"

assert_contains \
  "$help_output" \
  "tpod is the short day-to-day alias for terrapod." \
  "Terrapod help documents tpod as the day-to-day alias"

assert_contains \
  "$help_output" \
  "tpod apply" \
  "Terrapod help examples lead with the short apply command"

assert_contains \
  "$help_output" \
  "setup" \
  "Terrapod help describes setup"

assert_contains \
  "$help_output" \
  "terrapod status" \
  "Terrapod help documents status"

assert_contains \
  "$help_output" \
  "terrapod doctor" \
  "Terrapod help documents doctor"

macos_help_output="$(TERRAPOD_PROFILE=macos-terminal sh "$terrapod" help)"

assert_contains \
  "$macos_help_output" \
  "terrapod configure <minimal|development|workstation>" \
  "macOS Terminal Profile help exposes workstation Preset"

vps_help_output="$(TERRAPOD_PROFILE=vps-shell sh "$terrapod" help)"

assert_contains \
  "$vps_help_output" \
  "terrapod configure <minimal|development>" \
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
write_gum_responses "$setup_responses" workstation yes yes yes yes yes yes yes

if ! run_terrapod_setup_command macos-terminal "$setup_responses" "$setup_home" "$setup_xdg" "$setup_output" "$setup_gum_log"; then
  sed 's/^/  /' "$setup_output" >&2
  fail "macOS Terminal Profile setup uses gum prompts before final confirmation and completes with workstation"
fi
pass "macOS Terminal Profile setup uses gum prompts before final confirmation and completes with workstation"

setup_output_text="$(cat "$setup_output")"
setup_gum_log_text="$(cat "$setup_gum_log")"
assert_contains "$setup_output_text" "🌱 Terrapod Setup" "gum setup prints a rich setup heading"
assert_contains "$setup_output_text" "Profile  macOS Terminal Profile" "gum setup shows detected macOS profile in aligned setup context"
assert_contains "$setup_output_text" "Choose a Preset" "gum setup labels the Preset choice section"
assert_not_contains "$setup_output_text" "Preset guide:" "gum setup does not print a separate Preset guide"
assert_contains "$setup_output_text" "Settings to write:" "gum setup shows concrete settings summary"
assert_contains "$setup_output_text" "Customize Terrapod settings." "gum setup offers sequential setting customization"
assert_not_contains "$setup_output_text" "Option guide:" "gum setup does not print a separate option guide"
assert_contains "$setup_output_text" "Optional Development Workspace: Dev Zellij layouts; also includes Editor and AI tool stacks." "gum setup explains Optional Development Workspace"
assert_contains "$setup_output_text" "Optional AI Tool Stack: Antigravity CLI, Claude Code, and Codex." "gum setup explains Optional AI Tool Stack"
assert_contains "$setup_output_text" "terminal-apps macOS App Group: Ghostty and cmux." "gum setup explains terminal-apps macOS App Group"
assert_contains "$setup_output_text" "ai-apps macOS App Group: Claude Desktop, Codex Desktop, Antigravity 2.0, and Antigravity IDE." "gum setup explains ai-apps macOS App Group"
assert_contains "$setup_output_text" "Optional Editor Stack: included by Optional Development Workspace" "gum setup presents workspace-included Optional Editor Stack"
assert_contains "$setup_output_text" "enableEditorStack = true" "gum setup summary includes concrete Editor Stack setting"
assert_contains "$setup_output_text" "enableMacosAppGroupMonitoring = true" "gum setup summary includes concrete macOS App Group setting"
assert_contains "$setup_output_text" "enableMacosAppGroupAiApps = true" "gum setup summary includes concrete ai-apps App Group setting"
assert_contains "$setup_output_text" "Configured Terrapod Preset 'workstation'" "gum setup reports successful configuration"
assert_contains "$setup_gum_log_text" "gum args: style" "gum setup uses gum style for setup-only presentation"
assert_contains "$setup_gum_log_text" "gum args: choose" "gum setup uses gum choose for Preset selection"
assert_contains "$setup_gum_log_text" "gum stdin: minimal      Core shell/runtime baseline:minimal" "gum setup offers minimal Preset with nearby explanation"
assert_contains "$setup_gum_log_text" "gum stdin: development  Coding machine with editor, AI CLI, workspace:development" "gum setup offers development Preset with nearby explanation"
assert_contains "$setup_gum_log_text" "gum stdin: workstation  macOS workstation with development setup and app groups:workstation" "gum setup offers workstation Preset with nearby explanation"
assert_contains "$setup_gum_log_text" "gum args: confirm Optional Development Workspace" "gum setup asks Optional Development Workspace with gum confirm"
assert_contains "$setup_gum_log_text" "gum args: confirm terminal-apps macOS App Group" "gum setup asks terminal-apps macOS App Group with gum confirm"
assert_contains "$setup_gum_log_text" "gum args: confirm ai-apps macOS App Group" "gum setup asks ai-apps macOS App Group with gum confirm"
assert_contains "$setup_gum_log_text" "gum args: confirm Write these Terrapod settings" "gum setup asks final confirmation with gum confirm"
assert_first_occurrence_before "$setup_output_text" "Profile  macOS Terminal Profile" "Customize Terrapod settings." "gum setup shows profile before settings customization"
assert_first_occurrence_before "$setup_output_text" "Choose a Preset" "Customize Terrapod settings." "gum setup presents Preset selection before customization"
assert_first_occurrence_before "$setup_output_text" "Optional Development Workspace: Dev Zellij layouts; also includes Editor and AI tool stacks." "Optional Editor Stack: included by Optional Development Workspace" "gum setup explains an option immediately before related output"
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
assert_contains "$gum_error_output_text" "rerun 'terrapod setup'" "gum operational error gives rerun guidance"
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
  "Run 'terrapod help' for usage." \
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
enableEditorStack = true
enableAiCliTools = true
enableDevelopmentWorkspace = true
enableMacosAppGroupTerminalApps = true
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = true
enableMacosAppGroupMonitoring = false
enableMacosAppGroupAiApps = true
TOML

macos_status_path="$(status_doctor_path macos chezmoi git zsh mise brew nvim agy claude codex zellij ghostty cmux op)"

macos_status_output="$(
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$status_config" PATH="$macos_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$macos_status_output" "Terrapod status" "Terrapod status prints a command heading"
assert_contains "$macos_status_output" "Profile: macOS Terminal Profile" "Terrapod status reports macOS Terminal Profile context"
assert_contains "$macos_status_output" "Config: $status_config (present)" "Terrapod status reports explicit config path"
assert_contains "$macos_status_output" "Optional Editor Stack: enabled (rich Neovim configuration)" "Terrapod status reports enabled Optional Editor Stack state"
assert_contains "$macos_status_output" "Optional AI Tool Stack: enabled (tools available: agy, claude, codex)" "Terrapod status reports enabled Optional AI Tool Stack tool state"
assert_contains "$macos_status_output" "Optional Development Workspace: enabled (development Zellij layouts)" "Terrapod status reports enabled Optional Development Workspace state"
assert_contains "$macos_status_output" "terminal-apps: enabled (Ghostty and cmux)" "Terrapod status reports enabled terminal-apps macOS App Group"
assert_contains "$macos_status_output" "automation: disabled" "Terrapod status reports disabled automation macOS App Group"
assert_contains "$macos_status_output" "launcher: enabled (Raycast and 1Password CLI)" "Terrapod status reports enabled launcher macOS App Group"
assert_contains "$macos_status_output" "monitoring: disabled" "Terrapod status reports disabled monitoring macOS App Group"
assert_contains "$macos_status_output" "ai-apps: enabled (Claude Desktop, Codex Desktop, Antigravity 2.0, and Antigravity IDE)" "Terrapod status reports enabled ai-apps macOS App Group"
assert_contains "$macos_status_output" "chezmoi: available" "Terrapod status reports chezmoi availability"
assert_contains "$macos_status_output" "brew: available" "Terrapod status reports macOS Bootstrap Package Manager availability"
assert_contains "$macos_status_output" "Warnings: none" "Terrapod status reports no warnings when enabled tools are present"
assert_not_contains "$macos_status_output" "Warning:" "Terrapod status emits no warning lines when enabled tools are present"
assert_no_ansi_escape "$help_output" "Terrapod help does not use setup ANSI presentation"
assert_no_rich_setup_emoji "$help_output" "Terrapod help does not use setup emoji presentation"
assert_no_ansi_escape "$macos_status_output" "Terrapod status does not use setup ANSI presentation"
assert_no_rich_setup_emoji "$macos_status_output" "Terrapod status does not use setup emoji presentation"

status_dotted_config="$tmp_dir/status-dotted.toml"
cat >"$status_dotted_config" <<'TOML'
data.enableEditorStack = false
data.enableAiCliTools = false
data.enableDevelopmentWorkspace = true
data.enableMacosAppGroupTerminalApps = true
data.enableMacosAppGroupAiApps = true
TOML

dotted_status_path="$(status_doctor_path dotted chezmoi git zsh mise brew nvim agy claude codex zellij)"

dotted_status_output="$(
  TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG="$status_dotted_config" PATH="$dotted_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$dotted_status_output" "Optional Editor Stack: enabled (rich Neovim configuration)" "Terrapod status reads root dotted data keys for effective editor stack state"
assert_contains "$dotted_status_output" "Optional AI Tool Stack: enabled (tools available: agy, claude, codex)" "Terrapod status reads root dotted data keys for effective AI stack state"
assert_contains "$dotted_status_output" "terminal-apps: enabled (Ghostty and cmux)" "Terrapod status reads root dotted data keys for macOS App Groups"
assert_contains "$dotted_status_output" "ai-apps: enabled (Claude Desktop, Codex Desktop, Antigravity 2.0, and Antigravity IDE)" "Terrapod status reads root dotted data keys for ai-apps macOS App Group"
assert_contains "$dotted_status_output" "Warnings: none" "Terrapod status has no warnings for root dotted data keys when tools are present"

status_ubuntu_config="$tmp_dir/status-ubuntu.toml"
status_ubuntu_os_release="$tmp_dir/status-ubuntu-os-release"
cat >"$status_ubuntu_config" <<'TOML'
[data]
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupAiApps = false
TOML
write_os_release "$status_ubuntu_os_release" ubuntu 24.04 "Ubuntu 24.04 LTS"

ubuntu_status_path="$(status_doctor_path ubuntu chezmoi git zsh mise nvim zellij apt)"
write_stub "$ubuntu_status_path/uname" 'printf "%s\n" "Linux"'

ubuntu_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$ubuntu_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$ubuntu_status_output" "Profile: VPS Shell Profile" "Terrapod status reports VPS Shell Profile context on Ubuntu 24.04"
assert_contains "$ubuntu_status_output" "Optional Editor Stack: disabled" "Terrapod status reports disabled Optional Editor Stack without treating nvim as missing"
assert_contains "$ubuntu_status_output" "Optional AI Tool Stack: disabled" "Terrapod status reports disabled Optional AI Tool Stack without missing-tool warnings"
assert_contains "$ubuntu_status_output" "Optional Development Workspace: disabled" "Terrapod status reports disabled Optional Development Workspace without missing-tool warnings"
assert_contains "$ubuntu_status_output" "macOS App Groups: not applicable for VPS Shell Profile" "Terrapod status omits macOS App Group details on VPS Shell Profile"
assert_contains "$ubuntu_status_output" "apt: available" "Terrapod status reports Ubuntu Bootstrap Package Manager availability"
assert_contains "$ubuntu_status_output" "Warnings: none" "Terrapod status has no warnings for disabled optional stacks"
assert_not_contains "$ubuntu_status_output" "Warning:" "Terrapod status emits no warning lines for disabled optional stacks"
assert_not_contains "$ubuntu_status_output" "missing tools: nvim" "Terrapod status distinguishes disabled Optional Editor Stack from missing tools"
assert_not_contains "$ubuntu_status_output" "missing tools: agy" "Terrapod status distinguishes disabled Optional AI Tool Stack from missing tools"

core_missing_status_path="$(status_doctor_path core-missing-status chezmoi git zsh mise apt)"
write_stub "$core_missing_status_path/uname" 'printf "%s\n" "Linux"'

core_missing_status_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" PATH="$core_missing_status_path" \
    /bin/sh "$terrapod" status
)"

assert_contains "$core_missing_status_output" "Optional Editor Stack: disabled" "Terrapod status keeps disabled Optional Editor Stack separate from missing core Neovim"
assert_contains "$core_missing_status_output" "Optional Development Workspace: disabled" "Terrapod status keeps disabled Optional Development Workspace separate from missing core Zellij"
assert_contains "$core_missing_status_output" "nvim: missing" "Terrapod status reports missing plain Neovim as a key tool"
assert_contains "$core_missing_status_output" "zellij: missing" "Terrapod status reports missing Zellij as a key tool"
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

assert_contains "$missing_status_output" "Optional Editor Stack: enabled (rich Neovim configuration)" "Terrapod status reports enabled Optional Editor Stack as rich config state"
assert_contains "$missing_status_output" "Optional AI Tool Stack: enabled (missing tools: agy, claude, codex)" "Terrapod status reports missing tools only for enabled Optional AI Tool Stack"
assert_contains "$missing_status_output" "Optional Development Workspace: enabled (development Zellij layouts)" "Terrapod status reports enabled Optional Development Workspace as layout state"
assert_contains "$missing_status_output" "Warning: Optional AI Tool Stack is enabled but missing tools: agy, claude, codex" "Terrapod status warns for enabled missing AI tools"

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

assert_contains "$workspace_bundle_status_output" "Optional Editor Stack: enabled (rich Neovim configuration)" "Terrapod status treats Optional Development Workspace as enabling Optional Editor Stack"
assert_contains "$workspace_bundle_status_output" "Optional AI Tool Stack: enabled (missing tools: agy, claude, codex)" "Terrapod status treats Optional Development Workspace as enabling Optional AI Tool Stack"
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
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
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
assert_contains "$doctor_ok_output" "ok - Optional Development Workspace is disabled" "Terrapod doctor treats disabled Optional Development Workspace as valid"
assert_contains "$doctor_ok_output" "Guidance: none" "Terrapod doctor prints no guidance when checks pass"

doctor_missing_key_path="$(status_doctor_path doctor-missing-key git zsh mise nvim zellij apt)"
write_stub "$doctor_missing_key_path/uname" 'printf "%s\n" "Linux"'

if TERRAPOD_OS_RELEASE_FILE="$doctor_os_release" TERRAPOD_CHEZMOI_CONFIG="$doctor_config" PATH="$doctor_missing_key_path" \
  /bin/sh "$terrapod" doctor >"$tmp_dir/doctor-missing-key.out" 2>"$tmp_dir/doctor-missing-key.err"; then
  fail "Terrapod doctor fails when a required key tool is missing"
fi

doctor_missing_key_output="$(cat "$tmp_dir/doctor-missing-key.out")"

assert_contains "$doctor_missing_key_output" "warn - chezmoi is missing" "Terrapod doctor warns when required chezmoi is missing"
assert_contains "$doctor_missing_key_output" "Install or apply the configured Core Shell Stack so 'chezmoi' is available on PATH." "Terrapod doctor gives actionable guidance for missing key tools"

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
assert_contains "$doctor_missing_output" "Run terrapod chezmoi -- apply after enabling Optional AI Tool Stack, or install/apply the configured tools before relying on them." "Terrapod doctor gives actionable missing optional-stack guidance"

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
enableEditorStack = true
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosDesktopApps = false
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

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod update does not call brew, apt, sudo, mise, or npm upgrade flows"
fi

pass "Terrapod update does not call brew, apt, sudo, mise, or npm upgrade flows"

override_config="$tmp_dir/override-chezmoi.toml"

cat >"$override_config" <<'TOML'
[data]
enableEditorStack = false
enableAiCliTools = true
enableDevelopmentWorkspace = false
enableMacosDesktopApps = false
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
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = true
enableMacosAppGroupTerminalApps = true
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = true
enableMacosAppGroupMonitoring = false
enableMacosAppGroupAiApps = true
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
  "Optional Editor Stack: enabled" \
  "Terrapod diff prints effective enabled Optional Editor Stack state"

assert_contains \
  "$diff_output" \
  "Optional AI Tool Stack: enabled" \
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
  "terminal-apps: enabled" \
  "Terrapod diff prints enabled terminal-apps macOS App Group state"

assert_contains \
  "$diff_output" \
  "automation: disabled" \
  "Terrapod diff prints disabled automation macOS App Group state"

assert_contains \
  "$diff_output" \
  "launcher: enabled" \
  "Terrapod diff prints enabled launcher macOS App Group state"

assert_contains \
  "$diff_output" \
  "monitoring: disabled" \
  "Terrapod diff prints disabled monitoring macOS App Group state"

assert_contains \
  "$diff_output" \
  "ai-apps: enabled" \
  "Terrapod diff prints enabled ai-apps macOS App Group state"

assert_contains \
  "$diff_output" \
  "Delegating declared-state diff to: chezmoi diff" \
  "Terrapod diff explains the delegated command"

assert_contains \
  "$diff_output" \
  "stub diff output" \
  "Terrapod diff includes delegated chezmoi diff output"

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

if ! HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
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
  "Optional Editor Stack: enabled" \
  "Terrapod apply prints effective enabled Optional Editor Stack state"

assert_contains \
  "$apply_output" \
  "Optional AI Tool Stack: enabled" \
  "Terrapod apply prints effective enabled Optional AI Tool Stack state"

assert_contains \
  "$apply_output" \
  "Optional Development Workspace: enabled" \
  "Terrapod apply prints enabled Optional Development Workspace state"

assert_contains \
  "$apply_output" \
  "terminal-apps: enabled" \
  "Terrapod apply prints enabled terminal-apps macOS App Group state"

assert_contains \
  "$apply_output" \
  "automation: disabled" \
  "Terrapod apply prints disabled automation macOS App Group state"

assert_contains \
  "$apply_output" \
  "launcher: enabled" \
  "Terrapod apply prints enabled launcher macOS App Group state"

assert_contains \
  "$apply_output" \
  "monitoring: disabled" \
  "Terrapod apply prints disabled monitoring macOS App Group state"

assert_contains \
  "$apply_output" \
  "ai-apps: enabled" \
  "Terrapod apply prints enabled ai-apps macOS App Group state"

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

if [ -e "$BROAD_UPGRADE_CALL_FILE" ]; then
  printf '%s\n' "unexpected broad upgrade command calls during apply:" >&2
  sed 's/^/  /' "$BROAD_UPGRADE_CALL_FILE" >&2
  fail "Terrapod apply does not call brew, apt, sudo, mise, or npm upgrade flows"
fi

pass "Terrapod apply does not call brew, apt, sudo, mise, or npm upgrade flows"

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

if HOME="$diff_home" XDG_CONFIG_HOME="$diff_xdg" PATH="$tmp_dir/bin:/usr/bin:/bin" \
  sh "$terrapod" apply >"$tmp_dir/apply-validation.out" 2>"$tmp_dir/apply-validation.err"; then
  fail "Terrapod apply fails when tpod alias is not managed"
fi

apply_validation_error="$(cat "$tmp_dir/apply-validation.err")"

assert_contains \
  "$apply_validation_error" \
  "terrapod: post-apply validation failed: tpod alias is not managed (.local/bin/tpod missing)" \
  "Terrapod apply explains missing tpod alias managed target"

assert_contains \
  "$apply_validation_error" \
  "Run 'terrapod chezmoi -- managed' to inspect managed targets, then rerun 'terrapod apply'." \
  "Terrapod apply gives actionable post-apply validation guidance"

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
  "Run 'terrapod chezmoi -- --config $diff_config managed' to inspect managed targets, then rerun 'terrapod apply'." \
  "Terrapod apply gives config-aware post-apply validation guidance"
