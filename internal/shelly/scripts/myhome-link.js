// myhome-link.js — device <-> daemon distributed execution: calling convention
// demo and self-test.
//
// The same script runs on both sides ("every device script can assume it is
// also present on the daemon"):
// - On the daemon script host (goja), the MyHome global exists: the script
//   registers invocation handlers via MyHome.on(name, fn).
// - On a Shelly device, the script invokes those handlers through the
//   script.invoke RPC verb: publish a request on "<instance>/rpc", receive
//   the response on "<own-mqtt-prefix>/myhome/rpc".
//
// Device-side request format (myhome RPC protocol):
//   { id: "<unique>", src: "<prefix>/myhome", dst: "<instance>",
//     method: "script.invoke",
//     params: { script: "<name>", name: "<handler>", params: {...} } }
// The daemon answers on "<src>/rpc" with { id, result | error }.
//
// KVS configuration (device side):
//   script/myhome-link/instance — daemon instance to call (default "myhome")

var CONFIG = {
  instance: "myhome",
  pingPeriodMs: 60000,
  timeoutMs: 10000
};

// ---------------------------------------------------------------- daemon side

function onDaemonPing(params) {
  return {
    pong: true,
    instance: MyHome.instance(),
    received: params || null,
    ts: Date.now()
  };
}

function setupDaemon() {
  MyHome.on("ping", onDaemonPing);
  MyHome.log("myhome-link ready on instance " + MyHome.instance());
}

// ---------------------------------------------------------------- device side

var STATE = {
  topicPrefix: null,
  pending: {}, // id -> { sentAt: ms, cb: function|null } (null = slot free)
  nextId: 1
};

function log() {
  var parts = [];
  for (var i = 0; i < arguments.length; i++) {
    var a = arguments[i];
    parts.push(typeof a === "object" ? JSON.stringify(a) : String(a));
  }
  print("[myhome-link] " + parts.join(" "));
}

// callDaemon invokes handler `name` of daemon-hosted script `script`.
// cb(result, error) is optional.
function callDaemon(script, name, params, cb) {
  var id = "dev-" + STATE.nextId;
  STATE.nextId++;
  STATE.pending[id] = { sentAt: Date.now(), cb: cb || null };
  var req = {
    id: id,
    src: STATE.topicPrefix + "/myhome",
    dst: CONFIG.instance,
    method: "script.invoke",
    params: { script: script, name: name, params: params }
  };
  MQTT.publish(CONFIG.instance + "/rpc", JSON.stringify(req));
}

function onResponse(topic, message) {
  var res = null;
  try {
    res = JSON.parse(message);
  } catch (e) {
    log("bad response:", message, String(e));
    return;
  }
  if (!res || !("id" in res)) return;
  var p = STATE.pending[res.id];
  if (!p) return;
  STATE.pending[res.id] = null; // free the slot (no delete on Espruino)
  var rtt = Date.now() - p.sentAt;
  if ("error" in res && res.error) {
    log("daemon error (rtt " + rtt + "ms):", res.error.message);
    if (p.cb) p.cb(null, res.error);
    return;
  }
  log("daemon response (rtt " + rtt + "ms):", res.result);
  if (p.cb) p.cb(res.result, null);
}

// checkTimeouts drops expired requests and compacts the pending map.
function checkTimeouts() {
  var now = Date.now();
  var keep = {};
  for (var id in STATE.pending) {
    var p = STATE.pending[id];
    if (!p) continue;
    if (now - p.sentAt > CONFIG.timeoutMs) {
      log("timeout for", id);
      if (p.cb) p.cb(null, { code: -1, message: "timeout" });
    } else {
      keep[id] = p;
    }
  }
  STATE.pending = keep;
}

function pingDaemon() {
  checkTimeouts();
  var sys = Shelly.getComponentStatus("sys");
  var uptime = sys && ("uptime" in sys) ? sys.uptime : null;
  callDaemon("myhome-link", "ping", { from: STATE.topicPrefix, uptime: uptime }, null);
}

function setupDevice() {
  var mqttConfig = Shelly.getComponentConfig("mqtt");
  STATE.topicPrefix = mqttConfig.topic_prefix;
  MQTT.subscribe(STATE.topicPrefix + "/myhome/rpc", onResponse);
  Timer.set(CONFIG.pingPeriodMs, true, pingDaemon);
  Timer.set(2000, false, pingDaemon); // first ping shortly after start
  log("device mode: prefix=" + STATE.topicPrefix + " instance=" + CONFIG.instance);
}

function onInstanceLoaded(result, error_code, error_message) {
  if (error_code === 0 && result && ("value" in result) && result.value) {
    CONFIG.instance = String(result.value);
  }
  setupDevice();
}

function main() {
  if (typeof MyHome !== "undefined") {
    setupDaemon();
  } else {
    Shelly.call("KVS.Get", { key: "script/myhome-link/instance" }, onInstanceLoaded);
  }
}

main();
