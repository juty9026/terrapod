#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
managed_config="$repo_root/dot_config/git/config"
user_config_source="$repo_root/create_dot_gitconfig"
readme="$repo_root/README.md"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

assert_contains() {
  file="$1"
  needle="$2"
  message="$3"

  if ! grep -F "$needle" "$file" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  file="$1"
  needle="$2"
  message="$3"

  if grep -F "$needle" "$file" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

if [ ! -f "$managed_config" ]; then
  fail "Terrapod manages shared Git settings through ~/.config/git/config"
fi
pass "Terrapod manages shared Git settings through ~/.config/git/config"

if [ -e "$repo_root/private_dot_gitconfig.tmpl" ]; then
  fail "Terrapod leaves ~/.gitconfig unmanaged"
fi
pass "Terrapod leaves ~/.gitconfig unmanaged"

if [ ! -f "$user_config_source" ]; then
  fail "Terrapod creates ~/.gitconfig only when it does not exist"
fi
pass "Terrapod creates ~/.gitconfig only when it does not exist"

if [ -e "$repo_root/private_dot_ssh/allowed_signers.tmpl" ]; then
  fail "Terrapod leaves SSH allowed signers unmanaged"
fi
pass "Terrapod leaves SSH allowed signers unmanaged"

assert_contains "$managed_config" "[merge]" "shared Git config preserves merge settings"
assert_contains "$managed_config" "[pull]" "shared Git config preserves pull settings"
assert_contains "$managed_config" "[delta]" "shared Git config preserves delta settings"

for personal_setting in \
  "[user]" \
  "signingkey" \
  "[gpg]" \
  "op-ssh-sign" \
  "allowedSignersFile" \
  "gpgsign"
do
  assert_not_contains "$managed_config" "$personal_setting" \
    "shared Git config excludes personal setting: $personal_setting"
done

test_home="$tmp_dir/home"
test_xdg="$tmp_dir/xdg"
mkdir -p "$test_home" "$test_xdg/git"
cp "$managed_config" "$test_xdg/git/config"
cp "$managed_config" "$tmp_dir/shared-config-before"

HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" \
  chezmoi --source "$repo_root" --destination "$test_home" apply "$test_home/.gitconfig"
HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" \
  git config set --global user.name "Test User"
HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" \
  git config set --global user.email "test@example.com"

if ! cmp -s "$tmp_dir/shared-config-before" "$test_xdg/git/config"; then
  fail "setting user-level Git identity leaves shared Terrapod config unchanged"
fi
pass "setting user-level Git identity leaves shared Terrapod config unchanged"

if [ "$(HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" git config get user.name)" != "Test User" ] ||
   [ "$(HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" git config get user.email)" != "test@example.com" ]; then
  fail "user-level Git identity overrides shared Terrapod settings"
fi
pass "user-level Git identity overrides shared Terrapod settings"

if [ "$(HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" git config get merge.ff)" != "false" ]; then
  fail "setting user-level Git identity preserves shared Terrapod settings"
fi
pass "setting user-level Git identity preserves shared Terrapod settings"

cp "$test_home/.gitconfig" "$tmp_dir/user-config-before"
HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" \
  chezmoi --source "$repo_root" --destination "$test_home" apply "$test_home/.gitconfig"
if ! cmp -s "$tmp_dir/user-config-before" "$test_home/.gitconfig"; then
  fail "Terrapod preserves user changes in ~/.gitconfig on later applies"
fi
pass "Terrapod preserves user changes in ~/.gitconfig on later applies"

assert_contains "$readme" 'git config set --global user.name "Your Name"' \
  "README documents user-level Git name setup"
assert_contains "$readme" 'git config set --global user.email "you@example.com"' \
  "README documents user-level Git email setup"
assert_contains "$readme" 'git config set --global user.signingKey "ssh-ed25519 YOUR_PUBLIC_KEY"' \
  "README documents optional SSH signing setup"
assert_not_contains "$readme" "gitAllowedSigners" \
  "README removes the managed Git signer option"
