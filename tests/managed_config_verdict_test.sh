#!/bin/sh
set -eu

# Behavior tests for the managed config verdict in the shared reader. The
# verdict answers "what state is this managed Terrapod Setup config in" once,
# for a path it is handed, so tpod and the first-run installer print and exit
# on the answer instead of rebuilding the usable, supported, complete ladder.
# The reader is sourced directly, and under dash when it is available, because
# the installer runs it under dash on Ubuntu.

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

reader="$repo_root/dot_local/lib/terrapod/config-toml.sh"

# The reader's load contract: callers define fatal() before use. The verdict
# must never call it; a config problem is an answer, not a stop.
fatal() {
  printf '%s\n' "fatal called: $1" >&2
  exit 97
}

. "$reader"

all_keys() {
  managed_setup_keys | tr '\n' ',' | sed 's/,$//; s/,/, /g'
}

# A config with every managed key present, from the schema itself so the test
# does not keep its own copy of the key list.
write_complete_config() {
  {
    printf '%s\n' '[data]'
    printf '%s\n' 'profile = "macos-terminal"'
    managed_setting_keys | while IFS= read -r key; do
      printf '%s = false\n' "$key"
    done
  } >"$1"
}

# --- missing ---------------------------------------------------------------

missing_path="$tmp_dir/absent.toml"
verdict="$(managed_setup_config_verdict "$missing_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "missing" ] || fail "a missing path is reported as missing"
[ "$(managed_setup_verdict_detail "$verdict")" = "$(all_keys)" ] ||
  fail "a missing path is missing every managed key, in schema order: $(managed_setup_verdict_detail "$verdict")"
pass "a missing path lists every managed key as missing, in schema order"

# --- non-regular -----------------------------------------------------------

directory_path="$tmp_dir/a-directory"
mkdir "$directory_path"
verdict="$(managed_setup_config_verdict "$directory_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "non-regular" ] || fail "a directory is reported as non-regular"
[ -z "$(managed_setup_verdict_detail "$verdict")" ] || fail "a non-regular verdict carries no detail"
pass "a directory is reported as non-regular with no detail"

dangling_path="$tmp_dir/dangling.toml"
ln -s "$tmp_dir/nowhere.toml" "$dangling_path"
verdict="$(managed_setup_config_verdict "$dangling_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "non-regular" ] || fail "a dangling symlink is reported as non-regular"
pass "a dangling symlink is reported as non-regular"

linked_path="$tmp_dir/linked.toml"
write_complete_config "$tmp_dir/link-target.toml"
ln -s "$tmp_dir/link-target.toml" "$linked_path"
verdict="$(managed_setup_config_verdict "$linked_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "complete" ] ||
  fail "a symlink to a regular file reads as that file"
pass "a symlink to a regular file reads as that file"

# --- unreadable ------------------------------------------------------------

unreadable_path="$tmp_dir/unreadable.toml"
write_complete_config "$unreadable_path"
chmod 000 "$unreadable_path"
if [ -r "$unreadable_path" ]; then
  # Root reads through mode 000, so the state cannot be produced.
  skip "an unreadable file is reported as unreadable (this user reads mode 000 files)"
else
  verdict="$(managed_setup_config_verdict "$unreadable_path")"
  chmod 644 "$unreadable_path"
  [ "$(managed_setup_verdict_state "$verdict")" = "unreadable" ] || fail "an unreadable file is reported as unreadable"
  [ -z "$(managed_setup_verdict_detail "$verdict")" ] || fail "an unreadable verdict carries no detail"
  pass "an unreadable file is reported as unreadable with no detail"
fi
chmod 644 "$unreadable_path"

# --- unsupported -----------------------------------------------------------

multiline_string_path="$tmp_dir/multiline-string.toml"
# Only `profile` is present, so the other managed keys are missing too:
# unsupported syntax outranks missing keys (file state, then syntax, then keys).
cat >"$multiline_string_path" <<'TOML'
notes = """
kept
"""

[data]
profile = "macos-terminal"
TOML
verdict="$(managed_setup_config_verdict "$multiline_string_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "unsupported" ] || fail "a multiline string is reported as unsupported"
[ "$(managed_setup_verdict_detail "$verdict")" = "$(unsupported_managed_config_problem_message "$multiline_string_path")" ] ||
  fail "the unsupported detail is the multiline string problem message"
assert_contains "$(managed_setup_verdict_detail "$verdict")" "unsupported multiline string in config" "a multiline string is reported as unsupported with its problem message"

multiline_array_path="$tmp_dir/multiline-array.toml"
cat >"$multiline_array_path" <<'TOML'
extras = [
  [section]
]

[data]
profile = "macos-terminal"
TOML
verdict="$(managed_setup_config_verdict "$multiline_array_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "unsupported" ] || fail "a section-like multiline array is reported as unsupported"
assert_contains "$(managed_setup_verdict_detail "$verdict")" "unsupported multiline array with section-like entries in config" "a section-like multiline array is reported as unsupported with its problem message"

inline_table_path="$tmp_dir/inline-table.toml"
cat >"$inline_table_path" <<'TOML'
data = { profile = "macos-terminal" }
TOML
verdict="$(managed_setup_config_verdict "$inline_table_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "unsupported" ] || fail "an inline data table is reported as unsupported"
assert_contains "$(managed_setup_verdict_detail "$verdict")" "unsupported inline data table in config" "an inline data table is reported as unsupported with its problem message"

# --- incomplete ------------------------------------------------------------

no_profile_path="$tmp_dir/no-profile.toml"
write_complete_config "$no_profile_path"
grep -v '^profile = ' "$no_profile_path" >"$no_profile_path.next"
mv "$no_profile_path.next" "$no_profile_path"
verdict="$(managed_setup_config_verdict "$no_profile_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "incomplete" ] || fail "a config without profile is incomplete"
[ "$(managed_setup_verdict_detail "$verdict")" = "profile" ] || fail "the detail names exactly the missing profile key: $(managed_setup_verdict_detail "$verdict")"
pass "a config without profile is incomplete and the detail names profile"

missing_key="$(managed_setting_keys | sed -n '2p')"
one_missing_path="$tmp_dir/one-missing.toml"
write_complete_config "$one_missing_path"
grep -v "^$missing_key = " "$one_missing_path" >"$one_missing_path.next"
mv "$one_missing_path.next" "$one_missing_path"
verdict="$(managed_setup_config_verdict "$one_missing_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "incomplete" ] || fail "a config missing one Managed Setting key is incomplete"
[ "$(managed_setup_verdict_detail "$verdict")" = "$missing_key" ] ||
  fail "the detail names exactly the missing key $missing_key: $(managed_setup_verdict_detail "$verdict")"
pass "a config missing one Managed Setting key is incomplete and the detail names exactly that key"

two_missing_path="$tmp_dir/two-missing.toml"
write_complete_config "$two_missing_path"
first_key="$(managed_setting_keys | sed -n '1p')"
last_key="$(managed_setting_keys | sed -n '$p')"
grep -v -e "^$first_key = " -e "^$last_key = " "$two_missing_path" >"$two_missing_path.next"
mv "$two_missing_path.next" "$two_missing_path"
verdict="$(managed_setup_config_verdict "$two_missing_path")"
[ "$(managed_setup_verdict_detail "$verdict")" = "$first_key, $last_key" ] ||
  fail "several missing keys are listed comma-separated in schema order: $(managed_setup_verdict_detail "$verdict")"
pass "several missing keys are listed comma-separated in schema order"

# --- complete --------------------------------------------------------------

complete_path="$tmp_dir/complete.toml"
write_complete_config "$complete_path"
verdict="$(managed_setup_config_verdict "$complete_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "complete" ] || fail "a config with every managed key is complete"
[ -z "$(managed_setup_verdict_detail "$verdict")" ] || fail "a complete verdict carries no detail"
pass "a config with every managed key is complete with no detail"

# The profile match is a first-run rule, not part of the verdict: a stored
# profile that differs from any detected one is still complete here.
mismatched_path="$tmp_dir/mismatched-profile.toml"
write_complete_config "$mismatched_path"
sed 's/^profile = .*/profile = "vps-shell"/' "$mismatched_path" >"$mismatched_path.next"
mv "$mismatched_path.next" "$mismatched_path"
[ "$(managed_setup_verdict_state "$(managed_setup_config_verdict "$mismatched_path")")" = "complete" ] ||
  fail "the verdict does not compare the stored profile to a detected profile"
pass "the verdict does not compare the stored profile to a detected profile"

# --- under dash ------------------------------------------------------------

if command -v dash >/dev/null 2>&1; then
  dash_state="$(dash -c '
    fatal() { exit 97; }
    . "$1"
    verdict="$(managed_setup_config_verdict "$2")"
    printf "%s|%s\n" "$(managed_setup_verdict_state "$verdict")" "$(managed_setup_verdict_detail "$verdict")"
  ' dash "$reader" "$one_missing_path")"
  [ "$dash_state" = "incomplete|$missing_key" ] || fail "the verdict works under dash: $dash_state"
  pass "the verdict works under dash"
else
  skip "the verdict works under dash (dash is not installed)"
fi
