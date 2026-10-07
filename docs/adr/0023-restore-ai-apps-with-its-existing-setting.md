# Restore ai-apps with its existing setting

Restore the ai-apps macOS App Group for Claude, ChatGPT including Codex, and
Gemini using `enableMacosAppGroupAiApps`. Keeping the original key preserves
the user's earlier group selection instead of requiring a differently named
group. An existing `true` value intentionally selects the new three-app
membership on the next apply; it does not enable development-apps or AI CLIs.

The workstation Preset enables the group, while minimal and development leave
it disabled. Configs that no longer contain the key require explicit setup or
configure, following the existing managed config completeness policy.

Gemini requires Apple Silicon and macOS 15 or later. Exclude it from the
effective installation bundle on unsupported Macs before bulk installation
or individual retries, and report the exclusion without an install warning.
Claude and ChatGPT remain selected.

## Considered Options

- Create a new ai-assistants key: rejected in favor of restoring the familiar
  ai-apps selection, accepting the changed membership of old true values.
- Add the apps to development-apps: rejected because conversational assistants
  should remain independently selectable from developer desktop tools.
- Let Homebrew reject unsupported Gemini installs: rejected because that
  creates a recurring install warning for a machine that cannot install it.
