# Tokens stay machine-local in ~/.config/op/sa; only these functions are managed.
function opx() {
  emulate -L zsh
  local name="${1-}" token
  if (( $# < 2 )); then
    print -u2 -- 'usage: opx <name> <op arguments...>'
    return 64
  fi
  if [[ ! "$name" =~ '^[A-Za-z0-9][A-Za-z0-9_-]*$' ]]; then
    print -u2 -- 'opx: invalid account name; use [A-Za-z0-9][A-Za-z0-9_-]*'
    return 64
  fi
  local token_file="$HOME/.config/op/sa/$name"
  if [[ -L "$token_file" || ! -f "$token_file" || ! -r "$token_file" ]] ||
      ! token="$(<"$token_file")" || [[ -z "$token" ]]; then
    print -u2 -- "opx: no usable token for '$name'; run: opx-add $name"
    return 1
  fi
  if ! whence -p op >/dev/null; then
    print -u2 -- 'opx: 1Password CLI (op) is not installed; install it and retry'
    return 127
  fi
  shift
  OP_SERVICE_ACCOUNT_TOKEN="$token" command op "$@"
}

function opx-add() (
  emulate -L zsh
  local name="${1-}" token confirmation temp_file=''
  if (( $# != 1 )); then
    print -u2 -- 'usage: opx-add <name>'
    return 64
  fi
  if [[ ! "$name" =~ '^[A-Za-z0-9][A-Za-z0-9_-]*$' ]]; then
    print -u2 -- 'opx-add: invalid account name; use [A-Za-z0-9][A-Za-z0-9_-]*'
    return 64
  fi
  local token_dir="$HOME/.config/op/sa"
  local token_file="$token_dir/$name"
  if [[ -L "$token_dir" || -L "$token_file" ||
        ( -e "$token_file" && ! -f "$token_file" ) ]]; then
    print -u2 -- 'opx-add: unsafe token path; remove the symlink or non-file target and retry'
    return 1
  fi
  if [[ -e "$token_file" ]]; then
    print -nu2 -- "Replace token for $name? [y/N] "
    if ! IFS= read -r confirmation ||
        [[ "${confirmation:l}" != y && "${confirmation:l}" != yes ]]; then
      print -u2 -- 'opx-add: cancelled; existing token preserved'
      return 1
    fi
  fi
  print -nu2 -- "Paste token for $name: "
  IFS= read -rs token
  local read_result=$?
  print -u2
  if (( read_result != 0 )) || [[ -z "$token" ]]; then
    print -u2 -- 'opx-add: cancelled; a non-empty token is required'
    return 1
  fi

  umask 077
  if ! command mkdir -p -- "$token_dir" || ! command chmod 700 "$token_dir"; then
    print -u2 -- 'opx-add: could not prepare token storage; check directory permissions'
    return 1
  fi
  # Stage beside the destination so a failed write never truncates a registration.
  trap '[[ -z "$temp_file" ]] || command rm -f -- "$temp_file"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  if ! temp_file="$(command mktemp "$token_dir/.token.XXXXXXXXXX")" ||
      ! print -rn -- "$token" >"$temp_file" ||
      ! command chmod 600 "$temp_file" ||
      [[ -L "$token_file" || ( -e "$token_file" && ! -f "$token_file" ) ]] ||
      ! command mv -f -- "$temp_file" "$token_file"; then
    print -u2 -- 'opx-add: could not save token; check storage permissions and retry'
    return 1
  fi
  temp_file=''
  print -u2 -- "opx-add: saved $token_file"
)
