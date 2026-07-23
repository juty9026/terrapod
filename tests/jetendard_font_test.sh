#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
[ ! -e "$repo_root/dot_local/lib/terrapod/executable_jetendard-font" ] || {
  printf '%s\n' "not ok - legacy Jetendard font helper remains" >&2
  exit 1
}
go test ./internal/resource/jetendard -run 'Test(PlanAndApplyUsesResolvedMetadataWithoutLatestLookup|UpgradePreservesUserFontsAndPruneRemovesOnlyReceipt)$'
printf '%s\n' "ok - Jetendard font is a typed resource"
