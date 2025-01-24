## Ubuntu Linux

```bash
sudo snap install --classic go
sudo apt install make build-essential
sudo apt-get install libsqlite3-dev
sudo apt install sqlite3
sqlite3 myhome/myhome.db .dump
```

## Windows

```cmd
winget install --id Git.Git -e --source winget
winget install --id GoLang.Go --source winget
winget install --id GnuWin32.Make --source winget
winget install --id SQLite.SQLite --source winget
```

```cmd
cd myhome
go run . -v
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