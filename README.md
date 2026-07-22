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

The first-run installer installs standard-prefix Homebrew, then installs
`chezmoi` and `gum` through Homebrew before Terrapod Setup. It initializes
`https://github.com/juty9026/terrapod.git`, launches Setup from the checked-out
source repository, and runs the initial declared-state apply only after setup
succeeds. After the initial apply completes, the installer prints
`tpod help` so the short day-to-day command is immediately visible.

Terrapod Setup is the first-run review step. It asks you to choose a Preset,
shows the concrete Terrapod-managed machine-local settings that Preset would
write, lets you customize those settings, and asks for confirmation before it
writes them. If setup is cancelled or fails, the installer stops before the
initial apply and prints a resume command for the checked-out source
repository.

Terrapod Setup is an interactive first-run prompt. Routine Terrapod command
output remains operational and scan-friendly after bootstrap.

Terrapod Setup requires `gum` (the Bootstrap UI Dependency) and an interactive
terminal supported by gum. Missing `gum`, failed gum bootstrap, non-TTY
sessions, `dumb` terminals, and unsupported interactive terminals stop setup
before apply with guidance text. There is no plain text fallback.

You do not need to install `chezmoi` manually before running this installer.

After bootstrap, use `tpod` for normal checks and source updates.

```sh
tpod status
tpod doctor
tpod update
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

`terrapod configure <Preset>` is the script-friendly Preset configuration
command. It writes concrete settings for exactly one supported Preset, does not
require `gum`, and has no interactive customization. `terrapod configure
<Preset>` is not a plain fallback for Terrapod Setup. Terrapod Setup and
`terrapod configure <Preset>` are intentionally separate. The latter writes
settings without the setup UI. If Terrapod Setup cannot run because `gum` or an
interactive terminal is unavailable, fix the `gum` or
terminal environment and rerun `terrapod setup`.

## What Terrapod Leaves Alone

Terrapod applies this repository's declared dotfiles state. It does not own the whole operating system.

- Broad Homebrew or APT upgrades
- mise-managed tool and runtime upgrades
- Machine-local secrets
- Untracked personal overrides

Terrapod does not run broad Homebrew, APT, or mise upgrades.

## Daily Commands

Use `tpod` as the day-to-day management command after bootstrap.
`terrapod` remains the full command and brand name.

```sh
tpod status
tpod doctor
tpod diff
tpod apply
tpod update
terrapod status
```

`terrapod update` refreshes the Terrapod Source Repository through `chezmoi update --exclude scripts`.
It does not run Homebrew, APT, or mise upgrades.

Direct chezmoi use remains an advanced escape hatch.

```sh
terrapod chezmoi -- cd
terrapod chezmoi -- status
```

`tpod status` is a human-readable snapshot. It reports missing or shadowed
mandatory Homebrew commands but still exits successfully. `tpod doctor` is the
readiness gate: it exits non-zero when a mandatory command is missing, resolves
outside the standard Homebrew prefix, or another enabled requirement or install
warning remains unresolved.

## Platform Details

Homebrew is the Modern CLI Provider for the Core Shell Stack on both supported profiles.
mise is the Development Runtime Manager for Bun, Node.js, Python, and uv.
The first-run installer installs `chezmoi` and `gum` through Homebrew before Terrapod Setup.
The shared `Brewfile` declares the 20 mandatory CLI formulae for both profiles.

### macOS

Run the installer on macOS.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

On macOS, the initial apply also runs setup scripts under `.chezmoiscripts` for the initial terminal environment:

On Apple Silicon, Homebrew installs at `/opt/homebrew`; on Intel Macs, it installs at `/usr/local`.

- Standard-prefix Homebrew and the shared 20-formula `Brewfile` bundle
- Core Shell Stack CLIs such as ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), starship, and mise through Homebrew
- Terminal font casks
- Oh My Zsh, zinit, and SCM Breeze
- Bun, Node.js 24, Python 3.13, and uv/uvx through mise
- pnpm through Node.js Corepack
- Optional AI Tool Stack casks through Homebrew when that stack is enabled

macOS desktop applications are split into opt-in App Groups controlled by
machine-local data keys. During Homebrew bootstrap, chezmoi renders selected
groups from `Brewfile.macos-desktop-apps.tmpl` into a temporary Brewfile and
installs that rendered bundle:

- `terminal-apps`: Ghostty.
- `automation`: Hammerspoon, Karabiner-Elements, and Scroll Reverser.
- `launcher`: Raycast and 1Password CLI.
- `monitoring`: iStat Menus.
- `development-apps`: Zed and Orca ADE (`stablyai/orca/orca`).

When installing Orca, Terrapod trusts only the fully-qualified `stablyai/orca/orca` cask, not the entire `stablyai/orca` tap.

Machine-specific Homebrew packages should live outside the tracked `Brewfile`.

### Ubuntu 24.04 VPS

Ubuntu support targets 24.04 LTS only on `x86_64` and `aarch64`. Use one
non-root management user with initial sudo access; Terrapod uses the standard
Homebrew prefix and does not manage a shared multi-user Linuxbrew installation.
We recommend 1 vCPU, 1 GiB RAM, and at least 3 GiB of free disk space before installation;
2 GiB RAM is comfortable. This is not an installer hard gate: below 3 GiB the
installer warns and continues. The VPS profile is read-only by default, so no
GitHub authentication is required for the initial setup. Run the installer.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

Ubuntu 24.04 installs Homebrew at `/home/linuxbrew/.linuxbrew` for every Preset.
Before Terrapod Setup, APT installs only Ubuntu system and Homebrew bootstrap
prerequisites; Terrapod adds no third-party APT repository. Homebrew then
installs `chezmoi` and `gum`. The installer adds Homebrew and `~/.local/bin` to
`PATH` for bootstrap, and managed zsh sessions restore those paths after reconnecting.

On Ubuntu, the initial apply runs setup scripts for the VPS shell profile:

- APT system and Homebrew bootstrap prerequisites only
- Python build dependencies required by the mise-managed Python runtime
- Standard-prefix Homebrew and the shared 20-formula `Brewfile` bundle
- Oh My Zsh, zinit, and SCM Breeze
- Core Shell Stack CLIs such as ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), starship, and mise through Homebrew
- Bun, Node.js 24, Python 3.13, and uv/uvx through mise
- pnpm through Node.js Corepack
- Login shell switch to zsh
- Optional AI Tool Stack casks through Homebrew when that stack is enabled

The VPS Shell Profile is headless: macOS App Groups and other GUI applications
remain in the optional macOS Desktop App Stack and are never installed on Ubuntu.
Only configure GitHub authentication on a VPS if write access is needed later.

If the login shell could not be changed automatically, switch it after the first apply and reconnect.

```sh
chsh -s "$(command -v zsh)"
```

Terrapod handles normal management after bootstrap. For an unusual recovery
path, install `chezmoi` manually and initialize
`https://github.com/juty9026/terrapod.git` directly, then review and apply the
result.

### Intentional Upgrades

Homebrew owns shared user-facing CLI tools and the enabled Optional AI Tool
Stack on both profiles. APT owns only Ubuntu system and bootstrap prerequisites.
mise owns only the Development Runtime Stack.

`tpod apply` restores missing Homebrew packages with
`HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade`; it never performs an
automatic update, upgrade, or removal. Existing mise, APT, and vendor-installed payloads are not removed automatically.

Use OS package managers directly only when intentionally updating OS-managed packages.

```sh
# macOS
brew update
brew upgrade

# Ubuntu
sudo apt update
sudo apt upgrade
```

Intentional CLI upgrades are explicit Homebrew operations. Upgrade all
Homebrew-managed CLIs with `brew update` and `brew upgrade`, or target only the
AI CLI casks when that is the intended scope.

```sh
brew update
brew upgrade --cask claude-code codex antigravity-cli
```

Use mise directly when intentionally updating development runtimes.

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

Run `tpod setup` or `terrapod configure <Preset>` first so Terrapod writes a
complete managed setup config. Routine commands treat the setup config as
complete only when `profile` and every managed optional stack and macOS App
Group key are present, including disabled keys. When editing manually, change
values in the existing `[data]` section instead of replacing it with a partial
snippet.

Optional stack profiles and macOS App Group settings are disabled by default.

| Key | Default | Purpose |
| --- | --- | --- |
| `profile` | Detected by setup/configure | Records the active Terrapod machine profile. |
| `enableEditorStack` | `false` | Enables the Optional Editor Stack, which manages the rich Neovim configuration. Plain Neovim remains in the Core Shell Stack either way. |
| `enableAiCliTools` | `false` | Installs Antigravity CLI, Claude Code, and Codex through Homebrew casks `antigravity-cli`, `claude-code`, and `codex`. |
| `enableDevelopmentWorkspace` | `false` | Enables the Optional Development Workspace preset, including the Optional Editor Stack, Optional AI Tool Stack, and development-specific Zellij workspace surfaces. |
| `enableMacosAppGroupTerminalApps` | `false` | Installs the terminal-apps macOS App Group: Ghostty. |
| `enableMacosAppGroupAutomation` | `false` | Installs the automation macOS App Group: Hammerspoon, Karabiner-Elements, and Scroll Reverser. |
| `enableMacosAppGroupLauncher` | `false` | Installs the launcher macOS App Group: Raycast and 1Password CLI. |
| `enableMacosAppGroupMonitoring` | `false` | Installs the monitoring macOS App Group: iStat Menus. |
| `enableMacosAppGroupDevelopmentApps` | `false` | Installs the development-apps macOS App Group: Zed and Orca ADE (`stablyai/orca/orca`). |
| `gitAllowedSigners` | `[]` | Adds workstation-specific SSH signing identities to `~/.ssh/allowed_signers`. |

When `enableDevelopmentWorkspace` is `true`, it enables both the Optional Editor Stack and Optional AI Tool Stack
even when `enableEditorStack` or `enableAiCliTools` are recorded as false.

macOS Desktop App Stack installation remains separate from `enableDevelopmentWorkspace`
because desktop casks can affect shared applications outside one user's home directory.

Opting out of an optional stack excludes its files from chezmoi management; it does not remove files already present on a machine.

Terrapod preserves existing mise-, APT-, and vendor-installed payloads. When a
legacy command shadows a mandatory Homebrew command, `tpod status` reports the
ownership warning and `tpod doctor` fails with manual cleanup guidance. Legacy
AI CLI shadowing remains advisory. Terrapod does not remove legacy vendor-installed AI CLI binaries.

`enableMacosAppGroupAiApps` is deprecated and is not treated as an alias for `enableMacosAppGroupDevelopmentApps`. Run `tpod setup` or `terrapod configure <Preset>` to migrate explicitly; Terrapod does not install Zed based on the old selection.

### Zellij shortcuts

Terrapod-managed `.zshrc` exposes these Zellij helpers:

- `zja [session]`: attaches to a Zellij session. When `session` is omitted, it defaults to the current directory name.
- `zdac [session]`: attaches to or creates a dev-layout Zellij session. It is available when `enableDevelopmentWorkspace` is true, and defaults to the current directory name when `session` is omitted.

### Optional stack profile examples

The examples below show values to keep or change inside an existing complete
`[data]` section. They are not standalone config files.

Minimal VPS:

```toml
profile = "vps-shell"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
```

Editor-only machine:

```toml
enableEditorStack = true
```

AI-only machine:

```toml
enableAiCliTools = true
```

Full development workspace machine:

```toml
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = true
```

Git signing identities can be configured alongside any profile.

```toml
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
