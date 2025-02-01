## Ubuntu Linux

```bash
sudo snap install --classic go
sudo apt install make build-essential
sudo apt-get install libsqlite3-dev
sudo apt install sqlite3
sqlite3 myhome/myhome.db .dump
```

## Windows

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
