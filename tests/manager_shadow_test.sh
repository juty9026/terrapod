#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

run_go() {
  if command -v mise >/dev/null 2>&1; then
    mise exec go@1.26.0 -- go "$@"
  else
    command go "$@"
  fi
}

cd "$repo_root"
run_go test ./internal/cli \
  -run 'TestManagerActivationHasNoChezmoiMutationScripts|TestManagerCatalogOwnsEveryRecordedLegacyMutation|TestComposeRegistryRegistersEveryPlan02And03Adapter' \
  -count=1
run_go test ./cmd/tpod \
  -run TestBuiltBinaryDispatchesThroughRealConstrainedChezmoiClient \
  -count=1

printf '%s\n' 'ok - typed catalog owns the recorded legacy mutation inventory'
printf '%s\n' 'ok - built tpod uses the real constrained chezmoi client'
