## Agent skills

### Issue tracker

Issues and PRDs are tracked in GitHub Issues for `juty9026/terrapod`. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the default five-label triage vocabulary. See `docs/agents/triage-labels.md`.

### Domain docs

This repo uses a single-context domain docs layout. See `docs/agents/domain.md`.

## Tests

Run the whole suite from the repo root with `tests/run`. It executes every
`tests/*_test.sh` and `tests/*_test.zsh` file, prints each file's assertions,
and exits non-zero when any file fails.

The suite needs `chezmoi`, `zsh`, `python3`, and `jq` on `PATH`.

`python3` has to be a real interpreter, not a mise shim. The tests that
exercise the Jetendard helpers override `HOME`, and a shim resolving its
tool versions under that empty `HOME` blocks on a download instead of
running. Put the resolved interpreter's directory ahead of the shims for
the run: `PATH="$(mise where python)/bin:$PATH" tests/run`.

`tests/homebrew_ubuntu_smoke.sh` is not part of `tests/run` — it builds an
Ubuntu container image and has to be invoked directly with Docker available.
