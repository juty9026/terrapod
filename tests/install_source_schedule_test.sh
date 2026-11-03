#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
chezmoi_bin="$(command -v chezmoi)"

# Exercise real scheduling and persistent chezmoi state, replacing only the
# external package-manager and font-helper processes.
new_fixture() {
  fixture="$tmp_dir/$1"
  source_dir="$fixture/source"
  mkdir -p "$source_dir/.chezmoiscripts" "$source_dir/dot_local/lib" "$fixture/home" "$fixture/bin"
  cp -R "$repo_root/dot_local/lib/terrapod" "$source_dir/dot_local/lib/"
  for template in "$repo_root"/.chezmoiscripts/*bootstrap-ubuntu* "$repo_root"/.chezmoiscripts/*ubuntu-bootstrap* "$repo_root"/.chezmoiscripts/*jetendard-font*; do
    [ ! -f "$template" ] || cp "$template" "$source_dir/.chezmoiscripts/"
  done
  printf '%s\n' '.local' >"$source_dir/.chezmoiignore"
  : >"$fixture/config.toml"
  : >"$fixture/calls"
  write_stub "$source_dir/.chezmoiscripts/run_before_05-observe.sh" 'echo before >>"$SCHEDULE_PHASE_LOG"'
  write_stub "$source_dir/.chezmoiscripts/run_after_70-observe.sh" 'echo settings >>"$SCHEDULE_PHASE_LOG"'
  write_stub "$fixture/bin/id" 'case "$1" in -u) echo 1000 ;; -un) echo testuser ;; esac'
  write_stub "$fixture/bin/sudo" 'echo sudo >>"$SCHEDULE_LOG"' 'exec "$@"'
  write_stub "$fixture/bin/apt-get" \
    'echo "apt $1" >>"$SCHEDULE_LOG"' \
    '[ "$1" != install ] || echo ubuntu >>"$SCHEDULE_PHASE_LOG"' \
    'if [ "$1" = install ] && [ "${INTERRUPT_INSTALL:-0}" = 1 ]; then kill -TERM "$PPID"; exit 143; fi' \
    '[ "$1" != install ] || exit "${INSTALL_EXIT:-0}"'
  write_stub "$fixture/bin/getent" 'echo "testuser:x:1000:1000::/home/testuser:/usr/bin/zsh"'
  write_stub "$fixture/bin/python3" \
    'echo font >>"$SCHEDULE_LOG"' \
    'echo font >>"$SCHEDULE_PHASE_LOG"' \
    'if [ "${INTERRUPT_INSTALL:-0}" = 1 ]; then kill -TERM "$PPID"; exit 143; fi' \
    'exit "${INSTALL_EXIT:-0}"'
  write_stub "$fixture/bin/mv" \
    'for arg do case "$arg" in */install-sources/*) [ "${FAIL_STATE_MV:-0}" != 1 ] || exit 1 ;; esac; done' \
    'exec /bin/mv "$@"'
  write_stub "$fixture/bin/rm" \
    'for arg do case "$arg" in */install-warnings/*) [ "${FAIL_WARNING_CLEAR:-0}" != 1 ] || exit 1 ;; esac; done' \
    'exec /bin/rm "$@"'
  state_home="$fixture/state"
  case "$1" in
    ubuntu*)
      profile_os=linux; category=ubuntu-bootstrap; install_call='apt install'
      body=ubuntu-bootstrap.sh
      entry=run_before_00-bootstrap-ubuntu.sh.tmpl
      ;;
    *)
      profile_os=darwin; category=jetendard-font; install_call=font
      body=jetendard-font-install.sh
      entry=run_after_65-install-jetendard-font.sh.tmpl
      ;;
  esac
  record="$state_home/terrapod/install-sources/$category"
  marker="$state_home/terrapod/install-warnings/$category"
  profile_release=24.04
  install_exit=0
  fail_state_mv=0
  fail_warning_clear=0
  interrupt_install=0
}

apply_fixture() {
  apply_status=0
  HOME="$fixture/home" XDG_STATE_HOME="$state_home" \
    XDG_CONFIG_HOME="$fixture/xdg-config" XDG_DATA_HOME="$fixture/xdg-data" \
    PATH="$fixture/bin:$PATH" SCHEDULE_LOG="$fixture/calls" INSTALL_EXIT="${install_exit:-0}" \
    SCHEDULE_PHASE_LOG="$fixture/phases" \
    FAIL_STATE_MV="$fail_state_mv" FAIL_WARNING_CLEAR="$fail_warning_clear" INTERRUPT_INSTALL="$interrupt_install" \
    "$chezmoi_bin" --config "$fixture/config.toml" --source "$source_dir" \
    --destination "$fixture/home" --cache "$fixture/cache" \
    --persistent-state "$fixture/chezmoi-state.boltdb" \
    --override-data "{\"chezmoi\":{\"os\":\"$profile_os\",\"osRelease\":{\"id\":\"ubuntu\",\"versionID\":\"$profile_release\"}}}" \
    apply >"$fixture/output" 2>&1 || apply_status=$?
}

assert_apply_calls() {
  expected_calls="$1"
  label="$2"
  : >"$fixture/calls"
  apply_fixture
  if [ "$apply_status" != 0 ]; then cat "$fixture/output" >&2; fail "$label (apply failed)"; fi
  assert_equals "$(grep -c "^$install_call$" "$fixture/calls" || true)" "$expected_calls" "$label"
  if [ "$expected_calls" = 0 ]; then
    assert_equals "$(cat "$fixture/calls")" '' "$label (no sudo, APT or font work)"
  fi
}

new_fixture ubuntu-first-failure
profile_os=linux
install_exit=42
apply_fixture
assert_status "$apply_status" 0 "a recorded Ubuntu install failure remains non-blocking"
assert_equals "$(grep -c '^apt install$' "$fixture/calls")" 1 \
  "a fresh failing apply attempts Ubuntu installation once"

for profile in ubuntu jetendard; do
  new_fixture "$profile-matrix"
  assert_apply_calls 1 "$profile establishes successful state on its first apply"
  if [ "$profile" = ubuntu ]; then expected_phases='ubuntu
before
settings'; else expected_phases='before
font
settings'; fi
  assert_equals "$(cat "$fixture/phases")" "$expected_phases" "$profile preserves install and font-settings phase ordering"
  cp "$record" "$fixture/baseline"
  for previous in matches differs missing invalid; do
    for warning in no yes; do
      for outcome in success failure; do
        cp "$fixture/baseline" "$record"
        cp "$repo_root/dot_local/lib/terrapod/$body" "$source_dir/dot_local/lib/terrapod/$body"
        case "$previous" in
          differs) printf '\n# installation source changed\n' >>"$source_dir/dot_local/lib/terrapod/$body" ;;
          missing) rm "$record" ;;
          invalid) printf 'invalid fingerprint\n' >"$record" ;;
        esac
        rm -f "$marker"
        if [ "$warning" = yes ]; then mkdir -p "${marker%/*}"; printf 'outstanding warning\n' >"$marker"; fi
        install_exit=0
        [ "$outcome" = success ] || install_exit=2
        expected_calls=1
        if [ "$previous" = matches ] && [ "$warning" = no ]; then expected_calls=0; fi
        before_record="$(cat "$record" 2>/dev/null || true)"
        label="$profile: fingerprint=$previous warning=$warning outcome=$outcome"
        assert_apply_calls "$expected_calls" "$label"
        if [ "$expected_calls" = 1 ] && [ "$outcome" = failure ]; then
          assert_equals "$(cat "$record" 2>/dev/null || true)" "$before_record" "$label preserves prior successful state"
          assert_file_exists "$marker" "$label retains a retry warning"
        elif [ "$outcome" = success ]; then
          assert_file_exists "$record" "$label saves successful state"
          [ ! -e "$marker" ] || fail "$label clears warning"
        fi
      done
    done
  done

  new_fixture "$profile-recovery"
  install_exit=2
  assert_apply_calls 1 "$profile fresh failure is attempted once"
  [ ! -e "$record" ] || fail "$profile failure must not save successful state"
  assert_apply_calls 1 "$profile unchanged failure retries once on the next apply"
  install_exit=0
  assert_apply_calls 1 "$profile recovery installs once"
  assert_apply_calls 0 "$profile unchanged successful source is offline"

  # Corrupt records must not accidentally compare equal after newline trimming.
  printf '\n' >>"$record"
  assert_apply_calls 1 "$profile rejects a malformed record with extra trailing bytes"

  # Edits to the adapter itself still count as installation-source changes.
  printf '\n# entry point changed\n' >>"$source_dir/.chezmoiscripts/$entry"
  assert_apply_calls 1 "$profile entry point change triggers installation"
  for library in install-warnings.sh install-warning-script.sh jetendard-font-status.sh; do
    printf '\n# guidance or warning maintenance\n' >>"$source_dir/dot_local/lib/terrapod/$library"
    assert_apply_calls 0 "$profile $library edit does not trigger installation"
  done
  if [ "$profile" = jetendard ]; then
    printf '\n# font helper changed\n' >>"$source_dir/dot_local/lib/terrapod/executable_jetendard-font"
    assert_apply_calls 1 "font helper change triggers installation"
    for helper_status in 2 3 4 1; do
      printf 'outstanding warning\n' >"$marker"
      install_exit="$helper_status"
      assert_apply_calls 1 "font helper exit $helper_status records a warning after one attempt"
      case "$helper_status" in
        2) guidance='GitHub API rate limit reached while resolving the Jetendard release. Export a temporary GITHUB_TOKEN or run gh auth login, then rerun tpod apply.' ;;
        3) guidance='GitHub was unreachable while resolving the Jetendard release. Restore network access, then rerun tpod apply.' ;;
        4) guidance='The latest Jetendard GitHub release is not installable. Wait for a corrected upstream release, then rerun tpod apply.' ;;
        1) guidance='Restore Python and GitHub access, then rerun tpod apply.' ;;
      esac
      assert_file_contains "$marker" "$guidance" "font helper exit $helper_status preserves recovery guidance"
    done
    install_exit=0
    assert_apply_calls 1 "font recovery clears categorized helper failures"
    assert_file_contains "$fixture/output" 'Jetendard is ready.' "font success reports readiness"
  else
    # Release inputs are independent of source file bytes.
    profile_release=22.04
    assert_apply_calls 0 "unsupported Ubuntu release does no APT work"
    assert_file_exists "$marker" "unsupported Ubuntu release stays pending"
    profile_release=24.04
    assert_apply_calls 1 "restoring the supported Ubuntu release retries installation"
  fi

  cp "$record" "$fixture/previous-success"
  printf '\n# install body changed\n' >>"$source_dir/dot_local/lib/terrapod/$body"
  interrupt_install=1
  apply_fixture
  [ "$apply_status" != 0 ] || fail "$profile interrupted installation must stop apply"
  assert_equals "$(cat "$record")" "$(cat "$fixture/previous-success")" "$profile interruption preserves successful state"
  interrupt_install=0
  fail_state_mv=1
  assert_apply_calls 1 "$profile state publication failure remains non-blocking when recorded"
  assert_equals "$(cat "$record")" "$(cat "$fixture/previous-success")" "$profile failed atomic rename preserves prior state"
  assert_file_contains "$marker" 'Restore write access to' "$profile failed state publication records recovery guidance"
  assert_file_contains "$fixture/output" 'could not save successful installation state' "$profile state failure is visible"
  assert_equals "$(find "${record%/*}" -name '.success.*' -print)" '' "$profile failed publication cleans staging files"
  fail_state_mv=0
  assert_apply_calls 1 "$profile retries after successful-state write failure"

  # A successful body whose marker cannot be cleared is still pending.
  cp "$record" "$fixture/previous-success"
  printf '\n# next source\n' >>"$source_dir/dot_local/lib/terrapod/$body"
  printf 'outstanding warning\n' >"$marker"
  fail_warning_clear=1
  assert_apply_calls 1 "$profile warning cleanup failure remains non-blocking"
  assert_equals "$(cat "$record")" "$(cat "$fixture/previous-success")" "$profile warning cleanup failure does not advance state"
  assert_file_exists "$marker" "$profile warning cleanup failure leaves a retry trigger"
  fail_warning_clear=0
  assert_apply_calls 1 "$profile retries after warning cleanup failure"
  assert_apply_calls 0 "$profile recovered cleanup stops further installs"

  # Both marker persistence and successful-state persistence can fail together.
  rm -rf "${marker%/*}"
  printf 'blocked warning directory\n' >"${marker%/*}"
  printf '\n# another source\n' >>"$source_dir/dot_local/lib/terrapod/$body"
  fail_state_mv=1
  apply_fixture
  [ "$apply_status" != 0 ] || fail "$profile unrecordable state failure must stop apply"
  pass "$profile state failure is fatal when its warning cannot be written"
  fail_state_mv=0
  install_exit=2
  apply_fixture
  [ "$apply_status" != 0 ] || fail "$profile unrecordable install failure must stop apply"
  pass "$profile install failure is fatal when its warning cannot be written"

  new_fixture "$profile-blocked-state"
  mkdir -p "${record%/*}"
  # A non-file record is invalid and may not be used as mv's destination dir.
  mkdir "$record"
  assert_apply_calls 1 "$profile invalid record directory leaves a recovery warning"
  assert_file_exists "$marker" "$profile blocked state remains eligible through its warning"
  assert_equals "$(find "$record" -type f -print)" '' "$profile does not publish inside a record directory"
  rm -rf "${record%/*}"
  printf 'blocked successful-state directory\n' >"${record%/*}"
  assert_apply_calls 1 "$profile blocked state directory remains non-blocking when recorded"
  assert_file_exists "$marker" "$profile state directory failure is visible"
  rm "${record%/*}"
  assert_apply_calls 1 "$profile recovers from a blocked state directory"

  new_fixture "$profile-migration"
  cp "$source_dir/.chezmoiscripts/$entry" "$fixture/current-entry"
  rm "$source_dir/.chezmoiscripts/$entry"
  old_entry="$(printf '%s' "$entry" | sed 's/^run_/run_onchange_/')"
  # Seed real chezmoi onchange state with an old marker-and-success outcome.
  # The old exit status cannot tell Terrapod whether installation succeeded.
  printf '#!/bin/sh\nexit 0\n' >"$source_dir/.chezmoiscripts/$old_entry"
  apply_fixture
  assert_status "$apply_status" 0 "$profile migration fixture records old chezmoi success"
  rm "$source_dir/.chezmoiscripts/$old_entry"
  cp "$fixture/current-entry" "$source_dir/.chezmoiscripts/$entry"
  assert_apply_calls 1 "$profile migration installs once despite old onchange success state"
  assert_apply_calls 0 "$profile migrated successful source stays offline"

  new_fixture "$profile-home-fallback"
  state_home=''
  record="$fixture/home/.local/state/terrapod/install-sources/$category"
  assert_apply_calls 1 "$profile supports the home-state fallback"
  assert_file_exists "$record" "$profile stores a successful fingerprint under HOME"
  assert_apply_calls 0 "$profile home-state fallback suppresses unchanged installations"
  fallback_warnings="$fixture/home/.local/state/terrapod/install-warnings"
  mkdir -p "$fallback_warnings"
  printf 'retired warning\n' >"$fallback_warnings/retired-category"
  HOME="$fixture/home" XDG_STATE_HOME='' sh -c '. "$1"; terrapod_install_warning_prune' sh \
    "$source_dir/dot_local/lib/terrapod/install-warnings.sh" >/dev/null
  [ ! -e "$fallback_warnings/retired-category" ] || fail "$profile warning prune removes retired markers"
  assert_file_exists "$record" "$profile warning pruning preserves successful state"
done

new_fixture ubuntu-independent-state
assert_apply_calls 1 "Ubuntu publishes its own successful state"
ubuntu_record="$record"
cp "$ubuntu_record" "$fixture/ubuntu-success"
profile_os=darwin
category=jetendard-font
install_call=font
record="$state_home/terrapod/install-sources/$category"
assert_apply_calls 1 "Jetendard independently publishes its successful state"
assert_equals "$(cat "$ubuntu_record")" "$(cat "$fixture/ubuntu-success")" \
  "Jetendard publication preserves Ubuntu's record"
profile_os=linux
install_call='apt install'
assert_apply_calls 0 "Ubuntu remains complete after Jetendard publication"
