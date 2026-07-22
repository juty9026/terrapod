#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

fail() { printf '%s\n' "not ok - $1" >&2; exit 1; }
pass() { printf '%s\n' "ok - $1"; }

expected_formulae="$tmp_dir/expected-formulae"
actual_formulae="$tmp_dir/actual-formulae"
cat >"$expected_formulae" <<'EOF'
brew "bat"
brew "btop"
brew "chezmoi"
brew "duf"
brew "dust"
brew "fastfetch"
brew "fd"
brew "fzf"
brew "gh"
brew "git"
brew "git-delta"
brew "gum"
brew "lazygit"
brew "lsd"
brew "mise"
brew "neovim"
brew "ripgrep"
brew "starship"
brew "zellij"
brew "zoxide"
EOF

sed '/^[[:space:]]*#/d; /^[[:space:]]*$/d' "$repo_root/Brewfile" |
  LC_ALL=C sort >"$actual_formulae"
if ! cmp -s "$expected_formulae" "$actual_formulae"; then
  diff -u "$expected_formulae" "$actual_formulae" >&2 || true
  fail "root Brewfile declares exactly the mandatory cross-profile CLI formulae"
fi
pass "root Brewfile declares exactly the mandatory cross-profile CLI formulae"

records="$tmp_dir/records"
TERRAPOD_PRINT_HOMEBREW_CLI_RECORDS=1 "$repo_root/dot_local/bin/executable_terrapod" >"$records"
cut -f1 "$records" | LC_ALL=C sort >"$tmp_dir/record-formulae"
sed 's/^brew "//; s/"$//' "$expected_formulae" >"$tmp_dir/expected-record-formulae"
if ! cmp -s "$tmp_dir/expected-record-formulae" "$tmp_dir/record-formulae"; then
  diff -u "$tmp_dir/expected-record-formulae" "$tmp_dir/record-formulae" >&2 || true
  fail "doctor command ownership records stay synchronized with Brewfile"
fi
pass "doctor command ownership records stay synchronized with Brewfile"

ubuntu_smoke_fixture="$repo_root/tests/fixtures/homebrew-ubuntu-24.04.Dockerfile"
if ! grep -F 'args: ["force-bottle"]' "$ubuntu_smoke_fixture" >/dev/null ||
   ! grep -F 'brew bundle --no-upgrade --file=/tmp/Brewfile.bottles' "$ubuntu_smoke_fixture" >/dev/null; then
  fail "Ubuntu smoke bundle requires bottles through the supported Brewfile args mechanism"
fi
pass "Ubuntu smoke bundle requires bottles through the supported Brewfile args mechanism"

expected_bottle_brewfile="$tmp_dir/expected-bottle-brewfile"
actual_bottle_brewfile="$tmp_dir/actual-bottle-brewfile"
actual_bottle_brewfile_sorted="$tmp_dir/actual-bottle-brewfile-sorted"
sed 's/"$/", args: ["force-bottle"]/' "$expected_formulae" >"$expected_bottle_brewfile"
fixture_transform="$(sed -n '/^[[:space:]]*&& sed / {
  s/^[[:space:]]*&& //
  s/[[:space:]]*\\$//
  p
  q
}' "$ubuntu_smoke_fixture")"
if [ -z "$fixture_transform" ]; then
  fail "Ubuntu smoke bottle Brewfile transformation is discoverable"
fi
eval "$fixture_transform \"$repo_root/Brewfile\" >\"$actual_bottle_brewfile\""
LC_ALL=C sort "$actual_bottle_brewfile" >"$actual_bottle_brewfile_sorted"
if [ "$(wc -l <"$actual_bottle_brewfile")" -ne 20 ] ||
   ! cmp -s "$expected_bottle_brewfile" "$actual_bottle_brewfile_sorted"; then
  diff -u "$expected_bottle_brewfile" "$actual_bottle_brewfile_sorted" >&2 || true
  fail "Ubuntu smoke bottle Brewfile preserves all formula names and closing quotes"
fi
pass "Ubuntu smoke bottle Brewfile preserves all formula names and closing quotes"

if grep -F '| tee /tmp/mise.toml' "$ubuntu_smoke_fixture" >/dev/null ||
   ! grep -F -- '--file /workspace/dot_config/mise/config.toml.tmpl > /tmp/mise.toml' "$ubuntu_smoke_fixture" >/dev/null ||
   ! grep -F '&& cat /tmp/mise.toml' "$ubuntu_smoke_fixture" >/dev/null; then
  fail "Ubuntu smoke template render fails before output and assertions"
fi
pass "Ubuntu smoke template render fails before output and assertions"

expected_macos="$tmp_dir/expected-macos"
actual_macos="$tmp_dir/actual-macos"
printf '%s\n' \
  'cask "font-d2coding"' \
  'cask "font-jetbrains-mono-nerd-font"' >"$expected_macos"
sed '/^[[:space:]]*#/d; /^[[:space:]]*$/d' "$repo_root/Brewfile.macos" |
  LC_ALL=C sort >"$actual_macos"
if ! cmp -s "$expected_macos" "$actual_macos"; then
  diff -u "$expected_macos" "$actual_macos" >&2 || true
  fail "macOS Brewfile contains only mandatory terminal fonts"
fi
pass "macOS Brewfile contains only mandatory terminal fonts"

runtime_config="$tmp_dir/mise.toml"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux"}}' \
  --file "$repo_root/dot_config/mise/config.toml.tmpl" >"$runtime_config"

expected_runtimes="$tmp_dir/expected-runtimes"
actual_runtimes="$tmp_dir/actual-runtimes"
printf '%s\n' \
  'bun = "latest"' \
  'node = "24"' \
  'python = "3.13"' \
  'uv = "latest"' >"$expected_runtimes"
awk '
  /^\[/ { in_tools = ($0 == "[tools]"); next }
  in_tools && $0 !~ /^[[:space:]]*(#|$)/ { print }
' "$runtime_config" | LC_ALL=C sort >"$actual_runtimes"
if ! cmp -s "$expected_runtimes" "$actual_runtimes"; then
  diff -u "$expected_runtimes" "$actual_runtimes" >&2 || true
  fail "mise declares exactly the mandatory runtime tools"
fi
pass "mise declares exactly the mandatory runtime tools"
