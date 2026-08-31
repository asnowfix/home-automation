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

## Water Supply Input — a LEVEL, not an event

**This is normal operation. Do not troubleshoot it as a fault.** It has cost more than one
investigation already.

`input:0` on the pump device is the **water-supply level** reported by a water gauge. The gauge fills
the pool up to a target level and then stops. The device surfaces changes as *events*, and
`handleInputEvent()` → `setWater()` consumes them as edges, but the underlying signal is a **level**,
not an edge — that distinction is what makes the observed behaviour make sense.

### What this looks like in normal operation

Running the pump draws the level down, the gauge trips, the pump stops until the gauge has refilled
to target, then the pump resumes. Measured on `filtration-hiver`:

| date | pump ran | protected | released | protection lasted |
|---|---|---|---|---|
| 2026-08-18 | 11:12:44 | 11:20:50 | 11:47:54 | ~27 min |
| 2026-08-19 | 12:17:01 | 12:23:29 | 12:43:47 | ~20 min |

So a mid-window stop of roughly 20–30 minutes, a few minutes after the pump starts, with
`pool.pump_stop {"reason":"water supply"}`, **is the interlock working**. The durations vary because
they track the refill, not a timer.

### The supply is a TANK, and rain refills it

**Maintainer, 2026-08-23.** The water the gauge tops the pool up from is a **tank**, not an unlimited
mains feed. So how often the interlock fires tracks the tank level, which tracks recent rainfall.

This is the difference between two readings of the same observation:

| dates | interlock | why |
|---|---|---|
| 2026-08-18, 08-19 | tripped a few minutes into every window, ~20–30 min each | tank low |
| 2026-08-21, 08-22 | **did not fire at all** | a rain storm had refilled the tank |

**So the absence of trips is not evidence that anything was fixed.** It means it rained. I recorded
"the interruption did not recur, so my earlier framing over-generalised" — that was half right: it is
not a daily certainty, but the reason is the tank, not chance. It will return in dry weather.

**The consequence worth thinking about:** a dry spell lowers the tank, which makes the interlock fire
more often, which costs filtration time — during exactly the weather when evaporation is highest and
the pool most needs filtering. The failure mode is self-reinforcing rather than random, and that is
the case #524's runtime recovery is really for.

Unknown and not assumed here: whether the tank also has a mains top-up, what its capacity is, and
what refill rate the gauge achieves. Anyone tuning thresholds should establish those first rather
than inferring them from pump behaviour.

### Two ways this has already misled an investigation

1. **`F_WATER` is a latch that clears.** Probing it hours after the event shows `false` and looks
   like protection never fired. It reflects *now*, not the day.
2. **`Switch.GetStatus.input.state` and `F_WATER` can legitimately disagree**, because one is the
   instantaneous level and the other is the last edge the script consumed. A single disagreeing
   sample is not evidence of a bug.

If the pump appears to stop mid-window for no reason, **look for `pool.water_supply_protected` in the
events DB first** — and remember events can be lost when the device's MQTT link flaps (#499), so
absence is not evidence of absence.

### What *is* worth acting on

- The pump not reaching its turnover target because protection eats the window — that is a **supply**
  matter (flow rate, gauge target, refill time), not a code defect. See #524 for the software half:
  the run window is a fixed clock interval and does not compensate for time lost.
- `F_WATER` is maintained from edges only (plus a one-time read at init) — **deliberately, not as a
  gap**. A periodic reconciler was proposed and implemented in #523, then withdrawn: the only
  evidence on record is one 2026-08-19 sample in the *opposite* direction from the one a safety
  argument needs, and the issue itself says a single sample cannot distinguish a missed edge from a
  genuine input flap. On this supply's ~30-minute flip/flop cadence, a missed edge self-corrects at
  the next transition well before any periodic tick would fire, and no lost *protecting* edge has
  ever been observed. See #523 for the full argument; it is relabelled `blocked-diagnosis` and stays
  open pending real evidence rather than a hypothesis.


### Priority: water supply overrides everything, including solar

Running dry can destroy the pump, so the water-supply level is an **absolute** override — not one
input among several. `desiredOutput()` encodes this as the first line of the policy:

```js
if (F_WATER) return -1;      // safety first, always
```

Nothing below it — manual override, solar, the run window — can re-enable the pump while the supply
is low. **Any change to the policy ordering must keep this first.**

**Now covered by `TestPoolPump_WaterSupplyOverridesSolar`** (#523, split off #527's Test 3): boots
protected (`input:0.state=true`), publishes solar availability well above `solarStartThresholdW`
with zero start delay, and asserts the pump stays off. Verified as a real regression check by
temporarily removing the `if (F_WATER) return -1;` line — the test fails without it.

### Solar accumulates *through* a resupply, and starts the pump when it ends

The solar opinion is deliberately independent of the pump's physical state. `checkSolarHysteresis()`
runs on every solar MQTT message and on its own periodic tick, and it is **not** gated on `F_WATER`,
so while the pump is held off for a refill:

- solar readings keep arriving and keep being evaluated;
- `SOLAR.aboveStartSince` keeps accumulating toward `solarStartDelayMs`;
- `F_SOLAR_WANT` can therefore *become* true during the hold.

When the gauge releases, `setWater(false)` calls `reconcile("water supply off")`, and
`desiredOutput()` — now past the `F_WATER` guard — reaches `if (F_SOLAR_WANT) return
mapSpeedToSwitch(...)`. **The pump starts immediately at the end of the resupply**, with no wait for
the next solar evaluation and no re-arming of the start delay.

This is a consequence of #476 making `F_SOLAR_WANT` a *latched opinion* rather than a reading of the
relay. The comment there records why: reading physical state meant a window-driven run suppressed
solar's start hysteresis, so "solar went away" could cut a scheduled run short.

**Status: correct by construction, not yet proven by test.** There is no emulator test covering
"solar rises during a water hold, pump starts on release". Until there is, this section describes the
intended design and a reading of the code — not verified behaviour. See #524.


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

Every morning at sunrise, `handleDailyCheck()` fetches the Open-Meteo forecast (which includes `hourly=temperature_2m&daily=sunrise,sunset`), computes how many hours the pump should run today, centres that window on the hottest forecast hour, and calls `Schedule.Update` to write absolute `HH:MM` timespecs for the morning-start and evening-stop schedules.

This replaces the old fixed `@sunrise+3h` / `@sunset` timespecs. The algorithm only applies in summer mode (above `temp-threshold`). Winter mode is unchanged.

**Pro1 note**: the Pro1 can only toggle on/off at max speed — it does not participate in proportional scheduling. The algorithm runs on the Pro3 (the preferred device in summer).

### Run-hours calculation

```
flowRate  = maxFlowRate × (preferredSpeedRpm / maxRpm)
baseHours = (poolVolume × turnover) / flowRate
scale     = clamp((todayMaxTemp − tempThreshold) / (maxTemp − tempThreshold), 0, 1)
runHours  = clamp(baseHours × scale, baseHours × 0.5, baseHours × 1.5)
```

- At `tempThreshold` → scale = 0 → pump runs `baseHours × 0.5` (minimum, half a turnover)
- At `maxTemp` → scale = 1 → pump runs `baseHours` (one full turnover)
- Above `maxTemp` → scale clamped to 1 → same as at `maxTemp`
- The `baseHours × 1.5` ceiling is a safety cap; it is not reachable through normal temperature variation

### Window centering

```
startHour = peakHour − runHours / 2
stopHour  = peakHour + runHours / 2
```

`peakHour` is the index (= hour of day) of the maximum temperature in the hourly forecast — the pump runs centred on the hottest part of the day when UV and algae pressure are highest.

Boundary enforcement (applied in order):
1. If `startHour < sunriseHour + 1h` → shift window forward: `startHour = sunriseHour + 1h`, `stopHour = startHour + runHours`
2. If `stopHour > sunsetHour − 0.5h` → shift window backward: `stopHour = sunsetHour − 0.5h`, `startHour = stopHour − runHours`
3. Hard floor `startHour = max(startHour, sunriseHour + 1h)` — applied after both shifts in case `runHours` is longer than the available window

When `runHours` exceeds the available window (e.g. eco speed with a large pool), the pump runs the full available window rather than failing.

### Worked example (default config, southern France summer)

- Pool: 46 m³, turnover target: 5×, preferred speed: eco (2000 rpm)
- `flowRate` = 31 × (2000 / 2900) ≈ 21.4 m³/h
- `baseHours` = (46 × 5) / 21.4 ≈ 10.7 h

| Forecast max | scale | runHours | Centred on 14:00 | Start | Stop |
|---|---|---|---|---|---|
| 20 °C (threshold) | 0.0 | 5.4 h (min) | 11:18 – 16:42 | 11:18 | 16:42 |
| 25 °C | 0.33 | 3.6 h | 12:12 – 15:48 | 12:12 | 15:48 |
| 30 °C | 0.67 | 7.1 h | 10:27 – 17:33 | 10:27 | 17:33 |
| 35 °C (max-temp) | 1.0 | 10.7 h | 08:39 – 19:21 | 08:39 | 19:21 |

At high speed (2900 rpm, 31 m³/h): `baseHours` = (46 × 5) / 31 ≈ 7.4 h — shorter windows, same centering.

### Fallback values

| Condition | Fallback |
|-----------|----------|
| `peakForecastHour` not available | 14:00 |
| `sunriseHour` not available | 06:00 |
| `sunsetHour` not available | 21:00 |
| Flow rate zero or invalid config | 8 h fixed |
| Forecast fetch fails | schedule unchanged, mode preserved |

### Upgrade note — forecast URL migration

Devices running a pre-#230 version of the script have a forecast URL without `daily=sunrise,sunset` in `Script.storage`. On the first `handleDailyCheck()` after upgrade, `ensureForecastUrl()` detects the missing `daily=` parameter, re-detects the device location, and rebuilds the URL. No manual intervention is needed.

---

## Solar-Driven Hysteresis (#405)

Layers a solar start/stop hysteresis on top of the existing forecast-driven schedule — **strictly additive**, never a replacement scheduler. When solar is disabled, absent, or stale, the schedule above runs exactly as if this feature didn't exist.

### Data source

The daemon publishes a retained MQTT topic `myhome/energy/solar/available` (see #403):
```json
{ "available_w": 1230, "ts": 1732650000, "sources": [{"name":"beem","watts":1230,"stale":false}] }
```
`ts` is unix-epoch-seconds — the time the daemon *computed* the value, not when this script received it. `sources` is optional debug detail the script ignores.

### Staleness is judged from `ts`, not receipt time

MQTT delivers retained messages to a new subscriber immediately, no matter how old they are. If the daemon published a value and then died, and this script rebooted and re-subscribed hours or days later, it would receive that stale retained message immediately on subscribe. Tracking `Date.now()` at *message-receipt* time as the freshness marker would make that stale value look perfectly fresh — defeating the entire point of a staleness check.

Instead, `SOLAR.publishedTs` is derived from the payload's own `ts` field (converted to ms), and `checkSolarHysteresis()` compares `Date.now() - SOLAR.publishedTs` against `CONFIG.solarStaleMs`. `SOLAR.lastMsgTs` (receipt time) is tracked separately for debugging only and never feeds the staleness decision.

### Hysteresis state machine

Ported from the (now-deleted) Go `SolarAutomation.step()` state machine, but calling this script's own `doStart(speed, reason)` / `doStop(reason)` instead of a raw `Switch.Set` — so the fuse, `isMyTurnToRun()`, and water-supply protection remain in force for solar-triggered runs exactly as for scheduled/manual runs.

| State | Trigger | Action |
|-------|---------|--------|
| Off, solar ≥ `solar-start-w` for `solar-start-delay` ms | and hard ceiling not reached | `doStart(preferredSpeed, ...)` |
| On, hard ceiling reached | (any solar level) | `doStop(...)` — always wins |
| On, soft target reached | and solar < `solar-start-w` | `doStop(...)` — solar gone after meeting the daily minimum |
| On, solar < `solar-stop-w` for `solar-stop-delay` ms | | `doStop(...)` |
| Topic stale or never received | | early return — schedule keeps running untouched |

Re-evaluated on every solar MQTT message (`onSolarAvailable` calls `checkSolarHysteresis()` synchronously) **and** on a 30-second periodic tick (`SOLAR.tickTimer`, only allocated when `solarEnabled`), so staleness is detected even if the daemon dies mid-hold-delay and no further MQTT message ever arrives.

### Soft-stop / hard-ceiling targets

Both reuse `computeFlowRate()` (added by #402) so the speed→RPM mapping is only ever implemented once:
```
target = poolVolume × turnoverFraction / flowRate × 3600   (seconds)
```
- **Soft target** (`solar-min-turnover`, default 5): once reached, the pump keeps running while solar stays available, but stops as soon as solar drops below `solar-start-w`.
- **Hard ceiling** (`solar-max-turnover`, default 7): the pump always stops (and won't solar-start) once reached, regardless of solar availability.

### Config (KVS, all under `script/pool-pump/`)

| Key | Default | Meaning |
|-----|---------|---------|
| `solar-enabled` | `false` | Enables the whole feature; subscription and tick timer are only allocated when true |
| `solar-start-w` | `500` | Available solar power (W) required to trigger a solar start |
| `solar-stop-w` | `200` | Available solar power (W) below which a solar-driven run stops |
| `solar-start-delay` | `300000` (5 min) | Solar must hold above the start threshold this long before starting |
| `solar-stop-delay` | `600000` (10 min) | Solar must hold below the stop threshold this long before stopping |
| `solar-min-turnover` | `5` | Soft-stop target (pool volumes/day) |
| `solar-max-turnover` | `7` | Hard ceiling (pool volumes/day) |
| `solar-stale-ms` | `300000` (5 min) | Treat the topic as stale (fall back to schedule only) after this long without a **fresh-`ts`** message |
| `override-ms` | `7200000` (2 h) | How long a manual override (button press, or an out-of-band `switch:N` change) holds the pump against the schedule/solar policy. The reconciler would otherwise revert a hand-made change within one 200 ms task-queue tick |

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
| `solar-enabled`  | `false` | Enable solar-driven start/stop (see #405) |
| `solar-start-w`  | `500` | Solar power (W) required to trigger a solar start |
| `solar-stop-w`   | `200` | Solar power (W) below which a solar-driven run stops |
| `solar-start-delay` | `300000` | Hold time (ms) above start threshold before starting |
| `solar-stop-delay`  | `600000` | Hold time (ms) below stop threshold before stopping |
| `solar-min-turnover` | `5` | Soft-stop target (pool volumes/day) |
| `solar-max-turnover` | `7` | Hard ceiling (pool volumes/day) |
| `solar-stale-ms` | `300000` | Treat solar topic as stale after this long without a fresh `ts` |
| `override-ms` | `7200000` | How long a manual override holds against the policy |

### State (managed by script, per device)
| Key | Notes |
|-----|-------|
| `active-output` | `-1` or switch ID currently active |
| `schedule-mode` | `"summer"` or `"winter"` |
| `runtime-sec` | Cumulative pump-on seconds today (see #402) |
| `runtime-ts` | Epoch second `runtime-sec` applies to; KVS recovery path if `Script.storage` is lost, e.g. a script reinstall (see #469) |
| `turnover-today` | Pool-volume turnovers achieved today (see #402) |

### Script.storage (script-private)
| Key | Notes |
|-----|-------|
| `forecast-url` | Open-Meteo URL built from GPS coordinates |
| `my-device-id` | Cached device ID from `Shelly.getDeviceInfo().id` |
| `runtime` | Boot-safe mirror of today's runtime counter, as one JSON object `{sec, ts}` (see #402, #469). The day is derived from `ts` (epoch seconds), never from a stored date string — a pre-#469 device may still have the legacy `runtime-sec` / `runtime-date` scalar pair, migrated to `runtime` once on first boot after upgrade. |

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
| `STATE.runtimeFlushTimer` | Runtime checkpoint (60 s, see #402/#547) | Only while the pump is running |
| `SOLAR.tickTimer` | Solar hysteresis re-evaluation (30 s recurring, see #405) | Only while `solar-enabled` is true |

Peak simultaneous: **4** (task queue + grace timer + runtime flush timer + solar tick timer). Well
within the 5-timer limit.

**#547**: `STATE.runtimeFlushTimer` used to be a single recurring `Timer.set(60000, true, ...)`.
On a 7h32m production run that timer returned a live handle but never fired again — the whole
run's accrual survived only in RAM until the stop. A first fix attempt moved it onto the task
queue's 200 ms drain via a self-requeuing task, but that leaks one `TASK_QUEUE` entry per tick for
as long as the pump runs (`processTaskQueue()` only compacts the array once it fully drains, which
a task that always re-queues itself before returning never allows) — a likely OOM on a long run.
It now re-arms a **fresh one-shot** `Timer.set(60000, false, ...)` at the end of every checkpoint
(`runtimeFlushFire()`) instead of relying on `repeat = true` to keep firing unattended for hours —
still exactly one timer slot, held only while the pump runs, same as before #547.
`STATE.runtimeFlushCount`/`runtimeFlushLastTs` let a hardware probe observe the chain is still
advancing, rather than only inferring it from `STATE.runStartTs` staying put.

---

## Resource Limits Summary

| Resource | Limit | Used |
|----------|-------|------|
| Timers | 5 | ≤ 4 |
| Event subscriptions | 5 | 1 (`addEventHandler`) |
| MQTT subscriptions | 10 | ≤ 5 (up to 4 peer switch-status topics — see note below — + 1 solar-available topic when `solar-enabled`) |
| KVS keys | — | ≤ 28 config + 4 state |

**Peer switch-status subscription count**: `ctl pool add`/`setupDevice()` writes *both* `pro3-id` and `pro1-id` KVS keys identically on every device in the mesh (including a device's own ID, when it is itself the pro3 or pro1). As a result, a Pro3 device subscribes to 1 pro1 topic *plus* 3 topics for its own switches (harmless but redundant self-subscription), and a Pro1 device subscribes to 3 pro3 topics *plus* 1 topic for its own switch — 4 peer subscriptions on either device type today, independent of this issue. Verified empirically at implementation time (not just estimated from the issue text, which assumed 2).
