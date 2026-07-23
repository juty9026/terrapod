#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
installer="$repo_root/install.sh"
tmp="${TMPDIR:-/tmp}/terrapod-manager-installer-$$"
trap 'rm -rf "$tmp"' 0 1 2 15
mkdir -p "$tmp"

fail() { printf '%s\n' "not ok - $*" >&2; exit 1; }
contains() { case "$1" in *"$2"*) ;; *) fail "$3; missing: $2" ;; esac; }
absent() { case "$1" in *"$2"*) fail "$3; unexpected: $2" ;; *) ;; esac; }
before() {
  remainder="${1#*"$2"}"
  [ "$remainder" != "$1" ] && [ "${remainder#*"$3"}" != "$remainder" ] || fail "$4"
}

[ -x "$installer" ] && sh -n "$installer" || fail "install.sh must be executable POSIX shell"
installer_text="$(cat "$installer")"
contains "$installer_text" "__TERRAPOD_RELEASE_BASE_URL__" \
  "installer keeps the release base placeholder"
absent "$installer_text" "__TERRAPOD_RELEASE_ROOT_KEY_ID__" \
  "installer has no release root key ID placeholder"
absent "$installer_text" "__TERRAPOD_RELEASE_ROOT_PUBLIC_KEY__" \
  "installer has no release root public key placeholder"

make_case() {
  case_dir="$tmp/$1"
  mkdir -p "$case_dir/home/.local/bin" "$case_dir/bin"
  launcher="$case_dir/home/.local/bin/tpod"
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' 'printf "%s\n" "tpod:$*" >>"$TERRAPOD_TEST_LOG"'
    printf '%s\n' 'case "$1" in'
    printf '%s\n' ' setup) exit "${TERRAPOD_TEST_SETUP_STATUS:-0}" ;;'
    printf '%s\n' ' apply) exit "${TERRAPOD_TEST_APPLY_STATUS:-0}" ;;'
    printf '%s\n' ' migrate-current) exit "${TERRAPOD_TEST_MIGRATE_STATUS:-0}" ;;'
    printf '%s\n' 'esac'
  } >"$launcher"
  chmod 755 "$launcher"
  brew="$case_dir/bin/brew"
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' 'if [ "$1" = shellenv ]; then printf "%s\n" "PATH=$PATH; export PATH"; fi'
  } >"$brew"
  chmod 755 "$brew"
  rendered="$case_dir/install.sh"
  sed '$d' "$installer" >"$rendered"
  {
    printf '%s\n' 'detect_profile() { printf "%s\n" macos-terminal; }'
    printf '%s\n' 'profile_label() { printf "%s\n" "$1"; }'
    printf '%s\n' 'ensure_user_local_bin() { mkdir -p "$1"; }'
    printf '%s\n' 'require_non_root_linux_user() { printf "%s\n" "non-root:$1" >>"$TERRAPOD_TEST_LOG"; }'
    printf '%s\n' 'ensure_source_repo_prerequisites() { :; }'
    printf '%s\n' 'warn_low_linuxbrew_disk_space() { :; }'
    printf '%s\n' 'ensure_homebrew() { printf "%s\n" "$TERRAPOD_TEST_BREW"; }'
    printf '%s\n' 'prepare_brew_bootstrap_tools() { printf "%s\n" "bootstrap:chezmoi gum" >>"$TERRAPOD_TEST_LOG"; }'
    printf '%s\n' 'repair_management_core() {'
    printf '%s\n' ' if [ "${1:-}" = --stage-only ]; then'
    printf '%s\n' '   release_version=1.2.3; manifest_digest=test-manifest-digest; data_home="${XDG_DATA_HOME:-$HOME/.local/share}"; releases="$data_home/terrapod/releases";'
    printf '%s\n' '   mkdir -p "$releases/$release_version/bin";'
    printf '%s\n' '   cp "$HOME/.local/bin/tpod" "$releases/$release_version/bin/tpod";'
    printf '%s\n' '   printf "%s\n" "stage-only:manager" >>"$TERRAPOD_TEST_LOG";'
    printf '%s\n' ' else printf "%s\n" "activate:manager" >>"$TERRAPOD_TEST_LOG"; fi'
    printf '%s\n' '}'
    printf '%s\n' 'main "$@"'
  } >>"$rendered"
  chmod 755 "$rendered"
  printf '%s\n' "$case_dir"
}

fresh="$(make_case fresh)"
HOME="$fresh/home" TERRAPOD_TEST_LOG="$fresh/events" TERRAPOD_TEST_BREW="$fresh/bin/brew" \
  "$fresh/install.sh" >"$fresh/stdout" 2>"$fresh/stderr" || fail "fresh manager install must succeed"
events="$(cat "$fresh/events")"
contains "$events" "bootstrap:chezmoi gum" "fresh install bootstraps setup tools"
contains "$events" "activate:manager" "fresh install activates manager"
contains "$events" "tpod:setup" "fresh install runs setup"
contains "$events" "tpod:apply" "fresh install runs apply"
before "$events" "activate:manager" "tpod:setup" "fresh activation must precede setup"
before "$events" "tpod:setup" "tpod:apply" "setup must precede apply"

failed="$(make_case failure)"
if HOME="$failed/home" TERRAPOD_TEST_LOG="$failed/events" TERRAPOD_TEST_BREW="$failed/bin/brew" \
  TERRAPOD_TEST_APPLY_STATUS=69 "$failed/install.sh" >"$failed/stdout" 2>"$failed/stderr"; then
  fail "apply failure must be non-zero"
fi
[ -x "$failed/home/.local/bin/tpod" ] || fail "launcher must survive apply failure"
contains "$(cat "$failed/stderr")" "launcher remains available" "apply failure must print recovery"

migration="$(make_case migration)"
HOME="$migration/home" XDG_DATA_HOME="$migration/data" TERRAPOD_TEST_LOG="$migration/events" \
  TERRAPOD_TEST_BREW="$migration/bin/brew" "$migration/install.sh" --migrate >"$migration/stdout" 2>"$migration/stderr" ||
  fail "migration dispatch must succeed"
events="$(cat "$migration/events")"
contains "$events" "stage-only:manager" "migration must only stage before hidden preflight"
contains "$events" "tpod:migrate-current" "migration must invoke staged hidden command"
absent "$events" "activate:manager" "shell must not activate migration release before hidden preflight"
absent "$events" "tpod:setup" "migration must not run setup"

preflight_failure="$(make_case migration-preflight-failure)"
if HOME="$preflight_failure/home" XDG_DATA_HOME="$preflight_failure/data" TERRAPOD_TEST_LOG="$preflight_failure/events" \
  TERRAPOD_TEST_BREW="$preflight_failure/bin/brew" TERRAPOD_TEST_MIGRATE_STATUS=69 \
  "$preflight_failure/install.sh" --migrate >"$preflight_failure/stdout" 2>"$preflight_failure/stderr"; then
  fail "migration preflight failure must be non-zero"
fi
failure_stderr="$(cat "$preflight_failure/stderr")"
contains "$failure_stderr" "rerun with --migrate" "preflight failure must provide retry guidance"
absent "$failure_stderr" "manager is active" "preflight failure must not claim staged manager is active"

invalid="$(make_case invalid)"
if HOME="$invalid/home" TERRAPOD_TEST_LOG="$invalid/events" TERRAPOD_TEST_BREW="$invalid/bin/brew" \
  "$invalid/install.sh" --unknown >"$invalid/stdout" 2>"$invalid/stderr"; then
  fail "unknown argument must fail"
fi

printf '%s\n' "ok - Terrapod manager installer dispatch"
