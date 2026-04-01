---
name: shelly
description: Help the user work with Shelly devices — listing devices, querying status, calling RPC methods, managing scripts and KVS, working with the Shelly API (Gen1/Gen2+/BLU). Use when the user asks about Shelly devices, home automation devices, device status, or device control.
---

Help the user work with Shelly device APIs in this project.

## Live Device Interaction (prefer MCP tools)

When the user wants to **list devices** or **call RPC methods on devices**, use the MCP tools — they go directly over MQTT without requiring a separate CLI invocation:

| Task | MCP tool | Key params |
|------|----------|------------|
| List all devices | `shelly_list` | `filter` (optional substring, default `*`) |
| Call an RPC method | `shelly_call` | `device_id`, `method`, `params` (JSON string) |

Examples:
```
shelly_list {}                                            → all devices
shelly_list {"filter": "pool"}                           → devices matching "pool"
shelly_call {"device_id": "shellypm-abc123", "method": "Shelly.GetStatus", "params": "{}"}
shelly_call {"device_id": "pool-pump", "method": "Switch.Set", "params": "{\"id\":0,\"on\":true}"}
shelly_call {"device_id": "living-room", "method": "KVS.Get", "params": "{\"key\":\"script/heater/config\"}"}
```

Fall back to the CLI commands below only when the MCP server is unavailable or for operations not covered by `shelly_call` (e.g. script upload, script update).

## Generation Detection

Check device ID or model to determine the generation — it matters because Gen1 and Gen2+ use completely different APIs.

- **Gen1**: older devices (Shelly1, Shelly H&T, Shelly Plug, etc.) — HTTP REST API only, no RPC
- **Gen2+**: newer devices (Plus, Pro, Gen3, Gen4 lines) — JSON-RPC over HTTP and MQTT

Use `shelly.IsGen1Device(deviceId)` (in `pkg/shelly/`) to detect Gen1 programmatically.

## Gen1 API

API reference: https://shelly-api-docs.shelly.cloud/gen1/#shelly-family-overview

Gen1 devices expose a **plain HTTP REST API** — no JSON-RPC, no MQTT RPC.

Key endpoints (all `GET` unless noted):
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

In this project, Gen1 types live in `pkg/shelly/gen1/types.go`. The `gen1.Device` struct represents the HTTP payload. The proxy that relays Gen1 data into the MyHome MQTT bus is in `pkg/shelly/gen1/proxy.go`.

Direct testing with curl:
```bash
curl http://<device-ip>/status
curl http://<device-ip>/settings
curl "http://<device-ip>/relay/0?turn=on"
```

## Gen2+ API

API reference: https://shelly-api-docs.shelly.cloud/gen2/

Gen2+ devices use **JSON-RPC 2.0** over both HTTP and MQTT.

### Via HTTP
```bash
curl -X POST http://<device-ip>/rpc \
  -H 'Content-Type: application/json' \
  -d '{"id":1,"method":"Shelly.GetStatus","params":{}}'
```

### Via the project CLI
```bash
# Generic RPC call to any device
go run ./myhome ctl shelly call <device-name> <Method.Name> '<json-params>'

# Examples
go run ./myhome ctl shelly call living-room Shelly.GetStatus '{}'
go run ./myhome ctl shelly call living-room Switch.Set '{"id":0,"on":true}'
go run ./myhome ctl shelly call living-room KVS.Get '{"key":"script/heater/config"}'
go run ./myhome ctl shelly call living-room Script.List '{}'
```

### Common Gen2+ RPC methods
```
Shelly.GetStatus          → full device status
Shelly.GetDeviceInfo      → model, firmware, MAC
Shelly.Reboot             → reboot device
Switch.GetStatus          → relay/switch state   {"id": 0}
Switch.Set                → control relay         {"id": 0, "on": true}
Input.GetStatus           → input state           {"id": 0}
KVS.Get                   → read stored value      {"key": "..."}
KVS.Set                   → write stored value     {"key": "...", "value": "..."}
KVS.List                  → list all keys          {}
Script.List               → list scripts           {}
Script.GetStatus          → script status          {"id": 1}
Script.Start / .Stop      → run/stop a script      {"id": 1}
Schedule.List             → list schedules         {}
```

## Script Management CLI

```bash
# Upload a script (always use --no-minify for debugging)
go run ./myhome ctl shelly script upload <device> <script.js> --no-minify
go run ./myhome ctl shelly script upload <device> <script.js> --force   # bypass hash check

# Update all embedded scripts on a device
go run ./myhome ctl shelly script update <device>

# List scripts on a device
go run ./myhome ctl shelly script list <device>

# Start / stop / delete by name
go run ./myhome ctl shelly script start <device> <script-name>
go run ./myhome ctl shelly script stop  <device> <script-name>
go run ./myhome ctl shelly script delete <device> <script-name>

# Toggle script debug logging to stdout
go run ./myhome ctl shelly script debug <device> true
go run ./myhome ctl shelly script debug <device> false
```

Embedded scripts live in `internal/shelly/scripts/`. KVS version hashes are stored under `script/<name>/version` and checked before upload to avoid unnecessary re-uploads.

## KVS Key Convention

KVS keys use only `[0-9a-z-/]`:
- `script/<script-name>/<key>` — script-owned data
- `follow/<category>/<id>` — follow configuration
- `state/<category>/<id>` — follow state

## Shelly BLU (Bluetooth Low Energy) Devices

API reference: https://shelly-api-docs.shelly.cloud/docs-ble/

BLU devices (Shelly BLU Button, BLU Motion, BLU H&T, etc.) are **BLE peripherals** — they do not connect to the network directly. They broadcast BLE advertisements which are picked up by a nearby Gen2+ Shelly acting as a BLE gateway.

### How it works in this project

1. A Gen2+ gateway device runs one of the embedded BLU scripts (`internal/shelly/scripts/blu-listener.js` or `blu-publisher.js`).
2. The script subscribes to BLE scan results via `Shelly.BLE.Scanner.Start()`.
3. Events (button press, motion, temperature) are parsed from the BLE advertisement payload and forwarded to MQTT.

### BLU device identification

BLU devices are identified by their BLE MAC address (e.g. `e8:e0:7e:d0:f9:89`). MAC addresses are used as KVS keys:
```
follow/shelly-blu/<mac>   → follow configuration for a BLU device
state/shelly-blu/<mac>    → current state data
```

Use `blu.ResolveMac(ctx, identifier)` (`internal/myhome/blu/resolve.go`) to resolve a human-readable name or partial MAC to a full MAC address.

### CLI commands for BLU

```bash
# Follow a BLU device (relay its events to another device or action)
go run ./myhome ctl blu follow <blu-mac-or-name> <target>

# List followed BLU devices
go run ./myhome ctl blu list
```

BLU script resource limits apply (same as any Shelly script): 5 timers, 5 event subscriptions, 10 MQTT subscriptions.

## Other CLI Commands

```bash
go run ./myhome ctl list                               # list all devices known to the daemon
go run ./myhome ctl shelly status <device>             # device status
go run ./myhome ctl shelly reboot <device>             # reboot
go run ./myhome ctl shelly kvs get <device> <key>      # read KVS entry
go run ./myhome ctl shelly kvs set <device> <key> <v>  # write KVS entry
go run ./myhome ctl shelly sys config <device>         # system config
go run ./myhome ctl shelly mqtt status <device>        # MQTT config & status
```
