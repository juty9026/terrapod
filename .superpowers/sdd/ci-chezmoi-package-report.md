# Ubuntu Release CI chezmoi package fix

## Status

완료. Release test job은 존재하지 않는 Go package를 설치하지 않고,
official chezmoi v2.71.1 `linux_amd64.deb`를 pinned HTTPS URL에서 내려받아
hard-coded SHA-256을 검증한 뒤 `zsh`와 함께 설치한다.

Release build/publish job, exact eight assets, stable tag validation, signing
제거 contract, no-mise fallback은 변경하지 않았다.

## Root cause

기존 prerequisite는 다음 package를 설치했다.

```text
github.com/twpayne/chezmoi/v2/cmd/chezmoi@v2.71.1
```

chezmoi v2.71.1 module에는 이 package가 존재하지 않는다. Module root
`go install github.com/twpayne/chezmoi/v2@v2.71.1`도 upstream `go.mod`의
`exclude` directives 때문에 사용할 수 없다.

## Changes

- `$RUNNER_TEMP/chezmoi_2.71.1_linux_amd64.deb`로만 download한다.
- exact URL을 pin했다.
  - `https://github.com/twpayne/chezmoi/releases/download/v2.71.1/chezmoi_2.71.1_linux_amd64.deb`
- `curl --proto '=https' --proto-redir '=https'`로 initial request와 redirect를
  모두 HTTPS로 제한했다.
- 다음 exact SHA-256을 `sha256sum -c`로 검증한다.
  - `49b68f441f60fbf84a79928e35076ccd4d0d7d5c050924c27d03bda25a7024eb`
- 검증된 local `.deb`와 `zsh`를 한 번의 `apt-get install`로 설치한다.
- 설치 후 `chezmoi --version`을 실행한다.
- chezmoi `go install`과 `$GITHUB_PATH` mutation을 제거했다.
- contract test에 wrong package path와 missing checksum negative fixtures를
  추가했다.

## TDD evidence

`tests/release_artifacts_test.sh` contract를 먼저 변경한 뒤 production
workflow를 변경하지 않은 상태에서 다음 RED를 확인했다.

```text
not ok - release test job installs the verified pinned chezmoi package and zsh
```

Prerequisite step만 최소 변경한 뒤 같은 test가 exit 0으로 GREEN이 됐다.

## Docker validation

Host에는 package를 설치하지 않았다. Docker Desktop이 중지된 상태여서
daemon만 기동한 뒤 disposable container에서 검증했다.

Apple Silicon의 기본 `arm64` container에서는 checksum이 `OK`인 뒤
의도대로 amd64 package architecture mismatch가 발생했다. GitHub runner와
동일하게 `--platform linux/amd64`를 명시해 다시 실행한 결과 exit 0이었다.

```text
/tmp/chezmoi_2.71.1_linux_amd64.deb: OK
chezmoi version v2.71.1, commit 94f58519ad17b954a5e7621a3c4e24385fd5417a, built at 2026-07-20T20:06:37Z, built by goreleaser
zsh 5.9 (x86_64-ubuntu-linux-gnu)
```

Base image에 없는 `curl`과 CA certificates만 같은 disposable container
안에서 bootstrap했다.

## Verification

다음 검증은 모두 exit 0으로 통과했다.

```sh
sh tests/release_artifacts_test.sh

mise exec go@1.26.0 -- go test ./...

mise exec go@1.26.0 -- sh -c '
  set -eu
  for test_script in tests/*_test.sh; do
    sh "$test_script"
  done
'

git diff --check
```

첫 full suite 시도는 local `mise` shim에 default Go version이 없어 test
실행 전에 중단됐다. Repo가 pin한 `mise exec go@1.26.0` environment로
재실행한 full Go/shell suite는 모두 통과했다.

## Self-review

- Workflow diff는 `Install test prerequisites` step에만 한정된다.
- exact pinned URL, HTTPS-only protocols, checksum, local package install,
  version verification이 contract로 고정돼 있다.
- wrong package path와 checksum 제거 fixture가 validator를 통과하지 못한다.
- executable `go install` line은 workflow에 남아 있지 않다.
- `$GITHUB_PATH` mutation은 test job에 남아 있지 않다.
- release job과 exact eight published assets는 diff가 없으며 기존 contract가
  계속 검증한다.
- signing 관련 configuration이나 artifact를 추가하지 않았다.

## Safety

real home migration/install/`chezmoi apply`는 실행하지 않았다. Host OS에는
chezmoi나 zsh를 설치하지 않았고, package installation은 `--rm` Docker
container 내부에서만 수행했다.

## Concerns

Known code concern 없음. Local Docker image pull은 Docker Desktop credential
helper가 응답하지 않아 empty temporary `DOCKER_CONFIG`로 anonymous pull을
수행했으며, 검증 결과에는 영향이 없다.
