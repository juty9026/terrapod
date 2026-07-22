#!/bin/sh

TERRAPOD_INSTALL_WARNINGS_LOADED=1

terrapod_install_warning_categories() {
  printf '%s\n' \
    homebrew-core \
    homebrew-macos-platform \
    homebrew-desktop-apps \
    ubuntu-bootstrap \
    shell-integrations \
    mise-tools \
    optional-ai-cli-tools
}

terrapod_install_warning_is_category() {
  case "$1" in
    homebrew-core|homebrew-macos-platform|homebrew-desktop-apps|ubuntu-bootstrap|shell-integrations|mise-tools|optional-ai-cli-tools)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
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
  write_id="$$:${tmp_file##*/}"
  {
    printf 'category=%s\n' "$(terrapod_install_warning_quote "$category")"
    printf 'summary=%s\n' "$(terrapod_install_warning_quote "$summary")"
    printf 'guidance=%s\n' "$(terrapod_install_warning_quote "$guidance")"
    printf 'updated_at=%s\n' "$(terrapod_install_warning_quote "$(terrapod_install_warning_now)")"
    printf 'write_id=%s\n' "$(terrapod_install_warning_quote "$write_id")"
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
