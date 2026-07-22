# Final Review Fix Report

## Status

Complete. All three final-branch-review findings are fixed with regression coverage.

## Finding 1: routine Homebrew shellenv recovery

- The declared-state Homebrew bootstrap now captures both `brew shellenv` command failure and shell evaluation failure explicitly.
- Either failure writes `homebrew-core` with `Fix Homebrew shellenv, then rerun tpod apply.` and exits successfully after the marker is recorded.
- Marker write failure remains a hard failure, and no bundle runs after an invalid shell environment.
- Core/platform/desktop retry paths also capture evaluation failure explicitly.

## Finding 2: Darwin hardware-aware Homebrew prefix

- Added a shared declared-state prefix helper that detects Rosetta with `sysctl -in sysctl.proc_translated` and maps translated `x86_64` to Apple Silicon hardware.
- Homebrew bootstrap/retries, mise install/retry, Optional AI, desktop retry, and shell integrations consume the shared decision.
- `install.sh`, Terrapod status/doctor, and `.zshenv` apply the same hardware decision at their standalone boundaries.
- Apple Silicon always selects `/opt/homebrew`; `/usr/local` is used only for non-translated Intel Darwin.
- A valid standard `/opt/homebrew` installation is used by absolute path even when a legacy Intel brew shadows PATH.
- Installer and doctor Rosetta simulations both resolve the Apple Silicon prefix.

## Finding 3: duplicate status warnings

- `status_key_tool_warnings` now checks only `zsh` plus profile package managers (`brew`, and `apt` on VPS).
- Homebrew-owned `chezmoi`, `git`, `mise`, `nvim`, and `zellij` are reported exclusively by the mandatory Homebrew ownership registry.

## RED / GREEN evidence

- RED: bootstrap shellenv command failure did not create `homebrew-core`.
- RED: Rosetta doctor mapping returned `/usr/local` instead of `/opt/homebrew`.
- RED: Rosetta installer mapping did not select `/opt/homebrew`.
- RED: status emitted the legacy `nvim, zellij` key-tool warning.
- RED: valid Apple Silicon Homebrew was rejected when an Intel brew shadowed PATH.
- GREEN: focused `chezmoiignore`, `terrapod_command`, and `terrapod_installer` suites pass with all new assertions.

## Full verification

- Static and rendered POSIX shell syntax: pass.
- Rendered Darwin `.zshenv` zsh syntax: pass.
- Full repository suite, fresh after the final change: **14 sh + 1 zsh, all pass**.
- Forbidden legacy installer/destructive-operation audit: no new forbidden installation or destructive apply behavior.

## Docker smoke

- `linux/amd64`: pass; 20 formulae installed, all 20 commands resolve below `/home/linuxbrew/.linuxbrew`, runtime-only mise config verified.
- `linux/arm64`: pass; 20 formulae installed, all 20 commands resolve below `/home/linuxbrew/.linuxbrew`, runtime-only mise config verified.

## Commit

- `feat: harden Homebrew recovery and Rosetta prefixes` (the commit containing this report; resolve with `git rev-parse HEAD`).

## Self-review / concerns

- Independent read-only review found no Critical, Important, or Minor issues and assessed the change Ready.
- The shared helper centralizes managed-script prefix selection; installer and Terrapod retain small equivalent decisions because each must run independently of the managed library.
- No known remaining concerns.
