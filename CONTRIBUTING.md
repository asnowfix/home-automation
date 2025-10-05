# Contributor's Guide <!-- omit in toc -->

## Table of Contents <!-- omit in toc -->

- [Release Workflow](#release-workflow)
  - [Creating a Minor Release](#creating-a-minor-release-vmm0)
  - [Creating a Patch Release](#creating-a-patch-release-vmmp)
  - [Branch Strategy](#branch-strategy)
  - [Best Practices](#best-practices)
  - [Automated Workflows](#automated-workflows)
  - [Release Notes Process](#release-notes-process)
- [Code signing](#code-signing)
- [Ubuntu/Debian Linux](#ubuntudebian-linux)
- [Windows - WSL](#windows-wsl)
- [Windows - Native](#windows-native)
- [macOS TBC](#macos-tbc)
- [Profiling](#profiling)
- [VSCode](#vscode)

## Release Workflow

This project uses semantic versioning (vMAJOR.MINOR.PATCH) with automated tagging and branching.

### Creating a Minor Release (vM.m.0)

**When to use**: New features, API changes, or significant updates

**Process**:
1. Create and push a tag ending in `.0`:
   ```bash
   git tag -s v1.2.0 -m "Release v1.2.0"
   git push origin v1.2.0
   ```
2. The workflow automatically:
   - Creates a branch `v1.2.x` from the tag
   - Triggers the packaging workflow to build release artifacts

**Example**: Tag `v1.2.0` → Creates branch `v1.2.x`

### Creating a Patch Release (vM.m.p)

**When to use**: Bug fixes, security patches, or minor improvements

**Process**:
1. Create a PR targeting the release branch:
   ```bash
   git checkout -b fix-bug-123
   # Make your changes
   git commit -s -m "Fix bug #123"
   gh pr create --base v1.2.x --head fix-bug-123
   ```
2. When the PR is merged, the workflow automatically:
   - Finds the latest patch tag on that branch (e.g., `v1.2.3`)
   - Creates the next patch tag (e.g., `v1.2.4`)
   - Triggers the packaging workflow to build release artifacts

**Example**: PR merged to `v1.2.x` → Automatically creates tag `v1.2.4`

### Branch Strategy

```
main (development)
  ├── v1.0.x (release branch)
  │   ├── v1.0.0 (tag)
  │   ├── v1.0.1 (tag)
  │   └── v1.0.2 (tag)
  ├── v1.1.x (release branch)
  │   ├── v1.1.0 (tag)
  │   └── v1.1.1 (tag)
  └── v2.0.x (release branch)
      └── v2.0.0 (tag)
```

### Best Practices

1. **For new features**: Develop on `main`, create a minor release tag (vM.m.0) when ready
2. **For bug fixes**:
   - If fixing current release: Create PR to the `vM.m.x` branch
   - If fixing next release: Develop on `main`
3. **For hotfixes**: Create PR directly to the affected `vM.m.x` branch

### Automated Workflows

- **create-branch-on-minor-tag.yml**: Creates `vM.m.x` branch when `vM.m.0` tag is pushed
- **auto-tag-patch.yml**: Creates `vM.m.p+1` tag when PR is merged to `vM.m.x` branch
- **package-release.yml**: Builds and publishes release artifacts

### Release Notes Process

#### 1. Prerequisites

Install GitHub CLI:
```bash
# macOS
brew install gh

# Debian/Ubuntu
sudo apt install gh

# Authenticate
gh auth login
```

#### 2. Creating Release Notes

For each release, create release notes from the template:

```bash
# Copy the template
cp RELEASE_NOTES.md RELEASE_NOTES_v0.5.2.md

# Edit and fill in all sections
# Use git log to help generate content:
git log v0.5.1..HEAD --oneline
git log v0.5.1..HEAD --pretty=format:"%h %s" --reverse
```

**Release Notes Checklist**:
- [ ] Update version numbers (replace `vX.Y.Z` with actual version)
- [ ] Update release date
- [ ] Fill in all sections with actual changes from git log
- [ ] Update installation URLs with correct version
- [ ] Review breaking changes section carefully
- [ ] Add migration instructions if needed
- [ ] Proofread for clarity and accuracy

#### 3. Uploading Release Notes

After the automated workflow creates the release:

```bash
# Upload release notes for the latest tag
make upload-release-notes

# Or specify a version
make upload-release-notes VERSION=v0.5.2
```

This will:
- Detect the latest git tag (or use VERSION if specified)
- Find the corresponding `RELEASE_NOTES_vX.Y.Z.md` file
- Upload it to the GitHub release using `gh release edit`

#### 4. Publishing the Release

1. Review the draft release on GitHub
2. Verify all artifacts are attached
3. Review the release notes
4. Click "Publish release"

#### Complete Release Workflow

```bash
# 1. Create release notes
cp RELEASE_NOTES.md RELEASE_NOTES_v0.5.2.md
# Edit the file...

# 2. Commit release notes
git add RELEASE_NOTES_v0.5.2.md
git commit -m "docs: add release notes for v0.5.2"
git push

# 3. Create and push tag (or let auto-tagging handle it)
git tag -s v0.5.2 -m "Release v0.5.2"
git push origin v0.5.2

# 4. Wait for CI/CD to build packages (check GitHub Actions)

# 5. Upload release notes
make upload-release-notes

# 6. Review and publish on GitHub
gh release view v0.5.2 --web
```

For detailed documentation, see [docs/RELEASE_PROCESS.md](docs/RELEASE_PROCESS.md).

## Code signing

See
- <https://wiki.gnupg.org/AgentForwarding>
- <https://stackoverflow.com/questions/63440623/no-gpg-passphrase-prompt-in-visual-studio-code-on-windows-10-for-signed-git-comm>
- <https://stackoverflow.com/questions/49630601/signing-commits-with-git-doesnt-ask-for-my-passphrase>
- <https://r-pufky.github.io/docs/apps/gpg/usage/windows-forward-gpg.html>

## Ubuntu/Debian Linux

```bash
sudo snap install --classic go
sudo apt install make build-essential
sudo apt-get install libsqlite3-dev
sudo apt install sqlite3
```

Some useful SQLite 3 commands:

```bash
sqlite3 myhome/myhome.db .dump
sqlite3 myhome.db "DROP TABLE IF EXISTS groups;"
```

```bashrc
if ! type go 1>/dev/null 2>&1 && test -d /snap/go/current/bin; then
    export PATH=${PATH}:/snap/go/current/bin
fi
```

## Windows - WSL

In addition to the native Linux instructions above, you need to run the following command on the Windows host.

Route intress MQTT traffic to the WSL guest:

```cmd
netsh interface portproxy add v4tov4 listenport=1883 listenaddress=0.0.0.0 connectport=1883 connectaddress=<WSL-Addr>
```

## Windows - Native

### Services

#### Build the Service

As developer:
```cmd
go build myhome
```

#### Install the Service

As administrator:

```cmd
# 1. Create program and log directories with proper permissions
mkdir "C:\Program Files\MyHome"
mkdir "C:\ProgramData\MyHome"
mkdir "C:\ProgramData\MyHome\logs"
icacls "C:\ProgramData\MyHome" /inheritance:r
icacls "C:\ProgramData\MyHome" /grant:r "NT AUTHORITY\SYSTEM":(OI)(CI)F
icacls "C:\ProgramData\MyHome" /grant:r "BUILTIN\Administrators":(OI)(CI)F

# 2. Register event log source
eventcreate /ID 1 /L APPLICATION /T INFORMATION /SO MyHome /D "MyHome service registration"

# 3. Copy the executable
taskkill /F /IM myhome.exe 2>nul
copy /y "myhome.exe" "C:\Program Files\MyHome\myhome.exe"

# 4. Create and configure the service
sc create MyHome binPath= "\"C:\Program Files\MyHome\myhome.exe\" daemon -B mqtt.local"
sc config MyHome start= auto
sc config MyHome obj= LocalSystem
sc config MyHome DisplayName= "MyHome Automation Service"
sc config MyHome description= "MyHome Automation Service"

# 5. Start the service
sc start MyHome

# 6. Verify service status
sc query MyHome

SERVICE_NAME: MyHome
        TYPE               : 10  WIN32_OWN_PROCESS
        STATE              : 4  RUNNING
                                (STOPPABLE, NOT_PAUSABLE, ACCEPTS_SHUTDOWN)
        WIN32_EXIT_CODE    : 0  (0x0)
        SERVICE_EXIT_CODE  : 0  (0x0)
        CHECKPOINT         : 0x0
        WAIT_HINT          : 0x0

```

#### Service Management Commands

```cmd
# 7. Stop service
sc stop MyHome

# 8. Verify service status
sc query MyHome

SERVICE_NAME: MyHome
        TYPE               : 10  WIN32_OWN_PROCESS
        STATE              : 3  STOP_PENDING
                                (NOT_STOPPABLE, NOT_PAUSABLE, IGNORES_SHUTDOWN)
        WIN32_EXIT_CODE    : 0  (0x0)
        SERVICE_EXIT_CODE  : 0  (0x0)
        CHECKPOINT         : 0x0
        WAIT_HINT          : 0x2710

# 9. Remove the service
sc delete MyHome
```

#### Troubleshooting

1. Check Windows Event Viewer > Windows Logs > Application for events from source "MyHome"
   ```cmd
   # View last 100 events from MyHome service
   wevtutil qe Application /q:"*[System[Provider[@Name='MyHome']]]" /f:text /c:100
   
   # Or using PowerShell
   Get-WinEvent -FilterHashtable @{LogName='Application'; ProviderName='MyHome'} -MaxEvents 100 | Format-List
   ```
2. Check service logs in `C:\ProgramData\MyHome\logs\myhome.log`
3. If the service fails to start, ensure all directories have correct permissions
4. Use `sc qc MyHome` to verify service configuration

example output:

```
[SC] QueryServiceConfig SUCCESS

SERVICE_NAME: MyHome
        TYPE               : 10  WIN32_OWN_PROCESS
        START_TYPE         : 3   DEMAND_START
        ERROR_CONTROL      : 1   NORMAL
        BINARY_PATH_NAME   : C:\Program Files\MyHome\myhome.exe daemon -B mqtt.local
        LOAD_ORDER_GROUP   :
        TAG                : 0
        DISPLAY_NAME       : MyHome
        DEPENDENCIES       :
        SERVICE_START_NAME : LocalSystem


TODO: add all of the above steps in wix.json

### Git Bash

In `~/.bashrc`:

```bash
if ! type make 1>/dev/null 2>&1 && test -d /c/Program\ Files\ \(x86\)/GnuWin32/bin; then
    export PATH=${PATH}:/c/Program\ Files\ \(x86\)/GnuWin32/bin
fi
```

### PowerShell

```pwsh
winget install --id Git.Git -e --source winget
$env:Path += ";C:\Program Files\Git\bin"
```

```pwsh
winget install --id GnuWin32.Make --source winget
$env:Path += ";C:\Program Files (x86)\GnuWin32\bin"
```

```pwsh
winget install --id GoLang.Go --source winget
$env:Path += ";C:\Program Files\Go\bin;C:\Users\$env:Username\Go\bin"
Get-Command go
```

```
CommandType     Name    Version    Source
-----------     ----    -------    ------
Application     go.exe  0.0.0.0    C:\Program Files\Go\bin\go.exe    
```

```pwsh
winget install --id SQLite.SQLite --source winget

# Copy SQLite binaries to local bin directory
$src = "C:\Users\$env:Username\AppData\Local\Microsoft\WinGet\Packages\SQLite.SQLite_*"
$dst = "C:\Users\$env:Username\.local.bin"
New-Item -Path $dst -ItemType Directory -Force | Out-Null
Get-ChildItem $src -Recurse -Filter *sqlite* | Copy-Item -Destination $dst

$env:Path += ";C:\Users\$env:Username\.local.bin"
Get-Command sqlite3
```

As administrator, install the WiX Toolset:

```pwsh
Enable-WindowsOptionalFeature -Online -FeatureName NetFx3
winget install --id WiXToolset.WiXToolset --version 3.14.1.8722 --source winget --disable-interactivity --accept-source-agreements --force
$env:Path += ";C:\Program Files (x86)\WiX Toolset v3.14\bin"
Get-Command candle
```

```
CommandType     Name        Version    Source
-----------     ----        -------    ------
Application     candle.exe  3.14.87... C:\Program Files (x86)\WiX Toolset v3.14\bin\candle.exe
```

```pwsh
winget install --id Chocolatey.Chocolatey --source winget
Get-Command choco
```

```
CommandType     Name        Version    Source
-----------     ----        -------    ------
Application     choco.exe   0.12.1.0   C:\ProgramData\chocolatey\bin\choco.exe
```

Run as administrator:

```pwsh
choco install go-msi
Get-Command go-msi
```

```
CommandType     Name        Version    Source
-----------     ----        -------    ------
Application     go-msi.exe  0.0.0.0    C:\Program Files\go-msi\go-msi.exe
```

Allow ingress MQTT:

```pwsh
netsh advfirewall firewall add rule name="Allow MQTT" dir=in action=allow protocol=TCP localport=1883
The requested operation requires elevation (Run as administrator).
```

```pwsh
go-msi make --msi MyHome.msi --version 0.0.0 --path .\wix.json --arch amd64 --license .\LICENSE
```

```pwsh
cd myhome
go run . -h
```

## macOS TBC

```bash
brew install git go sqlite sqlite3 graphviz
```

## Profiling

See <https://go.dev/blog/pprof> for details.

To profile myhome:

```bash
myhome --cpuprofile 0.prof
```

To view the profile:

```bash
$go tool pprof 0.prof 
File: myhome
Type: cpu
Time: 2025-05-20 21:51:58 CEST
Duration: 1.01s, Total samples = 20ms ( 1.98%)
Entering interactive mode (type "help" for commands, "o" for options)
(pprof) top 10
Showing nodes accounting for 20ms, 100% of 20ms total
Showing top 10 nodes out of 23
      flat  flat%   sum%        cum   cum%
      10ms 50.00% 50.00%       10ms 50.00%  runtime.(*unwinder).resolveInternal
      10ms 50.00%   100%       10ms 50.00%  runtime.pthread_cond_signal
         0     0%   100%       10ms 50.00%  github.com/eclipse/paho%2emqtt%2egolang.startOutgoingComms.func1
         0     0%   100%       10ms 50.00%  runtime.(*unwinder).next
         0     0%   100%       10ms 50.00%  runtime.deductAssistCredit
         0     0%   100%       10ms 50.00%  runtime.gcAssistAlloc
         0     0%   100%       10ms 50.00%  runtime.gcAssistAlloc.func2
         0     0%   100%       10ms 50.00%  runtime.gcAssistAlloc1
         0     0%   100%       10ms 50.00%  runtime.gcDrainN
         0     0%   100%       10ms 50.00%  runtime.mallocgc
```

To see the graph of dependencies:

```bash
$go tool pprof 0.prof
(pprof) web
```

## VSCode

`launch.json`:

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Launch myhome daemon",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceFolder}/myhome/main.go",
            "env": {},
            "cwd": "${workspaceFolder}/myhome",
            "args": ["daemon","-B","192.168.1.2"],
        }
    ]
}
