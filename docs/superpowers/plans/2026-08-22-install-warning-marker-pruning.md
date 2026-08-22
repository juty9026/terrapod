# Install Warning Marker Pruning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `tpod apply` delete install warning marker files whose names no longer correspond to any category Terrapod knows, so retiring a category stops leaving a dead file on every machine forever.

**Architecture:** Two new functions in the shared POSIX shell library `dot_local/lib/terrapod/install-warnings.sh` — one that derives the set of recognized filenames from the existing category list and legacy-alias mapping, one that walks the marker directory and removes everything else. `run_apply()` in the `tpod` command calls the prune before delegating to `chezmoi apply` and reports each removal on one line. Failures never change apply's exit status.

**Tech Stack:** POSIX `sh` (the library must stay `sh -n` clean and must not use `local`), chezmoi templates, the repo's hand-rolled test harness in `tests/terrapod_command_test.sh`.

**Spec:** `docs/superpowers/specs/2026-08-22-install-warning-marker-pruning-design.md`

## Global Constraints

- **POSIX `sh` only.** No `local`, no bashisms, no arrays. `dot_local/lib/terrapod/install-warnings.sh` and `dot_local/bin/executable_terrapod` are both validated by `sh -n` in the test suite.
- **`dot_local/bin/executable_terrapod` runs under `set -eu`.** Never write `cmd && continue` or `cmd && something` as a bare statement where `cmd` failing is an expected case — a failing `A && B` list aborts the shell. Use an `if` block.
- **No `local` means no variable scoping.** Helper functions invoked through command substitution (`x="$(f)"`) run in a subshell and are safe. Helpers called directly share the caller's variables.
- **Marker directory resolution always goes through `terrapod_install_warning_dir()`**, which honours `XDG_STATE_HOME` and falls back to `$HOME/.local/state`. Never hardcode a path.
- **Recognized filenames are:** the eight names from `terrapod_install_warning_categories()` (`homebrew-core`, `homebrew-desktop-apps`, `ubuntu-bootstrap`, `shell-integrations`, `mise-tools`, `optional-ai-cli-tools`, `jetendard-font`, `jetendard-settings`), plus every legacy alias produced by `terrapod_install_warning_legacy_path()` (currently only `ai-cli-tools`, for `optional-ai-cli-tools`).
- **Dot-prefixed files must survive.** `terrapod_install_warning_write()` stages atomic writes via `mktemp "$marker_dir/.$category.XXXXXX"`. Protection comes from POSIX glob semantics — `"$dir"/*` does not match a leading dot. Do not switch to `find` or `ls -a`.
- **Removal output text is exactly:** `Removed retired install warning marker: <name>`
- **Prune failure warning text is exactly:** `could not remove some retired install warning markers`
- **Run the full suite with:** `sh tests/terrapod_command_test.sh` — it exits 0 and prints `ok - ...` lines. Baseline before this work: 483 passing assertions, exit 0.
- **Commit style:** imperative subject, no `feat:`/`fix:` prefixes (see `git log`), and every commit ends with:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  ```

---

### Task 1: Prune functions in the shared library

Adds the two library functions and their unit tests. Nothing calls them yet, so this task is reviewable purely as library behavior.

**Files:**
- Modify: `dot_local/lib/terrapod/install-warnings.sh` — insert between `terrapod_install_warning_clear()` (ends line 155) and `terrapod_install_warning_list()` (starts line 157)
- Test: `tests/terrapod_command_test.sh` — insert after line 1099, the line reading `pass "install warning marker write stores Optional AI CLI warnings only under the stable category slug"`, and before line 1101 `fake_warning_bin="$tmp_dir/fake-warning-bin"`

**Interfaces:**
- Consumes: existing `terrapod_install_warning_dir()`, `terrapod_install_warning_categories()`, `terrapod_install_warning_legacy_path()` from the same file.
- Produces:
  - `terrapod_install_warning_known_names()` — no arguments. Prints every recognized marker filename, one per line, in category order with each category immediately followed by its legacy alias when one exists. Always returns 0.
  - `terrapod_install_warning_prune()` — no arguments. Prints the basename of each marker file it removed, one per line, and nothing else. Returns 0 normally, 1 if at least one `rm` failed. Returns 0 with no output when the marker directory does not exist. Task 2 depends on exactly this contract.

- [ ] **Step 1: Write the failing tests**

Insert this block into `tests/terrapod_command_test.sh` immediately after line 1099 (`pass "install warning marker write stores Optional AI CLI warnings only under the stable category slug"`):

```sh
prune_marker_home="$tmp_dir/prune-marker-home"
prune_marker_state="$tmp_dir/prune-marker-state"
prune_marker_dir="$prune_marker_state/terrapod/install-warnings"
mkdir -p "$prune_marker_home" "$prune_marker_dir/subdir"

printf '%s\n' "category='managed-package-migration'" >"$prune_marker_dir/managed-package-migration"
printf '%s\n' "category='homebrew-core'" >"$prune_marker_dir/homebrew-core"
printf '%s\n' "category='optional-ai-cli-tools'" >"$prune_marker_dir/ai-cli-tools"
printf '%s\n' "category='homebrew-core'" >"$prune_marker_dir/.homebrew-core.aB3xY9"

prune_removed="$(
  HOME="$prune_marker_home" XDG_STATE_HOME="$prune_marker_state" sh -c '. "$1"; terrapod_install_warning_prune' sh "$install_warnings_lib"
)"

if [ "$prune_removed" != "managed-package-migration" ]; then
  printf '%s\n' "expected pruned names: managed-package-migration" >&2
  printf '%s\n' "actual pruned names:" >&2
  printf '%s\n' "$prune_removed" | sed 's/^/  /' >&2
  fail "install warning marker prune removes and reports only unrecognized marker files"
fi
pass "install warning marker prune removes and reports only unrecognized marker files"

if [ -e "$prune_marker_dir/managed-package-migration" ]; then
  fail "install warning marker prune deletes retired category marker files"
fi
pass "install warning marker prune deletes retired category marker files"

if [ ! -f "$prune_marker_dir/homebrew-core" ]; then
  fail "install warning marker prune keeps current category marker files"
fi
pass "install warning marker prune keeps current category marker files"

if [ ! -f "$prune_marker_dir/ai-cli-tools" ]; then
  fail "install warning marker prune keeps legacy alias marker files"
fi
pass "install warning marker prune keeps legacy alias marker files"

if [ ! -f "$prune_marker_dir/.homebrew-core.aB3xY9" ]; then
  fail "install warning marker prune keeps in-flight mktemp staging files"
fi
pass "install warning marker prune keeps in-flight mktemp staging files"

if [ ! -d "$prune_marker_dir/subdir" ]; then
  fail "install warning marker prune keeps directories"
fi
pass "install warning marker prune keeps directories"

prune_remaining_list="$(
  HOME="$prune_marker_home" XDG_STATE_HOME="$prune_marker_state" sh -c '. "$1"; terrapod_install_warning_list' sh "$install_warnings_lib"
)"
expected_prune_remaining_list="$(printf '%s\n' homebrew-core optional-ai-cli-tools)"

if [ "$prune_remaining_list" != "$expected_prune_remaining_list" ]; then
  printf '%s\n' "expected remaining categories:" >&2
  printf '%s\n' "$expected_prune_remaining_list" | sed 's/^/  /' >&2
  printf '%s\n' "actual remaining categories:" >&2
  printf '%s\n' "$prune_remaining_list" | sed 's/^/  /' >&2
  fail "install warning marker prune leaves current and legacy categories readable"
fi
pass "install warning marker prune leaves current and legacy categories readable"

prune_missing_state="$tmp_dir/prune-missing-state"
if ! HOME="$prune_marker_home" XDG_STATE_HOME="$prune_missing_state" \
  sh -c '. "$1"; terrapod_install_warning_prune' sh "$install_warnings_lib" >"$tmp_dir/prune-missing.out"; then
  fail "install warning marker prune succeeds when the marker directory is missing"
fi

if [ -s "$tmp_dir/prune-missing.out" ]; then
  fail "install warning marker prune prints nothing when the marker directory is missing"
fi
pass "install warning marker prune succeeds quietly when the marker directory is missing"

if [ "$(id -u)" = 0 ]; then
  pass "install warning marker prune reports removal failures (skipped as root)"
else
  prune_locked_state="$tmp_dir/prune-locked-state"
  prune_locked_dir="$prune_locked_state/terrapod/install-warnings"
  mkdir -p "$prune_locked_dir"
  printf '%s\n' "category='managed-package-migration'" >"$prune_locked_dir/managed-package-migration"
  chmod 555 "$prune_locked_dir"

  prune_locked_status=0
  HOME="$prune_marker_home" XDG_STATE_HOME="$prune_locked_state" \
    sh -c '. "$1"; terrapod_install_warning_prune' sh "$install_warnings_lib" \
    >"$tmp_dir/prune-locked.out" 2>/dev/null || prune_locked_status="$?"
  chmod 755 "$prune_locked_dir"

  if [ "$prune_locked_status" -eq 0 ]; then
    fail "install warning marker prune returns non-zero when a removal fails"
  fi

  if [ -s "$tmp_dir/prune-locked.out" ]; then
    fail "install warning marker prune does not report files it failed to remove"
  fi
  pass "install warning marker prune reports removal failures through its exit status"
fi
```

The last block is skipped under `root`, where a read-only directory does not
stop a removal. Every other case runs unconditionally.

Why the `prune_remaining_list` assertion matters: it proves the legacy alias is not merely present on disk but still resolvable through `terrapod_install_warning_list()` after a prune. The `ai-cli-tools` case is the one place this design can destroy live state.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
sh tests/terrapod_command_test.sh
```

Expected: FAIL. The run stops at `not ok - install warning marker prune removes and reports only unrecognized marker files`, because `terrapod_install_warning_prune` does not exist and the command substitution yields an empty string. (`set -eu` is on in the test file, but a command substitution assignment does not abort on the inner command's failure, so the mismatch check is what fires.)

- [ ] **Step 3: Write the implementation**

Insert into `dot_local/lib/terrapod/install-warnings.sh` between `terrapod_install_warning_clear()` and `terrapod_install_warning_list()`:

```sh
terrapod_install_warning_known_names() {
  for category in $(terrapod_install_warning_categories); do
    printf '%s\n' "$category"

    legacy_path="$(terrapod_install_warning_legacy_path "$category" 2>/dev/null)" || continue
    printf '%s\n' "${legacy_path##*/}"
  done
}

terrapod_install_warning_prune() {
  marker_dir="$(terrapod_install_warning_dir)"
  [ -d "$marker_dir" ] || return 0

  known_names="$(terrapod_install_warning_known_names)"
  prune_status=0

  for marker_path in "$marker_dir"/*; do
    [ -f "$marker_path" ] || continue

    marker_name="${marker_path##*/}"
    if printf '%s\n' "$known_names" | grep -Fx "$marker_name" >/dev/null; then
      continue
    fi

    if rm -f "$marker_path"; then
      printf '%s\n' "$marker_name"
    else
      prune_status=1
    fi
  done

  return "$prune_status"
}
```

Three things that are easy to get wrong here:

- The recognized-name check is an `if` block, not `grep … && continue`. Under `set -eu` — which `tpod` uses — a failing `A && B` list aborts the shell, and grep failing is the ordinary case for a file about to be pruned.
- `"$marker_dir"/*` deliberately does not match dot-prefixed files. That is the whole protection for in-flight `mktemp` staging files. The `[ -f "$marker_path" ] || continue` guard also handles the unmatched-glob case, where the literal `.../\*` string is not a file.
- Derive aliases from `terrapod_install_warning_legacy_path()`. Do not add a second `case` statement listing alias names — splitting that mapping across two places recreates the exact bug this change fixes.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
sh tests/terrapod_command_test.sh
```

Expected: exit 0, with the eight new `ok -` lines present and the pre-existing 483 assertions still passing.

- [ ] **Step 5: Commit**

```bash
git add dot_local/lib/terrapod/install-warnings.sh tests/terrapod_command_test.sh
git commit -F - <<'EOF'
Add install warning marker pruning to the shared library

terrapod_install_warning_prune removes marker files whose names are not
current categories or legacy aliases. Recognized names are derived from
the existing category list and legacy path mapping so retiring a category
needs no second edit.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 2: Call the prune from `tpod apply`

Wires the library function into the only apply entry point and gives it user-visible output. `tpod update` ends in `exec "$installed_tpod" apply` and the first-run installer calls `TERRAPOD_FIRST_RUN_APPLY=1 "$tpod_bin" apply`, so this one call site covers every real apply path.

**Files:**
- Modify: `dot_local/bin/executable_terrapod` — add a helper immediately before `run_apply()` (line 1498) and one call inside `run_apply()` after `run_apply_preflight "$config_file"`
- Test: `tests/terrapod_command_test.sh` — two insertions described in Step 1

**Interfaces:**
- Consumes: `terrapod_install_warning_prune()` from Task 1 (no arguments; removed names on stdout one per line; exit 1 if any removal failed). Also the existing `print_warning_line "$message"` helper at line 122 and the existing `TERRAPOD_INSTALL_WARNINGS_LOADED` guard convention used by `print_remaining_install_warnings()` at line 1644.
- Produces: nothing consumed by later tasks. Task 3 documents this behavior but does not call it.

- [ ] **Step 1: Write the failing tests**

Two insertions.

**1a.** In the successful-apply block, the existing setup at lines 3137-3140 writes a `mise-tools` marker into `$apply_marker_state` before running apply. Add the orphan and a control file right after that `sh -c` invocation ends (after the line `sh "$install_warnings_lib"` at line 3140, before the `if ! HOME="$diff_home" ...` at line 3142):

```sh
apply_marker_dir="$apply_marker_state/terrapod/install-warnings"
printf '%s\n' "category='managed-package-migration'" >"$apply_marker_dir/managed-package-migration"
printf '%s\n' "category='optional-ai-cli-tools'" >"$apply_marker_dir/ai-cli-tools"
```

Then, after the existing `apply_output="$(cat "$tmp_dir/apply.out")"` assignment (line 3151), add:

```sh
assert_line \
  "$apply_output" \
  "Removed retired install warning marker: managed-package-migration" \
  "Terrapod apply reports each retired install warning marker it removed"

if [ -e "$apply_marker_dir/managed-package-migration" ]; then
  fail "Terrapod apply removes retired install warning marker files"
fi
pass "Terrapod apply removes retired install warning marker files"

if [ ! -f "$apply_marker_dir/mise-tools" ]; then
  fail "Terrapod apply keeps current install warning marker files"
fi
pass "Terrapod apply keeps current install warning marker files"

if [ ! -f "$apply_marker_dir/ai-cli-tools" ]; then
  fail "Terrapod apply keeps legacy alias install warning marker files"
fi
pass "Terrapod apply keeps legacy alias install warning marker files"
```

**1b.** In the failing-apply block, the stub chezmoi at lines ~3446-3468 exits 91 and `$apply_failure_marker_state` is declared at line 3470. Add the orphan setup right after that declaration and before the `if HOME="$diff_home" ... sh "$terrapod" apply` at line 3472:

```sh
apply_failure_marker_dir="$apply_failure_marker_state/terrapod/install-warnings"
mkdir -p "$apply_failure_marker_dir"
printf '%s\n' "category='managed-package-migration'" >"$apply_failure_marker_dir/managed-package-migration"
```

Then, after the existing `apply_failure_output="$(cat "$tmp_dir/apply-failure.out")"` assignment (line 3477), add:

```sh
if [ -e "$apply_failure_marker_dir/managed-package-migration" ]; then
  fail "Terrapod apply prunes retired install warning markers even when delegated chezmoi apply fails"
fi
pass "Terrapod apply prunes retired install warning markers even when delegated chezmoi apply fails"

assert_line \
  "$apply_failure_output" \
  "Removed retired install warning marker: managed-package-migration" \
  "Terrapod apply reports pruned markers before delegating to chezmoi apply"
```

This second block is the one that pins the ordering decision. If someone later moves the call after `run_chezmoi_command apply`, the early-return failure branch skips it and both of these assertions fail.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
sh tests/terrapod_command_test.sh
```

Expected: FAIL at `not ok - Terrapod apply reports each retired install warning marker it removed`. The orphan file is still on disk and no removal line appears in the output.

- [ ] **Step 3: Write the implementation**

Add this helper to `dot_local/bin/executable_terrapod` immediately before `run_apply()` at line 1498:

```sh
prune_retired_install_warnings() {
  if [ "${TERRAPOD_INSTALL_WARNINGS_LOADED:-}" != "1" ]; then
    return
  fi

  prune_status=0
  removed_markers="$(terrapod_install_warning_prune)" || prune_status=1

  if [ -n "$removed_markers" ]; then
    printf '%s\n' "$removed_markers" | while IFS= read -r removed_marker; do
      printf 'Removed retired install warning marker: %s\n' "$removed_marker"
    done
  fi

  if [ "$prune_status" -ne 0 ]; then
    print_warning_line "could not remove some retired install warning markers"
  fi
}
```

The `|| prune_status=1` is what keeps `set -e` from killing apply when a removal fails, and it also preserves the removed names so they are still reported alongside the warning. The `while` loop reads from a pipeline rather than iterating `$removed_markers` unquoted, so a stray filename containing whitespace or glob characters cannot split or expand.

Then in `run_apply()`, insert one call after the preflight:

```sh
  config_file="$(chezmoi_config_file)"
  show_apply_context "$config_file"
  run_apply_preflight "$config_file"
  prune_retired_install_warnings

  if ! run_chezmoi_command apply; then
```

Placement is deliberate. Pruning does not depend on apply succeeding, and the `run_chezmoi_command apply` failure branch returns early — running first means the directory is consistent on that branch too.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
sh tests/terrapod_command_test.sh
```

Expected: exit 0, with the six new `ok -` lines from this task and all earlier assertions still passing.

- [ ] **Step 5: Commit**

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh
git commit -F - <<'EOF'
Prune retired install warning markers on tpod apply

Apply removes marker files under unrecognized names before delegating to
chezmoi, so the state directory stays consistent even when the delegated
apply fails. Removals are reported one line each and never change the
apply exit status.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 3: Record the ownership decision

The substance of this change is the decision that the marker directory is Terrapod-owned, not the twenty lines of shell. Without it recorded, the next reader sees the prune contradicting a CONTEXT.md invariant and reverts it.

**Files:**
- Create: `docs/adr/0014-prune-unrecognized-install-warning-markers.md`
- Modify: `CONTEXT.md` — amend line 229, amend line 220, add three invariants after line 229

**Interfaces:**
- Consumes: the behavior built in Tasks 1 and 2.
- Produces: nothing.

- [ ] **Step 1: Write the ADR**

Create `docs/adr/0014-prune-unrecognized-install-warning-markers.md`. Match the structure of `docs/adr/0012-keep-package-source-migration-advisory-only.md`: an untitled opening statement of the decision, then `## Considered Options`, then `## Consequences`.

```markdown
# Prune unrecognized install warning markers on apply

The Terrapod install warning directory is owned exclusively by Terrapod. The
only valid filenames in it are the current categories from
`terrapod_install_warning_categories()`, the legacy aliases Terrapod still
reads, and in-flight `mktemp` staging files. `tpod apply` removes every other
regular file in that directory and reports each removal on one line.

Retiring a category therefore requires no second edit. Removing the name from
the category list is sufficient, and machines that already hold the marker are
cleaned by their next apply.

## Considered Options

- Keep a list of retired category names and delete only those: rejected because
  it requires a person to remember a second edit at retirement time, and
  forgetting exactly that edit is what produced issue #143. A mechanism whose
  correctness depends on remembering the step that was already forgotten is not
  a fix.
- Delete only files that parse as the four-key marker format: rejected because
  it adds a parser and a second reporting path to protect files that, under
  Terrapod-only ownership, should not exist.
- Report orphans from `tpod doctor` instead of removing them: rejected because
  `tpod doctor` is read-only for markers, so the files would still accumulate.

## Consequences

- No future feature may stage a marker file in the install warning directory
  under a name that is not yet a category. A feature needing staged state
  introduces its own state location or lands its category first.
- Legacy aliases are derived from `terrapod_install_warning_legacy_path()`
  rather than restated, so adding an alias cannot desynchronize from the prune.
- Dot-prefixed staging files survive because the prune iterates a POSIX glob,
  which does not match a leading dot. An implementation switching to `find` or
  `ls -a` must reintroduce the exclusion.
- Prune failures print a warning and do not change the exit status of
  `tpod apply`; the remaining files are retried on the next apply.
- `tpod status` and `tpod doctor` are unchanged and remain read-only for
  markers.
- The `managed-package-migration` markers left by ADR 0012's retirement are
  removed by the general rule, with no category-specific code.
```

- [ ] **Step 2: Amend the conflicting CONTEXT.md invariants**

Line 229 currently reads:

```
- Terrapod install warning marker writes should be atomic at the category file level, and marker clears remove only the matching category file.
```

Replace it with:

```
- Terrapod install warning marker writes should be atomic at the category file level, and `terrapod_install_warning_clear()` removes only the matching category file.
```

Line 220 currently reads:

```
- Terrapod install warnings are category-scoped markers that remain actionable until the same installer category completes successfully; interrupted or failed reruns must not hide the previous recovery signal.
```

Replace it with:

```
- Terrapod install warnings are category-scoped markers that, while their category exists, remain actionable until the same installer category completes successfully; interrupted or failed reruns must not hide the previous recovery signal.
```

Both edits exist so the new prune does not read as a violation of a flat prohibition. The first scopes the clause to the clearing function; the second adds the qualifier the prune depends on.

- [ ] **Step 3: Add the new invariants**

Insert these three lines after the amended line 229, keeping the surrounding one-bullet-per-line style:

```
- The Terrapod install warning directory is Terrapod-owned; its only valid filenames are current categories, the legacy aliases Terrapod still reads, and in-flight staging files.
- `tpod apply` removes install warning marker files whose names are not valid and prints one line per removal; `tpod status` and `tpod doctor` stay read-only.
- Install warning marker prune failures print a warning and do not change the exit status of `tpod apply`.
```

- [ ] **Step 4: Verify nothing regressed**

```bash
sh tests/terrapod_command_test.sh
```

Expected: exit 0. This task changes only documentation, so the suite must be unchanged from Task 2. If `tests/readme_korean_test.sh` or `tests/chezmoiignore_test.sh` cover docs, run those too:

```bash
sh tests/chezmoiignore_test.sh && sh tests/readme_korean_test.sh
```

Expected: exit 0 for both.

- [ ] **Step 5: Commit**

```bash
git add docs/adr/0014-prune-unrecognized-install-warning-markers.md CONTEXT.md
git commit -F - <<'EOF'
Record Terrapod ownership of the install warning directory

ADR 0014 states that unrecognized filenames in the install warning
directory are unreachable by definition, which is what lets apply prune
them without per-retirement bookkeeping. Scopes the clear-only-matching
invariant to terrapod_install_warning_clear so the prune does not read as
a violation of it.

Closes #143

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
```

---

## Verification

After all three tasks:

```bash
sh tests/terrapod_command_test.sh
```

Expected: exit 0 with 497 `ok -` assertions — the 483 baseline plus eight from Task 1 and six from Task 2.

Manual check against the machine state that produced the issue, using a throwaway state directory so the real one is untouched:

```bash
tmp_state="$(mktemp -d)"
mkdir -p "$tmp_state/terrapod/install-warnings"
printf "category='managed-package-migration'\n" >"$tmp_state/terrapod/install-warnings/managed-package-migration"
XDG_STATE_HOME="$tmp_state" sh -c '. dot_local/lib/terrapod/install-warnings.sh; terrapod_install_warning_prune'
ls -A "$tmp_state/terrapod/install-warnings"
rm -rf "$tmp_state"
```

Expected: prints `managed-package-migration`, then `ls -A` prints nothing.
