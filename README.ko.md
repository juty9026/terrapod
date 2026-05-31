# Terrapod

🌐 언어: [English](README.md) | **한국어**

Terrapod은 익숙한 shell, editor, runtime, desktop 습관을 새 Mac이나 Ubuntu 24.04 VPS로 가져오는 작은 landing pod입니다.

내부적으로 Terrapod은 chezmoi를 apply engine으로 사용하며, package-manager upgrade는 범위 밖에 둡니다.

## Quick Start

지원되는 machine에서 Terrapod first-run installer를 실행합니다.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

installer는 필요할 때 `chezmoi`를 `~/.local/bin`에 설치하고,
`https://github.com/juty9026/terrapod.git`을 초기화한 뒤, checked-out source
repository에서 Terrapod Setup을 실행합니다. setup이 성공한 다음에만 initial
declared-state apply를 실행합니다.

Terrapod Setup은 first-run review 단계입니다. Preset을 선택하면 그 Preset이
쓸 구체적인 Terrapod-managed machine-local settings를 보여 주고, 필요한
값을 customize한 다음, 확인을 받은 후에 설정을 씁니다. setup이 cancel되거나
실패하면 installer는 initial apply 전에 멈추고, checked-out source
repository에서 다시 시작할 resume command를 출력합니다.

Terrapod Setup은 interactive first-run prompt에 한정됩니다. bootstrap 이후
routine Terrapod command output은 operational하고 scan-friendly한 형태를
유지합니다.

이 installer를 실행하기 전에 `chezmoi`를 직접 설치할 필요는 없습니다.

bootstrap 이후에는 일반 점검과 source update에 Terrapod을 사용합니다.

```sh
terrapod status
terrapod doctor
terrapod update
```

## What Terrapod Carries

- macOS terminal workstation과 Ubuntu 24.04 VPS를 위한 machine profile.
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

## What Terrapod Leaves Alone

Terrapod은 이 저장소의 declared dotfiles state를 적용합니다. 전체 운영체제를 소유하지는 않습니다.

- 광범위한 Homebrew 또는 APT upgrade
- mise-managed tool과 runtime upgrade
- Machine-local secret
- 추적하지 않는 개인 override

Terrapod은 광범위한 Homebrew, APT, mise upgrade를 실행하지 않습니다.

## Daily Commands

bootstrap 이후의 기본 관리 명령은 `terrapod`입니다. `tpod`는 같은 명령의 짧은 alias입니다.

```sh
terrapod status
terrapod doctor
terrapod diff
terrapod apply
terrapod update
tpod status
```

`terrapod update`는 `chezmoi update --exclude scripts`를 통해 Terrapod Source Repository를 새로고침합니다. Homebrew, APT, mise upgrade는 실행하지 않습니다.

직접 chezmoi를 사용하는 길은 advanced escape hatch로 남아 있습니다.

```sh
terrapod chezmoi -- cd
terrapod chezmoi -- status
```

## Platform Details

### macOS

macOS에서 installer를 실행합니다.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

macOS에서는 initial apply가 초기 terminal environment를 위해 `.chezmoiscripts` 아래 setup script도 실행합니다.

- Homebrew bootstrap과 macOS `Brewfile` bundle
- mise
- mise를 통한 ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), starship 같은 CLI tool
- btop. mise-managed release asset이 macOS arm64를 지원하지 않기 때문에 Homebrew로 설치합니다.
- Terminal font cask
- Oh My Zsh, zinit, SCM Breeze
- `~/.config/mise/config.toml`을 통한 Bun, Python, uv/uvx, Node.js
- Node.js Corepack을 통한 pnpm

macOS desktop application은 machine-local data key로 제어되는 opt-in App Group으로 나뉩니다. Homebrew bootstrap 중 chezmoi는 선택한 group을 `Brewfile.macos-desktop-apps.tmpl`에서 temporary Brewfile로 render하고, 그 rendered bundle을 설치합니다.

- `terminal-apps`: Ghostty.
- `automation`: Hammerspoon과 Karabiner-Elements.
- `launcher`: Raycast와 1Password CLI.
- `monitoring`: iStat Menus.

Machine-specific Homebrew package는 tracked `Brewfile` 밖에 둬야 합니다.

### Ubuntu 24.04 VPS

Ubuntu support는 24.04 LTS만 대상으로 합니다. VPS profile은 기본적으로 read-only이므로 initial setup에 GitHub authentication이 필요하지 않습니다. installer를 실행합니다.

```sh
sh -c "$(curl -fsLS https://raw.githubusercontent.com/juty9026/terrapod/main/install.sh)"
```

installer는 bootstrap process 동안 `~/.local/bin`을 `PATH`에 추가합니다. first apply 이후에는 managed zsh session이 `~/.local/bin`을 `PATH`에 유지하므로, reconnect 후에도 `chezmoi` 같은 user-local binary를 사용할 수 있습니다.

Ubuntu에서는 initial apply가 VPS shell profile을 위한 setup script를 실행합니다.

- APT bootstrap package: zsh, git, curl, ca-certificates, gpg, unzip, build-essential
- mise-managed Python runtime에 필요한 Python build dependency
- official mise APT repository에서 설치하는 mise
- Oh My Zsh, zinit, SCM Breeze
- mise를 통한 ripgrep, neovim, zellij, lazygit, GitHub CLI (`gh`), starship 같은 CLI tool
- mise를 통한 Bun, Python, uv/uvx, Node.js
- Node.js Corepack을 통한 pnpm
- login shell을 zsh로 전환

VPS에서 write access가 나중에 필요할 때만 GitHub authentication을 설정하세요. 첫 mise install이 aqua tool을 resolve하는 동안 GitHub API rate limit에 걸리면, 임시 `GITHUB_TOKEN`을 export하고 `chezmoi apply`를 다시 실행합니다.

login shell이 자동으로 바뀌지 않았다면 first apply 이후 직접 전환하고 다시 접속합니다.

```sh
chsh -s "$(command -v zsh)"
```

bootstrap 이후의 일반 관리는 Terrapod이 처리합니다. 특이한 recovery path에서는 `chezmoi`를 직접 설치하고 `https://github.com/juty9026/terrapod.git`을 initialize한 뒤, 결과를 검토하고 apply합니다.

### Intentional Upgrades

여기서 Homebrew와 APT는 Bootstrap Package Manager입니다. declared shell state를 위해 machine을 준비하는 역할입니다. mise는 supported machine profile 전반의 shared command-line tool과 development runtime을 위한 Modern CLI Provider입니다.

OS-managed package를 의도적으로 업데이트할 때만 OS package manager를 직접 사용합니다.

```sh
# macOS
brew update
brew upgrade

# Ubuntu
sudo apt update
sudo apt upgrade
```

modern CLI tool이나 development runtime을 의도적으로 업데이트할 때는 mise를 직접 사용합니다.

```sh
mise outdated
mise upgrade
```

configured major/minor range를 넘어설 때만 `--bump`를 사용합니다. 예를 들어 Node.js를 현재 `24` line에서 더 새 major로 옮기는 경우입니다.

```sh
mise outdated --bump
mise upgrade --bump
```

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

Machine-local option은 이 repo 밖에서 `chezmoi edit-config`로 설정합니다. 여기에는 option name, default, example만 둡니다. workstation-specific value는 commit하지 않습니다.

Optional stack profile과 macOS App Group setting은 기본적으로 disabled입니다.

| 설정 키 | 기본값 | 목적 |
| --- | --- | --- |
| `enableEditorStack` | `false` | rich Neovim configuration을 관리하는 Optional Editor Stack을 활성화합니다. Plain Neovim은 어느 쪽이든 Core Shell Stack에 남아 있습니다. |
| `enableAiCliTools` | `false` | mise-managed Node.js runtime을 통해 npm으로 Gemini CLI, Claude Code, Codex를 설치합니다. |
| `enableDevelopmentWorkspace` | `false` | Optional Editor Stack, Optional AI Tool Stack, development-specific Zellij workspace surface를 포함하는 Optional Development Workspace preset을 활성화합니다. |
| `enableMacosAppGroupTerminalApps` | `false` | terminal-apps macOS App Group에 포함된 Ghostty를 설치합니다. |
| `enableMacosAppGroupAutomation` | `false` | automation macOS App Group인 Hammerspoon과 Karabiner-Elements를 설치합니다. |
| `enableMacosAppGroupLauncher` | `false` | launcher macOS App Group인 Raycast와 1Password CLI를 설치합니다. |
| `enableMacosAppGroupMonitoring` | `false` | monitoring macOS App Group인 iStat Menus를 설치합니다. |
| `gitAllowedSigners` | `[]` | workstation-specific SSH signing identity를 `~/.ssh/allowed_signers`에 추가합니다. |

`enableDevelopmentWorkspace`가 `true`이면 `enableEditorStack`이나 `enableAiCliTools`가 false이거나 생략되어도 Optional Editor Stack과 Optional AI Tool Stack이 함께 활성화됩니다.

macOS Desktop App Stack installation은 `enableDevelopmentWorkspace`와 분리되어 있습니다. desktop cask가 한 사용자의 home directory 밖에 있는 shared application에 영향을 줄 수 있기 때문입니다.

optional stack에서 opt out하면 해당 file은 chezmoi management에서 제외됩니다. 이미 machine에 존재하는 file을 제거하지는 않습니다.

### Optional stack profile examples

Minimal VPS:

```toml
[data]
```

Editor-only machine:

```toml
[data]
enableEditorStack = true
```

AI-only machine:

```toml
[data]
enableAiCliTools = true
```

Full development workspace machine:

```toml
[data]
enableEditorStack = false
enableAiCliTools = false
enableDevelopmentWorkspace = true
```

Git signing identity는 어느 profile과도 함께 설정할 수 있습니다.

```toml
[data]
gitAllowedSigners = [
  "name@company.com ssh-ed25519 AAAA_COMPANY_PUBLIC_KEY company",
]
```

그 다음 dotfiles를 apply합니다.

```sh
terrapod apply
```

## Repository Conventions

- `dot_`: home directory의 dotfile
- `private_`: private permission이 필요한 file
- `executable_`: executable bit가 필요한 file
- `.tmpl`: machine-specific value나 secret injection을 위한 template

static configuration에는 template을 사용하지 않습니다.

secret, token, private key를 commit하지 마세요.
