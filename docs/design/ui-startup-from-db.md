# Design: the UI shows known devices and rooms from the DB at startup, with no device polling

Status: draft for owner review. Part of the kiosk campaign #681. Applies Rule 3 of `AGENTS.md`
("Devices push state; nobody polls devices") to the Home (Devices) and Rooms sections. Companions:
`682-retained-state.md` (polling inventory P1-P10), `683-kiosk-shell.md`, `684-room-view.md`.

Read on branch `docs/ui-startup-from-db`, at `origin/campaign/681-kiosk-ui`. Line numbers refer to
that checkout unless marked **(main)** (the main checkout, on `main`) or **(#735)** (the working tree
of `fix/732-render-no-device-calls`, PR #735, commit 055f47e1). **(verified)** means read in code;
**(inferred)** means not traced to the end or not run.

**The architecture reference is partly stale.** `docs/architecture/ARCHITECTURE.md` predates the
rooms refactor (#224, #720) and 682 PR 3. This document relies on the code only. A fresh
`principal-documenter` run should follow (already noted in `684-room-view.md`).

## The ask

Owner decision, 2026-11-07: *"The UI shows known devices and rooms from the DB at startup, with no
device polling."* When the daemon starts, `/htmx/devices` and `/htmx/rooms` must list every device
and room stored in `myhome.db` straight away: names, rooms, and the last-known config. Live status
shows as unknown until the device's own notification (or retained) state arrives. Rendering never
contacts a device, and nothing new polls devices.

Trigger: on 2026-11-07, a side daemon with a fresh DB blocked `/htmx/devices` for more than 40 s. A
goroutine dump showed `DeviceToView → sswitch.OnValue → kvs.GetValue`, waiting on a device's MQTT
dialog lock while devices timed out (#732).

## Findings

### What is in `myhome.db`, and what is in memory only (verified)

| Data | Where | Source |
|---|---|---|
| `devices(manufacturer, id, mac, name, host, info JSON, config_revision, config JSON, room_id)`, PK `(manufacturer, id)` | `myhome.db` | `myhome/storage/db.go:65-76`; upsert `db.go:232-249` |
| `info` = `shelly.DeviceInfo` (model, app, BTHome capabilities for BLU) | DB | `internal/myhome/device.go:73` |
| `config` = `shelly.Config`: switch names (`Switch0..3`), scripts (`Script1..4`, so `heater.js`), `sys` | DB | `device.go:74` |
| `host`: `<id>.local` or empty. IPs are never persisted (migrated away, #252) | DB | `db.go:147-189` |
| `rooms(room_id, name, kinds, levels, ical_url, updated_at)`, `room_kind_schedules`, `room_weekday_defaults`, `weather_cache`, `room_agenda_cache` | DB | `myhome/rooms/storage.go:105-139` |
| `myhome.Device.Status.Sensors` (Gen1/BLU temperature, humidity, window, ...) | **memory only**, "ephemeral" | `device.go:75`; `devices/cache.go:293-338` |
| `Impl()`, the `*shelly.Device` with its `status` (switch outputs, inputs, wifi) | **memory only** | `device.go:76`; `pkg/shelly/device.go:126-127` |
| Switch polarity (`normally-closed` KVS key) | **memory only**, KVS cache | `pkg/shelly/kvs/cache.go:21-80` |
| Pool water supply, turnover, runtime | **nowhere**: read live per request | `myhome/daemon/pool_notices.go:144-177` |

### F1. The device cache is already warm-loaded from the DB before the UI serves (verified)

`DeviceManager.Start` calls `dm.dr.Load(ctx)` (`myhome/devices/impl/manager.go:369-374`) before it
starts any loop or listener. `Cache.Load` copies every row of `devices` into the cache
(`myhome/devices/cache.go:53-74`, via `DeviceStorage.GetAllDevices`, `db.go:296-305`). `dm.Start`
runs at `myhome/daemon/daemon.go:467`. `ui.Start` runs at `daemon.go:720`. So **no change is needed
to warm-load the cache**: every known device, with its `info`, `config` and `room_id`, is in memory
before the first request can arrive. A load failure is logged and the daemon continues with an empty
cache (`manager.go:371-374`). A row whose JSON does not parse is logged and kept, with `Info` or
`Config` nil (`db.go:447-459`).

Devices loaded this way have `Impl() == nil` and no status. They get an `Impl` when the MQTT watcher
first sees them (`myhome/daemon/watch/mqtt.go:110-118`, which writes it into the cache's own
pointer), or when a refresh runs.

### F2. Before #735, rendering the Home section called devices (verified; fixed by #735)

`DeviceToView` (`internal/myhome/ui/template.go:146-366`):

- once a device has an `Impl`, calls `sswitch.OnValue` per switch per render (`template.go:292`),
  which is a `KVS.Get` on a cache miss (`kvs/main.go:58-94`, 5 min TTL);
- with no `Config`, falls back to a live `GetSwitchesSummary` (`template.go:303-316`);
- with no status, renders the switch as **off**, not unknown (`template.go:265-299`).

**Covered by PR #735.** It renders with zero device calls. The polarity comes from
`kvs.Peek` (cache only, TTL ignored, **(#735)** `pkg/shelly/kvs/cache.go:76-94`) through
`sswitch.OnValueCached` (**(#735)** `sswitch/service.go:195-215`). A switch with no status or no
resolved polarity is marked in `DeviceView.SwitchUnknown` (**(#735)** `template.go:65,251-317`). A
device without config shows no switch rows instead of being queried (**(#735)** `template.go:321`).
Measured live on 2026-11-07: `/htmx/devices` took 5 ms with 39 switch rows (5 unknown), against
more than 40 s before.

### F3. Rendering the Home section still calls the pool device (verified; #731, not covered by #735)

`DeviceCards` and `DeviceCard` call `applyPoolStatus` on every render (`htmx.go:149`, `htmx.go:181`),
for **any** device's card. It invokes `pool.getstatus` in-process (`template.go:73-96`), and the
handler (`myhome/daemon/pool_rpc.go:37-61`) makes 3 KVS reads (`pool_notices.go:144-161`, KVS cache
5 min) plus one **uncached** `Input.GetStatus` (`pool_notices.go:166-177`), all over MQTT to the pool
Pro1. It runs on `h.ctx`, the daemon context, so no per-request timeout bounds it. Errors are
swallowed, so a dead pool device costs one MQTT timeout per render. The Pool section does the same
(`htmx_poolpanel.go:70-71`). #735 leaves both in place (**(#735)** `htmx.go:149,178`).

Impact: every `device-update` SSE makes the shell re-fetch one card, so every switch flip anywhere
in the house costs the Pro1 one `Input.GetStatus`. Rate: not measured; bounded above by the
`device-update` broadcast rate.

### F4. The Rooms section already meets the decision (verified)

`RoomsList` (`htmx.go:283-382`) reads `temperature.list`, which is SQLite only
(`myhome/rooms/methods.go:69-87` → `storage.go:217`), the device cache (`GetAllDevices`, memory) for
the room's devices, and `room_agenda_cache` (SQLite, `htmx.go:221-236`). No device call. The rooms
handlers are registered at `daemon.go:562-572`, before `ui.Start`. Device-to-room assignment is the
`room_id` column, so it is in the cache from F1. Nothing needs to change. A regression test should
pin it, because 684's room page will add device rows (`684-room-view.md` §3) and inherits whatever
`DeviceToView` does.

### F5. When live status arrives after a restart (verified)

- `ApplyStatusNotification` ignores every notification, `NotifyFullStatus` included, while a device
  has no status yet (`pkg/shelly/status.go:39-52`). The only thing that sets a status is a refresh
  (`pkg/shelly/device.go:239-240`).
- Every notification from a `shelly*` device queues a full refresh (`watch/mqtt.go:127`, 682 P1).
  So a device that emits anything has a known status within one refresh round trip.
- A device that emits nothing waits for the round-robin job: one Gen2 device per `--refresh-interval`
  (default 1 min, `myhome/ctl/options/options.go:33`; `manager.go:656-701`). With N Gen2 devices,
  the worst case is N minutes. The #735 measurement (5 of 39 switch rows unknown) is that window.
- No daemon-published retained status topic exists yet (682 PR 7). A grep for `PublishStatus` and
  `myhome/switch/` finds nothing. Nothing subscribes to the firmware's `<id>/status/<component>`
  topics (682 experiment X1, not run).

### F6. The UI port opens only after weather location discovery (verified; delay never measured)

`ui.Start` (`daemon.go:720`) comes after `discoverLocation` (`daemon.go:541-559`) when no latitude
and longitude are configured. Discovery calls `Cloud.GetStatus` and `Shelly.DetectLocation` over HTTP
on each device that already has an `Impl` (`daemon.go:86-141`). It then calls `IPGeoLocation`
(`myhome/weather/location.go:17-40`), which uses `http.DefaultClient` on the daemon context **with no
timeout**. That breaks the CLAUDE.md internet-optional rule. With the WAN down, the UI cannot answer
until the OS gives up on the TCP connect (inferred). How long that takes on the NAS has **never been
measured**, and whether the NAS has latitude and longitude configured was not checked.

### F7. Unsynchronised writes of `Impl` on shared cache pointers (inferred; never observed)

The watcher calls `device.WithImpl(sd)` on the cache's pointer without the cache mutex
(`watch/mqtt.go:117`). #735's startup pass does the same (**(#735)** `manager.go:654-660`). Render
goroutines read `d.Impl()` at the same time (`template.go:258`). This is a data race by the Go memory
model. `-race` has not flagged it, probably because no test runs these paths at the same time. It is
recorded here and not fixed (see Open questions).

## Is #735's startup pass acceptable under Rule 3?

Rule 3 says: "One forced refresh is allowed, at daemon start." #735 adds three device contacts:

1. **`primeAllPolarities` at `dm.Start`** (**(#735)** `manager.go:388,637-666`): one `KVS.Get` of
   `normally-closed` per known Gen2 device that has switches. It runs once per process, in a
   goroutine, one device at a time, with a 15 s timeout each. **Acceptable.** It is narrower than the
   forced refresh the rule allows: one key, not `GetDeviceInfo` + `GetComponents`. It does not
   block serving. It runs at the 2-day restart by design, which is the case the rule describes.
   Caveat: offline devices are visited in turn, so the pass can take 15 s per offline device. That
   delays only when their polarity becomes known, not the UI.
2. **`primeSwitchPolarity` after each refresh** (**(#735)** `manager.go:608`): part of a refresh that
   already happens (P1, P2, P3, P10). It adds no trigger. A KVS cache hit makes it free within the
   TTL. **Acceptable**, and its cost goes away whenever 682 PR 4 or the round-robin follow-up B
   removes a refresh trigger.
3. **`reprimePolarity` on a `kvs_rev` invalidation** (**(#735)** `watch/mqtt.go:133-138,174-183`):
   started by the device's own `NotifyStatus`. That is push followed by one targeted read, not a
   timer. **Acceptable.** This is exactly the "invalidated on `kvs_rev`" path that 682 follow-up E
   asks for.

None of these is a timer or a render-path read. **#735 conforms to Rule 3.**

## Goals and non-goals

Goals

- G1. From the first request after start, `/htmx/devices` lists every device in `myhome.db` (name,
  room, switch names from the stored config) and `/htmx/rooms` lists every room with its devices.
- G2. Rendering either section makes zero device RPCs, enforced by tests.
- G3. Unknown live state renders as unknown: a disabled switch button, `--` pills. It never shows a
  default such as "off" or "water supply OK".
- G4. No new timer or trigger that reads device state.

Non-goals

- **Warm start from retained topics.** That would mean seeding status from 682 PR 7's
  `myhome/switch/<id>/status` or from the firmware's `<id>/status/...` topics (X1). It would shrink
  the unknown window to zero, but 682 Scope question 2 recommends "not now", and the decision accepts
  unknown until the device speaks.
- **A forced full-status sweep at start.** Rule 3 allows one, but the decision does not need it, and
  #735's narrower polarity pass covers what rendering needs.
- **Seeding status from `NotifyFullStatus`.** That would be a small change in `status.go:50`. Today it
  gains nothing, because every notification already queues a refresh (P1). It becomes worth doing
  together with 682 PR 4.
- **Persisting live state to the DB.** Status stays in memory (it was ephemeral by design, `device.go:75`).
- **Fixing F7** and the other refresh triggers (#728, #729, #730).

## Quality drivers

1. **Time to first render.** When the daemon has started and the UI port is open, `/htmx/devices`
   answers in under 100 ms for about 40 devices, whatever state the devices are in (measured with
   #735: 5 ms).
2. **Device heap.** With the kiosk open on Home for 24 h, the pool Pro1 receives zero RPCs caused by
   rendering. Today it receives one `Input.GetStatus` per card render (F3).
3. **Honesty.** After a restart, a device that has not spoken shows unknown, never off.
4. **Internet-optional.** With the WAN down, the UI port opens no later than with it up (F6).

## Fit

**This fits the existing architecture as-is.** The cache warm-load (F1) and the device-free Rooms
fragment (F4) already exist. #735 makes `DeviceToView` pure. What remains is one more render-path
read to remove (#731), tests that pin the behaviour, and one startup-ordering fix. No new component,
table, verb or config option is needed.

## Proposed design

1. **Render from the cache only.** Keep `Cache.Load` before `dm.Start` returns (F1) and
   `DeviceToView` as #735 leaves it. Rule for every `/htmx/*` handler on the Home and Rooms path: read
   `DeviceRegistry` (cache, then DB on a miss: `cache.go:152-163`, which is SQLite, not a device), the
   rooms tables, and in-process RPC verbs **that are memory or SQLite only**. Any verb that reaches a
   device is banned from render paths.
2. **Pool fields from memory (#731).** The render path must stop calling `pool.getstatus` while that
   verb reads the device. Recommended shape, smallest first:
   - `WaterSupplyActive` is `input:0.state` of the pool device. It is read from the cached device's
     `Status().Input0`, which the refresh fills and `ApplyStatusNotification` keeps current
     (`status.go:81-98`). That is the same value `Input.GetStatus` returns (`pool_notices.go:175`), at
     zero RPC cost. If it is nil, it is unknown and renders `--`.
   - Turnover and runtime come from a snapshot in `PoolNotices`, written where it already reads them
     (`recordTurnoverToday` on `pool.pump_stop`, `pool_notices.go:106-111`) and once at start, with a
     `ts`. If there is no snapshot, they render `--`.
   - The UI reaches the snapshot through the in-process method registry, because `internal/myhome/ui`
     cannot import `myhome/daemon`. Whether `pool.getstatus` itself starts answering from the
     snapshot, which changes what `ctl pool status` shows, is Open question 1.
   - Templates: `IsPoolPump` becomes a fact about the device id. The pills render `--` when a field
     is nil, instead of "OK" (today `htmx.go:722-727` shows "OK" whenever the value is false *or*
     unknown).
3. **Open the UI port before location discovery (F6).** Run `discoverLocation` and the weather
   publisher in a goroutine, and give `IPGeoLocation` a 5 s timeout (`context.WithTimeout`). This does
   not reorder anything the UI depends on: the weather publisher has no UI consumer (verified: no
   `/htmx` handler reads `weather_cache`). Gated on a measurement (see PR D).
4. **Pin it with tests.** Build the cache from a SQLite `:memory:` `DeviceStorage` holding devices
   (with and without config, one with `room_id`) and rooms. Render `/htmx/devices` and `/htmx/rooms`
   through `newMux`. Assert that every name and room appears, that switches are unknown, and that the
   pool pills show `--`. Register a `pool.getstatus` fake that fails the test if it is called after
   PR B. This extends #735's `render_nocalls_test.go` pattern.

### Interaction with the startup refresh and retained topics

- At start: the cache is loaded (F1), then #735's polarity pass runs. Status is filled by P1 (first
  notification of each device), P2 (one device per minute) and P3 (mDNS). Rows go from unknown to
  known through the existing `device-update` SSE (`manager.go:620-625` via `storeDeviceLoop`).
- When 682 PR 4 removes the refresh on status-only notifications, F5's path matters: status would then
  be filled only by P2, P3 and `cfg_rev` refreshes. At that point either seed from `NotifyFullStatus`
  or keep a narrow refresh for devices with no status. That decision belongs to 682 PR 4, which must
  state it.
- When 682 PR 7 publishes retained status topics, nothing here changes. The render still reads
  memory. Seeding memory from those topics at start is 682 Scope question 2, a separate decision.

## Alternatives considered

1. **A forced full refresh of every device at start, before `ui.Start`.** Strongest form: the first
   render shows real switch state, not unknown, and Rule 3 explicitly allows one forced refresh.
   **It loses on time to first render.** About 40 devices at 2 to 3 RPCs each, with a 10-wide
   semaphore (`manager.go:61`) and offline devices timing out, puts seconds to minutes in front of
   the first page. It also directly contradicts "immediately". The same sweep in the background is
   compatible with this design, but the decision does not ask for it.
2. **Render from the DB directly** (`storage.GetAllDevices` in the handlers). Strongest form: it is
   obviously device-free, and it lists exactly what the DB holds. **It loses because the cache is a
   superset**: it holds Gen1/BLU sensor values and live status, and is already loaded from the same
   rows (F1). A DB render would show no live state at all and needs a second data path.
   `DeviceRegistry`'s own comment rejects it (`template.go:30-36`).
3. **Keep `applyPoolStatus` but bound it with a short timeout.** Strongest form: a one-line change,
   and it keeps the values exact. **It loses on device heap and on Rule 3.** It is still one Pro1 RPC
   per card render, which is the P5 path 682 retires.

## Consequences

- Better: startup render is constant-time and device-independent. The Pro1 stops getting
  render-driven RPCs. Unknown is shown honestly, including for the pool.
- Worse: after a restart, quiet devices stay unknown for up to N minutes (F5). The pool turnover
  shows `--` until the first pump stop, or until the start read succeeds.
- Harder to change later: "render paths call only memory or SQLite verbs" becomes a tested contract.
  A future verb that reaches a device must not be called from `/htmx`.

## Cross-cutting concerns

- **Security, privacy:** not affected.
- **Observability:** keep `DeviceCards`' Info logs. A V(1) log line at the polarity pass's end exists
  in #735 (`manager.go:665`). PR B logs the pool snapshot's source and `ts` at V(1).
- **Failure modes (degraded modes):**

  | Situation | Home and Rooms behaviour |
  |---|---|
  | Daemon restart (2-day, upgrade) | Every device and room listed on the first render, from the cache warm-loaded from `myhome.db`. Switches unknown until the device's first notification or refresh. Polarity known after #735's pass reaches the device |
  | Fresh or empty DB | Only devices seen so far (MQTT, mDNS, BLU) are listed. Devices without config show no switch rows. Rooms empty until created |
  | DB unreadable at load | Daemon logs the error and continues with an empty cache (`manager.go:371-374`): same as a fresh DB |
  | Broker down at start | `mc.Start` fails and the daemon exits before `ui.Start` (`daemon.go:693-696`); unchanged. Broker lost later: rows keep their last state, not marked stale (682 Open question 3) |
  | Device offline | Listed with its stored name, room and switch names. State unknown, or the last value seen in this process. Render time unaffected (#735) |
  | Pool Pro1 offline | Pool pills `--` (after PR B). Today: one MQTT timeout per card render |
  | Internet down | After PR D, no effect on the UI. Today: UI port delayed by `IPGeoLocation` when no latitude and longitude are configured (F6, unmeasured) |

- **Rollout and migration:** no schema change. Each PR can be reverted on its own.

## Impact on existing conventions

- Applies Rule 3 as written. Bends nothing.
- Makes concrete the 683 rule "unknown renders `--`" for the pool pills.
- PR D brings the weather location lookup in line with the CLAUDE.md internet-optional rule (timeout
  plus fallback).

## PR decomposition

Across all PRs, these must not change: fragment URLs, SSE event names and payloads, RPC verb
signatures (a new optional result field is allowed), device scripts, and the `devices` schema.

- **PR A: #735 (done, under review).** Device-free `DeviceToView`, `SwitchUnknown`, `kvs.Peek`, the
  polarity startup pass, the re-read on `kvs_rev`. Covers F2.
- **PR B: pool fields from memory (#731).** Files: `myhome/daemon/pool_notices.go`, `pool_rpc.go`,
  `internal/myhome/ui/template.go` (`applyPoolStatus`), `htmx.go` (pool pill templates),
  `htmx_poolpanel.go`, their tests. Must not change: `PoolGetStatusResult`'s existing fields, the
  `pool.turnover_today` notice. Acceptance: rendering `/htmx/devices` 100 times with a counting fake
  pool device gives 0 calls; nil water supply renders `--`, not "OK"; the snapshot updates on
  `pool.pump_stop`. Verification without hardware: `make test` (in the background); a daemon against
  the embedded broker with the pool id set to a non-existent device renders instantly, with `--`.
- **PR C: startup render regression tests.** Files: a new `internal/myhome/ui/startup_render_test.go`.
  It may need a small test seam in `myhome/devices` to build a `Cache` from a `DeviceStorage` (it
  already takes a `DeviceRegistry`, `cache.go:24`). Acceptance: the four assertions of design item 4.
  It can land before PR B, with the pool assertion added by PR B.
- **PR D (gated): UI before location discovery.** First measure: on the next NAS restart, compare the
  `"Starting MyHome daemon"` and `"UI server listening"` log timestamps (the journal is volatile, so
  sample it live), with and without WAN. Also check whether the NAS config sets latitude and
  longitude. If the gap is under 1 s and the NAS has coordinates, do only the timeout half. Files:
  `myhome/daemon/daemon.go`, `myhome/weather/location.go`. Acceptance: a unit test with an
  `httptest` server that never answers returns within the timeout. Verification: start the daemon
  with the default route removed and see the UI listen at once.

Order: A (in review) → C and B in either order → D when measured.

## Confirmation

- PR C's test fails if any Home or Rooms render calls a device or a device-reaching verb.
- Review question for every `/htmx` change: "Does this handler call anything other than the cache,
  SQLite, or a memory-only verb?"
- Live check after B: kiosk on Home for 1 h, the Pro1's `Input.GetStatus` count from daemon debug
  logs is 0.

## Backport to main

`main` has the same defects. `DeviceToView` calls `OnValue` (**(main)** `template.go:284,303`) and
`GetSwitchesSummary` (**(main)** `template.go:298`), and `applyPoolStatus` runs on every render
(**(main)** `htmx.go:149,181`). The cache is warm-loaded the same way (**(main)** `manager.go:371`).
`main` differs from the campaign branch in ways that matter for porting:

- **No notification projection** (682 PR 3 is campaign-only). There is no `pkg/shelly/status.go`; the
  watcher still has `// TODO: Update device from status` (**(main)** `watch/mqtt.go:151`), and
  `Status()` takes no lock (**(main)** `pkg/shelly/device.go:362-364`). So on `main` the status comes
  only from refreshes, and #735's `reprimePolarity` hook must sit next to `ObserveRevision`
  (**(main)** `watch/mqtt.go:149`), not inside the `ApplyStatusNotification` block.
- **#735 changes `kvs.ObserveRevision` to return whether it invalidated** (**(#735)**
  `watch/mqtt.go:176`). That signature change, `kvs.Peek`, `OnValueCached`, `PrimeOnValue`,
  `primeSwitchPolarity`, `primeAllPolarities` and the `DeviceToView` change all port as they are:
  they depend only on `pkg/shelly/kvs` and `sswitch`, which `main` has.
- **`SwitchUnknown` is consumed by the campaign's device row** (`htmx_row.go`, 683 PR 4, campaign-only).
  On `main` the port must also teach both card templates (**(main)** `htmx.go`, `deviceCardsTemplate` /
  `deviceCardTemplate`) to render an unknown switch as a disabled button. Without that, unknown would
  render as off: the field would be set but never shown.
- **PR B** ports to `applyPoolStatus` and the card templates. `htmx_poolpanel.go` is campaign-only.
  Reading `Input0` from the cached status works on `main`, but there it only updates at refresh,
  because `main` has no notification merge. The value is the last refresh's, which is still better
  than one RPC per render.
- **PR C and PR D** port unchanged (PR C minus the device-row assertions).

Recommendation: land on the campaign branch first. Then port #735 + PR B as one `main` PR, with the
template adaptation above, and list the commits in the issue.

## Open questions

**Owner answers (2026-11-08).** All three are settled; the original questions are kept below for the record.

1. **Snapshot.** `pool.getstatus` (and so `ctl pool status`) answers from the in-memory snapshot, with
   `ts` in the result. The owner accepts that the CLI then shows the snapshot, not a live read. No
   `--refresh` now. Follow-up PR "E" below.
2. **Make it atomic now, under a lock.** The device-handle (`Impl`) write must go through a
   mutex-protected accessor or equivalent, not a lock-free trick, and gets a race-detector test
   (`-race`, run in the background per CLAUDE.md). It is still never observed: the owner chose to fix it
   because #735 added a second writer. Follow-up PR "F" below.
3. **No one-off refresh at start** (stays "not now"). Instead, a row whose live state is not yet known
   after startup shows a clear **"Starting…"** state, not a misleading on/off and not a bare unknown,
   until the first retained event or notification for that device arrives. It is implemented where
   PR D and the Rooms and Devices views (#744) are touched. Follow-up PR "G" below. Open detail for the
   implementer: when "Starting…" turns into plain unknown for a device that never reports (a quiet or
   offline device). The design says only "until the first retained event arrives"; propose a rule
   (e.g. after the first full rediscovery pass) in the PR rather than leave a device "Starting…" forever.
   **Decided in #763 (PR #773):** the phase ends when the refresh round-robin
   (`runDeviceRefreshJob`) wraps, i.e. on the tick after the last Gen2 device was queued, or when that job
   exits for any reason. Worst case (N+1) x `--refresh-interval` for N Gen2 devices (longer if discovery
   adds devices mid-pass). Accepted gaps: the pass is counted by index, so a shrinking device list can
   end it early, and an empty cache ends it at once. No hard cap. Open browsers are then told to
   re-fetch the rows still unknown.

Follow-up PRs from these answers (each into the campaign branch, after PR B #745 lands, since they touch
the same files):
- **PR E: `pool.getstatus` from the snapshot.** Add `ts` to the result (a new optional field is allowed
  by the rules above). The CLI prints the snapshot's age.
- **PR F: atomic `Impl` write.** Mutex-protected accessor, plus a `-race` test with two concurrent
  writers and a reader.
- **PR G: "Starting…" state** for switch rows whose state is unknown since startup, in the device row
  (`htmx_row.go`) shared by Rooms and Devices, with the end-of-starting rule above.

Original questions:

1. **`ctl pool status`: live or snapshot?** If `pool.getstatus` answers from the snapshot, the CLI and
   the UI agree and the Pro1 is never read on request. The cost is that `ctl` shows values up to one
   pump cycle old, dated by `ts`. The alternative keeps `pool.getstatus` live for `ctl` (an explicit
   user action under Rule 3) and adds a memory-only path for the UI. That alternative needs a new verb
   (the four RPC steps) or a parameter. *Recommendation:* snapshot, with `ts` in the result, plus an
   explicit `--refresh` later if anyone misses the live read.
2. **F7, the `Impl` write race.** Should `WithImpl` on cached devices go through the cache mutex (or
   become an atomic pointer) now, given that #735 adds a second writer? It has never been observed, and
   it is outside this decision.
3. **Background startup sweep of status.** Should the unknown window (F5, up to N minutes for quiet
   devices) be closed by a one-off background refresh at start, as Rule 3 allows? The decision accepts
   unknown, so this is the owner's call. It ties into 682 PR 4.

## Ready-to-file issue

**Title:** UI: render Home and Rooms from the DB-loaded cache at startup with zero device calls (pool card still reads the Pro1)

**Body:**

> Owner decision (2026-11-07): the UI shows known devices and rooms from `myhome.db` at startup, with
> no device polling (AGENTS.md Rule 3). Design: `docs/design/ui-startup-from-db.md`, on the branch
> that adds it, targeting `campaign/681-kiosk-ui`.
>
> **State today.** The device cache is already loaded from `myhome.db` before the UI starts
> (`myhome/devices/impl/manager.go:369-374`, `myhome/daemon/daemon.go:467` before `:720`), and the
> Rooms fragment reads only SQLite and memory (`internal/myhome/ui/htmx.go:283-382`). PR #735 (#732)
> made `DeviceToView` device-free: `/htmx/devices` measured 5 ms with 39 switch rows (5 unknown),
> against more than 40 s before.
>
> **Remaining defects:**
> 1. `applyPoolStatus` (`htmx.go:149,181`) calls `pool.getstatus` on every card render, for any
>    device. That makes an uncached `Input.GetStatus` plus 3 KVS reads on the pool Pro1
>    (`myhome/daemon/pool_notices.go:144-177`), on the daemon context with no timeout. This is #731;
>    fix shape in the design's PR B. The water-supply pill also shows "OK" when the value is unknown.
> 2. No test pins "Home and Rooms render with zero device calls from a DB-loaded cache" (design PR C).
> 3. `ui.Start` waits for weather location discovery. When no coordinates are configured, that
>    includes `IPGeoLocation` with no timeout (`myhome/weather/location.go:17-40`), which breaks the
>    internet-optional rule. The delay has never been measured; measure it before changing anything
>    (design PR D).
>
> **Also affects `main`:** the same `OnValue`/`GetSwitchesSummary` render calls (`main`
> `internal/myhome/ui/template.go:284,298,303`) and `applyPoolStatus` (`main` `htmx.go:149,181`). The
> fix lands on `campaign/681-kiosk-ui` first. Backport note: port #735 + PR B to `main` as one PR. The
> `reprimePolarity` hook moves next to `ObserveRevision` (`main` `myhome/daemon/watch/mqtt.go:149`),
> because `main` has no `ApplyStatusNotification`. The `main` card templates must render
> `SwitchUnknown` as a disabled button, because `htmx_row.go` is campaign-only.
>
> **Acceptance:** rendering `/htmx/devices` and `/htmx/rooms` 100 times from a cache built from a
> SQLite `:memory:` DB makes 0 device or device-reaching-verb calls. All stored devices and rooms are
> listed. Unknown switch state and pool fields render as unknown (`--` or a disabled button).
>
> Related: #682 (polling inventory P5, P6), #728, #729, #730 (refresh triggers), #731, #732 / #735.
