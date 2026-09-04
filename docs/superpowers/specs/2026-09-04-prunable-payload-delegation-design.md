# Prunable payload delegation design

Design for issue #246. Replaces Terrapod's own unreachable-payload computation
with the Development Runtime Manager's, and makes the resulting advisory
actionable by listing the payloads and naming the command that reclaims them.

## Problem

#238 restored the information that pre-ADR-0010 payloads still occupy disk, as
one summary line:

```
  advisory - 18 mise payloads are outside any canonical declaration
             'mise uninstall' reclaims the disk; Terrapod does not remove them.
```

The line names a command but not what to run it on. A user who wants to act on
it has to reconstruct `leftover_payload_count()` by hand before they know a
single payload name.

Investigating that gap surfaced a larger defect: **the count is wrong, and it is
wrong in the direction that makes the missing list dangerous to supply.**

### The count over-reports

`leftover_payload_count()` walks `$MISE_DATA_DIR/installs/*` and calls a payload
reachable when a `mise bin-paths` entry lies beneath it. `mise bin-paths` reports
only the toolset resolved from the directory `tpod` runs in. Every version a
project-local `mise.toml` selects is therefore counted as unreachable.

Measured on the macOS workstation in the issue:

| payload | Terrapod's verdict | reality |
| --- | --- | --- |
| `ruby` 151.7 MB | outside any declaration | 6 live project configs declare it |
| `supabase` 108.6 MB | outside any declaration | 6 live project configs declare it |
| `flyctl` 104.8 MB | outside any declaration | 6 live project configs declare it |
| `pnpm` 61.2 MB | outside any declaration | 1 live project config declares it |

Roughly 426 MB of the issue's 620.3 MB is in active use. While the check prints
only a number this is over-reporting. The moment it prints a list and an
uninstall command, the over-report becomes advice that breaks working projects.

### The count is also blind at the wrong granularity

The walk is per top-level directory, so `installs/node` counts as reachable in
full when one of its five versions is selected. Four stale `node` versions are
invisible. The issue records this as unresolved.

### The directory name is not the tool name

`mise uninstall` takes `TOOL@VERSION`. Install directories are slugs:
`installs/npm-pnpm` is the tool `npm:pnpm`, `installs/aqua-sharkdp-bat` is
`aqua:sharkdp/bat`. A command built from directory names does not run. The walk
would also have to normalize alias symlinks — `installs/node/24` points at
`./24.18.0`, and `mise bin-paths` reports the alias path, not the real one.

### mise already computes this, correctly

`mise ls --prunable` judges per version against
`~/.local/state/mise/tracked-configs`, which records every `mise.toml` mise has
seen. It reports real tool names, resolves alias symlinks, and excludes versions
a project declares. On the same workstation it reports **20 payloads, 769.1 MB**
— larger than the issue's figure and safe to reclaim, because the 426 MB in use
drops out and the stale `node` versions come in.

## Decision

Delegate the verdict to mise. Terrapod reports the finding and names the
recovery command; it computes neither the set nor the removal.

This narrows ADR 0020's definition of the leftover-payload finding. ADR 0012's
non-destructive apply contract is untouched: Terrapod still removes nothing.

### Part 2 closes here

The issue proposes taking removal through its own ADR. It does not need one.
What a user cannot reconstruct is the exact `TOOL@VERSION` set; naming the
command that acts on it is what ADR 0020 already permits for a provider
established by location. Terrapod executing removal stays rejected by ADR 0012,
and this design does not reopen it.

## Verdict source

    mise -C "$HOME" ls --prunable --no-header

Emits one `tool<space...>version` line per prunable payload; a linked payload
carries a trailing `(symlink)` field. The count is the line count.

`-C "$HOME"` pins the invocation. The prunable set does not vary with the
working directory on the machines measured, but ADR 0020 already established
that a verdict must not depend on how `tpod` was invoked, and pinning costs
nothing.

Sizes, when asked for, come from `mise where "$tool@$version"` and `du -sk` on
the result. Twenty payloads resolve in 0.53s.

## Output contract

Default `tpod doctor` and `tpod apply` keep #238's one-line shape:

```
  advisory - 20 mise payloads can be pruned
             'mise prune --tools' reclaims the disk; Terrapod does not remove them.
```

Singular stays a separate string, as today: `1 mise payload can be pruned`.

`tpod doctor --payloads` adds the total and the list, in the same indented block:

```
  advisory - 20 mise payloads can be pruned (769.1 MB)
             aqua:zellij-org/zellij@0.44.3   38.9 MB
             node@22.22.2                    61.0 MB
             npm:pnpm@10.33.3                 4.2 MB
             ...
             'mise prune --tools' reclaims the disk; Terrapod does not remove them.
```

Two decisions inside this shape.

**The named command is `mise prune --tools`, not `mise uninstall <list>`.** A
frozen list acts on the set as it was when doctor ran. `mise prune` recomputes
at execution, so a version a project started declaring between the two moments
is not removed. Terrapod never runs it.

**The total is computed only under `--payloads`.** `du` over twenty payloads
costs 0.53s against a 1.1s `tpod doctor`. The default line, which every `apply`
also prints, stays a count.

## Code

`dot_local/lib/terrapod/executable_executable-selection`

- `leftover_payload_count()` becomes `prunable_payloads()`, emitting the raw
  `tool version` lines. The installs walk, the `normalize_path` boundary
  comparison, and the `mise bin-paths` reachability test are deleted.
- `render_leftover_payloads()` takes the detail flag. It counts the lines for
  the summary, and only in detail mode resolves each payload's path and size.
- A fifth positional argument `<show-payloads>` (`true`/`false`) follows the
  existing `<ai-enabled>` and `<launcher-enabled>` arguments, and the usage
  string grows with it.

`dot_local/bin/executable_terrapod`

- `run_doctor()` accepts `--payloads` and rejects every other argument through
  the existing `fail_usage`.
- `run_executable_selection()` takes and forwards the flag; `status` and `apply`
  pass `false`.
- `show_help()` gains `tpod doctor [--payloads]` in Usage and an entry under
  Options.

## Boundaries

- **mise absent, or the installs directory absent** — skipped, as today.
- **A mise too old for `--prunable`** — the advisory is skipped entirely. No
  fallback to the deleted computation: this check exists to ask mise what is
  reachable, and carrying two definitions of that is the complexity the change
  removes.
- **A payload symlinked outside the data directory**, such as `rust/stable`
  pointing at `~/.cargo/bin` — listed, and `du -sk` measures the symlink rather
  than the target, so the target's disk is not attributed to mise.
- **Readiness** — unchanged. The finding does not set the issue flag, does not
  affect `tpod status`, and does not change any exit status.

## Tests

`tests/executable_selection_test.sh`. The stand-in mise grows `ls` and `where`
cases answering from fixture files, matching how `bin-paths` and `which` already
work.

- The prunable list's line count is the number the summary prints.
- `--payloads` lists exactly the payloads the count covers, and no others.
- Default `doctor` output is two lines and names no payload.
- A `mise ls --prunable` that exits non-zero produces no advisory and no
  failure.
- mise absent and installs absent keep their existing assertions.
- `tpod status` stays ready on a machine whose only finding is prunable
  payloads.
- A linked payload appears in the list without its target's size.

`tests/terrapod_*_test.sh` covers `--payloads` argument parsing and the
rejection of any other `doctor` argument.

The `installs/node` versus `installs/node-canary` prefix test is deleted with
the code it guards. The issue's acceptance criteria name that case, but the
requirement rests on Terrapod keeping its own string comparison; no comparison
remains to protect.

## Documentation

- **ADR 0021**, `docs/adr/0021-delegate-unreachable-payload-detection-to-mise.md`.
  Supersedes ADR 0020's definition of the leftover-payload finding and its
  advisory wording. Records the supersession on ADR 0020 as well, per the
  convention in 1d3970e. States that ADR 0012's non-destructive apply contract
  remains in force.
- **CONTEXT.md**, the two sentences covering this finding: the one ending
  "never when the provider would have to be inferred", and the one beginning
  "**Development Runtime Manager** payloads that no active declaration can
  reach".
