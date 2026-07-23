#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/terrapod-release-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

fail() {
  printf 'not ok - %s\n' "$1" >&2
  exit 1
}

pass() {
  printf 'ok - %s\n' "$1"
}

run_go() {
  if command -v mise >/dev/null 2>&1; then
    mise exec go@1.26.0 -- go "$@"
  else
    command go "$@"
  fi
}

validate_go_dispatch_contract() {
  expected_call_sites="$1"
  script_contract="$2"
  helper_contract="$(
    printf '%s\n' "$script_contract" |
      awk '
        /^run_go\(\) \{$/ { helper = 1 }
        helper { print }
        helper && /^}$/ { exit }
      '
  )"

  [ "$(printf '%s\n' "$script_contract" | awk 'index($0, "run_go() {") { count++ } END { print count + 0 }')" -eq 1 ] &&
    [ "$(printf '%s\n' "$script_contract" | awk '$0 ~ /^[[:space:]]*run_go[[:space:]]+test([[:space:]]|$)/ { count++ } END { print count + 0 }')" -eq "$expected_call_sites" ] &&
    [ "$(printf '%s\n' "$script_contract" | awk 'index($0, "mise exec go@1.26.0 -- go") { count++ } END { print count + 0 }')" -eq 1 ] &&
    [ "$(printf '%s\n' "$script_contract" | awk 'index($0, "command go \"$@\"") { count++ } END { print count + 0 }')" -eq 1 ] &&
    [ "$(printf '%s\n' "$helper_contract" | awk 'index($0, "if command -v mise >/dev/null 2>&1; then") { count++ } END { print count + 0 }')" -eq 1 ] &&
    [ "$(printf '%s\n' "$helper_contract" | awk 'index($0, "mise exec go@1.26.0 -- go \"$@\"") { count++ } END { print count + 0 }')" -eq 1 ] &&
    [ "$(printf '%s\n' "$helper_contract" | awk 'index($0, "command go \"$@\"") { count++ } END { print count + 0 }')" -eq 1 ] ||
    return 1

  if printf '%s\n' "$script_contract" |
    grep -E '^[[:space:]]*(command[[:space:]]+)?go[[:space:]]+test([[:space:]]|$)' >/dev/null; then
    return 1
  fi
  if printf '%s\n' "$script_contract" |
    grep -E '^[[:space:]]*mise[[:space:]].*--[[:space:]]+go[[:space:]]+test([[:space:]]|$)' >/dev/null; then
    return 1
  fi
}

validate_no_runner_tool_reinstall() {
  workflow_contract="$1"
  apt_install_lines="$(
    printf '%s\n' "$workflow_contract" |
      sed -n -E 's/^[[:space:]]*((sudo[[:space:]]+)?apt-get[[:space:]]+install([[:space:]]|$).*)/\1/p'
  )"
  go_install_lines="$(
    printf '%s\n' "$workflow_contract" |
      sed -n -E 's/^[[:space:]]*(go[[:space:]]+install([[:space:]]|$).*)/\1/p'
  )"

  [ "$apt_install_lines" = 'sudo apt-get install -y zsh' ] &&
    [ "$go_install_lines" = 'go install github.com/twpayne/chezmoi/v2/cmd/chezmoi@v2.71.1' ] ||
    return 1

  if printf '%s\n' "$workflow_contract" |
    grep -E '^[[:space:]]*(-[[:space:]]+uses:.*mise|mise[[:space:]]+install([[:space:]]|$))' >/dev/null; then
    return 1
  fi
}

extract_published_assets() {
  awk '
    /gh release create/ { publish = 1; next }
    publish && /^[[:space:]]+/ {
      line = $0
      sub(/^[[:space:]]+/, "", line)
      continued = line ~ /\\$/
      sub(/[[:space:]]*\\$/, "", line)
      if (line !~ /^--/) {
        print line
      }
      if (!continued) {
        exit
      }
      next
    }
    publish { exit }
  '
}

extractor_fixture='          gh release create "$GITHUB_REF_NAME" \
            --verify-tag \
            artifacts/release.json \
            extra-release-note.txt'
extractor_expected='artifacts/release.json
extra-release-note.txt'
extractor_actual="$(printf '%s\n' "$extractor_fixture" | extract_published_assets)"
[ "$extractor_actual" = "$extractor_expected" ] ||
  fail "release asset extractor includes every positional asset"
pass "release asset extractor includes every positional asset"

[ -x "$repo_root/scripts/package-source.sh" ] || fail "source packager is executable"
[ -f "$repo_root/.github/workflows/release.yml" ] || fail "release workflow exists"
manager_dispatch_contract="$(cat "$repo_root/tests/manager_shadow_test.sh")"
if validate_go_dispatch_contract 2 \
  "$(printf '%s\n' "$manager_dispatch_contract"; printf '%s\n' 'go test ./direct')"; then
  fail "Go dispatch contract rejects a direct go test call"
fi
if validate_go_dispatch_contract 2 \
  "$(printf '%s\n' "$manager_dispatch_contract"; printf '%s\n' 'mise exec go@1.26.0 -- go test ./direct')"; then
  fail "Go dispatch contract rejects a direct mise test call"
fi
for go_test_script_and_count in \
  "manager_shadow_test.sh 2" \
  "terrapod_manager_migration_test.sh 4"; do
  set -- $go_test_script_and_count
  go_test_script="$1"
  expected_call_sites="$2"
  go_test_contract="$(cat "$repo_root/tests/$go_test_script")"
  validate_go_dispatch_contract "$expected_call_sites" "$go_test_contract" ||
    fail "$go_test_script keeps every Go test dispatch behind run_go"
done

fixture="$tmp_dir/source"
mkdir -p "$fixture/sub"
git -C "$fixture" init -q
git -C "$fixture" config user.email release-test@example.invalid
git -C "$fixture" config user.name "Release Test"
printf 'alpha\n' >"$fixture/a.txt"
printf '#!/bin/sh\nexit 0\n' >"$fixture/sub/tool.sh"
chmod 0755 "$fixture/sub/tool.sh"
git -C "$fixture" add a.txt sub/tool.sh
GIT_AUTHOR_DATE=2020-01-02T03:04:05Z GIT_COMMITTER_DATE=2020-01-02T03:04:05Z \
  git -C "$fixture" commit -qm initial
git -C "$fixture" tag v1.2.3

(cd "$fixture" && "$repo_root/scripts/package-source.sh" v1.2.3 "$tmp_dir/source-1.tar.gz")
(cd "$fixture" && "$repo_root/scripts/package-source.sh" v1.2.3 "$tmp_dir/source-2.tar.gz")
digest_one="$(shasum -a 256 "$tmp_dir/source-1.tar.gz" | awk '{print $1}')"
digest_two="$(shasum -a 256 "$tmp_dir/source-2.tar.gz" | awk '{print $1}')"
[ "$digest_one" = "$digest_two" ] || fail "source archives are reproducible"
[ "$(od -An -t u1 -j 4 -N 4 "$tmp_dir/source-1.tar.gz" | tr -d ' ')" = "0000" ] ||
  fail "gzip timestamp is zero"
archive_names="$(tar -tzf "$tmp_dir/source-1.tar.gz")"
[ "$archive_names" = "$(printf '%s\n' "$archive_names" | LC_ALL=C sort)" ] ||
  fail "source archive paths are bytewise sorted"
mkdir "$tmp_dir/unpacked"
tar -xzf "$tmp_dir/source-1.tar.gz" -C "$tmp_dir/unpacked"
[ -x "$tmp_dir/unpacked/sub/tool.sh" ] || fail "source archive preserves executable mode"
if stat -c %Y "$tmp_dir/unpacked/a.txt" >/dev/null 2>&1; then
  archived_timestamp="$(stat -c %Y "$tmp_dir/unpacked/a.txt")"
else
  archived_timestamp="$(stat -f %m "$tmp_dir/unpacked/a.txt")"
fi
[ "$archived_timestamp" = "$(git -C "$fixture" show -s --format=%ct v1.2.3)" ] ||
  fail "source archive uses the tag commit timestamp"
tar -tvzf "$tmp_dir/source-1.tar.gz" | grep -E 'root[/[:space:]]+root|[[:space:]]0[/[:space:]]+0[[:space:]]' >/dev/null ||
  fail "source archive uses numeric owner and group zero"
pass "source archive is reproducible and preserves tracked modes"

printf 'dirty\n' >>"$fixture/a.txt"
if (cd "$fixture" && "$repo_root/scripts/package-source.sh" v1.2.3 "$tmp_dir/dirty.tar.gz") >/dev/null 2>&1; then
  fail "dirty worktree release is rejected"
fi
git -C "$fixture" checkout -q -- a.txt
printf 'untracked\n' >"$fixture/untracked.txt"
if (cd "$fixture" && "$repo_root/scripts/package-source.sh" v1.2.3 "$tmp_dir/untracked.tar.gz") >/dev/null 2>&1; then
  fail "untracked worktree release is rejected"
fi
rm "$fixture/untracked.txt"
git -C "$fixture" tag v1.2.4-rc.1
if (cd "$fixture" && "$repo_root/scripts/package-source.sh" v1.2.4-rc.1 "$tmp_dir/prerelease.tar.gz") >/dev/null 2>&1; then
  fail "prerelease tag is rejected"
fi
pass "invalid release states are rejected"

assets="$tmp_dir/assets"
mkdir "$assets"
for platform in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
  printf '%s\n' "$platform" >"$assets/tpod-$platform"
done
cp "$tmp_dir/source-1.tar.gz" "$assets/terrapod-source.tar.gz"
run_go run ./cmd/release-manifest \
  --version 1.2.3 --catalog-schema 1 --state-schema 1 \
  --catalog-source "$repo_root/catalog/v1/resources.json" \
  --catalog-output "$assets/resources.json" \
  --asset "binary,darwin,amd64,$assets/tpod-darwin-amd64" \
  --asset "binary,darwin,arm64,$assets/tpod-darwin-arm64" \
  --asset "binary,linux,amd64,$assets/tpod-linux-amd64" \
  --asset "binary,linux,arm64,$assets/tpod-linux-arm64" \
  --asset "source,,,$assets/terrapod-source.tar.gz" \
  --asset "catalog,,,$assets/resources.json" >"$assets/release.json"
[ "$(grep -c '"kind": "binary"' "$assets/release.json")" -eq 4 ] ||
  fail "manifest contains four binaries"
catalog_digest="$(shasum -a 256 "$assets/resources.json" | awk '{print $1}')"
grep -F "\"sha256\": \"$catalog_digest\"" "$assets/release.json" >/dev/null ||
  fail "manifest contains the catalog digest"
grep -F '"release": "1.2.3"' "$assets/resources.json" >/dev/null ||
  fail "published catalog is bound to the stable release version"
grep -F '"release": "development"' "$repo_root/catalog/v1/resources.json" >/dev/null ||
  fail "release rendering leaves the development source catalog unchanged"

if grep -R -F "PRIVATE KEY" "$assets" >/dev/null 2>&1; then
  fail "private key material appears in release artifacts"
fi
pass "manifest binds all release assets"

workflow="$(cat "$repo_root/.github/workflows/release.yml")"
test_job="$(printf '%s\n' "$workflow" | sed -n '/^  test:$/,/^  release:$/p')"
for runner_tool in mise jq gh; do
  if validate_no_runner_tool_reinstall \
    "$(printf '%s\n' "$workflow"; printf '          sudo apt-get install -y %s\n' "$runner_tool")"; then
    fail "release workflow contract rejects apt reinstall of $runner_tool"
  fi
done
if validate_no_runner_tool_reinstall \
  "$(printf '%s\n' "$workflow"; printf '%s\n' '          go install github.com/jdx/mise@v1.0.0')"; then
  fail "release workflow contract rejects go install of mise"
fi
validate_no_runner_tool_reinstall \
  "$(printf '%s\n' "$workflow"; printf '%s\n' '          # sudo apt-get install -y jq' '          echo "go install github.com/jdx/mise@v1.0.0"')" ||
  fail "release workflow install contract ignores documentation"
validate_no_runner_tool_reinstall "$workflow" ||
  fail "release workflow only installs required test prerequisites"
[ "$(printf '%s\n' "$workflow" | grep -c -F 'go install github.com/twpayne/chezmoi/v2/cmd/chezmoi@')" -eq 1 ] ||
  fail "release workflow installs chezmoi exactly once"
printf '%s\n' "$test_job" | grep -F 'go install github.com/twpayne/chezmoi/v2/cmd/chezmoi@v2.71.1' >/dev/null ||
  fail "release test job installs pinned chezmoi v2.71.1"
printf '%s\n' "$test_job" | grep -F 'echo "$(go env GOPATH)/bin" >> "$GITHUB_PATH"' >/dev/null ||
  fail "release test job exposes GOPATH bin to later steps"
printf '%s\n' "$test_job" | grep -F '"$(go env GOPATH)/bin/chezmoi" --version' >/dev/null ||
  fail "release test job verifies the installed chezmoi binary"
printf '%s\n' "$test_job" | grep -F 'apt-get install -y zsh' >/dev/null ||
  fail "release test job installs zsh with Ubuntu package tooling"
printf '%s' "$workflow" | grep -F 'release_base="https://github.com/${GITHUB_REPOSITORY}/releases/latest/download"' >/dev/null ||
  fail "versioned installer repairs from the latest stable release base"
for required in \
  'CGO_ENABLED: "0"' \
  'scripts/build-tpod-release.sh' \
  'scripts/package-source.sh' \
  '--catalog-source catalog/v1/resources.json' \
  '--catalog-output artifacts/resources.json' \
  'internal-release-contract-check' \
  'gh release create' \
  'install.sh'; do
  printf '%s' "$workflow" | grep -F -- "$required" >/dev/null ||
    fail "release workflow contains $required"
done
for removed in \
  'RELEASE_ROOT_KEY_ID' \
  'RELEASE_ROOT_PUBLIC_KEY' \
  'RELEASE_SIGNING_PRIVATE_KEY'; do
  printf '%s' "$workflow" | grep -F -- "$removed" >/dev/null &&
    fail "release workflow contains removed configuration $removed"
done
printf '%s' "$workflow" | grep -F 'release.json.sig' >/dev/null &&
  fail "release workflow publishes a signature envelope"
expected_assets='artifacts/tpod-darwin-amd64
artifacts/tpod-darwin-arm64
artifacts/tpod-linux-amd64
artifacts/tpod-linux-arm64
artifacts/terrapod-source.tar.gz
artifacts/resources.json
artifacts/release.json
artifacts/install.sh'
published_assets="$(printf '%s\n' "$workflow" | extract_published_assets)"
[ "$published_assets" = "$expected_assets" ] ||
  fail "release workflow must publish exactly the expected eight assets"
printf '%s' "$workflow" | grep -F 'pull-requests: write' >/dev/null &&
  fail "release workflow requests unrelated permissions"
pass "release workflow has the required bounded publication steps"

for removed in \
  'RELEASE_ROOT_KEY_ID' \
  'RELEASE_ROOT_PUBLIC_KEY' \
  'RELEASE_SIGNING_PRIVATE_KEY' \
  'release\.json\.sig' \
  'crypto/ed25519'
do
  if grep -R -n -E "$removed" \
    "$repo_root/cmd" \
    "$repo_root/internal" \
    "$repo_root/scripts" \
    "$repo_root/install.sh" \
    "$repo_root/.github/workflows/release.yml" >/dev/null; then
    fail "production implementation contains removed signing term $removed"
  fi
done
pass "production implementation has no removed signing dependencies"
