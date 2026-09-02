# Install Claude Code through its vendor installer

The **Optional AI Tool Stack** installs Claude Code through the **Claude Code
Installer**, the vendor-published script at `https://claude.ai/install.sh`,
instead of the `claude-code` Homebrew cask. `antigravity-cli` and `codex` remain
Homebrew casks. The canonical Claude Code executable is `$HOME/.local/bin/claude`.

The Homebrew cask lags vendor releases, and Claude Code releases often enough for
that lag to show in daily use.

This decision supersedes ADR 0008's package-source choice for Claude Code alone;
ADR 0008 stays in force for the other two casks, and Homebrew remains the
**Modern CLI Provider**. It also corrects ADR 0015's stated reason as it applies
to Claude Code: the stack stays scoped to the **macOS Terminal Profile**, but for
Claude Code that scope is now a profile decision rather than a consequence of
Homebrew not installing casks on Linux. ADR 0010's no-upgrade contract and ADR
0012's advisory-only migration policy remain in force.

## Considered Options

- Keep the cask and accept the release lag: rejected because the lag is the
  problem being solved.
- Move the whole stack back to vendor installers: rejected because ADR 0008's
  single declared source still holds for `antigravity-cli` and `codex`, whose
  release cadence does not create the same pressure.
- Run the vendor installer on every apply: rejected because the script always
  downloads the latest build, which would make apply an upgrade and break ADR
  0010's `brew bundle --no-upgrade` contract.
- Extend the stack to the **VPS Shell Profile** now that its installer supports
  Linux: rejected because it confuses a package-source change with a profile
  decision.

## Consequences

- `Brewfile.ai-cli-tools.tmpl` declares two casks; the **Optional AI Tool Stack**
  draws from two package sources on the **macOS Terminal Profile**.
- `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl` runs the vendor
  installer only when `$HOME/.local/bin/claude` is absent. Claude Code's own
  updater owns version freshness.
- The Homebrew bundle step and the Claude Code step run independently; either
  failure records the single `optional-ai-cli-tools` warning, naming the failed
  source, and apply continues.
- `executable-selection` gains the `claude-installer` provider. A missing install
  reports `claude-code is not installed through the Claude Code installer`.
- A machine that still has the `claude-code` cask keeps resolving `claude` to the
  cask copy, because `dot_zshenv.tmpl` evaluates Homebrew's `shellenv` after
  prepending `$HOME/.local/bin`. Terrapod reports the provenance-neutral advisory
  and removes nothing, per ADR 0012.
- `claude install` may edit shell startup files. `run_before_60` runs before
  chezmoi writes the managed `.zshenv` and `.zshrc`, so those edits are
  overwritten; nothing is lost, because the managed PATH already includes
  `$HOME/.local/bin`.
- Intentional cask upgrades use `brew upgrade --cask codex antigravity-cli`.
