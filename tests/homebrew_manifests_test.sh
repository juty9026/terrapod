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
bat
btop
chezmoi
duf
dust
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

sed -n 's/^brew "\([^"]*\)"$/\1/p' "$repo_root/Brewfile" | sort >"$actual_formulae"
if ! cmp -s "$expected_formulae" "$actual_formulae"; then
  diff -u "$expected_formulae" "$actual_formulae" >&2 || true
  fail "root Brewfile declares exactly the mandatory cross-profile CLI formulae"
fi
pass "root Brewfile declares exactly the mandatory cross-profile CLI formulae"

expected_macos="$tmp_dir/expected-macos"
actual_macos="$tmp_dir/actual-macos"
printf '%s\n' font-d2coding font-jetbrains-mono-nerd-font >"$expected_macos"
sed -n 's/^cask "\([^"]*\)"$/\1/p' "$repo_root/Brewfile.macos" | sort >"$actual_macos"
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

for runtime in 'bun = "latest"' 'node = "24"' 'python = "3.13"' 'uv = "latest"'; do
  grep -Fx "$runtime" "$runtime_config" >/dev/null || fail "mise retains runtime declaration: $runtime"
done
if grep -F 'aqua:' "$runtime_config" >/dev/null; then
  fail "mise no longer declares shared CLI tools through aqua"
fi
pass "mise owns runtimes only"
