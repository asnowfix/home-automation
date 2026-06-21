# Garden Sprinkler Controller (`garden.js`) — Implementation Plan

> **Status:** Phases 0–6 complete. Device `arrosage` verified in production (2026-06-14/15).
> All verification steps passed; two runtime bugs found and fixed during live testing.
> PR #265 open for review.

---

## 1. Context & goal

The garden has a Shelly **Pro3** physically named **`arrosage`** (`shellypro3-34987a48c26c`,
`192.168.1.83`) that drives three independent watering zones. Its power supply can energize **only one
valve at a time**, so zones must run **sequentially**.

The device keeps its French name `arrosage`. **All new code uses the English basename `garden`**
(script `garden.js`, CLI `ctl garden`, KVS prefix `script/garden/`, Go file `garden.go`).

Zone → switch mapping (fixed, defaults for the `zoneN-name` config keys):

| Switch | Zone (FR) | Default name | Type |
|---|---|---|---|
| `switch:0` | pelouse côté maison | `pelouse-maison` | lawn (2 pop-up heads) |
| `switch:1` | massifs | `massifs` | flower beds (drip pipe) |
| `switch:2` | pelouse côté barrière | `pelouse-barriere` | lawn (2 pop-up heads) |

**Goal:** water the minimum amount that keeps the garden healthy, by combining **recent actual rainfall**,
**reference evapotranspiration (ET₀)**, and the **weather forecast**; and run the zones during the
**calmest, coolest window of the morning** (least wind dominates, then lowest temperature → least
evaporation), **never** during the household's outdoor-presence windows:

- **12:00–14:00** (lunch) — every day
- **19:00–23:30** (dinner/evening outside) — every day

There is **no pre-existing script** to refactor (verified: not on the device, not in the repo, not on any
branch). Build from scratch.

### Reference implementation — read this first

The existing **`internal/shelly/scripts/pool-pump.js`** is a Pro3 (3-switch) controller that already
implements almost every building block this feature needs. **Read it in full before writing `garden.js`**
and reuse its patterns. Line citations below refer to that file as it currently stands.

### Hard constraints (from `CLAUDE.md` / `AGENTS.md`)

- **Resilience — internet-optional:** all logic runs **on the device**; no `myhome` daemon dependency.
  Every external HTTP call needs a timeout **and** a fallback. If Open-Meteo is unreachable, the planner
  must fall back to a fixed morning schedule with default durations and never crash or block.
- **Resilience — daemon-optional per device:** the device must work standalone. (This design has no
  cross-device dependency, so it satisfies this by construction.)
- **Shelly JS (Espruino ES5) rules** — violating these crashes the device:
  - Use `var` (never `let`/`const`).
  - **No hoisting:** define every function before it is referenced, including callbacks.
  - Max 2–3 levels of nested anonymous functions — extract named top-level functions.
  - **Never an empty `catch`:** write `catch (e) { if (e && false) {} }` (minifier turns `catch {}` into
    a syntax error).
  - Property checks via `"prop" in obj`, not `obj.prop !== undefined`.
  - No `Array.shift()`/`unshift()`, no `Array.prototype.slice.call(arguments)` — use manual `for` loops.
  - Per-script limits: **5 timers**, 5 event subs, 5 status subs, 5 concurrent RPCs, 10 MQTT subs →
    use the **single recurring timer + task queue** pattern (pool-pump.js:371-403).
  - **KVS keys must be < 42 chars** (target ≤ 32). Prefix `script/garden/` is 14 chars, so keep
    suffixes ≤ 18 chars. Keys: lowercase, hyphens/slashes only.
  - Storage: `Script.storage` for script-internal evolving values (synchronous read, survives reboot);
    `KVS` for external config; in-memory vars for cache.
- **Config option rule:** any genuinely new daemon config option requires touching `options.go`,
  `run.go`, `docs/configuration.md`, `myhome-example.yaml`. **This feature stores config in device KVS,
  not daemon config**, so that rule does **not** apply here.
- **File moves:** use `git mv` (n/a here — all files are new).
- **Tests:** `make test` is canonical (covers all `go.work` sub-modules). Never bare `go test ./...`.

---

## 2. Weather data — Open-Meteo (no API key)

Single endpoint `https://api.open-meteo.com/v1/forecast`. Extend pool-pump's `setForecastURL`
(pool-pump.js:543-551) to build:

```
https://api.open-meteo.com/v1/forecast?latitude=<lat>&longitude=<lon>
  &hourly=temperature_2m,wind_speed_10m,precipitation
  &daily=sunrise,sunset,precipitation_sum,et0_fao_evapotranspiration
  &past_days=3&forecast_days=2&timezone=auto
```

- `past_days=3` → `daily.precipitation_sum[0..2]` and `daily.et0_fao_evapotranspiration[0..2]` are the
  **recent actuals** (today is index `past_days`).
- `daily.*[past_days]` = today; `[past_days+1]` = tomorrow.
- `hourly.*` arrays are indexed by hour; today's hour `h` is at index `past_days*24 + h`.
- Location: reuse `Shelly.DetectLocation` + `ensureForecastUrl` (pool-pump.js:648-680). Cache the URL in
  `Script.storage` (key `forecast-url`), refresh forecast at most once/day (pool-pump.js:553-561).
- HTTP call: `Shelly.call("HTTP.GET", {url: url, timeout: 10}, onForecast, cb)` (pool-pump.js:638-641).

`onForecast` (model on pool-pump.js:563-627) must parse and cache into `STATE`:
`et0Today`, `rainTodayForecast`, plus the recent actuals arrays, the hourly `temp`/`wind`/`precip`
slices for today, and `sunriseHour`/`sunsetHour` (via `parseHourFromISO`, pool-pump.js:1558-1569).
Free the big arrays after extracting what's needed (memory hygiene, as pool-pump does).

---

## 3. Core algorithm

### 3.1 Per-zone soil-water deficit (ET₀ balance)

Persist a per-zone deficit `D[z]` in **`Script.storage`** key `deficit/<z>` (mm), using
`storeStorageValue`/`loadStorageValue` (pool-pump.js:471-505).

Once per day, in `handlePlan()`:

```
for each zone z (enabled):
    # apply the most recent completed day's actuals (correct forecast drift)
    D[z] += et0_lastDay * Kc[z]        # plant water loss
    D[z] -= rain_lastDay               # recent rain credit  (cumulative recent rain enters here)
    clamp D[z] to [0, maxDeficitMm]
```

`et0_lastDay` = `daily.et0_fao_evapotranspiration[past_days-1]`,
`rain_lastDay` = `daily.precipitation_sum[past_days-1]`.

> The "cumulative precipitation over the past few days" requirement is satisfied because each day's rain
> reduces the deficit; several wet days drive `D[z]` to 0 and watering is skipped. `past_days=3` gives a
> few days of correction headroom on first run after downtime.

### 3.2 Decide today's per-zone run minutes

```
for each enabled zone z:
    if D[z] >= triggerDeficitMm[z]:        # deep, infrequent watering
        depthMm   = min(D[z], maxDeficitMm)
        runMin[z] = min(depthMm / appRateMmPerH[z] * 60, maxRunMinutes[z])
    else:
        runMin[z] = 0                      # skip this zone today
```

`appRateMmPerH[z]` (mm of water delivered per hour) is **measured per zone** via the calibrate command
(§5) and stored in KVS; defaults are first-guesses (lawn = 192, beds = 18).

### 3.3 Whole-cycle skip guards

- **Rain hold-off:** if `daily.precipitation_sum[today] >= rainHoldoffMm` → skip entire cycle,
  `Shelly.emitEvent("garden.skip_rain", {...})`.
- **Frost guard:** if min hourly `temperature_2m` over the candidate window `< frostCutoffC` → skip,
  emit `garden.skip_frost`.

### 3.4 Pick the calmest morning window

```
totalMin = sum(runMin[z]) + gapSeconds*(#dueZones-1)/60
candidates = start hours in [earliestStartHour, lunchStart)        # default [3, 12)
for each candidate start s such that [s, s+totalMin] fits before lunchStart
        and does not overlap any quiet window:
    score(s) = mean(wind over span) * W_wind + mean(temp over span) * W_temp   # wind dominates
choose s* = argmin score
update the handleWateringStart() schedule timespec via makeTimespec(s*)         # pool-pump.js:1604-1612
emit garden.plan {start_h: s*, zones: [...], minutes: [...], wind, temp}
persist the due-zone list [{id, minutes}] to Script.storage for the runtime state machine
```

Use `W_wind` ≫ `W_temp` (e.g. 1.0 vs 0.1) so wind dominates, per the requirement. Quiet windows
(`[lunchStart,lunchEnd]`, `[eveningStart,eveningEnd]`) are excluded both as start and as overlap.

### 3.5 Fallback (no forecast / offline)

If forecast data is unavailable: schedule `handleWateringStart()` at `fallbackStartHour` (05:00) with
per-zone `fallbackRunMinutes`, emit `garden.plan_fallback`. Never throw.

### 3.6 Sequential watering state machine — `handleWateringStart()`

- Read the due-zone list from `Script.storage`.
- Drive with **one recurring tick timer** (≈ 20 s): turn current zone ON; when elapsed ≥ its minutes,
  turn it OFF and advance; only ever one switch ON. Emit `garden.zone_start` / `garden.zone_stop`.
- **Abort guard:** before each zone, if `now` is in (or the zone would cross into) a quiet window,
  stop and emit `garden.aborted_quiet`. (Planner sizing should prevent this; guard is defense-in-depth.)
- On finish: clear the tick timer, emit `garden.cycle_done`.
- Reuse pool-pump's **one-at-a-time enforcement** (`handleSwitchEvent`, pool-pump.js:1176-1260): if any
  zone turns on from any source, force the other two off. Reuse the **software anti-cycle fuse**
  (pool-pump.js:947-1006) to protect the valves/relays.

### 3.7 Manual button

Pro3 button cycles zones off→0→1→2→off (model on `cycleOutputs`, pool-pump.js:1071-1140), one-at-a-time,
ignoring the deficit model but **still refusing to turn on inside a quiet window** (emit event).

---

## 4. Reboot resilience & run counting

### 4.1 Startup state detection (`enforceOutputState`)

At every script start (power-on, firmware update, script restart), `enforceOutputState()` runs before
`STATE.initializing` is cleared. It inspects all three switch states synchronously via
`Shelly.getComponentStatus("switch:N")`:

| Switch state at startup | Meaning | Action |
|---|---|---|
| All OFF | Normal start or graceful stop before reboot | No action — proceed |
| Any ON | Device rebooted **mid-cycle** | Turn all ON switches OFF; clear `Script.storage` watering queue; emit `garden.reboot_recovery {zones_on:[...]}` |

Turning off and clearing the queue is safe because:
- Elapsed time for the interrupted zone is unknown, so deficit credit cannot be applied correctly.
- The planner rebuilds tonight's cycle from KVS deficits (which were persisted before the reboot) and
  emits a fresh `garden.plan`.
- The operator sees the `garden.reboot_recovery` event in the daemon events DB and knows a cycle was cut short.

### 4.2 Per-zone run counting

Every time a zone completes normally (after `garden.zone_stop`), `incrementRunCount(zoneId)`:
- Increments `Script.storage` key `runs/<id>` (survives reboots, synchronous).
- Mirrors the new count to KVS key `script/garden/zone<id>-runs` (readable by `ctl garden status`
  without the daemon).

The daemon events DB additionally gives the full run history (one `garden.zone_stop` row per
completion) with timestamps, applied mm, and resulting deficit — useful for multi-week analysis.

Runs interrupted by a reboot are **not** counted (the zone never emitted `garden.zone_stop`).

---

## 5. Measured calibration data

### Grass zones (switch:0 and switch:2)

Each lawn zone has **two rotating pop-up heads** connected to the same valve, spraying from opposite
sides of the zone. Measurement method: catch-cup, single head running alone.

| Measurement | Value |
|---|---|
| Single head, 5 min | **8 mm** |
| Single head application rate | 8 mm / 5 min × 60 = **96 mm/h** |
| Both heads combined (same zone) | 2 × 96 = **192 mm/h** |

Summer budget check (ET₀ = 6 mm/day, Kc = 0.8, ETc = 4.8 mm/day):
- `triggerMm = 12` → deficit accumulates in ~2.5 days without rain
- At 192 mm/h: deliver 12 mm in **3.75 min**, deliver max-deficit 25 mm in **7.8 min**
- `fallbackMin = 8` → delivers ~25.6 mm ≈ 5-day deficit (internet-outage fallback)
- `maxMin = 15` → hard cap at 48 mm (above maxDeficitMm=25, so effectively never reached)

### Flower beds (switch:1 — massifs)

Uses drip pipe; no emitter specs or area measurements available. Conservative starting defaults retained:

| Default | Value | Rationale |
|---|---|---|
| `appRateMmH` | 18 mm/h | Placeholder — to be updated via KVS after observing plant health |
| `triggerMm` | 8 mm | Water more frequently than lawn (shallow-rooted ornamentals) |
| `fallbackMin` | 15 min | Conservative placeholder |
| `maxMin` | 30 min | Conservative cap |

To adjust without re-uploading: `KVS.Set script/garden/zone1-app-rate <new_value>` then restart script.

---

## 6. CONFIG_SCHEMA (device KVS, prefix `script/garden/`)

Model the schema object + `initConfig`/`loadConfig` loader on pool-pump.js:37-353. **Keep KVS key
suffixes ≤ 18 chars** (prefix is 14; hard limit 42).

**Global keys**

| Config field | KVS suffix | Default | Type | Notes |
|---|---|---|---|---|
| enableLogging | `logging` | `true` | bool | |
| mqttTopicPrefix | `mqtt-topic` | `garden` | string | cliOnly |
| earliestStartHour | `earliest-start` | `3` | number | |
| lunchStart / lunchEnd | `lunch-start` / `lunch-end` | `12.0` / `14.0` | number | fractional hours |
| eveningStart / eveningEnd | `evening-start` / `evening-end` | `19.0` / `23.5` | number | fractional hours |
| fallbackStartHour | `fallback-start` | `5` | number | |
| frostCutoffC | `frost-cutoff-c` | `2` | number | |
| rainHoldoffMm | `rain-holdoff-mm` | `8` | number | |
| maxDeficitMm | `max-deficit-mm` | `25` | number | |

**Per-zone keys** (`z` ∈ {0,1,2})

| Config field | KVS suffix | Default (z0 lawn / z1 beds / z2 lawn) | Type |
|---|---|---|---|
| name | `zone<z>-name` | `pelouse-maison` / `massifs` / `pelouse-barriere` | string |
| appRateMmPerH | `zone<z>-app-rate` | `192` / `18` / `192` | number |
| cropFactor (Kc) | `zone<z>-kc` | `0.8` / `0.6` / `0.8` | number |
| triggerDeficitMm | `zone<z>-trigger-mm` | `12` / `8` / `12` | number |
| maxRunMinutes | `zone<z>-max-min` | `15` / `30` / `15` | number |
| fallbackRunMinutes | `zone<z>-fallback-min` | `8` / `15` / `8` | number |
| group | `zone<z>-group` | `lawn` / `beds` / `lawn` | string |
| intervalDays | `zone<z>-interval` | `1` / `4` / `1` | number |
| enabled | `zone<z>-enabled` | `true` | bool |
| run count (read-only) | `zone<z>-runs` | — | number | written by script, read by CLI |
| group last-watered (read-only) | `group/<name>-last` | — | number | day-number, written by script (§12) |

---

## 7. CLI + Go integration (mirror the pool-pump wiring)

| pool-pump artifact (existing) | new garden artifact |
|---|---|
| `myhome/ctl/pool/main.go` | `myhome/ctl/garden/garden.go` (`gardenCmd`, `GardenCmd()`) ✅ |
| `myhome/ctl/pool/setup.go` | `myhome/ctl/garden/setup.go` ✅ |
| `myhome/ctl/pool/status.go` | `myhome/ctl/garden/status.go` ✅ |
| — | `myhome/ctl/garden/calibrate.go` ✅ |
| `myhome/ctl/pool/generate.go` | `myhome/ctl/garden/generate.go` ✅ |
| `tools/extract-pool-defaults/main.go` | `tools/extract-garden-defaults/main.go` ✅ |

Commands:
- `ctl garden setup <device>` — upload `garden.js`, write `script/garden/*` KVS, create schedules ✅
- `ctl garden status <device>` — read per-zone deficits, run counts, last plan ✅
- `ctl garden calibrate <device> <zone> <minutes>` — `script.eval handleCalibrate(<zone>, <minutes>)` ✅

---

## 8. Files created / modified

**Created** ✅
- `internal/shelly/scripts/garden.js` — the controller (auto-embedded via `//go:embed *.js`).
- `myhome/ctl/garden/garden.go`, `setup.go`, `status.go`, `calibrate.go`, `generate.go`.
- `tools/extract-garden-defaults/main.go`.

**Modified** ✅
- `myhome/ctl/ctl.go` — registered `garden.GardenCmd()` + import.
- `.gitignore` — added `*_defaults_generated.go` (both pool and garden); untracked generated files.
- `docs/garden-sprinklers-plan.md` — this file.

**Removed** ✅
- `internal/shelly/scripts/precipitation-irrigation.js` — third-party single-zone example, wrong API.

**No change needed**
- `go.work` (codegen tool is in root module), `internal/shelly/scripts/scripts.go` (`//go:embed *.js`).

---

## 9. Phased execution checklist

- [x] **Phase 0 — Skeleton & wiring.** Create empty `garden.js`, `myhome/ctl/garden/garden.go`, register in `ctl.go`. `make build` green.
- [x] **Phase 1 — Config + codegen.** Full `CONFIG_SCHEMA`; `tools/extract-garden-defaults`; `generate.go`; `make generate && make build` green.
- [x] **Phase 2 — Forecast fetch.** Open-Meteo fetch/cache/location with extended query; ET₀/rain/wind/temp extraction in `onForecast`.
- [x] **Phase 3 — Planner.** Deficit update, per-zone minutes, skip guards, calm-window selection, schedule timespec update, fallback. `garden.plan*` events.
- [x] **Phase 4 — Runtime state machine.** `handleWateringStart()` tick-timer sequencing, one-at-a-time enforcement, anti-cycle fuse, quiet-window abort guard, button cycling.
- [x] **Phase 5 — CLI.** `setup` (upload + KVS + schedules), `status`, `calibrate` + `handleCalibrate`.
- [x] **Phase 5b — Calibration data.** Grass zones updated to 192 mm/h (measured: 8 mm/5 min per head, 2 heads/zone). Massifs kept at placeholder defaults pending observation.
- [x] **Phase 5c — Reboot resilience.** `enforceOutputState()` turns off any ON switch at startup, clears the interrupted watering queue, emits `garden.reboot_recovery`. Per-zone run counters in `Script.storage` + KVS mirror (`zone<z>-runs`), incremented on `garden.zone_stop`.
- [x] **Phase 6 — Verify on device** (§10). Live testing on `arrosage` (2026-06-14/15). Two runtime bugs found and fixed:
  - **OOM** (`out_of_memory`): Open-Meteo `past_days=3,forecast_days=2` response was ~5 KB → ~28 KB heap peak on 30 KB limit. Fixed by switching to `past_days=1,forecast_days=1` (~2 KB, 18.5 KB peak, 14 KB free). Added stale-URL guard to invalidate any cached URL with old parameters.
  - **Too many calls** in `updatePlanSchedule`: `updateDeficits()` fired 3 fire-and-forget `KVS.Set` calls (per-zone deficit mirrors), plus 2 more for plan data = 5 concurrent RPC calls, then `Schedule.List` exceeded the 5-call limit. Fixed by extracting a serial commit chain (`doPlanCommitStep`/`commitPlan`/`commitFallback`) that processes one KVS write at a time via the task queue, then calls `updatePlanSchedule` only after all writes complete.
  - **KVS pagination**: `KVS.GetMany "script/garden/*"` hit MQTT message size limit at 22 of 38 keys. Fixed in `status.go` by making 4 targeted calls (`zone0*`, `zone1*`, `zone2*`, `*`) and merging results.
  - **First automated cycle ran overnight** successfully (2026-06-15 ~03:00): all 3 zones completed, `zone<n>-runs = 1`, deficits reduced in Script.storage.

---

## 10. Verification

1. **Build/codegen/tests:** `make generate && make build && make test` (canonical; all sub-modules). ✅
2. **JS sanity (device powered):**
   `go run ./myhome ctl shelly script upload arrosage internal/shelly/scripts/garden.js --no-minify`
   then `go run ./myhome ctl shelly script debug arrosage true` and watch logs.
3. **Live dry-run** via MCP `shelly_call` on `arrosage`: ✅
   - `Schedule.List` → confirmed `handlePlan` (00:30) + `handleWateringStart` jobs exist. ✅
   - `KVS.GetMany` → confirmed 38 config keys written. ✅
   - `Script.Eval handlePlan()` → `garden.plan` emitted, schedule timespec updated. ✅
   - `Script.Eval handleWateringStart()` → quiet-window abort at 20:33 (switches stayed OFF). ✅
   - `go run ./myhome ctl garden calibrate arrosage 0 2` → refused in quiet window (switch stayed OFF). ✅
     Note: CLI prints "Calibration started" regardless of quiet-window refusal (script returns `undefined` in both cases). Device emits `garden.calibrate_refused` event. Minor UX improvement noted for follow-up.
   - `script/garden/zone<n>-runs` KVS keys written by serial commit chain. ✅
4. **Reboot recovery:** switch:0 turned on manually, script stopped/started → switch turned off (source: "loopback"), `garden.reboot_recovery` emitted. ✅
5. **Resilience:** set forecast URL to `http://192.0.2.1/...?daily=...` (unreachable, 10 s timeout). Planner emitted `garden.plan_fallback` with `start_h=5`, all zones at `fallbackMin`. ✅
6. **Quiet-window guard:** `handleWateringStart()` called at 20:33 → returned immediately without turning on any switch. ✅
7. **Full automated cycle (overnight 2026-06-15):** `handlePlan()` at 00:30 computed plan; `handleWateringStart()` ran all 3 zones sequentially. Post-run: `zone<n>-runs = 1`, deficits reduced in Script.storage. ✅
8. **`ctl garden status arrosage`:** shows all 38 KVS keys, correct deficits and plan. ✅

---

## 11. Phase 7 — Differentiated cadence by group

> **Status:** implemented. See `docs/garden-differentiated-cadence-plan.md` for the full design
> rationale and verification checklist (this section summarizes the shipped result).

The per-zone ET₀ deficit model alone doesn't let zones with the same deficit profile diverge in
*how often* they run. Each zone now carries a **`group`** (string) and **`intervalDays`** (minimum
days between waterings for that group, effective interval = `min` across enabled members):

- `pelouse-maison` (0) and `pelouse-barriere` (2) → `group: "lawn"`, `intervalDays: 1` — they fire
  **together**: whenever the lawn group is due and *any* enabled member's deficit crosses its
  trigger, both water that day, each with its own ET₀-computed minutes.
- `massifs` (1) → `group: "beds"`, `intervalDays: 4` — waters at most every 4 days, even though its
  own deficit may cross `triggerMm` sooner; the deficit keeps accumulating (capped at
  `maxDeficitMm`) during the "rest" days. **Tuned 2026-06-21** (was 7 at initial ship): this bed
  mixes true-mediterranean plants (rosemary, society garlic, boxwood, Phormium, abelia, feijoa —
  happy with a weekly soak) with thirstier ones (lemon, orange, Strelitzia, Agapanthus, daylily,
  Carex) that stress without water for a full week in peak summer heat. 4 days is the compromise;
  see plant list in `ZONE_DEFAULTS` comment in `garden.js`.
- A future `pots` ("plantes en pot") group needs no script changes — just a new `group` string once
  a hardware channel exists.

`computeZonePlan()` groups enabled zones by `group`, gates each group by
`todayDayNumber() - group-last/<name> >= groupInterval` (day-number tracked in `Script.storage`,
mirrored to KVS key `group/<name>-last`), and only updates `group-last` when a member **actually
completes** watering (in `tickWatering()`, not at plan time) — so a quiet-window or rain abort never
wrongly skips a week. The offline fallback planner intentionally ignores cadence (resilience over
schedule fidelity).

**Manual runs** (button / external `Switch.Set`) remain uncounted and do not affect deficits or
group cadence — by design, to keep the model simple; only the existing `garden.manual` event
records them.

---

## 12. Assumptions & out of scope

- **Forecast-only** — no physical rain/soil-moisture sensor assumed on Pro3 inputs.
- Liters/volume reporting is informational only (no per-zone area model unless added later).
- **Winter** is handled implicitly (ET₀ ≈ 0 + rain ⇒ deficit stays below trigger ⇒ no watering); add an
  explicit season switch only if desired.
- `appRateMmPerH` / `cropFactor` defaults are guesses to be calibrated in the field.
- Massifs drip rate is a placeholder; adjust `zone1-app-rate` KVS key after observing plant health.
