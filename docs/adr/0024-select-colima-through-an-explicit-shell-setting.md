# Select Colima through an explicit shell setting

Issue [#288](https://github.com/juty9026/terrapod/issues/288) introduces
`enableColimaDockerHost` as a macOS-only **Managed Setting** for **Colima
Docker Host Selection**. It is a shell environment preference, independent of
**macOS App Groups** and the **Optional Development Workspace**, and every
**Preset** leaves it disabled. Keeping the choice explicit prevents a stopped
Colima VM or a shell started before the VM from changing the intended endpoint.

The managed `.zshenv` exports the default profile endpoint only when
`DOCKER_HOST` is unset. Empty values are explicit choices too. Resolve the root
using an existing `COLIMA_HOME` directory, an existing `~/.colima`, then the
managed `XDG_CONFIG_HOME`, following [Colima's location rules](https://github.com/abiosoft/colima/blob/main/docs/FAQ.md#specifying-the-config-location).
Machine-local overrides continue to run last. Shell startup does not invoke a
runtime or inspect socket availability.

This setting configures connections; it does not install or start Colima,
change Docker contexts or system socket links, or remove OrbStack integration.
Status and doctor report the configured selection without checking daemon
health. Ordinary zsh and inheriting children receive the environment; existing
parent and independent GUI/launchd processes, directly started sh, and zsh
sessions that skip user startup files do not receive it automatically.

The schema uses a `shell-environment` kind so the preference shares existing
writing, backup, boolean validation, and completeness rules without becoming
a package selection or inheriting the Workspace's optional-stack implication.
Existing configs missing the key require explicit setup/configure; routine
apply does not open Setup. The VPS Shell Profile always writes it disabled.
