# Design: devices publish retained state on change; UI and daemon never poll devices (#682)

Status: draft for owner review. Part of the kiosk campaign #681. Companion: `683-kiosk-shell.md`.

Reference: `docs/architecture/ARCHITECTURE.md` (written 2026-11-04, judged current). Every claim
below marked **(verified)** was read in code on branch `docs/681-kiosk-design`; **(inferred)** was
not traced to the end.

## The ask

Write down, and make enforceable, the principle that fresh device state reaches the daemon and
the UI through push (device events, retained MQTT topics), never through periodic RPC polling of
devices. Fix a topic and payload convention, list every existing polling path with one follow-up
issue each, define once how the daemon relays state to browsers, and pick a daemon-liveness
signal for the kiosk's offline banner.

## Goals and non-goals

Goals

- G1. A short rule in `AGENTS.md`, with a pointer in `CLAUDE.md`, that a reviewer can apply to a
  PR: "does this read device state on a timer or on the render path?"
- G2. One naming and payload convention for daemon-published retained status topics.
- G3. The UI's render path stops contacting devices. Every fragment renders from daemon memory.
- G4. An inventory of polling paths. Each gets a verdict (retire, re-scope or keep) and a
  follow-up issue text ready to file.
- G5. One relay pattern from daemon state to browser, used by every kiosk section.
- G6. A liveness signal that lets the browser show "Penates (My Home Daemon) is offline" within a
  bounded time.

Non-goals

- Device-side publishing of new fields. Pool Pro1 scripts are at their heap limit (#474/#433). No
  device-side publish is proposed here. The rule defines when one may be proposed later.
- Making devices re-emit their state after an MQTT reconnect. That is experiment #688. This design
  works whether #688 succeeds or not.
- An MQTT Last Will or birth message for the daemon. That would be useful to device scripts and
  `ctl`, but browsers cannot read MQTT, so it does not serve the banner (see Alternatives).
- Showing "daemon up, but its MQTT link is down" as a separate banner. The hook for it is
  described under Open questions.
- Changing the hourly config reconciliation or the vacuum robot link. Neither polls state (see
  the inventory).
- Retiring any polling path in this document. Retirement is the job of the follow-up issues.

## Quality drivers

The architecture reference lists these quality attributes: local-first, daemon-optional devices,
low device heap, operability. This feature adds the drivers below.

1. **Device heap.** When a kiosk is left on a page for 24 h, the Pro1 pool device receives zero
   RPCs caused by rendering. Today it receives one `Input.GetStatus` per rendered card of any
   device (see P5).
2. **Freshness.** When a switch flips, whether by a physical button, a schedule or the UI, the
   kiosk row shows the new state within 2 s. It does so without a device RPC. Today the change does
   not reach the browser at all (see P1, verified).
3. **Offline detection.** When the daemon stops or restarts, the banner appears within 2 s. When
   the network silently partitions (Pi Wi-Fi drops, NAS loses power), it appears within 45 s. The
   last-known data stays on screen.
4. **Restart cost.** After the 6-hourly restart (`RuntimeMaxSec=21600`), the daemon may do one
   forced refresh. That is the only device sweep it is allowed.

## Fit

**The design fits the existing architecture, with one small increment.** The increment is a
daemon-side, in-memory "state projection" per domain, owned by the service that already receives
that domain's events. Nothing new sits between the components. The parts that already exist:

- The device manager already receives every `+/events/rpc` message (`watch/mqtt.go`). It has a
  `// TODO: Update device from status` exactly where the projection belongs (verified,
  `watch/mqtt.go:151`).
- `PoolNotices` already receives every recorded event through the events broadcast hook
  (`pool_notices.go` `OnEvent`, verified). This includes `pool.water_supply_*`, `pool.pump_*` and
  `switch.on/off` for the pool device.
- The vacuum service already holds robot state in memory, pushed by the robot.
- The SSE broadcaster already does invalidate-and-refetch for device cards (`device-update` makes
  the client call `GET /htmx/device/{id}`).
- Retained publishing has three precedents (`occupancy`, `solar`, `temperature`).

**What the code shows about the devices:** setup and reconciliation call `mqtt.SetServer`, which
sets `status_ntf: true` (verified, `pkg/shelly/mqtt/ops.go:71`, `setup.go:258,513`). Per the field
comment in `pkg/shelly/mqtt/types.go:73`, this makes each Gen2 device publish its complete
component status on `<device-id>/status/<component>:<id>` when a significant change occurs.
**Nothing in the daemon subscribes to those topics** (verified by grep). Whether the firmware sets
the retain flag on them is **unknown**. If it does, Gen2 devices already implement this principle
for native components, at zero script heap. That would make a daemon restart need no device
sweep for switch and input state. Checking it is cheap, and the result is listed as experiment X1
under Open questions. The design does not depend on it.

## Proposed design

### 1. The rule (text for `AGENTS.md`)

Add a section **"Device-state publishing"** under "Design Philosophy", after "Resilience Rules for
Agents". The text is ready to paste:

> #### Rule 3: Devices push state; nobody polls devices
>
> Fresh device state reaches the daemon and the UI by push: device notifications on
> `<id>/events/rpc`, the device's own `<id>/status/...` and `<id>/online` topics, and retained
> topics the daemon publishes under `myhome/<domain>/<device-id>/status`. It never arrives by
> periodic RPC to a device.
>
> - **No timers that read device state.** Do not add a ticker, `hx-trigger="every ..."` or
>   retry loop that results in an RPC to a device to learn its state. Config *writes* that heal
>   drift (the hourly reconcile) are not state reads and are out of scope.
> - **No device RPC on the render path.** UI fragments render from daemon memory only. If a
>   value is not in memory, render it as unknown. Do not fetch it.
> - **One forced refresh is allowed, at daemon start.** Explicit user actions (a Refresh button,
>   `myhome ctl ...`) are allowed too. Neither is polling.
> - **Projection before publication.** If a value can be derived from an event the device
>   already emits, derive it in the daemon (zero device heap). A device-side `MQTT.publish` or a
>   new event field is a last resort. It is acceptable only for a value no event carries, and only
>   with a measured `mem_peak` before and after, on the target hardware. Pool Pro1 scripts are at
>   the heap limit (#474, #433).
> - **Retained topics carry their own age.** Every daemon-published status payload is JSON with
>   a `ts` field (Unix seconds, the time the state was determined). Consumers decide staleness
>   from `ts`, never from receipt time.
> - **Browsers cannot read MQTT.** The daemon relays state to pages by SSE invalidation plus an
>   htmx re-fetch (see `docs/design/682-retained-state.md`, "Relay pattern").
>
> Before proposing to retire a polling path, check whether it is the only source of some field.
> Before proposing a device-side publish, state the observed rate of the problem it solves
> (`events.db`).

Add a pointer under **Conventions** in `CLAUDE.md`, next to the resilience bullets:

> - **Device state is pushed, never polled**: no timers or render paths that RPC a device for
>   state; derive from events in the daemon; device-side publishes need a heap measurement. See
>   AGENTS.md "Rule 3".

### 2. Topic and payload convention

| Aspect | Convention |
|---|---|
| Topic | `myhome/<domain>/<device-id>/status`. `<domain>` is lowercase (`switch`, `pool`, `vacuum`). `<device-id>` is the device's own ID (`shellypro1-ec62608c0230`), never its name, because names change. |
| Flags | retained, QoS 1 (`mqtt.AtLeastOnce`, `retained=true`), as in the existing precedents. |
| Payload | One JSON object. Required: `ts` (number, Unix seconds, fractional allowed). Optional: `source`, one of `event`, `refresh`, `startup`, `user`. All other fields are flat and domain-specific, snake_case, the same names as the RPC result types where one exists (`PoolGetStatusResult` fields for pool). |
| Unknown values | The key is omitted. It is never set to `null` or to a default. "Absent" means unknown. That way a stale default cannot pose as a fact, which is the #589 failure class. |
| Change-only | Publish only when a field other than `ts` changed. Also publish once at startup after the forced refresh. |
| Removal | When a device is forgotten, publish an empty retained payload to clear the topic. That is standard MQTT retained-message deletion. |

Why `ts` is Unix seconds: devices stamp events in Unix seconds (`params.ts`), and `events.db`
stores `ts` the same way (verified, `events/storage.go`). One unit means no conversions in
queries.

Why the existing `myhome/occupancy` stays as it is: it has no device in its path and no `ts`.
Renaming it would break its device-script consumers. It is listed as an exception rather than
migrated.

Initial topics. They are produced by the follow-up PRs, not by this design:

- `myhome/switch/<id>/status`, for example
  `{"ts":1762250000.4,"source":"event","switches":{"0":{"output":true,"on":true}}}`. `output` is
  the raw relay. `on` is the logical state after the normally-closed polarity (`sswitch.OnValue`).
  `on` is omitted if polarity is unknown.
- `myhome/pool/<id>/status`, for example
  `{"ts":...,"pump_on":true,"speed":"eco","water_supply_active":false,"turnover_achieved":1.2,"turnover_target":2,"runtime_sec":5400}`.

### 3. The projection: where state lives in the daemon

Each domain keeps its state in memory, in the service that already sees the domain's events. The
daemon gets no new "state store" subsystem.

| Domain | Owner | Fed by (zero device heap) | Device contact that remains |
|---|---|---|---|
| Switch / input | device manager (`watch/mqtt.go` → `UpdateFromMqttEvent`) | `NotifyStatus` deltas `switch:N.output`, `input:N.state`; `NotifyFullStatus` | forced refresh at start; refresh on `cfg_rev` change; polarity KVS read when `kvs_rev` changes (cached) |
| Pool | `PoolNotices` (`myhome/daemon/pool_notices.go`) | `OnEvent`: `pool.water_supply_protected/restored`, `pool.pump_start{speed}`, `pool.pump_stop`, `switch.on/off` of the pool device | one `Input.GetStatus` + KVS reads at start; turnover KVS reads after `pool.pump_stop` (already done today for the notice) and on `kvs_rev` change |
| Vacuum | `vacuum.Service` (unchanged) | robot push | none new |
| Sensors (Gen1/BLU) | unchanged | already retained at source and replayed at connect | none |

On every change, the owner calls one helper. The helper (a) updates memory, (b) broadcasts an SSE
invalidation, and (c) publishes the retained topic if publishing is enabled. To keep the topic
format in one place, the helper is a small function in `myhome/mqtt`, for example
`PublishStatus(ctx, mc, domain, id string, payload map[string]any) error`, which stamps `ts`.

**Concurrency (inferred risk).** Today `pkg/shelly` `Device.Refresh` replaces `d.status`
wholesale, with no lock seen. Merging `NotifyStatus` deltas from the watcher goroutine while a
refresh runs is a data race. The implementer must merge under a lock owned by the device, or
copy-on-write the status pointer. The PR must run its test with `-race` (default in `make test`).

### 4. Relay pattern: daemon state to browser (defined once)

Browsers cannot reach the broker (verified, TCP listener only). So retained MQTT state is
relayed as follows:

1. **Fragments render from memory only.** Every `/htmx/<domain>...` handler reads the domain's
   projection, or the device cache, and performs no device RPC. A missing value renders as
   unknown (`--`).
2. **SSE carries invalidations, not views.** When a projection changes, the owner broadcasts
   `event: <domain>-update` with `data: {"id":"<entity-id>","ts":<unix>}`. The existing
   `device-update` already follows this shape in practice: the client uses only `.id` and
   re-fetches the card. It keeps its current full-view payload for compatibility, but new
   events carry only `id` and `ts`.
3. **The client maps each event to one htmx GET** of the entity's fragment, and swaps it in
   place. A section that is not visible marks itself dirty and re-fetches when shown (detailed
   in `683-kiosk-shell.md`).
4. **No replay, no event IDs.** After an SSE (re)connect, the client re-fetches the visible
   fragments. Fragments render current memory, so a missed invalidation costs nothing.

This keeps markup in Go templates, the existing pattern that `htmx_*_test.go` tests. It adds no
client-side rendering of JSON, and it lets the broadcaster stay a plain fan-out.

The retained MQTT topic and the SSE event are two outputs of the same state change. The MQTT
topic serves non-browser consumers (device scripts, `ctl`, debugging with any MQTT client) and,
optionally, a warm start (see Scope questions). The SSE event serves pages.

### 5. Daemon liveness for the offline banner

**Mechanism: an SSE heartbeat event plus a client-side timeout.** This is the smallest option
that the browser can actually observe.

- **Server (`internal/myhome/ui/sse.go` `ServeHTTP`).** Each connection's loop gets a
  `time.Ticker` at 15 s. On each tick it writes
  `event: ping\ndata: {"ts":<unix>,"started":<daemon start unix>,"version":"<ver>"}\n\n` and
  flushes. One ping is also sent right after `connected`. The stream also starts with
  `retry: 3000\n\n`, which sets EventSource's reconnect delay to 3 s. The ping is written by the
  connection goroutine itself, not through `broadcast()`, so it never counts toward slow-client
  eviction. `connected` (literal `ok`) and `eventlog` stay byte-identical. The CLI `events follow`
  ignores every event other than `eventlog` (verified, `myhome/ctl/events/events.go:223-226`).
- **Client (in the kiosk shell, `683`).** `lastSeen` is set to the current time on *any* SSE
  frame. The shell is offline when `EventSource.readyState !== OPEN` or when
  `now - lastSeen > 40 s`. A 5 s client interval checks this.
  - Daemon stopped or restarting: the TCP connection closes, so `onerror` fires and the banner
    shows within about 1 s.
  - Silent partition: the banner shows within 40 to 45 s.
  - The client does not rely on EventSource's own retry: when the watchdog expires, and on each
    tick while offline (backoff 3 s doubling to a 30 s cap), and when `readyState` is `CLOSED`
    (a non-200 answer, which EventSource never retries), it closes the stream and opens a new one.
  - On reconnect, a different `started` means the daemon restarted, so the client re-fetches all
    sections. A different `version` means the daemon was upgraded, so the client calls
    `location.reload()`. It does this only once the daemon is reachable again, so it cannot land
    on a browser error page.
- **Constants, not options.** 15 s, 40 s and 3 s are Go/JS constants. Making them config options
  would trigger the 4-file config rule for something nobody needs to tune.

**Cost.** One ticker per connected SSE client (the kiosk plus any open desktop tab), and about
120 bytes every 15 s per client. A side benefit: a dead client is detected on the next failed
write, instead of lingering until a real event arrives. The cost is that `ServeHTTP` gains a third
`select` arm, and the test needs an injectable interval, such as a package-level variable that the
test overrides.

## Polling-path inventory and follow-up issues

"Contacts a device" means the path ends in an RPC to a Shelly. Frequencies are from code. **No
rates were measured.** Each follow-up issue below says how to measure, as `CLAUDE.md` requires.

| # | Path | Contacts a device? | How often | Verdict |
|---|---|---|---|---|
| P1 | Event-triggered full refresh: `watch/mqtt.go:124` `dm.UpdateChannel() <- device` → `refreshOneDevice` → `Shelly.GetDeviceInfo` + `Shelly.GetComponents(config,status)` (+ `Sys.GetConfig` if no `sys`) | **Yes**, 2 to 3 RPCs | Every `NotifyStatus`/`NotifyEvent`/`NotifyFullStatus` from any `shelly*` source, at most 10 concurrent | **Re-scope** after the projection lands. Keep it for unknown devices and `cfg_rev` changes. |
| P2 | Round-robin refresh: `manager.go:644` `runDeviceRefreshJob` | **Yes**, same RPCs | 1 Gen2 device per `--refresh-interval` (default 1 min, `options.go:33`), so ~1440/day | **Keep for now.** It is the only status source after a restart for a device that emits nothing. Decide later. |
| P3 | mDNS browse: `zeroconf.go:103` sends every browsed entry to `UpdateChannel` | **Yes**, same RPCs | Per mDNS entry yielded; cadence **not traced** (browse runs on the daemon context; restart delay 7 s) | **Measure first.** |
| P4 | Hourly reconcile: `manager.go:698` `runReconciliationLoop` | Yes, HTTP | Every Gen2 device, hourly | **Keep.** It writes config to heal drift and reads no state. Out of scope. |
| P5 | `applyPoolStatus` → `pool.getstatus` → `Input.GetStatus` (uncached) + 3 KVS reads (5 min cache, cleared on `kvs_rev`) | **Yes**, every render | Every `/htmx/devices` and **every `/htmx/device/{id}` render, for any device** (verified, `htmx.go:145,177`), so every `device-update` SSE triggers it | **Retire** (pool projection). |
| P6 | `DeviceToView` fallbacks: `sswitch.OnValue` KVS read per switch per render; `GetSwitchesSummary` when `Config == nil` | Yes, on cache miss / no config | Per render × switches; miss after 5 min or after `kvs_rev` change | **Retire** from the render path. |
| P7 | `solar.claimerslist` → `PoolNotices.ActiveSpeed` KVS reads | Yes | Per call (CLI/RPC, not UI) | Fold into P5 (speed comes from `pool.pump_start{speed}`). |
| P8 | Vacuum panel `hx-trigger="load, every 5s"` | **No.** It reads daemon memory; the robot pushes. | 12/min per open tab | **Keep allowed.** Replace by SSE invalidation for the kiosk (low priority). |
| P9 | MQTT periodic reconnect (2 h) | No. It re-receives retained messages. | 12/day | Keep. |
| P10 | `device.refresh` button, `ctl ...` | Yes | User-initiated | Keep (explicit action). |

### Verified finding that changes priorities (P1)

`storeDeviceLoop` broadcasts `device-update` only when `SetDevice` reports `modified`
(`manager.go:636`). `Cache.SetDevice` returns the SQLite upsert's result (`cache.go:93`). That
upsert changes rows only when one of `mac, name, host, info, config_revision, config, room_id`
differs (`storage/db.go:243-249`). Switch output lives in the device's `status`, which is not
persisted. So **a switch flip triggers a full device refresh but no SSE push.** The UI shows the
new state only after the next full page or card load. That rests on one inference: that the
`info` and `config` JSON are byte-stable across a toggle. The reference guessed the opposite. The
first projection PR should confirm this with a test.

### Follow-up issues (one per path; problem statements, not solution mandates)

**A. "Every device notification triggers a full GetDeviceInfo + GetComponents poll of the
emitting device" (P1).** Each message on `+/events/rpc` from a `shelly*` device makes the
daemon send the device 2 to 3 RPCs (`watch/mqtt.go:124` → `refreshOneDevice`), even though
the notification payload usually *is* the new state, and that payload is discarded
(`// TODO: Update device from status`, `watch/mqtt.go:151`). Despite the cost, switch changes do not
reach the UI, because the refreshed state is not persisted, so `modified` is false and nothing is
broadcast. The refresh rate per device is unmeasured. Count `"Updating device from MQTT event"` log
lines over a day on the NAS (the journal is volatile, so sample live), or use the
`switch.*`/`*.change` rows in `events.db` as a lower bound. Retiring the refresh is wrong
until the payload is applied, because it is currently the only path that updates `Status()` on an
event. Config changes (`cfg_rev`) and unknown devices still need a refresh.

**B. "Decide what the 1-minute round-robin device refresh is for" (P2).** `runDeviceRefreshJob`
refreshes one Gen2 device per minute, forever: ~1440 device polls a day, a full cycle every N
minutes. After a daemon restart, it is the only thing that fills switch state for a device that
stays quiet. It also picks up name, config and IP changes made outside MyHome. Once projections
and a startup sweep exist, it may be redundant as a *state* source, but still useful as an
*inventory* check, possibly far slower. "Keep it" is a valid outcome. The issue should first list
which fields reach the cache only through this job.

**C. "How often does mDNS browsing trigger a full device refresh?" (P3).** Every entry from
the zeroconf browser is pushed to `UpdateChannel` (`zeroconf.go:103`), which is a full refresh.
The browse runs on the daemon context and restarts after a 7 s delay when it stops. How often
entries are re-yielded per device is not known from the code. If the answer is "once per
announcement", there is nothing to do. If it is "on every re-query", this is a hidden poll of
the whole fleet. Measure with the `"Browsed"` log line count per device over an hour.

**D. "Pool status is read live from the pool device on every card render" (P5, P7).**
`applyPoolStatus` runs on every `/htmx/devices` and every single-card render *for any device*.
Each run is an uncached `Input.GetStatus` over MQTT to the pool Pro1 plus up to 3 KVS reads,
and failures are swallowed. The Pro1 runs at the heap limit. The water-supply state is already
carried by `pool.water_supply_protected/restored` events and by `input:0` in `NotifyStatus`.
The pump state and speed are carried by `switch.*` and `pool.pump_start{speed}`. Only turnover
needs KVS. `PoolNotices` already receives all these events. The startup value of the water-supply
input is unknown until its first edge, so one read at start is needed.

**E. "Device cards can trigger device RPCs at render time" (P6).** `DeviceToView` calls
`sswitch.OnValue` (a KVS read on cache miss) per switch per render, and falls back to a live
`GetSwitchesSummary` when the cached device has no config (`template.go:284,295-309`).
`parseOnValueIndexed` can also panic on a malformed value (NOTE in code). Rendering should be
a pure function of memory. Showing "unknown" for a device not yet refreshed is honest; a
render-time RPC is not. Check that the polarity resolved at refresh time is invalidated on
`kvs_rev`, which is already observed.

**F. "Vacuum panel polls the daemon every 5 s" (P8).** It never reaches the robot, so it is
not a breach of the principle. On a kiosk it keeps 12 requests a minute flowing while the section
is hidden, and it differs from every other section's update path. The vacuum service knows when
robot state changes and could emit a `vacuum-update` SSE invalidation instead. Low priority;
"keep it" is acceptable.

No issue for P4, P9 and P10 (keep, with reasons in the table).

## Alternatives considered

1. **Device-side retained publishing from scripts** (each script `MQTT.publish`es its state
   retained). In its strongest form, the state survives any daemon outage, the device is the
   single source of truth, and the principle's title says "devices publish". **It loses on the heap
   driver.** Each publish allocates a topic and a payload string on a device at the heap limit,
   and most of the fields are already in events the device emits. It is kept as the documented last
   resort.
2. **Subscribe to the firmware's `<id>/status/<component>` topics instead of a projection.** In its
   strongest form, it costs zero heap, the payload is the complete component status, and, if
   retained, the state is warm at daemon restart. **It does not lose on merit; it loses only on
   evidence.** The retain flag is unverified, and it covers only native components (not pool
   turnover or script state). It is recorded as experiment X1. If X1 is positive, the switch
   projection should consume these topics and P2 can likely go.
3. **Browser-visible MQTT (WebSocket listener on the broker) plus an MQTT LWT for liveness.** In its
   strongest form, the browser subscribes to retained topics directly, the relay code disappears,
   and LWT gives a standard liveness signal. **It loses on scope and security.** It adds a new
   listener on the embedded broker and needs the external NAS broker reconfigured. It adds an MQTT
   client library to the front end, and it exposes an allow-all broker to every browser on the LAN.
   Also, LWT fires only when the *broker* notices the daemon has gone, which says nothing about
   the HTTP path the kiosk depends on.
4. **Liveness by polling `/_health` every N s.** It is simple and independent of SSE. **It loses
   because it duplicates a channel the kiosk already holds open**, and it disagrees with that
   channel in edge cases: health is OK while SSE is wedged, so the page looks live but receives
   nothing.

## Consequences

- Better: the render paths of all sections become pure and testable without devices. The Pro1
  stops receiving render-driven RPCs. Switch state reaches the UI, which it does not today. The
  kiosk gets a defined offline signal.
- Worse: the daemon now holds derived state that can drift from the device if an event is lost.
  Under QoS 0 notifications and the #517 drop-on-full queues, that is possible. Mitigations: the
  forced refresh at start, P2 while it is kept, and `ts` on every payload. The rate of lost
  edges is measurable in `events.db` (the #523 method) and should be checked before anyone argues
  for more polling.
- Harder to change later: the topic names become a contract for any external consumer. The
  removal rule (an empty retained payload) must be followed, or ghosts persist on the external
  broker.

## Cross-cutting concerns

- **Security:** not affected beyond today's LAN trust. New retained topics are readable by any
  broker client, like the existing ones. They contain no secrets.
- **Privacy:** not affected (switch and pool state are already in `events.db`).
- **Observability:** each projection update logs at V(1). Retained topics make the state
  inspectable with any MQTT client. The ping makes SSE health visible in browser devtools.
- **Failure modes:**
  - Daemon down: devices are unaffected (Rule 2). Retained topics on the external broker keep
    their last `ts`. The kiosk shows its banner.
  - Broker down: projections freeze and pings continue. The UI shows data that is not marked
    stale. This is a known gap (Open question 3).
  - Internet down: not affected. No path here touches the internet.
- **Rollout / migration:** each PR is independently revertible. No schema change. Retained topics
  are additive. If a projection is wrong, reverting its PR restores the polling path.

## Impact on existing conventions

- **Extends** `AGENTS.md` Design Philosophy with a third rule. It **bends** "Decentralized: no
  central device manager maintaining persistent state". Projections are central *derived* state,
  in memory only, rebuilt from events and one startup sweep, and never authoritative over the
  device.
- **Extends** the "Add a retained status topic" recipe (architecture reference §9) with the
  naming, `ts` and absent-means-unknown rules.
- `myhome/occupancy` and `myhome/energy/...` do not follow the new naming. They are grandfathered,
  not migrated.

## PR decomposition

Each PR is reviewable in one sitting and must not change the RPC verbs and signatures, the
`connected`/`eventlog` SSE payloads, or any device script.

1. **Rule text.** Files: `AGENTS.md`, `CLAUDE.md`, this document. Acceptance: Rule 3 text present,
   with the pointer in place. Verification: review.
2. **SSE heartbeat.** Files: `internal/myhome/ui/sse.go`, `sse_test.go`, `server.go` (pass the start
   time and version). Must not change: the existing event bytes. Acceptance: a test with an
   injected 10 ms interval reads `connected`, then `ping` frames with `started`/`version`; an
   existing-client test still passes. Verification: `curl -N http://<host>:6080/events` shows a
   ping every 15 s. Degraded mode: none (server-only).
3. **Switch projection, apply and broadcast.** Files: `myhome/daemon/watch/mqtt.go`,
   `myhome/devices/impl/manager.go`, `pkg/shelly` device status setter (lock), tests using
   `pkg/shelly/mqtt/testdata` captures. Must not change: the refresh-on-event behaviour (still
   there in this PR). Acceptance: a test proves that a `NotifyStatus` with `switch:0.output` changes
   `Status()` and broadcasts `device-update`, with the merge race-free under `-race`. Verification:
   flip a test switch and see the card change within 2 s.
4. **Stop the refresh on status-only notifications** (follow-up A outcome). Files:
   `watch/mqtt.go`. Acceptance: notifications without a `cfg_rev` change, and not
   `NotifyFullStatus`, do not enqueue a refresh; unknown devices still do. Verification: the log
   count of refreshes per hour before and after, posted on the issue.
5. **Pool projection** (follow-up D). Files: `myhome/daemon/pool_notices.go`, `pool_rpc.go`,
   `solar_rpc.go`, `internal/myhome/ui/template.go`, `htmx.go` (call `applyPoolStatus` only for the
   pool device's card). Must not change: the `PoolGetStatusResult` shape, and the
   `pool.turnover_today` notice. Acceptance: rendering `/htmx/devices` 100 times issues 0
   `Input.GetStatus` calls (counting device-caller fake). Degraded mode: if the pool device is
   offline at start, the fields stay unknown until the first event, which is what happens today
   after an error.
6. **Device-free `DeviceToView`** (follow-up E). Files: `template.go`, `sswitch/service.go`
   (polarity resolved at refresh), tests. Acceptance: `DeviceToView` with a device-caller that
   fails the test on any call renders without error.
7. **Retained publication** of `myhome/switch/<id>/status` and `myhome/pool/<id>/status`. Files: a
   `PublishStatus` helper in `myhome/mqtt`, call sites from PRs 3 and 5. Acceptance:
   `RecordingMockClient.Published(topic)` shows a retained payload with `ts`, change-only, and an
   empty payload on `device.forget`. Verification: `mosquitto_sub -v -t 'myhome/+/+/status'` on
   the NAS broker.
8. *(Optional, follow-up F)* **Vacuum SSE invalidation.** It may instead land with the kiosk vacuum
   section.

Order: 1 and 2 first and independent (2 unblocks the kiosk banner). Then 3, then 4 (gated on
the measurement in A). 5 and 6 can run in parallel with 3. 7 comes last.

## Confirmation

- Review question for every UI or daemon PR: "Does anything here read device state on a timer
  or during rendering?" (Rule 3).
- Tests: the PR 5 and PR 6 counting fakes make "no device RPC on render" a regression test, not a
  review hope.
- Live check after PR 5: with the kiosk open for 1 h on the Home section, the pool device's
  `Input.GetStatus` count is 0. Measure it from daemon debug logs.

## Scope questions for the owner

1. **Publish projections retained now, or keep them in memory until a second consumer exists?**
   The UI needs only memory plus SSE. Retained topics add value for `ctl`, scripts, debugging and a
   possible warm start, at the cost of a public naming contract. *Recommendation:* define the
   convention now (done here) and implement publication last (PR 7). Drop PR 7 if no consumer
   materialises.
2. **Warm start from our own retained topics at daemon restart?** This means subscribing to
   `myhome/+/+/status` once at start and seeding projections, accepting values by `ts`. It cuts
   the post-restart "unknown" window, but can show state that changed while the daemon was down,
   honestly dated by `ts`. It works only with the external broker (the embedded one is in-memory,
   inferred). *Recommendation:* not now. The kiosk keeps its own last-known view across restarts
   (`683`).
3. **New helper in `myhome/mqtt` vs inline publishes at each call site.** Inline is smaller today.
   A helper keeps the topic format in one place. *Recommendation:* the helper (one function, no new
   package).

## Open questions

1. **Experiment X1 (new): are the firmware's `<id>/status/<component>:<id>` messages retained?**
   Run `mosquitto_sub -v -t 'shellyplus1-<id>/status/#'` against the NAS broker *after* the device
   has published. Any output on connect means retained. It is read-only and needs no device
   authorization. If yes, alternative 2 replaces most of PR 3, and follow-up B probably closes as
   "retire".
2. **#688 (existing): does a device re-emit `NotifyFullStatus` after its MQTT reconnects?** If
   yes, the forced refresh at daemon start could shrink to "devices that stayed silent".
3. **Should the ping carry `mqtt_connected`** (`mc.IsConnected()`; `mc` is already passed to
   `ui.Start` and unused)? The kiosk could then show "daemon up, broker unreachable, data may be
   stale". That needs owner-chosen wording.
4. **The lost-edge rate.** Before follow-up A retires refreshes, query `events.db` for
   unpaired `switch.on`/`switch.off` per device per day, excluding restart days (the #523 method),
   so that the projection's drift risk is a number.
