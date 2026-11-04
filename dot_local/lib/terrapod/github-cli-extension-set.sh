#!/bin/sh

# The GitHub CLI Extension Set is declared here once for the before-apply
# installer (which inlines this file) and checkout/installed status commands.
terrapod_github_cli_extension_set_members() {
  awk 'NF && $1 !~ /^#/ { print $1 }' <<'GH_EXTENSIONS'
github/gh-stack
GH_EXTENSIONS
}

# Accept the caller's selected gh executable. Emit owner/repo|available or
# owner/repo|missing in declaration order. A failed list is an empty observation,
# as before; installation and warning/exit policy belong to the caller.
terrapod_observe_github_cli_extension_set() (
  installed_extensions="$("$1" extension list 2>/dev/null || true)"
  terrapod_github_cli_extension_set_members | while IFS= read -r extension; do
    if printf '%s\n' "$installed_extensions" | awk -v want="$extension" '
      {
        for (i = 1; i <= NF; i++) {
          if ($i == want) found = 1
        }
      }
      END { exit !found }
    '; then
      printf '%s|available\n' "$extension"
    else
      printf '%s|missing\n' "$extension"
    fi
  done
)
