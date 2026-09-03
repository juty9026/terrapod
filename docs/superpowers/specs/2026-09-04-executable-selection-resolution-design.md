# Executable selection resolution design

Design for issues #233-#238. Covers the resolution model that `tpod doctor`,
`tpod status`, and `tpod apply` use to judge executable selection, the advisory
output shape, a new leftover-payload finding, and the macOS login-shell PATH
bug that the resolution model exposes.

## Problem

`dot_local/lib/terrapod/executable_executable-selection` answers "does the path
string `command -v` returned equal the path string we computed?" That question
is wrong in three separate ways, and the three ways are the first three issues.

- **#236 — which PATH.** The check runs against whatever PATH the `tpod` process
  inherited. `ssh vps 'tpod doctor'` reports `bun` as a failure; `ssh vps 'zsh
  -ic "tpod doctor"'` reports it clean. A verdict that flips on shell
  interactivity is unusable in CI, over SSH, and in cron.
- **#233 — what that PATH resolves to.** A mise shim forwards transparently to
  the next PATH match, so a command already running the Homebrew build is
  reported as a different installation. `files_are_same` cannot catch it either:
  `-ef` compares inodes, and the shim is a symlink to the `mise` binary, never
  to the canonical executable. 15 false advisories on a macOS workstation.
- **#234 — what counts as canonical.** `expected_executable` hardcodes
  `$MISE_DATA_DIR/shims/<command>` for every mise declaration, but
  `dot_zshrc.tmpl:7` activates mise with `eval "$(mise activate zsh)"`, which
  puts resolved install directories at the front of PATH instead. The two
  disagree permanently, so all four Development Runtime declarations report an
  advisory on a correctly configured machine.

Two further issues sit on top of the checker rather than inside it.

- **#237 — output shape.** Every advisory prints three lines and the third is
  invariant, so 16 advisories produce 48 lines with one sentence repeated 16
  times. The one real finding is buried in the repetition.
- **#238 — a finding with no home.** 15 pre-ADR-0010 aqua payloads remain under
  `~/.local/share/mise/installs/`. They are in no mise config, can never be
  selected, and still generate shims. Today they are visible only as the false
  advisories #233 removes; after that fix they become invisible entirely.

One issue is independent of the checker.

- **#235 — a real PATH misconfiguration.** `dot_zshenv.tmpl` states that
  "declared Homebrew tools win by default", but on macOS `/etc/zprofile` runs
  `/usr/libexec/path_helper` after `.zshenv` and re-prepends `/etc/paths`.
  `/opt/homebrew/bin` and `$HOME/.local/bin` end up behind `/usr/bin`. The
  visible symptom is `git` resolving to Apple Git 2.50.1 instead of the declared
  Homebrew 2.55.0; any future overlap between `/usr/bin` and a declared formula
  regresses the same way, silently.

## Model

Replace string equality with one question: **in the managed default PATH, does
typing `<command>` actually execute a canonical binary?** The file that would
actually execute is the **effective executable**.

Four functions carry the model inside `executable-selection`.

    managed_path()                     # the PATH every verdict is computed in
    resolve_in_path <command>          # command -v within that PATH
    effective_executable <command> <resolved>
    canonical_candidates <provider> <command>

### managed_path (#236)

The managed default PATH is what a login shell builds, plus mise's active bin
directories.

    base=$(PATH=/usr/bin:/bin:/usr/sbin:/sbin \
           env -u HOMEBREW_PREFIX -u HOMEBREW_CELLAR -u HOMEBREW_REPOSITORY \
               -u MISE_SHELL \
           zsh -lc 'print -rn -- $PATH')
    bins=$(mise bin-paths 2>/dev/null | tr '\n' ':')
    managed="$bins$base"

Two details are load-bearing.

PATH is reset to the system default before the probe. Inheriting the caller's
PATH is exactly the bug #236 describes; letting it leak into the probe would
reproduce it one level down.

`HOMEBREW_PREFIX` and its siblings are unset. `dot_zshenv.tmpl:11` short-circuits
the `brew shellenv` probe when the environment already carries a prefix, so a
leaked variable whose `bin` directory is not on the reset PATH would yield a
managed PATH with no Homebrew prefix in it at all.

`zsh -lc` reads `.zshenv`, `/etc/zprofile`, and `.zprofile`, but not `.zshrc`.
That is the correct boundary: it excludes fastfetch, oh-my-zsh, and compinit,
and mise activation is supplied by `mise bin-paths` instead. `dot_zshrc.tmpl`
mutates PATH only through `mise activate`, so nothing else is lost.

Fallbacks and overrides:

- `TERRAPOD_MANAGED_PATH`, when set, is used verbatim. Tests use this.
- When `zsh` is absent or the probe fails, fall back to the inherited PATH and
  print which PATH the verdict was computed from. The verdict stays
  interpretable even when it cannot be made invocation-independent.
- The probe runs once per process; the result is cached in a variable.

Because `zsh -lc` reads `/etc/zprofile`, the managed PATH reflects path_helper's
reordering on macOS. Doctor therefore keeps reporting the `git` advisory
honestly until #235 lands, and the advisory disappears on its own once it does.

### effective_executable (#233)

Given the path `resolve_in_path` returned:

1. If it is not under the mise shims directory, it is the effective executable.
2. If it is, ask `mise which <command>`. On success that target is the effective
   executable.
3. When mise reports the tool is not currently active, the shim falls through.
   Resolve the command again against the managed PATH with the shims directory
   removed; that result is the effective executable.
4. Normalize the result before comparing.

Step 3 is the case that produces the 15 false advisories: `rg` executes 15.2.0
from Homebrew while mise's own payload sits at 15.1.0, unreachable.

### canonical_candidates (#234)

Canonical becomes a set, not a single path.

| provider | candidates |
| --- | --- |
| `homebrew-formula`, `homebrew-cask` | `<prefix>/bin/<command>` |
| `claude-installer` | `$HOME/.local/bin/<command>` |
| `mise` | `mise which <command>`, plus `<dir>/<command>` for each `mise bin-paths` directory, plus `<shims>/<command>` |

Both `mise activate` and `mise activate --shims` are supported resolution modes.
Keeping the shims path in the set preserves `--shims` without making it the only
answer. Selection is canonical when the effective executable equals, or is `-ef`
to, any candidate.

Only the `mise` provider gains a set; the others keep a single candidate and the
same behavior they have today.

### Advisory output (#237)

Advisories are buffered rather than printed inside the loop, keyed by the
directory the original `resolve_in_path` landed in — that directory is the shared
cause.

    resolved_dir|package|actual|canonical

After the loop, groups of two or more print as one block; singletons keep the
current two-line form.

      advisory - 15 commands resolve through ~/.local/share/mise/shims
                 bat, dust, duf, fastfetch, fd, fzf, gh, git-delta, lazygit,
                 lsd, neovim, ripgrep, starship, zellij, zoxide
                 canonical: /opt/homebrew/bin
      advisory - git resolves to /usr/bin/git
                 canonical: /opt/homebrew/bin/git
      Adjust PATH or remove the other installation manually, then rerun 'tpod doctor'.

The guidance sentence prints **once per advisory block**, as its last line,
rather than once per group as #237's mock shows. #237's stated goal is to stop
repeating invariant text per row; end-of-block is strictly better than per-group
at that goal, and there is only one guidance sentence to give. Per-package actual
and canonical paths remain available, as ADR 0012 requires: a group prints the
shared resolved directory and the shared canonical directory, and falls back to
per-package lines when the group's canonical paths do not share a directory.

### Leftover mise payloads (#238)

Enumerate the top-level directories under
`${MISE_DATA_DIR:-$HOME/.local/share/mise}/installs`. A directory is a leftover
when no `mise bin-paths` entry lies beneath it. Report the count as a single
advisory line.

      advisory - 15 mise payloads are outside any canonical declaration
                 'mise uninstall' reclaims the disk; Terrapod does not remove them.

This is advisory, never a failure, and the whole check is skipped when mise is
absent or the installs directory does not exist. Terrapod does not run
`mise uninstall` and does not choose which payloads to remove.

ADR 0012 and `CONTEXT.md` currently forbid suggesting provider-specific uninstall
commands. That rule exists to stop Terrapod from *inferring* an alternate
installer. Here the provider is not inferred: the payloads are inside mise's own
data directory, so mise is the provider by construction. ADR 0020 narrows the
rule to match that reasoning — no uninstall command is named unless the provider
is established by location rather than guessed.

### macOS login-shell PATH (#235)

`dot_zprofile.tmpl` gains a macOS branch that runs after `/etc/zprofile` has
already run `path_helper`: re-evaluate `brew shellenv` and re-prepend
`$HOME/.local/bin`. This is Homebrew's own documented answer to path_helper.
The Ubuntu path is unaffected and keeps its single-place initialization.

The comment in `dot_zprofile.tmpl` that says "Login-shell PATH is initialized by
the managed .zshenv for both profiles" is the assumption path_helper breaks; it
is corrected rather than left in place.

This is an ordering repair, not a new PATH source. `.zshenv` keeps building the
PATH; `.zprofile` restores the intended order after the system reshuffles it.

## Testing

Existing coverage lives in `tests/executable_selection_test.sh`, which builds a
fixture tree of fake executables and drives the helper with
`TERRAPOD_EXECUTABLE_SELECTION_INVENTORY_DIR`. Extend that harness rather than
adding a parallel one.

- `TERRAPOD_MANAGED_PATH` makes the resolution PATH injectable, so no test needs
  a real login shell. One test covers the fallback: with the probe forced to
  fail, the helper uses the inherited PATH and says so in its output.
- Shim pass-through needs a fake `mise` on PATH that reports `not currently
  active` for a command, a shim that is a symlink to that fake `mise`, and a
  canonical Homebrew executable later in `TERRAPOD_MANAGED_PATH`. The assertion
  is that no advisory is printed.
- Canonical sets need a fake `mise bin-paths` emitting a directory that holds the
  runtime executable; the assertion is that resolving there is canonical while
  the shims path is still accepted.
- Grouping asserts one header line, the package list, and exactly one occurrence
  of the guidance sentence in output carrying several advisories.
- Leftover payloads assert the count line against a fixture installs directory,
  and assert silence when the directory is absent.
- #235 renders `dot_zprofile.tmpl` with the pattern in
  `tests/zprofile_orbstack_test.sh`, sources it from a login zsh, and asserts the
  Homebrew prefix index is lower than `/usr/bin`'s. It skips on Linux, where
  path_helper does not exist.

## Documentation

ADR 0020 records the resolution model. It supersedes three clauses of ADR 0012 —
"the executable selected first on PATH", the singular "the expected executable",
and "Exact path matches and symlinks resolving to the same file are canonical" —
and narrows 0012's uninstall-command rule as described above. Per the repo
convention established in #230, the supersession is recorded on ADR 0012's side
as well.

`CONTEXT.md` lines 214-220 restate the superseded rules and are updated with
ADR 0020.

## Delivery

Six issues, one shared file, one independent file pair. Lanes are cut by file
overlap.

| lane | issues | files | start |
| --- | --- | --- | --- |
| — | ADR 0020, `CONTEXT.md` | `docs/adr/`, `CONTEXT.md` | coordinator, before dispatch |
| A | #235 | `dot_zprofile.tmpl`, `dot_zshenv.tmpl`, new test | immediately |
| B | #236, #234, #233 | `executable-selection` core, its test | immediately, parallel with A |
| C | #237, #238 | `executable-selection` output, its test | after lane B merges |

Lane B's three issues are one refactor delivered as three ordered PRs, one per
issue. The order is fixed: #236 defines the PATH that #233's fall-through
resolution searches, and #234 defines the candidate set that #233 compares
against.

Lane C edits the middle of the same file lane B is rewriting, so it waits for
lane B rather than rebasing through three merges.

Every branch is cut from `origin/main`. No stacked PRs.
