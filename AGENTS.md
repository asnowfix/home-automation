# Agent Guidelines for Home Automation Project

This document contains coding guidelines, best practices, and important knowledge for AI coding agents (like Cascade) working on this project.

## Table of Contents

- [Shelly Device Scripting](#shelly-device-scripting)
- [Go Development](#go-development)
- [GitHub Workflows](#github-workflows)
- [Project Structure](#project-structure)
- [Common Issues and Solutions](#common-issues-and-solutions)

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
