# Reconcile Managed Package sources during apply

Every active effective Terrapod package declaration is a Managed Package. Terrapod uses an explicit ownership registry as the single source of truth for canonical provider/package identity, exact alternate identities, protected prerequisites, known vendor state, and verification rules.

`tpod apply` first installs canonical declared state with the existing no-upgrade contract, then performs a read-only deep scan, renders one migration plan, asks once with a default-No confirmation, removes only verified safe legacy payloads, and verifies the result. It records declined, skipped, failed, or unresolved work under the `managed-package-migration` install warning category without making apply fail. Reconciliation has no permanent completion flag and is serialized by an XDG state lock.

`tpod update` first runs `chezmoi update --exclude scripts`, then hands off to the refreshed `~/.local/bin/tpod apply` process. First-run installation joins the same flow after recovery-core installation by invoking the installed `tpod apply`. Raw `chezmoi apply` and `tpod chezmoi -- apply` remain intentional bypasses.

This decision supersedes ADR 0010's non-destructive apply consequence and its choice to leave all legacy payloads unmanaged. It also supersedes the source-only `tpod update` decision recorded in the domain context. ADR 0010's canonical Homebrew and mise ownership assignments and its `brew bundle --no-upgrade` contract remain in force.

## Considered Options

- Keep warning-only legacy detection: rejected because canonical installation alone does not resolve secondary duplicates or safely converge package ownership.
- Remove every duplicate automatically: rejected because unresolved provenance, shared ownership, OS dependencies, nonstandard prefixes, and project-local runtime selections require conservative protection.
- Record a migration-completed flag: rejected because package state can drift after any apply.
- Make raw chezmoi paths migration-aware: rejected because advanced recovery requires an intentional bypass.

## Consequences

- Active Core Shell Stack, Development Runtime Stack, enabled Optional AI Tool Stack, enabled macOS App Groups, and Jetendard declarations enter the registry; disabled optional declarations do not.
- Scanning probes only locally available providers and is read-only. Exact registry identities are required; fuzzy package-name matching is forbidden.
- Standard current-user-owned provider payloads may be removed after confirmation. APT is the only approved sudo exception and additionally requires exact identity, manual-install state, non-prerequisite status, and a no-cascade simulated transaction.
- Package managers, OS system runtimes, protected prerequisites, shared/root-owned payloads, nonstandard Homebrew prefixes, unknown PATH copies, config/data/cache/session state, cask zap, and apt autoremove are never removed automatically.
- Canonical mise runtime versions and project-local runtime selection are preserved.
- Existing macOS app artifacts rely on Homebrew Cask adoption. An artifact that Homebrew cannot adopt remains a manual action and is not moved to Trash.
- `~/.config/zsh/path.d` remains the highest-priority machine-local PATH override. Terrapod installs the canonical package but reports the override as advisory and does not remove it.
- `tpod doctor` fails for missing canonical packages or a legacy primary executable on the managed default PATH. Secondary duplicates, unknown provenance, and machine-local PATH overrides are advisory.
- `tpod status` remains a fast snapshot of canonical, shadowed, and pending counts; `apply`, `update`, and `doctor` perform the full deep scan.
