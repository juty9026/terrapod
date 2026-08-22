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
- The existing vendor-installed Maestro binary under `~/.maestro/bin` is left
  in place, consistent with Terrapod's non-destructive apply contract. The
  managed shell environment no longer adds that directory to PATH, so the
  Homebrew copy is the one that resolves. Terrapod does not detect or report a
  vendor copy that a user's own PATH additions place ahead of it.
- Adding `enableMacosAppGroupMobileDev` changes managed setup config
  completeness, so already-configured machines are asked to rerun setup.
