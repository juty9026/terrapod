# Terrapod

This context defines the language and invariants of the Terrapod Personal
Development Environment Manager.

## Language

**Terrapod**:
The Personal Development Environment Manager that reconciles a signed declared
environment.
_Avoid_: bootstrap script, dotfiles wrapper, package manager

**tpod**:
The preferred day-to-day command spelling for Terrapod.
_Avoid_: separate tool, product name

**Management Core**:
The signed `tpod` binary and release metadata that plan and reconcile resources.
_Avoid_: shell wrapper, source checkout

**Signed Release**:
An immutable Terrapod source archive and typed resource catalog bound by a
signed release manifest.
_Avoid_: branch head, mutable checkout

**Authoring Checkout**:
A maintainer's editable repository checkout, separate from the active signed
release.
_Avoid_: active runtime source, user installation

**Resource Catalog**:
The signed declaration of config schema, resources, dependencies, providers,
version policies, and declared roots.
_Avoid_: Brewfile, installer script list

**Stable Resource ID**:
The catalog identity that joins desired state, operations, journals, and
ownership receipts across releases.
_Avoid_: package name, filesystem path

**Declared Root**:
The exact package or filesystem namespace within which an adapter may establish
and exercise ownership.
_Avoid_: whole package manager, whole home directory

**Ownership Receipt**:
Verified state proving that Terrapod owns a resource under its declared root.
_Avoid_: installation detection, command availability

**Typed Adapter**:
A resource-specific implementation of inspect, plan, execute, verify, transfer,
and prune.
_Avoid_: generic shell hook, chezmoi script

**Ready**:
A resource whose actual state satisfies its signed declaration.
_Avoid_: command exists

**Unavailable**:
A resource that cannot be reconciled safely. Its dependent operations are also
blocked without mutation.
_Avoid_: silently skipped, partially managed

**Personal Development Environment Manager**:
The product position in which Terrapod owns and reconciles its declared
development resources throughout their lifecycle.
_Avoid_: first-run bootstrapper, non-destructive dotfiles installer

**Independent Terrapod Config**:
The machine-local JSON document at `~/.config/terrapod/config.json`.
_Avoid_: chezmoi data, authoring config

**Terrapod Setup**:
The gum-backed interactive workflow that turns a **Preset** into reviewed,
concrete machine-local config.
_Avoid_: automatic apply, plain-text fallback

**Bootstrap UI Dependency**:
gum, installed before **Terrapod Setup** and retained as declared state.
_Avoid_: optional helper, plain-text enhancement

**Modern CLI Provider**:
Homebrew, the provider for shared user-facing CLI tools on both profiles.
_Avoid_: APT, mise runtime provider

**Development Runtime Manager**:
mise, which installs and selects Bun, Node.js, Python, and uv.
_Avoid_: Homebrew CLI provider, OS package manager

**macOS Terminal Profile**:
The `macos-terminal` resource selection for supported macOS machines.
_Avoid_: desktop profile, local profile

**VPS Shell Profile**:
The `vps-shell` resource selection for Ubuntu 24.04 LTS on `x86_64` and
`aarch64`.
_Avoid_: generic Linux profile, Linux desktop profile

**Preset**:
A first-run proposal that expands into concrete config values.
_Avoid_: permanent mode, dynamic policy

**Core Shell Stack**:
The mandatory command-line environment shared by both supported profiles.
_Avoid_: optional tools, operating-system packages

**Development Runtime Stack**:
The declared language runtimes managed through mise.
_Avoid_: project-local runtime, Homebrew CLI stack

**Optional Editor Stack**:
The opt-in rich editor configuration.
_Avoid_: mandatory Neovim, default editor state

**Optional AI Tool Stack**:
The opt-in AI command-line tools.
_Avoid_: core shell tools, unmanaged vendor install

**Optional Development Workspace**:
The opt-in editor, AI, and terminal workspace bundle.
_Avoid_: mandatory workspace, desktop app bundle

**macOS App Group**:
A selectable group of Terrapod-managed Homebrew casks.
_Avoid_: individual app toggle, trusted tap

**Canonical README**:
The authoritative English `README.md`.
_Avoid_: generated translation, independent product contract

**Korean README**:
`README.ko.md`, which mirrors the Canonical README heading order and contract in
natural Korean.
_Avoid_: separate product definition, self-labeled translation

## Relationships

- A **Signed Release** is the only runtime authority for desired state.
- An **Authoring Checkout** never becomes active merely because its files
  change.
- The **Resource Catalog** assigns every resource a **Stable Resource ID**,
  typed provider, dependencies, version policy, and **Declared Root**.
- A **Typed Adapter** may mutate only a resource backed by the current
  declaration and, after adoption, an **Ownership Receipt**.
- Terrapod never upgrades, transfers, or removes an undeclared or unowned
  package.
- A pre-existing declared resource is transferred completely into Terrapod
  ownership after recovery preparation and verification, or it is
  **Unavailable**. There is no intermediate managed state.
- `tpod plan` inspects actual state and prints the deterministic operation plan.
- `tpod apply` executes and verifies the active release plan.
- `tpod update` verifies a newer **Signed Release**, prints its plan, activates
  it atomically, and reconciles it.
- `tpod resolve <resource-id>` is the explicit conflict workflow for managed
  files.
- Resources are reported as **Ready** or **Unavailable**. An unavailable
  resource blocks dependent mutations.
- Apply and update automatically prune no-longer-desired resources only when
  their exact historical ownership is verified.
- Homebrew cask pruning never uses `brew uninstall --zap`, because support
  files are outside the package ownership receipt.
- The **Independent Terrapod Config** is versioned and migrated against the
  active catalog schema. It does not depend on chezmoi data.
- **Terrapod Setup** uses gum for Preset selection, setting customization, and
  final confirmation. Missing gum or an unsupported interactive terminal stops
  setup; there is no parallel plain-text UI.
- Explicit `tpod setup` writes config and does not apply. `terrapod configure
  <Preset>` is the non-interactive, script-friendly config-writing path and is
  not a fallback for interactive setup.
- A **Preset** proposes concrete settings rather than creating a permanent mode.
  Future Preset changes do not silently reshape an already configured machine.
- The Presets are minimal, development, and workstation. Minimal disables
  optional stacks; development enables the Editor, AI, and Development
  Workspace stacks; workstation additionally enables every macOS App Group.
- Workstation is available only for the **macOS Terminal Profile**.
- chezmoi is a script-free managed-files engine. Terrapod fixes its active
  signed source, destination, independent data file, and script exclusion.
- `tpod chezmoi -- <read-only-command>` is a constrained inspection escape
  hatch; mutating chezmoi commands are rejected.
- `install.sh --repair` restores only the signed **Management Core**.
- `install.sh --migrate` is a maintainer-only, one-shot bridge from the legacy
  shell/chezmoi installation. It converts config, imports eligible ownership,
  reconciles current state, and removes legacy source only after verification.
- The supported profiles are **macOS Terminal Profile** and
  **VPS Shell Profile**.
- Both profiles use Homebrew as the **Modern CLI Provider** and mise as the
  **Development Runtime Manager**. Ubuntu APT owns only system and Homebrew
  bootstrap prerequisites.
- The **Core Shell Stack** includes Oh My Zsh and tools such as fd, ripgrep,
  zoxide, lazygit, GitHub CLI, Zellij, and plain Neovim.
- The **Development Runtime Stack** includes Bun, Node.js 24, Python 3.13,
  uv/uvx, and pnpm through Node.js Corepack.
- Rich Neovim belongs to the **Optional Editor Stack**. Antigravity CLI, Claude
  Code, and Codex belong to the **Optional AI Tool Stack**.
- Enabling only the AI stack does not imply the Editor or Development Workspace
  stacks. Enabling the Development Workspace includes both Editor and AI.
- The Development Workspace never implies the macOS Desktop App Stack.
- The **VPS Shell Profile** uses `/home/linuxbrew/.linuxbrew`, supports one
  non-root management user with initial sudo access, and does not manage a
  multi-user Homebrew prefix.
- The recommended VPS floor is 1 vCPU, 1 GiB RAM, and 3 GiB free disk; this is
  guidance, not an installer gate.
- The macOS App Groups are terminal-apps (Ghostty), automation (Hammerspoon,
  Karabiner-Elements, Scroll Reverser), launcher (Raycast, 1Password CLI),
  monitoring (iStat Menus), and development-apps (Zed, Orca ADE).
- Orca uses only the fully qualified `stablyai/orca/orca` cask trust boundary.
- The macOS profile manages verified Jetendard TTF assets and only the
  font-family settings used by Ghostty, Zed, and Orca. The VPS profile excludes
  this resource.
- `tpod status` is a successful human-readable snapshot even when it reports
  unavailable resources. `tpod doctor` is the non-zero readiness gate.
- Broad `brew upgrade`, `apt upgrade`, and `mise upgrade` operations remain
  outside Terrapod. This does not prevent a typed adapter from upgrading a
  specifically declared, Terrapod-owned resource.
- The **Canonical README** is English `README.md`; `README.ko.md` follows the
  same heading order and product contract in Korean.
