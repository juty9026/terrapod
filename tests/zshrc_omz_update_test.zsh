#!/usr/bin/env zsh

set -u

repo_root="${0:A:h:h}"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

chezmoi_bin="$(command -v chezmoi)" || fail "chezmoi is required to render templates"

mkdir -p "$tmp_dir/home/.oh-my-zsh"
: >"$tmp_dir/chezmoi.toml"

# Oh My Zsh reads its update mode while oh-my-zsh.sh runs, so the stub records
# the value visible at that moment.
cat >"$tmp_dir/home/.oh-my-zsh/oh-my-zsh.sh" <<'STUB'
zstyle -s ':omz:update' mode OMZ_UPDATE_MODE_AT_LOAD || OMZ_UPDATE_MODE_AT_LOAD=unset
STUB

"$chezmoi_bin" \
  --config "$tmp_dir/chezmoi.toml" \
  --destination "$tmp_dir/home" \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
  cat "$tmp_dir/home/.zshrc" \
  >"$tmp_dir/home/.zshrc"

export HOME="$tmp_dir/home"
export PATH="/usr/bin:/bin:/usr/sbin:/sbin"
export CLAUDECODE=""

source "$tmp_dir/home/.zshrc"

assert_equals "$OMZ_UPDATE_MODE_AT_LOAD" "auto" \
  "Oh My Zsh sees the auto update mode when it loads"
