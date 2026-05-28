# Install gum as the required Bootstrap UI Dependency

Terrapod Setup needs a reliable interactive selection interface before the initial apply, but mise-managed tools are not available until after that setup writes concrete machine-local settings. We will use gum as the required Bootstrap UI Dependency and install it through the platform Bootstrap Package Manager before Terrapod Setup, failing with guidance when gum cannot be bootstrapped instead of maintaining a parallel plain text interaction model.

## Considered Options

- Keep the rich prompt as direct POSIX shell key handling: rejected because terminal key handling is easy to get subtly wrong and duplicates behavior provided by purpose-built command-line UI tools.
- Preserve a separate plain text fallback: rejected because Terrapod Setup would have two interaction models to maintain and test while non-interactive setup options are explicitly out of scope.
- Use fzf through mise: rejected because mise and its aqua-managed tools are part of the post-setup declared state, so they are not reliably available before first-run Terrapod Setup.
- Download gum release binaries directly: rejected because Terrapod would take on package-manager-like version, verification, and upgrade responsibilities.

## Consequences

- Homebrew and APT install gum as part of bootstrap preparation and declared machine state restoration, but Terrapod still does not run broad Homebrew, APT, or mise upgrades.
- gum remains available after first-run so later Terrapod Setup runs can use the same interaction model.
- When gum is available, Terrapod Setup uses it for Preset selection, setting customization, and final confirmation rather than maintaining a parallel direct terminal-key implementation.
- gum-backed setting customization uses sequential questions instead of a stateful toggle menu so Terrapod does not rebuild a custom terminal UI on top of gum.
- Cancelling gum-backed Terrapod Setup preserves the existing cancellation contract: no config write, non-zero exit, and `terrapod: setup cancelled` guidance.
- Terrapod Setup does not keep a plain text fallback; missing gum, non-TTY, dumb terminal, or failed gum bootstrap stops setup with guidance until non-interactive setup options are designed.
- Both the first-run installer and `terrapod setup` report gum dependency failures: the installer covers failed pre-setup bootstrap, and `terrapod setup` covers direct post-bootstrap execution after gum is missing or removed.
- `terrapod configure <Preset>` remains a separate script-friendly configuration command, not a fallback interaction model for Terrapod Setup.
- Terrapod Setup has one gum-backed interactive presentation and does not keep `TERRAPOD_SETUP_PRESENTATION` as a presentation mode switch.
- On macOS, the first-run installer may perform a best-effort Homebrew and gum bootstrap before Terrapod Setup for setup UI only; the initial apply remains responsible for the declared-state Homebrew bootstrap.
- On Ubuntu, the first-run installer may add Charm's APT repository and install gum before Terrapod Setup for setup UI only; this third-party APT repository is an explicit Bootstrap UI Dependency trust boundary.
