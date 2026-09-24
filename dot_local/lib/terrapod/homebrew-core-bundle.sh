#!/bin/sh

TERRAPOD_HOMEBREW_CORE_BUNDLE_LOADED=1
TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT=

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

terrapod_homebrew_core_run_bundle() {
  TERRAPOD_HOMEBREW_CORE_FAILURE_GUIDANCE_TEXT="Review Homebrew core bundle output, fix package access, then rerun tpod apply."
  core_failures_file="$(mktemp "${TMPDIR:-/tmp}/terrapod-core-failures.XXXXXX")" || return 1
  if terrapod_homebrew_bundle_run brew "$1" "$core_failures_file"; then
    rm -f "$core_failures_file"
    return 0
  fi

  failed_formulae="$(terrapod_homebrew_bundle_join_names brew "$core_failures_file")"
  failed_casks="$(terrapod_homebrew_bundle_join_names cask "$core_failures_file")"
  rm -f "$core_failures_file"
  detail=
  if [ -n "$failed_formulae" ]; then
    detail="failed formulae: $failed_formulae"
  fi
  if [ -n "$failed_casks" ]; then
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
  return 1
}
