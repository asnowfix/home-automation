# Review: Planned Improvements

> Last updated: 2026-02-27 — Initial review

This document captures seven improvement areas identified during a full codebase
review. Each section includes concrete evidence (file paths, line numbers),
analysis, and a proposed fix.

## Table of Contents

1. [`pkg/shelly/` Architecture Violations](#1-pkgshelly-architecture-violations) — **High**
2. [YAML Library Proliferation](#2-yaml-library-proliferation) — **High**
3. [Workspace Module Consolidation](#3-workspace-module-consolidation) — **Medium**
4. [Test Coverage Gaps](#4-test-coverage-gaps) — **Medium**
5. [Inconsistent RPC Parameter Types](#5-inconsistent-rpc-parameter-types) — **Low**
6. [Duplicate Result Type Definitions](#6-duplicate-result-type-definitions) — **Low**
7. [Handler Registration Style Inconsistency](#7-handler-registration-style-inconsistency) — **Low**

---

## 1. `pkg/shelly/` Architecture Violations

**Severity: High**

### Problem

AGENTS.md states that `pkg/shelly/` is a *"Pure, reusable Shelly device API
implementation"* with *"No business logic or application-specific code"*. Three
files violate this by importing from `myhome/` (which resolves to
`internal/myhome/`).

### Evidence

#### Violation A — Dead blank import

**File:** `pkg/shelly/device.go:7`

```go
import (
    _ "myhome/net"  // blank import — no init() exists in myhome/net
    ...
)
```

`internal/myhome/net/` contains `mynet.go` and `resolver.go`, neither of which
defines an `init()` function. This import has no side effects and does nothing.
It is dead code that creates a compile-time dependency on MyHome networking.

#### Violation B — BLU listener creates `myhome.Device`

**File:** `pkg/shelly/blu/listener.go:7-8`

```go
import (
    "myhome"       // for myhome.NewDevice(), myhome.SHELLY
    "myhome/mqtt"  // for mqtt.Client
    ...
)
```

The listener constructs a concrete `myhome.Device` at line ~336:

```go
device := myhome.NewDevice(log, myhome.SHELLY, deviceID)
device = device.WithMAC(macAddr)
device = device.WithName(deviceID)
```

This hardcodes the listener to the MyHome device model — it cannot be reused
with a different device representation.

#### Violation C — Gen1 listener creates `myhome.Device`

**File:** `pkg/shelly/gen1/listener.go:7-9`

```go
import (
    "myhome"          // for myhome.Device{}
    "myhome/devices"  // for devices.DeviceRegistry
    "myhome/model"    // for model.Router
    ...
)
```

The listener constructs `&myhome.Device{}` directly at line ~122 and calls
builder methods (`.WithId()`, `.WithName()`, `.WithMAC()`).

### Proposed Fix

1. **Remove the dead import** in `pkg/shelly/device.go:7` — no code changes
   needed beyond deleting the line.

2. **Inject a device factory** into BLU and Gen1 listeners. Define a factory
   interface in `pkg/shelly/types/`:

   ```go
   // DeviceFactory creates device objects for the host application.
   type DeviceFactory func(log logr.Logger, id string, mac net.HardwareAddr) any
   ```

   Pass it as a parameter to `StartBLUListener()` and `StartGen1Listener()`
   instead of importing `myhome.NewDevice` directly. The caller in
   `myhome/daemon/daemon.go` supplies a factory that creates `myhome.Device`.

3. **Move `myhome/devices.DeviceRegistry`** — the interface is already
   abstract, but it lives under `myhome/`. Define an equivalent interface in
   `pkg/shelly/types/` so the generic layer doesn't depend on the business
   layer's package.

### Impact

After this change, `pkg/shelly/` has zero imports from `myhome/` and can be
extracted as a standalone library or reused in other Shelly-based projects.

---

## 2. YAML Library Proliferation

**Severity: High**

### Problem

Four different YAML libraries are imported across the codebase. One of them
uses a wrong Go module path.

### Evidence

| Library | Files | Notes |
|---------|-------|-------|
| `gopkg.in/yaml.v3` | `myhome/ctl/show/show.go`, `show/shelly.go`, `list/list.go`, `open/main.go`, `heater/show.go`, `shelly/kvs/set.go`, `shelly/jobs/show.go` | Community standard (7 files) |
| `sigs.k8s.io/yaml` | `myhome/ctl/options/options.go`, `temperature/save_load.go`, `shelly/wifi/status.go`, `shelly/sys/main.go`, `shelly/components/main.go` | Kubernetes wrapper (5 files) |
| `gopkg.in/yaml.v2` | `myhome/ctl/shelly/main.go` | Legacy; same file also imports yaml.v3 |
| `go.yaml.in/yaml/v3` | `myhome/ctl/sswitch/main.go:19` | **Wrong module path** — should be `gopkg.in/yaml.v3` |

The `myhome/ctl/sswitch/main.go` import is the most urgent issue — it pulls
in v3.0.4 from the `go.yaml.in` registry instead of v3.0.1 from
`gopkg.in`. These are different modules with different checksums, producing
unnecessary dependency bloat.

### Proposed Fix

1. **Fix the typo** in `myhome/ctl/sswitch/main.go:19`:
   change `"go.yaml.in/yaml/v3"` to `"gopkg.in/yaml.v3"`.

2. **Standardize on `gopkg.in/yaml.v3`** everywhere. Replace all
   `sigs.k8s.io/yaml` usage — every call site only uses `yaml.Marshal()` for
   CLI output formatting and `yaml.Unmarshal()` for config loading, both of
   which `gopkg.in/yaml.v3` supports directly.

3. **Remove `gopkg.in/yaml.v2`** from `myhome/ctl/shelly/main.go` — identify
   what v2-specific feature is used (likely none) and switch to v3.

4. Run `go mod tidy` across all affected modules to prune the unused
   dependencies from `go.sum`.

### Files to Change

- `myhome/ctl/sswitch/main.go` — fix import path
- `myhome/ctl/shelly/main.go` — remove yaml.v2 import
- `myhome/ctl/options/options.go` — replace sigs.k8s.io/yaml
- `myhome/ctl/temperature/save_load.go` — replace sigs.k8s.io/yaml
- `myhome/ctl/shelly/wifi/status.go` — replace sigs.k8s.io/yaml
- `myhome/ctl/shelly/sys/main.go` — replace sigs.k8s.io/yaml
- `myhome/ctl/shelly/components/main.go` — replace sigs.k8s.io/yaml

---

## 3. Workspace Module Consolidation

**Severity: Medium**

### Problem

The project has **58 workspace modules** (58 separate `go.mod` files listed in
`go.work`). Most sub-modules under `pkg/shelly/` and `myhome/ctl/` have
identical, minimal dependencies (`go-logr/logr` only) and no justification for
being separate modules.

### Evidence

**`pkg/shelly/*` — 13 sub-modules, 10 of which have only `logr` as a dependency:**

| Module | Unique Dependencies | Separate Module Justified? |
|--------|--------------------|----|
| `pkg/shelly/types` | None | No |
| `pkg/shelly/kvs` | logr | No |
| `pkg/shelly/wifi` | logr | No |
| `pkg/shelly/sswitch` | logr | No |
| `pkg/shelly/shttp` | logr | No |
| `pkg/shelly/input` | logr | No |
| `pkg/shelly/ethernet` | logr | No |
| `pkg/shelly/system` | logr | No |
| `pkg/shelly/shelly` | logr | No |
| `pkg/shelly/schedule` | logr | No |
| `pkg/shelly/blu` | logr | No |
| `pkg/shelly/mqtt` | logr, `golang.org/x/exp` | Marginal (see below) |
| `pkg/shelly/script` | goja, minify | **Yes** |
| `pkg/shelly/gen1` | gorilla/schema | **Yes** |

**`myhome/ctl/*` — 17 sub-modules** with near-identical dependency sets
(cobra, logr, yaml).

**Also notable:** `pkg/shelly/mqtt` imports `golang.org/x/exp/rand` for
`rand.Seed()` — this function is deprecated since Go 1.20 and unnecessary
since Go 1.22 (the runtime auto-seeds `math/rand`). Removing it eliminates
the only unique dependency of that module.

### Proposed Fix

**Phase 1 — `pkg/shelly/` consolidation:**

Merge the 10 logr-only sub-modules into the parent `pkg/shelly/go.mod`.
They become sub-packages instead of sub-modules. Keep `script` and `gen1` as
separate modules (they have genuinely unique heavy dependencies).

Remove the deprecated `golang.org/x/exp/rand` usage from
`pkg/shelly/mqtt/ops.go` and merge `pkg/shelly/mqtt` into the parent too.

Result: 13 modules → 3 modules (`pkg/shelly`, `pkg/shelly/script`,
`pkg/shelly/gen1`).

**Phase 2 — `myhome/ctl/` consolidation:**

Merge the 17 CLI sub-modules into a single `myhome/ctl/go.mod`. All share
cobra + logr + yaml and are always built together as part of the CLI binary.

Result: 17 modules → 1 module.

**Overall:** 58 modules → ~30 modules. Simpler `go.work`, fewer `go mod tidy`
commands, and consistent Go version across packages (currently a mix of 1.23.0
and 1.24.2).

### Risks

- Consolidation changes import paths (e.g., `pkg/shelly/kvs` stays the same
  Go import path but loses its own `go.mod`).
- Requires updating `go.work` and all `replace` directives.
- Large diff — best done in a dedicated branch.

---

## 4. Test Coverage Gaps

**Severity: Medium**

### Current State

| Test File | Package | Tests | What's Covered |
|-----------|---------|-------|----------------|
| `myhome/storage/db_test.go` | storage | 21 | SQLite CRUD, device upsert, MAC lookup |
| `myhome/devices/cache_test.go` | devices | 17 | In-memory device cache, concurrent access |
| `pkg/shelly/script/timer_test.go` | script | 11 | Script timer debouncing and scheduling |
| `myhome/ctl/config/config_test.go` | config | 5 | CLI argument validation |
| `myhome/ctl/heater/main_test.go` | heater | 1 | Heater CLI parsing |
| `myhome/temperature/testutil_test.go` | temperature | 0 | Helper functions only (no test cases) |

**Total: 6 files, ~55 test functions.**

### Gaps

The following critical areas have **zero test coverage**:

| Area | Key Files | Risk |
|------|-----------|------|
| RPC handler logic | `myhome/temperature/temperature.go:78-109` | 9 handlers with type assertions — panics on wrong type |
| Device manager methods | `myhome/devices/impl/manager.go:76-334` | 10 inline handlers with business logic |
| MQTT client | `myhome/mqtt/client.go` | Connection management, subscription routing |
| RPC server loop | `internal/myhome/server.go` | Request unmarshaling, dispatch, error handling |
| Daemon initialization | `myhome/daemon/daemon.go` | Service orchestration, handler registration order |
| Occupancy service | `myhome/occupancy/rpc.go` | Time-based window logic |

### Proposed Fix

Prioritize by risk:

1. **RPC handler tests** — Test each handler in `temperature.RegisterHandlers()`
   and `manager.go` with valid params, invalid params, and nil params. This
   catches the type-assertion panics (e.g., `in.(string)` on line 77 of
   `manager.go` panics if `in` is not a string).

2. **MQTT client tests** — The existing `mqtt.RecordingMockClient` (used in
   `temperature/testutil_test.go`) provides a foundation. Write tests for
   subscribe/publish/timeout flows.

3. **Occupancy time-window tests** — `IsOccupied()` at
   `myhome/occupancy/rpc.go:48-58` uses `time.Now()` and atomic loads.
   Inject a clock interface to test edge cases around the window boundary.

### Existing Test Infrastructure

The project already has useful test helpers:

- `myhome/storage/db_test.go` — `newTestStorage()` with `t.Helper()`,
  `t.Cleanup()`, and in-memory SQLite.
- `myhome/devices/cache_test.go` — `fakeRegistry` (lines 46-189) with
  call-counting for assertions.
- `myhome/temperature/testutil_test.go` — `newTestService()` factory and
  `mqtt.RecordingMockClient`.

These patterns should be reused and expanded.

---

## 5. Inconsistent RPC Parameter Types

**Severity: Low**

### Problem

Four RPC methods use raw `string` as their parameter type while all other
methods (29 of 33) use typed structs.

### Evidence

In `internal/myhome/methods.go`:

| Method | Param Factory (line) | Actual Type |
|--------|---------------------|-------------|
| `DevicesMatch` | `return ""` (line 46) | `string` |
| `DeviceLookup` | `return ""` (line 54) | `string` |
| `DeviceForget` | `return ""` (line 70) | `string` |
| `DeviceRefresh` | `return ""` (line 78) | `string` |

Compare with every other method:

| Method | Param Factory | Actual Type |
|--------|--------------|-------------|
| `DeviceShow` | `return &DeviceShowParams{}` (line 62) | `*DeviceShowParams` |
| `TemperatureGet` | `return &TemperatureGetParams{}` (line 102) | `*TemperatureGetParams` |
| `SwitchToggle` | `return &SwitchParams{}` (line 262) | `*SwitchParams` |
| ... (25 more) | All use typed structs | |

The handlers in `myhome/devices/impl/manager.go` use unguarded type assertions
on these string params:

```go
// manager.go:77
name := in.(string)  // panics if in is not a string

// manager.go:119
return nil, dm.ForgetDevice(ctx, in.(string))  // same risk
```

### Proposed Fix

Define typed param structs for the four methods:

```go
// internal/myhome/device.go

type DeviceMatchParams struct {
    Pattern string `json:"pattern"`
}

type DeviceLookupParams struct {
    Identifier string `json:"identifier"`
}

type DeviceForgetParams struct {
    Identifier string `json:"identifier"`
}

type DeviceRefreshParams struct {
    Identifier string `json:"identifier"`
}
```

Update `methods.go` signatures and `manager.go` handlers to use
`params.(*DeviceMatchParams).Pattern` instead of `in.(string)`.

### Benefits

- Consistent API surface — every method uses the same pattern.
- Extensibility — adding optional fields (e.g., `Force bool` to
  `DeviceForgetParams`) doesn't require a signature change.
- Safety — struct type assertions are validated by the RPC framework's
  signature system; raw string assertions are not.

---

## 6. Duplicate Result Type Definitions

**Severity: Low**

### Problem

Four result types in `internal/myhome/heater.go` share the exact same
structure but are defined independently:

### Evidence

```go
// heater.go:44-47
type HeaterSetConfigResult struct {
    Success bool   `json:"success"`
    Message string `json:"message,omitempty"`
}

// heater.go:95-98
type RoomCreateResult struct {
    Success bool   `json:"success"`
    Message string `json:"message,omitempty"`
}

// heater.go:109-112
type RoomEditResult struct {
    Success bool   `json:"success"`
    Message string `json:"message,omitempty"`
}

// heater.go:120-123
type RoomDeleteResult struct {
    Success bool   `json:"success"`
    Message string `json:"message,omitempty"`
}
```

All four are byte-for-byte identical except for the type name.

### Proposed Fix

Define a single reusable type:

```go
// internal/myhome/types.go

// ActionResult is the standard result for mutation operations that
// return a success flag and optional message.
type ActionResult struct {
    Success bool   `json:"success"`
    Message string `json:"message,omitempty"`
}
```

Then use type aliases to preserve name semantics in the signature map,
or use `ActionResult` directly:

```go
// methods.go
HeaterSetConfig: { NewResult: func() any { return &ActionResult{} } },
RoomCreate:      { NewResult: func() any { return &ActionResult{} } },
RoomEdit:        { NewResult: func() any { return &ActionResult{} } },
RoomDelete:      { NewResult: func() any { return &ActionResult{} } },
```

### Additional Candidates

`heater.go` also contains room types (`RoomInfo`, `RoomCreateParams`,
`RoomEditParams`, `RoomDeleteParams`, `RoomListResult`) that are logically
separate from heater configuration. These should be extracted to a new
`internal/myhome/room.go` for clarity.

---

## 7. Handler Registration Style Inconsistency

**Severity: Low**

### Problem

Three different patterns are used to register RPC handlers, making the
codebase harder to navigate and extend.

### Evidence

#### Pattern A — Temperature: inline lambdas in `RegisterHandlers()`

**File:** `myhome/temperature/temperature.go:78-109`

```go
func (s *Service) RegisterHandlers() {
    myhome.RegisterMethodHandler(myhome.TemperatureGet, func(ctx context.Context, params any) (any, error) {
        return s.HandleGet(ctx, params.(*myhome.TemperatureGetParams))
    })
    // ... 8 more inline lambdas
}
```

The service itself has a `RegisterHandlers()` method. Lambdas perform type
assertions and delegate to named `Handle*` methods.

#### Pattern B — Occupancy: dedicated `RPCHandler` struct

**File:** `myhome/occupancy/rpc.go:12-39`

```go
type RPCHandler struct {
    service *Service
    log     logr.Logger
}

func (h *RPCHandler) RegisterHandlers() {
    myhome.RegisterMethodHandler(myhome.OccupancyGetStatus, h.handleGetStatus)
}

func (h *RPCHandler) handleGetStatus(ctx context.Context, params any) (any, error) {
    occupied := h.service.IsOccupied(ctx)
    return &myhome.OccupancyStatusResult{Occupied: occupied}, nil
}
```

A separate `RPCHandler` wraps the service. Named methods registered directly
(no lambdas). Clean separation between service logic and RPC wiring.

#### Pattern C — Device Manager: inline closures in constructor

**File:** `myhome/devices/impl/manager.go:76-334`

```go
// Inside NewDeviceManager() constructor:
myhome.RegisterMethodHandler(myhome.DevicesMatch, func(ctx context.Context, in any) (any, error) {
    name := in.(string)
    devices := make([]devices.Device, 0)
    // ... 20+ lines of business logic inline
})
// ... 9 more handlers, all inline in the constructor
```

Handlers are registered during construction; business logic is written
directly inside the closure rather than delegating to named methods.

### Comparison

| Aspect | Temperature (A) | Occupancy (B) | Device Mgr (C) |
|--------|----------------|---------------|-----------------|
| Registration owner | Service itself | Dedicated RPCHandler | Constructor |
| Handler body | Delegates to `Handle*` | Named methods | Inline closures |
| Type assertion | In lambda | In handler method | In closure |
| Testability | Handlers testable via `Handle*` | Handlers testable via RPCHandler | Not independently testable |
| Lines per handler | 3 | 5-10 | 10-30 |

### Proposed Fix

Standardize on **Pattern B** (occupancy) as the canonical pattern. It provides:

- Clear separation: `RPCHandler` struct isolates RPC concerns from service logic.
- Named handler methods: each is individually testable and shows up in stack traces.
- No inline business logic: handler calls service methods, doesn't implement them.

For the Device Manager, extract the 10 inline closures in `manager.go:76-334`
into a separate `myhome/devices/impl/rpc.go` file with a `DeviceRPCHandler`
struct following pattern B.

For Temperature, the migration is smaller — replace the lambdas with named
methods on an `RPCHandler` struct (the `Handle*` methods already exist on the
service).

---

## Summary

| # | Area | Severity | Effort | Quick Win? |
|---|------|----------|--------|------------|
| 1 | `pkg/shelly/` architecture violations | High | Medium | Partly (dead import removal is instant) |
| 2 | YAML library proliferation | High | Low | Yes — mostly find-and-replace |
| 3 | Module consolidation (58 → ~30) | Medium | High | No — large refactor |
| 4 | Test coverage gaps | Medium | Ongoing | Partly (infra exists, needs test cases) |
| 5 | Inconsistent RPC param types | Low | Low | Yes — add 4 structs |
| 6 | Duplicate result types | Low | Low | Yes — extract 1 type |
| 7 | Handler registration inconsistency | Low | Low-Medium | No — touches 3 packages |
