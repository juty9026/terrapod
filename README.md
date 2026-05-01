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

- Homebrew bootstrap
- Ghostty and cmux
- Ghostty font dependencies
- CLI tools such as ripgrep, neovim, zellij, lazygit, starship, and mise
- Oh My Zsh, zinit, and SCM Breeze
- Node.js 24 via mise, plus Claude Code, Codex CLI, and Gemini CLI

To edit an existing checkout, move to the chezmoi source directory.

```sh
chezmoi cd
```

## Conventions

- `dot_`: dotfiles in the home directory
- `private_`: files that need private permissions
- `executable_`: files that need the executable bit
- `.tmpl`: templates for machine-specific values or secret injection

Do not use templates for static configuration.

Do not commit secrets, tokens, or private keys.
