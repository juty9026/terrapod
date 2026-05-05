# dotfiles

Personal dotfiles managed with chezmoi.

## Setup

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

- Homebrew bootstrap and the common `Brewfile` bundle
- Ghostty, cmux, Hammerspoon, and font casks
- CLI tools such as ripgrep, neovim, zellij, lazygit, starship, mise, and Gemini CLI
- Oh My Zsh, zinit, and SCM Breeze
- Python 3.13 and uv/uvx via mise
- Node.js 24 and pnpm 10 via mise, plus Claude Code and Codex CLI

Machine-specific Homebrew packages should live outside the tracked `Brewfile`.

To edit an existing checkout, move to the chezmoi source directory.

```sh
chezmoi cd
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

| Key | Default | Purpose |
| --- | --- | --- |
| `enableHermesAgentPath` | `false` | Adds `~/.local/bin` to `PATH` for Hermes Agent on machines that need it. |
| `gitAllowedSigners` | `[]` | Adds workstation-specific SSH signing identities to `~/.ssh/allowed_signers`. |

Example:

```toml
[data]
enableHermesAgentPath = true

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
