#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

mkdir -p "$tmp_dir/home"
: >"$tmp_dir/chezmoi.toml"
render_shell() {
  chezmoi --config "$tmp_dir/chezmoi.toml" --source "$repo_root" \
    execute-template --override-data "$1" \
    --file "$repo_root/dot_$2.tmpl" >"$tmp_dir/home/.$2"
}
render_zshenv() {
  render_shell "$1" zshenv
}
enabled_data='{"chezmoi":{"os":"darwin"},"enableColimaDockerHost":true}'
render_zshenv "$enabled_data"

# -d skips host-wide startup files while still reading the rendered user files.
# Ubuntu's global zshrc can run compinit and prompt about host fpath permissions,
# which is unrelated to the managed Colima environment and fails without a TTY.
actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test \
  zsh -d -c 'sh -c '\''printf "%s" "${DOCKER_HOST-unset}"'\''' 2>"$tmp_dir/stderr")"
assert_equals "$actual" "unix://$tmp_dir/home/.config/colima/default/docker.sock" \
  "enabled macOS exports the default Colima endpoint to child sh before Colima is installed"
assert_equals "$(cat "$tmp_dir/stderr")" "" "missing Colima produces no shell diagnostics"

mkdir -p "$tmp_dir/home/.colima"
actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test \
  zsh -d -c 'printf "%s" "$DOCKER_HOST"')"
assert_equals "$actual" "unix://$tmp_dir/home/.colima/default/docker.sock" \
  "an existing legacy Colima directory takes precedence over managed XDG config"

mkdir -p "$tmp_dir/custom colima"
actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test \
  COLIMA_HOME="$tmp_dir/custom colima" zsh -d -c 'printf "%s" "$DOCKER_HOST"')"
assert_equals "$actual" "unix://$tmp_dir/custom colima/default/docker.sock" \
  "a valid COLIMA_HOME takes precedence over the legacy directory"

actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test \
  COLIMA_HOME="$tmp_dir/missing" zsh -d -c 'printf "%s" "$DOCKER_HOST"')"
assert_equals "$actual" "unix://$tmp_dir/home/.colima/default/docker.sock" \
  "a nonexistent COLIMA_HOME falls back according to Colima rules"
rmdir "$tmp_dir/home/.colima"
actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test \
  COLIMA_HOME= zsh -d -c 'printf "%s" "$DOCKER_HOST"')"
assert_equals "$actual" "unix://$tmp_dir/home/.config/colima/default/docker.sock" \
  "an empty COLIMA_HOME uses managed XDG config"

for existing_host in '' tcp://explicit.example:2376; do
  actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test \
    DOCKER_HOST="$existing_host" zsh -d -c 'sh -c '\''printf "%s" "${DOCKER_HOST-unset}"'\''')"
  assert_equals "$actual" "$existing_host" "an explicit DOCKER_HOST is preserved, including an empty value"
done

# Use a filesystem boundary stub to detect runtime commands without starting
# any daemon. Test HOME has neither a socket nor runtime configuration.
mkdir -p "$tmp_dir/home/.local/bin"
for runtime in colima docker supabase; do
  cat >"$tmp_dir/home/.local/bin/$runtime" <<'STUB'
#!/bin/sh
printf '%s\n' "$0 $*" >>"$HOME/runtime-calls"
exit 99
STUB
  chmod +x "$tmp_dir/home/.local/bin/$runtime"
done
for mode in -c -ic -lc; do
  actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test TERM=dumb \
    zsh -d "$mode" 'sh -c '\''printf "%s" "$DOCKER_HOST"'\''' 2>"$tmp_dir/stderr")"
  assert_equals "$actual" "unix://$tmp_dir/home/.config/colima/default/docker.sock" \
    "$mode zsh exports the endpoint without runtime commands"
  assert_equals "$(cat "$tmp_dir/stderr")" "" "$mode zsh starts quietly"
done
[ ! -e "$tmp_dir/home/runtime-calls" ] || fail "shell startup must not invoke a runtime"
pass "shell startup never invokes Colima, Docker, or Supabase"

# The installed OrbStack integration extends PATH in login shells.
mkdir -p "$tmp_dir/home/.orbstack/shell" "$tmp_dir/home/.docker"
printf '%s\n' 'path=("$HOME/.orbstack/bin" $path)' >"$tmp_dir/home/.orbstack/shell/init.zsh"
printf '%s\n' '{"currentContext":"kept-context"}' >"$tmp_dir/home/.docker/config.json"
ln -s "$tmp_dir/home/.orbstack/run/docker.sock" "$tmp_dir/home/docker.sock"
render_shell '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupDevelopmentApps":true}' zprofile
actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test \
  DOCKER_CONTEXT=kept-context zsh -d -lc 'printf "%s|%s|%s" "$DOCKER_HOST" "$DOCKER_CONTEXT" "${path[1]}"')"
assert_equals "$actual" "unix://$tmp_dir/home/.config/colima/default/docker.sock|kept-context|$tmp_dir/home/.orbstack/bin" \
  "login shells coexist with OrbStack and preserve DOCKER_CONTEXT"
assert_equals "$(cat "$tmp_dir/home/.docker/config.json")" '{"currentContext":"kept-context"}' "saved Docker context stays unchanged"
assert_equals "$(readlink "$tmp_dir/home/docker.sock")" "$tmp_dir/home/.orbstack/run/docker.sock" "existing socket links stay unchanged"

mkdir -p "$tmp_dir/home/.config/zsh/path.d"
printf '%s\n' 'export DOCKER_HOST=tcp://local-override:2376' >"$tmp_dir/home/.config/zsh/path.d/docker.zsh"
actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test \
  zsh -d -c 'printf "%s" "$DOCKER_HOST"')"
assert_equals "$actual" tcp://local-override:2376 "machine-local overrides run after managed Colima selection"
rm "$tmp_dir/home/.config/zsh/path.d/docker.zsh"

for data in \
  '{"chezmoi":{"os":"darwin"}}' \
  '{"chezmoi":{"os":"darwin"},"enableColimaDockerHost":false,"enableDevelopmentWorkspace":true,"enableMacosAppGroupDevelopmentApps":true}' \
  '{"chezmoi":{"os":"linux"},"enableColimaDockerHost":true}'; do
  render_zshenv "$data"
  actual="$(env -i HOME="$tmp_dir/home" PATH=/usr/bin:/bin HOMEBREW_PREFIX=/test \
    zsh -d -c 'printf "%s" "${DOCKER_HOST-unset}"')"
  assert_equals "$actual" unset "disabled macOS and VPS do not select a Docker host"
done
