# Design: room view: everything about a room (#684)

Status: draft for owner review, **amended 2026-11-08 by the owner's answers** (see
[Addendum 2026-11-08: owner answers](#addendum-2026-11-08-owner-answers), which is binding where it
conflicts with the text above it; superseded passages are marked **Superseded** in place). Part of
the kiosk campaign #681. Builds on PR #224 (rooms refactor, merged), `683-kiosk-shell.md` (shell,
touch rules, confirm dialog) and `682-retained-state.md` (no device polling, daemon-side
projection, SSE relay).

Read on branch `docs/684-room-view-design`, based on `origin/campaign/681-kiosk-ui`. **(verified)**
means read in code there. **(inferred)** means not traced to the end or not testable from files.

**The architecture reference is partly stale.** `docs/architecture/ARCHITECTURE.md` (2026-11-04)
predates #224 and the first kiosk PRs. It is wrong in these places, and a fresh
`principal-documenter` run should follow:

- §3.1, §8 and §9 name `myhome/temperature`. That package is gone. Rooms now live in
  `myhome/rooms`, the tables are renamed (`rooms`, `room_kind_schedules`, `room_weekday_defaults`),
  and `ical_url`, `weather_cache`, `room_agenda_cache` and the `room.setup` verb were added.
- §5.2 and §5.3: `ui.Start` now also takes the `*sqlx.DB`. `newMux` is split out and tested
  (`server_test.go`). `/htmx/pool` exists. The rooms fragment reads `room_agenda_cache` directly.
- §5.3 and debt #2: `NotifyStatus` deltas are now applied to switch and input state
  (`watch/mqtt.go` `UpdateFromMqttEvent` → `pkg/shelly` `ApplyStatusNotification`, under a lock).
  That is 682 PR 3.
- §2 and §14: the daemon restarts every 2 days (`RuntimeMaxSec=172800`, per `CLAUDE.md`), not 6 h.
- (Added by the addendum) see the addendum's own list of places where the reference misled.

## The ask

Make the Rooms section the one place for everything about a room. A room page shows the current
temperature and target, the heater's state with a control, the room's lights and switches, and a
"clean this room" action for the robot vacuum. For that last action, the robot's own room ids
(regions) must be mapped to the daemon's rooms, and the mapping must be stored and editable. All of
it must work by touch at 1024x600 inside the kiosk shell.

## What exists after #224 (verified)

**Rooms.** `myhome/rooms` (`storage.go`, `methods.go`, `temperature.go`) owns the room config in
`myhome.db`: `rooms(room_id PK, name, kinds JSON, levels JSON, ical_url, updated_at)`, plus kind
schedules and weekday defaults. Its migration renames the old `temperature_*` tables and adds
columns using the `COUNT(*) FROM pragma_table_info` check. Verbs: `room.list|create|edit|delete|setup`
and `temperature.get|set|list|delete|getschedule|...`. `room.edit` saves the levels and republishes
the retained topic `myhome/rooms/<id>/temperature/ranges` (`{room_id, date, day_type, levels,
ranges}`). `heater.js` subscribes to that topic and takes its levels and comfort ranges from it
(`heater.js:801-844`). **There is no daemon API for a room's live temperature or heating state.**
`temperature.getschedule` returns the levels and today's comfort ranges, read from SQLite only.

**How devices map to rooms.** `Device.RoomId` is a column of the `devices` table in `myhome.db`.
`device.setroom` sets it and also writes the device's KVS key `room-id`. `device.listbyroom` reads
it. The UI's `RoomsList` scans `GetAllDevices` (the in-memory cache) for each room and labels each
device with `deviceRoleLabel`, which calls **every** device that is neither BLU nor `shellyht-` a
"heater" (`htmx.go:259-280`). `DeviceView.HasHeaterScript` (from `d.Config` scripts, in memory) is
the reliable signal for a heater.

**Live temperature.** It is per device, in memory: `Status.Sensors.Temperature`, written only by the
Gen1 (`shellies/#`) and BLU (`shelly-blu/events/+`) listeners through `Cache.UpdateSensorValue`. It
is not persisted and carries no update time (`devices/cache.go:293-338`). Gen2 temperature
components are not fed into `Sensors`. `thermometer.list` classifies Gen1 H&T and BLU sensors only.

**Heater.** `heater.js` chooses its target from three inputs (`heater.js:1153-1162`): **away** if
not occupied, **comfort** if inside a comfort range, **eco** otherwise. "Not occupied" includes
"no occupancy message received yet", because `STATE.occupied` starts undefined. The final decision
also weighs cheap hours, preheat, open doors and a frost floor. The script calls `Switch.Set` on
every control cycle (`setHeaterState`). **A manual toggle of the heater relay is undone at the next
cycle.** The relay state reaches the daemon by `NotifyStatus` through the switch projection.
`heater.getconfig` reads the device's KVS, so it is a device RPC and unusable on a render path.

**Vacuum.** `myhome/vacuum` keeps `vacuum_robots` and `vacuum_regions` in `myhome.db`. The primary
key of `vacuum_regions` is `(blid, pmap_id, region_id)`, and its columns are `pmap_version,
region_type, name, source, map_name, kind, removed, synced_at`. Regions come from two sources:
`vacuum.roomsync` (iRobot cloud, run once at daemon start and on demand; the cloud owns the names),
or regions learned from the robot's echo of a room-clean command (`learnRegion`). Both upserts list
their `SET` columns explicitly, so **an added column survives both**. `vacuum.rooms` returns the
regions with `Stale` computed. `vacuum.clean` takes region names **or ids**, refuses stale regions
and regions on different maps, then watches 15 s for an immediate abort with at most one retry, so
one call can take ~30 s. `vacuum.*` verbs are always registered. With no robot configured,
`vacuum.list` is empty.

**UI on this branch.** The shell (683 PR 2) is in place: rail, five sections, hash routing,
`kiosk.visible()`, `data-refresh`, and a "couldn't refresh" pill. Not yet on this branch: the
device row and htmx toggle (683 PR 4; the cards still use the JS `toggleSwitch`), the offline
banner (683 PR 3), and the shared confirm dialog (683 PR 5). Native `confirm()` is still used for
room delete. The vacuum card still carries the per-room checklist that PR #725 removes, and
`VacuumAction`'s `clean-rooms` branch is the handler kept for this issue (`htmx_vacuum.go:131-137`).

## Goals and non-goals

Goals

- G1. A **room page** (a sub-view of Rooms) showing: current temperature, the heating target and
  the level that applies now, each heater's state, a heater control, the room's devices (lights,
  plugs, sensors), and the vacuum block. **Amended:** the heater control is an eco/comfort
  **toggle**, built last in its own PR (addendum §C).
- G2. A **persistent robot-region → room mapping**, many regions to one room, editable by touch.
  Name matches are offered as suggestions. **Superseded:** editable by **CLI and RPC**
  (`vacuum.roomassign`, `ctl vacuum rooms assign`); no touch editor and no suggestions in this
  issue (addendum §A, answer 7).
- G3. **"Clean this room"**, using the mapped regions. The confirmation policy is one line to change.
  **Amended:** it is confirmed on the room page (answer 3).
- G4. **No device RPC added on any render path**. Every widget renders from daemon memory or SQLite
  (682 Rule 3).
- G5. **Testable without hardware.** Every PR is verifiable with fakes and template tests, and
  manually with a daemon that has no devices.
- G6. (Added) **At most one temperature sensor per room**, enforced when a sensor is assigned
  (addendum §B, answer 1).

Non-goals

- **A temporary heating boost or override** ("comfort for 2 h"). This needs a new input to
  `heater.js` on devices with little heap. The control offered here changes the room's level
  persistently instead (Scope question 2). **Superseded:** a temporary eco/comfort override *is*
  now in scope, implemented daemon-side through the existing ranges topic with no `heater.js`
  change (addendum §C). A boost that forces the relay on regardless of cheap hours stays a
  non-goal.
- **Room temperature from Gen2 sensors**, or from the heater's configured internal topic. Only what
  already reaches `Sensors` is shown. Widening that is a separate device-cache change.
- **A daemon `room.status` RPC** for `ctl` and scripts. The page composes existing verbs. A second
  consumer would justify the RPC; this page alone does not.
- **Automatic mapping without a tap**, for example applied by `vacuum.roomsync` at start.
- **A region spanning several daemon rooms** (an open-plan area cleaned as one robot region and
  shown as two rooms). The mapping is many-to-one (Scope question 4). **Confirmed by the owner:
  not now.**
- **Multi-robot UI.** The data model is per robot. The page shows one block per robot, but only one
  robot is supported today.
- **On-screen keyboard.** Room create and rename stay in the existing modal, which needs a keyboard
  (as in 683).
- (Added) **Averaging or choosing among several room thermometers.** A room with two is a
  configuration error, not something the page resolves.
- (Added) **The Rooms landing page and its "Unassigned" group**, which #744 builds in parallel. This
  design only adds the room page that #744's room entries open.

## Quality drivers

The starting points are the reference's local-first and daemon-optional attributes and 683's touch
rules. This feature adds:

1. **No device contact from viewing.** With the kiosk left on a room page for an hour, the daemon
   sends zero RPCs to any Shelly *caused by rendering*. Today's exception, the polarity KVS read in
   `DeviceToView` (682 P6, follow-up E), is inherited, not added to.
2. **Honest values.** An unknown temperature, target or state renders `--`, never a default. The
   shown target uses the same rule as `heater.js`, including "away until presence is known". A
   room page never claims the heater is heating unless the relay state says so.
3. **Safe cleaning.** A tap on "Clean this room" sends only regions that are mapped to *this* room,
   not stale, and on one map. They are resolved on the server at tap time, not from a page that may
   be minutes old. The tap asks for confirmation first (answer 3).
4. **Touch and size.** At 1024x600 (944 px content), the room page shows climate, vacuum and the
   first six device rows without scrolling. Every target is ≥ 48 px, and nothing opens by default.
5. (Added) **Configuration errors are caught where they are made.** When a second temperature
   sensor is assigned to a room, by any path (CLI, web UI, `/rpc`, database import), the assignment
   is refused with a message naming the sensor already there, and nothing is written.

## Fit

**This fits the existing architecture as-is**, with one small schema increment owned by an
existing service:

- **Mapping: one column, `vacuum_regions.room_id`.** It is added through the existing
  `regionColumns` migration loop, with one new verb, `vacuum.roommap`, and `room_id` added to the
  `VacuumRegion` result. No new table, module or dependency. **Amended:** the verb is named
  `vacuum.roomassign` and has a `ctl` command (addendum §A).
- **Room page: new htmx fragments in `internal/myhome/ui`.** They follow the "Add a UI section"
  recipe and 683's sub-view pattern (Back button, state in Alpine, no `pushState`).
- **Room temperature and target: computed at render time** from memory (device cache, occupancy)
  and SQLite (levels, ranges). This is the 682 projection idea at its smallest: no stored state.
  One pure helper in `internal/myhome` mirrors the `heater.js` target rule.

Nothing here adds a subsystem, a Go module, a JS dependency or a config option. **Amended:** the
addendum adds two increments that are still inside existing services, flagged as scope growth in
addendum §D: a room-assignment check in the device manager (answer 1) and a per-room override with
an expiry timer in the rooms service (toggle). Still no new module, dependency or config option.

## Proposed design

### 1. Mapping schema and ownership

> **Superseded in part (2026-11-08):** the verb name, its params and result, the region resolution
> and the CLI are replaced by addendum §A. The schema rows of the table below stand.

| Aspect | Decision |
|---|---|
| Where | `myhome.db`, table `vacuum_regions`, new column `room_id TEXT NOT NULL DEFAULT ''` (`''` = unmapped). |
| Migration | One entry appended to `regionColumns` in `myhome/vacuum/storage.go`. `addRegionColumns` already checks with `COUNT(*)` and runs `ALTER TABLE ... ADD COLUMN` (AGENTS.md "Database Patterns"). |
| Key | The region's existing key `(blid, pmap_id, region_id)`. A mapping belongs to one region on one map. |
| Cardinality | Many regions → one room, through the column. One region → several rooms is not representable (non-goal). |
| Preserved by | `syncCloud` and `learnRegion` (verified: neither `SET` clause names `room_id`). A cloud rename keeps the mapping. A cloud removal marks the region stale and keeps the mapping. |
| Room identity | `rooms.room_id`, never the room's name. Renaming a room keeps its mappings. |
| Deleted room | The `room_id` is left dangling. Readers treat a `room_id` that is not in `room.list` as unmapped. `room.delete` does not reach into the vacuum tables (Alternatives, B). |
| New robot map | A new `pmap_id` gives new regions. The old ones go stale, with their mappings. Those mappings are shown as "robot map changed" and are never cleaned. |

~~**RPC** (the four steps in `CLAUDE.md`): `vacuum.roommap`, `VacuumRoomMapParams{Robot, MapID,
RegionID, RoomID}`, result `VacuumOKResult`, `Storage.mapRegion(...)`.~~ **Superseded** by addendum
§A (`vacuum.roomassign`, optional map id, a result that reports the previous room). Unchanged from
the original: `room_id` is added to `VacuumRegion` (additive, older clients ignore it), `regions()`
selects it, the room check is an in-process RPC call rather than an import of `myhome/rooms`, and
mapping a stale region is allowed while cleaning one is not.

### 2. Name-based suggestions (how `vacuum.roomsync` helps)

> **Superseded (2026-11-08): not built in this issue** (answer 7: no name-suggestions sheet). The
> text is kept as the design for a later issue if touch editing of the mapping is wanted.

`vacuum.roomsync` does not map anything. It is what gives regions *names* (learned regions have
none until `vacuum.roomname`). The UI uses those names to **propose**, never to apply:

- A pure function `suggestRegions(regions []VacuumRegion, rooms []RoomInfo) map[regionKey]roomID`
  in `internal/myhome/ui`. It suggests room R for region G when G is unmapped (or mapped to a
  room that no longer exists), G is not stale, and `norm(G.Name)` equals `norm(R.Name)` or
  `norm(R.ID)` **for exactly one room**. `norm` lowercases, trims, folds runs of space, `-` and `_`
  to one space, and strips Latin diacritics (a fixed table: the house names are in French, so
  "Entrée" should match "entree"). It does no fuzzy matching: a wrong suggestion is worse than none.
- Suggestions appear as a "Suggested" pill in the mapping sheet. They also appear on the room page
  as a one-tap hint: *"Robot area 'Cuisine' matches this room. [Assign]"*.
- Why not apply them in `syncCloud`: the sync runs unattended at every daemon start. A silent
  mapping change can redirect what "clean this room" cleans. And `myhome/vacuum` would then need to
  read the rooms service during its startup sync, which makes startup ordering matter.

### 3. The room page and its data sources

> **Superseded in part (2026-11-08):** the tiles grid is dropped (the Rooms landing page is #744's);
> the "Current temperature", "Heater control" and "Vacuum areas" rows of the widget table are
> replaced as marked; the `level` and `areas` routes are dropped. See addendum §B and §D.

~~**Rooms section root (tiles).** `roomsListTemplate` becomes a grid of tiles ...~~
**Superseded:** #744 makes Rooms the landing page, listing each room's devices with an
"Unassigned" group. This design does not change that page; it only needs #744's room heading to
open the room page (`openRoom = <id>`).

**Room page (sub-view).** It is swapped into a `#room-view` container inside `#sec-rooms`. Alpine
holds `openRoom` (an id or null). Back, a rail tap and a section switch set it to null (683 §3). It
is never open on load: a reload on `/#rooms` shows the room list.

```
+--------------------------------------------------------------------------+
| [< Back]   Cuisine                                     [Edit]            |  56 px
+------------------------------------+-------------------------------------+
| CLIMATE                            | DEVICES                             |
|   21.4 °C          (40 px text)    | (O) Plafonnier              ( P )   |
|   2 h ago (only past 30 min)       | (O) Prise four              ( P )   |
|   Target 20 °C  comfort until 22:00| (H) Radiateur   Heating    no btn   |
|   Heater: heating                  | (T) H&T   [21.2 °C] [58 %]          |
|   [ Eco | Comfort ] (toggle, PR 5) | (D) Fenêtre [Closed]                |
+------------------------------------+   ... (main scrolls)                |
| VACUUM                             |                                     |
|   Areas: Cuisine  (read-only)      |                                     |
|   [   Clean this room   ]  56 px   |                                     |
+------------------------------------+-------------------------------------+
   ~400 px                              ~528 px
```

(The sketch is updated for the addendum: no "N sensors, avg", no stepper, no areas [Edit].)

The layout is a CSS grid, `minmax(0,5fr) minmax(0,7fr)`, with an 8 px gap. Below 700 px wide it
collapses to one column (climate, vacuum, devices). That is the hook for the later phone layout:
no fixed heights, no hover, and no width assumption beyond the breakpoint. Only `main.mh-content`
scrolls (683 rule 4).

| Widget | Shown | Source | Device contact |
|---|---|---|---|
| Name, Edit | `rooms.name`. Edit dispatches the existing `edit-room` event (the modal) | `temperature.get` (SQLite) | none |
| Current temperature | ~~One sensor: its value. Several: the arithmetic mean, labelled "N sensors, avg".~~ **Superseded:** exactly one temperature sensor: its value, plus "· 2 h ago" when older than 30 min. None, or no value yet: `--`. **Two or more** (data predating the rule): no headline value, a warning "Configuration error: N temperature sensors in this room; keep one" (addendum §B) | `DeviceRegistry.GetAllDevices`, filtered on `RoomId` and `myhome.IsTemperatureSensor` (memory); age from the new in-memory update time | none |
| Target and level | "Target 20 °C · comfort until 22:00", "eco until 07:00", or "away (nobody home)". If the occupancy verb is absent: "away (no presence data)", which is what `heater.js` does | `temperature.getschedule` (levels, today's ranges; SQLite) + `occupancy.getstatus` (memory) → `myhome.HeatingTarget(...)` | none |
| Heater state | Per heater device: "Heating", "Idle" or `--` | Devices in the room with `HasHeaterScript`; switch 0 logical state from `DeviceToView` (switch projection, memory) | inherits P6 (polarity KVS read on a cache miss) |
| Heater control | ~~A stepper on the level that applies now, ±0.5 °C ...~~ **Superseded:** an Eco / Comfort toggle, built last (PR 5, addendum §C). Absent until then | `room.override` (addendum §C) | none from the daemon; `heater.js` reads the retained topic |
| Devices | 683 device rows. Switch rows get the round toggle. Heater rows show state with **no toggle**, captioned "set by heater script". Sensor rows show pills | `DeviceToView` per room device (memory) | as Home today |
| Vacuum areas | Mapped, non-stale region names. A stale mapping shows "robot map changed". ~~A suggestion shows as a hint~~ **Superseded:** read-only; with none, "No robot area assigned (`myhome ctl vacuum rooms assign`)" | `vacuum.list` + `vacuum.rooms` (memory + SQLite) | none (the robot pushes) |
| Clean button | See §5 | same | the command itself |

**`myhome.HeatingTarget`** is a pure function in `internal/myhome`, next to the existing
`VacuumPhaseText` helper:
`HeatingTarget(levels map[string]float64, ranges []TemperatureTimeRange, occupied *bool, now time.Time) (level string, celsius float64, until *time.Time, ok bool)`.
It mirrors `heater.js:1153-1162` and the midnight-crossing range test
(`heater.js:821-844`, `rooms.isInTimeRange`). A missing level gives `ok=false`, so the widget
renders `--`. It lives in `internal/myhome` because the UI module must not import `myhome/rooms`,
and because `ctl` can reuse it later. (Addendum: once PR 5 lands it is fed the *effective* ranges,
override included.)

**Freshness.** The 682 relay applies. On `device-update` or `sensor-update` for a device id listed
in the open view's `data-devices` attribute, the shell triggers `refresh` on that view. Bursts are
coalesced into one GET per 2 s (a JS constant). Hidden views are marked dirty (683 §2). The vacuum
block alone re-polls at 5 s, and only while the room page is shown
(`every 5s [kiosk.visible('rooms') && kiosk.roomOpen]`). That is the P8 pattern: it reads cache and
never reaches the robot. A future `vacuum-update` SSE replaces it.

**Routes** (Go 1.22 method patterns; registered on the mux, or they fall into the device proxy):

| Route | Renders |
|---|---|
| `GET /htmx/rooms` | the room list (#744's; URL unchanged) |
| `GET /htmx/room/{id}` | room page; 404 fragment "Room not found" for an unknown id |
| ~~`POST /htmx/room/{id}/level`~~ **Superseded:** `POST /htmx/room/{id}/override` (`level=comfort|eco|clear`), PR 5 | climate block |
| ~~`GET`/`POST /htmx/room/{id}/areas`~~ **Superseded: dropped** (no touch editor) | |
| `POST /htmx/vacuum/action` with `command=clean-rooms&room_id=…` | room vacuum block (§5) |

### 4. Creating and editing a mapping (1024x600)

> **Superseded (2026-11-08): not built in this issue** (answer 7). The mapping is edited with
> `myhome ctl vacuum rooms assign|unassign` over the `vacuum.roomassign` RPC (addendum §A). The
> sheet below remains the design for a later touch editor.

- **Entry points.** The room page's vacuum block has [Edit] next to "Areas". With no mapping it
  shows "No robot area assigned" and an [Assign areas] button. Both open the **areas sheet**: an
  overlay over the content area with a 48 px close (x) in its corner and no backdrop dismiss
  (683 §3).
- **Sheet content.** One 56 px chip row per non-stale region of the robot, sorted: suggested for
  this room first, then unmapped, then mapped elsewhere. Each chip shows the region name (or
  "Area <id>"), a "Suggested" pill if applicable, and a subtitle "in <other room>" when it is mapped
  elsewhere. A check glyph marks the regions mapped to this room. Stale regions that are still
  mapped to this room appear at the bottom, greyed, with an [Unassign] action.
- **Interaction.** A tap toggles the region. Assigning a region that is mapped elsewhere moves it,
  and the subtitle says so before the tap. Each tap is one POST → the mapping verb → the sheet
  re-renders (`hx-disabled-elt="this"`). No confirm, no Save button.
- **No robot, or no regions.** The sheet says "The robot has no named areas yet. Run a room sync
  from the Vacuum section, or start one room clean from the robot's app."
- **Desktop and phone.** The same sheet works by mouse. On a phone it becomes full width.

### 5. "Clean this room"

> **Amended (2026-11-08):** confirmation is **on** for the room page (answer 3) and uses the vacuum
> card's existing in-place two-step confirm, not the 683 shared dialog, which is not on the campaign
> branch yet. **The Vacuum section's confirm scope is settled** (owner decision, 2026-11-08): it is
> #685 as written. In the Vacuum section only **Clean all** and **Stop** ask for confirmation, and
> nothing else does, Dock included. That is what the vacuum card already does (verified:
> `htmx_vacuum.go` confirms `start` and `stop` only), so this issue does not change the Vacuum
> section. See addendum §D, PR 4.

**States of the vacuum block** (rendered server-side from `vacuum.list` + `vacuum.rooms`):

| Condition | Block shows |
|---|---|
| Robot list empty (integration off) | block omitted |
| No mapped, non-stale region for this room | "No robot area assigned" ~~+ [Assign areas] (+ suggestion hint)~~ + the CLI hint |
| Mapped regions span several maps | button disabled, "Areas are on different robot maps" |
| Robot not connected | button disabled, "Robot offline" (plus "last seen at <host>" as today) |
| Robot running or paused | button disabled, "Robot busy: cleaning <targets>" (the existing `targetNames`) |
| Otherwise | **[Clean this room]**, primary, full block width, 56 px |

**Flow.**

1. Tap. ~~If the policy says confirm, the 683 dialog shows~~ **Amended:** the block switches to its
   in-place confirm, "Clean <room> now? The robot leaves its dock." with [Cancel] and [Clean]. It
   cancels itself after 20 s, as the vacuum card's start/stop confirm does
   (`htmx_vacuum.go`, `ask`/`open`/`close`, verified).
2. `POST /htmx/vacuum/action` with `robot`, `command=clean-rooms` and `room_id`. **When `room_id` is
   present, the handler ignores any `room` fields.** It resolves the regions on the server, at that
   moment, as those with `RoomID == room_id`, not stale, and on one map, re-checks that the robot is
   connected and idle (as `vacuumStillApplies` does for start/stop), then calls `vacuum.clean` with
   their ids. The response is the room's vacuum block, not the vacuum panel. Without `room_id` the
   handler keeps its current behaviour.
3. While the request is open (up to ~30 s: abort watch plus one retry, verified in `clean.go`),
   the button shows "Starting…" and is disabled (`hx-disabled-elt`).
4. Result in the block: `running` → "Cleaning <room>". `no-report` → "Sent; the robot has not
   confirmed yet". An error → the error text, the existing `actionErr` pattern, including
   `VacuumErrorHint`.

~~**Confirm policy as a single line** (`vacuumConfirm` map shared by the Vacuum section and the room
page, `confirmFor` template func; "owner decision pending, #685 vs 683 §8.5").~~ **Superseded:** the
decision is made (owner, 2026-11-08), so no policy table is needed. The confirmed actions are fixed:
**Clean all** and **Stop** in the Vacuum section (#685 as written; nothing else there, Dock
included), and **Clean this room** on the room page (answer 3). The room page's confirm is
unconditional in its template, and the Vacuum section is not touched. When the shared 683 dialog
lands, it replaces the in-place confirms in one change, for exactly those three actions.

### 6. Degraded modes

| Situation | Room page behaviour | Devices |
|---|---|---|
| **Daemon down** | 683 banner. The room list and the last-open room page stay as greyed DOM, with actions inert. Opening a room never opened before shows "Not available while Penates (My Home Daemon) is offline" in the sub-view, without a request | `heater.js` keeps regulating from retained ranges and occupancy (daemon-optional rule). An active override stays in the retained ranges (addendum §C) |
| **Robot offline** | Clean is disabled with "Robot offline". The mapping stays viewable (SQLite). Learned regions cannot be judged stale without robot state, so they are listed, and `vacuum.clean` would refuse anyway ("robot does not report map") | n/a |
| **Internet down** | No effect on the page. The start-time cloud sync fails and the stored regions and mappings are kept (verified `sync.go`). Room clean is local | n/a |
| **Broker down, daemon up** | Values freeze; the temperature age (answer 5) shows them ageing past 30 min | devices unaffected |
| **Rooms service disabled** | The Rooms section shows its existing "not enabled" state. `vacuum.roomassign` refuses room assignments | n/a |
| **Occupancy service disabled** | The target shows the away level with "(no presence data)", matching `heater.js` | n/a |
| **A mapped room deleted** | Its regions read as unmapped | n/a |

## Alternatives considered

**A. Mapping table in the rooms package, `room_vacuum_regions(room_id, blid, pmap_id, region_id)`.**
Strongest form: the room owns "what belongs to me", deleting a room can cascade, and a region can
belong to two rooms (open-plan). **It loses on smallness and on the robot's own lifecycle.** The
vacuum service decides when a region is stale, renamed or replaced (`syncCloud`, `learnRegion`).
A second table keyed on that region must track the same lifecycle from outside. `myhome/rooms`
would also learn the robot's identifiers. The column moves with the region for free. If
open-plan regions turn out to matter (answer 4: not now), the table is the upgrade path, with a
one-off copy from the column.

**B. `room.delete` clears the mapping.** Strongest form: no dangling ids. **It loses on coupling:**
the rooms service would call into vacuum on every delete, for a case that the readers already
handle by treating unknown ids as unmapped.

**C. A daemon `room.status` RPC aggregating temperature, target, heater and vacuum.** Strongest
form: one call per room, reusable by `ctl` and a phone UI, and the target logic lives behind the
service boundary. **It loses for now:** the data sits in three services (device manager, rooms,
vacuum). `myhome/rooms` deliberately does not import devices (`setup_rpc.go` comment). So the RPC
would have to live in the daemon composition root, or call three verbs itself, which is what the
UI handler does at no extra cost. It is the natural next step once a second consumer exists.

**D. Heater control as a relay toggle.** Strongest form: simplest, and it is what a "heater button"
suggests. **It loses on honesty:** `heater.js` calls `Switch.Set` on every cycle, so the toggle is
reverted silently within `poll_interval_ms`. (Still rejected by the addendum: the owner's toggle is
a *level* toggle, eco/comfort, which acts through the ranges the script reads; addendum §C.)

**E. Auto-apply name matches in `vacuum.roomsync`.** Strongest form: zero taps on a fresh setup.
**It loses on safety and ordering:** see §2.

Alternatives for the addendum's new decisions are in addendum §A-§C.

## Consequences

- **Better:** one screen per room. The vacuum mapping survives cloud renames, robot echoes and
  binary downgrades. The nightly updater can install an older release (`CLAUDE.md`); older code
  selects explicit columns and ignores `room_id`, and the next upgrade finds the mapping intact.
  The mislabelling of every switch as "heater" goes. Cleaning resolves regions on the server
  at tap time. (Addendum) A room's thermometer is never ambiguous, so `room.setup` stops silently
  wiring the first of several sensors into a heater.
- **Worse:** the heating-target rule now exists twice, in `heater.js` and `myhome.HeatingTarget`,
  and can drift. Mitigation: a table test whose cases are copied from the script's branches, a
  comment in both places naming the other, and the label "target", not "the heater will…". The
  room page's devices inherit the P6 polarity read until 682 follow-up E lands. (Addendum) The
  ranges the heater receives are no longer the pure schedule while an override is active, so
  "what the schedule says" and "what the heater gets" can differ for a few hours; the page shows
  which.
- **Harder to change later:** `vacuum_regions.room_id` and `VacuumRegion.room_id` become a contract
  (many-to-one). Moving to the join table (A) is a migration plus a JSON shape change. (Addendum)
  The one-sensor rule becomes a contract of `device.setroom` and `device.update`; relaxing it later
  (a primary-sensor column) is easy, tightening it again would not be.

## Cross-cutting concerns

- **Security:** unchanged LAN trust model (reference §13). Any LAN client can already call
  `vacuum.clean` and `device.setroom` through `/rpc`. The new verbs are no more powerful. No secret
  is involved; the robot password stays in `.env`.
- **Privacy:** not affected. Room names and robot area names are already stored. (The device names
  that appear in the one-sensor error text are already shown in the UI.)
- **Observability:** `vacuum.roomassign` logs at Info (robot, map, region, room, previous room). A
  refused room assignment logs at Info with both device ids. `room.override` logs set, clear and
  expiry at Info. The clean path already logs every attempt. Room page renders log at V(1).
- **Failure modes:** see §6 and addendum §B/§C. Failed fragment loads use the existing "couldn't
  refresh" pill.
- **Rollout and migration:** additive columns (`vacuum_regions.room_id`, and for PR 5
  `rooms.override_level`, `rooms.override_until`) are applied at daemon start; no backfill. Each PR
  can be reverted alone. Reverting PR 1 or PR 5 leaves unused columns, which is harmless; reverting
  PR 5 while an override is active leaves the last published ranges until the next publish, which
  every daemon start does.

## Impact on existing conventions

- **Extends** 683's sub-view pattern with its first real use (the room page).
- **Bends** the "UI goes through RPC" rule only where #224 already did: the room page keeps reading
  `room_agenda_cache` through `sqlDB`. Nothing new reads storage directly.
- **Adds** a business-rule helper (`HeatingTarget`) in `internal/myhome`, next to existing
  presentation helpers. It is a pure function, not a service. (Addendum) Also adds
  `myhome.IsTemperatureSensor`, which replaces two diverging classifiers.
- ~~**Changes** the tile markup of `/htmx/rooms`.~~ **Superseded:** `/htmx/rooms` is #744's.
- **Removes** `deviceRoleLabel`'s "everything else is a heater" heuristic, replaced by
  `HasHeaterScript` plus capability checks (if #744 has not already done so).
- (Addendum) **Adds a validation** to `device.setroom` and `device.update`, which until now accepted
  any room id. And **changes `room.setup`**, which picked the first of several sensors, into a
  refusal.

## PR decomposition

> **Superseded (2026-11-08):** replaced by the five PRs of addendum §D. The original list below is
> kept for reference only; PRs 3 (stepper) and 4 (areas sheet and suggestions) are not built.

1. ~~Region → room mapping (daemon only).~~ → addendum PR 1 (adds the CLI).
2. ~~Room tiles and read-only room page.~~ → addendum PR 3 (no tiles).
3. ~~Heater level stepper.~~ → dropped (answer 2); addendum PR 5 is the toggle.
4. ~~Areas sheet and suggestions.~~ → dropped (answer 7).
5. ~~Clean this room.~~ → addendum PR 4.

## Confirmation

- Go tests named in each PR, run by `make test`. The PR 3 fake device caller makes "no added device
  RPC on render" a regression test.
- Review questions: "Does any new handler call a device or `heater.getconfig`?", "Is the clean
  target resolved on the server from `room_id`?", "Does an unknown value render `--`?", "Does
  anything open by default?", "Does any vacuum action other than Clean all, Stop and Clean this
  room ask for confirmation?", and (addendum) "Is there any path that writes `devices.room_id`
  without going through the device manager's room check?", "Does any code still pick
  `tempSensors[0]`?", "Does the toggle write anything but the override columns?".
- The 683 Chromium protocol (1024x600 touch, tap-target audit, zero cross-origin requests) on the
  room page.

## Scope questions for the owner

> **Answered 2026-11-08** (binding; the answers are restated in the addendum).

1. **Which sensor is "the room temperature" when there are several?** → **Answered:** none; a
   second sensor is a configuration error, rejected at assignment time. No averaging.
2. **What is "heater control"?** → **Answered:** a toggle between eco (off) and comfort (on). Not a
   stepper.
3. **Vacuum confirm scope** → **Answered:** confirm `clean-room` on the room page (answer 3). For the
   Vacuum section the owner decided later the same day: **#685 as written**, so only Clean all and
   Stop ask for confirmation, and nothing else does, Dock included. That is the current behaviour.
4. **Can one robot area belong to two rooms?** → **Answered:** not now.
5. **Show the age of temperatures?** → **Answered:** yes, past 30 min.
6. **A CLI for the mapping?** → **Answered:** yes, with an RPC verb.
7. (Asked in review) **The smaller alternative** (mapping + read-only page + clean this room) →
   **Answered:** OK, provided the CLI exists.

## Open questions

1. **Region id collisions across maps.** `vacuum.clean` matches regions by name *or* id across all
   maps. It prefers the non-stale match and errors on ambiguity (verified, `findRegion`). The page
   sends ids. If the robot ever holds two live maps with overlapping ids, `vacuum.clean` would need
   an optional `map_id`. Has a second map ever existed on this robot? (Still open.)
2. **Gen2 thermometers.** Are there rooms whose only thermometer is a Gen2 device? If so, their
   temperature shows `--` until the device cache learns Gen2 temperature components. (Still open.)
3. ~~**Stepper bounds.**~~ Obsolete (no stepper).

Further open questions from the addendum are in addendum §E.

---

## Addendum 2026-11-08: owner answers

### The answers (binding)

1. A room with several temperature sensors is a **configuration error, raised when the sensor is
   added to the room**: the assignment is rejected. Never pick one silently, never average.
2. Heater control is **not a stepper**. The controlled layer has two levels, eco (off) and comfort
   (on): a **toggle**.
3. Confirm `clean-room` on the room page: **yes**. For the wider vacuum confirm scope, the owner
   decided later the same day: **#685 as written**. In the Vacuum section only Clean all and Stop
   ask for confirmation, and nothing else does (Dock included). This matches the current vacuum
   card, so the Vacuum section is unchanged by this issue.
4. One robot area for two rooms: **not now**.
5. Show a temperature's age past 30 min: **yes**.
6. A CLI for the robot-area → room mapping: **yes, with an API** (an RPC verb), since otherwise
   nothing can configure the mapping.
7. The smaller alternative (mapping + read-only room page + clean this room; no stepper, no
   name-suggestions sheet): **OK, provided the CLI exists**.

**Principal's interpretation** (an interpretation, not an owner answer): the toggle is designed now
but built **last**, as its own PR, after the three PRs of answer 7. Answer 1 also implies a fourth
PR the owner did not list by name (the assignment check), described in §B and flagged as scope
growth in §D.

### Where the architecture reference misled (2026-11-08)

On top of the list at the top of this document:

- It describes `thermometer.list`/`door.list` as classifying "by capability" as if there were one
  rule. There are **two classifiers that disagree**: `classifyThermometers`
  (`myhome/devices/impl/manager.go`, also accepts `Info.Application == "shellyht"`) and
  `classifyDevice` (`myhome/devices/impl/setup.go`, used by `room.setup`, returns the **first**
  matching BTHome capability, so a BLU device listing `window` before `temperature` is a door
  sensor). Verified.
- It lists the setup modal as calling `device.setroom` but not that **the modal ignores the
  result** (`myhome.js:639-647`, verified), so a refusal would be invisible without a change.
- It does not mention `ctl db import` (#738), which writes `devices.room_id` through
  `device.update`'s upsert (`room_id = excluded.room_id`, `myhome/storage/db.go`), nor the
  `vacuum.export`/`vacuum.import` verbs, which **already anticipate** `vacuum_regions.room_id`
  (`myhome/vacuum/dump.go` and `internal/myhome/vacuum.go` comments, verified).
- It says the ranges topic is published "at startup and on room/schedule change". That is exact,
  and it hides a consequence worth stating: **nothing republishes at midnight**, so the day type
  (work day / day off) the heaters use is the one computed at the last daemon start or edit, up to
  2 days old (verified: the only callers of `PublishRangesUpdate` are startup, `room.edit` and
  kind-schedule changes). This matters for §C and is open question E4.

### §A. Mapping: RPC verb, persistence, validation, CLI (answers 4, 6)

**Persistence** is §1's column, unchanged: `vacuum_regions.room_id TEXT NOT NULL DEFAULT ''`,
appended to `regionColumns`. Verified facts that make it cheap:

- `syncCloud` and `learnRegion` list their `SET` columns, so neither touches `room_id`.
- `Storage.Export` reads `SELECT *` and `Storage.Import` writes whichever keys match this build's
  columns, so `ctl db export|import` carries the mapping with **no change**, and a dump from an older
  build imports with every region unmapped.

**Cardinality (answer 4).** One area → at most one room is guaranteed by the single column: there
is no way to store a second room. Many areas → one room stays allowed. Assigning an area that is
already in room X to room Y **moves** it; the result reports the previous room so the CLI can say
so (open question E8 asks whether a move should need `unassign` first).

**Verb name: `vacuum.roomassign`**, replacing §1's `vacuum.roommap`. Reason: in this API "map"
already means the robot's persistent map (`map_id`, `VacuumMap`, `pmap_id`), and `--map` is a flag
of the new command; `roomassign` follows the existing `vacuum.roomname` / `vacuum.roomsync`
pattern. Nothing was built under the old name.

**The four registration steps** (`CLAUDE.md` "RPC System"):

1. `internal/myhome/const.go`: `VacuumRoomAssign Verb = "vacuum.roomassign"`.
2. `internal/myhome/vacuum.go`:
   - `VacuumRoomAssignParams{Robot string; MapID string (optional); RegionID string; RoomID string}`,
     JSON `robot,omitempty`, `map_id,omitempty`, `region_id`, `room_id`. `RoomID == ""` unassigns.
   - `VacuumRoomAssignResult{MapID, RegionID, RoomID, Previous string}`.
   - `VacuumRegion` gains `RoomID string \`json:"room_id,omitempty"\`` (additive).
3. `internal/myhome/methods.go`: a `signatures` entry with those two types.
4. `myhome/vacuum/rpc.go`: `RegisterMethodHandler(myhome.VacuumRoomAssign, ...)` inside the existing
   `RegisterHandlers`, calling `Service.AssignRoom(sel, mapID, regionID, roomID)`. No new MQTT
   subscription.

**`Service.AssignRoom`** (business logic, `myhome/vacuum`):

1. `pick(sel)` the robot, as every other verb does.
2. Resolve the region among the robot's stored regions: with `MapID`, the exact
   `(map, region)`; without, every region with that id, and if several, the single non-stale one
   (the `findRegion` rule). Otherwise: `region <id> exists on <n> robot maps; pass --map <map-id>`
   or `no region <id> for robot <name>`.
3. If `RoomID != ""`, check the room exists through an in-process call of `temperature.get`
   (an RPC boundary, not an import of `myhome/rooms`). Unregistered verb: `rooms are not enabled on
   this daemon`. Unknown room: `unknown room "<id>"`.
4. `Storage.assignRegion(blid, mapID, regionID, roomID) (previous string, err error)`: one
   transaction, `SELECT room_id` then `UPDATE`.
5. A stale region may be assigned (the owner may be repairing a mapping); cleaning one stays
   refused by `vacuum.clean`.

`Service.regions()` copies `room_id` into `VacuumRegion`; a `room_id` that names a deleted room is
returned as stored, and readers treat it as unmapped (§1).

**CLI (CLI only, `myhome/ctl/vacuum/vacuum.go`):**

- `myhome ctl vacuum rooms assign <region-id> <room-id> [--map <map-id>]` prints
  `region 12 is now in room cuisine` or `region 12 moved from salon to cuisine`.
- `myhome ctl vacuum rooms unassign <region-id> [--map <map-id>]`.
- `myhome ctl vacuum rooms` gains a `ROOM` column (`-` when unassigned).

No region resolution or room check happens in the CLI: the daemon does both (three-tier rule).

*Alternative considered:* a `--room` flag on the existing `vacuum rooms name`. Rejected: naming is
refused for cloud regions when the iRobot account is configured, assignment must not be, and one
verb with two unrelated effects is harder to test and to log.

### §B. One temperature sensor per room (answer 1)

**What "temperature sensor" means in code.** A device the daemon already treats as a room
thermometer: a Gen1 H&T (`Info.Application == "shellyht"` or id prefix `shellyht-`) or a BLU
device whose stored BTHome capabilities include `temperature`. A heater relay and Gen2 temperature
components do not count (consistent with the non-goal above). Today the rule exists twice and the
two copies disagree (see "Where the reference misled"). The design adds **one exported predicate**,
`myhome.IsTemperatureSensor(d *Device) bool` in `internal/myhome/device.go`, and makes both
`classifyThermometers` and `classifyDevice` (for its temperature role) call it, so
`thermometer.list`, `room.setup` and the new check can never disagree.

**Every path that gives a device a room** (verified unless marked):

| # | Path | Writes `devices.room_id` via | Callers |
|---|---|---|---|
| 1 | `device.setroom` | `Cache.SetDeviceRoom` → `UPDATE devices SET room_id ... WHERE id = $2 OR mac = $2 OR name = $2 OR host = $2` | `ctl room set`/`clear`, `ctl heater setup --room-id`, the web UI setup modal, any `/rpc` client, and (expected) #744 |
| 2 | `device.update` | `Cache.SetDevice` → upsert with `room_id = excluded.room_id` | `ctl db import` (#738, `ImportDatabase`), any `/rpc` client |
| 3 | Discovery (Gen1, BLU listeners, `storeDeviceLoop`) | `SetDevice` | New devices are created with an empty room; known devices are re-saved from the cache record, room unchanged. **Never assigns a room** (verified for Gen1 and BLU; the Gen2 refresh path *inferred* from it passing cache records) |
| 4 | `room.setup` | does not write `room_id`; writes heater KVS | **Silently picks `tempSensors[0]`** as the heater's internal-temperature topic when a room has several (`setup.go:105-108`) |

`ctl heater setup` and `room.setup` also write the device KVS key `room-id`, which `heater.js` uses
to pick its ranges topic. That is the heater's own room, not a sensor assignment, and is unaffected.

**Where the check lives.** In the device manager (`myhome/devices/impl`), as one method used by the
two RPC handlers that can change a room, paths 1 and 2:

- `checkRoomAssignment(ctx, d *Device, roomID string) error` returns nil when `roomID == ""`, when
  `d` is not a temperature sensor, or when `d` is already in `roomID`. Otherwise it scans the cache
  (`GetAllDevices`, memory) for another temperature sensor with that room and a different id.
- `device.setroom` first resolves the identifier with `GetDeviceByAny`, runs the check, and only
  then writes (DB, then best-effort KVS as today). An identifier that resolves to no device keeps
  today's behaviour. *Inferred risk noted in passing:* the storage `UPDATE` matches id **or** MAC
  **or** name **or** host, so an identifier shared by two records updates both; the implementer
  should update by the resolved id.
- `device.update` compares the incoming `RoomId` with the cached record's and runs the check only
  when the room changes to a non-empty value, so discovery-style re-saves of an existing legacy
  conflict are not refused.

**Error text** (one string, returned by both verbs, surfaced verbatim by every caller):

> `configuration error: room "<room-id>" already has a temperature sensor, <name> (<id>); a room can have only one. Remove it first: myhome ctl room clear <id>`

**Why not in the cache or storage**, which every write passes through: the cache is also written by
the discovery listeners on their goroutines, a business rule there couples a data structure to the
device classification, and the only paths that can *change* a room are the two RPC handlers. A
future path that sets a room must go through `device.setroom`; that is a review question
(Confirmation) and a requirement on #744.

**Callers that must surface the refusal:**

- `ctl room set` and `ctl heater setup` already print the RPC error (verified). `ctl heater setup`
  then carries on with the KVS writes, as it does today for any `device.setroom` failure; that is
  unchanged.
- The web UI setup modal must check the `device.setroom` response (`/rpc` answers 500 with the
  error text, verified in `ui/rpc.go`), show it, and stop before `device.setup`. A few lines of
  `myhome.js`.
- `room.setup`: a room with two or more temperature sensors is refused as a whole, with the same
  "configuration error" wording in each heater's `Error` field and no KVS written for that room. No
  more `tempSensors[0]`.

**Rooms that already hold several sensors** (legacy data, or written before the rule):

- *Measured:* the main daemon's export available on 2026-11-08 holds 13 rooms and 4 devices the
  predicate classifies as temperature sensors (all Gen1 H&T), and **no device has a room
  assignment** in `devices.room_id`. So **0 rooms have more than one sensor today**. (*Inferred:*
  heaters are bound to rooms only through their KVS `room-id`.) The case is therefore defensive,
  and a strict import costs nothing today.
- *Caveat (inferred):* the 12 BLU devices in that export store only `battery` as a BTHome
  capability, so a BLU thermometer may not be recognised by the predicate at all. That is a
  limitation of the stored capabilities, not of this design; open question E7.
- *Room page:* no headline temperature; the warning "Configuration error: N temperature sensors in
  this room; keep one" in the climate block; each sensor still shows its own value in its device
  row. Target and heater state are shown normally. Nothing is fixed automatically.
- *`room.setup`:* refused for that room, as above.
- *`ctl db import`:* **rejects before writing anything.** `ImportDatabase`
  (`internal/myhome/dbdump.go`) runs a pure pre-check over the file's devices with the same
  predicate; any room with two or more temperature sensors aborts the import with one line per
  conflict and a non-zero exit. A conflict with an assignment already on the *target* daemon is
  caught by the daemon's check, and that device is counted as failed, which is the import's
  existing contract ("a partial import must not look like a complete one"). Open question E3 offers
  the softer alternative.

*Alternatives considered:* (1) a per-room "primary thermometer" column, which allows several
sensors and picks one explicitly: strongest form, no data is ever refused. It loses because the
owner's answer is a rule, not a preference, and it adds a column and a UI to choose. (2) Check only
in the UI: loses, because the CLI, `/rpc` and import are the paths most likely to create the
conflict.

### §C. The heater toggle (answer 2), built last

**What exists (verified).** `heater.js` picks its target as *away* if not occupied, *comfort* if the
current minute is inside one of the `ranges` of the retained message
`myhome/rooms/<id>/temperature/ranges`, *eco* otherwise. The `levels` map (`eco`, `comfort`, `away`
in °C) comes from the same message and is edited through `room.edit`. The script ignores the
message's `date`. The relay then goes on only if the filtered temperature is below the target **and**
it is a cheap hour or preheat applies, or below the eco floor while occupied, or below the away
level (frost).

**Is a toggle coherent with that model?** Yes, cheaply, **if "comfort" means "the comfort level is
selected"** rather than "the relay is on". The toggle changes which level applies by changing the
`ranges` the daemon publishes; it does not touch `levels`, the kind schedules, or `heater.js`.
Consequence to state plainly in the UI and to the owner: selecting comfort outside cheap hours may
not switch the heater on until a cheap hour or preheat (open question E1). A toggle that forces the
relay on now would need a new input to `heater.js`, a heap measurement on the device, and is **not
buildable cheaply**.

**Semantics: a temporary override, not a schedule change.**

- The toggle shows the level that applies now (from `HeatingTarget`). Tap when the schedule says
  eco → **comfort until the next scheduled transition** (the start of the next comfort range today,
  or midnight). Tap when it says comfort → **eco until the end of the current comfort range**.
- Tap while an override is active → **cancel it** (back to the schedule). That is the undo. A third
  state is never shown.
- When nobody is home, `heater.js` uses *away* whatever the ranges say, so the toggle is disabled
  with "Nobody home: away level applies" (open question E5).
- The override ends by itself at its `until`. It never outlives the day it was set on while the
  daemon is up.

**What it writes.**

- `rooms` gains `override_level TEXT NOT NULL DEFAULT ''` (`''`, `comfort` or `eco`) and
  `override_until TIMESTAMP`, by the existing `COUNT(*)` column migration. Persisted, because the
  daemon restarts every 2 days by design and an in-memory override would vanish silently at a
  restart.
- One pure function in `myhome/rooms`, `effectiveRanges(scheduled []TimeRange, override, now)`:
  comfort adds `[now, until)`, eco cuts `[now, until)` out of the current range. Midnight is
  encoded as `end = 0`, which `heater.js`'s midnight-crossing test already treats as "until
  midnight" (verified `heater.js:832-835`).
- `PublishRangesUpdate` publishes the effective ranges. `temperature.getschedule` gains two additive
  fields, `override` (`{level, until}`, omitted when none) and `effective_ranges`, so the page and
  `HeatingTarget` show "comfort (manual) until 22:00".
- One timer per active override (`time.AfterFunc`) clears it and republishes at `until`. At startup
  expired overrides are cleared before `publishAllRanges`.

**RPC** (same four steps): `RoomOverride Verb = "room.override"`;
`RoomOverrideParams{RoomID string; Level string}` (`comfort`, `eco`, or `""` to cancel);
`RoomOverrideResult{RoomID, Level string; Until *time.Time}`; handler in `myhome/rooms`'
`RegisterHandlers`. The daemon computes `until`; the client never sends a time. A read-only side
instance (`NewDataService`, no MQTT client) stores the override and publishes nothing, as for every
other room change. A `ctl room override <room> comfort|eco|clear` command is cheap and makes the
PR testable without the UI (open question E6).

**Daemon-down degraded mode** (`CLAUDE.md` "daemon-optional per device"). Each heater keeps
regulating from the last retained ranges and occupancy, as today. If the daemon dies with an
override active, the override stays in the retained ranges, and because the ranges are minutes of a
day with no date, it **repeats every day** at the same times until the daemon is back and
republishes. That is the same staleness the whole schedule already has with the daemon down (and
partly with it up, see E4); the toggle adds no new dependency of a device on the daemon. The toggle
cannot be used while the daemon is down (the page is offline).

*Alternatives considered:* (1) Edit `levels` (swap eco and comfort values): persistent, invisible to
the schedule, and reverts nothing; rejected, it is the stepper in disguise. (2) A new retained
`override` topic read by `heater.js`: cleanest semantics, but a script change on devices with
~7 KB of free heap, a sixth subscription on a script already using several, and a heap measurement
first; rejected for this issue. (3) Relay toggle: reverted by the script (Alternative D).

### §D. Revised PR decomposition

Across all PRs, these must not change: `heater.js` and every device script, the retained ranges
topic's **format**, `vacuum.clean` semantics, existing JSON fields of `vacuum.*`, `temperature.*`
and `device.*` results, the fragment URLs `/htmx/rooms` and `/htmx/vacuum*`, the Vacuum section's
confirm scope (Clean all and Stop only, #685 as written), and SSE event names and payloads. Each PR
merges into `campaign/681-kiosk-ui` on its own and is verified with `make test` run in the
background. Sizes are estimates of changed lines including tests.

**Scope growth beyond the brief of #684, to go back to the owner before build:** PR 2 as a whole
(not in answer 7's three PRs; implied by answer 1; it changes `myhome/devices`, `internal/myhome`
and `ctl db import`, outside the UI); in PR 3, the sensor update time in `myhome/devices` (answer 5);
in PR 5, two `rooms` columns, a new verb and an expiry timer in `myhome/rooms`. No PR adds a Go
module, a dependency, a config option or a new subsystem.

| PR | Content | Size | Depends on |
|---|---|---|---|
| 1 | Mapping: column, `vacuum.roomassign`, `ctl vacuum rooms assign|unassign`, `ROOM` column, `docs/vacuum.md` | S-M, ~300 | none |
| 2 | One temperature sensor per room: predicate, check in `device.setroom`/`device.update`, `room.setup` refusal, import pre-check, setup modal error | M, ~300-350 | none |
| 3 | Read-only room page, `HeatingTarget`, temperature age | L, ~650-800 (splittable) | #744 (entry point), 683 PR 4 if landed; PR 2 for the predicate |
| 4 | Clean this room, with in-place confirm | S-M, ~250 | PR 1, PR 3 |
| 5 | Eco/comfort toggle (last) | M, ~400 | PR 3 |

**PR 1. Mapping and CLI.** Files: `myhome/vacuum/storage.go`, `rpc.go`, tests (`service_test.go`,
`dump_test.go`); `internal/myhome/const.go`, `vacuum.go`, `methods.go`; `myhome/ctl/vacuum/vacuum.go`,
`vacuum_test.go`; `docs/vacuum.md`. *Done looks like:* on a scratch DB, `ctl vacuum rooms assign`
sets, moves (reporting the previous room) and unassigns; `ctl vacuum rooms` shows the room; an
ambiguous id asks for `--map`; an unknown room is refused when a fake `temperature.get` is
registered and "rooms are not enabled" when none is. *Tests:* an old-schema DB gains `room_id` and
keeps its rows; `syncCloud` (rename, removal) and `learnRegion` preserve `room_id`; export then
import round-trips it; the methods map is saved and restored in `t.Cleanup`, no `t.Parallel()`.
*Without hardware:* a scratch instance with dummy `MYHOME_VACUUM_*` values and a few rows inserted
into `vacuum_regions` (*inferred* to work: `pick` needs only a configured robot, not a connected one).

**PR 2. One temperature sensor per room.** Files: `internal/myhome/device.go` (predicate) and a
test; `myhome/devices/impl/manager.go` (check, both handlers, `classifyThermometers`), `setup.go`
(`classifyDevice`, refusal), `manager_test.go`; `internal/myhome/dbdump.go` (pre-check) and
`dbdump_test.go`; `internal/myhome/ui/static/myhome.js` (setup modal). *Done looks like:* assigning
a second H&T to a room fails from `ctl room set` and from the web modal with the §B text, and
nothing is written; reassigning the same sensor to its own room succeeds; `room.setup` on a seeded
two-sensor room returns the error and writes no KVS; `ctl db import` of a file with a two-sensor
room exits non-zero before any write. *Tests:* predicate table (Gen1 by application, Gen1 by
prefix, BLU with and without `temperature`, BLU with `window` first); check table (empty room,
non-sensor device, same room, conflict); `SetupRoom` with a fake device lookup; import pre-check.
No hardware.

**PR 3. Read-only room page and temperature age.** Files: `internal/myhome/ui/htmx_room.go` (new:
handler and template), `server.go` (route), `static/index.html` (`#room-view`), `myhome.js`
(`openRoom`, Back, SSE refresh with 2 s coalescing), `myhome.css` (grid, breakpoint),
`internal/myhome/heating.go` and test (`HeatingTarget`), `myhome/devices/cache.go` (record the
update time in `UpdateSensorValue`, in memory, plus an additive `omitempty` field on the device
status), `htmx_room_test.go`, `server_test.go`. Removes `deviceRoleLabel`'s heuristic if #744 has
not. Can be split into 3a (`HeatingTarget` + age, daemon side) and 3b (page) if review prefers.
*Done looks like:* from #744's room list, a room opens its page with temperature (and age past
30 min), target, heater state, devices and mapped areas (read-only, with the CLI hint when none);
Back returns; a reload never reopens it. *Tests:* `HeatingTarget` table (occupied, unoccupied, nil
occupancy; inside and outside a range; midnight crossing; missing level → `--`); template tests for
zero sensors (`--`), one sensor fresh and 2 h old, two sensors (configuration error, no headline),
heater row with no toggle, vacuum block omitted with no robot; `GET /htmx/room/x` not proxied and
unknown id → not-found fragment; a render with a device caller that fails on any call passes with
the polarity cache pre-filled (the P6 caveat, stated in the test). *Without hardware:* Chromium at
1024x600 against a daemon with the embedded broker and no devices, plus the 683 tap-target audit.

**PR 4. Clean this room.** Files: `internal/myhome/ui/htmx_vacuum.go` (`room_id` branch, server-side
resolution, still-applies re-check), `htmx_room.go` (vacuum block states, in-place confirm),
`htmx_vacuum_test.go`, `htmx_room_test.go`. *Done looks like:* the button asks, Cancel sends
nothing, Clean sends exactly this room's mapped non-stale region ids. *Tests:* with `room_id`, a
fake `vacuum.clean` receives exactly those ids and client `room` values are ignored; every state of
the §5 table renders; the confirm is present; an error renders with its hint; the Vacuum section's
markup is unchanged and still confirms only Clean all and Stop (golden substring test: Dock, Pause,
Resume, Find and Empty bin post directly). *Without hardware:* Chromium with a dummy, disconnected
robot (button disabled) and the network panel (Cancel sends no request). **A real clean on the
robot is left to the owner** and recorded on the issue.

**PR 5. Eco/comfort toggle.** Files: `myhome/rooms/storage.go` (columns), `temperature.go`
(`effectiveRanges`, publish, timers, startup clean-up), `methods.go` (handler), tests;
`internal/myhome/const.go`, `temperature.go`, `methods.go`; optionally `myhome/ctl/room` (E6);
`htmx_room.go` (toggle, `POST /htmx/room/{id}/override`). *Done looks like:* a tap during eco publishes
ranges with `[now, next start)` added and the page shows "comfort (manual) until …"; a second tap
publishes the plain schedule; at `until` the daemon republishes the plain schedule by itself; a
restart with an active override keeps it, with an expired one clears it. *Tests:* `effectiveRanges`
table (comfort in eco, eco in comfort, midnight, no next range, override expired); handler with a
`RecordingMockClient` asserting the retained payload; expiry with an injected clock. *Without
hardware:* subscribe to the embedded broker's ranges topic and watch it change on a tap and revert.
`heater.js` is not touched, so no emulator test changes.

Order: PR 1 and PR 2 in parallel → PR 3 → PR 4 → PR 5.

### §E. Open questions for the owner

1. **"Comfort (on)": level or relay?** The toggle selects the comfort *level*; the heater still
   heats only in cheap hours, preheat, or below the eco floor. *Recommendation:* accept the level
   semantics and label the toggle "Eco / Comfort", not "Off / On". Forcing the relay needs a
   `heater.js` change and a heap measurement: a separate issue.
2. **How long does an override last?** *Recommendation:* until the next scheduled transition
   (described above). Alternatives: a fixed 2 h, or until midnight.
3. **Import of a file with a two-sensor room: reject or warn?** *Recommendation:* reject the whole
   import before writing (as designed); 0 such rooms exist today. Alternative: import those devices
   without their room and print a warning, still exiting non-zero.
4. **Daily republish of the ranges.** Independent of this issue, nothing republishes the ranges at
   midnight, so the day type the heaters use can be up to 2 days old. *Recommendation:* file a
   separate issue; do not fold it into PR 5.
5. **Toggle when nobody is home.** *Recommendation:* disabled with "Nobody home: away level
   applies". Alternative: allowed, effective when someone comes home.
6. **A `ctl room override` command in PR 5?** *Recommendation:* yes (about 40 lines, and it makes the
   verb usable without the kiosk, consistent with answer 6).
7. **BLU thermometers.** The stored BTHome capabilities of the BLU devices list only `battery`, so a
   BLU thermometer may escape the one-sensor rule and `thermometer.list`. *Recommendation:* accept
   for now; open a separate issue if BLU thermometers are, or will be, assigned to rooms.
8. **Moving an area between rooms.** *Recommendation:* allow the move in one command and report the
   previous room. Alternative: refuse while assigned, requiring `unassign` first.
9. **Order of PR 2 and PR 3.** PR 3 needs the predicate from PR 2. *Recommendation:* land PR 2
   first; otherwise PR 3 carries a temporary copy of the predicate.

(The vacuum confirm scope is no longer open: #685 as written, decided 2026-11-08.)
