# Dotfiles

This context describes the machine profiles and shared terminal experience managed by this dotfiles management tool.

## Language

**macOS Terminal Profile**:
The tracked terminal setup for personal macOS workstations.
_Avoid_: desktop profile, local profile

**VPS Shell Profile**:
The tracked terminal setup for Ubuntu VPS machines used as headless development machines.
_Avoid_: server clone, Linux desktop profile

**Supported Ubuntu Release**:
Ubuntu 24.04 LTS, the only Linux distribution release targeted by the VPS Shell Profile.
_Avoid_: generic Linux support, Ubuntu 22.04 support

**Core Shell Stack**:
The mandatory command-line tool set expected to exist in the VPS Shell Profile.
_Avoid_: optional tools, nice-to-have tools

**Development Runtime Stack**:
The mandatory language/runtime tool set expected to exist in the VPS Shell Profile.
_Avoid_: project-local runtime, optional runtime

**Optional Editor Stack**:
The opt-in rich editor configuration that is excluded from every machine profile unless explicitly enabled.
_Avoid_: core shell editor, mandatory editor config, default LazyVim setup

**Optional AI Tool Stack**:
The opt-in command-line AI tools that may be installed on selected development machines.
_Avoid_: core shell tools, mandatory AI tools

**Optional Development Workspace**:
The opt-in full development stack bundle for machines that need rich editor configuration, AI tools, and an integrated terminal workspace together.
_Avoid_: core terminal multiplexer config, default dev layout, mandatory workspace

**Bootstrap Package Manager**:
The operating-system package manager used only to prepare a machine for the shared tool stack.
_Avoid_: primary CLI provider, runtime manager

**Bootstrap UI Dependency**:
A command-line UI tool that Terrapod requires before **Terrapod Setup** so first-run interactive choices use a reliable selection interface.
_Avoid_: optional UI tool, setup-only helper, plain fallback enhancement, Core Shell Stack synonym

**Modern CLI Provider**:
Homebrew, the shared package provider for mandatory user-facing CLI tools across the macOS Terminal Profile and VPS Shell Profile.
_Avoid_: mise CLI provider, aqua provider, platform-specific CLI source

**Development Runtime Manager**:
mise, which installs and selects Bun, Node.js, Python, and uv independently from Homebrew-managed user-facing CLI tools.
_Avoid_: Modern CLI Provider, Homebrew runtime manager, aqua tool provider

**macOS Desktop App Stack**:
The opt-in macOS cask set for GUI apps, system-style desktop apps, and cask-delivered desktop support tools.
_Avoid_: Homebrew bootstrap, shared CLI formula, Core Shell Stack

**macOS App Group**:
A user-selectable subset of the **macOS Desktop App Stack** that keeps related desktop apps installable together.
_Avoid_: individual app toggle, Homebrew tap group, Core Shell Stack

**Dotfiles Management Tool**:
The user-facing Terrapod installer and management command layer that configures machines while using chezmoi as its internal apply engine.
_Avoid_: plain chezmoi repo, standalone package manager, OS provisioning platform

**Terrapod**:
The branded name for the **Dotfiles Management Tool**, evoking a pod that lands on an unknown machine and terraforms it into a familiar environment.
_Avoid_: dotfiles command, chezmoi wrapper

**Terrapod Source Repository**:
The GitHub repository that hosts **Terrapod** and this repository's declared dotfiles state.
_Avoid_: dotfiles repository, legacy source URL

**tpod**:
The preferred day-to-day command spelling for **Terrapod**.
_Avoid_: separate tool, brand name

**Terrapod Setup**:
The interactive setup workflow that turns a **Preset** into concrete machine-local **Terrapod** settings before the initial apply.
_Avoid_: OS provisioning wizard, standalone installer, configure shortcut

**Preset**:
A first-run setup choice that expands into concrete optional stack and app-group settings for a machine.
_Avoid_: machine preset, permanent mode, dynamic policy

**Canonical README**:
The English `README.md` that defines the authoritative Terrapod README content.
_Avoid_: primary docs, source README

**Korean README**:
The Korean-language README that follows the **Canonical README** for Korean readers.
_Avoid_: separate Korean introduction, independent README, self-labeled translation

## Relationships

- The **Dotfiles Management Tool** is the user-facing entry point for first install, configuration changes, and routine dotfiles maintenance.
- **Terrapod** is the brand name and full command for the **Dotfiles Management Tool**.
- **tpod** is the preferred day-to-day command spelling for **Terrapod**, not a separate interface.
- User-facing help, quick-start, routine command examples, and guidance should use `tpod` for copyable commands while preserving `terrapod` as the full executable spelling in a concise alias note.
- Installation and bootstrap recovery examples may continue to use checked-out executable paths or `terrapod` when the full command name improves clarity before the alias is installed.
- The **Terrapod Source Repository** uses `juty9026/terrapod` as its canonical GitHub slug.
- Public first-run installation references use the HTTPS **Terrapod Source Repository** URL `https://github.com/juty9026/terrapod.git`.
- Maintainer remotes may use the SSH **Terrapod Source Repository** URL `git@github.com:juty9026/terrapod.git`.
- The legacy `juty9026/dotfiles` slug is not a supported **Terrapod** installation path after repository renaming.
- **Terrapod** and its first-run installer are implemented as POSIX shell entry points.
- The first-run **Terrapod** installer installs standard-prefix Homebrew on both supported profiles, then installs chezmoi and the gum **Bootstrap UI Dependency** through Homebrew before **Terrapod Setup**.
- The first-run **Terrapod** installer uses `https://github.com/juty9026/terrapod.git` as the default source repository URL.
- The first-run **Terrapod** installer resumes from an existing default chezmoi source directory when it is a checked-out **Terrapod Source Repository** that has not completed initial apply.
- A resumable **Terrapod Source Repository** checkout must contain the checked-out **Terrapod** command, expected recovery-core source files, and repository identity for `juty9026/terrapod`; an arbitrary chezmoi source directory is not resumable.
- An existing **Terrapod Source Repository** checkout with installed `terrapod` and `tpod` commands is treated as an already installed **Terrapod** machine; the first-run installer should guide users to routine `tpod` commands instead of reinstalling.
- Existing **Terrapod** installation detection requires the installed command surface to pass the same `~/.local/bin/tpod help` validation used for recovery-core validation; broken command files or symlinks are treated as incomplete first-run state.
- When first-run is rerun on an already installed **Terrapod** machine, it exits without running `tpod apply` automatically and guides users to routine commands such as `~/.local/bin/tpod status` and `~/.local/bin/tpod apply`.
- An existing **Terrapod Source Repository** checkout without the installed **Terrapod** command surface is treated as an incomplete first-run installation and is eligible for resume.
- Incomplete first-run resume reuses existing managed **Terrapod Setup** config when it is present and complete; **Terrapod Setup** is rerun only when managed setup config is missing or incomplete.
- When incomplete first-run resume reruns **Terrapod Setup**, it remains in first-run context and continues into recovery-core apply and full declared-state apply after setup completes.
- Managed **Terrapod Setup** config is complete only when the profile and all current managed optional stack and **macOS App Group** setting keys are present; missing managed keys cause setup to rerun instead of silently filling Preset defaults.
- Managed **Terrapod Setup** config completeness is schema-based rather than platform-pruned; unsupported platform options such as **macOS App Group** keys on the **VPS Shell Profile** are still stored as concrete disabled settings.
- First-run resume may rerun **Terrapod Setup** when managed setup config is incomplete, but routine `tpod apply` must not open interactive setup prompts automatically; routine apply should report missing managed setup keys and guide users to an explicit setup or configure command.
- Routine `tpod apply` exits non-zero when required managed setup config keys are missing because declared state cannot be computed safely without explicit user configuration.
- The first-run **Terrapod** installer stops with guidance when the default chezmoi source directory already exists but is not a resumable **Terrapod Source Repository** checkout.
- The first-run **Terrapod** installer invokes **Terrapod Setup** before the initial apply instead of asking users to run a second setup command manually.
- A **Bootstrap UI Dependency** is not a temporary setup-only helper; it remains available after first-run so later **Terrapod Setup** runs can use the same interaction model.
- The first **Bootstrap UI Dependency** is gum, installed through the **Modern CLI Provider** before **Terrapod Setup**.
- Failure to install a **Bootstrap UI Dependency** fails first-run installation before **Terrapod Setup** rather than falling back to a separate plain text interaction model.
- **Terrapod Setup** uses gum for Preset selection, setting customization, and final confirmation instead of maintaining parallel rich and plain interaction models.
- **Terrapod Setup** should place concise Preset explanations beside or near each selectable Preset instead of printing a separate Preset guide before the gum choice prompt.
- **Terrapod Setup** should print concise optional stack and **macOS App Group** explanations immediately before the related gum confirmation prompt instead of making the confirm prompt verbose or printing a separate option guide before sequential confirmations.
- gum-backed setting customization uses sequential questions rather than a stateful toggle menu.
- gum belongs to the declared machine state as well as the first-run bootstrap path, so **Terrapod** restores it through the cross-profile `Brewfile` after initial apply.
- Cancelling gum-backed **Terrapod Setup** preserves the existing setup cancellation contract: no config write, non-zero exit, and `terrapod: setup cancelled` guidance.
- The first-run installer explains **Bootstrap UI Dependency** bootstrap failures before **Terrapod Setup**, and `terrapod setup` also explains missing gum when run directly after bootstrap.
- On both supported profiles, the first-run installer requires standard-prefix Homebrew and installs `chezmoi` and `gum` with Homebrew before **Terrapod Setup**; bootstrap failure stops first-run with guidance.
- On the **VPS Shell Profile**, APT installs only Ubuntu system and Homebrew bootstrap prerequisites and does not add Charm or mise repositories.
- On the **VPS Shell Profile**, pre-Setup Homebrew prerequisite installation may use interactive `sudo` prompts, but missing or unusable sudo prevents **Terrapod Setup** and remains a hard first-run failure.
- A gum bootstrap failure before **Terrapod Setup** is a hard first-run failure, while a later declared-state package-manager failure involving gum is a machine profile readiness warning after the recovery core is available.
- chezmoi remains the internal apply engine for the **Dotfiles Management Tool**, not the primary workflow users need to remember.
- The **Dotfiles Management Tool** exposes first-class maintenance commands when they add profile, preset, installer, or validation context around chezmoi behavior.
- Direct chezmoi commands remain an escape hatch for advanced maintenance, not the default documented workflow.
- **Terrapod Setup** belongs to the `terrapod` command surface, while `install.sh` remains the thin first-run bootstrap entry point.
- **Terrapod Setup** invoked by the first-run installer is followed by initial apply, while explicit routine `tpod setup` writes config only and guides users to run `tpod apply`.
- `terrapod setup` is the human-facing **Terrapod Setup** wizard, while `terrapod configure <Preset>` remains the script-friendly command for writing concrete settings from one **Preset**.
- `terrapod configure <Preset>` is not a plain fallback for **Terrapod Setup**; it is a separate script-friendly configuration command without interactive customization.
- `terrapod configure <Preset>` may overwrite existing managed setup settings non-interactively after the user explicitly invokes it; config backup rules still apply.
- `terrapod configure <Preset>` writes config only and does not run `tpod apply` automatically; its output should guide users to the next explicit apply step.
- `terrapod configure <Preset>` does not create shell startup file backups because it does not apply or overwrite shell startup files.
- **Terrapod Setup** does not expose a setup presentation mode switch; it has one gum-backed interactive presentation.
- Explicit routine `tpod setup` may review and change existing complete managed setup config; routine `tpod apply` does not open this interactive workflow automatically.
- Routine `tpod setup` changes managed setup config only and does not create shell startup file backups because it does not apply or overwrite shell startup files.
- A **Preset** is a starting point for concrete settings, not a permanent dynamic policy.
- A **Preset** shows a summary of the optional stack and app-group settings it will enable before installation.
- First-run setup allows users to customize the concrete settings produced by a **Preset** before they are saved.
- **Terrapod Setup** lets users customize **Optional Editor Stack**, **Optional AI Tool Stack**, **Optional Development Workspace**, and applicable **macOS App Group** settings before saving concrete machine-local settings.
- **Terrapod Setup** customization should feel like reviewing and adjusting concrete settings proposed by the selected **Preset**, not answering standalone yes/no questions about abstract option names.
- gum-backed **Terrapod Setup** confirmations should use action labels that expose the current **Preset**-proposed value, such as `Keep enabled`/`Disable` or `Enable`/`Keep disabled`, instead of generic `Yes`/`No` labels.
- gum-backed **Terrapod Setup** setting prompts should show the setting name first, then a concise explanation, then an action-oriented confirmation prompt that repeats the setting name or otherwise makes the target unambiguous.
- gum-backed **Terrapod Setup** setting confirmation prompts should keep a stable `Enable <setting>?` shape while the confirmation action labels communicate whether the selected **Preset** currently proposes enabling or disabling that setting.
- Rich **Terrapod Setup** presentation should reserve colored emphasis for section headings; individual setting prompt titles should stay visually quieter so their explanations remain attached to the choice.
- In **Terrapod Setup**, enabling **Optional Development Workspace** presents **Optional Editor Stack** and **Optional AI Tool Stack** as a grouped inclusion list under the workspace prompt rather than as repeated standalone included-setting messages.
- In the **macOS App Groups** section of **Terrapod Setup**, each group should use the same setting block structure while leading with the group name, because the section heading already supplies the **macOS App Group** context.
- The final **Terrapod Setup** settings summary should continue showing the concrete machine-local key/value settings that will be written, even when earlier customization prompts use more human-facing labels.
- Changing a **Preset** in the future must not silently change machines that already saved concrete optional stack and app-group settings.
- The first **Preset** choices are minimal, development, and workstation.
- The minimal **Preset** keeps optional stacks and macOS app groups disabled.
- The development **Preset** enables the **Optional Editor Stack**, **Optional AI Tool Stack**, and **Optional Development Workspace**.
- The workstation **Preset** includes the development **Preset** settings and enables every **macOS App Group**.
- The workstation **Preset** is available only for the **macOS Terminal Profile** and is hidden for the **VPS Shell Profile**.
- First-run setup and later configuration changes use the same config-writing rules.
- Config writes update only managed **Dotfiles Management Tool** settings and preserve unrelated chezmoi config values.
- Interactive setup asks before updating an existing chezmoi config.
- Config writes use a conservative POSIX shell upsert for managed `[data]` keys instead of attempting to parse all TOML features.
- Config writes back up an existing chezmoi config before changing managed `[data]` keys.
- The **macOS Terminal Profile** and **VPS Shell Profile** are separate machine profiles in one **Terrapod Source Repository**.
- The **VPS Shell Profile** targets exactly one **Supported Ubuntu Release**.
- The **VPS Shell Profile** includes the **Core Shell Stack**.
- The **VPS Shell Profile** includes the **Development Runtime Stack**.
- The **Core Shell Stack** includes Oh My Zsh and modern CLI tools such as fd, ripgrep, zoxide, lazygit, GitHub CLI (`gh`), and plain Neovim.
- The **Development Runtime Stack** includes Bun, Node.js 24, Python 3.13, and uv managed by the **Development Runtime Manager**.
- pnpm belongs to the **Development Runtime Stack** through Node.js Corepack, not as a mise-managed tool.
- Rich Neovim configuration belongs to the **Optional Editor Stack**, not the **Core Shell Stack**, and is opt-in for every machine profile.
- Antigravity CLI, Claude Code, and Codex belong to the **Optional AI Tool Stack**, not the **Core Shell Stack**.
- The **Optional AI Tool Stack** installs Homebrew casks `antigravity-cli`, `claude-code`, and `codex` on both supported machine profiles.
- `Brewfile.ai-cli-tools.tmpl` is the canonical cross-profile declaration for Optional AI Tool Stack packages.
- The **Modern CLI Provider** installs the 20 mandatory formulae declared in `Brewfile` on both supported profiles and owns the three Optional AI Tool Stack casks when that stack is enabled.
- The **VPS Shell Profile** installs Homebrew at `/home/linuxbrew/.linuxbrew` for every **Preset** on supported `x86_64` and `aarch64` systems.
- The **VPS Shell Profile** supports one non-root management user with initial sudo access; it does not manage multi-user Homebrew prefix ownership.
- The recommended **VPS Shell Profile** floor is 1 vCPU, 1 GiB RAM, and 3 GiB free disk, with 2 GiB RAM comfortable; the installer warns below 3 GiB but does not make this recommendation a hard gate.
- APT remains Ubuntu's **Bootstrap Package Manager** and installs only system and Homebrew bootstrap prerequisites; the **Modern CLI Provider** owns user-facing CLI tools, including Git after bootstrap.
- The **Development Runtime Manager** installs and selects Bun, Node.js, Python, and uv and does not own shared user-facing CLI tools.
- **Optional AI Tool Stack** installer failures do not block first-run declared-state apply; missing optional AI tools are reported by `tpod status` and `tpod doctor`.
- Homebrew bundle apply uses `HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade`; Terrapod apply restores missing declared packages without auto-updating or upgrading.
- `tpod status` and `tpod doctor` compare resolved AI commands with the active Homebrew prefix and warn when legacy commands shadow managed casks.
- When the **Optional AI Tool Stack** is disabled, `tpod apply` clears the optional AI CLI tools warning marker because those tools are no longer part of desired machine readiness.
- Existing mise-, APT-, and vendor-installed payloads are unmanaged legacy tools; Terrapod does not uninstall them automatically.
- Enabling only the **Optional AI Tool Stack** does not imply the **Optional Editor Stack** or **Optional Development Workspace**.
- Development-specific terminal layouts belong to the **Optional Development Workspace**, not the **Core Shell Stack**.
- Enabling the **Optional Development Workspace** also enables the **Optional Editor Stack** and **Optional AI Tool Stack**.
- The **Optional Development Workspace** is a stack bundle that takes precedence over disabled optional stack flags.
- Zellij and its general launcher alias belong to the **Core Shell Stack**, while development-specific Zellij layouts and aliases belong to the **Optional Development Workspace**.
- Disabling an optional stack excludes its files from management but does not remove files already present on a machine.
- Homebrew is the **Modern CLI Provider** on both supported profiles; APT prepares Ubuntu system/bootstrap state, and mise is the **Development Runtime Manager**.
- **Terrapod** applies this repository's declared dotfiles state; it does not act as the package manager for OS packages or mise-managed tool upgrades.
- **Terrapod** may install declared dependencies needed to reach the target state, but it does not run broad version upgrade commands such as `brew upgrade`, `apt upgrade`, or `mise upgrade`.
- After **Terrapod Setup** writes concrete settings, first-run declared-state apply should prioritize installing the **Terrapod** command surface and managed dotfiles so the machine reaches a recoverable state.
- First-run completion separates **Terrapod** installation from machine profile readiness: installing the command surface and managed dotfiles can succeed while the **Core Shell Stack** or **Development Runtime Stack** remains incomplete.
- Ubuntu package bootstrap failures can leave the **VPS Shell Profile** shell experience incomplete while **Terrapod** itself is installed; this is a machine profile readiness warning, not a first-run installer hard failure after **Terrapod Setup** succeeds.
- The first-run installer should not report a machine profile as ready when declared tool installation failed; it should complete with warning guidance and direct the user to `tpod doctor`.
- After the recovery core is validated, first-run warning completion exits with status 0 because **Terrapod** installation succeeded even though machine profile readiness remains incomplete.
- Scripted readiness checks should run `tpod doctor` rather than interpreting first-run warning completion as full machine readiness.
- Non-blocking first-run installer failures are recorded as Terrapod install warnings so first-run completion and `tpod doctor` can surface incomplete machine profile readiness without relying on brittle command-output parsing.
- Terrapod install warnings are machine-local recovery state stored outside the **Terrapod Source Repository** and managed dotfiles, under the user's XDG state area such as `${XDG_STATE_HOME:-$HOME/.local/state}/terrapod/install-warnings/<category>`.
- Terrapod install warnings are category-scoped markers that remain actionable until the same installer category completes successfully; interrupted or failed reruns must not hide the previous recovery signal.
- A successful rerun of an installer category clears that category's warning marker, while a failed rerun replaces it with the current failure summary and guidance.
- Mandatory stack warning markers such as Homebrew core, Ubuntu bootstrap, shell integrations, and mise tools are cleared only by successful reruns because their desired state cannot be disabled by optional settings.
- Optional stack or app-group warning marker content may be cleared or reduced when the corresponding desired optional setting is disabled.
- Terrapod install warnings are updated by both first-run installation and routine `tpod apply` because routine apply is the recovery path for previously failed installer categories.
- Terrapod install warning categories include stable filename slugs for Homebrew core bundle (`homebrew-core`), Homebrew desktop app bundle (`homebrew-desktop-apps`), Ubuntu bootstrap (`ubuntu-bootstrap`), shell integrations (`shell-integrations`), mise runtime tools (`mise-tools`), optional AI CLI tools (`optional-ai-cli-tools`), Jetendard fonts (`jetendard-font`), and Jetendard settings (`jetendard-settings`); best-effort UI nudges such as opening Karabiner do not need install warning markers.
- Terrapod install warning markers use shell-friendly key/value content with stable category, summary, guidance, and `updated_at` fields instead of free-form logs or captured stack traces.
- Terrapod install warning marker values stay single-line so shell parsing remains predictable; longer human-readable explanations belong in `tpod doctor` output.
- Terrapod install warning marker `updated_at` values use UTC ISO 8601 timestamps such as `2026-06-02T03:12:45Z`.
- Terrapod install warning marker writes should be atomic at the category file level, and marker clears remove only the matching category file.
- Terrapod install warnings do not include a retained installer log; recovery guidance should direct users to rerun the relevant apply path for fresh command output.
- Terrapod install warning marker path resolution, atomic write, timestamp creation, and clear behavior should be implemented through shared shell helper logic rather than duplicated independently in each installer script.
- Source-side installer scripts and installed `tpod` commands should share the same Terrapod install warning marker contract even if the helper file placement is decided during implementation.
- The optional AI CLI tools warning marker keeps one category for the **Optional AI Tool Stack** but includes the failed tool names in its summary or guidance fields.
- The Homebrew desktop app warning marker keeps one category for the **macOS Desktop App Stack** but includes failed cask names and, when available, their **macOS App Group** names in its summary or guidance fields.
- The Homebrew core warning marker includes failed formula or cask names only when they can be identified reliably; otherwise it records the core bundle failure summary and points users to the visible installer output and rerun guidance.
- Terrapod install warning marker detail should reflect reliable observations only; bulk installers may record bulk failure summaries when failed item names cannot be identified without brittle output parsing.
- `tpod doctor` treats current command availability checks and Terrapod install warning markers as separate signals; unresolved install warning markers remain actionable warnings even when some commands are currently available.
- `tpod doctor` is read-only for Terrapod install warning markers; it reports marker state but does not clear markers based on current command availability.
- `tpod doctor` exits non-zero when enabled machine-profile requirements are missing or unresolved Terrapod install warning markers remain.
- `tpod doctor` exits non-zero when required managed setup config keys are missing.
- `tpod doctor` does not fail merely because disabled optional stacks have missing tools.
- `tpod status` remains a human-readable snapshot command and exits successfully even when it reports warnings; automation should use `tpod doctor` for readiness gating.
- `tpod status` reports incomplete managed setup config in its Config section and points users to `tpod setup` or `tpod configure <Preset>` without failing.
- `tpod status` summarizes whether Terrapod install warnings are present and points to `tpod doctor`; `tpod doctor` prints category-level warning summary and guidance.
- `tpod apply` should surface remaining Terrapod install warnings after apply, while `tpod help` stays free of install warning state.
- `tpod apply` exit status reflects whether the declared-state apply command itself succeeded; unresolved install warning markers after apply are surfaced in output but do not make apply fail.
- `tpod doctor` recovery guidance points to `tpod apply` as the general retry path; category-specific retry commands are outside the current command surface.
- `mise-tools` install warning guidance covers failures while the **Development Runtime Manager** installs the declared runtime versions.
- First-run initial apply runs a forced recovery-core apply for managed shell startup files and the **Terrapod** command surface before the full declared-state apply.
- First-run recovery-core apply failure is a hard installer failure because users do not yet have a reliable `tpod` command surface for recovery.
- First-run recovery-core validation requires the installed `terrapod` executable, the installed `tpod` alias, and a successful `~/.local/bin/tpod help` invocation; file presence alone is not enough to mark the installer recoverable.
- First-run recovery-core command surface overwrite is allowed for existing Terrapod-managed or broken Terrapod command files, but non-Terrapod executables at `~/.local/bin/terrapod` or `~/.local/bin/tpod` stop installation with guidance instead of being backed up and overwritten.
- Terrapod command surface ownership detection is conservative: command files are Terrapod-owned only when they validate as Terrapod help output or clearly point to the **Terrapod Source Repository** command; ambiguous existing command files are treated as non-Terrapod conflicts.
- Terrapod command surface conflict guidance identifies the conflicting path and asks the user to move or remove it before rerunning the installer; Terrapod does not automatically rename non-Terrapod executables.
- Shell integration installation is outside the recovery core; shell integration failures may degrade prompt or plugin readiness but must not prevent the installed `tpod` command surface from working.
- First-run full declared-state apply may use keep-going behavior to write as much managed state as possible after the recovery core is installed.
- After the recovery core is installed and validated, first-run full apply failures should produce an incomplete-profile warning and still let the installer exit successfully so users can recover with `tpod`.
- First-run warning completion after full apply is limited to known non-blocking installer categories outside the recovery core; unknown chezmoi, template, or managed-file rendering failures remain hard installer failures.
- A full apply failure is treated as non-blocking only when the failing installer script explicitly records a Terrapod install warning marker; script names alone do not make failures recoverable.
- Installer scripts for known non-blocking categories record Terrapod install warning markers and exit successfully so chezmoi apply can continue; recovery-core failures and unknown failures must not use this marker-and-success contract.
- Installer scripts for known non-blocking categories continue attempting remaining items after an item fails when that is practical, and their warning markers accumulate the failed item names instead of stopping at the first failure.
- First-run full apply failure output should remain visible in the installer session; Terrapod install warning markers store summary and guidance, not captured command traces.
- First-run declared-state apply may non-interactively overwrite managed shell startup files such as `.zshenv`, `.zprofile`, and `.zshrc` after the user confirms **Terrapod Setup**, because those files define the managed shell environment.
- First-run forced shell startup overwrite is limited to the current recovery-core startup files: `.zshenv`, `.zprofile`, and `.zshrc`.
- Before first-run force-overwriting managed shell startup files that contain different existing user content, Terrapod should save lightweight user-visible backups so local shell customizations can be recovered manually.
- First-run shell startup file backups are timestamped and retained until the user removes them; Terrapod reports their paths but does not automatically merge or delete them.
- Routine `tpod apply` keeps the normal interactive chezmoi apply behavior instead of silently overwriting user-modified managed files.
- Machine-local PATH customizations should live in the managed zsh extension point rather than direct edits to managed shell startup files.
- Terrapod does not automatically migrate vendor-installer shell startup edits such as Antigravity PATH lines into the managed zsh extension point; first-run guidance should point users to the backup and extension point instead.
- After first-run completion, the installer should explain how to make `tpod` available in the current terminal because a child installer process cannot update the parent shell's `PATH`.
- First-run `tpod` availability guidance should include the absolute `~/.local/bin/tpod` command for immediate recovery and a login-shell refresh or new-terminal instruction for normal use.
- External package manager, runtime manager, shell integration, desktop app, and vendor tool installer failures during first-run declared-state apply should warn without blocking **Terrapod** command installation when the managed dotfiles can still be written.
- `tpod status` and `tpod doctor` own recovery visibility for missing tools after non-blocking first-run installer failures.
- `tpod update` delegates repository update semantics to `chezmoi update` and adds Terrapod-specific context and validation around it.
- README and command output describe `tpod update` as a source update so it is not confused with Homebrew, APT, or mise upgrades.
- README and command output describe `tpod diff` and `tpod apply` as declared-state operations delegated to chezmoi.
- After successful first-run apply, the installer should surface `tpod help` so users discover the day-to-day short command immediately.
- A clean first-run success does not need extra `tpod doctor` guidance beyond the installer's final `tpod help` output.
- When first-run completes with install warnings, the final installer output shows a separate warning block and the absolute `~/.local/bin/tpod doctor` recovery command in addition to surfacing `tpod help`.
- The **macOS Desktop App Stack** is opt-in because Homebrew casks can install shared applications and desktop support assets outside a single user's home directory.
- The **macOS Desktop App Stack** excludes Homebrew itself, shared CLI formulae such as mise and btop, and the Jetendard release installer owned by the **macOS Terminal Profile**.
- On shared Macs with multiple login users, Homebrew prefix ownership and permissions remain outside Terrapod's automatic repair scope because changing them can affect other users and shared applications.
- Homebrew permission failures for a second macOS login user should warn without blocking **Terrapod** command and managed dotfiles installation, with `tpod doctor` surfacing manual recovery guidance.
- Homebrew permission guidance should identify the unwritable shared prefix and ask the user to fix Homebrew ownership or administration outside Terrapod before rerunning `tpod apply`; Terrapod should not suggest or run broad `chown` repair commands.
- The **macOS Terminal Profile** installs every TTF from the latest stable `kuskhan/jetendard` GitHub release and verifies the asset digest published by GitHub instead of using a Homebrew font cask.
- The Jetendard installer queries GitHub only when its managed source changes or a failed installation is retried; ordinary `tpod status` and `tpod doctor` checks remain offline, and an upstream release alone does not trigger an upgrade.
- Jetendard installation records its tag, digest, and owned font files in a user-scoped manifest, and cleanup removes only obsolete files named by that manifest after a successful replacement.
- Jetendard app-setting management changes only font-family keys for Ghostty, Zed buffers and terminals, and initialized Orca terminal profiles; other app settings remain outside Terrapod ownership.
- Jetendard settings for Orca are deferred while Orca is running and require quitting Orca before rerunning `tpod apply`.
- The **VPS Shell Profile** excludes the Jetendard installer, app-setting adapter, status, and doctor checks.
- ADR 0009 supersedes only ADR 0001's Homebrew font-provider consequence; ADR 0001's Homebrew and mise ownership boundaries for all other tools remain unchanged.
- Enabling the **Optional Development Workspace** does not enable the **macOS Desktop App Stack**.
- **macOS App Groups** are configured during **Terrapod** setup and remain within the **macOS Desktop App Stack** boundary.
- When **macOS App Group** settings change, `tpod apply` keeps Homebrew desktop app warning marker content aligned with currently enabled groups; failures for disabled groups are removed from readiness warnings while enabled group failures remain.
- The implemented **macOS App Groups** are terminal-apps, automation, launcher, monitoring, and development-apps.
- The terminal-apps **macOS App Group** contains Ghostty.
- cmux is outside the declared **macOS Desktop App Stack**; existing cmux installs or settings may remain on a machine unmanaged and are not removed by **Terrapod**.
- The automation **macOS App Group** contains Hammerspoon and Karabiner-Elements.
- The launcher **macOS App Group** contains Raycast and 1Password CLI.
- The monitoring **macOS App Group** contains iStat Menus.
- The development-apps **macOS App Group** contains Zed and Orca ADE.
- The development-apps **macOS App Group** installs Homebrew casks `zed` and `stablyai/orca/orca`.
- The development-apps **macOS App Group** declares `stablyai/orca/orca` with `trusted: true` so Homebrew trusts only the Orca cask, not the entire `stablyai/orca` tap.
- Disabling the development-apps **macOS App Group** does not revoke an existing Orca cask trust entry; Terrapod leaves Homebrew trust removal to an explicit user action.
- `enableMacosAppGroupAiApps` is deprecated and is not an alias for `enableMacosAppGroupDevelopmentApps`; users must run explicit setup or configure migration.
- The Hammerspoon app launcher maps Codex Desktop to `1`, Claude Desktop to `2`, Antigravity 2.0 to `3`, Orca to `4`, and Antigravity IDE to `i`.
- Orca's bundled `orca` CLI remains an artifact of the development-apps **macOS App Group** and is not a member of the cross-profile **Optional AI Tool Stack**.
- Removing ChatGPT Atlas from the Hammerspoon app launcher is part of the planned launcher change.
- Individual macOS app toggles are excluded from the current **Terrapod** setup scope.
- Repository renaming makes `juty9026/terrapod` the canonical slug for the **Terrapod Source Repository** without adding legacy URL fallback behavior.
- Non-interactive setup options are deferred outside the current **Terrapod** installer and management command work, so **Terrapod Setup** may require an interactive terminal and the **Bootstrap UI Dependency**.
- README presentation should make **Terrapod** feel like a small product with a clear identity and a quick-start guide, not primarily like an operations manual.
- README still pairs the evocative **Terrapod** product promise with visible chezmoi and package-manager boundaries.
- README treats chezmoi as visible underlying machinery, not as the main story or default workflow.
- README should lead with a lightweight quick start that shows the first-run installer and a few normal management commands before platform details.
- README should show Terrapod's non-goals near the top as product boundaries, especially broad OS package upgrades, mise-managed upgrades, machine-local secrets, and untracked overrides.
- README should summarize what **Terrapod** carries near the top using domain concepts: machine profiles, **Preset** choices, optional stacks, and **macOS App Groups**.
- README should move deeper platform, Preset, optional stack, app-group, and update-boundary details below the product introduction and quick start.
- README section naming should support the product-first quick-start shape, using names such as Quick Start, What Terrapod Carries, Choose a Preset, What Terrapod Leaves Alone, Daily Commands, Platform Details, Local Overrides, Manual Restore, and Repository Conventions.
- README section titles and command examples use canonical domain terms such as **Preset**, while product metaphor stays in supporting explanatory copy.
- README should explain **Preset** choices by the kind of machine they suit before listing the concrete optional stack and app-group settings they expand into.
- The **Canonical README** is the source of truth for README content.
- The **Korean README** follows the **Canonical README**, but it does not need to label itself as a translation.
- The **Korean README** lives at `README.ko.md`.
- The **Korean README** keeps canonical domain terms such as **Terrapod**, **Preset**, optional stack names, and **macOS App Group** in English while explaining them in natural Korean.
- The **Korean README** keeps section headings in English to mirror the **Canonical README** heading structure directly.
- The **Korean README** translates body copy, table headers, and list explanations into natural Korean while preserving command names, config keys, file names, literal values, and canonical domain terms in English.
- The **Canonical README** and **Korean README** use a compact globe-marked language switcher directly below the `# Terrapod` heading, with the current language bolded and the other README linked.
- Changes to the **Canonical README** should make heading-structure drift in the **Korean README** visible during maintenance.
- README drift checks compare corresponding section headings only; they do not enforce paragraph, table, or code block parity.
- README drift checks compare all Markdown heading lines from `README.md` and `README.ko.md` for exact text and order.
- `tpod help` may carry a concise product introduction, but routine command output stays operational and scan-friendly.
- `tpod help` introduces **Terrapod** as a small landing pod for dotfiles and immediately states that chezmoi remains underneath while package-manager upgrades stay outside scope.
- Routine command output uses stable labels such as Profile, Config, Preflight, Delegating, Post-apply validation, and Guidance, with visual alignment and terminal color when they improve scanning without obscuring copied logs.
- Routine command stage labels may be polished when the result stays concise, stable, and clear.
- Routine command visual treatment applies to `tpod help`, `tpod status`, `tpod doctor`, `tpod diff`, `tpod apply`, and `tpod update`, without emoji or a separate plain-mode environment variable.
- Routine command color is enabled only for compatible terminal output and disabled for non-TTY output, `TERM=dumb`, or `NO_COLOR`.
- Routine command color uses cyan or bold for titles and sections, green for successful or enabled states, yellow for warnings and missing/unsupported states, red for failed states, and neutral styling for paths, commands, and values.
- `tpod apply` should stay focused on declared-state apply context, preflight, delegation, and post-apply validation; it should not expand into a rich installed-tool report in the current scope.
- The first-run **Terrapod** installer uses a gum-backed terminal presentation for initial setup prompts such as **Preset** selection.
- The gum-backed first-run installer presentation is a required interactive path; non-TTY, dumb terminal, scripted, and missing-gum environments fail with guidance until non-interactive setup options are designed.
- Rich **Terrapod Setup** presentation may use setup-only emoji, color, spacing, and aligned prompt layout when it improves first-run review clarity.
- Routine command visual treatment is separate from rich **Terrapod Setup** presentation; Setup remains its own gum-backed UI.
- Error output avoids product metaphor and states the failed condition plus the next useful action.

## Example Dialogue

> **Dev:** "Should the VPS just reuse the macOS terminal setup?"
> **Domain expert:** "No. The **VPS Shell Profile** should share the Homebrew-managed **Core Shell Stack** and mise-managed **Development Runtime Stack**, while excluding macOS-only applications."

## Flagged Ambiguities

- "VPS shell experience" means the **VPS Shell Profile**, not a full desktop or GUI environment.
- "development environment" can mean runtimes, editor configuration, or AI tools; resolved here as **Optional Editor Stack** when discussing opt-in LazyVim-style editor configuration.
- "dev layout" means the **Optional Development Workspace**, not the baseline Zellij installation.
- "preset" means a **Preset** in first-run setup unless specifically referring to the **Optional Development Workspace** stack bundle.
- "dotfiles command" means **Terrapod** unless discussing legacy naming or chezmoi internals.
- "dotfiles repository" means the **Terrapod Source Repository** unless discussing the unsupported legacy `juty9026/dotfiles` slug.
- "colorful output" can mean first-run installer prompt polish or routine command log styling; resolved here as first-run installer prompt polish only.
- "plain fallback" previously meant preserving a separate text-only **Terrapod Setup** path for non-TTY or missing UI tools; resolved: first-run **Terrapod Setup** now requires the **Bootstrap UI Dependency** and fails with guidance instead.
