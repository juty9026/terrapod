# Install Grok Bot from its official .deb on the VPS Shell Profile

The **VPS Shell Profile** runs Grok Bot, the desktop agent app that the package
`grok-bot` ships, headless under Xvfb as a systemd user service, so that Grok
Bot can run tasks on that machine. Homebrew has no formula or cask for it. So
`.chezmoiscripts/run_after_86-install-grok-bot.sh.tmpl` installs Grok Bot from
the vendor's official `.deb` when the `grok-bot` package is missing. Grok Bot is
mandatory on the VPS profile and has no setting of its own.

The step has the shape of ADR 0026 and uses the same shared library,
`dot_local/lib/terrapod/vendor-deb-install.sh`. It is a narrow exception to APT
being the **Bootstrap Package Manager**, for the `grok-bot` package alone, with
its ordinary APT dependencies. It does not make a vendor `.deb` an approved
package source for other tools.

The step runs only while `dpkg-query` does not report `grok-bot` as installed.
An installed Grok Bot exits the script before any network access. Terrapod never
upgrades or replaces an installed Grok Bot.

When Grok Bot is missing, the step reads the vendor's APT index at
`https://downloads.cursor.com/aptrepo/dists/grok-bot/main/binary-<arch>/Packages`,
with `arm64` for `aarch64` and `arm64`, and `amd64` for `x86_64` and `amd64`. It
takes the `Filename` and `SHA256` fields of the `grok-bot` stanza for that
architecture. The index lists one version per architecture, so the first
matching stanza is the only one. The `Filename` must match
`pool/<path>/grok-bot_<version>_<arch>.deb`, with no `..`, before it enters a
URL or a path, and the digest must be 64 lowercase hex digits. The script
downloads `https://downloads.cursor.com/aptrepo/<Filename>` into a private
temporary directory, compares the sha256 digest, and installs through the shared
`apt-get update` and `apt-get install -y <absolute path>` tail.

## Considered Options

- Install the AppImage: rejected because Ubuntu 24.04 sets
  `kernel.apparmor_restrict_unprivileged_userns=1`, which stops Electron's
  sandbox unless an AppArmor profile for the app is loaded. The `.deb` installs
  that profile, and an AppImage cannot. The VPS also has no `libfuse2t64`.
- Add the vendor's APT repository from Terrapod and run `apt-get install
  grok-bot`: rejected because it makes Terrapod own a third-party signing key
  and source file. The `.deb` registers its own source.
- Pin a version in the repository: rejected because Grok Bot releases often, and
  a pin recreates a stale install that nobody updates.

## Consequences

- The script follows ADR 0022. The `grok-bot` install warning category names the
  failed step, such as the architecture check, `sudo`, the index, the download,
  the checksum, `apt update` or `apt install`, and apply continues. A later apply
  retries a missing Grok Bot and clears the warning. The script exits 1 only
  when the warning cannot be recorded.
- The script runs after the Orca step and renders only on Linux. It has no
  `tpod status` or `tpod doctor` check.
- The `.deb`'s own post-install step registers
  `/etc/apt/sources.list.d/grok-bot.sources`, a signed APT repository, and an
  AppArmor profile. Terrapod writes neither. After the install, `apt upgrade`
  is the update path, and Terrapod does not run it.
- The index is read over HTTPS without verifying its `InRelease` signature, and
  the digest comes from the same index as the `.deb` path. That is the same trust
  level as Orca's manifest in ADR 0026: it catches a truncated or corrupted
  download, but it does not authenticate the publisher beyond the address.
- A release published between the index read and the download shows up as a
  recorded download or checksum failure, and the next apply retries.
- The package declares `Conflicts`, `Provides` and `Replaces` for `sand`, the
  app's internal name (its bundle identifier is `com.anysphere.sand`), so an
  install under the old package name is replaced. Terrapod does not manage
  `sand`.
- ADR 0028 supersedes this decision's exclusion of headless service
  management. The package script still does not start, enable, or restart
  anything. A separate opt-in step manages the user service and Xvfb display.
- The Linux command is `grok-bot`.
