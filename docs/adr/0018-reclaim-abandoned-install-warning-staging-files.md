# Reclaim abandoned install warning staging files on apply

`terrapod_install_warning_prune` reclaims dot-prefixed `mktemp` staging files in
the install warning directory once their modification time is older than one day.
Staging files younger than that are still left alone, and every other rule from
ADR 0014 is unchanged.

This narrows ADR 0014's consequence that "dot-prefixed staging files survive
because the prune iterates a POSIX glob". That rule protected in-flight writes
and, by the same token, made an abandoned staging file permanently unreachable:
`terrapod_install_warning_write` stages `.<category>.XXXXXX` and renames it over
the category file, so a process killed between the two leaves a file that nothing
in Terrapod could ever remove. These writes run inside installers users interrupt,
so the window is reached in practice.

Age is what separates the two cases. Terrapod owns the directory, and the only
legitimate reason a staging file exists is a `mv` that has not happened yet.
A `mktemp` and its `mv` are separated by four `printf`s, so a staging file that
has survived a day has no live writer waiting on it. One day is far past any real
write and far short of any interval at which a machine applies, so no in-flight
write can be pruned out from under itself.

## Considered Options

- `trap`-remove the staging file inside `terrapod_install_warning_write`:
  rejected because `trap` in POSIX shell is process-global, and the function is
  called from scripts that already install their own `EXIT` traps for downloaded
  files - `run_before_60-install-ai-cli-tools.sh.tmpl` among them. Setting a trap
  there would silently discard the caller's. It also would not cover `SIGKILL` or
  power loss, so the prune reclaim would still be needed.
- Stage under a deterministic `.<category>.staging` name so the next write
  truncates it: rejected because it drops the uniqueness `mktemp` provides
  between concurrent writers, and a category that never fails again still leaks
  the file forever.
- Reclaim every dot-prefixed file regardless of age: rejected because it makes
  a concurrent `tpod apply` able to delete a live write's staging file between
  its `mktemp` and its `mv`, turning an unrelated prune into a lost warning.

## Consequences

- ADR 0014's rule that an implementation switching to `find` or `ls -a` must
  reintroduce the dot-prefix exclusion still holds for the unrecognized-marker
  loop, which keeps iterating `"$marker_dir"/*`. The reclaim is a second,
  separately guarded loop over `"$marker_dir"/.*.??????`.
- Reclaimed staging files are reported through the same channel as retired
  markers, so `tpod apply` prints one line per reclaimed file.
- A failed reclaim sets the prune exit status like any other failed removal, so
  it prints a warning and does not change the exit status of `tpod apply`.
- The staging file lifecycle is stated in the contract comment at the top of
  `dot_local/lib/terrapod/install-warnings.sh`, which is what ADR 0014 left
  unwritten.
