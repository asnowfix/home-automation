# Contributor's Guide <!-- omit in toc -->

## Table of Contents <!-- omit in toc -->

- [Ubuntu/Debian Linux](#ubuntu-debian-linux)
- [Windows - WSL](#windows-wsl)

## Code signin

See
- <https://wiki.gnupg.org/AgentForwarding>
- <https://stackoverflow.com/questions/63440623/no-gpg-passphrase-prompt-in-visual-studio-code-on-windows-10-for-signed-git-comm>
- <https://stackoverflow.com/questions/49630601/signing-commits-with-git-doesnt-ask-for-my-passphrase>

## Ubuntu/Debian Linux

```bash
sudo snap install --classic go
sudo apt install make build-essential
sudo apt-get install libsqlite3-dev
sudo apt install sqlite3
sqlite3 myhome/myhome.db .dump
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
```
