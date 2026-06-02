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

Env vars: `MYHOME_BEEM_EMAIL`, `MYHOME_BEEM_PASSWORD`, `MYHOME_BEEM_POLL_INTERVAL`

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

```
IDLE  →  (solar_w > start_threshold_w  for  start_delay)  →  RUNNING
RUNNING  →  (solar_w < stop_threshold_w  for  stop_delay)  →  IDLE
RUNNING  →  (remaining_runtime_sec <= 0)                   →  IDLE
```

### New files

```
myhome/daemon/solar_automation.go     — hysteresis state machine, pump control
myhome/daemon/pool_runtime_tracker.go — daily runtime accumulator (see Part 3)
```

### Config additions (pool stanza)

```yaml
pool:
  solar:
    enabled:             true
    start_threshold_w:   500    # start pump when solar exceeds this
    stop_threshold_w:    200    # stop when solar falls below this
    start_delay:         5m     # must hold above threshold before starting
    stop_delay:          10m    # must hold below threshold before stopping
```

### Interaction with existing schedule (additive / substitute semantics)

- Solar goroutine starts the pump **only when** `RemainingRuntimeSec() > 0`
- Solar goroutine stops the pump when solar drops OR `RemainingRuntimeSec() <= 0`
- The normal JS schedule (morning start / evening stop / night run) fires independently
- When the JS schedule starts the pump and `RemainingRuntimeSec() > 0`, the runtime tracker counts that time too — the schedule naturally fills the gap left by solar
- When `RemainingRuntimeSec() <= 0` and the JS night schedule fires at 23:15, the daemon detects the pump turning on and sends a stop command ~30 s later (one relay click; no JS changes required)

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

## Part 3 — Daily runtime accumulator (durability options)

### Problem

The accumulator (`dailyRuntimeSec`) lives in memory. A daemon crash or restart loses the count, causing the solar trigger to over-run the pump (it thinks no filtration has happened yet today).

### Option A — SQLite event log (recommended)

Log every pump ON/OFF transition to a `pump_events` table:

```sql
CREATE TABLE pump_events (
    id           INTEGER PRIMARY KEY,
    ts           DATETIME NOT NULL,
    type         TEXT NOT NULL,      -- 'on' or 'off'
    duration_sec INTEGER             -- NULL for 'on' rows; set on 'off'
);
```

**Normal operation:**
- Pump ON → `INSERT (ts=now, type='on')`
- Pump OFF → `INSERT (ts=now, type='off', duration_sec=elapsed)`

**On daemon restart:**
1. `SELECT SUM(duration_sec) FROM pump_events WHERE date(ts)=date('now') AND type='off'` → completed runtime
2. Call `Switch.GetStatus` RPC on pool device → if pump is ON now, find the last unmatched `type='on'` row, add `now - last_on_ts` to the sum
3. Accumulator is fully reconstructed

**Daily reset:** no explicit reset; the query filters by `date(ts)=date('now')`.

| | |
|---|---|
| **Pros** | Durable across restarts; exact restart recovery; full audit trail (history of all pump runs, useful for statistics and correlation with solar/temperature data); fits existing SQLite patterns in the project |
| **Cons** | New SQLite table; slightly more complex restart logic |

**Database path:** plain relative filename `pool.db` (matching `myhome.db` convention). Configurable via `pool.db` config key or `MYHOME_POOL_DB` env var.

---

### Option B — Shelly KVS persistence

The Go daemon writes `{date, runtime_sec}` to the pool Shelly's KVS, throttled to every 5 minutes:

- `script/pool-pump/runtime-today-date` → `"2026-05-30"`
- `script/pool-pump/runtime-today-sec`  → `"14400"`

**On daemon restart:** read both KVS keys via `KVS.Get` RPC; if date matches today, restore the counter; otherwise start at 0.

| | |
|---|---|
| **Pros** | Survives daemon AND NAS/broker restarts simultaneously (Shelly KVS is flash-backed); simplest state structure; no new storage dependency |
| **Cons** | Up to 5-minute granularity loss on crash; KVS writes over MQTT add latency; KVS namespace is shared with the JS script (risk of key collision if JS is updated); date-mismatch edge case at midnight rollover |

---

### Option C — Shelly `aenergy` as ground truth

The Shelly switch already accumulates `aenergy.total` (total Wh since last reset, hardware counter). Record baseline Wh at midnight; compute daily runtime from energy delta:

```
daily_wh = aenergy.total - midnight_baseline_wh
runtime_sec = daily_wh / power_at_speed_w × 3600
```

Baseline stored as a retained MQTT message (`myhome/pool/aenergy-baseline`) written once at midnight.

**Key hardware note:** the Shelly switch controls a contactor, not a VFD. Once the contactor is closed, the pump runs at a fixed speed with constant power draw. There is no power variation while running — `aenergy` is therefore a direct, exact proxy for runtime (no averaging or approximation needed). The `power_at_speed_w` value is a one-time constant derived from `max-power` KVS key (see daily target computation above).

**Variant — pool-pump.js subscribes to energy topic:** the JS script can call `Shelly.call("Switch.GetStatus")` periodically to read `aenergy.total`, accumulate daily Wh in a variable, and write `{date, runtime_sec}` to KVS whenever it changes by more than ~5%. This keeps the accumulator on the device, survives daemon restarts, and requires no Go changes.

| | |
|---|---|
| **Pros** | Hardware counter is independent of daemon uptime; with contactor control, energy→runtime is exact (not indirect); JS-side variant survives daemon restarts without any daemon code |
| **Cons** | `aenergy.total` resets on Shelly reboot (mitigated by reading at every timer tick and detecting drops); midnight baseline still needs durable storage; JS-side variant requires a pool-pump.js change and uses one of the 5 recurring timers |

**Open question (see follow-up issue #246):** with contactor control making Option C viable and simpler than originally assessed, should Option C replace the already-implemented Option A SQLite tracker, or run alongside it as a cross-check?

---

## Implementation phases

| Phase | Scope | Files |
|---|---|---|
| 1 | `pkg/beem` REST client + MQTT publisher | `pkg/beem/types.go`, `client.go`, `watcher.go` |
| 2 | Config wiring | `options.go`, `run.go`, `docs/configuration.md`, `myhome-example.yaml` |
| 3 | Pool runtime tracker (Option A: SQLite) | `myhome/daemon/pool_runtime_tracker.go`, migration in `pool.db` |
| 4 | Solar automation goroutine | `myhome/daemon/solar_automation.go` |
| 5 | Daemon wiring | `myhome/daemon/` startup sequence |
| 6 | Tests | `pkg/beem/*_test.go`, `myhome/daemon/solar_automation_test.go`, `pool_runtime_tracker_test.go` |

Implement phases in order. Mark each phase done in this file before starting the next.

---

## Open questions / future work

- **Beem Battery upgrade:** if a Beem Battery is added later, enable the MQTT channel in `pkg/beem` and populate `GridW` in `PowerSample`. The solar trigger can then switch to net-surplus mode (`solar_w - grid_w > threshold`) for more accurate triggering.
- **aenergy-based runtime tracker (Option C):** see follow-up issue #246. With contactor control, Option C is simpler and more hardware-independent than assessed in the original design. Decide whether to replace or complement the SQLite tracker.
- **Prometheus metrics:** see follow-up issue #247. Publish `myhome/metrics/beem/solar_w` and `myhome/metrics/pool/runtime_today_sec` so Grafana can display them.
- **Speed-adaptive solar triggering:** see follow-up issue #248. Valid only if the multi-speed variator (currently managed by pro3) can be repaired. Would dynamically select pump speed based on available solar W.
