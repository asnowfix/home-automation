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

### Design constraints

- Auth token kept in memory only; no disk persistence
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
runtime_sec = daily_wh / avg_power_w(current_speed) × 3600
```

Baseline stored as a retained MQTT message (`myhome/pool/aenergy-baseline`) written once at midnight.

| | |
|---|---|
| **Pros** | Hardware counter is completely independent of daemon uptime; no mid-day accumulator to lose |
| **Cons** | Requires configuring pump power draw per speed (W at eco/mid/high — must be measured or estimated); `aenergy.total` resets on Shelly reboot; energy→runtime conversion is indirect and less precise; midnight baseline still needs durable storage (deferred problem, not eliminated) |

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
- **Prometheus metrics:** publish `myhome/metrics/beem/solar_w` and `myhome/metrics/pool/runtime_today_sec` so Grafana can display them.
- **Speed selection during solar run:** currently uses `preferredSpeed` from KVS. Could dynamically select speed based on available solar W (more sun → higher speed → faster turnover).
