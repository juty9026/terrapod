# Mobile Dev App Group Design

## Goal

Reproduce this machine's Android development entry points on a fresh macOS
Terminal Profile through a new `mobile-dev` macOS App Group. The group installs
Android Studio and Maestro, and renders the Android and Java shell environment
that those tools need.

The group covers entry points only. Android SDK components and Xcode stay
outside Terrapod's ownership.

This change is non-destructive. Enabling the group does not remove an existing
Android Studio, Maestro, or SDK installation, and disabling it does not
uninstall anything.

## Current State

The configured workstation runs Android development entirely outside Terrapod:

- Android Studio is installed manually in `/Applications`, not through Homebrew.
- Maestro is installed by its own vendor script into `~/.maestro/bin`.
- No JDK is installed through Homebrew. `JAVA_HOME` points at Android Studio's
  bundled JBR, and `java` resolves to that OpenJDK 25 runtime.
- The Android SDK at `~/Library/Android/sdk` is populated by Android Studio's
  SDK Manager and holds `build-tools`, `cmake`, `ndk`, `platforms`, and
  `system-images`.
- Xcode is installed from the App Store at `/Applications/Xcode.app`.
- `~/.zshrc` carries a hand-added tail that exports `ANDROID_HOME`,
  `ANDROID_SDK_ROOT`, `JAVA_HOME`, and PATH entries for `platform-tools`,
  `emulator`, and `~/.maestro/bin`. It also repeats
  `eval "$(mise activate zsh)"`, which the managed `dot_zshrc.tmpl` already
  runs.

Because Terrapod owns `.zshrc`, applying the declared state deletes that tail.
No declared state replaces it, so a fresh machine has no Android environment at
all.

Neither CocoaPods, Watchman, nor Flutter is installed, so this machine is not
running a React Native or Flutter workflow. The group is scoped to native
Android tooling plus the Maestro end-to-end runner.

## Considered Approaches

### New `mobile-dev` macOS App Group

Add one setting key and one group to the rendered macOS Desktop App Stack
Brewfile, and render the shell environment from the same key.

This is the selected approach. It reuses the entire App Group mechanism:
the setup confirmation prompt, the `tpod status` line, the desktop app warning
marker, the reconcile script's group attribution, the documentation tables, and
the existing test fixtures. The `workstation` Preset already enables every App
Group, so the group joins that Preset without new Preset logic. macOS-only
scope is already inherent to the App Group concept.

Its cost is two cask-only assumptions that must be loosened, both local and
both reusable by any future group that ships a tap formula. See
"Reconcile Record Format".

### New `enableMobileDevStack` optional stack

Shape the group like the Optional AI Tool Stack, with its own rendered
Brewfile. This fits a mixed app-and-CLI payload without amending any
definition.

Rejected because every existing optional stack is cross-profile while this one
depends on the `android-studio` cask and an `/Applications` path. It would
introduce "macOS-only optional stack" as a new concept and duplicate the
installation, warning, status, and setup machinery that App Groups already
provide.

### Extend the existing `development-apps` App Group

Add both packages to the group that already carries Zed, Orca ADE, and
OrbStack, with no new setting key.

Rejected because it removes the user's choice. A machine that wants Zed and
Orca would be forced to install Android Studio and its multi-gigabyte payload,
and mobile development is a separate concern from general desktop development
tooling.

## Package Ownership

The `mobile-dev` macOS App Group owns:

- `android-studio`, a Homebrew cask, providing the IDE, the SDK Manager, and
  the bundled JBR that `JAVA_HOME` resolves to
- `mobile-dev-inc/tap/maestro`, a Homebrew formula, providing the `maestro`
  end-to-end test runner

Maestro is declared exactly the way Orca already is, as a fully-qualified token
carrying `trusted: true`:

```
brew "mobile-dev-inc/tap/maestro", trusted: true
```

Homebrew refuses to load formulae and casks from non-official taps unless they
are trusted, and `trusted: true` trusts this one entry rather than the whole
`mobile-dev-inc/tap` tap. No separate `tap` statement is needed; the
fully-qualified token taps on demand.

The cask named `maestro` in homebrew-cask is a different product, an AI agent
command center published at runmaestro.ai. The fully-qualified token also keeps
that name collision from resolving to the wrong package.

`android-platform-tools` is deliberately excluded. The Android SDK already
provides `adb` and `emulator`, and the rendered PATH points into the SDK.

## Scope Boundary

These stay outside Terrapod's ownership and are documented as such:

- **Android SDK components.** `platforms`, `build-tools`, `ndk`, `cmake`, and
  `system-images` are license-gated multi-gigabyte downloads owned by Android
  Studio's SDK Manager. Declaring them would put license acceptance and
  multi-gigabyte transfers inside a non-interactive `tpod apply`.
- **Xcode.** It is distributed through the App Store and needs Apple account
  authentication, which conflicts with non-interactive apply.
- **The JDK.** `JAVA_HOME` resolves to Android Studio's bundled JBR rather than
  a separately declared JDK, so the command-line toolchain and the IDE always
  agree on a Java version and no second multi-hundred-megabyte runtime is
  downloaded.

## Setting Surface

The group adds one machine-local setting key,
`enableMacosAppGroupMobileDev`, defaulting to `false`.

Preset behavior follows the existing rule without new logic. Only the
`workstation` Preset enables every macOS App Group, so `minimal` and
`development` record the key as `false`.

`render_settings_data` in `dot_local/bin/executable_terrapod` currently consumes
positional parameters `$1` through `$9`. The tenth key must be written as
`${10}`, and each `render_preset_data` branch grows from eight boolean
arguments to nine.

Adding a key changes managed setup config completeness. Terrapod treats a
managed setup config as complete only when the profile and every current
optional stack and App Group key are present, so every already-configured
machine is asked to rerun setup after this change. This matches what previous
App Group additions did.

## Shell Environment

The Android and Java environment renders from `dot_zshenv.tmpl`, not
`dot_zshrc.tmpl`. `.zshrc` loads only for interactive shells, while
`ANDROID_HOME` and `JAVA_HOME` must also reach non-interactive callers such as
Gradle wrapper scripts and IDE-spawned shells. PATH initialization is already
`.zshenv`'s stated responsibility.

The block renders only when `enableMacosAppGroupMobileDev` is true, and sits
immediately before the `~/.config/zsh/path.d` loop so explicit machine-local
overrides still run last.

The rendered block differs from the current hand-added `.zshrc` tail in three
ways:

- `eval "$(mise activate zsh)"` is dropped. The managed `dot_zshrc.tmpl`
  already activates mise, so the tail repeated it.
- `$HOME/.maestro/bin` is dropped from PATH. Maestro now comes from Homebrew,
  whose prefix `.zshenv` already exports. Terrapod does not delete the existing
  vendor-installed binary; the existing status and doctor warning for a command
  resolving outside the active Homebrew prefix reports the shadowing instead.
- Existence guards wrap the exports, matching the guard the OrbStack shell
  integration uses. A failed Android Studio install would otherwise leave
  `JAVA_HOME` pointing at a missing directory and break `java` entirely, and
  absent SDK directories would otherwise be appended to PATH as noise.

`ANDROID_HOME` remains `$HOME/Library/Android/sdk`. That path is created by the
SDK Manager rather than by the cask, which is exactly the ownership boundary
this design documents.

## Reconcile Record Format

`10-reconcile-homebrew.sh` installs the rendered desktop Brewfile in one bulk
`brew bundle` pass and drops to per-package retry only when that pass fails.
The retry path is cask-only in two places:

- `desktop_app_cask_records` parses group comments and `cask "` lines only.
- The retry loop rewrites each record as a single-package Brewfile using
  `cask "%s"`.

Left unchanged, Maestro would install in the bulk pass but would never be named
in the desktop app warning marker when installation fails.

The record format grows a declaration kind and the declaration's options, so
each record carries group, kind, token, and options. The awk program recognizes
`brew "` lines alongside `cask "` lines, and the retry loop rewrites the
matching declaration with its options preserved.

The same change fixes an existing defect in that function. Records currently
carry only the package name, so Orca's `trusted: true` is dropped when a bulk
failure sends `stablyai/orca/orca` through per-cask retry. Homebrew stores
trust persistently in `~/.homebrew/trust.json`, so a machine that already
trusts Orca is unaffected, which is why the defect has gone unnoticed; a
machine whose bulk pass failed before trust was recorded cannot load the entry
on retry. Preserving options alongside the kind repairs that path, and Maestro
depends on the same preservation.

## Command Surface

Both App Group inventory surfaces are populated in the same change:

- `show_setup_option_block` gains a `mobile-dev macOS App Group` case. Because
  this block prints immediately before the gum confirmation prompt, it is the
  disclosure the user acts on. It names Android Studio and Maestro and states
  that the group sets `ANDROID_HOME` and `JAVA_HOME`, since this is the first
  App Group to modify the shell environment.
- `print_macos_app_group_status` gains a `mobile-dev` line.

`setup_option_title` emits group names only and needs no inventory text.

`tpod doctor` gains no new check. Optional group installation failures are
reported through the desktop app warning marker, and this group is not an
exception to that structure.

## Documentation

- `README.md` and `README.ko.md` gain the group in the App Group inventory list
  and the machine-local settings table, plus a paragraph stating that Android
  SDK components and Xcode stay outside Terrapod.
- `CONTEXT.md` widens the macOS Desktop App Stack definition so it covers
  companion CLIs delivered through a tap alongside cask-delivered apps, and
  records the group's membership, the `JAVA_HOME` resolution, and the scope
  boundary.
- A new ADR records why SDK components, Xcode, and a separately declared JDK
  are excluded from the reproduction target.

## Testing

- `tests/chezmoiignore_test.sh`: the `mobile-dev` group renders exactly
  `android-studio` and the fully-qualified Maestro token with `trusted: true`;
  other groups and the macOS default render neither; `.zshenv` renders the
  Android environment only when the group is enabled, and renders it before the
  `path.d` loop.
- `tests/terrapod_command_test.sh`: the setup option block and the `tpod status`
  line name Android Studio, Maestro, `ANDROID_HOME`, and `JAVA_HOME`.
- `tests/readme_optional_stack_profiles_test.sh` and
  `tests/readme_korean_test.sh`: both READMEs document the group and the scope
  boundary.
- `tests/terrapod_config_test.sh` and `tests/terrapod_installer_test.sh`: each
  Preset writes the new key, and a config missing the key is treated as
  incomplete.
- `tests/zshenv_local_bin_test.sh`: the Android environment does not displace
  `$HOME/.local/bin` ordering or the Homebrew shellenv block.
- Reconcile coverage in `tests/chezmoiignore_test.sh`: a bulk failure attributes
  a failed tap formula to the `mobile-dev` group in the desktop app warning
  marker, and per-package retry of `stablyai/orca/orca` preserves
  `trusted: true`.
