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
  "README.ko.md documents script-friendly Preset configuration"
assert_contains "$korean_readme" '지원되는 Preset 정확히 하나의 concrete settings를 쓰고,' \
  "README.ko.md documents configure writes concrete settings for one Preset"
assert_contains "$korean_readme" '`gum`이 필요 없으며, interactive customization은 제공하지 않습니다.' \
  "README.ko.md documents configure as no-gum and non-interactive"
assert_contains "$korean_readme" '`terrapod configure <Preset>`는 Terrapod Setup의 plain fallback이 아닙니다.' \
  "README.ko.md states configure is not a Setup fallback"
assert_contains "$korean_readme" 'Terrapod Setup이 `gum` 또는 interactive terminal 부재로 실행되지 않으면' \
  "README.ko.md documents missing-gum Setup recovery start"
assert_contains "$korean_readme" '`gum` 또는 terminal environment를 고친 뒤 `terrapod setup`을 다시 실행합니다.' \
  "README.ko.md documents missing-gum Setup recovery"

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
