# Homebrew AI CLI and Development Apps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Install Antigravity CLI, Claude Code, and Codex from one optional Homebrew bundle on macOS and Ubuntu, and replace the macOS ai-apps group with an explicitly migrated development-apps group containing Zed and Orca ADE.

**Architecture:** A new cross-profile rendered Brewfile owns the three AI CLI casks. The existing Optional AI Tool Stack installer becomes a Homebrew adapter that uses existing macOS Homebrew or conditionally bootstraps Linux Homebrew, while Terrapod status and doctor verify both command availability and Homebrew provenance. The macOS App Group schema is renamed without aliasing the legacy key, and setup/configure removes the deprecated key during an explicit rewrite.

**Tech Stack:** POSIX shell, chezmoi Go templates, Homebrew Bundle, shell-based integration tests, Markdown ADR/domain/README documentation.

## Global Constraints

- Support exactly the macOS Terminal Profile and Ubuntu 24.04 VPS Shell Profile.
- Do not uninstall existing GUI apps, Homebrew casks, Linuxbrew, or legacy vendor-installed AI CLI binaries.
- Keep all non-AI modern CLI tools and development runtimes owned by mise.
- Keep `tpod update` source-only and use `brew bundle --no-upgrade` during apply.
- Install Linux Homebrew only when `enableAiCliTools` or `enableDevelopmentWorkspace` enables the Optional AI Tool Stack.
- Do not interpret `enableMacosAppGroupAiApps` as an alias for `enableMacosAppGroupDevelopmentApps`.
- Keep Development Apps independent from Optional Development Workspace.
- Manage only the Zed cask; do not add Zed settings, extensions, or keymaps.
- Preserve first-run warning-backed recovery through the `optional-ai-cli-tools` marker.
- Use the Canonical README as the source of truth and keep the Korean README aligned.

---

### Task 1: Rename the macOS App Group and migrate the managed schema

**Files:**
- Modify: `tests/chezmoiignore_test.sh`
- Modify: `tests/terrapod_config_test.sh`
- Modify: `tests/terrapod_command_test.sh`
- Modify: `tests/terrapod_installer_test.sh`
- Modify: `Brewfile.macos-desktop-apps.tmpl`
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-homebrew.sh.tmpl`
- Modify: `.chezmoiscripts/run_before_01-retry-homebrew-desktop-apps.sh.tmpl`
- Modify: `.chezmoiscripts/run_onchange_after_50-open-karabiner-if-needed.sh.tmpl`
- Modify: `install.sh`
- Modify: `dot_local/bin/executable_terrapod`

**Interfaces:**
- Consumes: Existing managed setup config parsing, writing, Preset rendering, macOS desktop Brewfile rendering, and status/doctor preflight.
- Produces: Managed key `enableMacosAppGroupDevelopmentApps`; setup label `development-apps`; cask bundle containing `zed` and `stablyai/orca/orca`; explicit removal of deprecated key `enableMacosAppGroupAiApps` during setup/configure writes.

- [ ] **Step 1: Replace App Group test fixtures and add explicit migration coverage**

Update test data and expectations so the new group is represented as:

```sh
macos_development_apps_data='{"chezmoi":{"os":"darwin"},"enableMacosAppGroupTerminalApps":false,"enableMacosAppGroupAutomation":false,"enableMacosAppGroupLauncher":false,"enableMacosAppGroupMonitoring":false,"enableMacosAppGroupDevelopmentApps":true}'

assert_contains_text "$development_apps_brewfile" 'cask "zed"' \
  "development-apps group renders Zed"
assert_contains_text "$development_apps_brewfile" \
  'cask "stablyai/orca/orca", trusted: true' \
  "development-apps group trusts only Orca's fully-qualified vendor cask"
for removed_cask in claude codex-app chatgpt antigravity antigravity-ide; do
  assert_not_contains_text "$development_apps_brewfile" "cask \"$removed_cask\"" \
    "development-apps group excludes removed desktop cask: $removed_cask"
done
```

Change Preset assertions to require `enableMacosAppGroupDevelopmentApps = false`
for minimal/development and `true` for workstation. Add an existing config
fixture containing `enableMacosAppGroupAiApps = true`, run configure/setup, and
assert:

```sh
assert_data_key_once_with_value "$migrated_config" \
  "enableMacosAppGroupDevelopmentApps" "true" \
  "workstation migration writes development-apps exactly once"
assert_not_contains "$migrated_config" "enableMacosAppGroupAiApps" \
  "explicit setup migration removes deprecated ai-apps key"
```

Add status/apply preflight coverage showing a legacy config without the new key
is incomplete and the missing-key output names
`enableMacosAppGroupDevelopmentApps`.

- [ ] **Step 2: Run focused tests and verify the new expectations fail**

Run:

```sh
sh tests/chezmoiignore_test.sh
sh tests/terrapod_config_test.sh
sh tests/terrapod_command_test.sh
sh tests/terrapod_installer_test.sh
```

Expected: FAIL because the current templates and Terrapod command still expose
`ai-apps` and `enableMacosAppGroupAiApps`, and setup/configure does not write or
clean the new key.

- [ ] **Step 3: Implement the new App Group and managed-key rewrite**

Replace the final block in `Brewfile.macos-desktop-apps.tmpl` with:

```ruby
{{ if default false (get . "enableMacosAppGroupDevelopmentApps") -}}
# development-apps macOS App Group
cask "zed"
cask "stablyai/orca/orca", trusted: true
{{ end -}}
```

Replace every macOS App Group enable calculation with
`enableMacosAppGroupDevelopmentApps`. In `executable_terrapod`, update setup
titles, descriptions, summary output, status output, Preset variables, and
`managed_setup_keys()` to use the new key and the detail `(Zed and Orca ADE)`.

Keep the old name only in the config rewrite removal set:

```awk
function is_managed_name(name) {
  return name == "profile" ||
    name == "enableEditorStack" ||
    name == "enableAiCliTools" ||
    name == "enableDevelopmentWorkspace" ||
    name == "enableMacosAppGroupTerminalApps" ||
    name == "enableMacosAppGroupAutomation" ||
    name == "enableMacosAppGroupLauncher" ||
    name == "enableMacosAppGroupMonitoring" ||
    name == "enableMacosAppGroupDevelopmentApps" ||
    name == "enableMacosAppGroupAiApps" ||
    name == "enableMacosDesktopApps" ||
    name == "terrapodPreset"
}
```

Because `managed_setup_keys()` contains only the new key, legacy config is
incomplete until an explicit setup/configure rewrite. Because the AWK removal
set contains both names, the rewrite removes the deprecated assignment.

- [ ] **Step 4: Run focused tests and verify the App Group migration passes**

Run the four commands from Step 2.

Expected: PASS with development-apps and managed-schema assertions succeeding;
no output expectation refers to ai-apps except explicit legacy migration tests.

- [ ] **Step 5: Commit the App Group slice**

```sh
git add Brewfile.macos-desktop-apps.tmpl .chezmoiscripts \
  dot_local/bin/executable_terrapod tests
git commit -m "feat: replace AI apps with development apps"
```

---

### Task 2: Install the Optional AI Tool Stack from a common Homebrew bundle

**Files:**
- Create: `Brewfile.ai-cli-tools.tmpl`
- Modify: `.chezmoiscripts/run_onchange_before_60-install-ai-cli-tools.sh.tmpl`
- Modify: `.chezmoiscripts/run_onchange_before_00-bootstrap-ubuntu.sh.tmpl`
- Modify: `tests/chezmoiignore_test.sh`
- Modify: `tests/bootstrap_ubuntu_test.sh`
- Modify: `tests/terrapod_command_test.sh`

**Interfaces:**
- Consumes: Effective Optional AI Tool Stack flags, supported Homebrew prefixes, Homebrew official installer, install-warning library.
- Produces: Rendered AI CLI Brewfile with casks `antigravity-cli`, `claude-code`, and `codex`; `find_brew`; `setup_brew_environment`; conditional Linux Homebrew bootstrap; warning-backed `brew bundle --no-upgrade` execution.

- [ ] **Step 1: Add failing bundle and installer behavior tests**

Render the new template for enabled, workspace-enabled, and disabled data. Add
these assertions:

```sh
assert_contains_text "$ai_cli_brewfile" 'cask "antigravity-cli"' \
  "Optional AI Tool Stack declares Antigravity CLI cask"
assert_contains_text "$ai_cli_brewfile" 'cask "claude-code"' \
  "Optional AI Tool Stack declares Claude Code cask"
assert_contains_text "$ai_cli_brewfile" 'cask "codex"' \
  "Optional AI Tool Stack declares Codex CLI cask"
assert_text_equals "$disabled_ai_cli_brewfile" "" \
  "disabled Optional AI Tool Stack renders no Homebrew casks"
```

Replace vendor URL assertions with a stubbed Homebrew log. Verify macOS uses an
existing `brew`, executes `shellenv`, and calls:

```text
brew args:bundle --no-upgrade --file=<temporary Brewfile>
```

Verify Ubuntu with no Homebrew downloads only the official Homebrew installer,
runs it with `NONINTERACTIVE=1`, locates
`/home/linuxbrew/.linuxbrew/bin/brew`, and installs the same rendered bundle.
Assert the old three URLs never appear in rendered output.

Extend Ubuntu bootstrap tests so enabled AI tooling installs `file` and
`procps`, while the existing base bootstrap behavior remains unchanged when the
stack is disabled.

Preserve tests for stale marker cleanup, routine failure returning non-zero,
first-run failure returning zero after marker creation, and retry success
clearing the marker.

- [ ] **Step 2: Run focused tests and verify Homebrew expectations fail**

Run:

```sh
sh tests/chezmoiignore_test.sh
sh tests/bootstrap_ubuntu_test.sh
sh tests/terrapod_command_test.sh
```

Expected: FAIL because `Brewfile.ai-cli-tools.tmpl` does not exist and the AI
installer still downloads vendor scripts and skips already-present commands.

- [ ] **Step 3: Create the optional AI CLI Brewfile**

Create `Brewfile.ai-cli-tools.tmpl`:

```ruby
{{- if or (default false (get . "enableAiCliTools")) (default false (get . "enableDevelopmentWorkspace")) -}}
cask "antigravity-cli"
cask "claude-code"
cask "codex"
{{- end -}}
```

- [ ] **Step 4: Replace vendor installation with the Homebrew adapter**

Keep the current warning helper contract, then implement these shell interfaces:

```sh
find_brew() {
  if command -v brew >/dev/null 2>&1; then
    command -v brew
  elif [ -x /opt/homebrew/bin/brew ]; then
    printf '%s\n' /opt/homebrew/bin/brew
  elif [ -x /usr/local/bin/brew ]; then
    printf '%s\n' /usr/local/bin/brew
  elif [ -x /home/linuxbrew/.linuxbrew/bin/brew ]; then
    printf '%s\n' /home/linuxbrew/.linuxbrew/bin/brew
  else
    return 1
  fi
}

bootstrap_linux_homebrew() {
  installer_path="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-install.XXXXXX")" || return 1
  if ! curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh \
    -o "$installer_path"; then
    return 1
  fi
  NONINTERACTIVE=1 /bin/bash "$installer_path"
}

setup_brew_environment() {
  brew_bin="$(find_brew || true)"
  if [ -z "$brew_bin" ] && [ "$(uname -s)" = Linux ]; then
    bootstrap_linux_homebrew || return 1
    brew_bin="$(find_brew || true)"
  fi
  [ -n "$brew_bin" ] || return 1
  brew_shellenv="$($brew_bin shellenv)" || return 1
  eval "$brew_shellenv"
}
```

Render the bundle into a temporary file through chezmoi inclusion and install it:

```sh
# Optional AI Tool Stack Brewfile checksum: {{ includeTemplate "Brewfile.ai-cli-tools.tmpl" . | sha256sum }}
cat >"$ai_cli_brewfile" <<'BREWFILE'
{{ includeTemplate "Brewfile.ai-cli-tools.tmpl" . -}}
BREWFILE

if ! setup_brew_environment ||
  ! brew bundle --no-upgrade --file="$ai_cli_brewfile"; then
  record_ai_cli_failure
fi
```

Use cleanup traps for both temporary files. Do not inspect command presence to
skip the bundle. Keep first-run/routine exit behavior from the current
`finish_ai_cli_installers` contract, renamed for Homebrew-oriented wording.

In the Ubuntu bootstrap apt package list, render `file` and `procps` only when
the Optional AI Tool Stack is effective.

- [ ] **Step 5: Run focused tests and verify the common bundle passes**

Run the three commands from Step 2.

Expected: PASS; logs contain only Homebrew bootstrap/bundle operations, no
vendor AI installer URLs, and warning recovery tests preserve their exit
contracts.

- [ ] **Step 6: Commit the Homebrew installation slice**

```sh
git add Brewfile.ai-cli-tools.tmpl .chezmoiscripts \
  tests/chezmoiignore_test.sh tests/bootstrap_ubuntu_test.sh \
  tests/terrapod_command_test.sh
git commit -m "feat: install AI CLI tools with Homebrew"
```

---

### Task 3: Report conditional Homebrew readiness and legacy CLI shadowing

**Files:**
- Modify: `tests/terrapod_command_test.sh`
- Modify: `dot_local/bin/executable_terrapod`

**Interfaces:**
- Consumes: `effective_ai_cli_tools_enabled`, resolved `brew --prefix`, `command -v` for `agy`, `claude`, and `codex`.
- Produces: Conditional VPS `brew` key-tool status; shadowed-command list; status warning and doctor warning/guidance without automatic deletion.

- [ ] **Step 1: Add failing conditional readiness and provenance tests**

Add Ubuntu status/doctor fixtures for these cases:

1. AI stack disabled and no `brew`: output checks APT but does not mention
   missing Homebrew.
2. AI stack enabled and no `brew`: output shows `brew : missing` and a warning.
3. AI stack enabled with Homebrew commands under the stubbed prefix: no shadow
   warning.
4. AI stack enabled with `claude` resolving from `~/.local/bin`: status and
   doctor report `claude` as shadowing Homebrew and advise manual cleanup.

Use a brew stub that supports both `--prefix` and `shellenv`, and command stubs
whose absolute paths distinguish the Homebrew prefix from legacy locations.
Assert doctor remains warning-oriented for shadowing rather than treating it as
an automatic-removal or fatal-repair action.

- [ ] **Step 2: Run command tests and verify conditional checks fail**

Run:

```sh
sh tests/terrapod_command_test.sh
```

Expected: FAIL because VPS key-tool status currently checks only APT and AI
tool checks validate presence without provenance.

- [ ] **Step 3: Implement Homebrew provenance helpers and diagnostics**

Add focused helpers:

```sh
homebrew_prefix() {
  command -v brew >/dev/null 2>&1 || return 1
  brew --prefix 2>/dev/null
}

homebrew_managed_command() {
  command_path="$(command -v "$1" 2>/dev/null || true)"
  brew_prefix="$(homebrew_prefix 2>/dev/null || true)"
  [ -n "$command_path" ] && [ -n "$brew_prefix" ] &&
    case "$command_path" in
      "$brew_prefix"/*) return 0 ;;
      *) return 1 ;;
    esac
}

shadowed_ai_cli_tools() {
  shadowed=
  for tool in agy claude codex; do
    if command -v "$tool" >/dev/null 2>&1 &&
      ! homebrew_managed_command "$tool"; then
      shadowed="${shadowed:+$shadowed, }$tool"
    fi
  done
  printf '%s\n' "$shadowed"
}
```

When AI tooling is enabled, add `brew` to VPS key-tool display/warnings and
doctor checks. After missing-tool checks, print a shadowing warning such as:

```text
Optional AI Tool Stack has non-Homebrew commands shadowing managed casks: claude
```

Doctor guidance must tell the user to remove the legacy installation using its
original installer method, then open a new shell and rerun `tpod apply`. Never
delete, rename, or rewrite the legacy path.

- [ ] **Step 4: Run command tests and verify diagnostics pass**

Run:

```sh
sh tests/terrapod_command_test.sh
```

Expected: PASS for disabled/enabled VPS Homebrew readiness and all provenance
cases.

- [ ] **Step 5: Commit the diagnostic slice**

```sh
git add dot_local/bin/executable_terrapod tests/terrapod_command_test.sh
git commit -m "feat: diagnose shadowed Homebrew AI tools"
```

---

### Task 4: Record the architecture decision and update user documentation

**Files:**
- Create: `docs/adr/0008-use-homebrew-for-optional-ai-cli-tools.md`
- Modify: `CONTEXT.md`
- Modify: `README.md`
- Modify: `README.ko.md`
- Modify: `tests/readme_optional_stack_profiles_test.sh`
- Modify: `tests/readme_korean_test.sh`

**Interfaces:**
- Consumes: Implemented package tokens, managed setting name, Preset behavior, migration and upgrade commands.
- Produces: Authoritative ADR/domain vocabulary and aligned English/Korean user guidance.

- [ ] **Step 1: Write failing documentation assertions**

Update README tests to require:

```sh
assert_key_row_contains '`enableAiCliTools`' \
  'Homebrew casks `antigravity-cli`, `claude-code`, and `codex`' \
  "README documents Homebrew-owned Optional AI Tool Stack"
assert_key_row_contains '`enableMacosAppGroupDevelopmentApps`' \
  'Zed and Orca ADE' \
  "README documents development-apps membership"
assert_contains 'brew upgrade --cask claude-code codex antigravity-cli' \
  "README documents targeted AI CLI upgrades"
assert_contains 'enableMacosAppGroupAiApps' \
  "README documents explicit legacy-key migration"
```

Add equivalent natural-Korean assertions and remove old ai-apps membership
expectations. Keep the README heading parity test unchanged.

- [ ] **Step 2: Run README tests and verify they fail**

Run:

```sh
sh tests/readme_optional_stack_profiles_test.sh
sh tests/readme_korean_test.sh
```

Expected: FAIL because both READMEs still describe vendor installers and
ai-apps.

- [ ] **Step 3: Write ADR 0008 and update the domain model**

ADR 0008 must state:

- Homebrew owns the three Optional AI Tool Stack casks on both profiles.
- Linux Homebrew is conditional on the effective optional stack.
- APT remains Ubuntu's Bootstrap Package Manager and mise remains the general
  Modern CLI Provider.
- This decision supersedes ADR 0001's blanket Linuxbrew rejection and ADR
  0006's vendor-installer choice only for the Optional AI Tool Stack.
- Apply does not upgrade packages and disabling does not uninstall them.

Update `CONTEXT.md` relationships to remove vendor-installer claims, define the
narrow Homebrew exception, replace ai-apps with development-apps, and document
explicit config migration and legacy-command shadow warnings.

- [ ] **Step 4: Update the Canonical and Korean READMEs**

Document:

- the common AI CLI cask tokens;
- conditional Linux Homebrew installation;
- targeted upgrade commands;
- non-destructive legacy CLI migration and status/doctor warning;
- `development-apps` as Zed plus trusted Orca ADE;
- `enableMacosAppGroupDevelopmentApps` and the explicit old-key migration;
- Preset and Optional Development Workspace independence.

Remove current vendor-installer and ai-apps membership text. Keep English and
Korean headings identical and in the same order.

- [ ] **Step 5: Run README tests and verify documentation passes**

Run the two commands from Step 2.

Expected: PASS with heading parity and all new package/group assertions.

- [ ] **Step 6: Commit documentation**

```sh
git add docs/adr/0008-use-homebrew-for-optional-ai-cli-tools.md \
  CONTEXT.md README.md README.ko.md \
  tests/readme_optional_stack_profiles_test.sh tests/readme_korean_test.sh
git commit -m "docs: document Homebrew AI tool ownership"
```

---

### Task 5: Run repository-wide verification and review the change

**Files:**
- Verify: all modified files
- Test: all scripts under `tests/`

**Interfaces:**
- Consumes: Completed tasks 1-4.
- Produces: Fresh syntax, whitespace, focused behavior, full regression, and code-review evidence.

- [ ] **Step 1: Run shell syntax checks for changed executable templates**

Render both enabled profiles and run `sh -n` on the resulting AI installer,
desktop bootstrap, and Terrapod executable fixtures. Also run:

```sh
sh -n dot_local/bin/executable_terrapod
git diff --check
```

Expected: exit 0 with no syntax or whitespace diagnostics.

- [ ] **Step 2: Run every repository test**

Run:

```sh
for test_file in tests/*.sh; do
  sh "$test_file"
done
zsh tests/zshrc_zoxide_test.zsh
```

Expected: every command exits 0 and every test prints only `ok` results.

- [ ] **Step 3: Verify stale names and installer URLs are absent from active code**

Run:

```sh
rg -n 'enableMacosAppGroupAiApps|ai-apps|claude\.ai/install|chatgpt\.com/codex/install|antigravity\.google/cli/install' \
  Brewfile* .chezmoiscripts dot_local README.md README.ko.md CONTEXT.md tests
```

Expected: matches exist only in explicit legacy migration tests/guidance; no
active template, current option row, or command output uses stale names or
vendor AI installer URLs.

- [ ] **Step 4: Review the final diff against the design**

Compare every requirement in
`docs/superpowers/specs/2026-07-21-homebrew-ai-cli-and-development-apps-design.md`
with the final diff. Confirm conditional Linuxbrew, non-destructive migration,
warning recovery, provenance diagnostics, App Group membership, Preset behavior,
and documentation are each represented by both code and tests.

- [ ] **Step 5: Request focused code review and address findings**

Use the requesting-code-review template with the base commit before Task 1 and
the current HEAD. Fix every Critical and Important finding, rerun the affected
focused test, and then rerun the full commands from Steps 1-3.

- [ ] **Step 6: Record any final review fixes**

If review required changes:

```sh
git add -u
git commit -m "fix: address Homebrew AI migration review"
```

If review found no required changes, do not create an empty commit.
