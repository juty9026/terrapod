#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

fail() {
  printf '%s\n' "not ok - $*" >&2
  exit 1
}

grep -F 'migrate-current' "$repo_root/install.sh" >/dev/null ||
  fail "installer must expose explicit --migrate dispatch"
grep -F 'command == "migrate-current"' "$repo_root/internal/cli/app.go" >/dev/null ||
  fail "manager must expose the hidden migration command"
grep -F 'migration-current.json' "$repo_root/cmd/tpod/migration.go" >/dev/null ||
  fail "production migration must use a completion marker"
grep -F 'ApplyConfigConversion' "$repo_root/cmd/tpod/migration.go" >/dev/null ||
  fail "production migration must commit the lossless config conversion"
grep -F 'PlanLegacyOwnership' "$repo_root/cmd/tpod/migration.go" >/dev/null ||
  fail "production migration must import signed legacy ownership"
grep -F 'RemoveLegacySource' "$repo_root/cmd/tpod/migration.go" >/dev/null ||
  fail "production migration must use revalidated legacy source removal"

mise exec go@1.26.0 -- go test ./internal/migrate \
  -run 'TestRunCurrent|TestPlanLegacyOwnership|TestValidateLegacySource|TestRemoveLegacySource' >/dev/null ||
  fail "migration transaction, ownership, and source tests must pass"
mise exec go@1.26.0 -- go test ./internal/cli \
  -run 'TestHiddenMigrateCurrent' >/dev/null ||
  fail "hidden migration CLI tests must pass"
mise exec go@1.26.0 -- go test ./cmd/tpod \
  -run 'TestMigration' >/dev/null ||
  fail "production migration composition tests must pass"

printf '%s\n' "ok - Terrapod manager migration contract"
