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

assert_not_contains() {
  haystack="$1"
  needle="$2"
  message="$3"
  if printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi
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

# The Homebrew prefixes are absolute paths a test cannot create, so the rendered
# file is redirected at stub prefixes before it is exercised.
render_zshenv_with_stub_prefixes() {
  data="$1"

  render_zshenv "$data"
  sed \
    -e "s|/opt/homebrew/bin/brew|$tmp_dir/apple-silicon/bin/brew|g" \
    -e "s|/usr/local/bin/brew|$tmp_dir/intel/bin/brew|g" \
    -e "s|/home/linuxbrew/.linuxbrew/bin/brew|$tmp_dir/linuxbrew/bin/brew|g" \
    "$tmp_dir/home/.zshenv" >"$tmp_dir/zshenv.stubbed"
  mv "$tmp_dir/zshenv.stubbed" "$tmp_dir/home/.zshenv"
}

write_brew_stub() {
  prefix="$1"

  mkdir -p "$prefix/bin"
  cat >"$prefix/bin/brew" <<STUB
#!/bin/sh
printf '%s\n' "$prefix" >>"\$BREW_STUB_LOG"
printf '%s\n' 'export HOMEBREW_PREFIX="$prefix"'
printf '%s\n' 'export PATH="$prefix/bin:\$PATH"'
STUB
  chmod +x "$prefix/bin/brew"
}

remove_brew_stub() {
  rm -rf "$1"
}

# Runs a fresh zsh, which reads the rendered .zshenv. An empty first argument
# leaves HOMEBREW_PREFIX unset.
run_zshenv() {
  inherited_prefix="$1"

  : >"$tmp_dir/brew.log"

  if [ -n "$inherited_prefix" ]; then
    env -i \
      HOME="$tmp_dir/home" \
      PATH="/usr/bin:/bin:/usr/sbin:/sbin" \
      BREW_STUB_LOG="$tmp_dir/brew.log" \
      HOMEBREW_PREFIX="$inherited_prefix" \
      zsh -c 'printf "%s\n" "$PATH"' >"$tmp_dir/zshenv.path"
  else
    env -i \
      HOME="$tmp_dir/home" \
      PATH="/usr/bin:/bin:/usr/sbin:/sbin" \
      BREW_STUB_LOG="$tmp_dir/brew.log" \
      zsh -c 'printf "%s\n" "$PATH"' >"$tmp_dir/zshenv.path"
  fi
}

assert_path_contains() {
  needle="$1"
  message="$2"

  if ! grep -F "$needle" "$tmp_dir/zshenv.path" >/dev/null; then
    fail "$message; expected PATH to contain '$needle', got $(cat "$tmp_dir/zshenv.path")"
  fi

  pass "$message"
}

assert_brew_stub_log() {
  expected="$1"
  message="$2"

  actual="$(cat "$tmp_dir/brew.log")"
  if [ "$actual" != "$expected" ]; then
    fail "$message; expected shellenv from '$expected', got '$actual'"
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

render_zshenv '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupMobileDev":true}'
rendered_macos_mobiledev_zshenv="$(cat "$tmp_dir/home/.zshenv")"

assert_contains "$rendered_macos_mobiledev_zshenv" 'path=("$HOME/.local/bin" $path)' \
  "mobile-dev App Group does not remove the \$HOME/.local/bin guard"
assert_order "$rendered_macos_mobiledev_zshenv" 'path=("$HOME/.local/bin" $path)' '/opt/homebrew/bin/brew shellenv' \
  "mobile-dev App Group does not displace \$HOME/.local/bin ahead of the Homebrew shellenv block"
assert_order "$rendered_macos_mobiledev_zshenv" '/opt/homebrew/bin/brew shellenv' 'for path_snippet in "$HOME"/.config/zsh/path.d/*.zsh(N); do' \
  "mobile-dev App Group does not displace the Homebrew shellenv block ahead of machine-local overrides"

assert_contains "$rendered_macos_mobiledev_zshenv" 'export ANDROID_HOME="$HOME/Library/Android/sdk"' \
  "mobile-dev App Group renders the Android SDK environment block"
assert_order "$rendered_macos_mobiledev_zshenv" '/opt/homebrew/bin/brew shellenv' 'export ANDROID_HOME="$HOME/Library/Android/sdk"' \
  "mobile-dev App Group's Android block renders after the Homebrew shellenv block so brew's prefix is already on PATH"
assert_order "$rendered_macos_mobiledev_zshenv" 'android_studio_jbr="/Applications/Android Studio.app/Contents/jbr/Contents/Home"' 'for path_snippet in "$HOME"/.config/zsh/path.d/*.zsh(N); do' \
  "mobile-dev App Group's Android block renders before the machine-local path.d override loop"

if printf '%s\n' "$rendered_macos_zshenv" | grep -F 'export ANDROID_HOME="$HOME/Library/Android/sdk"' >/dev/null; then
  fail "macOS zshenv omits the Android SDK block when the mobile-dev App Group is disabled"
fi
pass "macOS zshenv omits the Android SDK block when the mobile-dev App Group is disabled"

# .zshenv runs for every zsh process, including the non-interactive ones scripts
# and editors spawn, so the Homebrew probe has to be free once the environment
# already carries a prefix — and it has to find whichever prefix is installed,
# not the one the architecture predicts.
assert_not_contains "$rendered_macos_zshenv" 'uname -m' \
  "macOS zshenv does not spawn uname to choose a Homebrew prefix"
assert_not_contains "$rendered_macos_zshenv" 'sysctl.proc_translated' \
  "macOS zshenv does not spawn sysctl to detect Rosetta"
assert_order "$rendered_macos_zshenv" '/opt/homebrew/bin/brew shellenv' '/usr/local/bin/brew shellenv' \
  "macOS zshenv prefers the Apple Silicon prefix over the Intel one"

render_zshenv_with_stub_prefixes '{"chezmoi":{"os":"darwin"}}'

write_brew_stub "$tmp_dir/intel"
run_zshenv ""
assert_path_contains "$tmp_dir/intel/bin" \
  "macOS zshenv finds an Intel-only Homebrew regardless of the reported architecture"
assert_brew_stub_log "$tmp_dir/intel" \
  "macOS zshenv evaluates the Intel shellenv once when it is the only prefix"

write_brew_stub "$tmp_dir/apple-silicon"
run_zshenv ""
assert_path_contains "$tmp_dir/apple-silicon/bin" \
  "macOS zshenv prefers the Apple Silicon prefix when both are installed"
assert_brew_stub_log "$tmp_dir/apple-silicon" \
  "macOS zshenv evaluates only one shellenv when both prefixes are installed"

run_zshenv "$tmp_dir/apple-silicon"
assert_brew_stub_log "" \
  "macOS zshenv skips the Homebrew probe when the environment already carries a prefix"

remove_brew_stub "$tmp_dir/intel"
remove_brew_stub "$tmp_dir/apple-silicon"

render_zshenv_with_stub_prefixes '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}'

write_brew_stub "$tmp_dir/linuxbrew"
run_zshenv ""
assert_path_contains "$tmp_dir/linuxbrew/bin" \
  "Ubuntu zshenv still puts Linuxbrew on PATH"
assert_brew_stub_log "$tmp_dir/linuxbrew" \
  "Ubuntu zshenv evaluates the Linuxbrew shellenv"

run_zshenv "$tmp_dir/linuxbrew"
assert_brew_stub_log "" \
  "Ubuntu zshenv skips the Homebrew probe when the environment already carries a prefix"
