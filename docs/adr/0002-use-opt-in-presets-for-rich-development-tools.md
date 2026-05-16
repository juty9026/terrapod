# Use opt-in presets for rich development tools

Rich editor configuration, assistant CLI tools, and development-specific terminal layouts are excluded from every machine profile by default so low-memory VPS machines keep a small Core Shell Stack. `enableEditorStack` and `enableAiCliTools` remain independently controllable leaf flags, while `enableDevelopmentWorkspace` is a full development preset that also enables both leaf stacks and the development Zellij layout. Disabling an optional stack only excludes its files from chezmoi management; it does not remove files that already exist on a machine.

## Considered Options

- Keep LazyVim and the development Zellij layout in the default shell profile: rejected because a 2 GB RAM VPS can fail under the startup and plugin load.
- Use only independent leaf flags: rejected because a user asking for a full development environment should not have to discover and enable every lower-level stack.
- Let disabled leaf flags override the full workspace preset: rejected because it makes `false` mean both "default off" and "explicitly block this preset."
