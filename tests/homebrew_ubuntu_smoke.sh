#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
platform="${TERRAPOD_SMOKE_PLATFORM:-linux/amd64}"

docker buildx build \
  --load \
  --platform "$platform" \
  --file "$repo_root/tests/fixtures/homebrew-ubuntu-24.04.Dockerfile" \
  --tag "terrapod-homebrew-smoke:${platform##*/}" \
  "$repo_root"
