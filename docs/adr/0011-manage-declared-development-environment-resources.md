# Manage declared development environment resources

Terrapod is a Personal Development Environment Manager, not only a bootstrap
layer or a chezmoi wrapper. It converges a signed, declared environment across
the `macos-terminal` and `vps-shell` profiles.

## Decision

- A verified release catalog assigns every managed resource a stable resource
  ID, type, provider, dependencies, version policy, and declared root.
- Terrapod owns only resources backed by an ownership receipt under a declared
  root. It never mutates undeclared packages or paths.
- A declared resource found pre-existing is either transferred completely into
  Terrapod ownership after verification or marked `unavailable`. Terrapod does
  not create a partially managed state.
- Signed releases are the runtime authority. The active release is separate
  from an authoring checkout and contains the immutable source archive, typed
  resource catalog, release manifest, and signatures.
- Typed adapters inspect, plan, execute, verify, transfer, and prune packages,
  runtimes, fonts, Git checkouts, and managed files. Stable resource IDs bind
  catalog intent, operations, journals, and ownership receipts.
- chezmoi is a script-free managed-files engine. Terrapod invokes it with the
  active signed source, independent Terrapod config, fixed destination,
  `--exclude scripts`, and `--override-data-file`. Direct access is constrained
  to read-only commands.
- Machine-local configuration lives in
  `~/.config/terrapod/config.json`, independently of chezmoi data.
- A resource is `ready` or `unavailable`. An unavailable resource blocks its
  dependent operations without mutating them. Managed-file conflicts require
  explicit `tpod resolve <resource-id>`.
- Apply and update automatically prune resources that Terrapod owns but that
  are no longer desired. Provider adapters preserve the ownership boundary;
  Homebrew cask removal never uses `brew uninstall --zap`.
- Legacy activation is a maintainer-only, one-shot `install.sh --migrate`
  operation. It converts legacy config, imports eligible ownership, reconciles
  the signed catalog, and removes the legacy source only after verification.
  New installations and routine updates do not run this migration.

## Consequences

- `tpod plan` is the read-only preview; `tpod apply` reconciles the active
  release; `tpod update` verifies and activates a signed release before
  reconciliation; `tpod resolve <resource-id>` handles explicit conflicts.
- Terrapod may install, upgrade, transfer, or remove its owned resources, but
  leaves packages and files it never owned untouched.
- Package-source conflicts that cannot be transferred safely make the resource
  unavailable and require the reported external cause to be corrected.
- A broken Management Core is restored with the versioned
  `install.sh --repair` flow without applying resources or rewriting config.

## Supersession

This decision supersedes only the conflicting consequences of earlier ADRs;
their historical context remains intact:

- ADR 0002's rule that disabling an optional stack never removes its files.
- ADR 0003's bootstrap-oriented wrapper position and unrestricted direct
  chezmoi escape hatch.
- ADR 0007's mutable checked-out source and shell-script recovery model.
- ADR 0008's restore-only Homebrew apply and no-uninstall rule.
- ADR 0009's bespoke script/manifest ownership model for Jetendard.
- ADR 0010's Brewfile-centered, no-upgrade, no-removal reconciliation model.
