#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

cd "$repo_root"
mise exec go@1.26.0 -- go test ./internal/cli \
  -run 'TestManagerActivationHasNoChezmoiMutationScripts|TestManagerCatalogOwnsEveryRecordedLegacyMutation|TestComposeRegistryRegistersEveryPlan02And03Adapter' \
  -count=1
mise exec go@1.26.0 -- go test ./cmd/tpod \
  -run TestBuiltBinaryDispatchesThroughRealConstrainedChezmoiClient \
  -count=1

printf '%s\n' 'ok - typed catalog owns the recorded legacy mutation inventory'
printf '%s\n' 'ok - built tpod uses the real constrained chezmoi client'
