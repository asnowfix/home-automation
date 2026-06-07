// Shelly Script: BLE MQTT listener
// - Subscribes to MQTT topics under "shelly-blu/events/+"
// - Keeps only events whose MAC is followed via KVS keys: follow/shelly-blu/<MAC>
//   Value must be a JSON string: matching documentation below
// - On match: Switch.Set on the configured switch; if auto_off>0, turns it off after N seconds.
//
// Illuminance min/max are plain lux numbers. Percentage-based aggregation is
// handled by the daemon (see issue #249).

/**
 * The KVS value `follow/shelly-blu/<MAC>` must be a JSON string matching this type.
 * @typedef {Object} FollowConfig
 * @property {string} switch_id - The switch ID to be used for turning on the switch.
 * @property {number} auto_off - The number of seconds to wait before turning off the switch.
 * @property {number} illuminance_min - Minimum illuminance in lux (strict >).
 * @property {number} illuminance_max - Maximum illuminance in lux (strict <).
 * @property {string} next_switch - Optional next switch ID to turn on after auto-off.
 * @example
 * {"switch_id":"switch:0","auto_off":500,"illuminance_min":10,"illuminance_max":100}
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
  log: true
};

var STATE = {
  // switchIndex => timerId
  offTimers: {},

  // In-memory cache of follows loaded from KVS by loadFollowsFromKVS()
  // KVS keys are set externally via "myhome ctl follow blu" command
  // Each followed MAC has its own KVS key: follow/shelly-blu/<mac>
  follows: {}
};

// === TASK QUEUE (SINGLE TIMER FOR ALL SEQUENTIAL OPERATIONS) ===
// Prevents "Too many calls in progress" by dispatching Shelly.call invocations
// one at a time, 200 ms apart. Ported verbatim from pool-pump.js.
var TASK_QUEUE = [];
var TASK_INDEX = 0;
var TASK_TIMER = null;

function processTaskQueue() {
  if (TASK_INDEX >= TASK_QUEUE.length) {
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

function queueTask(task) {
  TASK_QUEUE.push(task);
  if (!TASK_TIMER) {
    TASK_TIMER = Timer.set(200, true, processTaskQueue);
  }
}

function getFollows() {
  return STATE.follows;
}

function setFollows(map) {
  STATE.follows = map || {};
}

/**
 * In-memory follows cache populated from KVS
 *
 * @typedef {Object.<string, FollowEntry>} FollowsMap
 * @typedef {Object} FollowEntry
 * @property {string} switchIdStr        // e.g. "switch:0"
 * @property {number} switchIndex        // numeric index parsed from switchIdStr
 * @property {number} autoOff            // seconds to auto-off; 0 to disable
 * @property {number|null} illuminanceMin // lux threshold (strict >); null = no bound
 * @property {number|null} illuminanceMax // lux threshold (strict <); null = no bound
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
  if (typeof switchIdStr !== "string") return null;
  var parts = switchIdStr.split(":");
  if (parts.length !== 2) return null;
  if (parts[0] !== "switch") return null;
  var n = Number(parts[1]);
  if (isNaN(n)) return null;
  return n;
}

function onProcessKvsKeyResponse(k, newMap, onComplete, gresp, gerr) {
  if (gerr) {
    log("KVS.Get error for", k, ":", gerr);
    onComplete();
    return;
  }
  if (!gresp || typeof gresp.value !== "string") {
    log("KVS.Get empty for", k);
    onComplete();
    return;
  }

  if (k.indexOf(CONFIG.kvsPrefix) !== 0) {
    log("Skipping non-follow key:", k);
    onComplete();
    return;
  }

  try {
    var value = JSON.parse(gresp.value);
    var switchIdStr = value && value.switch_id ? String(value.switch_id) : null;
    var autoOff = value && typeof value.auto_off === "number" ? value.auto_off : 0;
    var illumMin = value && typeof value.illuminance_min === "number" ? value.illuminance_min : null;
    var illumMax = value && typeof value.illuminance_max === "number" ? value.illuminance_max : null;
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
  Shelly.call("KVS.Get", { key: k }, onProcessKvsKeyResponse.bind(null, k, newMap, onComplete));
}

function onAllKeysProcessed(newMap, callback) {
  setFollows(newMap);
  log("Loaded follows:", newMap);
  if (callback) callback(true);
}

function processKeysSequentially(list, newMap, callback, index) {
  if (index >= list.length) {
    onAllKeysProcessed(newMap, callback);
    return;
  }
  processKvsKey(list[index], newMap, function() {
    queueTask(function() {
      processKeysSequentially(list, newMap, callback, index + 1);
    });
  });
}

function onKvsListResponse(callback, resp, err) {
  if (err) {
    log("KVS.List error:", err);
    if (callback) callback(false);
    return;
  }

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
    queueTask(function() {
      Shelly.call("Switch.Set", { id: follow.nextSwitchIndex, on: true }, onNextSwitchSetResponse.bind(null, follow));
    });
  }
}

function onAutoOffTimerFired(switchIndex, follow) {
  STATE.offTimers[switchIndex] = 0;
  queueTask(function() {
    Shelly.call("Switch.Set", { id: switchIndex, on: false }, onAutoOffSwitchSetResponse.bind(null, switchIndex, follow));
  });
}

function ensureAutoOffTimer(switchIndex, seconds, follow) {
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
  var data = null;
  try {
    data = JSON.parse(message);
  } catch (e) {
    log("Invalid JSON on", topic, "payload:", message, "err:", e);
    return;
  }
  var mac = normalizeMac(data && data.address);
  if (!mac) return;

  var follows = getFollows();
  var follow = follows[mac];
  if (!follow) return;

  // Only act on motion == 1 events
  var motion = data && data.motion;
  if (!(motion === 1 || motion === "1")) return;

  log("Motion detected for", mac, "illuminance", data.illuminance, "min", follow.illuminanceMin, "max", follow.illuminanceMax);

  // Enforce illuminance bounds (plain lux numbers; percentages are resolved by the daemon)
  var illum = (data && typeof data.illuminance === "number") ? data.illuminance : null;
  if (follow.illuminanceMin !== null || follow.illuminanceMax !== null) {
    if (illum === null) {
      log("Ignoring: no illuminance in event for bounds check", mac);
      return;
    }
    if (follow.illuminanceMin !== null && illum <= follow.illuminanceMin) {
      log("Illuminance", illum, "too low (<=", follow.illuminanceMin, ") for", mac);
      return;
    }
    if (follow.illuminanceMax !== null && illum >= follow.illuminanceMax) {
      log("Illuminance", illum, "too high (>=", follow.illuminanceMax, ") for", mac);
      return;
    }
  }
  log("Illuminance ok for", mac, "illuminance", illum);

  var idx = follow.switchIndex;
  queueTask(function() {
    Shelly.call("Switch.Set", { id: idx, on: true }, onSwitchSetOnResponse.bind(null, idx, follow, mac));
  });
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
          queueTask(function() { loadFollowsFromKVS(); });
        }
      } else if (eventData.info.event === "remote-input-event") {
        log("Remote input event detected (cancelAllTimers)");
        cancelAllTimers();
      } else if (eventData.info.component && eventData.info.component.indexOf("input:") === 0) {
        log("Local input event detected (cancelAllTimers)");
        cancelAllTimers();
      }
    }
  } catch (e) {
    log("Error handling event: ", e);
  }
}

function subscribeEvent() {
  Shelly.addEventHandler(onEventData);
}

function onLoadFollowsComplete(success) {
  if (success) {
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
