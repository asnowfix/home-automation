# Remote Access via SOCKS Proxy

How to run a local `myhome` daemon instance that reaches the home network
(SFR router, Shelly devices, MQTT broker) through an SSH SOCKS tunnel.

## Prerequisites

A working SSH connection to the home Pi. Add a persistent tunnel host to
`~/.ssh/config`:

```
Host home-pi-tunnel
    HostName home-pi          # or the Pi's IP / hostname
    DynamicForward 1080       # SOCKS5 proxy on localhost:1080
    SessionType none          # no interactive shell
    ExitOnForwardFailure yes
    ServerAliveInterval 30
    ServerAliveCountMax 3
```

## 1. Open the SOCKS tunnel

```bash
ssh -fN home-pi-tunnel
```

`-f` forks to background, `-N` skips executing a remote command.
The SOCKS5 proxy is now listening on `localhost:1080`.

## 2. Start the local daemon

Pass `HTTP_PROXY` (not `ALL_PROXY` — Go's HTTP client does not read `ALL_PROXY`)
and the known SFR box IP so gateway discovery does not probe the local network:

```bash
SFR_BOX_IP=192.168.1.1 \
HTTP_PROXY=socks5://localhost:1080 \
go run ./myhome daemon run \
    -B 127.0.0.1:1883 \
    -I myhome-local
```

| Flag / env var | Purpose |
|---|---|
| `SFR_BOX_IP=192.168.1.1` | Skip gateway probing; use this IP directly |
| `HTTP_PROXY=socks5://localhost:1080` | Route all outbound HTTP through the tunnel |
| `-B 127.0.0.1:1883` | Connect to the MQTT broker forwarded via SSH (see below) |
| `-I myhome-local` | Distinct instance name so RPC topics don't collide with the remote daemon |

If the MQTT broker is on the Pi, forward port 1883 alongside the SOCKS tunnel:

```
Host home-pi-tunnel
    HostName home-pi
    DynamicForward 1080
    LocalForward 1883 localhost:1883   # MQTT broker
    SessionType none
    ExitOnForwardFailure yes
    ServerAliveInterval 30
    ServerAliveCountMax 3
```

## 3. Seed the local database from the remote daemon

A fresh local instance has an empty device database. Export the device list
from the remote daemon and import it locally so the local instance knows which
devices exist.

### Export from the remote daemon

The remote daemon is reached over the SOCKS tunnel via its RPC topic
(`-I myhome` targets the default instance name on the Pi):

```bash
HTTP_PROXY=socks5://localhost:1080 \
go run ./myhome ctl db export \
    -B 127.0.0.1:1883 \
    --output database.json
```

### Import into the local daemon

```bash
HTTP_PROXY=socks5://localhost:1080 \
go run ./myhome ctl db import \
    -B 127.0.0.1:1883 \
    -I myhome-local \
    database.json
```

The local daemon now has the same device list as the remote one and can resolve
device identities without the SFR router being queried on every refresh.

## Troubleshooting

**`ALL_PROXY` is silently ignored by Go.**
Go's `net/http` reads `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`.
The `ALL_PROXY` variable used by curl/wget has no effect.

**Context deadline exceeded on SFR API calls.**
The SFR HTTP client has a 3-second timeout. If the tunnel is not open or
`HTTP_PROXY` is not set, requests to `192.168.1.1` time out quickly rather
than hanging indefinitely. Check that `ssh -fN home-pi-tunnel` is running
(`ps aux | grep ssh`).

**MQTT connection refused.**
Ensure port 1883 is forwarded in `~/.ssh/config` (see `LocalForward` above)
and that the tunnel process is alive before starting the daemon.
