# Use Terrapod as the dotfiles management tool

This repository will present Terrapod as the user-facing installer and management command for personal dotfiles while keeping chezmoi as the internal apply engine. This avoids requiring users to install or remember chezmoi directly, lets first-run setup collect Preset and macOS App Group choices before applying dotfiles, and gives routine commands such as status, diff, apply, and update room to add profile, installer, and validation context around chezmoi behavior.

## Considered Options

- Keep chezmoi as the documented entry point: rejected because the first setup flow would still require users to install chezmoi first and manually discover local configuration flags.
- Use only a first-run install script: rejected because later configuration changes and updates would still fall back to raw chezmoi commands without Terrapod-specific policy checks.
- Build Terrapod as a standalone provisioning tool: rejected because chezmoi already provides the target-state rendering, templating, and idempotent apply behavior this repository needs.

## Consequences

- `install.sh` becomes the first-run entry point, installs chezmoi under `~/.local/bin` through the official `get.chezmoi.io` installer as needed, collects interactive setup choices, and runs the initial chezmoi apply.
- `terrapod` becomes the primary post-install management command, with `tpod` as a short alias.
- Direct chezmoi commands remain available as an advanced escape hatch rather than the default workflow.
- The first-run installer uses `https://github.com/juty9026/dotfiles.git` as the default source repository URL until repository renaming is handled separately.
- The first-run installer stops with guidance if the default chezmoi source directory already exists instead of overwriting an existing checkout.
- Repository renaming, non-interactive installer options, and broader README or log-output design are deferred.
