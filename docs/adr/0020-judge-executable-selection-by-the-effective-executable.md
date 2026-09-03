# Judge executable selection by the effective executable

Terrapod judges executable selection by the file that would actually execute in the managed default PATH, not by the path string the ambient PATH happens to return. That file is the effective executable.

Terrapod computes the managed default PATH itself rather than inheriting it. It resets PATH to the system default, clears leaked Homebrew and mise environment state, reads the PATH a zsh login shell builds, and prepends the Development Runtime Manager's active bin directories. When zsh is unavailable or the probe fails, Terrapod falls back to the inherited PATH and reports which PATH the verdict was computed from. Resolution follows shim indirection: when a command resolves into the Development Runtime Manager's shims directory, Terrapod asks the manager for the target, and when the manager reports the tool is not currently active, Terrapod resolves what the shim falls through to.

Canonical selection is a set of accepted locations rather than one path. `mise activate` and `mise activate --shims` are both supported resolution modes, so Development Runtime declarations accept the manager's active target, its bin directories, and the shims path. Other providers keep a single canonical location.

Advisories group by the directory a command resolved through and print invariant guidance once per advisory block instead of once per package. Payloads under the Development Runtime Manager's install directory that no active declaration can reach are reported as one advisory line with a recovery command; Terrapod never runs it.

This decision supersedes ADR 0012's definition of the compared executable as the one selected first on the ambient PATH, its singular expected executable, and its rule that only exact path matches and symlinks resolving to the same file count as canonical. It narrows ADR 0012's prohibition on suggesting provider-specific uninstall commands to cases where the provider is inferred; a payload inside a provider's own data directory establishes that provider by location. ADR 0012's non-destructive apply contract, advisory-only treatment of a non-canonical primary executable, and refusal to scan secondary PATH copies remain in force.

## Considered Options

- Reconstruct the managed default PATH in POSIX shell inside the helper: rejected because the managed shell templates and the helper would carry the same ordering rules in two places and drift on every PATH change.
- Probe through an interactive login shell: rejected because it runs the full interactive startup, including the greeting and completion initialization, for a value that non-interactive login plus the manager's bin directories already yields.
- Keep the ambient PATH and only report which PATH was used: rejected because a verdict that still changes with shell interactivity is not usable in CI, over SSH, or in cron, which are the contexts the check exists for.
- Resolve shim indirection by comparing inodes: rejected because a shim is a symlink to the manager binary and never to the canonical executable, so inode comparison cannot observe the forwarding.
- Treat the shims path as canonical and require `mise activate --shims`: rejected because it would make Terrapod's own managed zshrc non-canonical.

## Consequences

- `tpod apply`, `tpod status`, and `tpod doctor` produce the same executable selection verdict regardless of how the process was invoked.
- An unactivated non-interactive shell no longer reports an enabled command as unavailable on PATH.
- A shim that transparently forwards to the canonical executable is canonical selection and raises no advisory.
- Development Runtime declarations are canonical under either supported activation mode.
- The managed default PATH reflects system PATH ordering, so a login-shell ordering defect surfaces as an advisory rather than being hidden.
- Advisory output carries per-package actual and canonical paths, and repeats invariant guidance once per advisory block.
- Unreachable Development Runtime payloads are advisory, do not affect `tpod status` readiness, and are never removed by Terrapod.
- Executable selection remains read-only and does not persist state.
