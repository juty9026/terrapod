#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/build-tpod-release.sh <output>" >&2
  exit 2
fi

if command -v mise >/dev/null 2>&1; then
  exec mise exec go@1.26.0 -- go build -trimpath -o "$1" ./cmd/tpod
fi
exec go build -trimpath -o "$1" ./cmd/tpod
