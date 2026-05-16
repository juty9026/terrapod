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

**Optional AI Tool Stack**:
The opt-in command-line AI tools that may be installed on selected development machines.
_Avoid_: core shell tools, mandatory AI tools

**Bootstrap Package Manager**:
The operating-system package manager used only to prepare a machine for the shared tool stack.
_Avoid_: primary CLI provider, runtime manager

**Modern CLI Provider**:
The shared tool provider for modern command-line tools across supported machine profiles.
_Avoid_: Homebrew replacement, standalone aqua

## Relationships

- The **macOS Terminal Profile** and **VPS Shell Profile** are separate machine profiles in one dotfiles repository.
- The **VPS Shell Profile** targets exactly one **Supported Ubuntu Release**.
- The **VPS Shell Profile** includes the **Core Shell Stack**.
- The **VPS Shell Profile** includes the **Development Runtime Stack**.
- The **Core Shell Stack** includes Oh My Zsh and modern CLI tools such as fd, ripgrep, zoxide, lazygit, and Neovim.
- The **Development Runtime Stack** includes mise-managed Bun, Node.js, Python, and uv.
- pnpm belongs to the **Development Runtime Stack** through Node.js Corepack, not as a mise-managed tool.
- Gemini CLI, Claude Code, and Codex belong to the **Optional AI Tool Stack**, not the **Core Shell Stack**.
- Homebrew and APT are **Bootstrap Package Managers**, not the **Modern CLI Provider**.
- mise with its aqua backend is the **Modern CLI Provider**.

## Example Dialogue

> **Dev:** "Should the VPS just reuse the macOS terminal setup?"
> **Domain expert:** "No. The **VPS Shell Profile** should share the **Core Shell Stack** and **Development Runtime Stack**, but avoid macOS-only applications and Homebrew."

## Flagged Ambiguities

- "VPS shell experience" means the **VPS Shell Profile**, not a full desktop or GUI environment.
