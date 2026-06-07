#!/bin/sh

TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED=1
TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT=

terrapod_homebrew_core_join_lines_with_commas() {
  awk '
    NF {
      if (joined == "") {
        joined = $0
      } else {
        joined = joined ", " $0
      }
    }

    END {
      print joined
    }
  ' "$1"
}

terrapod_homebrew_core_item_records() {
  brewfile="$1"

  awk '
    /^[[:space:]]*brew[[:space:]]+"/ {
      item = $0
      sub(/^[[:space:]]*brew[[:space:]]+"/, "", item)
      sub(/".*$/, "", item)
      if (item != "") {
        printf "formula\t%s\n", item
      }
      next
    }

    /^[[:space:]]*cask[[:space:]]+"/ {
      item = $0
      sub(/^[[:space:]]*cask[[:space:]]+"/, "", item)
      sub(/".*$/, "", item)
      if (item != "") {
        printf "cask\t%s\n", item
      }
    }
  ' "$brewfile"
}

terrapod_homebrew_core_permission_guidance() {
  prefix="$(brew --prefix 2>/dev/null || true)"

  if [ -n "$prefix" ] && [ -e "$prefix" ] && [ ! -w "$prefix" ]; then
    printf '%s\n' "Homebrew prefix is not writable: $prefix. Fix Homebrew permissions for your user or ask the prefix owner/admin; avoid broad ownership changes."
    return
  fi

  if [ -n "$prefix" ]; then
    printf '%s\n' "If this was a permissions failure, check Homebrew permissions under $prefix without broad ownership changes."
    return
  fi

  printf '%s\n' "If this was a permissions failure, check Homebrew prefix permissions without broad ownership changes."
}

terrapod_homebrew_core_cleanup_temps() {
  [ -z "${core_records_file:-}" ] || rm -f "$core_records_file"
  [ -z "${failed_formulae_file:-}" ] || rm -f "$failed_formulae_file"
  [ -z "${failed_casks_file:-}" ] || rm -f "$failed_casks_file"
  [ -z "${single_core_brewfile:-}" ] || rm -f "$single_core_brewfile"
}

terrapod_homebrew_core_failure_guidance_from_files() {
  detail=

  if [ -s "$failed_formulae_file" ]; then
    failed_formulae="$(terrapod_homebrew_core_join_lines_with_commas "$failed_formulae_file")" || return 1
    detail="failed formulae: $failed_formulae"
  fi

  if [ -s "$failed_casks_file" ]; then
    failed_casks="$(terrapod_homebrew_core_join_lines_with_commas "$failed_casks_file")" || return 1
    if [ -n "$detail" ]; then
      detail="$detail; failed casks: $failed_casks"
    else
      detail="failed casks: $failed_casks"
    fi
  fi

  permission_guidance="$(terrapod_homebrew_core_permission_guidance)"

  if [ -n "$detail" ]; then
    TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output for $detail. $permission_guidance Then rerun tpod apply."
  else
    TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output, fix package access, then rerun tpod apply. $permission_guidance"
  fi
}

terrapod_homebrew_core_run_bundle() {
  brewfile="$1"
  TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output, fix package access, then rerun tpod apply."

  if brew bundle --no-upgrade --file="$brewfile"; then
    return 0
  fi

  core_records_file=
  failed_formulae_file=
  failed_casks_file=
  single_core_brewfile=

  core_records_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-core-records.XXXXXX")" || return 1
  failed_formulae_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-core-failed-formulae.XXXXXX")" || {
    terrapod_homebrew_core_cleanup_temps
    return 1
  }
  failed_casks_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-core-failed-casks.XXXXXX")" || {
    terrapod_homebrew_core_cleanup_temps
    return 1
  }

  if ! terrapod_homebrew_core_item_records "$brewfile" >"$core_records_file"; then
    terrapod_homebrew_core_cleanup_temps
    return 1
  fi

  if [ ! -s "$core_records_file" ]; then
    terrapod_homebrew_core_failure_guidance_from_files || {
      terrapod_homebrew_core_cleanup_temps
      return 1
    }
    terrapod_homebrew_core_cleanup_temps
    return 1
  fi

  tab="$(printf '\t')"
  while IFS="$tab" read -r item_kind item_name; do
    single_core_brewfile="$(mktemp "${TMPDIR:-/tmp}/terrapod-homebrew-core-item.XXXXXX")" || {
      terrapod_homebrew_core_cleanup_temps
      return 1
    }

    case "$item_kind" in
      formula)
        printf 'brew "%s"\n' "$item_name" >"$single_core_brewfile" || {
          terrapod_homebrew_core_cleanup_temps
          return 1
        }
        if ! brew bundle --no-upgrade --file="$single_core_brewfile"; then
          printf '%s\n' "$item_name" >>"$failed_formulae_file" || {
            terrapod_homebrew_core_cleanup_temps
            return 1
          }
        fi
        ;;
      cask)
        printf 'cask "%s"\n' "$item_name" >"$single_core_brewfile" || {
          terrapod_homebrew_core_cleanup_temps
          return 1
        }
        if ! brew bundle --no-upgrade --file="$single_core_brewfile"; then
          printf '%s\n' "$item_name" >>"$failed_casks_file" || {
            terrapod_homebrew_core_cleanup_temps
            return 1
          }
        fi
        ;;
    esac

    rm -f "$single_core_brewfile"
    single_core_brewfile=
  done <"$core_records_file"

  terrapod_homebrew_core_failure_guidance_from_files || {
    terrapod_homebrew_core_cleanup_temps
    return 1
  }

  terrapod_homebrew_core_cleanup_temps
  return 1
}
