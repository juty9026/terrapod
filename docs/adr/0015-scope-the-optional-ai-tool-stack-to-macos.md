# Scope the Optional AI Tool Stack to macOS

The **Optional AI Tool Stack** installs Antigravity CLI, Claude Code, and Codex
only on the **macOS Terminal Profile**. On the **VPS Shell Profile** the stack is
not applicable: Terrapod Setup does not offer it, the `development` **Preset**
writes it disabled, `Brewfile.ai-cli-tools.tmpl` renders empty, and the installer
script only clears a stale `optional-ai-cli-tools` warning marker.

This decision supersedes ADR 0008's claim that the stack installs "from the same
Homebrew casks on the macOS Terminal Profile and VPS Shell Profile" and its
consequence that Ubuntu bootstraps Homebrew only when the stack is enabled. ADR
0010 already made standard-prefix Homebrew mandatory on both profiles, so the
Optional AI Tool Stack no longer governs Linux Homebrew at all. ADR 0008's choice
of Homebrew over vendor installers stays in force for macOS.

Homebrew publishes `antigravity-cli`, `claude-code`, and `codex` as casks with no
formula counterpart, and Homebrew on Linux does not install casks. Handing that
manifest to `brew bundle` on Ubuntu failed on every apply, permanently recorded an
`optional-ai-cli-tools` install warning, and kept `tpod doctor` failing.

## Considered Options

- Add a Linux install path through vendor installers or npm: rejected because it
  reintroduces the split installation, recovery, and upgrade behavior that ADR
  0008 removed, for three tools whose Linux demand is unproven.
- Block the `development` **Preset** on the **VPS Shell Profile**: rejected
  because it would also remove the **Optional Editor Stack** and **Optional
  Development Workspace**, which work there.
- Reject `enableAiCliTools = true` on the **VPS Shell Profile** as a
  configuration error: rejected because **Preset**s are the configuration unit
  and macOS App Groups already establish "silently not applicable" as the
  profile-mismatch behavior.

## Consequences

- `Brewfile.ai-cli-tools.tmpl` and
  `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl` gate the stack on
  `.chezmoi.os` being `darwin`.
- The installer script still renders on Linux so that it clears a stale
  `optional-ai-cli-tools` marker left by an earlier apply. Marker pruning only
  removes unrecognized categories, so no other component would clear it.
- `effective_ai_cli_tools_enabled` reports `false` outside the **macOS Terminal
  Profile**, which also stops **Optional Development Workspace** from implying
  the stack there.
- The `development` **Preset** stays available on the **VPS Shell Profile** and
  keeps the **Optional Editor Stack** and **Optional Development Workspace**.
- `tpod status` and `tpod doctor` report the stack as not applicable on the
  **VPS Shell Profile**; executable selection does not check `agy`, `claude`, or
  `codex` there.
- A machine-local `enableAiCliTools = true` on the **VPS Shell Profile** is
  ignored rather than rejected.
