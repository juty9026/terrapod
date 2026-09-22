#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir

mkdir -p "$tmp_dir/bin"

rendered_linux="$tmp_dir/gh-extensions-linux.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux"}}' \
  --file "$repo_root/.chezmoiscripts/run_before_20-install-gh-extensions.sh.tmpl" \
  >"$rendered_linux"

sh -n "$rendered_linux" || fail "rendered Linux GitHub CLI Extension Set script should be valid sh"
pass "rendered Linux GitHub CLI Extension Set script is valid sh"

rendered_darwin="$tmp_dir/gh-extensions-darwin.sh"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"darwin"}}' \
  --file "$repo_root/.chezmoiscripts/run_before_20-install-gh-extensions.sh.tmpl" \
  >"$rendered_darwin"

sh -n "$rendered_darwin" || fail "rendered macOS GitHub CLI Extension Set script should be valid sh"
pass "rendered macOS GitHub CLI Extension Set script is valid sh"

assert_contains "$(cat "$rendered_linux")" "github/gh-stack" "rendered script declares github/gh-stack"

# The script resolves gh from the standard Homebrew prefix, so the test stub
# has to live at the exact hardcoded path the script embeds. TERRAPOD_MACHINE_ARCH
# pins hardware-arch detection so the substitution below always targets the
# right literal path regardless of the host running the test.
gh_extensions_script="$tmp_dir/gh-extensions.sh"
sed 's#/home/linuxbrew/.linuxbrew/bin/gh#'"$tmp_dir"'/gh-bin/gh#g' "$rendered_linux" >"$gh_extensions_script"

gh_bin_dir="$tmp_dir/gh-bin"
mkdir -p "$gh_bin_dir"

write_stub "$gh_bin_dir/gh" \
  'printf "%s\n" "gh args:$*" >>"$GH_EXTENSIONS_TEST_LOG"' \
  'case "$1 $2" in' \
  '  "extension list")' \
  '    for installed in $GH_EXTENSIONS_INSTALLED; do' \
  '      printf "%s\t%s\t%s\n" "${installed##*/}" "$installed" v0.1.1' \
  '    done' \
  '    exit 0' \
  '    ;;' \
  '  "extension install")' \
  '    exit "${GH_EXTENSIONS_INSTALL_STATUS:-0}"' \
  '    ;;' \
  'esac' \
  'exit 0'

export TERRAPOD_MACHINE_ARCH=aarch64
export GH_EXTENSIONS_TEST_LOG="$tmp_dir/gh-extensions.log"

# (a) extension absent: exactly one install call, no upgrade, no --pin, no marker.
absent_home="$tmp_dir/absent-home"
absent_state="$tmp_dir/absent-state"
mkdir -p "$absent_home"
: >"$GH_EXTENSIONS_TEST_LOG"
GH_EXTENSIONS_INSTALLED="" \
  HOME="$absent_home" \
  XDG_STATE_HOME="$absent_state" \
  sh "$gh_extensions_script"

absent_log="$(cat "$GH_EXTENSIONS_TEST_LOG")"
absent_install_calls="$(printf '%s\n' "$absent_log" | grep -c '^gh args:extension install github/gh-stack$' || true)"
if [ "$absent_install_calls" -ne 1 ]; then
  harness_report_text "expected exactly one install call" "$absent_log"
  fail "gh extensions installs exactly once when an extension is absent"
fi
pass "gh extensions installs exactly once when an extension is absent"

assert_not_contains "$absent_log" "extension upgrade" "gh extensions never calls extension upgrade"
assert_not_contains "$absent_log" "--pin" "gh extensions never passes a --pin tag"

absent_marker="$absent_state/terrapod/install-warnings/gh-extensions"
if [ -e "$absent_marker" ]; then
  fail "gh extensions leaves no warning marker after a successful install"
fi
pass "gh extensions leaves no warning marker after a successful install"

# (b) extension present: no install call, and a pre-existing marker is cleared.
present_home="$tmp_dir/present-home"
present_state="$tmp_dir/present-state"
mkdir -p "$present_home"
HOME="$present_home" XDG_STATE_HOME="$present_state" sh -c \
  '. "$1"; terrapod_install_warning_write gh-extensions "GitHub CLI Extension Set install needs attention" "Previous gh extensions warning."' \
  sh "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

: >"$GH_EXTENSIONS_TEST_LOG"
GH_EXTENSIONS_INSTALLED="github/gh-stack" \
  HOME="$present_home" \
  XDG_STATE_HOME="$present_state" \
  sh "$gh_extensions_script"

present_log="$(cat "$GH_EXTENSIONS_TEST_LOG")"
assert_not_contains "$present_log" "extension install" "gh extensions does not install an already-present extension"

present_marker="$present_state/terrapod/install-warnings/gh-extensions"
if [ -e "$present_marker" ]; then
  fail "gh extensions clears a pre-existing marker once every declared extension is present"
fi
pass "gh extensions clears a pre-existing marker once every declared extension is present"

# (c) install fails: a marker exists with the extension name in its guidance,
# and the script still exits zero. This also proves the gh-extensions category
# is registered: an unregistered category would fail the marker write and
# make exit_after_install_warning exit 1 instead.
failure_home="$tmp_dir/failure-home"
failure_state="$tmp_dir/failure-state"
mkdir -p "$failure_home"
: >"$GH_EXTENSIONS_TEST_LOG"
failure_status=0
GH_EXTENSIONS_INSTALLED="" \
  GH_EXTENSIONS_INSTALL_STATUS=17 \
  HOME="$failure_home" \
  XDG_STATE_HOME="$failure_state" \
  sh "$gh_extensions_script" || failure_status=$?
if [ "$failure_status" -ne 0 ]; then
  fail "gh extensions exits zero after recording an install failure warning"
fi
pass "gh extensions exits zero after recording an install failure warning"

failure_marker="$failure_state/terrapod/install-warnings/gh-extensions"
if [ ! -f "$failure_marker" ]; then
  fail "gh extensions records a warning marker when an install fails"
fi
pass "gh extensions records a warning marker when an install fails"

failure_marker_text="$(cat "$failure_marker")"
assert_contains "$failure_marker_text" "summary='GitHub CLI Extension Set install needs attention'" "gh extensions failure marker keeps the expected summary"
assert_contains "$failure_marker_text" "github/gh-stack" "gh extensions failure marker names the failed extension"

# (d) gh missing: marker recorded, exit zero, no install attempted.
missing_gh_home="$tmp_dir/missing-gh-home"
missing_gh_state="$tmp_dir/missing-gh-state"
mkdir -p "$missing_gh_home"
: >"$GH_EXTENSIONS_TEST_LOG"
missing_gh_script="$tmp_dir/gh-extensions-missing-gh.sh"
sed 's#/home/linuxbrew/.linuxbrew/bin/gh#'"$tmp_dir"'/missing-gh-bin/gh#g' "$rendered_linux" >"$missing_gh_script"

missing_gh_status=0
HOME="$missing_gh_home" \
  XDG_STATE_HOME="$missing_gh_state" \
  TERRAPOD_MACHINE_ARCH=aarch64 \
  sh "$missing_gh_script" || missing_gh_status=$?
if [ "$missing_gh_status" -ne 0 ]; then
  fail "gh extensions exits zero when gh itself is missing"
fi
pass "gh extensions exits zero when gh itself is missing"

if [ -s "$GH_EXTENSIONS_TEST_LOG" ]; then
  fail "gh extensions attempts no install when gh itself is missing"
fi
pass "gh extensions attempts no install when gh itself is missing"

missing_gh_marker="$missing_gh_state/terrapod/install-warnings/gh-extensions"
if [ ! -f "$missing_gh_marker" ]; then
  fail "gh extensions records a warning marker when gh itself is missing"
fi
pass "gh extensions records a warning marker when gh itself is missing"

missing_gh_marker_text="$(cat "$missing_gh_marker")"
assert_contains "$missing_gh_marker_text" "Homebrew core bundle" "gh extensions missing-gh marker points at the Homebrew core install warning"

# Successful rerun after a recorded failure clears the marker.
: >"$GH_EXTENSIONS_TEST_LOG"
GH_EXTENSIONS_INSTALLED="" \
  HOME="$failure_home" \
  XDG_STATE_HOME="$failure_state" \
  sh "$gh_extensions_script"
if [ -e "$failure_marker" ]; then
  fail "gh extensions clears its warning marker after a successful rerun"
fi
pass "gh extensions clears its warning marker after a successful rerun"

unset TERRAPOD_MACHINE_ARCH GH_EXTENSIONS_TEST_LOG
