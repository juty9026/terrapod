#!/bin/sh

# Run a Brewfile once, then identify observed failures by retrying its original
# declarations individually. The bundle step owns this internal record format.
# Result records are: kind<TAB>name<TAB>macOS App Group (or "-")<TAB>declaration.
# A machine-readable "# terrapod-macos-app-group: <setting-key>" annotation
# applies to subsequent declarations until the next annotation. Resolve only
# macOS App Group keys from managed_setting_schema; absent schema or unknown
# keys leave items unattributed. Ordinary comments never supply group identity.
terrapod_homebrew_bundle_records() {
  {
    if command -v managed_setting_schema >/dev/null 2>&1; then
      managed_setting_schema
    fi
  } | awk -F "|" '
    FILENAME == "-" {
      if ($2 == "macos-app-group") group_names[$1] = $3
      next
    }

    /^[[:space:]]*#[[:space:]]*terrapod-macos-app-group:/ {
      key = $0
      sub(/^[[:space:]]*#[[:space:]]*terrapod-macos-app-group:[[:space:]]*/, "", key)
      sub(/[[:space:]]+$/, "", key)
      group = group_names[key]
      next
    }

    /^[[:space:]]*(brew|cask)[[:space:]]+"/ {
      declaration = $0
      sub(/^[[:space:]]+/, "", declaration)
      sub(/[[:space:]]+$/, "", declaration)
      kind = declaration
      sub(/[[:space:]].*$/, "", kind)
      name = declaration
      sub(/^(brew|cask)[[:space:]]+"/, "", name)
      sub(/".*$/, "", name)
      if (name != "") {
        group_field = group
        if (group_field == "") group_field = "-"
        printf "%s\t%s\t%s\t%s\n", kind, name, group_field, declaration
      }
    }
  ' - "$1"
}

terrapod_homebrew_bundle_cleanup() {
  [ -z "${terrapod_bundle_records_file:-}" ] || rm -f "$terrapod_bundle_records_file"
  [ -z "${terrapod_bundle_item_file:-}" ] || rm -f "$terrapod_bundle_item_file"
}

terrapod_homebrew_bundle_run() {
  terrapod_bundle_brew="$1"
  terrapod_bundle_brewfile="$2"
  terrapod_bundle_failures="$3"
  : >"$terrapod_bundle_failures" || return 1

  if HOMEBREW_NO_AUTO_UPDATE=1 "$terrapod_bundle_brew" bundle --no-upgrade --file="$terrapod_bundle_brewfile"; then
    return 0
  fi

  terrapod_bundle_records_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-bundle-records.XXXXXX")" || return 1
  terrapod_bundle_item_file=
  if ! terrapod_homebrew_bundle_records "$terrapod_bundle_brewfile" >"$terrapod_bundle_records_file"; then
    terrapod_homebrew_bundle_cleanup
    return 1
  fi

  terrapod_bundle_tab="$(printf '\t')"
  # fd 3 prevents a brew process that reads stdin from consuming later records.
  while IFS="$terrapod_bundle_tab" read -r terrapod_bundle_kind terrapod_bundle_name terrapod_bundle_group terrapod_bundle_declaration <&3; do
    terrapod_bundle_item_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-bundle-item.XXXXXX")" || {
      terrapod_homebrew_bundle_cleanup
      return 1
    }
    if ! printf '%s\n' "$terrapod_bundle_declaration" >"$terrapod_bundle_item_file"; then
      terrapod_homebrew_bundle_cleanup
      return 1
    fi
    if ! HOMEBREW_NO_AUTO_UPDATE=1 "$terrapod_bundle_brew" bundle --no-upgrade --file="$terrapod_bundle_item_file"; then
      if ! printf '%s\t%s\t%s\n' "$terrapod_bundle_kind" "$terrapod_bundle_name" "$terrapod_bundle_group" >>"$terrapod_bundle_failures"; then
        terrapod_homebrew_bundle_cleanup
        return 1
      fi
    fi
    rm -f "$terrapod_bundle_item_file"
    terrapod_bundle_item_file=
  done 3<"$terrapod_bundle_records_file"

  terrapod_homebrew_bundle_cleanup
  return 1
}

# Public bundle step: status is the installation outcome, and failure detail is
# already attributed for category-specific recovery wording. Brew output remains
# visible on its original streams. Success resets the detail to an empty string;
# setup errors and unattributed bulk failures share the "Homebrew bundle" fallback.
# Arguments: brew executable, source Brewfile, hardware architecture.
terrapod_homebrew_bundle_step() {
  TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL="Homebrew bundle"
  terrapod_step_dir="$(mktemp -d "${TMPDIR:-/tmp}/terrapod-bundle-step.XXXXXX")" || return 1
  terrapod_step_status=1

  # Applicability must not rewrite the caller's source Brewfile.
  if cp "$2" "$terrapod_step_dir/Brewfile" &&
     terrapod_homebrew_bundle_filter_gemini "$terrapod_step_dir/Brewfile" "$3"; then
    if (export TMPDIR="$terrapod_step_dir"; terrapod_homebrew_bundle_run "$1" "$terrapod_step_dir/Brewfile" "$terrapod_step_dir/failures"); then
      TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL=
      terrapod_step_status=0
    else
      if terrapod_step_detail="$(awk -F '\t' '
        $1 == "brew" { formulae = formulae (formulae == "" ? "" : ", ") $2 }
        $1 == "cask" { casks = casks (casks == "" ? "" : ", ") $2 }
        $3 != "-" && $3 != "" && !seen[$3]++ {
          groups = groups (groups == "" ? "" : ", ") $3
        }
        END {
          if (formulae != "") detail = "failed formulae: " formulae
          if (casks != "") detail = detail (detail == "" ? "" : "; ") "failed casks: " casks
          if (detail != "" && groups != "") detail = detail "; App Groups: " groups
          print detail
        }
      ' "$terrapod_step_dir/failures")"; then
        TERRAPOD_HOMEBREW_BUNDLE_FAILURE_DETAIL="${terrapod_step_detail:-Homebrew bundle}"
      fi
    fi
  fi

  rm -rf "$terrapod_step_dir"
  return "$terrapod_step_status"
}

# Filter before both bulk installation and per-package retries. Homebrew's
# package listing then sees the same effective declarations as the installer.
terrapod_homebrew_bundle_filter_gemini() {
  gemini_brewfile="$1"
  gemini_arch="$2"
  if ! grep -Fx 'cask "google-gemini"' "$gemini_brewfile" >/dev/null; then
    return 0
  fi

  gemini_macos_version="$(sw_vers -productVersion 2>/dev/null || true)"
  gemini_macos_major="${gemini_macos_version%%.*}"
  case "$gemini_arch:$gemini_macos_major" in
    arm64:*|aarch64:*)
      case "$gemini_macos_major" in
        ''|*[!0-9]*) ;;
        *) [ "$gemini_macos_major" -ge 15 ] && return 0 ;;
      esac
      ;;
  esac

  gemini_filtered_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-gemini-filter.XXXXXX")" || return 1
  if ! sed '/^cask "google-gemini"$/d' "$gemini_brewfile" >"$gemini_filtered_file" ||
     ! mv "$gemini_filtered_file" "$gemini_brewfile"; then
    rm -f "$gemini_filtered_file"
    return 1
  fi
  printf '%s\n' 'Skipping google-gemini: not applicable (requires Apple Silicon and macOS 15 or later).'
}
