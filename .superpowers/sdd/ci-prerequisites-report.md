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

## Reviewer follow-up: dispatch and install contract hardening

Reviewer의 Important finding에 따라 production/workflow implementation은
변경하지 않고 `tests/release_artifacts_test.sh`의 regression contract만
강화했다.

### RED evidence

Go dispatch validator stub에 direct `go test` call을 추가한 오염 fixture를
검사해 다음 RED를 확인했다.

```text
not ok - Go dispatch contract rejects a direct go test call
```

workflow install validator stub에는 runner-provided `jq`를 다시 설치하는
executable `apt-get` line을 추가해 다음 RED를 확인했다.

```text
not ok - release workflow contract rejects runner-provided apt package reinstalls
```

### Contract changes

- `manager_shadow_test.sh`의 `run_go test` textual call-site count를 2로
  고정했다.
- `terrapod_manager_migration_test.sh`의 helper 내부 dispatch를 포함한
  `run_go test` textual call-site count를 4로 고정했다.
- 각 script에서 pinned `mise exec ... -- go "$@"`와
  `command go "$@"`가 helper body와 전체 script에 각각 정확히 한 번만
  존재하도록 검증한다.
- direct `go test`와 direct `mise ... -- go test`를 거부한다.
- workflow 전체의 executable `apt-get install`/`go install` line을 현재의
  exact zsh/chezmoi prerequisites로 allowlist하여 `mise`, `jq`, `gh`
  재설치를 거부한다.
- `mise` install action/direct command도 거부하며, comment와 `echo`에 있는
  documentation text는 install로 오인하지 않는 fixture를 추가했다.

### Follow-up verification

다음 검증은 모두 exit 0으로 통과했다.

```sh
sh tests/release_artifacts_test.sh

PATH="$(mise where go@1.26.0)/bin:/usr/bin:/bin" \
  sh tests/manager_shadow_test.sh

PATH="$(mise where go@1.26.0)/bin:/usr/bin:/bin" \
  sh tests/terrapod_manager_migration_test.sh

sh -n tests/release_artifacts_test.sh
git diff --check
```

Follow-up diff에는 `tests/release_artifacts_test.sh`와 이 report만 포함되며,
Release workflow, production code, manager/migration scripts는 변경하지
않았다. real home migration/install/`chezmoi apply`도 실행하지 않았다.

### Follow-up concerns

known concern 없음.
