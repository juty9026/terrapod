#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
helper="$repo_root/dot_local/lib/terrapod/executable_jetendard-settings"
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

home_dir="$tmp_dir/home"
zed_dir="$home_dir/.config/zed"
profiles_dir="$home_dir/Library/Application Support/orca/profiles"
mkdir -p "$zed_dir" "$profiles_dir/one" "$profiles_dir/two"

if python3 "$helper" >"$tmp_dir/arguments.out" 2>"$tmp_dir/arguments.err"; then
  fail "missing command is rejected"
fi
[ ! -s "$tmp_dir/arguments.out" ] || fail "missing command writes no stdout"
[ "$(wc -l <"$tmp_dir/arguments.err" | tr -d ' ')" = 1 ] || fail "missing command writes one stderr line"
[ "$(cat "$tmp_dir/arguments.err")" = "Jetendard settings: the following arguments are required: command" ] || fail "missing command writes an actionable error"
pass "invalid CLI arguments use the single-line error contract"

cat >"$zed_dir/settings.json" <<'JSONC'
// keep this comment
{
  "theme": "One Dark", // keep this inline comment
  "buffer_font_family": "Monaco",
  "terminal": {
    "font_family": "Menlo",
    "unrelated": true,
  },
  "unrelated": [1, 2,],
}
JSONC

cat >"$profiles_dir/one/orca-data.json" <<'JSON'
{
  "settings": {
    "terminalFontFamily": "Monaco",
    "theme": "dark"
  },
  "projects": {
    "preserved": true
  }
}
JSON

cat >"$profiles_dir/two/orca-data.json" <<'JSON'
{
  "settings": {
    "terminalFontFamily": "Menlo",
    "zoom": 1.25
  },
  "unrelated": [
    "preserved"
  ]
}
JSON

HOME="$home_dir" TERRAPOD_ORCA_RUNNING=0 python3 "$helper" apply
grep -F '// keep this comment' "$home_dir/.config/zed/settings.json" >/dev/null || fail "Zed comments survive"
grep -F '// keep this inline comment' "$home_dir/.config/zed/settings.json" >/dev/null || fail "Zed inline comments survive"
grep -F '"unrelated": [1, 2,],' "$home_dir/.config/zed/settings.json" >/dev/null || fail "Zed unrelated arrays survive"
grep -F '"buffer_font_family": "Jetendard"' "$home_dir/.config/zed/settings.json" >/dev/null || fail "Zed buffer font is applied"
grep -F '"font_family": "Jetendard"' "$home_dir/.config/zed/settings.json" >/dev/null || fail "Zed terminal font is applied"
python3 - "$home_dir/Library/Application Support/orca/profiles/one/orca-data.json" <<'PY'
import json
import pathlib
import sys
data = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert data["settings"]["terminalFontFamily"] == "Jetendard"
assert data["projects"] == {"preserved": True}
PY

python3 - "$home_dir/Library/Application Support/orca/profiles/two/orca-data.json" <<'PY'
import json
import pathlib
import sys
data = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert data["settings"]["terminalFontFamily"] == "Jetendard"
assert data["settings"]["zoom"] == 1.25
assert data["unrelated"] == ["preserved"]
PY

python3 - "$home_dir/Library/Application Support/orca/profiles/one/orca-data.json" <<'PY'
import json
import pathlib
import sys
path = pathlib.Path(sys.argv[1])
data = json.loads(path.read_text(encoding="utf-8"))
data["settings"]["terminalFontFamily"] = "Monaco"
path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
PY
before="$(shasum "$home_dir/Library/Application Support/orca/profiles/one/orca-data.json")"
if HOME="$home_dir" TERRAPOD_ORCA_RUNNING=1 python3 "$helper" apply; then
  fail "running Orca returns the deferred status"
else
  [ "$?" -eq 2 ] || fail "running Orca returns status 2"
fi
after="$(shasum "$home_dir/Library/Application Support/orca/profiles/one/orca-data.json")"
[ "$before" = "$after" ] || fail "running Orca profile remains untouched"

python3 "$helper" check-zed --home "$home_dir" || fail "Zed checker accepts applied settings"
if python3 "$helper" check-orca --home "$home_dir" 2>"$tmp_dir/orca-check.err"; then
  fail "Orca checker rejects a mismatched profile"
fi
grep -F 'profiles/one/orca-data.json' "$tmp_dir/orca-check.err" >/dev/null || fail "Orca checker names the mismatched profile"

HOME="$home_dir" TERRAPOD_ORCA_RUNNING=0 python3 "$helper" apply
python3 "$helper" check-orca --home "$home_dir" || fail "Orca checker accepts every repaired profile"

first_zed="$(shasum "$home_dir/.config/zed/settings.json")"
first_orca="$(shasum "$home_dir/Library/Application Support/orca/profiles/one/orca-data.json")"
HOME="$home_dir" TERRAPOD_ORCA_RUNNING=0 python3 "$helper" apply
[ "$first_zed" = "$(shasum "$home_dir/.config/zed/settings.json")" ] || fail "Zed apply is idempotent"
[ "$first_orca" = "$(shasum "$home_dir/Library/Application Support/orca/profiles/one/orca-data.json")" ] || fail "Orca apply is idempotent"
pass "settings application is idempotent"

empty_home="$tmp_dir/empty-home"
mkdir -p "$empty_home"
HOME="$empty_home" TERRAPOD_ORCA_RUNNING=0 python3 "$helper" apply
[ -f "$empty_home/.config/zed/settings.json" ] || fail "missing Zed settings are created"
python3 "$helper" check-zed --home "$empty_home" || fail "minimal Zed settings are valid"
[ ! -e "$empty_home/Library/Application Support/orca/profiles" ] || fail "apply does not create Orca profiles"
pass "empty home receives only minimal Zed settings"

inline_home="$tmp_dir/inline-home"
mkdir -p "$inline_home/.config/zed"
cat >"$inline_home/.config/zed/settings.json" <<'JSON'
{
  "buffer_font_family": "Jetendard",
  "terminal": {"shell": "zsh"},
  "unrelated": true
}
JSON
HOME="$inline_home" TERRAPOD_ORCA_RUNNING=0 python3 "$helper" apply
python3 "$helper" check-zed --home "$inline_home" || fail "inline nested terminal font is applied"
grep -F '  "terminal": {"shell": "zsh",' "$inline_home/.config/zed/settings.json" >/dev/null || fail "inline nested unrelated member survives"
grep -F '    "font_family": "Jetendard",' "$inline_home/.config/zed/settings.json" >/dev/null || fail "inline nested font stays inside terminal"
if grep -E '^  "font_family"' "$inline_home/.config/zed/settings.json" >/dev/null; then
  fail "inline nested font is not inserted at the root"
fi
grep -F '  "unrelated": true' "$inline_home/.config/zed/settings.json" >/dev/null || fail "inline fixture unrelated root data survives"
inline_first="$(shasum "$inline_home/.config/zed/settings.json")"
HOME="$inline_home" TERRAPOD_ORCA_RUNNING=0 python3 "$helper" apply
[ "$inline_first" = "$(shasum "$inline_home/.config/zed/settings.json")" ] || fail "inline nested object apply is idempotent"
pass "inline nested objects receive members inside the object"

for terminal_value in null '"not-an-object"' '[1, 2]'; do
  invalid_home="$tmp_dir/invalid-terminal-$(printf '%s' "$terminal_value" | tr -cd '[:alnum:]')"
  mkdir -p "$invalid_home/.config/zed"
  printf '{\n  "buffer_font_family": "Jetendard",\n  "terminal": %s\n}\n' "$terminal_value" >"$invalid_home/.config/zed/settings.json"
  invalid_before="$(shasum "$invalid_home/.config/zed/settings.json")"
  if HOME="$invalid_home" TERRAPOD_ORCA_RUNNING=0 python3 "$helper" apply >"$tmp_dir/invalid-terminal.out" 2>"$tmp_dir/invalid-terminal.err"; then
    fail "non-object Zed terminal is rejected during apply"
  fi
  [ "$invalid_before" = "$(shasum "$invalid_home/.config/zed/settings.json")" ] || fail "failed Zed apply preserves the original file"
  grep -F 'Jetendard settings: Zed setting terminal must be an object' "$tmp_dir/invalid-terminal.err" >/dev/null || fail "failed Zed apply reports the invalid intermediate node"
  if python3 "$helper" check-zed --home "$invalid_home" >"$tmp_dir/invalid-check.out" 2>"$tmp_dir/invalid-check.err"; then
    fail "Zed checker rejects a non-object terminal"
  fi
  grep -F 'Jetendard settings: Zed setting terminal must be an object' "$tmp_dir/invalid-check.err" >/dev/null || fail "Zed checker reports the invalid intermediate node"
done
pass "non-object Zed terminal values are rejected without mutation"

ghostty_home="$tmp_dir/ghostty-home"
mkdir -p "$ghostty_home/.config/ghostty"
printf '%s\n' 'font-family = "Jetendard"' >"$ghostty_home/.config/ghostty/config"
python3 "$helper" check-ghostty --home "$ghostty_home" || fail "Ghostty checker accepts one Jetendard family"
printf '%s\n' 'font-family = "Jetendard"' 'font-family = "Monaco"' >"$ghostty_home/.config/ghostty/config"
if python3 "$helper" check-ghostty --home "$ghostty_home" 2>/dev/null; then
  fail "Ghostty checker rejects multiple font families"
fi
pass "Ghostty checker enforces the single-family contract"
