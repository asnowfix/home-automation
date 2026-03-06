# Debugging Sensor Cache on Page Load

This guide helps debug why sensor values don't appear on page load after server restart.

## Architecture Flow

1. **Server starts** → MQTT listeners subscribe to topics
2. **Retained messages arrive** → Sensor data from last publish
3. **Cache updated** → `UpdateSensorValue()` stores in memory
4. **Page loads** → `DeviceToView()` reads from cache
5. **HTMX renders** → Templates display sensor values

## Expected Behavior

When the server restarts and a browser loads the page:
- Devices with sensors should show their last known values (from retained MQTT messages)
- Temperature, humidity, battery, door/window states should be visible immediately

## Debug Checklist

### Step 1: Verify Retained Messages Are Received

**What to check:** MQTT broker is delivering retained messages when listeners subscribe.

**How to verify:**
```bash
# Start server with verbose logging
./myhome daemon -v

# Look for these logs at startup:
```

**Gen1 devices:**
```
sensor update source=Gen1 device_id=shellyht-xxx sensor=temperature value=22.5
sensor update source=Gen1 device_id=shellyht-xxx sensor=humidity value=65.0
```

**BLU devices:**
```
sensor update source=BLU device_id=shellyblu-xxx mac=e8:e0:7e:xx:xx:xx sensor=temperature value=22.5
sensor update source=BLU device_id=shellyblu-xxx mac=e8:e0:7e:xx:xx:xx sensor=battery value=100
```

**Location in code:**
- Gen1: `pkg/shelly/gen1/listener.go:65`
- BLU: `pkg/shelly/blu/listener.go:115`

**If NOT seeing these logs:**
- ❌ MQTT broker may not be retaining messages
- ❌ Listeners may not be subscribing correctly
- ❌ Check MQTT broker with: `mqtt sub -t 'shellies/#' -h localhost` or `mqtt sub -t 'shelly/events' -h localhost`

### Step 2: Verify Cache Updates

**What to check:** `UpdateSensorValue()` is being called and succeeding.

**How to verify:**
```bash
# Look for these logs (with -v flag):
Updated sensor value in cache device_id=xxx sensor=temperature value=22.5
```

**Location in code:**
- `myhome/devices/cache.go:359`

**If NOT seeing these logs:**
- ❌ Sensor parsing may be failing
- ❌ Device may not exist in cache yet (check device registration order)
- ❌ Check for errors: `Failed to update sensor in cache`

### Step 3: Verify Sensor Values in Cache

**What to check:** Cached device objects contain sensor data.

**How to verify:**
```bash
# Look for these logs when page loads (with -v flag):
DeviceToView sensor values device_id=xxx temperature=22.5 humidity=65.0
```

**Location in code:**
- `internal/myhome/ui/template.go:252`

**If seeing "DeviceToView no sensor data":**
- ❌ Cache was updated but device object doesn't have Status/Sensors
- ❌ Timing issue: page loaded before retained messages arrived
- ❌ Device cache may have been flushed

### Step 4: Check Device Registration Order

**Critical timing issue:** Devices must exist in cache BEFORE sensor updates arrive.

**Expected order:**
1. Device registered (via `/info` topic or BLU discovery)
2. Sensor update arrives (retained message)
3. Cache updated with sensor value

**How to verify:**
```bash
# Look for device registration BEFORE sensor updates:
inserted/updated device id=shellyht-xxx name=xxx
Gen1 sensor update device_id=shellyht-xxx sensor=temperature value=22.5
```

**If sensor updates arrive BEFORE device registration:**
- ❌ You'll see: `device not found in cache: xxx`
- ❌ Solution: Ensure devices are registered before MQTT listeners start
- ❌ Check startup order in `myhome/devices/impl/manager.go`

### Step 5: Verify HTMX Template Rendering

**What to check:** Templates correctly render sensor values from DeviceView.

**How to verify:**
```bash
# Check browser DevTools → Network → Response for /htmx/devices
# Look for sensor values in HTML:
<span class="tag is-info ml-2" id="sensor-xxx-temperature">22.5°C</span>
```

**If seeing placeholder values:**
```html
<span class="tag is-light ml-2" id="sensor-xxx-temperature">--°C</span>
```
- ❌ DeviceView.Temperature is nil
- ❌ Go back to Step 3

## Common Issues

### Issue 1: Timing Race Condition

**Symptom:** Sensor values appear after a few seconds but not on initial page load.

**Cause:** MQTT listeners subscribe AFTER page is rendered.

**Solution:** Check startup order in `myhome/devices/impl/manager.go`:
```go
// Gen1/BLU listeners should start BEFORE HTTP server
gen1.StartMqttListener(...)  // ← Should be early
blu.StartBLUListener(...)     // ← Should be early
// ... wait for retained messages to arrive ...
ui.Start(...)                 // ← Should be later
```

### Issue 2: MQTT Broker Not Retaining Messages

**Symptom:** No sensor updates at all on startup.

**Cause:** MQTT broker not configured to retain messages.

**Verify:**
```bash
# Check if messages are retained
mqtt sub -t 'shellies/+/sensor/#' -h localhost

# Should see messages immediately upon subscription
# If not, check broker configuration
```

**Gen1 Proxy publishes with retain=true:**
- `pkg/shelly/gen1/proxy.go:113`

**BLU messages are NOT retained** (they're events, not state):
- BLU devices need to publish again after server restart

### Issue 3: Device Not in Cache

**Symptom:** `device not found in cache: xxx`

**Cause:** Sensor update arrives before device is registered.

**Solution:** Ensure device discovery/registration happens first:
1. Gen1: `/info` topic creates device
2. BLU: First event creates device
3. Then sensor updates can be cached

### Issue 4: Cache Flushed

**Symptom:** Sensors work, then disappear.

**Cause:** Cache.Flush() called somewhere.

**Check:** Search for `Flush()` calls in codebase.

## Testing Commands

```bash
# 1. Check MQTT retained messages
mqtt sub -t 'shellies/#' -h localhost

# 2. Publish test sensor value
mqtt pub -t 'shellies/shellyht-test/sensor/temperature' -m '22.5' -h localhost --retain

# 3. Run unit tests
go test -v -run TestCache_UpdateSensorValue ./myhome/devices

# 4. Start server with verbose logging
./myhome daemon -v

# 5. Check race conditions
go test -race ./...
```

## Quick Diagnosis

Run server with `-v` flag and check logs in this order:

1. ✅ `Gen1 MQTT listener started` or `BLU listener started`
2. ✅ `Gen1 sensor update` or `event emitter` (retained messages)
3. ✅ `Updated sensor value in cache`
4. ✅ `DeviceToView sensor values` (when page loads)
5. ✅ Browser shows sensor values

If any step fails, that's where the issue is.

## Expected Log Sequence

```
[startup]
Starting device manager
Gen1 MQTT listener started topic=shellies/#
BLU listener started topic=shelly/events

[retained messages arrive]
Gen1 sensor update device_id=shellyht-001 sensor=temperature value=22.5
Updated sensor value in cache device_id=shellyht-001 sensor=temperature value=22.5
Gen1 sensor update device_id=shellyht-001 sensor=humidity value=65.0
Updated sensor value in cache device_id=shellyht-001 sensor=humidity value=65.0

[page load]
DeviceToView sensor values device_id=shellyht-001 temperature=22.5 humidity=65.0

[browser receives HTML with sensor values]
```

## Next Steps

If sensor values still don't appear:

1. Share the full startup logs (with `-v` flag)
2. Check MQTT broker logs
3. Verify retained messages exist: `mqtt sub -t 'shellies/#' -h localhost`
4. Check browser DevTools → Network → Response for `/htmx/devices`
