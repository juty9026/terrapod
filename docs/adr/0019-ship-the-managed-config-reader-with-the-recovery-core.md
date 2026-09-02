# Ship the managed config reader with the recovery core

The reader for the Terrapod-managed chezmoi TOML config lives in one file,
`dot_local/lib/terrapod/config-toml.sh`, and the first-run recovery core
installs it alongside the `terrapod` command and the `tpod` alias. The parser
was previously copied verbatim into both `install.sh` and
`dot_local/bin/executable_terrapod`, so every fix had to be made twice and the
two copies had already drifted.

`tpod` loads the reader once at process start and stops if it is absent. This
is the opposite of the rule ADR 0016 gives `install-warnings.sh`, which `tpod`
resolves lazily at each call site precisely because a decision made at process
start is wrong during the first-run apply. The difference is what the recovery
core installs. Install warnings are written by the full declared-state apply,
so their library legitimately does not exist yet when the first-run `tpod
apply` starts. The config reader is different: `tpod apply` reads the managed
config before it delegates to chezmoi, so a `tpod` that cannot parse the config
is not a degraded command, it is a broken one. Making it part of the recovery
core is what makes the startup load correct, and ADR 0007's definition of the
recovery core — what must exist for `tpod` to work before the full apply — is
what it belongs to.

Only `config-toml.sh` joins the recovery core. The rest of
`~/.local/lib/terrapod/` keeps arriving with the full declared-state apply.

`install.sh` sources the reader from the checked-out source directory and
treats absence as fatal, the same rule ADR 0016 gives it for
`install-warnings.sh`: a checkout missing the library is a broken clone.
`source_has_recovery_core_files` requires the reader too, so an
unresumable checkout is rejected before the installer reaches the load.

The two entry points read the config path by different rules, on purpose. The
library exposes `terrapod_default_chezmoi_config_file`, which computes the path
chezmoi itself would use, and `terrapod_chezmoi_config_file`, which wraps it
and honours `TERRAPOD_CHEZMOI_CONFIG`. `tpod` uses the second, because
`render_chezmoi_command` passes the override on to chezmoi as `--config`.
`install.sh` uses the first, because it runs chezmoi without `--config` and
hands Terrapod Setup an empty override; honouring the variable there would make
the installer resume from a config it never applies.

## Considered Options

- Give `tpod` a source-tree fallback so the parser could stay outside the
  recovery core: rejected as circular. The source directory is
  `$XDG_DATA_HOME/chezmoi` by default, but resolving where chezmoi actually
  reads from needs the config the fallback exists to parse. ADR 0016 rejected a
  `chezmoi source-path` fallback for the same family of reasons.
- Promote all of `~/.local/lib/terrapod/` to the recovery core: rejected. The
  recovery core is the minimum needed to reach a working `tpod`, not a
  convenient place to put libraries. Widening it would make every future helper
  a first-run installer concern.
- Extract the parser for `install.sh` only and leave the copy in `tpod`:
  rejected because it leaves the duplication the change exists to remove.
- Unify `chezmoi_config_file` on the `tpod` behaviour so both honour
  `TERRAPOD_CHEZMOI_CONFIG`: rejected. The installer passes no `--config` to
  chezmoi, so honouring the override would leave it reading one config and
  applying another — a worse state than the drift it replaces.

## Consequences

- `tpod` fails with an explicit message naming the missing path when
  `config-toml.sh` is absent, including for `tpod help`. A command that cannot
  read the managed config has nothing useful to offer, and recovery-core
  validation runs `tpod help` after installing the reader, so the check has
  something real to assert.
- A partial apply that writes `~/.local/bin/terrapod` without
  `~/.local/lib/terrapod/config-toml.sh` breaks `tpod` until the apply is
  repeated. Both files come from the same chezmoi apply and the same
  recovery-core step, so no ordinary path produces that state.
- The installer keeps ignoring `TERRAPOD_CHEZMOI_CONFIG` end to end, and that
  is now a documented divergence at one call site rather than a difference
  hidden in a duplicated function.
- Adding a config key still means editing `managed_setup_keys` once. Before
  this change it meant editing two copies that could disagree.
