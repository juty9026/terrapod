#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

render_zshenv() {
  data="$1"

  "$chezmoi_bin" \
    --config "$tmp_dir/chezmoi.toml" \
    --destination "$tmp_dir/home" \
    --source "$repo_root" \
    execute-template \
    --override-data "$data" \
    --file "$repo_root/dot_zshenv.tmpl" \
    >"$tmp_dir/home/.zshenv"
}

lookup_chezmoi() {
  env -i \
    HOME="$tmp_dir/home" \
    PATH="/usr/bin:/bin:/usr/sbin:/sbin" \
    zsh -c 'command -v chezmoi' \
    >"$tmp_dir/lookup.out" \
    2>"$tmp_dir/lookup.err"
}

assert_lookup_success() {
  data="$1"
  message="$2"

  render_zshenv "$data"

  if ! lookup_chezmoi; then
    fail "$message; expected ~/.local/bin/chezmoi in PATH"
  fi

  expected="$tmp_dir/home/.local/bin/chezmoi"
  actual="$(cat "$tmp_dir/lookup.out")"

  if [ "$actual" != "$expected" ]; then
    fail "$message; expected '$expected', got '$actual'"
  fi

  pass "$message"
}

chezmoi_bin="$(command -v chezmoi)" || fail "chezmoi is required to render templates"

mkdir -p "$tmp_dir/home/.local/bin"
: >"$tmp_dir/chezmoi.toml"

cat >"$tmp_dir/home/.local/bin/chezmoi" <<'STUB'
#!/bin/sh
exit 0
STUB
chmod +x "$tmp_dir/home/.local/bin/chezmoi"

assert_lookup_success \
  '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
  "Ubuntu zshenv exposes user-local chezmoi after reconnect"

assert_lookup_success \
  '{"chezmoi":{"os":"darwin"}}' \
  "macOS zshenv exposes user-local binaries by default"
