# Homebrew AI CLI and Development Apps Design

## Goal

Use Homebrew as the single declared installation source for Antigravity CLI,
Claude Code, and Codex on both supported machine profiles. At the same time,
replace the macOS `ai-apps` App Group with a `development-apps` App Group that
installs only Zed and Orca ADE.

This change is non-destructive. Removing an app or CLI from Terrapod's declared
state does not uninstall an existing application or legacy vendor-installed
binary.

## Current State

- The Optional AI Tool Stack downloads and runs three vendor installer scripts.
- Homebrew is a Bootstrap Package Manager only on the macOS Terminal Profile.
- The VPS Shell Profile uses APT for bootstrap and mise as its Modern CLI
  Provider.
- The `ai-apps` macOS App Group installs Claude Desktop, the Codex desktop app
  that updates to the unified ChatGPT app, Antigravity 2.0, Antigravity IDE,
  and Orca ADE.
- `enableMacosAppGroupAiApps` is part of the managed Terrapod Setup schema.

ADR 0001 explicitly rejected Homebrew on Linux, and ADR 0006 selected vendor
installers for the Optional AI Tool Stack. The new decision supersedes those
parts of the earlier ADRs without changing mise ownership of other modern CLI
tools and runtimes.

## Considered Approaches

### Common optional Homebrew bundle on both profiles

Create one rendered optional Brewfile containing the three AI CLI casks. The
macOS Terminal Profile uses its existing Homebrew installation, while the VPS
Shell Profile conditionally bootstraps Homebrew only when the Optional AI Tool
Stack is effectively enabled.

This is the selected approach. It gives both profiles the same package tokens,
installation path, and explicit upgrade command while preserving optional-stack
semantics.

### Add the AI CLI casks to the existing macOS Brewfile

This would reuse the macOS bootstrap implementation but would not provide one
installation source on Ubuntu. It would also make optional AI tools part of the
mandatory macOS bootstrap state.

### Keep separate OS-specific AI CLI installers

This would avoid Linuxbrew but retain two installation and recovery paths. It
does not meet the single-source goal and would continue to split upgrade
instructions between Homebrew and vendor installers.

## Package Ownership

The Optional AI Tool Stack owns these Homebrew casks:

- `antigravity-cli`, providing `agy`
- `claude-code`, providing `claude`
- `codex`, providing `codex`

A new rendered `Brewfile.ai-cli-tools.tmpl` is the canonical declaration for
all three casks. The existing Optional AI Tool Stack installer renders this
bundle and runs `brew bundle --no-upgrade` so `tpod apply` restores missing
declared packages but does not perform routine upgrades.

The VPS Shell Profile installs Homebrew at its supported Linux prefix,
`/home/linuxbrew/.linuxbrew`, only when `enableAiCliTools` or
`enableDevelopmentWorkspace` makes the Optional AI Tool Stack effective.
Ubuntu bootstrap continues to use APT for system prerequisites, gum, and mise.
Other modern CLI tools and development runtimes remain owned by mise.

Disabling the Optional AI Tool Stack does not uninstall its casks or Homebrew.
It clears stale Optional AI Tool Stack install warnings and stops declaring the
bundle on future applies.

## Installation and Recovery

On macOS, the Optional AI Tool Stack installer locates Homebrew through `PATH`
or the supported Apple Silicon and Intel prefixes. On Ubuntu, it also checks
`/home/linuxbrew/.linuxbrew/bin/brew`; if missing, it downloads the official
Homebrew installer to a temporary file and runs it non-interactively.

The installer adds the resolved Homebrew `shellenv` to its process before
running the optional bundle. It does not write unmanaged shell startup lines;
Terrapod remains responsible for the managed shell environment.

Homebrew bootstrap, shell environment, or bundle failures use the existing
`optional-ai-cli-tools` warning category. First-run apply records the warning
and remains recoverable. Routine apply returns non-zero after recording the
warning. A later successful apply clears the warning.

The Homebrew bundle is installed even if a legacy `agy`, `claude`, or `codex`
binary is already on `PATH`; command presence alone no longer skips declared
package installation.

## Legacy CLI Collision Detection

When the Optional AI Tool Stack is enabled, `tpod status` and `tpod doctor`
resolve each AI command and compare it with the active Homebrew prefix. A
command whose resolved path is outside that prefix is reported as a legacy
installation shadowing the Homebrew-managed command.

Terrapod does not delete or rename the legacy binary. Guidance asks the user to
remove it using the method appropriate to its original installer and rerun
`tpod apply` or open a new shell. This condition is a warning, not a hard doctor
failure.

On the VPS Shell Profile, `brew` is shown and checked as a required dependency
only while the Optional AI Tool Stack is enabled. A minimal VPS with the stack
disabled does not need Homebrew and receives no missing-Homebrew warning.

## macOS Development Apps

Replace the `ai-apps` App Group with `development-apps`:

- `cask "zed"`
- `cask "stablyai/orca/orca", trusted: true`

The group no longer declares Claude Desktop, the Codex/ChatGPT desktop app,
Antigravity 2.0, or Antigravity IDE. Existing installations are left alone.
Zed configuration, extensions, and keymaps are outside this change.

The new managed key is `enableMacosAppGroupDevelopmentApps`. The `workstation`
Preset enables it, while `minimal` and `development` keep it disabled. The
Optional Development Workspace does not imply this macOS-only App Group.

## Explicit Configuration Migration

`enableMacosAppGroupAiApps` is not interpreted as an alias for the new key.
Doing so could install Zed on a machine whose earlier selection referred to a
different app set.

A config that contains the old key but lacks
`enableMacosAppGroupDevelopmentApps` is incomplete under the new schema.
`tpod status`, `tpod doctor`, and apply preflight provide explicit guidance to
run `tpod setup` or `terrapod configure <Preset>`. A successful setup or
configure write removes the deprecated key while preserving unrelated config
data.

## Upgrade Policy

Terrapod continues to separate declared-state application from intentional
package upgrades. `tpod update` only refreshes the Terrapod Source Repository,
and `tpod apply` uses `brew bundle --no-upgrade`.

The Canonical README and Korean README document the explicit AI CLI upgrade
flow:

```sh
brew update
brew upgrade --cask claude-code codex antigravity-cli
```

No new `tpod upgrade` command is added.

## Documentation

- Add an ADR that records conditional Linuxbrew and Homebrew ownership of the
  Optional AI Tool Stack, superseding the conflicting portions of ADR 0001 and
  ADR 0006.
- Update `CONTEXT.md` so the Bootstrap Package Manager and Modern CLI Provider
  relationships describe the narrow Homebrew exception for the Optional AI
  Tool Stack.
- Update both READMEs, keeping the English Canonical README authoritative and
  the Korean README aligned.
- Historical implementation plans and specs remain unchanged.

## Testing

Shell tests cover these behaviors:

- Development Apps renders only Zed and the explicitly trusted Orca cask.
- The new setup key appears exactly once in every Preset configuration, and
  configure/setup removes the deprecated key.
- A legacy config is reported as incomplete and receives migration guidance.
- The common AI CLI Brewfile renders all three casks only when the stack is
  enabled.
- macOS uses existing Homebrew without downloading vendor installers.
- Ubuntu conditionally bootstraps Homebrew, configures `shellenv`, and installs
  the common bundle.
- Disabled Ubuntu AI tooling neither installs nor requires Homebrew.
- Homebrew and bundle failures preserve the existing recoverable warning
  contract.
- Status and doctor distinguish missing tools from non-Homebrew commands that
  shadow installed casks.
- README tests enforce the new membership, package source, migration, and
  upgrade guidance.

The full repository shell test suite must pass after the focused red-green
cycles.
