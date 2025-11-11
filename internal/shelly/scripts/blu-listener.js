// Shelly Script: BLE MQTT listener
// - Subscribes to MQTT topics under "shelly-blu/events/#"
// - Keeps only events whose MAC is followed via KVS keys: follow/shelly-blu/<MAC>
//   Value must be a JSON string: matching documentation below
// - On match: Switch.Set on the configured switch; if auto_off>0, turns it off after N seconds.
//

/**
 * The KVS value `follow/shelly-blu/<MAC>` must be a JSON string matching this type.
 * @typedef {Object} FollowConfig
 * @property {string} switch_id - The switch ID to be used for turning on the switch.
 * @property {number} auto_off - The number of seconds to wait before turning off the switch.
 * @property {number|string} illuminance_min - The minimum illuminance value in lux, or percentage string (e.g., "20%").
 * @property {number|string} illuminance_max - The maximum illuminance value in lux, or percentage string (e.g., "80%").
 * @property {string} next_switch - The next switch ID to be used for turning on the switch.
 * @example
 * {"switch_id":"switch:0","auto_off":500,"illuminance_min":"20%","illuminance_max":"80%"}
 * {"switch_id":"switch:0","auto_off":500,"illuminance_min":10,"illuminance_max":100}
 * 
 * Percentage values (0%-100%) are calculated from the 7-day min/max history:
 * - "0%" = minimum illuminance observed in past 7 days
 * - "100%" = maximum illuminance observed in past 7 days
 * - "20%" = 20% between min and max (min + 0.2 * (max - min))
 * 
 * topic: shelly-blu/events/e8:e0:7e:d0:f9:89
 * message: {
 *     "encryption":false,
 *     "BTHome_version":2,
 *     "pid":248,
 *     "battery":98,
 *     "illuminance":57,
 *     "motion":0,
 *     "rssi":-82,
 *     "address":"e8:e0:7e:d0:f9:89"
 * }
 */

var CONFIG = {
  script: "[blu-listener] ",
  eventName: "shelly-blu",
  topicPrefix: "shelly-blu/events",
  kvsPrefix: "follow/shelly-blu/",
  statePrefix: "state/shelly-blu/",
  log: true
};

var STATE = {
  // switchIndex => timerId
  offTimers: {},
  
  // mac (lowercase) => { dailyData: [{ date: "YYYY-MM-DD", min: number, max: number }], currentMin: number, currentMax: number, lastSaveDate: "YYYY-MM-DD" }
  illuminanceTracking: {},

  // Timer ID for daily save
  dailySaveTimer: null,

  // In-memory cache of follows loaded from KVS by loadFollowsFromKVS()
  // KVS keys are set externally via "myhome ctl follow blu" command
  // Each followed MAC has its own KVS key: follow/shelly-blu/<mac>
  follows: {}
};

function getFollows() {
  return STATE.follows;
}

function setFollows(map) {
  STATE.follows = map || {};
}

/**
 * In-memory follows cache populated from KVS
 * Stores a map of followed BLE MACs to local action and bounds info.
 *
 * @typedef {Object.<string, FollowEntry>} FollowsMap
 * @typedef {Object} FollowEntry
 * @property {string} switchIdStr        // e.g. "switch:0"
 * @property {number} switchIndex        // numeric index parsed from switchIdStr
 * @property {number} autoOff            // seconds to auto-off; 0 to disable
 * @property {number|string|null} illuminanceMin // number in lux or percentage string (e.g., "20%")
 * @property {number|string|null} illuminanceMax // number in lux or percentage string (e.g., "80%")
 * @property {string|null} nextSwitchIdStr      // e.g. "switch:1" for optional chaining
 * @property {number|null} nextSwitchIndex      // numeric index parsed from nextSwitchIdStr
 */

function log() {
  if (!CONFIG.log) return;
  var s = "";
  for (var i = 0; i < arguments.length; i++) {
    try {
      var a = arguments[i];
      if (typeof a === "object") {
        s += JSON.stringify(a);
      } else {
        s += String(a);
      }
    } catch (e) {
      s += String(arguments[i]);
      // Ensure 'e' is referenced so the minifier doesn't drop it and produce `catch {}`
      if (e && false) {}
    }
    if (i + 1 < arguments.length) s += " ";
  }
  print(CONFIG.script, s);
}

function normalizeMac(mac) {
  if (!mac) return "";
  return String(mac).toLowerCase();
}

function parseSwitchIndex(switchIdStr) {
  // Expecting format "switch:<number>"
  if (typeof switchIdStr !== "string") return null;
  var parts = switchIdStr.split(":");
  if (parts.length !== 2) return null;
  if (parts[0] !== "switch") return null;
  var n = Number(parts[1]);
  if (isNaN(n)) return null;
  return n;
}

function getCurrentDate() {
  var now = new Date();
  var year = now.getFullYear();
  var month = now.getMonth() + 1;
  var day = now.getDate();
  return year + "-" + (month < 10 ? "0" : "") + month + "-" + (day < 10 ? "0" : "") + day;
}

function onLoadIlluminanceStateResponse(mac, callback, resp, err) {
  if (err || !resp || !resp.value) {
    // Initialize new tracking state
    STATE.illuminanceTracking[mac] = {
      dailyData: [],
      currentMin: null,
      currentMax: null,
      lastSaveDate: getCurrentDate()
    };
    if (callback) callback();
    return;
  }
  
  try {
    var data = JSON.parse(resp.value);
    STATE.illuminanceTracking[mac] = {
      dailyData: data.dailyData || [],
      currentMin: data.currentMin || null,
      currentMax: data.currentMax || null,
      lastSaveDate: data.lastSaveDate || getCurrentDate()
    };
    log("Loaded illuminance state for", mac, ":", STATE.illuminanceTracking[mac]);
  } catch (e) {
    log("Error parsing illuminance state for", mac, ":", e);
    STATE.illuminanceTracking[mac] = {
      dailyData: [],
      currentMin: null,
      currentMax: null,
      lastSaveDate: getCurrentDate()
    };
  }
  if (callback) callback();
}

function loadIlluminanceState(mac, callback) {
  var key = CONFIG.statePrefix + mac;
  Shelly.call("KVS.Get", { key: key }, onLoadIlluminanceStateResponse.bind(null, mac, callback));
}

function onSaveIlluminanceStateResponse(mac, callback, resp, err) {
  if (err) {
    log("Error saving illuminance state for", mac, ":", err);
  } else {
    log("Saved illuminance state for", mac);
  }
  if (callback) callback();
}

function saveIlluminanceState(mac, callback) {
  var tracking = STATE.illuminanceTracking[mac];
  if (!tracking) {
    if (callback) callback();
    return;
  }
  
  var key = CONFIG.statePrefix + mac;
  var value = JSON.stringify({
    dailyData: tracking.dailyData,
    currentMin: tracking.currentMin,
    currentMax: tracking.currentMax,
    lastSaveDate: tracking.lastSaveDate
  });
  
  Shelly.call("KVS.Set", { key: key, value: value }, onSaveIlluminanceStateResponse.bind(null, mac, callback));
}

function updateIlluminanceTracking(mac, illuminance) {
  if (typeof illuminance !== "number") return;
  
  var tracking = STATE.illuminanceTracking[mac];
  if (!tracking) {
    tracking = STATE.illuminanceTracking[mac] = {
      dailyData: [],
      currentMin: null,
      currentMax: null,
      lastSaveDate: getCurrentDate()
    };
  }
  
  var currentDate = getCurrentDate();
  
  // Check if we need to save yesterday's data and start a new day
  if (tracking.lastSaveDate !== currentDate) {
    // Save previous day's min/max if we have data
    if (tracking.currentMin !== null && tracking.currentMax !== null) {
      tracking.dailyData.push({
        date: tracking.lastSaveDate,
        min: tracking.currentMin,
        max: tracking.currentMax
      });
      
      // Keep only last 7 days (remove oldest entries from beginning)
      while (tracking.dailyData.length > 7) {
        // Manual shift: remove first element (shift() not supported, pop() removes from end)
        var newArray = [];
        for (var i = 1; i < tracking.dailyData.length; i++) {
          newArray.push(tracking.dailyData[i]);
        }
        tracking.dailyData = newArray;
      }
      
      log("Saved daily illuminance for", mac, "date:", tracking.lastSaveDate, "min:", tracking.currentMin, "max:", tracking.currentMax);
    }
    
    // Reset for new day - initialize to null so first value sets both min and max
    tracking.currentMin = null;
    tracking.currentMax = null;
    tracking.lastSaveDate = currentDate;
  }
  
  // Update current day's min/max (handles both new day and same day updates)
  if (tracking.currentMin === null || illuminance < tracking.currentMin) {
    tracking.currentMin = illuminance;
  }
  if (tracking.currentMax === null || illuminance > tracking.currentMax) {
    tracking.currentMax = illuminance;
  }
}

function getSevenDayMinMax(mac) {
  var tracking = STATE.illuminanceTracking[mac];
  if (!tracking || !tracking.dailyData || tracking.dailyData.length === 0) {
    return { min: null, max: null };
  }
  
  var overallMin = null;
  var overallMax = null;
  
  // Check historical data
  for (var i = 0; i < tracking.dailyData.length; i++) {
    var day = tracking.dailyData[i];
    if (typeof day.min === "number") {
      if (overallMin === null || day.min < overallMin) {
        overallMin = day.min;
      }
    }
    if (typeof day.max === "number") {
      if (overallMax === null || day.max > overallMax) {
        overallMax = day.max;
      }
    }
  }
  
  // Include current day's data
  if (tracking.currentMin !== null) {
    if (overallMin === null || tracking.currentMin < overallMin) {
      overallMin = tracking.currentMin;
    }
  }
  if (tracking.currentMax !== null) {
    if (overallMax === null || tracking.currentMax > overallMax) {
      overallMax = tracking.currentMax;
    }
  }
  
  return { min: overallMin, max: overallMax };
}

function parseIlluminanceValue(value, mac) {
  if (typeof value === "number") {
    return value;
  }
  
  if (typeof value === "string" && value.length > 1 && value.charAt(value.length - 1) === "%") {
    var percentStr = value.substring(0, value.length - 1);
    var percent = Number(percentStr);
    
    if (isNaN(percent) || percent < 0 || percent > 100) {
      log("Invalid percentage value:", value, "for", mac);
      return null;
    }
    
    var sevenDayRange = getSevenDayMinMax(mac);
    if (sevenDayRange.min === null || sevenDayRange.max === null) {
      log("No historical data available for percentage calculation:", mac);
      return null;
    }
    
    // Calculate the actual value based on percentage
    var range = sevenDayRange.max - sevenDayRange.min;
    var actualValue = sevenDayRange.min + (range * percent / 100);
    
    log("Converted", value, "to", actualValue, "for", mac, "(range:", sevenDayRange.min, "-", sevenDayRange.max, ")");
    return actualValue;
  }
  
  return null;
}

function saveAllIlluminanceStates(callback) {
  var macs = Object.keys(STATE.illuminanceTracking);
  if (macs.length === 0) {
    if (callback) callback();
    return;
  }
  
  var pending = macs.length;
  function onStateSaved() {
    pending--;
    if (pending === 0 && callback) callback();
  }
  
  for (var i = 0; i < macs.length; i++) {
    saveIlluminanceState(macs[i], onStateSaved);
  }
}

function onDailySaveRecurring() {
  log("Daily save timer triggered (recurring)");
  saveAllIlluminanceStates();
}

function onDailySaveFirstTime() {
  log("Daily save timer triggered");
  saveAllIlluminanceStates();
  // Set up next day's timer (24 hours)
  STATE.dailySaveTimer = Timer.set(24 * 60 * 60 * 1000, true, onDailySaveRecurring);
}

function setupDailySaveTimer() {
  // Clear existing timer
  if (STATE.dailySaveTimer) {
    Timer.clear(STATE.dailySaveTimer);
  }
  
  // Calculate milliseconds until next midnight using basic Date methods
  var now = new Date();
  var currentTime = now.getTime();
  var currentHour = now.getHours();
  var currentMinute = now.getMinutes();
  var currentSecond = now.getSeconds();
  var currentMs = now.getMilliseconds();
  
  // Calculate milliseconds from now until midnight
  var msUntilMidnight = (23 - currentHour) * 60 * 60 * 1000 + // remaining hours
                        (59 - currentMinute) * 60 * 1000 +     // remaining minutes  
                        (59 - currentSecond) * 1000 +          // remaining seconds
                        (1000 - currentMs);                    // remaining milliseconds
  
  // Add 1 second to ensure we're past midnight
  msUntilMidnight += 1000;
  
  // Set timer for midnight, then repeat every 24 hours
  STATE.dailySaveTimer = Timer.set(msUntilMidnight, false, onDailySaveFirstTime);
  
  log("Set up daily save timer, next save in", Math.round(msUntilMidnight / 1000), "seconds");
}

function onProcessKvsKeyResponse(k, newMap, onComplete, gresp, gerr) {
  if (gerr) {
    log("KVS.Get error for", k, ":", gerr);
    onComplete();
    return;
  }
  if (!gresp || typeof gresp.value !== "string") {
    log("KVS.Get error for", k, gerr);
    onComplete();
    return;
  }
  
  // Skip keys that don't start with our prefix
  if (k.indexOf(CONFIG.kvsPrefix) !== 0) {
    log("Skipping non-follow key:", k);
    onComplete();
    return;
  }
  
  try {
    var value = JSON.parse(gresp.value);
    var switchIdStr = value && value.switch_id ? String(value.switch_id) : null;
    var autoOff = value && typeof value.auto_off === "number" ? value.auto_off : 0;
    var illumMin = value && ("illuminance_min" in value) ? value.illuminance_min : null;
    var illumMax = value && ("illuminance_max" in value) ? value.illuminance_max : null;
    var nextSwitchStr = value && value.next_switch ? String(value.next_switch) : null;
    var nextIdx = parseSwitchIndex(nextSwitchStr);
    var mac = k.substr(CONFIG.kvsPrefix.length);
    mac = normalizeMac(mac);
    var idx = parseSwitchIndex(switchIdStr);
    
    if (mac && idx !== null) {
      newMap[mac] = {
        switchIdStr: switchIdStr,
        switchIndex: idx,
        autoOff: autoOff,
        illuminanceMin: illumMin,
        illuminanceMax: illumMax,
        nextSwitchIdStr: nextSwitchStr,
        nextSwitchIndex: (typeof nextIdx === "number" ? nextIdx : null)
      };
    } else {
      log("Ignoring invalid follow entry:", k, gresp.value);
    }
  } catch (e) {
    log("JSON parse error for", k, e);
  }
  onComplete();
}

function processKvsKey(k, newMap, onComplete) {
  Shelly.call("KVS.Get", { key: k }, onProcessKvsKeyResponse.bind(null, k, newMap, onComplete))
}

function loadIlluminanceStatesSequentially(macs, callback, index) {
  if (index >= macs.length) {
    // All states loaded
    if (callback) callback();
    return;
  }
  
  // Load one state at a time
  loadIlluminanceState(macs[index], loadIlluminanceStatesSequentially.bind(null, macs, callback, index + 1));
}

function loadAllIlluminanceStates(macs, callback) {
  if (macs.length === 0) {
    if (callback) callback();
    return;
  }
  
  // Process sequentially to avoid "too many calls in progress"
  loadIlluminanceStatesSequentially(macs, callback, 0);
}

function onAllIlluminanceStatesLoaded(callback) {
  if (callback) callback(true);
}

function onAllKeysProcessed(newMap, callback) {
  setFollows(newMap);
  log("Loaded follows:", newMap);
  loadAllIlluminanceStates(Object.keys(newMap), onAllIlluminanceStatesLoaded.bind(null, callback));
}

function processKeysSequentially(list, newMap, callback, index) {
  if (index >= list.length) {
    // All keys processed
    onAllKeysProcessed(newMap, callback);
    return;
  }
  
  // Process one key at a time
  processKvsKey(list[index], newMap, processKeysSequentially.bind(null, list, newMap, callback, index + 1));
}

function onKvsListResponse(callback, resp, err) {
  if (err) {
    log("KVS.List error:", err);
    if (callback) callback(false);
    return;
  }
  
  // Normalize possible response shapes
  var list = [];
  if (resp) {
    if (resp.keys) {
      if (Array.isArray(resp.keys)) {
        list = resp.keys;
      } else if (typeof resp.keys === "object") {
        list = Object.keys(resp.keys);
      }
    } else if (Array.isArray(resp.items)) {
      for (var li = 0; li < resp.items.length; li++) {
        var it = resp.items[li];
        if (it && it.key) list.push(it.key);
      }
    }
  }
  
  log("KVS.List keys:", list.length);
  var newMap = {};
  
  if (!list || !list.length) {
    setFollows(newMap);
    log("No followed MACs.");
    if (callback) callback(true);
    return;
  }

  // Process keys sequentially to avoid "too many calls in progress"
  processKeysSequentially(list, newMap, callback, 0);
}

function loadFollowsFromKVS(callback) {
  Shelly.call("KVS.List", { prefix: CONFIG.kvsPrefix }, onKvsListResponse.bind(null, callback));
}

function onNextSwitchSetResponse(follow, r2, e2) {
  if (e2) log("Next Switch.Set on error", follow.nextSwitchIndex, e2);
  else log("Auto-next: turned on", follow.nextSwitchIdStr, "from", follow.switchIdStr);
}

function onAutoOffSwitchSetResponse(switchIndex, follow, r, e) {
  if (e) log("Switch.Set off error", switchIndex, e);
  else log("Auto-off switch", switchIndex);
  var hasNext = follow && typeof follow.nextSwitchIndex === "number";
  if (hasNext) {
    Shelly.call("Switch.Set", { id: follow.nextSwitchIndex, on: true }, onNextSwitchSetResponse.bind(null, follow));
  }
}

function onAutoOffTimerFired(switchIndex, follow) {
  // Always switch OFF current first
  Shelly.call("Switch.Set", { id: switchIndex, on: false }, onAutoOffSwitchSetResponse.bind(null, switchIndex, follow));
  STATE.offTimers[switchIndex] = 0;
}

function ensureAutoOffTimer(switchIndex, seconds, follow) {
  // Cancel previous timer, set new one if seconds>0
  var prev = STATE.offTimers[switchIndex];
  if (prev) {
    Timer.clear(prev);
    STATE.offTimers[switchIndex] = 0;
  }
  if (!seconds || seconds <= 0) return;
  var ms = Math.floor(seconds * 1000);
  var tid = Timer.set(ms, false, onAutoOffTimerFired.bind(null, switchIndex, follow));
  STATE.offTimers[switchIndex] = tid;
}

function handleBluEvent(topic, message) {
  // message is expected to be JSON with at least { address: ".." }
  var data = null;
  try {
    data = JSON.parse(message);
  } catch (e) {
    // Reference 'e' so minifier keeps the parameter (prevents `catch {}`)
    log("Invalid JSON on", topic, "payload:", message, "err:", e);
    return;
  }
  var mac = normalizeMac(data && data.address);
  if (!mac) return; // not a BLU payload we care about

  var follows = getFollows();
  var follow = follows[mac];
  if (!follow) return; // not followed

  // Track illuminance data for all followed devices (regardless of motion)
  var illuminance = (data && typeof data.illuminance === "number") ? data.illuminance : null;
  if (illuminance !== null) {
    updateIlluminanceTracking(mac, illuminance);
  }

  // Only act on motion == 1 events
  var motion = data && data.motion;
  if (!(motion === 1 || motion === "1")) {
    // Ignore events without motion or with motion 0
    return;
  }

  log("Motion detected for", mac, "illuminance", data.illuminance, "min", follow.illuminanceMin, "max", follow.illuminanceMax);

  // If illuminance bounds are configured, enforce them
  var parsedMin = follow.illuminanceMin !== null ? parseIlluminanceValue(follow.illuminanceMin, mac) : null;
  var parsedMax = follow.illuminanceMax !== null ? parseIlluminanceValue(follow.illuminanceMax, mac) : null;
  var hasMin = parsedMin !== null;
  var hasMax = parsedMax !== null;
  
  if (hasMin || hasMax) {
    var illum = (data && typeof data.illuminance === "number") ? data.illuminance : null;
    if (illum === null) {
      // No illuminance provided in event; cannot evaluate bounds -> ignore
      log("Ignoring due to missing illuminance for bounds", mac, { min: follow.illuminanceMin, max: follow.illuminanceMax });
      return;
    }
    // Strictly greater than illuminance_min
    if (hasMin && illum <= parsedMin) {
      log("Illuminance", illum, "too low (<=", parsedMin, "from", follow.illuminanceMin, ") for", mac);
      return;
    }
    // Strictly less than illuminance_max
    if (hasMax && illum >= parsedMax) {
      log("Illuminance", illum, "too high (>=", parsedMax, "from", follow.illuminanceMax, ") for", mac);
      return;
    }
  }
  log("Illuminance bounds ok for", mac, "illuminance", data.illuminance, "parsed min:", parsedMin, "parsed max:", parsedMax);

  // Act: turn on configured switch, then setup auto-off
  var idx = follow.switchIndex;
  Shelly.call("Switch.Set", { id: idx, on: true }, onSwitchSetOnResponse.bind(null, idx, follow, mac));
  ensureAutoOffTimer(idx, follow.autoOff, follow);
}

function onSwitchSetOnResponse(idx, follow, mac, resp, err) {
  if (err) log("Switch.Set on error", idx, err);
  else log("Turned on", follow.switchIdStr, "for", mac, "auto_off=", follow.autoOff, "s");
}

function onMqttMessage(t, m, r) {
  handleBluEvent(t, m);
}

function subscribeMqtt() {
  var topic = CONFIG.topicPrefix + "/#";
  MQTT.subscribe(topic, onMqttMessage);
  log("Subscribed to", topic);
}

function cancelAllTimers() {
  // Cancel all ongoing auto-off timers when manual operation is detected
  for (var switchIndex in STATE.offTimers) {
    var timerId = STATE.offTimers[switchIndex];
    if (timerId) {
      Timer.clear(timerId);
      STATE.offTimers[switchIndex] = 0;
      log("Cancelled auto-off timer for switch", switchIndex, "due to manual operation");
    }
  }
}

function onEventData(eventData) {
  log("Handling event: ", eventData);
  try {
    if (eventData && eventData.info) {
      if (eventData.info.event === CONFIG.eventName) {
        handleBluEvent(eventData.info.address, eventData.info.data);
      } else if (eventData.info.event === "kvs") {
        var kvsEvent = eventData.info;
        if (kvsEvent.key && kvsEvent.key.indexOf(CONFIG.kvsPrefix) === 0) {
          log("KVS change detected for key:", kvsEvent.key, "action:", kvsEvent.action);
          loadFollowsFromKVS();
        }
      } else if (eventData.info.event === "remote-input-event") {
        log("Remote input event detected (cancelAllTimers)");
        cancelAllTimers();
      } else if (eventData.info.component && eventData.info.component.indexOf("input:") === 0) {
        log("Local input event detected (cancelAllTimers)");
        cancelAllTimers();
      } else if (eventData.info.event === "reboot") {
        log("Device reboot detected, saving illuminance data");
        saveAllIlluminanceStates();
      } else if (eventData.info.event === "script_stop") {
        log("Script stopping, saving illuminance data");
        saveAllIlluminanceStates();
      }
    }
  } catch (e) {
    log("Error handling event: ", e);
  }
}

function subscribeEvent() {
  Shelly.addEventHandler(onEventData);
}

function onHourlyBackupTimer() {
  log("Hourly backup save triggered");
  saveAllIlluminanceStates();
}

function onLoadFollowsComplete(success) {
  if (success) {
    setupDailySaveTimer();
    
    // Set up periodic save every hour as backup
    Timer.set(60 * 60 * 1000, true, onHourlyBackupTimer);
    
    log("Script initialization complete");
  } else {
    log("Script initialization failed");
  }
}

// Init
log("Script starting...");
loadFollowsFromKVS(onLoadFollowsComplete);
subscribeMqtt();
subscribeEvent();
