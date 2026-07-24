# Keep package source migration advisory-only

Terrapod installs active package declarations through their canonical providers but does not remove alternate installations. After installation, Terrapod performs a read-only check of canonical provider presence, the expected executable, and the executable selected first on PATH. If the canonical executable exists but another file is primary, `tpod apply`, `tpod status`, and `tpod doctor` report the actual and canonical paths as an advisory.

This decision supersedes ADR 0011's ownership registry, deep alternate-provider scan, removal plan, confirmation, lock, and migration warning state. It restores ADR 0010's non-destructive apply contract. ADR 0011's `tpod update` handoff to the refreshed `tpod apply` and the first-run installer's use of the installed `tpod apply` remain in force.

## Considered Options

- Keep verified automatic removal: rejected because provider provenance, shared ownership, package-manager dependencies, runtime selections, application artifacts, and machine-local PATH policy create too many unsafe or ambiguous cases.
- Keep the alternate-provider inventory but make it read-only: rejected because maintaining provenance rules and secondary duplicate detection retains most of the complexity without improving installation.
- Check only command availability: rejected because it would hide cases where Terrapod installed the canonical executable but another installation remains primary.

## Consequences

- `tpod apply` and `tpod update` attempt installation only; they do not prompt for or execute package removal.
- Active command-bearing declarations include the Core Shell Stack, Development Runtime Stack, enabled Optional AI Tool Stack, and 1Password CLI when its macOS App Group is enabled.
- Canonical provider absence, canonical executable absence, and an enabled command unavailable on PATH fail `tpod doctor` but do not change the recovery-oriented exit semantics of `tpod apply`, `tpod update`, or `tpod status`.
- A different primary executable is advisory-only when the canonical executable exists. Exact path matches and symlinks resolving to the same file are canonical.
- Advisory output includes the actual and canonical paths and provenance-neutral manual guidance. Terrapod does not infer the alternate installer or suggest an uninstall command.
- Secondary PATH copies are not scanned, advisory state is not persisted, and the `managed-package-migration` warning category no longer exists.
- Nonstandard Homebrew prefixes and explicit `~/.config/zsh/path.d` overrides remain advisory-only and user-managed.
