# Final Whole-Branch Review Fix Report

## Status

Complete. The Important exact-eight GitHub Release boundary and both Minor findings are implemented with focused regression coverage.

## Implementation

- `Manifest` validation now binds every platform/kind to the exact canonical six filenames.
- `Client.LatestStable` now requires exactly the canonical eight GitHub Release assets.
- GitHub metadata size must be positive for all eight assets.
- Every asset URL, including `release.json` and `install.sh`, must use HTTPS and an allowed host.
- `release.json` bytes must exactly match its positive GitHub metadata size.
- The legacy bridge curl invocation pins HTTPS for the initial request and every redirect with `--proto '=https' --proto-redir '=https'`.
- Active migration test wording now says `release-bound`; historical ADRs and negative signing assertions were not changed.
- Existing strict JSON/manifest validation, tag/version binding, response limits, artifact size/checksum binding, pinned `v1.0.0`, temp-file protection, retry/restore behavior, recursion guard, argument forwarding, and migration retry safety remain intact.

## Changed files

- `internal/release/manifest.go`
- `internal/release/manifest_test.go`
- `internal/release/client.go`
- `internal/release/client_test.go`
- `dot_local/bin/executable_terrapod.tmpl`
- `tests/legacy_update_transition_test.sh`
- `tests/terrapod_manager_migration_test.sh`
- `internal/migrate/legacyownership_test.go`

## RED evidence

Command:

```sh
mise exec go@1.26.0 -- go test ./internal/release -count=1 -run 'TestParseManifestRequiresExactCanonicalAssetNames|TestLatestStableRequiresExactCanonicalGitHubAssets|TestLatestStableRequiresAllowedHTTPSMetadataAssetURLs' -v
```

Relevant output (exit 1):

```text
missing_install.sh: err=<nil>, want "exactly eight"
extra_asset: err=<nil>, want "exactly eight"
zero-size_release.json: err=<nil>, want "size"
zero-size_install.sh: err=<nil>, want "size"
install.sh_disallowed_host: err=<nil>, want "not allowed"
all six canonical manifest filename subtests: err=<nil>, want canonical asset name rejection
FAIL github.com/juty9026/terrapod/internal/release
```

Command:

```sh
sh tests/legacy_update_transition_test.sh
```

Relevant output (exit 1):

```text
terrapod transition: v1.0.0 is not available; retry this command after the release is published
not ok - second legacy invocation migrates and forwards to the manager
```

An earlier combined formatting/test attempt stopped before tests because the bare `gofmt` shim had no selected Go version. It was corrected to `mise exec go@1.26.0 -- gofmt`; this environment error is not counted as RED.

## GREEN evidence

Focused boundary command:

```sh
mise exec go@1.26.0 -- go test ./internal/release -count=1 -run 'TestParseManifestRequiresExactCanonicalAssetNames|TestLatestStableRequiresExactCanonicalGitHubAssets|TestLatestStableRequiresAllowedHTTPSMetadataAssetURLs' -v
```

Output (exit 0):

```text
--- PASS: TestLatestStableRequiresExactCanonicalGitHubAssets
--- PASS: TestLatestStableRequiresAllowedHTTPSMetadataAssetURLs
--- PASS: TestParseManifestRequiresExactCanonicalAssetNames
PASS
ok github.com/juty9026/terrapod/internal/release
```

Required focused verification:

```sh
mise exec go@1.26.0 -- go test ./internal/release -count=1
# ok github.com/juty9026/terrapod/internal/release 3.902s

sh tests/legacy_update_transition_test.sh
# ok - guided legacy update transition

mise exec go@1.26.0 -- go test ./internal/migrate -count=1 -run 'TestLoadLegacyBaselineValidatesCompleteCatalog|TestPlanLegacyOwnership' -v
# five selected tests PASS; ok github.com/juty9026/terrapod/internal/migrate

sh tests/terrapod_manager_migration_test.sh
# ok - Terrapod manager migration contract

git diff --check
# exit 0, no output
```

No migration, installer, or `chezmoi apply` was run against `/Users/minu` or a real home. The legacy and migration shell tests use their existing temporary HOME/fixture isolation.

## Self-review

- Exact count plus canonical-name membership and duplicate rejection proves the GitHub asset set is exactly the required eight names.
- Manifest validation binds names to their corresponding binary platform/source/catalog semantics, not only to a six-name set.
- All eight positive sizes and allowed HTTPS URLs are checked before downloading the manifest.
- Existing manifest artifact size checks, tag/version checks, limits, redirect allowlist, strict parsing, and checksum validation remain unchanged.
- The curl contract assertion verifies the pinned URL and both protocol restrictions while preserving output path and test behavior.
- Diff is limited to the eight requested files; no signing key, signature flow, configurable host, or checksum service was introduced.

## Concerns

No known concerns. The controller will run the full branch suite separately after review, as requested.
