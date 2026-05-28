# Rename the source repository to Terrapod

The Terrapod Source Repository will use `juty9026/terrapod` as its canonical GitHub slug. This aligns the repository URL, first-run installer, and issue tracker with Terrapod as the user-facing Dotfiles Management Tool instead of keeping `dotfiles` as the public identity after the Terrapod installer and management command work landed.

## Considered Options

- Keep `juty9026/dotfiles`: rejected because the repository would keep presenting the implementation category rather than the Terrapod product name.
- Rename to `juty9026/terrapod-dotfiles`: rejected because it preserves the legacy naming in the canonical slug without adding useful distinction for this personal tool.
- Support both old and new source URLs: rejected because there are no current external users to preserve and dual installation paths would make the first-run setup surface less clear.

## Consequences

- Public first-run installation references use `https://github.com/juty9026/terrapod.git`.
- Maintainer remotes may use `git@github.com:juty9026/terrapod.git`.
- The legacy `juty9026/dotfiles` slug is not a supported Terrapod installation path.
