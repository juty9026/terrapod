#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"
macos_karabiner_opener="$(render_template "$macos_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
macos_terminal_apps_karabiner_opener="$(render_template "$macos_terminal_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
macos_automation_apps_karabiner_opener="$(render_template "$macos_automation_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
macos_development_apps_karabiner_opener="$(render_template "$macos_development_apps_data" ".chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl")"
assert_texts_differ() {
  left="$1"
  right="$2"
  message="$3"

  if [ "$left" = "$right" ]; then
    fail "$message"
  fi

  pass "$message"
}
assert_contains \
  "$macos_karabiner_opener" \
  "macOS Desktop App Stack enabled: false" \
  "Karabiner opener tracks disabled macOS Desktop App Stack state"

assert_not_contains \
  "$macos_karabiner_opener" \
  "macOS Desktop App Stack Brewfile checksum" \
  "Karabiner opener default skips macOS Desktop App Stack Brewfile checksum"

assert_contains \
  "$macos_automation_apps_karabiner_opener" \
  "macOS Desktop App Stack enabled: true" \
  "Karabiner opener tracks enabled macOS Desktop App Stack state"

assert_contains \
  "$macos_automation_apps_karabiner_opener" \
  "macOS Desktop App Stack Brewfile checksum" \
  "Karabiner opener tracks macOS Desktop App Stack Brewfile changes"

assert_contains \
  "$macos_development_apps_karabiner_opener" \
  "macOS Desktop App Stack enabled: true" \
  "Karabiner opener tracks enabled development-apps Desktop App Stack state"

assert_contains \
  "$macos_development_apps_karabiner_opener" \
  "macOS Desktop App Stack Brewfile checksum" \
  "Karabiner opener tracks development-apps Desktop App Stack Brewfile changes"

assert_texts_differ \
  "$macos_terminal_apps_karabiner_opener" \
  "$macos_automation_apps_karabiner_opener" \
  "Karabiner opener tracks different macOS Desktop App Group combinations"
