#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
if find "$repo_root/.chezmoiscripts" -type f -print 2>/dev/null | grep . >/dev/null; then
  printf '%s\n' "not ok - Ubuntu bootstrap mutation script remains" >&2
  exit 1
fi

go test ./internal/catalog -run 'TestSeedCatalogDeclaresBootstrapAPTAndMiseResources$'
go test ./internal/provider/apt -run 'Test(InspectUsesExactDpkgQueryAndRejectsEssentialPackage|ExecuteSimulatesImmediatelyBeforeTargetedMutation)$'

printf '%s\n' "ok - Ubuntu bootstrap ownership is typed"
