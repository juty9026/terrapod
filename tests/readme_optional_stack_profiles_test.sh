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
assert_key_row_contains '`enableAiCliTools`' 'Antigravity CLI, Claude Code, and Codex' \
  "README documents the new Optional AI Tool Stack membership"
assert_key_row_contains '`enableAiCliTools`' 'Homebrew casks `antigravity-cli`, `claude-code`, and `codex`' \
  "README documents Homebrew-owned Optional AI Tool Stack"
assert_contains '| `enableDevelopmentWorkspace` | `false` |' \
  "README documents enableDevelopmentWorkspace default"
assert_contains '| `profile` |' \
  "README documents profile as a managed setup config key"
assert_not_contains 'enableMacosDesktopApps' \
  "README does not document legacy enableMacosDesktopApps option"

for key in \
  enableMacosAppGroupTerminalApps \
  enableMacosAppGroupAutomation \
  enableMacosAppGroupLauncher \
  enableMacosAppGroupMonitoring \
  enableMacosAppGroupDevelopmentApps
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
assert_key_row_contains '`enableMacosAppGroupAutomation`' 'Scroll Reverser' \
  "README documents Scroll Reverser on the automation option row"
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
assert_key_row_contains '`enableMacosAppGroupDevelopmentApps`' 'development-apps' \
  "README documents development-apps group on its option row"
assert_key_row_contains '`enableMacosAppGroupDevelopmentApps`' 'Zed and Orca ADE' \
  "README documents Zed and Orca ADE on the development-apps option row"
assert_key_row_contains '`enableMacosAppGroupDevelopmentApps`' 'stablyai/orca/orca' \
  "README documents Orca's fully-qualified cask source"
assert_contains 'When installing Orca, Terrapod trusts only the fully-qualified `stablyai/orca/orca` cask, not the entire `stablyai/orca` tap.' \
  "README documents Orca's cask-specific trust boundary"

assert_contains 'Optional stack profiles and macOS App Group settings are disabled by default.' \
  "README states optional stack profiles and App Groups are disabled by default"
assert_contains 'complete managed setup config' \
  "README explains local overrides must keep a complete managed setup config"
assert_contains 'not standalone config files' \
  "README marks optional stack examples as fragments instead of standalone configs"
assert_not_contains 'false or omitted' \
  "README no longer suggests omitted managed setup keys are valid routine-command config"
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
assert_contains 'Homebrew is the Modern CLI Provider for the Core Shell Stack on both supported profiles.' \
  "README names Homebrew as the cross-profile Modern CLI Provider"
assert_contains 'mise is the Development Runtime Manager for Bun, Node.js, Python, and uv.' \
  "README limits mise to development runtimes"
assert_contains 'On Apple Silicon, Homebrew installs at `/opt/homebrew`; on Intel Macs, it installs at `/usr/local`.' \
  "README documents macOS architecture-to-prefix mapping"
assert_contains 'Ubuntu 24.04 installs Homebrew at `/home/linuxbrew/.linuxbrew` for every Preset.' \
  "README documents mandatory Linuxbrew"
assert_contains 'The first-run installer installs `chezmoi` and `gum` through Homebrew before Terrapod Setup.' \
  "README documents cross-profile Setup bootstrap"
assert_contains '1 vCPU, 1 GiB RAM, and at least 3 GiB of free disk space before installation' \
  "README documents the recommended VPS floor"
assert_contains '`x86_64` and `aarch64`' \
  "README documents supported Ubuntu architectures"
assert_not_contains 'get.chezmoi.io' \
  "README removes the standalone chezmoi installer"
assert_not_contains 'Charm APT' \
  "README removes the Charm APT trust boundary"
assert_not_contains 'mise from the official mise APT repository' \
  "README removes mise APT ownership"
assert_contains '`~/.config/terrapod/config.json`' \
  "README documents the independent Terrapod config"
assert_contains 'declared-root ownership' \
  "README documents the ownership boundary"
assert_not_contains '| `gitAllowedSigners` |' \
  "README excludes unrelated chezmoi data from the Terrapod config table"
assert_not_contains '"gitAllowedSigners"' \
  "README excludes unrelated chezmoi data from Terrapod JSON examples"
assert_contains '`gitAllowedSigners` is not an independent Terrapod config field.' \
  "README documents the unrelated chezmoi root-data boundary"
assert_contains 'The authoring workflow must use chezmoi directly to render or apply it;' \
  "README documents how unrelated chezmoi root data is applied"
assert_not_contains 'Then reconcile the environment.' \
  "README does not imply tpod applies unrelated chezmoi root data"
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
assert_not_contains 'Terminal font casks' \
  "README no longer documents terminal font casks"
assert_contains 'Jetendard terminal font from the latest stable GitHub release' \
  "README documents the Jetendard release source"
assert_contains 'Terrapod checks the latest Jetendard release only when its managed font installer source changes or a failed install is retried.' \
  "README documents the Jetendard release-check trigger"
assert_contains 'Terrapod installs every TTF in that Jetendard release and verifies the asset digest published by GitHub.' \
  "README directly documents every-TTF installation and digest verification"
assert_contains 'It sets only the font-family keys used by Ghostty, Zed buffers and terminals, and Orca terminals.' \
  "README directly documents the app-key-only settings scope"
assert_contains 'Restart Ghostty, Zed, or Orca if an existing window still uses a cached font.' \
  "README directly documents cached-font restart guidance"
assert_contains 'Quit Orca before rerunning `tpod apply` when Jetendard settings are deferred.' \
  "README documents Orca font-setting recovery"
assert_contains 'separate from `enableDevelopmentWorkspace`' \
  "README documents macOS Desktop App Stack separation from enableDevelopmentWorkspace"
assert_contains 'casks can affect shared applications' \
  "README documents why macOS Desktop App Stack remains separate"
assert_contains '`tpod update` fetches the latest stable signed Terrapod release' \
  "README documents the signed stable Terrapod update"
assert_contains "It does not upgrade or remove packages outside Terrapod's ownership state." \
  "README limits update reconciliation to Terrapod-owned packages"
assert_contains 'Terrapod does not run broad Homebrew, APT, or mise upgrades.' \
  "README states Terrapod does not run broad package or tool upgrades"
assert_contains '`tpod plan`' \
  "README documents plan"
assert_contains '`tpod apply`' \
  "README documents apply"
assert_contains '`tpod update`' \
  "README documents update"
assert_contains '`tpod resolve <resource-id>`' \
  "README documents conflict resolution"
assert_contains 'automatically prunes Terrapod-owned resources' \
  "README documents automatic owned-resource pruning"
assert_contains 'never uses `brew uninstall --zap`' \
  "README documents the Homebrew uninstall boundary"
assert_contains '`ready`' \
  "README documents ready state"
assert_contains '`unavailable`' \
  "README documents unavailable state"
assert_contains 'read-only chezmoi escape hatch' \
  "README documents constrained direct chezmoi access"
assert_contains '`install.sh --migrate`' \
  "README documents the maintainer migration"
assert_contains 'authoring checkout is separate from the active signed release' \
  "README documents authoring and active release separation"
assert_contains '`install.sh --repair`' \
  "README documents repair"
assert_contains '`macos-terminal` and `vps-shell`' \
  "README documents supported profiles"
assert_raycast_restore_contains '`enableMacosAppGroupLauncher`' \
  "README Raycast restore procedure mentions launcher App Group"
assert_contains '`enableMacosAppGroupAiApps` is deprecated and is not treated as an alias' \
  "README documents explicit development-apps key migration"
assert_not_contains 'keeps package-manager upgrades outside its scope' \
  "README removes the bootstrap-only package boundary"
assert_not_contains 'does not uninstall' \
  "README removes non-destructive ownership language"
assert_not_contains 'Opting out of an optional stack excludes its files from chezmoi management; it does not remove files already present on a machine.' \
  "README removes non-destructive optional stack opt-out"

assert_contains 'Minimal VPS' \
  "README includes a minimal VPS example"
assert_contains 'Editor-only machine' \
  "README includes an editor-only example"
assert_contains 'AI-only machine' \
  "README includes an AI-only example"
assert_contains 'Full development workspace machine' \
  "README includes a full development workspace example"
