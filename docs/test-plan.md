# Test Suite Plan

> **Purpose**: Durable reference for building a comprehensive Go test suite.
> Safe to use across context resets — written to be self-contained.
> Last updated: 2026-02-26 — Phase 1 complete

---

## Table of Contents

- [Current State](#current-state)
- [Key Findings](#key-findings)
- [Phase 0 — Build System Fixes](#phase-0--build-system-fixes)
- [Phase 1 — Shared Test Infrastructure](#phase-1--shared-test-infrastructure)
- [Phase 2 — temperature Service Tests](#phase-2--temperature-service-tests)
- [Phase 3 — occupancy Service Tests](#phase-3--occupancy-service-tests)
- [Phase 4 — internal/myhome RPC Tests](#phase-4--internalmyhome-rpc-tests)
- [Phase 5 — pkg/shelly/script Extended Tests](#phase-5--pkgshellyScript-extended-tests)
- [Phase 6 — pkg/shelly/mqtt Channel Tests](#phase-6--pkgshellyMqtt-channel-tests)
- [Prerequisite Code Changes](#prerequisite-code-changes)
- [Module Wiring Rule](#module-wiring-rule)

---

## Current State

| Metric | Value |
|---|---|
| Workspace modules | 54 |
| Modules with tests | 5 (9.3%) |
| Test files | 5 |
| Lines of test code | ~2 100 |

### Passing tests

| Module | Test file | Coverage |
|---|---|---|
| `myhome/storage` | `db_test.go` | 19 tests — schema, CRUD, rooms, MAC-path quirk |
| `myhome/devices` | `cache_test.go` | 14 tests — all key types, miss, Flush, concurrency |
| `pkg/shelly/script` | `timer_test.go` | 12 tests — timer lifecycle |
| `myhome/ctl/heater` | `main_test.go` | 1 test — KVS key ↔ JS schema parity |
| `myhome/ctl/config` | `config_test.go` | 6 tests — CLI flag validation |

### Pre-existing failures

| Failure | Root cause | Fix (see Phase 0) |
|---|---|---|
| `FAIL myhome/ctl/heater` | Missing `door-sensor-topics` key in `heaterKVSKeys` — fixed by user in `main.go` before this plan was written | Already fixed |
| `FAIL ./... [setup failed]` from `myhome/ctl/temperature` | Module is nested inside `myhome/ctl/` (also a workspace module). When `go test ./...` runs from the nested dir in workspace mode, Go confuses package path resolution. Module has **no test files**. | Phase 0: skip modules with no `*_test.go` in Makefile loop |

---

## Key Findings

### Interfaces usable as test seams

| Interface | Defined in | Used by |
|---|---|---|
| `devices.DeviceRegistry` | `myhome/devices/device.go` | `devices.Cache`, `myhome/storage` |
| `mqtt.Client` (myhome) | `myhome/mqtt/client.go` | `temperature.Service`, `occupancy.Service`, `myhome/server.go` |
| `mqtt.Client` (shelly) | `pkg/shelly/mqtt/client.go` | Shelly device channels |
| `myhome.Server` | `internal/myhome/server.go` | RPC server loop |

### Existing mock / stub types

| Type | Location | Implements |
|---|---|---|
| `mqtt.MockClient` | `pkg/shelly/mqtt/mock.go` | `pkg/shelly/mqtt.Client` — but NOT `myhome/mqtt.Client` |
| `mqtt.RecordingMockClient` | `myhome/mqtt/mock.go` | `myhome/mqtt.Client` — records publishes, injects via Feed |
| `fakeRegistry` | `myhome/devices/cache_test.go` | `devices.DeviceRegistry` — call counting, thread-safe |
| `newTestStorage()` | `myhome/storage/db_test.go` | Helper — creates in-memory SQLite |
| `newTestDB/Storage/Service()` | `myhome/temperature/testutil_test.go` | Test helpers for temperature package |

### Global state risks

`internal/myhome/methods.go` holds a **package-level** `methods map[Verb]*Method`.
`RegisterMethodHandler` panics on unknown verbs and overwrites existing entries silently.
Tests that call `RegisterMethodHandler` must either:
- run sequentially (no `t.Parallel()`), or
- restore the previous handler in `t.Cleanup()`.

### `myhome/ctl/heater` not in go.work

`myhome/ctl/heater/go.mod` exists on disk but `./myhome/ctl/heater` is **absent from
`go.work`**. It is tested indirectly when `myhome/ctl`'s `go test ./...` traverses
the `heater/` sub-directory in workspace mode. The Makefile `mods` glob finds its
`go.mod` and also tries to run `cd myhome/ctl/heater/ && go test ./...` — this works
because the module is **not** in `go.work` (no workspace path confusion).

---

## Phase 0 — Build System Fixes ✅ DONE

**Goal**: make `make test` exit 0 when all actual tests pass.

### 0-A  Iterate from `go.work`, skip modules with no `*_test.go` ✅

The old `$(foreach m,$(mods),...)` iterated over all `go.mod` files found on disk via
filesystem globs. Two problems:

1. `myhome/ctl/temperature` is a workspace module nested inside `myhome/ctl/` — running
   `go test ./...` from it triggered a Go workspace path-resolution error.
2. `myhome/ctl/heater` has no `go.mod` (it is a plain sub-package of `myhome/ctl`), so a
   shallow `ls *_test.go` check on the parent skipped its tests entirely.

**Implemented solution** — `Makefile` `test` target now:

```makefile
test: build
    $(GO) test ./...
    @rc=0; for dir in $$(awk '/\t\.\//{sub(/\t\.\//, ""); print}' go.work); do \
      if find $$dir \( -mindepth 1 -type d -exec test -f "{}/go.mod" \; -prune \) \
              -o \( -type f -name "*_test.go" -print -quit \) 2>/dev/null | grep -q .; then \
        (cd $$dir && $(GO) test ./...) || rc=1; \
      fi; \
    done; exit $$rc
```

Key design decisions:
- **`awk` not `grep`** to extract go.work entries — `grep '^\t\./'` fails because single-quoted
  `\t` is a literal backslash-t in POSIX sh, not a tab.
- **`go.work` as iteration source** — the canonical module list; sub-module entries like
  `myhome/ctl/temperature` that have no test files are skipped cleanly.
- **`find -exec test -f go.mod \; -prune`** — recursive test-file search that stops at
  sub-module directory boundaries, so `myhome/ctl/heater/*_test.go` is found via
  `myhome/ctl`'s entry without double-running the nested `myhome/ctl/temperature`.

Result of `make test` after this change:

```
ok  myhome/ctl/config
ok  myhome/ctl/heater
ok  myhome/devices
ok  myhome/storage
ok  pkg/shelly/script
```

---

## Phase 1 — Shared Test Infrastructure ✅ DONE

### 1-A  Recording MQTT mock for `myhome/mqtt.Client` ✅

**File**: `myhome/mqtt/mock.go`
**Package**: `mqtt` (exported so other modules can import it)

`myhome/mqtt.Client` has more methods than `pkg/shelly/mqtt.MockClient`:

```go
type Client interface {
    GetServer() string
    BrokerUrl() *url.URL
    Id() string
    Subscribe(ctx, topic, qlen, name) (<-chan []byte, error)
    SubscribeWithHandler(ctx, topic, qlen, name, handler) error
    SubscribeWithTopic(ctx, topic, qlen, name) (<-chan Message, error)
    Publish(ctx, topic, payload, qos, retained, name) error
    Publisher(ctx, topic, qlen, qos, retained, name) (chan<- []byte, error)
    IsConnected() bool
    Close()
}
```

Implement a `RecordingMockClient` that:

- Records every `Publish()` call: `published map[string][][]byte` (topic → ordered payloads)
- Allows pre-seeding messages: `Feed(topic string, payload []byte)` — puts a message
  into the channel returned by the next `Subscribe()` / `SubscribeWithTopic()` for that topic
- Thread-safe via `sync.Mutex`
- `Published(topic string) [][]byte` — returns all payloads published to a topic
- `Reset()` — clears recorded state between subtests
- Configurable error injection: `InjectError(method string, err error)`

**Constructor**:
```go
func NewRecordingMockClient() *RecordingMockClient
```

**Note**: This mock lives in `myhome/mqtt` (production package), exported but only
used in tests of modules that import `myhome/mqtt`. Placing it there avoids a
circular dependency.  If that is a concern, move it to a `myhome/mqtt/mqtttest`
sub-package and wire the go.mod accordingly.

### 1-B  `newTestDB()` helper for temperature storage ✅

**File**: `myhome/temperature/testutil_test.go`

```go
func newTestDB(t *testing.T) *sqlx.DB {
    t.Helper()
    db, err := sqlx.Connect("sqlite3", ":memory:")
    if err != nil { t.Fatalf("open :memory: db: %v", err) }
    t.Cleanup(func() { db.Close() })
    return db
}

func newTestStorage(t *testing.T) *Storage {
    t.Helper()
    s, err := NewStorage(testr.New(t), newTestDB(t))
    if err != nil { t.Fatalf("NewStorage: %v", err) }
    return s
}
```

### 1-C  `newTestService()` helper for temperature service ✅

```go
func newTestService(t *testing.T) (*Service, *mqtt.RecordingMockClient) {
    t.Helper()
    store := newTestStorage(t)
    mc := mqtt.NewRecordingMockClient()
    ctx := logr.NewContext(context.Background(), testr.New(t))
    svc := NewService(ctx, testr.New(t), mc, store)
    return svc, mc
}
```

---

## Phase 2 — `myhome/temperature` Tests

### 2-A  Pure unit tests (no DB, no MQTT)

**File**: `myhome/temperature/temperature_test.go`
**Package**: `temperature` (white-box)

#### `TestIsInTimeRange` — table-driven

| Case | Start | End | Time | Expected |
|---|---|---|---|---|
| Normal range, inside | 360 | 1380 | 720 | true |
| Normal range, before start | 360 | 1380 | 300 | false |
| Normal range, at end (exclusive) | 360 | 1380 | 1380 | false |
| Midnight crossing, before midnight | 1380 | 360 | 1400 | true |
| Midnight crossing, after midnight | 1380 | 360 | 200 | true |
| Midnight crossing, outside | 1380 | 360 | 720 | false |

#### `TestParseTime` — table-driven

Test valid (`"06:00"` → 360), boundary (`"23:59"` → 1439), invalid format,
invalid hour (24), invalid minute (60).

#### `TestGetDayType`

Setup a `Service` with no storage (inject empty maps directly via struct literal —
white-box access). Test:

- Monday → `DayTypeWorkDay` (built-in default)
- Saturday (weekday 6) → `DayTypeDayOff`
- Sunday (weekday 0) → `DayTypeDayOff`
- Weekday default map overrides built-in (Monday configured as `DayTypeDayOff`)
- External API set and returning a value takes precedence

#### `TestGetComfortRanges`

- Room with single kind, schedule present → returns that kind's ranges
- Room with two kinds → returns union of both kinds' ranges (deduplication)
- Room with kind but no schedule for that day type → returns empty
- Room not found → returns error

#### `TestIsComfortTime`

- Currently comfort time → true
- Currently eco time → false
- Midnight crossing range, test at 23:30 (inside) and 04:00 (inside)

### 2-B  Storage tests

**File**: `myhome/temperature/storage_test.go`
**Package**: `temperature` (white-box)
Uses `newTestStorage(t)` from 1-B.

| Test | Behaviour verified |
|---|---|
| `TestNewStorage_CreatesSchema` | No error; tables exist |
| `TestSaveRoom_Insert` | Room inserted; `modified=true` returned |
| `TestSaveRoom_SameData` | No change; `modified=false` |
| `TestSaveRoom_UpdatedField` | Change detected; `modified=true` |
| `TestListRooms_Empty` | Returns empty map |
| `TestListRooms_Populated` | Returns inserted rooms |
| `TestSaveKindSchedule` | Insert and retrieval round-trip |
| `TestGetKindSchedules_Filter` | Filter by kind and/or day-type |
| `TestSaveWeekdayDefault` | Insert Monday → DayTypeWorkDay |
| `TestGetWeekdayDefaults` | Returns full 7-day map |

### 2-C  RPC handler tests

**File**: `myhome/temperature/methods_test.go`
**Package**: `temperature` (white-box)
Uses `newTestService(t)` from 1-C.

| Test | Handler | What to assert |
|---|---|---|
| `TestHandleSet_MissingRoomID` | `HandleSet` | Returns error |
| `TestHandleSet_MissingEcoLevel` | `HandleSet` | Returns error |
| `TestHandleSet_Valid` | `HandleSet` | Returns `status=ok`; room in `s.rooms` |
| `TestHandleGet_Found` | `HandleGet` | Returns matching `TemperatureRoomConfig` |
| `TestHandleGet_NotFound` | `HandleGet` | Returns error |
| `TestHandleList_Empty` | `HandleList` | Returns empty list |
| `TestHandleList_Populated` | `HandleList` | Returns all rooms |
| `TestHandleDelete_Found` | `HandleDelete` | Room removed |
| `TestHandleDelete_NotFound` | `HandleDelete` | Returns error |
| `TestHandleRoomCreate` | `HandleRoomCreate` | Room created; persisted |
| `TestHandleRoomEdit` | `HandleRoomEdit` | Room updated |
| `TestHandleRoomDelete` | `HandleRoomDelete` | Room removed |
| `TestHandleSetKindSchedule` | `HandleSetKindSchedule` | Schedule stored |
| `TestHandleGetKindSchedules` | `HandleGetKindSchedules` | Returns stored schedules |
| `TestHandleSetWeekdayDefault` | `HandleSetWeekdayDefault` | Default persisted |
| `TestPublishRangesUpdate` | `PublishRangesUpdate` | MQTT Publish called with correct topic |

For `TestPublishRangesUpdate`: use `RecordingMockClient.Published("myhome/rooms/r1/temperature/ranges")` to assert payload is valid JSON.

---

## Phase 3 — `myhome/occupancy` Tests

### 3-A  Prerequisite code change — `LanChecker` interface

`occupancy.Service` calls `sfr.Client` directly (concrete type from `pkg/sfr`).
To enable mocking without network access, extract an interface:

**Change in `myhome/occupancy/occupancy.go`**:

```go
// LanChecker abstracts LAN host polling (implemented by sfr.Client in production).
type LanChecker interface {
    GetHosts(ctx context.Context) ([]sfr.Host, error) // or equivalent
}
```

Change `Service` to hold `LanChecker` instead of the concrete `sfr.Client`.
Production code passes a real `sfr.Client`; tests pass a `fakeLanChecker`.

### 3-B  Occupancy unit tests

**File**: `myhome/occupancy/occupancy_test.go`
**Package**: `occupancy` (white-box)

Fake MQTT client: `mqtt.RecordingMockClient` from Phase 1-A.
Fake LAN checker: inline `fakeLanChecker` struct.

| Test | Scenario | Expected |
|---|---|---|
| `TestOccupancy_RecentInputEvent` | Input event just now | `occupied=true`, `reason="input"` |
| `TestOccupancy_StaleInputEvent` | Input event > window ago | `occupied=false` |
| `TestOccupancy_MobileDeviceSeen` | Mobile seen just now | `occupied=true`, `reason="mobile"` |
| `TestOccupancy_MobileDeviceStale` | Mobile seen > window ago | `occupied=false` |
| `TestOccupancy_BothSources` | Both recent | `reason` includes both |
| `TestOccupancy_NeitherSource` | No events | `occupied=false`, `reason="none"` |

---

## Phase 4 — `internal/myhome` RPC Tests

### 4-A  Method registration tests

**File**: `internal/myhome/methods_test.go`
**Package**: `myhome` (white-box — access to `methods` map)

**Important**: tests must NOT call `t.Parallel()` due to the global `methods` map.
Each test that registers a handler must restore the previous value in `t.Cleanup`.

Helper:
```go
func withHandler(t *testing.T, v Verb, h MethodHandler) {
    t.Helper()
    prev := methods[v]
    RegisterMethodHandler(v, h)
    t.Cleanup(func() {
        if prev == nil {
            delete(methods, v)
        } else {
            methods[v] = prev
        }
    })
}
```

| Test | Behaviour |
|---|---|
| `TestRegisterMethodHandler_KnownVerb` | Handler stored; `Methods(verb)` returns it |
| `TestRegisterMethodHandler_UnknownVerb_Panics` | Panics with meaningful message |
| `TestMethods_Unregistered` | Returns error, not panic |
| `TestMethods_Registered` | Returns `*Method` with correct name |
| `TestSignatures_AllHaveNewParams` | Every entry in `signatures` has non-nil `NewParams` |
| `TestSignatures_AllHaveNewResult` | Every entry in `signatures` has non-nil `NewResult` (or documents the nil-result verbs) |
| `TestMethodHandler_Dispatch` | Registered handler is called with correct params type |

### 4-B  RPC server tests

**File**: `internal/myhome/server_test.go`
**Package**: `myhome`

The `server` struct subscribes to an MQTT topic and dispatches incoming JSON messages.
This requires a mock MQTT client that can inject inbound messages.

Needed: a mock implementing `myhome/mqtt.Client` (Phase 1-A `RecordingMockClient`)
AND able to feed messages into the subscribe channel.

| Test | Behaviour |
|---|---|
| `TestNewServerE_SubscribesToServerTopic` | After construction, MQTT client has a subscriber for `ServerTopic()` |
| `TestServer_DispatchKnownMethod` | Inject a valid JSON-RPC message; verify the registered handler is called |
| `TestServer_UnknownMethod_ReturnsError` | Inject unknown method; verify error response published back |
| `TestServer_ContextCancellation` | Cancel context; server goroutine exits |

---

## Phase 5 — `pkg/shelly/script` Extended Tests

**File**: `pkg/shelly/script/compat_test.go`
**Package**: `script`

These document Shelly JS engine constraints using goja as the test runtime.

| Test | What it validates |
|---|---|
| `TestScript_NoHoisting` | Function used before definition causes runtime error |
| `TestScript_CatchParameterRequired` | `catch {}` (no param) is syntax error; `catch (e) {}` works |
| `TestScript_ArrayShiftUnsupported` | `[].shift()` is undefined or throws |
| `TestScript_CallbackDepthLimit` | Deeply nested anonymous functions fail |
| `TestScript_InOperatorMinifierSafe` | `"key" in obj` works; documents the `!== undefined` risk |
| `TestScript_VarOverLet` | `var` always works; documents `let` compatibility notes |
| `TestScript_BindSupported` | `Function.prototype.bind()` works |
| `TestScript_ES5ArrayMethods` | `map`, `filter`, `forEach`, `reduce`, `indexOf` all work |

---

## Phase 6 — `pkg/shelly/mqtt` Channel Tests

**File**: `pkg/shelly/mqtt/channel_test.go`
**Package**: `mqtt`

The existing `MockClient` in `pkg/shelly/mqtt/mock.go` is minimal (discards publishes,
returns empty subscribe channels). Extend it or write a new `RecordingClient` here
with the same `Feed`/`Published` API as Phase 1-A.

| Test | Behaviour |
|---|---|
| `TestMockClient_Subscribe_ReturnsChannel` | Subscribe returns a readable channel |
| `TestMockClient_Publish_Recorded` | Published payloads can be retrieved |
| `TestMockClient_Feed_MessageDelivered` | Seeded message appears on subscribe channel |
| `TestMockClient_Publisher_DrainsSafely` | Publisher channel does not block |

---

## Prerequisite Code Changes

These are **minimal changes to production code** required before some phases:

| Change | File | Required for | Complexity |
|---|---|---|---|
| Extract `LanChecker` interface | `myhome/occupancy/occupancy.go` | Phase 3 | Low |
| Wire `heater` into go.work | `go.work` | Cleaner test isolation for heater | Low (1 line) |
| (Optional) `StorageInterface` for temperature | `myhome/temperature/storage.go` | Phase 2 unit tests (currently using `:memory:` SQLite instead) | Medium |

---

## Module Wiring Rule

From `AGENTS.md`:

> **Any new test command must be wired in both places**:
>
> | Where | What |
> |---|---|
> | Local | `test` target in `Makefile` |
> | CI | `.github/workflows/test.yml` and `.github/workflows/auto-tag-patch.yml` |
>
> New test modules (those that add a `go.mod`) must also be added to `go.work`
> (unless deliberately kept standalone like `myhome/ctl/heater`).

---

## Execution Order

```
Phase 0   → Makefile fix (skip no-test modules)
Phase 1-A → myhome/mqtt/mock.go (RecordingMockClient)
Phase 1-B → myhome/temperature/storage_test.go helpers
Phase 1-C → myhome/temperature/newTestService helper
Phase 2-A → myhome/temperature/temperature_test.go (pure unit)
Phase 2-B → myhome/temperature/storage_test.go
Phase 2-C → myhome/temperature/methods_test.go
Phase 3-A → occupancy LanChecker interface (code change)
Phase 3-B → myhome/occupancy/occupancy_test.go
Phase 4-A → internal/myhome/methods_test.go
Phase 4-B → internal/myhome/server_test.go
Phase 5   → pkg/shelly/script/compat_test.go
Phase 6   → pkg/shelly/mqtt/channel_test.go
```

Phases 1–2 are independent of 3–6 and can be implemented in parallel.
Each phase within its own module can be worked on independently.
