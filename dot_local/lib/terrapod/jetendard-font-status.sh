#!/bin/sh

# Maps an executable_jetendard-font exit status to install warning guidance.
# The helper exits 2 for a GitHub API rate limit, 3 for an unreachable GitHub,
# and 4 for a release it cannot use; every other non-zero status keeps the
# generic guidance. Both the install and the retry wrapper read this, so the
# two markers cannot drift apart.

terrapod_jetendard_font_guidance() {
  case "$1" in
    2)
      printf '%s\n' "GitHub API rate limit reached while resolving the Jetendard release. Export a temporary GITHUB_TOKEN or run gh auth login, then rerun tpod apply."
      ;;
    3)
      printf '%s\n' "GitHub was unreachable while resolving the Jetendard release. Restore network access, then rerun tpod apply."
      ;;
    4)
      printf '%s\n' "The latest Jetendard GitHub release is not installable. Wait for a corrected upstream release, then rerun tpod apply."
      ;;
    *)
      printf '%s\n' "Restore Python and GitHub access, then rerun tpod apply."
      ;;
  esac
}
