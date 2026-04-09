# Architecture Review

> Detailed review of the `home-automation` repository as of 2026-04-09.

## 1. High-Level Overview

The repository is a Go multi-module workspace (~58 modules in `go.work`) that builds a single binary (`myhome`) for home automation using Shelly IoT devices. It also supports TP-Link Tapo and SFR devices, though Shelly is the dominant focus.

```
home-automation/
├── go.work                        # Workspace: 58 modules
├── hlog/                          # Custom structured logger
├── cmd/                           # Entry points
├── pkg/                           # Reusable public packages
│   ├── shelly/       (8.4k LOC)   # Generic Shelly API (16 sub-modules)
│   ├── devices/                   # Device interface abstraction
│   ├── tapo/                      # TP-Link Tapo support
│   ├── sfr/                       # Smart Frequency Response
│   └── version/                   # Version management
├── internal/
│   ├── myhome/                    # MyHome core (RPC types, verb registry)
│   │   └── shelly/   (2.9k LOC)  # Shelly business logic
│   ├── shelly/scripts/ (324 LOC) # Embedded .js scripts + version tracking
│   └── ...                        # myip, myzone, debug, global, tools
├── myhome/                        # Application layer
│   ├── ctl/                       # CLI (cobra commands)
│   │   └── shelly/   (3.1k LOC)  # 12 command groups, 30+ subcommands
│   ├── daemon/                    # Background service
│   ├── devices/                   # Device manager
│   ├── mqtt/                      # MQTT client
│   ├── storage/                   # SQLite persistence
│   ├── temperature/               # Temperature service
│   └── occupancy/                 # Occupancy detection
├── docs/                          # Plans, configuration reference
└── linux/                         # systemd / Debian packaging
```

### Binary Modes

- `myhome run` — daemon (eager MQTT, receives retained messages)
- `myhome ctl ...` — CLI (lazy MQTT, auto-connects on first RPC)

### Three-Tier Shelly Architecture

| Layer | Package | Role | LOC |
|---|---|---|---|
| 1. Generic API | `pkg/shelly/` + 16 sub-modules | Raw Shelly RPC, no business logic | 8,377 |
| 2. Business Logic | `internal/myhome/shelly/` | Version tracking, setup orchestration | 2,867 |
| 3. CLI | `myhome/ctl/shelly/` | Cobra commands, user output | 3,132 |

Supporting: `internal/shelly/scripts/` (324 LOC) — embedded `.js` files + version computation.

---

## 2. Module & Dependency Structure

### Module Naming Problem

Module names are **inconsistent and non-standard**. Go modules intended for external consumption should use full GitHub paths:

| Current module name | Expected name |
|---|---|
| `pkg/shelly` | `github.com/asnowfix/home-automation/pkg/shelly` |
| `pkg/devices` | `github.com/asnowfix/home-automation/pkg/devices` |
| `schedule` | `github.com/asnowfix/home-automation/pkg/shelly/schedule` |
| `shelly/scripts` | `github.com/asnowfix/home-automation/internal/shelly/scripts` |
| `myhome/net` | `github.com/asnowfix/home-automation/internal/myhome/net` |
| `hlog` | `github.com/asnowfix/home-automation/hlog` |
| `myhome` | `github.com/asnowfix/home-automation/internal/myhome` |
| `tapo` | `github.com/asnowfix/home-automation/pkg/tapo` |

Only the root module (`github.com/asnowfix/home-automation`) and one CLI module (`myhome/ctl/temperature`) use the full GitHub path. The rest use **short, non-importable names** that only work within `go.work` via `replace` directives.

This means **none of these packages can be consumed by external projects** without `replace` directives — the module names are not resolvable by the Go toolchain.

### Dependency Graph (pkg/shelly focus)

```
pkg/shelly
├── pkg/shelly/types          (interfaces, enums)
├── pkg/shelly/mqtt           (MQTT channel)
├── pkg/shelly/shelly         (Shelly.* RPC)
├── pkg/shelly/system         (System.* RPC)
├── pkg/shelly/wifi           (WiFi.* RPC)
├── pkg/shelly/script         (Script.* RPC)
├── pkg/shelly/kvs            (KVS.* RPC)
├── pkg/shelly/sswitch        (Switch.* RPC)
├── pkg/shelly/input          (Input.* RPC)
├── pkg/shelly/ethernet       (Ethernet.* RPC)
├── pkg/shelly/matter         (Matter.* RPC)
├── pkg/shelly/schedule       (Schedule.* — module name: "schedule")
├── pkg/shelly/shttp          (HTTP channel)
├── pkg/shelly/ble            (Bluetooth Low Energy)
├── pkg/shelly/blu            (Shelly BLU)
├── pkg/shelly/gen1           (Gen1 legacy)
├── pkg/shelly/ratelimit      (rate limiter)
│
├── pkg/devices               (Device interface — external dep)
├── myhome/net                (blank import _ — side effect dep)
└── shelly/scripts            (embedded .js filesystem)
```

**External deps of pkg/shelly:** Only `logr` and `zeroconf` — very clean.

**Inward deps (leaking from myhome into pkg/shelly):**
- `_ "myhome/net"` in `device.go` — blank import for side-effect network init
- `"shelly/scripts"` in `ops.go` — embedded .js files passed to `script.Init()`
- `"schedule"` in `ops.go` and `shelly/types.go` — misnamed module for `pkg/shelly/schedule`

---

## 3. Flaws and Issues

### F1. Non-Standard Module Names (Critical for extraction)

**Severity: High** — Blocks external consumption.

All 57 sub-modules (except root + temperature) use short names (`pkg/shelly`, `hlog`, `myhome`). These only resolve within the workspace via `replace` directives in `go.mod`. External projects cannot `go get` any of these packages.

**Impact:** Extracting `pkg/shelly` to a separate repo requires renaming the module from `pkg/shelly` to `github.com/asnowfix/go-shellies` (or similar) and updating all 57 modules' `replace` directives.

### F2. Leaky Abstraction: pkg/shelly depends on myhome internals

**Severity: High** — Violates the three-tier rule.

`pkg/shelly/device.go` imports `_ "myhome/net"` (a blank import for side-effect gateway discovery). A generic Shelly library should not depend on the application's networking layer.

`pkg/shelly/ops.go` imports `shelly/scripts` (embedded `.js` files from `internal/shelly/scripts/`). These are MyHome-specific scripts (heater, pool-pump, watchdog, etc.) — they should not be compiled into the generic library.

### F3. Inconsistent Module Name: `schedule`

**Severity: Medium** — The module at `pkg/shelly/schedule/go.mod` declares itself as `module schedule` instead of `module pkg/shelly/schedule`. This is misleading and will conflict with any other package named `schedule`.

### F4. `pkg/devices` Coupled to pkg/shelly

**Severity: Medium** — The `Device` interface in `pkg/devices` is used by `pkg/shelly` as its device abstraction. If `pkg/shelly` is extracted, `pkg/devices` must either:
- Be extracted too (into `go-shellies` or its own repo), or
- Be replaced with an interface defined within `pkg/shelly/types`

The current `pkg/devices.Device` interface is generic (Manufacturer, Id, Name, Host, Ip, Mac) and could remain as a shared interface, but the coupling adds extraction complexity.

### F5. Global Mutable State

**Severity: Medium** — Several critical components use package-level singletons:

- `pkg/shelly/registrar.go`: `var registrar Registrar` — singleton method registry
- `pkg/shelly/device.go`: `var deviceMqttRegistry` — global MQTT channel map
- `pkg/shelly/mqtt/`: `SetClient()` / `GetClient()` — global MQTT client
- `pkg/shelly/ratelimit/`: `Init()` / `GetLimiter()` — global rate limiter

This makes the library hard to test in isolation and impossible to use with multiple independent configurations (e.g., two MQTT brokers). It also complicates concurrent test execution.

### F6. Commented-Out Code Accumulation

**Severity: Low** — `device.go` contains ~50 lines of commented-out code (lines 168-206, 684-714). Similarly, `registrar.go` has commented-out methods. This code should be removed or tracked in issues.

### F7. Panic-Driven Error Handling

**Severity: Medium** — Several functions in `pkg/shelly/device.go` call `panic()` for conditions that should return errors:
- `UpdateId()` panics on invalid ID
- `UpdateMac()` panics on empty MAC
- `init()` panics if info is nil
- `initMqtt()` panics if device ID is empty

A library should never panic — it should return errors and let the caller decide.

### F8. `fmt.Printf` in Library Code

**Severity: Low-Medium** — `pkg/shelly/device.go` `Foreach()` uses `fmt.Printf` for user-facing output (lines 827-834). A generic library should use structured logging or callbacks — not write directly to stdout. This mixes CLI concerns into the API layer.

### F9. Excessive Module Granularity

**Severity: Medium** — `pkg/shelly` has **16 separate Go modules** for what could be sub-packages of a single module. Each sub-module has its own `go.mod`, requiring individual dependency management and `replace` directives. This creates:
- 16× `go.mod` files to maintain per dependency update
- 16× entries in `go.work`
- Complex `replace` chains in any consuming `go.mod`

Most mature Go libraries (e.g., `google.golang.org/grpc`) ship as a single module with sub-packages.

### F10. Missing Test Coverage

**Severity: Medium** — Key packages lack tests:
- `pkg/shelly/device.go` — core device logic, no tests
- `pkg/shelly/registrar.go` — method registry, no tests
- `pkg/shelly/ops.go` — initialization, no tests
- `pkg/shelly/config.go` — device configuration, no tests
- Most sub-packages under `pkg/shelly/` (kvs, system, wifi, ethernet, etc.) have no test files

Only `pkg/shelly/script/` and `pkg/shelly/mqtt/` have test coverage.

### F11. Tight Coupling Between Init and Embedded Scripts

**Severity: Medium** — `pkg/shelly/ops.go:Init()` calls `script.Init(log, &registrar, scripts.GetFS())` passing the embedded filesystem from `internal/shelly/scripts/`. This means the generic library's initialization requires MyHome-specific scripts. The embedded filesystem should be passed by the application, not baked into the library init.

### F12. MQTT Client as Global Singleton

**Severity: Medium** — `pkg/shelly/mqtt.SetClient()` sets a global MQTT client that all devices share. There's no way to use different MQTT brokers for different device sets or to inject a mock client for testing without affecting global state.

### F13. Mixed Concerns in `Foreach()`

**Severity: Low** — `pkg/shelly/device.go:Foreach()` combines:
- Device filtering (skip Gen1, skip BLU)
- Device instantiation (NewDeviceFromSummary + init)
- Parallel execution with WaitGroup
- Error aggregation
- User-facing output formatting (fmt.Printf)

This should be decomposed: filtering and output belong at the application layer.

---

## 4. Strengths

- **Clean three-tier separation** (when respected) — the conceptual architecture is sound
- **Minimal external dependencies** in pkg/shelly — only `logr` + `zeroconf`
- **Well-documented conventions** in CLAUDE.md and AGENTS.md
- **Goroutine leak prevention** — DeviceMqttChannels registry is thoughtfully designed
- **Rate limiting** — per-device request queuing prevents overwhelming devices
- **Channel abstraction** — HTTP/MQTT/UDP channels are cleanly separated via the Registrar
- **Comprehensive CLI** — rich set of device management commands

---

## 5. Summary of Issues by Priority

| # | Issue | Severity | Blocks Extraction? |
|---|---|---|---|
| F1 | Non-standard module names | High | Yes |
| F2 | pkg/shelly depends on myhome internals | High | Yes |
| F9 | 16 separate modules instead of 1 | Medium | Yes |
| F4 | pkg/devices coupling | Medium | Yes |
| F3 | `schedule` module name | Medium | Yes |
| F11 | Init requires embedded scripts | Medium | Yes |
| F5 | Global mutable state | Medium | No (but limits reusability) |
| F7 | Panic-driven error handling | Medium | No (but bad library practice) |
| F8 | fmt.Printf in library code | Low-Medium | No (but bad library practice) |
| F12 | MQTT client as global singleton | Medium | No (future improvement) |
| F10 | Missing test coverage | Medium | No (but needed before refactor) |
| F13 | Mixed concerns in Foreach | Low | No |
| F6 | Commented-out code | Low | No |
