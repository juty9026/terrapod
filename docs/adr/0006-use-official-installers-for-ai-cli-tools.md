# Use official installers for AI CLI tools

The **Optional AI Tool Stack** now installs Antigravity CLI, Claude Code, and Codex through each vendor's official native or standalone installer instead of managing all three as global npm packages through the mise-managed Node.js runtime. This follows current vendor installation guidance and replaces Gemini CLI with Antigravity CLI for the Google assistant slot. Terrapod's command output follows the same naming: setup describes Antigravity CLI, while status and doctor validate the `agy` binary. The transition remains non-destructive by leaving any existing npm-installed AI CLIs unmanaged rather than uninstalling them.

## Consequences

- Vendor installers may edit shell profiles during installation, so the AI CLI installer runs before the final chezmoi-managed shell files are applied.
- `terrapod setup`, `terrapod status`, and `terrapod doctor` keep the **Optional AI Tool Stack** command surface aligned with Antigravity CLI, Claude Code, and Codex.
- Existing `gemini` binaries may remain on machines but are no longer part of Terrapod's declared optional AI tool state.
