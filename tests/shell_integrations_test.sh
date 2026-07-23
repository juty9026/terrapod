#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
if find "$repo_root/.chezmoiscripts" -type f -print 2>/dev/null | grep . >/dev/null; then
  printf '%s\n' "not ok - legacy shell integration mutation scripts remain" >&2
  exit 1
fi
go test ./internal/resource/gitcheckout -run 'Test(PlanLifecycle|PruneRemovesRecordedTrackedFilesButPreservesUntrackedChildren|EngineOwnershipHistoricalPruneRoundTripPreservesUntracked)$'
go test ./internal/resource/integration -run 'TestKarabinerOpenerOwnsNoGeneralState$'
printf '%s\n' "ok - shell integrations are typed resources"
