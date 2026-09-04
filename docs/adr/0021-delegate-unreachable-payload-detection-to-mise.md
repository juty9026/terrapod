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
