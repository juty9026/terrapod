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
assert_contains "$korean_readme" 'Terrapod Setup 실행 전, first-run installer는 `gum` 누락 시 Homebrew로 `gum`을' \
  "README.ko.md documents macOS bootstrap UI dependency setup before setup"
assert_contains "$korean_readme" '`gum` 설치에만 해당되며 broad Homebrew upgrade는 실행하지 않습니다.' \
  "README.ko.md verifies macOS bootstrap UI dependency scope excludes broad Homebrew upgrades"
assert_contains "$korean_readme" 'Terrapod Setup 실행 전, first-run installer는 `gum` 누락 시 Charm APT' \
  "README.ko.md documents Ubuntu bootstrap UI dependency setup before setup"
assert_contains "$korean_readme" 'repository를 등록하고 APT로 `gum`을 설치해 Bootstrap UI Dependency를' \
  "README.ko.md documents Ubuntu Bootstrap UI Dependency boundary"
assert_contains "$korean_readme" '준비합니다. 이 Bootstrap UI bootstrap은 `gum` 설치에만 해당되며 broad APT upgrade는 실행하지 않습니다.' \
  "README.ko.md verifies Ubuntu bootstrap UI dependency scope excludes broad APT upgrades"
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
assert_contains "$korean_readme" 'Terrapod은 기존에 설치된 JetBrains Mono Nerd Font 또는 D2Coding을 제거하지 않습니다.' \
  "README.ko.md documents non-destructive legacy font migration"
assert_contains "$korean_readme" 'Jetendard 설정 적용이 보류되면 Orca를 종료한 뒤 `tpod apply`를 다시 실행합니다.' \
  "README.ko.md documents Orca font-setting recovery"
assert_contains "$korean_readme" 'brew upgrade --cask claude-code codex antigravity-cli' \
  "README.ko.md documents targeted AI CLI upgrades"
assert_contains "$korean_readme" '`enableMacosAppGroupAiApps`는 deprecated key이며 alias로 해석하지 않습니다.' \
  "README.ko.md documents explicit development-apps key migration"

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
