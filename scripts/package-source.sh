#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: scripts/package-source.sh <stable-tag> <output.tar.gz>" >&2
  exit 2
fi

tag="$1"
output="$2"
printf '%s\n' "$tag" | grep -E '^v(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)$' >/dev/null ||
  { echo "release tag must be stable vMAJOR.MINOR.PATCH" >&2; exit 1; }

root="$(git rev-parse --show-toplevel)"
tag_commit="$(git rev-parse --verify "refs/tags/$tag^{commit}")" ||
  { echo "release tag does not exist: $tag" >&2; exit 1; }
[ "$(git rev-parse HEAD)" = "$tag_commit" ] ||
  { echo "release tag must point at HEAD" >&2; exit 1; }
[ -z "$(git status --porcelain)" ] ||
  { echo "worktree is dirty" >&2; exit 1; }
if git -C "$root" submodule status --recursive 2>/dev/null |
  grep -E '^[+-]|^U' >/dev/null; then
  echo "submodule state differs from the release commit" >&2
  exit 1
fi

timestamp="$(git show -s --format=%ct "$tag_commit")"
temporary="${output}.tmp.$$"
trap 'rm -f "$temporary"' EXIT HUP INT TERM
mkdir -p "$(dirname "$output")"

# Git trees contain tracked files in bytewise path order. git archive emits
# their recorded modes with numeric owner/group 0; --mtime binds every entry
# to the tagged commit time. gzip -n removes its own timestamp and filename.
LC_ALL=C git -C "$root" archive --format=tar --mtime="@$timestamp" "${tag_commit}^{tree}" |
  gzip -n >"$temporary"
mv "$temporary" "$output"
trap - EXIT HUP INT TERM
