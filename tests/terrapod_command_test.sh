#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
fail() { printf '%s\n' "not ok - $*" >&2; exit 1; }

[ ! -e "$repo_root/dot_local/bin/executable_terrapod" ] ||
  fail "legacy chezmoi-owned terrapod launcher remains"
[ ! -e "$repo_root/dot_local/bin/symlink_tpod" ] ||
  fail "legacy chezmoi-owned tpod symlink remains"

if find "$repo_root/.chezmoiscripts" -type f -print 2>/dev/null | grep . >/dev/null; then
  fail "chezmoi mutation scripts remain"
fi

go test ./internal/cli -run 'Test(HelpDescribesShadowCommandSurfaceWithoutDependencies|ChezmoiDispatchesOnlyExplicitReadOnlyPassthrough|ApplyBuildsNonUpgradePlanAndRendersDeterministicSummary)$'
go test ./cmd/tpod -run 'Test(BuiltBinaryDispatchesThroughRealConstrainedChezmoiClient|ProductionPlannerComposesRealStateBoundAdapters)$'

printf '%s\n' "ok - stable manager command surface is outside chezmoi"
