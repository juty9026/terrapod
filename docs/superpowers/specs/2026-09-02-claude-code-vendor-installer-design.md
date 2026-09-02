# Claude Code Vendor Installer Design

## Goal

Install Claude Code through its vendor-published install script instead of the
`claude-code` Homebrew cask, because the cask lags vendor releases and Claude
Code releases often enough for that lag to show in daily use.

Only Claude Code moves. `antigravity-cli` and `codex` stay Homebrew casks, the
**Optional AI Tool Stack** stays scoped to the **macOS Terminal Profile**, and
the stack keeps one install warning category and one status and doctor report.

This change is non-destructive. An existing `claude-code` cask is not
uninstalled; executable selection reports it as an advisory the way it reports
any other alternate installation.

## Current State

- `Brewfile.ai-cli-tools.tmpl` declares `antigravity-cli`, `claude-code`, and
  `codex`, and renders empty outside the **macOS Terminal Profile**.
- `.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl` renders that
  manifest into a temporary Brewfile and runs
  `HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade`. Failure records the
  `optional-ai-cli-tools` install warning and lets apply continue.
- `executable-selection` carries the record `homebrew-cask|claude-code|claude`
  and knows three providers: `homebrew-formula`, `homebrew-cask`, and `mise`.
- `README.md` and `README.ko.md` document
  `brew upgrade --cask claude-code codex antigravity-cli` as the explicit
  upgrade command and describe `enableAiCliTools` as installing three casks.

ADR 0008 chose Homebrew over vendor installers for all three tools. ADR 0015
scoped the stack to macOS because all three are casks and Homebrew on Linux
does not install casks. ADR 0010 established the `brew bundle --no-upgrade`
contract, and ADR 0012 established the read-only three-step check that
`executable-selection` performs.

## Considered Approaches

### A Claude Code specific provider in the executable selection registry

Add a fourth provider, `claude-installer`, and change the stack's Claude Code
record to `claude-installer|claude-code|claude`. Provider presence is the
canonical executable at `$HOME/.local/bin/claude`, and the provider label makes
the failure text name the vendor installer rather than Homebrew Cask.

This is the selected approach. Reporting stays on one path, and the change is
confined to one record and three `case` branches.

### A general vendor installer provider carrying its own path

Extend the record format with a fourth field holding the install path, so other
tools could move later. Rejected as unused generality: only Claude Code moves,
and widening this provider later is a record-format change, not a redesign.

### Check Claude Code outside the registry

Leave `executable-selection` alone and special-case Claude Code in the `tpod`
command. Rejected because it splits reporting in two and exempts one tool from
the provider, canonical executable, and PATH precedence model that ADR 0012
established for every other declaration.

## Package Ownership

The **Optional AI Tool Stack** now draws from two package sources on the
**macOS Terminal Profile**:

- The **Modern CLI Provider** owns the casks `antigravity-cli`, providing `agy`,
  and `codex`, providing `codex`.
- The **Claude Code Installer** owns Claude Code, providing `claude` at
  `$HOME/.local/bin/claude`.

**Claude Code Installer** is the vendor-published install script at
`https://claude.ai/install.sh`. The glossary entry needs an explicit _Avoid_
line for "Terrapod installer", because this repository's own bootstrap script
is also named `install.sh`.

The stack stays macOS-only. The vendor installer supports Linux, so ADR 0015's
recorded reason no longer holds for Claude Code; applicability is a profile
decision rather than a consequence of the package source, and ADR 0017 records
that correction.

## Installation

The Claude Code step lives inside the existing
`run_before_60-install-ai-cli-tools.sh.tmpl` rather than in a new script,
because the `optional-ai-cli-tools` warning category is one per stack. Two
scripts sharing a category would overwrite each other's marker.

The existing template gate is unchanged: the body renders only when
`.chezmoi.os` is `darwin` and the stack is effectively enabled. Inside that
body, the Homebrew bundle step and the Claude Code step run independently. A
failing bundle does not skip Claude Code, and a failing Claude Code install does
not affect the bundle result.

### Install only when the canonical executable is absent

The vendor script always downloads the latest build and then runs
`claude install`. Running it unconditionally would upgrade Claude Code on every
apply, which contradicts the no-upgrade contract that governs every other
declared package. The step therefore returns early when
`$HOME/.local/bin/claude` is executable.

Version freshness belongs to Claude Code's own updater. Terrapod restores a
missing install and nothing more.

### Download to a temporary file and run it with bash

The step follows the download-then-run pattern already used for Oh My Zsh: fetch
with `curl` into a `mktemp` file, run it, and remove it from the existing `trap`
handler. Two details differ from the Oh My Zsh step.

The script is run with `bash`, not `sh`, invoked by name so it resolves through
`PATH`. The vendor script declares `#!/bin/bash` and uses `[[ =~ ]]`,
`BASH_REMATCH`, and `$'...'` quoting. Terrapod's installer scripts are
`#!/bin/sh`, so the interpreter must be named explicitly. macOS always provides
`/bin/bash`, and the features used work in bash 3.2.

Standard input is redirected from `/dev/null`. The vendor script ends by running
`claude install`, which has no unattended flag and can present a terminal UI —
the vendor script itself restores the terminal with `stty sane` after a signal
death. First-run installation happens in an interactive terminal driving `gum`
prompts, so an install that waits for input would stall apply. This repository
has no existing `</dev/null` precedent, so the redirection carries a comment
explaining why it is there.

### Failure recording

The stack keeps one warning category covering both sources, and marker values
stay single-line. What the marker can name differs by source:

- A `brew bundle` failure cannot identify which cask failed without parsing
  Homebrew output, so it keeps the existing bulk failure summary.
- A **Claude Code Installer** failure covers exactly one tool, so the marker
  names `Claude Code`.
- When both fail, one marker records both, joined on a single line.

The category is cleared only when both steps succeed. Either failure leaves a
marker, so a partially installed stack never reports itself as ready.

Apply continues in every case, and a marker that cannot be written still fails
loudly through the existing `install_warning_recorded` policy.

### Shell profile edits

`claude install` sets up shell integration and may edit shell startup files.
`run_before_60` runs before chezmoi writes the managed `.zshenv` and `.zshrc`,
so those edits are overwritten. This is the behavior ADR 0006 already accepted
for vendor installers. Nothing is lost in practice: `dot_zshenv.tmpl` already
places `$HOME/.local/bin` on the managed PATH.

## Executable Selection

`executable-selection` gains one provider. The changes are confined to four
places.

The stack's record becomes `claude-installer|claude-code|claude`. The package
token stays `claude-code` — it is the domain name already used in status and
doctor output, and it does not assert a package source.

`provider_has_package` gains a `claude-installer` branch testing whether
`$HOME/.local/bin/claude` is executable. `expected_executable` gains a
`claude-installer` branch returning `$HOME/.local/bin/$command`. No new
environment seam is introduced: `TERRAPOD_MISE_SHIMS_DIR` exists because mise
has its own data directory variable, whereas this path derives from `$HOME`
alone, and the tests already override `HOME`.

`provider_label` maps `claude-installer` to `the Claude Code installer`, so a
missing install reads `claude-code is not installed through the Claude Code
installer`. The wording overlaps slightly, but it keeps the sentence frame
shared with the other providers and tells the reader that `brew` will not fix
it.

ADR 0012's three outcomes are preserved:

| State | Report |
| --- | --- |
| `$HOME/.local/bin/claude` absent | failure, naming the Claude Code installer |
| present and selected first on PATH | canonical |
| present but another file is primary | advisory with both paths |

A machine that still has the `claude-code` cask lands in the third row.
`dot_zshenv.tmpl` prepends `$HOME/.local/bin` before evaluating Homebrew's
`shellenv`, so the Homebrew prefix ends up ahead of it and the cask copy stays
primary until the user removes it. Terrapod reports the advisory and does not
remove anything, per ADR 0012.

On a real machine the `claude-installer` provider makes provider presence and
canonical executable existence the same predicate, so the
`canonical executable is missing` branch is unreachable — an absent install is
always caught by the first check. User-visible behavior is correct; only one
step is formally redundant. Under the inventory seam the two are decoupled, so
tests can still exercise both branches.

## Documentation

`CONTEXT.md` adds a **Claude Code Installer** glossary entry and changes four
rules:

- The stack installs the casks `antigravity-cli` and `codex` and installs Claude
  Code through the **Claude Code Installer**, on the **macOS Terminal Profile**
  only.
- `Brewfile.ai-cli-tools.tmpl` is the canonical declaration for the stack's
  Homebrew packages, not for the whole stack.
- The **Modern CLI Provider** owns two stack casks, not three.
- The `optional-ai-cli-tools` warning category covers both package sources.

It adds three rules:

- The **Claude Code Installer** is the canonical provider for Claude Code, whose
  canonical executable is `$HOME/.local/bin/claude`.
- `tpod apply` runs the **Claude Code Installer** only when that executable is
  absent; Claude Code's own updater owns version freshness, occupying the same
  place as the no-upgrade contract for Homebrew packages.
- Claude Code stays scoped to the **macOS Terminal Profile** even though the
  vendor installer supports Linux.

ADR 0017, `Install Claude Code through its vendor installer`, records the
decision. It supersedes ADR 0008's package-source choice for Claude Code alone
and leaves that choice in force for the other two casks. It corrects ADR 0015's
stated reason for the macOS scope as it applies to Claude Code. Rejected
options: keeping the cask and accepting the lag; moving the whole stack back to
vendor installers, which discards ADR 0008's single-source benefit for two tools
that do not need it; running the vendor script on every apply, which breaks
ADR 0010's no-upgrade contract; and extending the stack to the **VPS Shell
Profile**, which confuses a package-source change with a profile decision.

`README.md` and `README.ko.md` change in three places each. The explicit upgrade
command becomes `brew upgrade --cask codex antigravity-cli`. One sentence states
that Claude Code's own updater keeps it current, so the upgrade section does not
leave Claude Code unexplained. The `enableAiCliTools` row describes both
sources.

The READMEs carry no migration guidance for an existing `claude-code` cask.
Executable selection's advisory is the mechanism for that case, and ADR 0012
requires it to stay provenance-neutral.

## Testing

`homebrew_manifests_test.sh` compares the rendered AI manifest against an exact
expected list, so removing `cask "claude-code"` there is what prevents the cask
from reappearing.

`chezmoiignore_test.sh` renders and executes the installer script against
stubs. Its `brew` stub asserts each expected cask line in the temporary
Brewfile, so the `claude-code` assertion and the "declares Claude Code cask"
check are removed. Four cases are added, using `curl` and `bash` stubs placed in
the same stub directory the harness already puts on `PATH`:

- An existing `$HOME/.local/bin/claude` leaves the vendor installer unrun.
- A missing one runs it and clears the warning on success.
- A failing vendor installer records a marker whose text names `Claude Code`.
- A failing `brew bundle` still attempts the Claude Code step.

`executable_selection_test.sh` gains `claude-installer` coverage for the three
outcomes above: missing install, canonical selection, and an advisory when
another file is primary.

`terrapod_command_test.sh` updates the stubbed executable-selection output
string that reads `not installed through Homebrew Cask`, and the labels of the
shadow advisory cases that describe Claude Code as a Homebrew cask.

`readme_korean_test.sh` and `readme_optional_stack_profiles_test.sh` update
their expected README strings.

## Out of Scope

- Moving `antigravity-cli` or `codex` off Homebrew.
- Making the **Optional AI Tool Stack** applicable on the **VPS Shell Profile**.
- Removing, detecting, or advising on an existing `claude-code` cask beyond the
  existing provenance-neutral advisory.
- Documenting or running an explicit Claude Code upgrade command such as
  `claude update`. The READMEs state only that Claude Code updates itself.
