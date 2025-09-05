// Shelly Script: Device Status Listener
// - Subscribes to MQTT topics "+/events/rpc" and mirrors remote device switch status
// - Keeps only events whose device is followed via KVS keys: follow/status/<DEVICE_ID>
//   Value must be a JSON string: { "switch_id":"switch:0" }
// - On match: Switch.Set on the configured local switch to the same state as remote switch:0.output

/**
 * The KVS value `follow/status/<DEVICE_ID>` must be a JSON string:
 * @typedef {Object} FollowConfig
 * @property {string} switch_id - Local switch ID to drive, e.g. "switch:0"
 * @example
 * {"switch_id":"switch:0"}
 */

var CONFIG = {
  script: "[status-listener] ",
  topicFilter: "+/events/rpc", // wildcard per remote device
  kvsPrefix: "follow/status/",
  log: true
};

var STATE = {
  // deviceId (lowercase) => { switchIdStr: string, switchIndex: number }
  follows: {}
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
      if (e && false) {}
    }
    if (i + 1 < arguments.length) s += " ";
  }
  print(CONFIG.script, s);
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

function normalizeId(s) {
  if (!s) return "";
  return String(s).toLowerCase();
}

function loadFollowsFromKVS(callback) {
  Shelly.call("KVS.List", { prefix: CONFIG.kvsPrefix }, function (resp, err) {
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
          for (var k in resp.keys) if (resp.keys.hasOwnProperty(k)) list.push(k);
        }
      } else if (resp.items && resp.items.length) {
        for (var i = 0; i < resp.items.length; i++) {
          var it = resp.items[i];
          if (it && it.key) list.push(it.key);
        }
      }
    }

    var newMap = {};
    if (!list || !list.length) {
      STATE.follows = newMap;
      log("No followed devices.");
      if (callback) callback(true);
      return;
    }

    var pending = list.length;
    for (var li = 0; li < list.length; li++) {
      (function (k) {
        Shelly.call("KVS.Get", { key: k }, function (gresp, gerr) {
          if (gerr) {
            log("KVS.Get error for", k, ":", gerr);
          } else if (gresp && typeof gresp.value === "string") {
            try {
              var value = JSON.parse(gresp.value);
              var switchIdStr = value && value.switch_id ? String(value.switch_id) : null;
              var devId = k.substr(CONFIG.kvsPrefix.length);
              devId = normalizeId(devId);
              var idx = parseSwitchIndex(switchIdStr);
              if (devId && idx !== null) {
                newMap[devId] = {
                  switchIdStr: switchIdStr,
                  switchIndex: idx
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
      })(list[li]);
    }
  });
}


function handleStatusEvent(topic, message) {
  var msg = null;
  try {
    msg = JSON.parse(message);
  } catch (e) {
    log("Invalid JSON on", topic, "payload:", message, "err:", e);
    return;
  }

  if (!msg || msg.method !== "NotifyStatus") return;
  var src = normalizeId(msg.src);
  if (!src) return;

  var follow = STATE.follows[src];
  if (!follow) return;

  // Expecting switch:0 presence; mirror its 'output' to local switch
  var params = msg.params || {};
  var sw = params["switch:0"]; // simple first version
  if (!sw || typeof sw.output !== "boolean") {
    // Nothing to mirror
    return;
  }

  var desired = sw.output ? true : false;
  var idx = follow.switchIndex;
  Shelly.call("Switch.Set", { id: idx, on: desired }, function (resp, err) {
    if (err) log("Switch.Set error", idx, err);
    else log("Mirrored", src, "=>", follow.switchIdStr, "on=", desired);
  });
}

function subscribeMqtt() {
  var topic = CONFIG.topicFilter;
  MQTT.subscribe(topic, function (t, m, r) {
    handleStatusEvent(t, m);
  });
  log("Subscribed to", topic);
}

function subscribeKvsEvents() {
  Shelly.addEventHandler(function (eventData) {
    try {
      if (eventData && eventData.info && eventData.info.event === "kvs") {
        var kvsEvent = eventData.info;
        // Check if the KVS change affects our prefix
        if (kvsEvent.key && kvsEvent.key.indexOf(CONFIG.kvsPrefix) === 0) {
          log("KVS change detected for key:", kvsEvent.key, "action:", kvsEvent.action);
          loadFollowsFromKVS();
        }
      }
    } catch (e) {
      log("Error handling KVS event:", e);
    }
  });
  log("Subscribed to KVS change events");
}

// Init
loadFollowsFromKVS();
subscribeMqtt();
subscribeKvsEvents();
