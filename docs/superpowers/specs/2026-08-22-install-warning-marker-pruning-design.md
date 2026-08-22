# Install Warning Marker Pruning Design

## Goal

Make retiring an install warning category self-cleaning. When a category leaves
`terrapod_install_warning_categories()`, the marker files earlier versions wrote
must stop surviving forever on machines that already have them.

`tpod apply` gains one step: it removes marker files whose names no longer
correspond to anything Terrapod knows, and reports what it removed. No
bookkeeping is required when a future category is retired.

This resolves issue #143.

## Current State

`~/.local/state/terrapod/install-warnings/` on the configured workstation holds
a `managed-package-migration` marker written by the code from #135. ADR 0012
retired that category in #137 and removed it from the allowlist, but nothing
removed the file.

`terrapod_install_warning_list()` iterates the allowlist rather than the
directory:

```sh
terrapod_install_warning_list() {
  for category in $(terrapod_install_warning_categories); do
    if terrapod_install_warning_existing_path "$category" >/dev/null; then
      printf '%s\n' "$category"
    fi
  done
}
```

A retired category therefore becomes invisible to `tpod status`, `tpod doctor`,
and every clearing path at the same moment. The file is unreachable by any code
path and survives every future apply. Both commands report the directory as
empty while the file sits there, which sends anyone inspecting the machine down
a false trail.

Two facts about the directory shape the design and are not obvious from the
issue:

- `terrapod_install_warning_legacy_path()` still resolves `optional-ai-cli-tools`
  to the older filename `ai-cli-tools`, and `terrapod_install_warning_read()`
  and `terrapod_install_warning_value()` still read it. That name is live state
  under a name the allowlist does not contain.
- `terrapod_install_warning_write()` stages atomic writes through
  `mktemp "$marker_dir/.$category.XXXXXX"`, so dot-prefixed temporary files
  appear in the same directory while a write is in flight.

A rule that deleted every file not in `terrapod_install_warning_categories()`
would destroy both.

## Ownership Decision

The marker directory is Terrapod-only. The valid names in it are the current
categories, the legacy aliases Terrapod still reads, and in-flight temporary
files. Any other name is unreachable by definition, so apply may delete it.

This is the decision the issue asked to be made deliberately, and it is what
makes the mechanism self-maintaining. It also forecloses one thing: no future
feature may stage a marker file under a name that is not yet a category. A
feature that needs staged state introduces its own state location or lands its
category first.

## Considered Approaches

### Prune unrecognized marker files on apply

Apply walks the directory and removes regular files whose names are not
recognized. This is the selected approach.

It needs no bookkeeping when a category is retired, which matters because
missing exactly that bookkeeping is what produced this issue. It also subsumes
the issue's first request: the known `managed-package-migration` orphan needs no
special-case code, because the general rule already covers it.

Its cost is the ownership constraint above, and the need to enumerate legacy
aliases correctly. Both are addressed below.

### Keep a list of retired category names

A `terrapod_install_warning_retired_categories()` list would name
`managed-package-migration` and grow by one entry each retirement. Apply would
delete only those names.

Rejected. It leaves the directory open to unknown names, which is not a property
this project needs, and in exchange it requires a person to remember a second
edit at retirement time. The failure mode of forgetting that edit is silent and
indefinite, which is exactly this issue. A mechanism whose correctness depends
on remembering the thing that was already forgotten is not a fix.

### Delete only files matching the marker format

Apply would parse each unrecognized file and delete it only if it carried the
four expected keys, leaving anything else for `tpod doctor` to report.

Rejected. It adds a parser and a second reporting path to protect files that,
under the ownership decision, should not exist. The directory is not a shared
namespace.

## Prune Rule

For each entry directly inside `terrapod_install_warning_dir()`:

| Entry | Action | Reason |
| --- | --- | --- |
| A name in `terrapod_install_warning_categories()` | keep | current category |
| A legacy alias such as `ai-cli-tools` | keep | still read by `terrapod_install_warning_read()` |
| A dot-prefixed name such as `.homebrew-core.aB3xY9` | keep | `mktemp` staging file for an in-flight write |
| A directory or other non-regular file | keep | not a marker; narrows the blast radius to files |
| Any other regular file | **remove** | unreachable retired marker |

"Regular file" is the `[ -f ]` test, which follows symbolic links. A symlink
pointing at a regular file under an unrecognized name is removed; the link is
unreachable state under the ownership decision like any other such name.

Dot-prefixed files are protected by POSIX glob semantics: `"$marker_dir"/*` does
not match a leading dot. This is a deliberate dependency rather than an
accident, so it is stated here and pinned by a test. An implementation that
switches to `find` or `ls -a` must reintroduce the exclusion explicitly.

Legacy aliases are derived from the existing mapping rather than restated. A
second `case` statement listing alias names would split the mapping across two
places, and the next alias added to one and not the other reproduces this
issue's failure mode in a new form.

```sh
terrapod_install_warning_known_names() {
  for category in $(terrapod_install_warning_categories); do
    printf '%s\n' "$category"
    legacy_path="$(terrapod_install_warning_legacy_path "$category" 2>/dev/null)" || continue
    printf '%s\n' "${legacy_path##*/}"
  done
}
```

## Library Interface

`dot_local/lib/terrapod/install-warnings.sh` gains
`terrapod_install_warning_known_names()` above and the prune itself.

```sh
terrapod_install_warning_prune() {
  marker_dir="$(terrapod_install_warning_dir)"
  [ -d "$marker_dir" ] || return 0
  known="$(terrapod_install_warning_known_names)"
  prune_status=0

  for marker_path in "$marker_dir"/*; do
    [ -f "$marker_path" ] || continue
    name="${marker_path##*/}"
    if printf '%s\n' "$known" | grep -Fx "$name" >/dev/null; then
      continue
    fi

    if rm -f "$marker_path"; then
      printf '%s\n' "$name"
    else
      prune_status=1
    fi
  done

  return "$prune_status"
}
```

The contract separates the two things a caller needs. Standard output carries
the names actually removed, one per line, and nothing else. The exit status is
non-zero only when at least one removal failed. A missing directory is success
with no output.

`known_names` runs inside a command substitution, so its `category` and
`legacy_path` assignments stay in a subshell and do not leak into the caller,
which matters in a POSIX shell library with no `local`.

The recognized-name check is written as an `if` block rather than
`grep … && continue`. `dot_local/bin/executable_terrapod` runs under `set -eu`,
where an `A && B` list whose `A` fails is a failing statement and aborts the
shell. Since `grep` failing is the ordinary case for a file being pruned, the
`&&` form would kill `tpod apply` on the first orphan it found.

## Apply Integration

`run_apply()` in `dot_local/bin/executable_terrapod` calls the prune after
`run_apply_preflight` and before delegating to `chezmoi apply`.

Ordering before the delegation is deliberate. Pruning is hygiene that does not
depend on apply succeeding, and the failure branch of `run_chezmoi_command
apply` returns early. Running first means the directory is consistent even on
that branch.

`tpod apply` is the only apply entry point that needs the call. `tpod update`
ends in `exec "$installed_tpod" apply`, and the first-run installer invokes
`TERRAPOD_FIRST_RUN_APPLY=1 "$tpod_bin" apply`. A `.chezmoiscripts` entry would
additionally cover a bare `tpod chezmoi -- apply`, at the cost of a new template
file, another copy of the library-loading boilerplate, and an OS gate on an
OS-independent operation. That escape hatch catches up on the next `tpod apply`.

Output appears only when something was removed, between the apply context block
and chezmoi's own output:

```
Terrapod apply
  Profile: ...
  Delegating declared-state apply to: chezmoi apply
Preflight: chezmoi is available
Preflight: config file is readable
Removed retired install warning marker: managed-package-migration
```

The call is guarded the way the command's other marker calls are, so a `tpod`
running without `install-warnings.sh` loaded skips the step silently.

## Error Handling

No prune failure changes the exit status of `tpod apply`. This follows the
existing invariant that apply's exit status reflects the declared-state apply
itself.

- **A removal fails** (permissions, read-only state directory): the names that
  were removed are still reported, and `print_warning_line` adds
  `could not remove some retired install warning markers`. The remaining files
  are retried on the next apply; the operation is idempotent.
- **The directory does not exist**: no work, success. This is the first-run
  path.
- **The library is not loaded**: the step is skipped silently.

Deliberately excluded: backing up removed files, prompting before removal, and
logging removed contents. These files are by definition unreadable by any code
path, and a backup directory would recreate the accumulation this change exists
to stop.

## Testing

Both harnesses already exist in `tests/terrapod_command_test.sh`: the
`marker_home` / `marker_xdg_state` pattern for library behavior and the chezmoi
stub pattern for `tpod apply`.

**Library.** One directory populated with all five cases, one prune run:

| Fixture | Expected |
| --- | --- |
| `managed-package-migration` | removed, name on standard output |
| `homebrew-core` | kept |
| `ai-cli-tools` | kept |
| `.optional-ai-cli-tools.aB3xY9` | kept |
| `subdir/` | kept |

Then `terrapod_install_warning_list` reports `homebrew-core` only, and a prune
against a missing directory exits 0.

The `ai-cli-tools` case is the most important test in this change. It is the one
place where the design can destroy live state, and the issue's original second
option would have failed exactly there.

**Command.** With a stub chezmoi, `tpod apply` removes the orphan, prints
`Removed retired install warning marker: managed-package-migration`, and leaves
a known marker in place. A second run with a failing chezmoi stub confirms the
prune still completed, pinning the decision to run before delegation.

`sh -n` validation of the library already runs, so the new functions are covered
for POSIX syntax without a new test.

## Documentation

**ADR 0014, "Prune unrecognized install warning markers on apply."** The
substance of this change is the ownership decision, not the code. #135 and #137
were recorded as ADR 0011 and 0012, and the reasoning for rejecting a retired
name list belongs in the same series.

**CONTEXT.md.** Three invariants are added:

- The install warning directory is Terrapod-owned; the only valid names are
  current categories and the legacy aliases Terrapod still reads.
- `tpod apply` removes marker files whose names are not valid, and reports each
  removal on one line.
- Marker prune failures do not fail `tpod apply`.

One existing invariant is amended. "Terrapod install warning marker writes
should be atomic at the category file level, and marker clears remove only the
matching category file" must scope its second clause to
`terrapod_install_warning_clear()`. Left as written, it reads as a flat
prohibition on the new behavior. The related invariant that markers remain until
their category completes successfully needs the qualifier "while that category
exists" for the same reason: without it, the next reader can mistake the prune
for a violation and revert it.

## Out of Scope

`tpod status` and `tpod doctor` stay read-only and unchanged. They report the
directory as empty today and will report it as empty after the prune, so a
single apply removes the disagreement the issue describes.

The three executable selection advisories on the same machine are working as
designed under ADR 0012. The `python` advisory's downstream consequence is
tracked in #142.
