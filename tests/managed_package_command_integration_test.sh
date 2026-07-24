#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
terrapod="$repo_root/dot_local/bin/executable_terrapod"
managed_packages="$repo_root/dot_local/lib/terrapod/executable_managed-packages"
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
  text="$1"
  expected="$2"
  label="$3"
  case "$text" in
    *"$expected"*) pass "$label" ;;
    *)
      printf '%s\n' "$text" >&2
      fail "$label (missing: $expected)"
      ;;
  esac
}

assert_before() {
  text="$1"
  first="$2"
  second="$3"
  label="$4"
  first_line="$(printf '%s\n' "$text" | grep -n -F "$first" | sed -n '1s/:.*//p')"
  second_line="$(printf '%s\n' "$text" | grep -n -F "$second" | sed -n '1s/:.*//p')"
  [ -n "$first_line" ] && [ -n "$second_line" ] && [ "$first_line" -lt "$second_line" ] ||
    fail "$label"
  pass "$label"
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

write_config() {
  path="$1"
  mkdir -p "${path%/*}"
  cat >"$path" <<'EOF'
[data]
profile = "macos-terminal"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
EOF
}

write_empty_inventory() {
  directory="$1"
  mkdir -p "$directory"
  for name in homebrew-formula homebrew-cask mise aqua npm apt snap cargo pipx vendor path apps; do
    : >"$directory/$name"
  done
}

home_dir="$tmp_dir/home"
fake_bin="$tmp_dir/bin"
inventory="$tmp_dir/inventory"
config="$tmp_dir/config/chezmoi.toml"
command_log="$tmp_dir/commands.log"
mkdir -p "$home_dir/.local/bin" "$fake_bin"
HOME="$home_dir"
XDG_CONFIG_HOME="$tmp_dir/xdg"
export HOME XDG_CONFIG_HOME
write_config "$config"
write_empty_inventory "$inventory"
: >"$command_log"

write_stub "$fake_bin/uname" \
  'case "${1:-}" in -m) printf "%s\n" arm64 ;; *) printf "%s\n" Darwin ;; esac'
write_stub "$fake_bin/chezmoi" \
  'printf "chezmoi:%s\n" "$*" >>"$TERRAPOD_COMMAND_LOG"' \
  'case " $* " in *" managed "*) printf "%s\n" ".local/bin/terrapod" ".local/bin/tpod" ;; esac'

apply_output="$(
  HOME="$home_dir" \
    XDG_CONFIG_HOME="$tmp_dir/config" \
    XDG_STATE_HOME="$tmp_dir/state-apply" \
    TERRAPOD_PROFILE=macos-terminal \
    TERRAPOD_CHEZMOI_CONFIG="$config" \
    TERRAPOD_COMMAND_LOG="$command_log" \
    TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" \
    PATH="$fake_bin:$PATH" \
    "$terrapod" apply
)"
assert_contains "$apply_output" "Managed Package reconciliation: ready" "tpod apply runs Managed Package reconciliation"
assert_before "$(cat "$command_log")" "chezmoi:--config $config apply" "chezmoi:--config $config managed" "tpod apply installs canonical declared state before post-apply validation"

: >"$command_log"
write_stub "$home_dir/.local/bin/tpod" \
  'printf "installed-tpod:%s\n" "$*" >>"$TERRAPOD_COMMAND_LOG"' \
  'exit 7'
set +e
HOME="$home_dir" \
  XDG_CONFIG_HOME="$tmp_dir/config" \
  TERRAPOD_PROFILE=macos-terminal \
  TERRAPOD_CHEZMOI_CONFIG="$config" \
  TERRAPOD_COMMAND_LOG="$command_log" \
  PATH="$fake_bin:$PATH" \
  "$terrapod" update >"$tmp_dir/update.out" 2>&1
update_status="$?"
set -e
[ "$update_status" -eq 7 ] || fail "tpod update returns refreshed apply exit status"
pass "tpod update returns refreshed apply exit status"
update_log="$(cat "$command_log")"
assert_before "$update_log" "chezmoi:--config $config update --exclude scripts" "installed-tpod:apply" "tpod update refreshes source before installed tpod apply handoff"

probe_bin="$tmp_dir/probe-bin"
probe_log="$tmp_dir/probe.log"
mkdir -p "$probe_bin"
: >"$probe_log"
write_stub "$probe_bin/brew" \
  'printf "brew:%s\n" "$*" >>"$TERRAPOD_PROBE_LOG"' \
  'case "$*" in "--prefix") printf "%s\n" "$HOME/homebrew" ;; "list --formula --full-name") printf "%s\n" "bun" ;; "list --cask --full-name") : ;; esac'
write_stub "$probe_bin/mise" \
  'printf "mise:%s\n" "$*" >>"$TERRAPOD_PROBE_LOG"' \
  'case "$*" in' \
  '  "ls --installed --no-header") printf "%s\n" "bat 0.25.0" "btop 1.4.0" ;;' \
  '  "where bat@0.25.0") printf "%s\n" "$HOME/.local/share/mise/installs/bat/0.25.0" ;;' \
  '  "where btop@1.4.0") printf "%s\n" "/opt/shared/mise/btop/1.4.0" ;;' \
  'esac'
write_stub "$probe_bin/npm" \
  'printf "npm:%s\n" "$*" >>"$TERRAPOD_PROBE_LOG"' \
  'if [ "$*" = "root -g" ]; then printf "%s\n" "$HOME/.npm/lib/node_modules"; fi'
mkdir -p "$home_dir/.npm/lib/node_modules/@openai/codex"
scan_config="$tmp_dir/config/scan.toml"
write_config "$scan_config"
sed 's/enableAiCliTools = false/enableAiCliTools = true/' "$scan_config" >"$scan_config.tmp"
mv "$scan_config.tmp" "$scan_config"

scan_output="$(
  HOME="$home_dir" \
    TERRAPOD_PROBE_LOG="$probe_log" \
    PATH="$probe_bin:$PATH" \
    "$managed_packages" scan "$scan_config" macos-terminal
)"
assert_contains "$scan_output" "homebrew-formula|bun|nonstandard" "deep scan inventories exact Homebrew formula identities and prefix safety"
assert_contains "$scan_output" "mise|bat|user|bat@0.25.0" "deep scan inventories exact mise tool versions"
assert_contains "$scan_output" "mise|btop|shared|btop@1.4.0" "deep scan refuses shared mise payload ownership"
assert_contains "$scan_output" "npm|@openai/codex|user" "deep scan inventories exact global npm identities"
assert_contains "$(cat "$probe_log")" "brew:list --formula --full-name" "deep scan uses read-only Homebrew inventory"

diagnostic_inventory="$tmp_dir/diagnostic-inventory"
write_empty_inventory "$diagnostic_inventory"
registry="$("$managed_packages" registry "$config" macos-terminal)"
printf '%s\n' "$registry" | awk -F'|' '
  $3 == "homebrew" { print $4 "|user" }
  $3 == "homebrew-cask" { print $4 "|user" }
' >"$diagnostic_inventory/homebrew-formula"
printf '%s\n' "$registry" | awk -F'|' '$3 == "mise" { print $1 "|user|" $1 "@test" }' >"$diagnostic_inventory/mise"
printf '%s\n' "$registry" | awk -F'|' '$5 == "executable" { print $1 "|/opt/homebrew/bin/" $6 "|canonical" }' >"$diagnostic_inventory/path"

doctor_output="$(
  TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$diagnostic_inventory" \
    "$managed_packages" doctor "$config" macos-terminal
)"
assert_contains "$doctor_output" "Managed Package diagnostics: ready" "doctor accepts installed canonical packages with canonical primary executables"

fast_status="$(
  XDG_STATE_HOME="$tmp_dir/status-state" \
  TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$diagnostic_inventory" \
    "$managed_packages" status "$config" macos-terminal
)"
assert_contains "$fast_status" "Managed Package status: canonical=25 missing=0 shadowed=0 pending=0" "status returns a fast canonical and pending summary"

sed '/^bat|/d' "$diagnostic_inventory/homebrew-formula" >"$diagnostic_inventory/homebrew-formula.tmp"
mv "$diagnostic_inventory/homebrew-formula.tmp" "$diagnostic_inventory/homebrew-formula"
set +e
missing_output="$(
  TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$diagnostic_inventory" \
    "$managed_packages" doctor "$config" macos-terminal 2>&1
)"
missing_status="$?"
set -e
[ "$missing_status" -ne 0 ] || fail "doctor fails when canonical package is missing"
pass "doctor fails when canonical package is missing"
assert_contains "$missing_output" "failure|bat|canonical package is missing" "doctor identifies missing canonical package"

printf '%s\n' "bat|user" >>"$diagnostic_inventory/homebrew-formula"
sed 's#^bat|/opt/homebrew/bin/bat|canonical$#bat|/usr/local/bin/bat|mise#' "$diagnostic_inventory/path" >"$diagnostic_inventory/path.tmp"
mv "$diagnostic_inventory/path.tmp" "$diagnostic_inventory/path"
set +e
primary_output="$(
  TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$diagnostic_inventory" \
    "$managed_packages" doctor "$config" macos-terminal 2>&1
)"
primary_status="$?"
set -e
[ "$primary_status" -ne 0 ] || fail "doctor fails when legacy executable is primary on managed PATH"
pass "doctor fails when legacy executable is primary on managed PATH"
assert_contains "$primary_output" "failure|bat|legacy executable is primary" "doctor identifies legacy primary executable"

awk '
  $0 == "bat|/usr/local/bin/bat|mise" {
    print "bat|/opt/homebrew/bin/bat|canonical"
    print "bat|/usr/local/bin/bat|mise"
    next
  }
  { print }
' "$diagnostic_inventory/path" >"$diagnostic_inventory/path.tmp"
mv "$diagnostic_inventory/path.tmp" "$diagnostic_inventory/path"
secondary_output="$(
  TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$diagnostic_inventory" \
    "$managed_packages" doctor "$config" macos-terminal
)"
assert_contains "$secondary_output" "advisory|bat|secondary duplicate remains at /usr/local/bin/bat" "doctor reports secondary duplicate as advisory"

override_dir="$home_dir/.config/zsh/path.d"
mkdir -p "$override_dir"
printf '%s\n' 'export PATH="/usr/local/bin:$PATH"' >"$override_dir/local.zsh"
override_output="$(
  HOME="$home_dir" \
    XDG_CONFIG_HOME="$home_dir/.config" \
    TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$diagnostic_inventory" \
    "$managed_packages" doctor "$config" macos-terminal
)"
assert_contains "$override_output" "advisory|machine-local-path-override|" "doctor reports path.d override as advisory without failing"
