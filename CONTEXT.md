# Dotfiles

This context describes the machine profiles and shared terminal experience managed by this chezmoi repository.

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
The opt-in full development preset for machines that need rich editor configuration, AI tools, and an integrated terminal workspace together.
_Avoid_: core terminal multiplexer config, default dev layout, mandatory workspace

**Bootstrap Package Manager**:
The operating-system package manager used only to prepare a machine for the shared tool stack.
_Avoid_: primary CLI provider, runtime manager

**Modern CLI Provider**:
The shared tool provider for modern command-line tools across supported machine profiles.
_Avoid_: Homebrew replacement, standalone aqua

**macOS Desktop App Stack**:
The opt-in macOS cask set for GUI apps, system-style desktop apps, fonts, and cask-delivered desktop support tools.
_Avoid_: Homebrew bootstrap, shared CLI formula, Core Shell Stack

## Relationships

- The **macOS Terminal Profile** and **VPS Shell Profile** are separate machine profiles in one dotfiles repository.
- The **VPS Shell Profile** targets exactly one **Supported Ubuntu Release**.
- The **VPS Shell Profile** includes the **Core Shell Stack**.
- The **VPS Shell Profile** includes the **Development Runtime Stack**.
- The **Core Shell Stack** includes Oh My Zsh and modern CLI tools such as fd, ripgrep, zoxide, lazygit, and plain Neovim.
- The **Development Runtime Stack** includes mise-managed Bun, Node.js, Python, and uv.
- pnpm belongs to the **Development Runtime Stack** through Node.js Corepack, not as a mise-managed tool.
- Rich Neovim configuration belongs to the **Optional Editor Stack**, not the **Core Shell Stack**, and is opt-in for every machine profile.
- Gemini CLI, Claude Code, and Codex belong to the **Optional AI Tool Stack**, not the **Core Shell Stack**.
- Enabling only the **Optional AI Tool Stack** does not imply the **Optional Editor Stack** or **Optional Development Workspace**.
- Development-specific terminal layouts belong to the **Optional Development Workspace**, not the **Core Shell Stack**.
- Enabling the **Optional Development Workspace** also enables the **Optional Editor Stack** and **Optional AI Tool Stack**.
- The **Optional Development Workspace** is a preset that takes precedence over disabled optional stack flags.
- Zellij and its general launcher alias belong to the **Core Shell Stack**, while development-specific Zellij layouts and aliases belong to the **Optional Development Workspace**.
- Disabling an optional stack excludes its files from management but does not remove files already present on a machine.
- Homebrew and APT are **Bootstrap Package Managers**, not the **Modern CLI Provider**.
- mise with its aqua backend is the **Modern CLI Provider**.
- The **macOS Desktop App Stack** is opt-in because Homebrew casks can install shared applications and desktop support assets outside a single user's home directory.
- The **macOS Desktop App Stack** excludes Homebrew itself and shared CLI formulae such as mise and btop.
- Enabling the **Optional Development Workspace** does not enable the **macOS Desktop App Stack**.

## Example Dialogue

> **Dev:** "Should the VPS just reuse the macOS terminal setup?"
> **Domain expert:** "No. The **VPS Shell Profile** should share the **Core Shell Stack** and **Development Runtime Stack**, but avoid macOS-only applications and Homebrew."

## Flagged Ambiguities

- "VPS shell experience" means the **VPS Shell Profile**, not a full desktop or GUI environment.
- "development environment" can mean runtimes, editor configuration, or AI tools; resolved here as **Optional Editor Stack** when discussing opt-in LazyVim-style editor configuration.
- "dev layout" means the **Optional Development Workspace**, not the baseline Zellij installation.
