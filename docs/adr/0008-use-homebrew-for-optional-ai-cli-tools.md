# Use Homebrew for optional AI CLI tools

The **Optional AI Tool Stack** installs Antigravity CLI, Claude Code, and Codex
from the same Homebrew casks on the macOS Terminal Profile and VPS Shell
Profile: `antigravity-cli`, `claude-code`, and `codex`. The VPS Shell Profile
bootstraps Homebrew only when the Optional AI Tool Stack is effectively enabled.

This decision supersedes ADR 0001's blanket rejection of Homebrew on Linux and
ADR 0006's vendor-installer choice only for the Optional AI Tool Stack. APT
remains Ubuntu's Bootstrap Package Manager, and mise remains the Modern CLI
Provider for other shared command-line tools and development runtimes.

## Considered Options

- Keep vendor installers on both profiles: rejected because installation,
  recovery, and upgrade behavior remains split across three vendors.
- Use Homebrew only on macOS: rejected because it does not provide one declared
  package source across supported profiles.
- Replace mise with Homebrew: rejected because the immediate goal covers only
  the three AI CLI casks and does not justify migrating runtimes or other tools.

## Consequences

- `Brewfile.ai-cli-tools.tmpl` is the canonical declaration for the three casks.
- Ubuntu installs Linux Homebrew only when `enableAiCliTools` or
  `enableDevelopmentWorkspace` enables the Optional AI Tool Stack.
- `tpod apply` restores missing declared casks with
  `brew bundle --no-upgrade`; it does not upgrade them.
- Disabling the stack does not uninstall its casks or Homebrew.
- Existing vendor-installed commands are not deleted. `tpod status` and
  `tpod doctor` warn when a command outside the active Homebrew prefix shadows
  a managed cask.
- Intentional upgrades use
  `brew upgrade --cask claude-code codex antigravity-cli`.
