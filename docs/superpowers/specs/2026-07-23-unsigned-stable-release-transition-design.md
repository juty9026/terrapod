# Unsigned Stable Release and Legacy Update Transition Design

## Goal

Publish Terrapod `v1.0.0` without application-level Ed25519 signing while
preserving release integrity checks, atomic activation, and a guided transition
from the currently installed legacy `tpod` command.

## Product Decision

Terrapod trusts stable assets published by the canonical
`juty9026/terrapod` GitHub repository over HTTPS. It does not maintain a
separate application-level signing root.

This accepts the risk that an attacker controlling the GitHub repository or its
release publishing authority can replace both a manifest and its assets. The
trade-off is appropriate for the current single-maintainer, effectively
single-user stage of the project.

Terrapod still rejects incomplete, corrupt, inconsistent, or unexpectedly
hosted releases.

## Domain Language

`Signed Release` becomes `Stable Release`: a versioned source archive,
platform binaries, and a release-bound Resource Catalog described by a stable
release manifest.

`signed declaration`, `signed catalog`, and similar authorization language
become `release-bound declaration`, `release catalog`, or `declared resource`
as appropriate. Resource ownership and Declared Root boundaries do not weaken;
only the independent cryptographic signing layer is removed.

This design updates `CONTEXT.md` and adds ADR 0012 to supersede the
signing-specific parts of ADR 0011. Historical ADR text remains intact.

## Stable Release Contract

The Release workflow publishes:

- `tpod-darwin-amd64`
- `tpod-darwin-arm64`
- `tpod-linux-amd64`
- `tpod-linux-arm64`
- `terrapod-source.tar.gz`
- `resources.json`
- `release.json`
- rendered `install.sh`

The workflow no longer publishes `release.json.sig` and no longer requires:

- `RELEASE_ROOT_KEY_ID`
- `RELEASE_ROOT_PUBLIC_KEY`
- `RELEASE_SIGNING_PRIVATE_KEY`

`release.json` remains canonical release metadata. It binds:

- stable SemVer
- supported catalog and state schema versions
- the exact platform binary set
- one source archive
- one Resource Catalog
- each asset's filename, byte size, and SHA-256 digest

Trusted-key additions and trust-rotation metadata are removed from the
manifest.

## Release Validation

The release client:

1. Fetches the latest GitHub Release through the canonical GitHub API endpoint.
2. Requires a non-draft, non-prerelease stable tag.
3. Downloads `release.json` from an allowed HTTPS host.
4. Strictly decodes and structurally validates the manifest.
5. Requires the GitHub tag to equal `v` plus the manifest version.
6. Requires GitHub asset names and sizes to match the manifest.
7. Downloads each required asset from an allowed HTTPS host.
8. Verifies every downloaded asset's size and SHA-256 digest before staging.

The existing response limits, redirect allowlist, safe filenames, regular-file
checks, cache permissions, source archive checks, catalog/version binding,
self-check, and atomic activation remain.

`VerifiedRelease` may continue to mean a release whose metadata and assets
passed this validation. It must not imply cryptographic authenticity.

## Simplification Boundary

Remove the complete application-level signing path rather than bypassing it:

- Ed25519 manifest verification
- compiled release root build flags and placeholders
- signature envelope parsing
- persisted trust proofs and key rotation
- signing-specific update record fields
- signing workflow steps, variables, and secrets
- OpenSSL 3 requirement used only for Ed25519 verification

Keep release manifest digests where they bind staged files, journals, and
migration resumability. These are integrity and transaction identifiers, not
proofs of publisher identity.

The installer uses the platform's normal SHA-256 utility (`shasum -a 256` on
macOS or `sha256sum` on Ubuntu) to validate assets.

## Legacy `tpod update` Transition

### Existing Constraint

The currently installed legacy command runs:

```text
chezmoi update --exclude scripts
```

Its running shell process cannot execute code that the same update installs.
Therefore one legacy invocation cannot safely complete the manager transition.

### First Invocation

The new `main` source reintroduces a small transition bridge for legacy
chezmoi data only.

The bridge is a chezmoi template. During the first legacy `tpod update`, its
rendering calls `warnf` and prints an exit-zero message equivalent to:

```text
Terrapod manager transition is ready.
Run `tpod update` once more to complete it automatically.
```

The manager's independent chezmoi data has a root `terrapod` object. The
`.chezmoiignore` contract excludes the transition bridge whenever that object
exists, so an active Go manager never manages or overwrites its own launchers.

### Second Invocation

When the transition bridge runs, it:

1. Preserves the original command arguments.
2. Refuses root execution.
3. Downloads the version-pinned
   `https://github.com/juty9026/terrapod/releases/download/v1.0.0/install.sh`
   to a mode-restricted temporary file.
4. Runs the existing migration transaction internally.
5. Confirms that migration replaced the bridge with the manager launcher.
6. Re-executes the original command through the new launcher.

For `tpod update`, the visible result is that the second invocation migrates
and then runs the Go manager's normal stable-release update flow. Any other
`tpod` command also migrates first and then continues with its original
arguments.

The user never needs to type `install.sh --migrate`. The installer migration
mode remains as an internal recovery primitive.

## Transition Failure Behavior

- If `v1.0.0` is not published yet, the bridge reports that the release is not
  available and remains installed for retry.
- If download, validation, preflight, or migration fails, the legacy source,
  bridge, recovery data, and resumable migration marker remain available.
- A retry runs the same command again and resumes through the existing
  migration transaction.
- A recursion guard stops if migration reports success without replacing the
  bridge.
- The bridge never deletes the legacy source itself. Only the verified
  migration transaction finalizes the source after config conversion,
  ownership import, reconciliation, and postcondition verification.

## Documentation

Update the English and Korean READMEs to describe:

- GitHub/HTTPS and SHA-256 as the Stable Release trust model
- release assets without `release.json.sig`
- automatic legacy transition through two guided `tpod` invocations
- the explicit migration command only as recovery guidance, if retained

Remove claims that releases, catalogs, Management Core binaries, or desired
state are Ed25519-signed.

## Tests

### Release and Update Tests

- accept a valid unsigned stable manifest
- reject malformed or trailing manifest JSON
- reject unstable versions and GitHub tag/version mismatch
- reject missing, duplicate, undeclared, or size-mismatched assets
- reject non-HTTPS and unapproved asset hosts
- reject downloaded asset size or SHA-256 mismatch
- preserve catalog/version and staged-release bindings
- preserve downgrade rejection, self-check, journaling, handoff, and atomic
  activation
- prove no trust proof or signing configuration remains required

### Installer and Workflow Tests

- render an installer with only the release base placeholder
- validate asset checksums without Ed25519 or OpenSSL 3
- publish the exact expected eight assets
- assert the workflow has no signing variables, signing secret, signature
  generation, or `release.json.sig`
- retain repair and migration failure safety

### Legacy Bridge Tests

Use fixture homes and repositories only:

- the first legacy update renders the bridge and prints the second-run warning
- the first update exits successfully without running migration
- manager-mode chezmoi data ignores the bridge
- the second `tpod update` downloads the exact `v1.0.0` installer
- the bridge invokes migration internally and forwards the original arguments
- successful migration re-executes the Go manager command
- unavailable release and failed migration remain retryable
- a non-replaced bridge triggers the recursion guard

No test or implementation step runs migration against the maintainer's real
home.

## Delivery Sequence

1. Implement this design in an independent Orca Workspace based on merged
   `origin/main`.
2. Run Go tests, all shell contract tests, `git diff --check`, and independent
   review.
3. Merge the implementation through a new PR.
4. Refresh `main` and confirm no signing variables or secrets are required.
5. Create `v1.0.0` at the merged commit.
6. Monitor the Release workflow to completion.
7. Verify the eight assets, rendered installer placeholders, checksums, and
   legacy bridge download target.
8. Do not run the maintainer's real-home migration without fresh explicit
   authorization.
