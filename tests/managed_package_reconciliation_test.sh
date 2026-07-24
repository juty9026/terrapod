#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
managed_packages="$repo_root/dot_local/lib/terrapod/executable_managed-packages"
warnings_lib="$repo_root/dot_local/lib/terrapod/install-warnings.sh"
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
    *"$expected"*)
      pass "$label"
      ;;
    *)
      printf '%s\n' "$text" >&2
      fail "$label (missing: $expected)"
      ;;
  esac
}

assert_not_contains() {
  text="$1"
  unexpected="$2"
  label="$3"

  case "$text" in
    *"$unexpected"*)
      printf '%s\n' "$text" >&2
      fail "$label (unexpected: $unexpected)"
      ;;
    *)
      pass "$label"
      ;;
  esac
}

write_config() {
  path="$1"
  ai="$2"
  terminal_apps="$3"

  mkdir -p "${path%/*}"
  {
    printf '%s\n' '[data]'
    printf '%s\n' 'profile = "macos-terminal"'
    printf '%s\n' 'enableEditorStack = false'
    printf 'enableAiCliTools = %s\n' "$ai"
    printf '%s\n' 'enableDevelopmentWorkspace = false'
    printf 'enableMacosAppGroupTerminalApps = %s\n' "$terminal_apps"
    printf '%s\n' 'enableMacosAppGroupAutomation = false'
    printf '%s\n' 'enableMacosAppGroupLauncher = false'
    printf '%s\n' 'enableMacosAppGroupMonitoring = false'
    printf '%s\n' 'enableMacosAppGroupDevelopmentApps = false'
  } >"$path"
}

write_inventory() {
  directory="$1"
  mkdir -p "$directory"
  : >"$directory/homebrew-formula"
  : >"$directory/homebrew-cask"
  : >"$directory/mise"
  : >"$directory/aqua"
  : >"$directory/npm"
  : >"$directory/apt"
  : >"$directory/snap"
  : >"$directory/cargo"
  : >"$directory/pipx"
  : >"$directory/vendor"
  : >"$directory/path"
  : >"$directory/apps"
}

write_executor() {
  path="$1"
  fail_action="${2:-}"

  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' 'printf "%s\n" "$*" >>"$TERRAPOD_EXECUTOR_LOG"'
    printf 'if [ "$*" = %s ]; then exit 1; fi\n' "'$fail_action'"
  } >"$path"
  chmod +x "$path"
}

[ -x "$managed_packages" ] || fail "managed package deep module exists"
pass "managed package deep module exists"

config="$tmp_dir/config/chezmoi.toml"
HOME="$tmp_dir/home"
export HOME
XDG_CONFIG_HOME="$HOME/.config"
export XDG_CONFIG_HOME
mkdir -p "$HOME"
write_config "$config" false false

active_registry="$("$managed_packages" registry "$config" macos-terminal)"
if ! printf '%s\n' "$active_registry" | awk -F'|' 'NF != 10 { exit 1 }'; then
  fail "every ownership registry record has the explicit ten-field schema"
fi
pass "every ownership registry record has the explicit ten-field schema"
assert_contains "$active_registry" "bat|core|homebrew|bat|executable|bat|" "active registry includes Core Shell Stack"
assert_contains "$active_registry" "bun|runtime|mise|bun|executable|bun|" "active registry includes Bun"
assert_contains "$active_registry" "node|runtime|mise|node@24|executable|node|" "active registry includes Node.js 24"
assert_contains "$active_registry" "python|runtime|mise|python@3.13|executable|python3|" "active registry includes Python 3.13"
assert_contains "$active_registry" "uv|runtime|mise|uv|executable|uv|" "active registry includes uv"
assert_contains "$active_registry" "jetendard|font|vendor|kuskhan/jetendard|" "active registry keeps the Jetendard canonical exception"
assert_not_contains "$active_registry" "codex|optional-ai|" "disabled Optional AI Tool Stack is absent from active registry"
assert_not_contains "$active_registry" "ghostty|macos-app|" "disabled macOS App Group is absent from active registry"

write_config "$config" true true
active_registry="$("$managed_packages" registry "$config" macos-terminal)"
assert_contains "$active_registry" "codex|optional-ai|homebrew-cask|codex|" "enabled Optional AI Tool Stack is active"
assert_contains "$active_registry" "ghostty|macos-app|homebrew-cask|ghostty|app|Ghostty.app|" "enabled macOS App Group is active"

inventory="$tmp_dir/inventory"
write_inventory "$inventory"
printf '%s\n' "bat|user" >"$inventory/mise"
printf '%s\n' "bat|/opt/mise/bin/bat|mise" >"$inventory/path"
plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" "$managed_packages" plan "$config" macos-terminal)"
assert_contains "$plan" "remove|bat|mise|bat|mise uninstall bat" "planner uses exact registry identity for safe mise uninstall"

printf '%s\n' "bun|user|bun@1.2.0" >>"$inventory/mise"
plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" "$managed_packages" plan "$config" macos-terminal)"
assert_not_contains "$plan" "remove|bun|mise|" "planner preserves canonical mise runtime versions"

printf '%s\n' "bun|user|/custom/homebrew" >"$inventory/homebrew-formula"
plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" "$managed_packages" plan "$config" macos-terminal)"
assert_contains "$plan" "manual|bun|homebrew|bun|installation is not in the standard current-user-owned Homebrew prefix" "planner never uninstalls a nonstandard Homebrew package"
: >"$inventory/homebrew-formula"

printf '%s\n' "claude|user|verified-native-payload" >"$inventory/vendor"
plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" "$managed_packages" plan "$config" macos-terminal)"
assert_contains "$plan" 'remove|claude|vendor|claude|rm -f "$HOME/.local/bin/claude" && rm -rf "$HOME/.local/share/claude"' "planner uses the documented native Claude Code payload removal"
assert_not_contains "$plan" "claude uninstall" "planner never emits the nonexistent Claude Code uninstall command"
printf '%s\n' "codex|user|unresolved-signature" >"$inventory/vendor"
plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" "$managed_packages" plan "$config" macos-terminal)"
assert_contains "$plan" "manual|codex|vendor|codex|official uninstaller or ownership signature is unresolved" "planner keeps unverified vendor payload manual"
: >"$inventory/vendor"

printf '%s\n' "bat|/usr/local/bin/bat|unknown" >>"$inventory/path"
plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" "$managed_packages" plan "$config" macos-terminal)"
assert_contains "$plan" "manual|bat|path|/usr/local/bin/bat|provenance is unresolved" "unknown PATH copy is manual action"

override_dir="$tmp_dir/home/.config/zsh/path.d"
mkdir -p "$override_dir"
printf '%s\n' 'export PATH="/opt/legacy/bin:$PATH"' >"$override_dir/legacy.zsh"
plan="$(HOME="$tmp_dir/home" TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" "$managed_packages" plan "$config" macos-terminal)"
assert_contains "$plan" "manual|machine-local-path-override|path.d|$override_dir/legacy.zsh|" "path.d override remains advisory and is never removed"

executor="$tmp_dir/executor"
executor_log="$tmp_dir/executor.log"
write_executor "$executor"
: >"$executor_log"

decline_output="$(
  TERRAPOD_EXECUTOR_LOG="$executor_log" \
    TERRAPOD_MANAGED_PACKAGE_EXECUTOR="$executor" \
    XDG_STATE_HOME="$tmp_dir/state-decline" \
    "$managed_packages" execute "$plan" interactive </dev/null
)"
assert_contains "$decline_output" "Proceed with removing these legacy package installations? [y/N]" "migration renders one default-No prompt"
[ ! -s "$executor_log" ] || fail "default-No refusal executes no uninstall"
pass "default-No refusal executes no uninstall"
[ -f "$tmp_dir/state-decline/terrapod/install-warnings/managed-package-migration" ] ||
  fail "refusal records managed-package-migration warning"
pass "refusal records managed-package-migration warning"
marker_line_count="$(wc -l <"$tmp_dir/state-decline/terrapod/install-warnings/managed-package-migration" | tr -d ' ')"
[ "$marker_line_count" -eq 4 ] || fail "migration warning marker keeps the four-field single-line schema"
pass "migration warning marker keeps the four-field single-line schema"

: >"$executor_log"
noninteractive_output="$(
  TERRAPOD_EXECUTOR_LOG="$executor_log" \
    TERRAPOD_MANAGED_PACKAGE_EXECUTOR="$executor" \
    XDG_STATE_HOME="$tmp_dir/state-noninteractive" \
    "$managed_packages" execute "$plan" non-interactive
)"
assert_contains "$noninteractive_output" "pending" "non-interactive migration reports pending candidates"
[ ! -s "$executor_log" ] || fail "non-interactive migration executes no uninstall"
pass "non-interactive migration executes no uninstall"

safe_plan="$tmp_dir/safe.plan"
printf '%s\n' "remove|bat|mise|bat|mise uninstall bat" >"$safe_plan"
: >"$executor_log"
approve_output="$(
  printf '%s\n' y |
    TERRAPOD_EXECUTOR_LOG="$executor_log" \
      TERRAPOD_MANAGED_PACKAGE_EXECUTOR="$executor" \
      XDG_STATE_HOME="$tmp_dir/state-approve" \
      "$managed_packages" execute "$safe_plan" interactive
)"
assert_contains "$(cat "$executor_log")" "mise uninstall bat" "approved migration runs the exact uninstall action"
assert_contains "$approve_output" "removed: bat from mise" "successful exact uninstall is reported"
[ ! -e "$tmp_dir/state-approve/terrapod/install-warnings/managed-package-migration" ] ||
  fail "successful migration clears managed-package-migration warning"
pass "successful migration clears managed-package-migration warning"

partial_plan="$tmp_dir/partial.plan"
{
  printf '%s\n' "remove|codex|npm|@openai/codex|npm uninstall -g @openai/codex"
  printf '%s\n' "remove|bat|mise|bat|mise uninstall bat"
} >"$partial_plan"
write_executor "$executor" "npm uninstall -g @openai/codex"
: >"$executor_log"
partial_output="$(
  printf '%s\n' y |
    TERRAPOD_EXECUTOR_LOG="$executor_log" \
      TERRAPOD_MANAGED_PACKAGE_EXECUTOR="$executor" \
      XDG_STATE_HOME="$tmp_dir/state-partial" \
      "$managed_packages" execute "$partial_plan" interactive
)"
assert_contains "$partial_output" "pending: codex from npm" "partial removal failure remains pending"
assert_contains "$(cat "$executor_log")" "mise uninstall bat" "partial removal continues with remaining independent candidates"
[ -f "$tmp_dir/state-partial/terrapod/install-warnings/managed-package-migration" ] ||
  fail "partial removal failure records warning"
pass "partial removal failure records warning"

apt_inventory="$tmp_dir/apt-inventory"
write_inventory "$apt_inventory"
printf '%s\n' "ripgrep|manual|safe" >"$apt_inventory/apt"
apt_plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$apt_inventory" "$managed_packages" plan "$config" vps-shell)"
assert_contains "$apt_plan" "remove|ripgrep|apt|ripgrep|sudo apt-get remove -y ripgrep" "APT safe candidate requires exact manual no-cascade inventory result"

printf '%s\n' "ripgrep|manual|cascade" >"$apt_inventory/apt"
apt_plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$apt_inventory" "$managed_packages" plan "$config" vps-shell)"
assert_contains "$apt_plan" "manual|ripgrep|apt|ripgrep|APT simulation would remove additional packages" "APT cascade is manual action"

printf '%s\n' "git|manual|safe" >"$apt_inventory/apt"
apt_plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$apt_inventory" "$managed_packages" plan "$config" vps-shell)"
assert_contains "$apt_plan" "keep|git|apt|git|protected Terrapod prerequisite" "APT protected prerequisite is retained"

apt_exec_bin="$tmp_dir/apt-exec-bin"
apt_exec_log="$tmp_dir/apt-exec.log"
mkdir -p "$apt_exec_bin"
: >"$apt_exec_log"
{
  printf '%s\n' '#!/bin/sh'
  printf '%s\n' 'printf "%s\n" "Remv ripgrep" "Remv required-library"'
} >"$apt_exec_bin/apt-get"
chmod +x "$apt_exec_bin/apt-get"
{
  printf '%s\n' '#!/bin/sh'
  printf '%s\n' 'printf "%s\n" "$*" >>"$TERRAPOD_APT_EXEC_LOG"'
} >"$apt_exec_bin/sudo"
chmod +x "$apt_exec_bin/sudo"
apt_drift_plan="$tmp_dir/apt-drift.plan"
printf '%s\n' "remove|ripgrep|apt|ripgrep|sudo apt-get remove -y ripgrep" >"$apt_drift_plan"
apt_drift_output="$(
  printf '%s\n' y |
    PATH="$apt_exec_bin:$PATH" \
      TERRAPOD_APT_EXEC_LOG="$apt_exec_log" \
      XDG_STATE_HOME="$tmp_dir/state-apt-drift" \
      "$managed_packages" execute "$apt_drift_plan" interactive
)"
assert_contains "$apt_drift_output" "pending: ripgrep from apt" "APT transaction drift leaves removal pending"
[ ! -s "$apt_exec_log" ] || fail "APT transaction drift blocks sudo removal"
pass "APT transaction drift blocks sudo removal"

cask_inventory="$tmp_dir/cask-inventory"
write_inventory "$cask_inventory"
printf '%s\n' "Ghostty.app|unowned" >"$cask_inventory/apps"
cask_plan="$(TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$cask_inventory" "$managed_packages" plan "$config" macos-terminal)"
assert_contains "$cask_plan" "manual|ghostty|homebrew-cask|ghostty|Homebrew cask adoption did not complete; run brew install --cask --adopt ghostty" "failed Homebrew cask adoption remains a manual action"
assert_not_contains "$cask_plan" "Trash" "manual app migration never moves app bundles to Trash"

lock_dir="$tmp_dir/state-lock/terrapod/managed-package-migration.lock"
mkdir -p "$lock_dir"
if TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" \
  XDG_STATE_HOME="$tmp_dir/state-lock" \
  "$managed_packages" reconcile "$config" macos-terminal non-interactive >"$tmp_dir/lock.out" 2>&1; then
  fail "concurrent reconciliation is serialized by lock"
fi
assert_contains "$(cat "$tmp_dir/lock.out")" "managed package reconciliation is already running" "lock contention is reported"

rm -f "$override_dir/legacy.zsh"
write_inventory "$inventory"
printf '%s\n' "bat|user|bat@0.25.0" >"$inventory/mise"
verifying_executor="$tmp_dir/verifying-executor"
{
  printf '%s\n' '#!/bin/sh'
  printf '%s\n' ': >"$TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR/mise"'
} >"$verifying_executor"
chmod +x "$verifying_executor"
verified_output="$(
  printf '%s\n' y |
    TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" \
      TERRAPOD_MANAGED_PACKAGE_EXECUTOR="$verifying_executor" \
      XDG_STATE_HOME="$tmp_dir/state-verified" \
      "$managed_packages" reconcile "$config" macos-terminal interactive
)"
assert_contains "$verified_output" "Managed Package verification: ready" "reconciliation rescans after approved removals"
[ ! -e "$tmp_dir/state-verified/terrapod/install-warnings/managed-package-migration" ] ||
  fail "verified reconciliation clears the migration warning"
pass "verified reconciliation clears the migration warning"

write_inventory "$inventory"
first_repeat="$(
  TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" \
    XDG_STATE_HOME="$tmp_dir/state-repeat" \
    "$managed_packages" reconcile "$config" macos-terminal non-interactive
)"
second_repeat="$(
  TERRAPOD_MANAGED_PACKAGE_INVENTORY_DIR="$inventory" \
    XDG_STATE_HOME="$tmp_dir/state-repeat" \
    "$managed_packages" reconcile "$config" macos-terminal non-interactive
)"
assert_contains "$first_repeat" "Managed Package reconciliation: ready" "clean reconciliation is ready"
assert_contains "$second_repeat" "Managed Package reconciliation: ready" "repeated apply reconciliation is idempotent"

. "$warnings_lib"
terrapod_install_warning_is_category managed-package-migration ||
  fail "managed-package-migration is a stable warning category"
pass "managed-package-migration is a stable warning category"
