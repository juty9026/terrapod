# Mobile Dev App Group Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `mobile-dev` macOS App Group that installs Android Studio and Maestro and renders the Android and Java shell environment they need.

**Architecture:** One new machine-local setting key, `enableMacosAppGroupMobileDev`, gates a new group in the rendered macOS Desktop App Stack Brewfile and a new block in `dot_zshenv.tmpl`. The reconcile script's per-package retry path is first generalized so it can attribute failures for tap formulae as well as casks.

**Tech Stack:** POSIX `sh`, chezmoi Go templates, zsh, Homebrew Bundle, awk. Tests are hand-rolled `sh` scripts under `tests/` using `chezmoi execute-template` and `chezmoi cat` against fixture data.

**Spec:** `docs/superpowers/specs/2026-08-22-mobile-dev-app-group-design.md`

## Global Constraints

- Managed scripts are POSIX `sh`, not bash. `dot_zshenv.tmpl` is zsh.
- Homebrew declarations for non-official taps use a fully-qualified token plus `trusted: true`, never a bare `tap` statement. Exact Maestro token: `mobile-dev-inc/tap/maestro`.
- Never declare the homebrew-cask `maestro`; it is a different product (runmaestro.ai AI agent command center).
- `ANDROID_HOME` is exactly `$HOME/Library/Android/sdk`.
- `JAVA_HOME` is exactly `/Applications/Android Studio.app/Contents/jbr/Contents/Home`.
- Do not declare `android-platform-tools`, a JDK cask, Xcode, or any Android SDK component.
- Every apply path stays non-destructive: never uninstall, never `brew upgrade`, always `HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade`.
- Run tests with `sh tests/<name>.sh` from the repository root.
- Three test files fail in this environment for reasons unrelated to this work and also fail on `main`: `tests/jetendard_font_test.sh`, `tests/jetendard_settings_test.sh`, `tests/terrapod_command_test.sh`. `tests/homebrew_ubuntu_smoke.sh` needs Docker and Ubuntu. Do not treat these as regressions, and do not attempt to fix them.

## Decisions Made While Planning

Two points where this plan resolves detail the spec left open. Both are inside the spec's intent; flagged here so a reviewer can reject them independently.

1. **Record format carries the verbatim declaration line, not parsed kind plus options.** The spec asks each record to carry group, kind, token, and options. Storing the trimmed source line achieves all four with one field and no re-serialization, so `cask "stablyai/orca/orca", trusted: true` round-trips exactly. The parsed token is still emitted separately for failure reporting.

2. **The warning marker keeps the wording `failed casks:`.** With a formula in the group this wording is imprecise. Renaming it touches five assertions across two test files plus the guidance string, which is scope the spec did not ask for. Left as-is; raise separately if the imprecision matters.

---

### Task 1: Generalize the reconcile record format

The per-package retry path in the reconcile script only understands `cask "` lines and rebuilds each record as a bare `cask "<name>"`. That drops declaration options and ignores formulae. This task makes records carry the full declaration so tap formulae are retried and attributed, and so Orca's `trusted: true` survives retry.

**Files:**
- Modify: `.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl:93-109` (`desktop_app_cask_records`) and `:154-179` (retry loop)
- Test: `tests/chezmoiignore_test.sh`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `desktop_app_package_records <brewfile>` writing tab-separated `group<TAB>token<TAB>declaration` lines to stdout. Task 2 relies on this recognizing `brew "` lines.

- [ ] **Step 1: Write the failing test**

Add to `tests/chezmoiignore_test.sh`, immediately after the block that ends with `pass "development-apps bootstrap script is valid sh"`:

```sh
package_records_probe="$tmp_dir/package-records-probe.sh"
package_records_brewfile="$tmp_dir/package-records-brewfile"
cat >"$package_records_brewfile" <<'PROBE_BREWFILE'
# development-apps macOS App Group
cask "zed"
cask "stablyai/orca/orca", trusted: true
# mobile-dev macOS App Group
cask "android-studio"
brew "mobile-dev-inc/tap/maestro", trusted: true
PROBE_BREWFILE

sed -n '/^desktop_app_package_records()/,/^}/p' \
  "$repo_root/.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl" \
  >"$package_records_probe"
printf '%s\n' 'desktop_app_package_records "$1"' >>"$package_records_probe"

package_records_output="$(sh "$package_records_probe" "$package_records_brewfile")"

expected_package_records="$(printf '%s\n' \
  'development-apps	zed	cask "zed"' \
  'development-apps	stablyai/orca/orca	cask "stablyai/orca/orca", trusted: true' \
  'mobile-dev	android-studio	cask "android-studio"' \
  'mobile-dev	mobile-dev-inc/tap/maestro	brew "mobile-dev-inc/tap/maestro", trusted: true')"

assert_text_equals \
  "$package_records_output" \
  "$expected_package_records" \
  "desktop app package records carry group, token, and the verbatim declaration for casks and tap formulae"

assert_contains_text \
  "$macos_development_apps_bootstrap" \
  'read -r app_group token declaration' \
  "per-package retry reads the declaration field alongside the group and token"
assert_contains_text \
  "$macos_development_apps_bootstrap" \
  '>"$single_package_brewfile"' \
  "per-package retry writes the recorded declaration so options such as trusted: true survive"
```

The three separators inside each `expected_package_records` line must be real tab characters. The `PROBE_BREWFILE` heredoc contains no tabs.

- [ ] **Step 2: Run test to verify it fails**

Run: `sh tests/chezmoiignore_test.sh`
Expected: FAIL with `not ok - desktop app package records carry group, token, and the verbatim declaration for casks and tap formulae`. The `sed` range extracts nothing because `desktop_app_package_records` does not exist yet, so the probe prints nothing.

- [ ] **Step 3: Rewrite the record parser**

In `.chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl`, replace the whole `desktop_app_cask_records` function with:

```sh
desktop_app_package_records() {
  brewfile="$1"

  awk '
    /^[[:space:]]*#[[:space:]]+.*[[:space:]]macOS App Group[[:space:]]*$/ {
      group = $0
      sub(/^[[:space:]]*#[[:space:]]+/, "", group)
      sub(/[[:space:]]+macOS App Group[[:space:]]*$/, "", group)
      next
    }

    /^[[:space:]]*(cask|brew)[[:space:]]+"/ {
      declaration = $0
      sub(/^[[:space:]]+/, "", declaration)
      sub(/[[:space:]]+$/, "", declaration)

      token = declaration
      sub(/^(cask|brew)[[:space:]]+"/, "", token)
      sub(/".*$/, "", token)

      if (token != "") {
        printf "%s\t%s\t%s\n", group, token, declaration
      }
    }
  ' "$brewfile"
}
```

- [ ] **Step 4: Rewrite the retry loop**

In the same file, replace the retry loop body. The old loop reads two fields and rebuilds `cask "%s"`; the new one reads three and writes the declaration verbatim. Replace from `tab="$(printf '\t')"` through the `done <"$records_file"` line with:

```sh
  tab="$(printf '\t')"
  while IFS="$tab" read -r app_group token declaration; do
    single_package_brewfile="$(mktemp "${TMPDIR:-/tmp}/terrapod-macos-desktop-package.XXXXXX")" || {
      cleanup_desktop_app_bundle_temps
      return 1
    }
    if ! printf '%s\n' "$declaration" >"$single_package_brewfile"; then
      cleanup_desktop_app_bundle_temps
      return 1
    fi

    if ! HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade --file="$single_package_brewfile"; then
      if ! printf '%s\n' "$token" >>"$failed_casks_file"; then
        cleanup_desktop_app_bundle_temps
        return 1
      fi
      if [ -n "$app_group" ] && ! grep -Fx "$app_group" "$failed_groups_file" >/dev/null 2>&1; then
        if ! printf '%s\n' "$app_group" >>"$failed_groups_file"; then
          cleanup_desktop_app_bundle_temps
          return 1
        fi
      fi
    fi

    rm -f "$single_package_brewfile"
    single_package_brewfile=
  done <"$records_file"
```

- [ ] **Step 5: Update the remaining references to the renamed identifiers**

Search the file for the old names and update each hit:

```bash
grep -n "desktop_app_cask_records\|single_cask_brewfile" .chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl
```

Rename `desktop_app_cask_records` to `desktop_app_package_records` at its call site, and rename every `single_cask_brewfile` to `single_package_brewfile`, including the declaration near the top of the temp-cleanup section and inside `cleanup_desktop_app_bundle_temps`. Leave `failed_casks_file`, `failed_casks`, and the `failed casks:` guidance wording unchanged; those feed user-facing text asserted elsewhere.

- [ ] **Step 6: Loosen the test stub's whole-line package matching**

This step is required, not optional. `write_brew_bundle_stub` in `tests/chezmoiignore_test.sh` injects failures by matching a package declaration with `grep -Fx`, a whole-line match. Now that the retry Brewfile carries options, the line reads `cask "stablyai/orca/orca", trusted: true` and no longer equals `cask "stablyai/orca/orca"`. Left unchanged, the existing Orca bulk-failure assertions stop firing.

In the stub body, replace the formula matcher:

```sh
    '      if [ -n "$bundle_file" ] && grep -Eq "^brew \"$formula\"(,[[:space:]]|$)" "$bundle_file" 2>/dev/null; then' \
```

and the cask matcher:

```sh
    '      if [ -n "$bundle_file" ] && grep -Eq "^cask \"$cask\"(,[[:space:]]|$)" "$bundle_file" 2>/dev/null; then' \
```

Both now match a declaration whether it ends at the token or continues into options. Leave the `MACOS_BREW_FAIL_CORE_BULK`, `MACOS_BREW_FAIL_DESKTOP_BULK`, and `MACOS_BREW_FAIL_BULK` matchers on `grep -Fx`; those match fixed marker lines that never carry options.

- [ ] **Step 7: Run tests to verify they pass**

Run: `sh tests/chezmoiignore_test.sh`
Expected: PASS, including the two new assertions and every pre-existing Orca and Ghostty failure-marker assertion.

If an Orca marker assertion fails here, the stub change in Step 6 is wrong or incomplete; fix it rather than weakening the assertion.

- [ ] **Step 8: Verify nothing else regressed**

Run: `sh tests/homebrew_manifests_test.sh && sh tests/terrapod_config_test.sh`
Expected: both PASS.

- [ ] **Step 9: Commit**

```bash
git add .chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl tests/chezmoiignore_test.sh
git commit -m "Retry desktop app packages with their full declaration"
```

---

### Task 2: Declare the mobile-dev group packages

**Files:**
- Modify: `Brewfile.macos-desktop-apps.tmpl` (append after the `development-apps` block)
- Test: `tests/chezmoiignore_test.sh`

**Interfaces:**
- Consumes: `desktop_app_package_records` from Task 1 recognizing `brew "` lines.
- Produces: the template data key `enableMacosAppGroupMobileDev`, consumed by Task 3 and Task 4.

- [ ] **Step 1: Write the failing test**

In `tests/chezmoiignore_test.sh`, add the fixture data and rendering next to the other group data definitions. Put this immediately after the line defining `macos_development_apps_data`:

```sh
macos_mobile_dev_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":false,"enableMacosAppGroupMobileDev":true}'
```

Immediately after the line defining `development_apps_brewfile`, add:

```sh
mobile_dev_brewfile="$(render_template "$macos_mobile_dev_data" "Brewfile.macos-desktop-apps.tmpl")"
```

Then add the assertions immediately after the existing `"development-apps group renders exactly the expected casks"` assertion:

```sh
assert_contains_text "$mobile_dev_brewfile" 'cask "android-studio"' "mobile-dev group renders Android Studio"
assert_contains_text "$mobile_dev_brewfile" 'brew "mobile-dev-inc/tap/maestro", trusted: true' "mobile-dev group trusts only the fully-qualified Maestro formula, not the entire mobile-dev-inc/tap tap"
assert_not_contains_text "$mobile_dev_brewfile" 'cask "maestro"' "mobile-dev group does not render the unrelated homebrew-cask maestro"
assert_not_contains_text "$mobile_dev_brewfile" 'tap "mobile-dev-inc/tap"' "mobile-dev group taps on demand through the fully-qualified token instead of trusting the whole tap"
assert_not_contains_text "$mobile_dev_brewfile" 'android-platform-tools' "mobile-dev group leaves platform tools to the Android SDK"
assert_not_contains_text "$mobile_dev_brewfile" 'cask "temurin"' "mobile-dev group resolves Java through the Android Studio bundled runtime instead of a declared JDK"
assert_not_contains_text "$mobile_dev_brewfile" 'cask "zed"' "mobile-dev group does not render development-apps casks"

mobile_dev_packages="$(
  printf '%s\n' "$mobile_dev_brewfile" |
    awk '/^[[:space:]]*(cask|brew)[[:space:]]+"/ { print }'
)"
expected_mobile_dev_packages='cask "android-studio"
brew "mobile-dev-inc/tap/maestro", trusted: true'
assert_text_equals \
  "$mobile_dev_packages" \
  "$expected_mobile_dev_packages" \
  "mobile-dev group renders exactly the expected packages"

assert_not_contains_text "$macos_brewfile" 'cask "android-studio"' "macOS default does not render Android Studio"
assert_not_contains_text "$macos_brewfile" 'mobile-dev-inc/tap/maestro' "macOS default does not render Maestro"
assert_not_contains_text "$development_apps_brewfile" 'cask "android-studio"' "development-apps group does not render Android Studio"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `sh tests/chezmoiignore_test.sh`
Expected: FAIL with `not ok - mobile-dev group renders Android Studio`.

- [ ] **Step 3: Add the group to the Brewfile template**

Append to the end of `Brewfile.macos-desktop-apps.tmpl`, after the `development-apps` block's `{{ end -}}`:

```
{{ if default false (get . "enableMacosAppGroupMobileDev") -}}
# mobile-dev macOS App Group
cask "android-studio"
brew "mobile-dev-inc/tap/maestro", trusted: true
{{ end -}}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `sh tests/chezmoiignore_test.sh`
Expected: PASS.

- [ ] **Step 5: Verify the rendering by hand**

```bash
d=$(mktemp -d); : > "$d/chezmoi.toml"
chezmoi --config "$d/chezmoi.toml" --source . \
  --override-data '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupMobileDev":true}' \
  execute-template --file Brewfile.macos-desktop-apps.tmpl
rm -rf "$d"
```

Expected output is exactly the header comment, the `# mobile-dev macOS App Group` comment, the Android Studio cask, and the Maestro formula. Passing `--config` with an empty file is required; without it chezmoi merges this machine's real settings and renders every group.

- [ ] **Step 6: Write the failure attribution test**

This proves the Task 1 record change actually reaches the warning marker for a tap formula, which the record-level assertions alone do not show.

Add near the top of the test, immediately after the line defining `macos_development_apps_bootstrap`:

```sh
macos_mobile_dev_bootstrap="$(render_template "$macos_mobile_dev_data" ".chezmoiscripts/run_before_10-reconcile-homebrew.sh.tmpl")"
```

Then add this after the existing development-apps failure assertions, immediately before the section that follows them:

```sh
mobile_dev_bootstrap_script="$tmp_dir/macos-mobile-dev-bootstrap.sh"
printf '%s\n' "$macos_mobile_dev_bootstrap" | sed \
  -e "s#/opt/homebrew/bin/brew#$tmp_dir/mobile-dev-failure-bin/brew#g" \
  -e "s#/usr/local/bin/brew#$tmp_dir/mobile-dev-failure-bin/brew#g" \
  >"$mobile_dev_bootstrap_script"
sh -n "$mobile_dev_bootstrap_script" || fail "mobile-dev bootstrap script should be valid sh"
pass "mobile-dev bootstrap script is valid sh"

mobile_dev_failure_bin="$tmp_dir/mobile-dev-failure-bin"
mobile_dev_failure_state="$tmp_dir/mobile-dev-failure-state"
mobile_dev_failure_home="$tmp_dir/mobile-dev-failure-home"
mobile_dev_failure_log="$tmp_dir/mobile-dev-failure-brew.log"
mkdir -p "$mobile_dev_failure_bin" "$mobile_dev_failure_home"
write_brew_bundle_stub "$mobile_dev_failure_bin/brew"

if ! HOME="$mobile_dev_failure_home" XDG_STATE_HOME="$mobile_dev_failure_state" MACOS_BREW_LOG="$mobile_dev_failure_log" MACOS_BREW_FAIL_DESKTOP_BULK=1 MACOS_BREW_FAIL_FORMULAE="mobile-dev-inc/tap/maestro" PATH="$mobile_dev_failure_bin:/usr/bin:/bin" \
  sh "$mobile_dev_bootstrap_script" >"$tmp_dir/mobile-dev-failure.out" 2>"$tmp_dir/mobile-dev-failure.err"; then
  fail "Maestro desktop app bundle failure does not block bootstrap script"
fi

mobile_dev_failure_marker="$mobile_dev_failure_state/terrapod/install-warnings/homebrew-desktop-apps"
if [ ! -f "$mobile_dev_failure_marker" ]; then
  fail "Maestro formula failure records a homebrew-desktop-apps marker"
fi
pass "Maestro formula failure records a homebrew-desktop-apps marker"

mobile_dev_failure_marker_text="$(cat "$mobile_dev_failure_marker")"
assert_contains_text "$mobile_dev_failure_marker_text" "mobile-dev-inc/tap/maestro" \
  "a failed tap formula is named in the desktop app warning marker"
assert_contains_text "$mobile_dev_failure_marker_text" "App Groups: mobile-dev" \
  "a failed tap formula is attributed to its macOS App Group"
```

The failing package is injected through `MACOS_BREW_FAIL_FORMULAE` rather than `MACOS_BREW_FAIL_CASKS` because Maestro is declared with `brew`, and the stub matches formulae and casks with separate loops.

- [ ] **Step 7: Run tests to verify they pass**

Run: `sh tests/chezmoiignore_test.sh`
Expected: PASS. If the marker exists but omits `App Groups: mobile-dev`, the awk group tracking in Task 1 is not carrying the group across `brew` lines; fix Task 1's parser rather than relaxing the assertion.

- [ ] **Step 8: Commit**

```bash
git add Brewfile.macos-desktop-apps.tmpl tests/chezmoiignore_test.sh
git commit -m "Declare the mobile-dev macOS App Group packages"
```

---

### Task 3: Render the Android shell environment

**Files:**
- Modify: `dot_zshenv.tmpl:23` (insert before the machine-local override loop)
- Test: `tests/chezmoiignore_test.sh`, `tests/zshenv_local_bin_test.sh`

**Interfaces:**
- Consumes: `enableMacosAppGroupMobileDev` from Task 2.
- Produces: exported `ANDROID_HOME`, `ANDROID_SDK_ROOT`, and `JAVA_HOME` in the rendered `.zshenv`.

- [ ] **Step 1: Write the failing test**

In `tests/chezmoiignore_test.sh`, add after the mobile-dev Brewfile assertions from Task 2:

```sh
mobile_dev_zshenv="$(render_managed_file "$macos_mobile_dev_data" ".zshenv")"
macos_default_zshenv="$(render_managed_file "$macos_data" ".zshenv")"

assert_contains_text "$mobile_dev_zshenv" 'export ANDROID_HOME="$HOME/Library/Android/sdk"' "mobile-dev group exports ANDROID_HOME"
assert_contains_text "$mobile_dev_zshenv" 'export ANDROID_SDK_ROOT="$ANDROID_HOME"' "mobile-dev group exports ANDROID_SDK_ROOT"
assert_contains_text "$mobile_dev_zshenv" 'export JAVA_HOME="$android_studio_jbr"' "mobile-dev group resolves JAVA_HOME to the Android Studio bundled runtime"
assert_contains_text "$mobile_dev_zshenv" 'if [[ -d "$android_studio_jbr" ]]; then' "JAVA_HOME is guarded so a failed Android Studio install cannot break java"
assert_contains_text "$mobile_dev_zshenv" 'if [[ -d "$ANDROID_HOME/platform-tools" ]]; then' "platform-tools joins PATH only when the SDK provides it"
assert_not_contains_text "$mobile_dev_zshenv" '.maestro/bin' "Maestro resolves through the Homebrew prefix instead of its vendor install directory"
assert_not_contains_text "$mobile_dev_zshenv" 'mise activate' "the Android environment does not repeat the mise activation the managed zshrc already runs"
assert_not_contains_text "$macos_default_zshenv" 'ANDROID_HOME' "macOS default does not render the Android environment"

mobile_dev_android_line="$(printf '%s\n' "$mobile_dev_zshenv" | grep -n 'export ANDROID_HOME' | head -1 | cut -d: -f1)"
mobile_dev_override_line="$(printf '%s\n' "$mobile_dev_zshenv" | grep -n 'zsh/path.d' | head -1 | cut -d: -f1)"
if [ -z "$mobile_dev_android_line" ] || [ -z "$mobile_dev_override_line" ]; then
  fail "rendered .zshenv should contain both the Android environment and the machine-local override loop"
fi
if [ "$mobile_dev_android_line" -ge "$mobile_dev_override_line" ]; then
  fail "the Android environment must render before the machine-local override loop so explicit overrides still run last"
fi
pass "the Android environment renders before the machine-local override loop"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `sh tests/chezmoiignore_test.sh`
Expected: FAIL with `not ok - mobile-dev group exports ANDROID_HOME`.

- [ ] **Step 3: Add the block to the zshenv template**

In `dot_zshenv.tmpl`, insert this immediately before the line `# Explicit machine-local overrides run last.`:

```
{{ if default false (get . "enableMacosAppGroupMobileDev") }}
# Android tooling from the mobile-dev macOS App Group.
# The Android SDK Manager owns this directory; the cask does not create it.
export ANDROID_HOME="$HOME/Library/Android/sdk"
export ANDROID_SDK_ROOT="$ANDROID_HOME"

# emulator is prepended first so platform-tools ends up ahead of it.
if [[ -d "$ANDROID_HOME/emulator" ]]; then
  path=("$ANDROID_HOME/emulator" $path)
fi
if [[ -d "$ANDROID_HOME/platform-tools" ]]; then
  path=("$ANDROID_HOME/platform-tools" $path)
fi

# Java resolves to the runtime Android Studio bundles, so the command line and
# the IDE always agree on a version.
android_studio_jbr="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
if [[ -d "$android_studio_jbr" ]]; then
  export JAVA_HOME="$android_studio_jbr"
fi
unset android_studio_jbr
{{ end }}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `sh tests/chezmoiignore_test.sh`
Expected: PASS.

- [ ] **Step 5: Verify PATH precedence did not regress**

Run: `sh tests/zshenv_local_bin_test.sh`
Expected: PASS. This suite asserts `$HOME/.local/bin` ordering and the Homebrew shellenv block; the Android block must not displace either.

- [ ] **Step 6: Verify the rendered file is valid zsh**

```bash
d=$(mktemp -d); : > "$d/chezmoi.toml"
chezmoi --config "$d/chezmoi.toml" --source . --destination "$d/home" \
  --override-data '{"chezmoi":{"os":"darwin"},"enableMacosAppGroupMobileDev":true}' \
  cat "$d/home/.zshenv" > "$d/zshenv"
zsh -n "$d/zshenv" && echo "rendered .zshenv parses"
rm -rf "$d"
```

Expected: `rendered .zshenv parses`.

- [ ] **Step 7: Commit**

```bash
git add dot_zshenv.tmpl tests/chezmoiignore_test.sh
git commit -m "Render the Android environment for the mobile-dev App Group"
```

---

### Task 4: Wire the setting key through the command surfaces

This task adds the key everywhere Terrapod reads or writes managed settings, and populates both App Group inventory surfaces. Setup disclosure and status reporting ship together deliberately: a key that installs software without naming it in the confirmation prompt is the defect this repository already corrected once for OrbStack.

**Files:**
- Modify: `dot_local/bin/executable_terrapod` at `managed_setup_keys` (:638), `setup_option_title` (:484), `show_setup_option_block` (:520), `prompt_for_setup_settings` (:625), `print_macos_app_group_status` (:1065), the settings section list (:1327-1338), `render_settings_data` (:1789), `render_preset_data` (:1802), `load_setup_defaults` (:1821), and the setup settings writer (:1866)
- Modify: `install.sh:769-778` (`managed_setup_keys`)
- Test: `tests/terrapod_command_test.sh`, `tests/terrapod_config_test.sh`, `tests/terrapod_installer_test.sh`

**Interfaces:**
- Consumes: `enableMacosAppGroupMobileDev` from Task 2.
- Produces: `render_settings_data <profile> <editor> <ai> <workspace> <terminalApps> <automation> <launcher> <monitoring> <developmentApps> <mobileDev>` — ten positional arguments. The tenth must be referenced as `${10}`, since `$10` parses as `$1` followed by a literal `0` in POSIX `sh`.

- [ ] **Step 1: Write the failing tests**

In `tests/terrapod_command_test.sh`, update the development-apps setup assertion by adding a mobile-dev assertion directly after it:

```sh
assert_contains "$setup_output_text" "  Installs Android Studio and Maestro." "gum setup lists Android Studio and Maestro in the mobile-dev App Group"
assert_contains "$setup_output_text" "  Sets ANDROID_HOME and JAVA_HOME for Android builds." "gum setup discloses that the mobile-dev App Group changes the shell environment"
```

Then, directly after each of the two existing assertions matching `development-apps              : enabled (Zed, Orca ADE, and OrbStack)`, add the matching mobile-dev line. For the one using `$macos_status_output`:

```sh
assert_contains "$macos_status_output" "mobile-dev                   : enabled (Android Studio and Maestro)" "Terrapod status lists Android Studio and Maestro in the enabled mobile-dev App Group"
```

For the one using `$dotted_status_output`:

```sh
assert_contains "$dotted_status_output" "mobile-dev                   : enabled (Android Studio and Maestro)" "Terrapod status reads root dotted data keys for the mobile-dev App Group"
```

The column padding above must place the colon in the same column as the neighbouring lines. Confirm the exact width in Step 6 rather than trusting the count here.

In `tests/terrapod_config_test.sh` and `tests/terrapod_installer_test.sh`, every fixture that lists the managed setting keys needs the new key. Find them with:

```bash
grep -rn "enableMacosAppGroupDevelopmentApps" tests/terrapod_config_test.sh tests/terrapod_installer_test.sh
```

For each hit that is an expected config body, add `enableMacosAppGroupMobileDev = false` after the development-apps line, except in a `workstation` Preset expectation, where it is `true`. For the single-line TOML fixture in `tests/terrapod_installer_test.sh` that spells the keys inside `data = { ... }`, append `, enableMacosAppGroupMobileDev = false` before the closing brace.

- [ ] **Step 2: Run tests to verify they fail**

Run: `sh tests/terrapod_config_test.sh`
Expected: FAIL, reporting a configured Preset whose written config lacks `enableMacosAppGroupMobileDev`.

`sh tests/terrapod_command_test.sh` also fails, but it fails on `main` too and aborts before reaching the new assertions. Use `terrapod_config_test.sh` as this task's red signal.

- [ ] **Step 3: Add the key to both managed key lists**

In `dot_local/bin/executable_terrapod`, in `managed_setup_keys`, add a line after `enableMacosAppGroupDevelopmentApps`:

```sh
    enableMacosAppGroupDevelopmentApps \
    enableMacosAppGroupMobileDev
}
```

Make the identical change in `install.sh`. Both lists must stay in sync; a key present in one and missing from the other makes setup completeness disagree between the installer and the command.

- [ ] **Step 4: Extend the settings and Preset writers**

In `render_settings_data`, add a line to the heredoc after the development-apps line:

```sh
enableMacosAppGroupDevelopmentApps = $9
enableMacosAppGroupMobileDev = ${10}
EOF
```

In `render_preset_data`, each branch grows from eight booleans to nine:

```sh
    minimal)
      render_settings_data "$profile" false false false false false false false false false
      ;;
    development)
      render_settings_data "$profile" true true true false false false false false false
      ;;
    workstation)
      render_settings_data "$profile" true true true true true true true true true
      ;;
```

In `load_setup_defaults`, add one line to each of the three Preset branches, after the development-apps line. It is `false` for `minimal` and `development`, and `true` for `workstation`:

```sh
      setup_enableMacosAppGroupMobileDev=false
```

In the setup settings writer that passes `"$setup_enableMacosAppGroupDevelopmentApps"` as its last argument, append a tenth argument on a new line:

```sh
    "$setup_enableMacosAppGroupDevelopmentApps" \
    "$setup_enableMacosAppGroupMobileDev"
```

- [ ] **Step 5: Add the setup prompt and its disclosure**

In `setup_option_title`, add a case before the `*)` fallback:

```sh
    "mobile-dev macOS App Group")
      printf '%s\n' "mobile-dev"
      ;;
```

In `show_setup_option_block`, add a case after the development-apps case:

```sh
    "mobile-dev macOS App Group")
      printf '%s\n' "  Installs Android Studio and Maestro."
      printf '%s\n' "  Sets ANDROID_HOME and JAVA_HOME for Android builds."
      printf '%s\n' "  Trusts only the fully-qualified mobile-dev-inc/tap/maestro formula, not the entire mobile-dev-inc/tap tap."
      printf '%s\n' "  Android SDK components and Xcode stay outside Terrapod."
      ;;
```

In `prompt_for_setup_settings`, add a prompt after the development-apps prompt:

```sh
    setup_enableMacosAppGroupMobileDev="$(prompt_setup_bool "mobile-dev macOS App Group" "$setup_enableMacosAppGroupMobileDev")"
```

In the same function's non-macOS branch, which sets every App Group variable to `false`, add:

```sh
    setup_enableMacosAppGroupMobileDev=false
```

Apply that same addition to every other place that zeroes the App Group variables. Find them with:

```bash
grep -n "setup_enableMacosAppGroupDevelopmentApps=false" dot_local/bin/executable_terrapod
```

- [ ] **Step 6: Add the status reporting**

In `print_macos_app_group_status`, add after the development-apps block:

```sh
  if is_enabled "$(config_data_bool "$config_file" enableMacosAppGroupMobileDev)"; then
    print_indented_colon_state_line "mobile-dev" "enabled" "(Android Studio and Maestro)"
  else
    print_indented_colon_state_line "mobile-dev" "disabled"
  fi
```

In the settings section that builds `*_state` variables, add the lookup and the printed line:

```sh
  mobile_dev_state="$(config_bool_state_label "$config_file" enableMacosAppGroupMobileDev)"
```

and, after `print_named_state_line "development-apps" "$development_apps_state"`:

```sh
  print_named_state_line "mobile-dev" "$mobile_dev_state"
```

Now confirm the padding used in the Step 1 assertions:

```bash
sh dot_local/bin/executable_terrapod status | grep -E "development-apps|mobile-dev"
```

Copy the `mobile-dev` line's exact spacing into the two assertions in `tests/terrapod_command_test.sh` if it differs from what Step 1 wrote.

Do not add a `tpod doctor` check for this group. Optional group installation failures are reported through the `homebrew-desktop-apps` warning marker that Task 2 exercises, and no other App Group has a dedicated doctor check.

- [ ] **Step 7: Run tests to verify they pass**

Run: `sh tests/terrapod_config_test.sh && sh tests/terrapod_installer_test.sh`
Expected: both PASS.

- [ ] **Step 8: Verify the shell syntax and the status output**

```bash
sh -n dot_local/bin/executable_terrapod && sh -n install.sh && echo "both parse"
sh dot_local/bin/executable_terrapod status | sed -n '/macOS App Groups:/,/^$/p'
```

Expected: `both parse`, and a status block whose last line is the `mobile-dev` line. On this machine the workstation Preset predates the new key, so `mobile-dev` reports `disabled` until setup is rerun. That is the intended completeness behavior, not a bug.

- [ ] **Step 9: Commit**

```bash
git add dot_local/bin/executable_terrapod install.sh tests/terrapod_command_test.sh tests/terrapod_config_test.sh tests/terrapod_installer_test.sh
git commit -m "Add the mobile-dev App Group setting to the command surfaces"
```

---

### Task 5: Document the group and its scope boundary

**Files:**
- Modify: `README.md` (App Group inventory list, machine-local settings table, scope paragraph)
- Modify: `README.ko.md` (the same three places)
- Modify: `CONTEXT.md` (macOS Desktop App Stack definition, App Group facts)
- Create: `docs/adr/0013-scope-the-mobile-dev-app-group.md`
- Test: `tests/readme_optional_stack_profiles_test.sh`, `tests/readme_korean_test.sh`

**Interfaces:**
- Consumes: the group membership and setting key from Tasks 2 and 4.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Write the failing tests**

In `tests/readme_optional_stack_profiles_test.sh`, add after the development-apps option row assertions:

```sh
assert_key_row_contains '`enableMacosAppGroupMobileDev`' 'mobile-dev' \
  "README documents mobile-dev group on its option row"
assert_key_row_contains '`enableMacosAppGroupMobileDev`' 'Android Studio' \
  "README documents Android Studio on the mobile-dev option row"
assert_key_row_contains '`enableMacosAppGroupMobileDev`' 'mobile-dev-inc/tap/maestro' \
  "README documents Maestro's fully-qualified formula source"
assert_contains 'Terrapod does not install Android SDK components or Xcode. Android Studio'"'"'s SDK Manager owns the SDK, and Xcode is distributed through the App Store.' \
  "README documents the mobile development scope boundary"
```

In `tests/readme_korean_test.sh`, add after the development-apps assertions:

```sh
assert_contains "$korean_readme" '`mobile-dev`: Android Studio와 Maestro(`mobile-dev-inc/tap/maestro`).' \
  "README.ko.md documents Android Studio and Maestro in the mobile-dev inventory"
assert_contains "$korean_readme" '| `enableMacosAppGroupMobileDev` | `false` | mobile-dev macOS App Group인 Android Studio와 Maestro(`mobile-dev-inc/tap/maestro`)를 설치합니다. |' \
  "README.ko.md documents the mobile-dev option row"
assert_contains "$korean_readme" 'Terrapod은 Android SDK component와 Xcode를 설치하지 않습니다. SDK는 Android Studio의 SDK Manager가 소유하고, Xcode는 App Store로 배포됩니다.' \
  "README.ko.md documents the mobile development scope boundary"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `sh tests/readme_optional_stack_profiles_test.sh`
Expected: FAIL with `not ok - README documents mobile-dev group on its option row`.

- [ ] **Step 3: Update README.md**

Add to the App Group inventory list, after the `development-apps` bullet:

```markdown
- `mobile-dev`: Android Studio and Maestro (`mobile-dev-inc/tap/maestro`).
```

Add to the machine-local settings table, after the development-apps row:

```markdown
| `enableMacosAppGroupMobileDev` | `false` | Installs the mobile-dev macOS App Group: Android Studio and Maestro (`mobile-dev-inc/tap/maestro`). It also sets `ANDROID_HOME` and `JAVA_HOME`. |
```

Add a paragraph after the sentence describing Orca's trust boundary:

```markdown
Terrapod trusts only the fully-qualified `mobile-dev-inc/tap/maestro` formula, not the entire `mobile-dev-inc/tap` tap. The unrelated homebrew-cask `maestro` is a different product and is never installed.

Terrapod does not install Android SDK components or Xcode. Android Studio's SDK Manager owns the SDK, and Xcode is distributed through the App Store. `JAVA_HOME` resolves to the runtime bundled with Android Studio rather than a separately declared JDK, so the command line and the IDE always agree on a Java version.
```

- [ ] **Step 4: Update README.ko.md**

Add the matching bullet after the `development-apps` bullet:

```markdown
- `mobile-dev`: Android Studio와 Maestro(`mobile-dev-inc/tap/maestro`).
```

Add the matching settings row after the development-apps row:

```markdown
| `enableMacosAppGroupMobileDev` | `false` | mobile-dev macOS App Group인 Android Studio와 Maestro(`mobile-dev-inc/tap/maestro`)를 설치합니다. `ANDROID_HOME`과 `JAVA_HOME`도 함께 설정합니다. |
```

Add the matching paragraphs after the Orca trust sentence:

```markdown
Terrapod은 fully-qualified `mobile-dev-inc/tap/maestro` formula만 trust하며, `mobile-dev-inc/tap` tap 전체를 trust하지 않습니다. homebrew-cask의 `maestro`는 다른 제품이므로 설치하지 않습니다.

Terrapod은 Android SDK component와 Xcode를 설치하지 않습니다. SDK는 Android Studio의 SDK Manager가 소유하고, Xcode는 App Store로 배포됩니다. `JAVA_HOME`은 별도로 선언한 JDK가 아니라 Android Studio가 번들한 runtime을 가리키므로, command line과 IDE가 항상 같은 Java version을 사용합니다.
```

- [ ] **Step 5: Update CONTEXT.md**

Widen the macOS Desktop App Stack definition so it admits tap-delivered companion CLIs. Replace its description line with:

```markdown
The opt-in macOS package set for GUI apps, system-style desktop apps, cask-delivered desktop support tools, and tap-delivered companion CLIs that ship with those apps.
```

Add these facts next to the existing App Group facts:

```markdown
- The mobile-dev **macOS App Group** contains Android Studio and Maestro.
- The mobile-dev **macOS App Group** declares `mobile-dev-inc/tap/maestro` with `trusted: true` so Homebrew trusts only the Maestro formula, not the entire `mobile-dev-inc/tap` tap.
- The homebrew-cask `maestro` is a different product from the mobile-dev **macOS App Group**'s Maestro and is never declared.
- The managed `.zshenv` exports `ANDROID_HOME`, `ANDROID_SDK_ROOT`, and `JAVA_HOME` only when the mobile-dev **macOS App Group** is enabled, and renders them before the machine-local override loop so explicit overrides still run last.
- `JAVA_HOME` resolves to the runtime bundled with Android Studio, so **Terrapod** declares no separate JDK.
- Android SDK components and Xcode remain outside **Terrapod** ownership; the Android SDK Manager owns the former and the App Store distributes the latter.
```

- [ ] **Step 6: Write the ADR**

Create `docs/adr/0013-scope-the-mobile-dev-app-group.md`:

```markdown
# Scope the mobile-dev App Group to entry points

The `mobile-dev` **macOS App Group** installs Android Studio and Maestro and
renders the Android and Java shell environment. It does not install Android SDK
components, Xcode, or a separate JDK.

`JAVA_HOME` resolves to the runtime Android Studio bundles at
`/Applications/Android Studio.app/Contents/jbr/Contents/Home`.

## Considered Options

- Declare Android SDK components through `sdkmanager`: rejected because
  `platforms`, `build-tools`, `ndk`, and `system-images` are license-gated
  multi-gigabyte downloads, and accepting licences inside a non-interactive
  `tpod apply` is not something Terrapod should do on a user's behalf.
- Install Xcode through a third-party CLI: rejected because App Store
  distribution requires Apple account authentication, which a non-interactive
  apply cannot supply.
- Declare a JDK through the `temurin` cask or mise: rejected because a second
  Java runtime can disagree with the version Android Studio uses for Gradle
  builds, and the bundled runtime is already installed by the cask this group
  declares.
- Model the group as a cross-profile optional stack: rejected because it
  depends on the `android-studio` cask and an `/Applications` path, so it would
  introduce a macOS-only optional stack and duplicate the App Group machinery.

## Consequences

- A fresh macOS workstation gets the IDE, the SDK Manager, and Maestro from
  `tpod apply`, then populates the SDK through Android Studio on first launch.
- `ANDROID_HOME` points at `$HOME/Library/Android/sdk`, a directory the SDK
  Manager creates rather than the cask.
- Existing vendor-installed Maestro binaries under `~/.maestro/bin` are not
  removed. The existing shadowing warning reports them when they resolve ahead
  of the Homebrew copy.
- Adding `enableMacosAppGroupMobileDev` changes managed setup config
  completeness, so already-configured machines are asked to rerun setup.
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `sh tests/readme_optional_stack_profiles_test.sh && sh tests/readme_korean_test.sh`
Expected: both PASS.

- [ ] **Step 8: Run the full suite**

```bash
for f in tests/*.sh tests/*.zsh; do
  case "$f" in *homebrew_ubuntu_smoke.sh) continue;; esac
  case "$f" in *.zsh) r=zsh;; *) r=sh;; esac
  if out=$("$r" "$f" 2>&1); then echo "PASS $f"; else echo "FAIL $f"; printf '%s\n' "$out" | grep -v '^ok - ' | tail -3; fi
done
```

Expected: every file PASSes except `tests/jetendard_font_test.sh`, `tests/jetendard_settings_test.sh`, and `tests/terrapod_command_test.sh`, which fail identically on `main`. If any other file fails, or if those three fail at a different assertion than they do on `main`, stop and investigate rather than proceeding.

- [ ] **Step 9: Commit**

```bash
git add README.md README.ko.md CONTEXT.md docs/adr/0013-scope-the-mobile-dev-app-group.md tests/readme_optional_stack_profiles_test.sh tests/readme_korean_test.sh
git commit -m "Document the mobile-dev App Group and its scope boundary"
```
