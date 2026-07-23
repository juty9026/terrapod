#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

sh "$root/scripts/build-tpod-release.sh" "$tmp/tpod"
"$tmp/tpod" internal-release-contract-check
if "$tmp/tpod" internal-self-check >/dev/null 2>&1; then
  echo "argument-free staged self-check was accepted" >&2
  exit 1
fi
build_script="$(cat "$root/scripts/build-tpod-release.sh")"
case "$build_script" in
  *RELEASE_ROOT*|*-ldflags*)
    echo "release build still embeds release-root configuration" >&2
    exit 1
    ;;
esac
echo "ok - release build configures the stable manifest contract"
