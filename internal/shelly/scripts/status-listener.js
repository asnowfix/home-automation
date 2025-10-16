// Shelly Script: Device Status Listener
// - Subscribes to MQTT topics "+/events/rpc" and mirrors remote device switch status
// - Keeps only events whose device is followed via KVS keys: follow/status/<DEVICE_ID>
//   Value must be a JSON string: { "switch_id":"switch:0", "follow_id":"switch:0" }
// - Action is inferred from follow_id type:
//   - switch:X -> mirrors the switch state (set action)
//   - input:X -> toggles on button release (toggle action)

/**
 * The KVS value `follow/status/<DEVICE_ID>` must be a JSON string:
 * @typedef {Object} FollowConfig
 * @property {string} switch_id - Local switch ID to control, e.g. "switch:0"
 * @property {string} [follow_id="switch:0"] - Remote input to monitor: "switch:0" (mirror) or "input:0" (toggle)
 * @example
 * {"switch_id":"switch:0", "follow_id":"switch:0"} // Mirror switch state
 * @example
 * {"switch_id":"switch:1", "follow_id":"input:0"} // Toggle on button release
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

function parseInputId(inputIdStr) {
  if (typeof inputIdStr !== "string") return null;
  var parts = inputIdStr.split(":");
  if (parts.length !== 2) return null;
  var type = parts[0];
  var n = Number(parts[1]);
  if (isNaN(n)) return null;
  if (type !== "switch" && type !== "input") return null;
  return { type: type, index: n, id: inputIdStr };
}

function normalizeId(s) {
  if (!s) return "";
  return String(s).toLowerCase();
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
  
  try {
    var value = JSON.parse(gresp.value);
    var switchIdStr = value && value.switch_id ? String(value.switch_id) : null;
    var followIdStr = value && value.follow_id ? String(value.follow_id) : "switch:0";
    
    var devId = k.substr(CONFIG.kvsPrefix.length);
    devId = normalizeId(devId);
    var switchIdx = parseSwitchIndex(switchIdStr);
    var inputInfo = parseInputId(followIdStr);
    
    if (devId && switchIdx !== null && inputInfo) {
      // Infer action from input type: switch -> set, input -> toggle
      var action = inputInfo.type === "switch" ? "set" : "toggle";
      newMap[devId] = {
        switchIdStr: switchIdStr,
        switchIndex: switchIdx,
        followId: followIdStr,
        inputType: inputInfo.type,
        inputIndex: inputInfo.index,
        action: action
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
  STATE.follows = newMap;
  log("Loaded follows:", newMap);
  if (callback) callback(true);
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

    // Process keys sequentially to avoid "too many calls in progress"
    processKeysSequentially(list, newMap, callback, 0);
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

  if (!msg || (msg.method !== "NotifyStatus" && msg.method !== "NotifyEvent")) {
    log("Ignoring non-NotifyStatus/NotifyEvent message", msg ? msg.method : "null", "from topic", topic);
    return;
  }
  
  var src = normalizeId(msg.src);
  if (!src) {
    log("No valid src in message", msg);
    return;
  }

  //log("Checking follows for device", src, "available follows:", Object.keys(STATE.follows));
  var follow = STATE.follows[src];
  if (!follow) {
    //log("Device not followed", src);
    return;
  }

  log("Got message from a device we are following", src, follow, msg);

  // Handle NotifyEvent messages (single_push events treated as input:0 state false)
  if (msg.method === "NotifyEvent") {
    var events = msg.params && msg.params.events;
    if (!events || !Array.isArray(events)) {
      log("No events array in NotifyEvent message", msg);
      return;
    }
    
    // Look for single_push events on the followed input component
    var relevantEvent = null;
    for (var i = 0; i < events.length; i++) {
      var evt = events[i];
      if (evt.component === follow.followId && evt.event === "single_push") {
        relevantEvent = evt;
        break;
      }
    }
    
    if (!relevantEvent) {
      log("No relevant single_push event found for", follow.followId, "in events:", events);
      return;
    }
    
    // Only handle input types for single_push events (buttons)
    if (follow.inputType !== "input") {
      log("Ignoring single_push event for non-input type:", follow.inputType);
      return;
    }
    
    // Treat single_push as equivalent to input:0 state false (button release)
    log("Processing single_push event as button release for", follow.followId);
    var idx = follow.switchIndex;
    
    Shelly.emitEvent("remote-input-event", {
      "src": src,
      "component": follow.switchIdStr,
    });

    if (follow.action === "toggle") {
      log("Attempting to toggle switch", follow.switchIdStr, "index", idx, "triggered by single_push on", follow.followId);
      Shelly.call("Switch.Toggle", { id: idx }, function (resp, err) {
        if (err) {
          log("Switch.Toggle error for", follow.switchIdStr, "index", idx, "error:", err);
        } else {
          log("Switch.Toggle success for", follow.switchIdStr, "index", idx, "response:", resp);
          log("Toggled", src, "=>", follow.switchIdStr, "(single_push event)");
        }
      });
    }
    return;
  }

  var params = msg.params || {};
  var idx = follow.switchIndex;
  
  // Infer action from input type: switch -> set, input -> toggle
  var inferredAction = follow.inputType === "switch" ? "set" : "toggle";
  
  if (inferredAction === "toggle") {
    var inputComponent = params[follow.followId];
    if (!inputComponent) {
      log("No data for toggle input", src, follow.followId);
      return;
    }
    
    var triggerState = null;
    var shouldTrigger = false;
    
    if (follow.inputType === "switch") {
      // For switches/relays: mirror the state (toggle when switch changes to ON)
      if (typeof inputComponent.output === "boolean") {
        triggerState = inputComponent.output;
        shouldTrigger = (triggerState === true);
        log("Switch input detected:", follow.followId, "output:", triggerState, "will trigger:", shouldTrigger);
      }
    } else if (follow.inputType === "input") {
      // For buttons/inputs: toggle on button release (state: false)
      if (typeof inputComponent.state === "boolean") {
        triggerState = inputComponent.state;
        shouldTrigger = (triggerState === false);
        log("Button input detected:", follow.followId, "state:", triggerState, "will trigger:", shouldTrigger);
      }
    }
    
    if (!shouldTrigger) {
      log("Ignoring toggle trigger", src, follow.followId, "type:", follow.inputType, "state:", triggerState);
      return;
    }
    
    var triggerReason = follow.inputType === "switch" ? "switch ON" : "button release";
    log("Attempting to toggle switch", follow.switchIdStr, "index", idx, "triggered by", follow.followId, triggerReason);
    Shelly.call("Switch.Toggle", { id: idx }, function (resp, err) {
      if (err) {
        log("Switch.Toggle error for", follow.switchIdStr, "index", idx, "error:", err);
      } else {
        log("Switch.Toggle success for", follow.switchIdStr, "index", idx, "response:", resp);
        log("Toggled", src, "=>", follow.switchIdStr, "(" + triggerReason + ")");
      }
    });
    return;
  }
  
  // Action is "set" (inferred from switch input type) - mirror the input status
  var inputComponent = params[follow.followId];
  if (!inputComponent) {
    log("No data for the specified input", src, follow.followId);
    return;
  }
  
  var desired = null;
  if (follow.inputType === "switch") {
    if (typeof inputComponent.output === "boolean") {
      desired = inputComponent.output;
    }
  } else if (follow.inputType === "input") {
    if (typeof inputComponent.state === "boolean") {
      desired = inputComponent.state;
    }
  }
  
  if (desired === null) {
    log("No valid status found for", src, follow.inputId);
    return;
  }

  Shelly.call("Switch.Set", { id: idx, on: desired }, function (resp, err) {
    if (err) log("Switch.Set error", idx, err);
    else log("Mirrored", src, follow.inputId, "=>", follow.switchIdStr, "on=", desired);
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
      } else if (eventData && eventData.info && eventData.info.event === "script_stop") {
        log("Script stopping");
      }
    } catch (e) {
      log("Error handling event:", e);
    }
  });
  log("Subscribed to KVS change events");
}

// Init
log("Script starting...");
loadFollowsFromKVS(function(success) {
  if (success) {
    log("Script initialization complete");
  } else {
    log("Script initialization failed");
  }
});
subscribeMqtt();
subscribeKvsEvents();
