// pool-pump.js
// ------------
//
// Unified pool pump control — same script runs on all devices in the mesh.
// Each device compares CONFIG.preferredDeviceId to its own ID to decide if it
// should be the active pump controller.
//
// Pro3 (3-switch variator):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pumps)
//   - Input 1: High-water sensor (MQTT notification)
//   - Input 2: Max speed active from other device (MQTT notification)
//   - Switch 0-2: Pump speed stages (eco/mid/high, configurable via KVS)
//   - Button: Cycles through speeds
//
// Pro1 (1-switch):
//   - Input 0: Water supply sensor (inverted: HIGH = water supply ON → turn off pump)
//   - Input 1: High-water sensor (MQTT notification)
//   - Switch 0: Pump on/off
//   - Button: Toggles on/off
//
// Features:
//   - Schedule-driven automation: daily-check (sunrise), morning-start (SR+3h),
//     evening-stop (sunset), night-start (23:15), night-stop (00:15)
//   - Summer/winter mode based on weather forecast (Open-Meteo)
//   - Water supply protection with speed restoration
//   - Cross-device safety: grace delay prevents Pro3 and Pro1 from running simultaneously
//   - Physical button cycling with detached input mode

// === STATIC CONSTANTS ===
var SCRIPT_NAME = "pool-pump";
var CONFIG_KEY_PREFIX = "script/" + SCRIPT_NAME + "/";
var SCRIPT_PREFIX = "[" + SCRIPT_NAME + "] ";

// Configuration schema
// Both Pro3 and Pro1 run this same script with shared KVS configuration
// Script compares preferred_device_id against its own device ID to decide if it should run
var CONFIG_SCHEMA = {
  enableLogging: {
    description: "Enable logging when true",
    key: "logging",
    default: true,
    type: "boolean"
  },
  mqttTopicPrefix: {
    description: "MQTT topic prefix (written by CLI, not used by script)",
    key: "mqtt-topic",
    default: "pool/pump",
    type: "string",
    cliOnly: true
  },
  preferredDeviceId: {
    description: "Which device ID should run (actual Shelly device ID). Script compares this to its own ID",
    key: "preferred",
    default: null,
    type: "string",
    required: true
  },
  pro3DeviceId: {
    description: "Pro3 device ID (for MQTT subscriptions - cross-device status tracking)",
    key: "pro3-id",
    default: null,
    type: "string",
    required: false
  },
  pro1DeviceId: {
    description: "Pro1 device ID (for MQTT subscriptions - cross-device status tracking)",
    key: "pro1-id",
    default: null,
    type: "string",
    required: false
  },
  preferredSpeed: {
    description: "Speed: 'eco', 'mid', 'high', 'max'. Maps to switches based on device capabilities",
    key: "speed",
    default: "eco",
    type: "string",
    required: false
  },
  ecoSpeed: {
    description: "Pro3 switch ID for eco/low speed (0, 1, or 2)",
    key: "eco-speed",
    default: 2,
    type: "number",
    required: false
  },
  midSpeed: {
    description: "Pro3 switch ID for mid speed (0, 1, or 2)",
    key: "mid-speed",
    default: 1,
    type: "number",
    required: false
  },
  highSpeed: {
    description: "Pro3 switch ID for high speed (0, 1, or 2)",
    key: "high-speed",
    default: 0,
    type: "number",
    required: false
  },
  nightRunDurationMs: {
    description: "Night run duration in ms (written by CLI, not used by script)",
    key: "night-duration",
    default: 3600000,
    type: "number",
    cliOnly: true
  },
  graceDelayMs: {
    description: "Cross-device grace delay in ms (minimum 10000)",
    key: "grace-delay",
    default: 10000,
    type: "number",
    required: false
  },
  temperatureThreshold: {
    description: "Temperature threshold (°C) for summer mode (day schedule)",
    key: "temp-threshold",
    default: 20,
    type: "number",
    required: false
  },
  poolVolume: {
    description: "Pool volume in m³",
    key: "pool-volume",
    default: 46,
    type: "number"
  },
  turnover: {
    description: "Daily turnover target (number of full pool volumes to filter per day)",
    key: "turnover",
    default: 5,
    type: "number"
  },
  maxFlowRate: {
    description: "Pump max flow rate in m³/h at max RPM",
    key: "max-flow-rate",
    default: 31,
    type: "number"
  },
  maxRpm: {
    description: "Pump rated max RPM",
    key: "max-rpm",
    default: 2900,
    type: "number"
  },
  ecoRpm: {
    description: "Variator RPM setting for eco speed",
    key: "eco-rpm",
    default: 2000,
    type: "number"
  },
  midRpm: {
    description: "Variator RPM setting for mid speed",
    key: "mid-rpm",
    default: 2600,
    type: "number"
  },
  highRpm: {
    description: "Variator RPM setting for high speed",
    key: "high-rpm",
    default: 2900,
    type: "number"
  },
  maxTemp: {
    description: "Temperature (°C) at which run time reaches one full turnover",
    key: "max-temp",
    default: 35,
    type: "number"
  }
};

// Component names by device type (inputs are static; switch names are built
// dynamically from CONFIG speed mapping — see buildSwitchNames)
var COMPONENT_NAMES = {
  "pro3": {
    inputs: [
      {id: 0, name: "water-supply", invert: true},
      {id: 1, name: "high-water", invert: false},
      {id: 2, name: "max-speed-active", invert: false}
    ]
  },
  "pro1": {
    inputs: [
      {id: 0, name: "water-supply", invert: true},
      {id: 1, name: "high-water", invert: false}
    ]
  }
};

// Build switch name list from CONFIG speed-to-switch mapping.
// Called after config is loaded so CONFIG.ecoSpeed etc. are available.
function buildSwitchNames() {
  if (STATE.deviceType === "pro3") {
    var names = [];
    names.push({id: CONFIG.ecoSpeed, name: "pump-eco"});
    names.push({id: CONFIG.midSpeed, name: "pump-mid"});
    names.push({id: CONFIG.highSpeed, name: "pump-high"});
    return names;
  } else if (STATE.deviceType === "pro1") {
    return [{id: 0, name: "pump-max"}];
  }
  return [];
}

// Runtime configuration values (initialized from defaults)
var CONFIG = {};

// Initialize CONFIG with default values
function initConfig() {
  for (var key in CONFIG_SCHEMA) {
    CONFIG[key] = CONFIG_SCHEMA[key].default;
  }
}

// Initialize CONFIG with defaults immediately so logging works
initConfig();

// Load configuration from KVS and validate required fields
function loadConfig(callback) {
  log("Loading configuration from KVS...");
  
  var missingRequired = [];
  var configKeys = [];
  
  // Build array of config keys to load (skip cliOnly — written by CLI, not needed at runtime)
  for (var key in CONFIG_SCHEMA) {
    if (!CONFIG_SCHEMA[key].cliOnly) {
      configKeys.push(key);
    }
  }
  
  var keyIndex = 0;
  
  // Process one key at a time using task queue
  function loadNextKey() {
    if (keyIndex >= configKeys.length) {
      // All keys loaded, validate
      if (missingRequired.length > 0) {
        log("ERROR: Missing required configuration:");
        for (var i = 0; i < missingRequired.length; i++) {
          log("  -", missingRequired[i]);
        }
        log("Script cannot start without required configuration.");
        log("Please run: ctl pool setup <pro3-device> --pro1 <pro1-device>");
        callback(false);
        return;
      }

      // Clamp grace delay to minimum safe value
      if (CONFIG.graceDelayMs < 10000) {
        log("WARNING: grace-delay below minimum (10000ms), clamping");
        CONFIG.graceDelayMs = 10000;
      }

      // Enumerate available outputs and inputs
      var availableOutputs = [];
      for (var oi = 0; oi < 4; oi++) {
        var swSt = Shelly.getComponentStatus("switch:" + oi);
        if (swSt && ("output" in swSt)) {
          availableOutputs.push(oi);
        }
      }
      STATE.outputs = availableOutputs;

      var availableInputs = [];
      for (var ii = 0; ii < 4; ii++) {
        var inSt = Shelly.getComponentStatus("input:" + ii);
        if (inSt && ("state" in inSt)) {
          availableInputs.push(ii);
        }
      }
      STATE.inputs = availableInputs;

      // Detect device type based on switch count
      if (availableOutputs.length >= 3) {
        STATE.deviceType = "pro3";
        log("Detected device type: Pro3 (3 switches)");
      } else if (availableOutputs.length === 1) {
        STATE.deviceType = "pro1";
        log("Detected device type: Pro1 (1 switch)");
      } else {
        STATE.deviceType = "unknown";
        log("WARNING: Could not detect device type");
      }
      log("Switches:", availableOutputs, "Inputs:", availableInputs);

      // Cache my device ID
      var deviceInfo = Shelly.getDeviceInfo();
      if (deviceInfo && deviceInfo.id) {
        STATE.myDeviceId = deviceInfo.id;
        log("My device ID:", STATE.myDeviceId);
      } else {
        log("ERROR: Could not get device ID");
        callback(false);
        return;
      }

      log("Configuration loaded successfully");
      // Free schema object — only needed during KVS loading, not at runtime
      CONFIG_SCHEMA = null;
      callback(true);
      return;
    }
    
    var key = configKeys[keyIndex];
    var schema = CONFIG_SCHEMA[key];
    var kvsKey = CONFIG_KEY_PREFIX + schema.key;
    keyIndex++;
    
    // Load from KVS asynchronously
    Shelly.call("KVS.Get", {key: kvsKey}, function(result, err) {
      if (err) {
        log("WARNING: KVS.Get failed for", kvsKey, ":", err, "- using default");
        CONFIG[key] = schema.default;
        if (schema.required && CONFIG[key] === null) {
          missingRequired.push(key + " (" + kvsKey + ") - KVS error: " + err);
        }
        queueTask(loadNextKey);
        return;
      }

      if (result && ("value" in result) && result.value !== null && result.value !== "") {
        var value = result.value;
        
        // Parse value based on type
        if (schema.type === "boolean") {
          CONFIG[key] = value === "true" || value === true;
        } else if (schema.type === "number") {
          var num = Number(value);
          if (!isNaN(num)) {
            CONFIG[key] = num;
          } else {
            log("WARNING: Invalid number for", key, ":", value);
            CONFIG[key] = schema.default;
          }
        } else {
          CONFIG[key] = value;
        }
      } else {
        // Use default
        CONFIG[key] = schema.default;
        
        // Check if required
        if (schema.required && CONFIG[key] === null) {
          missingRequired.push(key + " (" + kvsKey + ")");
        }
      }
      
      // Queue next key
      queueTask(loadNextKey);
    });
  }
  
  loadNextKey();
}


// Script.storage keys for continuously evolving values (survives reboots, synchronous)
var STORAGE_KEYS = {
  forecastUrl:   "forecast-url",    // Open-Meteo forecast URL built from device location
  scheduleMode:  "schedule-mode"    // "summer" or "winter" — moved here from KVS because
                                    // Script.storage.getItem() is synchronous; KVS.Get is
                                    // async-only, so Shelly.call without a callback always
                                    // returns null and schedule mode was lost on every reboot
};

// State keys for KVS persistence (fire-and-forget writes only; reads use Script.storage)
var STATE_KEYS = {
  activeOutput: "active-output"     // -1 (all off), 0, 1, 2 for pro3; 0 for pro1
};

// === TASK QUEUE (SINGLE TIMER FOR ALL SEQUENTIAL OPERATIONS) ===
var TASK_QUEUE = [];
var TASK_INDEX = 0;
var TASK_TIMER = null;

function processTaskQueue() {
  if (TASK_INDEX >= TASK_QUEUE.length) {
    // No tasks left — stop timer and reset so queueTask() can restart it later
    if (TASK_TIMER) {
      Timer.clear(TASK_TIMER);
      TASK_TIMER = null;
    }
    TASK_QUEUE = [];
    TASK_INDEX = 0;
    return;
  }

  // Execute next task; new tasks queued by the task itself extend TASK_QUEUE
  // and will be picked up on subsequent timer ticks.
  var task = TASK_QUEUE[TASK_INDEX];
  TASK_INDEX++;
  task();
}

function queueTask(task) {
  // Simply append to queue
  TASK_QUEUE.push(task);
  
  // Start timer only if not already running
  if (!TASK_TIMER) {
    TASK_TIMER = Timer.set(200, true, processTaskQueue);
  }
}

// === STATE (DYNAMIC RUNTIME VALUES) ===
var STATE = {
  // Device configuration (auto-detected at startup)
  deviceType: null,           // "pro3" or "pro1"
  myDeviceId: null,           // My Shelly device ID from Shelly.getDeviceInfo().id
  outputs: [],                // Array of available output IDs
  inputs: [],                 // Array of available input IDs

  // Component name mappings
  inputNames: {},             // {id: name}
  switchNames: {},            // {id: name}

  // Current state
  activeOutput: -1,           // Current active output (-1 = all off)
  savedOutput: -1,            // Saved output before water-supply protection

  // Cross-device safety (grace delay during switchover)
  graceTimer: null,
  graceActive: false,

  // Tracked pro1 switch:0 state (updated via MQTT status subscription, controller only)
  pro1On: false,

  // Tracked pro3 switch states (updated via MQTT status subscription, bootstrap only)
  // Key: switch id (0,1,2), value: true/false
  pro3SwitchStates: {},
  
  // MQTT connection
  mqttConnected: false,
  
  // Forecast cache (in-memory, refreshed daily) - cleared after use to save memory
  forecastUrl: null,          // Open-Meteo forecast URL
  maxForecastTemp: null,      // Only store max temp, not full array (memory optimization)
  lastForecastFetchDate: null,// Date string (YYYY-M-D) of last fetch
  peakForecastHour: null,      // Hour-of-day (0-23) of max temperature in today's forecast
  sunriseHour: null,           // Fractional hour of today's sunrise (from forecast API)
  sunsetHour: null,            // Fractional hour of today's sunset (from forecast API)

  // Schedule mode
  scheduleMode: null,         // "summer" or "winter"

  // Initialization flag
  initializing: true          // Prevents KVS writes during init
};

// === LOGGING ===
function log() {
  if (!CONFIG.enableLogging) return;
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
  print(SCRIPT_PREFIX, s);
}

// === SCRIPT.STORAGE HELPERS (for forecast URL) ===
function storeStorageValue(key, value) {
  var valueStr;
  if (typeof value === "undefined" || value === null) {
    valueStr = "null";
  } else if (typeof value === "number" || typeof value === "boolean") {
    valueStr = value.toString();
  } else if (typeof value === "object") {
    valueStr = JSON.stringify(value);
  } else {
    valueStr = String(value);
  }
  Script.storage.setItem(key, valueStr);
}

function loadStorageValue(key) {
  var v = Script.storage.getItem(key);
  if (v === null || v === undefined) return null;
  if (v === "null" || v === "undefined") return null;
  if (v === "true") return true;
  if (v === "false") return false;
  try {
    var num = Number(v);
    if (!isNaN(num)) return num;
  } catch (e) {
    // Espruino throws "String too big to convert to float" on long strings
    if (e && false) {}
  }
  try {
    return JSON.parse(v);
  } catch (e) {
    if (e && false) {}
    return v;
  }
}

// === KVS HELPERS ===
function storeValue(key, value) {
  var valueStr;
  if (typeof value === "undefined" || value === null) {
    valueStr = "null";
  } else if (typeof value === "number" || typeof value === "boolean") {
    valueStr = value.toString();
  } else if (typeof value === "object") {
    valueStr = JSON.stringify(value);
  } else {
    valueStr = String(value);
  }
  // Fire-and-forget to avoid callback depth issues
  Shelly.call("KVS.Set", {key: CONFIG_KEY_PREFIX + key, value: valueStr});
}

function loadValue(key) {
  var result = Shelly.call("KVS.Get", {key: CONFIG_KEY_PREFIX + key});
  if (result && ("value" in result)) {
    var v = result.value;
    if (v === "null" || v === "undefined") return null;
    if (v === "true") return true;
    if (v === "false") return false;
    var num = Number(v);
    if (!isNaN(num)) return num;
    try {
      return JSON.parse(v);
    } catch (e) {
      if (e && false) {}
      return v;
    }
  }
  return null;
}

// === WEATHER FORECAST FUNCTIONS (Memory-Optimized) ===
function setForecastURL(lat, lon) {
  log('setForecastURL', lat, lon);
  if (lat !== null && lon !== null) {
    var url = 'https://api.open-meteo.com/v1/forecast?latitude=' + lat + '&longitude=' + lon + '&hourly=temperature_2m&daily=sunrise,sunset&forecast_days=1&timezone=auto';
    STATE.forecastUrl = url;
    storeStorageValue(STORAGE_KEYS.forecastUrl, url);
    log('Forecast URL ready');
  }
}

function shouldRefreshForecast() {
  var now = new Date();
  var today = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();

  if (STATE.lastForecastFetchDate === null || STATE.lastForecastFetchDate !== today) {
    return true;
  }
  return false;
}

function onForecast(result, error_code, error_message, cb) {
  if (error_code !== 0) {
    log('Forecast fetch error code:', error_code, 'message:', error_message);
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  if (!result || !result.body) {
    log('No forecast data in response');
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  var data = null;
  try {
    data = JSON.parse(result.body);
  } catch (e) {
    log('JSON parse error');
    if (e && false) {}
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  // Clear result to free memory immediately
  result = null;

  if (!data || !data.hourly || !data.hourly.temperature_2m || data.hourly.temperature_2m.length === 0) {
    log('Invalid forecast structure');
    data = null;
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  // Extract peak hour and max temp from hourly data; discard array immediately after
  var temps = data.hourly.temperature_2m;
  var maxTemp = null;
  var peakHour = 12;
  for (var i = 0; i < temps.length; i++) {
    var temp = temps[i];
    if (temp !== null && (maxTemp === null || temp > maxTemp)) {
      maxTemp = temp;
      peakHour = i;
    }
  }
  temps = null;
  STATE.maxForecastTemp = maxTemp;
  STATE.peakForecastHour = peakHour;

  // Parse sunrise/sunset from daily section (added to URL for schedule centering)
  if (data.daily && data.daily.sunrise && data.daily.sunrise.length > 0) {
    STATE.sunriseHour = parseHourFromISO(data.daily.sunrise[0]);
  }
  if (data.daily && data.daily.sunset && data.daily.sunset.length > 0) {
    STATE.sunsetHour = parseHourFromISO(data.daily.sunset[0]);
  }
  data = null;

  var now = new Date();
  STATE.lastForecastFetchDate = now.getFullYear() + '-' + (now.getMonth() + 1) + '-' + now.getDate();
  log('Forecast cached, max temp:', maxTemp);

  if (typeof cb === 'function') {
    queueTask(function() { cb(); });
  }
}

function fetchAndCacheForecast(cb) {
  var url = STATE.forecastUrl || loadStorageValue(STORAGE_KEYS.forecastUrl);
  if (!url) {
    log('Forecast URL not configured. Skipping.');
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  log('Fetching forecast...');
  Shelly.call("HTTP.GET", {
    url: url,
    timeout: 10
  }, onForecast, cb);
}

function getMaxForecastTemp() {
  return STATE.maxForecastTemp;
}

function onDeviceLocation(result, error_code, error_message, cb) {
  if (error_code === 0 && result) {
    if (result.lat !== null && result.lon !== null) {
      log('Auto-detected location: lat=' + result.lat + ', lon=' + result.lon);
      setForecastURL(result.lat, result.lon);
      if (typeof cb === 'function') queueTask(function() { cb(); });
    } else {
      log('Location detection returned null coordinates');
      if (typeof cb === 'function') queueTask(function() { cb(); });
    }
  } else {
    log('Location detection error:', error_code, error_message);
    if (typeof cb === 'function') queueTask(function() { cb(); });
  }
}

function ensureForecastUrl(cb) {
  if (STATE.forecastUrl) {
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  var storedUrl = loadStorageValue(STORAGE_KEYS.forecastUrl);
  if (storedUrl && storedUrl.indexOf('daily=') !== -1) {
    STATE.forecastUrl = storedUrl;
    log('Loaded forecast URL from storage');
    if (typeof cb === 'function') queueTask(function() { cb(); });
    return;
  }

  log('Forecast URL not found, detecting location...');
  Shelly.call('Shelly.DetectLocation', {}, onDeviceLocation, cb);
}

// === INTER-DEVICE SAFETY (Grace Delay Guards) ===
// Uses STATE.graceTimer — a single one-shot timer that occupies one timer slot only while
// a transition is in progress (never running during steady-state operation).

function startGraceDelay(delayMs, callback) {
  if (STATE.graceActive) {
    log("Grace delay already active, queueing continuation");
    queueTask(function() { startGraceDelay(delayMs, callback); });
    return;
  }
  STATE.graceActive = true;
  log("Grace delay started:", delayMs, "ms");
  STATE.graceTimer = Timer.set(delayMs, false, function() {
    STATE.graceTimer = null;
    STATE.graceActive = false;
    log("Grace delay complete");
    if (callback) callback();
  });
}

// Called when pro3 (controller) wants to activate one of its own outputs (pump variator).
// If pro1 is currently on, turn it off first and wait graceDelayMs.
function safeActivatePro3(targetOutputId, callback) {
  if (STATE.deviceType !== "pro3") {
    if (callback) callback();
    return;
  }

  if (targetOutputId === -1) {
    // Turning everything off — no guard needed
    if (callback) callback();
    return;
  }

  // Use MQTT-tracked pro1 state (kept current by subscribePro1Status).
  if (!STATE.pro1On) {
    if (callback) callback();
    return;
  }

  log("pro1 is on — turning off before activating pro3 (grace:", CONFIG.graceDelayMs, "ms)");
  turnOffPro1(function(err) {
    if (err) {
      log("WARNING: turnOffPro1 failed during safeActivatePro3:", err);
    }
    startGraceDelay(CONFIG.graceDelayMs, callback);
  });
}

// === COMPONENT NAMING ===
function configureComponentNames() {
  log("Configuring component names...");
  
  var names = COMPONENT_NAMES[STATE.deviceType];
  if (!names) {
    log("ERROR: No component names defined for device type:", STATE.deviceType);
    return;
  }
  
  // Build name mappings (synchronous)
  for (var i = 0; i < names.inputs.length; i++) {
    var input = names.inputs[i];
    if (STATE.inputs.indexOf(input.id) !== -1) {
      STATE.inputNames[input.id] = input.name;
    }
  }

  // Switch names derived from CONFIG speed mapping
  var switchNames = buildSwitchNames();
  for (var i = 0; i < switchNames.length; i++) {
    var sw = switchNames[i];
    if (STATE.outputs.indexOf(sw.id) !== -1) {
      STATE.switchNames[sw.id] = sw.name;
    }
  }
}

function applyComponentNames(callback) {
  log("Applying component names to device...");
  
  var names = COMPONENT_NAMES[STATE.deviceType];
  if (!names) {
    if (callback) callback();
    return;
  }
  
  // Build list of components to configure
  var componentsToConfig = [];
  
  for (var i = 0; i < names.inputs.length; i++) {
    var input = names.inputs[i];
    if (STATE.inputs.indexOf(input.id) !== -1) {
      componentsToConfig.push({
        type: "input", 
        id: input.id, 
        name: input.name,
        invert: input.invert
      });
    }
  }
  
  // Switch names derived from CONFIG speed mapping
  var switchNames = buildSwitchNames();
  for (var i = 0; i < switchNames.length; i++) {
    var sw = switchNames[i];
    if (STATE.outputs.indexOf(sw.id) !== -1) {
      componentsToConfig.push({type: "switch", id: sw.id, name: sw.name});
    }
  }
  
  if (componentsToConfig.length === 0) {
    log("No components to configure");
    if (callback) callback();
    return;
  }
  
  // Process components sequentially using task queue
  var index = 0;
  
  function processNext() {
    if (index >= componentsToConfig.length) {
      log("All component names applied");
      // Free static data — only needed during component setup
      COMPONENT_NAMES = null;
      if (callback) callback();
      return;
    }
    
    var comp = componentsToConfig[index];
    index++;
    
    if (comp.type === "input") {
      var config = {name: comp.name};
      if (typeof comp.invert === "boolean") {
        config.invert = comp.invert;
      }
      Shelly.call("Input.SetConfig", {id: comp.id, config: config}, function(res, err) {
        if (err && false) {}
        log("Applied input:" + comp.id + " config:", JSON.stringify(config));
        queueTask(processNext);
      });
    } else if (comp.type === "switch") {
      Shelly.call("Switch.SetConfig", {id: comp.id, config: {name: comp.name, in_mode: "detached"}}, function(res, err) {
        if (err && false) {}
        log("Applied switch:" + comp.id + " name:", comp.name, "in_mode: detached");
        queueTask(processNext);
      });
    }
  }
  
  processNext();
}

// === STATE PERSISTENCE ===
function loadState() {
  log("Loading persisted state...");

  // activeOutput: KVS fire-and-forget write; read is skipped here because
  // enforceOutputState() reads the actual hardware switch state right after
  // this call — hardware truth overrides any stale KVS value.

  // scheduleMode: use Script.storage (synchronous getItem/setItem) so that
  // the correct mode survives a reboot without needing an async callback chain.
  var savedMode = loadStorageValue(STORAGE_KEYS.scheduleMode);
  if (savedMode !== null && (savedMode === "summer" || savedMode === "winter")) {
    STATE.scheduleMode = savedMode;
    log("Restored schedule mode:", STATE.scheduleMode);
  } else {
    STATE.scheduleMode = "winter";
    log("No saved schedule mode, defaulting to winter");
  }
}

function saveState() {
  // Skip writes during initialization to avoid callback depth issues
  if (STATE.initializing) {
    return;
  }

  // activeOutput → KVS (fire-and-forget, read by CLI status command)
  queueTask(function() {
    storeValue(STATE_KEYS.activeOutput, STATE.activeOutput);
  });

  // scheduleMode → Script.storage (synchronous read in loadState on next boot)
  //              → KVS as well (CLI status command reads it there)
  if (STATE.scheduleMode !== null) {
    storeStorageValue(STORAGE_KEYS.scheduleMode, STATE.scheduleMode);
    queueTask(function() {
      storeValue("schedule-mode", STATE.scheduleMode);
    });
  }
}

// === DEVICE ACTIVATION DECISION ===
function isMyTurnToRun() {
  // Compare preferred device ID against my device ID
  var preferredId = CONFIG.preferredDeviceId;
  var myId = STATE.myDeviceId;

  if (!preferredId || !myId) {
    log("ERROR: Cannot determine activation - missing device ID");
    return false;
  }

  var shouldRun = preferredId === myId;
  log("Activation check: preferred=" + preferredId + ", me=" + myId + ", shouldRun=" + shouldRun);
  return shouldRun;
}

function mapSpeedToSwitch(speed) {
  // Map semantic speed to physical switch ID based on device type
  // speed: 'eco', 'mid', 'high', 'max'
  // Returns switch ID or -1 for off

  if (!speed || speed === 'off') {
    return -1;
  }

  if (STATE.deviceType === 'pro3') {
    // Pro3: 3 switches
    if (speed === 'eco') return CONFIG.ecoSpeed;
    if (speed === 'mid') return CONFIG.midSpeed;
    if (speed === 'high' || speed === 'max') return CONFIG.highSpeed;
  } else if (STATE.deviceType === 'pro1') {
    // Pro1: only 1 switch, all speeds map to switch:0
    return 0;
  }

  log("WARNING: Unknown speed or device type, defaulting to off");
  return -1;
}

// === OUTPUT CONTROL ===
function turnOffAllSwitches(callback) {
  for (var i = 0; i < STATE.outputs.length; i++) {
    Shelly.call("Switch.Set", {id: STATE.outputs[i], on: false}, function(res, err) {
      if (err && false) {}
    });
  }
  if (callback) queueTask(callback);
}

function turnOffPro1(callback) {
  if (!CONFIG.pro1DeviceId) {
    log("WARNING: pro1 device ID not configured");
    if (callback) callback("no pro1 device ID");
    return;
  }
  log("Sending turn-off command to pro1");
  MQTT.publish(CONFIG.pro1DeviceId + "/command/switch:0", "off", 0, false);
  if (callback) queueTask(function() { callback(null); });
}

function setOutput(outputId, on, callback) {
  if (STATE.outputs.indexOf(outputId) === -1) {
    log("ERROR: Invalid output ID:", outputId);
    if (callback) callback();
    return;
  }
  
  log("Setting switch", outputId, "to", on);
  Shelly.call("Switch.Set", {id: outputId, on: on}, callback);
}

// === SOFTWARE FUSE (ANTI-CYCLING PROTECTION) ===
// Prevents rapid relay cycling that generates repeated motor inrush currents
// and trips circuit breakers. Tracks output state changes in a sliding window;
// if too many transitions occur, the fuse "trips": all switches are turned off
// and ON activations are refused for a cooldown period.
// OFF activations (-1) always pass — safety trumps the fuse.
var FUSE_WINDOW_MS = 120000;      // 2-minute sliding window
var FUSE_MAX_CHANGES = 4;         // max state changes per window
var FUSE_COOLDOWN_MS = 300000;    // 5-minute cooldown after trip
var FUSE_CHANGES = [];            // timestamps of recent state changes
var FUSE_TRIPPED = false;
var FUSE_TRIP_TIME = 0;

function fuseRecord() {
  FUSE_CHANGES.push(Date.now());
}

function fuseAllowOn() {
  var now = Date.now();

  // If tripped, check cooldown
  if (FUSE_TRIPPED) {
    if (now - FUSE_TRIP_TIME >= FUSE_COOLDOWN_MS) {
      log("FUSE: cooldown expired, resetting");
      FUSE_TRIPPED = false;
      FUSE_CHANGES = [];
      return true;
    }
    log("FUSE: tripped, refusing activation (cooldown remaining:",
        Math.round((FUSE_COOLDOWN_MS - (now - FUSE_TRIP_TIME)) / 1000), "s)");
    return false;
  }

  // Prune entries outside the window (no shift — manual loop per Shelly constraint)
  var recent = [];
  for (var i = 0; i < FUSE_CHANGES.length; i++) {
    if (now - FUSE_CHANGES[i] < FUSE_WINDOW_MS) {
      recent.push(FUSE_CHANGES[i]);
    }
  }
  FUSE_CHANGES = recent;

  // Check threshold
  if (FUSE_CHANGES.length >= FUSE_MAX_CHANGES) {
    log("FUSE: TRIPPED — " + FUSE_CHANGES.length + " state changes in " +
        (FUSE_WINDOW_MS / 1000) + "s window. Blocking ON activations for " +
        (FUSE_COOLDOWN_MS / 1000) + "s");
    FUSE_TRIPPED = true;
    FUSE_TRIP_TIME = now;
    turnOffAllSwitches();
    Shelly.emitEvent("pool.fuse_tripped", {
      changes: FUSE_CHANGES.length,
      window_s: FUSE_WINDOW_MS / 1000,
      cooldown_s: FUSE_COOLDOWN_MS / 1000
    });
    return false;
  }

  return true;
}

function activateOutput(outputId, callback) {
  log("Activating output:", outputId);

  // Software fuse: always allow OFF (-1), check fuse for ON activations
  if (outputId !== -1 && !fuseAllowOn()) {
    log("FUSE: activation refused, forcing off");
    outputId = -1;
  }

  // Record actual state changes for fuse tracking
  if (outputId !== STATE.activeOutput) {
    fuseRecord();
  }

  if (STATE.deviceType === "pro3") {
    safeActivatePro3(outputId, function() {
      // Turn off all outputs simultaneously
      for (var i = 0; i < STATE.outputs.length; i++) {
        Shelly.call("Switch.Set", {id: STATE.outputs[i], on: false}, function(res, err) {
          if (err && false) {}
        });
      }

      // Use task queue instead of a one-shot timer to avoid consuming a timer slot
      queueTask(function() {
        if (outputId !== -1) {
          setOutput(outputId, true, function() {
            STATE.activeOutput = outputId;

            saveState();
            if (callback) callback();
          });
        } else {
          STATE.activeOutput = -1;
          saveState();
          if (callback) callback();
        }
      });
    });
  } else {
    // Pro1: guard against activating while pro3 is on
    var on = outputId === 0;
    if (!on) {
      // Turning off — no guard needed
      setOutput(0, false, function() {
        STATE.activeOutput = -1;
        saveState();
        if (callback) callback();
      });
      return;
    }

    // For Pro1 turning on: check if Pro3 is active, wait grace delay
    // (This is handled by the cross-device protection logic)
    setOutput(0, true, function() {
      STATE.activeOutput = 0;
      saveState();
      if (callback) callback();
    });
  }
}

// === BUTTON HANDLING ===
function cycleOutputs() {
  log("Button pressed, cycling outputs");

  // Check if water supply is active
  var input0 = Shelly.getComponentStatus("input:0");
  if (input0 && input0.state) {
    log("Water supply protection active, ignoring button press");
    return;
  }

  if (STATE.deviceType === "pro3") {
    // Cycle: all off → 0 → 1 → 2 → all off
    var nextOutput;
    if (STATE.activeOutput === -1) {
      nextOutput = 0;
    } else if (STATE.activeOutput === 0) {
      nextOutput = 1;
    } else if (STATE.activeOutput === 1) {
      nextOutput = 2;
    } else {
      nextOutput = -1;
    }

    log("Cycling from", STATE.activeOutput, "to", nextOutput);

    if (nextOutput === -1) {
      // Target is OFF: turn off all speeds
      log("Power off: turning off all speeds");
      turnOffAllSwitches(function() {
        STATE.activeOutput = -1;
        saveState();
      });
    } else if (STATE.activeOutput === -1) {
      // From OFF to speed: just turn on target speed
      log("Power on: starting speed", nextOutput);
      setOutput(nextOutput, true, function() {
        STATE.activeOutput = nextOutput;
        saveState();
      });
    } else {
      // Speed-to-speed: make-before-break (turn ON new, then OFF old)
      var prevOutput = STATE.activeOutput;
      log("Speed change: ON speed", nextOutput, "then OFF speed", prevOutput);
      setOutput(nextOutput, true, function() {
        // New speed is now on, turn off the old speed
        setOutput(prevOutput, false, function() {
          STATE.activeOutput = nextOutput;
          saveState();
        });
      });
    }
  } else {
    // Pro1: simple toggle
    var nextOutput = STATE.activeOutput === -1 ? 0 : -1;
    log("Toggling from", STATE.activeOutput, "to", nextOutput);
    if (nextOutput === -1) {
      // Turning off
      turnOffAllSwitches(function() {
        STATE.activeOutput = -1;
        saveState();
      });
    } else {
      // Turning on
      setOutput(0, true, function() {
        STATE.activeOutput = 0;
        saveState();
      });
    }
  }
}

// === WATER SUPPLY PROTECTION ===
var WATER_SUPPLY_ACTIVE = false;  // debounce guard

function handleWaterSupply(waterSupplyActive) {
  log("Water supply active signal:", waterSupplyActive);

  // Debounce: ignore duplicate events for the same state
  if (waterSupplyActive === WATER_SUPPLY_ACTIVE) {
    log("Water supply state unchanged, ignoring duplicate");
    return;
  }
  WATER_SUPPLY_ACTIVE = waterSupplyActive;

  if (waterSupplyActive) {
    // Water supply is ON (signal is HIGH after invert) - save current state and turn off all pumps
    STATE.savedOutput = STATE.activeOutput;
    log("Water supply ON - saving current output:", STATE.savedOutput);

    Shelly.emitEvent("pool.water_supply_protected", {saved_output: STATE.savedOutput});
    activateOutput(-1, function() {
      log("All pumps turned off for water supply protection");
    });
  } else {
    // Water supply is OFF (signal is LOW after invert) - restore previous state
    log("Water supply OFF - restoring output:", STATE.savedOutput);

    Shelly.emitEvent("pool.water_supply_restored", {restored_output: STATE.savedOutput});
    activateOutput(STATE.savedOutput, function() {
      log("Pump restored after water supply turned off");
    });
  }
}

// === EVENT HANDLERS ===
function handleSwitchEvent(info) {
  log("Switch event:", info);

  // Ignore switch events during water supply protection
  var input0 = Shelly.getComponentStatus("input:0");
  if (input0 && input0.state && info.state === true) {
    log("Water supply protection active, turning off switch", info.id);
    setOutput(info.id, false);
    return;
  }

  if (STATE.deviceType === "pro3" && info.state === true) {
    // Ensure only one output is on
    var activatedOutput = info.id;
    for (var i = 0; i < STATE.outputs.length; i++) {
      var outputId = STATE.outputs[i];
      if (outputId !== activatedOutput) {
        setOutput(outputId, false);
      }
    }
    STATE.activeOutput = activatedOutput;
    saveState();

    // Inter-device safety: if pro1 is on (any source, including manual), turn it off.
    // The grace delay is NOT applied here because pro3 turning on means variator is
    // now active — pro1 must be off immediately; pro3 is already on.
    // Note: cannot pre-intercept; this is the earliest reactive point.
    if (STATE.pro1On) {
      log("pro3 switch turned on but pro1 is on — sending turn-off to pro1");
      turnOffPro1(function(err) {
        if (err) {
          log("WARNING: failed to turn off pro1 after pro3 switch on:", err);
        }
      });
    }

  } else if (STATE.deviceType === "pro1" && info.state === true) {
    // Inter-device safety: if any pro3 switch is on, immediately turn ourselves off
    // and queue a re-activation after the grace delay.
    var anyPro3On = false;
    for (var k in STATE.pro3SwitchStates) {
      if (STATE.pro3SwitchStates[k]) {
        anyPro3On = true;
        break;
      }
    }
    if (anyPro3On) {
      log("pro1 switch turned on but pro3 is on — turning off immediately, will retry after grace delay");
      setOutput(0, false, function() {
        STATE.activeOutput = -1;
        saveState();
        startGraceDelay(CONFIG.graceDelayMs, function() {
          log("Grace delay complete — re-activating pro1");
          setOutput(0, true, function() {
            STATE.activeOutput = 0;

            saveState();
          });
        });
      });
      return;
    }

    STATE.activeOutput = 0;
    saveState();

  } else if (STATE.deviceType === "pro1" && info.state === false) {
    STATE.activeOutput = -1;
    saveState();
  } else if (STATE.deviceType === "pro3" && info.state === false) {
    // Track when all pro3 outputs are off
    var anyStillOn = false;
    for (var j = 0; j < STATE.outputs.length; j++) {
      var st = Shelly.getComponentStatus("switch:" + STATE.outputs[j]);
      if (st && st.output) {
        anyStillOn = true;
        break;
      }
    }
    if (!anyStillOn) {
      STATE.activeOutput = -1;
      saveState();
    }
  }
}

function handleInputEvent(info) {
  log("Input event:", info);
  
  // Handle input:0 (water-supply)
  if (info.id === 0) {
    handleWaterSupply(info.state);
  }
  // Input:1 (high-water) and input:2 (max-speed-active) are just notifications
}

function handleButtonEvent(info) {
  log("Button event:", info);
  
  // System button events (component="sys"):
  // - sys_btn_down: Button pressed down
  // - sys_btn_up: Button released
  // - sys_btn_push: Complete brief push (down + up)
  // - brief_btn_down: Legacy event (deprecated, use sys_btn_push instead)
  
  if (info.component === "sys" && info.event === "sys_btn_push") {
    cycleOutputs();
  }
}

// === MQTT SETUP ===

// Parse a Shelly switch status payload and return the output boolean (or null on error)
function parseSwitchStatus(message) {
  var data = null;
  try {
    data = JSON.parse(message);
  } catch (e) {
    if (e && false) {}
    return null;
  }
  if (!data || !("output" in data)) return null;
  return data.output;
}

// --- Controller (pro3): track pro1 switch:0 ---
function onPro1StatusMessage(topic, message) {
  var on = parseSwitchStatus(message);
  if (on === null) return;
  if (STATE.pro1On !== on) {
    log("pro1 switch:0 state updated via MQTT:", on);
    STATE.pro1On = on;
  }
}

function subscribePro1Status() {
  // Subscribe to Pro1 status for cross-device protection
  if (!CONFIG.pro1DeviceId) return;
  var topic = CONFIG.pro1DeviceId + "/status/switch:0";
  MQTT.subscribe(topic, onPro1StatusMessage);
  log("Subscribed to pro1 status topic:", topic);
  // Request current state immediately — status topics are NOT retained
  MQTT.publish(CONFIG.pro1DeviceId + "/command/switch:0", "status_update", 0, false);
  log("Requested pro1 switch:0 status_update");
}

// --- Bootstrap (pro1): track pro3 switch:0, switch:1, switch:2 ---
function onPro3StatusMessage(topic, message) {
  var on = parseSwitchStatus(message);
  if (on === null) return;
  // Extract switch id from topic suffix "…/status/switch:<id>"
  var id = -1;
  var parts = topic.split(":");
  if (parts.length >= 2) {
    var n = Number(parts[parts.length - 1]);
    if (!isNaN(n)) id = n;
  }
  if (id < 0) return;
  var prev = (id in STATE.pro3SwitchStates) ? STATE.pro3SwitchStates[id] : null;
  if (prev !== on) {
    log("pro3 switch:" + id + " state updated via MQTT:", on);
    STATE.pro3SwitchStates[id] = on;
  }
}

function subscribePro3Status() {
  // Subscribe to Pro3 status for cross-device protection
  if (!CONFIG.pro3DeviceId) return;
  for (var i = 0; i < 3; i++) {
    var topic = CONFIG.pro3DeviceId + "/status/switch:" + i;
    MQTT.subscribe(topic, onPro3StatusMessage);
    log("Subscribed to pro3 status topic:", topic);
    // Request current state immediately — status topics are NOT retained
    MQTT.publish(CONFIG.pro3DeviceId + "/command/switch:" + i, "status_update", 0, false);
  }
  log("Requested pro3 switch status_update for all 3 channels");
}

function setupMQTT() {
  var mqttStatus = Shelly.getComponentStatus("mqtt");
  if (mqttStatus && mqttStatus.connected) {
    STATE.mqttConnected = true;
    log("MQTT connected");
  } else {
    log("MQTT not connected");
  }
  subscribePro1Status();
  subscribePro3Status();
}

// === SCHEDULE MANAGEMENT ===
function clearNonUpdateSchedules(callback) {
  log('Clearing existing schedules...');
  
  Shelly.call('Schedule.List', {}, function(result, err) {
    if (err) {
      log('ERROR: Failed to list schedules:', err);
      if (err && false) {}
      if (callback) callback();
      return;
    }
    
    if (!result || !result.jobs) {
      log('No schedules found');
      if (callback) callback();
      return;
    }
    
    var jobsToDelete = [];
    for (var i = 0; i < result.jobs.length; i++) {
      var job = result.jobs[i];
      // Keep only device auto-update schedules
      if (job.calls && job.calls.length > 0) {
        var isUpdate = false;
        for (var j = 0; j < job.calls.length; j++) {
          if (job.calls[j].method === 'Shelly.Update') {
            isUpdate = true;
            break;
          }
        }
        if (!isUpdate) {
          jobsToDelete.push(job.id);
        }
      }
    }
    
    if (jobsToDelete.length === 0) {
      log('No schedules to delete');
      if (callback) callback();
      return;
    }
    
    log('Deleting', jobsToDelete.length, 'schedules');
    var deleteIndex = 0;
    
    function deleteNext() {
      if (deleteIndex >= jobsToDelete.length) {
        log('All schedules deleted');
        if (callback) callback();
        return;
      }
      
      var jobId = jobsToDelete[deleteIndex];
      deleteIndex++;
      
      Shelly.call('Schedule.Delete', {id: jobId}, function(res, err) {
        if (err && false) {}
        log('Deleted schedule:', jobId);
        queueTask(deleteNext);
      });
    }
    
    deleteNext();
  });
}

function createSchedules(callback) {
  log('Creating pool pump schedules...');

  var scriptId = Shelly.getCurrentScriptId();

  var schedules = [
    {
      enable: true,
      timespec: '@sunrise * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleDailyCheck()'
        }
      }]
    },
    {
      enable: true,
      timespec: '@sunrise+3h * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleMorningStart()'
        }
      }]
    },
    {
      enable: true,
      timespec: '@sunset * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleEveningStop()'
        }
      }]
    },
    {
      enable: true,
      timespec: '0 15 23 * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleNightStart()'
        }
      }]
    },
    {
      enable: true,
      timespec: '0 15 0 * * SUN,MON,TUE,WED,THU,FRI,SAT',
      calls: [{
        method: 'script.eval',
        params: {
          id: scriptId,
          code: 'handleNightStop()'
        }
      }]
    }
  ];
  
  var scheduleIndex = 0;
  
  function createNext() {
    if (scheduleIndex >= schedules.length) {
      log('All schedules created');
      if (callback) callback();
      return;
    }
    
    var schedule = schedules[scheduleIndex];
    scheduleIndex++;
    
    Shelly.call('Schedule.Create', schedule, function(res, err) {
      if (err) {
        log('ERROR: Failed to create schedule:', err);
        if (err && false) {}
      } else {
        log('Created schedule:', schedule.timespec);
      }
      queueTask(createNext);
    });
  }
  
  createNext();
}

// Verify schedules exist (lightweight check, logs warning if missing)
function verifySchedules(cb) {
  Shelly.call('Schedule.List', {}, function(result, err) {
    if (err) {
      log('WARNING: Cannot verify schedules:', err);
      if (typeof cb === 'function') queueTask(function() { cb(); });
      return;
    }

    var hasPoolSchedules = false;
    if (result && result.jobs) {
      for (var i = 0; i < result.jobs.length; i++) {
        var job = result.jobs[i];
        if (job.calls && job.calls.length > 0 && job.calls[0].method === 'script.eval') {
          var code = job.calls[0].params && job.calls[0].params.code;
          if (code && (code.indexOf('handleNight') === 0 || code.indexOf('handleDaily') === 0 || code.indexOf('handleMorning') === 0 || code.indexOf('handleEvening') === 0)) {
            hasPoolSchedules = true;
            break;
          }
        }
      }
    }

    if (!hasPoolSchedules) {
      log('FATAL: No pool pump schedules found. Run: ctl pool setup');
      // Stop script - schedules are required for operation
      return;
    }

    log('Pool pump schedules verified');
    if (typeof cb === 'function') queueTask(function() { cb(); });
  });
}

// === SCHEDULE MODE MANAGEMENT ===

// Parse fractional hour from Open-Meteo ISO timestamp ("2025-07-15T06:15")
function parseHourFromISO(isoStr) {
  var tIdx = isoStr.indexOf('T');
  if (tIdx < 0) return null;
  var timePart = isoStr.slice(tIdx + 1);
  var colonIdx = timePart.indexOf(':');
  if (colonIdx < 0) return Number(timePart);
  var h = Number(timePart.slice(0, colonIdx));
  var m = Number(timePart.slice(colonIdx + 1, colonIdx + 3));
  if (isNaN(h)) return null;
  if (isNaN(m)) m = 0;
  return h + m / 60;
}

function lpad2(n) {
  return n < 10 ? '0' + n : String(n);
}

// Flow rate (m3/h) at the currently configured preferred speed
function computeFlowRate() {
  var speedRpms = {eco: CONFIG.ecoRpm, mid: CONFIG.midRpm, high: CONFIG.highRpm, max: CONFIG.highRpm};
  var rpm = speedRpms[CONFIG.preferredSpeed];
  if (!rpm) rpm = CONFIG.highRpm;
  return CONFIG.maxFlowRate * (rpm / CONFIG.maxRpm);
}

// Daily run hours proportional to today's max temperature
function computeRunHours(maxForecastTemp) {
  var flowRate = computeFlowRate();
  if (!flowRate || flowRate <= 0) {
    log('WARNING: invalid flow rate, defaulting run hours to 8');
    return 8;
  }
  var baseHours = (CONFIG.poolVolume * CONFIG.turnover) / flowRate;
  var minHours  = baseHours * 0.5;
  var maxHours  = baseHours * 1.5;
  var range = CONFIG.maxTemp - CONFIG.temperatureThreshold;
  var scale = range > 0 ? (maxForecastTemp - CONFIG.temperatureThreshold) / range : 1;
  if (scale < 0) scale = 0;
  if (scale > 1) scale = 1;
  var runHours = baseHours * scale;
  if (runHours < minHours) runHours = minHours;
  if (runHours > maxHours) runHours = maxHours;
  return runHours;
}

// Build a Shelly cron timespec string from a fractional hour (e.g. 9.625 → "0 37 9 * * SUN,...")
function makeTimespec(fractHours) {
  if (fractHours < 0) fractHours = 0;
  if (fractHours >= 24) fractHours = 23.99;
  var h = Math.floor(fractHours);
  var m = Math.round((fractHours - h) * 60);
  if (m >= 60) { h++; m = 0; }
  h = h % 24;
  return '0 ' + m + ' ' + h + ' * * SUN,MON,TUE,WED,THU,FRI,SAT';
}

function updateScheduleMode(newMode, morningStartHours, eveningStopHours) {
  var hasTimings = morningStartHours !== null && morningStartHours !== undefined;
  var modeChanged = STATE.scheduleMode !== newMode;

  if (!modeChanged && !hasTimings) {
    log('Mode already:', newMode, '- no changes needed');
    return;
  }

  if (modeChanged) {
    log('MODE CHANGE:', STATE.scheduleMode || 'unknown', '->', newMode);
    log(newMode === 'summer' ? '  Summer: enabling morning/evening schedules' : '  Winter: enabling night schedules only');
    STATE.scheduleMode = newMode;
    saveState();
  } else {
    log('Updating', newMode, 'schedule times');
  }

  // Enable/disable schedules based on mode; include new timespec for summer timings
  Shelly.call('Schedule.List', {}, function(result, err) {
    if (err) {
      log('ERROR: Failed to list schedules:', err);
      if (err && false) {}
      return;
    }

    if (!result || !result.jobs) {
      log('No schedules found');
      return;
    }

    var schedulesToUpdate = [];
    for (var i = 0; i < result.jobs.length; i++) {
      var job = result.jobs[i];
      if (job.calls && job.calls.length > 0) {
        var code = job.calls[0].params && job.calls[0].params.code;
        if (code === 'handleMorningStart()') {
          var updM = {id: job.id, enable: newMode === 'summer', name: code};
          if (hasTimings && newMode === 'summer') {
            updM.timespec = makeTimespec(morningStartHours);
          }
          schedulesToUpdate.push(updM);
        } else if (code === 'handleEveningStop()') {
          var updE = {id: job.id, enable: newMode === 'summer', name: code};
          if (hasTimings && newMode === 'summer') {
            updE.timespec = makeTimespec(eveningStopHours);
          }
          schedulesToUpdate.push(updE);
        } else if (code === 'handleNightStart()' || code === 'handleNightStop()') {
          schedulesToUpdate.push({id: job.id, enable: newMode === 'winter', name: code});
        }
      }
    }

    if (schedulesToUpdate.length === 0) {
      log('No schedules to update');
      return;
    }

    var updateIndex = 0;
    function updateNext() {
      if (updateIndex >= schedulesToUpdate.length) {
        log('All schedules updated for', newMode, 'mode');
        return;
      }
      var sched = schedulesToUpdate[updateIndex];
      updateIndex++;
      var params = {id: sched.id, enable: sched.enable};
      if (sched.timespec) {
        params.timespec = sched.timespec;
      }
      Shelly.call('Schedule.Update', params, function(res, err) {
        if (err && false) {}
        var msg = sched.name + ' ' + (sched.enable ? 'ENABLED' : 'DISABLED');
        if (sched.timespec) msg = msg + ' (' + sched.timespec + ')';
        log('Schedule', msg);
        queueTask(updateNext);
      });
    }

    updateNext();
  });
}

function performDailyModeCheck() {
  log('Performing daily mode check...');

  // Ensure forecast URL is configured
  ensureForecastUrl(function() {
    // Fetch or use cached forecast
    if (shouldRefreshForecast()) {
      log('Fetching fresh forecast for mode check...');
      fetchAndCacheForecast(function() {
        decideModeFromForecast();
      });
    } else {
      log('Using cached forecast for mode check');
      decideModeFromForecast();
    }
  });
}

function decideModeFromForecast() {
  var maxTemp = getMaxForecastTemp();

  if (maxTemp === null) {
    log('No forecast data available, keeping mode:', STATE.scheduleMode || 'winter');
    return;
  }

  log('Forecast max temp:', maxTemp + '°C', '(threshold:', CONFIG.temperatureThreshold + '°C)');
  var newMode = maxTemp > CONFIG.temperatureThreshold ? 'summer' : 'winter';
  log('Selected mode:', newMode, maxTemp > CONFIG.temperatureThreshold ? '(above threshold)' : '(below threshold)');

  if (newMode !== 'summer') {
    Shelly.emitEvent("pool.run_window", {mode: "winter", max_temp_c: maxTemp});
    updateScheduleMode(newMode, null, null);
    return;
  }

  var runHours   = computeRunHours(maxTemp);
  var peakHour   = STATE.peakForecastHour !== null ? STATE.peakForecastHour : 14;
  var startFloor = (STATE.sunriseHour !== null ? STATE.sunriseHour : 6) + 1;
  var stopCeil   = (STATE.sunsetHour  !== null ? STATE.sunsetHour  : 21) - 0.5;

  var startHour = peakHour - runHours / 2;
  var stopHour  = peakHour + runHours / 2;

  // Shift window forward if start is too early
  if (startHour < startFloor) {
    startHour = startFloor;
    stopHour  = startFloor + runHours;
  }
  // Shift window backward if stop is too late
  if (stopHour > stopCeil) {
    stopHour  = stopCeil;
    startHour = stopCeil - runHours;
  }
  // Hard floor after both shifts
  if (startHour < startFloor) startHour = startFloor;

  log('Run hours:', Math.round(runHours * 10) / 10,
      'Start:', Math.floor(startHour) + ':' + lpad2(Math.round((startHour % 1) * 60)),
      'Stop:',  Math.floor(stopHour)  + ':' + lpad2(Math.round((stopHour  % 1) * 60)));

  Shelly.emitEvent("pool.run_window", {
    mode: "summer",
    max_temp_c: maxTemp,
    run_hours: Math.round(runHours * 10) / 10,
    start_h: Math.round(startHour * 100) / 100,
    stop_h:  Math.round(stopHour  * 100) / 100
  });

  updateScheduleMode(newMode, startHour, stopHour);
}

// === UNIFIED START/STOP FUNCTIONS ===
// Both devices run same script. Each checks if it should activate based on preferred_device_id.
function doStart(speed, reason) {
  log(reason || 'Start pump');

  // Check if this device should run
  if (!isMyTurnToRun()) {
    log('Not my turn to run (preferred device: ' + CONFIG.preferredDeviceId + ', me: ' + STATE.myDeviceId + ')');
    // Ensure I'm off if I'm not the preferred device
    if (STATE.activeOutput !== -1) {
      log('Turning off as I am not the preferred device');
      activateOutput(-1);
    }
    return;
  }

  var input0 = Shelly.getComponentStatus('input:0');
  if (input0 && input0.state) {
    log('Water supply protection active, ignoring start request');
    return;
  }

  // Map speed to physical switch
  var switchId = mapSpeedToSwitch(speed);
  if (switchId === -1) {
    log('Invalid speed or off requested, turning off');
    activateOutput(-1);
    return;
  }

  log('Starting pump at speed:', speed, '-> switch:', switchId);
  Shelly.emitEvent("pool.pump_start", {speed: speed, switch_id: switchId, reason: reason || "start"});
  activateOutput(switchId);
}

function doStop(reason) {
  log(reason || 'Stop pump');

  // Only stop if I'm currently the one running
  if (!isMyTurnToRun() && STATE.activeOutput === -1) {
    log('Not running and not preferred device, nothing to do');
    return;
  }

  var input0 = Shelly.getComponentStatus('input:0');
  if (input0 && input0.state) {
    log('Water supply protection active, still turning off');
    // Continue to turn off even with water supply active
  }

  Shelly.emitEvent("pool.pump_stop", {reason: reason || "stop"});
  activateOutput(-1, function() {
    log('Pump stopped');
  });
}

// === SCHEDULE EVENT HANDLERS ===
// Schedules only execute on the preferred device (determined by isMyTurnToRun check in doStart/doStop)
function handleDailyCheck() {
  log('Daily check event');

  var input0 = Shelly.getComponentStatus('input:0');
  if (input0 && input0.state) {
    log('Water supply protection active, ignoring event');
    return;
  }

  // Summer/winter mode check only runs on preferred device
  if (isMyTurnToRun()) {
    performDailyModeCheck();
  }
}

function handleMorningStart() {
  // Morning start uses preferred_speed from KVS
  doStart(CONFIG.preferredSpeed, 'Morning start event');
}

function handleEveningStop() {
  doStop('Evening stop event');
}

function handleNightStart() {
  // Night start uses preferred_speed from KVS
  doStart(CONFIG.preferredSpeed, 'Night start event');
}

function handleNightStop() {
  doStop('Night stop event');
}

// === INITIALIZATION ===
function enforceOutputState() {
  log("Enforcing output state at startup...");
  
  if (STATE.deviceType === "pro3") {
    // Ensure only one output is on
    var onOutputs = [];
    for (var i = 0; i < STATE.outputs.length; i++) {
      var outputId = STATE.outputs[i];
      var status = Shelly.getComponentStatus("switch:" + outputId);
      if (status && status.output) {
        onOutputs.push(outputId);
      }
    }
    
    if (onOutputs.length > 1) {
      log("Multiple outputs on, keeping first:", onOutputs[0]);
      activateOutput(onOutputs[0]);
    } else if (onOutputs.length === 1) {
      STATE.activeOutput = onOutputs[0];
      saveState();
    } else {
      STATE.activeOutput = -1;
      saveState();
    }
  } else {
    // Pro1
    var status = Shelly.getComponentStatus("switch:0");
    if (status) {
      STATE.activeOutput = status.output ? 0 : -1;
      saveState();
    }
  }
  
  log("Current active output:", STATE.activeOutput);
}

function init() {
  log("Script starting...");
  
  // Load configuration from KVS first (asynchronous)
  loadConfig(function(success) {
    if (!success) {
      log("FATAL: Configuration validation failed, script cannot start");
      return;
    }
    
    continueInit();
  });
}

function continueInit() {
  // Device type and ID are already detected in loadConfig
  // Just log them here for confirmation
  log('Device type:', STATE.deviceType);
  log('Device ID:', STATE.myDeviceId);
  log('Preferred device:', CONFIG.preferredDeviceId);

  configureComponentNames();
  loadState();
  enforceOutputState();
  setupMQTT();

  // Initialization complete - enable state persistence and flush initial state to KVS
  STATE.initializing = false;
  saveState();

  log("Script initialization complete");

  // Unified initialization for all devices
  var initSteps = [
    function(next) {
      log('Step 1/4: Disabling sys_btn_toggle...');
      Shelly.call('Sys.SetConfig', {config: {device: {sys_btn_toggle: false}}}, function(res, err) {
        if (err) {
          log('WARNING: Failed to disable sys_btn_toggle:', err);
          if (err && false) {}
        } else {
          log('sys_btn_toggle disabled (script handles button)');
        }
        next();
      });
    },
    function(next) {
      log('Step 2/4: Checking water supply status...');
      var input0 = Shelly.getComponentStatus('input:0');
      if (input0 && input0.state) {
        handleWaterSupply(true);
      }
      next();
    },
    function(next) {
      log('Step 3/4: Configuring component names...');
      applyComponentNames(next);
    },
    function(next) {
      log('Step 4/4: Verifying schedules...');
      // Only Pro3 has schedules, but all devices verify
      verifySchedules(next);
    }
  ];

  var stepIndex = 0;

  function runNextStep() {
    if (stepIndex >= initSteps.length) {
      log('✓ All initialization steps complete - script is now running');
      log('Current mode:', STATE.scheduleMode || 'winter');
      log('Should I run?', isMyTurnToRun());
      queueTask(handleDailyCheck);
      return;
    }

    var step = initSteps[stepIndex];
    stepIndex++;

    step(function() {
      queueTask(runNextStep);
    });
  }

  queueTask(runNextStep);
}

// === EVENT SUBSCRIPTION ===
Shelly.addEventHandler(function(event) {
  if (!event || !event.info) return;
  
  var info = event.info;
  
  // Handle script stop event
  if (info.event === "script_stop") {
    log("Script stopping");
    return;
  }
  
  // Handle component events
  if (typeof info.component === "string") {
    if (info.component.indexOf("switch:") === 0 && typeof info.state === "boolean") {
      handleSwitchEvent(info);
    } else if (info.component.indexOf("input:") === 0 && typeof info.state === "boolean") {
      handleInputEvent(info);
    } else if (info.component === "sys" && info.event === "sys_btn_push") {
      handleButtonEvent(info);
    } else {
      log("Unhandled component event:", JSON.stringify(info));
    }
  } else {
    log("Unhandled event:", JSON.stringify(info));
  }
});

// Start the script
init();