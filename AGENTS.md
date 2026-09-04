# Agent Guidelines for Home Automation Project

Detailed reference for AI coding agents. `CLAUDE.md` contains the concise always-loaded summary; read sections here when actively working in a specific area.

## Table of Contents

- [Project Goals](#project-goals)
- [Design Philosophy](#design-philosophy)
- [Shelly Device Scripting](#shelly-device-scripting)
- [Go Development](#go-development)
- [GitHub Workflows](#github-workflows)
- [Project Structure](#project-structure)
- [Common Issues and Solutions](#common-issues-and-solutions)

---

## Project Goals

This is a **hobby project** with three explicit, equally-important goals:

1. **Learn Go** — explore Go idioms, concurrency, tooling, and best practices by building real software.
2. **Learn Claude Code** — understand how to work effectively with an AI coding agent as a pair programmer and development collaborator.
3. **Home automation** — build a personal, self-hosted system to control and automate the house using [Shelly devices](https://www.shelly.com/) by Alterco Robotics.

**Implications for an AI agent working on this project**:
- Prefer Go patterns that are idiomatic and educational, not just the shortest path to a working result.
- Keep changes small and well-explained so the owner can learn from them.
- When multiple approaches exist, briefly name the trade-offs rather than silently choosing one.
- This is a solo hobby project — avoid over-engineering; simplicity beats generality.

---

## Design Philosophy

MyHome is designed with the following core principles (from README):

- **Cloud-Independent**: Operates entirely on the local network; no cloud connectivity required.
- **Decentralized**: No central device manager maintaining persistent state. Devices are discovered dynamically when needed.
- **Minimal Infrastructure**: The only required central component is an MQTT broker (lightweight message bus).
- **Ephemeral Discovery**: No "stickiness" — devices join/leave without a persistent registry.
- **Local Control**: All automation logic runs locally; the home works even without internet.

These principles ensure resilience, privacy, and independence from third-party services, while remaining compatible with the manufacturers' own apps (e.g., Shelly app & Cloud).

### Resilience Rules for Agents

Two rules must hold for every change. Check them before marking a task done.

#### Rule 1 — Internet-optional

The system must stay fully functional on the local LAN when the internet is unreachable. Any code that reaches an external URL (weather APIs, firmware update servers, cloud dashboards, package registries at runtime) must:

1. Set an explicit HTTP/dial timeout (no infinite waits).
2. Return a cached value, a zero value, or a no-op — never an error that propagates up and kills unrelated functionality.
3. Log the failure at `warn` level and continue.

**Violation examples**: a goroutine that blocks indefinitely on a DNS lookup; a startup function that `log.Fatal`s when a weather endpoint is unreachable; an HTTP handler that returns 500 because a cloud API is down.

**Test signal**: if you add an external call, add a test (or at minimum a comment) that proves the feature degrades cleanly when the call fails.

#### Rule 2 — Daemon-optional per device

Each Shelly device must keep operating normally when the `myhome` daemon is down. Device scripts run on the device firmware (Espruino/JS) and must not rely on the daemon for their core function.

- Cross-device flows implemented *through* the daemon (device A → MQTT → daemon → device B) are acceptable but must be documented as "degraded when daemon is down".
- A device's local automation (heater script, watchdog, BLU listener) must keep working with only the MQTT broker running and no daemon.
- When refactoring logic from a device script into the daemon, the PR description must name the degraded mode explicitly.

**Violation examples**: moving a script's keep-alive timer into a daemon goroutine; making a device's switch behaviour depend on a live RPC call to `myhome/rpc`; removing a device-side fallback because "the daemon handles it now".

---

## Shelly Device Scripting

**The detail lives in the `shelly` skill: `.claude/skills/shelly/`.** It covers the JavaScript
engine's limits and the patterns that work around them (`references/scripting.md`), the shared JS
heap and how to measure it (`references/memory.md`), where state belongs (`references/storage.md`),
the RPC and CLI surface (`references/api.md`), and the rules for running experiments on live
hardware (`references/field-discipline.md`).

Read the skill before writing or reviewing any Shelly JavaScript, or before touching a device. What
follows is only the set of rules whose violation has actually destroyed something — kept here
because it must not depend on a skill triggering.

### The kill list

Each of these has terminated a running script on real hardware. A dead script on
`filtration-hiver` is a day of lost pool filtration, because every Schedule job on the device
dispatches through `script.eval` and silently becomes a no-op.

1. **Never send an unwrapped `Script.Eval`**, including ad-hoc debug probes. Always
   `(function(){try{ ... }catch(e){return "ERR:"+e}})()` — the return value survives wrapping, so
   there is no case where skipping it is justified. Wrapping genuinely contains a `ReferenceError`
   from an undefined identifier, not just a thrown value — measured 2026-09-02 on **both** Plus1/1.7.5
   and **Pro1/2.0.0 (the production pump)**, where the error came back as a return value and the script
   stayed alive. An earlier note claiming a `ReferenceError` escapes the `try`/`catch` does not
   reproduce; see `.claude/skills/shelly/references/scripting.md`. Prefer the `typeof`-guarded,
   quote-free probe form documented there, so a probe pointed at the wrong build degrades instead of
   throwing.
2. **Never leave a callback unwrapped.** A throw inside any `addEventHandler`, `addStatusHandler`,
   `MQTT.subscribe` callback or queued task kills the script the same way. Wrap the body *in place*,
   not via a higher-order function — an extra call frame per dispatch matters on these devices.
3. **Never call anything on the line after `MQTT.subscribe()`.** It overflows the stack even with
   ~23 KB of heap free. Log *before* the subscribe.
4. **Define functions before use** — there is no hoisting, including for callback references.
5. **Never write an empty `catch (e) {}`** — the minifier turns it into `catch {}`, a syntax error
   on-device. Reference the parameter: `catch (e) { if (e && false) {} }`.
6. **`mem_peak` is a device-wide high-water mark** that does not reset on script restart. Reboot
   between measurement arms or every arm returns the maximum of all arms so far.
7. **Never run two things against one device**, and never two `make test` runs on one machine. Both
   have produced hours of confounded results.
8. **The goja emulator reproduces none of this.** It has no stack-depth limit and no heap ceiling. A
   green test suite says your logic is right and says nothing about whether the script will start on
   a Pro1.

Device authorizations are bounded and explicit — which devices, which operations, which hours. They
are listed in `.claude/skills/shelly/references/field-discipline.md`. Anything not listed is not
authorized.

---

## Go Development

### Testing Guidelines

**CRITICAL**: Every new feature MUST include incremental unit test cases.

#### Sub-agents: do NOT background `make test` — commit first (see issue #432)

`make test` is slow here (`internal/shelly/scripts` alone ~183s, full run several minutes), which
tempts agents into `run_in_background: true`. **Do not do this if you are a sub-agent.** Observed 4
times out of 8 agent runs: the agent backgrounds the test, ends its turn to wait for the completion
notification, and never resumes — so its work is left **uncommitted** and its final report,
measurements and caveats are lost, even though the tests passed.

Rules for sub-agents:

1. **Commit your work BEFORE running the full test suite.** Then a stall costs you the report, never
   the code.
2. **Run `make test` in the foreground** with a generous timeout. Use `go test ./path/...` on a
   single package for fast iteration and save the full `make test` for the end.
3. Do not end your turn waiting on a background task.

If you are the **coordinating** agent and a sub-agent reports "waiting for the background test run",
treat it as finished: review its worktree (`git -C <worktree> status` / `diff`), run the
verification yourself, and commit on its behalf. Do not re-launch it — the work is usually already
complete and correct.

#### Testing Requirements

1. **Write tests before or alongside implementation**
   - Tests help validate the design and catch issues early
   - Tests serve as documentation for how the feature works

2. **Test coverage should include:**
   - ✅ **Happy path**: Normal operation with valid inputs
   - ✅ **Edge cases**: Boundary conditions, empty values, nil pointers
   - ✅ **Error handling**: Invalid inputs, missing data, type mismatches
   - ✅ **Concurrent access**: Thread safety when applicable (use Go's race detector)

3. **Test organization:**
   ```go
   // Group related tests with table-driven approach
   func TestFeature_HappyPath(t *testing.T) {
       tests := []struct {
           name  string
           input string
           want  string
       }{
           {"case1", "input1", "output1"},
           {"case2", "input2", "output2"},
       }
       for _, tt := range tests {
           t.Run(tt.name, func(t *testing.T) {
               // test implementation
           })
       }
   }
   ```

4. **Running tests:**
   ```bash
   # Run all tests (canonical — also covers workspace sub-modules)
   make test

   # Run specific package tests
   go test ./myhome/devices

   # Run with race detector
   go test -race ./...

   # Run specific test
   go test -v -run TestFeature_HappyPath ./package

   # Stress test timing-sensitive packages before pushing (simulates CI load)
   make stress
   ```

5. **Avoid `time.Sleep` in async-protocol tests.** Fixed sleeps that "feel long enough" locally fail silently under CI load because the Go scheduler is starved when many packages run concurrently. Use observable-state polling instead:

   ```go
   // AVOID — drops the event if the async chain takes longer than 400ms
   time.Sleep(400 * time.Millisecond)
   injectEvent(t, ch, payload)
   ok := waitFor(5*time.Second, 50*time.Millisecond, func() bool {
       return stateIsReady()
   })

   // PREFER — re-injects on every poll iteration; robust to any reload latency
   ok := waitFor(8*time.Second, 100*time.Millisecond, func() bool {
       injectEvent(t, ch, payload)
       return stateIsReady()
   })
   ```

   The re-inject pattern works because the system under test drops the event when
   not ready and handles it correctly once it is — so repeated injection is safe
   and the test converges as soon as the async operation completes.

6. **Stress-test before pushing timing-sensitive tests.** CI runners typically have 2 cores and run all packages concurrently; a local fast Mac sees none of this contention. Before pushing any test that involves timers or async sequencing:

   ```bash
   make stress   # GOMAXPROCS=2, -count=10 on internal/shelly/scripts
   ```

   Add the package to the `stress` Makefile target if it contains timing-sensitive tests.

7. **Example: Sensor update tests**
   - See `myhome/devices/cache_test.go` for comprehensive examples
   - Tests cover float/int sensors, error handling, edge cases, and multiple sensors

### Logging System

The project uses a custom logging system (`hlog`) with the following principles:

- **Default to errors-only logging** (much less verbose)
- **Per-package loggers** for better organization
- **Standard log levels**: error, warn, info, debug

#### Usage

```go
// Get a logger for your package
var log = hlog.GetLogger("package/name")

// Or use automatic package detection
var log = hlog.GetCallerLogger()

// Logging
log.Error(err, "message", "key", value)
log.Info("message", "key", value)
```

#### Command-line Flags

- `--log-level <level>`: Set log level (error, warn, info, debug)
- `--verbose` or `-v`: Equivalent to `--log-level debug`
- `MYHOME_DEBUG_INIT=1`: Show hlog initialization messages

### MyHome RPC Service Architecture

**CRITICAL**: All new RPC methods MUST be added to the existing MyHome RPC service, NOT as separate RPC services.

#### Adding New RPC Methods

Follow this pattern (see temperature and occupancy services as examples):

1. **Add verb to `internal/myhome/const.go`:**
   ```go
   const (
       // ... existing verbs
       TemperatureGet      Verb = "temperature.get"
       OccupancyGetStatus  Verb = "occupancy.getstatus"
       YourNewMethod       Verb = "yourservice.method"  // Add here
   )
   ```

2. **Add types to `internal/myhome/yourservice.go` (create new file for each service):**
   ```go
   package myhome
   
   // YourService RPC types
   
   // YourServiceParams represents parameters for yourservice.method
   type YourServiceParams struct {
       Field string `json:"field"`
   }
   
   // YourServiceResult represents the result
   type YourServiceResult struct {
       Data string `json:"data"`
   }
   ```
   
   **Note**: Each service should have its own types file:
   - `internal/myhome/temperature.go` - Temperature RPC types
   - `internal/myhome/occupancy.go` - Occupancy RPC types
   - `internal/myhome/yourservice.go` - Your service RPC types

3. **Add method signature to `internal/myhome/methods.go`:**
   ```go
   var signatures map[Verb]MethodSignature = map[Verb]MethodSignature{
       // ... existing methods
       YourNewMethod: {
           NewParams: func() any {
               return &YourServiceParams{}
           },
           NewResult: func() any {
               return &YourServiceResult{}
           },
       },
   }
   ```

4. **Create handler in your service package (e.g., `myhome/yourservice/methods.go`):**
   ```go
   type MethodHandlers struct {
       service *Service
       log     logr.Logger
   }
   
   func NewMethodHandlers(log logr.Logger, service *Service) *MethodHandlers {
       return &MethodHandlers{
           service: service,
           log:     log.WithName("yourservice.methods"),
       }
   }
   
   func (h *MethodHandlers) RegisterHandlers() {
       myhome.RegisterMethodHandler(myhome.YourNewMethod, h.handleMethod)
       h.log.Info("Your service RPC handlers registered")
   }
   
   func (h *MethodHandlers) handleMethod(params any) (any, error) {
       p, ok := params.(*myhome.YourServiceParams)
       if !ok {
           return nil, fmt.Errorf("invalid params type")
       }
       
       // Your logic here
       return &myhome.YourServiceResult{Data: "result"}, nil
   }
   ```

5. **Register in `myhome/daemon/daemon.go` after device manager starts:**
   ```go
   // Register Your Service RPC methods if enabled
   if options.Flags.EnableYourService {
       log.Info("Registering your service RPC methods")
       
       yourHandlers := yourservice.NewMethodHandlers(log, yourServiceInstance)
       yourHandlers.RegisterHandlers()
       
       log.Info("Your service RPC methods registered")
   }
   ```

#### Why This Pattern?

✅ **Single RPC server** - All methods use the same MQTT topic (`myhome/rpc`)  
✅ **Unified lifecycle** - Methods registered when device manager starts  
✅ **Consistent patterns** - Same request/response structure  
✅ **Easy discovery** - All methods in one place (`internal/myhome/const.go`)  
✅ **Type safety** - Centralized type definitions  

#### Anti-Pattern: DON'T Do This

❌ **Don't create separate RPC servers:**
```go
// WRONG - Don't do this!
func NewRPCService(ctx context.Context) (*RPCService, error) {
    // Subscribing to a different topic
    from, err := mc.Subscribe(ctx, "thetopic/rpc", 1, "package/service")
    // This creates a separate RPC service!
}
```

✅ **Instead, register handlers with the main RPC system:**
```go
// CORRECT - Do this!
func (h *MethodHandlers) RegisterHandlers() {
    myhome.RegisterMethodHandler(myhome.YourMethod, h.handleMethod)
}
```

### Database Patterns

#### SQLite Column Existence Check

When adding database migrations to check if a column exists in SQLite, use `COUNT(*)` which returns an integer, not a boolean.

**Correct pattern:**
```go
var count int
query := `SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name='status'`
err := s.db.Get(&count, query)
if err != nil {
    return fmt.Errorf("failed to check for status column: %w", err)
}

if count == 0 {
    // Column doesn't exist, add it
    log.Info("Adding status column to devices table")
    alterQuery := `ALTER TABLE devices ADD COLUMN status TEXT DEFAULT ''`
    _, err = s.db.Exec(alterQuery)
    if err != nil {
        return fmt.Errorf("failed to add status column: %w", err)
    }
}
```

**Wrong pattern:**
```go
var columnExists bool  // WRONG - COUNT(*) returns int, not bool
query := `SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name='status'`
err := s.db.Get(&columnExists, query)  // This will fail to properly detect
```

**When to use**: Call migration functions in `createTable()` after the initial schema creation to handle upgrades from older database versions.

**Example location**: `myhome/storage/db.go`

### Event Log

#### Events DB path convention

`events.db` lives alongside `devices.db` (default `~/.myhome/events.db`). It is opened by `myhome/events/storage.go` with its own `*sqlx.DB` connection. The path is configurable via `--events-db` / `MYHOME_EVENTS_DB`. Never share the events `*sqlx.DB` with the devices store — independent backup and rotation are a design goal.

#### `SensorDailyTracker` pattern

`SensorDailyTracker` (in `myhome/events/tracker.go`) computes rolling daily min/max/avg for any numeric sensor without storing individual measurement rows. Each sample is upserted to `sensor_daily_stats` immediately, so a daemon restart never loses the current day's running extremes.

To add a new sensor type (e.g. energy kWh):

1. Call `tracker.Observe(ctx, events.Metric{DeviceID: id, Component: "em:0", Metric: "kWh"}, value)` from your listener.
2. That is all — no schema change needed; the `sensor_daily_stats` table uses `(date, device_id, component, metric)` as primary key and accepts any metric name.
3. At midnight, `Flush()` emits a synthetic `<metric>.daily_min` / `<metric>.daily_max` event row automatically.

The tracker is shared across the Gen2, Gen1, and BLU listeners. Always pass `nil` in tests — listeners must guard with `if tracker != nil`.

#### Event severity levels

| Level   | When to use |
|---------|-------------|
| `alarm` | Requires immediate attention: smoke alarm, temperature threshold breach |
| `warn`  | Degraded state, user should notice eventually: battery low (<20%), OTA error, future power-spike threshold |
| `info`  | Normal state changes worth recording: switch on/off, motion, device online/offline, daily stats |
| `debug` | High-frequency or low-significance: button raw events, periodic temperature/humidity changes, OTA progress |

When adding a new event type, default to `info`. Upgrade to `warn`/`alarm` only if the condition is operationally significant. Avoid `debug` for anything a user might want to query later.

### Command Output

**Important**: For user-facing commands (like `script upload`, `script update`, `script debug`), print progress and results to **stdout** using `fmt.Printf()`, not `log.Info()`.

#### Example Pattern

```go
func doUpload(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
    sd, ok := device.(*shelly.Device)
    if !ok {
        return nil, fmt.Errorf("device is not a Shelly: %s %v", reflect.TypeOf(device), device)
    }
    
    scriptName := args[0]
    
    // Print to stdout for user feedback
    fmt.Printf("Uploading %s to %s...\n", scriptName, sd.Name())
    
    id, err := script.Upload(ctx, via, sd, scriptName, minify, force)
    if err != nil {
        fmt.Printf("✗ Failed to upload %s to %s: %v\n", scriptName, sd.Name(), err)
        return nil, err
    }
    
    fmt.Printf("✓ Successfully uploaded %s to %s (id: %d)\n", scriptName, sd.Name(), id)
    return id, nil
}
```

### Script Upload Commands

#### Flags

- `--no-minify`: Do not minify script before upload (recommended for Shelly)
- `--force`: Force re-upload even if version hash matches
- `--verbose`: Enable verbose logging
- `--local-scripts-dir <dir>` (on `script upload` / `script update`, issue #457): load `<dir>/<name>`
  instead of the embedded copy when a same-named file exists there, so a one-line edit can be
  uploaded without a full `go build`. Flat directory only — no subdirectories, no path traversal.
  Falls back silently to the embedded copy when the file is absent, so an empty/unset `dir` (the
  default everywhere, including the packaged service) changes nothing. Every resolution logs its
  source (`local` or `embedded`) at INFO — check the log if a stale local file is ever suspected.
- `--no-local-scripts`: force embedded-only loading, overriding `--local-scripts-dir` even if it is
  set. Local script content is never schema-checked (see #439/#457 design note): it can change
  script *logic* freely, but if it renames or adds `CONFIG_SCHEMA` keys, `internal/myhome/shelly/script/pool.go`'s
  generated Go constants go out of sync with what the script actually reads/writes — that still
  needs a rebuild.

#### Gotchas that cost real debugging time

- **`SCRIPT` is a bare name resolved from the binary's embedded FS (`//go:embed *.js`), NOT a
  filesystem path**, unless `--local-scripts-dir` (issue #457) is given and the directory contains a
  same-named file — a plain `script upload`/`script update` with no such flag reads only the
  embedded copy, and a path like `./scripts/pool-pump.js` still fails with
  `Failed to read script <path>: file does not exist` (`io/fs` wording, the tell that it never
  touched the disk — `LoadScript` requires a flat name, not a path). Without `--local-scripts-dir`,
  **the version uploaded is whatever `internal/shelly/scripts/<name>.js` contained when that binary
  was built**; to upload a different version without rebuilding, point `--local-scripts-dir` at the
  directory holding the edited file instead.
- **A reported upload failure is often a successful upload.** The chunked transfer completes, then a
  post-upload RPC (`KVS.Get` version read, `Script.Start`) times out and the command exits non-zero
  with `all 1 device(s) failed`. Seen on every attempt against a Pro1 at `-T 60s`, `90s` and `120s`.
  **Always verify with `Script.GetStatus` before believing a failure** — see issue #428. Trusting a
  false failure once produced a wrong conclusion about real hardware.
- **`error_msg` is stale until the new script actually runs.** After uploading a new version, the
  device still reports the *previous* crash. Cross-check the message against what the installed
  source actually contains (`Script.GetCode` and grep for a version-specific identifier) before
  believing it. A crash trace naming a function the installed version does not contain is stale.
- **The device is briefly unresponsive to MQTT RPC right after `Script.Start`.** `Script.GetStatus`
  has timed out at 14s and even 45s immediately after a start, then answered normally a minute
  later. Use `-T 60s` and re-poll; do not conclude the device is wedged. The default `-T 14s` is too
  short for the Pro1 generally.

#### Examples

```bash
# Upload with minification (default)
go run . ctl shelly script upload device-name script.js

# Upload without minification (recommended)
go run . ctl shelly script upload device-name script.js --no-minify

# Force re-upload
go run . ctl shelly script upload device-name script.js --force

# Update all scripts on a device
go run . ctl shelly script update device-name

# Update with force
go run . ctl shelly script update --force device-name
```

---

## GitHub Workflows

### Auto-Tagging and Release Process

The project uses automated tagging workflows for releases:

#### Workflow Files

- `.github/workflows/auto-tag-patch.yml`: Auto-tags patch releases on `v*.*.x` branches
- `.github/workflows/auto-tag-minor.yml`: Auto-tags minor releases on `v*.x` branches
- `.github/workflows/package-release.yml`: Builds and releases packages

#### Tag Propagation

**Important**: After creating and pushing a git tag, wait for it to propagate before triggering dependent workflows.

```yaml
- name: Create signed tag
  run: |
    git tag -s "${{ steps.semver.outputs.v_patch }}" -m "Release ${{ steps.semver.outputs.v_patch }}"
    git push origin "${{ steps.semver.outputs.v_patch }}"
- name: Wait for tag to propagate
  run: sleep 5
- name: Trigger Packaging Workflow
  run: |
    curl -X POST ...
```

#### Version Detection

The packaging workflow uses `git describe` to determine the version. For this to work correctly:

1. The tag must exist before the workflow runs
2. The workflow must check out the tag (not the branch)
3. Use `ref: ${{ github.event.ref }}` in checkout action

---

## Configuration Management

### Adding New Configuration Options

**CRITICAL**: When adding any new configuration option (environment variable, CLI flag, or config file option), you MUST update both documentation and example files.

#### Required Updates

For every new configuration option, you must update:

1. **`myhome/ctl/options/options.go`**:
   - Add constant for default value (if applicable)
   - Add field to `Flags` struct with comment indicating the flag name

2. **`myhome/daemon/run.go`** (for daemon options):
   - Add `PersistentFlags()` declaration with flag name, default, and description
   - Add viper config binding in `PreRunE` (for config file support)

3. **`docs/configuration.md`**:
   - Add option to the complete example YAML at the top
   - Add detailed documentation entry in the appropriate section
   - Include: description, default value, flag name, environment variable name
   - Place alongside related options (e.g., MQTT options together)

4. **`myhome-example.yaml`**:
   - Add commented example in the appropriate section
   - Include inline comment explaining the option's purpose
   - Show the default value
   - Place alongside related options

#### Example: Adding `mqtt_reconnect_interval`

**Step 1**: Add to `options.go`:
```go
const MQTT_RECONNECT_INTERVAL time.Duration = 2 * time.Hour

var Flags struct {
    // ...
    MqttReconnectInterval time.Duration // the value taken by --mqtt-reconnect-interval
}
```

**Step 2**: Add to `run.go`:
```go
runCmd.PersistentFlags().DurationVar(&options.Flags.MqttReconnectInterval, 
    "mqtt-reconnect-interval", options.MQTT_RECONNECT_INTERVAL, 
    "Interval for periodic MQTT reconnection to refresh retained messages (0 to disable)")

// In PreRunE:
if v.IsSet("daemon.mqtt_reconnect_interval") && !cmd.Flags().Changed("mqtt-reconnect-interval") {
    options.Flags.MqttReconnectInterval = v.GetDuration("daemon.mqtt_reconnect_interval")
}
```

**Step 3**: Add to `docs/configuration.md`:
```yaml
# In complete example:
daemon:
  mqtt_reconnect_interval: 2h

# In detailed documentation:
**`mqtt_reconnect_interval`** (duration, default: `2h`)
- Interval for periodic MQTT reconnection to refresh retained messages
- Useful after suspend/resume cycles to ensure latest device states
- Set to `0` to disable periodic reconnection
- Flag: `--mqtt-reconnect-interval`
- Env: `MYHOME_DAEMON_MQTT_RECONNECT_INTERVAL`
```

**Step 4**: Add to `myhome-example.yaml`:
```yaml
daemon:
  # MQTT periodic reconnection interval to refresh retained messages (0 to disable)
  # Useful after suspend/resume cycles to ensure latest device states
  # mqtt_reconnect_interval: 2h
```

#### Grouping and Organization

- **Group related options together** in all files
- MQTT options should be near other MQTT options
- Service options should be near other service options
- Maintain consistent ordering across all files

#### Environment Variable Naming

Follow the pattern: `MYHOME_<SECTION>_<KEY>`

Examples:
- `daemon.mqtt_reconnect_interval` → `MYHOME_DAEMON_MQTT_RECONNECT_INTERVAL`
- `temperatures.port` → `MYHOME_TEMPERATURES_PORT`

---

## Project Structure

### Architecture: Shelly Code Organization

The project follows a three-tier architecture for Shelly device code, with clear separation of concerns:

#### 1. `pkg/shelly/` - Generic Shelly API Layer

**Purpose**: Pure, reusable Shelly device API implementation

**Responsibilities**:
- Direct Shelly API calls (RPC methods)
- Generic device operations (reboot, status, configuration)
- Script operations (upload, start, stop, delete)
- MQTT and HTTP channel implementations
- No business logic or application-specific code

**Examples**:
- `pkg/shelly/script/main.go`: `UploadAndStart()`, `StartStopDelete()`, `ListLoaded()`
- `pkg/shelly/device.go`: `Foreach()`, device initialization
- `pkg/shelly/mqtt/`: MQTT channel implementation

**Key Principle**: Code here should work for any Shelly-based application, not just MyHome.

#### 2. `internal/myhome/shelly/` - MyHome Business Logic

**Purpose**: MyHome-specific business logic that combines Shelly operations

**Responsibilities**:
- Application-specific workflows (e.g., version tracking with KVS)
- Combined operations (upload + version check + KVS update)
- MyHome-specific device management
- Business rules and policies

**Examples**:
- `internal/myhome/shelly/script/ops.go`: 
  - `UploadWithVersion()`: Uploads script + tracks version in KVS
  - `DeleteWithVersion()`: Deletes script + cleans up KVS entry

**Key Principle**: This layer orchestrates `pkg/shelly` operations to implement MyHome-specific features.

#### 3. `myhome/ctl/shelly/` - CLI/UI Layer

**Purpose**: User interface and command-line interaction only

**Responsibilities**:
- Command definitions (Cobra commands)
- User-facing output (fmt.Printf)
- Flag parsing
- Calling business logic from `internal/myhome/shelly`
- No business logic implementation

**Examples**:
- `myhome/ctl/shelly/script/start-stop-delete.go`: CLI commands that call `internal/myhome/shelly/script`
- `myhome/ctl/shelly/script/update.go`: Update command with user feedback

**Key Principle**: Thin layer that translates user commands into business logic calls.

#### Architecture Flow

```
User Command (myhome/ctl/shelly)
    ↓
Business Logic (internal/myhome/shelly)
    ↓
Generic Shelly API (pkg/shelly)
    ↓
Shelly Device
```

**Example: Script Upload with Version Tracking**

1. **CLI Layer** (`myhome/ctl/shelly/script/start-stop-delete.go`):
   - Parses command: `ctl shelly script upload device-name script.js`
   - Reads embedded file
   - Calls `mhscript.UploadWithVersion()`
   - Prints success/error messages

2. **Business Logic** (`internal/myhome/shelly/script/ops.go`):
   - Calculates SHA1 version hash
   - Checks KVS for existing version
   - Calls `pkgscript.UploadAndStart()` if needed
   - Updates KVS with new version

3. **Generic API** (`pkg/shelly/script/main.go`):
   - Minifies script (if requested)
   - Creates/finds script slot
   - Uploads code chunks
   - Starts script

### Utility Package Structure

**Rule**: Any utility function specific to MyHome should be placed under the `internal/myhome/` package structure, NOT under `myhome/ctl/`.

#### Why?

- `internal/myhome/` contains shared business logic and utilities that can be used across the application
- `myhome/ctl/` is strictly for CLI commands and user interface code
- Placing utilities under `myhome/ctl/` creates import cycles when multiple CLI packages need the same utility

#### Package Organization

| Location | Purpose | Examples |
|----------|---------|----------|
| `internal/myhome/` | Core MyHome types, client, RPC definitions | `client.go`, `device.go`, `temperature.go` |
| `internal/myhome/blu/` | BLU device utilities | `resolve.go` (MAC address resolution) |
| `internal/myhome/shelly/` | Shelly-specific business logic | `script/ops.go` (version-tracked uploads) |
| `internal/tools/` | Generic utilities (not MyHome-specific) | `normalize.go` (MAC normalization) |
| `myhome/ctl/` | CLI commands only | Command definitions, flag parsing |

#### Example: BLU MAC Resolution

```go
// CORRECT: Utility in internal/myhome/blu/
package blu

import "internal/myhome/blu"

mac, err := blu.ResolveMac(ctx, identifier)

// WRONG: Utility in myhome/ctl/blu/resolve/
// This creates import cycles when myhome/ctl/blu/follow needs it
```

### Script Organization

- `pkg/shelly/script/*.js`: Embedded Shelly scripts
  - `blu-listener.js`: BLE MQTT listener with motion detection and illuminance tracking
  - `blu-publisher.js`: BLE to MQTT publisher
  - `watchdog.js`: MQTT connection watchdog

### Command Structure

- `myhome/ctl/shelly/script/`: Shelly script management commands
  - `upload`: Upload a script to device(s)
  - `update`: Update all scripts on device(s)
  - `debug`: Enable/disable debugging
  - `list`: List scripts on device(s)
  - `start/stop/delete`: Script lifecycle management

### Developer Tools

One-off Go programs in `tools/`. Each is its own workspace module — always add new ones with `go work use ./tools/<name>`. Run from the repo root.

| Tool | Purpose |
|---|---|
| `tools/classify-events/` | Classifies raw Shelly MQTT event dumps → test fixtures in `pkg/shelly/mqtt/testdata/` |
| `tools/genconfigschema/` | Code-gen: JSON schema → JS `CONFIG_SCHEMA` block (in place) + Go KVS-key maps/`Default*` constants |

#### `tools/classify-events`

Reads every `*.json` file from an events dump directory (default: `myhome/events`), groups events by `(method, component, device-type)`, writes one pretty-printed representative per shape to a testdata directory (default: `pkg/shelly/mqtt/testdata/`), then deletes all originals.

```bash
go run ./tools/classify-events                            # use defaults
go run ./tools/classify-events <events-dir> <testdata-dir>
```

Output filenames follow the pattern:
- `notify_status__<device-type>__<component>.json`
- `notify_event__<device-type>__<component>__<event>.json`

The testdata lives alongside `pkg/shelly/mqtt/` so it travels with that package when it is eventually extracted into its own repository.

#### `tools/genconfigschema`

Code-generation tool invoked by `//go:generate` in `myhome/ctl/pool/generate.go`, `myhome/ctl/garden/generate.go`, and `internal/myhome/shelly/script/generate.go`. Reads a single JSON schema per script (`internal/shelly/scripts/pool-pump.schema.json`, `garden.schema.json`) — the source of truth for that script's configuration (issue #439) — and produces both:

- the JS `CONFIG_SCHEMA` (and, for `garden.js`, `ZONE_KEY_SPECS`) block, regenerated **in place** inside the `.js` file between `// >>> GENERATED: ... >>>` / `// <<< GENERATED: ... <<<` marker comments — `description` is emitted as a `//` comment above each field, never as an object property, so it costs zero device heap;
- Go KVS-key maps (`PoolKVSKeys`, `GardenKVSKeys`, `ZoneFieldKeys`) and `Default*` constants, replacing what used to be hand-maintained maps or regex-scraped from the `.js` source.

The Shelly KVS key length limit is validated at generation time (fails the build) instead of relying on hand-counted `// NN chars ✓` comments. Run via `make generate`, not directly:

```bash
go run ./tools/genconfigschema \
    -schema internal/shelly/scripts/pool-pump.schema.json \
    -js internal/shelly/scripts/pool-pump.js \
    -go myhome/ctl/pool/pool_defaults_generated.go -go-package pool -consts
```

---

## Common Issues and Solutions

### Issue: Script Upload Version Mismatch

**Problem**: Release shows version `0.5.4-2-g486115e` instead of `v0.5.6`

**Cause**: Git tag wasn't created or wasn't propagated before the build

**Solution**:
1. Create the tag: `git tag v0.5.6`
2. Push the tag: `git push origin v0.5.6`
3. Ensure workflows wait for tag propagation (5 seconds)

### Issue: Minified Script Syntax Error

**Problem**: `Got '{' expected '('` syntax error in catch blocks

**Cause**: Minifier converts `catch (e)` to `catch {}` (ES2019), which Shelly doesn't support

**Solution**: Use `--no-minify` flag or refactor to use minifier-safe patterns

### Issue: Too Many Calls in Progress

**Problem**: `Uncaught Error: Too many calls in progress`

**Cause**: Too many nested callbacks (>3 levels)

**Solution**: Refactor to use named functions and reduce callback nesting

### Issue: Array Method Errors on Shelly

**Problem**: `Cannot read property 'call' of undefined`

**Cause**: Shelly doesn't support ES5 Array methods

**Solution**: Use ES3-compatible patterns (for loops, manual operations)

---

## Development Workflow

### Planning and Context Survival

**Rule: every non-trivial task must be captured in a plan file before work begins,
and updated after each step completes.**

The goal is to survive context-window overflows: a new session must be able to
read the plan file and continue exactly where the previous session left off,
without any information loss.

#### How to create a plan

1. Before writing any code, create a Markdown plan file under `docs/`.
   Name it after the task, e.g. `docs/test-plan.md`, `docs/refactor-rpc.md`.
2. The file must be **self-contained**: include enough context for a cold start
   (key files, interfaces, design decisions, known pitfalls).
3. Organise work as numbered phases or steps.  Each phase has a clear,
   verifiable completion criterion.

#### How to maintain a plan

- Mark each phase/step **✅ DONE** (with the commit hash if applicable)
  the moment it is complete — before moving on to the next step.
- After marking a step done, commit *both* the implementation and the updated
  plan in the same commit so history stays coherent.
- If scope changes mid-task, update the plan to reflect reality; never let the
  plan and the code drift apart.

#### What to include in a plan file

| Section | Content |
|---|---|
| Purpose / Goal | One-paragraph summary of what the task achieves |
| Current state | What exists today (metrics, passing tests, known failures) |
| Phases / Steps | Numbered, each with a completion criterion |
| Key files | Paths to the most important files and what they contain |
| Interfaces / seams | Interfaces used as injection points for mocks / fakes |
| Known pitfalls | Gotchas discovered during earlier sessions |
| Prerequisite changes | Code changes needed before tests/features can be written |

#### Example skeleton

```
docs/my-feature.md

# My Feature Plan
> Last updated: YYYY-MM-DD — Phase N complete

## Goal
One paragraph.

## Current State
| Metric | Value |

## Phase 1 — ... ✅ DONE (commit abc1234)
### 1-A ...
### 1-B ...

## Phase 2 — ...
### 2-A ...
```

### Go Test Suite

`make test` is the canonical way to run the full test suite.  It runs
`go test ./...` on the root module and then on every sub-module listed in
`go.work`, so no module is silently skipped.

**Rule: any new test command must be wired up in both places:**

| Where | What to update |
|---|---|
| Local | `test` target in [`Makefile`](Makefile) |
| CI | `.github/workflows/test.yml` and `.github/workflows/auto-tag-patch.yml` |

The workflows must always invoke `make test` rather than bare `go test ./...`
so that the Makefile remains the single source of truth for how tests are run.

### Coverage Gate and Ratcheting

`make cover` is `make test`'s coverage-instrumented sibling: it runs every
module the same way `test` does, but with `-covermode=atomic
-coverprofile=...`, and merges the per-module profiles into `coverage.txt` at
the repo root. `.github/workflows/test.yml`'s `build` job runs it on every
push/PR and fails via `scripts/check-coverage.sh "$(cat .coverage-min)"` if
the aggregate drops below the floor recorded in `.coverage-min`.

**When a PR raises coverage, bump `.coverage-min` in that same PR** — this is
how the floor ratchets up over time instead of just preventing regressions
(precedent: commit `9ccc01b`). After running `make cover` locally,
`make cover-min-suggest` prints the integer floor to paste into
`.coverage-min`.

For visibility without needing to run anything locally:
- Every CI run's job summary includes a **per-package coverage breakdown**
  (`go tool cover -func=coverage.txt`), even if the gate fails.
- Every **pull_request** run additionally computes the aggregate coverage
  **delta vs `main`** (a separate `coverage-delta` job) and posts it to the
  job summary — purely informational, it never blocks merging.

### Testing Shelly Scripts

1. **Local testing**: Use `--no-minify` for easier debugging
2. **Enable debug logging**: `go run . ctl shelly script debug device-name true`
3. **Monitor logs**: Debug output goes to stdout (not hlog)
4. **Disable debug**: `go run . ctl shelly script debug device-name false`

**Never reboot the device after enabling UDP debug.** `ctl shelly script debug <device> true`
calls `Sys.SetConfig` and only reboots if the device itself reports `RestartRequired` — do not
add a manual reboot around this command. Always disable debug again once you're done
(`... false`); leaving UDP debug on permanently is not a valid workflow — it degrades device
performance and causes crashes/reboots on its own within a fairly short time.

### Launch Configurations

The project includes VS Code launch configurations in `.vscode/launch.json`:

- Script upload with various flags
- Script update commands
- Debug enable/disable
- All commands include `--verbose` flag for detailed logging

### Git Usage

- **Always use `git mv`** when moving or renaming files during refactoring — never `mv` followed by `git add/rm`, and never delete-and-recreate. `git mv` preserves history and makes the rename visible as a rename (not a delete + add) in `git log --follow` and code review diffs.

```bash
# Correct
git mv internal/myhome/old.go internal/myhome/new.go

# Wrong — loses history
mv internal/myhome/old.go internal/myhome/new.go
git rm internal/myhome/old.go
git add internal/myhome/new.go
```

