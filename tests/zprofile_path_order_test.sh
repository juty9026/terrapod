#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

# Line order inside a rendered file, the way tests/zshenv_local_bin_test.sh
# asserts it: the first line matching `first` has to come before the first line
# matching `second`.
assert_render_order() {
  awk -v first="$2" -v second="$3" '
    first_line == 0 && index($0, first) { first_line = NR }
    second_line == 0 && index($0, second) { second_line = NR }
    END { exit !(first_line > 0 && second_line > first_line) }
  ' "$1" || fail "$4"
  pass "$4"
}

render_template() {
  template="$1"
  data="$2"
  destination="$3"

  "$chezmoi_bin" \
    --config "$tmp_dir/chezmoi.toml" \
    --destination "$tmp_dir/home" \
    --source "$repo_root" \
    execute-template \
    --override-data "$data" \
    --file "$repo_root/$template" \
    >"$destination"
}

# The Homebrew prefixes .zshenv probes are absolute paths a test cannot create,
# so the rendered file is redirected at a stub prefix before it is exercised.
# .zprofile needs no such redirect: it reuses the HOMEBREW_PREFIX .zshenv
# exported, which is the stub.
render_zshenv_with_stub_prefix() {
  render_template dot_zshenv.tmpl '{"chezmoi":{"os":"darwin"}}' "$tmp_dir/home/.zshenv"
  sed \
    -e "s|/opt/homebrew/bin/brew|$tmp_dir/prefix/bin/brew|g" \
    -e "s|/usr/local/bin/brew|$tmp_dir/absent-intel/bin/brew|g" \
    "$tmp_dir/home/.zshenv" >"$tmp_dir/zshenv.stubbed"
  mv "$tmp_dir/zshenv.stubbed" "$tmp_dir/home/.zshenv"
}

write_brew_stub() {
  prefix="$1"

  mkdir -p "$prefix/bin"
  cat >"$prefix/bin/brew" <<STUB
#!/bin/sh
printf '%s\n' 'export HOMEBREW_PREFIX="$prefix"'
printf '%s\n' 'export PATH="$prefix/bin:\$PATH"'
STUB
  chmod +x "$prefix/bin/brew"
}

# Reproduces the macOS login-shell startup order in one zsh: the shell reads the
# rendered .zshenv on its own, then /etc/zprofile's path_helper call runs, and
# only then does the managed .zprofile get its turn.
run_login_shell() {
  after_path_helper="$1"

  env -i \
    HOME="$tmp_dir/home" \
    PATH="/usr/bin:/bin:/usr/sbin:/sbin" \
    zsh -c "eval \"\$(/usr/libexec/path_helper -s)\"; $after_path_helper print -l \$path" \
    >"$tmp_dir/login.path" \
    2>"$tmp_dir/login.err"
}

# Position of an entry in the login shell's $path, counting from 1. Absent
# entries report 0 so the assertions can tell "behind" from "missing".
path_index() {
  awk -v needle="$1" '$0 == needle { print NR; exit } END { if (!NR) print 0 }' "$tmp_dir/login.path"
}

report_login_path() {
  printf '%s\n' "login-shell \$path:" >&2
  sed 's/^/  /' "$tmp_dir/login.path" >&2
}

assert_path_order() {
  earlier="$1"
  later="$2"
  message="$3"

  earlier_index="$(path_index "$earlier")"
  later_index="$(path_index "$later")"

  if [ "$earlier_index" = 0 ]; then
    report_login_path
    fail "$message; $earlier is not on PATH at all"
  fi
  if [ "$later_index" = 0 ]; then
    report_login_path
    fail "$message; $later is not on PATH at all"
  fi
  if [ "$earlier_index" -ge "$later_index" ]; then
    report_login_path
    fail "$message; $earlier is at $earlier_index, behind $later at $later_index"
  fi

  pass "$message"
}

chezmoi_bin="$(command -v chezmoi)" || fail "chezmoi is required to render templates"
command -v zsh >/dev/null || fail "zsh is required to source the rendered files"

mkdir -p "$tmp_dir/home"
: >"$tmp_dir/chezmoi.toml"

render_template dot_zprofile.tmpl '{"chezmoi":{"os":"darwin"}}' "$tmp_dir/macos.zprofile"
render_template dot_zprofile.tmpl \
  '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
  "$tmp_dir/ubuntu.zprofile"

assert_file_contains "$tmp_dir/macos.zprofile" 'path=("$HOME/.local/bin" $path)' \
  "macOS zprofile re-prepends the user-local directory after path_helper"
assert_file_contains "$tmp_dir/macos.zprofile" 'eval "$("$HOMEBREW_PREFIX/bin/brew" shellenv)"' \
  "macOS zprofile re-evaluates the Homebrew shellenv after path_helper"
assert_file_contains "$tmp_dir/macos.zprofile" 'path_helper' \
  "macOS zprofile records why it repeats the PATH prepends"

# The two prepends run in .zshenv's order, so the login shell ends up with the
# same precedence between them that .zshenv declared.
assert_render_order "$tmp_dir/macos.zprofile" \
  'path=("$HOME/.local/bin" $path)' \
  'eval "$("$HOMEBREW_PREFIX/bin/brew" shellenv)"' \
  "macOS zprofile repeats the prepends in .zshenv's order, so Homebrew still wins"

# Linux has no path_helper: reordering PATH a second time there would only
# invent a second place to keep it correct.
assert_file_not_contains "$tmp_dir/ubuntu.zprofile" 'HOMEBREW_PREFIX' \
  "Ubuntu zprofile leaves the PATH the managed .zshenv built alone"
assert_file_not_contains "$tmp_dir/ubuntu.zprofile" 'path=(' \
  "Ubuntu zprofile does not reorder the path array"

if [ ! -x /usr/libexec/path_helper ]; then
  skip "a login shell puts the Homebrew prefix ahead of /usr/bin (no /usr/libexec/path_helper on this platform)"
  skip "a login shell puts \$HOME/.local/bin ahead of /usr/bin (no /usr/libexec/path_helper on this platform)"
  skip "path_helper is what pushes the Homebrew prefix behind /usr/bin (no /usr/libexec/path_helper on this platform)"
  skip "an installed Homebrew's own shellenv puts its prefix back ahead of /usr/bin (no /usr/libexec/path_helper on this platform)"
  exit 0
fi

mkdir -p "$tmp_dir/home/.local/bin"
write_brew_stub "$tmp_dir/prefix"
render_zshenv_with_stub_prefix
cp "$tmp_dir/macos.zprofile" "$tmp_dir/home/.zprofile"

# The bug the managed .zprofile exists to undo. Asserted first so a host where
# path_helper stopped reordering PATH cannot let the fix pass vacuously.
run_login_shell ""
assert_path_order /usr/bin "$tmp_dir/prefix/bin" \
  "path_helper is what pushes the Homebrew prefix behind /usr/bin"

run_login_shell '. "$HOME/.zprofile";'
if [ -s "$tmp_dir/login.err" ]; then
  sed 's/^/  /' "$tmp_dir/login.err" >&2
  fail "the login shell sources the managed .zprofile without diagnostics"
fi
assert_path_order "$tmp_dir/prefix/bin" /usr/bin \
  "a login shell puts the Homebrew prefix ahead of /usr/bin"
assert_path_order "$tmp_dir/home/.local/bin" /usr/bin \
  "a login shell puts \$HOME/.local/bin ahead of /usr/bin"
assert_path_order "$tmp_dir/prefix/bin" "$tmp_dir/home/.local/bin" \
  "a login shell keeps declared Homebrew tools ahead of user-local commands"

# The stub above pins the template's own logic. Whether re-evaluating
# `brew shellenv` still moves the prefix back to the front is Homebrew's
# behavior rather than the template's — recent versions answer path_helper with
# another path_helper call instead of a plain prepend — so an installed
# Homebrew is also exercised as itself.
brew_bin=""
for brew_candidate in /opt/homebrew/bin/brew /usr/local/bin/brew; do
  if [ -x "$brew_candidate" ]; then
    brew_bin="$brew_candidate"
    break
  fi
done

if [ -z "$brew_bin" ]; then
  skip "an installed Homebrew's own shellenv puts its prefix back ahead of /usr/bin (no Homebrew on this host)"
  exit 0
fi

render_template dot_zshenv.tmpl '{"chezmoi":{"os":"darwin"}}' "$tmp_dir/home/.zshenv"
run_login_shell '. "$HOME/.zprofile";'
assert_path_order "${brew_bin%/bin/brew}/bin" /usr/bin \
  "an installed Homebrew's own shellenv puts its prefix back ahead of /usr/bin"
