# Go Expert Review — home-automation

**Status: COMPLETE** (2026-07-13). Written incrementally so it survives session loss;
findings are numbered and stable. Start at §17 (prioritized action plan) for the TL;DR.

Scope: entire project — layout, module structure, architecture, correctness, concurrency,
error handling, API design, testing, build/release. Deliberately ignores the
"carefulness" conventions in CLAUDE.md; nothing is off the table.

Date: 2026-07-13. Reviewer: Claude (golang-expert-review worktree).

---

## 1. Module & workspace structure

### 1.1 ~59 Go modules in one repository is the single biggest structural problem

`go.work` lists ~59 `use` entries; there are 59 `go.mod` files. This is a
one-binary hobby project — it should almost certainly be **one module** (or two:
the app + a separately-versioned `pkg/shelly` client library if external reuse is
truly intended).

Costs the current layout imposes:

- Every cross-package import is a cross-*module* dependency, so each go.mod
  carries `require` + `replace`-by-workspace bookkeeping; `make tidy` must loop
  over all modules; dependabot opens one PR per module per dep (see recent
  dependabot merge churn in git history).
- `go build ./...`, `go test ./...`, `go vet ./...` don't work from the root —
  the Makefile has to reimplement recursion, and CLAUDE.md itself has a warning
  that "`go generate ./...` does NOT recurse into sub-modules" with a 4-place
  checklist to keep CI green. That checklist is a symptom, not a fact of life.
- Module boundaries at directories like `myhome/ctl/blu/follow` (a single cobra
  subcommand!) have no versioning meaning — nobody will ever `go get` that path.
- go.work.sum / go.sum drift causes phantom CI failures.

**Recommendation (revised per owner decision):** keep separate modules **only
for library trees that could be exported standalone**, collapse everything
else into the root module. Target: **59 modules → 6**. See 1.3 for the
inventory and prerequisites.

### 1.3 Which modules earn a standalone go.mod

Two criteria, both required: (a) **an external importer would actually get
value** — real functionality they can't get more directly elsewhere; (b) it
imports nothing app-specific (`internal/*`, `myhome/*`, `hlog`), or can
cheaply stop. Assessed against the actual code, not the directory names:

**Standalone module — clear external value (1):**

- **`pkg/shelly`** — a Gen2+ Shelly RPC client that works over **MQTT as well
  as HTTP** (script upload/minification, KVS, schedules, components). The
  existing Go ecosystem is HTTP-only; this is the one tree with a genuine
  audience. Today it's 17 modules(!) and has exactly two app-coupling
  violations: a blank import of `internal/myhome/net` (device.go:7) and
  `ops.go` importing `internal/shelly/scripts`. Work: collapse the whole
  tree into **one** module; remove the blank import (§8.5 — register from
  the composition root); invert the scripts dependency — house-specific JS
  (garden, pool-pump, front-door…) has no place in a generic client, the app
  should pass script sources *in*.

**Standalone modules — export decided by owner (2):**

- **`pkg/sfr`** — 750 LOC implementing the SFR box (French ISP router) local
  API including its auth handshake. Niche but real: no widely-available Go
  equivalent. Export-readiness work: replace package-global credentials
  (`sfr.Init(u, p)` mutating package vars, box.go:82) with a `Client`
  struct holding config; today's package-level functions become methods.
- **`pkg/beem`** — Beem Energy API client; same "niche but real" category.
  Export-readiness work: take `logr.Logger` (or `*slog.Logger`) as a
  parameter instead of importing `hlog`, and cut the `myhome/mqtt` import
  (watcher.go:37) — either accept a caller-supplied minimal MQTT interface
  declared in `pkg/beem` itself, or move the MQTT-watcher glue into the app
  and keep only the HTTP client + polling watcher in the library.

**No external value — fold into the root module (the rest of pkg/):**

- **`pkg/devices`** — *not* a library; it's app glue wearing a pkg/ path: a
  `Device`/`Host`/`Switch`/`Button` interface grab-bag, a **global mutable
  provider registry** (`Register`/`listDevicesFuncs`, list.go — §7 again), a
  generic `Filter` that `slices` superseded, and a `MarshalJSON` helper.
  Nobody imports interfaces; Go interfaces belong at the *consumer* (§8.4).
  Fold into the root module. Consequence for `pkg/shelly`, which currently
  imports it: its constructors should return the concrete `*shelly.Device`
  ("accept interfaces, return structs") instead of `devices.Device`
  (device.go:591,628,642), `Foreach` should take `[]*shelly.Device`, and the
  small `Resolver` interface it consumes (mdns.go:18) should be declared
  locally in `pkg/shelly`. That *removes* a dependency edge and fixes an
  idiom violation in one move; the app keeps its own `devices.Device`
  interface internally, which `*shelly.Device` satisfies implicitly.
- **`pkg/tapo`** — 71 lines wrapping `github.com/j-iot/tapo-go`, with
  credentials read from env vars into package globals. An external user
  would import tapo-go directly; there is nothing here to export.
- **`pkg/version`** — 51 LOC of app version/ldflags glue.

Also folded: all `internal/*`, all `myhome/*`, `hlog` (§4 wants it shrunk
anyway), `cmd/*`, `tools/*`.

**Target (owner-decided): 59 modules → 4** — root + `pkg/shelly` +
`pkg/sfr` + `pkg/beem`. Consequences:

- Library modules sit at the bottom of the dependency graph: they never
  import the root module and (as of today's imports) don't need to import
  each other. After the `pkg/shelly` and `pkg/beem` fixes above, zero
  violations remain — add a CI guard so it stays that way (a small test
  running `go list -deps`, or golangci-lint `depguard`).
- The §6 MQTT-interface merge must land the *interface* on the library side
  (`pkg/shelly/mqtt`, where it already is) and the paho *implementation* on
  the app side (`myhome/mqtt`) — the direction §6 recommends anyway.
- `go.work` shrinks to 4 lines; root `go.mod` keeps 3 `replace` directives
  (or none, once library modules get real subdirectory tags,
  `pkg/shelly/vX.Y.Z`, `pkg/sfr/vX.Y.Z`, `pkg/beem/vX.Y.Z`).
- Dependabot: 4 update targets instead of 59.
- `make test` stays the canonical entry point (`go test ./...` covers only
  the root module), now looping over 4 modules instead of 59.

Collapse order (each step independently green):

1. Merge the 16 `pkg/shelly/*` sub-modules into `pkg/shelly` (delete their
   go.mod/go.sum, `go work use` cleanup, one `go mod tidy`).
2. Sever `pkg/shelly`'s app imports: blank `internal/myhome/net` import,
   `internal/shelly/scripts` in ops.go, and the `pkg/devices` dependency
   (return concrete `*shelly.Device`).
3. Decouple `pkg/beem` (logger param, drop `myhome/mqtt`); give `pkg/sfr` a
   `Client` struct.
4. Fold everything else (`internal/*`, `myhome/*`, `hlog`, `pkg/devices`,
   `pkg/tapo`, `pkg/version`, `cmd/*`, `tools/*`) into the root module.
5. Add the depguard/`go list -deps` CI check; simplify Makefile/CI loops to
   the 4 modules; update CLAUDE.md/AGENTS.md (the whole "go generate
   sub-module gap" section reduces to the root-module `make generate`).

### 1.2 Root go.mod carries stale self-requires and replace bookkeeping

The root `go.mod` requires its own sub-modules at pseudo-versions like
`v0.0.0-20260402201030-0ed25e95389f` plus 16 `replace` directives. Some
self-deps appear as `// indirect` with zero-date pseudo-versions
(`v0.0.0-00010101000000-000000000000`). This is pure noise generated by the
multi-module split; with the 4-module target (1.3) it shrinks to three
`replace` directives (or zero, once library modules carry real subdirectory
tags).

---

## 2. Repository hygiene

### 2.1 Debug artifacts and personal data are committed

Tracked at repo root: `goroutine-dump.txt` (57 KB goroutine dump),
`data.js`, `data-shelly.js`, `data-list.json` (device dumps, ~100 KB, contain
real device IDs/IPs from the home network), `myhome.yaml` (a *live* config
including instance names). These are debugging leftovers; delete them and add
patterns to `.gitignore`. `PLAN.md` at root duplicates the `docs/` plan-file
convention.

### 2.2 `cmd/` exists but the main binary lives in `myhome/`

`cmd/` contains only `datacollector` and `fetchasset`, while the real
entry point is `myhome/main.go`, and `tools/` holds more mains
(`classify-events`, `extract-garden-defaults`, `mqtt-show.py`). Standard Go
layout: **all** binaries under `cmd/<name>/main.go` (`cmd/myhome`,
`cmd/datacollector`, `cmd/classify-events`, …). The current mix means three
different conventions for "where is a main package" in one repo. It also
frees the `myhome/` tree to be pure library packages (or better, move them
under `internal/`).

### 2.3 Package tree splits `internal/myhome` vs `myhome/*` arbitrarily

Business logic lives in *both* `internal/myhome/...` and `myhome/...`
(daemon, devices, mqtt, storage, temperature, occupancy, notify…). Nothing
under `myhome/` except `main.go` needs to be importable from outside; after
collapsing modules, move all of it under `internal/` and keep only
deliberately-public API under `pkg/`. One rule instead of three.

---

## 3. Entry point (`myhome/main.go`) — concrete bugs

### 3.1 CPU-profile block is duplicated verbatim (real bug)

`PersistentPreRunE` contains the *same* CPU-profile block twice
(myhome/main.go:52–62 and 70–80), each with a nested duplicate
`if options.Flags.CpuProfile != ""` inside itself. With `--cpuprofile` set,
`pprof.StartCPUProfile` runs twice; the second call fails (error ignored) and
the first `*os.File` in the context is overwritten and leaked.

### 3.2 Error handling and context-value abuse around profiling

- `pprof.StartCPUProfile(f)` error is never checked.
- The open `*os.File` is stored in `context.WithValue` and never closed;
  `PersistentPostRunE` checks `f != nil` only to run `defer pprof.StopCPUProfile()`
  — a `defer` as the last act of a function is a plain call, and the file is
  still not closed, so the tail of the profile can be lost.
- `ctx.Value(global.CancelKey).(context.CancelFunc)` is an unchecked type
  assertion — any code path that didn't stash the cancel func panics the CLI.

Context values are the wrong tool for both. Idiomatic shape: package-level
(or closure-captured) variables in `main.go` for the profile file and cancel
func, checked errors, `defer f.Close()`.

### 3.3 Daemon detection by string-matching command names

`cmd.Name() == "daemon" || cmd.Parent().Name() == "daemon"` breaks on
deeper nesting and is repeated logic. Use `cmd.Annotations` or set a flag in
the daemon command's own `PersistentPreRunE` instead of sniffing names from
the root.

---

## 4. Logging (`hlog`) — global state and a case for log/slog

- `hlog.Logger` is a mutable package-level global initialized by
  `Init*()` variants. Any package calling `hlog.GetLogger()` at
  `init()`/package-var time gets a zero `logr.Logger` (before `Init` ran) —
  a classic init-order trap.
- Four exported Init variants (`Init`, `InitWithDebug`, `InitForDaemon`,
  `InitForDaemonWithDebug`) whose bodies are one-liners into
  `InitWithLevel`. One function with options is enough; better, return the
  logger instead of setting a global: `hlog.New(opts) (logr.Logger, error)`.
- `Init` panics on log-writer failure (hlog/hlog.go:67) — a CLI should
  degrade to stderr, not crash.
- The dependency stack is heavy for a hobby daemon: logr → zerologr →
  zerolog (+ lumberjack + kardianos/service just to detect "interactive").
  Since Go 1.21, **`log/slog`** covers structured logging in the stdlib;
  `logr` has an official slog bridge if the logr API must stay. This would
  delete most of `hlog` and three dependencies.
- Environment sniffing (`VSCODE_PID` forces debug level) is surprising
  action-at-a-distance: running the binary from a VS Code terminal silently
  changes log level. Make it explicit (flag or documented env var only).

---

## 5. The MQTT RPC core (`internal/myhome`) — correctness first

This is the highest-risk code reviewed so far. `client.go` / `server.go` /
`methods.go`.

### 5.1 No request/response correlation — concurrent calls cross-wire (bug)

`client.CallE` publishes a request with a random `request_id`, then waits for
*whatever message arrives next* on its subscription channel
(internal/myhome/client.go:186). The response's `Id` is never compared with
the request's. Two goroutines calling `CallE` concurrently (the UI does
concurrent HTMX requests) can receive each other's responses — type-confused
results or spurious unmarshal errors. Fix: a response dispatcher goroutine
that routes by `Dialog.Id` to a per-call channel (`map[string]chan response`
under a mutex), which also makes timeouts per-call.

### 5.2 `client.start()` swallows errors → send on nil channel (deadlock bug)

`start()` returns nothing; if `Subscribe` or `Publisher` fails it just logs
(internal/myhome/client.go:50–62) — but `hc.me` is already set, so retries
are skipped, and `hc.to` stays nil. The next `CallE` does `hc.to <- reqStr`:
**send on a nil channel blocks forever**, ignoring ctx. `start` must return
an error, and `CallE` must propagate it.

### 5.3 Methods on `client` call the global singleton instead of the receiver

`LookupDevices`/`ForgetDevices` are methods on `hc *client` but invoke
`TheClient.CallE(...)` (internal/myhome/client.go:104–106,133) instead of
`hc.CallE(...)`. Works only while exactly one client exists and `TheClient`
is set — a landmine for tests and a second client. Also `TheClient` itself:
prefer passing the client as a dependency; the codebase already threads
`ctx`/loggers everywhere, one more parameter is cheap.

### 5.4 Double (un)marshal instead of `json.RawMessage`

Both sides unmarshal the full message once to peek at the method, then
unmarshal the *whole message again* into typed params
(internal/myhome/server.go:62,85); the client re-marshals `res.Result` and
re-unmarshals it into the typed result (client.go:206–213). Declare
`Params json.RawMessage` / `Result json.RawMessage` in the wire structs and
unmarshal each exactly once into the typed value. Simpler, faster, and
removes the "re-do Unmarshalling with proper types" comment.

### 5.5 The verb/signature registry should be generics, not `any` + reflect

`signatures` is a 270-line map of `func() any` constructors
(internal/myhome/methods.go:43), `CallE` checks parameter types with
`reflect.TypeOf` at runtime, and CLAUDE.md documents a 4-step ritual for
adding a method. A generic registration helper collapses all of it:

```go
func Register[P, R any](verb Verb, h func(ctx context.Context, p P) (R, error)) {
    methods[verb] = &Method{
        Name:      verb,
        NewParams: func() any { return new(P) },
        Action: func(ctx context.Context, raw json.RawMessage) (any, error) {
            var p P
            if err := json.Unmarshal(raw, &p); err != nil { return nil, err }
            return h(ctx, p)
        },
    }
}
```

Registration becomes one line per method, the signatures map disappears,
params are compile-time-typed, and the 4-step checklist becomes a 1-step one.
A typed client helper `Call[P, R](ctx, c, verb, p P) (R, error)` gives the
same benefit caller-side.

### 5.6 Assorted RPC-core issues

- `-E` suffix naming (`NewServerE`, `CallE`, `MethodE`, `ActionE`) is not a
  Go convention — returning an error is the default, no suffix needed.
- `NewServerE` panics when the context has no logger (server.go:24,48);
  library code should never panic on a missing logger — fall back to
  `logr.Discard()` or take the logger as a parameter.
- Server processes messages strictly sequentially in one goroutine — one
  slow handler (device RPC with 10 s timeout) head-of-line-blocks every
  other RPC, including the UI. Spawn per-message goroutines (bounded).
- `mc.Publish` errors are ignored in both response and `fail` paths
  (server.go:124,145), and `fail` ignores the `json.Marshal` error.
- `server.go` sets `res.Result = &tempResult` then unconditionally overwrites
  it with `&out` (server.go:93,114) — dead code from an abandoned
  type-check attempt, along with the commented-out `reflect.TypeOf` block and
  the many `// mc / to` comment corpses. Delete dead code; git remembers.
- `CallE` returns `Method{}` as the `any` result on the unknown-method error
  path (client.go:151) — inconsistent; return `nil`.
- Client timeout uses `time.After(hc.timeout)` *in addition to* ctx; fold it
  into `context.WithTimeout` so cancellation and timeout share one path.
- `RandStringBytesMaskImprRandReaderUnsafe` (blog-post name) — replace with
  `crypto/rand` + `hex.EncodeToString`, or a UUID; name it `newRequestID`.
- `*[]devices.Device` / `*[]DeviceSummary` pointer-to-slice returns —
  slices are already references; return `[]devices.Device`.
- Imports in client.go are unsorted/ungrouped (stdlib mixed with modules) —
  gofmt doesn't fix grouping; run `gci`/`goimports` in CI.

---

## 6. Two MQTT client stacks, and a library that imports CLI flags

### 6.1 `pkg/shelly/mqtt` vs `myhome/mqtt` duplicate the same abstraction

Both define a `Client` interface with near-identical methods, both define the
QoS constants (`AtMostOnce`/`AtLeastOnce`/`ExactlyOnce`), and each has its own
global singleton (`pkg/shelly/mqtt.SetClient/GetClient` registry;
`myhome/mqtt.theClient` + `GetClientE`). `myhome/mqtt.client` is the only real
implementation; `pkg/shelly` talks to it through its duplicate interface via
the global registry. Keep **one** small `mqtt.Client` interface in one package
(arguably `pkg/mqtt`), one implementation, and pass it explicitly to
constructors. The `SetClient`-panics-if-different + `ResetClient`-for-tests
dance is the classic global-singleton test smell.

### 6.2 Dependency inversion: the MQTT library imports the CLI options package

`myhome/mqtt/client.go` imports `myhome/ctl/options` (the cobra/viper flags
package) to read configuration. A transport library must not know about
command-line flags; it should take a config struct/functional options in its
constructor. This single import makes the MQTT client untestable without the
CLI package and is the root of several import-cycle workarounds the repo
documents.

### 6.3 `myhome/mqtt.client` struct stores a `context.Context`

`client.ctx context.Context` as a struct field ("process-wide context for
background services") is explicitly discouraged (`go vet` has a check
coming; the context package docs forbid it). Pass ctx to the methods that
need it; keep a `close` channel or a stored `cancel` only for shutdown.

---

## 7. Global mutable state inventory

Cross-cutting problem; each of these is a package-level mutable variable that
functions reach for instead of receiving a dependency:

| Global | Where | Used by |
|---|---|---|
| `hlog.Logger` | hlog | everything |
| `myhome.TheClient` | internal/myhome/proxy.go:9 | all `myhome/ctl/*`, `Foreach`, even *methods of the client type itself* (5.3) |
| `mqtt.theClient` + `mqttOps` + `mqttBroker` | myhome/mqtt | daemon + ctl |
| `pkg/shelly/mqtt.client` registry | pkg/shelly/mqtt | all device RPC |
| `deviceMqttRegistry` | pkg/shelly/device.go:112 | all `shelly.Device` instances |
| `options.Flags` + `options.ViperConfig` | myhome/ctl/options | CLI **and** libraries (6.2) |
| `signatures`/`methods` maps | internal/myhome/methods.go | RPC (why RPC tests can't be parallel, per CLAUDE.md) |
| `global.PanicOnBugs` | internal/global | scattered |

The daemon already has a natural composition root (`myhome/daemon`): build the
logger, MQTT client, storage, device manager there and pass them down. Almost
every "must not call t.Parallel()", "restore state in t.Cleanup", "panic: MQTT
client already set" rule in CLAUDE.md/AGENTS.md is a direct consequence of
this table. Constructor injection deletes the rules.

---

## 8. `pkg/shelly` API design

### 8.1 Trailing-underscore exported fields (`Id_`, `Name_`, `Host_`, `MacAddress_`)

`shelly.Device` exports fields named with a trailing underscore to dodge the
collision with getter methods `Id()`, `Name()`, `Host()`
(pkg/shelly/device.go:117–120). This is not a Go naming pattern from any
style guide. Two clean options: (a) unexported fields + `MarshalJSON`/
`UnmarshalJSON` (or a small `deviceJSON` DTO struct), keeping the accessor
methods; (b) exported plain fields and no getters. Given the `devices.Device`
*interface* requires the methods, (a) is the fit. Same applies to the
getter-name style: Go uses `Owner()`, not `GetOwner()` — the codebase is
mostly right here, keep it that way when refactoring.

### 8.2 `Device` mixes value/entity, transport, cache, and logger

One struct holds identity (id/mac/name/host), live MQTT channels, a `dialogs
sync.Map`, cached info/config/status, a `modified` flag, and a logger. That's
why the `deviceMqttRegistry` global exists — Device values get recreated from
the DB and must re-find their transport. Separate the **data** (a plain
serializable struct) from a **session/transport** object owned by the device
manager, and the registry becomes an ordinary map inside the manager.

### 8.3 Hand-maintained parallel enum arrays

`types.Api` is an iota enum whose `String()` indexes a 23-element array
literal (pkg/shelly/types/types.go). Any insertion in one list without the
other silently shifts every name after it. Use `//go:generate stringer -type=Api`
or a `map[Api]string` checked by a test.

### 8.4 Interface bloat

`types.Device` has ~25 methods (String, Name, Host, Manufacturer, Id, Mac,
ReplyTo, To, From, StartDialog, StopDialog, IsHttpReady, IsMqttReady, Channel,
UpdateName, UpdateHost, ClearHost, UpdateMac, UpdateId, IsModified,
ResetModified, CallE, …) — the `FakeDevice` in the same package needs ~30
lines of no-op stubs to satisfy it. "The bigger the interface, the weaker the
abstraction." Most consumers need only `CallE` + identity accessors; define
narrow interfaces where they're *consumed* (`type caller interface { CallE(...) }`
in the op packages) and let `*shelly.Device` satisfy them implicitly.

### 8.5 Blank-import side-effect coupling

`pkg/shelly/device.go:7` does `_ "github.com/asnowfix/home-automation/internal/myhome/net"`
— a generic device library blank-importing an *internal myhome* package for
its side effects. Besides being a pkg→internal layering violation, init-time
side effects are invisible to readers. Register explicitly from the
composition root instead.

### 8.6 Dead code as comments

`Refresh()` carries ~40 lines of commented-out wifi/ethernet/system-config
logic (pkg/shelly/device.go:168–205); server.go, client.go, main.go similar.
Delete it — git history preserves it, and stale comment-code actively
misleads (it references fields/functions that have since changed).

---

## 9. Concurrency & lifecycle

- **36 raw `go func` sites, 1 `WaitGroup`, 0 `errgroup`** in non-test code.
  Background goroutines (server loop, watchdog, subscribers, proxies) have no
  join point: shutdown is "cancel ctx and hope". `golang.org/x/sync/errgroup`
  in the daemon's run function gives structured startup/shutdown: each
  service is `g.Go(...)`, `g.Wait()` propagates the first failure.
- `go http.ListenAndServe(...)` at pkg/shelly/gen1/proxy.go:36 discards the
  error — if the port is taken the proxy is silently absent forever.
  daemon.go:98 logs it, but neither restarts nor stops the daemon; decide:
  fatal or degraded, but not "log to debug and continue".
- HTTP servers are built without timeouts (`&http.Server{Addr: addr}` at
  internal/myhome/ui/server.go:36; bare `ListenAndServe` for pprof). Set
  `ReadHeaderTimeout` at minimum; LAN-only softens but doesn't remove it.
- `myhome/ctl/ctl.go:93` calls `os.Exit(0)` mid-command — skips all defers
  (including MQTT graceful disconnect and CPU-profile stop). Return instead.
- CLAUDE.md notes RPC-handler tests "must not call t.Parallel()" — the fix is
  making the registry an injected struct (7.), not documenting the hazard.
- **CI never runs `go test -race`** (test.yml runs `make cover` only). For a
  codebase this goroutine-heavy, add a `-race` job; it will likely surface
  real races around the subscriber maps and client `start()`.

---

## 10. Error handling

- Wrapping is inconsistent: ~263 `fmt.Errorf` with `%w` but a large share
  still interpolate with `%v`/`(%v)` (e.g. pkg/shelly/device.go:147
  `"unable to init MQTT (%v)"`), which breaks `errors.Is/As` chains.
  Standardize on `%w` (a golangci-lint `errorlint` rule enforces it).
- Almost no sentinel errors or typed errors exist; callers string-match or
  just propagate. Define the handful that matter (`ErrDeviceNotFound`,
  `ErrNotConnected`, `ErrTimeout`) in the owning packages so callers can
  branch with `errors.Is`.
- Library code panics on missing logger/misconfiguration (server.go:25,
  hlog init, `SetClient`). Reserve panics for programmer errors detectable
  only at runtime *inside* the package; return errors otherwise.
- Errors logged **and** returned (e.g. storage constructors log then
  `return nil, err`; client.go logs then returns) double-report every
  failure. Pick one: return it and let the top level log.

---

## 11. Storage

Generally the strongest layer reviewed (WAL pragmas are well-reasoned and
well-commented, `db.SetMaxOpenConns(1)` rationale documented). Remaining:

- Queries use non-context `Exec/Query/Get/Select` almost everywhere
  (28 non-ctx vs 5 ctx calls). Use the `...Context` variants so daemon
  shutdown/timeouts can cancel in-flight queries.
- Device `info`/`config` stored as JSON TEXT columns is fine for this scale —
  no change needed; resist the temptation to normalize.
- Two separate DB layers (`myhome/storage` for devices, `myhome/temperature/storage.go`)
  each open their own SQLite database with their own migration logic. Fine at
  this scale, but share the open/pragma/migrate bootstrap helper.

---

## 12. Tooling, CI, style enforcement

- **No `.golangci.yml`, no `go vet` in CI.** This is the highest-leverage
  cheap fix in the whole review: `golangci-lint` with `govet, staticcheck,
  errcheck, errorlint, unused, ineffassign, gci, revive` would have caught
  mechanically: the duplicated profile block (3.1), ignored errors (5.6, 9),
  `%v` wrapping (10), unsorted imports, dead assignments (server.go:93).
- Constants use SCREAMING_SNAKE_CASE (`HTTP_DEFAULT_PORT`,
  `MQTT_DEFAULT_TIMEOUT`, `BROKER_SERVICE`, `HOSTNAME`) across
  `myhome/ctl/options` and `myhome/mqtt`. Go style is MixedCaps:
  `DefaultHTTPPort`, `DefaultMQTTTimeout`. `revive` flags these.
- Coverage gate exists (`.coverage-min`, check-coverage.sh) — good — but
  quality gates (vet/lint/race) matter more than the coverage number.
- The Makefile's `mods=` wildcard-glob recursion and per-module loops all
  evaporate with the single-module change (1.1): `test:` becomes
  `go test ./...`, `tidy:` becomes `go mod tidy`.
- `go.work`-era env pinning `GOTOOLCHAIN=go1.25.3` in Makefile *and*
  workflow env *and* go.work `toolchain` — pick one source of truth
  (the `toolchain` directive; delete the rest).

---

## 13. Naming and package conventions

- **`-E` suffix** (`CallE`, `NewServerE`, `NewClientE`, `MethodE`, `ActionE`,
  `GetClientE`) — not a Go convention; the error return *is* the signal.
  Rename in one sweep: `Call`, `NewServer`, `NewClient`, …
- **`pkg/shelly/shelly`** — import path stutters (`shelly/shelly.DeviceInfo`);
  same family: `sswitch` ("shelly switch"), `shttp`, `myhome/myhome` shapes.
  After the module merge, rename: `pkg/shelly/rpc` or fold `shelly/shelly`
  contents into parent; `sswitch` → `switchx` is no better — prefer
  `pkg/shelly/relay` (what the component actually is) and `pkg/shelly/http`
  (import-aliased where it clashes with net/http, which is rare in practice).
- Interface named `Server` whose only method is `MethodE(Verb) (*Method, error)`
  (server.go:18) — that's a `MethodResolver`/`Registry`, not a server. And
  `NewServerE` takes a `handler Server` and returns a `Server` wrapping it —
  circular naming that hides what anything does.
- `RandStringBytesMaskImprRandReaderUnsafe` — see 5.6.
- Receiver names: `hc`, `sp`, `d`, `m` mixed with full words; pick short
  consistent receivers per type (Go style: 1–2 letters, consistent).

---

## 14. Testing

- 74 test files is genuinely good for a hobby project; benchmarks exist
  (storage, digest), fakes exist (`FakeDevice`, `RecordingMockClient`,
  `fake_client.go`). The foundations are there.
- Main gaps: the RPC client/server core (the buggiest code found, §5) has
  tests for methods/registry but nothing exercising *concurrent* `CallE` or
  subscription failure paths — exactly where the bugs are. A race-enabled
  test with two parallel `CallE` calls against a fake MQTT client would
  catch 5.1 immediately.
- `FakeDevice` living in the production `types` package ships test doubles
  in the real dependency graph. Move it to `types/typestest` (stdlib
  pattern: `net/http/httptest`, `testing/fstest`).
- Global registries force `ResetClient()`-style test hooks and forbid
  parallel tests (7.) — dependency injection is also the *testing* fix.

---

## 15. Daemon composition & big functions

- `NewDaemon` pulls its cancel func out of a context value with an unchecked
  type assertion (`ctx.Value(global.CancelKey).(context.CancelFunc)`,
  myhome/daemon/daemon.go:69) — same anti-pattern as 3.2; a daemon should own
  `context.WithCancel` itself.
- `daemon.go` imports the same package twice under two aliases:
  `mqttclient` and `mqttserver` both alias `myhome/mqtt` (daemon.go:21–22).
  Legal Go, but it manufactures the illusion of two components.
- `Start()` runs `go d.Run()` and discards `Run`'s error — if startup fails
  (DB locked, port taken), the service manager believes the daemon is up.
  Propagate: run synchronously to a "started" barrier, or report the error
  through the service API.
- The daemon also depends on `myhome/ctl/options` (daemon.go:17) — daemon
  configured by CLI-flag globals, same inversion as 6.2.
- Giant functions in `pkg/shelly/script/run.go`: `createShellyRuntime` is
  ~600 lines, `createMethodsMap` ~340 lines (run.go:144, 764). Split by
  concern (one file or constructor per emulated Shelly API: Timer, MQTT,
  KVS, HTTP…) — same total code, far easier to navigate and test.
  `internal/myhome/ui/htmx.go` (791 lines) and
  `myhome/devices/impl/manager.go` (941 lines) deserve the same treatment.

---

## 16. Dependencies

- The root binary links **Google Cloud auth + gRPC + OpenTelemetry + protobuf**
  transitively via `internal/myzone/gcp_dns.go` (Cloud DNS updates). That's
  megabytes of binary and a large supply-chain surface for one feature. The
  Cloud DNS v1 REST API is a couple of `net/http` calls with an OAuth token;
  alternatively keep the SDK but note this is the single heaviest dependency
  decision in the project.
- `logr + zerologr + zerolog + lumberjack` — see 4; `log/slog` (+lumberjack
  if file rotation is truly needed — journald/launchd already rotate) shrinks
  this to at most one external dep.
- `kardianos/service` is used both for real service management *and* as an
  "am I interactive?" oracle inside hlog — keep the former, replace the
  latter with an explicit flag.
- Both `gopkg.in/yaml.v2` and `go.yaml.in/yaml/v3` and `sigs.k8s.io/yaml`
  appear in the root go.sum graph; after the module merge, `go mod tidy`
  and consciously converge on one YAML library.
- Dependabot churn (one PR per module × 59 modules) also collapses with 1.1.

---

## 17. Prioritized action plan

Ordered by leverage-per-effort; each item is independently shippable.

1. **Fix the RPC client correctness bugs (§5.1, 5.2, 5.3)** — response
   correlation by request ID, `start()` returning errors, receiver instead
   of `TheClient` inside methods. These are live bugs.
2. **Fix `main.go` duplicated CPU-profile block (§3.1–3.2)** — 15-minute fix.
3. **Add `golangci-lint` + `go vet` + a `-race` test job to CI (§12, §9)** —
   prevents whole classes of the above from recurring. Do this *before* big
   refactors so the refactors land against a linted baseline.
4. **Collapse 59 modules → 4 (§1.3, owner-decided)**: root module +
   standalone `pkg/shelly`, `pkg/sfr`, `pkg/beem`. Follow the 5-step
   collapse order at the end of §1.3: merge the 16 shelly sub-modules,
   sever `pkg/shelly`'s app imports (incl. dropping `pkg/devices` by
   returning concrete `*shelly.Device`), decouple beem / restructure sfr,
   fold the rest into root, then add the CI import-boundary guard.
5. **Delete committed debug artifacts & dead comment-code (§2.1, §8.6)**.
6. **Generics-based RPC registry (§5.5) + `json.RawMessage` wire types
   (§5.4)** — removes the 4-step method ritual and the reflection.
7. **Dependency injection pass (§7)**: kill `TheClient`, `options.Flags`
   reads from libraries, MQTT singletons; daemon becomes the composition
   root. Unlocks parallel tests.
8. **Merge the two MQTT abstractions (§6)**.
9. **Layout normalization (§2.2, 2.3, §13)**: all mains under `cmd/`,
   libraries under `internal/`, rename `-E` functions, de-stutter packages,
   MixedCaps constants.
10. **`log/slog` migration (§4, §16)** — optional, do last; the logr API is
    fine, this is about dependency weight and global state.

---

## 18. Overall grade: 11.5 / 20

Calibration: 10 = "works, but I'd be nervous maintaining it"; 14 = "solid
codebase with localized debt"; 17+ = "exemplary open-source Go".

**What earns points:**

- **Testing culture (best-in-class for a hobby project):** 74 test files,
  benchmarks, purpose-built fakes (`FakeDevice`, `RecordingMockClient`), a CI
  coverage gate with a ratcheted minimum, and a coverage-delta job on PRs.
  Most hobby projects have none of this.
- **The storage layer** (§11): WAL/synchronous pragmas with *correct,
  well-reasoned comments*, the `SetMaxOpenConns(1)` in-memory trap documented.
  This is senior-level care.
- **Operational maturity:** systemd units + timers, Debian/MSI packaging,
  Prometheus metrics, pprof, release automation, watchdogs, documented
  resilience principles (internet-optional, daemon-optional).
- **Documentation habits:** ARCHITECTURE.md, AGENTS.md, plan files, and code
  comments that explain *why* (buffer sizes, reconnection strategy).
- Errors are handled (not swallowed wholesale), contexts are threaded through
  most APIs, structured logging is universal.

**What costs points:**

- **Live correctness bugs in the RPC core** (§5.1–5.3): no request/response
  correlation under concurrency, a nil-channel deadlock path, methods
  bypassing their own receiver for a global. This is the daemon's spine. −3
  on its own.
- **Global mutable state as the default wiring** (§7): eight package-level
  singletons; the CLI flags package imported by transport libraries (§6.2).
  The architecture documents rules ("don't call t.Parallel()") to survive
  its own globals instead of removing them.
- **No static analysis at all** (§12): no vet, no golangci-lint, no -race in
  CI — and the mechanically-findable defects (duplicated profile block,
  ignored errors, dead assignments) are all still there, proving the cost.
- **Structural overhead:** 59 modules for one binary (§1), three layout
  conventions for mains (§2.2), duplicated MQTT abstraction (§6.1).
- **Hygiene lapses:** committed goroutine dump and device dumps with real
  IPs (§2.1), ~40-line blocks of commented-out code (§8.6), copy-paste
  duplication in main.go (§3.1).
- Unidiomatic surface: `-E` suffixes, `Id_` fields, SCREAMING_SNAKE
  constants, 25-method interface, pointer-to-slice returns (§8, §13).

**Trajectory note:** the weaknesses are concentrated and fixable (items 1–4
of §17 alone would lift this to ~14); the strengths — tests, ops, docs — are
the hard-to-retrofit part, and they're already here.

---

## Review complete

All planned areas covered. The checklist below is fully done; this document
is the final deliverable.

---

## Coverage checklist (all done)

- [x] Survey repo root, go.work, module list (§1)
- [x] Root module: main package(s), cmd/ layout, entry points (§2, §3)
- [x] Repo-root clutter (§2.1)
- [x] hlog custom logger vs log/slog (§4)
- [x] internal/myhome RPC system (§5)
- [x] pkg/shelly API design (§8)
- [x] Error handling patterns (§10)
- [x] Concurrency: goroutine lifecycles, context propagation (§9)
- [x] Globals/singletons audit (§7)
- [x] Testing: coverage, patterns, race detector, mocks (§14)
- [x] Build/CI/lint (§12)
- [x] Storage layer (§11)
- [x] Dependencies audit (§16)
- [x] Naming/package conventions (§13)
- [x] Daemon composition & big functions (§15)
- [x] Prioritized summary (§17)
