# Install ego lite from its official DMG

The browsers **macOS App Group** replaces Aside with ego lite. ego lite has no
official Homebrew cask, so `.chezmoiscripts/run_after_80-install-ego-lite.sh.tmpl`
installs it from the architecture-appropriate official macOS DMG. Google Chrome
stays a Homebrew cask, and `enableMacosAppGroupBrowsers` selects both apps.

This is a narrow exception to ADR 0001's Homebrew ownership of macOS GUI apps,
for ego lite alone. It does not make direct DMG installation an approved package
source for other desktop apps, and the step stays a single script rather than a
general DMG installer.

The step runs only while no `ego lite.app` with the bundle ID
`com.citrolabs.ego.lite` exists in `/Applications` or `~/Applications`. It never
opens the app, upgrades an existing install, replaces another app at the
destination, or calls `sudo`. ego lite's own updater owns version freshness, and
the first launch owns onboarding and data import.

## Considered Options

- Run the vendor's install script: rejected because it launches the app and
  starts its onboarding, and it replaces an existing app and falls back to
  `sudo`, none of which belongs in `tpod apply`.
- Publish and maintain a Terrapod Homebrew tap: rejected because it adds a
  packaging project to own for one app, while the upstream cask request remains
  open.
- Declare a nonexistent `ego-lite` cask: rejected because it would fail every
  bundle in the browsers group.
- Pin a DMG version and check upstream releases on apply: rejected because
  routine apply is install-only and the app already updates itself.
- Install into `~/Applications` when `/Applications` is not writable: rejected
  to keep one install destination. The failure is a visible warning, and
  detection still honors an app the user placed in `~/Applications`.

## Consequences

- The script follows ADR 0022. The `ego-lite` install warning category names the
  failed step, such as the download, mount, identity check, copy, or detach, and
  apply continues.
  A later apply retries a missing app and clears the warning. Disabling the group
  clears it too. The script exits 1 only when the warning cannot be recorded.
- The script runs last among the chezmoi scripts, so even that failure comes
  after every other declared step.
- The DMG URLs are the rolling latest-release addresses from the vendor's
  installer. A new upstream release changes what a fresh install receives, and
  a vendor address change appears as a recorded download failure.
- The app is staged in a hidden directory beside the destination and moved into
  place, so an interrupted copy cannot leave a partial `ego lite.app`.
- `curl` does not set the quarantine attribute, so the script does not strip it
  the way the vendor's installer does.
- Identity means the bundle ID in the app's `Info.plist`. The script does not
  verify the code signature or a pinned team identifier.
- Removing Aside from the declarations follows ADR 0012: Terrapod does not
  uninstall it, remove its data, or migrate a browser profile.
