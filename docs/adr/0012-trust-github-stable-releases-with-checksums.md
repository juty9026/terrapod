# Trust GitHub stable releases with checksums

## Context

Terrapod's first Management Core implementation introduced an independent
Ed25519 release root, signed manifests, persisted trust proofs, and key
rotation. That model protects against a compromise of GitHub release
publishing authority, but it adds key custody and substantial implementation
surface.

Terrapod currently has one maintainer and effectively one user. At this stage,
the separate signing root is disproportionate to the operating model.

## Decision

Terrapod trusts Stable Releases published by the canonical
`juty9026/terrapod` GitHub repository over HTTPS.

The stable release manifest remains mandatory. It binds the stable version,
schema versions, exact asset set, byte sizes, and SHA-256 digests. Terrapod
continues to enforce its GitHub tag/version binding, HTTPS and redirect host
allowlist, bounded downloads, checksum verification, catalog binding,
self-check, and atomic activation.

Terrapod does not publish or require an application-level manifest signature,
compiled release root, persisted trust proof, or signing key.

## Consequences

- Release publication requires no private-key custody or GitHub signing secret.
- Corrupt, incomplete, mismatched, or unexpectedly hosted assets remain
  rejected.
- A party controlling the canonical GitHub repository's release publishing
  authority can replace both the manifest and its assets. This risk is
  explicitly accepted for the current operating model.
- Resource authorization still comes from the active release-bound Resource
  Catalog and Declared Roots. Removing signature verification does not broaden
  adapter mutation scope or ownership.

## Supersession

This decision supersedes only the signing-specific parts of ADR 0011:

- the requirement that release manifests and catalogs are cryptographically
  signed
- the requirement that `tpod update`, repair, and migration verify an
  application-level signature

ADR 0011's release isolation, Resource Catalog authority, ownership,
reconciliation, migration safety, and repair boundaries remain in force.
