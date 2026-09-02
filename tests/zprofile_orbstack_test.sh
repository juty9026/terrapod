#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

render_zprofile() {
  data="$1"

  "$chezmoi_bin" \
    --config "$tmp_dir/chezmoi.toml" \
    --destination "$tmp_dir/home" \
    --source "$repo_root" \
    execute-template \
    --override-data "$data" \
    --file "$repo_root/dot_zprofile.tmpl" \
    >"$tmp_dir/home/.zprofile"
}

# Sources the rendered .zprofile the way a zsh login shell would, from an
# environment carrying nothing but HOME and a minimal PATH.
source_zprofile() {
  env -i \
    HOME="$tmp_dir/home" \
    PATH="/usr/bin:/bin:/usr/sbin:/sbin" \
    zsh -c '. "$HOME/.zprofile"' \
    >"$tmp_dir/source.out" \
    2>"$tmp_dir/source.err"
}

assert_orbstack_rendering() {
  data="$1"
  expected="$2"
  message="$3"

  render_zprofile "$data"

  if [ "$expected" = present ]; then
    if ! grep -F '. "$HOME/.orbstack/shell/init.zsh"' "$tmp_dir/home/.zprofile" >/dev/null; then
      fail "$message; expected the OrbStack shell integration"
    fi
  elif grep -F '.orbstack' "$tmp_dir/home/.zprofile" >/dev/null; then
    fail "$message; the OrbStack shell integration should be absent"
  fi

  pass "$message"
}

write_orbstack_init() {
  mkdir -p "$tmp_dir/home/.orbstack/shell"
  cat >"$tmp_dir/home/.orbstack/shell/init.zsh" <<'STUB'
print -- "orbstack shell integration loaded"
STUB
}

chezmoi_bin="$(command -v chezmoi)" || fail "chezmoi is required to render templates"
command -v zsh >/dev/null || fail "zsh is required to source the rendered zprofile"

mkdir -p "$tmp_dir/home"
: >"$tmp_dir/chezmoi.toml"

assert_orbstack_rendering \
  '{"chezmoi":{"os":"darwin"}}' \
  absent \
  "macOS without the development-apps App Group keeps OrbStack out of zprofile"

assert_orbstack_rendering \
  '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupDevelopmentApps":false}' \
  absent \
  "an explicitly disabled development-apps App Group keeps OrbStack out of zprofile"

assert_orbstack_rendering \
  '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupMonitoring":true,"enableMacosAppGroupMobileDev":true}' \
  absent \
  "other macOS App Groups do not pull in the OrbStack shell integration"

assert_orbstack_rendering \
  '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupDevelopmentApps":true}' \
  present \
  "the development-apps App Group renders the OrbStack shell integration"

# The rendered file is a login-shell entry point, so every variant has to be
# sourceable. Only the enabled variant can break, but a syntax error in the
# disabled one would be just as fatal.
render_zprofile '{"chezmoi":{"os":"darwin"}}'
if ! source_zprofile; then
  sed 's/^/  /' "$tmp_dir/source.err" >&2
  fail "zprofile without the development-apps App Group is sourceable"
fi
pass "zprofile without the development-apps App Group is sourceable"

render_zprofile '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupDevelopmentApps":true}'
rm -rf "$tmp_dir/home/.orbstack"

if ! source_zprofile; then
  sed 's/^/  /' "$tmp_dir/source.err" >&2
  fail "the readability guard keeps the login shell working when OrbStack is missing"
fi
if [ -s "$tmp_dir/source.err" ]; then
  sed 's/^/  /' "$tmp_dir/source.err" >&2
  fail "a missing OrbStack install produces no login-shell diagnostics"
fi
pass "the readability guard keeps the login shell working when OrbStack is missing"
pass "a missing OrbStack install produces no login-shell diagnostics"

write_orbstack_init
if ! source_zprofile; then
  sed 's/^/  /' "$tmp_dir/source.err" >&2
  fail "an installed OrbStack shell integration is sourced"
fi
if ! grep -F 'orbstack shell integration loaded' "$tmp_dir/source.out" >/dev/null; then
  fail "an installed OrbStack shell integration is sourced"
fi
pass "an installed OrbStack shell integration is sourced"

# An unreadable init.zsh is what the readability guard exists for: OrbStack can
# leave the file behind with the cask removed.
chmod 000 "$tmp_dir/home/.orbstack/shell/init.zsh"
if ! source_zprofile; then
  sed 's/^/  /' "$tmp_dir/source.err" >&2
  fail "an unreadable OrbStack shell integration is skipped instead of failing the login shell"
fi
if [ -s "$tmp_dir/source.err" ]; then
  sed 's/^/  /' "$tmp_dir/source.err" >&2
  fail "an unreadable OrbStack shell integration is skipped instead of failing the login shell"
fi
chmod 644 "$tmp_dir/home/.orbstack/shell/init.zsh"
pass "an unreadable OrbStack shell integration is skipped instead of failing the login shell"
