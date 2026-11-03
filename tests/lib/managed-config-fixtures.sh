# Complete input fixtures, independent of Preset expansion and config writes.
# Requires repo_root; source after tests/lib/harness.sh.
. "$repo_root/dot_local/lib/terrapod/config-toml.sh"

# render_complete_config [profile] [setting=true|false ...]
# All schema keys are included even when they do not apply to the profile.
# Apply syntax/value defects explicitly after building a valid base fixture.
render_complete_config() (
  fixture_profile="${1:-macos-terminal}"
  if [ "$#" -gt 0 ]; then shift; fi
  fixture_keys="$(managed_setting_keys)"
  for fixture_override do
    case "$fixture_override" in
      *=true|*=false) ;;
      *) fail "expected a boolean fixture override: $fixture_override" ;;
    esac
    printf '%s\n' "$fixture_keys" | grep -Fx "${fixture_override%%=*}" >/dev/null ||
      fail "unknown fixture setting: ${fixture_override%%=*}"
  done
  {
    printf '%s\n' '[data]' "profile = \"$fixture_profile\""
    for fixture_key in $fixture_keys; do
      fixture_value=false
      for fixture_override do
        if [ "${fixture_override%%=*}" = "$fixture_key" ]; then
          fixture_value="${fixture_override#*=}"
        fi
      done
      printf '%s = %s\n' "$fixture_key" "$fixture_value"
    done
  }
)

# write_complete_config <path> [profile] [setting=true|false ...]
write_complete_config() (
  fixture_path="$1"
  shift
  render_complete_config "$@" >"$fixture_path"
)
