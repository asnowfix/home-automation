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

Shelly devices run a **modified version of Espruino** JavaScript interpreter with specific limitations and capabilities:

#### Official Language Support (from Shelly API Documentation)

**Supported Features:**
- Global scope variables, `let`, `var`
- Function binding
- `String`, `Number`, `Function`, `Array`, `Math`, `Date` objects
- `new`, `delete` operators
- `Object.keys`
- Exceptions
- `ArrayBuffer` and `AES` (Gen 3/4 devices, firmware 1.6.0+)

**NOT Supported:**
- Hoisting
- ES6 Classes (function prototypes are supported)
- Promises and async functions
- `\u` escape sequences (use `\xHH` for byte encoding)

**Important Specifics:**
- `arguments.length` returns number of arguments passed (if more than defined) or number defined (if fewer passed)
- `delete` operator works without brackets only
- Strings use byte arrays (not UTF-16), optimized for memory with UTF-8 encoding support

#### ES3 Compatibility Requirements

1. **No ES5+ Array Methods**
   - ❌ BROKEN: `Array.prototype.slice.call(arguments)`
   - ❌ BROKEN: `.map()`, `.filter()`, `.forEach()` on arguments object
   - ✅ WORKING: Use traditional `for` loops and manual string concatenation

   ```javascript
   // BROKEN - causes "Cannot read property 'call' of undefined"
   var args = Array.prototype.slice.call(arguments);
   print(args.map(function(a) { return String(a); }).join(" "));

   // WORKING - ES3 compatible
   var s = "";
   for (var i = 0; i < arguments.length; i++) {
     s += String(arguments[i]);
     if (i + 1 < arguments.length) s += " ";
   }
   print(s);
   ```

2. **Avoid Modern JavaScript Patterns**
   - Use `var` instead of `let`/`const`
   - Use `function` declarations instead of arrow functions
   - Use `"property" in object` instead of `!== undefined` checks (minifier-safe)

#### Minification Issues

**Issue**: The JavaScript minifier converts `catch (e)` to `catch {}` (ES2019 optional catch binding), which isn't supported by Shelly's limited JavaScript engine.

**Solution**: Always use `--no-minify` flag when uploading scripts to Shelly devices:

```bash
go run . ctl shelly script upload device-name script.js --no-minify
```

**Why**: Unminified code is more compatible and easier to debug on Shelly devices.

**Alternative**: If minification is needed, use the `in` operator instead of `!== undefined`:

```javascript
// BROKEN with minifier
var illumMin = value && value.illuminance_min !== undefined ? value.illuminance_min : null;

// WORKING with minifier
var illumMin = value && ("illuminance_min" in value) ? value.illuminance_min : null;
```

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

## Best Practices Summary

### Shelly Scripts

✅ **DO**:
- Use ES3-compatible JavaScript
- Keep callback nesting ≤ 3 levels
- Use named functions over anonymous callbacks
- Add startup/stop logging
- Use `--no-minify` for uploads
- Use `"property" in object` for property checks

❌ **DON'T**:
- Use ES5+ features (arrow functions, let/const, array methods)
- Nest callbacks more than 3 levels deep
- Rely on minification working correctly
- Use `!== undefined` (breaks with minifier)

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
