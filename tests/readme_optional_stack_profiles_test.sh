#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
readme="$repo_root/README.md"

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

assert_contains() {
  needle="$1"
  message="$2"

  if ! grep -F "$needle" "$readme" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  needle="$1"
  message="$2"

  if grep -F "$needle" "$readme" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_key_row_contains() {
  key="$1"
  needle="$2"
  message="$3"

  if ! awk -v key="$key" -v needle="$needle" 'index($0, key) && index($0, needle) { found=1 } END { exit found ? 0 : 1 }' "$readme"; then
    fail "$message"
  fi

  pass "$message"
}

assert_ubuntu_setup_contains() {
  needle="$1"
  message="$2"

  if ! awk '
    /^### Ubuntu 24.04 VPS$/ { in_ubuntu = 1; next }
    /^### Intentional Upgrades$/ { in_ubuntu = 0 }
    in_ubuntu { print }
  ' "$readme" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_raycast_restore_contains() {
  needle="$1"
  message="$2"

  if ! awk '
    /^### Raycast$/ { in_raycast = 1; next }
    /^## Local Overrides$/ { in_raycast = 0 }
    in_raycast { print }
  ' "$readme" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_contains '| `enableEditorStack` | `false` |' \
  "README documents enableEditorStack default"
assert_contains '| `enableAiCliTools` | `false` |' \
  "README documents enableAiCliTools default"
assert_contains '| `enableDevelopmentWorkspace` | `false` |' \
  "README documents enableDevelopmentWorkspace default"
assert_not_contains 'enableMacosDesktopApps' \
  "README does not document legacy enableMacosDesktopApps option"

for key in \
  enableMacosAppGroupTerminalApps \
  enableMacosAppGroupAutomation \
  enableMacosAppGroupLauncher \
  enableMacosAppGroupMonitoring
do
  assert_contains "\`$key\`" "README documents $key option"
  if ! awk -F '|' -v key="\`$key\`" '$0 ~ key { gsub(/^[[:space:]]+|[[:space:]]+$/, "", $3); if ($3 == "`false`") found=1 } END { exit found ? 0 : 1 }' "$readme"; then
    fail "README documents $key default"
  fi
  pass "README documents $key default"
done

assert_key_row_contains '`enableMacosAppGroupTerminalApps`' 'terminal-apps' \
  "README documents terminal-apps group on its option row"
assert_key_row_contains '`enableMacosAppGroupTerminalApps`' 'Ghostty' \
  "README documents Ghostty on the terminal-apps option row"
assert_not_contains 'cmux' \
  "README no longer documents cmux as part of the macOS Desktop App Stack"
assert_key_row_contains '`enableMacosAppGroupAutomation`' 'automation' \
  "README documents automation group on its option row"
assert_key_row_contains '`enableMacosAppGroupAutomation`' 'Hammerspoon' \
  "README documents Hammerspoon on the automation option row"
assert_key_row_contains '`enableMacosAppGroupAutomation`' 'Karabiner-Elements' \
  "README documents Karabiner-Elements on the automation option row"
assert_key_row_contains '`enableMacosAppGroupLauncher`' 'launcher' \
  "README documents launcher group on its option row"
assert_key_row_contains '`enableMacosAppGroupLauncher`' 'Raycast' \
  "README documents Raycast on the launcher option row"
assert_key_row_contains '`enableMacosAppGroupLauncher`' '1Password CLI' \
  "README documents 1Password CLI on the launcher option row"
assert_key_row_contains '`enableMacosAppGroupMonitoring`' 'monitoring' \
  "README documents monitoring group on its option row"
assert_key_row_contains '`enableMacosAppGroupMonitoring`' 'iStat Menus' \
  "README documents iStat Menus on the monitoring option row"

assert_contains 'Optional stack profiles and macOS App Group settings are disabled by default.' \
  "README states optional stack profiles and App Groups are disabled by default"
assert_contains 'Terrapod is a small landing pod for your machines' \
  "README opens with the Terrapod product promise"
assert_contains 'Under the hood, Terrapod uses chezmoi as the apply engine' \
  "README keeps chezmoi visible as underlying machinery"
assert_contains '## Quick Start' \
  "README leads with a Quick Start section"
assert_contains '## What Terrapod Carries' \
  "README summarizes Terrapod's carried domain concepts"
assert_contains '## Choose a Preset' \
  "README uses the canonical Preset section title"
assert_contains 'Terrapod Setup requires `gum` (the Bootstrap UI Dependency)' \
  "README documents Bootstrap UI Dependency requirement for setup"
assert_contains '`terrapod configure <Preset>` is the script-friendly Preset configuration' \
  "README documents script-friendly Preset configuration"
assert_contains 'It writes concrete settings for exactly one supported Preset' \
  "README documents configure writes concrete settings for one Preset"
assert_contains 'require `gum`, and has no interactive customization.' \
  "README documents configure as no-gum and non-interactive"
assert_contains 'There is no plain text fallback.' \
  "README documents plain text fallback is intentionally disabled"
assert_contains '`terrapod configure <Preset>` are intentionally separate.' \
  "README documents setup and configure are intentionally separate"
assert_contains '`terrapod configure <Preset>` are intentionally separate. The latter writes' \
  "README documents setup and configure are intentionally separate"
assert_contains 'settings without the setup UI. If Terrapod Setup cannot run because `gum` or an' \
  "README documents configure is script-friendly and setup UI is intentionally separate"
assert_contains '<Preset>` is not a plain fallback for Terrapod Setup.' \
  "README states configure is not a Setup fallback"
assert_contains 'terminal environment and rerun `terrapod setup`.' \
  "README documents missing-gum Setup recovery guidance"
assert_contains 'Before Terrapod Setup, the first-run installer prepares `gum` as the required' \
  "README documents first-run gum bootstrap before setup on macOS"
assert_contains 'Bootstrap UI Dependency with Homebrew when `gum` is missing.' \
  "README documents macOS bootstrap UI dependency preparation path"
assert_contains 'bootstrap is limited to `gum`; it does not run broad Homebrew upgrades.' \
  "README documents macOS bootstrap UI dependency scope excludes broad Homebrew upgrades"
assert_contains 'Bootstrap UI Dependency from APT with the Charm APT repository when `gum` is' \
  "README documents Ubuntu bootstrap UI dependency preparation path"
assert_contains 'That setup UI bootstrap is limited to `gum`; it does not run broad APT' \
  "README documents Ubuntu bootstrap UI dependency scope excludes broad APT upgrades"
assert_contains '## What Terrapod Leaves Alone' \
  "README documents product boundaries near the top"
assert_contains '## Daily Commands' \
  "README uses a product-friendly daily command section"
assert_contains '## Platform Details' \
  "README moves platform inventory into platform details"
assert_ubuntu_setup_contains 'GitHub CLI (`gh`)' \
  "README documents gh as part of the Ubuntu Core Shell Stack"
assert_contains 'When `enableDevelopmentWorkspace` is `true`' \
  "README documents enableDevelopmentWorkspace behavior"
assert_contains 'Optional Editor Stack and Optional AI Tool Stack' \
  "README documents development workspace included stacks"
assert_contains 'macOS Desktop App Stack' \
  "README documents macOS Desktop App Stack"
assert_contains 'separate from `enableDevelopmentWorkspace`' \
  "README documents macOS Desktop App Stack separation from enableDevelopmentWorkspace"
assert_contains 'casks can affect shared applications' \
  "README documents why macOS Desktop App Stack remains separate"
assert_contains '`terrapod update` refreshes the Terrapod Source Repository through `chezmoi update --exclude scripts`.' \
  "README documents Terrapod update as source maintenance"
assert_contains 'Terrapod does not run broad Homebrew, APT, or mise upgrades.' \
  "README states Terrapod does not run broad package or tool upgrades"
assert_contains 'Homebrew and APT are Bootstrap Package Managers here: they prepare a machine for the declared shell state.' \
  "README preserves Bootstrap Package Manager boundary"
assert_contains 'mise is the Modern CLI Provider for shared command-line tools and development runtimes.' \
  "README preserves Modern CLI Provider boundary"
assert_contains 'Use OS package managers directly only when intentionally updating OS-managed packages.' \
  "README keeps OS package upgrades outside Terrapod"
assert_contains 'Use mise directly when intentionally updating modern CLI tools or development runtimes.' \
  "README keeps Modern CLI Provider upgrades outside Terrapod"
assert_raycast_restore_contains '`enableMacosAppGroupLauncher`' \
  "README Raycast restore procedure mentions launcher App Group"
assert_contains 'Opting out of an optional stack excludes its files from chezmoi management; it does not remove files already present on a machine.' \
  "README documents non-destructive optional stack opt-out"

assert_contains 'Minimal VPS' \
  "README includes a minimal VPS example"
assert_contains 'Editor-only machine' \
  "README includes an editor-only example"
assert_contains 'AI-only machine' \
  "README includes an AI-only example"
assert_contains 'Full development workspace machine' \
  "README includes a full development workspace example"
