// occupancy.js — home occupancy detection workflow (daemon-hosted).
//
// JS port of the Go occupancy service (myhome/occupancy): listed in
// daemon.scripts.run it REPLACES the Go implementation (both publish the
// retained myhome/occupancy topic).
//
// Heuristic (same as Go):
// - any Gen2 NotifyStatus event with an "input:N" change marks the home
//   occupied (button press, motion detector wired to an input, ...)
// - a configured mobile device seen online on the LAN marks the home occupied
//   (LAN polling is Go infrastructure, reached via MyHome.call("lan.hosts"))
// - occupancy expires after a quiet window; expiry publishes occupied=false
//
// Publishes (retained): myhome/occupancy = {"occupied":bool,"reason":string}
// RPC: occupancy.getstatus -> {"occupied":bool} (same verb as the Go service)
//
// KVS configuration (persisted in scripts-state/occupancy.json):
//   script/occupancy/window-hours     — quiet window before un-occupied (default 12)
//   script/occupancy/poll-minutes     — LAN presence poll period (default 5)
//   script/occupancy/mobile-patterns  — comma-separated device name patterns (default "iPhone")

var CONFIG = {
  windowMs: 12 * 3600 * 1000,
  pollMs: 5 * 60 * 1000,
  patterns: ["iPhone"],
  topic: "myhome/occupancy"
};

var STATE = {
  lastSeen: 0,
  occupied: false,
  expiryTimer: null
};

function publishStatus(occupied, reason) {
  STATE.occupied = occupied;
  var msg = JSON.stringify({ occupied: occupied, reason: reason });
  MQTT.publish(CONFIG.topic, msg, 1, true);
  MyHome.log("occupancy: " + msg);
}

function formatElapsed(ms) {
  var s = Math.floor(ms / 1000);
  var h = Math.floor(s / 3600);
  var m = Math.floor(s / 60) % 60;
  var sec = s % 60;
  if (h > 0) return h + "h " + m + "m " + sec + "s ago";
  if (m > 0) return m + "m " + sec + "s ago";
  return sec + "s ago";
}

function onWindowExpired() {
  STATE.expiryTimer = null;
  publishStatus(false, "last seen " + formatElapsed(Date.now() - STATE.lastSeen));
}

function markOccupied(reason) {
  STATE.lastSeen = Date.now();
  if (STATE.expiryTimer !== null) {
    Timer.clear(STATE.expiryTimer);
    STATE.expiryTimer = null;
  }
  publishStatus(true, reason);
  STATE.expiryTimer = Timer.set(CONFIG.windowMs, false, onWindowExpired);
}

// Gen2 events: {"method":"NotifyStatus","params":{"input:0":{...}}}
function onDeviceEvent(topic, message) {
  var payload = String(message);
  if (payload.indexOf('"NotifyStatus"') !== -1 && payload.indexOf('"input:') !== -1) {
    markOccupied("input change: " + payload);
  }
}

function matchesPattern(name) {
  var lower = String(name).toLowerCase();
  for (var i = 0; i < CONFIG.patterns.length; i++) {
    if (lower.indexOf(CONFIG.patterns[i].toLowerCase()) !== -1) return true;
  }
  return false;
}

function onLanHosts(result, error_code, error_message) {
  if (error_code !== 0) {
    MyHome.log("lan.hosts failed: " + error_message);
    return;
  }
  if (!result || !("hosts" in result) || !result.hosts) return;
  for (var i = 0; i < result.hosts.length; i++) {
    var h = result.hosts[i];
    if (!matchesPattern(h.name)) continue;
    if (String(h.status).toLowerCase() === "online" || h.alive > 0) {
      markOccupied("seen mobile: " + h.name + " (" + h.mac + ")");
      return;
    }
  }
}

function pollMobilePresence() {
  MyHome.call("lan.hosts", null, onLanHosts);
}

function onGetStatus(params) {
  return { occupied: STATE.occupied };
}

// ---- KVS-backed configuration (3 sequential loads, then start) ----

function applyConfigValue(key, value) {
  var v = String(value);
  if (key === "window-hours" && Number(v) > 0) {
    CONFIG.windowMs = Number(v) * 3600 * 1000;
  } else if (key === "poll-minutes" && Number(v) > 0) {
    CONFIG.pollMs = Number(v) * 60 * 1000;
  } else if (key === "mobile-patterns" && v.length > 0) {
    var parts = v.split(",");
    var patterns = [];
    for (var i = 0; i < parts.length; i++) {
      var p = parts[i].replace(/^\s+|\s+$/g, "");
      if (p.length > 0) patterns.push(p);
    }
    if (patterns.length > 0) CONFIG.patterns = patterns;
  }
}

var CONFIG_KEYS = ["window-hours", "poll-minutes", "mobile-patterns"];
var configIndex = 0;

function onConfigLoaded(result, error_code, error_message) {
  if (error_code === 0 && result && ("value" in result) && result.value !== null) {
    applyConfigValue(CONFIG_KEYS[configIndex], result.value);
  }
  configIndex++;
  loadNextConfig();
}

function loadNextConfig() {
  if (configIndex >= CONFIG_KEYS.length) {
    start();
    return;
  }
  Shelly.call("KVS.Get", { key: "script/occupancy/" + CONFIG_KEYS[configIndex] }, onConfigLoaded);
}

function start() {
  MQTT.subscribe("+/events/rpc", onDeviceEvent);
  Timer.set(CONFIG.pollMs, true, pollMobilePresence);
  Timer.set(3000, false, pollMobilePresence);
  MyHome.registerVerb("occupancy.getstatus", onGetStatus);
  MyHome.log("occupancy workflow started: window=" + (CONFIG.windowMs / 3600000) +
    "h poll=" + (CONFIG.pollMs / 60000) + "m patterns=" + CONFIG.patterns.join(","));
}

function main() {
  if (typeof MyHome === "undefined") {
    print("occupancy.js is a daemon workflow; it does nothing on a device");
    return;
  }
  loadNextConfig();
}

main();
