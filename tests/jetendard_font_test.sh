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

if python3 "$helper" >"$tmp_dir/arguments.out" 2>"$tmp_dir/arguments.err"; then
  fail "missing command is rejected"
fi
[ ! -s "$tmp_dir/arguments.out" ] || fail "missing command writes no stdout"
[ "$(wc -l <"$tmp_dir/arguments.err" | tr -d ' ')" = 1 ] || fail "missing command writes one stderr line"
[ "$(cat "$tmp_dir/arguments.err")" = "Jetendard font: the following arguments are required: command" ] || fail "missing command writes an actionable error"
pass "invalid CLI arguments use the single-line error contract"

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
  python3 "$helper" install 2>"$tmp_dir/digest.err"; then
  fail "installer rejects a digest mismatch"
fi
[ -e "$home_dir/Library/Fonts/Jetendard-Regular.ttf" ] || fail "failed replacement preserves working fonts"
pass "digest failure preserves the previous installation"

printf 'not a zip archive\n' >"$fixture_dir/Jetendard-TTF-corrupt.zip"
corrupt_digest="$(shasum -a 256 "$fixture_dir/Jetendard-TTF-corrupt.zip" | awk '{print $1}')"
corrupt_asset_url="$(python3 - "$fixture_dir/Jetendard-TTF-corrupt.zip" <<'PY'
import pathlib
import sys
print(pathlib.Path(sys.argv[1]).resolve().as_uri())
PY
)"
python3 - "$fixture_dir/release-corrupt.json" "$corrupt_asset_url" "$corrupt_digest" <<'PY'
import json
import pathlib
import sys

path, asset_url, digest = sys.argv[1:]
pathlib.Path(path).write_text(json.dumps({
    "tag_name": "v10.0.1",
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
corrupt_api_url="$(python3 - "$fixture_dir/release-corrupt.json" <<'PY'
import pathlib
import sys
print(pathlib.Path(sys.argv[1]).resolve().as_uri())
PY
)"

if HOME="$home_dir" XDG_STATE_HOME="$state_dir" TERRAPOD_JETENDARD_RELEASE_API_URL="$corrupt_api_url" \
  python3 "$helper" install 2>"$tmp_dir/corrupt.err"; then
  fail "installer rejects a corrupt archive"
fi
[ -e "$home_dir/Library/Fonts/Jetendard-Regular.ttf" ] || fail "corrupt archive preserves working fonts"
python3 "$helper" check --home "$home_dir" --state-home "$state_dir" || fail "corrupt archive preserves the working manifest"
pass "corrupt archive preserves the previous installation"

transaction_fixture="$tmp_dir/transaction-fixture"
mkdir -p "$transaction_fixture/ttf"
for variant in Thin ThinItalic ExtraLight ExtraLightItalic Light LightItalic Regular Italic Medium MediumItalic SemiBold SemiBoldItalic Bold BoldItalic ExtraBold ExtraBoldItalic; do
  printf 'replacement:%s\n' "$variant" >"$transaction_fixture/ttf/Jetendard-$variant.ttf"
done
python3 - "$transaction_fixture" <<'PY'
import hashlib
import json
import pathlib
import sys
import zipfile

root = pathlib.Path(sys.argv[1])
archive_path = root / "Jetendard-TTF.zip"
with zipfile.ZipFile(archive_path, "w") as archive:
    for path in sorted((root / "ttf").glob("Jetendard-*.ttf")):
        archive.write(path, f"ttf/{path.name}")
digest = hashlib.sha256(archive_path.read_bytes()).hexdigest()
(root / "release.json").write_text(json.dumps({
    "tag_name": "v10.0.0",
    "draft": False,
    "prerelease": False,
    "assets": [{
        "name": "Jetendard-TTF.zip",
        "state": "uploaded",
        "digest": f"sha256:{digest}",
        "browser_download_url": archive_path.resolve().as_uri(),
    }],
}), encoding="utf-8")
PY

python3 - "$helper" "$home_dir" "$state_dir" "$transaction_fixture/release.json" "$tmp_dir" <<'PY'
import importlib.util
import importlib.machinery
import contextlib
import io
import json
import os
from pathlib import Path
import shutil
import sys

helper_path, base_home_arg, base_state_arg, release_arg, temp_arg = sys.argv[1:]
loader = importlib.machinery.SourceFileLoader("jetendard_font_helper", helper_path)
spec = importlib.util.spec_from_loader(loader.name, loader)
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)
base_home = Path(base_home_arg)
base_state = Path(base_state_arg)
release_url = Path(release_arg).resolve().as_uri()
temp = Path(temp_arg)

def scenario(name):
    home = temp / f"{name}-home"
    state = temp / f"{name}-state"
    shutil.copytree(base_home, home)
    shutil.copytree(base_state, state)
    os.environ["XDG_STATE_HOME"] = str(state)
    os.environ["TERRAPOD_JETENDARD_RELEASE_API_URL"] = release_url
    return home, state

def snapshot(home, state):
    fonts = home / "Library" / "Fonts"
    manifest = state / "terrapod" / "jetendard" / "manifest.json"
    return ({path.name: path.read_bytes() for path in sorted(fonts.glob("Jetendard-*.ttf"))}, manifest.read_bytes() if manifest.exists() else None)

def assert_snapshot(home, state, expected):
    assert snapshot(home, state) == expected

real_replace = module.os.replace

home, state = scenario("backup-preparation")
before = snapshot(home, state)
real_copy2 = module.shutil.copy2
snapshot_destinations = []
def fail_snapshot_preparation(source, destination, *args, **kwargs):
    destination_path = Path(destination)
    if destination_path.name == "Jetendard-Regular.ttf":
        snapshot_destinations.append(destination_path)
        raise OSError("injected snapshot preparation failure")
    return real_copy2(source, destination, *args, **kwargs)
module.shutil.copy2 = fail_snapshot_preparation
try:
    module.install(home)
except OSError as error:
    assert "injected snapshot preparation failure" in str(error)
else:
    raise AssertionError("snapshot preparation failure was not injected")
finally:
    module.shutil.copy2 = real_copy2
assert_snapshot(home, state, before)
recovery_root = state / "terrapod" / "jetendard" / "recovery"
assert snapshot_destinations
assert snapshot_destinations[0].is_relative_to(recovery_root)
assert not recovery_root.exists() or not list(recovery_root.glob("rollback-*"))

old_state_home = os.environ.get("XDG_STATE_HOME")
os.environ["XDG_STATE_HOME"] = "relative-state"
try:
    assert module.state_root(home) == home.resolve() / ".local" / "state" / "terrapod" / "jetendard"
    assert module.state_root(home).is_absolute()
    relative_recovery_root = module.state_root(home) / "recovery"
    relative_transaction = module.create_transaction(relative_recovery_root)
    assert relative_transaction.is_absolute()
    assert relative_transaction.parent == home.resolve() / ".local" / "state" / "terrapod" / "jetendard" / "recovery"
    module.cleanup_transaction(relative_transaction, relative_recovery_root)
finally:
    if old_state_home is None:
        os.environ.pop("XDG_STATE_HOME", None)
    else:
        os.environ["XDG_STATE_HOME"] = old_state_home

home, state = scenario("late-replace")
before = snapshot(home, state)
replacement_count = 0
failed = False
def fail_late_replace(source, destination):
    global replacement_count, failed
    destination = Path(destination)
    if destination.parent == home / "Library" / "Fonts" and destination.name.startswith("Jetendard-"):
        replacement_count += 1
        if replacement_count == 10 and not failed:
            failed = True
            raise OSError("injected late font replacement failure")
    return real_replace(source, destination)
module.os.replace = fail_late_replace
try:
    module.install(home)
except OSError as error:
    assert "injected late font replacement failure" in str(error)
else:
    raise AssertionError("late replacement failure was not injected")
finally:
    module.os.replace = real_replace
assert_snapshot(home, state, before)
assert not (state / "terrapod" / "jetendard" / "recovery").exists()
module.install(home)
assert (home / "Library" / "Fonts" / "Jetendard-Regular.ttf").read_text() == "replacement:Regular\n"
module.check(home, str(state))

home, state = scenario("manifest")
before = snapshot(home, state)
manifest = state / "terrapod" / "jetendard" / "manifest.json"
failed = False
def fail_manifest(source, destination):
    global failed
    if Path(destination) == manifest and not failed:
        failed = True
        raise OSError("injected manifest replacement failure")
    return real_replace(source, destination)
module.os.replace = fail_manifest
try:
    module.install(home)
except OSError as error:
    assert "injected manifest replacement failure" in str(error)
else:
    raise AssertionError("manifest failure was not injected")
finally:
    module.os.replace = real_replace
assert_snapshot(home, state, before)
assert not (state / "terrapod" / "jetendard" / "recovery").exists()
module.install(home)
module.check(home, str(state))

home, state = scenario("obsolete")
fonts = home / "Library" / "Fonts"
manifest = state / "terrapod" / "jetendard" / "manifest.json"
(fonts / "Jetendard-Legacy.ttf").write_text("legacy-owned\n")
data = json.loads(manifest.read_text())
data["files"].append("Jetendard-Legacy.ttf")
manifest.write_text(json.dumps(data) + "\n")
before = snapshot(home, state)
real_unlink = module.os.unlink
failed = False
def fail_obsolete(path, *args, **kwargs):
    global failed
    candidate = Path(path)
    if candidate.name == "Jetendard-Legacy.ttf" and not failed:
        failed = True
        raise OSError("injected obsolete cleanup failure")
    return real_unlink(path, *args, **kwargs)
module.os.unlink = fail_obsolete
try:
    module.install(home)
except OSError as error:
    assert "injected obsolete cleanup failure" in str(error)
else:
    raise AssertionError("obsolete cleanup failure was not injected")
finally:
    module.os.unlink = real_unlink
assert_snapshot(home, state, before)
assert not (state / "terrapod" / "jetendard" / "recovery").exists()
module.install(home)
assert not (fonts / "Jetendard-Legacy.ttf").exists()
assert (fonts / "Jetendard-Manual.ttf").read_text() == "manual\n"
module.check(home, str(state))

home = temp / "first-install-home"
state = temp / "first-install-state"
(home / "Library" / "Fonts").mkdir(parents=True)
state.mkdir()
os.environ["XDG_STATE_HOME"] = str(state)
before = snapshot(home, state)
replacement_count = 0
failed = False
module.os.replace = fail_late_replace
try:
    module.install(home)
except OSError:
    pass
else:
    raise AssertionError("first-install failure was not injected")
finally:
    module.os.replace = real_replace
assert_snapshot(home, state, before)
module.install(home)
module.check(home, str(state))

def cli_install(home):
    old_argv = sys.argv
    old_home = os.environ.get("HOME")
    stdout = io.StringIO()
    stderr = io.StringIO()
    sys.argv = [helper_path, "install"]
    os.environ["HOME"] = str(home)
    try:
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            result = module.main()
    finally:
        sys.argv = old_argv
        if old_home is None:
            os.environ.pop("HOME", None)
        else:
            os.environ["HOME"] = old_home
    return result, stdout.getvalue(), stderr.getvalue()

home, state = scenario("font-restore")
before = snapshot(home, state)
replacement_count = 0
forward_failed = False
restore_failed = False
def fail_forward_and_font_restore(source, destination):
    global replacement_count, forward_failed, restore_failed
    source_path = Path(source)
    destination_path = Path(destination)
    if destination_path.parent == home / "Library" / "Fonts" and destination_path.name.startswith("Jetendard-") and "backup" not in source_path.parts:
        replacement_count += 1
        if replacement_count == 10 and not forward_failed:
            forward_failed = True
            raise OSError("injected forward replacement failure")
    if forward_failed and destination_path == home / "Library" / "Fonts" / "Jetendard-Regular.ttf" and not restore_failed:
        restore_failed = True
        raise OSError("injected font restore failure")
    return real_replace(source, destination)
module.os.replace = fail_forward_and_font_restore
try:
    result, stdout, stderr = cli_install(home)
finally:
    module.os.replace = real_replace
assert result == 1 and not stdout
assert stderr.count("\n") == 1
assert "injected forward replacement failure" in stderr
assert "injected font restore failure" in stderr
marker = "recovery backups: "
assert marker in stderr
recovery = Path(stderr.split(marker, 1)[1].strip())
assert recovery.parent == state.resolve() / "terrapod" / "jetendard" / "recovery"
assert recovery.stat().st_mode & 0o777 == 0o700
assert (recovery / "fonts" / "Jetendard-Regular.ttf").read_bytes() == before[0]["Jetendard-Regular.ttf"]
assert (recovery / "fonts" / "Jetendard-Thin.ttf").read_bytes() == before[0]["Jetendard-Thin.ttf"]
assert (recovery / "manifest.json").read_bytes() == before[1]
metadata = json.loads((recovery / "transaction.json").read_text())
assert metadata["status"] == "rollback_failed"
assert "Jetendard-Regular.ttf" in metadata["rollback_errors"][0], metadata
assert (home / "Library" / "Fonts" / "Jetendard-Manual.ttf").read_text() == "manual\n"

home, state = scenario("manifest-restore")
fonts = home / "Library" / "Fonts"
manifest = state / "terrapod" / "jetendard" / "manifest.json"
(fonts / "Jetendard-Legacy.ttf").write_text("legacy-owned\n")
data = json.loads(manifest.read_text())
data["files"].append("Jetendard-Legacy.ttf")
manifest.write_text(json.dumps(data) + "\n")
before = snapshot(home, state)
real_unlink = module.os.unlink
forward_failed = False
restore_failed = False
def fail_cleanup(path, *args, **kwargs):
    global forward_failed
    if Path(path).name == "Jetendard-Legacy.ttf" and not forward_failed:
        forward_failed = True
        raise OSError("injected forward cleanup failure")
    return real_unlink(path, *args, **kwargs)
def fail_manifest_restore(source, destination):
    global restore_failed
    if forward_failed and Path(destination) == manifest and not restore_failed:
        restore_failed = True
        raise OSError("injected manifest restore failure")
    return real_replace(source, destination)
module.os.unlink = fail_cleanup
module.os.replace = fail_manifest_restore
try:
    result, stdout, stderr = cli_install(home)
finally:
    module.os.unlink = real_unlink
    module.os.replace = real_replace
assert result == 1 and not stdout
assert stderr.count("\n") == 1
assert "injected forward cleanup failure" in stderr
assert "injected manifest restore failure" in stderr
assert marker in stderr
recovery = Path(stderr.split(marker, 1)[1].strip())
assert recovery.parent == state.resolve() / "terrapod" / "jetendard" / "recovery"
assert (recovery / "manifest.json").read_bytes() == before[1]
assert (recovery / "fonts" / "Jetendard-Regular.ttf").read_bytes() == before[0]["Jetendard-Regular.ttf"]
metadata = json.loads((recovery / "transaction.json").read_text())
assert metadata["status"] == "rollback_failed"
assert "manifest.json" in metadata["rollback_errors"][0]
assert (fonts / "Jetendard-Manual.ttf").read_text() == "manual\n"
PY
pass "installer rolls back failures and preserves backups when restore fails"
