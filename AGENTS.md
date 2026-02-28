# Agent Guidelines for Home Automation Project

This document contains project context, coding guidelines, best practices, and important knowledge for AI coding agents working on this project.

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

---

## Shelly Device Scripting

### JavaScript Engine Limitations

Shelly devices run a **modified version of Espruino** JavaScript interpreter. Espruino implements **most of ES5** with some ES6 features, but Shelly's implementation has specific limitations:

#### Official Language Support (from Shelly API Documentation)

**Supported Features:**
- Global scope variables, `let`, `var`
- Function binding (`Function.prototype.bind`)
- `String`, `Number`, `Function`, `Array`, `Math`, `Date` objects
- `new`, `delete` operators
- `Object.keys`, `Object.assign`
- Exceptions
- ES5 Array methods: `Array.isArray`, `[].map`, `[].filter`, `[].forEach`, `[].reduce`, `[].indexOf`, etc.
- `ArrayBuffer` and `AES` (Gen 3/4 devices, firmware 1.6.0+)

**NOT Supported:**
- Hoisting (intentionally omitted - requires two-pass parsing)
- ES6 Classes (function prototypes are supported)
- Promises and async functions
- Regular Expressions (on some boards)
- `\u` escape sequences (use `\xHH` for byte encoding)

**Important Specifics:**
- `arguments.length` returns number of arguments passed (if more than defined) or number defined (if fewer passed)
- `delete` operator works without brackets only
- Strings use byte arrays (not UTF-16), optimized for memory with UTF-8 encoding support

#### Best Practices for Shelly Scripts

1. **Array Methods - SAFE TO USE**
   - ✅ `Array.isArray()` - Supported
   - ✅ `[].map()`, `[].filter()`, `[].forEach()`, `[].reduce()`, `[].indexOf()` - Supported on arrays
   - ✅ `[].push()`, `[].pop()` - Supported (add/remove from end)
   - ❌ `[].shift()`, `[].unshift()` - **NOT supported** (add/remove from beginning)
   - ⚠️ `Array.prototype.slice.call(arguments)` - May fail, use for loops instead
   
   ```javascript
   // AVOID - may not work on arguments object
   var args = Array.prototype.slice.call(arguments);
   
   // PREFER - always works
   var args = [];
   for (var i = 0; i < arguments.length; i++) {
     args.push(arguments[i]);
   }
   
   // BROKEN - shift() not supported
   array.shift(); // Remove first element
   
   // WORKING - manual shift implementation
   var newArray = [];
   for (var i = 1; i < array.length; i++) {
     newArray.push(array[i]);
   }
   array = newArray;
   ```

2. **Variable Declarations**
   - ✅ `var` - Always safe
   - ⚠️ `let`/`const` - Supported in Espruino v2.14+, but `var` is safer for compatibility
   - **Recommendation**: Use `var` for maximum compatibility

3. **Function Definition Order - CRITICAL**
   - ⚠️ **No hoisting**: Functions must be defined BEFORE they are used
   - This applies to ALL function references, including those passed to callbacks
   - Even though JavaScript normally hoists function declarations, Shelly's engine does NOT
   
   ```javascript
   // BROKEN - function used before definition
   function subscribeEvents() {
     Shelly.addEventHandler(onEventData);  // ERROR: onEventData not defined yet
   }
   
   function onEventData(eventData) {
     // Handle event
   }
   
   // WORKING - function defined before use
   function onEventData(eventData) {
     // Handle event
   }
   
   function subscribeEvents() {
     Shelly.addEventHandler(onEventData);  // OK: onEventData already defined
   }
   ```

4. **Minifier-Safe Patterns**
   - Use `"property" in object` instead of `!== undefined` (minifier converts to unsafe syntax)
   
   ```javascript
   // AVOID - minifier breaks this
   var value = obj.prop !== undefined ? obj.prop : null;
   
   // USE - minifier-safe
   var value = ("prop" in obj) ? obj.prop : null;
   ```

5. **Catch Blocks - CRITICAL**
   - ⚠️ **NEVER write empty catch blocks**: `catch (e) {}` - the minifier converts this to `catch {}` (ES2019 optional catch binding) which causes syntax errors
   - ✅ **ALWAYS reference the error parameter**: `catch (e) { if (e && false) {} }`
   - This pattern prevents the minifier from removing the parameter while keeping the catch block functional
   - Apply this to **ALL** catch blocks, even if you don't use the error
   
   ```javascript
   // BROKEN - minifier converts to catch {} which fails
   try {
     data = JSON.parse(str);
   } catch (e) {}
   
   // WORKING - error parameter is referenced
   try {
     data = JSON.parse(str);
   } catch (e) {
     if (e && false) {}  // Prevents minifier from removing parameter
   }
   
   // ALSO WORKING - if you actually use the error
   try {
     data = JSON.parse(str);
   } catch (e) {
     log('Parse error:', e);
   }
   ```

#### Minification Issues

**Issue**: The JavaScript minifier can convert modern syntax (like `catch (e)` to `catch {}` - ES2019 optional catch binding) which may not be supported by Shelly's JavaScript engine.

**Troubleshooting**: Use `--no-minify` flag when debugging script issues:

```bash
go run . ctl shelly script upload device-name script.js --no-minify
```

**Why**: Unminified code is easier to debug and error messages show actual code. However, minification generally works fine if you follow minifier-safe patterns.

**Minifier-Safe Patterns**: Use the `in` operator instead of `!== undefined`:

```javascript
// BROKEN with minifier
var illumMin = value && value.illuminance_min !== undefined ? value.illuminance_min : null;

// WORKING with minifier
var illumMin = value && ("illuminance_min" in value) ? value.illuminance_min : null;
```

**Note**: `Function.prototype.bind()` is fully supported and works correctly with both minified and unminified code.

#### Callback Depth Limits

**Critical Rule**: No Shelly script should use more than **2-3 levels of nested anonymous functions**.

**Official Documentation**: "A limitation of the javascript engine that it cannot parse too many levels of nested anonymous functions. With more than 2 or 3 levels the device crashes when attempting to execute the code."

**Error Symptom**:
```
Uncaught Error: Too many calls in progress
```

**Solution**: Define asynchronous callback functions at the top level and pass them as named references. Where possible, prefer synchronous calls like `Shelly.getComponentStatus()` and `Shelly.getComponentConfig()` to avoid async callbacks altogether.

**Example Refactoring**:

```javascript
// BROKEN - 5+ levels of nesting
function loadData(callback) {
  Shelly.call("KVS.List", {}, function(resp, err) {
    for (var i = 0; i < list.length; i++) {
      (function(k) {
        Shelly.call("KVS.Get", {key: k}, function(gresp, gerr) {
          // More nesting...
        });
      })(list[i]);
    }
  });
}

// WORKING - extracted named functions
function processKey(k, map, onComplete) {
  Shelly.call("KVS.Get", {key: k}, function(gresp, gerr) {
    // Process key
    onComplete();
  });
}

function loadData(callback) {
  Shelly.call("KVS.List", {}, function(resp, err) {
    var pending = list.length;
    function onKeyProcessed() {
      pending--;
      if (pending === 0) callback();
    }
    for (var i = 0; i < list.length; i++) {
      processKey(list[i], map, onKeyProcessed);
    }
  });
}
```

#### Script Lifecycle Logging

Always add startup and stop logging to Shelly scripts:

```javascript
// At script start
log("Script starting...");

// After initialization
log("Script initialization complete");

// On stop event
Shelly.addEventHandler(function(eventData) {
  if (eventData && eventData.info && eventData.info.event === "script_stop") {
    log("Script stopping");
  }
});
```

### Resource Limits

**Official limits per script:**
- No more than **5 timers**
- No more than **5 event subscriptions**
- No more than **5 status change subscriptions**
- No more than **5 RPC calls** (concurrent)
- No more than **10 MQTT topic subscriptions**
- No more than **5 HTTP registered endpoints**

### KVS Key Naming Convention

**All KVS (Key-Value Storage) keys must use only lowercase letters, digits, hyphens, and slashes: `[0-9a-z-/]`**

#### Key Structure

Keys should follow a hierarchical structure with forward slashes as separators:

**For script-specific data:**
```
script/<script-name>/<purpose>
```

**Examples:**
- `script/heater/config` - Main configuration for heater script
- `script/heater/cooling-rate` - Learned cooling rate coefficient
- `script/heater/last-cheap-end` - Temperature at end of cheap window
- `script/heater/internal` - Internal temperature from MQTT
- `script/heater/external` - External temperature from MQTT

**For follow/state patterns:**
```
follow/<category>/<identifier>
state/<category>/<identifier>
```

**Examples:**
- `follow/shelly-blu/e8:e0:7e:d0:f9:89` - BLU device follow configuration
- `follow/status/shelly1minig3-abc123` - Device status follow configuration
- `state/shelly-blu/e8:e0:7e:d0:f9:89` - BLU device state data

#### Naming Rules

1. **Use hyphens, not underscores**: `cooling-rate` ✓ not `cooling_rate` ✗
2. **Use lowercase only**: `script/heater/config` ✓ not `script/Heater/Config` ✗
3. **Use forward slashes for hierarchy**: `script/heater/config` ✓ not `script.heater.config` ✗
4. **Be descriptive but concise**: `last-cheap-end` ✓ not `last_cheap_electricity_window_end_time` ✗
5. **Use consistent prefixes**: All script keys start with `script/`, all follow keys with `follow/`

#### Benefits

- **Consistent naming**: Easy to understand and maintain
- **Namespace isolation**: Prevents key collisions between scripts
- **Easy discovery**: Hierarchical structure allows prefix-based listing
- **URL-safe**: Keys can be used in URLs without encoding
- **Case-insensitive filesystems**: Avoids issues on case-insensitive systems

### Known Issues

#### Non-blocking Execution

Shelly scripts run on the main system task and share CPU time with firmware. Code that blocks for too long can cause:
- Issues with other firmware features
- Communication problems
- Device crashes

**Avoid**:
```javascript
// BROKEN - infinite/near-infinite loops
let n = 0;
while (n < 500000) { n = n + 1; }
```

**Note**: If a script crashes the device, the system will detect this and **automatically disable the script** at the next boot.

#### Error Handling

When a script contains errors:
- Execution is aborted
- Error message is printed to console
- Status change event is issued
- Error info available in `Script.GetStatus` RPC call

---

## Go Development

### Testing Guidelines

**CRITICAL**: Every new feature MUST include incremental unit test cases.

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
   # Run all tests
   go test ./...
   
   # Run specific package tests
   go test ./myhome/devices
   
   # Run with race detector
   go test -race ./...
   
   # Run specific test
   go test -v -run TestFeature_HappyPath ./package
   ```

5. **Example: Sensor update tests**
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

### Testing Shelly Scripts

1. **Local testing**: Use `--no-minify` for easier debugging
2. **Enable debug logging**: `go run . ctl shelly script debug device-name true`
3. **Monitor logs**: Debug output goes to stdout (not hlog)
4. **Disable debug**: `go run . ctl shelly script debug device-name false`

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

### Memory Management

When creating memories during AI interactions:

- **Shelly compatibility issues**: Tag with `shelly`, `javascript`, `compatibility`
- **Go patterns**: Tag with `golang`, `logging`, `commands`
- **Workflow issues**: Tag with `github`, `workflows`, `release`
- **Bug fixes**: Tag with `bug`, `fix`, specific component

---

### Best Practices Summary

### Shelly Scripts

✅ **DO**:
- Use ES5-compatible JavaScript (most ES5 features work)
- Keep callback nesting ≤ 2-3 levels
- Use named functions over anonymous callbacks
- Add startup/stop logging
- Use `--no-minify` for uploads (recommended)
- Use `"property" in object` for property checks (minifier-safe)
- Use `var` for variable declarations (maximum compatibility)
- Use ES5 array methods on arrays: `[].map()`, `[].filter()`, `[].forEach()`

❌ **DON'T**:
- Nest callbacks more than 2-3 levels deep (device will crash)
- Use `Array.prototype.slice.call(arguments)` (may fail)
- Use `!== undefined` (minifier breaks this)
- Use ES6 Classes, Promises, or async/await
- Use Regular Expressions (not supported on all boards)
- Rely on hoisting (not implemented)

### Go Commands

✅ **DO**:
- Print user-facing output to stdout with `fmt.Printf()`
- Use hlog for internal/debug logging
- Add `--verbose` flag to launch configurations
- Provide clear success/failure messages

❌ **DON'T**:
- Use `log.Info()` for user-facing output
- Assume commands run silently

### GitHub Workflows

✅ **DO**:
- Wait for tag propagation (5 seconds) before triggering dependent workflows
- Use `git describe` for version detection
- Check out the correct ref (tag, not branch)

❌ **DON'T**:
- Trigger workflows immediately after creating tags
- Assume tags are instantly available

---

## Changelog

- **2025-10-01**: Initial creation with Shelly scripting, Go development, and GitHub workflow guidelines
- **2025-10-01**: Added callback depth limits and refactoring patterns
- **2025-10-01**: Added command output guidelines and tag propagation fixes
- **2026-01-19**: Added utility package structure guidelines (internal/myhome/ for utilities)
