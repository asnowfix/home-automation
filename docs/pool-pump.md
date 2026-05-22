# Pool Pump Control — Design Reference

## Hardware

| Device | Model | Capability |
|--------|-------|-----------|
| Pro3 | ShellyPro3 | 3 switches → drives pump variator (eco/mid/high) + schedules |
| Pro1 | ShellyPro1 | 1 switch → drives pump at max speed via relay |

**Both devices run the same `pool-pump.js` script.** Device type is detected at runtime by switch count. Each device independently decides whether to activate based on shared KVS configuration.

### Pro3 wiring
| Component | Purpose |
|-----------|---------|
| input:0 (`water-supply`, inverted) | HIGH = water supply ON → turn off pump |
| switch:0 (`pump-eco`) | Variator eco/low speed |
| switch:1 (`pump-mid`) | Variator mid speed |
| switch:2 (`pump-high`) | Variator high speed |
| sys button | Cycles speed: off → eco → mid → high → off |

### Pro1 wiring
| Component | Purpose |
|-----------|---------|
| input:0 (`water-supply`, inverted) | HIGH = water supply ON → turn off pump |
| switch:0 (`pump-max`) | Full-speed relay |
| sys button | Toggles on/off |

**Both switches are configured with `in_mode: detached`** so physical inputs never override MQTT/RPC commands. All protection is handled in software.

---

## Button Handling (Power Cycling)

The system button (`sys_btn_push` event) on both devices provides manual pump control.

### Pro3 — Speed Cycling

**Sequence**: off → eco (switch:0) → mid (switch:1) → high (switch:2) → off

**Power cycling logic** (make-before-break for speed-to-speed transitions):

| Transition | Action | Rationale |
|------------|--------|-----------|
| Off → Speed | Turn ON target speed only | Clean start from idle |
| Speed → Off | Turn OFF all speeds | Full power cut |
| Speed A → Speed B | Turn ON new speed, then turn OFF old speed | Variator handles transition; no gap in power |

### Pro1 — Simple Toggle

Toggles switch:0 on/off directly (no speed cycling needed on single-speed device).

---

## Architecture

### Unified Script Model

All devices in the pool pump mesh run the **same** `pool-pump.js` script. There is no controller role, no bootstrap role, and no central coordinator. Each device:

1. Reads `preferred` KVS key (a Shelly device ID) on every activation event
2. Compares it to its own device ID (`Shelly.getDeviceInfo().id`)
3. If match → activate at `speed` KVS value
4. If no match → ensure all switches are off

**Mesh membership** is defined solely by the script running on a device. There is no separate registry — the Go CLI discovers mesh members dynamically by querying the server's device database and checking which devices have `pool-pump.js` loaded.

### Device type detection

At script startup, the device detects its own type by querying `Switch.GetStatus` for switch IDs 0, 1, 2. A device with 3 switches is a Pro3; with 1 switch, a Pro1. This drives speed mapping.

**Speed mapping:**
| Device | Speed | Physical Switch |
|--------|-------|-----------------|
| Pro3 | `eco` | switch:0 (or KVS `eco-speed`) |
| Pro3 | `mid` | switch:1 (or KVS `mid-speed`) |
| Pro3 | `high` / `max` | switch:2 (or KVS `high-speed`) |
| Pro1 | any speed | switch:0 (only switch) |

---

## Cross-Device Safety (Grace Delays)

Prevents multiple pool pump devices from being on simultaneously, which could damage the pump.

### Grace delay
| Config key | Default | Meaning |
|------------|---------|---------|
| `grace-delay` | 10 000 ms | Wait time when switching from one device to another |

### Implementation
- Before activating, check if **any peer device** has any switch ON (via MQTT status subscriptions)
- If a peer is ON: turn it OFF via MQTT RPC → wait `grace-delay` → then activate self
- `STATE.graceTimer` — single one-shot timer, only live during a transition
- `STATE.graceActive` — boolean guard; concurrent calls wait via task queue

### Cross-device state tracking (MQTT status subscriptions)
Each device subscribes to all peer devices' switch status topics. KVS keys `pro3-id` and `pro1-id` provide the peer device IDs:
- Device A subscribes to `<peer-id>/status/switch:*` for each peer
- On startup, a `status_update` command is published to each peer topic to get current state (topics are not retained)

### Reactive guards in `handleSwitchEvent`
- **Any local switch turns on** + `isAnyPeerDeviceOn()` returns true → immediately sends `Switch.Set {on:false}` to the active peer (no grace delay — cut peer as fast as possible)
- Protection is **reactive only**: the Shelly scripting API has no pre-intercept hook. `Shelly.addEventHandler` fires *after* the switch has already changed state. The brief window (~ms) is unavoidable without hardware interlocks.

The `in_mode: detached` switch config prevents the *physical input* from directly toggling the relay, but does not block app / HTTP / MQTT / RPC commands.

---

## Software Fuse (Anti-Cycling Protection)

Prevents rapid relay cycling that generates repeated motor inrush currents and trips circuit breakers. The fuse monitors output state changes regardless of their source (schedules, buttons, MQTT, water supply).

### Parameters
| Constant | Value | Meaning |
|----------|-------|---------|
| `FUSE_WINDOW_MS` | 120 000 ms (2 min) | Sliding window for counting state changes |
| `FUSE_MAX_CHANGES` | 4 | Max transitions allowed per window |
| `FUSE_COOLDOWN_MS` | 300 000 ms (5 min) | Lockout duration after the fuse trips |

### Behaviour
1. Every call to `activateOutput()` that actually changes the relay state (on→off or off→on) records a timestamp.
2. Before any **ON** activation, `fuseAllowOn()` prunes stale entries, then checks the count.
3. If the count reaches `FUSE_MAX_CHANGES`, the fuse **trips**: all switches are turned off and all ON activations are refused.
4. After `FUSE_COOLDOWN_MS` elapses, the fuse resets automatically and normal operation resumes.
5. **OFF activations (`outputId = -1`) always pass** — safety takes precedence over the fuse.

### Normal operation budget
A start/stop cycle produces 2 state changes (on + off). The threshold of 4 allows two full cycles plus margin — well above the one cycle per scheduled period (night run or day run).

### No extra timers
The fuse uses only in-memory variables (`FUSE_CHANGES` array, `FUSE_TRIPPED` flag, `FUSE_TRIP_TIME` timestamp). It does not allocate timers; the cooldown is checked lazily on the next activation attempt.

---

## Schedules

Schedules are created on **all devices** in the mesh. Each device's script self-selects via `isMyTurnToRun()` — only the preferred device activates on schedule events, others ignore them. Managed via `ctl pool setup` using a **delete-and-recreate** strategy (no incremental reconciliation).

| Schedule | Timespec | Handler | Default state |
|----------|----------|---------|---------------|
| Daily check | `@sunrise` daily | `handleDailyCheck()` | Enabled |
| Morning start | Absolute `HH:MM` (updated daily) | `handleMorningStart()` | **Disabled** (summer only) |
| Evening stop | Absolute `HH:MM` (updated daily) | `handleEveningStop()` | **Disabled** (summer only) |
| Night start | `23:15` daily | `handleNightStart()` | Enabled |
| Night stop | `00:15` daily | `handleNightStop()` | Enabled |

Morning-start and evening-stop timespecs are recalculated each morning by `handleDailyCheck()` and written directly to the Shelly schedule via `Schedule.Update`. The initial timespec created by `ctl pool add` is `@sunrise+3h` / `@sunset` and is overwritten on the first daily check.

### Schedule modes
- **Summer** (`maxForecastTemp > temperatureThreshold`): morning/evening schedules enabled (with computed absolute times), night schedules disabled
- **Winter** (default): night schedules enabled, morning/evening disabled

Mode is determined daily at sunrise via Open-Meteo forecast, stored in KVS (`schedule-mode`).

---

## Weather Forecast (Memory-Optimized)

- URL built from device GPS coordinates via `Shelly.DetectLocation` and stored in `Script.storage`
- Fetched once per day (date-gated)
- Only the **max temperature** is retained in `STATE.maxForecastTemp`; the full array is discarded immediately to save memory
- On error, forecast is skipped and current schedule mode is preserved

---

## Temperature-Proportional Scheduling

Every morning at sunrise, `handleDailyCheck()` fetches the Open-Meteo forecast (which now includes `daily=sunrise,sunset`), computes how many hours the pump should run today, centres that window on the hottest forecast hour, and calls `Schedule.Update` to write absolute `HH:MM` timespecs for the morning-start and evening-stop schedules.

### Run-hours calculation

1. **Base hours** = `(poolVolume × turnover) / flowRate`
   - `flowRate` is computed from `maxFlowRate × (preferredSpeedRpm / maxRpm)`
   - `preferredSpeed` maps to one of `ecoRpm` / `midRpm` / `highRpm`
2. **Scale factor** = clamp(`(todayMaxTemp − tempThreshold) / (maxTemp − tempThreshold)`, 0, 1)
3. **Run hours** = `baseHours × scale`, clamped to `[baseHours × 0.5, baseHours × 1.5]`

At `tempThreshold` the pump runs at half base hours; at `maxTemp` it runs at full base hours (one turnover). At temperatures above `maxTemp` the cap prevents overshooting.

### Window centering

The run window is centered on the peak-temperature hour from the forecast:

```
startHour = peakHour − runHours / 2
stopHour  = peakHour + runHours / 2
```

Boundary enforcement (applied in order):
- If `startHour < sunriseHour + 1h` → shift window forward
- If `stopHour > sunsetHour − 0.5h` → shift window backward
- Hard floor of `sunriseHour + 1h` applied after both shifts

### Fallback values

| Condition | Fallback used |
|-----------|--------------|
| `peakForecastHour` not available | 14:00 |
| `sunriseHour` not available | 06:00 |
| `sunsetHour` not available | 21:00 |
| Flow rate zero / invalid | 8 hours |

---

## KVS Layout

All keys use prefix `script/pool-pump/` (≤ 32 chars total).

### Configuration (set by `ctl pool add` on each device)
| Key | Default | Notes |
|-----|---------|-------|
| `preferred` | — | Shelly device ID of the device that should run the pump |
| `speed` | `eco` | `eco`/`mid`/`high`/`max` — mapped to switches per device type |
| `pro3-id` | — | Pro3 device ID (for peer MQTT subscriptions) |
| `pro1-id` | — | Pro1 device ID (for peer MQTT subscriptions) |
| `mqtt-topic` | `pool/pump` | MQTT topic prefix |
| `logging` | `true` | |
| `eco-speed` | `0` | Switch ID for `eco` speed (Pro3 only) |
| `mid-speed` | `1` | Switch ID for `mid` speed (Pro3 only) |
| `high-speed` | `2` | Switch ID for `high`/`max` speed (Pro3 only) |
| `grace-delay` | `10000` | Cross-device switchover delay (ms) |
| `night-duration` | `3600000` | Night run duration (ms) |
| `temp-threshold` | `20` | °C threshold for summer mode |
| `pool-volume`    | `46`  | Pool volume in m³ |
| `turnover`       | `5`   | Daily turnover target (× pool volumes) |
| `max-flow-rate`  | `31`  | Pump max flow rate (m³/h at max RPM) |
| `max-rpm`        | `2900` | Pump physical rated max RPM |
| `eco-rpm`        | `2000` | Variator RPM setting for eco speed |
| `mid-rpm`        | `2600` | Variator RPM setting for mid speed |
| `high-rpm`       | `2900` | Variator RPM setting for high speed |
| `max-temp`       | `35`  | °C at which run time = one full turnover |

### State (managed by script, per device)
| Key | Notes |
|-----|-------|
| `active-output` | `-1` or switch ID currently active |
| `schedule-mode` | `"summer"` or `"winter"` |

### Script.storage (script-private)
| Key | Notes |
|-----|-------|
| `forecast-url` | Open-Meteo URL built from GPS coordinates |
| `my-device-id` | Cached device ID from `Shelly.getDeviceInfo().id` |

---

## Go CLI (`ctl pool`)

Mesh membership is discovered dynamically — the CLI queries the myhome server's device database and filters to devices running `pool-pump.js`. No local registry file is used.

| Command | Action |
|---------|--------|
| `ctl pool add <device-identifier>` | Upload `pool-pump.js`, set KVS config, create schedules (Pro3 only) |
| `ctl pool preferred <device-id> <speed>` | Set `preferred` + `speed` KVS on **all** mesh devices |
| `ctl pool remove <device-identifier>` | Stop script, delete KVS on the specified device |
| `ctl pool list` | List all devices running `pool-pump.js` with their KVS state |
| `ctl pool start <device-identifier> <eco\|mid\|high>` | Activate pump at given speed on specified device |
| `ctl pool stop <device-identifier>` | Stop pump on specified device |
| `ctl pool status [pattern]` | Show KVS state of all (or matching) pool-pump devices |
| `ctl pool purge <device-identifier>` | Stop switches, delete KVS, remove script from device |

**Key principle**: `preferred` KVS value determines which device activates. `ctl pool preferred` propagates this to all devices atomically.

---

## Timer Budget

Shelly scripts are limited to **5 timers**. Current usage:

| Timer | Purpose | Lifetime |
|-------|---------|---------|
| `TASK_TIMER` | Task queue (200 ms recurring) | Only while queue is non-empty |
| `STATE.graceTimer` | Inter-device grace delay | During switchover only |

Peak simultaneous: **2** (task queue + grace timer). Well within the 5-timer limit.

---

## Resource Limits Summary

| Resource | Limit | Used |
|----------|-------|------|
| Timers | 5 | ≤ 2 |
| Event subscriptions | 5 | 1 (`addEventHandler`) |
| MQTT subscriptions | 10 | ≤ 4 (1 per peer switch topic) |
| KVS keys | — | ≤ 20 config + 2 state |
