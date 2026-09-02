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

# The configured diff tool has to be one Terrapod actually installs; `vimdiff`
# was not, so `git difftool` failed on the VPS Shell Profile and fell back to
# the system vim on macOS.
assert_contains "$managed_config" "tool = nvimdiff" "shared Git config drives diffs through nvim"
assert_not_contains "$managed_config" "tool = vimdiff" "shared Git config no longer names an uninstalled diff tool"
assert_contains "$repo_root/Brewfile" 'brew "neovim"' "the configured diff tool comes from a declared package"

difftool_repo="$tmp_dir/difftool-repo"
difftool_bin="$tmp_dir/difftool-bin"
mkdir -p "$difftool_repo" "$difftool_bin"

cat >"$difftool_bin/nvim" <<'STUB'
#!/bin/sh
printf '%s\n' "$*" >>"$NVIM_STUB_LOG"
STUB
chmod +x "$difftool_bin/nvim"

git -C "$difftool_repo" init -q -b main
git -C "$difftool_repo" config user.email "test@example.com"
git -C "$difftool_repo" config user.name "Test User"
printf 'one\n' >"$difftool_repo/tracked.txt"
git -C "$difftool_repo" add tracked.txt
git -C "$difftool_repo" commit -q -m "initial commit"
printf 'two\n' >"$difftool_repo/tracked.txt"

NVIM_STUB_LOG="$tmp_dir/nvim.log"
: >"$NVIM_STUB_LOG"

if ! HOME="$test_home" XDG_CONFIG_HOME="$test_xdg" NVIM_STUB_LOG="$NVIM_STUB_LOG" \
  PATH="$difftool_bin:$PATH" \
  git -C "$difftool_repo" difftool --no-prompt </dev/null >"$tmp_dir/difftool.out" 2>&1; then
  fail "git difftool should succeed with the shared Terrapod config: $(cat "$tmp_dir/difftool.out")"
fi

if [ ! -s "$NVIM_STUB_LOG" ]; then
  fail "git difftool should open the diff with nvim: $(cat "$tmp_dir/difftool.out")"
fi
pass "git difftool opens the diff with nvim"

if ! grep -F -- "-d " "$NVIM_STUB_LOG" >/dev/null; then
  fail "git difftool should open nvim in diff mode; got '$(cat "$NVIM_STUB_LOG")'"
fi
pass "git difftool opens nvim in diff mode"

if ! grep -F -- "tracked.txt" "$NVIM_STUB_LOG" >/dev/null; then
  fail "git difftool should pass the changed file to nvim; got '$(cat "$NVIM_STUB_LOG")'"
fi
pass "git difftool passes the changed file to nvim"
