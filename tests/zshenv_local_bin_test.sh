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

assert_contains() {
  haystack="$1"
  needle="$2"
  message="$3"
  printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null || fail "$message"
  pass "$message"
}

assert_order() {
  haystack="$1"
  first="$2"
  second="$3"
  message="$4"
  printf '%s\n' "$haystack" | awk -v first="$first" -v second="$second" '
    first_line == 0 && index($0, first) { first_line = NR }
    second_line == 0 && index($0, second) { second_line = NR }
    END { exit !(first_line > 0 && second_line > first_line) }
  ' || fail "$message"
  pass "$message"
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

lookup_command() {
  command_name="$1"

  env -i \
    HOME="$tmp_dir/home" \
    PATH="/usr/bin:/bin:/usr/sbin:/sbin" \
    zsh -c 'command -v "$1"' zsh "$command_name" \
    >"$tmp_dir/lookup.out" \
    2>"$tmp_dir/lookup.err"
}

assert_lookup_success() {
  data="$1"
  message="$2"

  render_zshenv "$data"

  if ! lookup_command terrapod-local-test-command; then
    fail "$message; expected ~/.local/bin/terrapod-local-test-command in PATH"
  fi

  expected="$tmp_dir/home/.local/bin/terrapod-local-test-command"
  actual="$(cat "$tmp_dir/lookup.out")"

  if [ "$actual" != "$expected" ]; then
    fail "$message; expected '$expected', got '$actual'"
  fi

  pass "$message"
}

assert_path_snippet_lookup_success() {
  data="$1"
  message="$2"

  render_zshenv "$data"

  mkdir -p "$tmp_dir/home/.config/zsh/path.d"
  mkdir -p "$tmp_dir/home/.snippet/bin"

  cat >"$tmp_dir/home/.config/zsh/path.d/custom.zsh" <<'STUB'
typeset -U path PATH
path=("$HOME/.snippet/bin" $path)
export PATH
STUB

  : >"$tmp_dir/home/.snippet/bin/snippet-tool"
  chmod +x "$tmp_dir/home/.snippet/bin/snippet-tool"

  if ! lookup_command snippet-tool; then
    fail "$message; expected path.d snippet directory in PATH"
  fi

  expected="$tmp_dir/home/.snippet/bin/snippet-tool"
  actual="$(cat "$tmp_dir/lookup.out")"

  if [ "$actual" != "$expected" ]; then
    fail "$message; expected '$expected', got '$actual'"
  fi

  pass "$message"
}

assert_linuxbrew_shellenv_rendering() {
  data="$1"
  expected="$2"
  message="$3"

  render_zshenv "$data"

  if [ "$expected" = present ]; then
    if ! grep -F '/home/linuxbrew/.linuxbrew/bin/brew shellenv' "$tmp_dir/home/.zshenv" >/dev/null; then
      fail "$message; expected persistent Linuxbrew shellenv setup"
    fi
  elif grep -F '/home/linuxbrew/.linuxbrew/bin/brew shellenv' "$tmp_dir/home/.zshenv" >/dev/null; then
    fail "$message; Linuxbrew shellenv setup should be absent"
  fi

  pass "$message"
}

chezmoi_bin="$(command -v chezmoi)" || fail "chezmoi is required to render templates"

mkdir -p "$tmp_dir/home/.local/bin"
: >"$tmp_dir/chezmoi.toml"

cat >"$tmp_dir/home/.local/bin/terrapod-local-test-command" <<'STUB'
#!/bin/sh
exit 0
STUB
chmod +x "$tmp_dir/home/.local/bin/terrapod-local-test-command"

render_zshenv '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}'
rendered_linux_zshenv="$(cat "$tmp_dir/home/.zshenv")"
render_zshenv '{"chezmoi":{"os":"darwin"}}'
rendered_macos_zshenv="$(cat "$tmp_dir/home/.zshenv")"

assert_order "$rendered_linux_zshenv" 'path=("$HOME/.local/bin" $path)' 'eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"' "Linuxbrew is placed ahead of user-local legacy commands"
assert_order "$rendered_linux_zshenv" 'eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"' 'source "$path_snippet"' "explicit path snippets run after the managed Homebrew default"
assert_contains "$rendered_macos_zshenv" '/opt/homebrew/bin/brew shellenv' "macOS zshenv configures Apple Silicon Homebrew"
assert_contains "$rendered_macos_zshenv" '/usr/local/bin/brew shellenv' "macOS zshenv configures Intel Homebrew"

assert_lookup_success \
  '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
  "Ubuntu zshenv exposes user-local chezmoi after reconnect"

assert_lookup_success \
  '{"chezmoi":{"os":"darwin"}}' \
  "macOS zshenv exposes user-local binaries by default"

assert_path_snippet_lookup_success \
  '{"chezmoi":{"os":"darwin"}}' \
  "macOS zshenv loads user PATH snippets"

assert_linuxbrew_shellenv_rendering \
  '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableAiCliTools":true,"enableDevelopmentWorkspace":false}' \
  present \
  "Ubuntu Optional AI Tool Stack persists Linuxbrew in new zsh sessions"

assert_linuxbrew_shellenv_rendering \
  '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableAiCliTools":false,"enableDevelopmentWorkspace":false}' \
  present \
  "Ubuntu without the Optional AI Tool Stack persists mandatory Linuxbrew shell setup"

assert_linuxbrew_shellenv_rendering \
  '{"chezmoi":{"os":"darwin"},"enableAiCliTools":true,"enableDevelopmentWorkspace":false}' \
  absent \
  "macOS keeps Linuxbrew shell setup out of zshenv"
