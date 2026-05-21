#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
karabiner_config="$repo_root/dot_config/private_karabiner/private_karabiner.json"

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

assert_json_value() {
  filter="$1"
  expected="$2"
  message="$3"

  actual="$(jq -r "$filter // empty" "$karabiner_config")"
  if [ "$actual" != "$expected" ]; then
    fail "$message; expected '$expected', got '${actual:-<missing>}'"
  fi

  pass "$message"
}

command -v jq >/dev/null || fail "jq is required to test Karabiner config"

jq empty "$karabiner_config" || fail "Karabiner config should be valid JSON"
pass "Karabiner config is valid JSON"

rule='.profiles[0].complex_modifications.rules[2]'
manipulator="$rule.manipulators[0]"

assert_json_value \
  "$rule.description" \
  "Right Command: Korean/English input shortcut when pressed alone" \
  "Karabiner documents the right Command Hangul toggle rule"

assert_json_value \
  "$manipulator.from.key_code" \
  "right_command" \
  "right Command is the source key"

assert_json_value \
  "$manipulator.to[0].key_code" \
  "right_command" \
  "right Command keeps modifier behavior in key chords"

assert_json_value \
  "$manipulator.to[0].lazy" \
  "true" \
  "right Command modifier output is lazy until a chord is used"

assert_json_value \
  "$manipulator.to_if_alone[0].key_code" \
  "spacebar" \
  "right Command alone sends the macOS input source shortcut key"

assert_json_value \
  "$manipulator.to_if_alone[0].modifiers[0]" \
  "left_control" \
  "right Command alone sends control-space for input source switching"

assert_json_value \
  "$manipulator.to_if_held_down[0].key_code" \
  "right_command" \
  "right Command can still be held as a modifier"

rdr_devices="$(jq '
  [
    .profiles[0].devices[]
    | select(
      .identifiers.vendor_id == 12815
      and .identifiers.product_id == 20565
      and .ignore == false
    )
  ]
  | length
' "$karabiner_config")"

if [ "$rdr_devices" -lt 2 ]; then
  fail "RDR IX PRO keyboard interfaces should be enabled for Karabiner modifications"
fi

pass "RDR IX PRO keyboard interfaces are enabled for Karabiner modifications"
