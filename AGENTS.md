## Agent skills

### Issue tracker

Issues and PRDs are tracked in GitHub Issues for `juty9026/terrapod`. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the default five-label triage vocabulary. See `docs/agents/triage-labels.md`.

### Domain docs

This repo uses a single-context domain docs layout. See `docs/agents/domain.md`.

## Language

Write everything you author for this repository in English: documents, code
comments, test names and messages, and user-facing strings such as prompts,
menu labels, and error messages. Korean text has no approved exceptions. This
is a language rule, not an ASCII rule: emoji, symbols, and other non-ASCII
characters stay allowed where they belong.

The rule also covers project metadata written from now on: commit messages,
and GitHub issue and pull request titles, bodies, and comments. PR bodies keep
a respectful, polite tone, written in English. Existing commits, issues, and
PRs stay as they were written. The automated language check covers tracked
files. The written rule applies to commit history and GitHub metadata.

## Repository checks

| Rule | Enforcement |
| --- | --- |
| Write tracked text in English. | `tests/repository_language_test.sh` reports Hangul by file and line. |
| Keep test entry points executable and the shared harness source-only. | `tests/test_file_modes_test.sh` checks both file modes. |
| Run the suite with a real Python interpreter. | `tests/run` rejects mise shims for `python3`. |
| Declare mandatory Homebrew formulae in `dot_local/lib/terrapod/core.Brewfile`. | The root `Brewfile` points to this managed file. `tests/homebrew_manifests_test.sh` checks the link and runs the installed executable-selection helper against a changed declaration. |
| Use `terrapod_homebrew_install` in scripts that install Homebrew packages during `tpod apply`. | `tests/homebrew_apply_policy_test.sh` catches direct install and bundle calls. The bundle and shell integration tests check the arguments and environment passed to Homebrew. |

## Tests

Run the whole suite from the repo root with `tests/run`. It executes every
`tests/*_test.sh` and `tests/*_test.zsh` file, prints each file's assertions,
and exits non-zero when any file fails. Test files carry a shebang and the
executable bit (mode 755) so each one also runs directly as
`./tests/<name>_test.sh`; `tests/lib/harness.sh` is sourced, not executed,
and stays 644.

The suite needs `git`, `chezmoi`, `zsh`, `python3`, and `jq` on `PATH`.

A Lua interpreter (`lua5.4`, `lua`, or `luajit`, in that order) is optional:
`tests/hammerspoon_runtime_test.sh` loads `dot_hammerspoon/init.lua` through
it with `hs` stubbed, and reports a skip instead of running when none is
found. CI installs one on both runners so the skip never hides a regression
there.

`python3` has to be a real interpreter, not a mise shim. The tests that
exercise the Jetendard helpers override `HOME`, and a shim resolving its
tool versions under that empty `HOME` blocks on a download instead of
running. `tests/run` checks for this before running anything and exits
with status 2 when `python3` resolves into the mise shim directory. Put
the resolved interpreter's directory ahead of the shims for the run:
`PATH="$(mise where python)/bin:$PATH" tests/run`.

`tests/homebrew_ubuntu_smoke.sh` is not part of `tests/run` — it builds an
Ubuntu container image and has to be invoked directly with Docker available.

Every test file sources `tests/lib/harness.sh` for its assertion vocabulary
instead of declaring its own. The harness provides `fail`, `pass`, `skip`, a
`make_tmp_dir` preamble that fills `$tmp_dir` and removes it however the test
ends, and seven assertions with fixed signatures:

    assert_contains          <haystack> <needle> <message>
    assert_not_contains      <haystack> <needle> <message>
    assert_file_contains     <file>     <needle> <message>
    assert_file_not_contains <file>     <needle> <message>
    assert_equals            <actual>   <expected> <message>
    assert_status            <actual>   <expected> <message>
    assert_file_exists       <path>                <message>

The haystack forms take a string and the file forms take a path; neither
guesses. Assertions specific to one test file still live in that file.
