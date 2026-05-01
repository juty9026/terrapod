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
- Node.js 24 via mise, plus Claude Code and Codex CLI

Machine-specific Homebrew packages should live outside the tracked `Brewfile`.

To edit an existing checkout, move to the chezmoi source directory.

```sh
chezmoi cd
```

## Local overrides

`~/.ssh/allowed_signers` is rendered from
`private_dot_ssh/allowed_signers.tmpl`. To trust additional SSH signing
identities on a workstation, add machine-local data with `chezmoi edit-config`.

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
