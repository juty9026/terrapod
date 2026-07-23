# Unsigned Stable Release Transition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove application-level Ed25519 release signing, retain GitHub/HTTPS plus manifest checksum validation, and give legacy users a guided two-invocation `tpod` transition to the Go manager.

**Architecture:** A Stable Release is validated from strict `release.json` metadata, canonical GitHub tag/asset metadata, allowed HTTPS hosts, byte sizes, and SHA-256 digests. The signing root, signature envelope, trust-proof persistence, and signing workflow disappear. A chezmoi-managed legacy bridge prints a warning during the first old `tpod update`, then the next `tpod` invocation runs the version-pinned migration internally and re-executes the original command through the Go launcher.

**Tech Stack:** Go 1.26.0, POSIX shell, chezmoi templates, GitHub Actions, GitHub Releases.

## Global Constraints

- Work only in `/Users/minu/orca/workspaces/terrapod/terrapod-unsigned-v1-release`, based on merged `origin/main` commit `1441dd9375cec686ee802acdcb6cbf76b4c09cd3`.
- The first release remains exactly `v1.0.0`.
- Trust only the canonical `juty9026/terrapod` GitHub Release over HTTPS; do not add another key, token, checksum service, or configurable release host.
- Keep stable tag/version binding, allowed HTTPS hosts, response limits, exact asset set, byte sizes, SHA-256 digests, catalog/version binding, self-check, downgrade prevention, resumable journals, and atomic activation.
- Remove `release.json.sig`, `RELEASE_ROOT_KEY_ID`, `RELEASE_ROOT_PUBLIC_KEY`, `RELEASE_SIGNING_PRIVATE_KEY`, compiled release roots, trust proofs, and key rotation completely.
- The Release workflow publishes exactly eight assets: four platform binaries, `terrapod-source.tar.gz`, `resources.json`, `release.json`, and rendered `install.sh`.
- The legacy bridge downloads exactly `https://github.com/juty9026/terrapod/releases/download/v1.0.0/install.sh`.
- The first legacy `tpod update` must print an exit-zero instruction to run `tpod update` once more; it must not run migration.
- The next legacy `tpod` invocation must migrate internally, verify launcher replacement, and forward the original arguments.
- Do not run migration against `/Users/minu` or any real home. Migration tests use fixture homes only.
- Keep changes surgical: no unrelated refactors, dependency additions, or formatting changes.

---

### Task 1: Replace signed release verification with stable manifest validation

**Files:**
- Modify: `internal/release/manifest.go`
- Modify: `internal/release/manifest_test.go`
- Modify: `internal/release/client.go`
- Modify: `internal/release/client_test.go`
- Delete: `internal/release/trust.go`
- Delete: `internal/release/trust_test.go`
- Delete: `internal/release/testdata/release.json.sig`
- Modify: `internal/release/testdata/release.json`
- Modify: `internal/release/stage.go`
- Modify: `internal/release/stage_test.go`
- Modify: `internal/update/update.go`
- Modify: `internal/update/update_test.go`
- Modify: `internal/state/store.go`
- Modify: `internal/state/store_test.go`
- Modify: `cmd/tpod/main.go`
- Modify: `cmd/tpod/main_test.go`
- Modify: `cmd/tpod/composition.go`
- Modify: `cmd/tpod/migration.go`
- Modify: `cmd/tpod/migration_test.go`
- Modify: `cmd/release-manifest/main.go`
- Modify: `cmd/release-manifest/main_test.go`

**Interfaces:**
- Produce: `func ParseManifest(data []byte) (Manifest, error)`.
- Produce: `func NewLocalVerifiedRelease(manifestData []byte, files map[string]string) (VerifiedRelease, error)`.
- Preserve: `Client.LatestStable(context.Context) (VerifiedRelease, error)`.
- Preserve: `Stager.Stage`, `Stager.Load`, `Stager.LoadActive`, `Stager.RepairAndActivate`, `update.Run`, and `update.Continue` behavior except trust-key handling.
- Remove: `Verifier`, `TrustProof`, `PersistedTrust`, `Manifest.TrustedKeys`, `Stager.Verifier`, and all compiled-root parameters.

- [ ] **Step 1: Change manifest and client tests to describe unsigned validation**

Replace signature fixtures with manifest-only fixtures. Add focused tests with these contracts:

```go
func TestParseManifestAcceptsStableManifest(t *testing.T) {
	data := stableManifestJSON(t)
	manifest, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "1.2.3" {
		t.Fatalf("version=%q", manifest.Version)
	}
	if _, err := manifest.Digest(); err != nil {
		t.Fatal(err)
	}
}

func TestParseManifestRejectsMalformedTrailingAndInvalidAssets(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		[]byte(`{"version":"1.2.3"}` + "\n{}"),
		manifestWithDuplicateAsset(t),
		manifestWithWrongPlatformSet(t),
	} {
		if _, err := ParseManifest(data); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
}

func TestLatestStableRequiresManifestButNoSignature(t *testing.T) {
	server, client := githubReleaseFixture(t, []string{"release.json"})
	client.Endpoint = server.URL + "/latest"
	if _, err := client.LatestStable(context.Background()); err != nil {
		t.Fatal(err)
	}
}
```

Update staging tests to expect `release.json` but no `release.json.sig`. Update manifest generator tests to assert that serialized JSON has no `trustedKeys` field.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
mise exec go@1.26.0 -- go test ./internal/release ./cmd/release-manifest ./internal/update ./internal/state ./cmd/tpod -count=1
```

Expected: FAIL because `ParseManifest` does not exist, signature assets and `TrustedKeys` are still required, and trust fields still participate in update state.

- [ ] **Step 3: Implement strict unsigned manifest parsing**

Change the manifest shape to:

```go
type Manifest struct {
	Version       string  `json:"version"`
	CatalogSchema int     `json:"catalogSchema"`
	StateSchema   int     `json:"stateSchema"`
	Assets        []Asset `json:"assets"`

	verified       bool
	manifestDigest string
}

func ParseManifest(data []byte) (Manifest, error) {
	if len(data) == 0 || len(data) > MaxManifestSize {
		return Manifest{}, fmt.Errorf("release manifest size is outside 1..%d bytes", MaxManifestSize)
	}
	var manifest Manifest
	if err := decodeStrict(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, fmt.Errorf("validate release manifest: %w", err)
	}
	digest := sha256.Sum256(data)
	manifest.verified = true
	manifest.manifestDigest = hex.EncodeToString(digest[:])
	return manifest, nil
}
```

Move the existing stable SemVer, schema range, exact platform set, singleton source/catalog, safe filename, size, digest, and duplicate-name checks into `validateManifest`. Delete trusted-key validation and Ed25519 imports.

Change the local constructor to:

```go
func NewLocalVerifiedRelease(manifestData []byte, files map[string]string) (VerifiedRelease, error) {
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return VerifiedRelease{}, err
	}
	value := VerifiedRelease{
		Manifest:     manifest,
		Files:        make(map[string]string, len(files)),
		manifestData: append([]byte(nil), manifestData...),
	}
	for name, path := range files {
		if !assetNamePattern.MatchString(name) || path == "" {
			return VerifiedRelease{}, fmt.Errorf("invalid local release asset %q", name)
		}
		value.Files[name] = path
	}
	if err := value.sealManifest(); err != nil {
		return VerifiedRelease{}, err
	}
	return value, nil
}
```

The in-memory seal hashes the normalized manifest and exact manifest bytes only.

- [ ] **Step 4: Remove signature fetching and trust persistence**

In `Client.LatestStable`, require only `release.json`, call `ParseManifest`, retain tag/version and GitHub asset metadata checks, and construct `VerifiedRelease` without `signatureData`.

Delete `internal/release/trust.go`, `internal/release/trust_test.go`, and the signature fixture.

Remove from `state.UpdateRecord`:

```go
TrustedKeys      map[string]string `json:"trustedKeys,omitempty"`
TrustProvenance  map[string]string `json:"trustProvenance,omitempty"`
TrustProofDigest string            `json:"trustProofDigest,omitempty"`
```

Remove from `update.Dependencies`:

```go
BuildTrusted   func(release.VerifiedRelease) (release.PersistedTrust, error)
PersistTrusted func(release.PersistedTrust) error
LoadTrusted    func() (release.PersistedTrust, error)
```

Delete the corresponding build, persist, rollback, and continuation comparisons. Keep `ReleaseDigest` and the update record's `ReleaseDigest` field.

- [ ] **Step 5: Remove compiled roots from staging, production composition, repair, and migration**

Make `Stager` independent of a verifier:

```go
type Stager struct {
	ReleaseDir, ActiveRelease string
	ExpectedPlatform          Platform
	// existing test hooks remain
}
```

Store and validate `release.json` only. Change installed-release validation and `Load` to call `ParseManifest`.

Delete `releaseRootKeyID`, `releaseRootPublicKey`, `compiledReleaseRoots`, and `internal-release-root-check`. Replace the build smoke command with:

```text
internal-release-contract-check
```

The replacement command succeeds only when production update, active catalog loading, repair staging, and migration dependencies are configured without test overrides.

Change production construction to pass no roots:

```go
client := release.Client{
	HTTP:     productionReleaseHTTPClient(),
	Endpoint: release.DefaultLatestReleaseEndpoint,
	CacheDir: layout.ReleaseCacheDir,
}
stager := release.Stager{
	ReleaseDir:       layout.ReleaseDir,
	ActiveRelease:    layout.ActiveRelease,
	ExpectedPlatform: release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
}
```

Remove root parameters from `configureStableUpdate`, `configureCurrentMigration`, `productionActiveCatalog`, `loadStagedMigrationRelease`, `resumeCurrentMigration`, and `currentMigrationBinding`. Preserve manifest digest and catalog digest comparisons.

- [ ] **Step 6: Remove `trustedKeys` from manifest generation**

Generate:

```go
manifest := release.Manifest{
	Version:       version,
	CatalogSchema: catalogSchema,
	StateSchema:   stateSchema,
	Assets:        assets,
}
```

Ensure deterministic indentation, ordering, and trailing newline remain unchanged.

- [ ] **Step 7: Run GREEN verification**

Run:

```bash
mise exec go@1.26.0 -- go test ./internal/release ./cmd/release-manifest ./internal/update ./internal/state ./cmd/tpod -count=1
git diff --check
```

Expected: PASS. No production Go source references `crypto/ed25519`, `release.json.sig`, `TrustProof`, `PersistedTrust`, `TrustedKeys`, `releaseRootKeyID`, or `releaseRootPublicKey`.

- [ ] **Step 8: Commit Task 1**

```bash
git add internal/release internal/update internal/state cmd/tpod cmd/release-manifest
git commit -m "refactor: trust stable release manifests"
```

---

### Task 2: Simplify installer, build, and Release workflow

**Files:**
- Modify: `install.sh`
- Modify: `scripts/build-tpod-release.sh`
- Modify: `.github/workflows/release.yml`
- Modify: `tests/build_tpod_release_test.sh`
- Modify: `tests/terrapod_manager_installer_test.sh`
- Modify: `tests/terrapod_installer_test.sh`
- Modify: `tests/release_artifacts_test.sh`

**Interfaces:**
- Produce: rendered `install.sh` with only `__TERRAPOD_RELEASE_BASE_URL__`.
- Preserve: repair and migration modes, manifest field parsing, exact platform binary selection, checksum verification, staging, launcher recovery, and failure atomicity.
- Produce: Release workflow with exactly eight assets and no repository variables or secret.

- [ ] **Step 1: Change installer and workflow contract tests first**

Update assertions to require:

```sh
assert_contains "$installer_text" "__TERRAPOD_RELEASE_BASE_URL__" \
  "installer keeps the release base placeholder"
assert_not_contains "$installer_text" "__TERRAPOD_RELEASE_ROOT_KEY_ID__" \
  "installer has no release root key ID placeholder"
assert_not_contains "$installer_text" "__TERRAPOD_RELEASE_ROOT_PUBLIC_KEY__" \
  "installer has no release root public key placeholder"
assert_not_contains "$workflow" "RELEASE_SIGNING_PRIVATE_KEY" \
  "workflow requires no signing secret"
assert_not_contains "$workflow" "release.json.sig" \
  "workflow publishes no signature envelope"
```

Change the expected asset list in `tests/release_artifacts_test.sh` to the exact eight names from Global Constraints. Add an assertion that workflow text contains none of the three removed variable/secret names.

- [ ] **Step 2: Run RED verification**

Run:

```bash
sh tests/build_tpod_release_test.sh
sh tests/terrapod_manager_installer_test.sh
sh tests/terrapod_installer_test.sh
sh tests/release_artifacts_test.sh
```

Expected: FAIL because build flags, installer placeholders, signature verification, and workflow signing still exist.

- [ ] **Step 3: Remove release-root build flags**

Reduce `scripts/build-tpod-release.sh` to the existing reproducible Go build without root ldflags:

```sh
if command -v mise >/dev/null 2>&1; then
  exec mise exec go@1.26.0 -- go build -trimpath -o "$1" ./cmd/tpod
fi
exec go build -trimpath -o "$1" ./cmd/tpod
```

Keep argument validation, `CGO_ENABLED=0`, and executable output behavior.

- [ ] **Step 4: Replace installer signature verification with platform SHA-256**

Delete root-key placeholder validation, signature download, signature envelope parsing, public-key PEM construction, and `pkeyutl`.

Add:

```sh
sha256_file() {
  path="$1"
  case "$(uname -s 2>/dev/null || printf unknown)" in
    Darwin)
      command -v shasum >/dev/null 2>&1 ||
        fatal "shasum is required to verify Terrapod release assets"
      shasum -a 256 "$path" | awk '{print $1}'
      ;;
    Linux)
      command -v sha256sum >/dev/null 2>&1 ||
        fatal "sha256sum is required to verify Terrapod release assets"
      sha256sum "$path" | awk '{print $1}'
      ;;
    *)
      fatal "unsupported platform for SHA-256 verification"
      ;;
  esac
}
```

Use `sha256_file` for manifest and asset digest checks. Keep canonical lowercase 64-hex validation and exact asset sizes. The installer downloads only `release.json` before assets.

Render only:

```sh
sed \
  -e "s|__TERRAPOD_RELEASE_BASE_URL__|${release_base}|g" \
  install.sh >artifacts/install.sh
```

- [ ] **Step 5: Remove the workflow signing job step and signature asset**

Keep the `test` and `release` jobs. Remove release-root `env`, the signing step, and secret use. After manifest creation and installer rendering, publish exactly:

```text
artifacts/tpod-darwin-amd64
artifacts/tpod-darwin-arm64
artifacts/tpod-linux-amd64
artifacts/tpod-linux-arm64
artifacts/terrapod-source.tar.gz
artifacts/resources.json
artifacts/release.json
artifacts/install.sh
```

Run `artifacts/tpod-linux-amd64 internal-release-contract-check` during build validation.

- [ ] **Step 6: Run GREEN verification**

Run with Homebrew and Go tools available:

```bash
PATH="/opt/homebrew/bin:$(mise where go@1.26.0)/bin:$PATH" \
  sh tests/build_tpod_release_test.sh
PATH="/opt/homebrew/bin:$(mise where go@1.26.0)/bin:$PATH" \
  sh tests/terrapod_manager_installer_test.sh
PATH="/opt/homebrew/bin:$(mise where go@1.26.0)/bin:$PATH" \
  sh tests/terrapod_installer_test.sh
PATH="/opt/homebrew/bin:$(mise where go@1.26.0)/bin:$PATH" \
  sh tests/release_artifacts_test.sh
git diff --check
```

Expected: PASS, with no OpenSSL 3, signing variable, signature envelope, or private-key requirement.

- [ ] **Step 7: Commit Task 2**

```bash
git add install.sh scripts/build-tpod-release.sh .github/workflows/release.yml \
  tests/build_tpod_release_test.sh tests/terrapod_manager_installer_test.sh \
  tests/terrapod_installer_test.sh tests/release_artifacts_test.sh
git commit -m "ci: publish checksum-validated releases"
```

---

### Task 3: Add the guided legacy update bridge

**Files:**
- Create: `dot_local/bin/executable_terrapod.tmpl`
- Create: `dot_local/bin/symlink_tpod`
- Modify: `.chezmoiignore`
- Create: `tests/legacy_update_transition_test.sh`
- Modify: `tests/chezmoiignore_test.sh`
- Modify: `tests/terrapod_manager_migration_test.sh`

**Interfaces:**
- Produce: a legacy-only `terrapod` transition bridge and `tpod -> terrapod` symlink.
- Consume: root-level `terrapod` object in manager `--override-data-file`.
- Consume: internal installer migration mode.
- Preserve: original argument boundaries, empty arguments, environment, retryability, and real-home isolation.

- [ ] **Step 1: Write bridge and ignore contract tests**

Add a fixture test that uses a temporary destination, temporary legacy data, stub `curl`, and stub installer. Its assertions must cover:

```sh
grep -F 'Terrapod manager transition is ready.' "$first_stderr" >/dev/null ||
  fail "first legacy update prints transition readiness"
grep -F 'Run `tpod update` once more to complete it automatically.' "$first_stderr" >/dev/null ||
  fail "first legacy update prints the second-run command"
[ ! -e "$migration_log" ] ||
  fail "first legacy update does not run migration"

grep -Fx 'installer-arg:--migrate' "$migration_log" >/dev/null ||
  fail "bridge invokes migration internally"
grep -Fx 'manager-arg:update' "$manager_log" >/dev/null ||
  fail "bridge forwards update after migration"
grep -Fx 'manager-arg:two words' "$manager_log" >/dev/null ||
  fail "bridge preserves argument boundaries"
grep -Fx 'manager-arg:' "$manager_log" >/dev/null ||
  fail "bridge preserves empty arguments"
```

Add failure scenarios for unavailable release, failed migration, and a stub installer that exits success without replacing the bridge. Confirm every failure leaves the bridge executable and does not invoke the manager.

In `tests/chezmoiignore_test.sh`, assert manager data excludes:

```text
dot_local/bin/executable_terrapod.tmpl
dot_local/bin/symlink_tpod
```

Also render with legacy flat data and assert both are managed.

- [ ] **Step 2: Run RED verification**

Run:

```bash
sh tests/legacy_update_transition_test.sh
sh tests/chezmoiignore_test.sh
sh tests/terrapod_manager_migration_test.sh
```

Expected: FAIL because the transition bridge, legacy warning, and manager-mode ignore rules do not exist.

- [ ] **Step 3: Add manager-mode ignore rules**

Add to `.chezmoiignore`:

```gotemplate
{{ if hasKey . "terrapod" }}
# The Go manager installs and owns its stable launcher pair.
.local/bin/terrapod
.local/bin/tpod
{{ end }}
```

Legacy flat chezmoi data has no root `terrapod` object, so the bridge remains managed only there.

- [ ] **Step 4: Implement the transition bridge template**

Create `dot_local/bin/executable_terrapod.tmpl` with this behavior:

```gotemplate
{{- warnf "Terrapod manager transition is ready.\nRun `tpod update` once more to complete it automatically." -}}
#!/bin/sh
set -eu

installer_url="https://github.com/juty9026/terrapod/releases/download/v1.0.0/install.sh"

fatal() {
  printf '%s\n' "terrapod transition: $*" >&2
  exit 1
}

[ "${TPOD_LEGACY_TRANSITION_ACTIVE:-}" != "1" ] ||
  fatal "migration did not replace the legacy transition bridge"
[ -x /usr/bin/id ] || fatal "trusted /usr/bin/id is unavailable"
[ "$(/usr/bin/id -u)" -ne 0 ] || fatal "refusing to run as root"

work="$(mktemp -d "${TMPDIR:-/tmp}/terrapod-transition.XXXXXX")" ||
  fatal "failed to create a temporary directory"
trap 'rm -rf "$work"' EXIT HUP INT TERM
umask 077
installer="$work/install.sh"
curl_bin="$(command -v curl 2>/dev/null || true)"
[ -n "$curl_bin" ] || fatal "curl is required"
"$curl_bin" -fsSL "$installer_url" -o "$installer" ||
  fatal "v1.0.0 is not available; retry this command after the release is published"
chmod 0700 "$installer" || fatal "failed to protect the downloaded installer"

TPOD_LEGACY_TRANSITION_ACTIVE=1 sh "$installer" --migrate ||
  fatal "manager migration did not complete; retry the same tpod command"

launcher="$HOME/.local/bin/tpod"
[ -x "$launcher" ] || fatal "migration did not install the tpod launcher"
TPOD_LEGACY_TRANSITION_ACTIVE=1 exec "$launcher" "$@"
```

Create `dot_local/bin/symlink_tpod` containing exactly:

```text
terrapod
```

- [ ] **Step 5: Keep migration internal but recovery-capable**

Update migration contract wording so `install.sh --migrate` remains an internal recovery primitive. Do not remove `migrate-current`, completion markers, preflight, config conversion, ownership import, reconciliation, source validation, or resumability.

- [ ] **Step 6: Run GREEN verification**

Run:

```bash
sh tests/legacy_update_transition_test.sh
sh tests/chezmoiignore_test.sh
sh tests/terrapod_manager_migration_test.sh
chezmoi execute-template --source . --override-data '{"profile":"macos-terminal"}' \
  --source-path dot_local/bin/executable_terrapod.tmpl | sh -n
git diff --check
```

Expected: PASS. Test logs prove first invocation only warns, second invocation migrates and forwards, manager data ignores the bridge, and no path under `/Users/minu` is mutated.

- [ ] **Step 7: Commit Task 3**

```bash
git add .chezmoiignore dot_local/bin/executable_terrapod.tmpl \
  dot_local/bin/symlink_tpod tests/legacy_update_transition_test.sh \
  tests/chezmoiignore_test.sh tests/terrapod_manager_migration_test.sh
git commit -m "feat: guide legacy manager transition"
```

---

### Task 4: Align product language and verify the complete release

**Files:**
- Modify: `README.md`
- Modify: `README.ko.md`
- Modify: `docs/adr/0011-manage-declared-development-environment-resources.md`
- Modify: release/signing-related comments and user-facing strings in changed Go and shell files
- Modify: `tests/readme_optional_stack_profiles_test.sh`
- Modify: `tests/readme_korean_test.sh`
- Modify: any contract assertion that still requires the removed signing model

**Interfaces:**
- Consume: Stable Release terminology from `CONTEXT.md`.
- Preserve: Canonical README and Korean README heading order.
- Produce: no user-facing claim that Terrapod uses Ed25519 or application-level signed releases.

- [ ] **Step 1: Change documentation contract tests first**

Require English and Korean documentation to contain the equivalent of:

```text
Terrapod validates Stable Release metadata from the canonical GitHub repository
over HTTPS and checks every asset's size and SHA-256 digest before activation.
```

Require the legacy section to state:

```text
Run `tpod update` once, follow the printed instruction, then run it once more.
The second invocation performs the one-shot manager transition automatically.
```

Add negative assertions for:

```text
Ed25519
release.json.sig
RELEASE_SIGNING_PRIVATE_KEY
latest stable signed Terrapod release
```

- [ ] **Step 2: Run RED documentation tests**

Run:

```bash
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
rg -n "signed release|Signed Release|signed catalog|signed declaration|Ed25519|release\\.json\\.sig" \
  README.md README.ko.md CONTEXT.md docs/adr/0011-manage-declared-development-environment-resources.md \
  cmd internal scripts install.sh tests .github/workflows/release.yml
```

Expected: tests FAIL and the search reports stale product language or signing implementation references.

- [ ] **Step 3: Update documentation and directly affected terminology**

Update README claims, release asset inventory, repair/update descriptions, and legacy migration instructions to the approved Stable Release model and two-invocation UX.

Add a supersession note to ADR 0011 pointing to ADR 0012. Do not rewrite ADR 0011's historical Decision text.

In directly changed production code, replace misleading user-facing errors such as:

```text
fetch latest signed release
load staged signed inputs
signed update dependencies are not configured
```

with:

```text
fetch latest stable release
load staged release inputs
stable update dependencies are not configured
```

Do not mechanically rename unrelated test fixture digests such as `"signed-v1"` unless they appear in user-visible output or domain documentation.

- [ ] **Step 4: Run focused GREEN documentation tests**

Run:

```bash
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
git diff --check
```

Expected: PASS and the English/Korean heading contract remains aligned.

- [ ] **Step 5: Run the complete fresh verification suite**

Run:

```bash
TASK_GO_BIN="$(mise where go@1.26.0)/bin"
PATH="/opt/homebrew/bin:$TASK_GO_BIN:$PATH"
export PATH
go test ./... -count=1
for test_script in tests/*_test.sh; do
  sh "$test_script"
done
git diff --check origin/main..HEAD
git status --porcelain=v1 --untracked-files=all
```

Expected:

- all Go packages PASS
- every shell contract test exits 0
- `git diff --check` has no output
- worktree contains only the intended committed branch changes
- no real-home migration was executed

- [ ] **Step 6: Audit the final scope**

Run:

```bash
git log --oneline origin/main..HEAD
git diff --stat origin/main..HEAD
rg -n "RELEASE_ROOT_KEY_ID|RELEASE_ROOT_PUBLIC_KEY|RELEASE_SIGNING_PRIVATE_KEY|release\\.json\\.sig|crypto/ed25519" \
  cmd internal scripts install.sh .github/workflows/release.yml
```

Expected: the production implementation search has no matches. Negative assertions in tests and historical context in ADR 0011, the approved design, and this plan may retain signing terms.

- [ ] **Step 7: Commit Task 4**

```bash
git add README.md README.ko.md docs/adr/0011-manage-declared-development-environment-resources.md \
  tests/readme_optional_stack_profiles_test.sh tests/readme_korean_test.sh \
  cmd internal scripts install.sh tests .github/workflows/release.yml
git commit -m "docs: describe stable release trust"
```

---

## Final Review and Delivery Gate

- [ ] Generate a whole-branch review package from `1441dd9375cec686ee802acdcb6cbf76b4c09cd3` to `HEAD`.
- [ ] Request independent review for spec compliance, security boundary accuracy, migration retry safety, and code quality.
- [ ] Fix all Critical and Important findings and re-run their covering tests.
- [ ] Re-run the complete verification suite after the final fix.
- [ ] Push `terrapod-unsigned-v1-release` and create a draft PR against `main`.
- [ ] Merge only after the PR is mergeable and review/check gates are clean.
- [ ] After merge, fetch `origin/main`, confirm the merged tree, and verify no `v1.0.0` tag or GitHub Release exists.
- [ ] Create and push `v1.0.0` only at the merged `main` commit.
- [ ] Monitor the Release workflow until terminal success.
- [ ] Verify the exact eight assets, rendered `install.sh` with no unresolved placeholders, manifest/catalog version binding, and downloadable legacy bridge target.
- [ ] Report the guided two-invocation legacy command:

```sh
tpod update
tpod update
```

- [ ] Do not execute those commands against the maintainer's real home without fresh explicit authorization.
