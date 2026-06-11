# Plan: MyHome workflows in JavaScript (goja), distributed device↔daemon execution

> Refactor so that MyHome-specific *workflows* are written in JavaScript and executed by
> the daemon's built-in goja engine, while communication, persistence and infrastructure
> stay in Go. Devices can invoke (via MQTT) scripts running on a daemon; a single logical
> workflow can therefore span a device and a daemon. Multiple daemons coexist,
> differentiated by their *instance name* in the RPC protocol.

## Decisions (agreed with FiX, 2026-06-11)

- **Scope this session**: framework + migrate **occupancy** and **heater** workflows.
  `myhome/temperature` is being refactored in a separate worktree — do not touch it.
- **Go workflow code stays**: JS versions are **opt-in via config**; Go implementations
  remain the default until validated in production. Cleanup happens later.
- **Instance names**: default = OS hostname; overridable via `--instance` /
  `daemon.instance_name` / `MYHOME_DAEMON_INSTANCE_NAME`. The daemon that runs the
  embedded MQTT broker is the *main* daemon and additionally serves the well-known
  `myhome/rpc` topic, so existing CLI/devices keep working unchanged.
- **Git**: incremental commits on `worktree-feature+my-js-home`, pushed, draft PR.
- **Testing**: dev daemon instance `dev-claude` against broker `tcp://192.168.1.2:1883`;
  live device `development.local` (192.168.1.31). The production daemon is not modified.

## Architecture

```
┌────────────────────────── daemon (Go) ─────────────────────────┐
│ myhome/scripthost          ← NEW: runs workflow JS in goja     │
│   ├─ engine per script (pkg/shelly/script Engine)              │
│   ├─ state persisted as JSON (scripts-state/<name>.json)       │
│   ├─ Shelly-compatible APIs (Timer, MQTT, KVS, Script.storage) │
│   └─ MyHome.* daemon APIs (call, deviceCall, on, registerVerb) │
│ internal/myhome RPC server ← existing myhome/rpc protocol      │
│   └─ NEW verb script.invoke → dispatches into scripthost       │
└────────────────────────────────────────────────────────────────┘
            ▲ MQTT <instance>/rpc (+ myhome/rpc on main daemon)
            │ request: {id, src, dst, method:"script.invoke",
            │           params:{script, name, params}}
            ▼ response to <src>/rpc
┌─────────── Shelly device (ES5 Espruino) ───────────┐
│ device script publishes RPC request, subscribes to │
│ <own-prefix>/myhome/rpc for the response           │
└────────────────────────────────────────────────────┘
```

Key properties:
- **One MQTT topic per daemon instance** (`<instance>/rpc`) — reuses the existing RPC
  server; *no* separate MQTT subscription (repo rule).
- A device script "calls home" by invoking `script.invoke` with the name of a script
  that is assumed to also run on the daemon (same name, daemon flavour).
- Daemon scripts use the *same* Shelly JS API as device scripts (so knowledge and code
  transfer), plus a `MyHome` object for daemon-only capabilities.

## Phases

### Phase 1 — Instance name plumbing
- [x] `--instance` flag default changes `"myhome"` → `""`; resolution order:
      flag > `daemon.instance_name` (config/env) > OS hostname.
- [x] Daemon with embedded broker also serves the well-known `myhome/rpc` alias
      (it is the *main* daemon by definition).
- [x] `myhome.NewServerE` accepts explicit topics (instance + optional alias).
- [x] 4-file config rule: options.go, run.go, docs/configuration.md, myhome-example.yaml.

### Phase 2 — Reusable goja Engine in pkg/shelly/script
- [x] Extract `createShellyRuntime` + event loop from `Run()` into an exported
      `Engine` (`NewEngine(ctx, EngineOptions)`, `Engine.Loop(ctx)`).
- [x] `EngineOptions`: script name, source, `*DeviceState`, extra-API hook
      (`Customize func(vm *goja.Runtime) error`).
- [x] **External invocation**: `Engine.Invoke(ctx, fnName, jsonParams) (any, error)` —
      thread-safe dispatch into the VM through the event loop (goja VMs are
      single-threaded); used by `script.invoke` and by Go→JS callbacks.
- [x] `Run()/RunWithDeviceState()` keep their behaviour (existing tests must pass).

### Phase 3 — Daemon script host (`myhome/scripthost`)
- [x] New workspace module `myhome/scripthost` (`go work use`).
- [x] Loads scripts by name: user dir (`daemon.scripts.dir`, optional) first, then the
      embedded `internal/shelly/scripts` FS — *every device script is also present on
      the daemon*.
- [x] Per-script engine goroutine; crash → restart with backoff; stop on ctx cancel.
- [x] State persisted per script: `scripts-state/<name>.json` (KVS + Script.storage),
      auto-saved on modification (reuses DeviceState persistence).
- [x] `MyHome` JS API:
      - `MyHome.instance()` → instance name
      - `MyHome.call(method, params, callback)` → in-process myhome verb dispatch
      - `MyHome.deviceCall(device, method, params, callback)` → RPC to a Shelly device
      - `MyHome.on(name, fn)` → handle `script.invoke` calls addressed to this script
      - `MyHome.registerVerb(verb, fn)` → JS implementation of an existing RPC verb
        (used for opt-in workflow replacement, e.g. `heater.getconfig`)
      - `MyHome.log(...)` → hlog-backed structured logging
- [x] Config: `daemon.scripts.enabled`, `daemon.scripts.dir`, `daemon.scripts.run`
      (list of script names) — 4-file rule.

### Phase 4 — `script.invoke` protocol (device→daemon)
- [x] RPC 4 steps: Verb `script.invoke` in const.go; `ScriptInvokeParams{Script, Name,
      Params}` / `ScriptInvokeResult{Result}` types; signatures map entry;
      `RegisterMethodHandler` from scripthost.
- [x] Device-side calling convention (ES5, minify-safe):
      publish request to `myhome/rpc` (or specific instance) with
      `src = <device-topic-prefix> + "/myhome"`, subscribe `<prefix>/myhome/rpc`,
      match responses by id. Helper shipped as `internal/shelly/scripts/myhome-link.js`
      (test/demo script for development.local).
- [x] Document the convention in AGENTS.md.

### Phase 5 — Occupancy workflow in JS (opt-in)
- [x] Go infra verb `lan.hosts` (wraps `pkg/sfr.GetHostsList`) — presence polling is
      infrastructure, stays in Go.
- [x] `internal/shelly/scripts/occupancy.js`: subscribes `+/events/rpc` (NotifyStatus
      with `input:` changes), polls `MyHome.call("lan.hosts")` for mobile-device
      patterns, publishes retained `myhome/occupancy` (same payloads as Go version),
      12 h expiry timer.
- [x] Opt-in: `occupancy` listed in `daemon.scripts.run` ⇒ daemon skips the Go
      occupancy service (logs the substitution). Default unchanged (Go).
- [x] Unit test runs occupancy.js in the emulator (pattern: blu_listener_test.go).

### Phase 6 — Heater workflow in JS (opt-in)
- [ ] `internal/shelly/scripts/heater-myhome.js` (daemon flavour):
      - `MyHome.registerVerb("heater.getconfig"/"heater.setconfig", …)` re-implementing
        internal/myhome/shelly/script/heater.go logic via `MyHome.deviceCall`
        (KVS.GetMany / KVS.Set) and `MyHome.uploadScript` Go binding (wraps
        UploadWithVersion — uploading firmware-grade JS to devices stays Go infra).
      - `MyHome.on("get_forecast", …)`: serves cached weather forecast to device heater
        scripts via `script.invoke` — demonstrates distributed device↔daemon execution.
- [ ] Opt-in: `heater-myhome` in `daemon.scripts.run` ⇒ Go HeaterService not registered.
- [ ] Unit test via emulator.

### Phase 7 — Live validation on development.local
- [ ] Build; run dev daemon `--instance dev-claude --mqtt-broker 192.168.1.2 …`
      (no mDNS publish, no embedded broker, separate db/state files, UI port off 6080).
- [ ] Upload `myhome-link.js` to development.local (`--no-minify`), enable script debug,
      verify device→daemon `script.invoke` round-trip and distributed heater forecast.
- [ ] `make test` green.

### Phase 8 — Draft PR
- [ ] Push branch, open draft PR with summary, config examples, and migration notes
      (main daemon: nothing to change — alias topic keeps current behaviour).

## Risks / notes

- goja VM is single-threaded: all external entry points (script.invoke, MyHome.call
  callbacks) are funneled through the engine event loop.
- Engine `MQTT.subscribe` passes the subscription *filter* (not the concrete topic) to
  callbacks for wildcard subscriptions; occupancy.js only needs payloads. Improvement
  tracked for later (use SubscribeWithTopic).
- Two occupancy publishers (Go + JS) would fight over the retained `myhome/occupancy`
  topic — hence hard substitution, not parallel run.
- `script.invoke` responses to devices reuse the existing response topic scheme
  (`<src>/rpc`); devices use `<prefix>/myhome` as src so the response topic does not
  collide with the device's own RPC prefix.
