#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
selection="$repo_root/dot_local/lib/terrapod/executable_executable-selection"
make_tmp_dir

write_executable() {
  path="$1"
  mkdir -p "${path%/*}"
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' 'exit 0'
  } >"$path"
  chmod +x "$path"
}

prefix="$tmp_dir/homebrew"
mise_shims="$tmp_dir/mise/shims"
inventory="$tmp_dir/inventory"
path_dir="$tmp_dir/path"
mkdir -p "$inventory" "$path_dir"

cat >"$inventory/homebrew-formula" <<'EOF'
bat
btop
chezmoi
dust
duf
fastfetch
fd
fzf
gh
git
git-delta
gum
lazygit
lsd
mise
neovim
ripgrep
starship
zellij
zoxide
EOF
: >"$inventory/homebrew-cask"
cat >"$inventory/mise" <<'EOF'
bun
node
python
uv
EOF

while IFS=' ' read -r package command; do
  write_executable "$prefix/bin/$command"
  ln -s "$prefix/bin/$command" "$path_dir/$command"
done <<'EOF'
bat bat
btop btop
chezmoi chezmoi
dust dust
duf duf
fastfetch fastfetch
fd fd
fzf fzf
gh gh
git git
git-delta delta
gum gum
lazygit lazygit
lsd lsd
mise mise
neovim nvim
ripgrep rg
starship starship
zellij zellij
zoxide zoxide
EOF

while IFS=' ' read -r package command; do
  write_executable "$mise_shims/$command"
  ln -s "$mise_shims/$command" "$path_dir/$command"
done <<'EOF'
bun bun
node node
python python3
uv uv
EOF

run_selection() {
  mode="$1"
  shift
  HOME="$tmp_dir/home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    PATH="$path_dir:/usr/bin:/bin" \
    "$selection" "$mode" macos-terminal false false "$@"
}

ready_output="$(run_selection doctor)"
assert_contains "$ready_output" "Canonical executable selection: ready" \
  "doctor accepts provider-installed commands that resolve to the canonical file"

rm -f "$path_dir/bat"
ln -s "$prefix/bin/bat" "$path_dir/bat"
same_file_output="$(run_selection doctor)"
assert_contains "$same_file_output" "Canonical executable selection: ready" \
  "doctor accepts a different symlink path that resolves to the canonical file"

rm -f "$path_dir/bat"
write_executable "$path_dir/bat"
set +e
shadow_output="$(run_selection doctor 2>&1)"
shadow_status="$?"
set -e
[ "$shadow_status" -eq 0 ] || fail "doctor keeps executable shadowing advisory-only"
pass "doctor keeps executable shadowing advisory-only"
assert_contains "$shadow_output" "advisory - bat resolves to $path_dir/bat" \
  "doctor reports the primary non-canonical executable"
assert_contains "$shadow_output" "canonical: $prefix/bin/bat" \
  "doctor reports the expected canonical executable"
assert_contains "$shadow_output" "Adjust PATH or remove the other installation manually, then rerun 'tpod doctor'." \
  "doctor gives provenance-neutral manual guidance"

rm -f "$path_dir/bat"
set +e
unavailable_output="$(run_selection doctor 2>&1)"
unavailable_status="$?"
set -e
[ "$unavailable_status" -ne 0 ] || fail "doctor fails when command -v cannot resolve an installed canonical executable"
pass "doctor fails when command -v cannot resolve an installed canonical executable"
assert_contains "$unavailable_output" "failure - bat is unavailable on PATH" \
  "doctor distinguishes an unavailable command"

ln -s "$prefix/bin/bat" "$path_dir/bat"
rm -f "$prefix/bin/bat"
set +e
missing_executable_output="$(run_selection doctor 2>&1)"
missing_executable_status="$?"
set -e
[ "$missing_executable_status" -ne 0 ] || fail "doctor fails when the canonical executable is missing"
pass "doctor fails when the canonical executable is missing"
assert_contains "$missing_executable_output" "failure - bat canonical executable is missing" \
  "doctor reports the missing canonical executable"

write_executable "$prefix/bin/bat"
sed '/^bat$/d' "$inventory/homebrew-formula" >"$inventory/homebrew-formula.tmp"
mv "$inventory/homebrew-formula.tmp" "$inventory/homebrew-formula"
set +e
missing_package_output="$(run_selection doctor 2>&1)"
missing_package_status="$?"
set -e
[ "$missing_package_status" -ne 0 ] || fail "doctor fails when the declared provider package is missing"
pass "doctor fails when the declared provider package is missing"
assert_contains "$missing_package_output" "failure - bat is not installed through Homebrew" \
  "doctor reports the missing declared provider package"

printf '%s\n' bat btop chezmoi dust duf fastfetch fd fzf gh git git-delta gum lazygit lsd mise neovim ripgrep starship zellij zoxide >"$inventory/homebrew-formula"
rm -f "$path_dir/bat"
write_executable "$path_dir/bat"
apply_output="$(run_selection apply)"
assert_contains "$apply_output" "advisory - bat resolves to $path_dir/bat" \
  "apply prints selection advisories"
assert_not_contains "$apply_output" "Canonical executable selection: ready" \
  "apply stays quiet when no selection concern exists"

: >"$inventory/homebrew-cask"
ai_disabled_output="$(run_selection doctor)"
assert_not_contains "$ai_disabled_output" "agy" \
  "disabled Optional AI Tool Stack is excluded from executable selection"

set +e
ai_enabled_output="$(
  HOME="$tmp_dir/home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    PATH="$path_dir:/usr/bin:/bin" \
    "$selection" doctor macos-terminal true false 2>&1
)"
ai_enabled_status="$?"
set -e
[ "$ai_enabled_status" -ne 0 ] || fail "enabled Optional AI Tool Stack participates in readiness"
pass "enabled Optional AI Tool Stack participates in readiness"
assert_contains "$ai_enabled_output" "failure - antigravity-cli is not installed through Homebrew Cask" \
  "enabled Optional AI Tool Stack checks its declared casks"
assert_contains "$ai_enabled_output" "failure - claude-code is not installed through the Claude Code installer" \
  "enabled Optional AI Tool Stack checks Claude Code against its vendor installer"

claude_home="$tmp_dir/claude-home"
claude_path_dir="$tmp_dir/claude-path"
mkdir -p "$claude_path_dir"
printf '%s\n' claude-code >"$inventory/claude-installer"
write_executable "$claude_home/.local/bin/claude"
ln -s "$claude_home/.local/bin/claude" "$claude_path_dir/claude"

run_claude_selection() {
  HOME="$claude_home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    PATH="$claude_path_dir:/usr/bin:/bin" \
    "$selection" doctor macos-terminal true false 2>&1 || true
}

claude_canonical_output="$(run_claude_selection)"
assert_not_contains "$claude_canonical_output" "claude-code" \
  "a canonical Claude Code install raises no executable selection concern"

claude_shadow_dir="$tmp_dir/claude-shadow"
mkdir -p "$claude_shadow_dir"
write_executable "$claude_shadow_dir/claude"
rm -f "$claude_path_dir/claude"
ln -s "$claude_shadow_dir/claude" "$claude_path_dir/claude"
claude_shadow_output="$(run_claude_selection)"
assert_contains "$claude_shadow_output" "advisory - claude-code resolves to $claude_path_dir/claude" \
  "a shadowed Claude Code install is advisory"
assert_contains "$claude_shadow_output" "canonical: $claude_home/.local/bin/claude" \
  "the Claude Code advisory names the vendor installer path"

rm -f "$inventory/claude-installer"
claude_missing_output="$(run_claude_selection)"
assert_contains "$claude_missing_output" "failure - claude-code is not installed through the Claude Code installer" \
  "an absent Claude Code install names the vendor installer"

status_output="$(run_selection status)"
assert_contains "$status_output" "Executable selection:" \
  "status exposes executable selection state"

# The canonical path appears in whichever concern the record raises, so these
# assertions observe the resolved prefix without depending on what the host
# machine has actually installed.
assert_canonical_prefix() {
  profile="$1"
  arch="$2"
  translated="$3"
  expected_prefix="$4"
  rejected_prefix="$5"
  label="$6"

  set +e
  prefix_output="$(
    HOME="$tmp_dir/home" \
      TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
      TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
      TERRAPOD_MACHINE_ARCH="$arch" \
      TERRAPOD_DARWIN_TRANSLATED="$translated" \
      PATH="$path_dir:/usr/bin:/bin" \
      "$selection" doctor "$profile" false false 2>&1
  )"
  set -e

  assert_contains "$prefix_output" "$expected_prefix/bin/bat" "$label"
  assert_not_contains "$prefix_output" "$rejected_prefix/bin/bat" "$label rejects the other prefix"
}

assert_canonical_prefix macos-terminal x86_64 1 /opt/homebrew /usr/local \
  "Rosetta on Apple Silicon resolves canonical executables under /opt/homebrew"
assert_canonical_prefix macos-terminal x86_64 0 /usr/local /opt/homebrew \
  "native Intel macOS resolves canonical executables under /usr/local"
assert_canonical_prefix macos-terminal arm64 0 /opt/homebrew /usr/local \
  "native Apple Silicon resolves canonical executables under /opt/homebrew"
assert_canonical_prefix vps-shell x86_64 0 /home/linuxbrew/.linuxbrew /opt/homebrew \
  "VPS Shell resolves canonical executables under the Linuxbrew prefix"

set +e
missing_lib_output="$(
  HOME="$tmp_dir/home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    TERRAPOD_HOMEBREW_PREFIX_LIB="$tmp_dir/absent-homebrew-prefix.sh" \
    PATH="$path_dir:/usr/bin:/bin" \
    "$selection" doctor macos-terminal false false 2>&1
)"
missing_lib_status="$?"
set -e
[ "$missing_lib_status" -ne 0 ] ||
  fail "a missing Homebrew prefix library fails instead of reporting absent executables"
pass "a missing Homebrew prefix library fails instead of reporting absent executables"
assert_contains "$missing_lib_output" "Homebrew prefix library is unavailable" \
  "a missing Homebrew prefix library is diagnosed distinctly"
assert_not_contains "$missing_lib_output" "canonical executable is missing" \
  "a missing Homebrew prefix library is not reported as missing executables"

integration_bin="$tmp_dir/integration-bin"
integration_config="$tmp_dir/integration-config/chezmoi.toml"
selection_log="$tmp_dir/selection.log"
mkdir -p "$integration_bin" "${integration_config%/*}"
cat >"$integration_config" <<'EOF'
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
enableMacosAppGroupMobileDev = false
EOF
cat >"$integration_bin/chezmoi" <<'EOF'
#!/bin/sh
case " $* " in
  *" managed "*)
    printf '%s\n' ".local/bin/terrapod" ".local/bin/tpod"
    ;;
esac
EOF
chmod +x "$integration_bin/chezmoi"
cat >"$integration_bin/executable-selection" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$TERRAPOD_EXECUTABLE_SELECTION_LOG"
printf '%s\n' "  advisory - bat resolves to /legacy/bin/bat"
exit 0
EOF
chmod +x "$integration_bin/executable-selection"

cat >"$integration_bin/executable-selection-failing" <<'EOF'
#!/bin/sh
printf '%s\n' "  failure - Homebrew prefix library is unavailable" >&2
exit 1
EOF
chmod +x "$integration_bin/executable-selection-failing"

integration_output="$(
  HOME="$tmp_dir/integration-home" \
    TERRAPOD_PROFILE=macos-terminal \
    TERRAPOD_CHEZMOI_CONFIG="$integration_config" \
    TERRAPOD_EXECUTABLE_SELECTION_HELPER="$integration_bin/executable-selection" \
    TERRAPOD_EXECUTABLE_SELECTION_LOG="$selection_log" \
    PATH="$integration_bin:/usr/bin:/bin" \
    "$repo_root/dot_local/bin/executable_terrapod" apply
)"
assert_contains "$integration_output" "advisory - bat resolves to /legacy/bin/bat" \
  "tpod apply prints executable selection advisories after installation"
assert_contains "$(cat "$selection_log")" "apply macos-terminal false false" \
  "tpod apply invokes executable selection with effective stack state"

run_integration_terrapod() {
  helper="$1"
  command_path="$2"
  shift 2
  HOME="$tmp_dir/integration-home" \
    TERRAPOD_PROFILE=macos-terminal \
    TERRAPOD_CHEZMOI_CONFIG="$integration_config" \
    TERRAPOD_EXECUTABLE_SELECTION_HELPER="$helper" \
    TERRAPOD_EXECUTABLE_SELECTION_LOG="$selection_log" \
    PATH="$integration_bin:/usr/bin:/bin" \
    "$command_path" "$@" 2>&1
}

run_integration_command() {
  helper="$1"
  shift
  run_integration_terrapod "$helper" "$repo_root/dot_local/bin/executable_terrapod" "$@"
}

run_absent_helper_command() {
  run_integration_command "$absent_helper" "$@"
}

failing_helper="$integration_bin/executable-selection-failing"
absent_helper="$integration_bin/executable-selection-absent"

set +e
failing_apply_output="$(run_integration_command "$failing_helper" apply)"
failing_apply_status="$?"
set -e
assert_contains "$failing_apply_output" "Warning: executable selection helper failed" \
  "tpod apply distinguishes a helper that ran and failed"
assert_not_contains "$failing_apply_output" "executable selection helper is missing" \
  "tpod apply does not report a failing helper as missing"
[ "$failing_apply_status" -ne 0 ] ||
  fail "tpod apply exits non-zero when the executable selection helper fails"
pass "tpod apply exits non-zero when the executable selection helper fails"

set +e
absent_apply_output="$(run_absent_helper_command apply)"
absent_apply_status="$?"
set -e
assert_contains "$absent_apply_output" "Warning: executable selection helper is missing" \
  "tpod apply reports an absent helper as missing"
[ "$absent_apply_status" -ne 0 ] ||
  fail "tpod apply exits non-zero when the executable selection helper is missing"
pass "tpod apply exits non-zero when the executable selection helper is missing"

set +e
ready_apply_output="$(run_integration_command "$integration_bin/executable-selection" apply)"
ready_apply_status="$?"
set -e
assert_not_contains "$ready_apply_output" "executable selection helper" \
  "tpod apply stays quiet when the executable selection helper succeeds"
[ "$ready_apply_status" -eq 0 ] ||
  fail "tpod apply succeeds when the executable selection helper succeeds"
pass "tpod apply succeeds when the executable selection helper succeeds"

set +e
failing_status_output="$(run_integration_command "$failing_helper" status)"
failing_status_status="$?"
set -e
assert_contains "$failing_status_output" "Warning: executable selection helper failed" \
  "tpod status distinguishes a helper that ran and failed"
[ "$failing_status_status" -eq 0 ] ||
  fail "tpod status stays informational when the executable selection helper fails"
pass "tpod status stays informational when the executable selection helper fails"

set +e
absent_status_output="$(run_absent_helper_command status)"
absent_status_status="$?"
set -e
assert_contains "$absent_status_output" "Warning: executable selection helper is missing" \
  "tpod status reports an absent helper as missing"
[ "$absent_status_status" -eq 0 ] ||
  fail "tpod status stays informational when the executable selection helper is missing"
pass "tpod status stays informational when the executable selection helper is missing"
