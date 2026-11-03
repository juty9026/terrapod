# Load install warnings by a chosen rule per entry point

Every consumer of `install-warnings.sh` loads it by one of three rules, chosen
for that entry point rather than by habit. No rule can degrade quietly: the
library is either compiled in, or its absence stops the script.

Always-run chezmoi scripts inline the library with `{{ include }}`. Compile-time
inlining is the default because it is the only rule under which the library
cannot be missing, so no guard is needed and none can be reintroduced.

Ubuntu bootstrap and Jetendard font installation each use one always-run entry
point, in the before and after phases respectively. Both inline the warning
library and exit policy under this default rule. Their former onchange/retry
pairs could invoke the same installation twice within one apply, so each entry
point now makes one decision: install when its installation-source fingerprint
has no matching successful record, or when its warning marker exists.

The fingerprint deliberately includes the raw entry-point source and shared
install body, plus Ubuntu release inputs or the Jetendard font helper. It does
not include expanded warning, policy or Jetendard guidance bytes. Inlining those
libraries therefore cannot turn maintenance edits into APT/sudo work or font
release lookups and downloads. Upstream font releases alone remain outside the
trigger boundary in ADR 0009.

Successful fingerprints are atomically published per category under
`${XDG_STATE_HOME:-$HOME/.local/state}/terrapod/install-sources/`, outside the
warning directory owned by ADR 0014/0018. Missing or invalid records trigger one
attempt, including on migration from old chezmoi onchange state. The shared
bodies terminate through the policy layer, so the coordinator runs each in its
own shell and checks both its exit status and the authoritative warning lookup.
A recorded failure's exit 0, an interruption, or a failed warning clear never
advances successful state. A successful-state write failure records a category
warning through the same exit policy; inability to write that warning is fatal.
Subsequent unchanged, warning-free applies do no installation work.

Any future `run_onchange_` script that records install warnings sources the
library and policy by path, unguarded, under `set -e`, so unrelated warning
library edits do not enter chezmoi's rendered-content hash.

`tpod` resolves the library lazily at each call site. A decision made once at
process start is always wrong during the first-run apply, because the library
target is created by that very apply.

`install.sh` cannot use `{{ include }}` — it is a `curl | sh` bootstrap, not a
chezmoi template. It loads the library once from the checkout and treats absence
as fatal, since a checkout missing the library is a broken clone.

`TERRAPOD_INSTALL_WARNINGS_LOADED` is a load memo for `tpod` and nothing else.
It never again decides whether warnings work.

A script records a warning through one of two call shapes, chosen by whether it
needs the exit policy. A script that must record a warning, then exit 0 because
the user was already told, inlines `install-warning-script.sh` and calls its
`mark_install_warning` / `install_warning_recorded` / `exit_after_install_warning`
trio. A script that only writes or clears a marker, with no exit decision to
make, calls `terrapod_install_warning_write` or `terrapod_install_warning_clear`
directly and does not inline the policy layer.

ADR 0022 partially supersedes this call-shape rule for exit-deciding installer
scripts; the loading rules in this record remain unchanged.

## Considered Options

- Keep the onchange/retry pairs and suppress markers written in the same
  apply: rejected because it retains two scheduling decisions and still needs
  to coordinate changed-source installs with pre-existing warnings. A single
  always-run entry point expresses both triggers together.
- Inline the warning library into the former onchange installers: previously
  rejected because every library edit would repeat expensive work. The explicit
  installation-source fingerprint now makes inlining safe without widening the
  trigger boundary.
- Move `prune_retired_install_warnings` after the apply delegation so the
  first-run prune works: rejected because ADR 0014 fixes that ordering so the
  directory stays consistent when apply fails, and reversing it on unrelated
  grounds is not this change's business.
- Give `tpod` a `chezmoi source-path` fallback: rejected as machinery for a
  state no code path produces. `tpod` already resolves the library relative to
  its own directory, which covers running from a checkout; the only remaining
  gap needs markers written by an apply that never wrote the library.

## Consequences

- The first-run prune remains a no-op, because it runs before the apply that
  installs the library. A fresh machine has no markers to prune, so this costs
  nothing. It is not a bug.
- `mark_install_warning` never returns non-zero. Callers run under `set -e`, so
  a failing bare call would kill the script before its exit policy runs. Code
  that needs the outcome calls `install_warning_recorded`.
- Editing warning/policy/guidance libraries does not repeat Ubuntu or Jetendard
  installation. Real consecutive-apply tests enforce their trigger matrix;
  loading-rule assertions enforce inlining for always-run scripts.
- The first apply after migration attempts each applicable category once,
  because old chezmoi exit-0 state cannot establish installation success.
- The guarantee is one body invocation per category within an apply; independently
  concurrent applies are not coordinated.
- `install-warning-script.sh` deploys to `~/.local/lib/terrapod/` even though
  only chezmoi scripts use it, following `homebrew-core-bundle.sh`. Making it
  source-tree-only would need an unconditional `.chezmoiignore` entry, a shape
  this repo does not otherwise use.
- `run_before_10` no longer has a fallback branch that runs a bare `brew bundle`
  when `homebrew-core-bundle.sh` fails to load. That branch existed only for a
  state inlining makes impossible.
