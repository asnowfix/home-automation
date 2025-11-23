# Contributor's Guide <!-- omit in toc -->

## Table of Contents <!-- omit in toc -->

- [Release Workflow](#release-workflow)
  - [Workflow Overview](#workflow-overview)
  - [Before First Release](#before-first-release)
  - [Creating a Minor Release (vM.m.0)](#creating-a-minor-release-vmm0)
  - [Creating a Patch Release (vM.m.p)](#creating-a-patch-release-vmmp)
  - [Post-Release](#post-release)
  - [Branch Strategy](#branch-strategy)
  - [Best Practices](#best-practices)
  - [Automated Workflows](#automated-workflows)
    - [Merge-Back Behavior](#merge-back-behavior)
  - [Required GitHub Secrets](#required-github-secrets)
  - [Version Numbering](#version-numbering)
  - [Troubleshooting](#troubleshooting)
    - [Workflow didn't trigger](#workflow-didnt-trigger)
    - [Branch not created](#branch-not-created)
    - [Build failed](#build-failed)
    - [Package build fails](#package-build-fails)
    - [GPG signing failed](#gpg-signing-failed)
    - [Release not created](#release-not-created)
    - [Auto-tag didn't create patch version](#auto-tag-didnt-create-patch-version)
  - [Release Notes Process](#release-notes-process)
    - [1. Prerequisites](#1-prerequisites)
    - [2. Creating Release Notes](#2-creating-release-notes)
    - [3. Uploading Release Notes](#3-uploading-release-notes)
    - [4. Publishing the Release](#4-publishing-the-release)
    - [Complete Release Workflow](#complete-release-workflow)
- [Code signing](#code-signing)
- [Ubuntu/Debian Linux](#ubuntudebian-linux)
- [Windows - WSL](#windows---wsl)
- [Windows - Native](#windows---native)
  - [Services](#services)
    - [Build the Service](#build-the-service)
    - [Install the Service](#install-the-service)
    - [Service Management Commands](#service-management-commands)
    - [Troubleshooting](#troubleshooting-1)
  - [PowerShell](#powershell)
- [macOS TBC](#macos-tbc)
- [Profiling](#profiling)
  - [CPU Profiling with --cpuprofile Flag](#cpu-profiling-with---cpuprofile-flag)
  - [Runtime Profiling with pprof HTTP Server](#runtime-profiling-with-pprof-http-server)
    - [Available Endpoints](#available-endpoints)
    - [Command-Line Tools](#command-line-tools)
    - [Common pprof Commands](#common-pprof-commands)
    - [Tips](#tips)
- [Architecture](#architecture)
  - [MyHome RPC Service Architecture](#myhome-rpc-service-architecture)
    - [Adding New RPC Methods](#adding-new-rpc-methods)
    - [Why This Pattern?](#why-this-pattern)
    - [Anti-Pattern: DON'T Do This](#anti-pattern-dont-do-this)
  - [Configuration Management](#configuration-management)
    - [Configuration Hierarchy (highest to lowest priority):](#configuration-hierarchy-highest-to-lowest-priority)
  - [Database Schema](#database-schema)
- [VSCode](#vscode)

## Release Workflow

This project uses semantic versioning (vMAJOR.MINOR.PATCH) with automated tagging and branching.

### Workflow Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         MAIN BRANCH                              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ git tag v0.5.0
                              │ git push origin v0.5.0
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  create-branch-on-minor-tag.yml (triggered by v*.*.0 tag)       │
│  ✓ Creates branch v0.5.x from tag v0.5.0                        │
│  ✓ Triggers package-release.yml                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  package-release.yml                                            │
│  ✓ Builds myhome binary                                         │
│  ✓ Creates MyHome-0.5.0.msi (Windows)                           │
│  ✓ Creates myhome_0.5.0_amd64.deb (Linux)                       │
│  ✓ Creates myhome_0.5.0_arm64.deb (Linux)                       │
│  ✓ Creates draft GitHub release                                 │
│  ✓ Merges tag v0.5.0 back to main (fast-forward if possible)    │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    📦 Release v0.5.0 published
                              │
                              ▼
                    ✓ Tag v0.5.0 is now an ancestor of main


┌─────────────────────────────────────────────────────────────────┐
│                     MAINTENANCE BRANCH v0.5.x                   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ PR merged (bugfix)
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  auto-tag-patch.yml (triggered by PR merge to v*.*.x)           │
│  ✓ Calculates next patch version (v0.5.1)                       │
│  ✓ Creates signed tag v0.5.1                                    │
│  ✓ Triggers package-release.yml                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  package-release.yml                                             │
│  ✓ Builds myhome binary                                          │
│  ✓ Creates MyHome-0.5.1.msi (Windows)                            │
│  ✓ Creates myhome_0.5.1_amd64.deb (Linux)                        │
│  ✓ Creates myhome_0.5.1_arm64.deb (Linux)                        │
│  ✓ Creates draft GitHub release                                 │
│  ✓ Merges tag v0.5.1 back to v0.5.x (fast-forward if possible)  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    📦 Release v0.5.1 published
                              │
                              ▼
                    ✓ Tag v0.5.1 is now an ancestor of v0.5.x
```

### Before First Release

- [ ] Configure GitHub Secrets:
  - [ ] `GPG_PRIVATE_KEY` - GPG private key for signing tags
  - [ ] `GPG_PASSPHRASE` - Passphrase for the GPG key
  - [ ] `SIGNING_CERTIFICATE` - Code signing certificate for Windows MSI (optional)
  - [ ] `SIGNING_PASSWORD` - Certificate password (optional)
- [ ] Verify workflows are enabled in repository settings
- [ ] Test build locally: `go build ./myhome`

### Creating a Minor Release (vM.m.0)

**When to use**: New features, API changes, or significant updates

**Process**:
1. **Tag the release from main**:
   ```bash
   git checkout main
   git pull origin main
   git tag -s v0.5.0 -m "Release v0.5.0"
   git push origin v0.5.0
   ```

2. **Automatic workflow trigger**:
   - The `create-branch-on-minor-tag.yml` workflow detects the tag
   - Creates a maintenance branch `v0.5.x` from the tag
   - Triggers the `package-release.yml` workflow
   - Builds release artifacts (MSI + DEB packages)

3. **Checklist**:
   - [ ] Ensure all changes are merged to `main`
   - [ ] Update version in documentation if needed
   - [ ] Create release notes: `cp RELEASE_NOTES.md RELEASE_NOTES_v0.5.0.md`
   - [ ] Fill in release notes with changes from `git log`
   - [ ] Commit release notes
   - [ ] Create and push tag (see step 1)
   - [ ] Wait for workflows to complete (~15 minutes)
   - [ ] Check GitHub Actions for any failures
   - [ ] Verify branch `v0.5.x` was created
   - [ ] Go to [GitHub Releases](https://github.com/asnowfix/home-automation/releases)
   - [ ] Find draft release `v0.5.0`
   - [ ] Upload release notes: `make upload-release-notes`
   - [ ] Review release notes and artifacts
   - [ ] Download and test packages
   - [ ] Publish release

### Creating a Patch Release (vM.m.p)

**When to use**: Bug fixes, security patches, or minor improvements

**Process**:
1. **Create a PR to the maintenance branch**:
   ```bash
   git checkout v0.5.x
   git pull origin v0.5.x
   git checkout -b fix/my-bugfix
   # Make your changes
   git commit -s -m "Fix: description"
   git push origin fix/my-bugfix
   gh pr create --base v0.5.x --head fix/my-bugfix
   ```

2. **Merge the PR**:
   - Create a PR targeting the `v0.5.x` branch
   - Get it reviewed and approved
   - Merge the PR (do NOT delete branch yet)

3. **Automatic patch tagging**:
   - The `auto-tag-patch.yml` workflow detects the merged PR
   - Runs tests (`go build`, `go test`)
   - Automatically calculates the next patch version (e.g., v0.5.1)
   - Creates and pushes the signed tag
   - Triggers the `package-release.yml` workflow
   - Builds release artifacts (MSI + DEB packages)

4. **Checklist**:
   - [ ] Create bugfix branch from maintenance branch (see step 1)
   - [ ] Make changes and commit
   - [ ] Push branch and create PR targeting `v0.5.x`
   - [ ] Get PR reviewed and approved
   - [ ] Merge PR
   - [ ] Wait for auto-tag workflow (~5 minutes)
   - [ ] Verify tag was created: `git fetch --tags && git tag -l "v0.5.*"`
   - [ ] Wait for package workflow (~15 minutes)
   - [ ] Check GitHub Actions for any failures
   - [ ] Create release notes: `cp RELEASE_NOTES.md RELEASE_NOTES_v0.5.1.md`
   - [ ] Fill in release notes
   - [ ] Commit and push release notes
   - [ ] Upload release notes: `make upload-release-notes`
   - [ ] Go to [GitHub Releases](https://github.com/asnowfix/home-automation/releases)
   - [ ] Find draft release (e.g., `v0.5.1`)
   - [ ] Review release notes and artifacts
   - [ ] Download and test packages
   - [ ] Publish release
   - [ ] Delete bugfix branch

### Post-Release

- [ ] Test DEB installation on clean Ubuntu/Debian machine
- [ ] Test MSI installation on clean Windows machine
- [ ] Verify services start automatically
- [ ] Test core functionality
- [ ] Update documentation if needed
- [ ] Announce release (if applicable)

### Branch Strategy

- **`main`**: Active development, all new features
- **`vMAJOR.MINOR.x`**: Maintenance branches for patch releases
  - Created automatically when tagging `vMAJOR.MINOR.0`
  - Only receives bug fixes via PRs
  - Patch tags created automatically on PR merge
  - Example: `v0.5.x`, `v0.6.x`, `v1.0.x`

### Best Practices

1. **For new features**: Develop on `main`, create a minor release tag (vM.m.0) when ready
2. **For bug fixes**:
   - If fixing current release: Create PR to the `vM.m.x` branch
   - If fixing next release: Develop on `main`
3. **For hotfixes**: Create PR directly to the affected `vM.m.x` branch
4. **Always sign commits**: Use `git commit -s` for signed commits

### Automated Workflows

- **create-branch-on-minor-tag.yml**: Creates `vM.m.x` branch when `vM.m.0` tag is pushed
- **auto-tag-patch.yml**: Creates `vM.m.p+1` tag when PR is merged to `vM.m.x` branch
- **package-release.yml**: Builds and publishes release artifacts (MSI + DEB), then merges the tag back to the source branch
- **on-tag-main.yml**: Triggers packaging workflow when a tag is pushed to main

#### Merge-Back Behavior

After a successful release, the workflow automatically merges the release tag back to its originating branch:

- **Minor releases** (v*.*.0): Tag is merged back to `main`
- **Patch releases** (v*.*.p): Tag is merged back to the maintenance branch (e.g., `v0.5.x`)

The merge uses **fast-forward when possible**, ensuring the tag becomes a direct ancestor of the branch. If fast-forward is not possible (e.g., if commits were added to the branch after tagging), a merge commit is created instead.

**Benefits**:
- Keeps branches in sync with released versions
- Ensures tags are ancestors of their source branches
- Maintains clean git history with fast-forward merges when possible

### Required GitHub Secrets

- `GPG_PRIVATE_KEY` - GPG private key for signing tags
- `GPG_PASSPHRASE` - Passphrase for the GPG key
- `SIGNING_CERTIFICATE` - Code signing certificate for Windows MSI (optional)
- `SIGNING_PASSWORD` - Certificate password (optional)

To generate a GPG key:
```bash
gpg --full-generate-key
gpg --armor --export-secret-keys YOUR_KEY_ID
```

### Version Numbering

Follow semantic versioning: `vMAJOR.MINOR.PATCH`

- **MAJOR**: Breaking changes (e.g., v1.0.0 → v2.0.0)
- **MINOR**: New features, backward compatible (e.g., v0.5.0 → v0.6.0)
- **PATCH**: Bug fixes, backward compatible (e.g., v0.5.0 → v0.5.1)

### Troubleshooting

#### Workflow didn't trigger
- Check GitHub Actions tab for workflow runs
- Verify tag matches pattern: `v[0-9]+.[0-9]+.[0-9]+`
- Ensure workflows are enabled in repository settings
- Verify tag was pushed: `git ls-remote --tags origin`

#### Branch not created
- Check if branch already exists: `git ls-remote --heads origin`
- Review workflow logs in GitHub Actions
- Ensure tag matches `v*.*.0` pattern

#### Build failed
- Check workflow logs in GitHub Actions
- Verify `go.mod` and dependencies are correct
- Test build locally: `go build ./myhome`
- Ensure all tests pass: `go test ./...`

#### Package build fails
- Check GoReleaser logs in workflow
- Verify `.goreleaser.yml` configuration
- Test DEB build locally with `jiro4989/build-deb-action`
- Check MSI build logs for WiX Toolset errors

#### GPG signing failed
- Check GPG key is valid and not expired (run `gpg --list-secret-keys`)
- Review GPG import step in workflow logs
- Ensure key has signing capabilities
- Running remotelly:
  1. check `~/.gnupg/gpg.conf` for the GPG key ID.
      ```
      use-agent
      default-key <your-default-key>
      pinentry-mode loopback
      ```
   2. check `~/.gnupg/gpg-agent.conf`:
      ```
      enable-ssh-support
      disable-scdaemon
      pinentry-program /usr/bin/pinentry-tty
      default-cache-ttl 34560000
      max-cache-ttl 34560000
      ```      
   3. run `echo test | gpg --clearsign` to load the GPG passphrase manually.
- Verify `GPG_PRIVATE_KEY` and `GPG_PASSPHRASE` secrets are set

#### Release not created
- Check if draft release exists in GitHub Releases
- Verify workflow completed successfully
- Check workflow logs for errors in release step
- Ensure previous tag reference is correct

#### Auto-tag didn't create patch version
- Verify PR was merged (not closed without merging)
- Check that PR target branch matches `v*.*.x` pattern
- Review auto-tag-patch workflow logs
- Ensure tests passed in the workflow

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
sqlite3 myhome.db "DROP TABLE IF EXISTS devices;"
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

### CPU Profiling with --cpuprofile Flag

To profile myhome with the CPU profiling flag:

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

### Runtime Profiling with pprof HTTP Server

The MyHome daemon automatically starts a pprof HTTP server on port **6060** when running. This allows you to profile the running daemon without restarting it.

#### Available Endpoints

Once the daemon is running, access profiling data at:

- **http://localhost:6060/debug/pprof/** - Index page with all available profiles
- **http://localhost:6060/debug/pprof/heap** - Memory heap profile
- **http://localhost:6060/debug/pprof/goroutine** - Goroutine profile
- **http://localhost:6060/debug/pprof/threadcreate** - Thread creation profile
- **http://localhost:6060/debug/pprof/block** - Block profile
- **http://localhost:6060/debug/pprof/mutex** - Mutex contention profile
- **http://localhost:6060/debug/pprof/profile** - CPU profile (30 seconds by default)
- **http://localhost:6060/debug/pprof/trace** - Execution trace

#### Command-Line Tools

**CPU Profiling** (captures 30 seconds of CPU activity):
```bash
# Capture and analyze CPU profile
go tool pprof http://localhost:6060/debug/pprof/profile

# Capture with custom duration (60 seconds)
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=60

# Save to file for later analysis
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof
```

**Heap Profiling** (memory allocations):
```bash
# Analyze current heap
go tool pprof http://localhost:6060/debug/pprof/heap

# Save heap snapshot
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof heap.prof
```

**Goroutine Profiling** (active goroutines):
```bash
# Analyze goroutines
go tool pprof http://localhost:6060/debug/pprof/goroutine

# View as text
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

**Interactive Web UI**:
```bash
# Open interactive web interface for heap analysis
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap

# Open interactive web interface for CPU profile
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile
```

**Comparing Profiles** (useful for detecting memory leaks):
```bash
# Take baseline heap snapshot
curl http://localhost:6060/debug/pprof/heap > heap1.prof

# Wait for some time or perform operations...
sleep 300

# Take second snapshot
curl http://localhost:6060/debug/pprof/heap > heap2.prof

# Compare to see what changed
go tool pprof -base heap1.prof heap2.prof
```

**Execution Trace** (detailed runtime events):
```bash
# Capture 5-second trace
curl http://localhost:6060/debug/pprof/trace?seconds=5 > trace.out

# View trace in browser
go tool trace trace.out
```

#### Common pprof Commands

Once in the pprof interactive mode:
```
(pprof) top 10          # Show top 10 functions by resource usage
(pprof) list funcName   # Show source code for a function
(pprof) web             # Open graph in browser (requires graphviz)
(pprof) pdf             # Generate PDF report
(pprof) png             # Generate PNG image
(pprof) help            # Show all commands
```

#### Tips

- **CPU profiling**: Use when investigating performance issues or high CPU usage
- **Heap profiling**: Use when investigating memory leaks or high memory usage
- **Goroutine profiling**: Use when investigating goroutine leaks or concurrency issues
- **Comparing profiles**: Essential for detecting memory leaks over time
- **Web UI**: Provides flame graphs and interactive exploration (`-http` flag)

## Architecture

### MyHome RPC Service Architecture

**CRITICAL**: All new RPC methods MUST be added to the existing MyHome RPC service, NOT as separate RPC services.

#### Adding New RPC Methods

Follow this pattern (see temperature and occupancy services as examples):

1. **Add verb to `internal/myhome/const.go`:**
   ```go
   const (
       // ... existing verbs
       TemperatureGet      Verb = "temperature.get"
       OccupancyGetStatus  Verb = "occupancy.getstatus"
       YourNewMethod       Verb = "yourservice.method"  // Add here
   )
   ```

2. **Add types to `internal/myhome/yourservice.go` (create new file for each service):**
   ```go
   package myhome
   
   // YourService RPC types
   
   // YourServiceParams represents parameters for yourservice.method
   type YourServiceParams struct {
       Field string `json:"field"`
   }
   
   // YourServiceResult represents the result
   type YourServiceResult struct {
       Data string `json:"data"`
   }
   ```

3. **Add method signature to `internal/myhome/methods.go`:**
   ```go
   var signatures map[Verb]MethodSignature = map[Verb]MethodSignature{
       // ... existing methods
       YourNewMethod: {
           NewParams: func() any {
               return &YourServiceParams{}
           },
           NewResult: func() any {
               return &YourServiceResult{}
           },
       },
   }
   ```

4. **Create handler in your service package (e.g., `myhome/yourservice/methods.go`):**
   ```go
   type MethodHandlers struct {
       service *Service
       log     logr.Logger
   }
   
   func NewMethodHandlers(log logr.Logger, service *Service) *MethodHandlers {
       return &MethodHandlers{
           service: service,
           log:     log.WithName("yourservice.methods"),
       }
   }
   
   func (h *MethodHandlers) RegisterHandlers() {
       myhome.RegisterMethodHandler(myhome.YourNewMethod, h.handleMethod)
       h.log.Info("Your service RPC handlers registered")
   }
   
   func (h *MethodHandlers) handleMethod(params any) (any, error) {
       p, ok := params.(*myhome.YourServiceParams)
       if !ok {
           return nil, fmt.Errorf("invalid params type")
       }
       
       // Your logic here
       return &myhome.YourServiceResult{Data: "result"}, nil
   }
   ```

5. **Register in `myhome/daemon/daemon.go` after device manager starts:**
   ```go
   // Register Your Service RPC methods if enabled
   if options.Flags.EnableYourService {
       log.Info("Registering your service RPC methods")
       
       yourHandlers := yourservice.NewMethodHandlers(log, yourServiceInstance)
       yourHandlers.RegisterHandlers()
       
       log.Info("Your service RPC methods registered")
   }
   ```

#### Why This Pattern?

✅ **Single RPC server** - All methods use the same MQTT topic (`myhome/rpc`)  
✅ **Unified lifecycle** - Methods registered when device manager starts  
✅ **Consistent patterns** - Same request/response structure  
✅ **Easy discovery** - All methods in one place (`internal/myhome/const.go`)  
✅ **Type safety** - Centralized type definitions  

#### Anti-Pattern: DON'T Do This

❌ **Don't create separate RPC servers:**
```go
// WRONG - Don't do this!
func NewRPCService(ctx context.Context) (*RPCService, error) {
    // Subscribing to a different topic
    from, err := mc.Subscriber(ctx, "yourservice/rpc", 1)
    // This creates a separate RPC service!
}
```

✅ **Instead, register handlers with the main RPC system:**
```go
// CORRECT - Do this!
func (h *MethodHandlers) RegisterHandlers() {
    myhome.RegisterMethodHandler(myhome.YourMethod, h.handleMethod)
}
```

### Configuration Management

The project uses **Viper** for unified configuration management:

- **Config files**: `./myhome.yaml`, `/etc/myhome/myhome.yaml`, `$HOME/.myhome/myhome.yaml`
- **Environment variables**: `MYHOME_` prefix (e.g., `MYHOME_DAEMON_MQTT_BROKER`)
- **Command-line flags**: Take precedence over config file and env vars

#### Configuration Hierarchy (highest to lowest priority):
1. Command-line flags
2. Environment variables
3. Configuration file
4. Default values

### Database Schema

Temperature configurations use SQLite with this schema:

```sql
CREATE TABLE temperature_rooms (
    room_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    comfort_temp REAL NOT NULL,
    eco_temp REAL NOT NULL,
    weekday_schedule TEXT NOT NULL,  -- JSON array: ["06:00-23:00"]
    weekend_schedule TEXT NOT NULL,  -- JSON array: ["08:00-23:00"]
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_temperature_rooms_updated 
    ON temperature_rooms(updated_at);
```

## BTHome BLE Protocol

BTHome is an open standard for broadcasting sensor data over Bluetooth Low Energy (BLE), sponsored by Allterco Robotics (manufacturer of Shelly devices). Shelly BLU devices use this protocol to broadcast sensor data.

### Protocol Overview

**UUID**: `0xFCD2` (free to use under BTHome license)  
**Version**: BTHome v2 (current)  
**Official Spec**: https://bthome.io/format/

### BLE Advertising Payload Structure

A complete BTHome advertisement consists of three parts:

```
020106 0B094449592D73656E736F72 0A16D2FC4002C40903BF13
```

#### 1. Flags (Required)

```
020106
```

- `0x02` = Length (2 bytes)
- `0x01` = AD Type: Flags
- `0x06` = Flags value (bits: 00000110)
  - Bit 1: LE General Discoverable Mode
  - Bit 2: BR/EDR Not Supported

**Always the same for BTHome**: `0x020106`

#### 2. Local Name (Optional)

```
0B094449592D73656E736F72
```

- `0x0B` = Length (11 bytes)
- `0x09` = AD Type: Complete Local Name
- `0x4449592D73656E736F72` = "DIY-sensor" in ASCII

#### 3. Service Data (Required)

```
0A16D2FC4002C40903BF13
```

- `0x0A` = Length (10 bytes)
- `0x16` = AD Type: Service Data - 16-bit UUID
- `0xD2FC4002C40903BF13` = BTHome data (see below)

### BTHome Data Format

The service data contains the UUID, device info byte, and measurements:

```
D2FC 40 02C409 03BF13
```

#### UUID (2 bytes)

```
D2FC
```

- **Reversed byte order**: `0xFCD2` (little-endian)
- Used by receivers to recognize BTHome messages
- Free to use for everyone

#### Device Info Byte (1 byte)

```
40 = 01000000 (binary)
```

| Bits | Purpose | Values |
|------|---------|--------|
| 0 | Encryption flag | 0=No encryption, 1=Encrypted |
| 1 | Reserved | - |
| 2 | Trigger-based flag | 0=Regular updates, 1=Event-based |
| 3-4 | Reserved | - |
| 5-7 | BTHome version | 010=Version 2 |

**Example**: `0x40` = No encryption, regular updates, BTHome v2

#### Measurements (Variable Length)

Each measurement consists of:
1. **Object ID** (1 byte): Defines the measurement type
2. **Value** (variable bytes): Little-endian encoded value

**Example - Temperature**: `02C409`
- `0x02` = Temperature object ID
- `0xC409` = 2500 (little-endian sint16) × 0.01 = **25.00°C**

**Example - Humidity**: `03BF13`
- `0x03` = Humidity object ID
- `0xBF13` = 5055 (little-endian uint16) × 0.01 = **50.55%**

### Sensor Object IDs

#### Environmental Sensors

| ID | Sensor | Type | Factor | Unit | Bytes |
|----|--------|------|--------|------|-------|
| `0x02` | Temperature | sint16 | 0.01 | °C | 2 |
| `0x03` | Humidity | uint16 | 0.01 | % | 2 |
| `0x04` | Pressure | uint24 | 0.01 | hPa | 3 |
| `0x05` | Illuminance | uint24 | 0.01 | lux | 3 |
| `0x08` | Dew Point | sint16 | 0.01 | °C | 2 |

#### Motion & Position

| ID | Sensor | Type | Factor | Unit | Bytes |
|----|--------|------|--------|------|-------|
| `0x21` | Motion | uint8 | 1 | binary | 1 |
| `0x2D` | Window | uint8 | 1 | binary | 1 |
| `0x3A` | Button | uint8 | 1 | event | 1 |
| `0x3F` | Rotation | uint16 | 0.1 | ° | 2 |
| `0x51` | Acceleration | uint16 | 0.001 | m/s² | 2 |

#### Power & Energy

| ID | Sensor | Type | Factor | Unit | Bytes |
|----|--------|------|--------|------|-------|
| `0x01` | Battery | uint8 | 1 | % | 1 |
| `0x0A` | Energy | uint24 | 0.001 | kWh | 3 |
| `0x0B` | Power | uint24 | 0.01 | W | 3 |
| `0x0C` | Voltage | uint16 | 0.001 | V | 2 |
| `0x43` | Current | uint16 | 0.001 | A | 2 |

#### Other Sensors

| ID | Sensor | Type | Factor | Unit | Bytes |
|----|--------|------|--------|------|-------|
| `0x06` | Mass | uint16 | 0.01 | kg | 2 |
| `0x40` | Distance (mm) | uint16 | 1 | mm | 2 |
| `0x41` | Distance (m) | uint16 | 0.1 | m | 2 |
| `0x50` | Timestamp | uint32 | 1 | seconds | 4 |
| `0x53` | Text | variable | - | UTF-8 | var |
| `0x54` | Raw | variable | - | hex | var |

### Variable Length Sensors

Text and raw sensors require a length byte:

```
530C48656C6C6F20576F726C6421
```

- `0x53` = Text object ID
- `0x0C` = Length (12 bytes)
- `0x48656C6C6F20576F726C6421` = "Hello World!" in UTF-8

### Multiple Measurements

You can include multiple measurements of the same type. Receivers will add postfixes (e.g., `temperature_2`, `temperature_3`) in the order they appear.

**Important**: Always use the same order in each advertisement to prevent measurements being assigned to the wrong entity.

### Decoding Example

Full payload: `020106 0B094449592D73656E736F72 0A16D2FC4002C40903BF13`

1. **Flags**: `020106` → Standard BTHome flags
2. **Name**: `0B094449592D73656E736F72` → "DIY-sensor"
3. **Service Data**: `0A16D2FC4002C40903BF13`
   - Length: `0x0A` (10 bytes)
   - AD Type: `0x16` (Service Data)
   - UUID: `D2FC` → `0xFCD2` (BTHome)
   - Device Info: `40` → v2, no encryption, regular updates
   - Temperature: `02C409` → 25.00°C
   - Humidity: `03BF13` → 50.55%

### Implementation Notes

**For Shelly Scripts**:
- Service data is available in `BLE.Scanner` events
- Parse manufacturer data and service data as hex strings
- Convert little-endian values correctly
- Handle variable-length sensors (text/raw) with length byte

**For Go/Backend**:
- Parse BLE advertisement data from MQTT events
- Decode little-endian values using `binary.LittleEndian`
- Apply scaling factors from the object ID table
- Store decoded values with appropriate units

### Resources

- **Official Specification**: https://bthome.io/format/
- **GitHub Repository**: https://github.com/Bluetooth-Devices/bthome-ble
- **Home Assistant Integration**: Native support via BTHome integration
- **License**: Free to use (Allterco Robotics sponsored UUID)

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
