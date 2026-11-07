#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
make_tmp_dir

HOME="$tmp_dir/home"
XDG_STATE_HOME="$tmp_dir/state"
export HOME XDG_STATE_HOME
mkdir -p "$HOME"
. "$repo_root/dot_local/lib/terrapod/install-warnings.sh"

terrapod_install_warning_now() { printf '%s\n' '2026-11-07T01:00:00Z'; }
terrapod_install_warning_write homebrew-core "old summary" "old guidance"
observation="$(terrapod_install_warning_observe homebrew-core)"
terrapod_install_warning_record_next <<EOF
$observation
EOF
assert_equals "$warning_record_summary" "old summary" "observation captures the summary"

# Publish a new version between consuming the summary and the other fields.
terrapod_install_warning_now() { printf '%s\n' '2026-11-07T02:00:00Z'; }
terrapod_install_warning_write homebrew-core "new summary" "new guidance"
assert_equals "$warning_record_guidance" "old guidance" "atomic replacement cannot change captured guidance"
assert_equals "$warning_record_updated_at" "2026-11-07T01:00:00Z" "atomic replacement cannot change the captured timestamp"
assert_equals "$warning_record_status" readable "observation reports a successful content read"

terrapod_install_warning_clear homebrew-core
terrapod_install_warning_record_next <<EOF
$observation
EOF
assert_equals "$warning_record_summary" "old summary" "captured records survive marker deletion without reopening the file"

TERRAPOD_TEST_WARNING_LIB="$repo_root/dot_local/lib/terrapod/install-warnings.sh"
. "$repo_root/tests/lib/install-warning-reader-fixture.sh"

for read_mode in delete fail; do
  terrapod_install_warning_write homebrew-core "existing summary" "existing guidance"
  observation="$(TERRAPOD_TEST_WARNING_READ_MODE="$read_mode" terrapod_install_warning_observe homebrew-core)"
  terrapod_install_warning_record_next <<EOF
$observation
EOF
  assert_equals "$warning_record_category" homebrew-core "$read_mode retains the observed warning category"
  assert_equals "$warning_record_status" unreadable "$read_mode reports the content read as unreadable"
  assert_equals "$warning_record_summary" unknown "$read_mode does not expose partial or stale summary data"
  assert_equals "$warning_record_guidance" unknown "$read_mode reports unknown guidance"
  assert_equals "$warning_record_updated_at" unknown "$read_mode reports an unknown timestamp"
done

terrapod_install_warning_clear homebrew-core
assert_equals "$(terrapod_install_warning_observations)" "" "initial absence remains clean"

marker_dir="$(terrapod_install_warning_dir)"
printf '%s\n' "category='ai-cli-tools'" "summary='legacy summary'" "guidance='legacy guidance'" \
  "updated_at='2026-11-07T00:00:00Z'" >"$marker_dir/ai-cli-tools"
terrapod_install_warning_record_next <<EOF
$(terrapod_install_warning_observe optional-ai-cli-tools)
EOF
assert_equals "$warning_record_category" optional-ai-cli-tools "observation normalizes the legacy alias"
assert_equals "$warning_record_summary" "legacy summary" "observation reads the legacy marker"

terrapod_install_warning_write optional-ai-cli-tools "canonical summary" "canonical guidance"
# Keep both names present to exercise precedence, rather than writer cleanup.
printf '%s\n' "summary='legacy summary'" >"$marker_dir/ai-cli-tools"
terrapod_install_warning_record_next <<EOF
$(terrapod_install_warning_observe optional-ai-cli-tools)
EOF
assert_equals "$warning_record_summary" "canonical summary" "canonical markers take precedence over legacy markers"

quoted_summary="It's literal: \$(touch $tmp_dir/executed) | a=b \\ tail"
terrapod_install_warning_write homebrew-core "$quoted_summary" ""
# An unexpected executable line must also remain inert marker data.
printf 'touch "%s"\n' "$tmp_dir/executed" >>"$marker_dir/homebrew-core"
terrapod_install_warning_record_next <<EOF
$(terrapod_install_warning_observe homebrew-core)
EOF
assert_equals "$warning_record_summary" "$quoted_summary" "observation safely decodes quotes and preserves literal metacharacters"
assert_equals "$warning_record_guidance" "" "observation preserves an empty field"
assert_equals "$(terrapod_install_warning_value homebrew-core summary)" "$quoted_summary" "compatibility reader shares quote decoding"
if [ -e "$tmp_dir/executed" ]; then
  fail "observation never executes marker data"
fi
pass "observation never executes marker data"

printf '%s\n' "summary='incomplete summary'" >"$marker_dir/homebrew-core"
terrapod_install_warning_record_next <<EOF
$(terrapod_install_warning_observe homebrew-core)
EOF
assert_equals "$warning_record_summary" "incomplete summary" "readable fields survive an incomplete marker"
assert_equals "$warning_record_guidance" unknown "reader owns missing-field fallback"
assert_equals "$warning_record_updated_at" unknown "missing timestamps remain explicit"
