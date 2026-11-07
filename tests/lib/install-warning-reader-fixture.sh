# Load the production observation logic with a deterministic filesystem adapter.
. "${TERRAPOD_TEST_WARNING_LIB:?}"

terrapod_install_warning_read_content() {
  case "${TERRAPOD_TEST_WARNING_READ_MODE:-read}" in
    delete)
      /bin/rm -f "$1"
      awk '{ print }' "$1"
      ;;
    fail)
      # A read error may arrive after partial content; none of it is trustworthy.
      printf '%s\n' "summary='partial summary'"
      return 73
      ;;
    replace)
      fixture_content="$(awk '{ print }' "$1")" || return 1
      terrapod_install_warning_write "${1##*/}" "replacement summary" "replacement guidance" || return 1
      printf '%s\n' "$fixture_content"
      ;;
    *) awk '{ print }' "$1" ;;
  esac
}
