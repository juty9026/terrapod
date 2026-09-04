# Prunable payload delegation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Terrapod's own unreachable-mise-payload computation with `mise ls --prunable`, and add `tpod doctor --payloads` so the advisory lists the payloads and names a recovery command that runs.

**Architecture:** `dot_local/lib/terrapod/executable_executable-selection` stops walking `$MISE_DATA_DIR/installs` and asks mise instead. The summary line keeps its one-line shape for every caller; a new fifth positional argument turns on a detailed listing that resolves each payload's path with `mise where` and its size with `du -sk`. `dot_local/bin/executable_terrapod` grows a `--payloads` flag on `doctor` alone and forwards it.

**Tech Stack:** POSIX `sh` (the helper and `tpod` both run under `/bin/sh`), `awk` for arithmetic and formatting, `mise` as the verdict source, `tests/run` with `tests/lib/harness.sh` assertions.

**Spec:** `docs/superpowers/specs/2026-09-04-prunable-payload-delegation-design.md`

## Global Constraints

- POSIX `sh` only in `dot_local/lib/terrapod/executable_executable-selection` and `dot_local/bin/executable_terrapod`. No bashisms, no `local`, no arrays.
- The mise subcommand is always spelled `mise ls -C "$HOME" --prunable --no-header`, with `-C` **after** `ls`. mise accepts global flags in that position, and the test stubs dispatch on `$1`.
- A `mise` that exits non-zero for `ls --prunable` means the advisory is skipped entirely. There is no fallback computation.
- Advisory copy, exactly:
  - `  advisory - N mise payloads can be pruned` (plural)
  - `  advisory - 1 mise payload can be pruned` (singular)
  - `  advisory - N mise payloads can be pruned (X.X MB)` (detail mode; singular form takes the size the same way)
  - `             'mise prune --tools' reclaims the disk; Terrapod does not remove them.`
- Every advisory continuation line is indented 13 spaces, matching the existing block.
- The finding never sets `issue_found`, never changes an exit status, and is printed by `apply` and `doctor` only — never `status`.
- Test files carry mode 755 and a shebang; `tests/lib/harness.sh` stays 644. `tests/test_file_modes_test.sh` enforces this.
- Sizes are reported as `%.1f MB` computed from `du -sk` kilobytes divided by 1024.
- Run the suite as `PATH="$(mise where python)/bin:$PATH" tests/run` — a shimmed `python3` makes `tests/run` exit 2.

---

### Task 1: Move the verdict to mise

Replaces the installs walk with `mise ls --prunable` and rewrites the advisory copy. No listing yet: after this task the default output is correct and comes from the new source.

**Files:**
- Modify: `dot_local/lib/terrapod/executable_executable-selection:509-577` (the comment block, `leftover_payload_count`, and `render_leftover_payloads`)
- Test: `tests/executable_selection_test.sh:91-120` (the stand-in mise) and `:698-790` (the leftover payload block)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `prunable_payloads()` — no arguments, prints zero or more `<tool> <version>[ (symlink)]` lines on stdout, returns 0 always. `render_leftover_payloads()` — no arguments in this task; Task 2 gives it one.

- [ ] **Step 1: Teach the stand-in mise to answer `ls`**

In `tests/executable_selection_test.sh`, add these fixture paths next to the existing `mise_bin_paths_file` declarations (around line 95):

```sh
# 'mise ls --prunable' is the verdict source for the payload advisory. The
# fixture file's three states are the three the helper has to tell apart: the
# file missing is a mise too old for the flag, empty is a machine with nothing
# to prune, and lines are payloads.
mise_prunable_file="$tmp_dir/mise-prunable"
```

Then extend the stub's `case` (the block that already handles `bin-paths` and `which`), inserting before `esac`:

```sh
  ls)
    if [ ! -f "$mise_prunable_file" ]; then
      printf '%s\n' "error: unexpected argument '--prunable' found" >&2
      exit 2
    fi
    cat "$mise_prunable_file"
    ;;
```

Note the stub is written through an unquoted heredoc, so `$mise_prunable_file` interpolates at write time and `\${2:-}` style escaping is only needed for runtime variables. `$mise_prunable_file` needs no escaping; leave it bare.

- [ ] **Step 2: Replace the leftover payload assertions**

Replace the whole block in `tests/executable_selection_test.sh` that currently starts at the comment `# Leftover mise payloads:` and ends with the `mise_absent_output` assertions. The `run_leftover_selection` helper and the `leftover_data_dir` / `leftover_installs` variables are kept as they are; only the fixtures and assertions change.

```sh
# Payloads no configuration mise has tracked can reach. mise owns the
# computation (ADR 0021): it judges per installed version against every config
# it has seen, so a version a project-local mise.toml selects is not counted.
leftover_data_dir="$tmp_dir/mise-data"
leftover_installs="$leftover_data_dir/installs"

run_leftover_selection() {
  leftover_mode="$1"
  leftover_path="${2:-$path_dir}"
  HOME="$tmp_dir/home" \
    MISE_DATA_DIR="$leftover_data_dir" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    TERRAPOD_MANAGED_PATH="$leftover_path:/usr/bin:/bin" \
    PATH="$leftover_path:/usr/bin:/bin" \
    "$selection" "$leftover_mode" macos-terminal false false 2>&1 || true
}

absent_installs_output="$(run_leftover_selection doctor)"
assert_not_contains "$absent_installs_output" "mise payload" \
  "an absent mise installs directory says nothing about prunable payloads"
assert_contains "$absent_installs_output" "Canonical executable selection: ready" \
  "an absent mise installs directory leaves the selection verdict alone"

mkdir -p "$leftover_installs"

# The installs directory exists but the fixture file does not, which is the
# mise too old to know '--prunable'. Skipping is the whole contract here: the
# check exists to ask mise what is reachable, so a mise that cannot answer
# leaves nothing to report.
unsupported_flag_output="$(run_leftover_selection doctor)"
assert_not_contains "$unsupported_flag_output" "mise payload" \
  "a mise that rejects --prunable produces no payload advisory"
assert_contains "$unsupported_flag_output" "Canonical executable selection: ready" \
  "a mise that rejects --prunable does not fail the selection verdict"

: >"$mise_prunable_file"
nothing_prunable_output="$(run_leftover_selection doctor)"
assert_not_contains "$nothing_prunable_output" "mise payload" \
  "a machine with nothing to prune says nothing"

cat >"$mise_prunable_file" <<'EOF'
aqua:sharkdp/bat              0.26.1
npm:pnpm                      10.33.3
node                          22.22.2
EOF
leftover_output="$(run_leftover_selection doctor)"
assert_line "$leftover_output" \
  "  advisory - 3 mise payloads can be pruned" \
  "prunable payloads are reported as one counted advisory"
assert_line "$leftover_output" \
  "             'mise prune --tools' reclaims the disk; Terrapod does not remove them." \
  "the payload advisory carries its own guidance line"
assert_not_contains "$leftover_output" \
  "Adjust PATH or remove the other installation manually" \
  "the payload advisory does not borrow the selection block's guidance sentence"
assert_not_contains "$leftover_output" "aqua:sharkdp/bat" \
  "the default advisory names no payload"
assert_contains "$leftover_output" "Canonical executable selection: ready" \
  "prunable payloads are a separate finding from executable selection"

cat >"$mise_prunable_file" <<'EOF'
node                          22.22.2
EOF
single_payload_output="$(run_leftover_selection doctor)"
assert_line "$single_payload_output" \
  "  advisory - 1 mise payload can be pruned" \
  "one prunable payload is reported in the singular"

# Decision guard: disk a user chose to keep is not a readiness problem. Without
# this assertion nothing stops a later change from setting the issue flag here.
leftover_status_output="$(run_leftover_selection status)"
assert_contains "$leftover_status_output" "Executable selection: ready" \
  "tpod status stays ready on a machine whose only finding is prunable payloads"
assert_not_contains "$leftover_status_output" "mise payload" \
  "status leaves the payload advisory to apply and doctor"

leftover_apply_output="$(run_leftover_selection apply)"
assert_line "$leftover_apply_output" \
  "  advisory - 1 mise payload can be pruned" \
  "apply reports prunable payloads"

# The whole check depends on mise to say what is reachable, so an absent mise
# skips it rather than calling every payload prunable.
mise_absent_path_dir="$tmp_dir/mise-absent-path"
mkdir -p "$mise_absent_path_dir"
for path_entry in "$path_dir"/*; do
  case "${path_entry##*/}" in
    mise) continue ;;
  esac
  ln -s "$path_entry" "$mise_absent_path_dir/${path_entry##*/}"
done
mise_absent_output="$(run_leftover_selection doctor "$mise_absent_path_dir")"
assert_not_contains "$mise_absent_output" "mise payload" \
  "an absent mise skips the payload check instead of reporting one"
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `PATH="$(mise where python)/bin:$PATH" ./tests/executable_selection_test.sh`

Expected: FAIL on `prunable payloads are reported as one counted advisory` — the helper still prints `are outside any canonical declaration` and still counts install directories, so the new line never appears.

- [ ] **Step 4: Replace the helper's computation**

In `dot_local/lib/terrapod/executable_executable-selection`, replace the comment block, `leftover_payload_count()`, and `render_leftover_payloads()` (from the comment beginning `# Payloads under the Development Runtime Manager's own install directory` through the closing brace of `render_leftover_payloads`) with:

```sh
# Payloads under the Development Runtime Manager's own install directory that
# no configuration it has tracked can reach. ADR 0010 moved the Core Shell
# Stack to Homebrew and ADR 0012 kept apply non-destructive, so every install
# made before that migration stays on disk with nothing selecting it.
#
# The manager owns this computation (ADR 0021). It judges per installed
# version rather than per install directory, against every config it has
# tracked rather than the one resolved from the current directory, and it
# reports the tool's real name -- 'npm:pnpm', not the install directory slug
# 'npm-pnpm' -- which is what the recovery command has to be spelled with.
#
# Terrapod computed this itself until ADR 0021, by comparing install
# directories against 'mise bin-paths'. That reports only the toolset resolved
# from the directory tpod runs in, so every version a project declared counted
# as unreachable: 426 MB of the 620 MB reported on the workstation in #246 was
# in active use.
#
# '-C "$HOME"' pins the invocation, on ADR 0020's rule that a verdict must not
# depend on how tpod was called. It follows the subcommand because the manager
# accepts global flags there. A manager too old for '--prunable' exits non-zero
# and the finding is skipped rather than falling back to a second definition of
# reachability.
prunable_payloads() {
  command -v mise >/dev/null 2>&1 || return 0
  [ -d "${MISE_DATA_DIR:-$HOME/.local/share/mise}/installs" ] || return 0

  prunable_listing="$(mise ls -C "$HOME" --prunable --no-header 2>/dev/null)" ||
    return 0
  [ -n "$prunable_listing" ] || return 0

  printf '%s\n' "$prunable_listing"
}

# A separate finding from executable selection, so it prints after that block
# and carries its own second line instead of sharing the block's guidance
# sentence. It never sets the issue flag: disk a user chose to keep is not a
# readiness problem, and Terrapod neither prunes nor decides what to prune. The
# command is named because the payloads sit in mise's own data directory, which
# is the case ADR 0020 exempts from ADR 0012's ban on suggesting a removal
# command. 'mise prune' rather than a frozen 'mise uninstall' list, because it
# recomputes when it runs: a version a project starts declaring between this
# report and that run is not removed.
render_leftover_payloads() {
  leftover_listing="$(prunable_payloads)"
  [ -n "$leftover_listing" ] || return 0

  leftover_found="$(printf '%s\n' "$leftover_listing" | awk 'NF { count++ } END { print count + 0 }')"
  [ "$leftover_found" -gt 0 ] || return 0

  if [ "$leftover_found" -eq 1 ]; then
    printf '%s\n' "  advisory - 1 mise payload can be pruned"
  else
    printf '  advisory - %s mise payloads can be pruned\n' "$leftover_found"
  fi
  printf '%s\n' "             'mise prune --tools' reclaims the disk; Terrapod does not remove them."
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `PATH="$(mise where python)/bin:$PATH" ./tests/executable_selection_test.sh`

Expected: PASS, every assertion.

- [ ] **Step 6: Run the whole suite**

Run: `PATH="$(mise where python)/bin:$PATH" tests/run`

Expected: PASS. `tests/terrapod_command_test.sh` stubs the helper, so it does not see this change.

- [ ] **Step 7: Commit**

```bash
git add dot_local/lib/terrapod/executable_executable-selection tests/executable_selection_test.sh
git commit -m "$(cat <<'EOF'
Ask mise which payloads are prunable

The walk this replaces compared install directories against 'mise
bin-paths', which reports only the toolset resolved from the directory
tpod runs in. Every version a project-local mise.toml selected counted
as unreachable: 426 MB of the 620 MB reported on the workstation in
 #246 was in active use.

'mise ls --prunable' judges per installed version against every config
mise has tracked, so the count no longer claims a running project's
runtime is unreachable, and the stale versions a per-directory walk
hides are now counted.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MJKttBwsvTWLqvY9JNyhKJ
EOF
)"
```

---

### Task 2: List the payloads with their sizes

Adds the fifth positional argument and the detailed listing. The helper can now produce the full output; nothing invokes it with the flag yet.

**Files:**
- Modify: `dot_local/lib/terrapod/executable_executable-selection:1-16` (argument parsing and usage) and the `render_leftover_payloads` block from Task 1
- Test: `tests/executable_selection_test.sh` (the stand-in mise, and the payload block from Task 1)

**Interfaces:**
- Consumes: `prunable_payloads()` from Task 1.
- Produces: `render_leftover_payloads <show_payloads>` where `<show_payloads>` is the string `true` or `false`. The helper's fifth positional argument is `$5`, defaulting to `false`, held in the shell variable `show_payloads`.

- [ ] **Step 1: Teach the stand-in mise to answer `where`, and stub `du`**

In `tests/executable_selection_test.sh`, add next to `mise_prunable_file`:

```sh
# 'mise where' turns a tool@version into an install path, and 'du' turns that
# path into a size. Both are stubbed so a size assertion is exact instead of
# depending on the filesystem's block accounting.
mise_where_dir="$tmp_dir/mise-where"
payload_size_dir="$tmp_dir/payload-sizes"
mkdir -p "$mise_where_dir" "$payload_size_dir"

# Tool names carry ':' and '/', neither of which can be a fixture filename.
payload_fixture_key() {
  printf '%s' "$1" | tr ':/@' '___'
}
```

Extend the stub mise's `case` with a `where` arm, inserted after the `ls` arm:

```sh
  where)
    where_key="\$(printf '%s' "\${2:-}" | tr ':/@' '___')"
    if [ -f "$mise_where_dir/\$where_key" ]; then
      cat "$mise_where_dir/\$where_key"
    else
      exit 1
    fi
    ;;
```

Then write a `du` stub into the directory the leftover runs put on PATH:

```sh
# 'du -sk <path>' prints kilobytes then the path. The fixture is keyed by the
# path's basename so a test states a payload's size in one line.
cat >"$path_dir/du" <<EOF
#!/bin/sh
du_path="\${2:-}"
du_key="\${du_path##*/}"
if [ -f "$payload_size_dir/\$du_key" ]; then
  printf '%s\t%s\n' "\$(cat "$payload_size_dir/\$du_key")" "\$du_path"
else
  printf '%s\t%s\n' 0 "\$du_path"
fi
EOF
chmod +x "$path_dir/du"
```

- [ ] **Step 2: Write the failing detail assertions**

Append to the payload block in `tests/executable_selection_test.sh`, after the `apply` assertion and before the absent-mise assertions:

```sh
# The detail listing. Sizes come from stubbed 'mise where' and 'du', so the
# rendered megabytes are exact rather than filesystem-dependent.
cat >"$mise_prunable_file" <<'EOF'
aqua:sharkdp/bat              0.26.1
npm:pnpm                      10.33.3
node                          22.22.2
rust                          stable (symlink)
EOF
printf '%s\n' "$tmp_dir/payloads/bat" >"$mise_where_dir/aqua_sharkdp_bat_0.26.1"
printf '%s\n' "$tmp_dir/payloads/pnpm" >"$mise_where_dir/npm_pnpm_10.33.3"
printf '%s\n' "$tmp_dir/payloads/node" >"$mise_where_dir/node_22.22.2"
printf '%s\n' "$tmp_dir/payloads/rust" >"$mise_where_dir/rust_stable"
mkdir -p "$tmp_dir/payloads/bat" "$tmp_dir/payloads/pnpm" \
  "$tmp_dir/payloads/node" "$tmp_dir/payloads/rust"
printf '%s\n' 12288 >"$payload_size_dir/bat"
printf '%s\n' 4096 >"$payload_size_dir/pnpm"
printf '%s\n' 62464 >"$payload_size_dir/node"
printf '%s\n' 0 >"$payload_size_dir/rust"

run_payload_detail() {
  HOME="$tmp_dir/home" \
    MISE_DATA_DIR="$leftover_data_dir" \
    TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR="$inventory" \
    TERRAPOD_STANDARD_HOMEBREW_PREFIX="$prefix" \
    TERRAPOD_MISE_SHIMS_DIR="$mise_shims" \
    TERRAPOD_MANAGED_PATH="$path_dir:/usr/bin:/bin" \
    PATH="$path_dir:/usr/bin:/bin" \
    "$selection" doctor macos-terminal false false true 2>&1 || true
}

payload_detail_output="$(run_payload_detail)"
assert_line "$payload_detail_output" \
  "  advisory - 4 mise payloads can be pruned (77.0 MB)" \
  "the detailed advisory carries the total size"
assert_line "$payload_detail_output" \
  "             aqua:sharkdp/bat@0.26.1   12.0 MB" \
  "the detailed advisory lists each payload with its size"
assert_line "$payload_detail_output" \
  "             npm:pnpm@10.33.3           4.0 MB" \
  "payload names are padded to a shared width"
assert_line "$payload_detail_output" \
  "             node@22.22.2              61.0 MB" \
  "a version-level payload is listed by tool and version"
assert_line "$payload_detail_output" \
  "             rust@stable                0.0 MB" \
  "a payload linked outside the data directory is listed without its target's size"
assert_line "$payload_detail_output" \
  "             'mise prune --tools' reclaims the disk; Terrapod does not remove them." \
  "the detailed advisory keeps the guidance line last"

# The acceptance criterion the issue states: the list and the count describe
# the same set, so a reader can act on the list without wondering what the
# number covered.
payload_row_count="$(
  printf '%s\n' "$payload_detail_output" |
    grep -c ' MB$' || true
)"
[ "$payload_row_count" = "4" ] ||
  fail "the detailed listing has exactly one row per counted payload (found $payload_row_count)"
pass "the detailed listing has exactly one row per counted payload"
```

The assertions above spell the spacing literally, so here is the arithmetic that produces it. Each row is `13 spaces + name padded to the longest name + 2 spaces + size right-aligned in a field of 8`. The longest name is `aqua:sharkdp/bat@0.26.1` at 23 characters, so every row is 13 + 23 + 2 + 8 = 46 characters wide. The gap between a name and its size is `(23 - name length) + 2 + (8 - size length)`: 3 for `aqua:sharkdp/bat@0.26.1`, 11 for `npm:pnpm@10.33.3`, 14 for `node@22.22.2`, 16 for `rust@stable`. If a row assertion fails, count the gap before changing the format string.

The fixture kilobytes are chosen to divide cleanly: 12288, 4096, 62464, and 0 render as 12.0, 4.0, 61.0, and 0.0 MB, and total 78848 KB, which is 77.0 MB.

- [ ] **Step 3: Run the test to verify it fails**

Run: `PATH="$(mise where python)/bin:$PATH" ./tests/executable_selection_test.sh`

Expected: FAIL on `the detailed advisory carries the total size` — the helper ignores a fifth argument and prints the plain summary.

- [ ] **Step 4: Accept the argument and render the detail**

In `dot_local/lib/terrapod/executable_executable-selection`, extend the header (currently lines 1-16):

```sh
#!/bin/sh
set -u

mode="${1:-}"
profile="${2:-}"
ai_enabled="${3:-false}"
launcher_enabled="${4:-false}"
show_payloads="${5:-false}"

case "$mode" in
  apply|status|doctor) ;;
  *)
    printf '%s\n' "usage: executable-selection {apply|status|doctor} <profile> <ai-enabled> <launcher-enabled> [show-payloads]" >&2
    exit 2
    ;;
esac
```

Add this function immediately above `render_leftover_payloads`:

```sh
# One row per payload: the name the recovery command takes, and the disk it
# holds. The manager is asked where each payload lives rather than the name
# being turned back into a path, because the install directory is a slug of the
# tool name and the two do not round-trip.
#
# Rows are collected before anything prints because the summary line carries
# the total, and the name column cannot be padded until the longest name is
# known.
collect_prunable_rows() {
  prunable_rows=""
  prunable_total_kb=0
  prunable_name_width=0

  while read -r prunable_tool prunable_version prunable_rest; do
    [ -n "$prunable_tool" ] || continue
    prunable_name="$prunable_tool@$prunable_version"

    prunable_path="$(mise where "$prunable_name" 2>/dev/null || true)"
    prunable_kb=0
    if [ -n "$prunable_path" ]; then
      prunable_kb="$(du -sk "$prunable_path" 2>/dev/null | awk 'NR == 1 { print $1 + 0 }')"
      [ -n "$prunable_kb" ] || prunable_kb=0
    fi

    prunable_total_kb=$((prunable_total_kb + prunable_kb))
    if [ "${#prunable_name}" -gt "$prunable_name_width" ]; then
      prunable_name_width="${#prunable_name}"
    fi
    prunable_rows="$prunable_rows$prunable_name $prunable_kb
"
  done <<EOF
$1
EOF
}

# Kilobytes as the megabytes a reader decides on.
format_payload_size() {
  awk -v kb="$1" 'BEGIN { printf "%.1f MB\n", kb / 1024 }'
}
```

Then rewrite `render_leftover_payloads` to take the flag:

```sh
render_leftover_payloads() {
  leftover_detail="$1"

  leftover_listing="$(prunable_payloads)"
  [ -n "$leftover_listing" ] || return 0

  leftover_found="$(printf '%s\n' "$leftover_listing" | awk 'NF { count++ } END { print count + 0 }')"
  [ "$leftover_found" -gt 0 ] || return 0

  leftover_total=""
  if [ "$leftover_detail" = "true" ]; then
    collect_prunable_rows "$leftover_listing"
    leftover_total=" ($(format_payload_size "$prunable_total_kb"))"
  fi

  if [ "$leftover_found" -eq 1 ]; then
    printf '  advisory - 1 mise payload can be pruned%s\n' "$leftover_total"
  else
    printf '  advisory - %s mise payloads can be pruned%s\n' "$leftover_found" "$leftover_total"
  fi

  if [ "$leftover_detail" = "true" ]; then
    printf '%s' "$prunable_rows" | while read -r leftover_name leftover_kb; do
      [ -n "$leftover_name" ] || continue
      printf '             %-*s  %8s\n' \
        "$prunable_name_width" "$leftover_name" "$(format_payload_size "$leftover_kb")"
    done
  fi

  printf '%s\n' "             'mise prune --tools' reclaims the disk; Terrapod does not remove them."
}
```

Finally update the one call site near the bottom of the file:

```sh
case "$mode" in
  apply|doctor) render_leftover_payloads "$show_payloads" ;;
esac
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `PATH="$(mise where python)/bin:$PATH" ./tests/executable_selection_test.sh`

Expected: PASS, every assertion.

- [ ] **Step 6: Run the whole suite**

Run: `PATH="$(mise where python)/bin:$PATH" tests/run`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add dot_local/lib/terrapod/executable_executable-selection tests/executable_selection_test.sh
git commit -m "$(cat <<'EOF'
List prunable payloads and the disk they hold

The summary line named a command but not what to run it on, so acting
on it meant reconstructing the check by hand. The listing spells each
payload the way the recovery command takes it, which is why the name
comes from 'mise ls' and the path from 'mise where': the install
directory is a slug of the tool name and the two do not round-trip.

Kept behind an argument. Per-payload lines are the noise #237 removed,
so the default output stays one line.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MJKttBwsvTWLqvY9JNyhKJ
EOF
)"
```

---

### Task 3: Wire `--payloads` through tpod

**Files:**
- Modify: `dot_local/bin/executable_terrapod:822-834` (`run_executable_selection`), `:1531-1534` (`run_doctor` argument handling), `:159-200` (`show_help`)
- Test: `tests/terrapod_command_test.sh:724-733` (the helper stub) and the doctor assertions

**Interfaces:**
- Consumes: the helper's fifth positional argument from Task 2.
- Produces: `run_executable_selection <mode> <config_file> <profile> <show_payloads>` — a fourth parameter, the string `true` or `false`. `run_doctor` sets the shell variable `doctor_show_payloads`.

- [ ] **Step 1: Record the helper's arguments in the stub**

In `tests/terrapod_command_test.sh`, replace the `write_stub "$executable_selection_stub"` call (around line 725) with:

Only the mode and the payload flag are recorded. Logging `$*` would tie the assertions to whatever the config fixture makes of the AI and launcher arguments, which this test is not about.

```sh
write_stub "$executable_selection_stub" \
  'if [ -n "${TERRAPOD_EXECUTABLE_SELECTION_ARGS_FILE:-}" ]; then' \
  '  printf "%s %s\n" "${1:-}" "${5:-unset}" >>"$TERRAPOD_EXECUTABLE_SELECTION_ARGS_FILE"' \
  'fi' \
  'if [ -n "${TERRAPOD_EXECUTABLE_SELECTION_OUTPUT:-}" ]; then' \
  '  printf "%s\n" "$TERRAPOD_EXECUTABLE_SELECTION_OUTPUT"' \
  '  exit "${TERRAPOD_EXECUTABLE_SELECTION_STATUS:-0}"' \
  'fi' \
  'case "${1:-}" in' \
  '  doctor) printf "%s\n" "Canonical executable selection: ready" ;;' \
  '  status) printf "%s\n" "Executable selection: ready" ;;' \
  'esac'
```

- [ ] **Step 2: Write the failing assertions**

Append near the other doctor assertions in `tests/terrapod_command_test.sh`, after the block that ends with `"doctor keeps a non-standard Homebrew prefix advisory-only"`:

```sh
# --payloads is the only argument doctor takes, and it reaches the helper as
# the fifth positional argument. Nothing else may turn the listing on: apply
# and status print the summary line, which is the shape #237 settled on.
payloads_args_file="$tmp_dir/payloads-args"
payloads_path="$(homebrew_owned_status_doctor_path payloads "$standard_brew_prefix" zsh apt)"

: >"$payloads_args_file"
TERRAPOD_EXECUTABLE_SELECTION_ARGS_FILE="$payloads_args_file" \
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" \
  TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" \
  PATH="$payloads_path" /bin/sh "$vps_homebrew_terrapod" doctor >/dev/null 2>&1 || true
assert_file_contains "$payloads_args_file" "doctor false" \
  "doctor leaves the payload listing off by default"

: >"$payloads_args_file"
TERRAPOD_EXECUTABLE_SELECTION_ARGS_FILE="$payloads_args_file" \
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" \
  TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" \
  PATH="$payloads_path" /bin/sh "$vps_homebrew_terrapod" doctor --payloads >/dev/null 2>&1 || true
assert_file_contains "$payloads_args_file" "doctor true" \
  "doctor --payloads turns the payload listing on"

: >"$payloads_args_file"
TERRAPOD_EXECUTABLE_SELECTION_ARGS_FILE="$payloads_args_file" \
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" \
  TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" \
  PATH="$payloads_path" /bin/sh "$vps_homebrew_terrapod" status >/dev/null 2>&1 || true
assert_file_contains "$payloads_args_file" "status false" \
  "status never turns the payload listing on"

set +e
doctor_bad_flag_output="$(
  TERRAPOD_OS_RELEASE_FILE="$status_ubuntu_os_release" \
    TERRAPOD_CHEZMOI_CONFIG="$status_ubuntu_config" \
    PATH="$payloads_path" /bin/sh "$vps_homebrew_terrapod" doctor --verbose 2>&1
)"
doctor_bad_flag_status=$?
set -e
assert_status "$doctor_bad_flag_status" 64 "doctor rejects an unknown argument"
assert_contains "$doctor_bad_flag_output" "terrapod: doctor accepts only --payloads" \
  "doctor explains which argument it takes"

assert_contains \
  "$help_output" \
  "tpod doctor [--payloads]" \
  "Terrapod help documents the doctor payload listing"
```

`$help_output` is captured earlier in the file; this assertion belongs with the other `help_output` assertions around line 1457, so place that last one there rather than in this block.

- [ ] **Step 3: Run the test to verify it fails**

Run: `PATH="$(mise where python)/bin:$PATH" ./tests/terrapod_command_test.sh`

Expected: FAIL on `doctor leaves the payload listing off by default` — `run_executable_selection` passes four arguments, so `$5` is unset in the stub and the recorded line reads `doctor unset`.

- [ ] **Step 4: Forward the flag**

In `dot_local/bin/executable_terrapod`, extend `run_executable_selection`:

```sh
run_executable_selection() {
  mode="$1"
  config_file="$2"
  profile="$3"
  show_payloads="${4:-false}"

  canonical_homebrew_prefix="$(standard_homebrew_prefix "$profile" 2>/dev/null || true)"
  TERRAPOD_STANDARD_HOMEBREW_PREFIX="$canonical_homebrew_prefix" \
    "$executable_selection_helper" \
    "$mode" \
    "$profile" \
    "$(effective_ai_cli_tools_enabled "$config_file" "$profile")" \
    "$(config_data_bool "$config_file" enableMacosAppGroupLauncher)" \
    "$show_payloads"
}
```

Replace `run_doctor`'s argument rejection:

```sh
run_doctor() {
  doctor_show_payloads=false

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --payloads)
        doctor_show_payloads=true
        shift
        ;;
      *)
        fail_usage "doctor accepts only --payloads"
        ;;
    esac
  done
```

Find the `run_executable_selection doctor` call inside `run_doctor` (near line 1606) and pass the flag as a fourth argument. The `status` and `apply` call sites keep three arguments and inherit the `false` default.

In `show_help`, change the Usage line `  tpod doctor` to `  tpod doctor [--payloads]`, and add under Options, after the `--yes` entry:

```sh
  printf '%s\n' "  --payloads                            Let doctor list the mise payloads its advisory counts,"
  printf '%s\n' "                                        with the disk each one holds."
```

Leave the Commands section's `doctor` line as it is.

- [ ] **Step 5: Run the test to verify it passes**

Run: `PATH="$(mise where python)/bin:$PATH" ./tests/terrapod_command_test.sh`

Expected: PASS, every assertion.

- [ ] **Step 6: Run the whole suite**

Run: `PATH="$(mise where python)/bin:$PATH" tests/run`

Expected: PASS.

- [ ] **Step 7: Check the real command end to end**

Run both, and compare:

```bash
/bin/sh dot_local/bin/executable_terrapod doctor --payloads
mise ls -C "$HOME" --prunable --no-header | wc -l
```

Expected: the count in the advisory line equals the second command's number, and the advisory is followed by exactly that many rows. If they disagree, stop — the delegation is not actually delegating, and no later task fixes that.

- [ ] **Step 8: Commit**

```bash
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh
git commit -m "$(cat <<'EOF'
Let doctor list the payloads its advisory counts

The listing is doctor's alone. apply prints the summary on every run
and status stays a readiness snapshot, so neither has a reason to
carry per-payload lines.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MJKttBwsvTWLqvY9JNyhKJ
EOF
)"
```

---

### Task 4: Record the decision

**Files:**
- Create: `docs/adr/0021-delegate-unreachable-payload-detection-to-mise.md`
- Modify: `docs/adr/0020-judge-executable-selection-by-the-effective-executable.md` (the supersession paragraph and the consequence about unreachable payloads)
- Modify: `CONTEXT.md` (the two sentences covering the recovery command rule and the payload advisory)
- Test: `tests/adr_supersession_test.sh` (existing, no changes)

**Interfaces:**
- Consumes: the behavior Tasks 1-3 shipped.
- Produces: nothing later tasks read.

- [ ] **Step 1: Write ADR 0021**

Create `docs/adr/0021-delegate-unreachable-payload-detection-to-mise.md`:

```markdown
# Delegate unreachable payload detection to the Development Runtime Manager

Terrapod asks the Development Runtime Manager which installed payloads no configuration it has tracked can reach, rather than computing that itself. The manager judges per installed version against every configuration it has seen, and reports each payload by the tool name and version its own removal command takes.

`tpod apply` and `tpod doctor` report the result as one advisory line with the count and a recovery command. `tpod doctor --payloads` additionally lists each payload with the disk it holds and the total. `tpod status` does not report the finding. Terrapod never removes a payload and never runs the recovery command.

The manager is asked with the working directory pinned to the user's home directory, so the verdict does not depend on how `tpod` was invoked. When the manager is absent, its install directory does not exist, or it is too old to answer, the finding is skipped rather than computed a second way.

This decision supersedes ADR 0020's definition of the unreachable payload finding as payloads under the Development Runtime Manager's install directory that no active declaration can reach, and its advisory wording. ADR 0020's effective executable model, managed default PATH, canonical location sets, and advisory grouping remain in force. ADR 0012's non-destructive apply contract and ADR 0020's narrowing of ADR 0012's rule on naming a removal command both remain in force: naming the manager's own command for payloads inside the manager's own data directory is the case that narrowing exempts.

## Considered Options

- Keep Terrapod's own computation and extend it to installed versions: rejected because it judges reachability from `mise bin-paths`, which reports only the toolset resolved from the directory `tpod` runs in. Every version a project-local configuration selects is counted as unreachable; on the workstation in issue #246 that is 426 MB of a reported 620 MB. Printing a list and a removal command built from that set would break working projects.
- Keep the count as computed and derive the removal command from the manager's prunable set: rejected because a user would be told one number and handed a command covering a different one.
- Read the manager's tracked configuration state directly: rejected because it reimplements the manager's own pruning rule against its internal state format.
- Keep Terrapod's computation as a fallback for a manager too old to answer: rejected because carrying two definitions of reachability is the cost the delegation removes, and the two disagree exactly where the disagreement is dangerous.

## Consequences

- The count is per installed version, so stale versions of a tool whose current version is selected are reported; a per-directory count hid them.
- Payloads a project-local configuration selects are not reported, so the count no longer claims a running project's runtime is unreachable.
- Payloads are named the way the manager's removal command takes them, rather than by their install directory, which is a slug of the tool name.
- `tpod doctor --payloads` is the only surface that lists payloads or measures disk. The default output stays one advisory line on every surface that reports the finding.
- The named recovery command recomputes the set when it runs, so a version a configuration starts selecting between the report and the run is not removed.
- Unreachable payloads remain advisory, do not affect `tpod status` readiness, and are never removed by Terrapod.
- A Development Runtime Manager too old to report prunable payloads produces no finding rather than a differently computed one.
```

- [ ] **Step 2: Record the supersession on ADR 0020**

In `docs/adr/0020-judge-executable-selection-by-the-effective-executable.md`, append to the supersession paragraph (the one beginning `This decision supersedes ADR 0012's definition`):

```
ADR 0021 supersedes this decision's definition of the unreachable payload finding and its advisory wording, and delegates that computation to the Development Runtime Manager. This decision's effective executable model, managed default PATH, canonical location sets, and advisory grouping remain in force.
```

In the same file, replace the consequence line `Unreachable Development Runtime payloads are advisory, do not affect \`tpod status\` readiness, and are never removed by Terrapod.` with:

```
- Unreachable Development Runtime payloads are advisory, do not affect `tpod status` readiness, and are never removed by Terrapod. ADR 0021 moves the computation of which payloads those are to the Development Runtime Manager.
```

- [ ] **Step 3: Update CONTEXT.md**

Replace the sentence ending `never when the provider would have to be inferred` with:

```
- Executable selection checks are read-only and do not persist pending state. They name a recovery command only when the provider is established by location, such as a payload inside the **Development Runtime Manager**'s own data directory, and never when the provider would have to be inferred. The command named for unreachable payloads is the manager's own pruning command, which recomputes the set when it runs.
```

Replace the sentence beginning `**Development Runtime Manager** payloads that no active declaration can reach` with:

```
- The **Development Runtime Manager** decides which of its installed payloads no tracked configuration can reach; Terrapod reports that verdict per installed version as one advisory line, does not let it affect `tpod status` readiness, and never removes a payload.
- `tpod doctor --payloads` lists those payloads with the disk each one holds; every other surface prints the count alone.
```

- [ ] **Step 4: Run the suite**

Run: `PATH="$(mise where python)/bin:$PATH" tests/run`

Expected: PASS, including `tests/adr_supersession_test.sh` reporting `ADR 0020 names ADR 0021 back`.

- [ ] **Step 5: Commit**

```bash
git add docs/adr/0021-delegate-unreachable-payload-detection-to-mise.md \
  docs/adr/0020-judge-executable-selection-by-the-effective-executable.md \
  CONTEXT.md
git commit -m "$(cat <<'EOF'
Record delegating payload reachability to mise (ADR 0021)

The decision worth recording is not that the count changed but that
Terrapod stopped owning the question. mise judges per installed version
against every config it has tracked; Terrapod's own test judged per
install directory against the config resolved from wherever tpod ran.

ADR 0012's non-destructive contract is untouched: Terrapod still names
a command it never runs.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MJKttBwsvTWLqvY9JNyhKJ
EOF
)"
```
