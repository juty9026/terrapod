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

# Every scenario sources .zshrc in its own subshell, so each one has to start
# from a shell that has never run compinit: no dump on disk, no table in memory.
reset_dumps() {
  rm -f "$tmp_dir"/home/.zcompdump*(N)
}

mkdir -p \
  "$tmp_dir/home/.oh-my-zsh" \
  "$tmp_dir/home/.local/share/zinit/zinit.git" \
  "$tmp_dir/completions" \
  "$tmp_dir/insecure"

# Oh My Zsh runs compinit itself against its own dump; that snapshot is what a
# late fpath addition misses. It picks its security flag the way the real
# oh-my-zsh.sh does, so it never stops to ask about an insecure directory.
cat >"$tmp_dir/home/.oh-my-zsh/oh-my-zsh.sh" <<'STUB'
ZSH_COMPDUMP="${ZDOTDIR:-$HOME}/.zcompdump-oh-my-zsh"
autoload -Uz compinit
if [[ "${ZSH_DISABLE_COMPFIX:-}" == true ]]; then
  compinit -u -d "$ZSH_COMPDUMP"
else
  compinit -i -d "$ZSH_COMPDUMP"
fi
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

# A completion directory compaudit refuses to vouch for. Machines grow these:
# a group-writable Homebrew site-functions directory is the everyday case, and
# the GitHub Actions Ubuntu runner carries one in its default fpath.
cat >"$tmp_dir/insecure/_terrapod-insecure-probe" <<'STUB'
#compdef terrapod-insecure-probe
_message 'terrapod insecure probe'
STUB
chmod 777 "$tmp_dir/insecure"

chezmoi_bin="$(command -v chezmoi)" || fail "chezmoi is required to render templates"

export HOME="$tmp_dir/home"
export PATH="/usr/bin:/bin:/usr/sbin:/sbin"
export COMPLETIONS_TEST_DIR="$tmp_dir/completions"
# SCM Breeze is unrelated here and pulls in a repository it does not have.
export CLAUDECODE=1

: >"$tmp_dir/chezmoi.toml"
render_zshrc '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}'

reset_dumps
(
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
) || exit 1

# The directory zinit contributes is the one only the rebuild ever sees, so the
# rebuild alone decides what to do about its security. compinit stops to ask
# unless it is told, and a shell with nothing to ask — every non-interactive
# one — aborts and keeps no table at all.
reset_dumps
(
  export COMPLETIONS_TEST_DIR="$tmp_dir/insecure"

  source "$tmp_dir/home/.zshrc"

  if (( ! ${+_comps} )); then
    fail "an insecure directory arriving late should not cost the table"
  fi

  if (( ${+_comps[terrapod-insecure-probe]} )); then
    fail "the rebuild should skip an insecure completion directory, \
the way oh-my-zsh.sh does"
  fi

  pass "the rebuild skips an insecure completion directory"
) || exit 1

# ZSH_DISABLE_COMPFIX is how a user tells Oh My Zsh to load insecure
# directories anyway. The rebuild has to make the same call, or it drops the
# completions that flag was set to keep.
reset_dumps
(
  export COMPLETIONS_TEST_DIR="$tmp_dir/insecure"
  export ZSH_DISABLE_COMPFIX=true

  source "$tmp_dir/home/.zshrc"

  if (( ! ${+_comps} )) || (( ! ${+_comps[terrapod-insecure-probe]} )); then
    fail "ZSH_DISABLE_COMPFIX should keep insecure directories in the rebuilt table"
  fi

  pass "ZSH_DISABLE_COMPFIX keeps insecure directories in the rebuilt table"
) || exit 1
