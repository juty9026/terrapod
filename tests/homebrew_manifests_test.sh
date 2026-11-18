#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/chezmoi-test-support.sh"
make_tmp_dir

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

selection_source="$tmp_dir/selection-source"
selection_lib="$selection_source/dot_local/lib/terrapod"
selection_home="$tmp_dir/selection-home"
mkdir -p "$selection_lib" "$selection_home/.local/lib"
cp "$repo_root/dot_local/lib/terrapod/executable_executable-selection" \
  "$repo_root/dot_local/lib/terrapod/homebrew-paths.sh" \
  "$repo_root/dot_local/lib/terrapod/homebrew-prefix.sh" "$selection_lib/"
printf '%s\n' 'brew "bat"' 'brew "git-delta"' 'brew "neovim"' 'brew "ripgrep"' 'brew "fixture-tool"' \
  >"$selection_lib/core.Brewfile"
run_test_chezmoi "$selection_source" "$selection_home" apply \
  "$selection_home/.local/lib/terrapod"
printf '%s\n' \
  'homebrew-formula|bat|bat' \
  'homebrew-formula|git-delta|delta' \
  'homebrew-formula|neovim|nvim' \
  'homebrew-formula|ripgrep|rg' \
  'homebrew-formula|fixture-tool|fixture-tool' >"$tmp_dir/deployed-expected"
"$selection_home/.local/lib/terrapod/executable-selection" core-records >"$tmp_dir/deployed-records"
if ! cmp -s "$tmp_dir/deployed-expected" "$tmp_dir/deployed-records"; then
  diff -u "$tmp_dir/deployed-expected" "$tmp_dir/deployed-records" >&2 || true
  fail "executable selection must read the installed Core Shell Stack Brewfile beside the helper"
fi
pass "the installed helper checks exactly the Core Shell Stack formulae in its Brewfile"

if [ ! "$repo_root/Brewfile" -ef "$repo_root/dot_local/lib/terrapod/core.Brewfile" ]; then
  fail "Brewfile must link to dot_local/lib/terrapod/core.Brewfile so brew bundle and executable selection share one declaration"
fi
pass "the root and managed Brewfile names refer to the same declaration"

expected_ai_casks="$tmp_dir/expected-ai-casks"
actual_ai_casks="$tmp_dir/actual-ai-casks"
printf '%s\n' \
  'cask "antigravity-cli"' \
  'cask "codex"' >"$expected_ai_casks"

chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"darwin"},"enableAiCliTools":true}' \
  --file "$repo_root/Brewfile.ai-cli-tools.tmpl" >"$actual_ai_casks"
printf '\n' >>"$actual_ai_casks"
if ! cmp -s "$expected_ai_casks" "$actual_ai_casks"; then
  diff -u "$expected_ai_casks" "$actual_ai_casks" >&2 || true
  fail "Optional AI Tool Stack declares exactly the two macOS casks"
fi
pass "Optional AI Tool Stack declares exactly the two macOS casks"

linux_ai_casks="$tmp_dir/linux-ai-casks"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux"},"enableAiCliTools":true,"enableDevelopmentWorkspace":true}' \
  --file "$repo_root/Brewfile.ai-cli-tools.tmpl" >"$linux_ai_casks"
if [ -s "$linux_ai_casks" ]; then
  cat "$linux_ai_casks" >&2
  fail "Optional AI Tool Stack declares no packages on Linux"
fi
pass "Optional AI Tool Stack declares no packages on Linux"

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

if grep -F 'execute-template' "$ubuntu_smoke_fixture" >/dev/null ||
   ! grep -F '/workspace/dot_config/mise/config.toml' "$ubuntu_smoke_fixture" >/dev/null; then
  fail "Ubuntu smoke reads the runtime config as a plain file"
fi
pass "Ubuntu smoke reads the runtime config as a plain file"

if ! grep -F "! grep -F 'aqua:'" "$ubuntu_smoke_fixture" >/dev/null ||
   ! grep -F 'node = "24"' "$ubuntu_smoke_fixture" >/dev/null; then
  fail "Ubuntu smoke still asserts the runtime declarations it read"
fi
pass "Ubuntu smoke still asserts the runtime declarations it read"

# Read, not rendered: the runtime declarations are the same on every machine,
# and the repository conventions reserve templates for machine-varying content.
runtime_config="$repo_root/dot_config/mise/config.toml"

if grep -F '{{' "$runtime_config" >/dev/null; then
  fail "mise runtime config stays free of template actions"
fi
pass "mise runtime config stays free of template actions"

if [ -e "$runtime_config.tmpl" ]; then
  fail "mise runtime config is managed under one name"
fi
pass "mise runtime config is managed under one name"

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
if grep -E '^[[:space:]]*cask[[:space:]]+"font-(jetbrains-mono-nerd-font|d2coding)"' "$repo_root/Brewfile" >/dev/null; then
  fail "core Brewfile no longer declares superseded terminal font casks"
fi
pass "core Brewfile no longer declares superseded terminal font casks"

if ! grep -Fx 'brew "gum"' "$repo_root/Brewfile" >/dev/null; then
  fail "core Brewfile declares gum as the setup UI dependency"
fi

pass "core Brewfile declares gum as the setup UI dependency"

if grep -E '^[[:space:]]*cask[[:space:]]+"' "$repo_root/Brewfile" >/dev/null; then
  fail "cross-profile core Brewfile excludes macOS-only casks"
fi
pass "cross-profile core Brewfile excludes macOS-only casks"
ubuntu_mise_config="$(cat "$repo_root/dot_config/mise/config.toml")"

if printf '%s\n' "$ubuntu_mise_config" | grep -F '"aqua:neovim/neovim" = "latest"' >/dev/null; then
  fail "Ubuntu VPS removes duplicate mise-managed Neovim"
fi

pass "Ubuntu VPS removes duplicate mise-managed Neovim"

if printf '%s\n' "$ubuntu_mise_config" | grep -F '"aqua:cli/cli" = "latest"' >/dev/null; then
  fail "Ubuntu VPS removes duplicate mise-managed GitHub CLI"
fi

pass "Ubuntu VPS removes duplicate mise-managed GitHub CLI"

for formula in neovim gh; do
  if ! grep -Fx "brew \"$formula\"" "$repo_root/Brewfile" >/dev/null; then
    fail "cross-profile Brewfile declares migrated formula: $formula"
  fi
done
pass "cross-profile Brewfile declares migrated Neovim and GitHub CLI"
