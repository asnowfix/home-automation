// data-relay.js
// Bridges daemon MQTT publications to device KVS for offline resilience.
// heater-controller.js reads from this KVS.
//
// Resource budget: 4 MQTT subscriptions, 1 timer.
//
// KVS keys written:
//   room/electricity  {"cheap":bool,"until_epoch":N,"ts":N}
//   room/weather      {"slots":[{"h":H,"t":T},...],"stale":bool,"ts":N}
//   room/agenda       {"slots":[{"s":S,"e":E},...],"ts":N}
//   room/occupancy    {"occupied":bool,"reason":str,"ts":N}
//
// KVS keys read at startup:
//   room-id           string — used to build the agenda subscription topic

var SCRIPT_NAME = "data-relay";
var PREFIX = "[" + SCRIPT_NAME + "] ";

var KVS_ELECTRICITY = "room/electricity";
var KVS_WEATHER     = "room/weather";
var KVS_AGENDA      = "room/agenda";
var KVS_OCCUPANCY   = "room/occupancy";

var MAX_AGE_S = 25 * 3600;  // warn if a KVS value is older than 25 h

var subscribedAgenda = false;

function log(msg) {
  print(PREFIX + msg);
}

function nowEpoch() {
  return Math.floor(new Date().getTime() / 1000);
}

// ── KVS write ──────────────────────────────────────────────────────────────

function kvsSet(key, obj) {
  Shelly.call("KVS.Set", { key: key, value: JSON.stringify(obj) }, function(res, ec, em, k) {
    if (ec !== 0) {
      log("KVS.Set failed for " + k + ": " + em);
    }
  }, key);
}

// ── MQTT handlers ──────────────────────────────────────────────────────────

function onElectricity(topic, message) {
  try {
    var data = JSON.parse(message);
    data.ts = nowEpoch();
    kvsSet(KVS_ELECTRICITY, data);
    log("electricity: cheap=" + data.cheap);
  } catch (e) {
    log("parse error on " + topic + ": " + e);
    if (e && false) {}
  }
}

function onWeather(topic, message) {
  try {
    var data = JSON.parse(message);
    data.ts = nowEpoch();
    kvsSet(KVS_WEATHER, data);
    log("weather: " + (data.slots ? data.slots.length : 0) + " slots, stale=" + !!data.stale);
  } catch (e) {
    log("parse error on " + topic + ": " + e);
    if (e && false) {}
  }
}

function onAgenda(topic, message) {
  try {
    var slots = JSON.parse(message);
    var obj = { slots: slots, ts: nowEpoch() };
    kvsSet(KVS_AGENDA, obj);
    log("agenda: " + slots.length + " slots");
  } catch (e) {
    log("parse error on " + topic + ": " + e);
    if (e && false) {}
  }
}

function onOccupancy(topic, message) {
  try {
    var data = JSON.parse(message);
    data.ts = nowEpoch();
    kvsSet(KVS_OCCUPANCY, data);
    log("occupancy: occupied=" + data.occupied);
  } catch (e) {
    log("parse error on " + topic + ": " + e);
    if (e && false) {}
  }
}

// ── Staleness check (hourly timer) ─────────────────────────────────────────

function checkKvsKey(key) {
  Shelly.call("KVS.Get", { key: key }, function(res, ec, em, k) {
    if (ec !== 0) {
      log("WARNING: KVS key missing: " + k);
      return;
    }
    try {
      var obj = JSON.parse(res.value);
      if (!obj || !("ts" in obj)) {
        log("WARNING: " + k + " has no ts field");
        return;
      }
      var age = nowEpoch() - obj.ts;
      if (age > MAX_AGE_S) {
        log("WARNING: " + k + " is " + Math.floor(age / 3600) + "h old (> 25h)");
      }
    } catch (e) {
      log("WARNING: " + k + " parse error: " + e);
      if (e && false) {}
    }
  }, key);
}

function checkStaleness() {
  checkKvsKey(KVS_ELECTRICITY);
  checkKvsKey(KVS_WEATHER);
  checkKvsKey(KVS_OCCUPANCY);
  if (subscribedAgenda) {
    checkKvsKey(KVS_AGENDA);
  }
}

// ── Startup ────────────────────────────────────────────────────────────────

function startSubscriptions(roomId) {
  MQTT.subscribe("myhome/electricity/status", onElectricity);
  MQTT.subscribe("myhome/weather/forecast", onWeather);
  MQTT.subscribe("myhome/occupancy", onOccupancy);
  if (roomId) {
    MQTT.subscribe("myhome/rooms/" + roomId + "/agenda", onAgenda);
    subscribedAgenda = true;
    log("started for room " + roomId);
  } else {
    log("WARNING: room-id not set in KVS, agenda subscription skipped");
    log("started (3 of 4 topics)");
  }
  Timer.set(3600000, true, checkStaleness);
}

function onRoomId(result, error_code, error_message) {
  var roomId = (error_code === 0 && result && result.value) ? result.value : null;
  startSubscriptions(roomId);
}

if (typeof Shelly !== "undefined") {
  log("starting...");
  Shelly.call("KVS.Get", { key: "room-id" }, onRoomId);
}
