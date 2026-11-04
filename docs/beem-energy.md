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
- **Data:** `POST https://api-x.beem.energy/beemapp/box/summary` (Bearer token) with body `{month, year}` (current period) → JSON array, one entry per registered Beem box: `wattHour` (instantaneous production W, despite the name), `totalDay` (Wh), `totalMonth` (Wh). This project assumes a single-box household and reads `[0]`.
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

### Revision note (#401 redesign)

This section originally documented a daemon-side goroutine (`myhome/daemon/solar_automation.go`)
as "Option B — daemon goroutine," and explicitly rejected "Option A — Shelly JS subscribes to the
beem topic" on the grounds of the JS MQTT-subscription budget and brittle JS hysteresis. Both of
those calls are now **reversed** — issue #401 found that:

- The daemon's `Switch.Set` bypassed `pool-pump.js`'s own `doStart`/`doStop`/anti-cycling-fuse/
  `isMyTurnToRun()`/speed-mapping logic, so a solar-triggered start could be silently vetoed by the
  device's own safety logic with no feedback loop, and the pump's solar behavior depended entirely
  on the daemon being up — violating this repo's "daemon-optional per device" resilience rule (see
  CLAUDE.md).
- JS unit-testability, the original objection to Option A, turned out to be fully solved by the
  existing `script.RunWithDeviceState` emulator harness with a real mock MQTT client — the same
  harness already used to test the rest of `pool-pump.js`.
- The MQTT subscription-budget concern was real but manageable: solar hysteresis is one more
  subscription (`myhome/energy/solar/available`) within the existing 10-subscription Shelly limit,
  not a new category of cost.

**Current architecture:** the daemon aggregates known solar sources (today: only Beem — see Part 1)
and republishes the total, retained, to `myhome/energy/solar/available` (`myhome/daemon/solar_aggregate.go`,
implemented in #403). `pool-pump.js` subscribes to that topic directly and applies its own
KVS-configured hysteresis (`script/pool-pump/solar-*` keys — see `docs/pool-pump.md`), calling its
own `doStart`/`doStop` exactly as scheduled/manual runs do (implemented in #405). All daemon-side
`Switch.Set` calls and the old hysteresis state machine were deleted in #406.

When the solar-availability topic is stale or absent, the pump simply keeps running its existing
forecast-derived schedule (Open-Meteo) — no separate fallback code; solar hysteresis is purely
additive on top of it.

### Runtime target computation

The daily-target/hard-ceiling seconds (`daily_target_sec` / `max_rotation_sec`) are now computed
on-device by `pool-pump.js` itself, from the same KVS keys it already reads for scheduling
(`script/pool-pump/{pool-volume,max-flow-rate,max-rpm,speed}`) — no daemon-side KVS read for this
purpose remains. See `docs/pool-pump.md` for the current formula and KVS key list.

---

## Part 3 — Daily runtime accumulator

### Problem

The accumulator must survive daemon restarts (and, since the #401 redesign, must survive the daemon
being down entirely — the pump's own solar/schedule decisions no longer depend on it being up).

### Implemented approach — on-device KVS accumulator (#402)

`pool-pump.js` accounts for its own daily runtime and turnover directly in `activateOutput()`
(on/off-transition accounting), mirroring both to KVS as it runs:

- `script/pool-pump/runtime-sec` — cumulative seconds run today
- `script/pool-pump/turnover-today` — cumulative water-volume turnovers achieved today

The daemon's `PoolNotices` type (`myhome/daemon/pool_notices.go`) reads these two keys back (plus
the configured `script/pool-pump/turnover` target) for `ctl pool status`, the web UI's pool tags,
and the `pool.turnover_today` notice — no daemon-side computation, no `events.db` dependency for
runtime tracking.

This supersedes the originally-implemented `events.db`/`OnDurationSec` approach (below), which
depended on the daemon being up to record every `switch.on`/`switch.off` event and has since been
deleted (`myhome/daemon/pool_runtime_tracker.go`, removed in #406).

**Issue #246** ("aenergy-based runtime tracker"), previously closed as **won't do** in favor of the
`events.db` approach, is effectively resolved by this redesign: `pool-pump.js` now tracks its own
on/off transitions directly rather than polling `aenergy` — a different (simpler, exact) mechanism
than #246 proposed, but the same on-device outcome. See #402 for the implementation.

### Historical record — original `events.db` + `OnDurationSec` approach

The gen2 listener (`internal/myhome/shelly/gen2/listener.go`) recorded every `switch.on` / `switch.off` event from every Shelly device into the shared `events.db`. A generic query `events.Storage.OnDurationSec(deviceID, component, onEvent, offEvent, date)` computed the total ON-duration for any switch on any calendar day:

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

`PoolRuntimeTracker` wrapped this query for the pool pump (`switch:0`, `switch.on` / `switch.off`).
Its main drawback — the one that motivated the #401 redesign — was depending on the daemon being up
to record events at all, so a Shelly reboot (or extended daemon outage) while the pump was running
would drop that interval, and pump behavior itself depended on the daemon for `canStart()` checks.

---

## Implementation phases

See issue #401 (umbrella) and its sub-issues for the current implementation history:

| Issue | Scope | Status |
|---|---|---|
| #402 | `pool-pump.js`: on-device runtime/turnover accumulator (fixes `ctl pool status`) | ✅ done |
| #403 | Daemon: aggregate solar sources, publish `myhome/energy/solar/available` | ✅ done |
| #405 | `pool-pump.js`: solar-driven start/stop hysteresis | ✅ done |
| #404 | Daemon: minimal energy-claimers registry + RPC | ✅ done |
| #406 | Remove legacy `SolarAutomation` daemon code + config/docs cleanup (this rewrite) | ✅ done |

The original phase table (daemon-side `solar_automation.go` + `pool_runtime_tracker.go`) is
superseded by the above and preserved only in git history.

---

## Open questions / future work

- **Beem Battery upgrade:** if a Beem Battery is added later, enable the MQTT channel in `pkg/beem` and populate `GridW` in `PowerSample`. The solar trigger can then switch to net-surplus mode (`solar_w - grid_w > threshold`) for more accurate triggering.
- **Prometheus metrics:** see follow-up issue #247. Publish `myhome/metrics/beem/solar_w` and `myhome/metrics/pool/runtime_today_sec` so Grafana can display them.
- **Speed-adaptive solar triggering:** see follow-up issue #248. Valid only if the multi-speed variator (currently managed by pro3) can be repaired. Would dynamically select pump speed based on available solar W.
