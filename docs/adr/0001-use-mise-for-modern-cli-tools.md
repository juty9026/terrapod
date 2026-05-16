# Use mise for modern CLI tools

The macOS Terminal Profile and VPS Shell Profile should share one Core Shell Stack, but Homebrew on macOS, APT on Ubuntu, standalone aqua, and manual GitHub release installers each create different package names, versions, and upgrade paths. We will use OS package managers only to bootstrap each machine and install GUI or platform-specific packages, then use mise with its aqua backend for modern CLI tools and mise runtimes for language tools. This keeps macOS and Ubuntu aligned without introducing Homebrew on Linux or a second standalone aqua configuration.

## Considered Options

- Homebrew for macOS and APT for Ubuntu: rejected because Ubuntu 24.04 has missing, older, or renamed packages for several Core Shell Stack tools.
- Homebrew on Linux: rejected because it is heavier than the VPS Shell Profile needs.
- Standalone aqua plus mise: rejected because mise already exposes the aqua backend and an additional tool would split PATH, update, and configuration ownership.

## Consequences

- Homebrew remains responsible for macOS bootstrap, GUI apps, casks, fonts, and macOS fallbacks for tools whose mise-managed release assets do not support macOS.
- APT remains responsible for Ubuntu bootstrap packages and installing mise itself.
- CLI upgrades move from `brew upgrade` to `mise upgrade`.
