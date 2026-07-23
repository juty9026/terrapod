#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/build-tpod-release.sh <output>" >&2
  exit 2
fi
: "${RELEASE_ROOT_KEY_ID:?RELEASE_ROOT_KEY_ID is required}"
: "${RELEASE_ROOT_PUBLIC_KEY:?RELEASE_ROOT_PUBLIC_KEY is required}"

case "$RELEASE_ROOT_KEY_ID" in
  *[!a-z0-9._-]*|'') echo "invalid RELEASE_ROOT_KEY_ID" >&2; exit 2 ;;
esac
decoded_size=$(printf '%s' "$RELEASE_ROOT_PUBLIC_KEY" | openssl base64 -d -A | wc -c | tr -d ' ')
canonical=$(printf '%s' "$RELEASE_ROOT_PUBLIC_KEY" | openssl base64 -d -A | openssl base64 -A)
if [ "$decoded_size" != 32 ] || [ "$canonical" != "$RELEASE_ROOT_PUBLIC_KEY" ]; then
  echo "RELEASE_ROOT_PUBLIC_KEY must be canonical base64 for 32 bytes" >&2
  exit 2
fi

if command -v mise >/dev/null 2>&1; then
  exec mise exec go@1.26.0 -- go build -trimpath -ldflags "-X main.releaseRootKeyID=$RELEASE_ROOT_KEY_ID -X main.releaseRootPublicKey=$RELEASE_ROOT_PUBLIC_KEY" -o "$1" ./cmd/tpod
fi
exec go build -trimpath -ldflags "-X main.releaseRootKeyID=$RELEASE_ROOT_KEY_ID -X main.releaseRootPublicKey=$RELEASE_ROOT_PUBLIC_KEY" -o "$1" ./cmd/tpod
