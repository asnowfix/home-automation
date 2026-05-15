# PLAN: Room-Centric Heating Refactor

Status: **PLANNING** — implement phases in order; mark each `DONE` before starting the next.

## Problem statement

The `myhome/temperature/` package is misnamed (it stores rooms, not temperatures), caches all its data
in memory redundantly with SQLite, has no UI of its own, and only loosely connects heaters to their
sensors. The heater script fetches heavy data (weather) itself and hard-codes sensor topics as manual
strings, making setup fragile and device autonomy fragile.

## Goal

Make the **room** the first-class unit. Each room owns temperature sensors, door/window sensors, and
one or more heaters. The daemon does daily setup and proxies heavy external data via MQTT. Device
scripts run an autonomous control loop that survives daemon and internet loss for at least one day.

---

## Six gate conditions

A heater turns ON only when **all** of the following pass simultaneously:

| # | Condition | Source on device |
|---|---|---|
| 0 | Internal temperature ≤ 7 °C (frost override — bypasses all other gates) | local MQTT sensor |
| 1 | Electricity is cheap now | KVS (written by data-relay) |
| 2 | Weather forecast predicts a temperature drop worth pre-heating | KVS (written by data-relay) |
| 3 | Home is globally occupied (someone seen in past 12 h) | KVS (written by data-relay) |
| 4 | Room agenda says this room should be occupied right now | KVS (written by data-relay) |
| 5 | All room doors/windows are closed | local MQTT sensors |

Gate 0 (frost) is a hard override: it turns the heater ON regardless of gates 1–5.
Gates 1–5 must all pass for the heater to run in normal comfort mode.

---

## MQTT topic map

| Topic | Publisher | Subscriber | Payload |
|---|---|---|---|
| `myhome/electricity/status` | daemon | data-relay | `{"cheap":true,"until_epoch":1234567890}` |
| `myhome/weather/forecast` | daemon | data-relay | `[{"h":6,"t":4.2},{"h":10,"t":5.1},{"h":14,"t":3.8},{"h":18,"t":2.1}]` |
| `myhome/rooms/<id>/agenda` | daemon | data-relay | `[{"s":480,"e":1020},{"s":1200,"e":1380}]` (minutes since midnight) |
| `myhome/rooms/<id>/schedule` | daemon | heater-controller | comfort time ranges (existing format) |
| `myhome/occupancy` | occupancy svc | data-relay | existing format |
| `shelly-blu/events/<mac>` | blu-publisher | heater-controller | BTHome v2 (temperature + window fields) |
| `shellies/shellyht-<id>/sensor/temperature` | device | heater-controller | float string |

All daemon-published topics use retained messages so scripts recover state after reboot without
waiting for the next publish cycle.

### KVS keys written by data-relay (read by heater-controller)

| Key | Content |
|---|---|
| `room/electricity` | `{"cheap":true,"until_epoch":N,"ts":N}` |
| `room/weather` | `[{"h":H,"t":T},...]` (4 slots) |
| `room/agenda` | `[{"s":S,"e":E},...]` |
| `room/occupancy` | `{"occupied":true,"ts":N}` |

---

## DB schema changes

### Renamed tables

| Old name | New name |
|---|---|
| `temperature_rooms` | `rooms` |
| `temperature_kind_schedules` | `room_kind_schedules` |
| `temperature_weekday_defaults` | `room_weekday_defaults` |

### `rooms` table (was `temperature_rooms`)

```sql
ALTER TABLE temperature_rooms RENAME TO rooms;
ALTER TABLE rooms ADD COLUMN ical_url TEXT DEFAULT '';
```

New columns vs current:
- `ical_url TEXT` — public iCal URL for room occupancy agenda (empty = no calendar)

### New: `weather_cache` table

```sql
CREATE TABLE IF NOT EXISTS weather_cache (
  fetched_at  INTEGER NOT NULL,  -- unix epoch
  forecast    TEXT NOT NULL,     -- JSON: [{h,t},...]
  stale       INTEGER NOT NULL DEFAULT 0  -- 1 if internet was unavailable
);
```

Only the most recent row is used. Previous rows are deleted on each successful fetch.

### New: `room_agenda_cache` table

```sql
CREATE TABLE IF NOT EXISTS room_agenda_cache (
  room_id    TEXT NOT NULL PRIMARY KEY,
  date       TEXT NOT NULL,       -- YYYY-MM-DD (local)
  slots      TEXT NOT NULL,       -- JSON: [{s,e},...]
  fetched_at INTEGER NOT NULL
);
```

---

## Room-type default comfort schedules

Defaults applied when a room has no overriding custom schedule. All times are local.

| Kind | Work-day | Day-off |
|---|---|---|
| `bedroom` | 22:00–07:00 | 23:00–09:00 |
| `living-room` | 17:00–22:30 | 10:00–23:00 |
| `office` | 08:00–18:30 | — |
| `kitchen` | 06:30–09:00, 18:00–21:00 | 09:00–11:00, 18:00–21:00 |
| `other` | — | — |

`—` means eco mode always; frost override (gate 0) still active.
All values are configurable per room; these are just the seeded defaults.

---

## Sensor MQTT topic derivation

The daemon derives sensor topics automatically from the `devices` table — no manual strings needed.

| Device ID prefix | Role detected by | MQTT topic |
|---|---|---|
| `shellyblu-<mac>` | `window` in BTHome capabilities | `shelly-blu/events/<mac:with:colons>` |
| `shellyblu-<mac>` | `temperature` in BTHome capabilities | `shelly-blu/events/<mac:with:colons>` |
| `shellyht-<id>` | Gen1 H&T model prefix | `shellies/shellyht-<id>/sensor/temperature` |

Device role classification within a room:
- **Heater**: device has heater script installed (detected via script list)
- **Temperature sensor**: `temperature` in BTHome capabilities, or Gen1 H&T device
- **Door/window sensor**: `window` in BTHome capabilities

---

## Package & file layout (after refactor)

```
myhome/
  rooms/                   ← replaces myhome/temperature/
    service.go             ← RPC handlers; no in-memory cache; all reads hit SQLite
    storage.go             ← SQLite layer
    config.go              ← YAML config binding
    setup.go               ← daily device setup job (pushes sensor topics to KVS)

  electricity/             ← new package
    electricity.go         ← ElectricityPricer interface + FixedWindowPricer impl
    publisher.go           ← MQTT publish loop for myhome/electricity/status

  weather/                 ← new package
    weather.go             ← Open-Meteo fetcher, 4-slot distiller
    storage.go             ← weather_cache SQLite table
    publisher.go           ← MQTT publish loop for myhome/weather/forecast

  ical/                    ← new package
    ical.go                ← iCal URL fetcher, VEVENT parser, busy-slot distiller
    storage.go             ← room_agenda_cache SQLite table
    publisher.go           ← MQTT publish loop for myhome/rooms/<id>/agenda

internal/shelly/scripts/
  data-relay.js            ← new: daemon MQTT → KVS bridge
  heater-controller.js     ← refactored from heater.js (no direct Open-Meteo)

internal/myhome/ui/
  rooms.go                 ← new: Rooms tab HTMX handlers
  static/index.html        ← add Rooms tab
```

---

## Phase 0 — Rename & DB refactor `[DONE]`

Goal: rename the Go package and DB tables; remove in-memory caches; no behaviour change yet.

Tasks:
- `git mv myhome/temperature/ myhome/rooms/`
- Update all import paths referencing the old package
- Rename DB tables via migrations (SQLite `ALTER TABLE RENAME`)
- Add `ical_url` column to `rooms`
- Add `weather_cache` and `room_agenda_cache` tables
- Remove `Service.rooms`, `Service.kindSchedules`, `Service.weekdayDefaults` maps
- All `Get*` methods read SQLite directly; `Set*` methods write and return
- Update RPC method names to `room.*` namespace (audit: some may already be correct)
- Update `internal/myhome/temperature.go` types to match new table names
- `make test` must pass

## Phase 1 — Electricity pricing interface `[DONE]`

Goal: daemon publishes cheap/expensive signal; scripted via a clean interface for future swap.

Tasks:
- New `myhome/electricity/` package
- `ElectricityPricer` interface: `IsCheapNow(ctx context.Context, horizonHours int) bool`
- `FixedWindowPricer` struct: config `cheap_start` / `cheap_end` (strings, e.g. `"23:15"`)
- YAML config keys: `electricity.cheap_start`, `electricity.cheap_end`; update `options.go`, `run.go`,
  `docs/configuration.md`, `myhome-example.yaml`
- Publish loop: every 15 min, publish retained to `myhome/electricity/status`
  payload: `{"cheap":<bool>,"until_epoch":<unix>}`
- `make test` must pass

## Phase 2 — Weather proxy `[DONE]`

Goal: daemon fetches Open-Meteo, distils to 4 hourly slots, publishes retained; persists for offline.

Tasks:
- New `myhome/weather/` package
- Fetch Open-Meteo hourly forecast for the configured location (lat/lon from existing heater config)
- Distil: select the next 4 hourly readings from now, format `[{"h":<hour>,"t":<celsius>},...]`
- Publish retained to `myhome/weather/forecast` 4× per day (e.g. 06:00, 12:00, 18:00, 00:00)
- Persist in `weather_cache` table after each successful fetch
- On internet loss: re-serve last cached forecast; if cache is >24 h old, repeat it with `"stale":true`
- `make test` must pass (mock HTTP for Open-Meteo)

## Phase 3 — iCal integration `[DONE]`

Goal: daemon fetches per-room public iCal URL, distils today's busy slots, publishes retained.

Tasks:
- New `myhome/ical/` package
- Fetch iCal URL (HTTP GET, no auth) — standard `net/http`
- Parse VEVENT: extract `DTSTART` / `DTEND`, convert to local time
- Distil today's busy slots: `[{"s":<min-since-midnight>,"e":<min-since-midnight>},...]`
- Publish retained to `myhome/rooms/<room-id>/agenda` (skip rooms with empty `ical_url`)
- Persist in `room_agenda_cache` for offline use
- Refresh daily at midnight + on room config change
- New RPC methods: `room.seticurl` / `room.geticurl`
- UI: iCal URL field in room edit form
- `make test` must pass (use fixture `.ics` file)

## Phase 4 — Automatic heater device setup `[ ]`

Goal: daemon computes and pushes sensor topics to each heater KVS; no more manual strings.

Tasks:
- New `setup.go` in `myhome/rooms/`
- `SetupRoom(ctx, roomID)` function:
  1. Load all devices in the room from DB
  2. Classify: heaters (have heater script), temp sensors, door sensors
  3. For each heater device: derive MQTT topics for each sensor (see derivation table above)
  4. Push to device KVS: `room-id`, `internalTemperatureTopic`, `doorSensorTopics`,
     comfort schedule, setpoints, electricity config
- Daemon runs setup at startup and daily at 01:00
- CLI: `myhome ctl room setup <room-id>` triggers `SetupRoom` immediately
- `make test` must pass

## Phase 5 — JS script split `[ ]`

Goal: split heater.js into data-relay.js + heater-controller.js to stay within Shelly resource limits.

### data-relay.js (new)

Resource budget: 4 MQTT subscriptions, 1 timer.

Config KVS key: `script/data-relay/room-id` (room-id string)

Behaviour:
- Subscribe to 4 topics: `myhome/electricity/status`, `myhome/weather/forecast`,
  `myhome/rooms/<id>/agenda`, `myhome/occupancy`
- On each message: parse JSON, add `ts` field (device epoch), write to KVS
- Timer (60 min): log warning if any KVS value is missing or older than 25 h

### heater-controller.js (refactored from heater.js)

Resource budget: 3 MQTT subscriptions, 2 timers.

Config KVS keys (pushed by daemon setup): `room-id`, `internalTemperatureTopic`,
`doorSensorTopics` (comma-separated), `comfortSchedule` (JSON), `setpoints` (JSON),
`frostThresholdC` (default 7), `cheapHorizonH` (default 2).

Behaviour:
- Subscribe to: temperature sensor topic, door sensor topic(s) (max 2 separate topics)
- Timer 1 (control loop, default 5 min): run gate evaluation, set switch
- Timer 2 (frost check, 1 min): if temp ≤ `frostThresholdC`, turn heater ON immediately
- Gate evaluation (in order):
  1. Read `room/occupancy` from KVS → gate 3
  2. Read `room/electricity` from KVS → gate 1
  3. Read `room/weather` from KVS → gate 2 (any slot temp < current temp → worth heating)
  4. Read `room/agenda` from KVS → gate 4 (is current minute in any busy slot?)
  5. Check local door state → gate 5
  6. All gates pass → Switch.Set ON; any fail → Switch.Set OFF
- Retain Kalman filter for temperature smoothing
- Remove direct Open-Meteo HTTP fetch
- Log which gate blocked on each OFF decision

### Subscription count summary

| Script | MQTT subs used | Timer used |
|---|---|---|
| blu-publisher | 0 (BLE) + 1 event handler | 0 |
| data-relay | 4 | 1 |
| heater-controller | 2–3 | 2 |
| Total per heater device | ≤ 8 of 10 MQTT | ≤ 3 of 5 timers |

## Phase 6 — Rooms UI tab `[ ]`

Goal: dedicated Rooms tab with per-room view of devices, sensor readings, and gate status.

Tasks:
- New `myhome/rooms/ui.go` with HTMX partial handlers
- Rooms tab in `static/index.html` (alongside existing Devices tab)
- Per-room card:
  - Room name, kind badges
  - Current temperature (last seen from any temp sensor in room)
  - Agenda: busy/free now + next slot time
  - Electricity: cheap/expensive + until when
  - Occupancy: occupied/vacant + last seen
  - Door/window sensors: open/closed per sensor
  - Heater(s): on/off + blocking gate name (from last MQTT log or KVS debug key)
- Room management panel: create, edit (name, kinds, setpoints, iCal URL), delete, assign devices
- Device-to-room assignment: dropdown on device card (existing) + bulk assign in room view
- `make test` must pass; manually verify golden path in browser

---

## Non-goals

- Per-room motion sensors (global occupancy only)
- Multi-daemon coordination
- Real-time spot electricity pricing (interface is ready; implementation deferred)
- Solar panel integration (interface placeholder only)

## Migration notes

- Existing `temperature_rooms` data migrates automatically via `ALTER TABLE RENAME`
- Existing `devices.room_id` values are preserved unchanged
- Heater KVS config keys will change in Phase 5; old keys remain until daemon runs setup
- `heater.js` remains deployed until Phase 5 is complete and tested
