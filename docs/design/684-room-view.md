# Design: room view: everything about a room (#684)

Status: draft for owner review. Part of the kiosk campaign #681. Builds on PR #224 (rooms
refactor, merged), `683-kiosk-shell.md` (shell, touch rules, confirm dialog) and
`682-retained-state.md` (no device polling, daemon-side projection, SSE relay).

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
  plugs, sensors), and the vacuum block.
- G2. A **persistent robot-region → room mapping**, many regions to one room, editable by touch.
  Name matches are offered as suggestions.
- G3. **"Clean this room"**, using the mapped regions. The confirmation policy is one line to change.
- G4. **No device RPC added on any render path**. Every widget renders from daemon memory or SQLite
  (682 Rule 3).
- G5. **Testable without hardware.** Every PR is verifiable with fakes and template tests, and
  manually with a daemon that has no devices.

Non-goals

- **A temporary heating boost or override** ("comfort for 2 h"). This needs a new input to
  `heater.js` on devices with little heap. The control offered here changes the room's level
  persistently instead (Scope question 2).
- **Room temperature from Gen2 sensors**, or from the heater's configured internal topic. Only what
  already reaches `Sensors` is shown. Widening that is a separate device-cache change.
- **A daemon `room.status` RPC** for `ctl` and scripts. The page composes existing verbs. A second
  consumer would justify the RPC; this page alone does not.
- **Automatic mapping without a tap**, for example applied by `vacuum.roomsync` at start.
- **A region spanning several daemon rooms** (an open-plan area cleaned as one robot region and
  shown as two rooms). The mapping is many-to-one (Scope question 4).
- **Multi-robot UI.** The data model is per robot. The page shows one block per robot, but only one
  robot is supported today.
- **On-screen keyboard.** Room create and rename stay in the existing modal, which needs a keyboard
  (as in 683).

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
   be minutes old. The tap needs a confirmation if the owner's policy asks for one.
4. **Touch and size.** At 1024x600 (944 px content), the room page shows climate, vacuum and the
   first six device rows without scrolling. Every target is ≥ 48 px, and nothing opens by default.

## Fit

**This fits the existing architecture as-is**, with one small schema increment owned by an
existing service:

- **Mapping: one column, `vacuum_regions.room_id`.** It is added through the existing
  `regionColumns` migration loop, with one new verb, `vacuum.roommap`, and `room_id` added to the
  `VacuumRegion` result. No new table, module or dependency.
- **Room page: new htmx fragments in `internal/myhome/ui`.** They follow the "Add a UI section"
  recipe and 683's sub-view pattern (Back button, state in Alpine, no `pushState`).
- **Room temperature and target: computed at render time** from memory (device cache, occupancy)
  and SQLite (levels, ranges). This is the 682 projection idea at its smallest: no stored state.
  One pure helper in `internal/myhome` mirrors the `heater.js` target rule.

Nothing here adds a subsystem, a Go module, a JS dependency or a config option.

## Proposed design

### 1. Mapping schema and ownership

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

**RPC** (the four steps in `CLAUDE.md`):

1. `VacuumRoomMap Verb = "vacuum.roommap"` in `internal/myhome/const.go`.
2. In `internal/myhome/vacuum.go`: `VacuumRoomMapParams{Robot, MapID, RegionID, RoomID string}`
   (`RoomID` empty = unmap). Result: the existing `VacuumOKResult`. Add
   `RoomID string \`json:"room_id,omitempty"\`` to `VacuumRegion`. This is additive, and older
   clients ignore it.
3. A `signatures` entry in `methods.go`.
4. Register the handler in `myhome/vacuum/rpc.go`. `Service.MapRoom` validates that the region
   exists for that robot and map. When `RoomID` is not empty and `temperature.get` is registered,
   it checks that the room exists through an in-process `myhome.Methods` call: an RPC boundary, not
   an import of `myhome/rooms`. If `temperature.get` is not registered (rooms disabled), it refuses
   with "rooms are not enabled". Mapping a stale region is allowed, because the owner may be fixing
   the map. Cleaning one is not.

`Storage` gets one method,
`mapRegion(blid, mapID, regionID, roomID string) (int64, error)`
(`UPDATE vacuum_regions SET room_id=? WHERE blid=? AND pmap_id=? AND region_id=?`).
`regions()` selects `room_id`, and `Service.regions()` copies it into `VacuumRegion`.

### 2. Name-based suggestions (how `vacuum.roomsync` helps)

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

**Rooms section root (tiles).** `roomsListTemplate` becomes a grid of tiles: three columns at
944 px, each ≥ 112 px tall. A tile shows the name, the current temperature (large), a target pill
("→ 20 °C comfort"), and a heater glyph (heating / idle / none). The whole tile is one tap target
that opens the room page. The edit and delete emoji buttons leave the tile; Edit moves to the room
page header.

**Room page (sub-view).** It is swapped into a `#room-view` container inside `#sec-rooms`. Alpine
holds `openRoom` (an id or null). Back, a rail tap and a section switch set it to null (683 §3). It
is never open on load: a reload on `/#rooms` shows the tiles.

```
+--------------------------------------------------------------------------+
| [< Back]   Cuisine                                     [Edit]            |  56 px
+------------------------------------+-------------------------------------+
| CLIMATE                            | DEVICES                             |
|   21.4 °C          (40 px text)    | (O) Plafonnier              ( P )   |
|   2 sensors, avg                   | (O) Prise four              ( P )   |
|   Target 20 °C  comfort until 22:00| (H) Radiateur   Heating    no btn   |
|   Heater: heating                  | (T) H&T   [21.2 °C] [58 %]          |
|   Comfort level  [ - ] 20.0 [ + ]  | (D) Fenêtre [Closed]                |
+------------------------------------+   ... (main scrolls)                |
| VACUUM                             |                                     |
|   Areas: Cuisine        [Edit]     |                                     |
|   [   Clean this room   ]  56 px   |                                     |
+------------------------------------+-------------------------------------+
   ~400 px                              ~528 px
```

The layout is a CSS grid, `minmax(0,5fr) minmax(0,7fr)`, with an 8 px gap. Below 700 px wide it
collapses to one column (climate, vacuum, devices). That is the hook for the later phone layout:
no fixed heights, no hover, and no width assumption beyond the breakpoint. Only `main.mh-content`
scrolls (683 rule 4).

| Widget | Shown | Source | Device contact |
|---|---|---|---|
| Name, Edit | `rooms.name`. Edit dispatches the existing `edit-room` event (the modal) | `temperature.get` (SQLite) | none |
| Current temperature | One sensor: its value. Several: the arithmetic mean, labelled "N sensors, avg". None with a value: `--` | `DeviceRegistry.GetAllDevices`, filtered on `RoomId` and `Status.Sensors.Temperature != nil` (memory) | none |
| Target and level | "Target 20 °C · comfort until 22:00", "eco until 07:00", or "away (nobody home)". If the occupancy verb is absent: "away (no presence data)", which is what `heater.js` does | `temperature.getschedule` (levels, today's ranges; SQLite) + `occupancy.getstatus` (memory) → `myhome.HeatingTarget(...)` | none |
| Heater state | Per heater device: "Heating", "Idle" or `--` | Devices in the room with `HasHeaterScript`; switch 0 logical state from `DeviceToView` (switch projection, memory) | inherits P6 (polarity KVS read on a cache miss) |
| Heater control | A stepper on the **level that applies now**, ±0.5 °C, clamped to 5–30 °C. Hidden when there is no heater | `room.edit` with the full `Levels` map; the service republishes the retained ranges topic | none from the daemon; `heater.js` reads the retained topic |
| Devices | 683 device rows. Switch rows get the round toggle. Heater rows show state with **no toggle**, captioned "set by heater script". Sensor rows show pills | `DeviceToView` per room device (memory) | as Home today |
| Vacuum areas | Mapped, non-stale region names. A stale mapping shows "robot map changed". A suggestion shows as a hint | `vacuum.list` + `vacuum.rooms` (memory + SQLite) | none (the robot pushes) |
| Clean button | See §5 | same | the command itself |

**`myhome.HeatingTarget`** is a pure function in `internal/myhome`, next to the existing
`VacuumPhaseText` helper:
`HeatingTarget(levels map[string]float64, ranges []TemperatureTimeRange, occupied *bool, now time.Time) (level string, celsius float64, until *time.Time, ok bool)`.
It mirrors `heater.js:1153-1162` and the midnight-crossing range test
(`heater.js:821-844`, `rooms.isInTimeRange`). A missing level gives `ok=false`, so the widget
renders `--`. It lives in `internal/myhome` because the UI module must not import `myhome/rooms`,
and because `ctl` can reuse it later.

**Freshness.** The 682 relay applies. On `device-update` or `sensor-update` for a device id listed
in the open view's `data-devices` attribute (the tiles grid or the room page), the shell triggers
`refresh` on that view. Bursts are coalesced into one GET per 2 s (a JS constant). Hidden views
are marked dirty (683 §2). The vacuum block alone re-polls at 5 s, and only while the room page is
shown (`every 5s [kiosk.visible('rooms') && kiosk.roomOpen]`). That is the P8 pattern: it reads
cache and never reaches the robot. A future `vacuum-update` SSE replaces it.

**Routes** (Go 1.22 method patterns; registered on the mux, or they fall into the device proxy):

| Route | Renders |
|---|---|
| `GET /htmx/rooms` | tiles (URL unchanged) |
| `GET /htmx/room/{id}` | room page; 404 fragment "Room not found" for an unknown id |
| `POST /htmx/room/{id}/level` (`level`, `delta`) | climate block |
| `GET /htmx/room/{id}/areas` | mapping sheet |
| `POST /htmx/room/{id}/areas` (`map_id`, `region_id`, `assign=1|0`) | mapping sheet |
| `POST /htmx/vacuum/action` with `command=clean-rooms&room_id=…` | room vacuum block (§5) |

### 4. Creating and editing a mapping (1024x600)

- **Entry points.** The room page's vacuum block has [Edit] next to "Areas". With no mapping it
  shows "No robot area assigned" and an [Assign areas] button. Both open the **areas sheet**: an
  overlay over the content area with a 48 px close (x) in its corner and no backdrop dismiss
  (683 §3).
- **Sheet content.** One 56 px chip row per non-stale region of the robot, sorted: suggested for
  this room first, then unmapped, then mapped elsewhere. Each chip shows the region name (or
  "Area <id>"), a "Suggested" pill if applicable, and a subtitle "in <other room>" when it is mapped
  elsewhere. A check glyph marks the regions mapped to this room. Stale regions that are still
  mapped to this room appear at the bottom, greyed, with an [Unassign] action. That is how a
  broken mapping gets cleared.
- **Interaction.** A tap toggles the region. Assigning a region that is mapped elsewhere moves it,
  and the subtitle says so before the tap. Each tap is one POST → `vacuum.roommap` → the sheet
  re-renders (`hx-disabled-elt="this"`). No confirm: a mapping change is reversible and sends
  nothing to the robot. There is no Save button, and the sheet's close returns to the room page,
  which refreshes.
- **No robot, or no regions.** The sheet says "The robot has no named areas yet. Run a room sync
  from the Vacuum section, or start one room clean from the robot's app." (This text points to the
  existing sync path. The sync button itself is #685's.)
- **Desktop and phone.** The same sheet works by mouse. On a phone it becomes full width.

### 5. "Clean this room"

**States of the vacuum block** (rendered server-side from `vacuum.list` + `vacuum.rooms`):

| Condition | Block shows |
|---|---|
| Robot list empty (integration off) | block omitted |
| No mapped, non-stale region for this room | "No robot area assigned" + [Assign areas] (+ suggestion hint) |
| Mapped regions span several maps | button disabled, "Areas are on different robot maps: edit areas" |
| Robot not connected | button disabled, "Robot offline" (plus "last seen at <host>" as today) |
| Robot running or paused | button disabled, "Robot busy: cleaning <targets>" (the existing `targetNames`) |
| Otherwise | **[Clean this room]**, primary, full block width, 56 px |

**Flow.**

1. Tap. If the policy says confirm (below), the 683 dialog shows "Clean <room> now? The robot
   leaves its dock." with [Cancel] and [Clean].
2. `POST /htmx/vacuum/action` with `robot`, `command=clean-rooms` and `room_id`. This is the
   handler #725 kept. **When `room_id` is present, the handler ignores any `room` fields.** It
   resolves the regions on the server, at that moment, as those with `RoomID == room_id`, not
   stale, and on one map, then calls `vacuum.clean` with their ids. The response is the room's
   vacuum block, not the vacuum panel. Without `room_id` the handler keeps its current behaviour.
3. While the request is open (up to ~30 s: abort watch plus one retry, verified in `clean.go`),
   the button shows "Starting…" and is disabled (`hx-disabled-elt`).
4. Result in the block: `running` → "Cleaning <room>". `no-report` → "Sent; the robot has not
   confirmed yet". An error → the error text, the existing `actionErr` pattern, including
   `VacuumErrorHint`.

**Confirm policy as a single line.** One package-level table in `htmx_vacuum.go` is used by
both the Vacuum section and the room page:

```go
// vacuumConfirm lists the vacuum actions that ask before sending, with the dialog text.
// Owner decision pending (#685 vs 683 §8.5): add or remove an entry, nothing else.
var vacuumConfirm = map[string]string{
	"start":      "Start a whole-home clean?",
	"stop":       "Stop the robot?",
	"clean-room": "Clean %s now? The robot leaves its dock.",
	// "dock":    "Send the robot back to its dock?",
}
```

Templates emit `hx-confirm` only through a `confirmFor` template func that reads this map. If #725
already introduced an equivalent set, this PR reuses it rather than adding a second one. A test
renders both pages with the entry present and absent.

### 6. Degraded modes

| Situation | Room page behaviour | Devices |
|---|---|---|
| **Daemon down** | 683 banner. Tiles and the last-open room page stay as greyed DOM, with actions inert. Opening a room never opened before shows "Not available while Penates (My Home Daemon) is offline" in the sub-view, without a request. Tiles carry the headline values, so the list stays useful | `heater.js` keeps regulating from retained ranges and occupancy (daemon-optional rule) |
| **Robot offline** | Clean is disabled with "Robot offline". Mapping stays viewable and editable (SQLite). Learned regions cannot be judged stale without robot state, so they are listed, and `vacuum.clean` would refuse anyway ("robot does not report map") | n/a |
| **Internet down** | No effect on the page. The start-time cloud sync fails and the stored regions and mappings are kept (verified `sync.go`). Room clean is local | n/a |
| **Broker down, daemon up** | Values freeze and are not marked stale (682 Open question 3). Scope question 5 offers a per-sensor age | devices unaffected |
| **Rooms service disabled** | The Rooms section shows its existing "not enabled" state. `vacuum.roommap` refuses room assignments | n/a |
| **Occupancy service disabled** | The target shows the away level with "(no presence data)", matching `heater.js` | n/a |
| **A mapped room deleted** | Its regions read as unmapped and become suggestions again | n/a |

## Alternatives considered

**A. Mapping table in the rooms package, `room_vacuum_regions(room_id, blid, pmap_id, region_id)`.**
Strongest form: the room owns "what belongs to me", deleting a room can cascade, and a region can
belong to two rooms (open-plan). **It loses on smallness and on the robot's own lifecycle.** The
vacuum service decides when a region is stale, renamed or replaced (`syncCloud`, `learnRegion`).
A second table keyed on that region must track the same lifecycle from outside. `myhome/rooms`
would also learn the robot's identifiers. The column moves with the region for free. If
open-plan regions turn out to matter (Scope question 4), the table is the upgrade path, with a
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
reverted silently within `poll_interval_ms`. The level stepper acts through the input the script
actually reads.

**E. Auto-apply name matches in `vacuum.roomsync`.** Strongest form: zero taps on a fresh setup.
**It loses on safety and ordering:** see §2.

## Consequences

- **Better:** one screen per room. The vacuum mapping survives cloud renames, robot echoes and
  binary downgrades. The nightly updater can install an older release (`CLAUDE.md`); older code
  selects explicit columns and ignores `room_id`, and the next upgrade finds the mapping intact.
  The mislabelling of every switch as "heater" goes. Cleaning resolves regions on the server
  at tap time.
- **Worse:** the heating-target rule now exists twice, in `heater.js` and `myhome.HeatingTarget`,
  and can drift. Mitigation: a table test whose cases are copied from the script's branches, a
  comment in both places naming the other, and the label "target", not "the heater will…". The
  room page's devices inherit the P6 polarity read until 682 follow-up E lands.
- **Harder to change later:** `vacuum_regions.room_id` and `VacuumRegion.room_id` become a contract
  (many-to-one). Moving to the join table (A) is a migration plus a JSON shape change.

## Cross-cutting concerns

- **Security:** unchanged LAN trust model (reference §13). Any LAN client can already call
  `vacuum.clean` through `/rpc`. The new verb is no more powerful. No secret is involved; the
  robot password stays in `.env`.
- **Privacy:** not affected. Room names and robot area names are already stored.
- **Observability:** `vacuum.roommap` logs at Info (robot, map, region, room, old room). The clean
  path already logs every attempt. Room page renders log at V(1).
- **Failure modes:** see §6. Failed fragment loads use the existing "couldn't refresh" pill.
- **Rollout and migration:** the additive column is applied at daemon start. There is no data
  backfill, and every region starts unmapped. Each PR can be reverted alone. Reverting PR 1 leaves
  an unused column, which is harmless.

## Impact on existing conventions

- **Extends** 683's sub-view pattern with its first real use (room page, areas sheet).
- **Bends** the "UI goes through RPC" rule only where #224 already did: the room page keeps reading
  `room_agenda_cache` through `sqlDB`. Nothing new reads storage directly.
- **Adds** a business-rule helper (`HeatingTarget`) in `internal/myhome`, next to existing
  presentation helpers. It is a pure function, not a service.
- **Changes** the tile markup of `/htmx/rooms`. The URL and the `roomsUpdated` trigger are kept.
- **Removes** `deviceRoleLabel`'s "everything else is a heater" heuristic, replaced by
  `HasHeaterScript` plus capability checks.

## PR decomposition

Across all PRs, these must not change: `heater.js` and every device script, the retained ranges
topic's format, `vacuum.clean` semantics, existing JSON fields of `vacuum.*` results, the fragment
URLs `/htmx/rooms` and `/htmx/vacuum*`, and SSE event names and payloads. No PR needs hardware.

1. **Region → room mapping (daemon only).** Files: `myhome/vacuum/storage.go`, `rpc.go`, and a
   storage test (new or `service_test.go`). In `internal/myhome`: `const.go`, `vacuum.go` and
   `methods.go`. Plus `docs/vacuum.md`. Acceptance, in tests: an old-schema DB gains `room_id` and
   keeps its rows; `syncCloud` (rename, removal) and `learnRegion` preserve `room_id`;
   `vacuum.roommap` sets, moves and clears; an unknown region errors; an unknown room errors when a
   fake `temperature.get` is registered (methods map saved and restored in `t.Cleanup`, no
   `t.Parallel()`); `vacuum.rooms` returns `room_id`. Verification: `make test`, run in the
   background.
2. **Room tiles and read-only room page.** Needs 683 PR 4 (device row). Files: `htmx_room.go` (new:
   handlers and templates), `htmx.go` (tiles; drop `deviceRoleLabel`), `server.go` (routes),
   `static/index.html` (`#room-view`), `myhome.js` (`openRoom`, Back, SSE refresh with 2 s
   coalescing), `myhome.css` (grid and breakpoint), `internal/myhome/heating.go` + test
   (`HeatingTarget`), `htmx_room_test.go`, `server_test.go`. Acceptance: a `HeatingTarget` table
   (occupied, unoccupied, nil occupancy; inside and outside a range; midnight-crossing; missing
   level → `--`); template tests for one, two and zero sensors (`--`), a heater row with no toggle,
   and the vacuum block omitted when there is no robot; `GET /htmx/room/x` returns 200 and is not
   proxied, and an unknown id gives the not-found fragment; nothing is open on load. A render with
   a device caller that fails on any call passes **with the polarity cache pre-filled** (the P6
   caveat, stated in the test). Verification: Chromium at 1024x600 against a daemon with the
   embedded broker and no devices (temperatures `--`), plus the 683 tap-target audit.
3. **Heater level stepper.** Files: `htmx_room.go`, its test. Acceptance: a handler test with fake
   `temperature.get` and `room.edit`: +0.5 changes only the active level and clamps at 5 and 30;
   no stepper without a heater device. Verification: subscribe to the embedded broker's
   `myhome/rooms/<id>/temperature/ranges` and see the level change after a tap.
4. **Areas sheet and suggestions.** Needs PRs 1 and 2. Files: `htmx_room.go` (sheet, POST),
   `suggest.go` + test, `myhome.js` (sheet open and close), `myhome.css`. Acceptance:
   `suggestRegions` cases (case, accents, separators; two rooms matching → none; stale excluded;
   mapped to a deleted room → suggested); the sheet has its own 48 px close and no backdrop
   dismiss; a tap POST calls a fake `vacuum.roommap` with the right map and region. Verification
   without a robot (*inferred to work*): set dummy `MYHOME_VACUUM_BLID` and `MYHOME_VACUUM_PASSWORD`
   in a scratch instance, insert a few `vacuum_regions` rows into a scratch `myhome.db`, then map
   and unmap from the sheet.
5. **Clean this room.** Needs PR 4, 683 PR 5 (confirm dialog) and #725. Files: `htmx_vacuum.go`
   (`room_id` branch, `vacuumConfirm`, `confirmFor`), `htmx_room.go` (vacuum block states),
   `htmx_vacuum_test.go`, `htmx_room_test.go`. Acceptance: with `room_id`, a fake `vacuum.clean`
   receives exactly the mapped, non-stale region ids, and client-sent `room` values are ignored;
   each state in the §5 table renders; `hx-confirm` is present if and only if `clean-room` is in
   `vacuumConfirm` (both tested); an error renders with its hint. Verification: Chromium, where
   Cancel sends no request (network panel) and a disconnected dummy robot disables the button.
   **A real clean on the robot is left to the owner and recorded on the issue.** It is not claimed
   here.

Order: 1 and 2 in parallel → 3 and 4 → 5. PR 3 can be dropped if Scope question 2 goes that way.

## Confirmation

- Go tests named in each PR, run by `make test`. The PR 2 fake device caller makes "no added device
  RPC on render" a regression test.
- Review questions: "Does any new handler call a device or `heater.getconfig`?", "Is the clean
  target resolved on the server from `room_id`?", "Does any vacuum `hx-confirm` bypass
  `vacuumConfirm`?", "Does an unknown value render `--`?", "Does anything open by default?"
- The 683 Chromium protocol (1024x600 touch, tap-target audit, zero cross-origin requests) on the
  tiles, the room page and the areas sheet.

## Scope questions for the owner

1. **Which sensor is "the room temperature" when there are several?** Options: the mean
   (recommended: no new config, and the sensors are listed below it); a per-room "primary
   thermometer" column in `rooms`; or the heater's internal topic (needs a device KVS read, which
   Rule 3 excludes). *Recommendation:* the mean, labelled, for now.
2. **What is "heater control"?** (a) Read-only state. (b) A stepper on the active level, changing
   the room's level persistently (proposed). (c) A relay toggle (reverted by the script, see
   Alternative D). (d) A temporary boost, which needs a `heater.js` change and a heap measurement.
   *Recommendation:* (b), in its own droppable PR 3.
3. **Vacuum confirm scope** (pending between #685 and 683 §8.5). This design makes it one entry per
   action in `vacuumConfirm`. *Recommendation for the room page:* confirm `clean-room`, since the
   robot leaves its dock and a mis-tap on a wall screen is likely.
4. **Can one robot area belong to two rooms (open-plan)?** *Recommendation:* no for now
   (many-to-one column). Revisit with the join table (Alternative A) if a real case shows up.
5. **Show the age of temperatures?** An in-memory `UpdatedAt` set in `Cache.UpdateSensorValue`,
   shown only when the value is older than 30 min ("21.4 °C · 2 h ago"). It is small, and it is the
   only defence when the broker is down. *Recommendation:* yes, folded into PR 2. Note that it
   touches `myhome/devices`.
6. **A CLI for the mapping** (`myhome ctl vacuum rooms map <region> <room>`)? *Recommendation:* not
   in this issue. The verb is callable through `/rpc`, and the kiosk sheet is the editor.

## Open questions

1. **Region id collisions across maps.** `vacuum.clean` matches regions by name *or* id across all
   maps. It prefers the non-stale match and errors on ambiguity (verified, `findRegion`). The page
   sends ids, as the old checklist did. If the robot ever holds two live maps with overlapping ids,
   `vacuum.clean` would need an optional `map_id`. Has a second map ever existed on this robot?
2. **Gen2 thermometers.** Are there rooms whose only thermometer is a Gen2 device? If so, their
   temperature shows `--` until the device cache learns Gen2 temperature components (a separate
   issue).
3. **Stepper bounds.** 5–30 °C and 0.5 °C steps are proposed. `room.edit` itself validates nothing
   beyond requiring `eco`. Should the service enforce bounds too?
