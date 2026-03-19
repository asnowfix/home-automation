# Bug Fix Plan - Code Review Findings (main vs v0.9.x)

> **Created**: 2026-03-18  
> **Review Scope**: 113 commits between main and v0.9.x branches  
> **Status**: Ready for implementation

## Overview

This document outlines bugs and code quality issues identified during a comprehensive code review of changes between the main and v0.9.x branches. Issues are prioritized by severity and potential impact.

---

## Critical Issues (High Priority)

### Issue #1: Race Condition in MQTT Client Subscriber Management

**Severity**: 🔴 CRITICAL  
**File**: `myhome/mqtt/client.go:639-696`  
**Impact**: Lost subscriptions under concurrent load, messages not delivered to some subscribers

#### Problem Description

The `subscribe()` function has a race condition when managing subscribers. The code loads a subscriber slice from `sync.Map`, modifies it, and stores it back without proper synchronization:

```go
value, loaded := c.subscribers.LoadOrStore(topic, make([]*subscriber, 0))
subscribers := value.([]*subscriber)
subscribers = append(subscribers, s)
c.subscribers.Store(topic, subscribers)
```

**Race Scenario**:
1. Goroutine A loads subscriber list for topic "foo" (contains [sub1])
2. Goroutine B loads subscriber list for topic "foo" (contains [sub1])
3. Goroutine A appends sub2, stores [sub1, sub2]
4. Goroutine B appends sub3, stores [sub1, sub3]
5. Result: sub2 is lost

#### Root Cause

`sync.Map` provides atomic operations for individual keys, but the slice stored as the value is not protected. Multiple goroutines can read the same slice, modify their copies, and overwrite each other's changes.

#### Proposed Fix

**Option 1: Use mutex-protected slice updates**
```go
type topicSubscribers struct {
    mu          sync.Mutex
    subscribers []*subscriber
}

// In subscribe():
value, _ := c.subscribers.LoadOrStore(topic, &topicSubscribers{
    subscribers: make([]*subscriber, 0),
})
ts := value.(*topicSubscribers)
ts.mu.Lock()
ts.subscribers = append(ts.subscribers, s)
ts.mu.Unlock()
```

**Option 2: Use atomic pointer swap**
```go
// Store atomic.Value containing []*subscriber
// Use CompareAndSwap loop to safely append
```

#### Testing Strategy

1. Create concurrent subscription test with 100 goroutines subscribing to same topic
2. Verify all subscribers receive messages
3. Run with `-race` flag to detect data races
4. Add stress test that subscribes/unsubscribes rapidly

#### Files to Modify

- `myhome/mqtt/client.go` - Fix subscriber management
- `myhome/mqtt/client_test.go` - Add concurrent subscription tests

---

### Issue #2: Task Queue Timing Window in Pool Script

**Severity**: 🔴 CRITICAL  
**File**: `internal/shelly/scripts/pool-pump.js:259-293`  
**Impact**: Lost tasks, incomplete initialization sequences

#### Problem Description

The task queue uses a single recurring timer but has a timing window where tasks can be lost:

```javascript
function queueTask(task) {
  TASK_QUEUE.push(task);
  
  if (!TASK_TIMER) {
    TASK_TIMER = Timer.set(200, true, processTaskQueue);
  }
}

function processTaskQueue() {
  if (TASK_INDEX >= TASK_QUEUE.length) {
    if (TASK_TIMER) {
      Timer.clear(TASK_TIMER);
      TASK_TIMER = null;  // Timer cleared
    }
    TASK_QUEUE = [];
    TASK_INDEX = 0;
    return;
  }
  // Process task...
}
```

**Race Scenario**:
1. Timer fires, processes last task
2. `processTaskQueue()` clears timer and sets `TASK_TIMER = null`
3. New task is queued via `queueTask()`
4. `queueTask()` sees `TASK_TIMER` is null and starts new timer
5. **BUT**: If step 3 happens between timer clear and null assignment, the new task won't trigger a new timer

#### Root Cause

Non-atomic check-and-set operation on `TASK_TIMER` combined with asynchronous timer callbacks.

#### Proposed Fix

**Option 1: Always restart timer if tasks remain**
```javascript
function processTaskQueue() {
  if (TASK_INDEX >= TASK_QUEUE.length) {
    // Check one more time before stopping
    if (TASK_QUEUE.length > TASK_INDEX) {
      // New tasks arrived, continue processing
      return;
    }
    if (TASK_TIMER) {
      Timer.clear(TASK_TIMER);
      TASK_TIMER = null;
    }
    TASK_QUEUE = [];
    TASK_INDEX = 0;
    return;
  }
  
  var task = TASK_QUEUE[TASK_INDEX];
  TASK_INDEX++;
  task();
}
```

**Option 2: Never stop timer, just idle when empty**
```javascript
function processTaskQueue() {
  if (TASK_INDEX >= TASK_QUEUE.length) {
    // Don't stop timer, just reset and idle
    TASK_QUEUE = [];
    TASK_INDEX = 0;
    return;
  }
  // Process task...
}
```

#### Testing Strategy

1. Add logging to track task queue state transitions
2. Test rapid task queueing during timer processing
3. Verify all initialization steps complete in bootstrap scenario
4. Monitor script logs for "initialization steps complete" message

#### Files to Modify

- `internal/shelly/scripts/pool-pump.js` - Fix task queue timing

---

### Issue #3: Missing Sensor Cache Updates in Gen1 Listener

**Severity**: 🔴 CRITICAL  
**File**: `internal/myhome/shelly/gen1/listener.go:94-99`  
**Impact**: Gen1 sensor data not displayed in UI, sensor values lost

#### Problem Description

The Gen1 MQTT listener receives sensor data but never updates the device cache:

```go
case 4:
    // Sensor value topic: shellies/<device-id>/sensor/<sensor-type>
    deviceId := parts[1]
    sensorType := parts[3]
    value := string(payload)
    log.Info("Received Gen1 sensor data", "device_id", deviceId, "sensor", sensorType, "value", value)
    // NO CACHE UPDATE - sensor value is lost!
```

The BLU listener correctly updates the cache (line 109-126), but Gen1 does not.

#### Root Cause

Code was refactored to move listeners from `pkg/shelly/` to `internal/myhome/shelly/` but the cache update logic was not added to Gen1 listener.

#### Proposed Fix

Add cache update call after logging:

```go
case 4:
    // Sensor value topic: shellies/<device-id>/sensor/<sensor-type>
    deviceId := parts[1]
    sensorType := parts[3]
    value := string(payload)
    log.Info("Received Gen1 sensor data", "device_id", deviceId, "sensor", sensorType, "value", value)
    
    // Update cache with sensor value
    if cache, ok := sc.(interface {
        UpdateSensorValue(context.Context, string, string, string) error
    }); ok {
        if err := cache.UpdateSensorValue(ctx, deviceId, sensorType, value); err != nil {
            log.V(1).Info("Failed to update sensor in cache", "error", err, "device_id", deviceId)
        } else {
            log.V(1).Info("Updated sensor value in cache", "device_id", deviceId, "sensor", sensorType, "value", value)
        }
    }
```

#### Testing Strategy

1. Set up Gen1 device (e.g., Shelly H&T) publishing sensor data
2. Subscribe to MQTT topic `shellies/+/sensor/#`
3. Verify sensor values appear in UI after page load
4. Check cache contains sensor values via debug endpoint
5. Add unit test for Gen1 sensor update flow

#### Files to Modify

- `internal/myhome/shelly/gen1/listener.go` - Add cache update
- `myhome/devices/cache.go` - Ensure UpdateSensorValue is exported
- `internal/myhome/shelly/gen1/listener_test.go` - Add test (create if needed)

---

## Medium Priority Issues

### Issue #4: Unsafe Device Access in Pool Service Error Handling

**Severity**: 🟡 MEDIUM  
**File**: `internal/myhome/shelly/script/pool.go:404-414`  
**Impact**: Potential nil pointer dereference, panic on device lookup failure

#### Problem Description

When `getDeviceStatus()` fails, the error handler tries to access a device that may not exist:

```go
controllerStatus, err := s.getDeviceStatus(ctx, resolvedControllerID, "controller")
if err != nil {
    controllerStatus = DeviceStatus{
        DeviceID:   resolvedControllerID,
        DeviceName: controllerDev.Name(),  // controllerDev may be invalid!
        Role:       "controller",
        Online:     false,
        Error:      err.Error(),
        Inputs:     make(map[string]bool),
    }
}
```

The `controllerDev` variable was retrieved earlier (line 383-387) but if that lookup failed, `controllerDev` could be nil or invalid.

#### Root Cause

Error handling assumes earlier device lookup succeeded, but doesn't verify.

#### Proposed Fix

Use device ID as fallback name:

```go
controllerStatus, err := s.getDeviceStatus(ctx, resolvedControllerID, "controller")
if err != nil {
    deviceName := resolvedControllerID
    if controllerDev != nil {
        deviceName = controllerDev.Name()
    }
    controllerStatus = DeviceStatus{
        DeviceID:   resolvedControllerID,
        DeviceName: deviceName,
        Role:       "controller",
        Online:     false,
        Error:      err.Error(),
        Inputs:     make(map[string]bool),
    }
}
```

Apply same fix to bootstrap device error handling (line 418-427).

#### Testing Strategy

1. Test pool status with non-existent controller device
2. Test pool status with non-existent bootstrap device
3. Verify error response contains device ID as name
4. Add unit test for error cases

#### Files to Modify

- `internal/myhome/shelly/script/pool.go` - Fix error handling

---

### Issue #5: Cache Pre-population Timing Race

**Severity**: 🟡 MEDIUM  
**File**: `myhome/devices/cache.go:50-74`, `myhome/daemon/daemon.go`  
**Impact**: Sensor updates fail with "device not found" during startup

#### Problem Description

The cache `Load()` method pre-populates devices from the database, but there's no guarantee it completes before MQTT listeners start receiving messages.

**Current startup sequence** (from `daemon.go`):
1. Start device manager (line 212)
2. Device manager starts MQTT listeners
3. Listeners immediately receive retained messages
4. Sensor updates fail if cache not yet populated

#### Root Cause

Asynchronous startup without explicit ordering guarantees.

#### Proposed Fix

**Option 1: Explicit cache pre-load in daemon startup**
```go
// In daemon.go after creating device manager:
d.dm = impl.NewDeviceManager(d.ctx, storage, resolver, mc, sseBroadcaster)

// Pre-load cache before starting listeners
log.Info("Pre-loading device cache from database")
if err := d.dm.Cache().Load(d.ctx); err != nil {
    log.Error(err, "Failed to pre-load cache")
    return err
}
log.Info("Device cache pre-loaded")

// Now start device manager (which starts listeners)
err = d.dm.Start(d.ctx)
```

**Option 2: Make cache Load() part of device manager Start()**
```go
// In impl/manager.go Start():
func (dm *DeviceManager) Start(ctx context.Context) error {
    // Pre-load cache first
    if err := dm.cache.Load(ctx); err != nil {
        return fmt.Errorf("failed to pre-load cache: %w", err)
    }
    
    // Then start listeners
    // ...
}
```

#### Testing Strategy

1. Clear database, restart daemon
2. Verify cache loads before first sensor message
3. Check logs for "device not found" errors during startup
4. Add integration test for startup sequence

#### Files to Modify

- `myhome/daemon/daemon.go` - Ensure cache loads first
- `myhome/devices/impl/manager.go` - Add cache pre-load to Start()

---

### Issue #6: Silent Error Masking in Pool Script Config Loading

**Severity**: 🟡 MEDIUM  
**File**: `internal/shelly/scripts/pool-pump.js:206-240`  
**Impact**: Configuration errors silently ignored, script runs with wrong config

#### Problem Description

The `loadConfig()` function ignores KVS.Get errors:

```javascript
Shelly.call("KVS.Get", {key: kvsKey}, function(result, err) {
    if (err && false) {}  // Error parameter kept for minifier, but error ignored
    
    if (result && ("value" in result) && result.value !== null && result.value !== "") {
        // Process value
    } else {
        // Use default - but was this a missing key or an error?
        CONFIG[key] = schema.default;
    }
});
```

**Problem**: Can't distinguish between:
- Key doesn't exist (use default - OK)
- KVS system error (should fail - NOT OK)

#### Root Cause

Error handling pattern that prevents minifier from removing error parameter, but doesn't actually check the error.

#### Proposed Fix

```javascript
Shelly.call("KVS.Get", {key: kvsKey}, function(result, err) {
    if (err) {
        log("ERROR: Failed to load config key", kvsKey, ":", err);
        // Still use default but log the error
        CONFIG[key] = schema.default;
        if (schema.required && CONFIG[key] === null) {
            missingRequired.push(key + " (" + kvsKey + ") - KVS error: " + err);
        }
        queueTask(loadNextKey);
        return;
    }
    
    if (result && ("value" in result) && result.value !== null && result.value !== "") {
        // Process value
    } else {
        // Key doesn't exist, use default
        CONFIG[key] = schema.default;
        if (schema.required && CONFIG[key] === null) {
            missingRequired.push(key + " (" + kvsKey + ")");
        }
    }
    queueTask(loadNextKey);
});
```

#### Testing Strategy

1. Test with corrupted KVS storage
2. Test with missing required keys
3. Verify error messages in logs
4. Test script startup with various config states

#### Files to Modify

- `internal/shelly/scripts/pool-pump.js` - Improve error handling

---

### Issue #7: SSE Slow Client Accumulation

**Severity**: 🟡 MEDIUM  
**File**: `internal/myhome/ui/sse.go:84-92`  
**Impact**: Slow clients remain connected indefinitely, consuming resources

#### Problem Description

When broadcasting to SSE clients, slow clients that can't keep up skip messages but remain connected:

```go
for ch := range b.clients {
    select {
    case ch <- msg:
        sentCount++
    default:
        skippedCount++
        b.log.Info("SSE client channel full, skipping message")
        // Client remains in map, will skip future messages too
    }
}
```

A client with a slow network connection will repeatedly skip messages but never get disconnected.

#### Root Cause

No mechanism to detect and remove consistently slow clients.

#### Proposed Fix

Track consecutive skips per client and disconnect after threshold:

```go
type sseClient struct {
    ch             chan string
    consecutiveSkips int
}

// In broadcast():
for client := range b.clients {
    select {
    case client.ch <- msg:
        client.consecutiveSkips = 0  // Reset on success
        sentCount++
    default:
        client.consecutiveSkips++
        skippedCount++
        
        if client.consecutiveSkips >= 10 {
            b.log.Info("Disconnecting slow SSE client", "consecutive_skips", client.consecutiveSkips)
            close(client.ch)
            delete(b.clients, client)
        } else {
            b.log.Info("SSE client channel full, skipping message", "consecutive_skips", client.consecutiveSkips)
        }
    }
}
```

#### Testing Strategy

1. Create slow SSE client test (small buffer, slow reader)
2. Verify client disconnects after 10 skips
3. Verify fast clients remain connected
4. Monitor SSE client count during load test

#### Files to Modify

- `internal/myhome/ui/sse.go` - Add slow client detection

---

### Issue #8: MQTT Reconnection Handler Loss

**Severity**: 🟡 MEDIUM  
**File**: `myhome/mqtt/client.go:375-426`  
**Impact**: Subscriptions permanently lost after failed re-registration

#### Problem Description

During periodic reconnection, if a subscription fails to re-register, it's logged but not retried:

```go
for _, sub := range subscriptions {
    token := c.mqtt.Subscribe(sub.topic, 1, sub.handler)
    for !token.WaitTimeout(c.timeout) {
        log.V(1).Info("Waiting for re-subscription", "topic", sub.topic)
    }
    if err := token.Error(); err != nil {
        log.Error(err, "Failed to re-subscribe after reconnection", "topic", sub.topic)
        // Handler is lost - not added back to pending or handlers map
    } else {
        log.Info("Re-subscribed successfully", "topic", sub.topic)
    }
}
```

#### Root Cause

No retry mechanism for failed re-subscriptions during periodic reconnection.

#### Proposed Fix

Add failed subscriptions back to pending list:

```go
failedSubscriptions := make([]subInfo, 0)

for _, sub := range subscriptions {
    token := c.mqtt.Subscribe(sub.topic, 1, sub.handler)
    for !token.WaitTimeout(c.timeout) {
        log.V(1).Info("Waiting for re-subscription", "topic", sub.topic)
    }
    if err := token.Error(); err != nil {
        log.Error(err, "Failed to re-subscribe after reconnection", "topic", sub.topic)
        failedSubscriptions = append(failedSubscriptions, sub)
    } else {
        log.Info("Re-subscribed successfully", "topic", sub.topic)
    }
}

// Add failed subscriptions to pending for next reconnection attempt
if len(failedSubscriptions) > 0 {
    c.pendingMutex.Lock()
    for _, sub := range failedSubscriptions {
        c.pendingSubscriptions[sub.topic] = sub.handler
    }
    c.pendingMutex.Unlock()
    log.Info("Added failed subscriptions to pending", "count", len(failedSubscriptions))
}
```

#### Testing Strategy

1. Simulate broker rejecting subscriptions
2. Verify subscriptions retry on next reconnection
3. Test with multiple failed subscriptions
4. Monitor pending subscriptions map

#### Files to Modify

- `myhome/mqtt/client.go` - Add retry for failed re-subscriptions

---

## Low Priority / Code Quality Issues

### Issue #9: Floating Point Precision in Pool Script Time Calculations

**Severity**: 🟢 LOW  
**File**: `internal/shelly/scripts/pool-pump.js:389-394`  
**Impact**: Minor precision errors in time-based bootstrap decisions

#### Problem Description

Time calculations use floating-point division:

```javascript
var now = Date.now() / 1000; // Current time in seconds
var hoursSinceLastRun = (now - STATE.lastRunTimestamp) / 3600;
```

JavaScript numbers are 64-bit floats, which can lose precision with large timestamps.

#### Proposed Fix

Use integer arithmetic:

```javascript
var now = Math.floor(Date.now() / 1000); // Integer seconds
var secondsSinceLastRun = now - STATE.lastRunTimestamp;
var hoursSinceLastRun = secondsSinceLastRun / 3600;
```

#### Files to Modify

- `internal/shelly/scripts/pool-pump.js` - Use integer timestamps

---

### Issue #10: Missing BLU Sensor Cache Updates

**Severity**: 🟢 LOW  
**File**: `internal/myhome/shelly/blu/listener.go:109-126`  
**Impact**: BLU sensor values not cached, lost on page reload

#### Problem Description

BLU listener broadcasts sensor updates via SSE but doesn't update device cache:

```go
if sseBroadcaster != nil && sensors != nil {
    for sensor, value := range *sensors {
        log.Info("Broadcasting BLU sensor update via SSE", ...)
        sseBroadcaster.BroadcastSensorUpdate(deviceID, sensor, value)
        // No cache update here
    }
}
```

#### Proposed Fix

Add cache update similar to Gen1 fix (Issue #3):

```go
if sensors != nil {
    for sensor, value := range *sensors {
        // Update cache
        if cache, ok := registry.(interface {
            UpdateSensorValue(context.Context, string, string, string) error
        }); ok {
            if err := cache.UpdateSensorValue(ctx, deviceID, sensor, value); err != nil {
                log.V(1).Info("Failed to update sensor in cache", "error", err)
            }
        }
        
        // Broadcast via SSE
        if sseBroadcaster != nil {
            sseBroadcaster.BroadcastSensorUpdate(deviceID, sensor, value)
        }
    }
}
```

#### Files to Modify

- `internal/myhome/shelly/blu/listener.go` - Add cache updates

---

## Implementation Priority

### Phase 1: Critical Fixes (Week 1)
1. ✅ Issue #1: MQTT subscriber race condition — DONE
2. ✅ Issue #2: Pool script task queue timing
3. ✅ Issue #3: Gen1 sensor cache updates — DONE

### Phase 2: Medium Priority (Week 2)
4. ✅ Issue #4: Pool service error handling
5. ✅ Issue #5: Cache pre-population timing
6. ✅ Issue #6: Pool script error masking
7. ✅ Issue #7: SSE slow client handling — DONE
8. ✅ Issue #8: MQTT reconnection handler loss — DONE

### Phase 3: Low Priority (Week 3)
9. ✅ Issue #9: Pool script time precision
10. ✅ Issue #10: BLU sensor cache updates

---

## Testing Requirements

### Unit Tests Required
- MQTT concurrent subscription test
- Pool script task queue test
- Gen1 sensor cache update test
- Pool service error handling test
- SSE slow client test

### Integration Tests Required
- Full daemon startup sequence test
- MQTT reconnection with subscriptions test
- Sensor data flow end-to-end test (Gen1, BLU, cache, SSE, UI)

### Manual Testing Checklist
- [ ] Gen1 device sensor data appears in UI
- [ ] BLU device sensor data appears in UI
- [ ] Pool pump initialization completes without errors
- [ ] MQTT subscriptions survive reconnection
- [ ] SSE clients receive all updates
- [ ] Slow SSE clients get disconnected
- [ ] Cache pre-loads before sensor messages arrive

---

## Notes for Implementation

### General Guidelines
1. **Run tests with race detector**: `go test -race ./...`
2. **Test on actual hardware**: Many issues only appear with real Shelly devices
3. **Monitor logs**: Look for "device not found", "failed to subscribe", etc.
4. **Check startup sequence**: Verify cache loads before listeners start
5. **Test concurrent operations**: Use stress tests with multiple goroutines

### Shelly Script Constraints
- Maximum 5 timers per script
- No hoisting - functions must be defined before use
- Minifier can break certain patterns (see AGENTS.md)
- Use `if (err && false) {}` pattern to prevent minifier from removing error parameters

### MQTT Client Considerations
- CleanSession=false for persistent sessions
- OrderMatters=false for wildcard routing
- Buffer sizes matter: 16 for sensors, 256 for $SYS topics
- Lazy-start for CLI, eager connection for daemon

---

## Success Criteria

### Critical Issues Fixed
- ✅ No race conditions detected with `-race` flag
- ✅ All MQTT subscriptions receive messages
- ✅ Gen1 sensor data appears in UI
- ✅ Pool script completes all initialization steps

### Medium Issues Fixed
- ✅ No panics on device lookup failures
- ✅ Cache loads before first sensor message
- ✅ Configuration errors logged properly
- ✅ Slow SSE clients disconnected after threshold
- ✅ Failed subscriptions retry on reconnection

### Low Priority Fixed
- ✅ Time calculations use integer arithmetic
- ✅ BLU sensor data cached properly

---

## References

- **AGENTS.md**: Shelly scripting guidelines and constraints
- **Code Review**: 113 commits between main and v0.9.x
- **Architecture**: Three-tier design (pkg/shelly, internal/myhome, myhome/ctl)
- **Testing**: See `docs/test-plan.md` for comprehensive test strategy
