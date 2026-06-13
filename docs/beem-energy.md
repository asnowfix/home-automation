# Beem Energy Integration — Design & Implementation Spec

## Context

Beem Energy produces solar PnP kits (Hoymiles micro-inverters + DTU gateway). There is **no official public API**; the community has reverse-engineered two channels used by the Beem app.

### Hardware in use

**PnP kit only** (no Beem Battery). This constrains the available data channel to REST polling only.

### Data channels

| Channel | Hardware required | Data | Frequency |
|---|---|---|---|
| REST `api-x.beem.energy` | Any Beem kit | Solar production (W, daily Wh, monthly Wh) | ~60 s poll |
| MQTT (Beem broker) | Beem Battery only | Solar W + battery W + grid W (real-time) | Real-time |

**Key limitation:** With a PnP kit only, the REST API returns solar production figures but **no home consumption and no grid draw**. There is no way to know what fraction of consumption comes from the grid. The trigger must be production-based ("solar is producing > X W") rather than self-sufficiency-based ("solar covers > Y% of my load").

### REST API

- **Login:** `POST https://api-x.beem.energy/beemapp/user/login` with `{email, password}` → JWT `accessToken`
- **Data:** `GET https://api-x.beem.energy/beemapp/box/summary` (Bearer token) → instantaneous production W, daily Wh, monthly Wh
- **Token refresh:** on 401 or proactively 60 s before expiry; token is stateless in memory (no disk persistence needed)

### Community references

- `CharlesP44/Beem_Energy` (GitHub) — unofficial Home Assistant integration, supports REST + MQTT
- `ClaraVnk/home-assistant-beem-energy` (GitHub) — earlier YAML-based HA config, REST only

---

## Part 1 — `pkg/beem`

### Package layout

```
pkg/beem/
  types.go      — PowerSample struct, ClientConfig
  client.go     — REST client: login(), refreshToken(), pollSummary()
  watcher.go    — Watcher: runs poll loop, publishes to home MQTT broker
```

### `PowerSample` type

```go
type PowerSample struct {
    SolarW     float64   // instantaneous solar production (W)
    DailyWh    float64   // solar production today (Wh)
    MonthlyWh  float64   // solar production this month (Wh)
    // GridW   float64   // reserved: only available with Beem Battery MQTT channel
    Source     string    // "rest" or "mqtt"
    TS         time.Time
}
```

`GridW` is reserved for a future Beem Battery upgrade; callers should check `Source == "mqtt"` before trusting it.

### MQTT event published to home broker

**Topic:** `myhome/energy/beem/power` (retained)

```json
{
  "solar_w":     1230,
  "daily_wh":    4500,
  "monthly_wh":  62000,
  "source":      "rest",
  "ts":          "2026-05-30T14:00:00Z"
}
```

### Config stanza

Added to `options.go`, `run.go`, `docs/configuration.md`, `myhome-example.yaml` per the 4-file convention:

```yaml
beem:
  email:         "you@example.com"
  password:      "..."
  poll_interval: 60s
```

Env vars: `MYHOME_BEEM_EMAIL`, `MYHOME_BEEM_PASSWORD`

**Poll interval:** 60 s matches the interval used by community integrations (CharlesP44/Beem_Energy, ClaraVnk/home-assistant-beem-energy). The actual Beem app poll rate is not publicly documented; 60 s is treated as a minimum. Set `poll_interval` higher (e.g. `120s`) to be more conservative with the cloud API.

### Design constraints

- Auth token kept in memory only; no disk persistence
- **Both `email` and `password` must be non-empty** for the watcher to start. If either is absent, `Watcher.Start` returns immediately without starting the poll loop (no unauthenticated requests are ever made).
- `pkg/beem` exposes a `Watcher` with a `PowerCh <-chan PowerSample` channel — callers decide what to do with the data
- No SQLite, no KVS — this package is stateless
- Log each sample at `DEBUG`; log auth events and errors at `INFO`/`ERROR`

---

## Part 2 — Pool-pump solar trigger

### Goal

Run the pool pump during daylight hours when solar production exceeds a threshold, so that free solar energy contributes to the daily filtration objective (5× pool volume). Solar-driven runtime must count against the same daily objective as the scheduled runs — it substitutes for grid-powered runtime, not adds on top of it.

### Trigger architecture chosen: daemon goroutine (Option B)

A new goroutine in `myhome/daemon/solar_automation.go` subscribes to `myhome/energy/beem/power`, applies hysteresis, and controls the pool pump via the existing switch RPC. The pool-pump JS script and `PoolService` require no changes.

**Rejected options:**

- **Option A — Shelly JS subscribes to beem topic:** consumes 1 of 10 Shelly MQTT subscriptions; harder to unit-test; JS hysteresis is brittle.
- **Option C — PoolService subscribes directly:** violates the three-tier layer rule; couples `pkg/shelly` to Beem.
- **Option D — Prometheus/alerting bridge:** overkill for a hobby project.

### Hysteresis state machine

Variables `daily_target_sec` and `max_rotation_sec` are computed from `min_volume_turnover` / `max_volume_turnover` and KVS at startup — see "Runtime target computation" below.

```
IDLE  →  (solar_w ≥ start_threshold_w  for  start_delay
          AND  runtime < max_rotation_sec)                          →  RUNNING

RUNNING  →  (solar_w < stop_threshold_w  for  stop_delay)          →  IDLE  [solar loss]
RUNNING  →  (runtime ≥ daily_target_sec
             AND  solar_w < start_threshold_w)                      →  IDLE  [soft stop]
RUNNING  →  (runtime ≥ max_rotation_sec)                           →  IDLE  [hard ceiling]
```

**Soft stop vs. hard ceiling:**

- `daily_target_sec` (`min_volume_turnover × …`) is the normal filtration goal. Reaching it stops the pump *only if solar has also dropped below `start_threshold_w`*. While solar is still producing, the pump keeps running past the daily target — free energy is used to over-filter rather than going to waste.
- `max_rotation_sec` (`max_volume_turnover × …`) is an absolute ceiling. The pump always stops when this is reached, regardless of solar output.
- The pump will not be *started* via solar once `max_rotation_sec` is already reached.

**Combined stop logic on each power sample (RUNNING state):**

| Condition | Action |
|---|---|
| `runtime ≥ max_rotation_sec` | Stop immediately (hard ceiling) |
| `runtime ≥ daily_target_sec` AND `solar_w < start_threshold_w` | Stop immediately (soft stop: goal met, solar gone) |
| `solar_w < stop_threshold_w` held for `stop_delay` | Stop (solar loss, regardless of runtime) |
| otherwise | Keep running |

### New files

```
myhome/daemon/solar_automation.go     — hysteresis state machine, pump control
myhome/daemon/pool_runtime_tracker.go — daily runtime accumulator (see Part 3)
```

### Config additions (pool stanza)

```yaml
pool:
  solar:
    enabled:               true
    start_threshold_w:     500   # start pump when solar exceeds this
    stop_threshold_w:      200   # stop when solar falls below this
    start_delay:           5m    # must hold above threshold before starting
    stop_delay:            10m   # must hold below threshold before stopping
    min_volume_turnover:   5     # soft stop: stop when this many pool volumes filtered AND solar gone
    max_volume_turnover:   7     # hard ceiling: always stop when this many pool volumes filtered
```

`min_volume_turnover` and `max_volume_turnover` are dimensionless multipliers (pool volumes filtered per day). The daemon converts them to seconds at startup by reading the pool device KVS — see "Runtime target computation" below.

**Startup validation:** solar automation refuses to initialize if `max_volume_turnover < min_volume_turnover`. The daemon logs an error and continues without solar automation.

### Runtime target computation

At solar automation startup, the daemon reads four KVS keys from the pool Shelly device (identified by `pool.device_id`) — the same keys pool-pump.js uses for its own autonomous scheduling, nothing new is written:

| KVS key | Unit | Description |
|---|---|---|
| `script/pool-pump/pool-volume` | m³ | Pool water volume |
| `script/pool-pump/max-flow-rate` | m³/h | Flow rate at max RPM |
| `script/pool-pump/max-rpm` | RPM | Motor max speed |
| `script/pool-pump/speed` | RPM | Current operating speed |

From these the daemon derives:

```
flow_rate        = max_flow_rate × (speed / max_rpm)            [m³/h]
daily_target_sec = pool_volume × min_volume_turnover / flow_rate × 3600   [s]
max_rotation_sec = pool_volume × max_volume_turnover / flow_rate × 3600   [s]
```

**KVS write policy:** the daemon reads KVS at startup but never writes it for this feature. The device KVS remains exclusively the JS script's domain, preserving its ability to run autonomously if the daemon is down.

### Interaction with existing schedule (additive / substitute semantics)

- Solar goroutine starts the pump **only when** `runtime < max_rotation_sec` (or `max_rotation_sec == 0`)
- Solar goroutine stops the pump on solar loss, soft stop (target met + solar gone), or hard ceiling
- The normal JS schedule (morning start / evening stop / night run) fires independently; the runtime tracker counts all pump-on time regardless of who started the pump
- When `max_rotation_sec` is reached and the JS night schedule fires at 23:15, the daemon detects the pump turning on and sends a stop command ~30 s later (one relay click; no JS changes required)

### Daily target computation (Go side)

Same formula as `computeRunHours()` in pool-pump.js:

```
flowRate = maxFlowRate × (preferredRpm / maxRpm)          [m³/h]
targetSec = poolVolume × turnover / flowRate × 3600        [seconds]
```

Values read from pool KVS at daemon startup:
- `script/pool-pump/pool-volume`
- `script/pool-pump/turnover`
- `script/pool-pump/max-flow-rate`
- `script/pool-pump/max-rpm`
- `script/pool-pump/eco-rpm` (or whichever speed is `script/pool-pump/speed`)
- `script/pool-pump/max-power` — nameplate power at `max-rpm` (W); used to derive power per speed:
  `power_at_speed_w = max_power_w × (speed_rpm / max_rpm)`
  Example: 1600 W at 2900 rpm → eco at 1450 rpm ≈ 800 W. This feeds the aenergy-based runtime
  option (Option C, Part 3) and future speed-adaptive triggering (see follow-up issue).

---

## Part 3 — Daily runtime accumulator

### Problem

The accumulator must survive daemon restarts. A restart without replay would cause the solar trigger to over-run the pump (it thinks no filtration has happened yet today).

### Implemented approach — shared `events.db` + `OnDurationSec`

The gen2 listener (`internal/myhome/shelly/gen2/listener.go`) already records every `switch.on` / `switch.off` event from every Shelly device into the shared `events.db`. No separate pump table or pool-specific subscriber is needed.

A generic query `events.Storage.OnDurationSec(deviceID, component, onEvent, offEvent, date)` computes the total ON-duration for any switch on any calendar day:

```sql
SELECT COALESCE(SUM(
    COALESCE(
        (SELECT MIN(e2.ts) FROM events e2
         WHERE e2.device_id = e1.device_id AND e2.component = e1.component
           AND e2.event = <offEvent> AND e2.ts > e1.ts),
        unixepoch('now')          -- open interval: pump still running
    ) - e1.ts
), 0)
FROM events e1
WHERE e1.device_id = <deviceID> AND e1.component = <component>
  AND e1.event = <onEvent> AND date(e1.ts, 'unixepoch') = <date>
  AND COALESCE(
      (SELECT e0.event FROM events e0
       WHERE e0.device_id = e1.device_id AND e0.component = e1.component
         AND (e0.event = <onEvent> OR e0.event = <offEvent>)
         AND e0.ts < e1.ts ORDER BY e0.ts DESC LIMIT 1),
      <offEvent>
  ) = <offEvent>    -- deduplicate consecutive ON events
```

`PoolRuntimeTracker` (`myhome/daemon/pool_runtime_tracker.go`) wraps this query for the pool pump (`switch:0`, `switch.on` / `switch.off`).

| | |
|---|---|
| **Pros** | No new database or table; durable and exact across restarts (events already persisted by gen2 listener); open intervals handled natively (pump currently running); deduplication of reconnect-induced duplicate ONs built into the query; full audit trail reusable for Prometheus / statistics |
| **Cons** | Depends on daemon being up to record events (Shelly reboot while daemon is down drops that interval); query runs on every `canStart()` call (~once per poll interval while solar is above threshold) |

### Considered alternatives

**Option B — Shelly KVS persistence** (daemon writes `{date, runtime_sec}` to KVS every 5 min): survives daemon restarts, but adds KVS write latency, shares the KVS namespace with the JS script, and loses up to 5 minutes on crash.

**Option C — `aenergy` as ground truth** (hardware Wh counter → runtime via `power_at_speed_w`): viable because the Shelly switch controls a contactor (constant power draw), so `aenergy` is an exact runtime proxy. A JS-side variant (pool-pump.js reads `aenergy.total` via `Switch.GetStatus` and writes to KVS) would keep the accumulator on the device and survive daemon restarts.

**Why Option C (JS-side variant) was not chosen:** the shared `events.db` already provides the same guarantees without any JS changes, without consuming a JS timer slot or a KVS namespace entry, and with better restart semantics (daemon replay vs. Shelly reboot reset). The only scenario where the JS KVS variant wins is a prolonged daemon outage while the Shelly is running — unlikely for a home automation setup. Issue #246 is therefore closed as **won't do**.

---

## Implementation phases ✅

| Phase | Scope | Status |
|---|---|---|
| 1 | `pkg/beem` REST client + MQTT publisher | ✅ done |
| 2 | Config wiring (4-file convention) | ✅ done |
| 3 | Pool runtime tracker (`events.db` + `OnDurationSec`) | ✅ done |
| 4 | Solar automation goroutine | ✅ done |
| 5 | Daemon wiring | ✅ done |
| 6 | Tests | ✅ done |
| 7 | Soft stop + hard ceiling: `min_volume_turnover` / `max_volume_turnover` config; KVS read at startup to derive `daily_target_sec` / `max_rotation_sec`; startup validation | ✅ done |

---

## Open questions / future work

- **Beem Battery upgrade:** if a Beem Battery is added later, enable the MQTT channel in `pkg/beem` and populate `GridW` in `PowerSample`. The solar trigger can then switch to net-surplus mode (`solar_w - grid_w > threshold`) for more accurate triggering.
- **Prometheus metrics:** see follow-up issue #247. Publish `myhome/metrics/beem/solar_w` and `myhome/metrics/pool/runtime_today_sec` so Grafana can display them.
- **Speed-adaptive solar triggering:** see follow-up issue #248. Valid only if the multi-speed variator (currently managed by pro3) can be repaired. Would dynamically select pump speed based on available solar W.
