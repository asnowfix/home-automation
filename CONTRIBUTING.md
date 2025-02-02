# Contributor's Guide <!-- omit in toc -->

## Table of Contents <!-- omit in toc -->

- [Ubuntu/Debian Linux](#ubuntu-debian-linux)
- [Windows - WSL](#windows-wsl)

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
```

```pwsh
winget install --id WiXToolset.WiXCLI --source winget
$env:Path += ";C:\Program Files\WiX Toolset v5.0\bin"
```

```pwsh
go install github.com/mh-cbon/go-msi
```

Allow ingress MQTT:

```cmd
netsh advfirewall firewall add rule name="Allow MQTT" dir=in action=allow protocol=TCP localport=1883
The requested operation requires elevation (Run as administrator).
```

```cmd
% go-msi gen-wix-cmd --msi MyHome.msi
CreateFile C:\Users\fixko\Go\bin\templates: The system cannot find the file specified.

% go-msi make --msi MyHome.msi --version 0.0.0
CreateFile C:\Users\fixko\Go\bin\templates: The system cannot find the file specified.
```

```cmd
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
