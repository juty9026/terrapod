# Setup First-Run Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

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

In `README.ko.md`, replace the paragraph that starts `installer는 필요할 때` with this text:

```md
installer는 필요할 때 `chezmoi`를 `~/.local/bin`에 설치하고,
`https://github.com/juty9026/terrapod.git`을 초기화한 뒤, checked-out source
repository에서 Terrapod Setup을 실행합니다. setup이 성공한 다음에만 initial
declared-state apply를 실행합니다.
```

- [ ] **Step 2: Insert the Korean Terrapod Setup explanation after that paragraph**

Immediately after the paragraph from Step 1, insert:

```md
Terrapod Setup은 first-run review 단계입니다. Preset을 선택하게 한 뒤 그
Preset이 쓸 구체적인 Terrapod-managed machine-local setting을 보여 주고,
필요한 값을 customize한 다음, 확인을 받은 후에 설정을 씁니다. setup이
cancel되거나 실패하면 installer는 initial apply 전에 멈추고, checked-out
source repository에서 다시 시작할 resume command를 출력합니다.
```

- [ ] **Step 3: Replace the Korean Choose a Preset opening paragraph**

In `README.ko.md`, replace:

```md
Preset은 first-run setup 중 Terrapod이 machine에 펼쳐 놓을 형태입니다.
```

with:

```md
Preset은 Terrapod Setup의 starting point입니다. machine에 쓸 구체적인
machine-local setting을 제안하며, setup 중 그 값을 검토하고 customize한 뒤
initial apply 전에 확정할 수 있습니다.
```

- [ ] **Step 4: Replace the Korean post-table Preset paragraph**

In `README.ko.md`, replace:

```md
Preset은 구체적인 machine-local setting을 씁니다. 영구적인 mode가 아니므로, 나중에 Preset 정의가 바뀌어도 이미 설정된 machine이 조용히 다시 바뀌지는 않습니다.
```

with:

```md
Setup은 확인을 받은 뒤 구체적인 machine-local setting을 씁니다. Preset은
영구적인 mode가 아니므로, 나중에 Preset 정의가 바뀌어도 이미 설정된
machine이 조용히 다시 바뀌지는 않습니다.
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
