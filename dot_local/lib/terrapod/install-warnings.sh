#!/bin/sh

# Terrapod install warning markers.
#
# The install warning directory is Terrapod-owned. Its only valid contents are
# one regular file per current category, the legacy aliases Terrapod still
# reads, and the staging files described below. Everything else is reclaimed by
# `terrapod_install_warning_prune`.
#
# Staging file lifecycle:
#
#   1. `terrapod_install_warning_write` creates `.<category>.XXXXXX` with
#      `mktemp`, fills it, and renames it over `<category>`. That rename is what
#      makes a marker write atomic at the category file level.
#   2. Between the `mktemp` and the `mv` the staging file is *in flight*. The
#      prune glob `"$marker_dir"/*` cannot match a leading dot, so a concurrent
#      prune leaves it alone.
#   3. A write that dies inside that window - the installers this runs inside
#      are routinely interrupted - leaves the staging file behind with no owner.
#      `terrapod_install_warning_prune` reclaims dot-prefixed staging files that
#      no `mv` can still be waiting on, meaning a modification time older than
#      `TERRAPOD_INSTALL_WARNING_STAGING_MAX_AGE_DAYS` day. Younger ones stay,
#      because a live write may still own them.
#
# No staging file therefore outlives the day it was created, and no in-flight
# write can be pruned out from under itself.

TERRAPOD_INSTALL_WARNINGS_LOADED=1

# `find -mtime +N` selects modification times older than N + 1 days, so the
# threshold below is expressed as the exclusive day count the reclaim uses.
TERRAPOD_INSTALL_WARNING_STAGING_MAX_AGE_DAYS=1

terrapod_install_warning_categories() {
  printf '%s\n' \
    homebrew-core \
    homebrew-desktop-apps \
    ubuntu-bootstrap \
    shell-integrations \
    mise-tools \
    optional-ai-cli-tools \
    jetendard-font \
    jetendard-settings
}

terrapod_install_warning_is_category() {
  wanted_category="$1"

  for known_category in $(terrapod_install_warning_categories); do
    if [ "$known_category" = "$wanted_category" ]; then
      return 0
    fi
  done

  return 1
}

terrapod_install_warning_dir() {
  if [ -n "${XDG_STATE_HOME:-}" ]; then
    printf '%s\n' "$XDG_STATE_HOME/terrapod/install-warnings"
  else
    printf '%s\n' "$HOME/.local/state/terrapod/install-warnings"
  fi
}

terrapod_install_warning_path() {
  category="$1"

  if ! terrapod_install_warning_is_category "$category"; then
    printf '%s\n' "terrapod install warning: unknown category: $category" >&2
    return 1
  fi

  printf '%s/%s\n' "$(terrapod_install_warning_dir)" "$category"
}

terrapod_install_warning_legacy_path() {
  category="$1"

  case "$category" in
    optional-ai-cli-tools)
      printf '%s/%s\n' "$(terrapod_install_warning_dir)" ai-cli-tools
      ;;
    *)
      return 1
      ;;
  esac
}

terrapod_install_warning_existing_path() {
  category="$1"

  marker_path="$(terrapod_install_warning_path "$category")" || return 1
  if [ -f "$marker_path" ]; then
    printf '%s\n' "$marker_path"
    return 0
  fi

  legacy_marker_path="$(terrapod_install_warning_legacy_path "$category")" || return 1
  if [ -f "$legacy_marker_path" ]; then
    printf '%s\n' "$legacy_marker_path"
    return 0
  fi

  return 1
}

terrapod_install_warning_path_is_legacy() {
  category="$1"
  marker_path="$2"

  legacy_marker_path="$(terrapod_install_warning_legacy_path "$category")" || return 1
  [ "$marker_path" = "$legacy_marker_path" ]
}

terrapod_install_warning_quote() {
  printf "'"
  printf '%s' "$1" | sed "s/'/'\\\\''/g"
  printf "'"
}

terrapod_install_warning_reject_multiline() {
  value="$1"
  label="$2"

  case "$value" in
    *'
'*)
      printf '%s\n' "terrapod install warning: $label must be single-line" >&2
      return 1
      ;;
  esac
}

terrapod_install_warning_now() {
  date -u '+%Y-%m-%dT%H:%M:%SZ'
}

terrapod_install_warning_write() {
  category="$1"
  summary="$2"
  guidance="$3"

  if ! terrapod_install_warning_is_category "$category"; then
    printf '%s\n' "terrapod install warning: unknown category: $category" >&2
    return 1
  fi
  terrapod_install_warning_reject_multiline "$summary" summary || return 1
  terrapod_install_warning_reject_multiline "$guidance" guidance || return 1

  marker_dir="$(terrapod_install_warning_dir)"
  marker_path="$marker_dir/$category"
  mkdir -p "$marker_dir" || return 1

  tmp_file="$(mktemp "$marker_dir/.$category.XXXXXX")" || return 1
  {
    printf 'category=%s\n' "$(terrapod_install_warning_quote "$category")"
    printf 'summary=%s\n' "$(terrapod_install_warning_quote "$summary")"
    printf 'guidance=%s\n' "$(terrapod_install_warning_quote "$guidance")"
    printf 'updated_at=%s\n' "$(terrapod_install_warning_quote "$(terrapod_install_warning_now)")"
  } >"$tmp_file" || {
    rm -f "$tmp_file"
    return 1
  }

  if ! mv "$tmp_file" "$marker_path"; then
    rm -f "$tmp_file"
    return 1
  fi

  legacy_marker_path="$(terrapod_install_warning_legacy_path "$category" 2>/dev/null || true)"
  if [ -n "$legacy_marker_path" ]; then
    rm -f "$legacy_marker_path"
  fi
}

terrapod_install_warning_clear() {
  category="$1"

  marker_path="$(terrapod_install_warning_path "$category")" || return 1
  rm -f "$marker_path"

  legacy_marker_path="$(terrapod_install_warning_legacy_path "$category")" || return 0
  rm -f "$legacy_marker_path"
}

terrapod_install_warning_known_names() {
  for category in $(terrapod_install_warning_categories); do
    printf '%s\n' "$category"

    legacy_path="$(terrapod_install_warning_legacy_path "$category" 2>/dev/null)" || continue
    printf '%s\n' "${legacy_path##*/}"
  done
}

terrapod_install_warning_staging_is_abandoned() {
  staging_path="$1"

  [ -n "$(find "$staging_path" -mtime "+$((TERRAPOD_INSTALL_WARNING_STAGING_MAX_AGE_DAYS - 1))" 2>/dev/null)" ]
}

terrapod_install_warning_prune() {
  marker_dir="$(terrapod_install_warning_dir)"
  [ -d "$marker_dir" ] || return 0

  known_names="$(terrapod_install_warning_known_names)"
  prune_status=0

  for marker_path in "$marker_dir"/*; do
    [ -f "$marker_path" ] || continue

    marker_name="${marker_path##*/}"
    if printf '%s\n' "$known_names" | grep -Fx -- "$marker_name" >/dev/null; then
      continue
    fi

    if rm -f "$marker_path"; then
      printf '%s\n' "$marker_name"
    else
      prune_status=1
    fi
  done

  for staging_path in "$marker_dir"/.*.??????; do
    [ -f "$staging_path" ] || continue
    terrapod_install_warning_staging_is_abandoned "$staging_path" || continue

    staging_name="${staging_path##*/}"
    if rm -f "$staging_path"; then
      printf '%s\n' "$staging_name"
    else
      prune_status=1
    fi
  done

  return "$prune_status"
}

terrapod_install_warning_list() {
  for category in $(terrapod_install_warning_categories); do
    if terrapod_install_warning_existing_path "$category" >/dev/null; then
      printf '%s\n' "$category"
    fi
  done
}

terrapod_install_warning_read() {
  category="$1"

  marker_path="$(terrapod_install_warning_existing_path "$category")" || return 1
  if terrapod_install_warning_path_is_legacy "$category" "$marker_path"; then
    awk -F= -v category="$category" '
      $1 == "category" {
        printf "category=\047%s\047\n", category
        next
      }

      {
        print
      }
    ' "$marker_path"
    return
  fi

  cat "$marker_path"
}

terrapod_install_warning_value() {
  category="$1"
  field="$2"

  marker_path="$(terrapod_install_warning_existing_path "$category")" || return 1

  case "$field" in
    category|summary|guidance|updated_at)
      ;;
    *)
      printf '%s\n' "terrapod install warning: unknown field: $field" >&2
      return 1
      ;;
  esac

  if [ "$field" = category ] && terrapod_install_warning_path_is_legacy "$category" "$marker_path"; then
    printf '%s\n' "$category"
    return
  fi

  awk -F= -v wanted="$field" '
    $1 == wanted {
      value = $0
      sub("^[^=]*=", "", value)
      if (value ~ /^\047.*\047$/) {
        sub(/^\047/, "", value)
        sub(/\047$/, "", value)
        gsub(/\047\\\047\047/, "\047", value)
      }
      print value
      found = 1
      exit
    }

    END {
      exit found ? 0 : 1
    }
  ' "$marker_path"
}
