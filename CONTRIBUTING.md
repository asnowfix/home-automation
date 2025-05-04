# Contributor's Guide <!-- omit in toc -->

## Table of Contents <!-- omit in toc -->

- [Ubuntu/Debian Linux](#ubuntu-debian-linux)
- [Windows - WSL](#windows-wsl)
- [Windows - Native](#windows-native)
- [macOS TBC](#macos-tbc)
- [VSCode](#vscode)

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
brew install git
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
