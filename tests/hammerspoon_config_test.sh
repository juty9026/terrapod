#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
hammerspoon_config="$repo_root/dot_hammerspoon/init.lua"
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

# Reads the appBindings table out of init.lua as tab-separated
# key/label/bundleID rows. Entries may name a top-level `local` string instead
# of a literal, so those definitions are resolved first; an entry naming
# anything else renders as UNRESOLVED(...) and fails the comparison below
# rather than silently dropping out of the table.
extract_bindings() {
  awk '
    function unquote(value) {
      if (value ~ /^".*"$/) {
        gsub(/^"|"$/, "", value)
        return value
      }

      if (value in literals) {
        return literals[value]
      }

      return "UNRESOLVED(" value ")"
    }

    /^local [A-Za-z_][A-Za-z0-9_]* = ".*"$/ {
      value = $0
      sub(/^local [A-Za-z_][A-Za-z0-9_]* = "/, "", value)
      sub(/"$/, "", value)
      literals[$2] = value
      next
    }

    /^local appBindings = \{$/ {
      in_table = 1
      next
    }

    in_table && /^\}$/ {
      in_table = 0
      next
    }

    in_table {
      entry = $0
      sub(/^[[:space:]]*\{[[:space:]]*/, "", entry)
      sub(/[[:space:]]*\},?[[:space:]]*$/, "", entry)

      key = ""
      label = ""
      bundle_id = ""

      field_count = split(entry, fields, /,[[:space:]]*/)
      for (field_index = 1; field_index <= field_count; field_index++) {
        split(fields[field_index], pair, /[[:space:]]*=[[:space:]]*/)

        if (pair[1] == "key") {
          key = unquote(pair[2])
        } else if (pair[1] == "label") {
          label = unquote(pair[2])
        } else if (pair[1] == "bundleID") {
          bundle_id = unquote(pair[2])
        }
      }

      printf "%s\t%s\t%s\n", key, label, bundle_id
    }
  ' "$hammerspoon_config"
}

extract_bindings >"$tmp_dir/actual"

[ -s "$tmp_dir/actual" ] || fail "Hammerspoon launcher declares an appBindings table"
pass "Hammerspoon launcher declares an appBindings table"

cat >"$tmp_dir/expected" <<'BINDINGS'
t	Ghostty	com.mitchellh.ghostty
s	Slack	com.tinyspeck.slackmacgap
d	Discord	com.hnc.Discord
n	Notion	notion.id
b	Google Chrome	com.google.Chrome
1	Orca	com.stablyai.orca
2	Zed	dev.zed.Zed
o	Obsidian	md.obsidian
p	Postman	com.postmanlabs.mac
f	Figma	com.figma.Desktop
BINDINGS

if ! diff -u "$tmp_dir/expected" "$tmp_dir/actual" >"$tmp_dir/bindings.diff"; then
  cat "$tmp_dir/bindings.diff" >&2
  fail "Hammerspoon launcher binds exactly the expected key, label and bundle ID for every app"
fi
pass "Hammerspoon launcher binds exactly the expected key, label and bundle ID for every app"

duplicate_keys="$(cut -f1 "$tmp_dir/actual" | sort | uniq -d)"
if [ -n "$duplicate_keys" ]; then
  printf 'duplicate keys: %s\n' "$(printf '%s' "$duplicate_keys" | tr '\n' ' ')" >&2
  fail "Hammerspoon launcher binds each key to a single app"
fi
pass "Hammerspoon launcher binds each key to a single app"

# init.lua binds "/" to the help toast and "r" to a reload on both the hyper
# chord and the leader modal. An app claiming either key would register a
# second handler for it.
for reserved_key in / r; do
  if cut -f1 "$tmp_dir/actual" | grep -Fx "$reserved_key" >/dev/null; then
    fail "Hammerspoon launcher leaves $reserved_key to the help and reload bindings"
  fi
done
pass "Hammerspoon launcher leaves / and r to the help and reload bindings"
