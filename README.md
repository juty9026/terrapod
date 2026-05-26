# dotfiles

Personal dotfiles managed with chezmoi.

## Setup

### macOS

Install chezmoi with Homebrew on macOS.

```sh
brew install chezmoi
```

Initialize the source repo, review the diff, then apply it.

```sh
chezmoi init git@github.com:juty9026/dotfiles.git
chezmoi diff
chezmoi apply
```

On macOS, `chezmoi apply` also runs setup scripts under `.chezmoiscripts` for
the initial terminal environment:

- Homebrew bootstrap and the macOS `Brewfile` bundle
- Ghostty, cmux, Hammerspoon, and font casks
- mise
- CLI tools such as ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), and starship via mise
- btop via Homebrew, because its mise-managed release assets do not support macOS arm64
- Oh My Zsh, zinit, and SCM Breeze
- Bun, Python, uv/uvx, and Node.js via `~/.config/mise/config.toml`
- pnpm through Node.js Corepack

Machine-specific Homebrew packages should live outside the tracked `Brewfile`.

To edit an existing checkout, move to the chezmoi source directory.

```sh
chezmoi cd
```

### Ubuntu 24.04 VPS

Ubuntu support targets 24.04 LTS only. The VPS profile is read-only by
default, so no GitHub authentication is required for the initial setup. Install
chezmoi, initialize this public repo over HTTPS, review the diff, then apply it.

```sh
sudo apt update
sudo apt install -y ca-certificates curl git
sh -c "$(curl -fsLS get.chezmoi.io)" -- -b "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$PATH"

chezmoi init https://github.com/juty9026/dotfiles.git
chezmoi diff
chezmoi apply
```

The `export PATH=...` line is only for the current bootstrap shell. After the
first `chezmoi apply`, managed zsh sessions keep `~/.local/bin` on `PATH` so
user-local binaries such as `chezmoi` remain available after reconnecting.

On Ubuntu, `chezmoi apply` runs setup scripts for the VPS shell profile:

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

If the login shell could not be changed automatically, switch it after the
first apply and reconnect.

```sh
chsh -s "$(command -v zsh)"
```

## Updates

Update OS-managed packages with the OS package manager.

```sh
# macOS
brew update
brew upgrade

# Ubuntu
sudo apt update
sudo apt upgrade
```

Update modern CLI tools and development runtimes with mise.

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

## Manual restore

### Raycast

Raycast Store extensions and app state are restored manually from a
`.rayconfig` backup stored in 1Password, rather than tracked directly in this
repo.

1. Install Raycast from the `Brewfile`.
2. Open the 1Password item for the Raycast settings export.
3. Download the latest `.rayconfig` file.
4. Run `Import Settings & Data` in Raycast.
5. Enter the Raycast export passphrase from the same 1Password item.
6. Select the categories to import, usually Store extensions, settings,
   aliases, hotkeys, quicklinks, and snippets.

When Raycast changes need to be shared across workstations, export a fresh
`.rayconfig` from the primary workstation and update the 1Password item.

## Local overrides

Machine-local options are configured outside this repo with
`chezmoi edit-config`. Keep only the option names, defaults, and examples here;
do not commit workstation-specific values.

All three optional stack profiles are disabled by default.

| Key | Default | Purpose |
| --- | --- | --- |
| `enableEditorStack` | `false` | Enables the Optional Editor Stack, which manages the rich Neovim configuration. Plain Neovim remains in the Core Shell Stack either way. |
| `enableAiCliTools` | `false` | Installs Gemini CLI, Claude Code, and Codex with npm through the mise-managed Node.js runtime. |
| `enableDevelopmentWorkspace` | `false` | Enables the Optional Development Workspace preset, including the Optional Editor Stack, Optional AI Tool Stack, and development-specific Zellij workspace surfaces. |
| `gitAllowedSigners` | `[]` | Adds workstation-specific SSH signing identities to `~/.ssh/allowed_signers`. |

When `enableDevelopmentWorkspace` is `true`, it enables both the Optional Editor Stack and Optional AI Tool Stack even if `enableEditorStack` or `enableAiCliTools` are false or omitted.

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
chezmoi apply
```

## Conventions

- `dot_`: dotfiles in the home directory
- `private_`: files that need private permissions
- `executable_`: files that need the executable bit
- `.tmpl`: templates for machine-specific values or secret injection

Do not use templates for static configuration.

Do not commit secrets, tokens, or private keys.
