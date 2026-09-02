#!/usr/bin/env zsh

set -u

repo_root="${0:A:h:h}"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  print -u2 -- "not ok - $1"
  exit 1
}

pass() {
  print -- "ok - $1"
}

render_zshrc() {
  local data="$1"

  "$chezmoi_bin" \
    --config "$tmp_dir/chezmoi.toml" \
    --destination "$tmp_dir/home" \
    --source "$repo_root" \
    --override-data "$data" \
    cat "$tmp_dir/home/.zshrc" \
    >"$tmp_dir/home/.zshrc"
}

mkdir -p \
  "$tmp_dir/home/.oh-my-zsh" \
  "$tmp_dir/home/.local/share/zinit/zinit.git" \
  "$tmp_dir/completions"

# Oh My Zsh runs compinit itself against its own dump; that snapshot is what a
# late fpath addition misses.
cat >"$tmp_dir/home/.oh-my-zsh/oh-my-zsh.sh" <<'STUB'
ZSH_COMPDUMP="${ZDOTDIR:-$HOME}/.zcompdump-oh-my-zsh"
autoload -Uz compinit
compinit -u -d "$ZSH_COMPDUMP"
STUB

# zsh-completions contributes nothing but fpath entries, so the stub does only that.
cat >"$tmp_dir/home/.local/share/zinit/zinit.git/zinit.zsh" <<'STUB'
function zinit() {
  if [[ "${1:-}" == light && "${2:-}" == zsh-users/zsh-completions ]]; then
    fpath=("$COMPLETIONS_TEST_DIR" $fpath)
  fi
}
STUB

cat >"$tmp_dir/completions/_terrapod-completions-probe" <<'STUB'
#compdef terrapod-completions-probe
_message 'terrapod completions probe'
STUB

chezmoi_bin="$(command -v chezmoi)" || fail "chezmoi is required to render templates"

export HOME="$tmp_dir/home"
export PATH="/usr/bin:/bin:/usr/sbin:/sbin"
export COMPLETIONS_TEST_DIR="$tmp_dir/completions"
# SCM Breeze is unrelated here and pulls in a repository it does not have.
export CLAUDECODE=1

: >"$tmp_dir/chezmoi.toml"
render_zshrc '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}'

source "$tmp_dir/home/.zshrc"

if (( ! ${+_comps} )); then
  fail "sourcing .zshrc should leave a completion table behind"
fi

pass "sourcing .zshrc leaves a completion table behind"

if (( ! ${+_comps[terrapod-completions-probe]} )); then
  fail "completions contributed by zsh-completions should be registered; \
compinit ran before the zinit block added them to fpath"
fi

pass "completions contributed by zsh-completions are registered"

if [[ ! -f "$tmp_dir/home/.zcompdump-oh-my-zsh" ]]; then
  fail "the rebuilt completion table should reuse Oh My Zsh's dump file"
fi

if [[ -e "$tmp_dir/home/.zcompdump" ]]; then
  fail "the rebuilt completion table should not add a second dump file"
fi

pass "the rebuilt completion table reuses Oh My Zsh's dump file"
