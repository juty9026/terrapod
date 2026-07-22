#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
helper="$repo_root/dot_local/lib/terrapod/executable_jetendard-font"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

fail() { printf '%s\n' "not ok - $1" >&2; exit 1; }
pass() { printf '%s\n' "ok - $1"; }

home_dir="$tmp_dir/home"
state_dir="$tmp_dir/state"
fixture_dir="$tmp_dir/fixture"
mkdir -p "$home_dir/Library/Fonts" "$state_dir" "$fixture_dir/ttf"

for variant in Thin ThinItalic ExtraLight ExtraLightItalic Light LightItalic Regular Italic Medium MediumItalic SemiBold SemiBoldItalic Bold BoldItalic ExtraBold ExtraBoldItalic; do
  printf 'font:%s\n' "$variant" >"$fixture_dir/ttf/Jetendard-$variant.ttf"
done

python3 - "$fixture_dir" <<'PY'
import pathlib
import sys
import zipfile

root = pathlib.Path(sys.argv[1])
with zipfile.ZipFile(root / "Jetendard-TTF.zip", "w") as archive:
    for path in sorted((root / "ttf").glob("Jetendard-*.ttf")):
        archive.write(path, f"ttf/{path.name}")
    archive.writestr("__MACOSX/ttf/._Jetendard-Regular.ttf", b"metadata")
PY

digest="$(shasum -a 256 "$fixture_dir/Jetendard-TTF.zip" | awk '{print $1}')"
asset_url="$(python3 - "$fixture_dir/Jetendard-TTF.zip" <<'PY'
import pathlib
import sys
print(pathlib.Path(sys.argv[1]).resolve().as_uri())
PY
)"

python3 - "$fixture_dir/release.json" "$asset_url" "$digest" <<'PY'
import json
import pathlib
import sys

path, asset_url, digest = sys.argv[1:]
pathlib.Path(path).write_text(json.dumps({
    "tag_name": "v9.9.9",
    "draft": False,
    "prerelease": False,
    "assets": [{
        "name": "Jetendard-TTF.zip",
        "state": "uploaded",
        "digest": f"sha256:{digest}",
        "browser_download_url": asset_url,
    }],
}), encoding="utf-8")
PY

api_url="$(python3 - "$fixture_dir/release.json" <<'PY'
import pathlib
import sys
print(pathlib.Path(sys.argv[1]).resolve().as_uri())
PY
)"

HOME="$home_dir" XDG_STATE_HOME="$state_dir" TERRAPOD_JETENDARD_RELEASE_API_URL="$api_url" \
  python3 "$helper" install

[ "$(find "$home_dir/Library/Fonts" -name 'Jetendard-*.ttf' -type f | wc -l | tr -d ' ')" = 16 ] || fail "installer writes every TTF"
python3 "$helper" check --home "$home_dir" --state-home "$state_dir" || fail "checker accepts complete manifest"
pass "installer writes and validates the complete release"

printf 'legacy\n' >"$home_dir/Library/Fonts/Jetendard-Legacy.ttf"
python3 - "$state_dir/terrapod/jetendard/manifest.json" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
data = json.loads(path.read_text(encoding="utf-8"))
data["files"].append("Jetendard-Legacy.ttf")
path.write_text(json.dumps(data) + "\n", encoding="utf-8")
PY
printf 'manual\n' >"$home_dir/Library/Fonts/Jetendard-Manual.ttf"

HOME="$home_dir" XDG_STATE_HOME="$state_dir" TERRAPOD_JETENDARD_RELEASE_API_URL="$api_url" \
  python3 "$helper" install

[ ! -e "$home_dir/Library/Fonts/Jetendard-Legacy.ttf" ] || fail "installer removes obsolete manifest-owned files"
[ -e "$home_dir/Library/Fonts/Jetendard-Manual.ttf" ] || fail "installer preserves unowned files"
pass "installer limits cleanup to manifest-owned files"

python3 - "$fixture_dir/release-bad.json" "$asset_url" <<'PY'
import json
import pathlib
import sys
pathlib.Path(sys.argv[1]).write_text(json.dumps({
    "tag_name": "v10.0.0",
    "draft": False,
    "prerelease": False,
    "assets": [{
        "name": "Jetendard-TTF.zip",
        "state": "uploaded",
        "digest": "sha256:" + "0" * 64,
        "browser_download_url": sys.argv[2],
    }],
}), encoding="utf-8")
PY
bad_api_url="$(python3 - "$fixture_dir/release-bad.json" <<'PY'
import pathlib
import sys
print(pathlib.Path(sys.argv[1]).resolve().as_uri())
PY
)"

if HOME="$home_dir" XDG_STATE_HOME="$state_dir" TERRAPOD_JETENDARD_RELEASE_API_URL="$bad_api_url" \
  python3 "$helper" install; then
  fail "installer rejects a digest mismatch"
fi
[ -e "$home_dir/Library/Fonts/Jetendard-Regular.ttf" ] || fail "failed replacement preserves working fonts"
pass "digest failure preserves the previous installation"
