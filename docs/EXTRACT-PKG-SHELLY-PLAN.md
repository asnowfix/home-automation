# Migration Plan: Extract `pkg/shelly` → `github.com/asnowfix/go-shellies`

> Plan to extract the generic Shelly device library from `home-automation` into a standalone, importable Go module at `github.com/asnowfix/go-shellies`.

## Goal

Create a **standalone Go library** that any Go project can `go get` and use to interact with Shelly IoT devices over MQTT and HTTP. The `home-automation` repo then becomes a consumer of this library.

## Target Repository

- **URL:** `https://github.com/asnowfix/go-shellies`
- **Module path:** `github.com/asnowfix/go-shellies`
- **Go version:** 1.24+

## Guiding Principles

1. **Single module** — collapse 16 sub-modules into one module with sub-packages
2. **Zero inward dependencies** — no imports from `myhome`, `internal/`, or `home-automation`
3. **No panics** — return errors from all public functions
4. **No stdout** — use `logr.Logger` for all output; let callers decide presentation
5. **Configurable** — accept MQTT client, embedded scripts, and options via function params (no global singletons)
6. **Incremental** — each phase produces a working state in both repos

---

## Phase 0: Preparation (in `home-automation`)

**Goal:** Clean up pkg/shelly so it's extractable without dragging myhome dependencies.

### 0.1 Remove inward dependencies from pkg/shelly

- [ ] **Remove `_ "myhome/net"` import** from `pkg/shelly/device.go`
  - Investigate what side-effect this blank import provides
  - Move that initialization to `myhome/daemon/` or `myhome/ctl/` where it belongs
  - Verify all tests still pass

- [ ] **Remove `"shelly/scripts"` import** from `pkg/shelly/ops.go`
  - Change `Init()` signature to accept `fs.FS` parameter (or `nil` for no embedded scripts)
  - Move `scripts.GetFS()` call to `myhome/daemon/daemon.go` and `myhome/ctl/` init
  - The `internal/shelly/scripts/` package stays in `home-automation`

- [ ] **Absorb `pkg/devices.Device` interface** into `pkg/shelly/types/`
  - Copy the `Device` interface (6 methods: Manufacturer, Id, Name, Host, Ip, Mac) into `pkg/shelly/types/types.go`
  - Update all pkg/shelly files to use `pkg/shelly/types.Device` instead of `pkg/devices.Device`
  - Keep `pkg/devices` in home-automation for Tapo/SFR; have it import from go-shellies later or keep its own copy

### ~~0.2 Fix module name anomalies~~ (DONE)

All 54 modules renamed to use full `github.com/asnowfix/home-automation/...` paths (208 files modified). This also fixes F1 and F3 from ARCHITECTURE.md. Stale replace directives (`devices`, `mymqtt`, `internal`) removed from root go.mod. Build and all tests pass.

- [x] **Rename all modules** to `github.com/asnowfix/home-automation/<path>` format
- [x] **Rename `schedule`** → `github.com/asnowfix/home-automation/pkg/shelly/schedule`
- [x] Update all imports in 154 .go files and 54 go.mod files
- [x] Fix `internal/myhome/...` imports that used filesystem paths instead of module paths
- [x] Clean up stale replace directives in root go.mod
- [x] Verify `make build` and `make test` pass

### 0.3 Replace panics with errors

- [ ] `device.go:UpdateId()` — return error instead of panic
- [ ] `device.go:UpdateMac()` — return error instead of panic
- [ ] `device.go:init()` — return error instead of panic on nil info
- [ ] `device.go:initMqtt()` — return error instead of panic on empty ID
- [ ] Update all callers to handle the new error returns

### 0.4 Remove stdout from library code

- [ ] `device.go:Foreach()` — remove `fmt.Printf` calls
  - Return structured results; let callers format output
  - Move the summary-printing to `internal/myhome/foreach.go` or `myhome/ctl/`
- [ ] `device.go:Print()` — move to CLI layer or make it a utility the caller opts into

### 0.5 Remove dead code

- [ ] Delete commented-out blocks in `device.go` (lines 168-206, 389-406, 684-714)
- [ ] Delete commented-out methods in `registrar.go`

### 0.6 Add baseline tests

- [ ] Add tests for `Device` creation and property accessors
- [ ] Add tests for `Registrar` method registration and lookup
- [ ] Add tests for `IsGen1Device()`, `IsBluDevice()`
- [ ] Ensure `make test` passes cleanly

**Checkpoint:** After Phase 0, `pkg/shelly` has zero imports from `myhome/*`, `internal/*`, or `shelly/scripts`. All tests pass. The code is still in `home-automation`.

---

## Phase 1: Consolidate into Single Module — DONE (via #359)

**Goal:** Merge 16 sub-modules into a single Go module while still inside `home-automation`.

**Status:** Completed as part of #359 ("collapse 59 Go modules into 4"), which merged pkg/shelly's sub-packages into one `pkg/shelly` module *and* folded everything else in the repo (`myhome/*`, `internal/*`, `hlog`, `pkg/devices`, `pkg/tapo`, `pkg/version`) into the root module in the same pass — a wider scope than this phase originally called for, but the mechanics below (go.mod/go.work surgery) are identical to what #359 actually did. `go.work` now lists exactly 4 modules: `.`, `pkg/shelly`, `pkg/sfr`, `pkg/beem`.

Note for whoever picks up Phase 2 (extraction to `go-shellies`): #359 also fixed the two remaining Phase-0-style couplings that would otherwise have blocked extraction — `pkg/shelly/script/ops.go` was discarding its injected `logr.Logger` and calling `hlog.GetLogger` instead (the last `hlog` import inside pkg/shelly), and the `internal/myhome/net` / `internal/shelly/scripts` / `pkg/devices` imports mentioned in 0.1/0.6 were already gone by the time #359 started (landed via #360). `make check-boundaries` now enforces in CI that pkg/shelly (and pkg/sfr, pkg/beem) never import a root-module package again.

### 1.1 Create unified go.mod

- [x] Create a new `pkg/shelly/go.mod` that declares `module github.com/asnowfix/home-automation/pkg/shelly` with all dependencies aggregated from the 16 sub-module go.mod files
- [x] Remove individual `go.mod` / `go.sum` from each sub-package directory:
  - `pkg/shelly/types/go.mod`
  - `pkg/shelly/mqtt/go.mod`
  - `pkg/shelly/script/go.mod`
  - `pkg/shelly/kvs/go.mod`
  - `pkg/shelly/system/go.mod`
  - `pkg/shelly/wifi/go.mod`
  - `pkg/shelly/ethernet/go.mod`
  - `pkg/shelly/input/go.mod`
  - `pkg/shelly/matter/go.mod` (had no separate go.mod by the time #359 ran)
  - `pkg/shelly/schedule/go.mod`
  - `pkg/shelly/shttp/go.mod`
  - `pkg/shelly/sswitch/go.mod`
  - `pkg/shelly/ble/go.mod` (had no separate go.mod by the time #359 ran)
  - `pkg/shelly/blu/go.mod`
  - `pkg/shelly/gen1/go.mod`
  - `pkg/shelly/ratelimit/go.mod` (had no separate go.mod by the time #359 ran)
  - `pkg/shelly/shelly/go.mod`, `pkg/shelly/typestest/go.mod` (present in the repo at #359 time, not anticipated by this original list)

### 1.2 Update go.work

- [x] Remove all pkg/shelly sub-module entries from `go.work`
- [x] Keep only `./pkg/shelly` entry (alongside `.`, `./pkg/sfr`, `./pkg/beem` — #359's scope was the whole repo, not just pkg/shelly)
- [x] Update replace directives in all consuming modules to point to `./pkg/shelly` only (the root module's go.mod is the only remaining consumer now that everything else folded into it too; it keeps a single `replace .../pkg/shelly => ./pkg/shelly`, per the "Versioning note" below)

### 1.3 Fix internal imports

- [x] All sub-packages now import each other as `pkg/shelly/<sub>` — no change needed since they already use this pattern
- [x] Update any consuming module's `go.mod` replace directives (remove the per-sub-package replaces)

### 1.4 Verify

- [x] `make test` passes
- [x] `make build` produces working binary
- [ ] All CLI commands work against real devices (manual smoke test) — not done as part of #359 (mechanical module-boundary change, no runtime behavior touched); do this before/alongside Phase 2 extraction work

**Checkpoint:** `pkg/shelly` is a single Go module with sub-packages. All ~15 pkg/shelly sub-module entries removed from `go.work` (along with everything else — go.work is down to 4 entries total). All tests pass.

---

## Phase 2: Extract to `go-shellies` Repository

**Goal:** Move the code to the new repo and make `home-automation` consume it.

### 2.1 Initialize the new repository

- [ ] Clone `github.com/asnowfix/go-shellies` (ensure it exists, empty or with just README)
- [ ] Copy `pkg/shelly/` contents into the root of `go-shellies`:
  ```
  go-shellies/
  ├── go.mod              # module github.com/asnowfix/go-shellies
  ├── go.sum
  ├── device.go
  ├── config.go
  ├── ops.go
  ├── mdns.go
  ├── registrar.go
  ├── shelly.go
  ├── types/
  │   └── types.go
  ├── mqtt/
  ├── script/
  ├── kvs/
  ├── system/
  ├── wifi/
  ├── ethernet/
  ├── input/
  ├── matter/
  ├── schedule/
  ├── shttp/
  ├── sswitch/
  ├── ble/
  ├── blu/
  ├── gen1/
  ├── ratelimit/
  └── temperature/
  ```

### 2.2 Rename the module

- [ ] Set `go.mod` to `module github.com/asnowfix/go-shellies`
- [ ] **Global find-replace** all import paths (now using full paths from Phase 0.2):
  - `"github.com/asnowfix/home-automation/pkg/shelly/types"` → `"github.com/asnowfix/go-shellies/types"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/mqtt"` → `"github.com/asnowfix/go-shellies/mqtt"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/script"` → `"github.com/asnowfix/go-shellies/script"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/kvs"` → `"github.com/asnowfix/go-shellies/kvs"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/system"` → `"github.com/asnowfix/go-shellies/system"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/wifi"` → `"github.com/asnowfix/go-shellies/wifi"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/ethernet"` → `"github.com/asnowfix/go-shellies/ethernet"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/input"` → `"github.com/asnowfix/go-shellies/input"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/matter"` → `"github.com/asnowfix/go-shellies/matter"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/schedule"` → `"github.com/asnowfix/go-shellies/schedule"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/shttp"` → `"github.com/asnowfix/go-shellies/shttp"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/sswitch"` → `"github.com/asnowfix/go-shellies/sswitch"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/shelly"` → `"github.com/asnowfix/go-shellies/shelly"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/ble"` → `"github.com/asnowfix/go-shellies/ble"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/blu"` → `"github.com/asnowfix/go-shellies/blu"`
  - `"github.com/asnowfix/home-automation/pkg/shelly/gen1"` → `"github.com/asnowfix/go-shellies/gen1"`
  - `"github.com/asnowfix/home-automation/pkg/shelly"` → `"github.com/asnowfix/go-shellies"`
  - `"github.com/asnowfix/home-automation/pkg/devices"` → `"github.com/asnowfix/go-shellies/types"` (if Device interface was absorbed)
- [ ] Run `go mod tidy` and verify `go build ./...` succeeds

### 2.3 Set up CI in go-shellies

- [ ] Add GitHub Actions workflow: `go test ./...`, `go vet ./...`, `golangci-lint`
- [ ] Add README.md with usage examples
- [ ] Add LICENSE (match home-automation)

### 2.4 Tag initial release

- [ ] Commit and push
- [ ] Tag `v0.1.0`

**Checkpoint:** `github.com/asnowfix/go-shellies` is a standalone, importable Go module. `go get github.com/asnowfix/go-shellies` works.

---

## Phase 3: Migrate `home-automation` to consume `go-shellies`

**Goal:** Replace the local `pkg/shelly/` with the published module.

### 3.1 Update home-automation imports

- [ ] In **every** Go file in `home-automation` that imports `pkg/shelly/*`, update to `github.com/asnowfix/go-shellies/*`
  - `internal/myhome/shelly/` (business logic layer)
  - `internal/shelly/scripts/` (embedded scripts)
  - `myhome/ctl/shelly/` (CLI layer)
  - `myhome/daemon/` (daemon)
  - `myhome/devices/impl/` (device manager)
  - `internal/myhome/device.go`
  - Root module files

### 3.2 Update go.mod files

- [ ] Add `require github.com/asnowfix/go-shellies v0.1.0` to all consuming modules
- [ ] Remove `replace` directives for the old `pkg/shelly` paths
- [ ] During development, use `replace github.com/asnowfix/go-shellies => ../go-shellies` for local iteration

### 3.3 Remove pkg/shelly from home-automation

- [ ] `git rm -r pkg/shelly/`
- [ ] Remove `./pkg/shelly` from `go.work`
- [ ] Update Makefile if it references `pkg/shelly` paths

### 3.4 Handle pkg/devices

- [ ] If Device interface was absorbed into go-shellies:
  - Update `pkg/tapo/` and `pkg/sfr/` to define their own Device interface or import from go-shellies
  - Or keep `pkg/devices` with a minimal interface and have both go-shellies and home-automation satisfy it
- [ ] Decide: does `pkg/devices` stay in home-automation or get its own repo?

### 3.5 Verify

- [ ] `make test` passes
- [ ] `make build` produces working binary
- [ ] All CLI commands work (manual smoke test)
- [ ] Remove local `replace` directive; verify `go get` resolves the published module

**Checkpoint:** `home-automation` no longer contains `pkg/shelly/`. It imports `github.com/asnowfix/go-shellies` as an external dependency. Both repos are independently buildable and testable.

---

## Phase 4: Extract Shelly CLI Tools (Optional)

**Goal:** Provide a standalone CLI tool in `go-shellies` for direct device interaction (no myhome daemon required).

### 4.1 Assess which CLI commands are generic

Commands that don't need MyHome business logic (candidates for extraction):

| Command | Generic? | Notes |
|---|---|---|
| `shelly call` | Yes | Raw RPC call |
| `shelly status` | Yes | Device status |
| `shelly sys` | Yes | System info/config |
| `shelly reboot` | Yes | Device reboot |
| `shelly wifi` | Yes | WiFi config/status/scan |
| `shelly mqtt` | Yes | MQTT config/status |
| `shelly kvs` | Yes | KVS get/set/delete |
| `shelly components` | Yes | List components |
| `shelly script list` | Yes | List scripts |
| `shelly script status` | Yes | Script status |
| `shelly script start/stop` | Yes | Start/stop scripts |
| `shelly script eval` | Yes | Evaluate JS |
| `shelly script upload` | Partial | Generic upload is yes; version tracking is MyHome-specific |
| `shelly script update` | No | Uses MyHome version tracking + embedded scripts |
| `shelly script delete` | Partial | KVS cleanup is MyHome-specific |
| `shelly setup` | No | Full MyHome setup orchestration |
| `shelly follow` | Partial | Generic MQTT follow is yes |
| `shelly jobs` | Yes | Schedule management |

### 4.2 Create CLI in go-shellies

- [ ] Add `cmd/shellies/` in go-shellies repo
- [ ] Build with cobra, minimal flags: `--host`, `--mqtt-broker`, `--device-id`
- [ ] Implement the "Yes" commands from the table above
- [ ] The `home-automation` CLI keeps its versions of these commands (with MyHome wiring) — no need to remove them

### 4.3 Release

- [ ] Tag `v0.2.0` with CLI support
- [ ] Update README with installation and usage

**Checkpoint:** `go install github.com/asnowfix/go-shellies/cmd/shellies@latest` provides a standalone Shelly CLI.

---

## Phase 5: Polish & Harden (Optional)

### 5.1 Dependency injection for globals

- [ ] Replace `var registrar Registrar` singleton with constructor: `NewRegistrar(log, opts...)`
- [ ] Replace `mqtt.SetClient()` global with `mqtt.NewChannel(client)` passed to `Init()`
- [ ] Replace `ratelimit.Init()` global with per-Registrar or per-Device option
- [ ] Replace `deviceMqttRegistry` global with registry owned by the Registrar

### 5.2 Comprehensive test suite

- [ ] Unit tests for Device, Registrar, channel routing
- [ ] Integration test helpers (mock MQTT broker)
- [ ] Test coverage badge in README

### 5.3 Documentation

- [ ] GoDoc comments on all exported types and functions
- [ ] Architecture overview in README
- [ ] Examples in `_example/` directory

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Import path rename breaks many files | Certain | Low | Scripted find-replace; low risk since it's mechanical |
| Sub-module consolidation breaks builds | Medium | Medium | Do it in home-automation first (Phase 1); verify before extraction |
| `myhome/net` blank import has hidden side effects | Medium | Medium | Investigate in Phase 0.1 before removing |
| Embedded scripts coupling is deeper than expected | Low | Medium | Phase 0 decouples; Init() already takes fs.FS |
| `pkg/devices` interface split causes churn | Medium | Low | Keep interface simple; can always add adapter |
| CI/CD needs dual-repo coordination | Certain | Low | Use `replace` directive during development; remove for release |

---

## Timeline Estimate

| Phase | Effort | Dependencies |
|---|---|---|
| Phase 0: Preparation | 2-3 sessions | None |
| Phase 1: Consolidate modules | 1-2 sessions | Phase 0 |
| Phase 2: Extract to go-shellies | 1-2 sessions | Phase 1 |
| Phase 3: Migrate home-automation | 1-2 sessions | Phase 2 |
| Phase 4: Extract CLI (optional) | 2-3 sessions | Phase 2 |
| Phase 5: Polish (optional) | Ongoing | Phase 3 |

Phases 0-3 are the core migration. Phases 4-5 are enhancements.
