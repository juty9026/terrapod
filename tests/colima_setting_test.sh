#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
. "$repo_root/tests/lib/test-support.sh"
make_tmp_dir
terrapod="$repo_root/dot_local/bin/executable_terrapod"
mkdir -p "$tmp_dir/home"

for profile in macos-terminal vps-shell; do
  for preset in minimal development workstation; do
    [ "$profile/$preset" != vps-shell/workstation ] || continue
    config="$tmp_dir/$profile-$preset.toml"
    HOME="$tmp_dir/home" TERRAPOD_PROFILE="$profile" TERRAPOD_CHEZMOI_CONFIG="$config" \
      sh "$terrapod" configure "$preset" >/dev/null
    assert_file_contains "$config" 'enableColimaDockerHost = false' \
      "$profile $preset leaves Colima Docker Host Selection disabled"
  done
done

# Exercise the real Setup command through its terminal interface. Only gum,
# the external prompt UI, is replaced with scripted answers.
mkdir -p "$tmp_dir/bin"
write_gum_stub "$tmp_dir/bin/gum" cancel default
write_gum_responses "$tmp_dir/responses" minimal no no no no no no no no no no no yes yes
command_text="env TERM=xterm HOME=$(shell_quote "$tmp_dir/home") XDG_CONFIG_HOME=$(shell_quote "$tmp_dir/xdg") TERRAPOD_PROFILE=macos-terminal TERRAPOD_CHEZMOI_CONFIG= sh $(shell_quote "$terrapod") setup"
PATH="$tmp_dir/bin:$PATH" TERRAPOD_GUM_RESPONSES="$tmp_dir/responses" TERRAPOD_GUM_LOG="$tmp_dir/gum.log" \
  run_in_pty "$command_text" >"$tmp_dir/setup.out"
config="$tmp_dir/xdg/chezmoi/chezmoi.toml"
assert_file_contains "$config" 'enableColimaDockerHost = true' "macOS Setup can explicitly enable Colima"
assert_file_contains "$config" 'enableDevelopmentWorkspace = false' "Colima does not enable the Development Workspace"
assert_file_contains "$config" 'enableMacosAppGroupDevelopmentApps = false' "Colima does not enable development-apps"

run_tpod() {
  HOME="$tmp_dir/home" PATH="$tmp_dir/bin:/usr/bin:/bin" TERRAPOD_PROFILE="${test_profile:-macos-terminal}" \
    TERRAPOD_CHEZMOI_CONFIG="$config" sh "$terrapod" "$@"
}
assert_contains "$(run_tpod status | tr -s ' ')" 'Colima Docker Host Selection : enabled' "status reports the connection setting"
doctor_output="$(run_tpod doctor 2>&1 || true)"
assert_contains "$doctor_output" 'Colima Docker Host Selection is enabled' "doctor reports the selection without requiring Colima"

cp "$config" "$tmp_dir/enabled.toml"
write_stub "$tmp_dir/bin/chezmoi" 'printf "%s\n" "$*" >>"$HOME/chezmoi-calls"' 'exit 99'
sed '/^enableColimaDockerHost = /d' "$config" >"$tmp_dir/incomplete.toml"
config="$tmp_dir/incomplete.toml"
for command in status doctor apply; do
  output="$(run_tpod "$command" 2>&1 || true)"
  assert_contains "$output" 'incomplete managed setup config' "$command reports an incomplete config"
  assert_contains "$output" 'enableColimaDockerHost' "$command explains the missing setting"
  assert_contains "$output" 'tpod setup' "$command provides explicit setup guidance"
  assert_not_contains "$output" 'Customize Terrapod settings' "$command does not open Setup automatically"
done
[ ! -e "$tmp_dir/home/chezmoi-calls" ] || fail "incomplete config must stop before invoking chezmoi"
pass "incomplete config stops apply before invoking chezmoi"

sed 's/enableColimaDockerHost = true/enableColimaDockerHost = "false"/' "$tmp_dir/enabled.toml" >"$tmp_dir/invalid.toml"
config="$tmp_dir/invalid.toml"
for command in status doctor apply; do
  output="$(run_tpod "$command" 2>&1 || true)"
  assert_contains "$output" 'unsupported Managed Setting value for enableColimaDockerHost' "$command rejects string booleans"
done

config="$tmp_dir/enabled.toml"
cp "$config" "$tmp_dir/before.toml"
run_tpod configure minimal --yes >/dev/null
assert_file_contains "$config" 'enableColimaDockerHost = false' "configure writes a concrete disabled value"
set -- "$config".terrapod-backup-*
assert_equals "$#" 1 "configure backs up the existing config once"
cmp -s "$1" "$tmp_dir/before.toml" || fail "backup preserves the enabled Colima setting"
pass "backup preserves the enabled Colima setting"

test_profile=vps-shell
assert_contains "$(run_tpod status | tr -s ' ')" 'Colima Docker Host Selection : not applicable' "VPS status marks Colima not applicable"
write_gum_responses "$tmp_dir/responses" minimal no no yes
command_text="env TERM=xterm HOME=$(shell_quote "$tmp_dir/home") XDG_CONFIG_HOME=$(shell_quote "$tmp_dir/vps-xdg") TERRAPOD_PROFILE=vps-shell TERRAPOD_CHEZMOI_CONFIG= sh $(shell_quote "$terrapod") setup"
PATH="$tmp_dir/bin:$PATH" TERRAPOD_GUM_RESPONSES="$tmp_dir/responses" TERRAPOD_GUM_LOG="$tmp_dir/vps-gum.log" \
  run_in_pty "$command_text" >"$tmp_dir/vps-setup.out"
assert_file_not_contains "$tmp_dir/vps-gum.log" 'Enable Colima Docker Host Selection?' "VPS Setup does not offer Colima"
assert_file_contains "$tmp_dir/vps-xdg/chezmoi/chezmoi.toml" 'enableColimaDockerHost = false' "VPS Setup stores false"
