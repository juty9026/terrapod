#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $*" >&2
  exit 1
}

run_go() {
  if command -v mise >/dev/null 2>&1; then
    mise exec go@1.26.0 -- go "$@"
  else
    command go "$@"
  fi
}

run_scenario() {
  package="$1"
  test_name="$2"
  label="$3"
  output="$tmp_dir/$(printf '%s' "$test_name" | tr '[:upper:]' '[:lower:]').out"

  run_go test "$package" -count=1 -run "^${test_name}$" -v >"$output" ||
    fail "$label"
  grep -F -- "--- PASS: $test_name " "$output" >/dev/null ||
    fail "$label did not execute"
}

grep -F 'migrate-current' "$repo_root/install.sh" >/dev/null ||
  fail "installer must expose explicit --migrate dispatch"
grep -F 'internal recovery primitive' "$repo_root/install.sh" >/dev/null ||
  fail "installer must document --migrate as an internal recovery primitive"
grep -F 'command == "migrate-current"' "$repo_root/internal/cli/app.go" >/dev/null ||
  fail "manager must expose the hidden migration command"
grep -F 'migration-current.json' "$repo_root/cmd/tpod/migration.go" >/dev/null ||
  fail "production migration must use a completion marker"
grep -F 'ApplyConfigConversion' "$repo_root/cmd/tpod/migration.go" >/dev/null ||
  fail "production migration must commit the lossless config conversion"
grep -F 'PlanLegacyOwnership' "$repo_root/cmd/tpod/migration.go" >/dev/null ||
  fail "production migration must import release-bound legacy ownership"
grep -F 'RemoveLegacySource' "$repo_root/cmd/tpod/migration.go" >/dev/null ||
  fail "production migration must use revalidated legacy source removal"

run_go test ./internal/migrate \
  -run 'TestRunCurrent|TestPlanLegacyOwnership|TestValidateLegacySource|TestRemoveLegacySource' >/dev/null ||
  fail "migration transaction, ownership, and source tests must pass"
run_go test ./internal/cli \
  -run 'TestHiddenMigrateCurrent' >/dev/null ||
  fail "hidden migration CLI tests must pass"
run_go test ./cmd/tpod \
  -run 'TestMigration' >/dev/null ||
  fail "production migration composition tests must pass"

run_scenario ./internal/migrate TestRunCurrentRetriesOnlySourceAfterReconciliation \
  "interruption after desired install resumes before legacy removal"
run_scenario ./internal/migrate TestPlanLegacyOwnershipUnknownProvenanceIsUnavailableWithoutMutation \
  "unknown legacy provenance remains unavailable"
run_scenario ./internal/migrate TestPlanLegacyOwnershipRefusesModifiedManagedTarget \
  "modified managed files block migration"
run_scenario ./internal/update TestRunProviderFailurePreservesActiveAndCreatesNoJournal \
  "provider metadata outage causes zero mutation"
run_scenario ./internal/update TestContinueRejectsNewActualUnavailableAndKeepsJournal \
  "post-activation update failure remains resumable"
run_scenario ./internal/resolve TestResolveDisplaysEveryExactBlockerAndDefaultsToCancellation \
  "unmanaged blocker removal requires confirmation"
run_scenario ./internal/cli TestMissingConfigBlocksApplyBeforeMutationDependenciesLoad \
  "lost config blocks manager planning before mutation dependencies load"
run_scenario ./internal/state TestAcquireRejectsLiveLock \
  "live reconciliation lock rejects concurrent mutation"
run_scenario ./internal/reconcile TestRepeatedApplySecondPlanAndApplyAreExactNoOp \
  "repeated apply reports ready resources without mutation"

printf '%s\n' "ok - Terrapod manager migration contract"
