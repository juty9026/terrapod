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

# The stand-in for mise answers from fixture files so a test can describe an
# activation mode without installing a runtime. Both files start empty, which
# is the machine where mise has no active declaration for anything: 'bin-paths'
# says nothing and 'which' reports the tool is not currently active.
mise_bin_paths_file="$tmp_dir/mise-bin-paths"
mise_bin_paths_log="$tmp_dir/mise-bin-paths.log"
mise_which_dir="$tmp_dir/mise-which"
mkdir -p "$mise_which_dir"
: >"$mise_bin_paths_file"
: >"$mise_bin_paths_log"

# 'mise ls --prunable' is the verdict source for the payload advisory. The
# fixture file's three states are the three the helper has to tell apart: the
# file missing is a mise too old for the flag, empty is a machine with nothing
# to prune, and lines are payloads.
mise_prunable_file="$tmp_dir/mise-prunable"

# 'mise where' turns a tool@version into an install path, and 'du' turns that
# path into a size. Both are stubbed so a size assertion is exact instead of
# depending on the filesystem's block accounting.
mise_where_dir="$tmp_dir/mise-where"
payload_size_dir="$tmp_dir/payload-sizes"
mkdir -p "$mise_where_dir" "$payload_size_dir"

# Tool names carry ':' and '/', neither of which can be a fixture filename.
payload_fixture_key() {
  printf '%s' "$1" | tr ':/@' '___'
}

cat >"$prefix/bin/mise" <<EOF
#!/bin/sh
case "\${1:-}" in
  bin-paths)
    printf '%s\n' bin-paths >>"$mise_bin_paths_log"
    cat "$mise_bin_paths_file"
    ;;
  which)
    if [ -f "$mise_which_dir/\${2:-}" ]; then
      cat "$mise_which_dir/\${2:-}"
    else
      printf '%s is a mise bin however it is not currently active\n' "\${2:-}" >&2
      exit 1
    fi
    ;;
  ls)
    if [ ! -f "$mise_prunable_file" ]; then
      printf '%s\n' "error: unexpected argument '--prunable' found" >&2
      exit 2
    fi
    cat "$mise_prunable_file"
    ;;
  where)
    where_key="\$(printf '%s' "\${2:-}" | tr ':/@' '___')"
    if [ -f "$mise_where_dir/\$where_key" ]; then
      cat "$mise_where_dir/\$where_key"
    else
      exit 1
    fi
    ;;
esac
exit 0
EOF
chmod +x "$prefix/bin/mise"

# 'du -sk <path>' prints kilobytes then the path. The fixture is keyed by the
# path's basename so a test states a payload's size in one line.
cat >"$path_dir/du" <<EOF
#!/bin/sh
du_path="\${2:-}"
du_key="\${du_path##*/}"
if [ -f "$payload_size_dir/\$du_key" ]; then
  printf '%s\t%s\n' "\$(cat "$payload_size_dir/\$du_key")" "\$du_path"
else
  printf '%s\t%s\n' 0 "\$du_path"
fi
EOF
chmod +x "$path_dir/du"

run_selection() {
  mode="$1"
  shift
  HOME="$tmp_dir/home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    TERRAPOD_MANAGED_PATH="$path_dir:/usr/bin:/bin" \
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
    TERRAPOD_MANAGED_PATH="$path_dir:/usr/bin:/bin" \
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
    TERRAPOD_MANAGED_PATH="$claude_path_dir:/usr/bin:/bin" \
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

# With no TERRAPOD_MANAGED_PATH and a zsh that refuses to run, the probe cannot
# produce a managed PATH. The verdict then falls back to the inherited PATH and
# has to say so, because that is the one case where it depends on the caller.
failing_probe_dir="$tmp_dir/failing-probe"
mkdir -p "$failing_probe_dir"
cat >"$failing_probe_dir/zsh" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod +x "$failing_probe_dir/zsh"

inherited_path="$failing_probe_dir:$path_dir:/usr/bin:/bin"
fallback_output="$(
  HOME="$tmp_dir/home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    PATH="$inherited_path" \
    "$selection" doctor macos-terminal false false 2>&1 || true
)"
assert_contains "$fallback_output" \
  "note - executable selection judged against the inherited PATH: $inherited_path" \
  "a failed login-shell probe names the PATH the verdict was computed from"
assert_not_contains "$fallback_output" "failure -" \
  "the inherited-PATH fallback still resolves the declared commands"

managed_output="$(run_selection doctor)"
assert_not_contains "$managed_output" "inherited PATH" \
  "a computed managed PATH says nothing about inheriting one"

status_output="$(run_selection status)"
assert_contains "$status_output" "Executable selection:" \
  "status exposes executable selection state"

# Canonical selection for a Development Runtime declaration is a set: the
# manager's active target, the directories it reports as its bin paths, and the
# shims path. The same machine has to stay canonical whether it runs
# 'mise activate' or 'mise activate --shims'.
mise_bin_dir="$tmp_dir/mise-installs/bin"
mise_active_dir="$tmp_dir/mise-active/bin"
mise_path_dir="$tmp_dir/mise-path"
mkdir -p "$mise_path_dir"
for runtime_command in bun node python3 uv; do
  write_executable "$mise_bin_dir/$runtime_command"
  write_executable "$mise_active_dir/$runtime_command"
done
printf '%s\n' "$mise_bin_dir" >"$mise_bin_paths_file"

run_mise_selection() {
  HOME="$tmp_dir/home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    TERRAPOD_MANAGED_PATH="$mise_path_dir:$path_dir:/usr/bin:/bin" \
    PATH="$mise_path_dir:$path_dir:/usr/bin:/bin" \
    "$selection" doctor macos-terminal false false 2>&1 || true
}

# Puts the runtime commands ahead of everything else, resolving to the given
# directory, so each accepted location can be judged on its own.
select_runtimes_from() {
  for runtime_command in bun node python3 uv; do
    rm -f "$mise_path_dir/$runtime_command"
    ln -s "$1/$runtime_command" "$mise_path_dir/$runtime_command"
  done
}

select_runtimes_from "$mise_bin_dir"
: >"$mise_bin_paths_log"
bin_paths_output="$(run_mise_selection)"
assert_not_contains "$bin_paths_output" "advisory - node" \
  "a runtime resolving through a reported mise bin directory is canonical"

# One process, one probe. The directory set feeds four Development Runtime
# declarations, and every read of it happens inside a pipeline, so a cache that
# fills itself on first read fills a subshell instead.
bin_paths_calls="$(grep -c . "$mise_bin_paths_log")"
[ "$bin_paths_calls" = 1 ] ||
  fail "the check asks mise for its bin paths once per process (asked $bin_paths_calls times)"
pass "the check asks mise for its bin paths once per process"

select_runtimes_from "$mise_shims"
shims_mode_output="$(run_mise_selection)"
assert_not_contains "$shims_mode_output" "advisory - node" \
  "the shims path stays canonical once the bin directories are accepted"

for runtime_command in bun node python3 uv; do
  printf '%s\n' "$mise_active_dir/$runtime_command" >"$mise_which_dir/$runtime_command"
done
select_runtimes_from "$mise_active_dir"
active_target_output="$(run_mise_selection)"
assert_not_contains "$active_target_output" "advisory - node" \
  "the target mise reports as active is canonical"

mise_rogue_dir="$tmp_dir/mise-rogue"
write_executable "$mise_rogue_dir/node"
rm -f "$mise_path_dir/node"
ln -s "$mise_rogue_dir/node" "$mise_path_dir/node"
rogue_runtime_output="$(run_mise_selection)"
assert_contains "$rogue_runtime_output" "advisory - node resolves to $mise_path_dir/node" \
  "a runtime outside every accepted location stays an advisory"
assert_contains "$rogue_runtime_output" "canonical: $mise_active_dir/node" \
  "the advisory names the active target as the single canonical location"

rm -f "$mise_which_dir/node"
inactive_runtime_output="$(run_mise_selection)"
assert_contains "$inactive_runtime_output" "canonical: $mise_shims/node" \
  "the advisory names the shims path when mise reports nothing active"

: >"$mise_bin_paths_file"
rm -f "$mise_which_dir"/*

# A shim is a symlink to the mise binary, never to the executable it ends up
# running, so neither string equality nor -ef can see through it. These four
# cases are the whole of that indirection: the shim forwards because mise has
# nothing active, or it forwards to what mise does have active, and either
# destination can be canonical or not.
ln -s "$prefix/bin/mise" "$mise_shims/rg"
shim_path="$mise_shims:$path_dir:/usr/bin:/bin"

run_shim_selection() {
  HOME="$tmp_dir/home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    TERRAPOD_MANAGED_PATH="$shim_path" \
    PATH="$shim_path" \
    "$selection" doctor macos-terminal false false 2>&1 || true
}

passthrough_output="$(run_shim_selection)"
assert_not_contains "$passthrough_output" "advisory - ripgrep" \
  "a shim mise cannot activate is judged by the canonical file it forwards to"

# The same fall-through, landing somewhere else. Without this the fix would
# also swallow the findings the check exists for.
rogue_shim_dir="$tmp_dir/rogue-shim-target"
write_executable "$rogue_shim_dir/rg"
rm -f "$path_dir/rg"
ln -s "$rogue_shim_dir/rg" "$path_dir/rg"
rogue_passthrough_output="$(run_shim_selection)"
assert_contains "$rogue_passthrough_output" "advisory - ripgrep resolves to $mise_shims/rg" \
  "a shim forwarding to a non-canonical file stays an advisory"
assert_contains "$rogue_passthrough_output" "canonical: $prefix/bin/rg" \
  "the shim advisory names the path the reader has to act on and the canonical one"

rm -f "$path_dir/rg"
ln -s "$prefix/bin/rg" "$path_dir/rg"

printf '%s\n' "$prefix/bin/rg" >"$mise_which_dir/rg"
active_canonical_output="$(run_shim_selection)"
assert_not_contains "$active_canonical_output" "advisory - ripgrep" \
  "a shim mise activates onto the canonical file raises no advisory"

printf '%s\n' "$rogue_shim_dir/rg" >"$mise_which_dir/rg"
active_rogue_output="$(run_shim_selection)"
assert_contains "$active_rogue_output" "advisory - ripgrep resolves to $mise_shims/rg" \
  "a shim mise activates onto its own payload is an advisory when Homebrew is canonical"

rm -f "$mise_shims/rg" "$mise_which_dir/rg"

rm -f "$path_dir/bat"
ln -s "$prefix/bin/bat" "$path_dir/bat"

# The output shape #237 is about. Fifteen commands resolving through one
# directory share one cause, so they collapse into a single finding with a
# wrapped package list, the lone genuine finding keeps the two-line form that
# makes it readable beside them, and the invariant guidance sentence prints
# once for the whole block instead of once per package.
#
# Advisory lines are asserted whole rather than as substrings, because a group
# line and a per-package line differ only in their tail.
assert_line() {
  if ! printf '%s\n' "$1" | grep -F -x -e "$2" >/dev/null; then
    printf '%s\n' "missing line: $2" >&2
    printf '%s\n' "$1" | sed 's/^/  /' >&2
    fail "$3"
  fi
  pass "$3"
}

assert_occurrences() {
  occurrences="$(printf '%s\n' "$1" | grep -c -F -e "$2" || true)"
  [ "$occurrences" = "$3" ] || fail "$4 (found $occurrences)"
  pass "$4"
}

group_dir="$tmp_dir/group-bin"
single_dir="$tmp_dir/single-bin"
mkdir -p "$group_dir" "$single_dir"
for group_command in bat dust duf fastfetch fd fzf gh delta lazygit lsd nvim rg starship zellij zoxide; do
  write_executable "$group_dir/$group_command"
done
write_executable "$single_dir/git"

run_group_selection() {
  HOME="$tmp_dir/home" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    TERRAPOD_MANAGED_PATH="$group_dir:$single_dir:$path_dir:/usr/bin:/bin" \
    PATH="$group_dir:$single_dir:$path_dir:/usr/bin:/bin" \
    "$selection" doctor macos-terminal false false 2>&1 || true
}

group_output="$(run_group_selection)"
assert_line "$group_output" "  advisory - 15 commands resolve through $group_dir" \
  "advisories that share a resolved directory collapse into one finding"
assert_line "$group_output" \
  "             bat, dust, duf, fastfetch, fd, fzf, gh, git-delta, lazygit," \
  "the grouped finding wraps its package list instead of running it off the line"
assert_line "$group_output" "             lsd, neovim, ripgrep, starship, zellij, zoxide" \
  "the wrapped package list carries every package in the group"
assert_line "$group_output" "             canonical: $prefix/bin" \
  "a group whose canonical paths share a directory names that directory once"
assert_not_contains "$group_output" "advisory - bat resolves to" \
  "a grouped package does not also print an advisory header of its own"
assert_line "$group_output" "  advisory - git resolves to $single_dir/git" \
  "a lone finding keeps the per-package form"
assert_line "$group_output" "             canonical: $prefix/bin/git" \
  "a lone finding still names its canonical executable"
assert_line "$group_output" \
  "  Adjust PATH or remove the other installation manually, then rerun 'tpod doctor'." \
  "the guidance sentence closes the advisory block"
assert_occurrences "$group_output" \
  "Adjust PATH or remove the other installation manually, then rerun 'tpod doctor'." 1 \
  "the guidance sentence prints once for the whole block, not once per finding"

# ADR 0012 requires the advisory to carry both paths, so a group collapses to a
# single canonical directory only when its members actually share one. A
# Development Runtime declaration resolving through the same directory as the
# Homebrew ones does not, and the group falls back to a line per package.
write_executable "$group_dir/node"
mixed_canonical_output="$(run_group_selection)"
assert_line "$mixed_canonical_output" "  advisory - 16 commands resolve through $group_dir" \
  "a Development Runtime declaration joins the group of its resolved directory"
assert_line "$mixed_canonical_output" "             canonical (bat): $prefix/bin/bat" \
  "a group with unshared canonical directories names each package's canonical path"
assert_line "$mixed_canonical_output" "             canonical (node): $mise_shims/node" \
  "the per-package fallback keeps each provider's own canonical location"
assert_occurrences "$mixed_canonical_output" "             canonical (" 16 \
  "the per-package fallback covers every package in the group"
rm -f "$group_dir/node"

# The canonical location appears in whichever concern the record raises -- a
# missing-executable failure, a per-package advisory, or a group's shared
# canonical directory -- so these assertions observe the resolved prefix
# without depending on what the host machine has actually installed or on
# which shape the finding takes.
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
      TERRAPOD_MANAGED_PATH="$path_dir:/usr/bin:/bin" \
      PATH="$path_dir:/usr/bin:/bin" \
      "$selection" doctor "$profile" false false 2>&1
  )"
  set -e

  assert_contains "$prefix_output" "$expected_prefix/bin" "$label"
  assert_not_contains "$prefix_output" "$rejected_prefix/bin" "$label rejects the other prefix"
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
    TERRAPOD_MANAGED_PATH="$path_dir:/usr/bin:/bin" \
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

# Payloads no configuration mise has tracked can reach. mise owns the
# computation (ADR 0021): it judges per installed version against every config
# it has seen, so a version a project-local mise.toml selects is not counted.
leftover_data_dir="$tmp_dir/mise-data"
leftover_installs="$leftover_data_dir/installs"

run_leftover_selection() {
  leftover_mode="$1"
  leftover_path="${2:-$path_dir}"
  HOME="$tmp_dir/home" \
    MISE_DATA_DIR="$leftover_data_dir" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    TERRAPOD_MANAGED_PATH="$leftover_path:/usr/bin:/bin" \
    PATH="$leftover_path:/usr/bin:/bin" \
    "$selection" "$leftover_mode" macos-terminal false false 2>&1 || true
}

absent_installs_output="$(run_leftover_selection doctor)"
assert_not_contains "$absent_installs_output" "mise payload" \
  "an absent mise installs directory says nothing about prunable payloads"
assert_contains "$absent_installs_output" "Canonical executable selection: ready" \
  "an absent mise installs directory leaves the selection verdict alone"

mkdir -p "$leftover_installs"

# The installs directory exists but the fixture file does not, which is the
# mise too old to know '--prunable'. Skipping is the whole contract here: the
# check exists to ask mise what is reachable, so a mise that cannot answer
# leaves nothing to report.
unsupported_flag_output="$(run_leftover_selection doctor)"
assert_not_contains "$unsupported_flag_output" "mise payload" \
  "a mise that rejects --prunable produces no payload advisory"
assert_contains "$unsupported_flag_output" "Canonical executable selection: ready" \
  "a mise that rejects --prunable does not fail the selection verdict"

: >"$mise_prunable_file"
nothing_prunable_output="$(run_leftover_selection doctor)"
assert_not_contains "$nothing_prunable_output" "mise payload" \
  "a machine with nothing to prune says nothing"

cat >"$mise_prunable_file" <<'EOF'
aqua:sharkdp/bat              0.26.1
npm:pnpm                      10.33.3
node                          22.22.2
EOF
leftover_output="$(run_leftover_selection doctor)"
assert_line "$leftover_output" \
  "  advisory - 3 mise payloads can be pruned" \
  "prunable payloads are reported as one counted advisory"
assert_line "$leftover_output" \
  "             'mise prune --tools' reclaims the disk; Terrapod does not remove them." \
  "the payload advisory carries its own guidance line"
assert_not_contains "$leftover_output" \
  "Adjust PATH or remove the other installation manually" \
  "the payload advisory does not borrow the selection block's guidance sentence"
assert_not_contains "$leftover_output" "aqua:sharkdp/bat" \
  "the default advisory names no payload"
assert_contains "$leftover_output" "Canonical executable selection: ready" \
  "prunable payloads are a separate finding from executable selection"

cat >"$mise_prunable_file" <<'EOF'
node                          22.22.2
EOF
single_payload_output="$(run_leftover_selection doctor)"
assert_line "$single_payload_output" \
  "  advisory - 1 mise payload can be pruned" \
  "one prunable payload is reported in the singular"

# Decision guard: disk a user chose to keep is not a readiness problem. Without
# this assertion nothing stops a later change from setting the issue flag here.
leftover_status_output="$(run_leftover_selection status)"
assert_contains "$leftover_status_output" "Executable selection: ready" \
  "tpod status stays ready on a machine whose only finding is prunable payloads"
assert_not_contains "$leftover_status_output" "mise payload" \
  "status leaves the payload advisory to apply and doctor"

leftover_apply_output="$(run_leftover_selection apply)"
assert_line "$leftover_apply_output" \
  "  advisory - 1 mise payload can be pruned" \
  "apply reports prunable payloads"

# The detail listing. Sizes come from stubbed 'mise where' and 'du', so the
# rendered megabytes are exact rather than filesystem-dependent.
cat >"$mise_prunable_file" <<'EOF'
aqua:sharkdp/bat              0.26.1
npm:pnpm                      10.33.3
node                          22.22.2
rust                          stable (symlink)
EOF
printf '%s\n' "$tmp_dir/payloads/bat" >"$mise_where_dir/aqua_sharkdp_bat_0.26.1"
printf '%s\n' "$tmp_dir/payloads/pnpm" >"$mise_where_dir/npm_pnpm_10.33.3"
printf '%s\n' "$tmp_dir/payloads/node" >"$mise_where_dir/node_22.22.2"
printf '%s\n' "$tmp_dir/payloads/rust" >"$mise_where_dir/rust_stable"
mkdir -p "$tmp_dir/payloads/bat" "$tmp_dir/payloads/pnpm" \
  "$tmp_dir/payloads/node" "$tmp_dir/payloads/rust"
printf '%s\n' 12288 >"$payload_size_dir/bat"
printf '%s\n' 4096 >"$payload_size_dir/pnpm"
printf '%s\n' 62464 >"$payload_size_dir/node"
printf '%s\n' 0 >"$payload_size_dir/rust"

run_payload_detail() {
  HOME="$tmp_dir/home" \
    MISE_DATA_DIR="$leftover_data_dir" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    TERRAPOD_MANAGED_PATH="$path_dir:/usr/bin:/bin" \
    PATH="$path_dir:/usr/bin:/bin" \
    "$selection" doctor macos-terminal false false true 2>&1 || true
}

payload_detail_output="$(run_payload_detail)"
assert_line "$payload_detail_output" \
  "  advisory - 4 mise payloads can be pruned (77.0 MB)" \
  "the detailed advisory carries the total size"
assert_line "$payload_detail_output" \
  "             aqua:sharkdp/bat@0.26.1   12.0 MB" \
  "the detailed advisory lists each payload with its size"
assert_line "$payload_detail_output" \
  "             npm:pnpm@10.33.3           4.0 MB" \
  "payload names are padded to a shared width"
assert_line "$payload_detail_output" \
  "             node@22.22.2              61.0 MB" \
  "a version-level payload is listed by tool and version"
assert_line "$payload_detail_output" \
  "             rust@stable                0.0 MB" \
  "a payload linked outside the data directory is listed without its target's size"
assert_line "$payload_detail_output" \
  "             'mise prune --tools' reclaims the disk; Terrapod does not remove them." \
  "the detailed advisory keeps the guidance line last"

# The acceptance criterion the issue states: the list and the count describe
# the same set, so a reader can act on the list without wondering what the
# number covered.
payload_row_count="$(
  printf '%s\n' "$payload_detail_output" |
    grep -c ' MB$' || true
)"
[ "$payload_row_count" = "4" ] ||
  fail "the detailed listing has exactly one row per counted payload (found $payload_row_count)"
pass "the detailed listing has exactly one row per counted payload"

# The whole check depends on mise to say what is reachable, so an absent mise
# skips it rather than calling every payload prunable.
mise_absent_path_dir="$tmp_dir/mise-absent-path"
mkdir -p "$mise_absent_path_dir"
for path_entry in "$path_dir"/*; do
  case "${path_entry##*/}" in
    mise) continue ;;
  esac
  ln -s "$path_entry" "$mise_absent_path_dir/${path_entry##*/}"
done
mise_absent_output="$(run_leftover_selection doctor "$mise_absent_path_dir")"
assert_not_contains "$mise_absent_output" "mise payload" \
  "an absent mise skips the payload check instead of reporting one"
