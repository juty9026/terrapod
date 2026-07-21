# Homebrew Unified Installation Source Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Homebrew the canonical installation source for macOS GUI applications and all shared user-facing CLI tools on both supported profiles, while retaining mise only for development runtimes and APT only for Ubuntu system bootstrap prerequisites.

**Architecture:** A cross-profile root `Brewfile` declares the mandatory CLI surface, while a new macOS-only Brewfile and the existing optional AI/desktop templates preserve independent failure and retry boundaries. The first-run installer bootstraps standard-prefix Homebrew before Terrapod Setup, and routine apply restores each Homebrew bundle without updating or upgrading it. Terrapod validates that mandatory commands resolve inside the active standard Homebrew prefix but leaves legacy mise, APT, and vendor payloads untouched.

**Tech Stack:** POSIX `sh`, zsh startup templates, chezmoi templates and scripts, Homebrew/Linuxbrew Brewfile DSL, mise TOML, Docker Buildx, repository shell tests.

## Global Constraints

- Supported profiles remain **macOS Terminal Profile** and **VPS Shell Profile**; Linux GUI/desktop support remains out of scope.
- The only **Supported Ubuntu Release** remains Ubuntu 24.04 LTS.
- Supported Ubuntu architectures are exactly `x86_64`/AMD64 and `aarch64`/ARM64; reject 32-bit and unknown architectures before installing Homebrew.
- Ubuntu Homebrew must use `/home/linuxbrew/.linuxbrew`; Apple Silicon macOS must use `/opt/homebrew`; Intel macOS must use `/usr/local`.
- The Ubuntu user model is one non-root management user per machine with initial `sudo` access; shared or nonstandard Linux Homebrew prefixes are unsupported.
- Homebrew owns the 20 mandatory CLI formulae: `bat`, `btop`, `chezmoi`, `dust`, `duf`, `fastfetch`, `fd`, `fzf`, `gh`, `git`, `git-delta`, `gum`, `lazygit`, `lsd`, `mise`, `neovim`, `ripgrep`, `starship`, `zellij`, and `zoxide`.
- mise owns only Bun, Node.js 24, Python 3.13, and uv; pnpm continues to come from Node.js Corepack.
- APT owns only Ubuntu system/bootstrap prerequisites. Remove the Charm and mise APT repositories and never install `gum` or `mise` through APT.
- `terrapod` and `tpod`, Oh My Zsh, Zinit, and SCM Breeze retain their existing project/upstream installation paths.
- `tpod apply` and every managed bundle use `HOMEBREW_NO_AUTO_UPDATE=1` and `brew bundle --no-upgrade`; they never run `brew update`, `brew upgrade`, `brew cleanup`, `mise upgrade`, or destructive uninstall/prune commands.
- Brewfile formulae remain unpinned stable names. New machines receive the stable version available at install time; existing machines change versions only through explicit user-run Homebrew commands.
- Migration and rollback are non-destructive: do not remove legacy mise installs, APT packages, vendor binaries, Homebrew packages, or caches.
- A mandatory CLI resolving outside the standard Homebrew prefix makes `tpod doctor` fail. `tpod status` remains successful and reports the collision. Existing Optional AI Tool Stack collision behavior remains advisory.
- First-run Homebrew/bootstrap failures before Terrapod Setup are hard failures. Declared-state bundle failures after the recovery core exists remain category-scoped warnings with item-level retries.
- Warn, but do not block, when the Ubuntu filesystem that will contain `/home/linuxbrew` has less than 3 GiB available.
- The Canonical README is authoritative; the Korean README must retain matching headings and aligned content.

---

## File Structure

- `Brewfile`: canonical cross-profile list of the 20 mandatory CLI formulae.
- `Brewfile.macos`: mandatory macOS font casks only.
- `Brewfile.ai-cli-tools.tmpl`: existing optional cross-profile AI CLI casks.
- `Brewfile.macos-desktop-apps.tmpl`: existing optional macOS GUI App Groups.
- `dot_config/mise/config.toml.tmpl`: development runtime declarations only.
- `install.sh`: pre-Setup architecture/prefix validation, Ubuntu APT prerequisite bootstrap, standard Homebrew installation, and `chezmoi`/`gum` bootstrap.
- `.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl`: routine Ubuntu system prerequisites and login-shell setup only.
- `.chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl`: cross-profile Homebrew bundle orchestration after Ubuntu prerequisites.
- `.chezmoiscripts/run_before_11-retry-homebrew-core.sh.tmpl`: cross-profile mandatory CLI retry.
- `.chezmoiscripts/run_before_12-retry-homebrew-macos-platform.sh.tmpl`: macOS font retry.
- `.chezmoiscripts/run_before_13-retry-homebrew-desktop-apps.sh.tmpl`: optional macOS desktop retry after mandatory bundles.
- `dot_local/lib/terrapod/homebrew-core-bundle.sh`: reusable bulk-then-item Homebrew bundle runner.
- `dot_local/lib/terrapod/install-warnings.sh`: adds the `homebrew-macos-platform` warning category.
- `.chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl` and `.chezmoiscripts/run_after_21-retry-mise-tools.sh.tmpl`: invoke the standard-prefix Brew-installed mise binary for runtimes.
- `dot_zshenv.tmpl` and `dot_zprofile`: establish cross-profile Homebrew PATH precedence once, before user path snippets.
- `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`: consume already-required standard Homebrew without bootstrapping a second copy.
- `dot_local/bin/executable_terrapod`: report and validate Homebrew ownership of mandatory CLI commands.
- `tests/homebrew_manifests_test.sh`: focused declaration and runtime-boundary tests.
- Existing installer/bootstrap/command/README tests: update behavior-level regressions in their established harnesses.
- `tests/fixtures/homebrew-ubuntu-24.04.Dockerfile`: real non-root Ubuntu Homebrew smoke environment.
- `tests/homebrew_ubuntu_smoke.sh`: opt-in native/Buildx smoke runner, deliberately outside `*_test.sh` so the normal suite stays fast.
- `docs/adr/0009-use-homebrew-for-shared-cli-tools.md`: superseding architecture decision.
- `CONTEXT.md`, `README.md`, and `README.ko.md`: domain vocabulary, operating contract, migration, requirements, and upgrade guidance.

### Task 1: Establish the Package Ownership Manifests

**Files:**
- Modify: `Brewfile:1-8`
- Create: `Brewfile.macos`
- Modify: `dot_config/mise/config.toml.tmpl:1-27`
- Create: `tests/homebrew_manifests_test.sh`
- Modify: `docs/superpowers/plans/2026-07-22-homebrew-unified-installation-source.md`

**Interfaces:**
- Produces: `Brewfile` as the canonical 20-formula mandatory CLI manifest consumed by Tasks 3, 5, 6, and 8.
- Produces: `Brewfile.macos` as the mandatory macOS platform manifest consumed by Task 3.
- Produces: `dot_config/mise/config.toml.tmpl` with exactly four runtime keys consumed by Task 4.

- [x] **Step 1: Create a failing manifest contract test**

Create `tests/homebrew_manifests_test.sh` with this exact content:

```sh
#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

fail() { printf '%s\n' "not ok - $1" >&2; exit 1; }
pass() { printf '%s\n' "ok - $1"; }

expected_formulae="$tmp_dir/expected-formulae"
actual_formulae="$tmp_dir/actual-formulae"
cat >"$expected_formulae" <<'EOF'
bat
btop
chezmoi
duf
dust
fastfetch
fd
fzf
gh
git
git-delta
gum
lazygit
lsd
mise
neovim
ripgrep
starship
zellij
zoxide
EOF

sed -n 's/^brew "\([^"]*\)"$/\1/p' "$repo_root/Brewfile" | sort >"$actual_formulae"
if ! cmp -s "$expected_formulae" "$actual_formulae"; then
  diff -u "$expected_formulae" "$actual_formulae" >&2 || true
  fail "root Brewfile declares exactly the mandatory cross-profile CLI formulae"
fi
pass "root Brewfile declares exactly the mandatory cross-profile CLI formulae"

expected_macos="$tmp_dir/expected-macos"
actual_macos="$tmp_dir/actual-macos"
printf '%s\n' font-d2coding font-jetbrains-mono-nerd-font >"$expected_macos"
sed -n 's/^cask "\([^"]*\)"$/\1/p' "$repo_root/Brewfile.macos" | sort >"$actual_macos"
if ! cmp -s "$expected_macos" "$actual_macos"; then
  diff -u "$expected_macos" "$actual_macos" >&2 || true
  fail "macOS Brewfile contains only mandatory terminal fonts"
fi
pass "macOS Brewfile contains only mandatory terminal fonts"

runtime_config="$tmp_dir/mise.toml"
chezmoi execute-template \
  --source "$repo_root" \
  --override-data '{"chezmoi":{"os":"linux"}}' \
  --file "$repo_root/dot_config/mise/config.toml.tmpl" >"$runtime_config"

for runtime in 'bun = "latest"' 'node = "24"' 'python = "3.13"' 'uv = "latest"'; do
  grep -Fx "$runtime" "$runtime_config" >/dev/null || fail "mise retains runtime declaration: $runtime"
done
if grep -F 'aqua:' "$runtime_config" >/dev/null; then
  fail "mise no longer declares shared CLI tools through aqua"
fi
pass "mise owns runtimes only"
```

- [x] **Step 2: Run the manifest test and verify it fails**

Run:

```sh
sh tests/homebrew_manifests_test.sh
```

Expected: FAIL because `Brewfile.macos` does not exist and the root Brewfile contains only `mise`, `btop`, and `gum` plus font casks.

- [x] **Step 3: Replace the package manifests**

Replace `Brewfile` with:

```ruby
# Mandatory cross-profile Core Shell Stack CLI tools.

brew "bat"
brew "btop"
brew "chezmoi"
brew "dust"
brew "duf"
brew "fastfetch"
brew "fd"
brew "fzf"
brew "gh"
brew "git"
brew "git-delta"
brew "gum"
brew "lazygit"
brew "lsd"
brew "mise"
brew "neovim"
brew "ripgrep"
brew "starship"
brew "zellij"
brew "zoxide"
```

Create `Brewfile.macos`:

```ruby
# Mandatory macOS Terminal Profile assets.

cask "font-jetbrains-mono-nerd-font"
cask "font-d2coding"
```

Replace the `[tools]` section in `dot_config/mise/config.toml.tmpl` with:

```toml
[tools]
bun = "latest"
node = "24"
python = "3.13"
uv = "latest"
```

Keep the existing `[settings.github]` and `[settings.node]` sections unchanged.

- [x] **Step 4: Run the focused declaration tests**

Run:

```sh
sh tests/homebrew_manifests_test.sh
chezmoi execute-template --source . --override-data '{"chezmoi":{"os":"darwin"}}' --file dot_config/mise/config.toml.tmpl | grep -F 'aqua:' && exit 1 || true
```

Expected: the test prints three `ok` lines; the second command produces no `aqua:` output.

- [x] **Step 5: Commit the manifest boundary**

```sh
git add Brewfile Brewfile.macos dot_config/mise/config.toml.tmpl tests/homebrew_manifests_test.sh docs/superpowers/plans/2026-07-22-homebrew-unified-installation-source.md
git commit -m "refactor: declare shared cli tools in Homebrew"
```

### Task 2: Bootstrap Standard Homebrew Before Terrapod Setup

**Files:**
- Modify: `install.sh:37-89,363-467,880-1082,1325-1372`
- Modify: `tests/terrapod_installer_test.sh:1-2570`

**Interfaces:**
- Produces: `expected_homebrew_path(profile, machine_arch) -> absolute brew path or non-zero`; `TERRAPOD_EXPECTED_HOMEBREW_PATH` is a test-only override used by the existing stub harness.
- Produces: `ensure_homebrew(profile) -> absolute standard-prefix brew path on stdout`.
- Produces: `prepare_brew_bootstrap_tools(brew_bin) -> status`; it installs `chezmoi` and `gum` without auto-update and prints no stdout.
- Consumes: supported profile detection and existing fatal/recovery output helpers in `install.sh`.

- [ ] **Step 1: Rewrite first-run tests around the new hard bootstrap boundary**

In `tests/terrapod_installer_test.sh`, replace assertions for `get.chezmoi.io`, Charm APT, and manual macOS Homebrew recovery with these explicit contracts in the existing macOS and Ubuntu cases:

```sh
assert_contains "$first_run_log_text" "brew args:install chezmoi gum" "macOS first-run installs chezmoi and gum through Homebrew"
assert_contains "$first_run_log_text" "brew install HOMEBREW_NO_AUTO_UPDATE:1" "macOS bootstrap tools disable Homebrew auto-update"
assert_not_contains "$first_run_log_text" "get.chezmoi.io" "first-run no longer invokes the standalone chezmoi installer"

assert_contains "$homebrew_missing_log_text" "curl args:-fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh -o " "macOS first-run downloads the official Homebrew installer when Homebrew is absent"
assert_contains "$homebrew_missing_log_text" "homebrew installer ran" "macOS first-run runs the downloaded Homebrew installer"
assert_contains "$homebrew_missing_log_text" "brew args:install chezmoi gum" "fresh macOS Homebrew installs both bootstrap tools"

assert_contains "$ubuntu_missing_git_log_text" "apt-get args:install -y build-essential ca-certificates curl file git procps" "Ubuntu first-run installs Homebrew system prerequisites through APT"
assert_contains "$ubuntu_missing_git_log_text" "curl args:-fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh -o " "Ubuntu first-run downloads the official Homebrew installer"
assert_contains "$ubuntu_missing_git_log_text" "brew args:install chezmoi gum" "Ubuntu first-run installs setup tools through Linuxbrew"
assert_not_contains "$ubuntu_missing_git_log_text" "repo.charm.sh" "Ubuntu first-run removes the Charm APT trust boundary"
assert_not_contains "$ubuntu_missing_git_log_text" "mise.en.dev" "Ubuntu first-run does not add the mise APT repository"
assert_first_occurrence_before "$ubuntu_missing_git_log_text" "apt-get args:install -y build-essential" "homebrew installer ran" "Ubuntu prerequisites precede Linuxbrew installation"
assert_first_occurrence_before "$ubuntu_missing_git_log_text" "brew args:install chezmoi gum" "terrapod args:setup" "Brew setup tools precede Terrapod Setup"
```

Add architecture, prefix, and disk-warning cases using the existing case/stub helpers:

```sh
write_uname_machine_stub "$unsupported_arch_case" "armv7l"
run_installer_case "$unsupported_arch_case"
assert_failure "$installer_status" "unsupported Ubuntu architecture fails before Homebrew bootstrap"
assert_contains "$(cat "$unsupported_arch_case/stderr")" "Unsupported CPU architecture: armv7l. Supported architectures: x86_64, aarch64." "unsupported architecture guidance is explicit"

TERRAPOD_HOMEBREW_CANDIDATE_PATHS="$nonstandard_prefix_case/opt/custom/bin/brew"
export TERRAPOD_HOMEBREW_CANDIDATE_PATHS
run_installer_case "$nonstandard_prefix_case"
unset TERRAPOD_HOMEBREW_CANDIDATE_PATHS
assert_failure "$installer_status" "nonstandard Homebrew prefix is rejected"
assert_contains "$(cat "$nonstandard_prefix_case/stderr")" "Homebrew exists outside the supported prefix" "nonstandard prefix guidance is explicit"

TERRAPOD_AVAILABLE_KB=2097152
export TERRAPOD_AVAILABLE_KB
run_installer_case "$low_disk_case" 'minimal
y
'
unset TERRAPOD_AVAILABLE_KB
assert_status "$installer_status" 0 "low disk space warning does not block first-run"
assert_contains "$(cat "$low_disk_case/stderr")" "less than 3 GiB is available" "low disk space is reported before Linuxbrew installation"
```

Extend the test harness with deterministic `uname -m` and disk overrides:

```sh
write_uname_machine_stub() {
  case_dir="$1"
  machine="$2"
  write_stub "$case_dir/bin/uname" \
    'case "$1" in' \
    '  -s) printf "%s\n" "${TERRAPOD_TEST_UNAME_S:-Linux}" ;;' \
    '  -m) printf "%s\n" "'"$machine"'" ;;' \
    '  *) exit 64 ;;' \
    'esac'
}
```

Update the Brew stub used by both profiles so `brew install chezmoi gum` materializes the existing chezmoi flow template and a gum command instead of relying on the removed standalone installer:

```sh
'  install)'
'    [ "$2" = chezmoi ] && [ "$3" = gum ] || exit 64'
'    cp "$TERRAPOD_CHEZMOI_STUB_TEMPLATE" "${0%/brew}/chezmoi"'
'    chmod +x "${0%/brew}/chezmoi"'
'    printf "%s\n" "#!/bin/sh" "exit 0" >"${0%/brew}/gum"'
'    chmod +x "${0%/brew}/gum"'
'    printf "%s\n" "brew install HOMEBREW_NO_AUTO_UPDATE:${HOMEBREW_NO_AUTO_UPDATE:-}" >>"$TERRAPOD_STUB_CALL_LOG"'
'    exit 0'
'    ;;'
```

For the formerly failing `homebrew_missing_case`, export `TERRAPOD_BREW_STUB_TEMPLATE` and make its downloaded installer payload contain:

```sh
#!/bin/sh
set -eu
printf '%s\n' "homebrew installer ran" >>"$TERRAPOD_STUB_CALL_LOG"
mkdir -p "${TERRAPOD_EXPECTED_HOMEBREW_PATH%/brew}"
cp "$TERRAPOD_BREW_STUB_TEMPLATE" "$TERRAPOD_EXPECTED_HOMEBREW_PATH"
chmod +x "$TERRAPOD_EXPECTED_HOMEBREW_PATH"
```

This case must now complete. Retain separate hard-failure cases whose downloaded payload exits `42`, whose `curl` exits non-zero, and whose Brew stub returns non-zero from `install chezmoi gum`.

- [ ] **Step 2: Run the installer test and confirm the old paths fail**

Run:

```sh
sh tests/terrapod_installer_test.sh
```

Expected: FAIL on the first new assertion because `install.sh` still downloads `get.chezmoi.io`, uses Charm APT on Ubuntu, and refuses to install Homebrew on a fresh Mac.

- [ ] **Step 3: Add standard platform and prefix validation to `install.sh`**

Add these functions after `detect_profile` and use `TERRAPOD_MACHINE_ARCH` only as a test seam:

```sh
machine_arch() {
  if [ -n "${TERRAPOD_MACHINE_ARCH:-}" ]; then
    printf '%s\n' "$TERRAPOD_MACHINE_ARCH"
  else
    uname -m
  fi
}

expected_homebrew_path() {
  profile="$1"
  arch="$2"

  if [ -n "${TERRAPOD_EXPECTED_HOMEBREW_PATH:-}" ]; then
    printf '%s\n' "$TERRAPOD_EXPECTED_HOMEBREW_PATH"
    return 0
  fi

  case "$profile:$arch" in
    vps-shell:x86_64|vps-shell:aarch64|vps-shell:arm64)
      printf '%s\n' /home/linuxbrew/.linuxbrew/bin/brew
      ;;
    macos-terminal:arm64|macos-terminal:aarch64)
      printf '%s\n' /opt/homebrew/bin/brew
      ;;
    macos-terminal:x86_64)
      printf '%s\n' /usr/local/bin/brew
      ;;
    vps-shell:*)
      fatal "Unsupported CPU architecture: $arch. Supported architectures: x86_64, aarch64."
      ;;
    *)
      fatal "Unsupported CPU architecture: $arch for profile $profile."
      ;;
  esac
}

reject_nonstandard_homebrew() {
  expected_brew="$1"
  if command -v brew >/dev/null 2>&1 && [ "$(command -v brew)" != "$expected_brew" ]; then
    fatal "Homebrew exists outside the supported prefix: $(command -v brew). Move or uninstall that Homebrew before installing the supported prefix at ${expected_brew%/bin/brew}."
  fi
}

require_non_root_linux_user() {
  if [ "$1" = "vps-shell" ] && [ "$(id -u)" -eq 0 ]; then
    fatal "Run the Terrapod installer as the non-root management user with sudo access; Homebrew does not support installation as root."
  fi
}
```

Set `TERRAPOD_EXPECTED_HOMEBREW_PATH="$case_dir/bin/brew"` in installer cases that intentionally use a stub Brew executable. Leave it unset in the nonstandard-prefix case so that case exercises the production rejection path.
Also leave it unset in the unsupported-architecture case; otherwise the test-only expected-path override would intentionally bypass architecture selection.

- [ ] **Step 4: Replace standalone chezmoi and Charm bootstrap with Homebrew bootstrap**

Delete `install_chezmoi_if_needed`, `ensure_charm_apt_repository`, and the APT `gum` branch. Replace them with:

```sh
warn_low_linuxbrew_disk_space() {
  [ "$1" = "vps-shell" ] || return 0
  if [ -n "${TERRAPOD_AVAILABLE_KB:-}" ]; then
    available_kb="$TERRAPOD_AVAILABLE_KB"
  else
    available_kb="$(df -Pk /home | awk 'NR == 2 { print $4 }')"
  fi
  case "$available_kb" in *[!0-9]*|'') return 0 ;; esac
  if [ "$available_kb" -lt 3145728 ]; then
    printf '%s\n' "terrapod installer: warning: less than 3 GiB is available for Linuxbrew; installation will continue and may need additional free space." >&2
  fi
}

ensure_source_repo_prerequisites() {
  profile="$1"
  [ "$profile" = "vps-shell" ] || return 0
  sudo_cmd="$(vps_sudo_cmd)"
  $sudo_cmd apt-get update -y || fatal "failed to update APT metadata before Homebrew bootstrap"
  $sudo_cmd apt-get install -y build-essential ca-certificates curl file git procps ||
    fatal "failed to install Ubuntu Homebrew prerequisites: build-essential, ca-certificates, curl, file, git, procps"
}

ensure_homebrew() {
  profile="$1"
  expected_brew="$(expected_homebrew_path "$profile" "$(machine_arch)")"
  reject_nonstandard_homebrew "$expected_brew"
  if [ ! -x "$expected_brew" ]; then
    installer="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-install.XXXXXX")" || fatal "failed to create Homebrew installer temporary file"
    if ! curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh -o "$installer"; then
      rm -f "$installer"
      fatal "failed to download the official Homebrew installer"
    fi
    if ! NONINTERACTIVE=1 /bin/bash "$installer" >&2; then
      rm -f "$installer"
      fatal "official Homebrew installer failed before Terrapod Setup"
    fi
    rm -f "$installer"
  fi
  [ -x "$expected_brew" ] || fatal "Homebrew install finished, but brew was not found at $expected_brew"
  printf '%s\n' "$expected_brew"
}

prepare_brew_bootstrap_tools() {
  brew_bin="$1"
  HOMEBREW_NO_AUTO_UPDATE=1 "$brew_bin" install chezmoi gum >&2 ||
    fatal "failed to install chezmoi and gum with Homebrew before Terrapod Setup"
  chezmoi_bin="${brew_bin%/brew}/chezmoi"
  [ -x "$chezmoi_bin" ] || fatal "Homebrew did not install chezmoi at $chezmoi_bin"
  command -v gum >/dev/null 2>&1 || fatal "Homebrew did not make gum available before Terrapod Setup"
}
```

Update `main` ordering to:

```sh
ensure_user_local_bin "$local_bin_dir"
# Preserve the existing source/resume/already-installed guards here.
require_non_root_linux_user "$profile"
ensure_source_repo_prerequisites "$profile"
warn_low_linuxbrew_disk_space "$profile"
brew_bin="$(ensure_homebrew "$profile")"
eval "$("$brew_bin" shellenv)" || fatal "failed to evaluate Homebrew shellenv"
prepare_brew_bootstrap_tools "$brew_bin"
chezmoi_bin="${brew_bin%/brew}/chezmoi"
if [ "$source_already_present" = "false" ]; then
  initialize_source_repository "$chezmoi_bin"
fi
ensure_first_run_setup "$profile" "$source_dir" "$chezmoi_bin"
```

Remove the now-redundant macOS-only `prepare_setup_ui_dependency` call from `ensure_first_run_setup`; `gum` is a hard precondition established before source initialization on both profiles.

- [ ] **Step 5: Run syntax and installer tests**

```sh
sh -n install.sh
sh -n tests/terrapod_installer_test.sh
sh tests/terrapod_installer_test.sh
```

Expected: all commands PASS; logs show APT prerequisites → official Homebrew → `brew install chezmoi gum` → source init → Terrapod Setup.

- [ ] **Step 6: Commit the first-run bootstrap**

```sh
git add install.sh tests/terrapod_installer_test.sh
git commit -m "feat: bootstrap Terrapod tools with Homebrew"
```

### Task 3: Apply Cross-Profile Homebrew State with Independent Warning Categories

**Files:**
- Modify: `.chezmoiignore:1-44`
- Rename: `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl` → `.chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl`
- Rename: `.chezmoiscripts/run_before_01-retry-homebrew-core.sh.tmpl` → `.chezmoiscripts/run_before_11-retry-homebrew-core.sh.tmpl`
- Create: `.chezmoiscripts/run_before_12-retry-homebrew-macos-platform.sh.tmpl`
- Rename: `.chezmoiscripts/run_before_01-retry-homebrew-desktop-apps.sh.tmpl` → `.chezmoiscripts/run_before_13-retry-homebrew-desktop-apps.sh.tmpl`
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl:1-200`
- Modify: `dot_local/lib/terrapod/homebrew-core-bundle.sh:1-180`
- Modify: `dot_local/lib/terrapod/install-warnings.sh:3-21`
- Modify: `tests/bootstrap_ubuntu_test.sh:1-247`
- Modify: `tests/chezmoiignore_test.sh:194-640,1280-1460`
- Modify: `tests/terrapod_command_test.sh` warning-category fixtures

**Interfaces:**
- Consumes: Task 1 `Brewfile` and `Brewfile.macos`.
- Produces: `terrapod_homebrew_core_run_bundle(brewfile) -> 0 or 1` with `TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT` containing failed formula/cask names.
- Produces: warning categories `homebrew-core` and `homebrew-macos-platform` for later doctor reporting.

- [ ] **Step 1: Change managed-path and Ubuntu bootstrap tests first**

In `tests/chezmoiignore_test.sh`, replace the old macOS-only Homebrew assertions with:

```sh
for entry in \
  .chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl \
  .chezmoiscripts/run_before_11-retry-homebrew-core.sh.tmpl \
  dot_local/lib/terrapod/homebrew-core-bundle.sh
do
  printf '%s\n' "$ubuntu_managed" | grep -Fx "$entry" >/dev/null ||
    fail "Ubuntu manages cross-profile Homebrew entry: $entry"
done
pass "Ubuntu manages cross-profile Homebrew core state"

printf '%s\n' "$ubuntu_managed" | grep -Fx '.chezmoiscripts/run_before_12-retry-homebrew-macos-platform.sh.tmpl' >/dev/null &&
  fail "Ubuntu must not manage macOS platform retry state"
pass "Ubuntu excludes macOS platform retry state"
```

Render the renamed Homebrew template for both profiles and assert:

```sh
assert_contains_text "$ubuntu_homebrew_bootstrap" 'core_brewfile="' "Ubuntu renders the mandatory CLI bundle"
assert_not_contains_text "$ubuntu_homebrew_bootstrap" 'Brewfile.macos"' "Ubuntu excludes the macOS platform bundle"
assert_contains_text "$macos_homebrew_bootstrap" 'macos_brewfile="' "macOS renders the platform bundle"
assert_contains_text "$macos_homebrew_bootstrap" 'homebrew-macos-platform' "macOS uses a separate platform warning category"
assert_contains_text "$ubuntu_homebrew_bootstrap" 'HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade' "Ubuntu bundle apply disables automatic updates"
```

In `tests/bootstrap_ubuntu_test.sh`, replace all Charm/mise repository assertions with:

```sh
assert_contains "$bootstrap_log" "apt-get args:install -y build-essential ca-certificates curl file git" "Ubuntu declared bootstrap installs Homebrew prerequisites"
assert_contains "$bootstrap_log" "procps" "Ubuntu declared bootstrap includes the Homebrew procps prerequisite"
assert_not_contains "$bootstrap_log" "mise.en.dev" "Ubuntu declared bootstrap removes the mise APT repository"
assert_not_contains "$bootstrap_log" "repo.charm.sh" "Ubuntu declared bootstrap removes the Charm APT repository"
assert_not_contains "$bootstrap_log" "apt-get args:install -y mise" "Ubuntu declared bootstrap does not install mise through APT"
assert_not_contains "$bootstrap_log" "apt-get args:install -y gum" "Ubuntu declared bootstrap does not install gum through APT"
```

- [ ] **Step 2: Run focused tests to verify the platform split fails**

```sh
sh tests/bootstrap_ubuntu_test.sh
sh tests/chezmoiignore_test.sh
sh tests/terrapod_command_test.sh
```

Expected: FAIL because Homebrew state remains macOS-only, Ubuntu still adds Charm/mise repositories, and `homebrew-macos-platform` is not a valid warning category.

- [ ] **Step 3: Reduce Ubuntu declared bootstrap to system prerequisites**

In `.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl`, keep the profile, sudo, warning, APT update, and `chsh` behavior. Replace the package/repository middle section with:

```sh
if ! $sudo_cmd apt-get install -y \
  build-essential \
  ca-certificates \
  curl \
  file \
  git \
  libbz2-dev \
  libffi-dev \
  liblzma-dev \
  libncursesw5-dev \
  libreadline-dev \
  libsqlite3-dev \
  libssl-dev \
  libxml2-dev \
  libxmlsec1-dev \
  procps \
  tk-dev \
  unzip \
  xz-utils \
  zlib1g-dev \
  zsh; then
  mark_install_warning \
    ubuntu-bootstrap \
    "Ubuntu bootstrap needs attention" \
    "Review APT install output for system and Homebrew prerequisites, then rerun tpod apply."
  exit_after_install_warning
fi
```

Delete every `/etc/apt/keyrings`, `mise.en.dev`, `repo.charm.sh`, `apt-get install mise`, and `apt-get install gum` block.

- [ ] **Step 4: Make Homebrew core apply cross-profile and split macOS platform failures**

Rename the scripts as listed above so Ubuntu APT bootstrap sorts before Homebrew bootstrap. Change the outer template guard of the Homebrew script to:

```gotemplate
{{- if or (eq .chezmoi.os "darwin") (eq .chezmoi.os "linux") -}}
```

Resolve only standard paths:

```sh
case "{{ .chezmoi.os }}:$(uname -m)" in
  darwin:arm64) brew_bin=/opt/homebrew/bin/brew ;;
  darwin:x86_64) brew_bin=/usr/local/bin/brew ;;
  linux:x86_64|linux:aarch64|linux:arm64) brew_bin=/home/linuxbrew/.linuxbrew/bin/brew ;;
  *) brew_bin= ;;
esac
```

Run the root manifest with automatic update disabled:

```sh
if ! terrapod_homebrew_core_run_bundle "$core_brewfile"; then
  mark_install_warning homebrew-core "Homebrew core install needs attention" "$TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT"
  continue_after_core_install_warning
fi
clear_install_warning homebrew-core
```

Inside the macOS template branch, run `Brewfile.macos` through the same helper but use the separate category:

```sh
macos_brewfile="{{ .chezmoi.sourceDir }}/Brewfile.macos"
if ! terrapod_homebrew_core_run_bundle "$macos_brewfile"; then
  mark_install_warning homebrew-macos-platform "Homebrew macOS platform install needs attention" "$TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT" || exit 1
else
  clear_install_warning homebrew-macos-platform
fi
```

Set `HOMEBREW_NO_AUTO_UPDATE=1` directly on every `brew bundle --no-upgrade` invocation inside `dot_local/lib/terrapod/homebrew-core-bundle.sh`, including bulk and individual formula/cask retries. Set it on every desktop bulk/per-cask call in this script and its renamed `run_before_13` retry script. Keeping the environment assignment at the external command site guarantees that `brew` inherits it on every POSIX shell.

- [ ] **Step 5: Add and exercise the macOS platform retry category**

Add `homebrew-macos-platform` to `terrapod_install_warning_categories` and `terrapod_install_warning_is_category`. Create `.chezmoiscripts/run_before_12-retry-homebrew-macos-platform.sh.tmpl` by using the same marker/read/standard-brew/helper flow as core retry, but bind:

```sh
category=homebrew-macos-platform
brewfile="{{ .chezmoi.sourceDir }}/Brewfile.macos"
summary="Homebrew macOS platform install needs attention"
```

Guard the whole file with `{{ if eq .chezmoi.os "darwin" }}` and call:

```sh
if terrapod_homebrew_core_run_bundle "$brewfile"; then
  terrapod_install_warning_clear "$category" || true
  exit 0
fi
terrapod_install_warning_write "$category" "$summary" "$TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT" || exit 1
exit 0
```

- [ ] **Step 6: Update `.chezmoiignore` and rerun focused tests**

Remove the cross-profile Homebrew bootstrap, core retry, and helper from the `ne darwin` ignore block. Add `Brewfile.macos` beside the existing source-only Brewfile entries at the top:

```text
Brewfile
Brewfile.macos
Brewfile.ai-cli-tools
Brewfile.macos-desktop-apps
```

Keep all docs and template source files ignored as targets. Add only the macOS platform and desktop retry targets to the non-Darwin block:

```gotemplate
.chezmoiscripts/12-retry-homebrew-macos-platform.sh
.chezmoiscripts/13-retry-homebrew-desktop-apps.sh
```

Remove the old `.chezmoiscripts/01-retry-homebrew-desktop-apps.sh` target entry. The new sort order guarantees Homebrew core and macOS platform recovery run first.

Run:

```sh
sh tests/bootstrap_ubuntu_test.sh
sh tests/chezmoiignore_test.sh
sh tests/terrapod_command_test.sh
```

Expected: PASS, including bulk failure → individual item retry, independent font marker behavior, and no Charm/mise APT requests.

- [ ] **Step 7: Commit cross-profile declared state**

```sh
git add .chezmoiignore .chezmoiscripts dot_local/lib/terrapod/homebrew-core-bundle.sh dot_local/lib/terrapod/install-warnings.sh tests/bootstrap_ubuntu_test.sh tests/chezmoiignore_test.sh tests/terrapod_command_test.sh
git commit -m "feat: apply Homebrew cli state across profiles"
```

### Task 4: Put Homebrew Before Legacy Paths and Use Brew mise for Runtimes

**Files:**
- Modify: `dot_zshenv.tmpl:1-25`
- Modify: `dot_zprofile:1-6`
- Modify: `.chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl:1-129`
- Modify: `.chezmoiscripts/run_after_21-retry-mise-tools.sh.tmpl:1-119`
- Modify: `tests/zshenv_local_bin_test.sh`
- Modify: `tests/chezmoiignore_test.sh` mise installer cases

**Interfaces:**
- Produces: `standard_mise_path() -> absolute standard-prefix mise path` in each mise installer template.
- Consumes: Task 3 mandatory Homebrew apply and Task 1 runtime-only mise configuration.

- [ ] **Step 1: Add failing PATH and mise-source assertions**

In `tests/zshenv_local_bin_test.sh`, render both profiles and add:

```sh
assert_contains() {
  haystack="$1"
  needle="$2"
  message="$3"
  printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null || fail "$message"
  pass "$message"
}

assert_order() {
  haystack="$1"
  first="$2"
  second="$3"
  message="$4"
  printf '%s\n' "$haystack" | awk -v first="$first" -v second="$second" '
    first_line == 0 && index($0, first) { first_line = NR }
    second_line == 0 && index($0, second) { second_line = NR }
    END { exit !(first_line > 0 && second_line > first_line) }
  ' || fail "$message"
  pass "$message"
}

assert_order "$rendered_linux_zshenv" 'path=("$HOME/.local/bin" $path)' 'eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"' "Linuxbrew is placed ahead of user-local legacy commands"
assert_order "$rendered_linux_zshenv" 'eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"' 'source "$path_snippet"' "explicit path snippets run after the managed Homebrew default"
assert_contains "$rendered_macos_zshenv" '/opt/homebrew/bin/brew shellenv' "macOS zshenv configures Apple Silicon Homebrew"
assert_contains "$rendered_macos_zshenv" '/usr/local/bin/brew shellenv' "macOS zshenv configures Intel Homebrew"
```

Change every existing Ubuntu call to `assert_linuxbrew_shellenv_rendering` so the expected state is `present` regardless of Optional AI Tool Stack or Optional Development Workspace settings. Homebrew is mandatory for every Preset.

In `tests/chezmoiignore_test.sh`, update the mise stub log checks to require the absolute Brew-installed path:

```sh
assert_contains_text "$mise_log_text" "/home/linuxbrew/.linuxbrew/bin/mise args:install --yes -C $HOME" "Ubuntu runtime install uses Linuxbrew mise"
assert_not_contains_text "$mise_log_text" "/usr/bin/mise" "Ubuntu runtime install never falls back to APT mise"
```

- [ ] **Step 2: Run focused tests and verify failure**

```sh
sh tests/zshenv_local_bin_test.sh
sh tests/chezmoiignore_test.sh
```

Expected: FAIL because Linux currently places `~/.local/bin` ahead of Linuxbrew, macOS Homebrew is in `.zprofile`, and Ubuntu mise expects APT.

- [ ] **Step 3: Make `.zshenv` the cross-profile Homebrew PATH owner**

Replace the beginning of `dot_zshenv.tmpl` with:

```zsh
export XDG_CONFIG_HOME="$HOME/.config"
typeset -U path PATH

# User-local commands remain available, but declared Homebrew tools win by default.
if [[ -d "$HOME/.local/bin" ]]; then
  path=("$HOME/.local/bin" $path)
fi

{{ if eq .chezmoi.os "linux" -}}
if [[ -x /home/linuxbrew/.linuxbrew/bin/brew ]]; then
  eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
fi
{{ else if eq .chezmoi.os "darwin" -}}
if [[ -x /opt/homebrew/bin/brew ]]; then
  eval "$(/opt/homebrew/bin/brew shellenv)"
elif [[ -x /usr/local/bin/brew ]]; then
  eval "$(/usr/local/bin/brew shellenv)"
fi
{{ end -}}

# Explicit machine-local overrides run last.
if [[ -d "$HOME/.config/zsh/path.d" ]]; then
  for path_snippet in "$HOME"/.config/zsh/path.d/*.zsh(N); do
    source "$path_snippet"
  done
  unset path_snippet
fi

export PATH
```

Replace `dot_zprofile` with:

```zsh
# Login-shell PATH is initialized by the managed .zshenv for both profiles.
```

- [ ] **Step 4: Resolve mise only from standard Homebrew prefixes**

In both mise installer templates, replace `setup_mise` with:

```sh
standard_mise_path() {
  case "{{ .chezmoi.os }}:$(uname -m)" in
    darwin:arm64) printf '%s\n' /opt/homebrew/bin/mise ;;
    darwin:x86_64) printf '%s\n' /usr/local/bin/mise ;;
    linux:x86_64|linux:aarch64|linux:arm64) printf '%s\n' /home/linuxbrew/.linuxbrew/bin/mise ;;
    *) return 1 ;;
  esac
}

mise_bin="$(standard_mise_path || true)"
if [ -z "$mise_bin" ] || [ ! -x "$mise_bin" ]; then
  mark_install_warning \
    mise-tools \
    "mise tool install needs attention" \
    "Install the mandatory Homebrew core bundle, then rerun tpod apply."
  exit_after_install_warning
fi
```

Replace calls with quoted absolute invocations:

```sh
if ! "$mise_bin" install --yes -C "$HOME"; then
  append_failed_mise_step "mise install"
fi
if "$mise_bin" exec --yes -C "$HOME" -- sh -c 'command -v corepack' >/dev/null 2>&1; then
  "$mise_bin" exec --yes -C "$HOME" -- corepack enable || append_failed_mise_step "corepack enable"
fi
```

- [ ] **Step 5: Run focused and syntax tests**

```sh
sh tests/zshenv_local_bin_test.sh
sh tests/chezmoiignore_test.sh
chezmoi execute-template --source . --override-data '{"chezmoi":{"os":"linux"}}' --file .chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl | sh -n
```

Expected: PASS; rendered mise configuration contains only runtimes and the installer cannot select `/usr/bin/mise` or a legacy mise shim.

- [ ] **Step 6: Commit PATH and runtime ownership**

```sh
git add dot_zshenv.tmpl dot_zprofile .chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl .chezmoiscripts/run_after_21-retry-mise-tools.sh.tmpl tests/zshenv_local_bin_test.sh tests/chezmoiignore_test.sh
git commit -m "refactor: reserve mise for development runtimes"
```

### Task 5: Reuse Mandatory Homebrew for Optional Bundles

**Files:**
- Modify: `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl:1-140`
- Modify: `.chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl` desktop bundle calls
- Modify: `.chezmoiscripts/run_before_13-retry-homebrew-desktop-apps.sh.tmpl` bundle calls
- Modify: `tests/chezmoiignore_test.sh` AI/desktop harnesses

**Interfaces:**
- Consumes: Task 3 standard-prefix Homebrew as mandatory state.
- Preserves: `optional-ai-cli-tools` and `homebrew-desktop-apps` warning contracts.

- [ ] **Step 1: Add failing optional-bundle assertions**

Add these checks to the rendered AI and desktop installer cases in `tests/chezmoiignore_test.sh`:

```sh
assert_not_contains_text "$ubuntu_ai_installer" "bootstrap_linux_homebrew" "AI installer no longer owns Linuxbrew bootstrap"
assert_not_contains_text "$ubuntu_ai_installer" "raw.githubusercontent.com/Homebrew/install" "AI installer never downloads Homebrew"
assert_contains_text "$ubuntu_ai_installer" "/home/linuxbrew/.linuxbrew/bin/brew" "AI installer uses mandatory standard Linuxbrew"
assert_contains_text "$ubuntu_ai_installer" "HOMEBREW_NO_AUTO_UPDATE=1" "AI bundle disables Homebrew auto-update"
assert_contains_text "$macos_desktop_installer" "HOMEBREW_NO_AUTO_UPDATE=1" "desktop bundle disables Homebrew auto-update"
```

- [ ] **Step 2: Run the Homebrew rendering test and confirm failure**

```sh
sh tests/chezmoiignore_test.sh
```

Expected: FAIL because the AI installer still conditionally installs Linuxbrew and bundle calls do not consistently disable auto-update.

- [ ] **Step 3: Simplify the AI installer to a standard-prefix consumer**

Delete `homebrew_installer`, `bootstrap_linux_homebrew`, and candidate-path discovery from `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`. Add:

```sh
standard_brew_path() {
  case "{{ .chezmoi.os }}:$(uname -m)" in
    darwin:arm64) printf '%s\n' /opt/homebrew/bin/brew ;;
    darwin:x86_64) printf '%s\n' /usr/local/bin/brew ;;
    linux:x86_64|linux:aarch64|linux:arm64) printf '%s\n' /home/linuxbrew/.linuxbrew/bin/brew ;;
    *) return 1 ;;
  esac
}

setup_brew_environment() {
  brew_bin="$(standard_brew_path || true)"
  if [ -z "$brew_bin" ] || [ ! -x "$brew_bin" ]; then
    echo "Mandatory standard-prefix Homebrew is unavailable for the Optional AI Tool Stack." >&2
    return 1
  fi
  eval "$("$brew_bin" shellenv)"
}
```

Run the rendered optional bundle with:

```sh
HOMEBREW_NO_AUTO_UPDATE=1 "$brew_bin" bundle --no-upgrade --file="$ai_cli_brewfile"
```

Apply the same environment assignment to macOS desktop bulk and per-cask retry calls.

- [ ] **Step 4: Verify optional behavior and commit**

```sh
sh tests/chezmoiignore_test.sh
sh tests/terrapod_command_test.sh
git add .chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl .chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl .chezmoiscripts/run_before_13-retry-homebrew-desktop-apps.sh.tmpl tests/chezmoiignore_test.sh
git commit -m "refactor: reuse mandatory Homebrew for optional bundles"
```

Expected: tests PASS; disabled AI settings still clear stale markers, enabled settings do not download Homebrew, and failures remain non-blocking warning markers.

### Task 6: Enforce Mandatory CLI Ownership in Status and Doctor

**Files:**
- Modify: `dot_local/bin/executable_terrapod:850-1185,1460-1680`
- Modify: `tests/terrapod_command_test.sh:1-3221`
- Modify: `tests/homebrew_manifests_test.sh`

**Interfaces:**
- Produces: `managed_homebrew_cli_records()` lines of `formula<TAB>command`.
- Produces: `standard_homebrew_prefix(profile, arch) -> prefix or non-zero`.
- Produces: `homebrew_cli_ownership_problems() -> newline-delimited "command: path-or-missing"`.
- Consumes: Task 1 root Brewfile and Task 4 PATH policy.

- [ ] **Step 1: Add the command/formula synchronization test**

Append to `tests/homebrew_manifests_test.sh`:

```sh
records="$tmp_dir/records"
TERRAPOD_PRINT_HOMEBREW_CLI_RECORDS=1 "$repo_root/dot_local/bin/executable_terrapod" >"$records"
cut -f1 "$records" | sort >"$tmp_dir/record-formulae"
if ! cmp -s "$expected_formulae" "$tmp_dir/record-formulae"; then
  diff -u "$expected_formulae" "$tmp_dir/record-formulae" >&2 || true
  fail "doctor command ownership records stay synchronized with Brewfile"
fi
pass "doctor command ownership records stay synchronized with Brewfile"
```

In `tests/terrapod_command_test.sh`, add three profile cases with existing command stubs:

```sh
assert_contains "$brew_owned_status_output" "Core Shell Stack source: Homebrew" "status reports Homebrew as Modern CLI Provider"
assert_contains "$brew_owned_status_output" "Homebrew CLI ownership: ready" "status accepts commands under the active prefix"

assert_contains "$shadowed_status_output" "Homebrew CLI ownership: warning" "status reports mandatory command collisions"
assert_contains "$shadowed_status_output" "chezmoi: $shadowed_home/.local/bin/chezmoi" "status prints the exact shadowing path"
assert_status "$shadowed_status_status" 0 "status remains informational for ownership collisions"

assert_failure "$shadowed_doctor_status" "doctor fails when a mandatory CLI is outside Homebrew"
assert_contains "$shadowed_doctor_output" "warn - mandatory Homebrew CLI is shadowed: chezmoi" "doctor identifies the shadowed command"
assert_contains "$shadowed_doctor_output" "Remove or move $shadowed_home/.local/bin/chezmoi" "doctor provides path-specific non-destructive guidance"
```

- [ ] **Step 2: Run focused tests and verify failure**

```sh
sh tests/homebrew_manifests_test.sh
sh tests/terrapod_command_test.sh
```

Expected: FAIL because the record-print seam and mandatory Homebrew ownership checks do not exist.

- [ ] **Step 3: Add one explicit formula-to-command registry**

Add near the existing Homebrew AI helpers in `dot_local/bin/executable_terrapod`:

```sh
managed_homebrew_cli_records() {
  tab="$(printf '\t')"
  while IFS=' ' read -r formula command; do
    printf '%s%s%s\n' "$formula" "$tab" "$command"
  done <<'EOF'
bat bat
btop btop
chezmoi chezmoi
dust dust
duf duf
fastfetch fastfetch
fd fd
fzf fzf
gh gh
git git
git-delta delta
gum gum
lazygit lazygit
lsd lsd
mise mise
neovim nvim
ripgrep rg
starship starship
zellij zellij
zoxide zoxide
EOF
}

if [ "${TERRAPOD_PRINT_HOMEBREW_CLI_RECORDS:-}" = "1" ]; then
  managed_homebrew_cli_records
  exit 0
fi
```

This test-only read seam prints static repository data and performs no mutation.

- [ ] **Step 4: Implement prefix and ownership checks**

Add:

```sh
standard_homebrew_prefix() {
  profile="$1"
  arch="${TERRAPOD_MACHINE_ARCH:-$(uname -m)}"
  case "$profile:$arch" in
    macos-terminal:arm64|macos-terminal:aarch64) printf '%s\n' /opt/homebrew ;;
    macos-terminal:x86_64) printf '%s\n' /usr/local ;;
    vps-shell:x86_64|vps-shell:aarch64|vps-shell:arm64) printf '%s\n' /home/linuxbrew/.linuxbrew ;;
    *) return 1 ;;
  esac
}

path_is_under_prefix() {
  path="$1"
  prefix="$2"
  case "$path" in "$prefix"/*) return 0 ;; *) return 1 ;; esac
}

homebrew_cli_ownership_problems() {
  prefix="$(standard_homebrew_prefix "$(current_profile)" || true)"
  [ -n "$prefix" ] || { printf '%s\n' "brew: unsupported-prefix"; return; }
  tab="$(printf '\t')"
  managed_homebrew_cli_records | while IFS="$tab" read -r formula command; do
    command_path="$(command -v "$command" 2>/dev/null || true)"
    if [ -z "$command_path" ]; then
      printf '%s: missing\n' "$command"
    elif ! path_is_under_prefix "$command_path" "$prefix"; then
      printf '%s: %s\n' "$command" "$command_path"
    fi
  done
}
```

In status, print `Core Shell Stack source: Homebrew` and either `Homebrew CLI ownership: ready` or each problem. In doctor, always require `brew` for both supported profiles, verify `brew --prefix` equals `standard_homebrew_prefix`, and turn each ownership problem into `doctor_warn` plus path-specific guidance. Keep `zsh` as a system command check and remove the old duplicate `chezmoi git mise nvim zellij` availability loop entries now covered by the 20-command registry.

- [ ] **Step 5: Run command and manifest tests**

```sh
sh -n dot_local/bin/executable_terrapod
sh tests/homebrew_manifests_test.sh
sh tests/terrapod_command_test.sh
```

Expected: PASS for both profiles, missing command, shadowed command, wrong prefix, standard prefix, status success, and doctor failure cases. Optional AI collisions remain advisory and do not set `doctor_failed` by themselves.

- [ ] **Step 6: Commit ownership diagnostics**

```sh
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh tests/homebrew_manifests_test.sh
git commit -m "feat: validate Homebrew cli ownership"
```

### Task 7: Record the New Domain Decision and User Contract

**Files:**
- Create: `docs/adr/0009-use-homebrew-for-shared-cli-tools.md`
- Modify: `CONTEXT.md`
- Modify: `README.md:120-245,270-305`
- Modify: `README.ko.md:120-230,250-275`
- Modify: `tests/readme_optional_stack_profiles_test.sh`
- Modify: `tests/readme_korean_test.sh`

**Interfaces:**
- Produces: **Modern CLI Provider** = Homebrew.
- Produces: **Development Runtime Manager** = mise.
- Supersedes: conflicting parts of ADR 0001 and ADR 0008 without deleting historical records.

- [ ] **Step 1: Replace README expectations with the new contract**

In `tests/readme_optional_stack_profiles_test.sh`, replace the mise/conditional-Linuxbrew/Charm assertions with:

```sh
assert_contains 'Homebrew is the Modern CLI Provider for the Core Shell Stack on both supported profiles.' \
  "README names Homebrew as the cross-profile Modern CLI Provider"
assert_contains 'mise is the Development Runtime Manager for Bun, Node.js, Python, and uv.' \
  "README limits mise to development runtimes"
assert_contains 'Ubuntu 24.04 installs Homebrew at `/home/linuxbrew/.linuxbrew` for every Preset.' \
  "README documents mandatory Linuxbrew"
assert_contains 'The first-run installer installs `chezmoi` and `gum` through Homebrew before Terrapod Setup.' \
  "README documents cross-profile Setup bootstrap"
assert_contains '1 vCPU, 1 GiB RAM, and at least 3 GiB of free disk space before installation' \
  "README documents the recommended VPS floor"
assert_contains '`x86_64` and `aarch64`' \
  "README documents supported Ubuntu architectures"
assert_not_contains 'get.chezmoi.io' \
  "README removes the standalone chezmoi installer"
assert_not_contains 'Charm APT' \
  "README removes the Charm APT trust boundary"
assert_not_contains 'mise from the official mise APT repository' \
  "README removes mise APT ownership"
assert_contains 'HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade' \
  "README documents restore-only apply semantics"
assert_contains 'Existing mise, APT, and vendor-installed payloads are not removed automatically.' \
  "README documents non-destructive migration"
```

Add matching Korean assertions to `tests/readme_korean_test.sh` while preserving the existing heading equality check.

- [ ] **Step 2: Run README tests and verify failure**

```sh
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
```

Expected: FAIL on the old Modern CLI Provider, conditional Linuxbrew, Charm APT, and mise APT descriptions.

- [ ] **Step 3: Create ADR 0009 with the complete decision**

Create `docs/adr/0009-use-homebrew-for-shared-cli-tools.md`:

```markdown
# Use Homebrew for shared CLI tools

The macOS Terminal Profile and VPS Shell Profile use Homebrew as the Modern CLI Provider for the mandatory Core Shell Stack and the Optional AI Tool Stack. The VPS Shell Profile installs Homebrew at `/home/linuxbrew/.linuxbrew` for every Preset. mise remains the Development Runtime Manager for Bun, Node.js, Python, and uv, while APT installs only Ubuntu system and Homebrew bootstrap prerequisites.

This decision supersedes ADR 0001's assignment of modern CLI tools to mise/aqua and its rejection of Linuxbrew, and ADR 0008's restriction of Linuxbrew to the Optional AI Tool Stack. It does not change the macOS Desktop App Stack, optional-stack semantics, or Terrapod's non-destructive apply contract.

## Considered Options

- Keep mise/aqua for shared CLI tools: rejected because GUI apps, AI CLIs, and ordinary CLIs would retain separate installation and recovery owners.
- Keep Linuxbrew conditional on AI tools: rejected because the Core Shell Stack would still have different providers across profiles.
- Move development runtimes to Homebrew: rejected because project/runtime version selection remains mise's responsibility.
- Remove legacy installs automatically: rejected because pruning mise, APT, or vendor state can affect tools outside Terrapod's ownership.

## Consequences

- `Brewfile` is the canonical declaration for 20 mandatory CLI formulae on both profiles.
- `Brewfile.macos`, `Brewfile.ai-cli-tools.tmpl`, and `Brewfile.macos-desktop-apps.tmpl` retain separate failure and retry boundaries.
- First-run installs standard-prefix Homebrew, then installs `chezmoi` and `gum` before Terrapod Setup.
- Ubuntu supports `x86_64` and `aarch64`, one management user, initial sudo access, and the standard `/home/linuxbrew/.linuxbrew` prefix.
- Apply restores missing packages with auto-update disabled and never upgrades or removes packages.
- Status reports mandatory command ownership; doctor fails when a mandatory command is missing or resolves outside Homebrew.
- Existing legacy payloads remain until the user chooses to clean them up.
```

- [ ] **Step 4: Update domain vocabulary and relationships**

In `CONTEXT.md`, redefine and add:

```markdown
**Modern CLI Provider**:
Homebrew, the shared package provider for mandatory user-facing CLI tools across the macOS Terminal Profile and VPS Shell Profile.
_Avoid_: mise CLI provider, aqua provider, platform-specific CLI source

**Development Runtime Manager**:
mise, which installs and selects Bun, Node.js, Python, and uv independently from Homebrew-managed user-facing CLI tools.
_Avoid_: Modern CLI Provider, Homebrew runtime manager, aqua tool provider
```

Replace relationships that mention `get.chezmoi.io`, Charm APT, mise APT, conditional Linuxbrew, or mise/aqua CLI ownership with the exact architecture and constraints in the ADR. Add `homebrew-macos-platform` to the stable warning-category relationship.

- [ ] **Step 5: Update both READMEs in lockstep**

Rewrite Platform Details and Intentional Upgrades so they state:

```markdown
- Homebrew installs the 20-command Core Shell Stack on both profiles.
- The first-run installer bootstraps Homebrew, chezmoi, and gum before Terrapod Setup.
- Ubuntu APT installs only system prerequisites; no Charm or mise repository is added.
- mise installs Bun, Node.js 24, Python 3.13, and uv.
- `tpod apply` uses `HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade` and never performs an upgrade.
- Intentional CLI upgrades use explicit `brew update` and `brew upgrade`; runtime upgrades remain explicit mise operations.
- Existing legacy payloads are preserved. `tpod doctor` reports mandatory commands that shadow Homebrew.
```

Document the Ubuntu floor as a recommendation, not an installer hard gate: 1 vCPU, 1 GiB RAM, 3 GiB free disk; 2 GiB RAM is comfortable. Document supported architectures and the single-user/standard-prefix constraint. Keep headings identical between `README.md` and `README.ko.md`.

- [ ] **Step 6: Run documentation tests and commit**

```sh
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
git add docs/adr/0009-use-homebrew-for-shared-cli-tools.md CONTEXT.md README.md README.ko.md tests/readme_optional_stack_profiles_test.sh tests/readme_korean_test.sh
git commit -m "docs: define Homebrew cli ownership"
```

Expected: both documentation tests PASS and README headings remain byte-for-byte aligned.

### Task 8: Add a Real Ubuntu Smoke Test and Run Final Verification

**Files:**
- Create: `tests/fixtures/homebrew-ubuntu-24.04.Dockerfile`
- Create: `tests/homebrew_ubuntu_smoke.sh`
- Verify: all files changed in Tasks 1-7

**Interfaces:**
- Consumes: Task 1 manifests, Task 2 bootstrap assumptions, Task 3 apply order, Task 4 PATH/runtime split, and Task 6 ownership registry.
- Produces: opt-in Docker Buildx validation for native Ubuntu 24.04 Homebrew bottles without slowing the normal shell test glob.

- [ ] **Step 1: Create the failing smoke runner contract**

Create `tests/homebrew_ubuntu_smoke.sh`:

```sh
#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
platform="${TERRAPOD_SMOKE_PLATFORM:-linux/amd64}"

docker buildx build \
  --load \
  --platform "$platform" \
  --file "$repo_root/tests/fixtures/homebrew-ubuntu-24.04.Dockerfile" \
  --tag "terrapod-homebrew-smoke:${platform##*/}" \
  "$repo_root"
```

Run:

```sh
TERRAPOD_SMOKE_PLATFORM=linux/amd64 sh tests/homebrew_ubuntu_smoke.sh
```

Expected: FAIL because the Dockerfile does not exist.

- [ ] **Step 2: Create the real non-root Ubuntu fixture**

Create `tests/fixtures/homebrew-ubuntu-24.04.Dockerfile`:

```dockerfile
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y \
    build-essential ca-certificates curl file git procps sudo zsh \
  && rm -rf /var/lib/apt/lists/* \
  && useradd --create-home --shell /usr/bin/zsh terrapod \
  && printf '%s\n' 'terrapod ALL=(ALL) NOPASSWD:ALL' >/etc/sudoers.d/terrapod \
  && chmod 0440 /etc/sudoers.d/terrapod

USER terrapod
WORKDIR /workspace
RUN NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

COPY --chown=terrapod:terrapod . /workspace

RUN eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)" \
  && HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade --file=/workspace/Brewfile

RUN eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)" \
  && TERRAPOD_PRINT_HOMEBREW_CLI_RECORDS=1 /workspace/dot_local/bin/executable_terrapod \
     | while IFS="$(printf '\t')" read -r formula command; do \
       command_path="$(command -v "$command")"; \
       case "$command_path" in \
         /home/linuxbrew/.linuxbrew/*) ;; \
         *) printf '%s\n' "not Homebrew-owned: $formula -> $command_path" >&2; exit 1 ;; \
       esac; \
     done

RUN eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)" \
  && chezmoi execute-template \
       --source /workspace \
       --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
       --file /workspace/dot_config/mise/config.toml.tmpl \
     | tee /tmp/mise.toml \
  && ! grep -F 'aqua:' /tmp/mise.toml \
  && grep -Fx 'node = "24"' /tmp/mise.toml
```

- [ ] **Step 3: Run the real amd64 and arm64 smoke builds**

```sh
TERRAPOD_SMOKE_PLATFORM=linux/amd64 sh tests/homebrew_ubuntu_smoke.sh
TERRAPOD_SMOKE_PLATFORM=linux/arm64 sh tests/homebrew_ubuntu_smoke.sh
```

Expected: both images build successfully. Each installs the 20 formulae from bottles, verifies every command path is under `/home/linuxbrew/.linuxbrew`, and renders a runtime-only mise configuration. If the local Docker engine lacks QEMU for the non-native architecture, run that second command on a native arm64 builder; do not waive the arm64 result.

- [ ] **Step 4: Run syntax checks**

```sh
sh -n install.sh
sh -n dot_local/bin/executable_terrapod
sh -n dot_local/lib/terrapod/homebrew-core-bundle.sh
sh -n dot_local/lib/terrapod/install-warnings.sh
macos_data='{"chezmoi":{"os":"darwin"},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false,"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false}'
ubuntu_data='{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}},"enableEditorStack":false,"enableAiCliTools":false,"enableDevelopmentWorkspace":false,"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false}'
for data in "$macos_data" "$ubuntu_data"; do
  for template in \
    .chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl \
    .chezmoiscripts/run_onchange_before_10-bootstrap-homebrew.sh.tmpl \
    .chezmoiscripts/run_before_11-retry-homebrew-core.sh.tmpl \
    .chezmoiscripts/run_before_12-retry-homebrew-macos-platform.sh.tmpl \
    .chezmoiscripts/run_before_13-retry-homebrew-desktop-apps.sh.tmpl \
    .chezmoiscripts/run_onchange_after_20-install-mise-tools.sh.tmpl \
    .chezmoiscripts/run_after_21-retry-mise-tools.sh.tmpl \
    .chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl
  do
    rendered="$(mktemp)"
    chezmoi execute-template --source . --override-data "$data" --file "$template" >"$rendered"
    [ ! -s "$rendered" ] || sh -n "$rendered"
    rm -f "$rendered"
  done
done
```

Expected: all static and rendered POSIX shell files parse successfully.

- [ ] **Step 5: Run the complete repository test suite**

```sh
for test_script in tests/*_test.sh; do
  sh "$test_script" || exit $?
done
for test_script in tests/*_test.zsh; do
  zsh "$test_script" || exit $?
done
```

Expected: every shell and zsh test passes, including the new manifest test and all existing installer, warning, status, doctor, and README regressions.

- [ ] **Step 6: Audit forbidden legacy installation paths and destructive operations**

```sh
if rg -n 'get\.chezmoi\.io|repo\.charm\.sh|mise\.en\.dev/(deb|gpg)|apt-get install -y (gum|mise)' \
  install.sh .chezmoiscripts dot_local README.md README.ko.md CONTEXT.md; then
  exit 1
fi
if rg -n 'brew (update|upgrade|cleanup)|mise (upgrade|prune)' install.sh .chezmoiscripts dot_local; then
  exit 1
fi
rg -n 'HOMEBREW_NO_AUTO_UPDATE=1.*bundle --no-upgrade|brew bundle --no-upgrade' \
  .chezmoiscripts README.md README.ko.md
```

Expected: the forbidden-path search produces no matches. Every managed bundle call shown by the second search has `HOMEBREW_NO_AUTO_UPDATE=1`; no bundle call silently upgrades packages.

- [ ] **Step 7: Review the final diff and commit smoke coverage**

```sh
git diff --check
git status --short
git diff --stat
git add tests/fixtures/homebrew-ubuntu-24.04.Dockerfile tests/homebrew_ubuntu_smoke.sh
git commit -m "test: smoke test Linuxbrew cli ownership"
```

Expected: `git diff --check` is silent, only intended files are staged, and the commit records the opt-in real-install verification.

## Self-Review Results

- **Spec coverage:** Tasks 1-8 cover all 31 resolved design decisions: package/runtime boundaries, mandatory Linuxbrew, standard prefixes and architectures, non-destructive migration/rollback, restore-only apply, partial retry, independent warning categories, PATH precedence, bootstrap repository removal, Homebrew-owned chezmoi/gum/mise, shell integration and Terrapod exclusions, macOS-only GUI scope, diagnostics, documentation, and real Ubuntu validation.
- **Placeholder scan:** The plan contains no deferred-work markers, generic validation requests, vague error-handling requests, or cross-task shorthand. Every code-changing step provides concrete replacement or insertion text.
- **Interface consistency:** `Brewfile` is the package source consumed by apply, diagnostics, docs, and smoke tests; the 20 formula/command records match across Task 6 and Task 8; standard prefixes and architecture names match in installer, apply scripts, PATH, diagnostics, and Docker verification; warning category spelling is consistently `homebrew-macos-platform`.
