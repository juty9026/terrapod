# Install Warnings Loading Design

## Goal

Give every consumer of `install-warnings.sh` a loading rule chosen on purpose, so
that a missing library can no longer silently disable warning recording, warning
reporting, or the retry path.

Today eleven entry points load the library four different ways, and three of
those ways fail silently. Afterwards there are three ways, each with a stated
reason, and none of them can degrade quietly: the library is either compiled in,
or its absence stops the script.

This resolves issue #153.

## Current State

**One-shot decision at process start.** `dot_local/bin/executable_terrapod:25-27`
tests `~/.local/lib/terrapod/install-warnings.sh` once and records the result in
`TERRAPOD_INSTALL_WARNINGS_LOADED`. Four call sites (`:1244`, `:1519`, `:1660`,
`:1685`) read that variable and return early when it is not `1`.

During the first-run apply this decision is always wrong.
`install.sh:1103-1121` copies `terrapod` into `~/.local/bin` and links `tpod`
beside it, then `install.sh:1260` runs `tpod apply`. The library target under
`~/.local/lib` is created by that very apply, so at the moment `tpod` starts it
does not exist. `prune_retired_install_warnings` (`:1519`) and
`print_remaining_install_warnings` (`:1685`) no-op for the whole run — the run
most likely to produce warnings.

**Guarded source by source-directory path.** Five chezmoi scripts —
`run_before_10:17-21`, `run_after_20:8-12`, `run_onchange_before_00:5-9`,
`run_before_30:7-11`, `run_before_60:8-12` — source the library behind
`if [ -f … ]`. When the file is absent every warning write becomes a no-op and
`exit_after_install_warning` exits 1 with no marker and no message: the script
fails and says nothing about why.

Each of the five also carries its own 30-40-line copy of the
`mark_install_warning` / `clear_install_warning` / `exit_after_install_warning`
trio, and the copies have drifted. `run_before_10:31-44` returns a status;
`run_after_20:16-26` does not.

Issue #153 lists six such scripts, naming `run_onchange_before_30` and
`run_before_31`. #187 (`bd1a784`) merged those two into `run_before_30`, so the
count is five.

**Guarded source that exits 0.** `run_before_01:5-9` is a sixth variant: when the
library is missing it exits 0, reporting success for a retry it never attempted.

**Unguarded source under `set -u` without `set -e`.** The three Jetendard scripts
— `run_after_70:7`, `run_before_02:7`, `run_onchange_after_65:8` — source the
library with no guard, but execution continues past a failed `.`. In
`run_after_70` a resulting 127 makes `|| exit 1` fire after the settings were
applied successfully. In `run_before_02:9` the undefined
`terrapod_install_warning_existing_path` returns 127, which reads as "no marker"
and silently disables the retry path.

**Bootstrap loaders.** `install.sh` repeats the guarded-source pattern three
times: `mark_install_warning_from_source:986`,
`clear_install_warning_from_source:1003`, and
`load_install_warnings_from_source:1018`. The first two are defined and never
called. The third is called from `snapshot_install_warnings_from_source:1035`
with `|| return 0` and from `install_warning_markers_changed_since_snapshot:1052`
with `|| return 1`.

## Layers

The library splits in two.

**`dot_local/lib/terrapod/install-warnings.sh` — marker API.** Unchanged. It
defines functions and sets no policy.

**`dot_local/lib/terrapod/install-warning-script.sh` — installer policy.** New.
It holds the `INSTALL_WARNING_RECORDED` flag and the four functions that read it.

Splitting rather than merging keeps `exit`-calling functions and mutable global
state out of the file `tpod` sources into its own runtime namespace.

Both files deploy to `~/.local/lib/terrapod/` with no `.chezmoiignore` entry.
`homebrew-core-bundle.sh` is the precedent: it is inlined into `run_before_10`
and nothing else, and it still deploys. (`ubuntu-bootstrap.sh` is *not* a
precedent for source-tree-only libraries — `.chezmoiignore:29-32` scopes it to
Linux, so Ubuntu machines do receive it.) Inventing an unconditional-ignore shape
to save twenty lines in the home directory is not worth a new pattern.

## Recording Contract

`mark_install_warning` becomes total. It never returns non-zero, and every
decision reads the flag:

```sh
INSTALL_WARNING_RECORDED=0

mark_install_warning() {
  category="$1"
  summary="$2"
  guidance="$3"
  INSTALL_WARNING_RECORDED=0

  if terrapod_install_warning_write "$category" "$summary" "$guidance"; then
    INSTALL_WARNING_RECORDED=1
  fi

  return 0
}

install_warning_recorded() {
  [ "$INSTALL_WARNING_RECORDED" -eq 1 ]
}

exit_after_install_warning() {
  if install_warning_recorded; then
    exit 0
  fi

  exit 1
}

continue_after_core_install_warning() {
  if ! install_warning_recorded; then
    exit 1
  fi

  return 0
}

clear_install_warning() {
  terrapod_install_warning_clear "$1" || true
}
```

Totality is required, not stylistic. Every caller runs under `set -e` and writes

```sh
mark_install_warning \
  homebrew-core \
  "…" \
  "…"
exit_after_install_warning
```

Promoting `run_before_10`'s status-returning variant to the shared helper would
make `set -e` kill the script on the `mark_install_warning` statement itself, so
`exit_after_install_warning` would never run and the user would get an exit code
with no explanation. Making the recorder total and the policy explicit removes
that trap and resolves the drift between the five copies.

`run_before_60:48` is the one call site that reads the old return value. It
becomes `mark_install_warning …` followed by `if ! install_warning_recorded`.

`continue_after_core_install_warning` moves into the shared helper even though
only `run_before_10` calls it: it is policy over the same flag, and leaving it
behind would recreate the divergence this design removes.

`shell_integrations_warning_exists` stays in `run_before_30`. It is bound to one
category, not to the flag, and once the guard is gone it reduces to
`terrapod_install_warning_existing_path shell-integrations >/dev/null 2>&1`.

## Loading Rules

Three rules, each chosen for a reason. All nine chezmoi scripts run under
`set -eu`.

### Always-run scripts inline both layers

Seven templates replace their loader block with `{{ include }}`, following
`homebrew-prefix.sh`. Compile-time inlining is the strongest rule available: the
library cannot be missing, so no guard is needed and none can be reintroduced.

Four of them use the trio and inline both files: `run_before_10`,
`run_after_20`, `run_before_30`, `run_before_60`.

Three call the marker API directly and inline only `install-warnings.sh`:
`run_before_01`, `run_before_02`, `run_after_70`.

`run_before_10` inlines `homebrew-core-bundle.sh` the same way and drops its
`TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED` guard (`:24`, `:301`). It is the identical
defect in a file this change rewrites; leaving it would put a guarded loader back
next to two inlined ones.

The Jetendard scripts gain `set -e`, which requires one change:
`run_after_70:14-15` captures `$?` from `python3` as a bare command. It becomes

```sh
settings_status=0
python3 "$settings_helper" apply || settings_status="$?"
```

Inlining also removes the failure mode that motivated the missing `set -e`: with
no `.` of an external path left in these scripts, there is no 127 to continue
past.

### The two `run_onchange_` scripts source by path, unguarded

`run_onchange_before_00` and `run_onchange_after_65` keep a path-based source,
with the guard removed and `set -e` in force:

```sh
. "{{ .chezmoi.sourceDir }}/dot_local/lib/terrapod/install-warnings.sh"
```

`run_onchange_before_00` sources `install-warning-script.sh` the same way.
`run_onchange_after_65` needs only the marker API. `run_onchange_after_65` also
moves from `set -u` to `set -eu`, which is what converts its silent 127 into a
stop.

This fixes the defect the issue reports — a missing library stops the script
instead of disabling warnings — without inlining, and inlining is what these two
scripts cannot afford.

Chezmoi triggers `run_onchange_` scripts on rendered-content change. Inlining
would put the library's bytes in the hash, so **any** edit to
`install-warnings.sh` would re-run the Ubuntu bootstrap and the Jetendard font
install on every machine. Neither is a cheap no-op:

- `executable_jetendard-font:228-237` has no installed-state short-circuit.
  `install()` always calls the GitHub releases API and always downloads the full
  `Jetendard-TTF.zip`.
- `ubuntu-bootstrap.sh:30` always runs `apt-get update`, and `:18-26` escalates
  through `sudo` for any non-root user.

Both therefore require network, and one can raise an unexpected `sudo` prompt.
On an offline machine a one-line library edit would manufacture two fresh install
warnings.

The trigger is not hypothetical: `install-warnings.sh` changed seven times
between `4d3a9dd` and `f314327`, and its most common edit — adding a name to
`terrapod_install_warning_categories()` — happens every time a new install step
gains a warning category.

Path-sourcing carries the cost that a third loading rule exists and a future
author could copy the wrong one. The regression test below pins the split, and
ADR 0016 records why it is two rules rather than one.

### `tpod` resolves lazily

Lines 25-27 are deleted and replaced by

```sh
ensure_install_warnings_loaded() {
  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" = "1" ]; then
    return 0
  fi
  if [ ! -f "$install_warnings_lib" ]; then
    return 1
  fi

  . "$install_warnings_lib"
}
```

Each of the four call sites opens with
`if ! ensure_install_warnings_loaded; then return; fi` in place of its
`TERRAPOD_INSTALL_WARNINGS_LOADED` test.

The expanded form is deliberate. Written as
`[ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" = "1" ] && return 0`, a failing test
makes the whole AND-list fail, and `set -eu` at `executable_terrapod:2` kills
`tpod`.

`tpod` needs no source-tree fallback. `install_warnings_lib` is derived from the
command's own directory (`:16`), so running
`./dot_local/bin/executable_terrapod` from a checkout already resolves
`dot_local/lib/terrapod/install-warnings.sh`. The remaining gap — the library
absent from `~/.local/lib` while markers exist in `~/.local/state` — requires an
apply to have written markers without ever writing the library, which no code
path produces.

### `install.sh` loads once and fails loudly

`mark_install_warning_from_source` (`:986`) and
`clear_install_warning_from_source` (`:1003`) are deleted; nothing calls them.
`load_install_warnings_from_source` sources the library unconditionally and is
called once from `main`, immediately after `ensure_first_run_setup` (`:1353`)
where the checkout is guaranteed:

```sh
load_install_warnings_from_source "$source_dir" ||
  fatal "failed to load the install warning library from $source_dir"
```

The `|| return 0` at `:1035` and `|| return 1` at `:1052` are removed with it. A
checkout missing the library now fails the install with a message instead of
silently skipping the snapshot that decides whether the first run reports
warnings.

`install.sh` is a `curl | sh` bootstrap, not a chezmoi template, so `{{ include }}`
is unavailable to it. Its checkout is a fresh clone, so a missing library means a
broken clone, which is a fatal condition rather than one to work around.

## The Sentinel

`TERRAPOD_INSTALL_WARNINGS_LOADED=1` stays in `install-warnings.sh`. What this
design deletes is every *guard* built on it — the sites that treat a non-`1`
value as "warnings are off." The variable survives with one job:
`ensure_install_warnings_loaded` reads it to know whether `tpod` has already
sourced the file. It never again decides whether a feature runs.

## Testing

### Jetendard adapter fixtures need a new seam

`tests/chezmoiignore_test.sh:403-411` renders each Jetendard script and rewrites
the `^warnings_lib=` line with `sed` to point at a stub library. Two of the three
adapters — `run_before_02` and `run_after_70` — are inlined by this design, so
that line disappears, the `sed` silently no-ops, the real library runs, the
stub's action log stays empty, and the assertions at `:421-437` fail.

The fix inserts the stub after the inlined library instead of replacing a path.
Shell function redefinition wins, and the `font_helper=` / `settings_helper=`
lines survive as anchors:

```sh
sed -e "/^font_helper=/r $jetendard_warnings_stub" …
```

`run_onchange_after_65` keeps its `warnings_lib=` line, so the same insertion
approach covers all three adapters uniformly rather than branching per script.

### Fixture source directories gain a file

Both the `{{ include }}` rule and the path-source rule read from the source
directory — one at render time, one at run time. The synthetic source tree built
by `copy_desktop_apply_source_fixture` in `tests/terrapod_command_test.sh:558-575`
must therefore also copy `install-warning-script.sh`. Omitting it now fails the
chezmoi render rather than producing a silently degraded script.

### Deployment assertion

`tests/terrapod_command_test.sh:846-848` asserts that
`.local/lib/terrapod/install-warnings.sh` is a managed target. An assertion for
`.local/lib/terrapod/install-warning-script.sh` joins it. The new file has no
profile scope, so it does not belong in the `macos_only_entries` or
`linux_only_entries` lists of `tests/chezmoiignore_test.sh:238-276`.

### Four regression tests

Each must fail against the current code.

1. With no `~/.local/lib/terrapod/install-warnings.sh` at process start and a
   stub chezmoi that creates it during apply, `tpod apply` reports the remaining
   warning. Today `print_remaining_install_warnings` prints nothing.
2. `run_after_70` rendered and run with `python3` absent from `PATH` records the
   `jetendard-settings` warning and exits 0, rather than exiting 1 from a 127
   after the settings applied.
3. `run_onchange_before_00` and `run_onchange_after_65`, rendered against a
   source directory with no `install-warnings.sh`, exit non-zero. Today
   `run_onchange_before_00` no-ops its warning writes and
   `run_onchange_after_65` treats a 127 as a missing marker.
4. Every rendered template is checked against the split: the seven always-run
   scripts contain a `terrapod_install_warning_write` definition in their body,
   the two `run_onchange_` scripts contain an unguarded `.` of the library path,
   and none of the nine contains an `if [ -f "$install_warnings_lib" ]` guard. A
   cheap text assertion, and the thing that stops a future entry point from
   inventing a fourth rule or putting the library into an onchange hash.

`sh -n` validation of `install-warning-script.sh` is added next to the existing
check at `tests/terrapod_command_test.sh:916`.

## Documentation

**ADR 0016, "Load install warnings by a chosen rule per entry point."** Three
decisions belong on the record:

- Compile-time inlining is the default for chezmoi scripts, because it is the
  only rule that cannot be silently degraded.
- `run_onchange_` scripts are the exception and source by path, unguarded, under
  `set -e`. Inlining would put the library into the rendered-content hash, and
  neither the Ubuntu bootstrap nor the Jetendard font install is cheap enough to
  re-run on every library edit. Any future `run_onchange_` script that records
  install warnings follows this rule.
- `TERRAPOD_INSTALL_WARNINGS_LOADED` is a load memo for `tpod`, never a feature
  gate.

The ADR also records that the first-run prune stays a no-op. ADR 0014 fixes the
prune before the `chezmoi apply` delegation so the directory is consistent even
when apply fails, and that ordering is pinned by a test. A fresh machine has no
markers to prune, so the no-op costs nothing — but writing it down keeps it from
being refiled as a bug.

This design contradicts no existing ADR.

## Out of Scope

`executable_terrapod:29-31` guards `homebrew-prefix.sh` with the same one-shot
`TERRAPOD_HOMEBREW_PREFIX_LOADED` pattern. It is a different library with no
observed symptom — `tpod` reaches its Homebrew paths only after the first apply
has deployed the file — and it gets its own issue.

Moving `prune_retired_install_warnings` after the apply delegation would make the
first-run prune work, but it reverses an ADR 0014 decision on unrelated grounds.
Revisiting that ordering is a separate change with its own ADR.

Giving `executable_jetendard-font` an installed-state short-circuit would remove
the strongest objection to inlining in `run_onchange_after_65`. It is a real
improvement and unrelated to this issue; it gets its own issue rather than
riding along.
