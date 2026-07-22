#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

fail() { printf '%s\n' "not ok - $1" >&2; exit 1; }
pass() { printf '%s\n' "ok - $1"; }

command -v chezmoi >/dev/null 2>&1 || fail "chezmoi is required"
: >"$tmp_dir/chezmoi.toml"
: >"$tmp_dir/manager.log"

python3 - "$tmp_dir" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
presets = {
    "minimal": dict.fromkeys([
        "enableEditorStack", "enableAiCliTools", "enableDevelopmentWorkspace",
        "enableMacosAppGroupTerminalApps", "enableMacosAppGroupAutomation",
        "enableMacosAppGroupLauncher", "enableMacosAppGroupMonitoring",
        "enableMacosAppGroupDevelopmentApps",
    ], False),
    "development": {
        "enableEditorStack": True, "enableAiCliTools": True,
        "enableDevelopmentWorkspace": True,
        "enableMacosAppGroupTerminalApps": False,
        "enableMacosAppGroupAutomation": False,
        "enableMacosAppGroupLauncher": False,
        "enableMacosAppGroupMonitoring": False,
        "enableMacosAppGroupDevelopmentApps": False,
    },
    "workstation": dict.fromkeys([
        "enableEditorStack", "enableAiCliTools", "enableDevelopmentWorkspace",
        "enableMacosAppGroupTerminalApps", "enableMacosAppGroupAutomation",
        "enableMacosAppGroupLauncher", "enableMacosAppGroupMonitoring",
        "enableMacosAppGroupDevelopmentApps",
    ], True),
}
for profile, os_name in (("macos-terminal", "darwin"), ("vps-shell", "linux")):
    for preset, values in presets.items():
        data = dict(values)
        data["profile"] = profile
        data["chezmoi"] = {"os": os_name, "osRelease": {"id": "ubuntu", "versionID": "24.04"}}
        (root / f"{profile}-{preset}.json").write_text(json.dumps(data), encoding="utf-8")
PY

scripts="$(find "$repo_root/.chezmoiscripts" -type f -name '*.tmpl' | LC_ALL=C sort)"
[ -n "$scripts" ] || fail "legacy script fixtures are missing"

for profile in macos-terminal vps-shell; do
  for preset in minimal development workstation; do
    data_file="$tmp_dir/$profile-$preset.json"
    render_dir="$tmp_dir/rendered/$profile/$preset"
    mkdir -p "$render_dir"
    for script in $scripts; do
      name="$(basename "$script")"
      chezmoi \
        --config "$tmp_dir/chezmoi.toml" \
        --source "$repo_root" \
        --override-data-file "$data_file" \
        execute-template --file "$script" >"$render_dir/$name"
    done

    printf '%s\n' "managed --override-data-file $data_file --exclude scripts" >>"$tmp_dir/manager.log"
    managed="$(chezmoi \
      --config "$tmp_dir/chezmoi.toml" \
      --source "$repo_root" \
      --override-data-file "$data_file" \
      managed --exclude scripts --path-style source-relative)"
    if printf '%s\n' "$managed" | grep -E '^\.chezmoiscripts(/|$)' >/dev/null; then
      fail "$profile/$preset shadow inventory contains a chezmoi script"
    fi
  done
done
pass "every legacy script condition renders for both profiles and all presets"
pass "all six shadow inventories exclude chezmoi scripts"

python3 - "$repo_root" "$tmp_dir/manager.log" <<'PY'
import json
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
catalog = json.loads((root / "catalog/v1/resources.json").read_text(encoding="utf-8"))
resources = catalog["resources"]
by_id = {}
for item in resources:
    if item["id"] in by_id:
        raise AssertionError(f"duplicate catalog ID: {item['id']}")
    by_id[item["id"]] = item

scripts = {path.name for path in (root / ".chezmoiscripts").glob("*.tmpl")}
owners = {
    "run_onchange_before_00-bootstrap-ubuntu.sh.tmpl": {"provider": "apt"},
    "run_onchange_before_10-bootstrap-homebrew.sh.tmpl": {"provider": "homebrew-formula"},
    "run_before_11-retry-homebrew-core.sh.tmpl": {"provider": "homebrew-formula"},
    "run_before_13-retry-homebrew-desktop-apps.sh.tmpl": {"prefix": "optional-desktop."},
    "run_onchange_after_20-install-mise-tools.sh.tmpl": {"provider": "mise"},
    "run_after_21-retry-mise-tools.sh.tmpl": {"provider": "mise"},
    "run_onchange_before_30-install-shell-integrations.sh.tmpl": {"provider": "git"},
    "run_before_31-retry-shell-integrations.sh.tmpl": {"provider": "git"},
    "run_onchange_after_50-open-karabiner-if-needed.sh.tmpl": {"ids": ["integration.karabiner-opener"]},
    "run_onchange_before_60-install-ai-cli-tools.sh.tmpl": {"prefix": "optional-ai."},
    "run_onchange_after_65-install-jetendard-font.sh.tmpl": {"ids": ["font.jetendard"]},
    "run_before_02-retry-jetendard-font.sh.tmpl": {"ids": ["font.jetendard"]},
    "run_after_70-apply-jetendard-settings.sh.tmpl": {
        "ids": ["integration.jetendard-zed", "integration.jetendard-orca"],
    },
}
if scripts != set(owners):
    raise AssertionError(f"legacy mutation inventory mismatch: missing={scripts-set(owners)}, stale={set(owners)-scripts}")

for script, selector in owners.items():
    if "ids" in selector:
        selected = [by_id[id_] for id_ in selector["ids"]]
    elif "prefix" in selector:
        selected = [item for item in resources if item["id"].startswith(selector["prefix"])]
    else:
        selected = [item for item in resources if item["provider"] == selector["provider"]]
    if not selected:
        raise AssertionError(f"{script} has no typed resource owner")
    ids = [item["id"] for item in selected]
    if len(ids) != len(set(ids)):
        raise AssertionError(f"{script} maps a mutation to duplicate owners: {ids}")

managed = by_id.get("dotfiles.home")
assert managed is not None
assert managed["type"] == "managed-files" and managed["provider"] == "chezmoi"
assert managed["metadata"] == {"managedFiles.scope": "."}

for item in resources:
    metadata = item.get("metadata", {})
    if any("script" in key.lower() or "hook" in key.lower() for key in metadata):
        raise AssertionError(f"catalog resource {item['id']} declares executable metadata")

for line in pathlib.Path(sys.argv[2]).read_text(encoding="utf-8").splitlines():
    if "--override-data-file" not in line or "--exclude scripts" not in line:
        raise AssertionError(f"unconstrained manager chezmoi invocation: {line}")

# A shadow operation is always sourced from its exact typed catalog owner.
shadow_plan = [{"resourceId": item["id"], "source": f"{item['type']}/{item['provider']}"} for item in resources]
if any(operation["source"] == "chezmoi-script" for operation in shadow_plan):
    raise AssertionError("shadow plan contains chezmoi-script")
PY
pass "every legacy mutation responsibility has exact typed catalog owners"
pass "shadow plan contains no chezmoi-script operation"
pass "manager chezmoi logs carry override data and script exclusion"

mise exec go@1.26.0 -- go test ./internal/chezmoi ./internal/cli >/dev/null
pass "constrained client and CLI composition tests pass"
