# Executed by write_gum_stub's wrapper with the reader and schema loaded.
log_file="${TERRAPOD_GUM_LOG:-${TERRAPOD_STUB_CALL_LOG:?}}"
responses_file="${TERRAPOD_GUM_RESPONSES:?}"
printf '%s' 'gum args:' >>"$log_file"
for arg do printf ' %s' "$arg" >>"$log_file"; done
printf '\n' >>"$log_file"

response_for() {
  response_key="$1"
  printf 'gum question: %s\n' "$response_key" >>"$log_file"
  response="$(awk -v key="$response_key" '
    index($0, "=") && substr($0, 1, index($0, "=") - 1) == key {
      print substr($0, index($0, "=") + 1)
      found = 1
      exit
    }
    END { if (!found) print "__DEFAULT__" }
  ' "$responses_file")"
}

confirm_default_status() {
  for arg do
    case "$arg" in --default=true) return 0 ;; --default=false) return 1 ;; esac
  done
  return 1
}

case "${1:-}" in
  --version) printf '%s\n' 'gum test stub' ;;
  choose)
    if [ "${2:-}" = --help ]; then
      printf '%s\n' 'Usage: gum choose [<options> ...] [flags]' '      --label-delimiter=""'
      exit 0
    fi
    while IFS= read -r option; do printf 'gum stdin: %s\n' "$option" >>"$log_file"; done
    response_for preset
    case "$response" in
      ''|__DEFAULT__|__CANCEL__) exit 130 ;;
      __ERROR__) printf '%s\n' 'simulated gum operational failure' >&2; exit 2 ;;
    esac
    printf '%s\n' "$response"
    ;;
  confirm)
    case "${2:-}" in
      'Write these Terrapod settings to '*) response_for write ;;
      *)
        question_key="$(managed_setting_schema | awk -F '|' -v prompt="${2:-}" '$3 != "" && prompt == "Enable " $3 "?" { print $1 }')"
        if [ -z "$question_key" ]; then
          printf 'unexpected gum question: %s\n' "${2:-}" >&2
          exit 2
        fi
        response_for "$question_key"
        case "$response" in
          ''|__DEFAULT__) confirm_default_status "$@"; exit $? ;;
        esac
        ;;
    esac
    case "$response" in
      yes|y|true|enabled) exit 0 ;;
      no|n|false|disabled) exit 1 ;;
      ''|__DEFAULT__|__CANCEL__) exit 130 ;;
      __ERROR__) printf '%s\n' 'simulated gum operational failure' >&2; exit 2 ;;
      *) printf 'unexpected gum confirm response: %s\n' "$response" >&2; exit 2 ;;
    esac
    ;;
  style) shift; for arg do case "$arg" in --*) ;; *) printf '%s\n' "$arg" ;; esac; done ;;
  *) printf 'unexpected gum command: %s\n' "${1:-}" >&2; exit 2 ;;
esac
