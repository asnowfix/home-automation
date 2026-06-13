# Garden Sprinkler Controller (`garden.js`) — Implementation Plan

> **Status:** not started. This document is the executable spec for the feature on branch
> `worktree-feature+garden-sprinklers`. It is written to be picked up by any coding agent
> (target: Sonnet 4.6) with no prior conversation context. Mark each phase done (check the boxes)
> and commit this file alongside the implementation, per the repo's "non-trivial tasks" rule in
> `CLAUDE.md`.

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
| `switch:0` | pelouse côté maison | `pelouse-maison` | lawn |
| `switch:1` | massifs | `massifs` | flower beds |
| `switch:2` | pelouse côté barrière | `pelouse-barriere` | lawn |

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
(§5) and stored in KVS; defaults are first-guesses (lawn ≈ 12, beds ≈ 18).

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

## 4. CONFIG_SCHEMA (device KVS, prefix `script/garden/`)

Model the schema object + `initConfig`/`loadConfig` loader on pool-pump.js:37-353. **Keep KVS key
suffixes ≤ 18 chars** (prefix is 14; hard limit 42).

**Global keys**

| Config field | KVS suffix | Default | Type | Notes |
|---|---|---|---|---|
| enableLogging | `logging` | `true` | bool | |
| mqttTopicPrefix | `mqtt-topic` | `garden` | string | cliOnly |
| pastDays | `past-days` | `3` | number | |
| earliestStartHour | `early-start-h` | `3` | number | |
| lunchStart / lunchEnd | `lunch-start` / `lunch-end` | `12.0` / `14.0` | number | fractional hours |
| eveningStart / eveningEnd | `eve-start` / `eve-end` | `19.0` / `23.5` | number | fractional hours |
| fallbackStartHour | `fb-start-h` | `5` | number | |
| frostCutoffC | `frost-c` | `2` | number | |
| rainHoldoffMm | `rain-holdoff` | `8` | number | |
| maxDeficitMm | `max-deficit` | `25` | number | |
| windWeight / tempWeight | `wind-w` / `temp-w` | `1.0` / `0.1` | number | scoring weights |
| gapSeconds | `gap-s` | `15` | number | between zones |

**Per-zone keys** (`z` ∈ {0,1,2})

| Config field | KVS suffix | Default (z0 lawn / z1 beds / z2 lawn) | Type |
|---|---|---|---|
| name | `z<z>-name` | `pelouse-maison` / `massifs` / `pelouse-barriere` | string |
| appRateMmPerH | `z<z>-rate` | `12` / `18` / `12` | number |
| cropFactor (Kc) | `z<z>-kc` | `0.8` / `0.6` / `0.8` | number |
| triggerDeficitMm | `z<z>-trig` | `12` / `8` / `12` | number |
| maxRunMinutes | `z<z>-max-min` | `30` | number |
| fallbackRunMinutes | `z<z>-fb-min` | `15` | number |
| enabled | `z<z>-on` | `true` | bool |

> All defaults are starting guesses. `appRateMmPerH` and `cropFactor` are meant to be tuned from real
> measurements (see calibrate, §5) without re-uploading the script.

---

## 5. CLI + Go integration (mirror the pool-pump wiring)

| pool-pump artifact (existing) | new garden artifact (to create) |
|---|---|
| `myhome/ctl/pool/main.go` (`poolCmd`, `PoolCmd()`) | `myhome/ctl/garden/main.go` (`gardenCmd`, `GardenCmd()`) |
| `myhome/ctl/pool/setup.go` / `status.go` | `myhome/ctl/garden/setup.go` / `status.go` |
| — | `myhome/ctl/garden/calibrate.go` (new sub-command) |
| `myhome/ctl/pool/generate.go` (`//go:generate`) | `myhome/ctl/garden/generate.go` |
| `myhome/ctl/pool/pool_defaults_generated.go` | `myhome/ctl/garden/garden_defaults_generated.go` (generated) |
| `tools/extract-pool-defaults/main.go` | `tools/extract-garden-defaults/main.go` |
| `internal/myhome/shelly/script/pool.go` | `internal/myhome/shelly/script/garden.go` |

Wiring details verified in this repo:
- **Register the command** in `myhome/ctl/ctl.go` next to line 159 `Cmd.AddCommand(pool.PoolCmd())` →
  add `Cmd.AddCommand(garden.GardenCmd())` (and the import).
- **Codegen** is run via `go run` from `generate.go`; `tools/extract-*-defaults` lives in the **root
  module** (it is **not** a separate `go.work` entry), so **no `go.work` change is needed**. New
  `generate.go` content:
  ```go
  package garden
  //go:generate go run ../../../tools/extract-garden-defaults/main.go ../../../internal/shelly/scripts/garden.js garden_defaults_generated.go
  ```
- **Script embedding** is automatic: `internal/shelly/scripts/scripts.go` has `//go:embed *.js`, so
  dropping `garden.js` into that directory is sufficient — no Go change to register it.
- `extract-garden-defaults/main.go` parses the `CONFIG_SCHEMA` literal (regex, as pool's does) and emits
  `const Default... = ...` into `package garden`. Mirror `tools/extract-pool-defaults/main.go`.

Commands:
- `ctl garden setup <device>` — upload `garden.js` (reuse the script upload/version machinery used by
  `ctl shelly script upload`), write `script/garden/*` KVS from flags (lat/lon optional → auto-detect),
  create the base `handlePlan` + `handleWateringStart` schedules (model on pool-pump.js:1432-1519 and
  `internal/myhome/shelly/script/pool.go`).
- `ctl garden status <device>` — read & print per-zone deficits, last plan (next start + per-zone
  minutes), and the most recent cycle.
- `ctl garden calibrate <device> <zone> <minutes>` — `script.eval handleCalibrate(<zone>, <minutes>)`:
  runs exactly one zone for the requested time (one-at-a-time + quiet windows honored), emitting
  `garden.calibrate_start`/`garden.calibrate_stop`. Operator measures applied depth (catch-cups / rain
  gauge), divides by minutes → mm/min × 60 = mm/h, then sets `z<z>-rate` via `ctl garden setup`.

---

## 6. Files to create / modify

**Create**
- `internal/shelly/scripts/garden.js` — the controller (auto-embedded).
- `myhome/ctl/garden/main.go`, `setup.go`, `status.go`, `calibrate.go`, `generate.go`.
- `tools/extract-garden-defaults/main.go`.
- (generated, committed) `myhome/ctl/garden/garden_defaults_generated.go`.

**Modify**
- `myhome/ctl/ctl.go` — register `garden.GardenCmd()` (~line 159) + import.
- `docs/garden-sprinklers-plan.md` — this file: tick phase boxes as you go.

**No change needed**
- `go.work` (codegen tool is in root module), `internal/shelly/scripts/scripts.go` (`//go:embed *.js`).

---

## 7. Phased execution checklist

- [ ] **Phase 0 — Skeleton & wiring.** Create empty `garden.js` (just `init()` + logging + KVS load
      stub), `myhome/ctl/garden/main.go`, register in `ctl.go`. `make build` green. Commit.
- [ ] **Phase 1 — Config + codegen.** Define full `CONFIG_SCHEMA` in `garden.js`; write
      `tools/extract-garden-defaults`; add `generate.go`; `make generate && make build` green; verify
      `garden_defaults_generated.go` matches the schema. Commit.
- [ ] **Phase 2 — Forecast fetch.** Port Open-Meteo fetch/cache/location with the extended query (§2);
      add the `et0`/rain/wind/temp extraction in `onForecast`. Add Go unit test stub if feasible. Commit.
- [ ] **Phase 3 — Planner.** Implement deficit update, per-zone minutes, skip guards, calm-window
      selection, schedule timespec update, fallback. Emit `garden.plan*`. Add Go unit tests for the
      math + window selection (mirror pool tests). Commit.
- [ ] **Phase 4 — Runtime state machine.** Implement `handleWateringStart()` tick-timer sequencing,
      one-at-a-time enforcement, anti-cycle fuse, quiet-window abort guard, button cycling. Commit.
- [ ] **Phase 5 — CLI.** Implement `setup` (upload + KVS + base schedules), `status`, `calibrate` +
      `handleCalibrate` in the script. Commit.
- [ ] **Phase 6 — Verify on device** (see §8) once `arrosage` is powered. Commit any fixes.

> Commit after each green phase. Push regularly to the PR branch.

---

## 8. Verification

1. **Build/codegen/tests:** `make generate && make build && make test` (canonical; all sub-modules).
2. **Go unit tests:** deficit→minutes math, calm-window selection incl. quiet-window exclusion,
   rain/frost skip. Mirror existing pool tests' style.
3. **JS sanity (device powered):**
   `go run ./myhome ctl shelly script upload arrosage internal/shelly/scripts/garden.js --no-minify`
   then `go run ./myhome ctl shelly script debug arrosage true` and watch logs.
4. **Live dry-run** via MCP `shelly_call` on `arrosage`:
   - `Schedule.List` → confirm `handlePlan` + `handleWateringStart` jobs exist.
   - `KVS.GetMany {match:"script/garden/*"}` → confirm config written.
   - `Script.Eval handlePlan()` → confirm `garden.plan` event emitted and the watering-start schedule
     timespec updated to the chosen calm hour.
   - `Script.Eval handleWateringStart()` → confirm zones fire **one at a time**, in sequence, and stop.
   - `go run ./myhome ctl garden calibrate arrosage 0 2` → confirm a single 2-minute zone-0 run.
5. **Resilience:** point `forecast-url` at an unreachable host (or block egress); confirm planner emits
   `garden.plan_fallback` and schedules the fixed fallback rather than erroring.
6. **Quiet-window:** craft a plan whose span would overlap 12:00 or 19:00; confirm the planner relocates/
   shrinks the window and the runtime abort-guard prevents any zone running inside a quiet window.

---

## 9. Assumptions & out of scope

- **Forecast-only** — no physical rain/soil-moisture sensor assumed on Pro3 inputs.
- Liters/volume reporting is informational only (no per-zone area model unless added later).
- **Winter** is handled implicitly (ET₀ ≈ 0 + rain ⇒ deficit stays below trigger ⇒ no watering); add an
  explicit season switch only if desired.
- `appRateMmPerH` / `cropFactor` defaults are guesses to be calibrated in the field.
