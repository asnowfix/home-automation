# Shelly APIs and CLI

Contents:
- [Live device interaction — prefer the MCP tools](#live-device-interaction--prefer-the-mcp-tools)
- [Generation detection](#generation-detection)
- [Gen1 API](#gen1-api)
- [Gen2+ API](#gen2-api)
- [Script management CLI](#script-management-cli)
- [BLU devices](#blu-devices)
- [Other CLI commands](#other-cli-commands)

---

## Live device interaction — prefer the MCP tools

To **list devices** or **call RPC methods**, use the MCP tools — they go straight over MQTT with no
separate CLI invocation:

| Task | MCP tool | Key params |
|------|----------|------------|
| List all devices | `shelly_list` | `filter` (optional substring, default `*`) |
| Call an RPC method | `shelly_call` | `device_id`, `method`, `params` (JSON string) |

```
shelly_list {}                                            → all devices
shelly_list {"filter": "pool"}                            → devices matching "pool"
shelly_call {"device_id": "shellypm-abc123", "method": "Shelly.GetStatus", "params": "{}"}
shelly_call {"device_id": "pool-pump", "method": "Switch.Set", "params": "{\"id\":0,\"on\":true}"}
shelly_call {"device_id": "living-room", "method": "KVS.Get", "params": "{\"key\":\"script/heater/config\"}"}
```

Fall back to the CLI when the MCP server is unavailable, or for operations `shelly_call` does not
cover (script upload, script update).

Before calling anything that changes device state, read `field-discipline.md` — several RPC methods
here are authorized only on two named devices, and one of them switches a relay on as a side effect.

---

## Generation detection

Gen1 and Gen2+ use completely different APIs, so establish which you have first.

- **Gen1** — older devices (Shelly1, H&T, Plug): HTTP REST only, no RPC.
- **Gen2+** — Plus, Pro, Gen3, Gen4 lines: JSON-RPC over HTTP and MQTT.

`shelly.IsGen1Device(deviceId)` in `pkg/shelly/` detects Gen1 programmatically.

---

## Gen1 API

Reference: https://shelly-api-docs.shelly.cloud/gen1/#shelly-family-overview

Plain HTTP REST — no JSON-RPC, no MQTT RPC. All `GET` unless noted:

```
GET  http://<device-ip>/shelly          → device info & model
GET  http://<device-ip>/status          → full status snapshot
GET  http://<device-ip>/settings        → configuration
POST http://<device-ip>/settings        → update configuration
GET  http://<device-ip>/relay/0         → relay status
GET  http://<device-ip>/relay/0?turn=on → turn relay on
GET  http://<device-ip>/meter/0         → energy meter reading
GET  http://<device-ip>/sensor          → H&T / Flood sensor data
```

In this project Gen1 types live in `pkg/shelly/gen1/types.go`; `gen1.Device` represents the HTTP
payload. The proxy relaying Gen1 data onto the MyHome MQTT bus is `pkg/shelly/gen1/proxy.go`.

```bash
curl http://<device-ip>/status
curl "http://<device-ip>/relay/0?turn=on"
```

---

## Gen2+ API

Reference: https://shelly-api-docs.shelly.cloud/gen2/

JSON-RPC 2.0 over both HTTP and MQTT.

```bash
# Direct HTTP — also the recovery path when MQTT RPC is dead
curl -X POST http://<device-ip>/rpc \
  -H 'Content-Type: application/json' \
  -d '{"id":1,"method":"Shelly.GetStatus","params":{}}'

# Via the project CLI
go run ./myhome ctl shelly call <device-name> <Method.Name> '<json-params>'
go run ./myhome ctl shelly call living-room Switch.Set '{"id":0,"on":true}'
go run ./myhome ctl shelly call living-room KVS.Get '{"key":"script/heater/config"}'
```

Common methods:

```
Shelly.GetStatus          → full device status
Shelly.GetDeviceInfo      → model, firmware, MAC        (record `ver` in any measurement)
Shelly.Reboot             → reboot device
Switch.GetStatus          → relay/switch state          {"id": 0}
Switch.Set                → control relay               {"id": 0, "on": true}
Input.GetStatus           → input state                 {"id": 0}
KVS.Get                   → read stored value           {"key": "..."}
KVS.Set                   → write stored value          {"key": "...", "value": "..."}
KVS.List                  → list all keys               {}
Script.List               → list scripts                {}
Script.GetStatus          → script status + heap        {"id": 1}
Script.GetCode            → fetch installed code back   {"id": 1}
Script.Start / .Stop      → run/stop a script           {"id": 1}
Script.Eval               → evaluate in script context  ALWAYS WRAPPED — see scripting.md
Schedule.List             → list schedules              {}
```

Two traps worth repeating here:

- **`Script.Eval` must always be wrapped**: `(function(){try{ ... }catch(e){return "ERR:"+e}})()`.
  An unwrapped throw kills the whole script. See `scripting.md`.
- **`KVS.Get`/`KVS.GetMany` cannot unmarshal numeric or boolean values** (#468) — read them out of
  the error payload.

---

## Script management CLI

```bash
# Upload (always --no-minify while debugging)
go run ./myhome ctl shelly script upload <device> <script.js> --no-minify
go run ./myhome ctl shelly script upload <device> <script.js> --force   # bypass hash check

go run ./myhome ctl shelly script update <device>          # update all embedded scripts
go run ./myhome ctl shelly script list   <device>

go run ./myhome ctl shelly script start  <device> <script-name>
go run ./myhome ctl shelly script stop   <device> <script-name>
go run ./myhome ctl shelly script delete <device> <script-name>

go run ./myhome ctl shelly script debug  <device> true|false   # UDP debug — see field-discipline.md
go run ./myhome ctl shelly script probe  <device> <script>     # settled mem_peak measurement
```

Embedded scripts live in `internal/shelly/scripts/`. KVS version hashes are stored under
`script/<name>/version` and checked before upload to avoid unnecessary re-uploads — note the
version-marker trap in #449 when force-uploading for a measurement.

### `upload` ignores the file you name unless you tell it not to

**This took the production pool pump down on 2026-08-23.** By default `upload` resolves the script
by *name* against the copy **embedded in the binary you are running**, not against the path you
typed. If that binary is old, you silently deploy an old script — and the device is left with the
script **stopped**.

The only warning is one line of output:

```
. Source: embedded      <-- the binary's copy, however stale that binary is
. Source: local         <-- the file you actually named
```

To upload the file in front of you:

```bash
go run ./myhome ctl shelly script upload --force \
    --local-scripts-dir "$PWD" <device> pool-pump.js
```

Three things must all be right:

- **`--local-scripts-dir <dir>`** — without it, local loading is disabled entirely and the embedded
  copy always wins. The flag's own help calls this "dev convenience"; it is closer to a safety
  requirement whenever you are deploying something that is not the binary's own build.
- **`--force`** — the KVS version hash check will otherwise skip the upload as unnecessary.
- **a bare filename relative to your cwd** — an absolute path fails with a misleading
  `file does not exist` even when the file is plainly there.

A prebuilt CLI kept around for convenience (rather than `go run` from the current checkout) carries
whatever scripts were embedded on the day it was built. Treat its embedded copies as stale by
definition.

**Always confirm what landed**, rather than trusting the success tick: grep the output for
`Source: local`, then check `Script.GetStatus` reports `"running": true` and that `mem_used` is
consistent with the build you meant to deploy. Identifying a build from its bytes is covered in
`field-discipline.md`.

---

## BLU devices

Reference: https://shelly-api-docs.shelly.cloud/docs-ble/

BLU devices (Button, Motion, H&T) are **BLE peripherals** — they never join the network. They
broadcast BLE advertisements picked up by a nearby Gen2+ Shelly acting as a gateway:

1. A Gen2+ gateway runs `internal/shelly/scripts/blu-listener.js` or `blu-publisher.js`.
2. The script subscribes to scan results via `Shelly.BLE.Scanner.Start()`.
3. Events (button press, motion, temperature) are parsed from the advertisement payload and
   forwarded to MQTT.

BLU devices are identified by BLE MAC (e.g. `e8:e0:7e:d0:f9:89`), used directly as KVS keys:

```
follow/shelly-blu/<mac>   → follow configuration
state/shelly-blu/<mac>    → current state data
```

`blu.ResolveMac(ctx, identifier)` in `internal/myhome/blu/resolve.go` resolves a human-readable name
or partial MAC to a full one.

```bash
go run ./myhome ctl blu follow <blu-mac-or-name> <target>
go run ./myhome ctl blu list
```

The gateway script is subject to the same per-script resource limits as any other — 5 timers, 5 event
subscriptions, 10 MQTT subscriptions.

---

## Other CLI commands

```bash
go run ./myhome ctl list                               # devices known to the daemon
go run ./myhome ctl shelly status <device>
go run ./myhome ctl shelly reboot <device>
go run ./myhome ctl shelly kvs get <device> <key>
go run ./myhome ctl shelly kvs set <device> <key> <v>
go run ./myhome ctl shelly sys config <device>
go run ./myhome ctl shelly mqtt status <device>
```

Long-running or remote invocations generally want explicit broker and timeout flags:
`-B tcp://<broker>:1883 -T 60s`.
