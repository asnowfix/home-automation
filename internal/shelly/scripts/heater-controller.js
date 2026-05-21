// heater-controller.js
// Six-gate heater control loop. Reads policy from KVS (written by heater-data-relay.js).
// Direct Open-Meteo and occupancy MQTT access removed; all external state comes from KVS.
//
// Resource budget: 2–3 MQTT subscriptions, 2 timers.
//
// Gate conditions (evaluated in order on each control cycle):
//   Gate 0: frost override — internal temp ≤ frostThresholdC → heater ON immediately (timer 2)
//   Gate 1: electricity cheap now          — KVS room/electricity
//   Gate 2: weather predicts a cold slot   — KVS room/weather
//   Gate 3: home globally occupied         — KVS room/occupancy
//   Gate 4: room agenda says occupied now  — KVS room/agenda
//   Gate 5: all doors/windows closed       — local MQTT sensors
//
// All gates must pass for the heater to run in comfort mode.
// Gate 0 is a hard override: it bypasses gates 1–5.
//
// KVS config keys (pushed by daemon setup, read at startup):
//   room-id                                 (string, shared with heater-data-relay)
//   normally-closed                         (bool, default true)
//   script/heater/enable-logging            (bool, default true)
//   script/heater/internal-temperature-topic (string)
//   script/heater/door-sensor-topics        (comma-separated string, max 2 topics)
//   script/heater/frost-threshold-c         (number, default 7)
//   script/heater/cheap-horizon-h           (number, default 2)
//   script/heater/poll-interval-ms          (number, default 300000)
//   script/heater/setpoints                 (JSON: {comfort:float,eco:float}, defaults 21/17)

var SCRIPT_NAME = "heater-controller";
var PREFIX = "[" + SCRIPT_NAME + "] ";
var KEY_PREFIX = "script/heater/";

// KVS keys written by heater-data-relay (read-only here)
var KVS_ELECTRICITY = "room/electricity";
var KVS_WEATHER     = "room/weather";
var KVS_AGENDA      = "room/agenda";
var KVS_OCCUPANCY   = "room/occupancy";

// ── Default configuration ──────────────────────────────────────────────────
var CONFIG = {
  enableLogging:            true,
  normallyClosed:           true,
  internalTemperatureTopic: null,
  doorSensorTopics:         "",
  frostThresholdC:          7,
  cheapHorizonH:            2,
  pollIntervalMs:           300000,
  setpoints: { comfort: 21, eco: 17 }
};

// ── Runtime state ──────────────────────────────────────────────────────────
var STATE = {
  filteredTemp: null,
  doorSensors:  {},   // { topic: bool (true = open) }
  subscribedDoors: []
};

// ── Logging ────────────────────────────────────────────────────────────────

function log(msg) {
  if (!CONFIG.enableLogging) return;
  print(PREFIX + msg);
}

// ── Kalman filter (single-input) ───────────────────────────────────────────

function KalmanFilter(R, Q) {
  this.R   = (typeof R !== "undefined") ? R : 0.01;
  this.Q   = (typeof Q !== "undefined") ? Q : 1;
  this.cov = NaN;
  this.x   = NaN;
}

KalmanFilter.prototype.filter = function(z) {
  if (isNaN(this.x)) {
    this.x   = z;
    this.cov = this.Q;
  } else {
    var p = this.cov + this.R;
    var K = p / (p + this.Q);
    this.x   = this.x + K * (z - this.x);
    this.cov = (1 - K) * p;
  }
  return this.x;
};

var kalman = new KalmanFilter();

// ── Temperature parsing ────────────────────────────────────────────────────

function parseTempFromMqtt(topic, message) {
  try {
    // Gen1 H&T via proxy: shellies/<id>/sensor/temperature → plain float
    if (topic.indexOf("shellies/") === 0 && topic.indexOf("/sensor/temperature") > 0) {
      var t = parseFloat(message);
      if (!isNaN(t)) return t;
    }
    // BLU via blu-publisher.js: shelly-blu/events/<mac> → BTHome JSON
    if (topic.indexOf("shelly-blu/events/") === 0) {
      var d = JSON.parse(message);
      if ("temperature" in d) return d.temperature;
    }
  } catch (e) {
    if (e && false) {}
  }
  return null;
}

function onInternalTemp(topic, message) {
  var t = parseTempFromMqtt(topic, message);
  if (t !== null) {
    STATE.filteredTemp = kalman.filter(t);
    log("temp=" + t + " filtered=" + STATE.filteredTemp);
  }
}

// ── Door / window sensor ───────────────────────────────────────────────────

function parseDoorFromMqtt(topic, message) {
  try {
    // BLU door/window via blu-publisher.js: window field (1=open, 0=closed)
    if (topic.indexOf("shelly-blu/events/") === 0) {
      var d = JSON.parse(message);
      if ("window" in d) return d.window === 1;
    }
  } catch (e) {
    if (e && false) {}
  }
  return null;
}

function onDoorSensor(topic, message) {
  var open = parseDoorFromMqtt(topic, message);
  if (open !== null) {
    STATE.doorSensors[topic] = open;
    log("door " + topic + ": " + (open ? "OPEN" : "closed"));
  }
}

function isAnyDoorOpen() {
  for (var t in STATE.doorSensors) {
    if (STATE.doorSensors[t]) return true;
  }
  return false;
}

// ── Heater switch ──────────────────────────────────────────────────────────

function setHeater(on) {
  var sw = on !== CONFIG.normallyClosed;
  Shelly.call("Switch.Set", { id: 0, on: sw }, function(res, ec, em) {
    if (ec !== 0) {
      log("Switch.Set error: " + em);
    } else {
      log("heater " + (on ? "ON" : "OFF"));
    }
  });
}

// ── KVS read helper ────────────────────────────────────────────────────────

function kvsGet(key, cb) {
  Shelly.call("KVS.Get", { key: key }, function(res, ec, em, k) {
    if (ec !== 0) {
      cb(null, k);
    } else {
      try {
        cb(JSON.parse(res.value), k);
      } catch (e) {
        if (e && false) {}
        cb(null, k);
      }
    }
  }, key);
}

// ── Gate functions ─────────────────────────────────────────────────────────
// Each calls cb(null) to pass or cb("gate-N-reason") to block.

function gate1Electricity(cb) {
  kvsGet(KVS_ELECTRICITY, function(data) {
    if (!data || data.cheap !== true) {
      cb("gate-1-electricity-not-cheap");
    } else {
      cb(null);
    }
  });
}

function gate2Weather(cb) {
  kvsGet(KVS_WEATHER, function(data) {
    if (!data || !data.slots || data.slots.length === 0) {
      cb("gate-2-weather-unavailable");
      return;
    }
    var h = new Date().getHours();
    var slots = data.slots;
    var horizon = CONFIG.cheapHorizonH;
    var threshold = CONFIG.setpoints.eco;
    for (var i = 0; i < slots.length; i++) {
      if (slots[i].h >= h && slots[i].h <= h + horizon && slots[i].t < threshold) {
        cb(null);
        return;
      }
    }
    cb("gate-2-weather-warm");
  });
}

function gate3Occupancy(cb) {
  kvsGet(KVS_OCCUPANCY, function(data) {
    if (!data || data.occupied !== true) {
      cb("gate-3-home-unoccupied");
      return;
    }
    var age = Math.floor(new Date().getTime() / 1000) - (data.ts || 0);
    if (age > 12 * 3600) {
      cb("gate-3-occupancy-stale");
    } else {
      cb(null);
    }
  });
}

function gate4Agenda(cb) {
  kvsGet(KVS_AGENDA, function(data) {
    if (!data || !data.slots || data.slots.length === 0) {
      cb("gate-4-agenda-empty");
      return;
    }
    var now = new Date();
    var m = now.getHours() * 60 + now.getMinutes();
    var slots = data.slots;
    for (var i = 0; i < slots.length; i++) {
      if (m >= slots[i].s && m < slots[i].e) {
        cb(null);
        return;
      }
    }
    cb("gate-4-not-in-agenda");
  });
}

function gate5Doors(cb) {
  if (isAnyDoorOpen()) {
    cb("gate-5-door-open");
  } else {
    cb(null);
  }
}

// Run gates [idx..end] sequentially. Each gate is async; no deep stack nesting.
function runGates(gates, idx, cb) {
  if (idx >= gates.length) {
    cb(null);
    return;
  }
  gates[idx](function(blocked) {
    if (blocked) {
      cb(blocked);
    } else {
      runGates(gates, idx + 1, cb);
    }
  });
}

function evalGates(cb) {
  var gates = [gate1Electricity, gate2Weather, gate3Occupancy, gate4Agenda, gate5Doors];
  runGates(gates, 0, cb);
}

// ── Control loop (timer 1) ─────────────────────────────────────────────────

function controlLoop() {
  if (STATE.filteredTemp === null) {
    log("no temperature reading yet, skipping control cycle");
    return;
  }

  var temp = STATE.filteredTemp;

  // Gate 0: frost override (also runs every minute in frostCheck)
  if (temp <= CONFIG.frostThresholdC) {
    log("FROST override: temp=" + temp + " <= " + CONFIG.frostThresholdC);
    setHeater(true);
    return;
  }

  // At or above comfort setpoint — no heating needed regardless of gates
  if (temp >= CONFIG.setpoints.comfort) {
    log("at comfort setpoint (" + CONFIG.setpoints.comfort + "), heater OFF");
    setHeater(false);
    return;
  }

  evalGates(function(blocked) {
    if (blocked) {
      log("heater OFF: " + blocked);
      setHeater(false);
    } else {
      log("all gates pass, temp=" + temp + " < " + CONFIG.setpoints.comfort + " => heater ON");
      setHeater(true);
    }
  });
}

// ── Frost check (timer 2, every 60 s) ─────────────────────────────────────

function frostCheck() {
  if (STATE.filteredTemp !== null && STATE.filteredTemp <= CONFIG.frostThresholdC) {
    log("FROST check: temp=" + STATE.filteredTemp + " => heater ON immediately");
    setHeater(true);
  }
}

// ── MQTT subscriptions ─────────────────────────────────────────────────────

function subscribeTemperature() {
  var t = CONFIG.internalTemperatureTopic;
  if (!t) {
    log("WARNING: internalTemperatureTopic not configured — heater will not control");
    return;
  }
  MQTT.subscribe(t, onInternalTemp);
  log("subscribed to temperature: " + t);
}

function subscribeDoors() {
  var topicsStr = CONFIG.doorSensorTopics;
  if (!topicsStr) {
    log("no door sensor topics configured");
    return;
  }
  var topics = topicsStr.split(",");
  for (var i = 0; i < topics.length && i < 2; i++) {
    var t = topics[i];
    while (t.length > 0 && t.charAt(0) === " ") t = t.substring(1);
    while (t.length > 0 && t.charAt(t.length - 1) === " ") t = t.substring(0, t.length - 1);
    if (t.length === 0) continue;
    MQTT.subscribe(t, onDoorSensor);
    STATE.subscribedDoors.push(t);
    STATE.doorSensors[t] = false;  // assume closed until first message
    log("subscribed to door sensor: " + t);
  }
}

// ── Config loading ─────────────────────────────────────────────────────────

function applyKvsConfig(items) {
  function find(key) {
    for (var i = 0; i < items.length; i++) {
      if (items[i].key === key) return items[i].value;
    }
    return null;
  }

  function parseBool(v) {
    if (v === "true" || v === true) return true;
    if (v === "false" || v === false) return false;
    return null;
  }

  function parseNum(v) {
    var n = parseFloat(v);
    return isNaN(n) ? null : n;
  }

  function parseObj(v) {
    try {
      return JSON.parse(v);
    } catch (e) {
      if (e && false) {}
      return null;
    }
  }

  var b = parseBool(find("normally-closed"));
  if (b !== null) CONFIG.normallyClosed = b;

  b = parseBool(find(KEY_PREFIX + "enable-logging"));
  if (b !== null) CONFIG.enableLogging = b;

  var v = find(KEY_PREFIX + "internal-temperature-topic");
  if (v) CONFIG.internalTemperatureTopic = v;

  v = find(KEY_PREFIX + "door-sensor-topics");
  if (v) CONFIG.doorSensorTopics = v;

  var n = parseNum(find(KEY_PREFIX + "frost-threshold-c"));
  if (n !== null) CONFIG.frostThresholdC = n;

  n = parseNum(find(KEY_PREFIX + "cheap-horizon-h"));
  if (n !== null) CONFIG.cheapHorizonH = n;

  n = parseNum(find(KEY_PREFIX + "poll-interval-ms"));
  if (n !== null) CONFIG.pollIntervalMs = n;

  var o = parseObj(find(KEY_PREFIX + "setpoints"));
  if (o && typeof o === "object") CONFIG.setpoints = o;

  var roomId = find("room-id");
  log("config: room=" + roomId +
      " tempTopic=" + CONFIG.internalTemperatureTopic +
      " frost=" + CONFIG.frostThresholdC +
      " comfort=" + CONFIG.setpoints.comfort);
}

function onKvsLoaded(result, error_code, error_message) {
  var items = (error_code === 0 && result && result.items) ? result.items : [];
  applyKvsConfig(items);
  subscribeTemperature();
  subscribeDoors();
  Timer.set(CONFIG.pollIntervalMs, true, controlLoop);
  Timer.set(60000, true, frostCheck);
  log("started: pollInterval=" + CONFIG.pollIntervalMs + "ms, frost check every 60s");
}

// ── Entry point ────────────────────────────────────────────────────────────

if (typeof Shelly !== "undefined") {
  log("starting...");
  Shelly.call("KVS.GetMany", { match: "*" }, onKvsLoaded);

  // Reload non-subscription config when KVS changes (setpoints, thresholds)
  Shelly.addStatusHandler(function(status) {
    if (status && status.component === "sys" && status.delta && ("kvs_rev" in status.delta)) {
      log("KVS updated, refreshing config...");
      Shelly.call("KVS.GetMany", { match: "*" }, function(result, ec) {
        if (ec === 0 && result && result.items) {
          applyKvsConfig(result.items);
        }
      });
    }
  });
}
