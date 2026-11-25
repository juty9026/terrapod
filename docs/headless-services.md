# Run Orca and Grok Bot headless on Ubuntu

Use the VPS Shell Profile's non-root management user for these steps.
Terrapod installs both apps, but their user services are opt-in per host.
No Preset enables them. Keep host configuration out of the source repository.

## Enable the services

1. Run `tpod apply` to install the packages and the service helper.
   Orca must support `serve --supervise --no-pairing --refuse-public`, as
   release `v1.4.274` does.
2. Install the virtual-display prerequisites if you want Grok Bot.

   ```sh
   sudo apt-get update
   sudo apt-get install -y xvfb xauth
   ```

3. Enable lingering so your user manager starts at boot and survives logout.

   ```sh
   sudo loginctl enable-linger "$(id -un)"
   loginctl show-user "$(id -un)" --property=Linger
   ```

   The last command must print `Linger=yes`. Run the remaining commands from
   an SSH login as that user, with a working `systemctl --user` connection.

4. If this host already has services, complete the migration below first.
5. Create `~/.config/terrapod/headless-services.json` on the host.

   ```sh
   mkdir -p "$HOME/.config/terrapod"
   (umask 077; cat >"$HOME/.config/terrapod/headless-services.json" <<'JSON'
   {
     "orca": {"enabled": true, "port": 6768},
     "grokBot": {"enabled": true, "display": 142}
   }
   JSON
   )
   ```

   Omit either object, or set its `enabled` to `false`, to exclude that service
   from apply. The defaults are both disabled, Orca port `6768`, and Grok Bot
   display `142`. Ports must be integers from `1` through `65535`. Displays
   use the same range, except `99`, which belongs to Orca's browser sidecar.
   Choose an unused display. Terrapod does not kill another display's X server.
   Unknown keys and invalid values fail before any service changes.

6. Run `tpod apply`, then inspect the services you enabled.

   ```sh
   systemctl --user status terrapod-orca.service terrapod-grok-bot.service
   journalctl --user -u terrapod-orca.service -u terrapod-grok-bot.service -n 50
   ```

Terrapod writes its units under `~/.config/systemd/user`, reloads the user
manager, enables the units, and starts them only when inactive or failed.
Apply never stops or restarts a service. A unit update or host configuration
change takes effect at the next service start. Schedule any manual restart
when no agents depend on the app.

A service-specific conflict or missing executable records a `headless-services`
install warning and leaves the other opted-in service eligible to start.
Fix the reported problem and rerun apply. Missing lingering or an unavailable
user manager prevents both services from being installed.

## Pair a client with Orca

The service uses Orca's default listener behavior and `--refuse-public`.
Orca can listen on all interfaces. Public client acceptance is refused, but
this flag does not restrict the listening socket to a tailnet interface.
Use your host firewall if you need that restriction. Terrapod does not change
firewall rules or Tailscale configuration.

On the host, request an access link for an address the client can reach.

```sh
orca-ide pair --address YOUR_TAILNET_HOST
```

Use that link in the client's **Settings > Remote Orca Servers > Add Server**.
Use `orca-ide pair --mobile --address YOUR_TAILNET_HOST` for a phone.
Pairing does not restart the server. Treat the printed link as a secret.
Do not paste it into tickets, service definitions, or logs.

The service always uses `--no-pairing`. It does not emit a readiness JSON
pairing offer or create `latest-readiness.json`. Existing Orca profile data
and paired clients remain under Orca's ownership.

## Sign in to Grok Bot through temporary VNC

Keep Grok Bot running on its managed virtual display while you sign in.
The example uses display `:142` and VNC port `5902`. Substitute your configured
display, and choose another unused VNC port if necessary. Port `5900` belongs
to an unrelated VNC service on the existing host.

1. Install the temporary VNC helper on the host.

   ```sh
   sudo apt-get install -y x11vnc
   ```

2. Check that port `5902` has no listener. The output should be empty.

   ```sh
   ss -ltnH '( sport = :5902 )'
   ```

3. Create a VNC password in the private state directory.

   ```sh
   (umask 077; x11vnc -storepasswd "$HOME/.local/state/terrapod/grok-bot/vnc-password")
   ```

4. Start VNC in the foreground on loopback only.

   ```sh
   x11vnc -norc -localhost -display :142 \
     -auth "$HOME/.local/state/terrapod/grok-bot/Xauthority" \
     -rfbauth "$HOME/.local/state/terrapod/grok-bot/vnc-password" \
     -rfbport 5902
   ```

5. From your workstation, open an SSH tunnel to the host.

   ```sh
   ssh -N -L 127.0.0.1:5902:127.0.0.1:5902 USER@HOST
   ```

6. Connect your VNC viewer to `127.0.0.1:5902` and sign in to Grok Bot.
   If the sign-in opens a browser URL, complete it through your normal account
   flow. Keep account credentials and session data on the host.
7. Disconnect the viewer after sign-in. Stop x11vnc and the SSH tunnel with
   Ctrl-C if they are still running. Leave the Grok Bot service running.

Terrapod never creates a permanent VNC service or opens a VNC firewall port.
The X cookie directory is mode `700`, and `Xauthority` is mode `600`.
Grok Bot's application profile remains outside Terrapod management.

## Migrate existing services during a maintenance window

These steps stop the old services. Finish active agent work first.
Keep the same management user so each app retains its profile and credentials.

Terrapod refuses existing `orca-remote.service`, upstream `orca-serve.service`,
and `grok-bot.service` definitions in either the system or user manager.
It also refuses an unmanaged file, mask, or alternate unit path with one of
its own names. It never overwrites an existing unit to claim ownership.

1. Save a private copy of the old definitions and wrapper scripts.
   For the existing system Orca and user Grok Bot layout, run the following.

   ```sh
   backup_dir="$HOME/.local/state/terrapod/headless-migration-$(date +%Y%m%d%H%M%S)"
   (umask 077; mkdir -p "$backup_dir")
   sudo cp /etc/systemd/system/orca-remote.service "$backup_dir/"
   sudo cp /usr/local/libexec/orca-remote-start "$backup_dir/"
   cp "$HOME/.config/systemd/user/grok-bot.service" "$backup_dir/"
   ```

2. Stop and disable the old services, then move their definitions out of the
   systemd search paths. Leave the old wrapper and application data intact.

   ```sh
   sudo systemctl disable --now orca-remote.service
   systemctl --user disable --now grok-bot.service
   sudo mv /etc/systemd/system/orca-remote.service "$backup_dir/orca-remote.service.retired"
   mv "$HOME/.config/systemd/user/grok-bot.service" "$backup_dir/grok-bot.service.retired"
   sudo systemctl daemon-reload
   systemctl --user daemon-reload
   ```

   If you used Orca's upstream service installer, back up its unit first,
   then use `orca-ide serve service uninstall` instead. For another layout,
   retire the definition from the manager that owns it.

3. Follow **Enable the services** above, then check logs and client access.
   The old readiness file can still contain a valid pairing link. Keep it
   private and retire it through your normal secret-handling process.

To roll back, disable the Terrapod units, restore the saved definitions to
their original paths, reload the corresponding managers, and enable the old
services. Do not run both sets against the same app profile.

## Stop managing a service

Setting `enabled` to `false` or removing the config only excludes future apply
actions. It does not stop, disable, or remove an installed unit. The installed
unit can still start at boot. This preserves running work when configuration
changes.

During a maintenance window, set `enabled` to `false`, then explicitly disable
and stop the selected service.

```sh
systemctl --user disable --now terrapod-orca.service
systemctl --user disable --now terrapod-grok-bot.service
```

You can then remove the corresponding Terrapod unit file from
`~/.config/systemd/user` and run `systemctl --user daemon-reload`.
Keep lingering enabled if other user services depend on it.
