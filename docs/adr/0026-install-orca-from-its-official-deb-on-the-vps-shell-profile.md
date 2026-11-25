# Install Orca from its official .deb on the VPS Shell Profile

The **VPS Shell Profile** runs Orca, the IDE from `stablyai/orca`, as a headless
remote server and uses its browser panes. Homebrew cannot install Orca on Linux:
the upstream `stablyai/orca` tap ships a macOS DMG cask only. So
`.chezmoiscripts/run_after_85-install-orca-ide.sh.tmpl` installs Orca from the
official `.deb` release asset when the `orca-ide` package is missing. Orca is
mandatory on the VPS profile and has no setting of its own.

This is a narrow exception to APT being the **Bootstrap Package Manager** that
installs only system and Homebrew bootstrap prerequisites, for the `orca-ide`
package alone. It does not make a release `.deb` an approved package source for
other tools, and the step stays a single script rather than a general `.deb`
installer. The `.deb` pulls about twenty desktop libraries and helpers from the
Ubuntu archive, such as `libgtk-3-0`, `xvfb` and `python3`, as ordinary APT
dependencies of that one package.

The step runs only while `dpkg-query` does not report `orca-ide` as installed.
An installed Orca exits the script before any network access, so there is no
release lookup on a machine that already has Orca. Terrapod never upgrades or
replaces an installed Orca. Orca's own update flow owns version freshness. For a
`.deb` install it announces a new release and hands over the `apt` command,
because it never raises privileges itself.
This keeps the rule that routine apply does no release lookup for software that
is already present.

When Orca is missing, the step reads the electron-updater manifest at a stable
`releases/latest/download` address: `latest-linux-arm64.yml` for `aarch64` and
`arm64`, and `latest-linux.yml` for `x86_64` and `amd64`. It takes the first
`.deb` entry and its base64 sha512 digest. The file name must match
`orca-ide_<version>_<arch>.deb` for the machine's architecture before it enters
a URL or a path. The script downloads that file into a private temporary
directory, compares the digest, and runs `apt-get update` and
`apt-get install -y <absolute path>` under `sudo` unless it runs as root. The
install passes `DEBIAN_FRONTEND=noninteractive` through `env`, because `sudo`
resets the environment.

## Considered Options

- Pin a version in the repository: rejected because Orca ships often, with more
  than a hundred upstream releases since 2026-08-06, so a pin recreates a stale
  install that nobody updates.
- Check the latest release on every apply and upgrade: rejected because it needs
  a service restart while agents run on the machine, and it breaks the rule that
  routine apply does no release lookup for an installed tool.
- Install the headless `orca-server-linux-*.tar.gz`: rejected because the
  server-only build has no browser panes, which this machine uses.
- Install the AppImage: rejected because it needs FUSE and the same desktop
  libraries, and its self-update is a flow in a desktop window.
- Publish and maintain a personal Homebrew tap for Orca: rejected because it adds
  a packaging project to own for one app.

## Consequences

- The script follows ADR 0022. The `orca-ide` install warning category names the
  failed step, such as the architecture check, `sudo`, the manifest, the
  download, the checksum, `apt update` or `apt install`, and apply continues. A
  later apply retries a missing Orca and clears the warning. The script exits 1
  only when the warning cannot be recorded.
- The script runs after the other always-run installers and renders only on
  Linux. It has no `tpod status` or `tpod doctor` check, like ego lite in
  ADR 0025.
- Orca takes about 700 MB installed, plus the desktop dependencies, against the
  disk floor of the **VPS Shell Profile**. The step is not gated on free disk.
- The `.deb` and its manifest come from the same release address. The digest
  catches a truncated or corrupted download, but it is not a signature that
  authenticates the publisher beyond that address.
- A release published between the manifest read and the package download shows
  up as a recorded download or checksum failure, and the next apply retries.
- `apt-get update` runs only on this path, so a stale package index cannot fail
  the dependency downloads on a machine whose bootstrap did not rerun.
- The `.deb` is installed from a private temporary directory by absolute path. On
  Ubuntu 24.04, APT installs from that directory without sandbox options.
- The hand-made `orca-remote.service` that runs Orca headless stays outside
  Terrapod. The script installs the package and does not start, enable or
  restart anything.
- The Linux command is `orca-ide`.
