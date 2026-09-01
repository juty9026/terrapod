# Load install warnings by a chosen rule per entry point

Every consumer of `install-warnings.sh` loads it by one of three rules, chosen
for that entry point rather than by habit. No rule can degrade quietly: the
library is either compiled in, or its absence stops the script.

Always-run chezmoi scripts inline the library with `{{ include }}`. Compile-time
inlining is the default because it is the only rule under which the library
cannot be missing, so no guard is needed and none can be reintroduced.

The two `run_onchange_` scripts are the exception. They source the library by
path, unguarded, under `set -e`. Chezmoi triggers `run_onchange_` scripts on
rendered-content change, so inlining would put the library's bytes into the
hash and re-run the work on every library edit — and neither piece of work is
cheap. `install()` in `executable_jetendard-font` has no installed-state
short-circuit: it always calls the GitHub releases API and downloads the full
archive. `ubuntu-bootstrap.sh` always runs `apt-get update` and escalates
through `sudo` for a non-root user. Both need network; one can raise an
unexpected password prompt. Any future `run_onchange_` script that records
install warnings follows this rule.

`tpod` resolves the library lazily at each call site. A decision made once at
process start is always wrong during the first-run apply, because the library
target is created by that very apply.

`install.sh` cannot use `{{ include }}` — it is a `curl | sh` bootstrap, not a
chezmoi template. It loads the library once from the checkout and treats absence
as fatal, since a checkout missing the library is a broken clone.

`TERRAPOD_INSTALL_WARNINGS_LOADED` is a load memo for `tpod` and nothing else.
It never again decides whether warnings work.

## Considered Options

- Inline everywhere, including the two `run_onchange_` scripts: rejected on the
  cost above. `install-warnings.sh` changed seven times between `4d3a9dd` and
  `f314327`, and its most common edit — a new name in
  `terrapod_install_warning_categories()` — happens whenever a new install step
  gains a warning category. Paying an `apt` run and a font download on every
  machine for that is a worse trade than one documented exception.
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
- Editing `install-warnings.sh` does not re-run any `run_onchange_` script. A
  future author who inlines it into one silently reintroduces that cost; the
  loading-rule assertions in `tests/chezmoiignore_test.sh` fail if they do.
- `install-warning-script.sh` deploys to `~/.local/lib/terrapod/` even though
  only chezmoi scripts use it, following `homebrew-core-bundle.sh`. Making it
  source-tree-only would need an unconditional `.chezmoiignore` entry, a shape
  this repo does not otherwise use.
- `run_before_10` no longer has a fallback branch that runs a bare `brew bundle`
  when `homebrew-core-bundle.sh` fails to load. That branch existed only for a
  state inlining makes impossible.
