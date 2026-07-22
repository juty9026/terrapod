# Install Jetendard from the latest stable release

The macOS Terminal Profile uses Jetendard as its shared coding and terminal font for Ghostty, Zed buffers and terminals, and Orca terminals. Jetendard has no official Homebrew cask, so Terrapod installs every TTF from the latest stable `kuskhan/jetendard` GitHub release, verifies the asset digest published by GitHub, and records the installed tag and owned files in a user-scoped manifest.

This is a narrow exception to ADR 0001's normal Homebrew ownership of macOS fonts. The installer checks GitHub only when its managed source changes or a failed installation is retried; ordinary `tpod status` and `tpod doctor` are offline checks, and an upstream release alone does not trigger an upgrade.

## Considered Options

- Keep JetBrains Mono Nerd Font plus D2Coding: rejected because Jetendard combines Nerd Font Latin and symbols with balanced Pretendard Korean glyphs in one monospace family.
- Add an unowned Homebrew cask token: rejected because neither `font-jetendard` nor `jetendard` exists in the official cask repository.
- Pin one Jetendard tag: rejected because this machine configuration should resolve the latest stable release when Terrapod intentionally reruns the managed installer.
- Query GitHub on every apply: rejected because an unchanged Terrapod source should not create a continuous font-upgrade channel.

## Consequences

- The installer needs Python and network access only during install or retry.
- Failed replacement preserves the prior working font files and records a non-blocking warning.
- Terrapod removes only obsolete files named by its own manifest.
- Existing Homebrew-installed JetBrains Mono Nerd Font and D2Coding copies remain installed but unmanaged.
- App settings change only font-family keys; Orca updates wait until the app is closed.
