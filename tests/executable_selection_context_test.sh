#!/bin/sh
set -eu

repo_root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
selection="$repo_root/dot_local/lib/terrapod/executable_executable-selection"
make_tmp_dir

prefix="$tmp_dir/homebrew"
ambient="$tmp_dir/ambient"
empty="$tmp_dir/empty"
brew_only="$tmp_dir/brew-only"
mise_only="$tmp_dir/mise-only"
which_dir="$tmp_dir/which"
runtime_bin="$tmp_dir/runtime/bin"
home_dir="$tmp_dir/home"
mkdir -p "$prefix/bin" "$ambient" "$empty" "$runtime_bin" "$home_dir" \
  "$brew_only" "$mise_only" "$which_dir"

write_executable() {
  printf '%s\n' '#!/bin/sh' 'exit 0' >"$1"
  chmod +x "$1"
}

"$selection" core-records >"$tmp_dir/core-records"
while IFS='|' read -r _provider package command; do
  printf '%s\n' "$package" >>"$tmp_dir/formulae"
  write_executable "$prefix/bin/$command"
done <"$tmp_dir/core-records"
for command in agy codex op; do
  write_executable "$prefix/bin/$command"
done
printf '%s\n' antigravity-cli codex 1password-cli >"$tmp_dir/casks"
mkdir -p "$home_dir/.local/bin"
write_executable "$home_dir/.local/bin/claude"
for command in bun node python3 uv; do
  write_executable "$runtime_bin/$command"
  printf '%s/%s\n' "$runtime_bin" "$command" >"$which_dir/$command"
done
printf '%s\n' "$runtime_bin" >"$tmp_dir/bin-paths"

cat >"$prefix/bin/brew" <<EOF_BREW
#!/bin/sh
case "\$*" in
  'list --formula --full-name') cat '$tmp_dir/formulae' ;;
  'list --cask --full-name') cat '$tmp_dir/casks' ;;
  *) exit 1 ;;
esac
EOF_BREW
chmod +x "$prefix/bin/brew"
# These providers deliberately disagree with the canonical installation.
write_executable "$ambient/brew"
write_executable "$ambient/mise"
ln -s "$ambient/brew" "$brew_only/brew"
ln -s "$ambient/mise" "$mise_only/mise"

# Exercise the real provider command adapter; no inventory overrides are used.
# Reject mutations and any payload query that is not pinned to HOME.
cat >"$prefix/bin/mise" <<EOF_MISE
#!/bin/sh
case "\${1:-}" in
  bin-paths) cat '$tmp_dir/bin-paths' ;;
  which) cat "$which_dir/\$2" ;;
  where)
    case "\$2" in
      node@20.0.0) printf '%s\n' '$tmp_dir/payload' ;;
      bun|node|python|uv) printf '%s\n' '$runtime_bin/..' ;;
      *) exit 1 ;;
    esac
    ;;
  ls)
    [ "\$*" = 'ls -C $home_dir --prunable --no-header' ] || exit 1
    cat '$tmp_dir/prunable'
    ;;
  *) exit 1 ;;
esac
EOF_MISE
chmod +x "$prefix/bin/mise"
managed_path="$runtime_bin:$home_dir/.local/bin:$prefix/bin:/usr/bin:/bin"

run_selection() {
  invoking_path="$1"
  selection_mode="$2"
  shift 2
  HOME="$home_dir" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR='' \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MANAGED_PATH="$managed_path" \
    PATH="$invoking_path:/usr/bin:/bin" \
    "$selection" "$selection_mode" macos-terminal true true "$@"
}

canonical_output="$(run_selection "$prefix/bin" doctor)"
assert_contains "$canonical_output" 'Canonical executable selection: ready' \
  'live provider commands accept canonical formulae, casks, and runtimes'

set +e
other_brew_output="$(run_selection "$ambient:$prefix/bin" doctor)"
other_brew_status="$?"
set -e
assert_status "$other_brew_status" 0 \
  'ambient providers cannot change the live inventory verdict'
assert_equals "$other_brew_output" "$canonical_output" \
  'canonical inventory and runtime observations ignore ambient providers'

for selection_mode in apply status doctor; do
  expected="$(run_selection "$prefix/bin" "$selection_mode")"
  for invoking_path in "$brew_only:$prefix/bin" "$mise_only:$prefix/bin" "$ambient:$prefix/bin" "$empty"; do
    actual="$(run_selection "$invoking_path" "$selection_mode")"
    assert_equals "$actual" "$expected" \
      "$selection_mode shares canonical observations with invoking PATH $invoking_path"
  done
done

# Payload reachability and locations must come from that same manager. Reject
# every invocation except the read-only, HOME-pinned protocol under test.
mkdir -p "$home_dir/.local/share/mise/installs" "$tmp_dir/payload"
printf '%s\n' 'node 20.0.0' >"$tmp_dir/prunable"

canonical_payloads="$(run_selection "$prefix/bin" doctor true)"
assert_contains "$canonical_payloads" 'advisory - 1 mise payload can be pruned' \
  'canonical mise delegates payload reachability with HOME pinned'
assert_contains "$canonical_payloads" 'node@20.0.0' \
  'canonical mise supplies payload detail locations'
for invoking_path in "$ambient:$prefix/bin" "$empty"; do
  payloads="$(run_selection "$invoking_path" doctor true)"
  assert_equals "$payloads" "$canonical_payloads" \
    'payload count and measured details ignore ambient mise replacement or absence'
done

# Use the same bootstrap zsh for every invocation. Its successful login probe
# supplies no runtime bin directory: only canonical mise can prepend that.
probe_dir="$tmp_dir/probe"
mkdir -p "$probe_dir"
login_path="$home_dir/.local/bin:$prefix/bin:/usr/bin:/bin"
cat >"$probe_dir/zsh" <<EOF_ZSH
#!/bin/sh
[ "\$PATH" = /usr/bin:/bin:/usr/sbin:/sbin ] || exit 1
printf '%s\n' '$login_path'
EOF_ZSH
chmod +x "$probe_dir/zsh"
managed_path=""
path_file="$tmp_dir/managed-path"
for invoking_path in "$prefix/bin" "$brew_only:$prefix/bin" "$mise_only:$prefix/bin" "$empty"; do
  output="$(TERRAPOD_EXECUTABLE_SELECTION_PATH_FILE="$path_file" \
    run_selection "$probe_dir:$invoking_path" doctor)"
  assert_contains "$output" 'Canonical executable selection: ready' \
    'the login probe and canonical runtime paths produce a ready verdict'
  assert_not_contains "$output" 'inherited PATH' \
    'a successful login probe does not fall back to invoking PATH'
  assert_equals "$(cat "$path_file")" "$runtime_bin:$login_path" \
    'automatic Managed Default PATH uses canonical mise bin-paths'
done

# In shim mode the active target and inactive fall-through must both use the
# selected manager. rg falls through while node has an active runtime target.
shims="$home_dir/.local/share/mise/shims"
mkdir -p "$shims"
for command in bun node python3 uv rg; do
  ln -s "$prefix/bin/mise" "$shims/$command"
done
: >"$tmp_dir/bin-paths"
managed_path="$shims:$login_path"
shim_baseline="$(run_selection "$prefix/bin" doctor)"
assert_contains "$shim_baseline" 'Canonical executable selection: ready' \
  'active runtime shims and inactive Homebrew fall-through remain canonical'
for invoking_path in "$mise_only:$prefix/bin" "$empty"; do
  shim_output="$(run_selection "$invoking_path" doctor)"
  assert_equals "$shim_output" "$shim_baseline" \
    'active shim targets and inactive shim fall-through ignore ambient mise'
done

# An active shim pointing at another installation must stay advisory. This
# distinguishes an actual mise-which observation from assuming fall-through.
write_executable "$runtime_bin/rg"
printf '%s\n' "$runtime_bin/rg" >"$which_dir/rg"
shim_advisory="$(run_selection "$empty" doctor)"
assert_contains "$shim_advisory" "advisory - ripgrep resolves to $shims/rg" \
  'canonical mise which exposes a noncanonical active shim target'
rm "$which_dir/rg"

# Restore active-bin mode before checking distinct absence diagnostics.
printf '%s\n' "$runtime_bin" >"$tmp_dir/bin-paths"
managed_path="$runtime_bin:$login_path"
# A second, fully working provider on invoking PATH must not mask the absence
# of a canonical one. The copies still answer from the same fixture files.
cp "$prefix/bin/brew" "$ambient/brew"
cp "$prefix/bin/mise" "$ambient/mise"

mv "$prefix/bin/brew" "$tmp_dir/canonical-brew"
for selection_mode in apply status doctor; do
  set +e
  missing_brew="$(run_selection "$ambient" "$selection_mode")"
  missing_status="$?"
  set -e
  expected_status=0
  [ "$selection_mode" != doctor ] || expected_status=1
  assert_status "$missing_status" "$expected_status" \
    "$selection_mode preserves its exit contract when canonical Homebrew is absent"
  assert_contains "$missing_brew" 'bat is not installed through Homebrew' \
    'ambient brew cannot mask a missing canonical provider'
  assert_contains "$missing_brew" 'codex is not installed through Homebrew Cask' \
    'canonical provider absence also fails enabled cask inventory'
done
mv "$tmp_dir/canonical-brew" "$prefix/bin/brew"

mv "$prefix/bin/mise" "$tmp_dir/canonical-mise"
set +e
missing_mise="$(run_selection "$ambient" doctor true)"
missing_status="$?"
set -e
assert_status "$missing_status" 1 'canonical mise absence fails doctor'
assert_contains "$missing_mise" 'node is not installed through mise' \
  'ambient mise cannot mask a missing canonical runtime manager'
assert_contains "$missing_mise" 'mise canonical executable is missing' \
  'installed formula inventory and missing canonical executable remain distinct'
assert_not_contains "$missing_mise" 'mise payload' \
  'payload observation is skipped when only the ambient manager exists'
mv "$tmp_dir/canonical-mise" "$prefix/bin/mise"

# Package inventory absence, missing executable, unavailable command, and a
# different selected executable retain their separate meanings.
sed '/^bat$/d' "$tmp_dir/formulae" >"$tmp_dir/formulae-next"
mv "$tmp_dir/formulae-next" "$tmp_dir/formulae"
set +e
missing_package="$(run_selection "$empty" doctor)"
missing_status="$?"
set -e
assert_status "$missing_status" 1 'canonical package absence fails doctor'
assert_contains "$missing_package" 'bat is not installed through Homebrew' \
  'live inventory can report a missing package independently of its executable'
printf '%s\n' bat >>"$tmp_dir/formulae"

rm "$prefix/bin/bat"
set +e
missing_executable="$(run_selection "$empty" doctor)"
missing_status="$?"
set -e
assert_status "$missing_status" 1 'canonical executable absence fails doctor'
assert_contains "$missing_executable" "bat canonical executable is missing: $prefix/bin/bat" \
  'a live installed package with no canonical executable has its own diagnostic'
write_executable "$prefix/bin/bat"

shadow_dir="$tmp_dir/shadow"
mkdir -p "$shadow_dir"
write_executable "$shadow_dir/bat"
managed_path="$shadow_dir:$managed_path"
for selection_mode in apply status doctor; do
  advisory="$(run_selection "$empty" "$selection_mode")"
  assert_contains "$advisory" "advisory - bat resolves to $shadow_dir/bat" \
    "$selection_mode keeps a noncanonical selected executable advisory-only"
  assert_contains "$advisory" "canonical: $prefix/bin/bat" \
    'the advisory still identifies the canonical executable'
done
