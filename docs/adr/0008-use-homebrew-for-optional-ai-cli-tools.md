# Use Homebrew for optional AI CLI tools

The **Optional AI Tool Stack** installs Antigravity CLI, Claude Code, and Codex
from the same Homebrew casks on the macOS Terminal Profile and VPS Shell
Profile: `antigravity-cli`, `claude-code`, and `codex`. The VPS Shell Profile
bootstraps Homebrew only when the Optional AI Tool Stack is effectively enabled.

This decision supersedes ADR 0001's blanket rejection of Homebrew on Linux and
ADR 0006's vendor-installer choice only for the Optional AI Tool Stack. APT
remains Ubuntu's Bootstrap Package Manager, and mise remains the Modern CLI
Provider for other shared command-line tools and development runtimes.

ADR 0010 supersedes this decision's restriction of Linuxbrew to the Optional AI
Tool Stack: standard-prefix Homebrew is the **Modern CLI Provider** for the Core
Shell Stack on both profiles, so the stack no longer governs whether Ubuntu
bootstraps Homebrew. ADR 0015 supersedes the claim that the stack installs from
the same casks on both profiles and the consequence that Ubuntu bootstraps
Homebrew only when the stack is enabled; the stack is scoped to the **macOS
Terminal Profile**. ADR 0017 supersedes the package source for Claude Code alone,
which the **Claude Code Installer** now installs while `antigravity-cli` and
`codex` stay Homebrew casks. This decision's choice of Homebrew over vendor
installers stays in force on macOS for those two casks, as does
`Brewfile.ai-cli-tools.tmpl` as their canonical declaration.

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
- Intentional cask upgrades use `brew upgrade --cask codex antigravity-cli`.
  Claude Code is not upgraded through Homebrew: ADR 0017 moved it to the
  **Claude Code Installer** at `https://claude.ai/install.sh`, and its own
  updater owns version freshness.
