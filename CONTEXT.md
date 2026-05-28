# Dotfiles

This context describes the machine profiles and shared terminal experience managed by this dotfiles management tool.

## Language

**macOS Terminal Profile**:
The tracked terminal setup for personal macOS workstations.
_Avoid_: desktop profile, local profile

**VPS Shell Profile**:
The tracked terminal setup for Ubuntu VPS machines used as headless development machines.
_Avoid_: server clone, Linux desktop profile

**Supported Ubuntu Release**:
Ubuntu 24.04 LTS, the only Linux distribution release targeted by the VPS Shell Profile.
_Avoid_: generic Linux support, Ubuntu 22.04 support

**Core Shell Stack**:
The mandatory command-line tool set expected to exist in the VPS Shell Profile.
_Avoid_: optional tools, nice-to-have tools

**Development Runtime Stack**:
The mandatory language/runtime tool set expected to exist in the VPS Shell Profile.
_Avoid_: project-local runtime, optional runtime

**Optional Editor Stack**:
The opt-in rich editor configuration that is excluded from every machine profile unless explicitly enabled.
_Avoid_: core shell editor, mandatory editor config, default LazyVim setup

**Optional AI Tool Stack**:
The opt-in command-line AI tools that may be installed on selected development machines.
_Avoid_: core shell tools, mandatory AI tools

**Optional Development Workspace**:
The opt-in full development stack bundle for machines that need rich editor configuration, AI tools, and an integrated terminal workspace together.
_Avoid_: core terminal multiplexer config, default dev layout, mandatory workspace

**Bootstrap Package Manager**:
The operating-system package manager used only to prepare a machine for the shared tool stack.
_Avoid_: primary CLI provider, runtime manager

**Modern CLI Provider**:
The shared tool provider for modern command-line tools across supported machine profiles.
_Avoid_: Homebrew replacement, standalone aqua

**macOS Desktop App Stack**:
The opt-in macOS cask set for GUI apps, system-style desktop apps, and cask-delivered desktop support tools.
_Avoid_: Homebrew bootstrap, shared CLI formula, Core Shell Stack

**macOS App Group**:
A user-selectable subset of the **macOS Desktop App Stack** that keeps related desktop apps installable together.
_Avoid_: individual app toggle, Homebrew tap group, Core Shell Stack

**Dotfiles Management Tool**:
The user-facing Terrapod installer and management command layer that configures machines while using chezmoi as its internal apply engine.
_Avoid_: plain chezmoi repo, standalone package manager, OS provisioning platform

**Terrapod**:
The branded name for the **Dotfiles Management Tool**, evoking a pod that lands on an unknown machine and terraforms it into a familiar environment.
_Avoid_: dotfiles command, chezmoi wrapper

**Terrapod Source Repository**:
The GitHub repository that hosts **Terrapod** and this repository's declared dotfiles state.
_Avoid_: dotfiles repository, legacy source URL

**tpod**:
The short command alias for **Terrapod**.
_Avoid_: separate tool, primary brand name

**Preset**:
A first-run setup choice that expands into concrete optional stack and app-group settings for a machine.
_Avoid_: machine preset, permanent mode, dynamic policy

## Relationships

- The **Dotfiles Management Tool** is the user-facing entry point for first install, configuration changes, and routine dotfiles maintenance.
- **Terrapod** is the primary command and brand name for the **Dotfiles Management Tool**.
- **tpod** is a compatibility and convenience alias for **Terrapod**, not a separate interface.
- The **Terrapod Source Repository** uses `juty9026/terrapod` as its canonical GitHub slug.
- Public first-run installation references use the HTTPS **Terrapod Source Repository** URL `https://github.com/juty9026/terrapod.git`.
- Maintainer remotes may use the SSH **Terrapod Source Repository** URL `git@github.com:juty9026/terrapod.git`.
- The legacy `juty9026/dotfiles` slug is not a supported **Terrapod** installation path after repository renaming.
- **Terrapod** and its first-run installer are implemented as POSIX shell entry points.
- The first-run **Terrapod** installer delegates chezmoi binary installation to the official `get.chezmoi.io` installer and installs it under `~/.local/bin`.
- The first-run **Terrapod** installer uses `https://github.com/juty9026/terrapod.git` as the default source repository URL.
- The first-run **Terrapod** installer stops with guidance when the default chezmoi source directory already exists instead of overwriting an existing checkout.
- chezmoi remains the internal apply engine for the **Dotfiles Management Tool**, not the primary workflow users need to remember.
- The **Dotfiles Management Tool** exposes first-class maintenance commands when they add profile, preset, installer, or validation context around chezmoi behavior.
- Direct chezmoi commands remain an escape hatch for advanced maintenance, not the default documented workflow.
- A **Preset** is a starting point for concrete settings, not a permanent dynamic policy.
- A **Preset** shows a summary of the optional stack and app-group settings it will enable before installation.
- First-run setup allows users to customize the concrete settings produced by a **Preset** before they are saved.
- Changing a **Preset** in the future must not silently change machines that already saved concrete optional stack and app-group settings.
- The first **Preset** choices are minimal, development, and workstation.
- The minimal **Preset** keeps optional stacks and macOS app groups disabled.
- The development **Preset** enables the **Optional Editor Stack**, **Optional AI Tool Stack**, and **Optional Development Workspace**.
- The workstation **Preset** includes the development **Preset** settings and enables every **macOS App Group**.
- The workstation **Preset** is available only for the **macOS Terminal Profile** and is hidden for the **VPS Shell Profile**.
- First-run setup and later configuration changes use the same config-writing rules.
- Config writes update only managed **Dotfiles Management Tool** settings and preserve unrelated chezmoi config values.
- Interactive setup asks before updating an existing chezmoi config.
- Config writes use a conservative POSIX shell upsert for managed `[data]` keys instead of attempting to parse all TOML features.
- Config writes back up an existing chezmoi config before changing managed `[data]` keys.
- The **macOS Terminal Profile** and **VPS Shell Profile** are separate machine profiles in one **Terrapod Source Repository**.
- The **VPS Shell Profile** targets exactly one **Supported Ubuntu Release**.
- The **VPS Shell Profile** includes the **Core Shell Stack**.
- The **VPS Shell Profile** includes the **Development Runtime Stack**.
- The **Core Shell Stack** includes Oh My Zsh and modern CLI tools such as fd, ripgrep, zoxide, lazygit, GitHub CLI (`gh`), and plain Neovim.
- The **Development Runtime Stack** includes mise-managed Bun, Node.js, Python, and uv.
- pnpm belongs to the **Development Runtime Stack** through Node.js Corepack, not as a mise-managed tool.
- Rich Neovim configuration belongs to the **Optional Editor Stack**, not the **Core Shell Stack**, and is opt-in for every machine profile.
- Gemini CLI, Claude Code, and Codex belong to the **Optional AI Tool Stack**, not the **Core Shell Stack**.
- Enabling only the **Optional AI Tool Stack** does not imply the **Optional Editor Stack** or **Optional Development Workspace**.
- Development-specific terminal layouts belong to the **Optional Development Workspace**, not the **Core Shell Stack**.
- Enabling the **Optional Development Workspace** also enables the **Optional Editor Stack** and **Optional AI Tool Stack**.
- The **Optional Development Workspace** is a stack bundle that takes precedence over disabled optional stack flags.
- Zellij and its general launcher alias belong to the **Core Shell Stack**, while development-specific Zellij layouts and aliases belong to the **Optional Development Workspace**.
- Disabling an optional stack excludes its files from management but does not remove files already present on a machine.
- Homebrew and APT are **Bootstrap Package Managers**, not the **Modern CLI Provider**.
- mise with its aqua backend is the **Modern CLI Provider**.
- **Terrapod** applies this repository's declared dotfiles state; it does not act as the package manager for OS packages or mise-managed tool upgrades.
- **Terrapod** may install declared dependencies needed to reach the target state, but it does not run broad version upgrade commands such as `brew upgrade`, `apt upgrade`, or `mise upgrade`.
- `terrapod update` delegates repository update semantics to `chezmoi update` and adds Terrapod-specific context and validation around it.
- The **macOS Desktop App Stack** is opt-in because Homebrew casks can install shared applications and desktop support assets outside a single user's home directory.
- The **macOS Desktop App Stack** excludes Homebrew itself, shared CLI formulae such as mise and btop, and terminal font casks.
- Terminal font casks belong to the macOS Terminal Profile core bootstrap because the managed terminal configuration depends on them and they are not desktop applications.
- Enabling the **Optional Development Workspace** does not enable the **macOS Desktop App Stack**.
- **macOS App Groups** are configured during **Terrapod** setup and remain within the **macOS Desktop App Stack** boundary.
- The first **macOS App Groups** are terminal-apps, automation, launcher, and monitoring.
- The terminal-apps **macOS App Group** contains Ghostty and cmux.
- The automation **macOS App Group** contains Hammerspoon and Karabiner-Elements.
- The launcher **macOS App Group** contains Raycast and 1Password CLI.
- The monitoring **macOS App Group** contains iStat Menus.
- Individual macOS app toggles are excluded from the current **Terrapod** setup scope.
- Repository renaming makes `juty9026/terrapod` the canonical slug for the **Terrapod Source Repository** without adding legacy URL fallback behavior.
- Non-interactive setup options are deferred outside the current **Terrapod** installer and management command work.
- README and command output should use the **Terrapod** name where needed for consistency; broader branding and log-output design are deferred.

## Example Dialogue

> **Dev:** "Should the VPS just reuse the macOS terminal setup?"
> **Domain expert:** "No. The **VPS Shell Profile** should share the **Core Shell Stack** and **Development Runtime Stack**, but avoid macOS-only applications and Homebrew."

## Flagged Ambiguities

- "VPS shell experience" means the **VPS Shell Profile**, not a full desktop or GUI environment.
- "development environment" can mean runtimes, editor configuration, or AI tools; resolved here as **Optional Editor Stack** when discussing opt-in LazyVim-style editor configuration.
- "dev layout" means the **Optional Development Workspace**, not the baseline Zellij installation.
- "preset" means a **Preset** in first-run setup unless specifically referring to the **Optional Development Workspace** stack bundle.
- "dotfiles command" means **Terrapod** unless discussing legacy naming or chezmoi internals.
- "dotfiles repository" means the **Terrapod Source Repository** unless discussing the unsupported legacy `juty9026/dotfiles` slug.
