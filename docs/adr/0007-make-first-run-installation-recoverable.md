# Make first-run installation recoverable

First-run Terrapod installation should prioritize reaching a recoverable state after **Terrapod Setup** has written concrete settings. The installer will resume from an existing checked-out **Terrapod Source Repository**, force-apply only the recovery core of managed shell startup files and the **Terrapod** command surface, then run the full declared-state apply while recording non-blocking package, runtime, shell integration, desktop app, and vendor tool installer failures as Terrapod install warnings. This supersedes ADR 0003's consequence that the installer stops whenever the default chezmoi source directory already exists.

## Consequences

- **Terrapod** installation and machine profile readiness are separate outcomes: `tpod` and managed dotfiles can be installed while the **Core Shell Stack**, **Development Runtime Stack**, **macOS Desktop App Stack**, or **Optional AI Tool Stack** remains incomplete.
- `tpod doctor` owns recovery visibility for incomplete machine profile readiness after non-blocking first-run installer failures.
- Routine `tpod apply` keeps normal chezmoi apply behavior instead of silently overwriting user-modified managed files.
- Terrapod does not automatically repair shared Homebrew prefix ownership or permissions on multi-user Macs.
