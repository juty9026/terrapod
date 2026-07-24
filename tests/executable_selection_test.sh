#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
selection="$repo_root/dot_local/lib/terrapod/executable_executable-selection"
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

assert_not_contains() {
  text="$1"
  unexpected="$2"
  label="$3"
  case "$text" in
    *"$unexpected"*)
      printf '%s\n' "$text" >&2
      fail "$label (unexpected: $unexpected)"
      ;;
    *) pass "$label" ;;
  esac
}

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

status_output="$(run_selection status)"
assert_contains "$status_output" "Executable selection:" \
  "status exposes executable selection state"

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
exit 1
EOF
chmod +x "$integration_bin/executable-selection"

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
