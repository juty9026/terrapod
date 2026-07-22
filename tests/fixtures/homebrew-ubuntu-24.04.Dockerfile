FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y \
    build-essential ca-certificates curl file git procps sudo zsh \
  && rm -rf /var/lib/apt/lists/* \
  && useradd --create-home --shell /usr/bin/zsh terrapod \
  && printf '%s\n' 'terrapod ALL=(ALL) NOPASSWD:ALL' >/etc/sudoers.d/terrapod \
  && chmod 0440 /etc/sudoers.d/terrapod

USER terrapod
WORKDIR /workspace
RUN NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

COPY --chown=terrapod:terrapod . /workspace

RUN eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)" \
  && HOMEBREW_NO_AUTO_UPDATE=1 brew bundle --no-upgrade --file=/workspace/Brewfile

RUN eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)" \
  && records=/tmp/homebrew-cli-records \
  && TERRAPOD_PRINT_HOMEBREW_CLI_RECORDS=1 /workspace/dot_local/bin/executable_terrapod >"$records" \
  && [ "$(wc -l <"$records")" -eq 20 ] \
  && while IFS="$(printf '\t')" read -r formula command; do \
       command_path="$(command -v "$command")"; \
       case "$command_path" in \
         /home/linuxbrew/.linuxbrew/*) ;; \
         *) printf '%s\n' "not Homebrew-owned: $formula -> $command_path" >&2; exit 1 ;; \
       esac; \
     done <"$records"

RUN eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)" \
  && chezmoi execute-template \
       --source /workspace \
       --override-data '{"chezmoi":{"os":"linux","osRelease":{"id":"ubuntu","versionID":"24.04"}}}' \
       --file /workspace/dot_config/mise/config.toml.tmpl \
     | tee /tmp/mise.toml \
  && ! grep -F 'aqua:' /tmp/mise.toml \
  && grep -Fx 'node = "24"' /tmp/mise.toml
