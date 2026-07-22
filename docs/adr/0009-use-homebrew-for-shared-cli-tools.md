# Use Homebrew for shared CLI tools

The macOS Terminal Profile and VPS Shell Profile use Homebrew as the Modern CLI Provider for the mandatory Core Shell Stack and the Optional AI Tool Stack. The VPS Shell Profile installs Homebrew at `/home/linuxbrew/.linuxbrew` for every Preset. mise remains the Development Runtime Manager for Bun, Node.js, Python, and uv, while APT installs only Ubuntu system and Homebrew bootstrap prerequisites.

This decision supersedes ADR 0001's assignment of modern CLI tools to mise/aqua and its rejection of Linuxbrew, and ADR 0008's restriction of Linuxbrew to the Optional AI Tool Stack. It does not change the macOS Desktop App Stack, optional-stack semantics, or Terrapod's non-destructive apply contract.

## Considered Options

- Keep mise/aqua for shared CLI tools: rejected because GUI apps, AI CLIs, and ordinary CLIs would retain separate installation and recovery owners.
- Keep Linuxbrew conditional on AI tools: rejected because the Core Shell Stack would still have different providers across profiles.
- Move development runtimes to Homebrew: rejected because project/runtime version selection remains mise's responsibility.
- Remove legacy installs automatically: rejected because pruning mise, APT, or vendor state can affect tools outside Terrapod's ownership.

## Consequences

- `Brewfile` is the canonical declaration for 20 mandatory CLI formulae on both profiles.
- `Brewfile.macos`, `Brewfile.ai-cli-tools.tmpl`, and `Brewfile.macos-desktop-apps.tmpl` retain separate failure and retry boundaries.
- First-run installs standard-prefix Homebrew, then installs `chezmoi` and `gum` before Terrapod Setup.
- Ubuntu supports `x86_64` and `aarch64`, one management user, initial sudo access, and the standard `/home/linuxbrew/.linuxbrew` prefix.
- Apply restores missing packages with auto-update disabled and never upgrades or removes packages.
- Status reports mandatory command ownership; doctor fails when a mandatory command is missing or resolves outside Homebrew.
- Existing legacy payloads remain until the user chooses to clean them up.
