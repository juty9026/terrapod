# Manage opt-in headless app user services

The VPS Shell Profile offers Terrapod-owned systemd user units for Orca and
Grok Bot. Package installation remains mandatory under ADRs 0026 and 0027.
Service activation is a separate per-host choice in
`~/.config/terrapod/headless-services.json`, outside the source repository.
This decision supersedes ADR 0026 and ADR 0027's statements that the service
definitions stay outside Terrapod. Their package-installation policy is unchanged.

One helper owns config validation, unit writes, and service lifecycle actions.
The Linux apply hook runs it after both package installers. User units avoid
privileged unit writes during apply. Lingering is an explicit administrator
step, rather than a hidden side effect of enabling a service.

The units use distinct names, `terrapod-orca.service` and
`terrapod-grok-bot.service`. Known legacy definitions block the corresponding
service until the user retires them. Existing unmanaged same-name units,
masks, and alternate unit paths are preserved. Each app reconciles separately,
so a Grok Bot prerequisite failure does not prevent Orca from starting.

Apply enables opted-in units and starts only inactive or failed services.
It never stops or restarts them. Opt-out leaves installed units enabled until
the user explicitly disables them during a maintenance window.

## Considered options

- Use Orca's upstream service installer: rejected because its Linux installer
  runs `systemctl --user restart` on each invocation. The
  [v1.4.274 installer source](https://github.com/stablyai/orca/blob/v1.4.274/src/cli/server/serve-service.ts)
  records that behavior.
- Manage units directly as chezmoi targets: rejected because chezmoi could
  overwrite a legacy unit before the lifecycle helper checks ownership.
- Use system units: rejected because apply would need privileged unit writes
  and a host-specific `User=` value.
- Capture Orca readiness JSON and store its pairing link: rejected because
  on-demand `orca-ide pair --address` works without restarting Orca. The service
  follows the upstream `--supervise --no-pairing` arguments and adds
  `--refuse-public`. It retains upstream default listener behavior. A pinned
  `--bind` prevents on-demand network pairing in v1.4.274.

## Consequences

- Orca uses its installed launcher. The unit follows the upstream
  [supervised service stop policy](https://github.com/stablyai/orca/blob/v1.4.274/src/cli/runtime/serve/supervision/serve-service-unit.ts),
  including `KillMode=process`, so the supervisor stops its server while
  terminal daemons can survive. Service logs contain no pairing offer.
- The pairing address is supplied only when the user requests a link.
  `--refuse-public` restricts public acceptance, not the listening socket.
  The user owns firewall and tailnet restrictions. See the upstream
  [serve command contract](https://github.com/stablyai/orca/blob/v1.4.274/src/cli/specs/serve.ts)
  and [pair command contract](https://github.com/stablyai/orca/blob/v1.4.274/src/cli/specs/pair.ts).
- Grok Bot uses `xvfb-run`, an explicit display other than `:99`, a private X
  authority file, and `Restart=always`. Missing Xvfb and xauth are reported as
  prerequisites. The service does not install or run VNC.
- Interactive Grok Bot sign-in uses temporary loopback VNC on an unused port
  through an SSH tunnel. Port `5900` is left for the existing VNC service.
- [The service guide](../headless-services.md) owns setup, migration, pairing,
  sign-in, and opt-out steps. Its lingering behavior follows
  [loginctl](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html).
  Its VNC options follow Ubuntu's
  [x11vnc manual](https://manpages.ubuntu.com/manpages/noble/man1/x11vnc.1.html).
