# Ubuntu release CI portable fixture fix

## Status

Complete. The two integration tests now create vendor-home fixtures below the
package workspace instead of the OS temporary root. The directories are unique,
absolute, symlink-resolved, and removed automatically by `t.Cleanup`.

Production vendor-home safety validation was not changed.

## Root cause

Both affected tests passed `t.TempDir()` directly to `legacy.WithVendor`.
On macOS the temporary directory is normally below `/var/folders`, while on
Ubuntu it is below `/tmp`. Production intentionally rejects `/tmp/**` and
`/private/tmp/**` vendor homes, so the Ubuntu fixtures failed before exercising
the intended integration behavior.

## Changes

- Added `testutil.WorkspaceTempDir`.
  - Creates a unique directory below the Go package working directory.
  - Converts it to an absolute path.
  - Resolves symlinks so the returned directory is a real path.
  - Registers automatic recursive cleanup with `t.Cleanup`.
- Updated `TestProductionPlannerComposesRealStateBoundAdapters` to use the
  workspace-backed fixture home.
- Updated `TestEngineRunsRealTransferPreflightWithoutPrivilege` to use the same
  helper.

No migration, install, or `chezmoi apply` command was run. No fixture data was
written to `/Users/minu` or the real `HOME`.

## TDD evidence

The supplied RED reproductions failed before this change with the expected
`legacy: unsafe vendor home "/tmp/..."` error.

After the fixture change, both exact reproductions passed:

```text
TMPDIR=/tmp mise exec go@1.26.0 -- go test ./cmd/tpod -run TestProductionPlannerComposesRealStateBoundAdapters -count=1 -v
PASS
ok github.com/juty9026/terrapod/cmd/tpod

TMPDIR=/tmp mise exec go@1.26.0 -- go test ./internal/reconcile -run TestEngineRunsRealTransferPreflightWithoutPrivilege -count=1 -v
PASS
ok github.com/juty9026/terrapod/internal/reconcile
```

## Verification

All requested verification completed successfully:

```text
TMPDIR=/tmp mise exec go@1.26.0 -- go test ./cmd/tpod ./internal/reconcile -count=1
ok github.com/juty9026/terrapod/cmd/tpod
ok github.com/juty9026/terrapod/internal/reconcile

TMPDIR=/tmp mise exec go@1.26.0 -- go test ./... -count=1
all packages passed

git diff --check
exit 0
```

## Self-review

- Scope is limited to test infrastructure and the two affected fixture call
  sites.
- Production `internal/provider/legacy` code is unchanged.
- The helper does not consult `TMPDIR` or `HOME`.
- Unique directory creation avoids collisions between concurrent tests.
- Cleanup is registered on the test lifecycle and reports removal failures.
- No unrelated formatting or refactoring is included.

## Concerns

None. The helper assumes the checked-out source workspace is writable, which is
already required by this repository's independent Orca Workspace test setup.

## Reviewer follow-up

Moved cleanup registration to immediately after `os.MkdirTemp` succeeds and
captured the original created path for removal. The fixture is now cleaned even
if `filepath.Abs` or `filepath.EvalSymlinks` fails. Cleanup errors continue to be
reported through the test.

The requested follow-up verification passed:

```text
TMPDIR=/tmp mise exec go@1.26.0 -- go test ./cmd/tpod -run TestProductionPlannerComposesRealStateBoundAdapters -count=1
ok github.com/juty9026/terrapod/cmd/tpod

TMPDIR=/tmp mise exec go@1.26.0 -- go test ./internal/reconcile -run TestEngineRunsRealTransferPreflightWithoutPrivilege -count=1
ok github.com/juty9026/terrapod/internal/reconcile

git diff --check
exit 0
```
