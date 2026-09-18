#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
managed_config="$repo_root/dot_config/gh/config.yml"
readme="$repo_root/README.md"
readme_ko="$repo_root/README.ko.md"
make_tmp_dir
chezmoi_config="$tmp_dir/chezmoi.toml"
: >"$chezmoi_config"

darwin_data='{"chezmoi":{"os":"darwin"}}'
linux_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}'

managed_target_paths() {
  data="$1"
  chezmoi \
    --config "$chezmoi_config" \
    --source "$repo_root" \
    --override-data "$data" \
    managed
}

if [ ! -f "$managed_config" ]; then
  fail "Terrapod manages shared GitHub CLI settings through ~/.config/gh/config.yml"
fi
pass "Terrapod manages shared GitHub CLI settings through ~/.config/gh/config.yml"

darwin_managed="$(managed_target_paths "$darwin_data")"
linux_managed="$(managed_target_paths "$linux_data")"

if ! printf '%s\n' "$darwin_managed" | grep -Fx ".config/gh/config.yml" >/dev/null; then
  fail "macOS Terminal Profile manages .config/gh/config.yml"
fi
pass "macOS Terminal Profile manages .config/gh/config.yml"

if ! printf '%s\n' "$linux_managed" | grep -Fx ".config/gh/config.yml" >/dev/null; then
  fail "VPS Shell Profile manages .config/gh/config.yml"
fi
pass "VPS Shell Profile manages .config/gh/config.yml"

if printf '%s\n' "$darwin_managed" | grep -Fx ".config/gh/hosts.yml" >/dev/null; then
  fail "Terrapod leaves ~/.config/gh/hosts.yml unmanaged on the macOS Terminal Profile"
fi
pass "Terrapod leaves ~/.config/gh/hosts.yml unmanaged on the macOS Terminal Profile"

if printf '%s\n' "$linux_managed" | grep -Fx ".config/gh/hosts.yml" >/dev/null; then
  fail "Terrapod leaves ~/.config/gh/hosts.yml unmanaged on the VPS Shell Profile"
fi
pass "Terrapod leaves ~/.config/gh/hosts.yml unmanaged on the VPS Shell Profile"

assert_file_contains "$managed_config" "git_protocol: ssh" \
  "shared GitHub CLI config sets git_protocol to ssh"
assert_file_contains "$managed_config" "co: pr checkout" \
  "shared GitHub CLI config declares the co alias for pr checkout"

for auth_setting in \
  "oauth_token" \
  "user:" \
  "github.com:"
do
  assert_file_not_contains "$managed_config" "$auth_setting" \
    "shared GitHub CLI config excludes authentication setting: $auth_setting"
done

# gh resolves its config directory from XDG_CONFIG_HOME (defaulting to
# $HOME/.config), and chezmoi's --destination writes target paths beneath
# that same $HOME. Pointing XDG_CONFIG_HOME at $test_home/.config, rather
# than a separate directory, keeps both tools reading and writing the file
# chezmoi actually applied.
test_home="$tmp_dir/home"
test_xdg="$test_home/.config"
mkdir -p "$test_xdg/gh"

seeded_hosts="$test_xdg/gh/hosts.yml"
cat >"$seeded_hosts" <<'EOF'
github.com:
    oauth_token: existing-machine-local-token
    user: existing-user
EOF
cp "$seeded_hosts" "$tmp_dir/hosts-before"

HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" \
  chezmoi --config "$chezmoi_config" --source "$repo_root" --destination "$test_home" \
  apply "$test_home/.config" "$test_home/.config/gh" "$test_home/.config/gh/config.yml"

if ! cmp -s "$tmp_dir/hosts-before" "$seeded_hosts"; then
  fail "applying Terrapod's managed gh config leaves an existing hosts.yml untouched"
fi
pass "applying Terrapod's managed gh config leaves an existing hosts.yml untouched"

gh_bin="$(command -v gh 2>/dev/null || true)"
if [ -z "$gh_bin" ]; then
  skip "gh is not on PATH; cannot verify git_protocol through gh config get"
else
  # Snapshot immediately after chezmoi apply, before any gh invocation: gh
  # rewrites a version-less config.yml on its very first read (even a plain
  # `gh config get`), not just on `gh config list`. Snapshotting later would
  # let that first-read rewrite hide inside the "before" copy instead of
  # being caught as the perpetual drift `tpod diff` (user story 5) would show.
  cp "$test_xdg/gh/config.yml" "$tmp_dir/gh-config-before-any-read"

  reported_protocol="$(HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" gh config get git_protocol)"
  if [ "$reported_protocol" != "ssh" ]; then
    fail "gh config get git_protocol reports ssh after apply, got: $reported_protocol"
  fi
  pass "gh config get git_protocol reports ssh after apply"

  HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" gh config list >/dev/null

  if ! cmp -s "$tmp_dir/gh-config-before-any-read" "$test_xdg/gh/config.yml"; then
    fail "gh config get/list do not rewrite the applied config.yml (perpetual drift)"
  fi
  pass "gh config get/list do not rewrite the applied config.yml"
fi

assert_file_contains "$readme" '`~/.config/gh/config.yml`' \
  "README documents the managed GitHub CLI config path"
assert_file_contains "$readme" '`~/.config/gh/hosts.yml`' \
  "README documents that GitHub CLI authentication stays machine-local"
assert_file_contains "$readme_ko" '`~/.config/gh/config.yml`' \
  "Korean README documents the managed GitHub CLI config path"
assert_file_contains "$readme_ko" '`~/.config/gh/hosts.yml`' \
  "Korean README documents that GitHub CLI authentication stays machine-local"
