# Ubuntu Release CI prerequisites fix

## Status

완료. Release workflow의 test job이 shell suite에 필요한 pinned `chezmoi`
와 `zsh`를 준비하며, 두 manager contract script는 `mise`가 없는 CI에서도
`actions/setup-go`가 제공한 `go`를 사용한다.

Release build/publish job, exact eight assets, release safety checks, signing
정책은 변경하지 않았다.

## Root cause

Ubuntu 24.04 hosted runner에는 `jq`와 `gh`가 있지만 `chezmoi`, `mise`,
`zsh`가 없다. 기존 Release test job은 `actions/setup-go`만 실행한 뒤 전체
shell suite를 시작했고, `tests/chezmoiignore_test.sh`가 `chezmoi: not found`
로 종료했다. 또한 두 manager script가 `mise`를 직접 호출해, 첫 prerequisite
문제를 해결한 뒤에도 CI에서 실패할 상태였다.

## Changes

- Release test job의 `actions/setup-go` 직후 test prerequisites step을 추가했다.
  - `chezmoi@v2.71.1`을 정확히 pin하여 `go install`한다.
  - `$(go env GOPATH)/bin`을 `$GITHUB_PATH`에 추가한다.
  - 같은 step에서 설치된 `chezmoi` binary를 absolute path로 검증한다.
  - Ubuntu `apt-get`으로 `zsh`를 설치한다.
- `tests/manager_shadow_test.sh`와
  `tests/terrapod_manager_migration_test.sh`에 local `run_go` helper를
  추가했다.
  - `mise`가 있으면 `mise exec go@1.26.0 -- go`를 사용한다.
  - 없으면 `command go`를 사용한다.
- `tests/release_artifacts_test.sh`에 workflow prerequisite와 두 `run_go`
  helper의 contract assertions를 추가했다.

## TDD evidence

Workflow contract를 먼저 추가한 뒤 다음 RED를 확인했다.

```text
not ok - release workflow installs chezmoi exactly once
```

Workflow를 최소 변경해 contract GREEN을 확인한 다음, helper contract를
추가해 두 번째 RED를 확인했다.

```text
not ok - manager_shadow_test.sh defines a local run_go helper
```

두 script에 helper를 적용한 뒤 `sh tests/release_artifacts_test.sh`가
exit 0으로 통과했다.

## Verification

요청된 검증은 모두 exit 0으로 통과했다.

```sh
sh tests/release_artifacts_test.sh

PATH="$(mise where go@1.26.0)/bin:/usr/bin:/bin" \
  sh tests/manager_shadow_test.sh

PATH="$(mise where go@1.26.0)/bin:/usr/bin:/bin" \
  sh tests/terrapod_manager_migration_test.sh

mise exec go@1.26.0 -- go test ./... -count=1

PATH="$(mise where go@1.26.0)/bin:$PATH"
export PATH
for test_script in tests/*_test.sh; do
  sh "$test_script"
done

git diff --check
```

첫 전체 shell loop 시도는 local `mise` shim에 global Go version이 설정되지
않아 test 실행 전에 중단됐다. CI의 `actions/setup-go` 조건을 재현하도록
`go@1.26.0/bin`을 PATH 앞에 둔 동일 loop 재실행은 전체 통과했다.

## Self-review

- `chezmoi` install은 정확히 한 번이며 test job에만 존재한다.
- `zsh`도 test job에만 설치한다.
- `mise`는 workflow에 설치하지 않는다.
- release job의 build/publish steps와 exact eight asset 목록은 변경하지 않았다.
- signing 관련 dependency나 configuration을 추가하지 않았다.
- 변경은 workflow prerequisite, 두 script의 Go launcher, 해당 contract
  assertions로 제한했다.

## Safety

real home을 대상으로 migration, install, `chezmoi apply`를 실행하지 않았다.
검증은 repository test fixtures만 사용했다.

## Concerns

known concern 없음. `actionlint`는 local environment에 설치되어 있지 않아
실행하지 못했지만, 변경된 shell script의 `sh -n`, workflow contract,
전체 Go/shell suite, `git diff --check`를 통과했다.
