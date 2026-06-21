# Garden Sprinklers — Differentiated Cadence by Group (implementation plan)

> **Status:** Implemented and verified live on `arrosage` (2026-06-20). This is a self-contained
> handover doc — a coding agent should be able to execute it end to end without further context.
> **Branch/worktree:** `feature+differentiated-sprinklers`.
> **Prerequisite reading:** `docs/garden-sprinklers-plan.md` (the original controller design),
> `CLAUDE.md` + `AGENTS.md` (Shelly JS / Espruino ES5 rules, KVS key limits, Pro3 limits).
>
> **Post-deployment tuning (2026-06-21):** the body below documents the *original* design
> (`massifs` `intervalDays: 7`). After reviewing the actual plant list in the massifs bed —
> rosemary, society garlic, boxwood, Phormium, abelia, feijoa (true mediterranean, happy weekly)
> mixed with lemon, orange, Strelitzia, Agapanthus, daylily, Carex (want water more often in peak
> summer heat) — the shipped default was retuned to **`intervalDays: 4`** as a compromise. See the
> plant-list comment above `ZONE_DEFAULTS` in `garden.js` and `docs/garden-sprinklers-plan.md` §11.
> Treat `7` below as historical context for *why* a per-group interval exists, not the current value.

---

## 1. Context & motivation

`internal/shelly/scripts/garden.js` runs on the Pro3 device **`arrosage`**
(`shellypro3-34987a48c26c`, `192.168.1.83`) and drives 3 valves, one at a time:

| Switch | Zone | Type | Kc | triggerMm |
|---|---|---|---|---|
| `switch:0` | `pelouse-maison` | lawn (2 pop-up heads) | 0.8 | 12 |
| `switch:1` | `massifs` | mediterranean beds (drip) | 0.6 | 8 |
| `switch:2` | `pelouse-barriere` | lawn (2 pop-up heads) | 0.8 | 12 |

**Today** the controller waters each zone purely by its **own ET₀ soil-water deficit**. Daily at
00:30 `handlePlan()` calls `updateDeficits()` (adds `ET₀·Kc − rain` to `deficit/<id>` in
`Script.storage`), then `computeZonePlan()` waters any zone whose `deficit ≥ triggerMm`. There is
**no group concept and no day cadence** — only the per-zone deficit gate.

**Problem this solves:**
- `switch:0` + `switch:2` are two halves of the **same lawn**; they should always water on the
  **same days** (today they only coincide because their config is identical — nothing enforces it).
- `switch:1` (massifs, drought-tolerant) should water **infrequently / weekly**, not whenever its
  deficit crosses 8 mm (~every 2 days in peak summer).
- A future **`pots`** ("plantes en pot") group is anticipated but has no hardware channel yet.

**Goal:** add a lightweight **group** abstraction with a **per-group minimum interval (days)** that
gates the existing deficit logic. The ET₀-computed water *amount* (especially for grass) is unchanged.

### Confirmed product decisions
1. **Cadence = minimum interval in days** (still subject to deficit/rain/frost gating), **not** fixed
   calendar weekdays.
2. **Massifs default = weekly** (`intervalDays = 7`); lawn = `1` (effectively unconstrained). Both
   tunable at runtime via KVS.
3. **Lawn zones 0 & 2 fire together**: when the lawn group is due and *any* member's deficit crosses
   its trigger, **both** enabled members water that day, each with its own ET₀-computed minutes.

---

## 2. Design

Add two per-zone config fields plus a per-group "last watered" tracker.

- **`group`** (string): `"lawn"` for zones 0 & 2, `"beds"` for zone 1. A future pots zone just uses
  `"pots"` — no code change needed to add a group, only config.
- **`intervalDays`** (number): minimum days between waterings for that zone's group (lawn `1`, beds `7`).
- A group's **effective interval** = `min(intervalDays among its enabled members)`.
- **`group-last/<name>`** in `Script.storage`: a local-aligned **day number** (not a date string) of
  the group's last actual watering. Default to a large-negative sentinel so the first run is always
  eligible.

**When is `group-last` written?** Only when a member of the group **actually completes** watering
(in the `tickWatering` zone-complete branch), **not** at plan time. This ensures a quiet-window or
rain abort does not wrongly advance the cadence and skip a week.

**Planner gate** (inside the daily plan):
1. `updateDeficits()` runs unchanged — deficits keep accumulating while a group "rests," so massifs
   builds up a deep weekly soak (clamped to `maxDeficitMm = 25`).
2. Group the enabled zones by `group`.
3. A group is **eligible** iff `todayDayNumber() − loadGroupLastDay(group) ≥ groupInterval`.
4. An eligible group **fires** iff *any* enabled member has `deficit ≥ triggerMm`.
5. When a group fires, add **every** enabled member whose computed minutes ≥ 1 (this is what makes
   the lawn "fire together"). Minutes math is unchanged:
   `min(deficit, maxDeficitMm) / appRateMmH * 60`, capped by `maxMin`, rounded, must be ≥ 1.
6. Ineligible / non-firing groups contribute nothing that day.

Whole-cycle **rain-holdoff** and **frost** guards stay as-is (they skip the entire day's cycle).
The **fallback planner** (offline, no forecast) keeps watering all enabled zones and **ignores
cadence by design** — document this with a one-line comment.

### Day-number helper
**Confirmed on-device:** Espruino's `Date` has no `getTimezoneOffset()` — calling it throws
`Uncaught Error: Function "getTimezoneOffset" not found!` and crashes the script (caught live
during verification on `arrosage`, see §4). Use plain UTC day number via `Date.now()` instead
(already used elsewhere in the script, e.g. `WATERING_START_MS = Date.now()`):
```js
function todayDayNumber() {
  return Math.floor(Date.now() / 86400000);
}
```
This is coarse by design — watering always runs near the same local hour, so a few hours of zone
offset near the UTC day boundary don't meaningfully affect "every N days" gating.

---

## 3. Exact changes

### 3.1 `internal/shelly/scripts/garden.js`  (Espruino ES5 — `var` only, no hoisting, define before use)

1. **`ZONE_DEFAULTS`** (~line 103) — add `group` + `intervalDays` to each entry:
   ```js
   {id: 0, name: "pelouse-maison",   appRateMmH: 192.0, kc: 0.8, triggerMm: 12.0, maxMin: 15, fallbackMin: 8,  group: "lawn", intervalDays: 1, enabled: true},
   {id: 1, name: "massifs",          appRateMmH:  18.0, kc: 0.6, triggerMm:  8.0, maxMin: 30, fallbackMin: 15, group: "beds", intervalDays: 7, enabled: true},
   {id: 2, name: "pelouse-barriere", appRateMmH: 192.0, kc: 0.8, triggerMm: 12.0, maxMin: 15, fallbackMin: 8,  group: "lawn", intervalDays: 1, enabled: true}
   ```
2. **`ZONE_KEY_SPECS`** (~line 123) — add two specs (KVS suffixes `zone1-group` = 11, `zone1-interval`
   = 14 chars; both within the ≤18-suffix / ≤32-total budget):
   ```js
   {field: "group",        key: "group",     type: "string"},
   {field: "intervalDays", key: "interval",  type: "number"}
   ```
3. **`initZones()`** (~line 133) — copy `group` and `intervalDays` into each runtime `ZONES` entry.
4. **New helpers** near the deficit helpers (~line 530): `todayDayNumber()` (above),
   `groupLastKey(name)` → `"group-last/" + name`, `loadGroupLastDay(name)` (returns a large-negative
   sentinel, e.g. `-99999`, when unset), `saveGroupLastDay(name)` → stores `todayDayNumber()`.
   Define all of these **before** any function that references them.
5. **`computeZonePlan()`** (~line 572) — restructure:
   - Build a map of `group → enabled members`.
   - For each group: compute `groupInterval = min(member.intervalDays)`; if
     `todayDayNumber() - loadGroupLastDay(group) < groupInterval`, `log("group " + group + " not due
     (" + n + " < " + groupInterval + " d)")` and skip the whole group.
   - Else compute each member's minutes; if **any** member has `deficit ≥ triggerMm`, push **all**
     members with `minutes ≥ 1` to the plan (fire-together). If no member crosses trigger, skip.
   - Preserve existing per-member logging.
   - Espruino has no `Array.prototype` niceties — iterate with `for` loops; build the group map with a
     plain object keyed by group name and a parallel array of names (no `Object.keys` reliance issues;
     use `for (k in obj)`).
6. **`tickWatering()` zone-complete branch** (~line 930, right after `incrementRunCount(entry.id)`):
   call `saveGroupLastDay(zone.group)` and add a best-effort KVS mirror (fire-and-forget, exactly like
   the runs mirror in `incrementRunCount`):
   ```js
   Shelly.call("KVS.Set", {key: CONFIG_KEY_PREFIX + "group/" + zone.group + "-last", value: String(todayDayNumber())},
     function(r, e) { if (e && false) {} });
   ```
   (`zone` is already resolved from `ZONES` earlier in `tickWatering`; `zone.group` exists after
   step 3.)
7. **`runFallbackPlanner()`** (~line 1061) — add a one-line comment that the offline fallback
   intentionally ignores group cadence and waters all enabled zones.

> ⚠️ Espruino guard-rails (crashes if violated): `var` not `let/const`; no function hoisting (define
> before reference, including callbacks); max 2–3 nested anon functions; never an empty `catch` — use
> `catch (e) { if (e && false) {} }`; property checks via `"prop" in obj`; no `Array.shift/unshift`;
> per-script limits 5 timers / 5 event-subs / 5 concurrent RPC. KVS keys lowercase, hyphens/slashes,
> ≤42 chars. Upload with `--no-minify` while debugging.

### 3.2 `tools/extract-garden-defaults/main.go`
Zone defaults are a **hardcoded literal inside `outputTemplate`** (they are NOT parsed from the JS;
only the global scalars are). Update the template's `ZoneDefault` struct and `defaultZoneDefaults`:
- Add fields `group string` and `intervalDays int` to the `ZoneDefault` struct text.
- Set them in the three literal rows (`lawn`/`1`, `beds`/`7`, `lawn`/`1`).
Global-scalar extraction (`extractDefaults`) and `tools/extract-garden-defaults/main_test.go` are
**unaffected** (the test only checks global scalars). Then run `make generate` to regenerate
`myhome/ctl/garden/garden_defaults_generated.go`.

### 3.3 `myhome/ctl/garden/setup.go`
In `defaultZoneKVS()` (~line 57), inside the per-zone loop, add:
```go
m[pfx+"group"]    = z.group
m[pfx+"interval"] = fmt.Sprintf("%d", z.intervalDays)
```
(`z` is a `ZoneDefault`; the new fields come from §3.2.)

### 3.4 `myhome/ctl/garden/status.go`  (minor, optional but recommended)
The new keys already arrive via the existing 4 `KVS.GetMany` prefix calls. Add a short **"Cadence"**
section that prints, per zone, `zoneN-group` + `zoneN-interval`, plus any `group/<name>-last` entries,
so operators can see cadence state without the daemon.

### 3.5 `docs/garden-sprinklers-plan.md`
Add a **"Phase 7 — Differentiated cadence by group"** section: the group model, the two new KVS keys,
weekly-massifs default, lawn fire-together rule; extend the per-zone KVS table with `zone<z>-group`
and `zone<z>-interval`. Mark this plan file as its companion.

### New KVS keys summary (prefix `script/garden/`)
| Key | Example | Meaning |
|---|---|---|
| `zone<z>-group` | `lawn` / `beds` | group membership |
| `zone<z>-interval` | `1` / `7` | min days between waterings for the zone's group |
| `group/<name>-last` | `group/beds-last = 20259` | day-number of group's last actual watering (script-written, read-only for CLI) |

---

## 4. Verification

1. **Build/test:** `make generate && make build && make test` (canonical; covers all `go.work`
   submodules — never bare `go test ./...`).
2. **JS sanity on device:**
   `go run ./myhome ctl shelly script upload arrosage internal/shelly/scripts/garden.js --no-minify`
   then `go run ./myhome ctl shelly script debug arrosage true` and watch the log for parse/runtime errors.
3. **Write & inspect new config:** `go run ./myhome ctl garden setup arrosage` then
   `go run ./myhome ctl garden status arrosage` — confirm `zone0-group=lawn`, `zone2-group=lawn`,
   `zone1-group=beds`, `zone1-interval=7`, `zone0-interval=1`.
4. **Cadence gate** (MCP `shelly_call` / `Script.Eval` on `arrosage`):
   - Seed `deficit/1` above 8 and `group-last/beds = todayDayNumber()`, run `handlePlan()` →
     `garden.plan` / `last-plan-zones` **excludes** zone 1, log shows "beds not due".
   - Set `group-last/beds` to `todayDayNumber() - 7`, run `handlePlan()` → zone 1 **included**.
5. **Lawn fire-together:** seed `deficit/0` above 12 and `deficit/2` just below 12, ensure
   `group-last/lawn` is ≥1 day old, run `handlePlan()` → **both** 0 and 2 appear in `last-plan-zones`.
6. **`group-last` advances only on real watering:** run `handleWateringStart()` for one zone outside a
   quiet window; after `garden.zone_stop`, confirm `Script.storage` `group-last/<name>` and the KVS
   mirror `group/<name>-last` updated to today's day number; confirm a quiet-window abort leaves them
   unchanged.

---

## 5. Out of scope / notes
- No new daemon config option (config lives in device KVS), so the `options.go`/`run.go`/
  `docs/configuration.md`/`myhome-example.yaml` 4-file rule does **not** apply.
- `pots` group: no hardware channel yet — supported by config only once a switch exists.
- Massifs amount stays ET₀-deficit-driven (capped by `maxMin`); tune `appRateMmH`/`maxMin`/
  `maxDeficitMm` later if weekly delivery is insufficient.
- All logic stays on-device (resilience: internet-optional, daemon-optional per device).
- **Manual runs (confirmed out of scope, asked of user):** button/external `Switch.Set` starts
  remain uncounted, apply no deficit credit, and do **not** advance `group-last` — only the existing
  `garden.manual` event records them. `incrementRunCount` and `saveGroupLastDay` stay solely in the
  automated `tickWatering` completion branch (§3.1 step 6). Do not wire manual starts into either.
