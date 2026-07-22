# Jetendard Font Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Install the latest stable Jetendard TTF release on the macOS Terminal Profile and apply the `Jetendard` family to Ghostty, Zed buffers/terminals, and every initialized local Orca terminal profile without taking ownership of unrelated app settings.

**Architecture:** Keep release acquisition and app-setting mutation in two focused Python standard-library helpers. A macOS-only `run_onchange_after_` installer queries GitHub only when its rendered source changes, verifies the release asset digest, atomically records a manifest, and owns only the font files named by that manifest; an always-run post-apply settings adapter makes idempotent, narrow edits and defers Orca while it is running. Terrapod install-warning markers make network, font, and deferred-setting failures recoverable, while `status` and `doctor` validate local state without network access.

**Tech Stack:** chezmoi templates, POSIX shell, Python 3 standard library, GitHub Releases REST API, macOS `~/Library/Fonts`, shell regression tests.

## Global Constraints

- Apply only to the macOS Terminal Profile; the VPS Shell Profile must not manage the helpers, scripts, font files, or application settings.
- Resolve `https://api.github.com/repos/kuskhan/jetendard/releases/latest` only when the font installer's rendered source changes or a prior `jetendard-font` warning is being retried.
- Accept only a non-draft, non-prerelease release with exactly one uploaded `Jetendard-TTF.zip` asset and a `sha256:` digest.
- Install every `Jetendard-*.ttf` member in the release archive; do not pin a tag or retain a repository-side checksum.
- Verify the downloaded archive against the SHA-256 digest returned by the same GitHub release response.
- Preserve previously working fonts on failed download, digest, ZIP, or extraction operations.
- Record installed tag, digest, and owned font basenames in a manifest under `${XDG_STATE_HOME:-$HOME/.local/state}/terrapod/jetendard/manifest.json`.
- Remove only obsolete files previously named by the Terrapod manifest, and only after the replacement release has been installed successfully.
- Remove `font-jetbrains-mono-nerd-font` and `font-d2coding` from Terrapod declarations and settings; do not uninstall copies already present on a machine.
- Set only font-family properties. Preserve font size, weight, line height, ligature, theme, and application UI-font settings.
- Ghostty uses one `font-family = "Jetendard"` entry.
- Zed uses `buffer_font_family = "Jetendard"` and `terminal.font_family = "Jetendard"`; preserve JSONC comments, trailing commas, unrelated values, and existing formatting.
- Create `~/.config/zed/settings.json` with minimal font settings when it does not exist.
- Orca uses `settings.terminalFontFamily = "Jetendard"` in every existing `profiles/*/orca-data.json`; do not create an Orca profile.
- Never mutate Orca profile files while Orca is running. Record a recoverable `jetendard-settings` warning and retry on the next `tpod apply` after Orca exits.
- Never automatically launch, quit, or restart Ghostty, Zed, or Orca.
- Font-install and settings-apply failures are non-blocking only when their install-warning marker is written successfully.
- `tpod status` and `tpod doctor` must perform local checks only; neither command may call GitHub or download assets.

---

## File Structure

- Create `dot_local/lib/terrapod/executable_jetendard-font`: release discovery, digest verification, TTF installation, owned-file cleanup, manifest read/write, and offline manifest validation.
- Create `dot_local/lib/terrapod/executable_jetendard-settings`: JSONC-aware Zed edits, closed-Orca profile edits, and offline Ghostty/Zed/Orca validation.
- Create `.chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl`: source-change-triggered font installation and warning-marker lifecycle.
- Create `.chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl`: marker-gated recovery for a failed font installation.
- Create `.chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl`: idempotent app-setting application on every apply, including delayed first Orca initialization.
- Modify `dot_local/lib/terrapod/install-warnings.sh`: register `jetendard-font` and `jetendard-settings` as known warning categories.
- Modify `.chezmoiignore`: exclude every Jetendard helper and script from the VPS Shell Profile.
- Modify `Brewfile`: stop declaring the two superseded Homebrew font casks.
- Modify `dot_config/ghostty/config`: use Jetendard as the sole family.
- Modify `dot_local/bin/executable_terrapod`: expose local Jetendard state in `status` and validate the manifest and three app configurations in `doctor`.
- Create `tests/jetendard_font_test.sh`: isolated GitHub-response/ZIP fixtures for installer and manifest ownership behavior.
- Create `tests/jetendard_settings_test.sh`: isolated Zed JSONC, Orca profile, running-app deferral, and checker behavior.
- Modify `tests/chezmoiignore_test.sh`: template rendering, platform exclusion, warning adapter, Brewfile, and Ghostty contracts.
- Modify `tests/terrapod_command_test.sh`: warning-category, status, doctor, and no-network command contracts.
- Modify `README.md`, `README.ko.md`, and `CONTEXT.md`: document source, update trigger, scope, recovery, and non-destructive migration.
- Create `docs/adr/0009-install-jetendard-from-latest-stable-release.md`: record why this font is a narrow GitHub-release exception to the normal package-provider boundary.
- Modify `tests/readme_optional_stack_profiles_test.sh` and `tests/readme_korean_test.sh`: enforce the new Canonical/Korean README claims.

---

### Task 1: Build the Owned Jetendard Font Installer

**Files:**
- Create: `tests/jetendard_font_test.sh`
- Create: `dot_local/lib/terrapod/executable_jetendard-font`

**Interfaces:**
- Consumes: `HOME`, optional `XDG_STATE_HOME`, and optional `TERRAPOD_JETENDARD_RELEASE_API_URL` for deterministic tests.
- Produces: CLI commands `install` and `check`; manifest schema `{"tag": string, "digest": "sha256:<hex>", "files": ["Jetendard-Regular.ttf"]}` with every installed basename in `files`; exit status `0` for success and non-zero with a single actionable stderr message for failure.

- [ ] **Step 1: Write the failing installer tests**

Create `tests/jetendard_font_test.sh` with a temporary home, a local release JSON response, ZIP fixtures containing all 16 current family members under `ttf/`, and these assertions:

```sh
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `sh tests/jetendard_font_test.sh`

Expected: FAIL because `dot_local/lib/terrapod/executable_jetendard-font` does not exist.

- [ ] **Step 3: Implement the font helper**

Create `dot_local/lib/terrapod/executable_jetendard-font` as an executable Python 3 program. Use these exact public constants and functions so scripts and command tests have stable seams:

```python
#!/usr/bin/env python3
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import tempfile
import urllib.request
import zipfile

RELEASE_API_URL = "https://api.github.com/repos/kuskhan/jetendard/releases/latest"
ASSET_NAME = "Jetendard-TTF.zip"
FONT_NAME = re.compile(r"Jetendard-[A-Za-z0-9]+\.ttf\Z")


def state_root(home: Path, state_home: str | None = None) -> Path:
    if state_home:
        return Path(state_home) / "terrapod" / "jetendard"
    configured = os.environ.get("XDG_STATE_HOME")
    return (Path(configured) if configured else home / ".local" / "state") / "terrapod" / "jetendard"


def manifest_path(home: Path, state_home: str | None = None) -> Path:
    return state_root(home, state_home) / "manifest.json"


def load_json_url(url: str) -> dict:
    request = urllib.request.Request(url, headers={"Accept": "application/vnd.github+json", "User-Agent": "terrapod-jetendard"})
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def select_asset(release: dict) -> tuple[str, str, str]:
    if release.get("draft") or release.get("prerelease"):
        raise ValueError("latest GitHub release is not a stable published release")
    tag = release.get("tag_name")
    assets = [asset for asset in release.get("assets", []) if asset.get("name") == ASSET_NAME and asset.get("state") == "uploaded"]
    if not isinstance(tag, str) or not tag or len(assets) != 1:
        raise ValueError("latest GitHub release must contain one uploaded Jetendard-TTF.zip asset")
    asset = assets[0]
    digest = asset.get("digest")
    url = asset.get("browser_download_url")
    if not isinstance(digest, str) or not re.fullmatch(r"sha256:[0-9a-fA-F]{64}", digest):
        raise ValueError("Jetendard-TTF.zip does not publish a SHA-256 digest")
    if not isinstance(url, str) or not url:
        raise ValueError("Jetendard-TTF.zip does not publish a download URL")
    return tag, digest.lower(), url


def download(url: str, destination: Path) -> None:
    request = urllib.request.Request(url, headers={"User-Agent": "terrapod-jetendard"})
    with urllib.request.urlopen(request, timeout=60) as response, destination.open("wb") as output:
        shutil.copyfileobj(response, output)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


def font_entries(archive: zipfile.ZipFile) -> dict[str, zipfile.ZipInfo]:
    selected: dict[str, zipfile.ZipInfo] = {}
    for info in archive.infolist():
        path = PurePosixPath(info.filename)
        if info.is_dir() or "__MACOSX" in path.parts:
            continue
        basename = path.name
        if not FONT_NAME.fullmatch(basename):
            continue
        if basename in selected:
            raise ValueError(f"release archive contains duplicate font: {basename}")
        selected[basename] = info
    if not selected:
        raise ValueError("release archive contains no Jetendard TTF files")
    return selected


def read_manifest(path: Path) -> dict:
    if not path.is_file():
        return {"tag": "", "digest": "", "files": []}
    data = json.loads(path.read_text(encoding="utf-8"))
    files = data.get("files")
    if not isinstance(files, list) or not all(isinstance(name, str) and FONT_NAME.fullmatch(name) for name in files):
        raise ValueError("Jetendard manifest contains an invalid files list")
    return data


def atomic_json(path: Path, data: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=".manifest.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(data, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
        os.chmod(temporary, 0o600)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def install(home: Path) -> None:
    api_url = os.environ.get("TERRAPOD_JETENDARD_RELEASE_API_URL", RELEASE_API_URL)
    tag, digest, asset_url = select_asset(load_json_url(api_url))
    fonts_dir = home / "Library" / "Fonts"
    fonts_dir.mkdir(parents=True, exist_ok=True)
    manifest = manifest_path(home)
    old = read_manifest(manifest)
    with tempfile.TemporaryDirectory(prefix="terrapod-jetendard-") as temporary:
        archive_path = Path(temporary) / ASSET_NAME
        download(asset_url, archive_path)
        actual_digest = sha256(archive_path)
        if actual_digest != digest:
            raise ValueError(f"Jetendard archive digest mismatch: expected {digest}, got {actual_digest}")
        with zipfile.ZipFile(archive_path) as archive:
            entries = font_entries(archive)
            staged = Path(temporary) / "fonts"
            staged.mkdir()
            for basename, info in entries.items():
                destination = staged / basename
                with archive.open(info) as source, destination.open("wb") as output:
                    shutil.copyfileobj(source, output)
                destination.chmod(0o644)
            for basename in sorted(entries):
                os.replace(staged / basename, fonts_dir / basename)
    new_files = sorted(entries)
    atomic_json(manifest, {"tag": tag, "digest": digest, "files": new_files})
    for basename in old.get("files", []):
        if basename not in new_files:
            candidate = fonts_dir / basename
            if candidate.is_file() or candidate.is_symlink():
                candidate.unlink()
    print(f"Installed Jetendard {tag} ({len(new_files)} TTF files).")


def check(home: Path, state_home: str | None) -> None:
    manifest = read_manifest(manifest_path(home, state_home))
    tag = manifest.get("tag")
    digest = manifest.get("digest")
    files = manifest.get("files", [])
    if not isinstance(tag, str) or not tag or not isinstance(digest, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest) or not files:
        raise ValueError("Jetendard install manifest is missing or incomplete")
    missing = [name for name in files if not (home / "Library" / "Fonts" / name).is_file()]
    if missing:
        raise ValueError("Jetendard manifest-owned font files are missing: " + ", ".join(missing))
    print(f"Jetendard {tag} is installed ({len(files)} TTF files).")


def main() -> int:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("install")
    checker = subparsers.add_parser("check")
    checker.add_argument("--home")
    checker.add_argument("--state-home")
    args = parser.parse_args()
    try:
        if args.command == "install":
            install(Path(os.environ.get("HOME", str(Path.home()))))
        else:
            check(Path(args.home or os.environ.get("HOME", str(Path.home()))), args.state_home)
    except Exception as error:
        print(f"Jetendard font: {error}", file=__import__("sys").stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
```

Mark it executable with `chmod +x dot_local/lib/terrapod/executable_jetendard-font`.

- [ ] **Step 4: Run the installer tests**

Run: `sh tests/jetendard_font_test.sh`

Expected: PASS for complete release installation, manifest-limited cleanup, digest rejection, and preservation of the working installation.

- [ ] **Step 5: Commit the owned installer**

```bash
git add tests/jetendard_font_test.sh dot_local/lib/terrapod/executable_jetendard-font
git commit -m "feat: add owned Jetendard font installer"
```

### Task 2: Wire the Font Lifecycle into Chezmoi and Install Warnings

**Files:**
- Create: `.chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl`
- Create: `.chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl`
- Modify: `dot_local/lib/terrapod/install-warnings.sh`
- Modify: `.chezmoiignore`
- Modify: `Brewfile`
- Modify: `tests/chezmoiignore_test.sh`
- Modify: `tests/terrapod_command_test.sh`

**Interfaces:**
- Consumes: Task 1 CLI `python3 executable_jetendard-font install`.
- Produces: install-warning category `jetendard-font`; macOS-only source-change installer; marker-gated retry on later applies; no Homebrew font declarations.

- [ ] **Step 1: Add failing lifecycle and platform-boundary assertions**

In `tests/chezmoiignore_test.sh`, add the three new source entries to template rendering, assert both rendered shell files pass `sh -n`, assert the installer contains the helper checksum and `install` invocation, and assert the retry invokes the helper only when the warning marker exists. Replace the old two-cask loop with:

```sh
if grep -E '^[[:space:]]*cask[[:space:]]+"font-(jetbrains-mono-nerd-font|d2coding)"' "$repo_root/Brewfile" >/dev/null; then
  fail "core Brewfile no longer declares superseded terminal font casks"
fi
pass "core Brewfile no longer declares superseded terminal font casks"

if grep -E '^[[:space:]]*cask[[:space:]]+' "$repo_root/Brewfile" >/dev/null; then
  fail "core Brewfile contains no casks after Jetendard moves to its release installer"
fi
pass "core Brewfile contains no casks after Jetendard moves to its release installer"
```

Add the helper and three rendered targets to `macos_only_entries`, then assert none appears in `ubuntu_managed` and all appear in `macos_managed`.

In `tests/terrapod_command_test.sh`, extend the warning-category fixture loop to expect `jetendard-font` and `jetendard-settings` as valid categories and unknown spellings to remain rejected.

- [ ] **Step 2: Run focused tests to verify they fail**

Run: `sh tests/chezmoiignore_test.sh`

Expected: FAIL because the scripts are absent and the old casks remain.

Run: `sh tests/terrapod_command_test.sh`

Expected: FAIL because the warning library rejects the new categories.

- [ ] **Step 3: Register warning categories and remove old casks**

Change both category functions in `dot_local/lib/terrapod/install-warnings.sh` so the canonical order ends with:

```sh
    optional-ai-cli-tools \
    jetendard-font \
    jetendard-settings
```

and the accepted `case` arm is:

```sh
    homebrew-core|homebrew-desktop-apps|ubuntu-bootstrap|shell-integrations|mise-tools|optional-ai-cli-tools|jetendard-font|jetendard-settings)
```

Delete these lines from `Brewfile` without adding a replacement cask:

```ruby
cask "font-jetbrains-mono-nerd-font"
cask "font-d2coding"
```

- [ ] **Step 4: Add the source-change installer and retry adapter**

Create `.chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl`:

```sh
{{ if eq .chezmoi.os "darwin" -}}
#!/bin/sh
set -u

warnings_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warnings.sh"
font_helper="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/executable_jetendard-font"
# Jetendard font helper checksum: {{ include "dot_local/lib/terrapod/executable_jetendard-font" | sha256sum }}
. "$warnings_lib"

if command -v python3 >/dev/null 2>&1 && python3 "$font_helper" install; then
  terrapod_install_warning_clear jetendard-font
  printf '%s\n' "Jetendard is ready. Restart Ghostty or Zed if an existing window still uses a cached font."
  exit 0
fi

terrapod_install_warning_write \
  jetendard-font \
  "Jetendard font install needs attention" \
  "Restore Python and GitHub access, then rerun tpod apply." || exit 1
exit 0
{{- end }}
```

Create `.chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl`:

```sh
{{ if eq .chezmoi.os "darwin" -}}
#!/bin/sh
set -u

warnings_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warnings.sh"
font_helper="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/executable_jetendard-font"
. "$warnings_lib"

if ! terrapod_install_warning_existing_path jetendard-font >/dev/null 2>&1; then
  exit 0
fi

if command -v python3 >/dev/null 2>&1 && python3 "$font_helper" install; then
  terrapod_install_warning_clear jetendard-font
  exit 0
fi

terrapod_install_warning_write \
  jetendard-font \
  "Jetendard font install needs attention" \
  "Restore Python and GitHub access, then rerun tpod apply." || exit 1
exit 0
{{- end }}
```

- [ ] **Step 5: Exclude the new lifecycle from Ubuntu**

Inside the non-Darwin block of `.chezmoiignore`, add exact rendered target paths:

```text
.chezmoiscripts/02-retry-jetendard-font.sh
.chezmoiscripts/65-install-jetendard-font.sh
.chezmoiscripts/70-apply-jetendard-settings.sh
.local/lib/terrapod/jetendard-font
.local/lib/terrapod/jetendard-settings
```

- [ ] **Step 6: Run focused lifecycle tests**

Run: `sh tests/chezmoiignore_test.sh`

Expected: PASS, including macOS inclusion, Ubuntu exclusion, rendered shell syntax, helper checksum, marker retry, and no font casks.

Run: `sh tests/terrapod_command_test.sh`

Expected: PASS, including both new warning categories.

- [ ] **Step 7: Commit lifecycle wiring**

```bash
git add Brewfile .chezmoiignore .chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl .chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl dot_local/lib/terrapod/install-warnings.sh tests/chezmoiignore_test.sh tests/terrapod_command_test.sh
git commit -m "feat: manage Jetendard font lifecycle"
```

### Task 3: Apply Narrow Ghostty, Zed, and Orca Font Settings

**Files:**
- Create: `tests/jetendard_settings_test.sh`
- Create: `dot_local/lib/terrapod/executable_jetendard-settings`
- Create: `.chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl`
- Modify: `dot_config/ghostty/config`
- Modify: `tests/chezmoiignore_test.sh`

**Interfaces:**
- Consumes: Task 2 warning category `jetendard-settings`.
- Produces: CLI commands `apply`, `check-ghostty`, `check-zed`, and `check-orca`; exit status `2` from `apply` means Zed succeeded but Orca was intentionally deferred because it is running.

- [ ] **Step 1: Write failing settings tests**

Create `tests/jetendard_settings_test.sh`. The fixtures must cover: a missing Zed file; a JSONC file with header and inline comments, trailing commas, and unrelated keys; an existing wrong font property; two Orca profiles with unrelated data; no Orca profiles; and `TERRAPOD_ORCA_RUNNING=1`. Assert exactly these semantic outcomes:

```sh
HOME="$home_dir" TERRAPOD_ORCA_RUNNING=0 python3 "$helper" apply
grep -F '// keep this comment' "$home_dir/.config/zed/settings.json" >/dev/null || fail "Zed comments survive"
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
```

Use the same fixture script to create a second profile before the first apply and assert both profile JSON files contain `settings.terminalFontFamily = "Jetendard"`. In a separate empty-home fixture, run `apply`, assert the minimal Zed file is created, and assert no Orca profile directory is created.

- [ ] **Step 2: Run the settings test to verify it fails**

Run: `sh tests/jetendard_settings_test.sh`

Expected: FAIL because the settings helper does not exist.

- [ ] **Step 3: Implement the JSONC-aware settings helper**

Create executable `dot_local/lib/terrapod/executable_jetendard-settings` in Python 3. The implementation must expose these functions and behavior:

```python
#!/usr/bin/env python3
import argparse
from dataclasses import dataclass
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

FONT = "Jetendard"


@dataclass
class Token:
    kind: str
    start: int
    end: int
    value: object = None


@dataclass
class Node:
    kind: str
    start: int
    end: int
    members: dict[str, tuple[Token, "Node"]] | None = None


def tokens(text: str) -> list[Token]:
    result = []
    index = 0
    while index < len(text):
        char = text[index]
        if char.isspace():
            index += 1
            continue
        if text.startswith("//", index):
            newline = text.find("\n", index + 2)
            index = len(text) if newline < 0 else newline
            continue
        if text.startswith("/*", index):
            end = text.find("*/", index + 2)
            if end < 0:
                raise ValueError("unterminated JSONC block comment")
            index = end + 2
            continue
        if char in "{}[],:":
            result.append(Token(char, index, index + 1, char))
            index += 1
            continue
        if char == '"':
            end = index + 1
            escaped = False
            while end < len(text):
                current = text[end]
                if current == '"' and not escaped:
                    end += 1
                    break
                if current == "\\" and not escaped:
                    escaped = True
                else:
                    escaped = False
                end += 1
            else:
                raise ValueError("unterminated JSONC string")
            raw = text[index:end]
            result.append(Token("string", index, end, json.loads(raw)))
            index = end
            continue
        end = index
        while end < len(text) and not text[end].isspace() and text[end] not in "{}[],:/":
            end += 1
        if end == index:
            raise ValueError(f"unexpected JSONC character at byte {index}")
        result.append(Token("literal", index, end, text[index:end]))
        index = end
    return result


def parse_value(stream: list[Token], index: int) -> tuple[Node, int]:
    token = stream[index]
    if token.kind == "{":
        members = {}
        cursor = index + 1
        while stream[cursor].kind != "}":
            key = stream[cursor]
            if key.kind != "string" or stream[cursor + 1].kind != ":":
                raise ValueError("invalid JSONC object member")
            value, cursor = parse_value(stream, cursor + 2)
            members[str(key.value)] = (key, value)
            if stream[cursor].kind == ",":
                cursor += 1
                if stream[cursor].kind == "}":
                    break
            elif stream[cursor].kind != "}":
                raise ValueError("missing JSONC object comma")
        return Node("object", token.start, stream[cursor].end, members), cursor + 1
    if token.kind == "[":
        cursor = index + 1
        while stream[cursor].kind != "]":
            _, cursor = parse_value(stream, cursor)
            if stream[cursor].kind == ",":
                cursor += 1
                if stream[cursor].kind == "]":
                    break
            elif stream[cursor].kind != "]":
                raise ValueError("missing JSONC array comma")
        return Node("array", token.start, stream[cursor].end), cursor + 1
    if token.kind in {"string", "literal"}:
        return Node(token.kind, token.start, token.end), index + 1
    raise ValueError("invalid JSONC value")


def root_node(text: str) -> Node:
    stream = tokens(text)
    if not stream:
        raise ValueError("empty JSONC document")
    root, cursor = parse_value(stream, 0)
    if root.kind != "object" or cursor != len(stream):
        raise ValueError("Zed settings must contain one JSONC object")
    return root


def line_indent(text: str, position: int) -> str:
    start = text.rfind("\n", 0, position) + 1
    return re.match(r"[ \t]*", text[start:position]).group(0)


def insert_member(text: str, obj: Node, key: str, value_text: str) -> str:
    close = obj.end - 1
    closing_line = text.rfind("\n", 0, close) + 1
    closing_indent = line_indent(text, close)
    child_indent = closing_indent + "  "
    rendered = f'{child_indent}{json.dumps(key)}: {value_text},\n'
    members = obj.members or {}
    if not members:
        if closing_line == close:
            return text[:closing_line] + rendered + text[closing_line:]
        return text[:close] + "\n" + rendered + closing_indent + text[close:]
    last = max((value for _, value in members.values()), key=lambda node: node.end)
    between = text[last.end:close]
    if not any(token.kind == "," for token in tokens(between)):
        text = text[:last.end] + "," + text[last.end:]
        close += 1
        closing_line += 1
    return text[:closing_line] + rendered + text[closing_line:]


def set_jsonc_string(text: str, path: tuple[str, ...], value: str) -> str:
    root = root_node(text)
    current = root
    for depth, key in enumerate(path):
        final = depth == len(path) - 1
        member = (current.members or {}).get(key)
        if final:
            if member:
                node = member[1]
                return text[:node.start] + json.dumps(value) + text[node.end:]
            return insert_member(text, current, key, json.dumps(value))
        if member and member[1].kind == "object":
            current = member[1]
            continue
        nested = "{\n" + line_indent(text, current.end - 1) + "    " + json.dumps(path[depth + 1]) + ": " + json.dumps(value) + ",\n" + line_indent(text, current.end - 1) + "  }"
        if member:
            node = member[1]
            return text[:node.start] + nested + text[node.end:]
        return insert_member(text, current, key, nested)
    return text


def jsonc_string(text: str, path: tuple[str, ...]) -> str | None:
    current = root_node(text)
    for key in path:
        member = (current.members or {}).get(key)
        if not member:
            return None
        current = member[1]
    if current.kind != "string":
        return None
    return json.loads(text[current.start:current.end])


def atomic_write(path: Path, content: str, default_mode: int) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    mode = path.stat().st_mode & 0o777 if path.exists() else default_mode
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write(content)
        os.chmod(temporary, mode)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def apply_zed(home: Path) -> None:
    path = home / ".config" / "zed" / "settings.json"
    text = path.read_text(encoding="utf-8") if path.exists() else "{\n}\n"
    changed = set_jsonc_string(text, ("buffer_font_family",), FONT)
    changed = set_jsonc_string(changed, ("terminal", "font_family"), FONT)
    root_node(changed)
    if changed != text:
        atomic_write(path, changed, 0o600)


def orca_running() -> bool:
    override = os.environ.get("TERRAPOD_ORCA_RUNNING")
    if override is not None:
        return override == "1"
    return subprocess.run(["pgrep", "-f", "/Applications/Orca.app/Contents/MacOS/Orca"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0


def orca_profiles(home: Path) -> list[Path]:
    return sorted((home / "Library" / "Application Support" / "orca" / "profiles").glob("*/orca-data.json"))


def apply_orca(home: Path) -> bool:
    profiles = orca_profiles(home)
    if not profiles:
        return True
    pending = []
    for path in profiles:
        data = json.loads(path.read_text(encoding="utf-8"))
        settings = data.get("settings")
        if not isinstance(settings, dict):
            raise ValueError(f"Orca settings is not an object: {path}")
        if settings.get("terminalFontFamily") != FONT:
            pending.append((path, data))
    if not pending:
        return True
    if orca_running():
        return False
    for path, data in pending:
        data["settings"]["terminalFontFamily"] = FONT
        atomic_write(path, json.dumps(data, ensure_ascii=False, indent=2) + "\n", 0o600)
    return True


def check_ghostty(home: Path) -> str:
    path = home / ".config" / "ghostty" / "config"
    families = []
    if path.is_file():
        for line in path.read_text(encoding="utf-8").splitlines():
            match = re.match(r"\s*font-family\s*=\s*[\"']?([^\"']+?)[\"']?\s*$", line)
            if match:
                families.append(match.group(1))
    if families != [FONT]:
        raise ValueError(f"Ghostty font-family must be exactly {FONT}: {path}")
    return "Ghostty uses Jetendard."


def check_zed(home: Path) -> str:
    path = home / ".config" / "zed" / "settings.json"
    if not path.is_file():
        raise ValueError(f"Zed settings is missing: {path}")
    text = path.read_text(encoding="utf-8")
    if jsonc_string(text, ("buffer_font_family",)) != FONT or jsonc_string(text, ("terminal", "font_family")) != FONT:
        raise ValueError(f"Zed buffer and terminal fonts must be {FONT}: {path}")
    return "Zed buffer and terminal use Jetendard."


def check_orca(home: Path) -> str:
    mismatches = []
    profiles = orca_profiles(home)
    for path in profiles:
        data = json.loads(path.read_text(encoding="utf-8"))
        if data.get("settings", {}).get("terminalFontFamily") != FONT:
            mismatches.append(str(path))
    if mismatches:
        raise ValueError("Orca terminal font must be Jetendard: " + ", ".join(mismatches))
    return "Orca profiles use Jetendard." if profiles else "No initialized Orca profiles require validation."


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=["apply", "check-ghostty", "check-zed", "check-orca"])
    parser.add_argument("--home")
    args = parser.parse_args()
    home = Path(args.home or os.environ.get("HOME", str(Path.home())))
    try:
        if args.command == "apply":
            apply_zed(home)
            if not apply_orca(home):
                print("Orca is running; quit Orca and rerun tpod apply.")
                return 2
            print("Jetendard application settings are applied.")
        else:
            checker = {"check-ghostty": check_ghostty, "check-zed": check_zed, "check-orca": check_orca}[args.command]
            print(checker(home))
    except Exception as error:
        print(f"Jetendard settings: {error}", file=__import__("sys").stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
```

Mark the source executable with `chmod +x dot_local/lib/terrapod/executable_jetendard-settings`.

- [ ] **Step 4: Add the always-run settings adapter and Ghostty setting**

Create `.chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl`:

```sh
{{ if eq .chezmoi.os "darwin" -}}
#!/bin/sh
set -u

warnings_lib="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warnings.sh"
settings_helper="{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/executable_jetendard-settings"
. "$warnings_lib"

if ! command -v python3 >/dev/null 2>&1; then
  terrapod_install_warning_write jetendard-settings "Jetendard app settings need attention" "Restore Python, quit Orca, then rerun tpod apply." || exit 1
  exit 0
fi

python3 "$settings_helper" apply
settings_status="$?"
case "$settings_status" in
  0)
    terrapod_install_warning_clear jetendard-settings
    ;;
  2)
    terrapod_install_warning_write jetendard-settings "Jetendard Orca setting is deferred" "Quit Orca, then rerun tpod apply." || exit 1
    ;;
  *)
    terrapod_install_warning_write jetendard-settings "Jetendard app settings need attention" "Repair the reported Zed or Orca settings file, quit Orca, then rerun tpod apply." || exit 1
    ;;
esac
exit 0
{{- end }}
```

Replace the two Ghostty font lines with exactly:

```text
font-family = "Jetendard"
```

In `tests/chezmoiignore_test.sh`, keep Zed script-managed rather than adding `.config/zed/settings.json` as a chezmoi target. Add these exact assertions after the existing user-scoped app-config block:

```sh
assert_managed_paths_include_prefix \
  "$macos_managed" \
  ".chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl" \
  "macOS default applies Jetendard app settings"

assert_managed_paths_include_prefix \
  "$macos_development_apps_managed" \
  ".chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl" \
  "development-apps selection applies the same user-scoped Jetendard settings"

assert_managed_paths_exclude_prefix \
  "$ubuntu_managed" \
  ".chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl" \
  "Ubuntu excludes Jetendard app settings"

ghostty_font_lines="$(grep -E '^[[:space:]]*font-family[[:space:]]*=' "$repo_root/dot_config/ghostty/config")"
assert_text_equals "$ghostty_font_lines" 'font-family = "Jetendard"' \
  "Ghostty uses Jetendard as its sole font family"
assert_not_contains_text "$(cat "$repo_root/dot_config/ghostty/config")" "JetBrainsMono Nerd Font" \
  "Ghostty no longer declares JetBrains Mono Nerd Font"
assert_not_contains_text "$(cat "$repo_root/dot_config/ghostty/config")" "D2Coding" \
  "Ghostty no longer declares D2Coding"
```

- [ ] **Step 5: Run settings and chezmoi tests**

Run: `sh tests/jetendard_settings_test.sh`

Expected: PASS for JSONC preservation, missing-file creation, idempotence, multi-profile Orca application, running-Orca deferral, and mismatch detection.

Run: `sh tests/chezmoiignore_test.sh`

Expected: PASS for macOS-only settings adapter management and the Ghostty single-family contract.

- [ ] **Step 6: Commit application settings**

```bash
git add dot_config/ghostty/config dot_local/lib/terrapod/executable_jetendard-settings .chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl tests/jetendard_settings_test.sh tests/chezmoiignore_test.sh
git commit -m "feat: apply Jetendard to terminal apps"
```

### Task 4: Add Offline Status and Doctor Validation

**Files:**
- Modify: `dot_local/bin/executable_terrapod`
- Modify: `tests/terrapod_command_test.sh`

**Interfaces:**
- Consumes: Task 1 `check`; Task 3 `check-ghostty`, `check-zed`, and `check-orca`.
- Produces: macOS-only `Jetendard font: installed|missing` status line and four doctor checks; no network activity.

- [ ] **Step 1: Add failing status and doctor fixtures**

In `tests/terrapod_command_test.sh`, set helper overrides next to `TERRAPOD_INSTALL_WARNINGS_LIB`:

```sh
export TERRAPOD_JETENDARD_FONT_HELPER="$repo_root/dot_local/lib/terrapod/executable_jetendard-font"
export TERRAPOD_JETENDARD_SETTINGS_HELPER="$repo_root/dot_local/lib/terrapod/executable_jetendard-settings"
```

Create an isolated macOS home containing a valid manifest, one font file, the managed Ghostty config, a Zed JSONC file, and two Orca profile files. Assert `status` contains `Jetendard font: installed`, `doctor` contains four `ok` lines, and both commands exit zero. Remove one manifest-owned font and assert status prints `Jetendard font: missing`, doctor exits non-zero, and guidance says `Run 'tpod apply' to restore Jetendard.` Mutate one Orca profile and assert doctor names the mismatched profile.

Extend the existing broad-command stub list with `curl` and assert neither `status` nor `doctor` invokes it.

- [ ] **Step 2: Run the command test to verify it fails**

Run: `sh tests/terrapod_command_test.sh`

Expected: FAIL because status and doctor do not inspect Jetendard.

- [ ] **Step 3: Add helper paths and local check adapters**

Near the installed warning helper at the top of `dot_local/bin/executable_terrapod`, add:

```sh
jetendard_font_helper="${TERRAPOD_JETENDARD_FONT_HELPER:-$command_dir/../lib/terrapod/jetendard-font}"
jetendard_settings_helper="${TERRAPOD_JETENDARD_SETTINGS_HELPER:-$command_dir/../lib/terrapod/jetendard-settings}"

run_jetendard_check() {
  helper="$1"
  command_name="$2"
  shift 2
  if [ ! -f "$helper" ] || ! command -v python3 >/dev/null 2>&1; then
    printf '%s\n' "Jetendard checker is unavailable"
    return 1
  fi
  python3 "$helper" "$command_name" "$@" 2>&1
}
```

Add `print_jetendard_status` and call it from `run_status` after key tools only when `profile=macos-terminal`:

```sh
print_jetendard_status() {
  if run_jetendard_check "$jetendard_font_helper" check >/dev/null; then
    print_colon_state_line "Jetendard font" "installed"
  else
    print_colon_state_line "Jetendard font" "missing"
  fi
}
```

- [ ] **Step 4: Add doctor checks for the manifest and app settings**

Add:

```sh
doctor_check_jetendard_component() {
  label="$1"
  helper="$2"
  command_name="$3"
  if detail="$(run_jetendard_check "$helper" "$command_name")"; then
    doctor_ok "$label: $detail"
  else
    doctor_warn "$label: $detail"
    doctor_guidance "Run 'tpod apply' to restore Jetendard. Quit Orca first when an Orca profile is out of sync."
  fi
}
```

In the macOS branch of `run_doctor`, immediately after `doctor_check_command brew`, call:

```sh
doctor_check_jetendard_component "Jetendard font files" "$jetendard_font_helper" check
doctor_check_jetendard_component "Ghostty font setting" "$jetendard_settings_helper" check-ghostty
doctor_check_jetendard_component "Zed font settings" "$jetendard_settings_helper" check-zed
doctor_check_jetendard_component "Orca font settings" "$jetendard_settings_helper" check-orca
```

Do not add these checks to the VPS Shell Profile branch.

- [ ] **Step 5: Run command tests**

Run: `sh tests/terrapod_command_test.sh`

Expected: PASS for installed, missing, mismatch, profile exclusion, guidance, and no-network assertions.

- [ ] **Step 6: Commit readiness reporting**

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh
git commit -m "feat: diagnose Jetendard readiness"
```

### Task 5: Document the Release Exception and Migration

**Files:**
- Create: `docs/adr/0009-install-jetendard-from-latest-stable-release.md`
- Modify: `CONTEXT.md`
- Modify: `README.md`
- Modify: `README.ko.md`
- Modify: `tests/readme_optional_stack_profiles_test.sh`
- Modify: `tests/readme_korean_test.sh`

**Interfaces:**
- Consumes: Tasks 1–4 behavior and exact warning/recovery commands.
- Produces: canonical user-facing contract and domain decision record.

- [ ] **Step 1: Add failing documentation assertions**

In `tests/readme_optional_stack_profiles_test.sh`, replace `Terminal font casks` expectations and add exact assertions for:

```text
Jetendard terminal font from the latest stable GitHub release
Terrapod checks the latest Jetendard release only when its managed font installer source changes or a failed install is retried.
Terrapod does not uninstall existing JetBrains Mono Nerd Font or D2Coding copies.
Quit Orca before rerunning `tpod apply` when Jetendard settings are deferred.
```

In `tests/readme_korean_test.sh`, add the corresponding Korean assertions and retain the existing heading parity check.

- [ ] **Step 2: Run documentation tests to verify they fail**

Run: `sh tests/readme_optional_stack_profiles_test.sh`

Expected: FAIL because the Canonical README still says terminal font casks.

Run: `sh tests/readme_korean_test.sh`

Expected: FAIL because the Korean README lacks the new lifecycle and recovery copy.

- [ ] **Step 3: Write ADR 0009**

Create `docs/adr/0009-install-jetendard-from-latest-stable-release.md`:

```markdown
# Install Jetendard from the latest stable release

The macOS Terminal Profile uses Jetendard as its shared coding and terminal font for Ghostty, Zed buffers and terminals, and Orca terminals. Jetendard has no official Homebrew cask, so Terrapod installs every TTF from the latest stable `kuskhan/jetendard` GitHub release, verifies the asset digest published by GitHub, and records the installed tag and owned files in a user-scoped manifest.

This is a narrow exception to ADR 0001's normal Homebrew ownership of macOS fonts. The installer checks GitHub only when its managed source changes or a failed installation is retried; ordinary `tpod status` and `tpod doctor` are offline checks, and an upstream release alone does not trigger an upgrade.

## Considered Options

- Keep JetBrains Mono Nerd Font plus D2Coding: rejected because Jetendard combines Nerd Font Latin and symbols with balanced Pretendard Korean glyphs in one monospace family.
- Add an unowned Homebrew cask token: rejected because neither `font-jetendard` nor `jetendard` exists in the official cask repository.
- Pin one Jetendard tag: rejected because this machine configuration should resolve the latest stable release when Terrapod intentionally reruns the managed installer.
- Query GitHub on every apply: rejected because an unchanged Terrapod source should not create a continuous font-upgrade channel.

## Consequences

- The installer needs Python and network access only during install or retry.
- Failed replacement preserves the prior working font files and records a non-blocking warning.
- Terrapod removes only obsolete files named by its own manifest.
- Existing Homebrew-installed JetBrains Mono Nerd Font and D2Coding copies remain installed but unmanaged.
- App settings change only font-family keys; Orca updates wait until the app is closed.
```

- [ ] **Step 4: Update domain and user documentation**

In `CONTEXT.md`, replace the two `terminal font casks` relationship bullets with explicit Jetendard release-installer ownership, source-change trigger, manifest cleanup boundary, app-setting scope, Orca deferral, and no-Ubuntu rules. Add a relationship that this decision supersedes only ADR 0001's font-provider consequence, not its Homebrew/mise boundaries for other tools.

In `README.md` and `README.ko.md`, update the macOS inventory and add a compact paragraph after it explaining:

- latest stable TTF release and digest verification;
- queries occur only after Terrapod installer-source changes or warning retry;
- Ghostty, Zed buffer/terminal, and Orca terminal scope;
- quit-Orca/reapply recovery;
- existing old fonts are not automatically uninstalled;
- app restart may be needed for cached fonts.

Keep all Markdown headings identical and in the same order between the two README files.

- [ ] **Step 5: Run documentation tests**

Run: `sh tests/readme_optional_stack_profiles_test.sh`

Expected: PASS with the Canonical README Jetendard lifecycle assertions.

Run: `sh tests/readme_korean_test.sh`

Expected: PASS with Korean copy and exact heading parity.

- [ ] **Step 6: Commit documentation**

```bash
git add README.md README.ko.md CONTEXT.md docs/adr/0009-install-jetendard-from-latest-stable-release.md tests/readme_optional_stack_profiles_test.sh tests/readme_korean_test.sh
git commit -m "docs: record Jetendard font policy"
```

### Task 6: Run Full Verification and Review the Delivered State

**Files:**
- Verify: all files changed in Tasks 1–5

**Interfaces:**
- Consumes: every prior task.
- Produces: one fully verified branch ready for review; no additional runtime interface.

- [ ] **Step 1: Run syntax checks**

Run:

```bash
sh -n dot_local/lib/terrapod/install-warnings.sh
sh -n dot_local/bin/executable_terrapod
python3 -m py_compile dot_local/lib/terrapod/executable_jetendard-font dot_local/lib/terrapod/executable_jetendard-settings
```

Expected: all commands exit 0 with no output.

- [ ] **Step 2: Run the focused Jetendard suites**

Run:

```bash
sh tests/jetendard_font_test.sh
sh tests/jetendard_settings_test.sh
sh tests/chezmoiignore_test.sh
sh tests/terrapod_command_test.sh
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
```

Expected: every script exits 0 and prints its named `ok -` results plus intentional fixture diagnostics.

- [ ] **Step 3: Run every repository test script**

Run:

```bash
for test_script in tests/*_test.sh; do
  sh "$test_script" || exit 1
done
```

Expected: all test scripts exit 0.

- [ ] **Step 4: Render and inspect the macOS managed surface**

Run:

```bash
chezmoi --source . --override-data '{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false,"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false}' managed --path-style source-relative | rg 'jetendard|ghostty'
```

Expected: both helpers, all three Jetendard scripts, and Ghostty config appear.

Run:

```bash
chezmoi --source . --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false,"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false}' managed --path-style source-relative | rg 'jetendard|ghostty'
```

Expected: `rg` exits 1 with no matches.

- [ ] **Step 5: Review diffs and whitespace**

Run:

```bash
git diff --check
git status --short
git diff -- Brewfile .chezmoiignore .chezmoiscripts dot_config/ghostty/config dot_local/lib/terrapod dot_local/bin/executable_terrapod README.md README.ko.md CONTEXT.md docs/adr tests
```

Expected: `git diff --check` exits 0; status and diff contain only the planned Jetendard files and the plan document.

- [ ] **Step 6: Commit verification-only fixes if needed**

If verification required a correction, stage only the corrected Jetendard files and create:

```bash
git commit -m "fix: complete Jetendard verification"
```

If no correction was needed, do not create an empty commit.

---

## Self-Review Results

- **Spec coverage:** Tasks 1–2 cover stable-latest resolution, digest verification, full TTF installation, manifest ownership, limited cleanup, source-change-only queries, recovery, macOS-only scope, and old-cask removal. Task 3 covers the exact Ghostty/Zed/Orca surfaces, JSONC preservation, missing Zed creation, multi-profile Orca behavior, running-Orca deferral, idempotence, and untouched display settings. Task 4 covers offline status/doctor validation. Task 5 covers the provider exception, migration, recovery, restart guidance, and bilingual documentation.
- **Placeholder scan:** The plan contains no deferred implementation markers or unspecified error-handling steps. Each new interface, test fixture, command, expected result, and documentation sentence is explicit.
- **Type consistency:** Both Python helpers use `--home`; the font helper alone uses `--state-home`; scripts invoke `install`/`apply`; Terrapod invokes `check`, `check-ghostty`, `check-zed`, and `check-orca`; warning categories are consistently `jetendard-font` and `jetendard-settings`.
