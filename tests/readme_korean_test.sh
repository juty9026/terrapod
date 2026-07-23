#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
readme="$repo_root/README.md"
korean_readme="$repo_root/README.ko.md"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

assert_file_exists() {
  file="$1"
  message="$2"

  if [ ! -f "$file" ]; then
    fail "$message"
  fi

  pass "$message"
}

assert_contains() {
  file="$1"
  needle="$2"
  message="$3"

  if ! grep -F "$needle" "$file" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

assert_not_contains() {
  file="$1"
  needle="$2"
  message="$3"

  if grep -F "$needle" "$file" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

extract_headings() {
  file="$1"

  awk '
    /^```/ {
      in_fence = !in_fence
      next
    }

    !in_fence && /^#{1,6} / {
      print
    }
  ' "$file" || true
}

assert_headings_ignore_fenced_code() {
  fixture="$tmp_dir/fenced-headings.md"
  actual="$tmp_dir/fenced-headings.actual"
  expected="$tmp_dir/fenced-headings.expected"

  cat >"$fixture" <<'EOF'
# Visible heading
```sh
# Ignored shell comment
```
## Second visible heading
EOF

  extract_headings "$fixture" >"$actual"
  {
    printf '%s\n' "# Visible heading"
    printf '%s\n' "## Second visible heading"
  } >"$expected"

  if ! cmp -s "$expected" "$actual"; then
    fail "heading extraction ignores fenced code blocks"
  fi

  pass "heading extraction ignores fenced code blocks"
}

assert_headings_ignore_fenced_code

assert_file_exists "$readme" "README.md exists"
assert_file_exists "$korean_readme" "README.ko.md exists"

assert_contains "$readme" '🌐 Language: **English** | [한국어](README.ko.md)' \
  "README.md has the agreed language switcher"
assert_contains "$korean_readme" '🌐 언어: [English](README.md) | **한국어**' \
  "README.ko.md has the agreed language switcher"
assert_not_contains "$korean_readme" '번역본' \
  "README.ko.md does not label itself as a translation"
assert_contains "$korean_readme" 'sh -c "$(curl -fsLS https://github.com/juty9026/terrapod/releases/latest/download/install.sh)"' \
  "README.ko.md documents the full installer command"
assert_not_contains "$korean_readme" 'https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh' \
  "README.ko.md does not document the unresolved raw installer URL"
assert_contains "$korean_readme" '`terrapod configure <Preset>`는 script-friendly Preset configuration' \
  "README.ko.md documents configure as script-friendly Preset configuration"
assert_contains "$korean_readme" 'command입니다. 지원되는 Preset 정확히 하나의 concrete settings를 쓰고,' \
  "README.ko.md documents configure writes concrete settings for one Preset"
assert_contains "$korean_readme" 'Terrapod Setup은 `gum`(Bootstrap UI Dependency)을 사용하며, gum이 지원하는' \
  "README.ko.md documents Bootstrap UI Dependency requirement for setup"
assert_contains "$korean_readme" '`gum`이 필요 없으며, interactive customization은 제공하지 않습니다.' \
  "README.ko.md documents configure as no-gum and non-interactive"
assert_contains "$korean_readme" 'Plain text fallback은 없습니다.' \
  "README.ko.md documents no plain text fallback behavior"
assert_contains "$korean_readme" 'initial apply가 끝나면 installer는 `tpod help`를' \
  "README.ko.md documents tpod help after first-run apply"
assert_contains "$korean_readme" 'bootstrap 이후의 day-to-day 관리 명령은 `tpod`입니다.' \
  "README.ko.md presents tpod as the day-to-day command"
assert_contains "$korean_readme" '`terrapod configure <Preset>`는 Terrapod Setup의 plain fallback이 아닙니다.' \
  "README.ko.md states configure is not a Setup fallback"
assert_contains "$korean_readme" '`terrapod configure <Preset>`는 setup UI 없이 설정을 쓰는' \
  "README.ko.md verifies configure writes without setup UI"
assert_contains "$korean_readme" 'script-friendly 경로입니다. 이 경로는 Terrapod Setup과 의도적으로 분리되어' \
  "README.ko.md documents setup and configure are separate by design"
assert_contains "$korean_readme" 'Terrapod Setup이 `gum` 또는 interactive terminal 부재로 실행되지 않으면' \
  "README.ko.md documents missing-gum Setup recovery start"
assert_contains "$korean_readme" '`gum` 또는 terminal environment를 고친 뒤 `terrapod setup`을 다시 실행합니다.' \
  "README.ko.md documents missing-gum Setup recovery"
assert_contains "$korean_readme" 'Homebrew는 지원되는 두 profile 모두에서 Core Shell Stack의 Modern CLI Provider입니다.' \
  "README.ko.md names Homebrew as the cross-profile Modern CLI Provider"
assert_contains "$korean_readme" 'mise는 Bun, Node.js, Python, uv의 Development Runtime Manager입니다.' \
  "README.ko.md limits mise to development runtimes"
assert_contains "$korean_readme" 'Apple Silicon에서는 Homebrew를 `/opt/homebrew`에 설치하고, Intel Mac에서는 `/usr/local`에 설치합니다.' \
  "README.ko.md documents macOS architecture-to-prefix mapping"
assert_contains "$korean_readme" 'Ubuntu 24.04는 모든 Preset에서 `/home/linuxbrew/.linuxbrew`에 Homebrew를 설치합니다.' \
  "README.ko.md documents mandatory Linuxbrew"
assert_contains "$korean_readme" 'first-run installer는 Terrapod Setup 전에 Homebrew로 `chezmoi`와 `gum`을 설치합니다.' \
  "README.ko.md documents cross-profile Setup bootstrap"
assert_contains "$korean_readme" '설치 전에 1 vCPU, 1 GiB RAM, 최소 3 GiB의 여유 disk space를 권장' \
  "README.ko.md documents the recommended VPS floor"
assert_contains "$korean_readme" '`x86_64`와 `aarch64`' \
  "README.ko.md documents supported Ubuntu architectures"
assert_not_contains "$korean_readme" 'get.chezmoi.io' \
  "README.ko.md removes the standalone chezmoi installer"
assert_not_contains "$korean_readme" 'Charm APT' \
  "README.ko.md removes the Charm APT trust boundary"
assert_not_contains "$korean_readme" '공식 mise APT repository' \
  "README.ko.md removes mise APT ownership"
assert_contains "$korean_readme" '`~/.config/terrapod/config.json`' \
  "README.ko.md documents the independent Terrapod config"
assert_contains "$korean_readme" 'declared-root ownership' \
  "README.ko.md documents the ownership boundary"
assert_not_contains "$korean_readme" '| `gitAllowedSigners` |' \
  "README.ko.md excludes unrelated chezmoi data from the Terrapod config table"
assert_not_contains "$korean_readme" '"gitAllowedSigners"' \
  "README.ko.md excludes unrelated chezmoi data from Terrapod JSON examples"
assert_contains "$korean_readme" '`gitAllowedSigners`는 independent Terrapod config field가 아닙니다.' \
  "README.ko.md documents the unrelated chezmoi root-data boundary"
assert_contains "$korean_readme" 'authoring workflow가 chezmoi를 직접 사용해 render 또는 apply해야 합니다.' \
  "README.ko.md documents how unrelated chezmoi root data is applied"
assert_not_contains "$korean_readme" '그 다음 environment를 reconcile합니다.' \
  "README.ko.md does not imply tpod applies unrelated chezmoi root data"
assert_contains "$korean_readme" '`development-apps`: Zed와 Orca ADE(`stablyai/orca/orca`).' \
  "README.ko.md documents Zed and Orca ADE in the development-apps inventory"
assert_contains "$korean_readme" '| `enableMacosAppGroupDevelopmentApps` | `false` | development-apps macOS App Group인 Zed와 Orca ADE(`stablyai/orca/orca`)를 설치합니다. |' \
  "README.ko.md documents Zed and Orca ADE on the development-apps option row"
assert_contains "$korean_readme" 'Terrapod은 Orca를 설치할 때 fully-qualified `stablyai/orca/orca` cask만 trust하며, `stablyai/orca` tap 전체를 trust하지 않습니다.' \
  "README.ko.md documents Orca's cask-specific trust boundary"
assert_not_contains "$korean_readme" 'Terminal font cask' \
  "README.ko.md no longer documents terminal font casks"
assert_contains "$korean_readme" '최신 안정 GitHub release에서 제공하는 Jetendard terminal font' \
  "README.ko.md documents the Jetendard release source"
assert_contains "$korean_readme" 'Terrapod은 managed font installer source가 변경되거나 실패한 설치를 재시도할 때만 최신 Jetendard release를 확인합니다.' \
  "README.ko.md documents the Jetendard release-check trigger"
assert_contains "$korean_readme" 'Terrapod은 해당 Jetendard release의 모든 TTF를 설치하고 GitHub에서 제공하는 asset digest를 검증합니다.' \
  "README.ko.md directly documents every-TTF installation and digest verification"
assert_contains "$korean_readme" 'Ghostty, Zed buffer와 terminal, Orca terminal에서 사용하는 font-family key만 설정합니다.' \
  "README.ko.md directly documents the app-key-only settings scope"
assert_contains "$korean_readme" '기존 window가 cached font를 계속 사용하면 Ghostty, Zed 또는 Orca를 다시 시작해야 할 수 있습니다.' \
  "README.ko.md directly documents cached-font restart guidance"
assert_contains "$korean_readme" 'Jetendard 설정 적용이 보류되면 Orca를 종료한 뒤 `tpod apply`를 다시 실행합니다.' \
  "README.ko.md documents Orca font-setting recovery"
assert_contains "$korean_readme" '`enableMacosAppGroupAiApps`는 deprecated key이며 alias로 해석하지 않습니다.' \
  "README.ko.md documents explicit development-apps key migration"
assert_contains "$korean_readme" '`tpod plan`' \
  "README.ko.md documents plan"
assert_contains "$korean_readme" '`tpod apply`' \
  "README.ko.md documents apply"
assert_contains "$korean_readme" '`tpod update`' \
  "README.ko.md documents update"
assert_contains "$korean_readme" '`tpod resolve <resource-id>`' \
  "README.ko.md documents conflict resolution"
assert_contains "$korean_readme" 'Terrapod-owned resource를 자동으로 prune' \
  "README.ko.md documents automatic owned-resource pruning"
assert_contains "$korean_readme" '`brew uninstall --zap`을 사용하지 않습니다' \
  "README.ko.md documents the Homebrew uninstall boundary"
assert_contains "$korean_readme" '`ready`' \
  "README.ko.md documents ready state"
assert_contains "$korean_readme" '`unavailable`' \
  "README.ko.md documents unavailable state"
assert_contains "$korean_readme" 'read-only chezmoi escape hatch' \
  "README.ko.md documents constrained direct chezmoi access"
assert_contains "$korean_readme" '`install.sh --migrate`' \
  "README.ko.md documents the maintainer migration"
assert_contains "$korean_readme" 'Terrapod은 canonical GitHub repository가 HTTPS로 제공하는 Stable Release metadata를 검증하고,' \
  "README.ko.md documents the canonical Stable Release metadata authority"
assert_contains "$korean_readme" 'activation 전에 모든 asset의 size와 SHA-256 digest를 확인합니다.' \
  "README.ko.md documents Stable Release asset validation"
assert_contains "$korean_readme" '먼저 `tpod update`를 한 번 실행하고 출력된 안내를 따른 뒤, 다시 한 번 실행합니다.' \
  "README.ko.md documents the first legacy update invocation"
assert_contains "$korean_readme" '두 번째 호출이 one-shot manager transition을 자동으로 수행합니다.' \
  "README.ko.md documents the second legacy update invocation"
for removed in \
  'Ed25519' \
  'release.json.sig' \
  'RELEASE_SIGNING_PRIVATE_KEY' \
  'latest stable signed Terrapod release'
do
  assert_not_contains "$korean_readme" "$removed" \
    "README.ko.md excludes removed release signing language: $removed"
done
assert_contains "$korean_readme" 'authoring checkout은 active Stable Release와 분리' \
  "README.ko.md documents authoring checkout and Stable Release separation"
assert_contains "$korean_readme" '`install.sh --repair`' \
  "README.ko.md documents repair"
assert_contains "$korean_readme" '`macos-terminal`과 `vps-shell`' \
  "README.ko.md documents supported profiles"
assert_not_contains "$korean_readme" 'package-manager upgrade를 범위 밖에 둡니다' \
  "README.ko.md removes the bootstrap-only package boundary"
assert_not_contains "$korean_readme" 'optional stack을 끄면 chezmoi 관리 대상에서는 제외되지만, machine에 이미 존재하는 file은 제거하지 않습니다.' \
  "README.ko.md removes non-destructive optional stack opt-out"

extract_headings "$readme" >"$tmp_dir/readme.headings"
extract_headings "$korean_readme" >"$tmp_dir/readme-ko.headings"

if ! cmp -s "$tmp_dir/readme.headings" "$tmp_dir/readme-ko.headings"; then
  printf '%s\n' "README heading mismatch:" >&2
  printf '%s\n' "--- README.md" >&2
  sed 's/^/  /' "$tmp_dir/readme.headings" >&2
  printf '%s\n' "--- README.ko.md" >&2
  sed 's/^/  /' "$tmp_dir/readme-ko.headings" >&2
  fail "README.ko.md mirrors README.md heading text and order"
fi

pass "README.ko.md mirrors README.md heading text and order"
