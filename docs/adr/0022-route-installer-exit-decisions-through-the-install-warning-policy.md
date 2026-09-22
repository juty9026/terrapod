# Route installer exit decisions through the install warning policy

Exit-deciding non-blocking installer scripts go through the high-level
interface in `install-warning-script.sh`. A script declares its one install
warning category and summary, reports each failed item while continuing the
remaining attempts, and ends through either the fail-now or finish call. The
finish call formats the joined failed-item names into the category's guidance.

Those terminal calls own the marker-and-success contract. They exit 0 after a
warning marker is recorded or after a successful category's marker is cleared,
and exit 1 only when a required marker cannot be written. Script-installed
`EXIT` traps still run. A marker clear failure is not fatal: the stale marker
remains visible in `tpod doctor` until a later successful rerun clears it.

This partially supersedes ADR 0016's final call-shape paragraph. Its loading
rules remain unchanged: always-run scripts inline the policy layer and
`run_onchange_` scripts source it by path without a guard. Direct
`terrapod_install_warning_write` and `terrapod_install_warning_clear` calls are
reserved for callers that make no exit decision. The low-level policy calls
remain public for the Homebrew reconciliation script, which coordinates more
than one category and includes a mark-then-continue path.

## Considered Options

- Keep direct marker writes in single-failure scripts: rejected because each
  caller would still decide whether a recorded warning exits 0, whether an
  unrecorded warning exits 1, and whether a failed clear blocks the apply. That
  is the policy this shared layer exists to own.
- Make marker clear failures fatal: rejected because the attempted install has
  succeeded and the stale marker remains visible for recovery. Failing the
  apply would hide that distinction and disagree with the existing policy
  layer's clear behavior.

## Consequences

- Adding a one-category non-blocking installer requires declaring the category
  and reporting attempts, without copying accumulator or exit-policy code.
- Existing marker summary and guidance text stays category-specific at the
  call site, while joining failed item names and choosing the exit status live
  in the policy layer.
- A successful installer can leave a stale warning when the marker cannot be
  cleared; `tpod doctor` continues to expose it until a later rerun succeeds.
