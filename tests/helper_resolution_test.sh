#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$repo_root/tests/lib/harness.sh"
terrapod="$repo_root/dot_local/bin/executable_terrapod"
make_tmp_dir

stub_bin="$tmp_dir/bin"
config_file="$tmp_dir/config/chezmoi.toml"
home_dir="$tmp_dir/home"
mkdir -p \
  "$stub_bin" \
  "${config_file%/*}" \
  "$home_dir/.local/state/terrapod/jetendard" \
  "$home_dir/Library/Fonts"

cat >"$home_dir/.local/state/terrapod/jetendard/manifest.json" <<'JSON'
{
  "tag": "v1.0.0",
  "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "files": ["Jetendard-Regular.ttf"]
}
JSON
printf '%s\n' "test font" >"$home_dir/Library/Fonts/Jetendard-Regular.ttf"

cat >"$config_file" <<'EOF'
[data]
profile = "macos-terminal"
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
enableMacosAppGroupMobileDev = false
EOF

cat >"$stub_bin/chezmoi" <<'EOF'
#!/bin/sh
case " $* " in
  *" managed "*)
    printf '%s\n' ".local/bin/terrapod" ".local/bin/tpod"
    ;;
esac
EOF
chmod +x "$stub_bin/chezmoi"

python3_path="$(command -v python3 2>/dev/null || true)"
[ -n "$python3_path" ] || fail "helper resolution tests require python3"
ln -s "$python3_path" "$stub_bin/python3"

# The source checkout carries chezmoi source names, so running the command from
# the checkout must still resolve every helper next to it. install.sh points
# users at this path for setup recovery.
run_from_checkout() {
  HOME="$home_dir" \
    TERRAPOD_PROFILE=macos-terminal \
    TERRAPOD_CHEZMOI_CONFIG="$config_file" \
    PATH="$stub_bin:/usr/bin:/bin" \
    /bin/sh "$terrapod" "$@" 2>&1
}

set +e
checkout_status_output="$(run_from_checkout status)"
checkout_doctor_output="$(run_from_checkout doctor)"
set -e

assert_contains "$checkout_status_output" "Jetendard font                : installed" \
  "tpod status resolves the Jetendard font helper from the source checkout"
assert_not_contains "$checkout_status_output" "executable selection helper is missing" \
  "tpod status resolves the executable selection helper from the source checkout"
assert_not_contains "$checkout_doctor_output" "Jetendard checker is unavailable" \
  "tpod doctor resolves the Jetendard font and settings helpers from the source checkout"
assert_not_contains "$checkout_doctor_output" "executable selection helper is missing" \
  "tpod doctor resolves the executable selection helper from the source checkout"

printf '%s\n' "all helper resolution tests passed"
