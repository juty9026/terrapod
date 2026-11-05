# Setup First-Run Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Translation note (2026-11-05, #318):** This plan originally quoted some text in Korean, mostly copy for the since-removed `README.ko.md`. That text is translated into English here, so quoted Korean README copy and the test strings that matched it show their English meaning and no longer match any file literally. The plan is otherwise unchanged.

**Goal:** Update the canonical and Korean README files so the public Quick Start and Preset documentation describe the Terrapod Setup first-run path from Issue #60.

**Architecture:** Keep `README.md` as the source of truth and mirror its heading text and order exactly in `README.ko.md` to satisfy the heading drift check. Update only the Quick Start and Choose a Preset narrative, preserving Terrapod's existing boundaries around chezmoi, package-manager upgrades, routine command output, and the macOS App Group model.

**Tech Stack:** Markdown, POSIX shell README tests in `tests/readme_korean_test.sh`, `tests/readme_optional_stack_profiles_test.sh`, and `tests/terrapod_installer_test.sh`.

---

## File Structure

- Modify `README.md`: describe first-run installer orchestration, Terrapod Setup, customizable concrete settings, and Preset behavior.
- Modify `README.ko.md`: mirror the canonical README heading structure and explain the same flow in natural Korean while preserving canonical domain terms.
- Create `docs/superpowers/plans/2026-05-28-document-setup-first-run-path.md`: this implementation plan for Issue #60.

---

### Task 1: Update Canonical README First-Run Flow

**Files:**
- Modify: `README.md`
- Test: `tests/readme_optional_stack_profiles_test.sh`, `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Replace the Quick Start installer paragraph**

In `README.md`, replace the paragraph that starts `The installer installs` with this text:

```md
The installer installs `chezmoi` into `~/.local/bin` when needed, initializes
`https://github.com/juty9026/terrapod.git`, launches Terrapod Setup from the
checked-out source repository, and runs the initial declared-state apply only
after setup succeeds.
```

- [ ] **Step 2: Insert the Terrapod Setup explanation after the installer paragraph**

Immediately after the paragraph from Step 1, insert:

```md
Terrapod Setup is the first-run review step. It asks you to choose a Preset,
shows the concrete Terrapod-managed machine-local settings that Preset would
write, lets you customize those settings, and asks for confirmation before it
writes them. If setup is cancelled or fails, the installer stops before the
initial apply and prints a resume command for the checked-out source repository.
```

- [ ] **Step 3: Replace the Choose a Preset opening paragraph**

In `README.md`, replace:

```md
A Preset is the shape Terrapod unfolds on a machine during first-run setup.
```

with:

```md
A Preset is a starting point for Terrapod Setup. It proposes concrete
machine-local settings for a machine, and setup lets you review and customize
those settings before the initial apply.
```

- [ ] **Step 4: Replace the post-table Preset paragraph**

In `README.md`, replace:

```md
Presets write concrete machine-local settings. They are not a permanent mode,
so future Preset changes do not silently reshape an already configured machine.
```

with:

```md
Setup writes the concrete machine-local settings after you confirm them. A
Preset is not a permanent mode, so future Preset changes do not silently reshape
an already configured machine.
```

- [ ] **Step 5: Verify canonical README expectations manually and with tests**

Run:

```sh
sh tests/readme_optional_stack_profiles_test.sh
sh tests/terrapod_installer_test.sh
```

Expected: both scripts print only `ok - ...` lines and exit 0. Confirm the README still contains these strings required by existing tests:

```text
Terrapod first-run installer
You do not need to install `chezmoi` manually before running this installer.
Use `terrapod` as the primary management command after bootstrap.
Direct chezmoi use remains an advanced escape hatch.
Terrapod does not run broad Homebrew, APT, or mise upgrades.
```

- [ ] **Step 6: Commit canonical README work**

```sh
git add README.md docs/superpowers/plans/2026-05-28-document-setup-first-run-path.md
git commit -m "docs: document setup first-run flow"
```

---

### Task 2: Mirror Korean README Naturally

**Files:**
- Modify: `README.ko.md`
- Test: `tests/readme_korean_test.sh`, `tests/readme_optional_stack_profiles_test.sh`

- [ ] **Step 1: Replace the Korean Quick Start installer paragraph**

In `README.ko.md`, replace the paragraph that starts `When needed, the installer` with this text:

```md
When needed, the installer installs `chezmoi` into `~/.local/bin`,
initializes `https://github.com/juty9026/terrapod.git`, and then runs
Terrapod Setup from the checked-out source repository. It runs the initial
declared-state apply only after setup succeeds.
```

- [ ] **Step 2: Insert the Korean Terrapod Setup explanation after that paragraph**

Immediately after the paragraph from Step 1, insert:

```md
Terrapod Setup is the first-run review step. It has you choose a Preset,
shows the concrete Terrapod-managed machine-local settings that Preset will
write, lets you customize the values you need, and writes the settings after
confirmation. If setup is canceled or fails, the installer stops before the
initial apply and prints a resume command that starts again from the
checked-out source repository.
```

- [ ] **Step 3: Replace the Korean Choose a Preset opening paragraph**

In `README.ko.md`, replace:

```md
A Preset is the shape Terrapod lays out on a machine during first-run setup.
```

with:

```md
A Preset is the starting point for Terrapod Setup. It proposes the concrete
machine-local settings to write to the machine, and during setup you can
review and customize those values and confirm them before the initial apply.
```

- [ ] **Step 4: Replace the Korean post-table Preset paragraph**

In `README.ko.md`, replace:

```md
A Preset writes concrete machine-local settings. It is not a permanent mode, so a later change to a Preset definition does not silently change an already configured machine.
```

with:

```md
Setup writes concrete machine-local settings after confirmation. A Preset
is not a permanent mode, so a later change to a Preset definition does not
silently change an already configured machine.
```

- [ ] **Step 5: Verify Korean README expectations**

Run:

```sh
sh tests/readme_korean_test.sh
sh tests/readme_optional_stack_profiles_test.sh
```

Expected: both scripts print only `ok - ...` lines and exit 0. Confirm the Korean README heading list is byte-for-byte identical to `README.md` headings, including English heading text.

- [ ] **Step 6: Commit Korean README work**

```sh
git add README.ko.md
git commit -m "docs: mirror setup first-run flow in Korean"
```

---

### Task 3: Final Documentation Verification

**Files:**
- Test: `README.md`, `README.ko.md`, `tests/readme_korean_test.sh`, `tests/readme_optional_stack_profiles_test.sh`, `tests/terrapod_installer_test.sh`

- [ ] **Step 1: Run README-related tests**

```sh
sh tests/readme_korean_test.sh
sh tests/readme_optional_stack_profiles_test.sh
sh tests/terrapod_installer_test.sh
```

Expected: all scripts exit 0.

- [ ] **Step 2: Check Issue #60 acceptance criteria manually**

Confirm:

```text
- README.md says the installer launches Terrapod Setup before initial apply.
- README.md says setup lets users review and customize concrete settings from a Preset.
- README.md still preserves chezmoi, upgrade, routine command, and macOS App Group boundaries.
- README.ko.md headings match README.md exactly.
- README.ko.md preserves canonical domain terms including Terrapod, Preset, Optional Editor Stack, Optional AI Tool Stack, Optional Development Workspace, macOS App Group.
- Neither README implies per-application macOS toggles outside existing macOS App Groups.
```

- [ ] **Step 3: Commit any final verification-only fixes**

If verification required follow-up edits, commit them:

```sh
git add README.md README.ko.md
git commit -m "docs: refine setup first-run documentation"
```

If no follow-up edits were needed, do not create an empty commit.
