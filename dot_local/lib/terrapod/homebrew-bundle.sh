#!/bin/sh

# Run a Brewfile once, then identify observed failures by retrying its original
# declarations individually. The caller owns the result file and warning text.
# Result records are: kind<TAB>name<TAB>macOS App Group (or "-").
terrapod_homebrew_bundle_records() {
  awk '
    /^[[:space:]]*#[[:space:]]+.*[[:space:]]macOS App Group[[:space:]]*$/ {
      group = $0
      sub(/^[[:space:]]*#[[:space:]]+/, "", group)
      sub(/[[:space:]]+macOS App Group[[:space:]]*$/, "", group)
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
  ' "$1"
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

terrapod_homebrew_bundle_join_names() {
  kind="$1"
  records="$2"
  awk -F '\t' -v kind="$kind" '
    $1 == kind {
      if (names != "") names = names ", "
      names = names $2
    }
    END { print names }
  ' "$records"
}

terrapod_homebrew_bundle_join_groups() {
  awk -F '\t' '
    $3 != "-" && $3 != "" && !seen[$3]++ {
      if (groups != "") groups = groups ", "
      groups = groups $3
    }
    END { print groups }
  ' "$1"
}
