# Plain Terrapod Setup Design

## Goal

Issue #56 adds the first usable `terrapod setup` command. The command is a plain terminal prompt flow that detects the machine profile, asks for a valid Preset, shows the concrete machine-local settings that Preset will write, asks for final confirmation, and writes the same managed chezmoi data keys as `terrapod configure <Preset>`.

## Scope

This slice covers the Terrapod command surface only. It does not replace the first-run installer prompt yet, and it does not add rich TTY controls or per-setting customization. Those are later Terrapod Setup slices described in `CONTEXT.md`.

## User Flow

`terrapod setup` accepts no arguments. It prints a heading, the detected profile label, and the target config path. It prompts on stderr for a Preset from the profile-specific available choices:

- macOS Terminal Profile: `minimal`, `development`, or `workstation`
- VPS Shell Profile: `minimal` or `development`

After the user enters a Preset, setup validates it with the same profile policy as `configure <Preset>`. Invalid input exits with usage status and does not write config. A VPS Shell Profile cannot complete setup with `workstation`.

For a valid Preset, setup prints a concrete settings summary generated from `render_preset_data`. The summary lists each managed Terrapod data key and its boolean value. Setup then asks for final confirmation. Only `y`, `Y`, `yes`, or `YES` writes config. Any other answer, including an empty answer or EOF, exits without writing, backing up, or leaving Terrapod temp artifacts.

## Architecture

The implementation stays inside `dot_local/bin/executable_terrapod`, matching the current POSIX shell command architecture. It reuses:

- `current_profile` and `profile_context_label` for detection and display
- `available_preset_args`, `known_presets`, `is_preset_available_for_profile`, and `validate_preset_for_profile` for Preset policy
- `render_preset_data` as the single source of concrete settings
- `reject_unsupported_managed_config` and `write_managed_config` for safe config writes

`write_preset_settings` remains the script-friendly `configure` path with its existing existing-config confirmation. `setup` uses a new flow-specific write wrapper so final confirmation is the only setup confirmation before a write.

## Error Handling

`setup` rejects extra arguments with `fail_usage`. EOF or cancellation at Preset selection exits with a clear error and no write. Invalid Presets use the existing usage error behavior. Unsupported config formats are rejected before final confirmation, preserving existing conservative config-write safety.

## Tests

Tests cover user-visible behavior through shell integration tests:

- help output lists `setup`
- macOS setup shows profile, available Presets, concrete summary, and final confirmation before writing
- VPS setup cannot complete with `workstation`
- confirmed setup writes the same concrete data keys as the selected Preset
- cancellation before final confirmation leaves new and existing config paths unchanged and creates no Terrapod artifacts

## Self-Review

- No incomplete markers remain.
- Scope is focused on Issue #56 acceptance criteria.
- The design explicitly keeps concrete Preset settings shared with `configure <Preset>`.
- Cancellation semantics happen before any write or backup.
