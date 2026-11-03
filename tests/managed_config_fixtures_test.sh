#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/managed-config-fixtures.sh"
make_tmp_dir

for profile in macos-terminal vps-shell; do
  config="$tmp_dir/$profile.toml"
  write_complete_config "$config" "$profile"
  assert_equals "$(managed_setup_verdict_state "$(managed_setup_config_verdict "$config")")" complete "default $profile fixture passes the verdict"
  assert_equals "$(awk '/ = / { print $1 }' "$config" | sort | uniq -d)" '' "fixture has no duplicate keys"
  assert_equals "$(awk '/ = / { print $1 }' "$config" | sort)" "$(managed_setup_keys | sort)" "fixture has exactly the current managed keys"
  assert_file_contains "$config" "profile = \"$profile\"" "fixture stores the chosen profile"
  assert_file_not_contains "$config" '= true' "default fixture disables every Managed Setting"
  assert_file_contains "$config" 'enableMacosAppGroupAiApps = false' "fixture includes non-applicable settings on VPS"

  write_complete_config "$config" "$profile" enableEditorStack=true enableColimaDockerHost=true
  assert_equals "$(managed_setup_verdict_state "$(managed_setup_config_verdict "$config")")" complete "boolean overrides pass the $profile verdict"
  assert_file_contains "$config" 'enableEditorStack = true' "fixture applies the Editor Stack override"
  assert_file_contains "$config" 'enableColimaDockerHost = true' "fixture applies the shell-environment override"
done

# An isolated schema extension must not change unrelated input fixtures or
# scripted answers. Exercise Setup itself through a PTY, with the same script
# before and after the extension, rather than calling its internal functions.
. "$repo_root/tests/lib/test-support.sh"
fixture_repo="$tmp_dir/source"
mkdir -p "$fixture_repo/dot_local/bin" "$fixture_repo/dot_local/lib/terrapod" "$fixture_repo/tests/lib" "$tmp_dir/bin" "$tmp_dir/home"
cp "$repo_root/dot_local/bin/executable_terrapod" "$fixture_repo/dot_local/bin/"
cp "$repo_root/dot_local/lib/terrapod/config-toml.sh" "$fixture_repo/dot_local/lib/terrapod/"
cp "$repo_root/tests/lib/managed-config-fixtures.sh" "$repo_root/tests/lib/gum-stub.sh" "$fixture_repo/tests/lib/"
write_gum_responses "$tmp_dir/responses" preset=development write=yes \
  enableDevelopmentWorkspace=no enableEditorStack=no enableMacosAppGroupMonitoring=yes enableColimaDockerHost=yes

for schema in original extended; do
  if [ "$schema" = extended ]; then
    awk '
      /^enableColimaDockerHost\|/ {
        print "enableFixtureApps|macos-app-group|fixture-apps|macos-terminal|development,workstation|Synthetic fixture apps"
      }
      { print }
    ' "$fixture_repo/dot_local/lib/terrapod/config-toml.sh" >"$tmp_dir/extended-reader"
    mv "$tmp_dir/extended-reader" "$fixture_repo/dot_local/lib/terrapod/config-toml.sh"
  fi
  (
    repo_root="$fixture_repo"
    . "$repo_root/tests/lib/managed-config-fixtures.sh"
    for profile in macos-terminal vps-shell; do
      write_complete_config "$tmp_dir/$schema-$profile.toml" "$profile" enableEditorStack=true
      assert_equals "$(managed_setup_verdict_state "$(managed_setup_config_verdict "$tmp_dir/$schema-$profile.toml")")" complete "$schema schema fixture remains complete on $profile"
    done
    write_gum_stub "$tmp_dir/bin/gum"
  )
  command_text="env TERM=xterm HOME=$(shell_quote "$tmp_dir/home") XDG_CONFIG_HOME=$(shell_quote "$tmp_dir/$schema-xdg") TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG= sh $(shell_quote "$fixture_repo/dot_local/bin/executable_terrapod") setup"
  PATH="$tmp_dir/bin:$PATH" TERRAPOD_GUM_RESPONSES="$tmp_dir/responses" TERRAPOD_GUM_LOG="$tmp_dir/$schema-gum.log" \
    run_in_pty "$command_text" >"$tmp_dir/$schema-setup.out"
  (
    . "$fixture_repo/dot_local/lib/terrapod/config-toml.sh"
    assert_setup_questions "$tmp_dir/$schema-gum.log" macos-terminal false
  )
  config="$tmp_dir/$schema-xdg/chezmoi/chezmoi.toml"
  assert_file_contains "$config" 'enableDevelopmentWorkspace = false' "$schema schema preserves the Workspace override"
  assert_file_contains "$config" 'enableEditorStack = false' "$schema schema preserves the leaf-stack override"
  assert_file_contains "$config" 'enableAiCliTools = true' "$schema schema follows the enabled Preset default"
  assert_file_contains "$config" 'enableMacosAppGroupMonitoring = true' "$schema schema preserves the App Group override"
  assert_file_contains "$config" 'enableColimaDockerHost = true' "$schema schema preserves the later shell-environment answer"
  assert_file_contains "$tmp_dir/$schema-gum.log" 'gum question: write' "$schema schema reaches the named final confirmation"
done
assert_file_contains "$tmp_dir/extended-macos-terminal.toml" 'enableFixtureApps = false' "the builder includes a new setting with a bare false default"
assert_file_contains "$tmp_dir/extended-vps-shell.toml" 'enableFixtureApps = false' "the VPS builder includes the non-applicable new setting"
assert_file_contains "$tmp_dir/extended-gum.log" 'confirm Enable fixture-apps? --default=true' "Setup asks the new applicable question with its actual default"
assert_file_contains "$config" 'enableFixtureApps = true' "the unscripted new question follows its actual default"
(
  . "$fixture_repo/dot_local/lib/terrapod/config-toml.sh"
  assert_equals "$(managed_setup_verdict_state "$(managed_setup_config_verdict "$config")")" complete "Setup writes a complete config under the extended schema"
)

# An unrelated answer must never stand in for final write consent, even after
# a new setting has been inserted before that prompt.
sed '/^write=/d' "$tmp_dir/responses" >"$tmp_dir/no-write-responses"
command_text="env TERM=xterm HOME=$(shell_quote "$tmp_dir/home") XDG_CONFIG_HOME=$(shell_quote "$tmp_dir/no-write-xdg") TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG= sh $(shell_quote "$fixture_repo/dot_local/bin/executable_terrapod") setup"
setup_status=0
PATH="$tmp_dir/bin:$PATH" TERRAPOD_GUM_RESPONSES="$tmp_dir/no-write-responses" TERRAPOD_GUM_LOG="$tmp_dir/no-write-gum.log" \
  run_in_pty "$command_text" >"$tmp_dir/no-write-setup.out" || setup_status=$?
assert_status "$setup_status" 1 "Setup rejects an omitted final confirmation under the extended schema"
assert_file_contains "$tmp_dir/no-write-setup.out" 'setup cancelled' "missing final confirmation explains cancellation"
[ ! -e "$tmp_dir/no-write-xdg/chezmoi/chezmoi.toml" ] || fail "missing final confirmation must not write a config"
pass "missing final confirmation leaves no config"
