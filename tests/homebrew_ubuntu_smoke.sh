#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
platform="${TERRAPOD_SMOKE_PLATFORM:-linux/amd64}"
dockerfile="$repo_root/tests/fixtures/homebrew-ubuntu-24.04.Dockerfile"

grep -F '/workspace/catalog/v1/resources.json' "$dockerfile" >/dev/null ||
  {
    printf '%s\n' "not ok - Ubuntu smoke must use the typed resource catalog" >&2
    exit 1
  }
if grep -F '/workspace/Brewfile' "$dockerfile" >/dev/null; then
  printf '%s\n' "not ok - Ubuntu smoke still uses the deleted Brewfile" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  printf '%s\n' "ok - SKIP Ubuntu Homebrew smoke: docker is unavailable"
  exit 0
fi
if ! docker info >/dev/null 2>&1; then
  printf '%s\n' "ok - SKIP Ubuntu Homebrew smoke: docker daemon is unavailable"
  exit 0
fi

docker buildx build \
  --load \
  --platform "$platform" \
  --file "$dockerfile" \
  --tag "terrapod-homebrew-smoke:${platform##*/}" \
  "$repo_root"
