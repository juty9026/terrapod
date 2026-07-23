#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
[ ! -e "$repo_root/dot_local/lib/terrapod/executable_jetendard-settings" ] || {
  printf '%s\n' "not ok - legacy Jetendard settings helper remains" >&2
  exit 1
}
go test ./internal/resource/integration -run 'Test(JSONFieldsLifecyclePreservesUnrelatedAndRestoresPrior|JSONCFieldsPreserveComments|PlistFieldsRoundTrip)$'
printf '%s\n' "ok - Jetendard settings are typed integrations"
