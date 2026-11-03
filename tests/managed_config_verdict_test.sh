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

. "$repo_root/tests/lib/managed-config-fixtures.sh"

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

# --- unsupported Managed Setting values ------------------------------------
# A Managed Setting value is valid only as a bare lowercase TOML boolean.
# chezmoi's templates gate on Go truthiness, where the string "false" is on,
# so the verdict rejects anything else instead of letting tpod and the
# templates disagree about it.

# write_config_with_value <path> <key> <raw value> [profile]
write_config_with_value() {
  write_complete_config "$1"
  sed "s/^$2 = .*/$2 = $3/; s/^profile = .*/profile = \"${4:-macos-terminal}\"/" "$1" >"$1.next"
  mv "$1.next" "$1"
}

value_key="$(managed_setting_keys | sed -n '1p')"
value_case=0
for raw_value in '"false"' '"true"' "'true'" '1' 'yes' 'True' ''; do
  value_case=$((value_case + 1))
  value_path="$tmp_dir/value-$value_case.toml"
  write_config_with_value "$value_path" "$value_key" "$raw_value"
  verdict="$(managed_setup_config_verdict "$value_path")"
  [ "$(managed_setup_verdict_state "$verdict")" = "unsupported" ] ||
    fail "$value_key = $raw_value is reported as unsupported: $(managed_setup_verdict_state "$verdict")"
  assert_contains "$(managed_setup_verdict_detail "$verdict")" "$value_key" "$value_key = $raw_value names the key in the unsupported detail"
  assert_contains "$(managed_setup_verdict_detail "$verdict")" "bare true or false" "$value_key = $raw_value says to rewrite it as a bare boolean"
  assert_contains "$(managed_setup_verdict_detail "$verdict")" ": $value_path" "$value_key = $raw_value names the config path"
done

two_values_path="$tmp_dir/two-values.toml"
write_complete_config "$two_values_path"
second_value_key="$(managed_setting_keys | sed -n '$p')"
sed -e "s/^$value_key = .*/$value_key = \"false\"/" -e "s/^$second_value_key = .*/$second_value_key = 'true'/" \
  "$two_values_path" >"$two_values_path.next"
mv "$two_values_path.next" "$two_values_path"
verdict="$(managed_setup_config_verdict "$two_values_path")"
assert_contains "$(managed_setup_verdict_detail "$verdict")" "$value_key, $second_value_key" "several rejected values are named comma-separated in schema order"

# A key that does not apply to the stored profile is still a stored setting.
vps_only_key="$(managed_setting_keys_of_kind macos-app-group | sed -n '1p')"
not_applicable_path="$tmp_dir/value-not-applicable.toml"
write_config_with_value "$not_applicable_path" "$vps_only_key" '"false"' vps-shell
verdict="$(managed_setup_config_verdict "$not_applicable_path")"
[ "$(managed_setup_verdict_state "$verdict")" = "unsupported" ] ||
  fail "a rejected value on a key that does not apply to the profile is unsupported"
assert_contains "$(managed_setup_verdict_detail "$verdict")" "$vps_only_key" "a rejected value on a key that does not apply to the profile is unsupported and named"

# A rejected value outranks a missing key: both are unsupported-or-worse, and
# the value cause is a kind of unsupported.
value_and_missing_path="$tmp_dir/value-and-missing.toml"
write_config_with_value "$value_and_missing_path" "$value_key" '"true"'
grep -v "^$second_value_key = " "$value_and_missing_path" >"$value_and_missing_path.next"
mv "$value_and_missing_path.next" "$value_and_missing_path"
[ "$(managed_setup_verdict_state "$(managed_setup_config_verdict "$value_and_missing_path")")" = "unsupported" ] ||
  fail "a rejected value outranks a missing key"
pass "a rejected value outranks a missing key"

commented_path="$tmp_dir/value-commented.toml"
write_config_with_value "$commented_path" "$value_key" 'true  # kept on'
[ "$(managed_setup_verdict_state "$(managed_setup_config_verdict "$commented_path")")" = "complete" ] ||
  fail "a bare boolean with a trailing comment is complete"
pass "a bare boolean with a trailing comment is complete"

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

# --- interpreted values ----------------------------------------------------
# The reader hands callers on/off and a plain profile, never raw tokens.

values_path="$tmp_dir/values.toml"
write_complete_config "$values_path"
sed "s/^$value_key = .*/$value_key = true # on/" "$values_path" >"$values_path.next"
mv "$values_path.next" "$values_path"
managed_setup_config_load "$values_path"
[ "$managed_setup_config_state" = "complete" ] || fail "the loaded config is complete: $managed_setup_config_state"
managed_setting_enabled "$value_key" || fail "a bare true is on"
if managed_setting_enabled "$second_value_key"; then
  fail "a bare false is off"
fi
[ "$(managed_setup_stored_profile)" = "macos-terminal" ] ||
  fail "a quoted profile comes back as a plain string: $(managed_setup_stored_profile)"
pass "a loaded config reads a bare true as on, a bare false as off, and the profile as a plain string"

literal_profile_path="$tmp_dir/literal-profile.toml"
write_complete_config "$literal_profile_path"
sed "s/^profile = .*/profile = 'vps-shell'/" "$literal_profile_path" >"$literal_profile_path.next"
mv "$literal_profile_path.next" "$literal_profile_path"
managed_setup_config_load "$literal_profile_path"
[ "$(managed_setup_stored_profile)" = "vps-shell" ] ||
  fail "a literal-string profile comes back as a plain string: $(managed_setup_stored_profile)"
pass "a literal-string profile comes back as a plain string"

bare_profile_path="$tmp_dir/bare-profile.toml"
write_complete_config "$bare_profile_path"
sed "s/^profile = .*/profile = macos-terminal/" "$bare_profile_path" >"$bare_profile_path.next"
mv "$bare_profile_path.next" "$bare_profile_path"
managed_setup_config_load "$bare_profile_path"
if managed_setup_stored_profile >/dev/null; then
  fail "a profile that is not a quoted string has no stored profile"
fi
pass "a profile that is not a quoted string has no stored profile"

dotted_path="$tmp_dir/dotted.toml"
managed_setting_keys | while IFS= read -r key; do
  printf 'data.%s = false\n' "$key"
done >"$dotted_path"
printf '%s\n' "data.\"profile\" = \"vps-shell\"" "data.$value_key = true" >>"$dotted_path"
managed_setup_config_load "$dotted_path"
[ "$managed_setup_config_state" = "complete" ] || fail "root dotted data keys are complete: $managed_setup_config_state"
managed_setting_enabled "$value_key" || fail "the last root dotted assignment of a key wins"
[ "$(managed_setup_stored_profile)" = "vps-shell" ] || fail "a quoted dotted profile key is read"
pass "root dotted data keys, quoted key names included, are read"

managed_setup_config_load "$tmp_dir/value-1.toml"
if managed_setting_enabled "$value_key"; then
  fail "a rejected value is off"
fi
pass "a rejected value is off"

managed_setup_config_load "$missing_path"
if managed_setting_enabled "$value_key" || managed_setup_stored_profile >/dev/null; then
  fail "a missing config has no values"
fi
pass "a missing config has no values"

# --- one read per evaluation -----------------------------------------------
# The verdict and every value come from a fixed number of passes over the
# file, however many Managed Settings the schema declares. An awk shim counts
# the passes; the second count runs a reader with one more schema row.

awk_count_bin="$tmp_dir/awk-count-bin"
mkdir -p "$awk_count_bin"
real_awk="$(command -v awk)"
cat >"$awk_count_bin/awk" <<SH
#!/bin/sh
for arg do
  [ "\$arg" = "$complete_path" ] && printf '%s\n' pass >>"\$AWK_COUNT_FILE"
done
exec "$real_awk" "\$@"
SH
chmod +x "$awk_count_bin/awk"

grown_reader="$tmp_dir/grown-config-toml.sh"
"$real_awk" '
  /^ROWS$/ && !added {
    print "enableMacosAppGroupCountProbe|macos-app-group|count-probe|macos-terminal|workstation|Probe"
    added = 1
  }
  { print }
' "$reader" >"$grown_reader"

count_config_reads() {
  count_file="$tmp_dir/awk-count-$1"
  : >"$count_file"
  AWK_COUNT_FILE="$count_file" PATH="$awk_count_bin:$PATH" sh -c '
    fatal() { exit 97; }
    . "$1"
    managed_setup_config_load "$2"
    managed_setting_enabled enableEditorStack || :
    managed_setup_stored_profile >/dev/null || :
  ' sh "$2" "$complete_path"
  wc -l <"$count_file" | tr -d ' '
}

current_reads="$(count_config_reads current "$reader")"
grown_reads="$(count_config_reads grown "$grown_reader")"
[ "$current_reads" -gt 0 ] || fail "the awk shim saw the reader read the config"
# Four: the three unsupported-syntax checks and the one scan of the values.
[ "$current_reads" -le 4 ] || fail "one load reads the config at most four times: $current_reads"
[ "$grown_reads" = "$current_reads" ] ||
  fail "one more Managed Setting does not add a read: $current_reads, then $grown_reads"
pass "one load reads the config $current_reads times, and a new Managed Setting adds no read"

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

  dash_values="$(dash -c '
    fatal() { exit 97; }
    . "$1"
    managed_setup_config_load "$2"
    if managed_setting_enabled "$3"; then printf on; else printf off; fi
    printf "|%s\n" "$(managed_setup_stored_profile)"
  ' dash "$reader" "$values_path" "$value_key")"
  [ "$dash_values" = "on|macos-terminal" ] || fail "the interpreted values work under dash: $dash_values"
  pass "the interpreted values work under dash"
else
  skip "the verdict works under dash (dash is not installed)"
fi
