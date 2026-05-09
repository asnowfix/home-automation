# Event Log Plan

Global persistent event log for all home-automation devices — queryable via CLI and UI, auto-truncated, zero duplication.

## Decisions recorded

| # | Decision |
|---|----------|
| 1 | Separate DB file `~/.myhome/events.db` (not shared with `devices.db`) |
| 2 | `myhome ctl events follow` tails via SSE `/events` HTTP stream |
| 3 | Power metering event types defined now; threshold config deferred until solar panel hardware arrives |
| 4 | Gen1 temperature & humidity readings → daily stats tracker only; NOT stored as individual event rows |
| 5 | BLU `device_id` = BLU sensor MAC (from BTHome payload), not gateway ID |
| 6 | Daily min/max only; shared `SensorDailyTracker` for illuminance lux, temperature tC, humidity rh; state persisted to DB on every update (restart-safe) |

---

## 1. Goals

- Capture every meaningful state-change event from every device (Shelly Gen1, Gen2, BLU, …).
- Persist in a dedicated `events.db` SQLite file; survives daemon restarts; queryable without replaying MQTT.
- Surface via `myhome ctl events` CLI and a new Events page in the web UI with live SSE push.
- Shared `SensorDailyTracker` subsystem handles daily min/max/avg for all numeric sensors (illuminance lux, temperature tC, humidity rh); state is written to DB on every sample so a daemon restart never loses the current day's running extremes.
- Auto-purge events older than a configurable retention period (default 90 days).
- No duplicate rows — MQTT QoS-1 redelivery and double-listener paths are idempotent via a UNIQUE constraint.

---

## 2. Event Catalog

### 2.1 Gen2 — via `NotifyEvent` on `<device_id>/events/rpc`

All Gen2 events arrive as a single MQTT topic with a structured payload:

```json
{
  "src": "shellypro4pm-aabbccdd1234",
  "method": "NotifyEvent",
  "params": {
    "ts": 1631266595.44,
    "events": [
      { "component": "switch:0", "id": 0, "event": "switch.on", "ts": 1631266595.44 }
    ]
  }
}
```

| Component           | Events stored as rows                                              | Notes |
|---------------------|--------------------------------------------------------------------|-------|
| `switch:<id>`       | `switch.on`, `switch.off`                                         | Core home-automation events |
| `switch:<id>`       | `switch.active_power_change`                                      | Power spike; severity `warn` once threshold config exists |
| `switch:<id>`       | `switch.active_power_measurement`                                 | 60 s monotonic; feeds daily energy stats (infrastructure ready, hardware pending) |
| `light:<id>`        | `light.on`, `light.off`                                           | Dimmers / RGBW |
| `input:<id>`        | `input.toggle_on`, `input.toggle_off`                             | Physical toggle |
| `input:<id>`        | `input.button_push`, `input.button_longpush`, `input.button_doublepush`, `input.button_triplepush` | Button presses |
| `input:<id>`        | `input.analog_change`, `input.count_change`, `input.freq_change`  | Analog / counter sensors |
| `illuminance:<id>`  | `illuminance.change`                                              | Carries `illumination` (dark/twilight/bright) and `lux`; also feeds `SensorDailyTracker` |
| `temperature:<id>`  | `temperature.change`                                              | Gen2 only emits above a delta threshold — not 60 s measurements; also feeds tracker |
| `humidity:<id>`     | `humidity.change`                                                 | Same as temperature |
| `em:<id>`           | `em.measurement`                                                  | Energy meter (Shelly Pro 3EM etc.); feeds daily energy stats; **hardware pending** |
| `emdata:<id>`       | `emdata.measurement`                                              | Accumulated energy data; **hardware pending** |
| `smoke:<id>`        | `smoke.alarm`, `smoke.alarm_off`, `smoke.alarm_test`              | Safety-critical; severity `alarm` |
| `sys`               | `ota_begin`, `ota_success`, `ota_error`, `scheduled_restart`, `component_added`, `component_removed` | Firmware & system lifecycle |

`temperature.measurement` and `humidity.measurement` (60 s periodic) are **not** stored as event rows — they would add ~1440 rows/device/day. Only `*.change` events become rows; the tracker handles running extremes.

### 2.2 Gen2 — Device connectivity via `<device_id>/online`

Shelly Gen2 publishes `true` on MQTT connect (retained) and `false` via LWT on disconnect.

Synthetic event types: `device.online` / `device.offline`. `component = "mqtt"`, `data = null`.

Retained messages are replayed by the broker on every reconnect — connectivity state is reconstructed at startup without gaps.

### 2.3 Gen1 — via existing `gen1/listener.go`

Gen1 devices do not emit a unified `NotifyEvent`.

| Derived event           | Source topic                              | Stored as event row? |
|-------------------------|-------------------------------------------|----------------------|
| `switch.on` / `switch.off` | `shellies/<id>/relay/<n>` → `on`/`off` | Yes |
| `device.online` / `device.offline` | `shellies/<id>/online` → `1`/`0` | Yes |
| temperature reading     | `shellies/<id>/sensor/temperature`        | **No** — tracker only |
| humidity reading        | `shellies/<id>/sensor/humidity`           | **No** — tracker only |

Gen1 temperature/humidity measurements (~every 12 min, ~120/device/day) are fed directly into `SensorDailyTracker`. No individual event rows are written. The daily min/max synthetic events (`temperature.daily_min`, `temperature.daily_max`) are the only event rows written for Gen1 sensors, once per device per day at midnight.

Gen1 does not expose firmware events over MQTT; skip.

### 2.4 BLU — via existing `blu/listener.go`

`device_id` = BLU sensor MAC address (from BTHome payload addr field), not the gateway device ID.

| Derived event          | BTHome object ID                     | Stored as row? |
|------------------------|--------------------------------------|----------------|
| `motion.detected`      | 0x21 motion = 1                      | Yes |
| `motion.cleared`       | 0x21 motion = 0                      | Yes |
| `window.opened`        | 0x2D window = 1                      | Yes |
| `window.closed`        | 0x2D window = 0                      | Yes |
| `button.push`          | 0x3A button events                   | Yes |
| `battery.low`          | Any BTHome battery < 20 %            | Yes — severity `warn` |
| temperature reading    | 0x02 tC                              | **No** — tracker only |
| humidity reading       | 0x03 rh                              | **No** — tracker only |

### 2.5 Synthetic events (myhome-generated)

| Event                       | Trigger                                                          |
|-----------------------------|------------------------------------------------------------------|
| `device.discovered`         | First time a device ID is seen and written to DB                 |
| `temperature.daily_min/max` | Midnight: flush `SensorDailyTracker` → emit one event per device per metric |
| `illuminance.daily_min/max` | Same midnight flush                                              |
| `humidity.daily_min/max`    | Same midnight flush                                              |
| `temperature.threshold_breach` | Room setpoint missed for > N minutes (temperature service hook) |

---

## 3. Severity Levels

| Level    | Events                                                              |
|----------|---------------------------------------------------------------------|
| `alarm`  | `smoke.alarm`, `temperature.threshold_breach`                       |
| `warn`   | `battery.low`, `ota_error`, `switch.active_power_change` (future threshold) |
| `info`   | `switch.on/off`, `light.on/off`, `motion.*`, `window.*`, `device.online/offline`, `illuminance.change`, `device.discovered`, `*.daily_min/max` |
| `debug`  | `input.button_*`, `temperature.change`, `humidity.change`, `ota_begin/progress` |

---

## 4. Architecture

```
MQTT broker
   |
   +-- "+/events/rpc"          ──► gen2/listener.go  (new)        ──┐
   |                                                                  │
   +-- "+/online"              ──► gen2/listener.go  (same)       ──┤
   |                                                                  │
   +-- "shellies/#"            ──► gen1/listener.go  (extended)   ──┤
   |                                                                  ▼
   +-- "shelly-blu/events/+"   ──► blu/listener.go   (extended)   EventService
                                                                      │
                                    ┌─────────────────────────────────┤
                                    │                                 │
                           SensorDailyTracker              events/storage.go
                           (shared, restart-safe)          │
                           - illuminance lux                events.db (SQLite)
                           - temperature tC                 ├── events table
                           - humidity rh                    └── sensor_daily_stats
                           - em energy kWh                      table
                                    │
                           midnight flush
                           → synthetic daily_min/max events
                                    │
                               EventService.Record()
                                         │
                                SSEBroadcaster.BroadcastEvent()
                                         │
                    ┌────────────────────┴─────────────────┐
                    │                                       │
             Web UI /htmx/events                  myhome ctl events
             (HTMX table + SSE live push)          list / follow / clear
```

### New packages / files

| Path                               | Role |
|------------------------------------|------|
| `myhome/events/`                   | New sub-module: `storage.go`, `service.go`, `tracker.go` |
| `internal/myhome/shelly/gen2/`     | New package: Gen2 `NotifyEvent` + `online` MQTT listener |
| `myhome/ctl/events/`               | CLI: `list`, `follow`, `clear` commands |

Extend:
- `internal/myhome/shelly/gen1/listener.go` — feed event log + tracker
- `internal/myhome/shelly/blu/listener.go` — feed event log + tracker
- `internal/myhome/ui/server.go` — `/htmx/events` route
- `internal/myhome/const.go` — `EventList`, `EventTail` verbs
- `myhome/ctl/options/options.go` — `EventsDBPath`, `EventsRetention` flags
- `myhome/daemon/run.go` — wire `EventService`

---

## 5. Database

### 5.1 Separate file

`events.db` lives in the same directory as `devices.db` (e.g. `~/.myhome/events.db`).

Rationale: independent backup, rotation, and deletion without touching device config. Configurable via `--events-db` flag and `MYHOME_EVENTS_DB` env var.

`myhome/events/storage.go` opens its own `*sqlx.DB` connection (same `ncruces/go-sqlite3` driver).

### 5.2 `events` table

```sql
CREATE TABLE IF NOT EXISTS events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          REAL    NOT NULL,                   -- device-reported Unix timestamp (float seconds)
    received_at REAL    NOT NULL,                   -- wall-clock when myhome received/generated it
    device_id   TEXT    NOT NULL,                   -- MQTT src / BLU MAC / "myhome"
    component   TEXT    NOT NULL,                   -- "switch:0", "sys", "mqtt", "blu:AA:BB:…"
    event       TEXT    NOT NULL,                   -- "switch.on", "ota_success", "device.online", …
    severity    TEXT    NOT NULL DEFAULT 'info',    -- alarm | warn | info | debug
    data        TEXT,                               -- JSON blob (nullable)
    UNIQUE (device_id, component, event, ts)        -- idempotent INSERT OR IGNORE
);

CREATE INDEX IF NOT EXISTS events_ts       ON events (ts DESC);
CREATE INDEX IF NOT EXISTS events_device   ON events (device_id, ts DESC);
CREATE INDEX IF NOT EXISTS events_event    ON events (event, ts DESC);
CREATE INDEX IF NOT EXISTS events_severity ON events (severity, ts DESC);
```

**Deduplication**: `INSERT OR IGNORE` on `UNIQUE(device_id, component, event, ts)` absorbs MQTT QoS-1 redelivery and any double-listener paths with no application logic.

**Why `ts REAL`**: Shelly timestamps are Unix floats (`1631266595.44`). Sub-second precision matters for burst button events. SQLite IEEE 754 double gives sub-millisecond precision across centuries.

### 5.3 `sensor_daily_stats` table

Shared across illuminance lux, temperature tC, humidity rh, and future energy kWh. Written on every sample update (not just at midnight) so a daemon restart never loses the current day's running min/max.

```sql
CREATE TABLE IF NOT EXISTS sensor_daily_stats (
    date        TEXT    NOT NULL,   -- ISO-8601 "YYYY-MM-DD" (local time)
    device_id   TEXT    NOT NULL,
    component   TEXT    NOT NULL,   -- "illuminance:0", "temperature:0", "humidity:0", "em:0"
    metric      TEXT    NOT NULL,   -- "lux", "tC", "rh", "kWh"
    min_val     REAL,
    max_val     REAL,
    sum_val     REAL    DEFAULT 0,  -- for computing avg
    samples     INTEGER DEFAULT 0,
    updated_at  REAL    NOT NULL,   -- Unix timestamp of last upsert
    PRIMARY KEY (date, device_id, component, metric)
);
```

**Restart recovery**: on startup, `SensorDailyTracker` reads today's rows from `sensor_daily_stats` and restores in-memory min/max/sum/count. No sample is ever lost.

**Midnight rollover**: at local midnight (using a timer that fires at next 00:00), the tracker:
1. Writes synthetic event rows (`temperature.daily_min`, `temperature.daily_max`, etc.) to the `events` table for yesterday.
2. Resets in-memory buckets for today (DB rows for yesterday remain, new rows created as today's samples arrive).

Migration follows the `COUNT(*) FROM pragma_table_info` pattern used in `myhome/storage/db.go`.

---

## 6. `SensorDailyTracker` — Shared Compute Infrastructure

Lives in `myhome/events/tracker.go`. Used by:
- Gen2 listener: illuminance lux, temperature tC, humidity rh
- Gen1 listener: temperature tC, humidity rh (no event rows; only tracker)
- BLU listener: temperature tC, humidity rh
- Gen2 listener (future): em energy kWh

```go
// Metric identifies a single measurable quantity on a component.
type Metric struct {
    DeviceID  string
    Component string  // "temperature:0", "illuminance:0", …
    Metric    string  // "tC", "lux", "rh", "kWh"
}

// DayBucket holds running statistics for one calendar day.
type DayBucket struct {
    Date    string   // "YYYY-MM-DD"
    Min     float64
    Max     float64
    Sum     float64
    Samples int64
}

type SensorDailyTracker struct { ... }

func NewSensorDailyTracker(log logr.Logger, store *Storage) *SensorDailyTracker

// Start loads today's buckets from DB and arms the midnight rollover timer.
func (t *SensorDailyTracker) Start(ctx context.Context) error

// Observe records a new sample; upserts the DB row immediately.
func (t *SensorDailyTracker) Observe(ctx context.Context, m Metric, value float64) error

// Flush writes synthetic daily_min / daily_max event rows for the given date.
// Called at midnight for yesterday, and at shutdown for today (partial day).
func (t *SensorDailyTracker) Flush(ctx context.Context, date string, emit func(Event) error) error
```

All sensor sources call `tracker.Observe(ctx, metric, value)`. The tracker handles DB upserts, in-memory rolling stats, and midnight event emission. No source needs to know about daily aggregation logic.

---

## 7. Go API

### 7.1 `myhome/events/storage.go`

```go
type Event struct {
    ID         int64   `db:"id"`
    Ts         float64 `db:"ts"`
    ReceivedAt float64 `db:"received_at"`
    DeviceID   string  `db:"device_id"`
    Component  string  `db:"component"`
    Event      string  `db:"event"`
    Severity   string  `db:"severity"`
    Data       *string `db:"data"`
}

type Query struct {
    DeviceID  string
    EventType string        // prefix match: "switch" matches switch.on + switch.off
    Severity  string
    Since     time.Duration // 0 = no limit
    Limit     int           // 0 = default 500
    Offset    int           // pagination
}

func NewStorage(log logr.Logger, dbPath string) (*Storage, error)
func (s *Storage) Record(ctx context.Context, e Event) error
func (s *Storage) Query(ctx context.Context, q Query) ([]Event, error)
func (s *Storage) Purge(ctx context.Context, before time.Time) (int64, error)
func (s *Storage) DB() *sqlx.DB
func (s *Storage) Close()
```

### 7.2 `myhome/events/service.go`

```go
type Service struct { ... }

func NewService(log logr.Logger, store *Storage, mqtt mqtt.Client,
    tracker *SensorDailyTracker, broadcast func(Event), retention time.Duration) *Service

// Start wires MQTT subscriptions and starts the purge ticker. Blocks until ctx is done.
func (s *Service) Start(ctx context.Context) error

// Record is the single write path; called by all MQTT bridges and midnight flush.
func (s *Service) Record(ctx context.Context, e Event) error
```

`Start` subscribes to:
1. `+/events/rpc` — unmarshal `NotifyEvent`, iterate `params.events`, call `Record()`
2. `+/online` — emit `device.online` / `device.offline`

Purge ticker: runs hourly, `DELETE FROM events WHERE ts < retention_cutoff`.

### 7.3 RPC verbs

Add to `internal/myhome/const.go`:

```go
EventList Verb = "event.list"
EventTail Verb = "event.tail"   // future streaming verb
```

`event.list` request: `{ device_id?, event?, severity?, since?, limit?, offset? }`
`event.list` response: `{ events: [...], total: int }`

---

## 8. MQTT Ingestion Details

### Gen2 `NotifyEvent`

Subscribe to `+/events/rpc`. Filter `method == "NotifyEvent"`. For each entry in `params.events[]`:

- `device_id` = message `src` field
- `component` = `event.component`
- `event` = `event.event`
- `ts` = per-event `ts` (falls back to `params.ts`)
- `data` = JSON of remaining fields (e.g. `{"apower":1234.5}` for power, `{"illumination":"dark","lux":3.2}` for illuminance)

`NotifyStatus` messages on the same topic are silently ignored (handled by existing status path).

### Gen2 connectivity

Subscribe to `+/online`. Topic prefix before `/online` = device ID. Emit `device.online`/`device.offline` with `received_at = time.Now()`, `ts = received_at` (LWT has no device timestamp).

### Gen1 bridge

In `gen1/listener.go`, after existing sensor cache update:
- `relay/<n>` → `Record()` with `event = "switch.on"` or `"switch.off"`
- `online` → `Record()` with `event = "device.online"` or `"device.offline"`
- `sensor/temperature` → `tracker.Observe(metric{tC}, value)` only — no event row
- `sensor/humidity` → `tracker.Observe(metric{rh}, value)` only — no event row

Pass `eventService *events.Service` into the Gen1 listener constructor. If nil (tests), skip.

### BLU bridge

In `blu/listener.go`, after BTHome object decode:
- BTHome 0x21 motion → `Record()` with `motion.detected` / `motion.cleared`
- BTHome 0x2D window → `Record()` with `window.opened` / `window.closed`
- BTHome 0x3A button → `Record()` with `button.push`
- Battery < 20 % → `Record()` with `battery.low`, severity `warn`
- BTHome 0x02 tC → `tracker.Observe()` only
- BTHome 0x03 rh → `tracker.Observe()` only

`device_id` = BLU sensor MAC from BTHome payload addr field (not gateway).

---

## 9. Retention & Truncation

| Config | Value |
|--------|-------|
| Config key | `events.retention` |
| CLI flag | `--events-retention` on `myhome daemon run` |
| Env var | `MYHOME_EVENTS_RETENTION` |
| Default | `2160h` (90 days) |

Purge: hourly background `DELETE FROM events WHERE ts < ?`. Only the `events` table is purged; `sensor_daily_stats` rows are small (one row per device/metric/day) and kept indefinitely (or configurable separately in future).

Manual: `myhome ctl events clear [--before <RFC3339 | duration>] [--dry-run]`

---

## 10. CLI

```
myhome ctl events list
    [--device <id|name|mac>]   filter by device
    [--type <event-prefix>]    e.g. "switch" matches switch.on + switch.off
    [--severity <level>]       alarm|warn|info|debug
    [--since <duration>]       e.g. 24h, 7d (default: 24h)
    [--limit <n>]              max rows (default: 100)
    [--json]                   machine-readable output

myhome ctl events follow
    [--device <id>]
    [--type <prefix>]
    [--severity <level>]       default: info+warn+alarm
    Tails live events via SSE GET /events stream (parses "event: eventlog" lines)

myhome ctl events clear
    [--before <RFC3339 | duration>]   default: retention threshold
    [--dry-run]
```

Output columns for `list`: `TIME` (human-relative), `DEVICE`, `COMPONENT`, `EVENT`, `SEVERITY`, `DATA`.

---

## 11. Web UI

### Events page

Route: `GET /htmx/events` — HTMX fragment rendering a table of recent events.

- Newest-first, paginated (`hx-get` with `?offset=N` for load-more)
- Filter bar: device dropdown, event-type text input, severity radio, date-range
- Color-coded rows by severity: red=alarm, orange=warn, default=info, muted=debug
- Relative timestamps updated client-side via Alpine.js (`x-text` on interval)
- Live push: `SSEBroadcaster.BroadcastEvent(e Event)` emits `event: eventlog\ndata: {...}\n\n` on `/events` SSE stream; client-side JS prepends row via `hx-swap-oob`

### SSE event type

New SSE event name `eventlog` alongside the existing `sensor` events. The `data` field is a JSON-serialized `Event` struct.

### Nav

Add "Events" tab to the top navigation bar.

### Daily stats sparkline (stretch)

Per-device card shows a 7-day mini-chart of temperature and/or lux from `sensor_daily_stats`. Simple SVG path; no JS charting library needed.

---

## 12. Configuration

Add to `myhome/ctl/options/options.go`:

```go
EventsDBPath        string         // default "~/.myhome/events.db"
EventsRetention     time.Duration  // default 2160h (90 days)
EnableEventsService bool           // default true when device manager is on
```

Add to `docs/configuration.md` and `myhome-example.yaml`:

```yaml
events:
  db: ~/.myhome/events.db   # path to the events SQLite database
  retention: 2160h          # auto-purge threshold (90 days)
  enabled: true             # set false to disable event recording entirely
```

Env vars: `MYHOME_EVENTS_DB`, `MYHOME_EVENTS_RETENTION`, `MYHOME_EVENTS_ENABLED`.

---

## 13. Implementation Phases

Mark each phase **[done]** before starting the next. Commit plan updates alongside code.

### Phase 1 — DB schema & storage layer `myhome/events/`
- [ ] `storage.go`: open `events.db`, create `events` + `sensor_daily_stats` tables
- [ ] `Record()`, `Query()`, `Purge()` implementations
- [ ] Unit tests with `:memory:` SQLite

### Phase 2 — `SensorDailyTracker`
- [ ] `tracker.go`: `Observe()`, in-memory DayBucket map, DB upsert on every sample
- [ ] `Start()`: load today's rows from DB on startup (restart recovery)
- [ ] Midnight rollover: compute next 00:00 wall-clock, fire once, re-arm
- [ ] `Flush()`: emit synthetic `*.daily_min` / `*.daily_max` event rows
- [ ] Unit tests: verify restart recovery, midnight rollover, multi-metric isolation

### Phase 3 — Gen2 `NotifyEvent` listener
- [ ] `internal/myhome/shelly/gen2/listener.go`
- [ ] Subscribe `+/events/rpc`; parse `NotifyEvent`; call `service.Record()`
- [ ] Subscribe `+/online`; emit connectivity events
- [ ] Feed `illuminance`, `temperature`, `humidity` change events to tracker
- [ ] Wire into `myhome/daemon/daemon.go`

### Phase 4 — Gen1 & BLU bridges
- [ ] `gen1/listener.go`: add `*events.Service` constructor arg; bridge relay, online, temperature→tracker, humidity→tracker
- [ ] `blu/listener.go`: same pattern; bridge motion, window, button, battery-low, temperature→tracker, humidity→tracker
- [ ] Use BLU MAC as `device_id`

### Phase 5 — `EventService` wiring & retention
- [ ] `service.go`: compose storage + tracker + MQTT subs + purge ticker
- [ ] Wire `EventsRetention` + `EventsDBPath` config options in `run.go`
- [ ] Graceful shutdown: call `tracker.Flush()` for today before exit

### Phase 6 — RPC API
- [ ] `EventList` verb + request/response types in `internal/myhome/`
- [ ] Register handler

### Phase 7 — CLI
- [ ] `myhome/ctl/events/`: `list`, `follow`, `clear` sub-commands
- [ ] Register under `myhome ctl`

### Phase 8 — Web UI
- [ ] `/htmx/events` HTMX route + events table template
- [ ] `SSEBroadcaster.BroadcastEvent()` + `eventlog` SSE event type
- [ ] Events tab in navigation

### Phase 9 — Docs & config
- [ ] `docs/configuration.md`, `myhome-example.yaml`, `options.go`, `run.go`

---

## 14. Remaining open questions

None at this time. Implementation can start at Phase 1.
