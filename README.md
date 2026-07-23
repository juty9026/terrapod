# Terrapod

🌐 Language: **English** | [한국어](README.ko.md)

Terrapod is a small landing pod for your machines: a personal development
environment manager that brings your familiar shell, editor, runtime, and
desktop habits to a fresh Mac or Ubuntu 24.04 VPS.

Under the hood, Terrapod uses chezmoi as the apply engine for managed files and
typed adapters for packages, runtimes, fonts, and Git checkouts. Its
declared-root ownership boundary means it manages only resources declared by a
verified Terrapod catalog.

## Quick Start

Run the Terrapod first-run installer on a supported machine.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

The first-run installer installs standard-prefix Homebrew, then installs
`chezmoi` and `gum` through Homebrew before Terrapod Setup. It installs the
latest stable signed release, launches Setup from that release, and runs the
initial declared-state apply only after setup succeeds. The authoring checkout is separate from the active signed release, so editing a checkout cannot change
the active manager until a signed release is installed. After the initial apply completes, the installer prints
`tpod help` so the short day-to-day command is immediately visible.
Maintainers may clone the authoring checkout from
`https://github.com/juty9026/terrapod.git`; that checkout is never the active
runtime source.

Existing maintainer machines from the pre-manager implementation use the
one-shot `install.sh --migrate` path. It converts legacy chezmoi data into
`~/.config/terrapod/config.json`, imports eligible existing resources into
Terrapod ownership, reconciles them, and removes the legacy source only after
verification. New installations and routine updates never run this migration.

Terrapod Setup is the first-run review step. It asks you to choose a Preset,
shows the concrete Terrapod-managed machine-local settings that Preset would
write, lets you customize those settings, and asks for confirmation before it
writes them. If setup is cancelled or fails, the installer stops before the
initial apply and prints the exact command that resumes the signed installer.

Terrapod Setup is an interactive first-run prompt. Routine Terrapod command
output remains operational and scan-friendly after bootstrap.

Terrapod Setup requires `gum` (the Bootstrap UI Dependency) and an interactive
terminal supported by gum. Missing `gum`, failed gum bootstrap, non-TTY
sessions, `dumb` terminals, and unsupported interactive terminals stop setup
before apply with guidance text. There is no plain text fallback.

You do not need to install `chezmoi` manually before running this installer.

After bootstrap, use `tpod` for routine management and signed updates.

```sh
tpod plan
tpod apply
tpod status
tpod doctor
tpod update
```

## What Terrapod Carries

- Machine profiles for a macOS terminal workstation and an Ubuntu 24.04 VPS.
- Stable resource IDs and ownership receipts for every Terrapod-managed resource.
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

Terrapod owns only resources under a declared root in the verified catalog. It
does not inspect, upgrade, or remove undeclared packages. If a declared package
already exists, Terrapod reports that it will take ownership and then either
imports it completely or leaves the resource unavailable; there is no partially
managed state.

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
tpod plan
tpod apply
tpod update
tpod resolve <resource-id>
terrapod status
```

`tpod plan` inspects actual state without mutation. `tpod apply` executes that
plan, upgrades only Terrapod-owned packages when the catalog requires it, and
automatically prunes Terrapod-owned resources that are no longer desired.
Package removal is provider-specific and never uses `brew uninstall --zap`, so
Homebrew cask support files remain outside Terrapod's ownership boundary.

`tpod update` fetches the latest stable signed Terrapod release, verifies its
manifest and every release asset, prints the complete plan, and then atomically
activates the new Management Core before reconciling Terrapod-owned resources.
It does not upgrade or remove packages outside Terrapod's ownership state.

Each resource is either `ready` or `unavailable`. An unavailable resource and
its dependents are not mutated. For a managed-file conflict, inspect the plan
and run `tpod resolve <resource-id>` to choose the desired state explicitly,
then rerun `tpod plan` or `tpod apply`.

Stable GitHub Releases contain four static `tpod` binaries for macOS and Linux
on `amd64` and `arm64`, the immutable source archive, the signed resource
catalog, `release.json`, `release.json.sig`, and a versioned `install.sh`.
Release manifests are signed with Ed25519; private signing keys are never
included in source archives or release assets.

If the stable launcher reports that the active Management Core is missing or
broken, run the exact versioned `install.sh --repair` command printed by the
launcher. Repair verifies the same signed release inputs and restores only the
Management Core; it does not apply resources or rewrite machine configuration.

Direct access is a read-only chezmoi escape hatch. Terrapod fixes the active
signed source, independent config, destination, and script exclusion; mutating
chezmoi subcommands are rejected.

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

The supported profiles are `macos-terminal` and `vps-shell`; the latter targets
Ubuntu 24.04 LTS on `x86_64` and `aarch64`.

Homebrew is the Modern CLI Provider for the Core Shell Stack on both supported profiles.
mise is the Development Runtime Manager for Bun, Node.js, Python, and uv.
The first-run installer installs `chezmoi` and `gum` through Homebrew before Terrapod Setup.
The signed resource catalog declares the 20 mandatory CLI formulae for both profiles.

### macOS

Run the installer on macOS.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

On macOS, typed adapters reconcile the initial terminal environment:

On Apple Silicon, Homebrew installs at `/opt/homebrew`; on Intel Macs, it installs at `/usr/local`.

- Standard-prefix Homebrew and the catalog-declared 20-formula CLI set
- Core Shell Stack CLIs such as ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), starship, and mise through Homebrew
- Jetendard terminal font from the latest stable GitHub release
- Oh My Zsh, zinit, and SCM Breeze
- Bun, Node.js 24, Python 3.13, and uv/uvx through mise
- pnpm through Node.js Corepack
- Optional AI Tool Stack casks through Homebrew when that stack is enabled

Terrapod installs every TTF in that Jetendard release and verifies the asset digest published by GitHub. Terrapod checks the latest Jetendard release only when its managed font installer source changes or a failed install is retried. It sets only the font-family keys used by Ghostty, Zed buffers and terminals, and Orca terminals. Quit Orca before rerunning `tpod apply` when Jetendard settings are deferred. Restart Ghostty, Zed, or Orca if an existing window still uses a cached font.

macOS desktop applications are split into opt-in App Groups controlled by
independent Terrapod config. Typed Homebrew adapters reconcile the selected
catalog resources:

- `terminal-apps`: Ghostty.
- `automation`: Hammerspoon, Karabiner-Elements, and Scroll Reverser.
- `launcher`: Raycast and 1Password CLI.
- `monitoring`: iStat Menus.
- `development-apps`: Zed and Orca ADE (`stablyai/orca/orca`).

When installing Orca, Terrapod trusts only the fully-qualified `stablyai/orca/orca` cask, not the entire `stablyai/orca` tap.

Machine-specific Homebrew packages remain outside Terrapod unless the signed
catalog declares them.

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

On Ubuntu, typed adapters reconcile the VPS shell profile:

- APT system and Homebrew bootstrap prerequisites only
- Python build dependencies required by the mise-managed Python runtime
- Standard-prefix Homebrew and the catalog-declared 20-formula CLI set
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

Terrapod handles normal management after bootstrap. If the Management Core is
unavailable, use the exact signed `install.sh --repair` command printed by the
launcher.

### Intentional Upgrades

`tpod apply` reconciles the active signed catalog. It installs missing declared
packages, upgrades only Terrapod-owned packages, and automatically prunes
Terrapod-owned resources removed from desired state. Terrapod does not run broad
Homebrew, APT, or mise upgrades and never changes packages that are outside its
ownership state.

When a desired package already exists under another installation source, the
typed adapter plans a complete ownership transfer. If safe transfer cannot be
verified, that resource becomes `unavailable` and Terrapod leaves it unchanged.
Resolve managed-file conflicts with `tpod resolve <resource-id>`; package-source
conflicts remain unavailable until their reported external cause is corrected.

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

Machine-local options live in Terrapod's independent config at
`~/.config/terrapod/config.json`, not in chezmoi data. Use `tpod setup` or
`terrapod configure <Preset>` to write it; do not commit workstation-specific
values.

Run `tpod setup` or `terrapod configure <Preset>` first so Terrapod writes a
complete managed setup config. Routine commands validate it against the schema
in the active signed catalog and add defaults for new optional fields. Unknown
legacy fields are pruned during the versioned config migration.

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

Opting out of an optional stack removes its Terrapod-owned resources on the next
apply. Files and packages that Terrapod never owned remain untouched.

When a declared resource is already installed, the plan announces ownership
transfer. Apply creates the required recovery material and records ownership
only after verification; otherwise the resource is `unavailable`.

`enableMacosAppGroupAiApps` is deprecated and is not treated as an alias for `enableMacosAppGroupDevelopmentApps`. Run `tpod setup` or `terrapod configure <Preset>` to migrate explicitly; Terrapod does not install Zed based on the old selection.

### Zellij shortcuts

Terrapod-managed `.zshrc` exposes these Zellij helpers:

- `zja [session]`: attaches to a Zellij session. When `session` is omitted, it defaults to the current directory name.
- `zdac [session]`: attaches to or creates a dev-layout Zellij session. It is available when `enableDevelopmentWorkspace` is true, and defaults to the current directory name when `session` is omitted.

### Optional stack profile examples

The examples below show values to keep or change inside the `terrapod` object of
an existing complete managed setup config. They are not standalone config files.

Minimal VPS:

```json
{
  "profile": "vps-shell",
  "enableEditorStack": false,
  "enableAiCliTools": false,
  "enableDevelopmentWorkspace": false,
  "enableMacosAppGroupTerminalApps": false,
  "enableMacosAppGroupAutomation": false,
  "enableMacosAppGroupLauncher": false,
  "enableMacosAppGroupMonitoring": false,
  "enableMacosAppGroupDevelopmentApps": false
}
```

Editor-only machine:

```json
{
  "enableEditorStack": true
}
```

AI-only machine:

```json
{
  "enableAiCliTools": true
}
```

Full development workspace machine:

```json
{
  "enableEditorStack": false,
  "enableAiCliTools": false,
  "enableDevelopmentWorkspace": true
}
```

Git signing identities can be configured alongside any profile.

```json
{
  "gitAllowedSigners": [
    "name@company.com ssh-ed25519 AAAA_COMPANY_PUBLIC_KEY company"
  ]
}
```

Then reconcile the environment.

```sh
tpod apply
```

## Repository Conventions

- `dot_`: dotfiles in the home directory
- `private_`: files that need private permissions
- `executable_`: files that need the executable bit
- `.tmpl`: templates for machine-specific values or secret injection

Do not use templates for static configuration.

Do not commit secrets, tokens, or private keys.
