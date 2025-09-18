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
 * @property {number} illuminance_min - The minimum illuminance value in lux.
 * @property {number} illuminance_max - The maximum illuminance value in lux.
 * @property {string} next_switch - The next switch ID to be used for turning on the switch.
 * @example
 * {"switch_id":"switch:0","auto_off":500,"illuminance_min":10}
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
  // mac (lowercase) => { switchIdStr: string, switchIndex: number, autoOff: number, illuminanceMin?: number, illuminanceMax?: number, nextSwitchIdStr?: string, nextSwitchIndex?: number }
  follows: {},
  // switchIndex => timerId
  offTimers: {}
};

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

function loadFollowsFromKVS(callback) {
  // Refresh STATE.follows from KVS
  Shelly.call("KVS.List", { prefix: CONFIG.kvsPrefix }, function (resp, err) {
    if (err) {
      log("KVS.List error:", err);
      if (callback) callback(false);
      return;
    }
    // Normalize possible response shapes:
    // - resp.keys: ["key1", "key2", ...]
    // - resp.keys: { "key1": true, ... } (object map)
    // - resp.items: [{ key: "key1" }, ...]
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
      STATE.follows = newMap;
      log("No followed MACs.");
      if (callback) callback(true);
      return;
    }

    var pending = list.length;
    for (var i = 0; i < list.length; i++) {
      (function (k) {
        Shelly.call("KVS.Get", { key: k }, function (gresp, gerr) {
          if (gerr) {
            log("KVS.Get error for", k, ":", gerr);
          } else if (gresp && typeof gresp.value === "string") {
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
          } else {
            log("KVS.Get error for", k, gerr);
          }
          pending--;
          if (pending === 0) {
            STATE.follows = newMap;
            log("Loaded follows:", newMap);
            if (callback) callback(true);
          }
        });
      })(list[i]);
    }
  });
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
  var tid = Timer.set(ms, false, function () {
    var hasNext = follow && typeof follow.nextSwitchIndex === "number";
    // Always switch OFF current first
    Shelly.call("Switch.Set", { id: switchIndex, on: false }, function (r, e) {
      if (e) log("Switch.Set off error", switchIndex, e);
      else log("Auto-off switch", switchIndex);
      if (hasNext) {
        Shelly.call("Switch.Set", { id: follow.nextSwitchIndex, on: true }, function (r2, e2) {
          if (e2) log("Next Switch.Set on error", follow.nextSwitchIndex, e2);
          else log("Auto-next: turned on", follow.nextSwitchIdStr, "from", follow.switchIdStr);
        });
      }
    });
    STATE.offTimers[switchIndex] = 0;
  });
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

  var follow = STATE.follows[mac];
  if (!follow) return; // not followed

  // Only act on motion == 1 events
  var motion = data && data.motion;
  if (!(motion === 1 || motion === "1")) {
    // Ignore events without motion or with motion 0
    return;
  }

  log("Motion detected for", mac, "illuminance", data.illuminance, "min", follow.illuminanceMin, "max", follow.illuminanceMax);

  // If illuminance bounds are configured, enforce them
  var hasMin = typeof follow.illuminanceMin === "number";
  var hasMax = typeof follow.illuminanceMax === "number";
  if (hasMin || hasMax) {
    var illum = (data && typeof data.illuminance === "number") ? data.illuminance : null;
    if (illum === null) {
      // No illuminance provided in event; cannot evaluate bounds -> ignore
      log("Ignoring due to missing illuminance for bounds", mac, { min: follow.illuminanceMin, max: follow.illuminanceMax });
      return;
    }
    // Strictly greater than illuminance_min
    if (hasMin && illum <= follow.illuminanceMin) {
      return;
    }
    // Strictly less than illuminance_max
    if (hasMax && illum >= follow.illuminanceMax) {
      return;
    }
  }
  log("Illuminance bounds ok for", mac, "illuminance", data.illuminance, "min", follow.illuminanceMin, "max", follow.illuminanceMax);

  // Act: turn on configured switch, then setup auto-off
  var idx = follow.switchIndex;
  Shelly.call("Switch.Set", { id: idx, on: true }, function (resp, err) {
    if (err) log("Switch.Set on error", idx, err);
    else log("Turned on", follow.switchIdStr, "for", mac, "auto_off=", follow.autoOff, "s");
  });
  ensureAutoOffTimer(idx, follow.autoOff, follow);
}

function subscribeMqtt() {
  var topic = CONFIG.topicPrefix + "/#";
  MQTT.subscribe(topic, function (t, m, r) {
    handleBluEvent(t, m);
  });
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

function subscribeEvent() {
  Shelly.addEventHandler(function (eventData) {
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
        }
      }
    } catch (e) {
      log("Error handling event: ", e);
    }
  });
}

// Init
loadFollowsFromKVS();
subscribeMqtt();
subscribeEvent();
