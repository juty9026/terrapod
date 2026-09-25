#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
. "$repo_root/tests/lib/chezmoi-test-support.sh"
inlined_warning_scripts="
.chezmoiscripts/run_before_01-retry-ubuntu-bootstrap.sh.tmpl
.chezmoiscripts/run_before_02-retry-jetendard-font.sh.tmpl
.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl
.chezmoiscripts/run_before_20-install-gh-extensions.sh.tmpl
.chezmoiscripts/run_before_30-install-shell-integrations.sh.tmpl
.chezmoiscripts/run_before_60-install-ai-cli-tools.sh.tmpl
.chezmoiscripts/run_after_20-install-mise-tools.sh.tmpl
.chezmoiscripts/run_after_70-apply-jetendard-settings.sh.tmpl
"

path_sourced_warning_scripts="
.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl
.chezmoiscripts/run_onchange_after_65-install-jetendard-font.sh.tmpl
"

warning_script_data() {
  case "$1" in
    *run_before_01*|*run_onchange_before_00*)
      printf '%s' "$ubuntu_data"
      ;;
    *)
      printf '%s' "$macos_data"
      ;;
  esac
}

# A `sha256sum` over a source file only does anything in a run_onchange_ script,
# where chezmoi hashes the rendered content to decide whether to run. Anywhere
# else it reads as change-gating that does not exist. And where it is
# load-bearing it has to hash what the user actually gets: `include` of a
# template returns the raw source, which does not change when a setting toggles
# an App Group on, so a checksum over a `.tmpl` source must use
# `includeTemplate … .`.
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

  assert_contains \
    "$rendered_warning_script" \
    "terrapod_install_warning_write() {" \
    "always-run script inlines the marker library: $warning_script"

  assert_not_contains \
    "$rendered_warning_script" \
    'if [ -f "$install_warnings_lib" ]; then' \
    "always-run script keeps no install warning loader guard: $warning_script"
done

for warning_script in $path_sourced_warning_scripts; do
  rendered_warning_script="$(render_template "$(warning_script_data "$warning_script")" "$warning_script")"

  assert_not_contains \
    "$rendered_warning_script" \
    "terrapod_install_warning_write() {" \
    "run_onchange script keeps the marker library out of its content hash: $warning_script"

  assert_contains \
    "$rendered_warning_script" \
    "/dot_local/lib/terrapod/install-warnings.sh" \
    "run_onchange script sources the marker library by path: $warning_script"

  assert_not_contains \
    "$rendered_warning_script" \
    "declare_install_warning_category() {" \
    "run_onchange script keeps the policy layer out of its content hash: $warning_script"

  assert_contains \
    "$rendered_warning_script" \
    "/dot_local/lib/terrapod/install-warning-script.sh" \
    "run_onchange script sources the policy layer by path: $warning_script"

  assert_not_contains \
    "$rendered_warning_script" \
    'if [ -f "$install_warnings_lib" ]; then' \
    "run_onchange script keeps no install warning loader guard: $warning_script"
done

# chezmoi reruns a run_onchange_ script when its rendered content changes, so
# the rendered text decides which library edits re-trigger an installer. Each
# installer's install body must be in that hash; the marker libraries, the
# policy layer and the Jetendard guidance text must not be.
content_hash_source="$tmp_dir/content-hash-source"
mkdir -p "$content_hash_source"
cp -R "$repo_root/." "$content_hash_source"

render_after_library_edit() {
  warning_script="$1"
  library="$2"
  library_path="$content_hash_source/dot_local/lib/terrapod/$library"

  cp "$library_path" "$library_path.orig"
  printf '%s\n' '# content hash probe' >>"$library_path"
  render_template_from_source "$(warning_script_data "$warning_script")" "$warning_script" "$content_hash_source"
  mv "$library_path.orig" "$library_path"
}

for warning_script in $path_sourced_warning_scripts; do
  baseline_render="$(render_template_from_source "$(warning_script_data "$warning_script")" "$warning_script" "$content_hash_source")"

  for unhashed_library in install-warnings.sh install-warning-script.sh jetendard-font-status.sh; do
    assert_equals \
      "$(render_after_library_edit "$warning_script" "$unhashed_library")" \
      "$baseline_render" \
      "run_onchange script content ignores edits to $unhashed_library: $warning_script"
  done

  case "$warning_script" in
    *bootstrap-ubuntu*) install_body_library=ubuntu-bootstrap.sh ;;
    *install-jetendard-font*) install_body_library=jetendard-font-install.sh ;;
  esac

  assert_file_exists \
    "$repo_root/dot_local/lib/terrapod/$install_body_library" \
    "install body library exists: $install_body_library"

  if [ "$(render_after_library_edit "$warning_script" "$install_body_library")" = "$baseline_render" ]; then
    fail "run_onchange script content changes with its install body library $install_body_library: $warning_script"
  fi
  pass "run_onchange script content changes with its install body library $install_body_library: $warning_script"
done
