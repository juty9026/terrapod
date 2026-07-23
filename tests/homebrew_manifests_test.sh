#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM
fail() { printf '%s\n' "not ok - $*" >&2; exit 1; }

for file in Brewfile Brewfile.ai-cli-tools.tmpl Brewfile.macos-desktop-apps.tmpl; do
  [ ! -e "$repo_root/$file" ] || fail "legacy manifest remains: $file"
done

cat >"$tmp_dir/want" <<'EOF'
homebrew-cask	1password-cli
homebrew-cask	antigravity-cli
homebrew-cask	claude-code
homebrew-cask	codex
homebrew-cask	ghostty
homebrew-cask	hammerspoon
homebrew-cask	istat-menus
homebrew-cask	karabiner-elements
homebrew-cask	raycast
homebrew-cask	scroll-reverser
homebrew-cask	stablyai/orca/orca
homebrew-cask	zed
homebrew-formula	bat
homebrew-formula	btop
homebrew-formula	chezmoi
homebrew-formula	duf
homebrew-formula	dust
homebrew-formula	fastfetch
homebrew-formula	fd
homebrew-formula	fzf
homebrew-formula	gh
homebrew-formula	git
homebrew-formula	git-delta
homebrew-formula	gum
homebrew-formula	lazygit
homebrew-formula	lsd
homebrew-formula	mise
homebrew-formula	neovim
homebrew-formula	ripgrep
homebrew-formula	starship
homebrew-formula	zellij
homebrew-formula	zoxide
EOF

jq -r '.resources[] | select(.provider == "homebrew-formula" or .provider == "homebrew-cask") | [.provider,.package] | @tsv' \
  "$repo_root/catalog/v1/resources.json" | LC_ALL=C sort >"$tmp_dir/got"
LC_ALL=C sort "$tmp_dir/want" -o "$tmp_dir/want"
cmp -s "$tmp_dir/want" "$tmp_dir/got" ||
  fail "catalog ownership does not cover every deleted Brewfile entry"

go test ./internal/catalog -run 'TestSeedCatalogHasCurrentConfigSchemaAndHomebrewResources$'
go test ./internal/provider/homebrew

printf '%s\n' "ok - deleted Homebrew manifests have complete typed ownership"
