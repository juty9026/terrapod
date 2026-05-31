# Terrapod

🌐 Language: **English** | [한국어](README.ko.md)

Terrapod is a small landing pod for your machines: it brings your familiar
shell, editor, runtime, and desktop habits to a fresh Mac or Ubuntu 24.04 VPS.

Under the hood, Terrapod uses chezmoi as the apply engine and keeps package-manager upgrades outside its scope.

## Quick Start

Run the Terrapod first-run installer on a supported machine.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

The installer installs `chezmoi` into `~/.local/bin` when needed, initializes
`https://github.com/juty9026/terrapod.git`, launches Terrapod Setup from the
checked-out source repository, and runs the initial declared-state apply only
after setup succeeds.

Terrapod Setup is the first-run review step. It asks you to choose a Preset,
shows the concrete Terrapod-managed machine-local settings that Preset would
write, lets you customize those settings, and asks for confirmation before it
writes them. If setup is cancelled or fails, the installer stops before the
initial apply and prints a resume command for the checked-out source
repository.

Terrapod Setup is an interactive first-run prompt. Routine Terrapod command
output remains operational and scan-friendly after bootstrap.

You do not need to install `chezmoi` manually before running this installer.

After bootstrap, use Terrapod for normal checks and source updates.

```sh
terrapod status
terrapod doctor
terrapod update
```

## What Terrapod Carries

- Machine profiles for a macOS terminal workstation and an Ubuntu 24.04 VPS.
- Presets that unfold into concrete machine-local settings.
- Optional stacks for rich editor configuration, AI CLI tools, and development workspace surfaces.
- macOS App Groups for selected desktop tools.

## Choose a Preset

A Preset is a starting point for Terrapod Setup. It proposes concrete
machine-local settings for a machine, and setup lets you review and customize
those settings before the initial apply.

| Preset | Best for | Shape |
| --- | --- | --- |
| `minimal` | Small VPSs, clean shells, and recovery installs | Core shell and runtime baseline only |
| `development` | Machines used for active coding | Optional Editor Stack, Optional AI Tool Stack, and Optional Development Workspace |
| `workstation` | Personal macOS workstations | Development setup plus every macOS App Group |

Setup writes the concrete machine-local settings after you confirm them. A
Preset is not a permanent mode, so future Preset changes do not silently reshape
an already configured machine.

The `workstation` Preset is available only for the macOS Terminal Profile.

## What Terrapod Leaves Alone

Terrapod applies this repository's declared dotfiles state. It does not own the whole operating system.

- Broad Homebrew or APT upgrades
- mise-managed tool and runtime upgrades
- Machine-local secrets
- Untracked personal overrides

Terrapod does not run broad Homebrew, APT, or mise upgrades.

## Daily Commands

Use `terrapod` as the primary management command after bootstrap.
`tpod` is the short alias for the same command.

```sh
terrapod status
terrapod doctor
terrapod diff
terrapod apply
terrapod update
tpod status
```

`terrapod update` refreshes the Terrapod Source Repository through `chezmoi update --exclude scripts`.
It does not run Homebrew, APT, or mise upgrades.

Direct chezmoi use remains an advanced escape hatch.

```sh
terrapod chezmoi -- cd
terrapod chezmoi -- status
```

## Platform Details

### macOS

Run the installer on macOS.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

On macOS, the initial apply also runs setup scripts under `.chezmoiscripts` for the initial terminal environment:

- Homebrew bootstrap and the macOS `Brewfile` bundle
- mise
- CLI tools such as ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), and starship via mise
- btop via Homebrew, because its mise-managed release assets do not support macOS arm64
- Terminal font casks
- Oh My Zsh, zinit, and SCM Breeze
- Bun, Python, uv/uvx, and Node.js via `~/.config/mise/config.toml`
- pnpm through Node.js Corepack

macOS desktop applications are split into opt-in App Groups controlled by
machine-local data keys. During Homebrew bootstrap, chezmoi renders selected
groups from `Brewfile.macos-desktop-apps.tmpl` into a temporary Brewfile and
installs that rendered bundle:

- `terminal-apps`: Ghostty.
- `automation`: Hammerspoon and Karabiner-Elements.
- `launcher`: Raycast and 1Password CLI.
- `monitoring`: iStat Menus.

Machine-specific Homebrew packages should live outside the tracked `Brewfile`.

### Ubuntu 24.04 VPS

Ubuntu support targets 24.04 LTS only. The VPS profile is read-only by
default, so no GitHub authentication is required for the initial setup. Run the
installer.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

The installer adds `~/.local/bin` to `PATH` for the bootstrap process. After
the first apply, managed zsh sessions keep `~/.local/bin` on `PATH` so
user-local binaries such as `chezmoi` remain available after reconnecting.

On Ubuntu, the initial apply runs setup scripts for the VPS shell profile:

- APT bootstrap packages: zsh, git, curl, ca-certificates, gpg, unzip, and build-essential
- Python build dependencies required by the mise-managed Python runtime
- mise from the official mise APT repository
- Oh My Zsh, zinit, and SCM Breeze
- CLI tools such as ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), and starship via mise
- Bun, Python, uv/uvx, and Node.js via mise
- pnpm through Node.js Corepack
- Login shell switch to zsh

Only configure GitHub authentication on a VPS if write access is needed later.
If the first mise install hits GitHub API rate limits while resolving aqua
tools, export a temporary `GITHUB_TOKEN` and rerun `chezmoi apply`.

If the login shell could not be changed automatically, switch it after the first apply and reconnect.

```sh
chsh -s "$(command -v zsh)"
```

Terrapod handles normal management after bootstrap. For an unusual recovery
path, install `chezmoi` manually and initialize
`https://github.com/juty9026/terrapod.git` directly, then review and apply the
result.

### Intentional Upgrades

Homebrew and APT are Bootstrap Package Managers here: they prepare a machine for the declared shell state.
mise is the Modern CLI Provider for shared command-line tools and development runtimes.

Use OS package managers directly only when intentionally updating OS-managed packages.

```sh
# macOS
brew update
brew upgrade

# Ubuntu
sudo apt update
sudo apt upgrade
```

Use mise directly when intentionally updating modern CLI tools or development runtimes.

```sh
mise outdated
mise upgrade
```

Use `--bump` only when intentionally moving beyond configured major/minor
ranges, such as changing Node.js from the current `24` line to a newer major.

```sh
mise outdated --bump
mise upgrade --bump
```

## Manual Restore

### Raycast

Raycast Store extensions and app state are restored manually from a
`.rayconfig` backup stored in 1Password, rather than tracked directly in this
repo.

1. Enable/install the launcher macOS App Group with `enableMacosAppGroupLauncher`, or otherwise ensure Raycast is installed.
2. Open the 1Password item for the Raycast settings export.
3. Download the latest `.rayconfig` file.
4. Run `Import Settings & Data` in Raycast.
5. Enter the Raycast export passphrase from the same 1Password item.
6. Select the categories to import, usually Store extensions, settings, aliases, hotkeys, quicklinks, and snippets.

When Raycast changes need to be shared across workstations, export a fresh
`.rayconfig` from the primary workstation and update the 1Password item.

## Local Overrides

Machine-local options are configured outside this repo with
`chezmoi edit-config`. Keep only the option names, defaults, and examples here;
do not commit workstation-specific values.

Optional stack profiles and macOS App Group settings are disabled by default.

| Key | Default | Purpose |
| --- | --- | --- |
| `enableEditorStack` | `false` | Enables the Optional Editor Stack, which manages the rich Neovim configuration. Plain Neovim remains in the Core Shell Stack either way. |
| `enableAiCliTools` | `false` | Installs Gemini CLI, Claude Code, and Codex with npm through the mise-managed Node.js runtime. |
| `enableDevelopmentWorkspace` | `false` | Enables the Optional Development Workspace preset, including the Optional Editor Stack, Optional AI Tool Stack, and development-specific Zellij workspace surfaces. |
| `enableMacosAppGroupTerminalApps` | `false` | Installs the terminal-apps macOS App Group: Ghostty. |
| `enableMacosAppGroupAutomation` | `false` | Installs the automation macOS App Group: Hammerspoon and Karabiner-Elements. |
| `enableMacosAppGroupLauncher` | `false` | Installs the launcher macOS App Group: Raycast and 1Password CLI. |
| `enableMacosAppGroupMonitoring` | `false` | Installs the monitoring macOS App Group: iStat Menus. |
| `gitAllowedSigners` | `[]` | Adds workstation-specific SSH signing identities to `~/.ssh/allowed_signers`. |

When `enableDevelopmentWorkspace` is `true`, it enables both the Optional Editor Stack and Optional AI Tool Stack
even if `enableEditorStack` or `enableAiCliTools` are false or omitted.

macOS Desktop App Stack installation remains separate from `enableDevelopmentWorkspace`
because desktop casks can affect shared applications outside one user's home directory.

Opting out of an optional stack excludes its files from chezmoi management; it does not remove files already present on a machine.

### Optional stack profile examples

Minimal VPS:

```toml
[data]
```

Editor-only machine:

```toml
[data]
enableEditorStack = true
```

AI-only machine:

```toml
[data]
enableAiCliTools = true
```

Full development workspace machine:

```toml
[data]
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = true
```

Git signing identities can be configured alongside any profile.

```toml
[data]
gitAllowedSigners = [
  "name@company.com ssh-ed25519 AAAA_COMPANY_PUBLIC_KEY company",
]
```

Then apply the dotfiles.

```sh
terrapod apply
```

## Repository Conventions

- `dot_`: dotfiles in the home directory
- `private_`: files that need private permissions
- `executable_`: files that need the executable bit
- `.tmpl`: templates for machine-specific values or secret injection

Do not use templates for static configuration.

Do not commit secrets, tokens, or private keys.
