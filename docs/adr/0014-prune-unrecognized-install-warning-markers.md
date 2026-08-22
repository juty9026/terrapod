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
