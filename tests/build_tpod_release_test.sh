#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

if RELEASE_ROOT_KEY_ID= RELEASE_ROOT_PUBLIC_KEY= sh "$root/scripts/build-tpod-release.sh" "$tmp/missing" >/dev/null 2>&1; then
  echo "missing trust root was accepted" >&2
  exit 1
fi
if RELEASE_ROOT_KEY_ID=root RELEASE_ROOT_PUBLIC_KEY=invalid sh "$root/scripts/build-tpod-release.sh" "$tmp/invalid" >/dev/null 2>&1; then
  echo "invalid trust root was accepted" >&2
  exit 1
fi

RELEASE_ROOT_KEY_ID=root RELEASE_ROOT_PUBLIC_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA= sh "$root/scripts/build-tpod-release.sh" "$tmp/tpod"
"$tmp/tpod" internal-release-root-check
if "$tmp/tpod" internal-self-check >/dev/null 2>&1; then
  echo "argument-free staged self-check was accepted" >&2
  exit 1
fi
echo "ok - release build requires and embeds a canonical public trust root"
