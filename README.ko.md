# Terrapod

🌐 언어: [English](README.md) | **한국어**

Terrapod은 익숙한 shell, editor, runtime, desktop 습관을 새 Mac이나 Ubuntu
24.04 VPS로 가져오는 Personal Development Environment Manager입니다.

내부적으로 Terrapod은 managed file에는 chezmoi apply engine을, package,
runtime, font, Git checkout에는 typed adapter를 사용합니다. declared-root ownership
경계 안에서 verified Terrapod catalog가 선언한 resource만 관리합니다.

## Quick Start

지원되는 machine에서 Terrapod first-run installer를 실행합니다.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

first-run installer는 standard prefix에 Homebrew를 설치한 뒤, Terrapod Setup 전에
Homebrew로 `chezmoi`와 `gum`을 설치합니다. 이어서 최신 stable signed release를
설치하고 그 release에서 Setup을 실행합니다. setup이 성공한 다음에만 initial
declared-state apply를 실행합니다. authoring checkout은 active signed release와 분리되어 있어 checkout을 편집해도 signed release를 설치하기 전에는 active
manager가 바뀌지 않습니다. initial apply가 끝나면 installer는 `tpod help`를 출력해서 짧은
day-to-day command를 바로 볼 수 있게 합니다.
maintainer는 `https://github.com/juty9026/terrapod.git`에서 authoring checkout을
clone할 수 있지만, 이 checkout은 active runtime source가 아닙니다.

pre-manager 구현을 쓰던 maintainer machine은 one-shot `install.sh --migrate`
경로를 사용합니다. legacy chezmoi data를
`~/.config/terrapod/config.json`으로 변환하고, 이관 가능한 기존 resource의
Terrapod ownership을 가져온 뒤 reconcile하며, 검증이 끝난 경우에만 legacy
source를 제거합니다. 신규 설치와 routine update는 이 migration을 실행하지 않습니다.

Terrapod Setup은 first-run review 단계입니다. Preset을 선택하면 그 Preset이
쓸 구체적인 Terrapod-managed machine-local settings를 보여 주고, 필요한
값을 customize한 다음, 확인을 받은 후에 설정을 씁니다. setup이 cancel되거나
실패하면 installer는 initial apply 전에 멈추고, signed installer를 재개하는
정확한 command를 출력합니다.

Terrapod Setup은 interactive first-run prompt에 한정됩니다. bootstrap 이후
routine Terrapod command output은 operational하고 scan-friendly한 형태를
유지합니다.

Terrapod Setup은 `gum`(Bootstrap UI Dependency)을 사용하며, gum이 지원하는
interactive terminal을 요구합니다. `gum` 누락, gum bootstrap 실패, non-TTY,
`dumb` terminal, 지원되지 않는 interactive terminal 환경에서는 setup이 apply 전에
중단되며, 안내 메시지를 출력합니다. Plain text fallback은 없습니다.

이 installer를 실행하기 전에 `chezmoi`를 직접 설치할 필요는 없습니다.

bootstrap 이후에는 routine management와 signed update에 `tpod`를 사용합니다.

```sh
tpod plan
tpod apply
tpod status
tpod doctor
tpod update
```

## What Terrapod Carries

- macOS terminal workstation과 Ubuntu 24.04 VPS를 위한 machine profile.
- Terrapod-managed resource별 stable resource ID와 ownership receipt.
- 구체적인 machine-local setting으로 펼쳐지는 Preset.
- 풍부한 editor configuration, AI CLI tools, development workspace surface를 위한 optional stack.
- 선택한 desktop tool을 묶는 macOS App Group.

## Choose a Preset

Preset은 Terrapod Setup의 starting point입니다. machine에 쓸 구체적인
machine-local settings를 제안하며, setup 중 그 값을 검토하고 customize한 뒤
initial apply 전에 확정할 수 있습니다.

| Preset | 적합한 용도 | 구성 |
| --- | --- | --- |
| `minimal` | 작은 VPS, 깨끗한 shell, recovery install | Core shell과 runtime baseline만 |
| `development` | active coding에 쓰는 machine | Optional Editor Stack, Optional AI Tool Stack, Optional Development Workspace |
| `workstation` | 개인 macOS workstation | Development setup에 모든 macOS App Group 추가 |

Setup은 확인을 받은 뒤 구체적인 machine-local settings를 씁니다. Preset은
영구적인 mode가 아니므로, 나중에 Preset 정의가 바뀌어도 이미 설정된
machine이 조용히 다시 바뀌지는 않습니다.

`workstation` Preset은 macOS Terminal Profile에서만 사용할 수 있습니다.

`terrapod configure <Preset>`는 script-friendly Preset configuration
command입니다. 지원되는 Preset 정확히 하나의 concrete settings를 쓰고,
`gum`이 필요 없으며, interactive customization은 제공하지 않습니다.
`terrapod configure <Preset>`는 Terrapod Setup의 plain fallback이 아닙니다.
`terrapod configure <Preset>`는 setup UI 없이 설정을 쓰는
script-friendly 경로입니다. 이 경로는 Terrapod Setup과 의도적으로 분리되어
있습니다.
Terrapod Setup이 `gum` 또는 interactive terminal 부재로 실행되지 않으면
`gum` 또는 terminal environment를 고친 뒤 `terrapod setup`을 다시 실행합니다.

## What Terrapod Leaves Alone

Terrapod은 verified catalog의 declared root에 속한 resource만 소유합니다.
선언되지 않은 package는 inspect, upgrade, remove하지 않습니다. declared
package가 이미 설치되어 있으면 ownership 이관을 알린 뒤 완전히 가져오거나 해당
resource를 `unavailable`로 둡니다. 일부만 관리하는 중간 상태는 없습니다.

- 광범위한 Homebrew 또는 APT upgrade
- mise-managed tool과 runtime upgrade
- Machine-local secret
- 추적하지 않는 개인 override

Terrapod은 광범위한 Homebrew, APT, mise upgrade를 실행하지 않습니다.

## Daily Commands

bootstrap 이후의 day-to-day 관리 명령은 `tpod`입니다. `terrapod`는 full command와 brand name으로 남아 있습니다.

```sh
tpod status
tpod doctor
tpod diff
tpod plan
tpod apply
tpod update
tpod resolve <resource-id>
terrapod status
```

`tpod plan`은 actual state를 mutation 없이 inspect합니다. `tpod apply`는 그
plan을 실행하고 catalog가 요구하는 Terrapod-owned package만 upgrade하며, 더
이상 desired state에 없는 Terrapod-owned resource를 자동으로 prune합니다.
package removal은 provider별로 수행하며 `brew uninstall --zap`을 사용하지 않습니다.
따라서 Homebrew cask support file은 Terrapod ownership 경계 밖에 남습니다.

`tpod update`는 최신 stable signed Terrapod release를 가져와 manifest와 모든
release asset을 검증하고, 전체 plan을 출력한 뒤 새 Management Core를 atomic하게
활성화하고 Terrapod-owned resource를 reconcile합니다. Terrapod ownership state
밖의 package는 upgrade하거나 remove하지 않습니다.

각 resource의 상태는 `ready` 또는 `unavailable`입니다. unavailable resource와
그 dependent는 mutation하지 않습니다. managed-file conflict는 plan을 확인하고
`tpod resolve <resource-id>`로 원하는 상태를 명시한 뒤 `tpod plan` 또는
`tpod apply`를 다시 실행합니다.

직접 접근은 read-only chezmoi escape hatch로만 제공합니다. Terrapod이 active
signed source, independent config, destination, script exclusion을 고정하며,
mutating chezmoi subcommand는 거부합니다.

```sh
terrapod chezmoi -- cd
terrapod chezmoi -- status
```

`tpod status`는 사람이 읽는 snapshot입니다. mandatory Homebrew command의 누락이나
shadowing을 보고해도 성공으로 종료합니다. `tpod doctor`는 readiness gate입니다.
mandatory command가 없거나 standard Homebrew prefix 밖의 command로 resolve되는 경우,
또는 enabled requirement나 install warning이 남아 있는 경우 non-zero로 종료합니다.

stable launcher가 active Management Core의 누락이나 손상을 보고하면 출력된
정확한 version의 `install.sh --repair`를 실행합니다. repair는 동일한 signed
release input을 검증하고 Management Core만 복구하며, resource를 apply하거나
machine config를 다시 쓰지 않습니다.

## Platform Details

지원 profile은 `macos-terminal`과 `vps-shell`이며, 후자는 Ubuntu 24.04
LTS의 `x86_64`와 `aarch64`를 대상으로 합니다.

Homebrew는 지원되는 두 profile 모두에서 Core Shell Stack의 Modern CLI Provider입니다.
mise는 Bun, Node.js, Python, uv의 Development Runtime Manager입니다.
first-run installer는 Terrapod Setup 전에 Homebrew로 `chezmoi`와 `gum`을 설치합니다.
signed resource catalog는 양쪽 profile의 mandatory CLI formula 20개를 선언합니다.

### macOS

macOS에서 installer를 실행합니다.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

macOS에서는 typed adapter가 초기 terminal environment를 reconcile합니다.

Apple Silicon에서는 Homebrew를 `/opt/homebrew`에 설치하고, Intel Mac에서는 `/usr/local`에 설치합니다.

- standard-prefix Homebrew와 catalog-declared 20-formula CLI set
- Homebrew를 통한 ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), starship, mise 같은 Core Shell Stack CLI
- 최신 안정 GitHub release에서 제공하는 Jetendard terminal font
- Oh My Zsh, zinit, SCM Breeze
- mise를 통한 Bun, Node.js 24, Python 3.13, uv/uvx
- Node.js Corepack을 통한 pnpm
- 해당 stack이 활성화된 경우 Homebrew를 통한 Optional AI Tool Stack cask

Terrapod은 해당 Jetendard release의 모든 TTF를 설치하고 GitHub에서 제공하는 asset digest를 검증합니다. Terrapod은 managed font installer source가 변경되거나 실패한 설치를 재시도할 때만 최신 Jetendard release를 확인합니다. Ghostty, Zed buffer와 terminal, Orca terminal에서 사용하는 font-family key만 설정합니다. Jetendard 설정 적용이 보류되면 Orca를 종료한 뒤 `tpod apply`를 다시 실행합니다. 기존 window가 cached font를 계속 사용하면 Ghostty, Zed 또는 Orca를 다시 시작해야 할 수 있습니다.

macOS desktop application은 independent Terrapod config로 제어되는 opt-in App
Group으로 나뉩니다. typed Homebrew adapter가 선택한 catalog resource를
reconcile합니다.

- `terminal-apps`: Ghostty.
- `automation`: Hammerspoon, Karabiner-Elements, Scroll Reverser.
- `launcher`: Raycast와 1Password CLI.
- `monitoring`: iStat Menus.
- `development-apps`: Zed와 Orca ADE(`stablyai/orca/orca`).

Terrapod은 Orca를 설치할 때 fully-qualified `stablyai/orca/orca` cask만 trust하며, `stablyai/orca` tap 전체를 trust하지 않습니다.

Machine-specific Homebrew package는 signed catalog가 선언하지 않는 한 Terrapod
밖에 둡니다.

### Ubuntu 24.04 VPS

Ubuntu support는 `x86_64`와 `aarch64`의 24.04 LTS만 대상으로 합니다. initial sudo
access가 있는 non-root management user 한 명을 사용합니다. Terrapod은 standard
Homebrew prefix를 사용하며 shared multi-user Linuxbrew installation을 관리하지 않습니다.
설치 전에 1 vCPU, 1 GiB RAM, 최소 3 GiB의 여유 disk space를 권장하며, 2 GiB RAM이면
더 여유롭습니다. 이는 installer hard gate가 아닙니다. 3 GiB 미만이면 경고하고 계속합니다.
VPS profile은 기본적으로 read-only이므로 initial setup에 GitHub authentication이
필요하지 않습니다. installer를 실행합니다.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

Ubuntu 24.04는 모든 Preset에서 `/home/linuxbrew/.linuxbrew`에 Homebrew를 설치합니다.
Terrapod Setup 전에 APT는 Ubuntu system 및 Homebrew bootstrap prerequisite만 설치하며,
Terrapod은 third-party APT repository를 추가하지 않습니다. 그 다음 Homebrew가
`chezmoi`와 `gum`을 설치합니다. installer는 bootstrap 동안 Homebrew와
`~/.local/bin`을 `PATH`에 추가하고, managed zsh session은 reconnect 후 이 경로를 복원합니다.

Ubuntu에서는 typed adapter가 VPS shell profile을 reconcile합니다.

- APT system 및 Homebrew bootstrap prerequisite만 설치
- mise-managed Python runtime에 필요한 Python build dependency
- standard-prefix Homebrew와 catalog-declared 20-formula CLI set
- Oh My Zsh, zinit, SCM Breeze
- Homebrew를 통한 ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), starship, mise 같은 Core Shell Stack CLI
- mise를 통한 Bun, Node.js 24, Python 3.13, uv/uvx
- Node.js Corepack을 통한 pnpm
- login shell을 zsh로 전환
- 해당 stack이 활성화된 경우 Homebrew를 통한 Optional AI Tool Stack cask

VPS Shell Profile은 headless입니다. macOS App Group과 다른 GUI application은 optional
macOS Desktop App Stack에만 속하며 Ubuntu에는 설치되지 않습니다. VPS에서 write
access가 나중에 필요할 때만 GitHub authentication을 설정하세요.

login shell이 자동으로 바뀌지 않았다면 first apply 이후 직접 전환하고 다시 접속합니다.

```sh
chsh -s "$(command -v zsh)"
```

bootstrap 이후의 일반 관리는 Terrapod이 처리합니다. Management Core가
unavailable이면 launcher가 출력한 정확한 signed `install.sh --repair` command를
사용합니다.

### Intentional Upgrades

`tpod apply`는 active signed catalog를 reconcile합니다. 누락된 declared
package를 설치하고 Terrapod-owned package만 upgrade하며, desired state에서
제외된 Terrapod-owned resource를 자동으로 prune합니다. Terrapod은 광범위한
Homebrew, APT, mise upgrade를 실행하지 않고 ownership state 밖의 package를
변경하지 않습니다.

desired package가 다른 installation source로 이미 설치되어 있으면 typed adapter가
완전한 ownership transfer를 plan합니다. 안전한 transfer를 검증할 수 없으면 해당
resource를 `unavailable`로 두고 변경하지 않습니다. managed-file conflict는
`tpod resolve <resource-id>`로 해결하며, package-source conflict는 안내된 외부
원인을 바로잡을 때까지 unavailable로 남습니다.

## Manual Restore

### Raycast

Raycast Store extension과 app state는 이 repo에서 직접 추적하지 않고, 1Password에 저장한 `.rayconfig` backup에서 수동으로 복원합니다.

1. `enableMacosAppGroupLauncher`로 launcher macOS App Group을 enable/install하거나, 다른 방식으로 Raycast가 설치되어 있는지 확인합니다.
2. Raycast settings export가 들어 있는 1Password item을 엽니다.
3. 최신 `.rayconfig` file을 다운로드합니다.
4. Raycast에서 `Import Settings & Data`를 실행합니다.
5. 같은 1Password item에 있는 Raycast export passphrase를 입력합니다.
6. 가져올 category를 선택합니다. 보통 Store extension, settings, alias, hotkey, quicklink, snippet을 선택합니다.

Raycast 변경사항을 workstation 간에 공유해야 할 때는 primary workstation에서 새 `.rayconfig`를 export하고 1Password item을 업데이트합니다.

## Local Overrides

Machine-local option은 chezmoi data가 아니라 Terrapod의 independent config인
`~/.config/terrapod/config.json`에 둡니다. `tpod setup` 또는
`terrapod configure <Preset>`로 쓰며, workstation-specific value는 commit하지 않습니다.

먼저 `tpod setup` 또는 `terrapod configure <Preset>`를 실행해 Terrapod이 complete managed setup config를 쓰게 합니다.
Routine command는 active signed catalog의 schema에 따라 config를 검증하고 새
optional field에는 default를 추가합니다. versioned config migration은 알 수 없는
legacy field를 prune합니다.

Optional stack profile과 macOS App Group setting은 기본적으로 disabled입니다.

| 설정 키 | 기본값 | 목적 |
| --- | --- | --- |
| `profile` | setup/configure가 감지 | 활성 Terrapod machine profile을 기록합니다. |
| `enableEditorStack` | `false` | rich Neovim configuration을 관리하는 Optional Editor Stack을 활성화합니다. Plain Neovim은 어느 쪽이든 Core Shell Stack에 남아 있습니다. |
| `enableAiCliTools` | `false` | Homebrew cask `antigravity-cli`, `claude-code`, `codex`를 통해 Antigravity CLI, Claude Code, Codex를 설치합니다. |
| `enableDevelopmentWorkspace` | `false` | Optional Editor Stack, Optional AI Tool Stack, development-specific Zellij workspace surface를 포함하는 Optional Development Workspace preset을 활성화합니다. |
| `enableMacosAppGroupTerminalApps` | `false` | terminal-apps macOS App Group에 포함된 Ghostty를 설치합니다. |
| `enableMacosAppGroupAutomation` | `false` | automation macOS App Group인 Hammerspoon, Karabiner-Elements, Scroll Reverser를 설치합니다. |
| `enableMacosAppGroupLauncher` | `false` | launcher macOS App Group인 Raycast와 1Password CLI를 설치합니다. |
| `enableMacosAppGroupMonitoring` | `false` | monitoring macOS App Group인 iStat Menus를 설치합니다. |
| `enableMacosAppGroupDevelopmentApps` | `false` | development-apps macOS App Group인 Zed와 Orca ADE(`stablyai/orca/orca`)를 설치합니다. |
| `gitAllowedSigners` | `[]` | workstation-specific SSH signing identity를 `~/.ssh/allowed_signers`에 추가합니다. |

`enableDevelopmentWorkspace`가 `true`이면 `enableEditorStack`이나 `enableAiCliTools`가 false로 기록되어 있어도 Optional Editor Stack과 Optional AI Tool Stack이 함께 활성화됩니다.

macOS Desktop App Stack installation은 `enableDevelopmentWorkspace`와 분리되어 있습니다. desktop cask가 한 사용자의 home directory 밖에 있는 shared application에 영향을 줄 수 있기 때문입니다.

optional stack에서 opt out하면 다음 apply에서 해당 Terrapod-owned resource를
제거합니다. Terrapod이 소유한 적 없는 file과 package는 그대로 둡니다.

declared resource가 이미 설치되어 있으면 plan에서 ownership transfer를 알립니다.
apply는 recovery material을 만들고 검증을 마친 뒤에만 ownership을 기록하며,
그렇지 않으면 resource를 `unavailable`로 둡니다.

`enableMacosAppGroupAiApps`는 deprecated key이며 alias로 해석하지 않습니다. 명시적으로 migrate하려면 `tpod setup` 또는 `terrapod configure <Preset>`를 실행합니다. Terrapod은 이전 선택만으로 Zed를 설치하지 않습니다.

### Zellij shortcuts

Terrapod-managed `.zshrc`는 다음 Zellij helper를 제공합니다.

- `zja [session]`: Zellij session에 attach합니다. `session`을 생략하면 현재 directory name을 사용합니다.
- `zdac [session]`: dev layout Zellij session에 attach하거나 없으면 create합니다. `enableDevelopmentWorkspace`가 true일 때만 제공되며, `session`을 생략하면 현재 directory name을 사용합니다.

### Optional stack profile examples

아래 예시는 기존 complete managed setup config의 `terrapod` object 안에서 유지하거나
바꿀 값입니다. standalone config file이 아닙니다.

Minimal VPS:

```json
{
  "profile": "vps-shell",
  "enableEditorStack": false,
  "enableAiCliTools": false,
  "enableDevelopmentWorkspace": false,
  "enableMacosAppGroupTerminalApps": false,
  "enableMacosAppGroupAutomation": false,
  "enableMacosAppGroupLauncher": false,
  "enableMacosAppGroupMonitoring": false,
  "enableMacosAppGroupDevelopmentApps": false
}
```

Editor-only machine:

```json
{
  "enableEditorStack": true
}
```

AI-only machine:

```json
{
  "enableAiCliTools": true
}
```

Full development workspace machine:

```json
{
  "enableEditorStack": false,
  "enableAiCliTools": false,
  "enableDevelopmentWorkspace": true
}
```

Git signing identity는 어느 profile과도 함께 설정할 수 있습니다.

```json
{
  "gitAllowedSigners": [
    "name@company.com ssh-ed25519 AAAA_COMPANY_PUBLIC_KEY company"
  ]
}
```

그 다음 environment를 reconcile합니다.

```sh
tpod apply
```

## Repository Conventions

- `dot_`: home directory의 dotfile
- `private_`: private permission이 필요한 file
- `executable_`: executable bit가 필요한 file
- `.tmpl`: machine-specific value나 secret injection을 위한 template

static configuration에는 template을 사용하지 않습니다.

secret, token, private key를 commit하지 마세요.
