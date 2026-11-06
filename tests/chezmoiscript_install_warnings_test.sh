#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"
inlined_warning_scripts="
.chezmoiscripts/run_before_00-bootstrap-ubuntu.sh.tmpl
.chezmoiscripts/run_after_65-install-jetendard-font.sh.tmpl
.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl
.chezmoiscripts/run_before_20-install-gh-extensions.sh.tmpl
.chezmoiscripts/run_before_30-install-shell-integrations.sh.tmpl
.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl
.chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl
.chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl
.chezmoiscripts/run_after_80-install-ego-lite.sh.tmpl
"

warning_script_data() {
  case "$1" in
    *run_before_00*)
      printf '%s' "$ubuntu_data"
      ;;
    *)
      printf '%s' "$macos_data"
      ;;
  esac
}

# Content checksums either feed chezmoi's onchange scheduling or the explicit
# successful-source gate. Other always-run scripts have no checksum consumer.
for checksum_script_template in "$repo_root"/.chezmoiscripts/*.tmpl; do
  checksum_script_name="${checksum_script_template##*/}"

  case "$checksum_script_name" in
    run_onchange_*)
      raw_template_checksums="$(
        grep -n 'sha256sum' "$checksum_script_template" |
          grep -E '(^|[^A-Za-z])include[[:space:]]+"[^"]*\.tmpl"' || true
      )"

      if [ -n "$raw_template_checksums" ]; then
        printf '%s\n' "$raw_template_checksums" | sed 's/^/  /' >&2
        fail "run_onchange checksum over a template source uses includeTemplate: $checksum_script_name"
      fi
      pass "run_onchange checksum over a template source uses includeTemplate: $checksum_script_name"
      ;;
    run_before_00-bootstrap-ubuntu.sh.tmpl|run_after_65-install-jetendard-font.sh.tmpl)
      assert_file_contains "$checksum_script_template" 'installation_fingerprint=' \
        "source-scheduled installer declares its fingerprint: $checksum_script_name"
      assert_file_contains "$checksum_script_template" '"$installation_fingerprint"' \
        "source-scheduled installer consumes its fingerprint: $checksum_script_name"
      ;;
    *)
      vestigial_checksums="$(grep -n 'sha256sum' "$checksum_script_template" || true)"

      if [ -n "$vestigial_checksums" ]; then
        printf '%s\n' "$vestigial_checksums" | sed 's/^/  /' >&2
        fail "always-run chezmoi script carries no vestigial checksum: $checksum_script_name"
      fi
      pass "always-run chezmoi script carries no vestigial checksum: $checksum_script_name"
      ;;
  esac
done

for warning_script in $inlined_warning_scripts; do
  rendered_warning_script="$(render_template "$(warning_script_data "$warning_script")" "$warning_script")"

  printf '%s\n' "$rendered_warning_script" >"$tmp_dir/rendered.sh"
  sh -n "$tmp_dir/rendered.sh" || fail "rendered installer is valid POSIX sh: $warning_script"

  assert_contains \
    "$rendered_warning_script" \
    "terrapod_install_warning_write() {" \
    "always-run script inlines the marker library: $warning_script"

  assert_not_contains \
    "$rendered_warning_script" \
    'if [ -f "$install_warnings_lib" ]; then' \
    "always-run script keeps no install warning loader guard: $warning_script"
done
