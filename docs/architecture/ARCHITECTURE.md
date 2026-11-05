# MyHome architecture reference

Living snapshot of what the system *is*, rewritten in place on every documentation run. Read from
files only (no code was executed). Claims not confirmed in code or config are tagged *inferred*.
Written as input for the kiosk-UI campaign (GitHub #681), so the web UI, the RPC system, MQTT, and
the places that touch devices get the deepest treatment.

## 1. Context and scope

MyHome is a self-hosted, LAN-only home-automation system for [Shelly](https://www.shelly.com/)
devices, written in Go as a hobby project (goals: learn Go, learn Claude Code, automate the house;
`AGENTS.md` "Project Goals"). One binary, `myhome`, is both the **daemon** (`myhome daemon run`) and
the **device-control CLI** (`myhome ctl ...`).

```
  Shelly Gen2/Gen3 --MQTT--+                      +-- browser (HTTP :6080: HTML, htmx, SSE, /rpc)
  Shelly Gen1 (shellies/#) +--> MQTT broker <-----+-- myhome daemon  --HTTP--> Shelly devices (:80 reverse proxy, config reconcile)
  Shelly BLU (via gateway) +    (external, or     |        |  \--> SFR box (LAN host list, HTTP)
  iRobot Roomba -- own TLS/MQTT, port 8883 -------+--------+   \--> Beem API, SMTP, iRobot cloud (optional, internet)
                  (direct to daemon, not via broker)        \--> SQLite: myhome.db, events.db
  myhome ctl (CLI) --MQTT RPC on <instance>/rpc--> daemon
```

- **Users/consumers**: the owner (browser UI, CLI), Shelly device scripts that run on the device and
  consume retained MQTT topics the daemon publishes, Prometheus (:9100) and pprof (:6060).
- **External systems**: MQTT broker (embedded Mochi broker when `--mqtt-broker` is not given;
  otherwise an external one, e.g. `tcp://192.168.1.2:1883` per `CLAUDE.md`), Shelly devices, SFR
  router, Beem Energy cloud, SMTP, iRobot cloud (room names only) and the robot itself.
- **Design principles** (README, `AGENTS.md` "Design Philosophy"): cloud-independent, decentralized,
  broker is the only required central piece, and two review-enforced rules: *internet-optional*
  and *daemon-optional per device* (each device keeps working with the daemon down).

## 2. Quality attributes it is built for (evidenced only)

| Attribute | Evidence |
|---|---|
| Local-first / internet-optional | `AGENTS.md` Rule 1; `cmd/fetchasset` has `-timeout 20s` and `-offline-ok` (cached asset accepted if sha256 matches); Beem/iRobot cloud are optional and gated on credentials (`myhome/daemon/run.go`, `myhome/vacuum/sync.go` `syncTimeout=90s`). |
| Daemon-optional devices | Logic lives in device scripts (`internal/shelly/scripts/*.js`); daemon-side pool code is read-only/notice-only (`myhome/daemon/pool_notices.go`). Retained MQTT topics let scripts start without the daemon. |
| Resilience of the MQTT link | Paho `AutoReconnect`, `ResumeSubs`, `CleanSession=false`, keepalive 30 s, ping timeout 10 s (`myhome/mqtt/client.go:207-215`); watchdog every 30 s that panics the process after 3 consecutive failed checks so systemd restarts it (`client.go:391-499`, consts in `myhome/ctl/options/options.go`); optional periodic disconnect/reconnect (default 2 h) to re-receive retained messages (`MQTT_RECONNECT_INTERVAL`, `client.go:411-481`); slow-subscriber drop counters instead of unsubscribing (`client.go:801-833`). |
| Operability | Packaged systemd unit restarts the daemon every 6 h (`RuntimeMaxSec=21600`, per `CLAUDE.md` "NAS operational facts"); pprof :6060; Prometheus exporter :9100; accounts-status panel (`internal/myhome/accounts`); per-package hlog loggers. |
| Low device heap | Shelly scripts constrained to ~23 KB heap; `docs/shelly-heap-allocation.md`; goja emulator tests cannot prove it (`AGENTS.md` kill list). |
| Test-suite cost control | Explicit `-timeout` derived from measurement in `Makefile` (`TESTTIMEOUT ?= 674s`), `-race` on by default, coverage gate (`.coverage-min`, `scripts/check-coverage.sh`, `.github/workflows/test.yml`). |
| Portability | Builds for linux/arm64 (Pi/NAS, `build-pr-deb` job), Windows MSI (`package-release.yml`), macOS dev; pure-Go SQLite (`modernc.org/sqlite` in `myhome/events`, driver name `"sqlite"`, `storage.go:13,61`; `ncruces/go-sqlite3` appears in the `ui` module graph). |
| Security | Weak by design on LAN: embedded broker uses `auth.AllowHook` (anyone connects, `myhome/mqtt/server.go:65`); UI has no authentication (no auth code in `internal/myhome/ui`); secrets only via env/.env/YAML, not flags (`run.go` comments). |

## 3. System shape

### 3.1 Go workspace
`go.work` (go 1.25.0, toolchain go1.25.3) lists the root module plus **60 sub-modules** (61 `use`
entries; `CLAUDE.md` says "~45", stale). All are tested by `make test`. New sub-module: `go work use <dir>`.
Top-level groups:

- `myhome/` (binary `myhome/main.go`, cobra root; adds `daemon.Cmd`, `ctl.Cmd`, `version.Cmd`).
  - `myhome/daemon` startup wiring + Solar/Pool RPC glue; `myhome/daemon/watch` MQTT/zeroconf device discovery.
  - `myhome/devices`, `myhome/devices/impl` (device cache + `DeviceManager`), `myhome/storage` (devices table in `myhome.db`).
  - `myhome/mqtt` (client + embedded Mochi broker + `RecordingMockClient`), `myhome/events` (events.db), `myhome/temperature` (rooms/schedules), `myhome/occupancy`, `myhome/vacuum`, `myhome/fetchproxy`, `myhome/notice`, `myhome/notify`, `myhome/metrics`.
  - `myhome/ctl/...` many CLI sub-modules (pool, garden, heater, shelly, temperature, events, vacuum, ...).
- `internal/myhome/` (module): RPC types/verbs (`const.go`, `methods.go`, per-service type files), RPC `server.go`/`client.go`, `device.go`; sub-packages `ui`, `proxy`, `accounts`, `shelly/{gen1,gen2,blu,script,setup,sswitch}`, `energy`, `net`, `sfr`, `mdnsrouter`, `model`.
  - `internal/myhome/ui` is **its own module** (has `go.mod`), as are `internal/myhome/blu` and `internal/myhome/net`.
- `internal/shelly/scripts/` (module): the Shelly JS sources (`pool-pump.js`, `heater.js`, `garden.js`, `watchdog.js`, BLU scripts, ...), embedded via `scripts.go`, plus goja-based emulator tests.
- `pkg/` generic libraries: `pkg/shelly/*` (RPC, KVS, MQTT/HTTP channels, script upload), `pkg/roomba` (iRobot local client), `pkg/beem`, `pkg/sfr`, `pkg/tapo`, `pkg/devices`, `pkg/version`.
- `hlog/` custom logger (`hlog.GetLogger("pkg/name")`), `cmd/fetchasset`, `cmd/datacollector`, `tools/` (genconfigschema, classify-events).

### 3.2 Three-tier layer rule (`CLAUDE.md`)
```
myhome/ctl/shelly/       CLI only: cobra, fmt.Printf, flags
internal/myhome/shelly/  MyHome business logic: workflows, policies
pkg/shelly/              generic Shelly API: RPC, script ops, MQTT/HTTP channels
```
No business logic in `myhome/ctl/`; no MyHome-specific code in `pkg/shelly/`; shared CLI helpers go in
`internal/myhome/` to avoid import cycles. Enforcement is by review; no lint rule found. Example of
the seam being kept: `pkg/shelly` re-resolves hosts through a `types.HostResolver` installed by the
daemon (`myhome/devices/impl/manager.go:367,557-576`) instead of importing `internal/myhome`.

### 3.3 Binary and modes
- `myhome daemon run` (cobra: `daemon.Cmd` -> `runCmd`, `myhome/daemon/run.go`) reads flags, env (`MYHOME_*`), `myhome.yaml` (searched in `.`, `/etc/myhome/`, `$HOME/.myhome/`) via viper, then `daemon.Run()`. (`CLAUDE.md`/README say `myhome run`; the actual subcommand is `daemon run`.)
- `myhome ctl ...` talks to the daemon through MQTT RPC (lazy MQTT connect) or directly to devices.
- Ports: 6080 UI (`--ui-port`, `-p`), 6060 pprof, 9100 Prometheus, 8888 Gen1 HTTP->MQTT proxy, 8889 mentioned in an occupancy log line, 1883 broker.

### 3.4 Daemon wiring order (`myhome/daemon/daemon.go` `Run`)
1. Instance name -> `myhome.InstanceName` (RPC topic prefix, default `myhome`, line 94).
2. pprof (:6060); embedded Mochi broker unless `--mqtt-broker` given (97-143).
3. MQTT client created (`NewClientE`, lazy `GetClientE`), wrapped into `ctx` once (`shellymqtt.NewContextWithClient`, line 163), `shelly.Init(...)`.
4. Optional: Gen1 proxy, Occupancy service, Prometheus exporter, Beem watcher + Solar aggregator (retained publish), `accounts.Registry`, SFR status reporter.
5. If the device manager is enabled (default): SQLite device storage -> `ui.NewSSEBroadcaster` -> events service (broadcast hook fans out to SSE, notice service, `PoolNotices`) -> `impl.NewDeviceManager` + `Start` -> Gen2 listener -> `myhome.NewServerE` (the single RPC server) -> `event.list`, temperature, fetchproxy, vacuum, occupancy, pool, solar RPC handler registration -> `mc.Start()` (connects; queued subscriptions fire, retained messages delivered) -> `$SYS/clients/#` subscription -> **`ui.Start(...)` (line 570)**.
6. Then optional MQTT connection monitor ticker (accounts "mqtt" status) and wait on `ctx.Done()`.

**The UI server only starts when the device manager is enabled** (it is inside `if !disableDeviceManager`, `daemon.go:283-577`). Events, occupancy, temperature services are auto-enabled by the same switch (`run.go:204-242`).

## 4. Boundaries and dependencies

- **RPC is the only service boundary.** Every cross-component action goes through `myhome.Methods(verb).ActionE(ctx, params)` (in-process) or the MQTT RPC (`<instance>/rpc`). The UI never touches storage/services directly except `DeviceRegistry` (device manager) and `events.Service.Store()`.
- **Handlers are a package-level map** (`internal/myhome/methods.go:41`, unsynchronised; tests must not `t.Parallel()` and must restore in `t.Cleanup`).
- **UI <-> daemon packages**: `internal/myhome/ui` imports `internal/myhome` (RPC types), `myhome/events`, `myhome/mqtt`, `internal/myhome/{accounts,proxy,net,shelly/sswitch}`, `pkg/shelly`. In the other direction `myhome/devices/impl` imports `ui` directly for `ui.DeviceView` / `ui.DeviceToView` (`manager.go:22,155,266,638`); the Gen1/BLU listeners use small local `SSEBroadcaster` interfaces. `internal/myhome/proxy` imports `ui/assets` and `ui/static`.
- **Browsers never speak MQTT.** The embedded broker adds only a TCP listener (`myhome/mqtt/server.go:71-79`); no WebSocket listener found. Anything "fed by retained MQTT" for a page must be consumed by the daemon and relayed via HTTP/SSE.
- **Device reach paths**: MQTT RPC (`<device-id>/rpc` topic, `pkg/shelly/device.go:97`), HTTP to the device IP (host resolved lazily: SFR router chain -> mDNS -> not found), Gen1 via `shellies/#`, BLU via `shelly-blu/events/+`.

## 5. The UI in depth (`internal/myhome/ui`, module `.../internal/myhome/ui`)

### 5.1 Files
| File | Role |
|---|---|
| `server.go` | `Start(ctx, log, port, resolver, db DeviceRegistry, mc, sse, eventsSvc, upstreamProxy, accountsRegistry)`: builds the mux, starts `http.Server`, shuts down on ctx cancel (3 s). `mc` is accepted but only logged (never used inside `Start`). |
| `template.go` | `//go:embed static/*` (`staticFS`), `DeviceView` struct, `DeviceToView`, `applyPoolStatus`, `RenderIndex`, `RenderEventLog`, `DeviceRegistry` interface. `init()` parses `static/index.html` with **`text/template`** (not `html/template`) and the event-log page. |
| `htmx.go` | `HTMXHandler` + all partial fragments as Go string constants (`deviceCardsTemplate`, `deviceCardTemplate`, `roomsListTemplate`, `accountsPanelTemplate`, `switchButtonTemplate`) parsed per request with `html/template`. |
| `htmx_vacuum.go` | Vacuum panel/action handlers + `vacuumPanelTemplate`. |
| `events_template.go` | Events fragments (`eventsTableTemplate`, `eventsRowsTemplate`, `eventRowTemplate`), the **whole event-log page as a Go string** (`eventLogPageHTML`, with its own inline Alpine/JS), and `eventTemplateFuncs`. |
| `sse.go` | `SSEBroadcaster` (clients map, per-client buffered chan of 10, evict after 10 consecutive full-channel skips). Context helpers `NewContextWithSSE`/`SSEFromContext` exist (no callers found besides definitions). |
| `rpc.go` | `POST /rpc` JSON bridge `{method, params}` -> `myhome.Methods(...).ActionE`; wraps result as `{"result": ...}`; converts `device.show/lookup/listbyroom` results to `DeviceView`. |
| `static/index.html` | The single-page dashboard (Bulma + Alpine + htmx), ~820 lines incl. inline `app()` Alpine component and 3 modals (room, setup, heater). |
| `static/main.go` | `go:embed` + `Assets` map (`/static/...` path -> bytes + content type) and the three `go:generate fetch` lines. |
| `static/myhome.css`, `penates.svg`, `manifest.json` | Embedded/served, but `myhome.css` is linked by no template (see debt). |
| `assets/ws-patch.{go,js}` | WebSocket rewrite shim injected into proxied device pages (used by `internal/myhome/proxy`, not by the dashboard). |

### 5.2 Routes (all registered in `server.go:39-108`)
| Route | Handler | Notes |
|---|---|---|
| `/` (catch-all) | inline | `path==""` -> `RenderIndex`; `path=="event-log"` -> `RenderEventLog`; **anything else -> `proxy.Handle`**, which serves `/_health`, `/_ws_patch.js`, `/bulma.min.css`, `/devices/{token}/...` (reverse proxy to the device on port 80/443, or to `--remote-proxy`) and 404s the rest. A new top-level page path (e.g. `/rooms`) must be registered on the mux or it falls into the device proxy and 404s. |
| `/events` | `SSEBroadcaster.ServeHTTP` | SSE; see 5.4. |
| `/rpc` | `RpcHandler` | in-process RPC bridge, JSON in/out. |
| `/static/` | inline lookup in `static.Assets` | exact-match map; `Cache-Control: public, max-age=31536000, immutable`. A file not in the map 404s even if embedded. |
| `/htmx/devices` | `DeviceCards` | all cards, sorted by lowercase name. |
| `/htmx/device/{id}` | `DeviceCard` | single card (used after SSE `device-update`). |
| `/htmx/rooms` | `RoomsList` | calls `temperature.list`. |
| `/htmx/switch/toggle` | `SwitchButton` | POST; calls `switch.toggle`; **no template in the repo targets it** (cards use JS `toggleSwitch` -> `/rpc`). |
| `/htmx/events`, `/htmx/events/more` | `EventsTable`, `EventsMore` | `events.Storage.Query` directly (not via RPC); fixed window `Since: 24h`, `Limit: 50`, offset pagination. |
| `/htmx/accounts` | `AccountsPanel` | `accounts.Registry.Snapshot()`. |
| `/htmx/vacuum`, `/htmx/vacuum/action` | `VacuumPanel`, `VacuumAction` | `vacuum.*` RPC; action re-renders the panel. |
| `/event-log` | via `/` | separate full page (not a fragment of index). |

There is **no `/health` UI route** (comments in `internal/myhome/accounts/registry.go` mention `/health`; only the proxy's `/_health` exists).

### 5.3 How each current UI section gets its data
Page load: `GET /` renders a shell only (`IndexData{Version}`); **device data is not rendered server-side** (`template.go:360-383`, #390). Each section self-loads via htmx.

| Section (index.html) | Trigger | Server source | Reaches a device? |
|---|---|---|---|
| Devices grid (`#devices-container`) | `hx-trigger="load"` once | `DeviceCards`: `db.GetAllDevices` (daemon's in-memory `Cache`, backed by `myhome.db`) -> `DeviceToView` per device -> `applyPoolStatus` | **Yes, indirectly** (see below). |
| Vacuum (`#vacuum-container`) | `load, every 5s` (index.html:95-99) | `vacuum.list` + one `vacuum.rooms` per robot; reads the service's in-memory robot state and the SQLite region table | **No.** The robot pushes status to the daemon over its own long-lived MQTT connection; polling only reads the daemon cache (comment at `htmx_vacuum.go:14-16`). Vacuum actions (`vacuum.command/clean`) do send to the robot. |
| Accounts | `load` once | in-memory registry | No. |
| Rooms (`#rooms-list`) | `load, roomsUpdated from:body` | `temperature.list` (SQLite config only: id, name, kinds, levels; **no live temperatures**) | No. Alpine `loadRooms()` also calls `room.list` on init. |
| Event log page | `load`; filter form `change`/`keyup delay:400ms` | `events.Store().Query` | No. |
| Live sensor values / door / device card refresh | SSE | see 5.4 | n/a |

Device card data (`DeviceToView`, `template.go:140-358`):
- **Switch on/off**: from `d.Impl().(*shelly.Device).Status()` = status captured by the **last `Shelly.GetComponents` refresh** (not live). Refresh happens (a) every `--refresh-interval` (default **1 min**) for **one** Gen2 device per tick, round-robin (`impl/manager.go:644-689`), and (b) after **every** `+/events/rpc` message from a `shelly*` source: `watch/mqtt.go` calls `dm.UpdateChannel() <- device`, which runs `refreshOneDevice` -> `Device.Refresh` -> `Shelly.GetDeviceInfo` + `Shelly.GetComponents(config,status)` against the device (`pkg/shelly/device.go:134-249`, concurrency capped at 10, `manager.go:61,458`). The `NotifyStatus` payload itself is not applied (`watch/mqtt.go:151` `// TODO: Update device from status`). Gen1 and BLU are skipped (updated only via MQTT).
- **"On" polarity**: `sswitchsvc.OnValue` -> `kvs.GetValue(... "normally-closed")` per switch per render (`template.go:284,303`). `kvs.GetValue` is cached per (device,key) for **5 min** including negative results (`pkg/shelly/kvs/cache.go:21`), invalidated when a NotifyStatus carries a changed `sys.kvs_rev` (`ObserveRevision`). A cache miss is a live KVS.Get RPC on the render path.
- **No-config fallback**: if `d.Config == nil` and an impl exists, `GetSwitchesSummary` is called live on the device at render time (`template.go:295-309`).
- **Temperature / humidity / window**: `d.Status.Sensors` in memory, filled only from Gen1 (`shellies/#`) and BLU (`shelly-blu/events/+`) MQTT listeners via `Cache.UpdateSensorValue` (not persisted, `devices/cache.go:293-336`). Retained messages replay these at (re)connect.
- **Pool pump card extras** (`applyPoolStatus`, `template.go:84-107`): called on every `DeviceCards` and `DeviceCard` render -> `pool.getstatus` RPC -> `PoolNotices.ComputeTurnover` (3 `kvs.GetValue` over MQTT: `script/pool-pump/runtime-sec`, `turnover-today`, `turnover`; 5-min cached) + **`Input.GetStatus` id 0, a live uncached RPC over MQTT to the pool device on every render** (`pool_notices.go:166-177`). Failure is swallowed (fields stay blank).
- Per-card buttons: web UI link (`/devices/{token}/` proxied), refresh (`device.refresh` RPC -> a device poll), setup modal (`device.setup`, `device.setroom`), heater modal (stub "demo").

### 5.4 SSE (`GET /events`)
One stream for all clients; event names (JSON `data`): `connected` (sent once, literal `ok`), `sensor-update` `{device_id,sensor,value}`, `device-update` (full `DeviceView`), `eventlog` (`events.Event` + `device_name`). Producers: Gen1/BLU listeners -> `sensor-update`; `DeviceManager.storeDeviceLoop` and `device.refresh/setup/update` -> `device-update`; `events.Service.Record` -> broadcast hook -> `eventlog` (also fans out to the notice service and `PoolNotices`). No keepalive/comment frames, no event ids or replay, no per-client filtering. Browser side: Alpine `connectSSE()` in index.html (`sensor-update` mutates DOM by element id; `device-update` triggers `htmx.ajax` GET `/htmx/device/{id}` swap `outerHTML` on the card, which **re-runs `DeviceToView` + `applyPoolStatus`**); the event-log page has its own `EventSource` that prepends every `eventlog` row regardless of the active filter. `onerror` only logs to the console (EventSource auto-reconnects; no UI state for "disconnected"). Events are recorded with `INSERT OR IGNORE` on a UNIQUE key but `Service.Record` broadcasts regardless of whether the row was a duplicate (`events/service.go:53-67`). Whether `storeDeviceLoop` broadcasts on every refresh depends on the `modified` flag returned by `db.SetDevice` (not traced; *inferred* that most refreshes report modified, since `pkg/shelly` `Device.Refresh` sets `modified = true` on success).

### 5.5 Front-end stack and assets
- Bulma 0.9.4, htmx 2.0.4, Alpine 3.14.3 are **downloaded at generate time** by `//go:generate fetch -url=... -sha256=...` lines in `static/main.go` using `go run ../../../../cmd/fetchasset` (pinned sha256; skipped if file already matches; offline accepted if cached file matches). The three files are **gitignored** (`.gitignore:70-75`) and `//go:embed`ed. `make generate` line 1 covers `./internal/myhome/ui/...` (explicit, because the ui dir is a sub-module).
- **No fonts, no icon fonts, no image assets other than `penates.svg`.** Icons are inline SVG in templates; emojis are used as glyphs. No build step for CSS/JS (no npm); custom styling is the inline `<style>` block (index.html:35-47) and Bulma classes.
- All custom JS is inline in `index.html` (`app()`), and a second inline component in `eventLogPageHTML`.
- Layout is Bulma `hero`/`section`/`columns` (`is-4-desktop`, `is-6-tablet`), responsive-desktop style; no left nav, no fixed viewport; page head includes PWA bits (`manifest.json` with `display: standalone`, `apple-mobile-web-app-*`).
- Only `static.Assets` entries are reachable at `/static/*` (`server.go:84-93`). `StaticFS()`/`StaticFileServer()` in `template.go` exist but no caller found.

### 5.6 How the UI is tested
`internal/myhome/ui/*_test.go` (all in package `ui`, unit level, no `httptest`, no live daemon):
- `htmx_vacuum_test.go`: renders `vacuumPanelTemplate` with hand-built `vacuumPanel` and asserts substrings (`strings.Contains`).
- `htmx_pool_test.go`: `cardTemplateFuncs` and `deviceCardTemplate` pool tags / web-UI link.
- `htmx_accounts_test.go`: `truncateError`, `toAccountRows`. `events_template_test.go`: template funcs.
- `sse_test.go`: broadcaster fan-out, slow-client eviction, unsubscribe-after-evict.
Not covered by tests (none found): `Start`/routing, `/rpc`, `DeviceToView`, `DeviceCards`/`RoomsList`/`EventsTable` handlers, `index.html`, JS behaviour, `applyPoolStatus`. Pattern to follow: templates are package-level string constants precisely so tests can parse and render them without a server.

## 6. RPC system and daemon wiring

- **Verbs**: `Verb` string constants in `internal/myhome/const.go` (device.*, temperature.*, room.*, switch.*, occupancy.getstatus, heater.*, thermometer.list, door.list, event.list, pool.getstatus, solar.claimerslist, fetch.*, vacuum.*).
- **Signatures**: `signatures` map in `internal/myhome/methods.go` (`NewParams`/`NewResult` factories). `RegisterMethodHandler(verb, MethodHandler)` **panics** if the verb has no signature (`methods.go:29-39`); handler type is `func(ctx context.Context, in any) (any, error)`.
- **Transport**: one topic `"<InstanceName>/rpc"` (`proxy.go:21`); replies on `"<client-id>/rpc"`. Server loop (`internal/myhome/server.go:45-126`) is a **single goroutine, one request at a time**; dialog validation, params re-unmarshal into the typed struct, `ActionE`, publish QoS 1 non-retained.
- **In-process callers** (UI handlers, `DeviceManager.CallE`) skip MQTT and call `ActionE` directly; the UI `/rpc` bridge runs under the daemon's root context (no per-request timeout).
- **Handler owners** (registration site): device.* + thermometer/door.list -> `impl.NewDeviceManager` (constructor, `manager.go:79-335`); heater.* -> `shellyscript.HeaterService` and switch.* -> `mhswitch.Service` (both at `DeviceManager.Start`); temperature.* and room.* -> `temperature.Service.RegisterHandlers`; vacuum.* -> `vacuum.Service`; occupancy.getstatus -> `occupancy` RPC handler; event.list -> inline closure in `daemon.go:421`; pool.getstatus -> `daemon/pool_rpc.go`; solar.claimerslist -> `daemon/solar_rpc.go`; fetch.* -> `fetchproxy`.
- **Optional services**: handlers for a disabled service are not registered, so `myhome.Methods(verb)` returns "unknown or unregistered method"; the UI treats that as "feature absent" (vacuum, pool, `applyPoolStatus`).
- Adding a verb is the documented 4-step procedure in `CLAUDE.md`/`AGENTS.md`; `AGENTS.md` shows a `func(params any)` handler signature and a `NewMethodHandlers` pattern that differ from the real code (see debt).

## 7. MQTT

### 7.1 Client (`myhome/mqtt/client.go`)
- Interface `Client`: `Subscribe` (payload chan), `SubscribeWithTopic` (Message chan), `SubscribeWithHandler`, `Publish(ctx, topic, payload, qos, retained, publisherName)`, `Publisher`, `Start`, `IsConnected`, `Close`, `DeviceServer` (loopback -> LAN IP for devices). Singleton `theClient`; subscriptions made before `Start()` are queued and applied in the `OnConnect` handler so retained messages reach handlers at connect (README "eager mode" for the daemon, lazy for CLI).
- One broker subscription per topic filter, fan-out to N subscribers via per-topic mutex; a full subscriber queue **drops that message** and counts it (`recordDrop`, reported every 5 min); the subscriber stays registered (#517).
- **Periodic reconnection** (`--mqtt-reconnect-interval`, config `daemon.mqtt_reconnect_interval`, default **2 h**, 0 disables): the watchdog disconnects, reconnects and re-subscribes every handler so the broker re-delivers retained messages (`client.go:411-481`). With `CleanSession=false` and a stable client id this is the existing mechanism for "refresh retained state".
- **No MQTT Last Will / birth message is set by the daemon** (no `SetWill` anywhere in the repo), and no daemon heartbeat topic exists.
- Embedded broker (Mochi, TCP :1883 only, allow-all auth, debug hook with `ShowPasswords: true`, mDNS-published as `_mqtt._tcp`). No persistence hook seen, so retained messages on the embedded broker are in-memory (*inferred*); the NAS uses an external broker per `CLAUDE.md` examples (*inferred*).

### 7.2 Topics the daemon consumes
| Topic | Consumer | Purpose |
|---|---|---|
| `<instance>/rpc` | `myhome.NewServerE` | RPC requests |
| `+/events/rpc` (Gen2 NotifyStatus/NotifyEvent/NotifyFullStatus) | `watch.StartMqttWatcher` (device refresh trigger), `shelly/gen2.Listener` (event recording incl. `switch.on/off`, `*.change`, BLU relays), `occupancy.Service` (input changes) | three independent subscriptions to the same filter |
| `+/online` | `gen2.Listener.handleOnline` | records `device.online/offline` events (published by the Shelly itself; `CLAUDE.md` counts ~164 online events/day from the 6-hourly restarts) |
| `shellies/#` | `shelly/gen1` listener | Gen1 sensor values + registration + events |
| `shelly-blu/events/+` | `shelly/blu` listener | BLU sensors |
| `shelly/metrics/#` (configurable) | `metrics.Exporter` | Prometheus |
| `$SYS/clients/#` | log only | broker monitoring |
| `myhome/fetch/subscribe` | `fetchproxy` | fetch-and-transform subscriptions |
| `myhome/heater/list`, `myhome/heater/show/...` | heater CLI request/response (client-side topics) | |

### 7.3 Retained topics the daemon publishes
| Topic | Publisher | Consumer |
|---|---|---|
| `myhome/occupancy` (`{occupied, reason}`) | `occupancy.Service` on every input event, every mobile-presence hit (SFR LAN poll every 5 min) and on expiry timer (12 h window) | device scripts (*inferred*) |
| `myhome/energy/solar/available` | `daemon.SolarAggregator` (sum of Beem sources; stale after 5 min) | `pool-pump.js` (`MQTT.subscribe`, line ~2414), tests |
| `myhome/energy/beem/power` | `pkg/beem.Watcher` (polls Beem REST every `beem.poll_interval`, default 60 s) | solar aggregator |
| `myhome/rooms/<room-id>/temperature/ranges` | `temperature.Service.PublishRangesUpdate` at startup and on room/schedule change | `heater.js` (references the topic) |
| `shellies/<id>/info|sensor/...` | `pkg/shelly/gen1` HTTP->MQTT proxy (:8888) | Gen1 listener |
| fetchproxy subscription topics (e.g. `myhome/fetch/<heater>/forecast-full`) | `fetchproxy` | device scripts |

Topics devices publish: `<id>/online`, `<id>/events/rpc`, `<id>/rpc` (request). **Nothing the daemon publishes today describes vacuum state, pool status, device switch state, or daemon liveness.**

### 7.4 Pool-pump events (device-emitted via `Shelly.emitEvent`, arrive on `+/events/rpc` as `NotifyEvent`, recorded by the gen2 listener; severity `notice` except `pool.fuse_tripped` = `warn`)
`pool.pump_start {speed, switch_id, reason}`, `pool.pump_stop {reason}`, `pool.water_supply_protected|restored {output}`, `pool.run_window {mode,...}`, `pool.fuse_tripped`, `pool.call_tracking_broken` (all in `internal/shelly/scripts/pool-pump.js`; severity table `gen2/listener.go:268-290`). The daemon adds `pool.turnover_today` (notice) on each `pool.pump_stop` (`PoolNotices.OnEvent`). Pool state mirrored to device KVS by the script: `script/pool-pump/{turnover,runtime-sec,turnover-today,active-output,...}`.

## 8. Domain services (short)

- **Pool**: provisioning is CLI-only (`myhome/ctl/pool`: add/update/setup/start/stop/purge/status; `internal/myhome/shelly/script/pool.go`; generated defaults `pool_defaults_generated.go` and `pool_kvs_generated.go` from `pool-pump.schema.json`). Daemon side is read-only: `PoolNotices` (needs `--enable-pool` + `--pool-device-id`), `pool.getstatus`, `solar.claimerslist`. `ctl pool status` reads KVS over MQTT per device and then calls `pool.getstatus`. One configured pool device system-wide.
- **Vacuum (#606/#626/#628/#630)**: `myhome/vacuum` supervises one robot (`pkg/roomba`, direct TLS MQTT to the robot, which allows a single local client), reconnect/rediscover loop (watch 30 s, 2 min down tolerance), status held in memory (`robot.status`), regions in SQLite (`vacuum_robots`, `vacuum_regions` in `myhome.db`), optional iRobot cloud room sync, optional ICMP wake probe. Enabled only with `MYHOME_VACUUM_BLID` + `MYHOME_VACUUM_PASSWORD`; otherwise `vacuum.list` is empty and the UI section renders empty. RPC: `vacuum.list|status|command|clean|rooms|roomname|roomsync`. Design docs: `docs/vacuum-plan.md`, `docs/vacuum.md`.
- **Rooms/temperature**: `myhome/temperature` holds room configs (id, name, kinds, levels), kind schedules and weekday defaults in `myhome.db`; "room" = heating/comfort configuration, with `Device.RoomId` assignment (`device.setroom`, `device.listbyroom`). `thermometer.list`/`door.list` classify devices by capability (MQTT topic per sensor). There is no daemon API that returns *live temperature per room* today; live values are per-device in `DeviceView`.
- **Events**: `myhome/events` -> `events.db` (SQLite WAL, single connection). Schema (`events/storage.go:95-124`): `events(id, ts, received_at, device_id, component, event, severity, data)` with `UNIQUE(device_id,component,event,ts)` and indexes on ts, device, event, severity; `sensor_daily_stats(date, device_id, component, metric, min/max/sum/samples)`. Default path `events.db` (relative, next to `myhome.db`); hourly purge to `--events-retention` (default 90 d); `event.list` RPC; SSE fan-out; notice service (needs events + occupancy). Event sources: gen2 listener, Gen1/BLU bridges, daemon-synthesised (`pool.turnover_today`, notices). `CLAUDE.md` calls it the long-term record to query before asserting a rate.
- **Occupancy**: `myhome/occupancy`: input-change events on `+/events/rpc` plus SFR box LAN poll (5 min) for mobile names (default `["iPhone"]`), publishes retained `myhome/occupancy`; `occupancy.getstatus` RPC.
- **Device manager** (`myhome/devices/impl`): in-memory `Cache` over `myhome.db`; loops: update channel (semaphore 10) -> `refreshOneDevice`; store loop; 1-min round-robin refresh; hourly (`--reconcile-interval`) HTTP reconcile of MQTT/NTP/Matter config on every Gen2 device (`runReconciliationLoop`); zeroconf discovery; auto-setup of unknown devices (disable with `--disable-auto-setup`).

## 9. Change-impact map

Dependency columns are derived from imports/wiring; *inferred* marks what was not traced.

| Module | Depends on | Depended on by |
|---|---|---|
| `internal/myhome` (RPC types, verbs, `Device`, client/server) | `myhome/mqtt`, `pkg/shelly`, `pkg/devices` | almost everything: daemon, ui, ctl/*, temperature, vacuum, occupancy, devices/impl. **Highest blast radius.** |
| `internal/myhome/ui` | `internal/myhome`, `myhome/events`, `myhome/storage` (go.mod), `internal/myhome/{accounts,proxy,net,shelly/sswitch}`, `pkg/shelly` | `myhome/daemon` (`ui.Start`, `NewSSEBroadcaster`), `myhome/devices/impl` (`ui.DeviceView`, `DeviceToView`), `internal/myhome/proxy` (imports `ui/assets`, `ui/static`) |
| `myhome/daemon` | everything service-level | `myhome/main.go` only |
| `myhome/devices/impl` | `ui`, `internal/myhome/shelly/*`, `myhome/events`, `myhome/mqtt`, `pkg/shelly` | `myhome/daemon`, `myhome/daemon/watch` (via `devices.Manager` interface) |
| `myhome/mqtt` | `myhome/ctl/options`, `internal/myhome/net`, paho, mochi, ristretto | every MQTT user (daemon, listeners, services, ctl) |
| `myhome/events` | sqlx/sqlite | `daemon`, `ui` (direct `Store().Query`), gen1/gen2/blu listeners, `notice`, pool notices |
| `myhome/temperature` | `internal/myhome`, `myhome/mqtt`, `myhome/storage` DB handle | `daemon`; heater scripts consume its retained topics |
| `myhome/vacuum` | `internal/myhome`, `pkg/roomba(/cloud)` | `daemon`, `ui` (via RPC only), `ctl vacuum` |
| `pkg/shelly/*` | none of `internal/myhome` (rule) | daemon, ctl, impl, ui (`DeviceToView`) |
| `internal/shelly/scripts` (JS + emulator tests) | `pkg/shelly/script` emulator | `internal/myhome/shelly/script` (embeds/uploads), release packaging |

Shared hot spots: `internal/myhome/methods.go` + `const.go` (every new verb), `myhome/ctl/options` flags struct (`options.Flags`, global), `myhome/daemon/daemon.go` (the single composition root), the package-level `methods` map, `myhome.db` (devices, temperature_*, vacuum_*, fetch_subscriptions tables share one DB handle), `DeviceView` (shared by SSE payloads and templates), `static.Assets` map, `kvs` 5-min cache (global).

### Recipes (what a change touches)

**Add a UI section / page**
1. Fragment handler on `HTMXHandler` (`htmx*.go`), template as a package-level string constant, route in the `server.go` mux (`/htmx/<x>`).
2. If it is a full page: register a mux route (otherwise `/` -> `proxy.Handle` 404s) and add a `Render*` function in `template.go` (index uses `text/template`, fragments `html/template`; a full-page template string like `eventLogPageHTML` is parsed in `init()`).
3. Data via `myhome.Methods(verb).ActionE` (preferred; keeps the UI free of storage imports) or `events.Store()`.
4. Live updates: add an SSE event name in `sse.go` (+ producer wiring in `daemon.go` / a listener) and an `addEventListener` in the page's inline JS.
5. Add the shell section/nav in `static/index.html` (and `eventLogPageHTML` if shared chrome is needed: nav is currently duplicated per page).
6. Tests: render the template constant in a `_test.go` (existing pattern); routing/handlers are untested today.
7. No existing viewport or layout rules target a fixed 1024x600 touch screen.

**Add an RPC method**: `Verb` in `const.go` -> params/result types in `internal/myhome/<service>.go` -> `signatures` entry in `methods.go` -> `RegisterMethodHandler` in the owning service, called from `daemon.go` (after device manager start) -> (optional) UI call via `Methods()`; if the result needs a UI-specific shape, `rpc.go` has a per-verb conversion block (`DeviceShow/Lookup/ListByRoom`). Tests: save/restore the `methods` map in `t.Cleanup`, no `t.Parallel()`.

**Add a retained status topic (daemon-published)**: choose a topic under `myhome/...`; publish with `mc.Publish(ctx, topic, payload, mqtt.AtLeastOnce, true, "<publisher>")` (existing examples: `solar_aggregate.go:159`, `temperature.go:210`, `occupancy.go:138`); publish at startup *and* on change (retained messages on an embedded broker are not persisted, *inferred*); to show it in the UI, subscribe in the daemon (`SubscribeWithHandler`) and relay via SSE or an in-memory cache, because the browser cannot reach the broker. Tests use `RecordingMockClient` (`myhome/mqtt/mock.go`, `Published(topic)`).

**Add an embedded asset (font/CSS/JS)**: drop the file in `internal/myhome/ui/static/`, add a `//go:embed` var and a `static.Assets` entry (`static/main.go`) so `/static/<name>` serves it, and link it from the template(s). If it is downloaded: add a `//go:generate fetch -url=... -out=... -sha256=...` line and add the output to `.gitignore`; nothing else is needed for CI/packaging because `internal/myhome/ui/...` is already in every generate path (Makefile `generate`, `.goreleaser.yml` hooks, `package-release.yml` "Run go generate"). `template.go` also embeds `static/*` wholesale. Check `cmd/fetchasset` timeout/offline behaviour for any new URL (internet-optional rule).

## 10. Conventions that are load-bearing

- **Layering** (3.2) and **single RPC server / topic** (`CLAUDE.md`, `AGENTS.md`); no separate subscriptions for RPC.
- **Logging**: `hlog.GetLogger("pkg/name")` / logr via context; CLI output via `fmt.Printf`, never log in CLI commands. `DeviceToView`/managers `panic("BUG: No logger initialized")` when ctx lacks a logger.
- **Config options**: 4-file rule (`options.go`, `run.go`, `docs/configuration.md`, `myhome-example.yaml`), env `MYHOME_<SECTION>_<KEY>`. Secrets (Beem, SFR, SMTP, vacuum, iRobot) are env/config-only, never flags.
- **SQLite**: relative plain filenames for new DBs, `MkdirAll` if absolute paths are allowed, idempotent `CREATE TABLE IF NOT EXISTS` per service sharing `myhome.db`; `COUNT(*)` for column checks.
- **Tests**: `make test` is canonical (root + all workspace modules, `-race`, explicit timeout); no `time.Sleep` in async tests (poll, re-inject); RPC-handler tests save/restore the package map; `make stress` for timing-sensitive packages. Never run the full suite in the foreground (600 s harness ceiling; `CLAUDE.md`).
- **Shelly JS rules** (no hoisting, nesting depth, no empty catch, wrapped `Script.Eval`, etc.): `AGENTS.md` kill list and `.claude/skills/shelly/`.
- **Resilience rules** in section 1; internet calls need a timeout and a fallback.
- `git mv` for moves; plan file under `docs/` for non-trivial tasks (`CLAUDE.md`).
- Docs are inconsistent in places (see debt); where they disagree, code won in this reference.

## 11. Dependencies and build

- Go 1.25.0, `GOTOOLCHAIN=go1.25.3` exported in `Makefile` and CI. Key libs: cobra/viper, logr (+ custom `hlog`), paho MQTT, Mochi MQTT broker, sqlx + pure-Go SQLite (`modernc.org/sqlite`; `ncruces/go-sqlite3` also in some module graphs), ristretto (MQTT cache), zeroconf, goja (emulator, in script tests), `kardianos/service` (service wrapper).
- Front-end: Bulma 0.9.4, htmx 2.0.4, Alpine 3.14.3 (generated, gitignored, sha256 pinned).
- **Build**: `make build` -> `generate` -> `make -C myhome build` (binary `./myhome/myhome`). `make generate` order: `./internal/myhome/ui/...`, `./myhome/ctl/pool`, `./myhome/ctl/garden`, `./internal/myhome/shelly/script`, `./internal/shelly/scripts/jstarget`, then `./...`. Generated, gitignored: `ui/static/{bulma.min.css,htmx.min.js,alpine.min.js}`, `myhome/ctl/pool/pool_defaults_generated.go`, `myhome/ctl/garden/garden_defaults_generated.go`, `internal/myhome/shelly/script/pool_kvs_generated.go` (from `tools/genconfigschema`), plus vendored `acorn.js` for `jstarget`.
- **The go generate sub-module gap**: `go generate ./...` at the workspace root does not recurse into sub-modules, hence the explicit lines. They are duplicated in: root `Makefile` `generate`; `.goreleaser.yml` `before.hooks`; `.github/workflows/package-release.yml` ("Run go generate", Windows MSI, then a bare `go build`). Other workflows call `make build` (`on-tag-main.yml`, `auto-tag-patch.yml`, `create-branch-on-minor-tag.yml`) or `make generate` (`test.yml` `build-pr-deb`). `CLAUDE.md` names only `pool` and `garden` as present in the Makefile; the Makefile also lists `shelly/script` and `jstarget`. Helper tools (`fetchasset`, `genconfigschema`) must run host-native, so cross-compile CI runs `make generate` before `GOOS=... make -C myhome build`.
- **Release/deploy**: goreleaser (`.goreleaser.yml`, `main: ./myhome`), Debian package (`make debpkg`/`debpkg-package`, systemd units in `linux/systemd/`, `postinst` pushes packaged scripts to devices), Windows MSI, nightly `myhome-update.timer` on the NAS installing `releases/latest`, `myhome-db-backup.timer`. Runtime: Linux systemd on arm64 NAS/Pi, reachable MQTT broker, LAN access to devices.

## 12. Testing and quality (from files; nothing run)

- Kinds: unit tests everywhere; goja-emulator tests of Shelly JS (`internal/shelly/scripts/*_test.go`, the slowest package, ~590-612 s under `-race` per Makefile history, with `smoke/` and `blu/` split out for parallelism); `RecordingMockClient` / `MockClient` for MQTT; `pkg/roomba/live_test.go` (live robot; presumably skipped by default, *inferred*); `docs/integration-tests.md`.
- Gates: `make cover` in `.github/workflows/test.yml` + `scripts/check-coverage.sh $(cat .coverage-min)`; `bash linux/systemd/update_test.sh` and `python3 -I linux/debian/postinst_test.py` run inside `make test`; the coverage-delta job is informational.
- Gaps visible from files: `internal/myhome/ui` has no handler/route/JS tests; no end-to-end browser tests; no `.golangci*` config and no lint step in `test.yml` were found.

## 13. Cross-cutting concerns

- **Security**: LAN trust model. Embedded broker allow-all; UI and `/rpc` unauthenticated (any LAN client can call any registered verb, including `device.setup`, `device.forget`, `switch.toggle`); reverse proxy to devices under `/devices/{token}/`. Secrets in `.env`/viper; `.env` gitignored; the embedded broker's debug hook logs passwords (`ShowPasswords: true`).
- **Config**: flags > env (`MYHOME_*`, dots->underscores) > `myhome.yaml` > defaults; a present-but-unparseable config file is a hard error (`loadConfigFile`, `run.go:30-40`). Documented in `docs/configuration.md`, `myhome-example.yaml`.
- **Observability**: hlog (errors-only by default for CLI, info for daemon); pprof; Prometheus; `accounts.Registry` panel; events.db as long-term record (90 d); NAS journal is volatile (`CLAUDE.md`).
- **Error propagation**: RPC errors become `{code:1,message}` responses (`server.go fail`); UI handlers answer `500` with the error text or silently blank the section (vacuum, pool); optional features degrade to empty rather than failing the page.
- **Panic policy**: the MQTT watchdog panics deliberately to force a restart; UI handlers recover panics unless `global.PanicOnBugs` (`--debug`).

## 14. Key design decisions and why (non-obvious)

- The page shell renders no device data; cards load by htmx after load (#390: enumerating devices server-side was discarded work and the dominant page cost) (`template.go:360-383`).
- The vacuum UI polls the daemon every 5 s *because* the robot pushes only on change and allows a single local client; polling reads cache, never the robot (`htmx_vacuum.go:14-16`, `service.go` header).
- `DeviceRegistry` in `ui` is a narrow local interface and must be fed the live device manager, not raw storage, so `Impl()`/`Status()` are attached (`template.go:41-47`).
- Host/IP is never persisted for devices; resolved lazily (SFR -> mDNS) (#252, `docs/no-ip-address-plan.md`).
- A single RPC topic/server for all services so one `signatures` map is the contract (`AGENTS.md`).
- `kvs.GetValue` caches negative results for 5 min since absent keys are a stable fact; invalidated on observed `kvs_rev` changes (#663 for transient vs definitive misses).
- MQTT subscriber queues drop single messages instead of unsubscribing (#517); periodic reconnect exists to refresh retained messages (`options.go` comment, README).
- `-timeout` is a separate Makefile variable from `TESTFLAGS` (#552). The daemon restarts every 6 h by design.
- Pool provisioning is deliberately unreachable from the daemon (`CLAUDE.md` NAS facts); the daemon only reads.
- Design docs index: `docs/vacuum-plan.md` (vacuum design, measurements), `docs/EVENT-LOG-PLAN.md` / `EVENT-LOG-UI-FIXES-PLAN.md` (event log UI), `docs/alpine-htmx-integration.md` (proposal that introduced htmx; references an `index-alpine.html` that does not exist), `docs/MQTT-BROKER-LOOPBACK-PLAN.md` (reconcile loop), `docs/pool-pump.md`, `docs/pool-notices-account-status-plan.md` (accounts panel, pool status), `docs/notice-events-plan.md`, `docs/configuration.md`. `docs/no-ip-address-plan.md` is cited by code comments but was not found in `docs/`. No ADR directory exists.

## 15. Tech-debt register (descriptive; status noted)

| # | Observation | Evidence | Status |
|---|---|---|---|
| 1 | Pool status does a live `Input.GetStatus` plus up to 3 KVS reads (cached 5 min) on every card render and every SSE-triggered card refresh | `template.go:84-107`, `pool_notices.go:166-177`, `pool_rpc.go:34-55` | unknown |
| 2 | Every `+/events/rpc` message triggers a full `Shelly.GetDeviceInfo`+`GetComponents` refresh of that device; the status payload is not applied (`TODO`) | `watch/mqtt.go:124,151`, `device.go:146-209` | unknown (TODO present) |
| 3 | 1-min round-robin refresh and hourly HTTP reconcile of all Gen2 devices | `manager.go:644-752` | accepted (documented design) |
| 4 | `OnValue` KVS read per switch on render; `GetSwitchesSummary` live fallback when no config; `parseOnValueIndexed` can panic on a bad value (documented in a NOTE) | `template.go:284,298-309`, `sswitch/service.go:183-217` | unknown / NOTE in code |
| 5 | Index-page JS reads `out.on` from the `/rpc` `switch.toggle` response, but `/rpc` wraps the result as `{"result":...}`, so the toggle shape update uses an undefined value | `index.html:397-399`, `rpc.go:73` | unknown |
| 6 | `/htmx/switch/toggle` + `switchButtonTemplate` unreferenced by any template; `StaticFS`/`StaticFileServer` and `NewContextWithSSE`/`SSEFromContext` have no callers | `htmx.go:214-271,779`, `template.go:27-39`, `sse.go:16-27` | unknown |
| 7 | `static/myhome.css` is embedded and served but linked by no template (grep finds it only in Go) | `static/main.go:25,45` | unknown |
| 8 | Duplicated templates: `deviceCardsTemplate` and `deviceCardTemplate` are near-identical copies (~120 lines each); event-log page chrome duplicates index chrome; "Load more" markup duplicated in two events templates | `htmx.go:481-718`, `events_template.go:80-156` | unknown |
| 9 | Templates parsed on every request with `template.Must`; `index.html` uses `text/template` while fragments use `html/template` | `htmx.go:114,150,180,205,258`, `template.go:127-130` | unknown |
| 10 | Event-log SSE row insertion ignores the active filter and the 24 h window; `Service.Record` broadcasts even for ignored duplicates | `events_template.go:233-291`, `service.go:53-67`, `htmx.go:382-395` | unknown |
| 11 | SSE has no keepalive and the UI shows no disconnected state (`onerror` logs only); `ui.Start` receives `mc` but never uses it; `/rpc` has no per-request timeout; no UI auth | `sse.go:183-217`, `index.html:500-502`, `server.go:33,81`, `rpc.go` | unknown |
| 12 | `ui.Start` only runs when the device manager is enabled; `accounts.Registry` comment mentions `/health`, which does not exist (only `/_health` in the proxy) | `daemon.go:283-575`, `accounts/registry.go:1-6` | unknown |
| 13 | `events.createTables` column-existence check computes `count` and discards it | `events/storage.go:132-139` | unknown |
| 14 | Docs vs code: `CLAUDE.md`/README `myhome run` vs actual `myhome daemon run`; `CLAUDE.md` "~45 sub-modules" vs 60 in `go.work`; `docs/configuration.md` says `--ui-port`/`-u` but code binds `-p`; `AGENTS.md` handler signature `func(params any)` and `NewMethodHandlers` pattern vs real `func(ctx, any)` and direct `Service.RegisterHandlers`; `CLAUDE.md` names only pool/garden explicit generate lines but the Makefile also has shelly/script and jstarget; `docs/alpine-htmx-integration.md` references an absent `static/index-alpine.html`; `docs/no-ip-address-plan.md` cited but absent | `run.go:62`, `go.work`, `methods.go:8`, `Makefile:343-349` | unknown |
| 15 | **`AGENTS.md` vs `CLAUDE.md` contradiction on tests**: `AGENTS.md` tells sub-agents "do NOT background `make test`; run in the foreground" (and gives ~183 s for the slowest package); `CLAUDE.md` says "never run the suite in the foreground; background and poll in the same turn" (and ~590 s) | `AGENTS.md` "Sub-agents: do NOT background `make test`", `CLAUDE.md` "The suite now outlives a foreground command" | unknown |
| 16 | The slowest test package is still one serial ~600 s binary; timeout history in the Makefile comment | `Makefile:202-289`, issue #552 | in progress (per `CLAUDE.md`) |
| 17 | Embedded broker: debug hook with `ShowPasswords: true`, allow-all auth | `myhome/mqtt/server.go:50-65` | accepted for LAN (*inferred*) |
| 18 | `daemon.go` imports the same package under two aliases (`mqttclient`, `mqttserver`); the RPC server loop is a single goroutine so one slow MQTT-borne call (e.g. `pool.getstatus`) delays others on that transport (in-process UI calls are unaffected) | `daemon.go:23-24`, `server.go:45-126` | unknown |
| 19 | TODO/FIXME: `watch/mqtt.go:151` update-from-status; `device.go:169` KVS-revision; `manager.go:72-73` configurable buffer size; `server.go:108` FIXME on type check | files cited | unknown |

## 16. Facts a kiosk-UI designer most needs (summary pointers)

1. The only periodic browser->daemon poll today is the vacuum panel (5 s, cache-only, never the robot). Everything else loads once or arrives by SSE.
2. Device cards are server-rendered from a cached status snapshot refreshed by daemon-side polling (1 min round-robin + a refresh after every device event); the pool card additionally triggers live device RPCs on each render.
3. SSE is the only push channel and only carries sensors, device cards, and events; there is no daemon liveness signal (no LWT, no heartbeat, no SSE keepalive) and the UI does not surface SSE errors.
4. Browsers cannot read retained MQTT; the broker exposes TCP only. Retained state must be consumed by the daemon.
5. No nav, no viewport rules, no fonts; one ~820-line inline-JS page plus a string-embedded event-log page; `/` is a catch-all into the device proxy, so new page routes need explicit mux entries.
6. The daemon currently publishes retained: occupancy, solar available, beem power, room temperature ranges, Gen1 proxy values, fetchproxy topics; nothing for vacuum, pool, switches or daemon health.
7. UI tests are template-render/unit only; routing and handlers are untested.

## 17. When documented

- Documented on 2026-11-04 from the worktree `/home/fix/GIT/home-automation/.claude/worktrees/docs+681-kiosk-design`, branch `docs/681-kiosk-design`, stated by the caller to be at `origin/main`. Session start showed `main` at `ccc92232` ("Merge pull request #622 from asnowfix/feat/606-vacuum"). The worktree's `.git` is a pointer file to a path outside the permitted read scope, so the exact commit was not verified from `HEAD`.
- Not read in this pass: `ctl/*` command internals beyond pool, `pkg/shelly` internals beyond Device/KVS, `notice`/`notify`/`fetchproxy`/`metrics` implementations, `linux/` packaging scripts, most `docs/*.md`.
